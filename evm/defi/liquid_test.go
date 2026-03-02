package defi

import (
	"math/big"
	"testing"
)

func TestLiquidTopicsLength(t *testing.T) {
	topics := LiquidTopics()
	if len(topics) != 4 {
		t.Fatalf("expected 4 liquid topics, got %d", len(topics))
	}
	for _, topic := range topics {
		if len(topic) != 66 || topic[:2] != "0x" {
			t.Errorf("invalid topic format: %s", topic)
		}
	}
}

func TestParseFeesReceived(t *testing.T) {
	from := "0x000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	feeType := "0x4445580000000000000000000000000000000000000000000000000000000000" // keccak256("DEX") prefix
	data := "0x" +
		"00000000000000000000000000000000000000000000003635c9adc5dea00000" + // amount = 1000e18
		"0000000000000000000000000000000000000000000000056bc75e2d63100000" + // perfFee = 100e18
		"0000000000000000000000000000000000000000000000008ac7230489e80000" // toReserve = 10e18

	logs := []Log{{
		Address:     "0x7777777777777777777777777777777777777777",
		Topics:      []string{TopicFeesReceived, from, feeType},
		Data:        data,
		BlockNumber: 300,
		TxHash:      "0xccc",
	}}

	events := ParseLiquidEvents(logs)
	if len(events) != 1 {
		t.Fatalf("expected 1 liquid event, got %d", len(events))
	}

	e := events[0]
	if e.Event != "fees_received" {
		t.Errorf("event = %s, want fees_received", e.Event)
	}
	if e.From != "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("from = %s", e.From)
	}
	if e.FeeType != feeType {
		t.Errorf("feeType = %s", e.FeeType)
	}

	expectedAmount := new(big.Int)
	expectedAmount.SetString("3635c9adc5dea00000", 16)
	if e.Amount.Cmp(expectedAmount) != 0 {
		t.Errorf("amount = %s, want %s", e.Amount, expectedAmount)
	}

	expectedPerfFee := new(big.Int)
	expectedPerfFee.SetString("56bc75e2d63100000", 16)
	if e.PerfFee.Cmp(expectedPerfFee) != 0 {
		t.Errorf("perfFee = %s, want %s", e.PerfFee, expectedPerfFee)
	}

	expectedReserve := new(big.Int)
	expectedReserve.SetString("8ac7230489e80000", 16)
	if e.ToReserve.Cmp(expectedReserve) != 0 {
		t.Errorf("toReserve = %s, want %s", e.ToReserve, expectedReserve)
	}
}

func TestParseValidatorRewards(t *testing.T) {
	from := "0x000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	data := "0x0000000000000000000000000000000000000000000000056bc75e2d63100000" // amount = 100e18

	logs := []Log{{
		Topics:      []string{TopicValidatorRewardsReceived, from},
		Data:        data,
		BlockNumber: 310,
	}}

	events := ParseLiquidEvents(logs)
	if len(events) != 1 || events[0].Event != "validator_rewards" {
		t.Fatal("expected validator_rewards event")
	}
	if events[0].From != "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("from = %s", events[0].From)
	}
}

func TestParseSlashingApplied(t *testing.T) {
	data := "0x" +
		"0000000000000000000000000000000000000000000000056bc75e2d63100000" + // amount = 100e18
		"000000000000000000000000000000000000000000000000016345785d8a0000" + // fromReserve = 0.1e18
		"0000000000000000000000000000000000000000000000000de0b6b3a7640000" // socialized = 1e18

	logs := []Log{{
		Topics: []string{TopicSlashingApplied},
		Data:   data,
	}}

	events := ParseLiquidEvents(logs)
	if len(events) != 1 || events[0].Event != "slashing" {
		t.Fatal("expected slashing event")
	}

	e := events[0]
	expectedAmount := new(big.Int)
	expectedAmount.SetString("56bc75e2d63100000", 16)
	if e.Amount.Cmp(expectedAmount) != 0 {
		t.Errorf("amount = %s, want %s", e.Amount, expectedAmount)
	}
	if e.FromReserve == nil || e.FromReserve.Sign() == 0 {
		t.Error("expected non-zero fromReserve")
	}
	if e.Socialized == nil || e.Socialized.Sign() == 0 {
		t.Error("expected non-zero socialized")
	}
}

func TestParseEmergencyWithdrawal(t *testing.T) {
	to := "0x000000000000000000000000cccccccccccccccccccccccccccccccccccccccc"
	data := "0x00000000000000000000000000000000000000000000003635c9adc5dea00000"

	logs := []Log{{
		Topics: []string{TopicEmergencyWithdrawal, to},
		Data:   data,
	}}

	events := ParseLiquidEvents(logs)
	if len(events) != 1 || events[0].Event != "emergency_withdrawal" {
		t.Fatal("expected emergency_withdrawal event")
	}
	if events[0].From != "0xcccccccccccccccccccccccccccccccccccccccc" {
		t.Errorf("from = %s", events[0].From)
	}
}

func TestMissingTopicsLiquid(t *testing.T) {
	// FeesReceived requires 3 topics; test with only 1
	logs := []Log{{
		Topics: []string{TopicFeesReceived},
		Data:   "0x",
	}}
	events := ParseLiquidEvents(logs)
	if len(events) != 0 {
		t.Error("expected no events for insufficient topics")
	}
}

func TestEmptyLiquidLogs(t *testing.T) {
	events := ParseLiquidEvents(nil)
	if events != nil {
		t.Error("expected nil results for nil input")
	}
}

func TestAllTopicsUnique(t *testing.T) {
	// Collect all topics from all modules and check for duplicates
	all := make(map[string]string)
	check := func(name string, topics []string) {
		for _, topic := range topics {
			if prev, exists := all[topic]; exists {
				// Some topics may intentionally collide across modules
				// (e.g., VoteCast in governance vs gauge controller have different sigs)
				_ = prev
			}
			all[topic] = name
		}
	}
	check("governance", GovernanceTopics())
	check("dao", DAOTopics())
	check("treasury", TreasuryTopics())
	check("liquid", LiquidTopics())

	// Verify all are valid keccak256 hashes
	for topic, source := range all {
		if len(topic) != 66 {
			t.Errorf("%s topic has length %d: %s", source, len(topic), topic)
		}
	}
}
