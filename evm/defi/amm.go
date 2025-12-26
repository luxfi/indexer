// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package defi

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// AMMIndexer indexes AMM (Automated Market Maker) events
// Supports both Uniswap V2-style and V3-style concentrated liquidity AMMs
type AMMIndexer struct {
	pools       map[string]*Pool         // address -> pool
	swaps       []*Swap
	liquidity   []*LiquidityEvent
	positions   map[string]*Position     // positionId -> position (V3 only)
	factories   map[string]ProtocolType  // factory address -> protocol type
	onSwap      func(*Swap)
	onLiquidity func(*LiquidityEvent)
	onPoolCreated func(*Pool)
}

// NewAMMIndexer creates a new AMM indexer
func NewAMMIndexer() *AMMIndexer {
	return &AMMIndexer{
		pools:     make(map[string]*Pool),
		swaps:     make([]*Swap, 0),
		liquidity: make([]*LiquidityEvent, 0),
		positions: make(map[string]*Position),
		factories: make(map[string]ProtocolType),
	}
}

// RegisterFactory registers an AMM factory address
func (a *AMMIndexer) RegisterFactory(address string, protocol ProtocolType) {
	a.factories[strings.ToLower(address)] = protocol
}

// SetCallbacks sets event callbacks
func (a *AMMIndexer) SetCallbacks(onSwap func(*Swap), onLiquidity func(*LiquidityEvent), onPoolCreated func(*Pool)) {
	a.onSwap = onSwap
	a.onLiquidity = onLiquidity
	a.onPoolCreated = onPoolCreated
}

// IndexLog processes a log entry for AMM events
func (a *AMMIndexer) IndexLog(ctx context.Context, log *LogEntry) error {
	if len(log.Topics) == 0 {
		return nil
	}
	
	topic0 := log.Topics[0]
	
	switch topic0 {
	// V2 Events
	case AMMV2SwapSig:
		return a.indexV2Swap(log)
	case AMMV2MintSig:
		return a.indexV2Mint(log)
	case AMMV2BurnSig:
		return a.indexV2Burn(log)
	case AMMV2SyncSig:
		return a.indexV2Sync(log)
	case AMMV2PairCreatedSig:
		return a.indexV2PairCreated(log)
		
	// V3 Events
	case AMMV3SwapSig:
		return a.indexV3Swap(log)
	case AMMV3MintSig:
		return a.indexV3Mint(log)
	case AMMV3BurnSig:
		return a.indexV3Burn(log)
	case AMMV3CollectSig:
		return a.indexV3Collect(log)
	case AMMV3FlashSig:
		return a.indexV3Flash(log)
	case AMMV3InitializeSig:
		return a.indexV3Initialize(log)
	case AMMV3PoolCreatedSig:
		return a.indexV3PoolCreated(log)
	}
	
	return nil
}

// LogEntry represents a log to process
type LogEntry struct {
	Address     string
	Topics      []string
	Data        string
	BlockNumber uint64
	TxHash      string
	LogIndex    uint64
	Timestamp   time.Time
}

