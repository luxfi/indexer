// Package defi provides EVM log parsers for DeFi protocol events.
package defi

import (
	"math/big"
	"strings"
)

// Liquidity Protocol event topic0 signatures (keccak256 hashes).
//
// Source contracts:
//   - contracts/liquidity/CrossChainDeFiRouter.sol
//   - contracts/liquidity/oma/OMARouter.sol
//   - contracts/liquidity/oma/LiquidityOracle.sol
//   - contracts/liquidity/dex/QuantumSwap.sol
//   - contracts/liquidity/bridges/IBridgeAggregator.sol
//   - contracts/liquidity/UniversalLiquidityRouter.sol
const (
	// CrossChainSwapInitiated(bytes32 indexed messageId, address indexed sender, uint256 srcChainId, uint256 dstChainId, address tokenIn, address tokenOut, uint256 amountIn, uint256 estimatedAmountOut)
	TopicCrossChainSwapInitiated = "0x5aaedcb0fb0c21816765e0b386414c2c85eeb7765fadfd7b7aa9b7f9d9abddf2"

	// CrossChainSwapCompleted(bytes32 indexed messageId, address indexed recipient, address tokenOut, uint256 amountOut, uint256 executionPrice)
	TopicCrossChainSwapCompleted = "0xeb74b510f8fc32c7f5b72af5efaa1488dd2f74ade0a466a92f21fc9b7cc169d3"

	// CrossChainLimitOrderPlaced(bytes32 indexed orderId, address indexed trader, uint256 dstChainId, uint256 limitPrice)
	TopicCrossChainLimitOrderPlaced = "0x91c234ac4f6ccb650ecaac76ec6b4a82bf396e93eb17d48b9b8c6af7f598c1e2"

	// ArbitrageExecuted(address indexed executor, uint256 profit, address[] tokens, uint256[] chainIds)
	TopicArbitrageExecuted = "0x010e8d8d5d71cf26ef51d7559461890175f20dd77dbb89117e0028bb3dc92342"

	// PoolAdded(address indexed pool, uint256 index)
	TopicOMAPoolAdded = "0x0881f4d3b99fd7463802bc8dfe78c3e826bd94c68121556d11b49e06f220dc78"

	// PoolRemoved(address indexed pool)
	TopicOMAPoolRemoved = "0x02b80da32f14fb41765f573f6d1c813bfed50d91af7f5ad0574a4f9bd270e4e9"

	// RouterSwap(address indexed user, string symbol, bool isBuy, uint256 amountIn, uint256 amountOut, uint256 poolUsed)
	TopicOMARouterSwap = "0xf29e26d254cd060d81fd68a5d2efe203d9e49370263ecd160a0f3bd1a1dce165"

	// PriceUpdated(bytes32 indexed symbolHash, uint256 price, uint256 timestamp)
	TopicPriceUpdated = "0x11efda7380f42c6625ecf15268120374ee099aeacaa5e274bec7f077d9b33c64"

	// PriceBatchUpdated(uint256 count, uint256 timestamp)
	TopicPriceBatchUpdated = "0xc27b551ca3c95d62aa00caf660ed02e5b93186e1919ba64d0da5ced66ab81a84"

	// StrategyOrderCreated(bytes32 indexed strategyId, address indexed trader, uint8 strategy, address tokenIn, address tokenOut, uint256 totalAmount)
	TopicStrategyOrderCreated = "0xa1a4e96a7d80a17112b5ecc47ff7b3074699c0df90b0e129d78c8e1ceeafae57"

	// StrategyOrderExecuted(bytes32 indexed strategyId, uint256 sliceFilled, uint256 slicePrice, uint256 remainingAmount)
	TopicStrategyOrderExecuted = "0x98a7391f1a8bf9ba961298845aa4df36db2d639b8f4b1735ad0396bfdaba91f5"

	// MarketMakingStarted(bytes32 indexed mmId, address indexed maker, address token0, address token1, uint256 spread)
	TopicMarketMakingStarted = "0x7b4e38181afcf221c4e7952a9131cd6319cdd0d5844e2c282b769e7603edfdf0"

	// MarketMakingQuote(bytes32 indexed mmId, uint256 bidPrice, uint256 bidSize, uint256 askPrice, uint256 askSize)
	TopicMarketMakingQuote = "0x0d4540fce44355fc16f26f689a9a62a5b4c2248b7bd909ed208abad67b49c22d"

	// BridgeInitiated(bytes32 indexed messageId, uint8 indexed protocol, uint256 srcChainId, uint256 dstChainId, address indexed sender, address recipient, address token, uint256 amount)
	TopicBridgeInitiated = "0x361f7ab6c28983fd8675e656733a3bec1d29dae3567998b3d6a39e094c0a3d86"

	// BridgeCompleted(bytes32 indexed messageId, uint8 indexed protocol, address indexed recipient, address token, uint256 amount)
	TopicBridgeCompleted = "0xc14ac049648829462fa6d9aa5d76efcfa4e6f3c67fe6273529889734f1ab4edb"

	// BridgeFailed(bytes32 indexed messageId, uint8 indexed protocol, string reason)
	TopicBridgeFailed = "0xe467f664c0e3537dd585564069ba0752ce99770dd8111d79843ed7a78a898b83"
)

