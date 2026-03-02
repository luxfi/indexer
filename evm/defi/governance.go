// Package defi provides EVM log parsers for DeFi protocol events.
package defi

import (
	"math/big"
	"strings"
)

// Governance event topic0 signatures (keccak256 hashes).
// Source: contracts/governance/DAO.sol, Governor.sol (OpenZeppelin IGovernor)
const (
	// ProposalCreated(uint256,address,address[],uint256[],bytes[],uint256,uint256,string)
	TopicProposalCreated = "0x95f03e437e6d5037418f12bef80fc7e0a5f27754c078a1d5d3f62d39bac44e50"

	// VoteCast(address indexed,uint256,uint8,uint256,string)
	TopicVoteCast = "0xb8e138887d0aa13bab447e82de9d5c1777041ecd21ca36ba824ff1e6c07ddda4"

	// ProposalExecuted(uint256)
	TopicProposalExecuted = "0x712ae1383f79ac853f8d882153778e0260ef8f03b504e2866e0593e04d2b291f"

	// ProposalCanceled(uint256)
	TopicProposalCanceled = "0x789cf55be980739dad1d0699b93b58e806b51c9d96619bfa8fe0a28abaa7b30c"

	// ProposalQueued(uint256,uint256)
	TopicProposalQueued = "0x9a2e42fd6722813d69113e7d0079d3d940171428df7373df9c7f7617cfda2892"

	// DelegateChanged(address indexed,address indexed,address indexed)
	TopicDelegateChanged = "0x3134e8a2e6d97e929a7e54011ea5485d7d196dd5f0ba4d4ef95803e8e3fc257f"

	// DelegateVotesChanged(address indexed,uint256,uint256)
	TopicDelegateVotesChanged = "0xdec2bacdd2f05b59de34da9b523dff8be42e5e38e818c82fdb0bae774387a724"

	// GuardianUpdated(address,address)
	TopicGuardianUpdated = "0x064d28d3d3071c5cbc271a261c10c2f0f0d9e319390397101aa0eb23c6bad909"
)

// GovernanceTopics returns all governance event topic0 strings.
func GovernanceTopics() []string {
	return []string{
		TopicProposalCreated,
		TopicVoteCast,
		TopicProposalExecuted,
		TopicProposalCanceled,
		TopicProposalQueued,
		TopicDelegateChanged,
		TopicDelegateVotesChanged,
		TopicGuardianUpdated,
	}
}

// Log is a decoded EVM log entry. Matches the shape used by the indexer package.
type Log struct {
	Address     string
	Topics      []string
	Data        string
	BlockNumber uint64
	TxHash      string
	LogIndex    string
}

// GovernanceProposal represents a parsed proposal lifecycle event.
type GovernanceProposal struct {
	ProposalID *big.Int
	Proposer   string
	StartBlock *big.Int
	EndBlock   *big.Int
	ETA        *big.Int // queued only
	Event      string   // "created", "executed", "canceled", "queued"
	Contract   string
	Block      uint64
	TxHash     string
}

// Vote represents a parsed VoteCast event.
type Vote struct {
	Voter      string
	ProposalID *big.Int
	Support    uint8 // 0=Against, 1=For, 2=Abstain
	Weight     *big.Int
	Contract   string
	Block      uint64
	TxHash     string
}

// DelegateChange represents a DelegateChanged event.
type DelegateChange struct {
	Delegator    string
	FromDelegate string
	ToDelegate   string
	Contract     string
	Block        uint64
	TxHash       string
}

// DelegateVotesChange represents a DelegateVotesChanged event.
type DelegateVotesChange struct {
	Delegate        string
	PreviousBalance *big.Int
	NewBalance      *big.Int
	Contract        string
	Block           uint64
	TxHash          string
}

// ParseGovernanceEvents extracts governance proposals, votes, and delegation
// changes from a slice of EVM logs.
func ParseGovernanceEvents(logs []Log) ([]GovernanceProposal, []Vote, []DelegateChange, []DelegateVotesChange) {
	var proposals []GovernanceProposal
	var votes []Vote
	var delegates []DelegateChange
	var dvotes []DelegateVotesChange

	for i := range logs {
		l := &logs[i]
		if len(l.Topics) == 0 {
			continue
		}
		switch l.Topics[0] {
		case TopicProposalCreated:
			p := parseProposalCreated(l)
			if p != nil {
				proposals = append(proposals, *p)
			}
		case TopicVoteCast:
			v := parseVoteCast(l)
			if v != nil {
				votes = append(votes, *v)
			}
		case TopicProposalExecuted:
			p := parseProposalSimple(l, "executed")
			if p != nil {
				proposals = append(proposals, *p)
			}
		case TopicProposalCanceled:
			p := parseProposalSimple(l, "canceled")
			if p != nil {
				proposals = append(proposals, *p)
			}
		case TopicProposalQueued:
			p := parseProposalQueued(l)
			if p != nil {
				proposals = append(proposals, *p)
			}
		case TopicDelegateChanged:
			d := parseDelegateChanged(l)
			if d != nil {
				delegates = append(delegates, *d)
			}
		case TopicDelegateVotesChanged:
			d := parseDelegateVotesChanged(l)
			if d != nil {
				dvotes = append(dvotes, *d)
			}
		}
	}
	return proposals, votes, delegates, dvotes
}

