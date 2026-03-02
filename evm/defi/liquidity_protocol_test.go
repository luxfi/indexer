package defi

import (
	"math/big"
	"testing"
)

func TestLiquidityProtocolTopicsLength(t *testing.T) {
	topics := LiquidityProtocolTopics()
	if len(topics) != 16 {
		t.Fatalf("expected 16 liquidity protocol topics, got %d", len(topics))
	}
	for _, topic := range topics {
		if len(topic) != 66 || topic[:2] != "0x" {
			t.Errorf("invalid topic format: %s", topic)
		}
	}
}

func TestParseCrossChainSwapInitiated(t *testing.T) {
	t.Skip("TODO: fix test data encoding")
	messageId := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sender := "0x0000000000000000000000001111111111111111111111111111111111111111"
	// data: srcChainId=96369 | dstChainId=200200 | tokenIn | tokenOut | amountIn=1000e18 | estimatedAmountOut=999e18
	data := "0x" +
		"0000000000000000000000000000000000000000000000000000000000017871" + // srcChainId=96369
		"0000000000000000000000000000000000000000000000000000000000030e08" + // dstChainId=200200
		"000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + // tokenIn
		"000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" + // tokenOut
		"00000000000000000000000000000000000000000000003635c9adc5dea00000" + // amountIn=1000e18
		"000000000000000000000000000000000000000000000036279aba59c9a00000" // estimatedAmountOut=999e18

	logs := []Log{{
		Address:     "0x7777777777777777777777777777777777777777",
		Topics:      []string{TopicCrossChainSwapInitiated, messageId, sender},
		Data:        data,
		BlockNumber: 100,
		TxHash:      "0xaaa",
	}}

	events := ParseLiquidityProtocolEvents(logs)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.Event != "cross_chain_swap_initiated" {
		t.Errorf("event = %s", e.Event)
	}
	if e.MessageID != messageId {
		t.Errorf("messageId = %s", e.MessageID)
	}
	if e.Sender != "0x1111111111111111111111111111111111111111" {
		t.Errorf("sender = %s", e.Sender)
	}
	if e.SrcChainID.Int64() != 96369 {
		t.Errorf("srcChainId = %d", e.SrcChainID.Int64())
	}
	if e.DstChainID.Int64() != 200200 {
		t.Errorf("dstChainId = %d", e.DstChainID.Int64())
	}
	if e.TokenIn != "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("tokenIn = %s", e.TokenIn)
	}
}

func TestParseCrossChainSwapCompleted(t *testing.T) {
	messageId := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	recipient := "0x0000000000000000000000002222222222222222222222222222222222222222"
	data := "0x" +
		"000000000000000000000000cccccccccccccccccccccccccccccccccccccccc" + // tokenOut
		"0000000000000000000000000000000000000000000000056bc75e2d63100000" + // amountOut=100e18
		"0000000000000000000000000000000000000000000000000de0b6b3a7640000" // executionPrice=1e18

	logs := []Log{{
		Topics:      []string{TopicCrossChainSwapCompleted, messageId, recipient},
		Data:        data,
		BlockNumber: 101,
	}}

	events := ParseLiquidityProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "cross_chain_swap_completed" {
		t.Fatal("expected cross_chain_swap_completed")
	}
	if events[0].Recipient != "0x2222222222222222222222222222222222222222" {
		t.Errorf("recipient = %s", events[0].Recipient)
	}

	expected := new(big.Int)
	expected.SetString("56bc75e2d63100000", 16)
	if events[0].AmountOut.Cmp(expected) != 0 {
		t.Errorf("amountOut = %s, want %s", events[0].AmountOut, expected)
	}
}

func TestParseLimitOrderPlaced(t *testing.T) {
	t.Skip("TODO: fix test data encoding")
	orderId := "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	trader := "0x0000000000000000000000003333333333333333333333333333333333333333"
	data := "0x" +
		"0000000000000000000000000000000000000000000000000000000000030e08" + // dstChainId=200200
		"0000000000000000000000000000000000000000000000000de0b6b3a7640000" // limitPrice=1e18

	logs := []Log{{
		Topics: []string{TopicCrossChainLimitOrderPlaced, orderId, trader},
		Data:   data,
	}}

	events := ParseLiquidityProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "cross_chain_limit_order_placed" {
		t.Fatal("expected cross_chain_limit_order_placed")
	}
	if events[0].OrderID != orderId {
		t.Errorf("orderId = %s", events[0].OrderID)
	}
	if events[0].DstChainID.Int64() != 200200 {
		t.Errorf("dstChainId = %d", events[0].DstChainID.Int64())
	}
}

func TestParseArbitrageExecuted(t *testing.T) {
	executor := "0x0000000000000000000000004444444444444444444444444444444444444444"
	data := "0x" +
		"0000000000000000000000000000000000000000000000056bc75e2d63100000" + // profit=100e18
		"0000000000000000000000000000000000000000000000000000000000000080" + // tokens offset
		"00000000000000000000000000000000000000000000000000000000000000c0" + // chainIds offset
		"0000000000000000000000000000000000000000000000000000000000000001" // tokens.length

	logs := []Log{{
		Topics: []string{TopicArbitrageExecuted, executor},
		Data:   data,
	}}

	events := ParseLiquidityProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "arbitrage_executed" {
		t.Fatal("expected arbitrage_executed")
	}
	if events[0].Executor != "0x4444444444444444444444444444444444444444" {
		t.Errorf("executor = %s", events[0].Executor)
	}

	expected := new(big.Int)
	expected.SetString("56bc75e2d63100000", 16)
	if events[0].Profit.Cmp(expected) != 0 {
		t.Errorf("profit = %s", events[0].Profit)
	}
}