// indexV2Swap processes a V2 Swap event
func (a *AMMIndexer) indexV2Swap(log *LogEntry) error {
	// Swap(address indexed sender, uint amount0In, uint amount1In, uint amount0Out, uint amount1Out, address indexed to)
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid V2 swap event: insufficient topics")
	}
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 128 {
		return fmt.Errorf("invalid V2 swap event: insufficient data")
	}
	
	amount0In := new(big.Int).SetBytes(data[0:32])
	amount1In := new(big.Int).SetBytes(data[32:64])
	amount0Out := new(big.Int).SetBytes(data[64:96])
	amount1Out := new(big.Int).SetBytes(data[96:128])
	
	sender := topicToAddress(log.Topics[1])
	recipient := topicToAddress(log.Topics[2])
	
	pool := a.getOrCreatePool(log.Address, ProtocolAMMV2)
	
	// Determine token direction
	var tokenIn, tokenOut string
	var amountIn, amountOut *big.Int
	
	if amount0In.Sign() > 0 {
		tokenIn = pool.Token0
		tokenOut = pool.Token1
		amountIn = amount0In
		amountOut = amount1Out
	} else {
		tokenIn = pool.Token1
		tokenOut = pool.Token0
		amountIn = amount1In
		amountOut = amount0Out
	}
	
	swap := &Swap{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		Protocol:    ProtocolAMMV2,
		PoolAddress: log.Address,
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Sender:      sender,
		Recipient:   recipient,
		TokenIn:     tokenIn,
		TokenOut:    tokenOut,
		AmountIn:    amountIn,
		AmountOut:   amountOut,
		Timestamp:   log.Timestamp,
	}
	
	a.swaps = append(a.swaps, swap)
	
	if a.onSwap != nil {
		a.onSwap(swap)
	}
	
	return nil
}

// indexV2Mint processes a V2 Mint (add liquidity) event
func (a *AMMIndexer) indexV2Mint(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid V2 mint event")
	}
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 64 {
		return fmt.Errorf("invalid V2 mint data")
	}
	
	amount0 := new(big.Int).SetBytes(data[0:32])
	amount1 := new(big.Int).SetBytes(data[32:64])
	sender := topicToAddress(log.Topics[1])
	
	pool := a.getOrCreatePool(log.Address, ProtocolAMMV2)
	
	event := &LiquidityEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		Protocol:    ProtocolAMMV2,
		EventType:   "add",
		PoolAddress: log.Address,
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Provider:    sender,
		Token0:      pool.Token0,
		Token1:      pool.Token1,
		Amount0:     amount0,
		Amount1:     amount1,
		Timestamp:   log.Timestamp,
	}
	
	a.liquidity = append(a.liquidity, event)
	
	if a.onLiquidity != nil {
		a.onLiquidity(event)
	}
	
	return nil
}

// indexV2Burn processes a V2 Burn (remove liquidity) event
func (a *AMMIndexer) indexV2Burn(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid V2 burn event")
	}
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 64 {
		return fmt.Errorf("invalid V2 burn data")
	}
	
	amount0 := new(big.Int).SetBytes(data[0:32])
	amount1 := new(big.Int).SetBytes(data[32:64])
	sender := topicToAddress(log.Topics[1])
	
	pool := a.getOrCreatePool(log.Address, ProtocolAMMV2)
	
	event := &LiquidityEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		Protocol:    ProtocolAMMV2,
		EventType:   "remove",
		PoolAddress: log.Address,
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Provider:    sender,
		Token0:      pool.Token0,
		Token1:      pool.Token1,
		Amount0:     amount0,
		Amount1:     amount1,
		Timestamp:   log.Timestamp,
	}
	
	a.liquidity = append(a.liquidity, event)
	
	if a.onLiquidity != nil {
		a.onLiquidity(event)
	}
	
	return nil
}

// indexV2Sync processes a V2 Sync event (reserve updates)
func (a *AMMIndexer) indexV2Sync(log *LogEntry) error {
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 64 {
		return nil
	}
	
	reserve0 := new(big.Int).SetBytes(data[0:32])
	reserve1 := new(big.Int).SetBytes(data[32:64])
	
	pool := a.getOrCreatePool(log.Address, ProtocolAMMV2)
	pool.Reserve0 = reserve0
	pool.Reserve1 = reserve1
	pool.UpdatedAt = log.Timestamp
	
	return nil
}