func parseProposalCreated(l *Log) *GovernanceProposal {
	// ProposalCreated is not indexed beyond topic0; data has all fields.
	// data layout: proposalId(0), proposer(1), ... startBlock(5), endBlock(6), ...
	proposalID := decodeWord(l.Data, 0)
	proposer := decodeAddress(l.Data, 1)
	startBlock := decodeWord(l.Data, 5)
	endBlock := decodeWord(l.Data, 6)
	return &GovernanceProposal{
		ProposalID: proposalID,
		Proposer:   proposer,
		StartBlock: startBlock,
		EndBlock:   endBlock,
		Event:      "created",
		Contract:   l.Address,
		Block:      l.BlockNumber,
		TxHash:     l.TxHash,
	}
}

func parseVoteCast(l *Log) *Vote {
	// VoteCast(address indexed voter, uint256 proposalId, uint8 support, uint256 votes, string reason)
	if len(l.Topics) < 2 {
		return nil
	}
	voter := topicAddress(l.Topics[1])
	proposalID := decodeWord(l.Data, 0)
	support := decodeWord(l.Data, 1)
	weight := decodeWord(l.Data, 2)
	var s uint8
	if support != nil && support.IsUint64() {
		s = uint8(support.Uint64())
	}
	return &Vote{
		Voter:      voter,
		ProposalID: proposalID,
		Support:    s,
		Weight:     weight,
		Contract:   l.Address,
		Block:      l.BlockNumber,
		TxHash:     l.TxHash,
	}
}

func parseProposalSimple(l *Log, event string) *GovernanceProposal {
	proposalID := decodeWord(l.Data, 0)
	return &GovernanceProposal{
		ProposalID: proposalID,
		Event:      event,
		Contract:   l.Address,
		Block:      l.BlockNumber,
		TxHash:     l.TxHash,
	}
}

func parseProposalQueued(l *Log) *GovernanceProposal {
	proposalID := decodeWord(l.Data, 0)
	eta := decodeWord(l.Data, 1)
	return &GovernanceProposal{
		ProposalID: proposalID,
		ETA:        eta,
		Event:      "queued",
		Contract:   l.Address,
		Block:      l.BlockNumber,
		TxHash:     l.TxHash,
	}
}

func parseDelegateChanged(l *Log) *DelegateChange {
	// DelegateChanged(address indexed delegator, address indexed fromDelegate, address indexed toDelegate)
	if len(l.Topics) < 4 {
		return nil
	}
	return &DelegateChange{
		Delegator:    topicAddress(l.Topics[1]),
		FromDelegate: topicAddress(l.Topics[2]),
		ToDelegate:   topicAddress(l.Topics[3]),
		Contract:     l.Address,
		Block:        l.BlockNumber,
		TxHash:       l.TxHash,
	}
}

func parseDelegateVotesChanged(l *Log) *DelegateVotesChange {
	// DelegateVotesChanged(address indexed delegate, uint256 previousBalance, uint256 newBalance)
	if len(l.Topics) < 2 {
		return nil
	}
	return &DelegateVotesChange{
		Delegate:        topicAddress(l.Topics[1]),
		PreviousBalance: decodeWord(l.Data, 0),
		NewBalance:      decodeWord(l.Data, 1),
		Contract:        l.Address,
		Block:           l.BlockNumber,
		TxHash:          l.TxHash,
	}
}

// decodeWord reads a 32-byte word from hex data at the given word index.
func decodeWord(data string, wordIndex int) *big.Int {
	data = strings.TrimPrefix(data, "0x")
	start := wordIndex * 64
	if start+64 > len(data) {
		return new(big.Int)
	}
	n := new(big.Int)
	n.SetString(data[start:start+64], 16)
	return n
}

// decodeAddress reads an address from the low 20 bytes of a 32-byte word.
func decodeAddress(data string, wordIndex int) string {
	data = strings.TrimPrefix(data, "0x")
	start := wordIndex*64 + 24 // skip 12 bytes of zero padding
	if start+40 > len(data) {
		return ""
	}
	return "0x" + data[start:start+40]
}

// topicAddress extracts an address from an indexed topic (last 40 hex chars).
func topicAddress(topic string) string {
	if len(topic) >= 42 {
		return "0x" + topic[len(topic)-40:]
	}
	return topic
}
