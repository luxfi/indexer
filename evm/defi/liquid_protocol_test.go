package defi

import (
	"math/big"
	"testing"
)

func TestLiquidProtocolTopicsLength(t *testing.T) {
	topics := LiquidProtocolTopics()
	if len(topics) != 17 {
		t.Fatalf("expected 17 liquid protocol topics, got %d", len(topics))
	}
	for _, topic := range topics {
		if len(topic) != 66 || topic[:2] != "0x" {
			t.Errorf("invalid topic format: %s", topic)
		}
	}
}

func TestParseLiquidFeesReceived(t *testing.T) {
	from := "0x000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	feeType := "0x4445580000000000000000000000000000000000000000000000000000000000"
	data := "0x" +
		"00000000000000000000000000000000000000000000003635c9adc5dea00000" + // amount=1000e18
		"0000000000000000000000000000000000000000000000056bc75e2d63100000" + // perfFee=100e18
		"0000000000000000000000000000000000000000000000008ac7230489e80000" // toReserve=10e18

	logs := []Log{{
		Address:     "0x7777777777777777777777777777777777777777",
		Topics:      []string{TopicLiquidFeesReceived, from, feeType},
		Data:        data,
		BlockNumber: 500,
		TxHash:      "0xfff",
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.Event != "fees_received" {
		t.Errorf("event = %s", e.Event)
	}
	if e.From != "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("from = %s", e.From)
	}
	if e.FeeType != feeType {
		t.Errorf("feeType = %s", e.FeeType)
	}

	expectedAmt := new(big.Int)
	expectedAmt.SetString("3635c9adc5dea00000", 16)
	if e.Amount.Cmp(expectedAmt) != 0 {
		t.Errorf("amount = %s, want %s", e.Amount, expectedAmt)
	}

	expectedFee := new(big.Int)
	expectedFee.SetString("56bc75e2d63100000", 16)
	if e.PerfFee.Cmp(expectedFee) != 0 {
		t.Errorf("perfFee = %s, want %s", e.PerfFee, expectedFee)
	}
}

func TestParseLiquidValidatorRewards(t *testing.T) {
	from := "0x000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	data := "0x0000000000000000000000000000000000000000000000056bc75e2d63100000"

	logs := []Log{{
		Topics:      []string{TopicLiquidValidatorRewards, from},
		Data:        data,
		BlockNumber: 510,
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "validator_rewards" {
		t.Fatal("expected validator_rewards event")
	}
	if events[0].From != "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("from = %s", events[0].From)
	}
}

func TestParseLiquidSlashing(t *testing.T) {
	data := "0x" +
		"0000000000000000000000000000000000000000000000056bc75e2d63100000" + // amount
		"000000000000000000000000000000000000000000000000016345785d8a0000" + // fromReserve
		"0000000000000000000000000000000000000000000000000de0b6b3a7640000" // socialized

	logs := []Log{{
		Topics: []string{TopicLiquidSlashing},
		Data:   data,
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "slashing" {
		t.Fatal("expected slashing event")
	}
	if events[0].FromReserve == nil || events[0].FromReserve.Sign() == 0 {
		t.Error("expected non-zero fromReserve")
	}
	if events[0].Socialized == nil || events[0].Socialized.Sign() == 0 {
		t.Error("expected non-zero socialized")
	}
}

func TestParseLiquidEmergencyWithdrawal(t *testing.T) {
	to := "0x000000000000000000000000cccccccccccccccccccccccccccccccccccccccc"
	data := "0x00000000000000000000000000000000000000000000003635c9adc5dea00000"

	logs := []Log{{
		Topics: []string{TopicLiquidEmergencyWithdrawal, to},
		Data:   data,
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "emergency_withdrawal" {
		t.Fatal("expected emergency_withdrawal event")
	}
}

func TestParseLiquidTokenPaused(t *testing.T) {
	minter := "0x0000000000000000000000001111111111111111111111111111111111111111"
	data := "0x0000000000000000000000000000000000000000000000000000000000000001" // true

	logs := []Log{{
		Topics: []string{TopicLiquidTokenPaused, minter},
		Data:   data,
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "paused" {
		t.Fatal("expected paused event")
	}
	if !events[0].State {
		t.Error("expected state=true")
	}
}

func TestParseLiquidTokenWhitelisted(t *testing.T) {
	minter := "0x0000000000000000000000002222222222222222222222222222222222222222"
	data := "0x0000000000000000000000000000000000000000000000000000000000000001"

	logs := []Log{{
		Topics: []string{TopicLiquidTokenWhitelisted, minter},
		Data:   data,
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "whitelisted" {
		t.Fatal("expected whitelisted event")
	}
	if events[0].Minter != "0x2222222222222222222222222222222222222222" {
		t.Errorf("minter = %s", events[0].Minter)
	}
}

func TestParseSetFlashMintFee(t *testing.T) {
	data := "0x0000000000000000000000000000000000000000000000000000000000000032" // 50 bps

	logs := []Log{{
		Topics: []string{TopicLiquidSetFlashMintFee},
		Data:   data,
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "set_flash_mint_fee" {
		t.Fatal("expected set_flash_mint_fee event")
	}
	if events[0].Fee.Int64() != 50 {
		t.Errorf("fee = %d, want 50", events[0].Fee.Int64())
	}
}

func TestParseDepositMinted(t *testing.T) {
	srcChain := "0x0000000000000000000000000000000000000000000000000000000000000001" // chain 1
	nonce := "0x0000000000000000000000000000000000000000000000000000000000000005"    // nonce 5
	recipient := "0x0000000000000000000000003333333333333333333333333333333333333333"
	data := "0x00000000000000000000000000000000000000000000003635c9adc5dea00000" // amount=1000e18

	logs := []Log{{
		Topics: []string{TopicDepositMinted, srcChain, nonce, recipient},
		Data:   data,
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "deposit_minted" {
		t.Fatal("expected deposit_minted event")
	}
	if events[0].SrcChainID.Int64() != 1 {
		t.Errorf("srcChainId = %d", events[0].SrcChainID.Int64())
	}
	if events[0].DepositNonce.Int64() != 5 {
		t.Errorf("depositNonce = %d", events[0].DepositNonce.Int64())
	}
	if events[0].Recipient != "0x3333333333333333333333333333333333333333" {
		t.Errorf("recipient = %s", events[0].Recipient)
	}
}

func TestParseYieldMinted(t *testing.T) {
	srcChain := "0x0000000000000000000000000000000000000000000000000000000000000001"
	yieldNonce := "0x000000000000000000000000000000000000000000000000000000000000000a"
	data := "0x0000000000000000000000000000000000000000000000056bc75e2d63100000"

	logs := []Log{{
		Topics: []string{TopicYieldMinted, srcChain, yieldNonce},
		Data:   data,
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "yield_minted" {
		t.Fatal("expected yield_minted event")
	}
	if events[0].YieldNonce.Int64() != 10 {
		t.Errorf("yieldNonce = %d", events[0].YieldNonce.Int64())
	}
}

func TestParseBurnedForWithdraw(t *testing.T) {
	user := "0x0000000000000000000000004444444444444444444444444444444444444444"
	withdrawNonce := "0x0000000000000000000000000000000000000000000000000000000000000003"
	data := "0x0000000000000000000000000000000000000000000000056bc75e2d63100000"

	logs := []Log{{
		Topics: []string{TopicBurnedForWithdraw, user, withdrawNonce},
		Data:   data,
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "burned_for_withdraw" {
		t.Fatal("expected burned_for_withdraw event")
	}
	if events[0].WithdrawNonce.Int64() != 3 {
		t.Errorf("withdrawNonce = %d", events[0].WithdrawNonce.Int64())
	}
}

func TestParseBackingUpdated(t *testing.T) {
	srcChain := "0x0000000000000000000000000000000000000000000000000000000000000001"
	data := "0x" +
		"00000000000000000000000000000000000000000000d3c21bcecceda1000000" + // totalBacking=1M e18
		"0000000000000000000000000000000000000000000000000000000065f5e100" // timestamp

	logs := []Log{{
		Topics: []string{TopicBackingUpdated, srcChain},
		Data:   data,
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "backing_updated" {
		t.Fatal("expected backing_updated event")
	}
	if events[0].TotalBacking.Sign() == 0 {
		t.Error("expected non-zero totalBacking")
	}
}

func TestParseMPCOracleSet(t *testing.T) {
	oracle := "0x0000000000000000000000005555555555555555555555555555555555555555"
	data := "0x0000000000000000000000000000000000000000000000000000000000000001"

	logs := []Log{{
		Topics: []string{TopicMPCOracleSet, oracle},
		Data:   data,
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "mpc_oracle_set" {
		t.Fatal("expected mpc_oracle_set event")
	}
	if !events[0].Active {
		t.Error("expected active=true")
	}
}

func TestParseStrategyAllocated(t *testing.T) {
	stratIdx := "0x0000000000000000000000000000000000000000000000000000000000000002"
	data := "0x00000000000000000000000000000000000000000000003635c9adc5dea00000"

	logs := []Log{{
		Topics: []string{TopicStrategyAllocated, stratIdx},
		Data:   data,
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "strategy_allocated" {
		t.Fatal("expected strategy_allocated event")
	}
	if events[0].StrategyIndex.Int64() != 2 {
		t.Errorf("strategyIndex = %d", events[0].StrategyIndex.Int64())
	}
}

func TestParseYieldHarvested(t *testing.T) {
	yieldNonce := "0x0000000000000000000000000000000000000000000000000000000000000007"
	data := "0x" +
		"0000000000000000000000000000000000000000000000056bc75e2d63100000" + // totalYield
		"0000000000000000000000000000000000000000000000000000000065f5e100" // timestamp

	logs := []Log{{
		Topics: []string{TopicYieldHarvested, yieldNonce},
		Data:   data,
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "yield_harvested" {
		t.Fatal("expected yield_harvested event")
	}
	if events[0].YieldNonce.Int64() != 7 {
		t.Errorf("yieldNonce = %d", events[0].YieldNonce.Int64())
	}
}

func TestParseStrategyAdded(t *testing.T) {
	idx := "0x0000000000000000000000000000000000000000000000000000000000000001"
	data := "0x0000000000000000000000006666666666666666666666666666666666666666"

	logs := []Log{{
		Topics: []string{TopicStrategyAdded, idx},
		Data:   data,
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "strategy_added" {
		t.Fatal("expected strategy_added event")
	}
	if events[0].Adapter != "0x6666666666666666666666666666666666666666" {
		t.Errorf("adapter = %s", events[0].Adapter)
	}
}

func TestParseStrategyRemoved(t *testing.T) {
	idx := "0x0000000000000000000000000000000000000000000000000000000000000003"

	logs := []Log{{
		Topics: []string{TopicStrategyRemoved, idx},
		Data:   "0x",
	}}

	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "strategy_removed" {
		t.Fatal("expected strategy_removed event")
	}
	if events[0].StrategyIndex.Int64() != 3 {
		t.Errorf("strategyIndex = %d", events[0].StrategyIndex.Int64())
	}
}

func TestLiquidProtocolEmptyLogs(t *testing.T) {
	events := ParseLiquidProtocolEvents(nil)
	if events != nil {
		t.Error("expected nil for nil input")
	}
}

func TestLiquidProtocolInsufficientTopics(t *testing.T) {
	logs := []Log{{
		Topics: []string{TopicLiquidFeesReceived}, // needs 3 topics
		Data:   "0x",
	}}
	events := ParseLiquidProtocolEvents(logs)
	if len(events) != 0 {
		t.Error("expected 0 events for insufficient topics")
	}
}

func TestLiquidProtocolTopicsUnique(t *testing.T) {
	seen := make(map[string]bool)
	for _, topic := range LiquidProtocolTopics() {
		if seen[topic] {
			t.Errorf("duplicate topic: %s", topic)
		}
		seen[topic] = true
	}
}