// LiquidityProtocolTopics returns all Liquidity Protocol event topic0 strings.
func LiquidityProtocolTopics() []string {
	return []string{
		TopicCrossChainSwapInitiated,
		TopicCrossChainSwapCompleted,
		TopicCrossChainLimitOrderPlaced,
		TopicArbitrageExecuted,
		TopicOMAPoolAdded,
		TopicOMAPoolRemoved,
		TopicOMARouterSwap,
		TopicPriceUpdated,
		TopicPriceBatchUpdated,
		TopicStrategyOrderCreated,
		TopicStrategyOrderExecuted,
		TopicMarketMakingStarted,
		TopicMarketMakingQuote,
		TopicBridgeInitiated,
		TopicBridgeCompleted,
		TopicBridgeFailed,
	}
}

// OmniSwapEvent represents a parsed Liquidity Protocol event.
type OmniSwapEvent struct {
	Event    string // event name
	Contract string
	Block    uint64
	TxHash   string

	// CrossChainSwapInitiated / Completed
	MessageID  string // bytes32 hex
	Sender     string
	Recipient  string
	SrcChainID *big.Int
	DstChainID *big.Int
	TokenIn    string
	TokenOut   string
	AmountIn   *big.Int
	AmountOut  *big.Int
	ExecPrice  *big.Int

	// CrossChainLimitOrderPlaced
	OrderID    string
	Trader     string
	LimitPrice *big.Int

	// ArbitrageExecuted
	Executor string
	Profit   *big.Int

	// OMA Router
	Pool     string
	Symbol   string
	IsBuy    bool
	PoolUsed *big.Int

	// Oracle
	SymbolHash string
	Price      *big.Int
	Timestamp  *big.Int
	BatchCount *big.Int

	// QuantumSwap strategy
	StrategyID  string
	Strategy    uint8
	TotalAmt    *big.Int
	SliceFilled *big.Int
	SlicePrice  *big.Int
	Remaining   *big.Int

	// Market making
	MMID     string
	Maker    string
	Token0   string
	Token1   string
	Spread   *big.Int
	BidPrice *big.Int
	BidSize  *big.Int
	AskPrice *big.Int
	AskSize  *big.Int

	// Bridge
	Protocol uint8
	Token    string
	Amount   *big.Int
	Reason   string
}

// ParseOmniSwapEvents extracts Liquidity Protocol events from EVM logs.
func ParseOmniSwapEvents(logs []Log) []OmniSwapEvent {
	var events []OmniSwapEvent
	for i := range logs {
		l := &logs[i]
		if len(l.Topics) == 0 {
			continue
		}
		switch l.Topics[0] {
		case TopicCrossChainSwapInitiated:
			if e := parseCrossChainSwapInitiated(l); e != nil {
				events = append(events, *e)
			}
		case TopicCrossChainSwapCompleted:
			if e := parseCrossChainSwapCompleted(l); e != nil {
				events = append(events, *e)
			}
		case TopicCrossChainLimitOrderPlaced:
			if e := parseCCLimitOrderPlaced(l); e != nil {
				events = append(events, *e)
			}
		case TopicArbitrageExecuted:
			if e := parseArbitrageExecuted(l); e != nil {
				events = append(events, *e)
			}
		case TopicOMAPoolAdded:
			if e := parseOMAPoolAdded(l); e != nil {
				events = append(events, *e)
			}
		case TopicOMAPoolRemoved:
			if e := parseOMAPoolRemoved(l); e != nil {
				events = append(events, *e)
			}
		case TopicOMARouterSwap:
			if e := parseOMARouterSwap(l); e != nil {
				events = append(events, *e)
			}
		case TopicPriceUpdated:
			if e := parsePriceUpdated(l); e != nil {
				events = append(events, *e)
			}
		case TopicPriceBatchUpdated:
			if e := parsePriceBatchUpdated(l); e != nil {
				events = append(events, *e)
			}
		case TopicStrategyOrderCreated:
			if e := parseStrategyOrderCreated(l); e != nil {
				events = append(events, *e)
			}
		case TopicStrategyOrderExecuted:
			if e := parseStrategyOrderExecuted(l); e != nil {
				events = append(events, *e)
			}
		case TopicMarketMakingStarted:
			if e := parseMarketMakingStarted(l); e != nil {
				events = append(events, *e)
			}
		case TopicMarketMakingQuote:
			if e := parseMarketMakingQuote(l); e != nil {
				events = append(events, *e)
			}
		case TopicBridgeInitiated:
			if e := parseBridgeInitiated(l); e != nil {
				events = append(events, *e)
			}
		case TopicBridgeCompleted:
			if e := parseBridgeCompleted(l); e != nil {
				events = append(events, *e)
			}
		case TopicBridgeFailed:
			if e := parseBridgeFailed(l); e != nil {
				events = append(events, *e)
			}
		}
	}
	return events
}