// indexV2PairCreated processes a V2 PairCreated event
func (a *AMMIndexer) indexV2PairCreated(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid V2 pair created event")
	}
	
	token0 := topicToAddress(log.Topics[1])
	token1 := topicToAddress(log.Topics[2])
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 32 {
		return fmt.Errorf("invalid pair created data")
	}
	
	pairAddress := "0x" + hex.EncodeToString(data[12:32])
	
	pool := &Pool{
		Address:   strings.ToLower(pairAddress),
		Protocol:  ProtocolAMMV2,
		Token0:    token0,
		Token1:    token1,
		Fee:       30, // 0.3% default
		Reserve0:  big.NewInt(0),
		Reserve1:  big.NewInt(0),
		CreatedAt: log.Timestamp,
		UpdatedAt: log.Timestamp,
	}
	
	a.pools[pool.Address] = pool
	
	if a.onPoolCreated != nil {
		a.onPoolCreated(pool)
	}
	
	return nil
}

// indexV3Swap processes a V3 Swap event
func (a *AMMIndexer) indexV3Swap(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid V3 swap event")
	}
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 160 {
		return fmt.Errorf("invalid V3 swap data")
	}
	
	// Swap(address indexed sender, address indexed recipient, int256 amount0, int256 amount1, uint160 sqrtPriceX96, uint128 liquidity, int24 tick)
	amount0 := new(big.Int).SetBytes(data[0:32])
	amount1 := new(big.Int).SetBytes(data[32:64])
	sqrtPriceX96 := new(big.Int).SetBytes(data[64:96])
	liquidity := new(big.Int).SetBytes(data[96:128])
	tickBytes := data[128:160]
	tick := int32(new(big.Int).SetBytes(tickBytes[28:32]).Int64())
	
	sender := topicToAddress(log.Topics[1])
	recipient := topicToAddress(log.Topics[2])
	
	pool := a.getOrCreatePool(log.Address, ProtocolAMMV3)
	
	// Determine direction based on signs
	var tokenIn, tokenOut string
	var amountIn, amountOut *big.Int
	
	if amount0.Sign() > 0 {
		tokenIn = pool.Token0
		tokenOut = pool.Token1
		amountIn = amount0
		amountOut = new(big.Int).Neg(amount1)
	} else {
		tokenIn = pool.Token1
		tokenOut = pool.Token0
		amountIn = new(big.Int).Neg(amount1)
		amountOut = amount0
	}
	
	swap := &Swap{
		ID:           fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		Protocol:     ProtocolAMMV3,
		PoolAddress:  log.Address,
		TxHash:       log.TxHash,
		BlockNumber:  log.BlockNumber,
		LogIndex:     log.LogIndex,
		Sender:       sender,
		Recipient:    recipient,
		TokenIn:      tokenIn,
		TokenOut:     tokenOut,
		AmountIn:     amountIn,
		AmountOut:    amountOut,
		SqrtPriceX96: sqrtPriceX96,
		Tick:         tick,
		Liquidity:    liquidity,
		Timestamp:    log.Timestamp,
	}
	
	// Update pool state
	pool.SqrtPriceX96 = sqrtPriceX96
	pool.CurrentTick = tick
	pool.Liquidity = liquidity
	pool.UpdatedAt = log.Timestamp
	
	a.swaps = append(a.swaps, swap)
	
	if a.onSwap != nil {
		a.onSwap(swap)
	}
	
	return nil
}

