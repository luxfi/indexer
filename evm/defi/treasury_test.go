package defi

import (
	"math/big"
	"testing"
)

func TestTreasuryTopicsLength(t *testing.T) {
	topics := TreasuryTopics()
	if len(topics) != 8 {
		t.Fatalf("expected 8 treasury topics, got %d", len(topics))
	}
	for _, topic := range topics {
		if len(topic) != 66 || topic[:2] != "0x" {
			t.Errorf("invalid topic format: %s", topic)
		}
	}
}

func TestParseVaultReceive(t *testing.T) {
	chain := "0x0000000000000000000000000000000000000000000000000000000000000001"
	data := "0x" +
		"00000000000000000000000000000000000000000000003635c9adc5dea00000" + // amount
		"abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789" // warpId

	logs := []Log{{
		Address:     "0x6666666666666666666666666666666666666666",
		Topics:      []string{TopicVaultReceive, chain},
		Data:        data,
		BlockNumber: 200,
		TxHash:      "0xbbb",
	}}

	vaults, _, _ := ParseTreasuryEvents(logs)
	if len(vaults) != 1 {
		t.Fatalf("expected 1 vault event, got %d", len(vaults))
	}
	v := vaults[0]
	if v.Event != "receive" {
		t.Errorf("event = %s, want receive", v.Event)
	}
	if v.Chain != chain {
		t.Errorf("chain = %s", v.Chain)
	}
	expected := new(big.Int)
	expected.SetString("3635c9adc5dea00000", 16)
	if v.Amount.Cmp(expected) != 0 {
		t.Errorf("amount = %s, want %s", v.Amount, expected)
	}
	if v.WarpID == "" || v.WarpID == "0x0" {
		t.Error("expected non-zero warpId")
	}
}

func TestParseVaultFlush(t *testing.T) {
	chain := "0x0000000000000000000000000000000000000000000000000000000000000002"
	data := "0x0000000000000000000000000000000000000000000000056bc75e2d63100000" // amount

	logs := []Log{{
		Topics: []string{TopicVaultFlush, chain},
		Data:   data,
	}}

	vaults, _, _ := ParseTreasuryEvents(logs)
	if len(vaults) != 1 || vaults[0].Event != "flush" {
		t.Fatal("expected flush event")
	}
	if vaults[0].Chain != chain {
		t.Errorf("chain = %s", vaults[0].Chain)
	}
}

func TestParseFeeGovRate(t *testing.T) {
	data := "0x" +
		"000000000000000000000000000000000000000000000000000000000000001e" + // rate = 30 bps
		"0000000000000000000000000000000000000000000000000000000000000005" // version = 5

	logs := []Log{{
		Topics: []string{TopicFeeGovRate},
		Data:   data,
	}}

	_, feegovs, _ := ParseTreasuryEvents(logs)
	if len(feegovs) != 1 || feegovs[0].Event != "rate" {
		t.Fatal("expected rate event")
	}
	if feegovs[0].Rate != 30 {
		t.Errorf("rate = %d, want 30", feegovs[0].Rate)
	}
	if feegovs[0].Version != 5 {
		t.Errorf("version = %d, want 5", feegovs[0].Version)
	}
}

func TestParseFeeGovChain(t *testing.T) {
	chainID := "0xabcdef0000000000000000000000000000000000000000000000000000000000"
	data := "0x0000000000000000000000000000000000000000000000000000000000000001" // active = true

	logs := []Log{{
		Topics: []string{TopicFeeGovChain, chainID},
		Data:   data,
	}}

	_, feegovs, _ := ParseTreasuryEvents(logs)
	if len(feegovs) != 1 || feegovs[0].Event != "chain" {
		t.Fatal("expected chain event")
	}
	if !feegovs[0].Active {
		t.Error("expected active = true")
	}
	if feegovs[0].ChainID != chainID {
		t.Errorf("chainID = %s", feegovs[0].ChainID)
	}
}

func TestParseFeeGovBroadcast(t *testing.T) {
	data := "0x" +
		"0000000000000000000000000000000000000000000000000000000000000003" + // version = 3
		"0000000000000000000000000000000000000000000000000000000000000005" // count = 5

	logs := []Log{{
		Topics: []string{TopicFeeGovBroadcast},
		Data:   data,
	}}

	_, feegovs, _ := ParseTreasuryEvents(logs)
	if len(feegovs) != 1 || feegovs[0].Event != "broadcast" {
		t.Fatal("expected broadcast event")
	}
	if feegovs[0].Version != 3 {
		t.Errorf("version = %d, want 3", feegovs[0].Version)
	}
	if feegovs[0].Count.Cmp(big.NewInt(5)) != 0 {
		t.Errorf("count = %s, want 5", feegovs[0].Count)
	}
}

func TestParseRouterWeight(t *testing.T) {
	recipient := "0x000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	data := "0x0000000000000000000000000000000000000000000000000000000000001388" // weight = 5000

	logs := []Log{{
		Topics: []string{TopicRouterWeight, recipient},
		Data:   data,
	}}

	_, _, routers := ParseTreasuryEvents(logs)
	if len(routers) != 1 || routers[0].Event != "weight" {
		t.Fatal("expected weight event")
	}
	if routers[0].Recipient != "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("recipient = %s", routers[0].Recipient)
	}
	if routers[0].Weight.Cmp(big.NewInt(5000)) != 0 {
		t.Errorf("weight = %s, want 5000", routers[0].Weight)
	}
}

func TestParseRouterDistribute(t *testing.T) {
	data := "0x00000000000000000000000000000000000000000000003635c9adc5dea00000"

	logs := []Log{{
		Topics: []string{TopicRouterDistribute},
		Data:   data,
	}}

	_, _, routers := ParseTreasuryEvents(logs)
	if len(routers) != 1 || routers[0].Event != "distribute" {
		t.Fatal("expected distribute event")
	}
}

func TestParseRouterClaim(t *testing.T) {
	recipient := "0x000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	data := "0x0000000000000000000000000000000000000000000000056bc75e2d63100000" // amount

	logs := []Log{{
		Topics: []string{TopicRouterClaim, recipient},
		Data:   data,
	}}

	_, _, routers := ParseTreasuryEvents(logs)
	if len(routers) != 1 || routers[0].Event != "claim" {
		t.Fatal("expected claim event")
	}
	if routers[0].Recipient != "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("recipient = %s", routers[0].Recipient)
	}
}

func TestEmptyTreasuryLogs(t *testing.T) {
	vaults, feegovs, routers := ParseTreasuryEvents(nil)
	if vaults != nil || feegovs != nil || routers != nil {
		t.Error("expected nil results for nil input")
	}
}