// CrossChainSwapInitiated(bytes32 indexed messageId, address indexed sender, uint256 srcChainId, uint256 dstChainId, address tokenIn, address tokenOut, uint256 amountIn, uint256 estimatedAmountOut)
// data: srcChainId(0) | dstChainId(1) | tokenIn(2) | tokenOut(3) | amountIn(4) | estimatedAmountOut(5)
func parseCrossChainSwapInitiated(l *Log) *OmniSwapEvent {
	if len(l.Topics) < 3 || len(l.Data) < 386 { // "0x" + 6*64
		return nil
	}
	d := stripHexPrefix(l.Data)
	return &OmniSwapEvent{
		Event:      "cross_chain_swap_initiated",
		Contract:   l.Address,
		Block:      l.BlockNumber,
		TxHash:     l.TxHash,
		MessageID:  l.Topics[1],
		Sender:     topicAddress(l.Topics[2]),
		SrcChainID: decodeWordStr(d, 0),
		DstChainID: decodeWordStr(d, 1),
		TokenIn:    wordAddress(d, 2),
		TokenOut:   wordAddress(d, 3),
		AmountIn:   decodeWordStr(d, 4),
		AmountOut:  decodeWordStr(d, 5),
	}
}

// CrossChainSwapCompleted(bytes32 indexed messageId, address indexed recipient, address tokenOut, uint256 amountOut, uint256 executionPrice)
func parseCrossChainSwapCompleted(l *Log) *OmniSwapEvent {
	if len(l.Topics) < 3 || len(l.Data) < 194 { // "0x" + 3*64
		return nil
	}
	d := stripHexPrefix(l.Data)
	return &OmniSwapEvent{
		Event:     "cross_chain_swap_completed",
		Contract:  l.Address,
		Block:     l.BlockNumber,
		TxHash:    l.TxHash,
		MessageID: l.Topics[1],
		Recipient: topicAddress(l.Topics[2]),
		TokenOut:  wordAddress(d, 0),
		AmountOut: decodeWordStr(d, 1),
		ExecPrice: decodeWordStr(d, 2),
	}
}

// CrossChainLimitOrderPlaced(bytes32 indexed orderId, address indexed trader, uint256 dstChainId, uint256 limitPrice)
func parseCCLimitOrderPlaced(l *Log) *OmniSwapEvent {
	if len(l.Topics) < 3 || len(l.Data) < 130 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	return &OmniSwapEvent{
		Event:      "cross_chain_limit_order_placed",
		Contract:   l.Address,
		Block:      l.BlockNumber,
		TxHash:     l.TxHash,
		OrderID:    l.Topics[1],
		Trader:     topicAddress(l.Topics[2]),
		DstChainID: decodeWordStr(d, 0),
		LimitPrice: decodeWordStr(d, 1),
	}
}

// ArbitrageExecuted(address indexed executor, uint256 profit, address[] tokens, uint256[] chainIds)
func parseArbitrageExecuted(l *Log) *OmniSwapEvent {
	if len(l.Topics) < 2 || len(l.Data) < 66 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	return &OmniSwapEvent{
		Event:    "arbitrage_executed",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
		Executor: topicAddress(l.Topics[1]),
		Profit:   decodeWordStr(d, 0),
	}
}