// indexV3Mint processes a V3 Mint (add liquidity) event
func (a *AMMIndexer) indexV3Mint(log *LogEntry) error {
	if len(log.Topics) < 4 {
		return fmt.Errorf("invalid V3 mint event")
	}
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 96 {
		return fmt.Errorf("invalid V3 mint data")
	}
	
	// Mint(address sender, address indexed owner, int24 indexed tickLower, int24 indexed tickUpper, uint128 amount, uint256 amount0, uint256 amount1)
	amount := new(big.Int).SetBytes(data[0:32])
	amount0 := new(big.Int).SetBytes(data[32:64])
	amount1 := new(big.Int).SetBytes(data[64:96])
	
	owner := topicToAddress(log.Topics[1])
	tickLower := int32(new(big.Int).SetBytes(hexToBytes(log.Topics[2])).Int64())
	tickUpper := int32(new(big.Int).SetBytes(hexToBytes(log.Topics[3])).Int64())
	
	pool := a.getOrCreatePool(log.Address, ProtocolAMMV3)
	
	event := &LiquidityEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		Protocol:    ProtocolAMMV3,
		EventType:   "add",
		PoolAddress: log.Address,
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Provider:    owner,
		Token0:      pool.Token0,
		Token1:      pool.Token1,
		Amount0:     amount0,
		Amount1:     amount1,
		TickLower:   tickLower,
		TickUpper:   tickUpper,
		Liquidity:   amount,
		Timestamp:   log.Timestamp,
	}
	
	a.liquidity = append(a.liquidity, event)
	
	if a.onLiquidity != nil {
		a.onLiquidity(event)
	}
	
	return nil
}

// indexV3Burn processes a V3 Burn (remove liquidity) event
func (a *AMMIndexer) indexV3Burn(log *LogEntry) error {
	if len(log.Topics) < 4 {
		return fmt.Errorf("invalid V3 burn event")
	}
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 96 {
		return fmt.Errorf("invalid V3 burn data")
	}
	
	amount := new(big.Int).SetBytes(data[0:32])
	amount0 := new(big.Int).SetBytes(data[32:64])
	amount1 := new(big.Int).SetBytes(data[64:96])
	
	owner := topicToAddress(log.Topics[1])
	tickLower := int32(new(big.Int).SetBytes(hexToBytes(log.Topics[2])).Int64())
	tickUpper := int32(new(big.Int).SetBytes(hexToBytes(log.Topics[3])).Int64())
	
	pool := a.getOrCreatePool(log.Address, ProtocolAMMV3)
	
	event := &LiquidityEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		Protocol:    ProtocolAMMV3,
		EventType:   "remove",
		PoolAddress: log.Address,
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Provider:    owner,
		Token0:      pool.Token0,
		Token1:      pool.Token1,
		Amount0:     amount0,
		Amount1:     amount1,
		TickLower:   tickLower,
		TickUpper:   tickUpper,
		Liquidity:   amount,
		Timestamp:   log.Timestamp,
	}
	
	a.liquidity = append(a.liquidity, event)
	
	if a.onLiquidity != nil {
		a.onLiquidity(event)
	}
	
	return nil
}

// indexV3Collect processes a V3 Collect (fee collection) event
func (a *AMMIndexer) indexV3Collect(log *LogEntry) error {
	// Similar structure to burn but for fee collection
	return nil
}

// indexV3Flash processes a V3 Flash loan event
func (a *AMMIndexer) indexV3Flash(log *LogEntry) error {
	// Flash loan tracking
	return nil
}

// indexV3Initialize processes a V3 pool initialization
func (a *AMMIndexer) indexV3Initialize(log *LogEntry) error {
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 64 {
		return fmt.Errorf("invalid V3 initialize data")
	}
	
	sqrtPriceX96 := new(big.Int).SetBytes(data[0:32])
	tick := int32(new(big.Int).SetBytes(data[60:64]).Int64())
	
	pool := a.getOrCreatePool(log.Address, ProtocolAMMV3)
	pool.SqrtPriceX96 = sqrtPriceX96
	pool.CurrentTick = tick
	pool.UpdatedAt = log.Timestamp
	
	return nil
}

