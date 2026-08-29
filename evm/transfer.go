package evm

import (
	"fmt"
	"math/big"
	"strings"
	"time"
)

// decodeTransfers reads the token movements out of one log.
//
// It is the only place in this package that turns an event into a
// TokenTransfer. Both indexing paths call it — Indexer.indexBlock, which the
// daemon runs, and Adapter.ProcessBlock — so a chain fact has one reading
// regardless of which path saw the block.
//
// Telling ERC-20 from ERC-721 is the whole trick, and it is not the topic
// hash: Transfer(address,address,uint256) is the same signature for both, so
// topic0 is identical. The difference is arity. ERC-721 declares the id
// `indexed`, so it rides in topics[3] and the log carries four topics; ERC-20
// leaves the amount unindexed, so it rides in data and the log carries three.
// Read the arity and both token types stay right.
//
// ERC-1155 is a different event entirely — TransferSingle carries one
// (id, amount) pair in data, TransferBatch carries two arrays — so it needs
// no arity trick, only its own decoding.
//
// Three representation rules hold for every transfer this returns:
//
//	TokenType is hyphenated ("ERC-721"), the spelling the API and the
//	  explorer's TokenType union use.
//	Value is decimal. An amount is a number; the hex in the log is transport.
//	TokenID is the 32-byte hex word the chain encoded, which is how it is
//	  stored — see hexWord. The API converts it to decimal at the edge.
//
// Wrapper mint/burn (WETH Deposit/Withdrawal) is not a transfer event and is
// not decoded here; Adapter handles those on its own path.
func decodeTransfers(l Log, ts time.Time) []TokenTransfer {
	if len(l.Topics) == 0 {
		return nil
	}

	at := func(i int) string {
		if i < len(l.Topics) {
			return l.Topics[i]
		}
		return ""
	}
	one := func(kind, from, to, value, id string) []TokenTransfer {
		return []TokenTransfer{{
			ID:           fmt.Sprintf("%s-%d", l.TxHash, l.LogIndex),
			TxHash:       l.TxHash,
			LogIndex:     l.LogIndex,
			BlockNumber:  l.BlockNumber,
			TokenAddress: l.Address,
			TokenType:    kind,
			From:         from,
			To:           to,
			Value:        value,
			TokenID:      id,
			Timestamp:    ts,
		}}
	}

	switch l.Topics[0] {
	case TopicTransferERC20:
		switch len(l.Topics) {
		case 4:
			// ERC-721: the id is indexed, so it is topics[3].
			return one(TypeERC721, topicToAddress(at(1)), topicToAddress(at(2)), "1", hexWord(at(3)))
		case 3:
			// ERC-20: the amount is not indexed, so it is data.
			return one(TypeERC20, topicToAddress(at(1)), topicToAddress(at(2)), hexToBigInt(l.Data).String(), "")
		case 1:
			// Some early ERC-721s indexed nothing and put all three
			// arguments in data. Three words and nothing else is the
			// signature of that shape.
			d := strings.TrimPrefix(l.Data, "0x")
			if len(d) == 192 {
				return one(TypeERC721,
					topicToAddress("0x"+d[:64]), topicToAddress("0x"+d[64:128]),
					"1", hexWord("0x"+d[128:192]))
			}
		}

	case TopicTransferSingle:
		// TransferSingle(address operator, address from, address to,
		//                uint256 id, uint256 value) — operator is topics[1].
		d := strings.TrimPrefix(l.Data, "0x")
		if len(l.Topics) >= 4 && len(d) >= 128 {
			return one(TypeERC1155, topicToAddress(at(2)), topicToAddress(at(3)),
				hexToBigInt("0x"+d[64:128]).String(), hexWord("0x"+d[:64]))
		}

	case TopicTransferBatch:
		// TransferBatch(..., uint256[] ids, uint256[] values) — one movement
		// per pair, each needing its own row so no id is lost to the batch.
		if len(l.Topics) >= 4 {
			ids, values := decodeBatchData(l.Data)
			from, to := topicToAddress(at(2)), topicToAddress(at(3))
			out := make([]TokenTransfer, 0, len(ids))
			for i, id := range ids {
				v := "0"
				if i < len(values) {
					v = values[i]
				}
				t := one(TypeERC1155, from, to, v, hexWord(id))[0]
				t.ID = fmt.Sprintf("%s-%d-%d", l.TxHash, l.LogIndex, i)
				out = append(out, t)
			}
			return out
		}

	case TopicERC404ERC20Transfer:
		if len(l.Topics) >= 3 {
			return one(TypeERC20, topicToAddress(at(1)), topicToAddress(at(2)), hexToBigInt(l.Data).String(), "")
		}

	case TopicERC404ERC721Transfer:
		// from/to indexed, id in data.
		if len(l.Topics) >= 3 {
			return one(TypeERC721, topicToAddress(at(1)), topicToAddress(at(2)), "1", hexWord(l.Data))
		}
	}

	return nil
}

// Token type spellings. Hyphenated, because that is what the API emits and
// what luxfi/explore's TokenType union reads.
const (
	TypeERC20   = "ERC-20"
	TypeERC721  = "ERC-721"
	TypeERC1155 = "ERC-1155"
)

// hexWord renders a uint256 as the 32-byte hex word the chain encodes it as:
// "0x" and 64 lower-case hex digits. It accepts either form — a hex word
// straight off a topic, or the decimal an ABI array decoder produced — so a
// token id has one stored shape whichever event carried it.
//
// The stored shape is the chain's own encoding rather than decimal because
// evm_token_balances keys ownership on (token_address, address, token_id):
// a fixed-width word sorts numerically, cannot collide, and needs no
// canonicalization pass over rows already written. Readers convert to decimal
// at the API edge, where people see it.
func hexWord(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	n, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), base(s))
	if !ok || n.Sign() < 0 {
		return ""
	}
	return fmt.Sprintf("0x%064x", n)
}

func base(s string) int {
	if strings.HasPrefix(s, "0x") {
		return 16
	}
	return 10
}