// PoolAdded(address indexed pool, uint256 index)
func parseOMAPoolAdded(l *Log) *OmniSwapEvent {
	if len(l.Topics) < 2 || len(l.Data) < 66 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	return &OmniSwapEvent{
		Event:    "oma_pool_added",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
		Pool:     topicAddress(l.Topics[1]),
		PoolUsed: decodeWordStr(d, 0),
	}
}

// PoolRemoved(address indexed pool)
func parseOMAPoolRemoved(l *Log) *OmniSwapEvent {
	if len(l.Topics) < 2 {
		return nil
	}
	return &OmniSwapEvent{
		Event:    "oma_pool_removed",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
		Pool:     topicAddress(l.Topics[1]),
	}
}

// RouterSwap(address indexed user, string symbol, bool isBuy, uint256 amountIn, uint256 amountOut, uint256 poolUsed)
// Dynamic types: string encoded with offset. data: offset(0) | isBuy(1) | amountIn(2) | amountOut(3) | poolUsed(4) | strLen(5) | strData(6+)
// Simplified: parse the numeric fields at known offsets.
func parseOMARouterSwap(l *Log) *OmniSwapEvent {
	if len(l.Topics) < 2 || len(l.Data) < 322 { // 5 words min
		return nil
	}
	d := stripHexPrefix(l.Data)
	isBuy := decodeWordStr(d, 1).Sign() > 0
	return &OmniSwapEvent{
		Event:     "oma_router_swap",
		Contract:  l.Address,
		Block:     l.BlockNumber,
		TxHash:    l.TxHash,
		Sender:    topicAddress(l.Topics[1]),
		IsBuy:     isBuy,
		AmountIn:  decodeWordStr(d, 2),
		AmountOut: decodeWordStr(d, 3),
		PoolUsed:  decodeWordStr(d, 4),
	}
}

// PriceUpdated(bytes32 indexed symbolHash, uint256 price, uint256 timestamp)
func parsePriceUpdated(l *Log) *OmniSwapEvent {
	if len(l.Topics) < 2 || len(l.Data) < 130 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	return &OmniSwapEvent{
		Event:      "price_updated",
		Contract:   l.Address,
		Block:      l.BlockNumber,
		TxHash:     l.TxHash,
		SymbolHash: l.Topics[1],
		Price:      decodeWordStr(d, 0),
		Timestamp:  decodeWordStr(d, 1),
	}
}

// PriceBatchUpdated(uint256 count, uint256 timestamp)
func parsePriceBatchUpdated(l *Log) *OmniSwapEvent {
	if len(l.Data) < 130 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	return &OmniSwapEvent{
		Event:      "price_batch_updated",
		Contract:   l.Address,
		Block:      l.BlockNumber,
		TxHash:     l.TxHash,
		BatchCount: decodeWordStr(d, 0),
		Timestamp:  decodeWordStr(d, 1),
	}
}

// StrategyOrderCreated(bytes32 indexed strategyId, address indexed trader, uint8 strategy, address tokenIn, address tokenOut, uint256 totalAmount)
func parseStrategyOrderCreated(l *Log) *OmniSwapEvent {
	if len(l.Topics) < 3 || len(l.Data) < 258 { // 4 words
		return nil
	}
	d := stripHexPrefix(l.Data)
	return &OmniSwapEvent{
		Event:      "strategy_order_created",
		Contract:   l.Address,
		Block:      l.BlockNumber,
		TxHash:     l.TxHash,
		StrategyID: l.Topics[1],
		Trader:     topicAddress(l.Topics[2]),
		Strategy:   uint8(decodeWordStr(d, 0).Uint64()),
		TokenIn:    wordAddress(d, 1),
		TokenOut:   wordAddress(d, 2),
		TotalAmt:   decodeWordStr(d, 3),
	}
}

// StrategyOrderExecuted(bytes32 indexed strategyId, uint256 sliceFilled, uint256 slicePrice, uint256 remainingAmount)
func parseStrategyOrderExecuted(l *Log) *OmniSwapEvent {
	if len(l.Topics) < 2 || len(l.Data) < 194 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	return &OmniSwapEvent{
		Event:       "strategy_order_executed",
		Contract:    l.Address,
		Block:       l.BlockNumber,
		TxHash:      l.TxHash,
		StrategyID:  l.Topics[1],
		SliceFilled: decodeWordStr(d, 0),
		SlicePrice:  decodeWordStr(d, 1),
		Remaining:   decodeWordStr(d, 2),
	}
}