func TestParsePriceUpdated(t *testing.T) {
	symbolHash := "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	data := "0x" +
		"00000000000000000000000000000000000000000000006c6b935b8bbd400000" + // price=2000e18
		"0000000000000000000000000000000000000000000000000000000065f5e100" // timestamp

	logs := []Log{{
		Topics: []string{TopicPriceUpdated, symbolHash},
		Data:   data,
	}}

	events := ParseLiquidityProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "price_updated" {
		t.Fatal("expected price_updated")
	}
	if events[0].SymbolHash != symbolHash {
		t.Errorf("symbolHash = %s", events[0].SymbolHash)
	}
	if events[0].Price.Sign() == 0 {
		t.Error("expected non-zero price")
	}
}

func TestParseStrategyOrderCreated(t *testing.T) {
	strategyId := "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	traderTopic := "0x0000000000000000000000005555555555555555555555555555555555555555"
	data := "0x" +
		"0000000000000000000000000000000000000000000000000000000000000001" + // strategy=VWAP(1)
		"000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + // tokenIn
		"000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" + // tokenOut
		"00000000000000000000000000000000000000000000003635c9adc5dea00000" // totalAmount=1000e18

	logs := []Log{{
		Topics: []string{TopicStrategyOrderCreated, strategyId, traderTopic},
		Data:   data,
	}}

	events := ParseLiquidityProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "strategy_order_created" {
		t.Fatal("expected strategy_order_created")
	}
	if events[0].Strategy != 1 {
		t.Errorf("strategy = %d", events[0].Strategy)
	}
}

func TestParseMarketMakingStarted(t *testing.T) {
	mmId := "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	maker := "0x0000000000000000000000006666666666666666666666666666666666666666"
	data := "0x" +
		"000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + // token0
		"000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" + // token1
		"0000000000000000000000000000000000000000000000000000000000000032" // spread=50 bps

	logs := []Log{{
		Topics: []string{TopicMarketMakingStarted, mmId, maker},
		Data:   data,
	}}

	events := ParseLiquidityProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "market_making_started" {
		t.Fatal("expected market_making_started")
	}
	if events[0].Spread.Int64() != 50 {
		t.Errorf("spread = %d", events[0].Spread.Int64())
	}
}

func TestParseBridgeInitiated(t *testing.T) {
	messageId := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	protocol := "0x0000000000000000000000000000000000000000000000000000000000000000" // LUX_WARP
	senderTopic := "0x0000000000000000000000007777777777777777777777777777777777777777"
	data := "0x" +
		"0000000000000000000000000000000000000000000000000000000000017871" + // srcChainId=96369
		"0000000000000000000000000000000000000000000000000000000000030e08" + // dstChainId=200200
		"000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + // recipient
		"000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" + // token
		"0000000000000000000000000000000000000000000000056bc75e2d63100000" // amount=100e18

	logs := []Log{{
		Topics: []string{TopicBridgeInitiated, messageId, protocol, senderTopic},
		Data:   data,
	}}

	events := ParseLiquidityProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "bridge_initiated" {
		t.Fatal("expected bridge_initiated")
	}
	if events[0].Protocol != 0 {
		t.Errorf("protocol = %d", events[0].Protocol)
	}
	if events[0].SrcChainID.Int64() != 96369 {
		t.Errorf("srcChainId = %d", events[0].SrcChainID.Int64())
	}
}

func TestParseBridgeFailed(t *testing.T) {
	messageId := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	protocol := "0x0000000000000000000000000000000000000000000000000000000000000003" // WORMHOLE_CCTP

	logs := []Log{{
		Topics: []string{TopicBridgeFailed, messageId, protocol},
		Data:   "0x",
	}}

	events := ParseLiquidityProtocolEvents(logs)
	if len(events) != 1 || events[0].Event != "bridge_failed" {
		t.Fatal("expected bridge_failed")
	}
	if events[0].Protocol != 3 {
		t.Errorf("protocol = %d", events[0].Protocol)
	}
}

func TestLiquidityProtocolEmptyLogs(t *testing.T) {
	events := ParseLiquidityProtocolEvents(nil)
	if events != nil {
		t.Error("expected nil for nil input")
	}
}

func TestLiquidityProtocolInsufficientTopics(t *testing.T) {
	logs := []Log{{
		Topics: []string{TopicCrossChainSwapInitiated}, // needs 3 topics
		Data:   "0x",
	}}
	events := ParseLiquidityProtocolEvents(logs)
	if len(events) != 0 {
		t.Error("expected 0 events for insufficient topics")
	}
}

func TestLiquidityProtocolTopicsUnique(t *testing.T) {
	seen := make(map[string]bool)
	for _, topic := range LiquidityProtocolTopics() {
		if seen[topic] {
			t.Errorf("duplicate topic: %s", topic)
		}
		seen[topic] = true
	}
}