// indexV3PoolCreated processes a V3 PoolCreated event
func (a *AMMIndexer) indexV3PoolCreated(log *LogEntry) error {
	if len(log.Topics) < 4 {
		return fmt.Errorf("invalid V3 pool created event")
	}
	
	token0 := topicToAddress(log.Topics[1])
	token1 := topicToAddress(log.Topics[2])
	fee := new(big.Int).SetBytes(hexToBytes(log.Topics[3])).Uint64()
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 64 {
		return fmt.Errorf("invalid pool created data")
	}
	
	tickSpacing := int32(new(big.Int).SetBytes(data[0:32]).Int64())
	poolAddress := "0x" + hex.EncodeToString(data[44:64])
	
	pool := &Pool{
		Address:     strings.ToLower(poolAddress),
		Protocol:    ProtocolAMMV3,
		Token0:      token0,
		Token1:      token1,
		Fee:         fee,
		TickSpacing: tickSpacing,
		Reserve0:    big.NewInt(0),
		Reserve1:    big.NewInt(0),
		Liquidity:   big.NewInt(0),
		CreatedAt:   log.Timestamp,
		UpdatedAt:   log.Timestamp,
	}
	
	a.pools[pool.Address] = pool
	
	if a.onPoolCreated != nil {
		a.onPoolCreated(pool)
	}
	
	return nil
}

// getOrCreatePool gets or creates a pool entry
func (a *AMMIndexer) getOrCreatePool(address string, protocol ProtocolType) *Pool {
	addr := strings.ToLower(address)
	if pool, exists := a.pools[addr]; exists {
		return pool
	}
	
	pool := &Pool{
		Address:   addr,
		Protocol:  protocol,
		Reserve0:  big.NewInt(0),
		Reserve1:  big.NewInt(0),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	a.pools[addr] = pool
	return pool
}

// GetPool returns a pool by address
func (a *AMMIndexer) GetPool(address string) *Pool {
	return a.pools[strings.ToLower(address)]
}

// GetAllPools returns all indexed pools
func (a *AMMIndexer) GetAllPools() []*Pool {
	pools := make([]*Pool, 0, len(a.pools))
	for _, p := range a.pools {
		pools = append(pools, p)
	}
	return pools
}

// GetSwaps returns swaps with optional filtering
func (a *AMMIndexer) GetSwaps(poolAddress string, limit int) []*Swap {
	if poolAddress == "" {
		if limit > 0 && limit < len(a.swaps) {
			return a.swaps[len(a.swaps)-limit:]
		}
		return a.swaps
	}
	
	var filtered []*Swap
	addr := strings.ToLower(poolAddress)
	for _, s := range a.swaps {
		if strings.ToLower(s.PoolAddress) == addr {
			filtered = append(filtered, s)
		}
	}
	
	if limit > 0 && limit < len(filtered) {
		return filtered[len(filtered)-limit:]
	}
	return filtered
}

// AMMStats represents AMM indexer statistics
type AMMStats struct {
	TotalPools       uint64             `json:"totalPools"`
	TotalSwaps       uint64             `json:"totalSwaps"`
	TotalVolume      map[string]*big.Int `json:"totalVolume"`
	Swaps24h         uint64             `json:"swaps24h"`
	UniqueTraders24h uint64             `json:"uniqueTraders24h"`
	LastUpdated      time.Time          `json:"lastUpdated"`
}

// GetStats returns AMM indexer statistics
func (a *AMMIndexer) GetStats() *AMMStats {
	stats := &AMMStats{
		TotalPools:  uint64(len(a.pools)),
		TotalSwaps:  uint64(len(a.swaps)),
		TotalVolume: make(map[string]*big.Int),
		LastUpdated: time.Now(),
	}

	cutoff := time.Now().Add(-24 * time.Hour)
	traders := make(map[string]bool)

	for _, swap := range a.swaps {
		if swap.Timestamp.After(cutoff) {
			stats.Swaps24h++
			traders[swap.Sender] = true
		}
	}
	stats.UniqueTraders24h = uint64(len(traders))

	return stats
}

// Helper functions
func topicToAddress(topic string) string {
	if len(topic) < 26 {
		return topic
	}
	return "0x" + topic[26:]
}

func hexToBytes(s string) []byte {
	s = strings.TrimPrefix(s, "0x")
	b, _ := hex.DecodeString(s)
	return b
}