// MarketMakingStarted(bytes32 indexed mmId, address indexed maker, address token0, address token1, uint256 spread)
func parseMarketMakingStarted(l *Log) *OmniSwapEvent {
	if len(l.Topics) < 3 || len(l.Data) < 194 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	return &OmniSwapEvent{
		Event:    "market_making_started",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
		MMID:     l.Topics[1],
		Maker:    topicAddress(l.Topics[2]),
		Token0:   wordAddress(d, 0),
		Token1:   wordAddress(d, 1),
		Spread:   decodeWordStr(d, 2),
	}
}

// MarketMakingQuote(bytes32 indexed mmId, uint256 bidPrice, uint256 bidSize, uint256 askPrice, uint256 askSize)
func parseMarketMakingQuote(l *Log) *OmniSwapEvent {
	if len(l.Topics) < 2 || len(l.Data) < 258 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	return &OmniSwapEvent{
		Event:    "market_making_quote",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
		MMID:     l.Topics[1],
		BidPrice: decodeWordStr(d, 0),
		BidSize:  decodeWordStr(d, 1),
		AskPrice: decodeWordStr(d, 2),
		AskSize:  decodeWordStr(d, 3),
	}
}

// BridgeInitiated(bytes32 indexed messageId, uint8 indexed protocol, uint256 srcChainId, uint256 dstChainId, address indexed sender, address recipient, address token, uint256 amount)
func parseBridgeInitiated(l *Log) *OmniSwapEvent {
	if len(l.Topics) < 4 || len(l.Data) < 258 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	protocolBig := new(big.Int)
	protocolBig.SetString(strings.TrimPrefix(l.Topics[2], "0x"), 16)
	return &OmniSwapEvent{
		Event:      "bridge_initiated",
		Contract:   l.Address,
		Block:      l.BlockNumber,
		TxHash:     l.TxHash,
		MessageID:  l.Topics[1],
		Protocol:   uint8(protocolBig.Uint64()),
		Sender:     topicAddress(l.Topics[3]),
		SrcChainID: decodeWordStr(d, 0),
		DstChainID: decodeWordStr(d, 1),
		Recipient:  wordAddress(d, 2),
		Token:      wordAddress(d, 3),
		Amount:     decodeWordStr(d, 4),
	}
}

// BridgeCompleted(bytes32 indexed messageId, uint8 indexed protocol, address indexed recipient, address token, uint256 amount)
func parseBridgeCompleted(l *Log) *OmniSwapEvent {
	if len(l.Topics) < 4 || len(l.Data) < 130 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	protocolBig := new(big.Int)
	protocolBig.SetString(strings.TrimPrefix(l.Topics[2], "0x"), 16)
	return &OmniSwapEvent{
		Event:     "bridge_completed",
		Contract:  l.Address,
		Block:     l.BlockNumber,
		TxHash:    l.TxHash,
		MessageID: l.Topics[1],
		Protocol:  uint8(protocolBig.Uint64()),
		Recipient: topicAddress(l.Topics[3]),
		Token:     wordAddress(d, 0),
		Amount:    decodeWordStr(d, 1),
	}
}

// BridgeFailed(bytes32 indexed messageId, uint8 indexed protocol, string reason)
func parseBridgeFailed(l *Log) *OmniSwapEvent {
	if len(l.Topics) < 3 {
		return nil
	}
	protocolBig := new(big.Int)
	protocolBig.SetString(strings.TrimPrefix(l.Topics[2], "0x"), 16)
	return &OmniSwapEvent{
		Event:     "bridge_failed",
		Contract:  l.Address,
		Block:     l.BlockNumber,
		TxHash:    l.TxHash,
		MessageID: l.Topics[1],
		Protocol:  uint8(protocolBig.Uint64()),
	}
}

// helpers

func stripHexPrefix(s string) string {
	if len(s) >= 2 && s[:2] == "0x" {
		return s[2:]
	}
	return s
}

func decodeWordStr(hex string, idx int) *big.Int {
	start := idx * 64
	end := start + 64
	if end > len(hex) {
		return new(big.Int)
	}
	v := new(big.Int)
	v.SetString(hex[start:end], 16)
	return v
}

func wordAddress(hex string, idx int) string {
	start := idx*64 + 24 // skip 12 zero bytes (24 hex chars)
	end := start + 40
	if end > len(hex) {
		return ""
	}
	return "0x" + hex[start:end]
}
