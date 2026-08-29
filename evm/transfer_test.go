package evm

import (
	"testing"
	"time"
)

// The three logs the Lux Genesis collection emitted when it minted, copied
// from eth_getLogs against C-Chain (chain 96369) at
// 0x9e04fc57c20b2ee45627c4aa280eb471f2ca6ea5. Real logs, so a decoder that
// only works on hand-written ones fails here.
var genesisMints = []Log{
	{
		TxHash: "0xac5b05e51c38ebfec88a743a0f2815d5082da105a679035ecea73a07e80717e3", BlockNumber: 1095748,
		Address: "0x9e04fc57c20b2ee45627c4aa280eb471f2ca6ea5",
		Topics: []string{
			"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
			"0x0000000000000000000000000000000000000000000000000000000000000000",
			"0x00000000000000000000000055be906ece0552797752e2894d9d7ef952414def",
			"0x0000000000000000000000000000000000000000000000000000000000000000",
		},
		Data: "0x",
	},
	{
		TxHash: "0x10f128e73ab9a1c0fedc9b459870a7cc5e8f45c2ee2a384f13be5ed7e3dbbb8a", BlockNumber: 1095749,
		Address: "0x9e04fc57c20b2ee45627c4aa280eb471f2ca6ea5",
		Topics: []string{
			"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
			"0x0000000000000000000000000000000000000000000000000000000000000000",
			"0x00000000000000000000000055be906ece0552797752e2894d9d7ef952414def",
			"0x0000000000000000000000000000000000000000000000000000000000000001",
		},
		Data: "0x",
	},
	{
		TxHash: "0xb5a96c04e0b9f8520bc6992d1e98e409eee0ca82f8f28460d304843a02ccff8d", BlockNumber: 1095750,
		Address: "0x9e04fc57c20b2ee45627c4aa280eb471f2ca6ea5",
		Topics: []string{
			"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
			"0x0000000000000000000000000000000000000000000000000000000000000000",
			"0x00000000000000000000000067bd7c7c3dbc53b8bde7d9888f90961d7af3d1a6",
			"0x0000000000000000000000000000000000000000000000000000000000000002",
		},
		Data: "0x",
	},
}

// The whole collection is three items with ids 0, 1 and 2. If the decoder
// drops the id, all three of these rows read alike and the collection has no
// items — the failure this test exists to catch.
func TestGenesisMintsKeepTheirIDs(t *testing.T) {
	wantOwner := []string{
		"0x55be906ece0552797752e2894d9d7ef952414def",
		"0x55be906ece0552797752e2894d9d7ef952414def",
		"0x67bd7c7c3dbc53b8bde7d9888f90961d7af3d1a6",
	}
	seen := map[string]bool{}
	for i, l := range genesisMints {
		got := decodeTransfers(l, time.Unix(0, 0))
		if len(got) != 1 {
			t.Fatalf("log %d: got %d transfers, want 1", i, len(got))
		}
		tr := got[0]
		if tr.TokenType != TypeERC721 {
			t.Errorf("log %d: type %q, want %q", i, tr.TokenType, TypeERC721)
		}
		if want := hexWord(string(rune('0' + i))); tr.TokenID != want {
			t.Errorf("log %d: id %q, want %q", i, tr.TokenID, want)
		}
		if tr.To != wantOwner[i] {
			t.Errorf("log %d: to %q, want %q", i, tr.To, wantOwner[i])
		}
		if tr.Value != "1" {
			t.Errorf("log %d: value %q, want 1", i, tr.Value)
		}
		if seen[tr.TokenID] {
			t.Errorf("log %d: id %q already seen — ids are being collapsed", i, tr.TokenID)
		}
		seen[tr.TokenID] = true
	}
}

// ERC-20 and ERC-721 Transfer share a signature, so topic0 cannot tell them
// apart. Arity can: the ERC-721 id is indexed and the ERC-20 amount is not.
func TestArityTellsTheTokenKindApart(t *testing.T) {
	const sig = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	from := "0x0000000000000000000000001111111111111111111111111111111111111111"
	to := "0x0000000000000000000000002222222222222222222222222222222222222222"
	amount := "0x0000000000000000000000000000000000000000000000000de0b6b3a7640000"

	fungible := decodeTransfers(Log{Topics: []string{sig, from, to}, Data: amount}, time.Unix(0, 0))
	if len(fungible) != 1 || fungible[0].TokenType != TypeERC20 {
		t.Fatalf("three topics should read as %s, got %+v", TypeERC20, fungible)
	}
	if fungible[0].Value != "1000000000000000000" {
		t.Errorf("amount %q, want decimal 1000000000000000000", fungible[0].Value)
	}
	if fungible[0].TokenID != "" {
		t.Errorf("an ERC-20 row carries no item id, got %q", fungible[0].TokenID)
	}

	item := decodeTransfers(Log{Topics: []string{sig, from, to, amount}, Data: "0x"}, time.Unix(0, 0))
	if len(item) != 1 || item[0].TokenType != TypeERC721 {
		t.Fatalf("four topics should read as %s, got %+v", TypeERC721, item)
	}
	if item[0].TokenID != amount {
		t.Errorf("id %q, want the fourth topic %q", item[0].TokenID, amount)
	}
}

// A batch moves several ids in one log. Folding them into a single row would
// lose every id but one.
func TestBatchYieldsARowPerItem(t *testing.T) {
	data := "0x" +
		"0000000000000000000000000000000000000000000000000000000000000040" + // ids offset
		"00000000000000000000000000000000000000000000000000000000000000a0" + // values offset
		"0000000000000000000000000000000000000000000000000000000000000002" + // len(ids)
		"0000000000000000000000000000000000000000000000000000000000000007" +
		"0000000000000000000000000000000000000000000000000000000000000008" +
		"0000000000000000000000000000000000000000000000000000000000000002" + // len(values)
		"0000000000000000000000000000000000000000000000000000000000000003" +
		"0000000000000000000000000000000000000000000000000000000000000004"

	got := decodeTransfers(Log{
		Topics: []string{
			TopicTransferBatch,
			"0x0000000000000000000000009999999999999999999999999999999999999999",
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		Data: data,
	}, time.Unix(0, 0))

	if len(got) != 2 {
		t.Fatalf("got %d rows, want one per id", len(got))
	}
	for i, want := range []struct{ id, value string }{{"7", "3"}, {"8", "4"}} {
		if got[i].TokenID != hexWord(want.id) {
			t.Errorf("[%d] id %q, want %q", i, got[i].TokenID, hexWord(want.id))
		}
		if got[i].Value != want.value {
			t.Errorf("[%d] amount %q, want %q", i, got[i].Value, want.value)
		}
		if got[i].TokenType != TypeERC1155 {
			t.Errorf("[%d] type %q, want %q", i, got[i].TokenType, TypeERC1155)
		}
	}
	if got[0].ID == got[1].ID {
		t.Errorf("both rows share the primary key %q — one would overwrite the other", got[0].ID)
	}
}

// hexWord is what makes an id from a topic and an id from an ABI array the
// same stored value.
func TestHexWordTakesEitherEncoding(t *testing.T) {
	want := "0x0000000000000000000000000000000000000000000000000000000000000042"
	for _, in := range []string{"66", "0x42", want} {
		if got := hexWord(in); got != want {
			t.Errorf("hexWord(%q) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{"", "  ", "0xzz", "-1"} {
		if got := hexWord(in); got != "" {
			t.Errorf("hexWord(%q) = %q, want empty", in, got)
		}
	}
}
