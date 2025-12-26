// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package stats provides blockchain statistics, counters, and caching functionality.
// Implements gas price oracle, block time calculations, and aggregate metrics.
package stats

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"
)

// GasPrice represents gas price for a fee tier
type GasPrice struct {
	Price       float64 `json:"price"`       // Gas price in Gwei
	PriorityFee float64 `json:"priority_fee"` // Priority fee in Gwei
	BaseFee     float64 `json:"base_fee"`     // Base fee in Gwei
	Time        float64 `json:"time"`         // Estimated confirmation time in ms
	FiatPrice   float64 `json:"fiat_price"`   // Fiat price for simple tx
	Wei         string  `json:"wei"`          // Full price in Wei
}

// GasPrices represents slow/average/fast gas price estimates
type GasPrices struct {
	Slow         *GasPrice `json:"slow"`
	Average      *GasPrice `json:"average"`
	Fast         *GasPrice `json:"fast"`
	UpdatedAt    time.Time `json:"updated_at"`
	BaseFee      string    `json:"base_fee"`      // Current base fee
	GasUsedRatio float64   `json:"gas_used_ratio"` // Recent block gas usage
}

// Counter represents an aggregate counter value
type Counter struct {
	Name      string    `json:"name"`
	Value     int64     `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Counters holds all aggregate statistics
type Counters struct {
	TotalBlocks         int64   `json:"total_blocks"`
	TotalTransactions   int64   `json:"total_transactions"`
	TotalAddresses      int64   `json:"total_addresses"`
	TotalContracts      int64   `json:"total_contracts"`
	TotalTokens         int64   `json:"total_tokens"`
	TotalTokenTransfers int64   `json:"total_token_transfers"`
	VerifiedContracts   int64   `json:"verified_contracts"`
	AverageBlockTime    float64 `json:"average_block_time_seconds"`
	TPS24h              float64 `json:"tps_24h"`
	GasUsed24h          int64   `json:"gas_used_24h"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// BlockTimeStats contains block time statistics
type BlockTimeStats struct {
	AverageBlockTime time.Duration `json:"average_block_time"`
	MinBlockTime     time.Duration `json:"min_block_time"`
	MaxBlockTime     time.Duration `json:"max_block_time"`
	BlocksAnalyzed   int           `json:"blocks_analyzed"`
	UpdatedAt        time.Time     `json:"updated_at"`
}

// TokenStats contains token-specific statistics
type TokenStats struct {
	Address      string    `json:"address"`
	HolderCount  int64     `json:"holder_count"`
	TransferCount int64    `json:"transfer_count"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AddressStats contains address-specific statistics
type AddressStats struct {
	Address       string `json:"address"`
	TransactionCount int64 `json:"transaction_count"`
	TokenTransferCount int64 `json:"token_transfer_count"`
	InternalTxCount int64 `json:"internal_tx_count"`
	GasUsed       int64 `json:"gas_used"`
	Balance       string `json:"balance"`
}

// Service provides statistics and caching functionality
type Service struct {
	db            *sql.DB
	cache         *Cache
	updateMutex   sync.Mutex
	config        Config
}

// Config holds statistics service configuration
type Config struct {
	CacheTTL             time.Duration
	GasPriceBlocks       int     // Number of blocks to analyze for gas prices
	SlowPercentile       float64 // Percentile for slow gas price
	AveragePercentile    float64 // Percentile for average gas price
	FastPercentile       float64 // Percentile for fast gas price
	SimpleTransactionGas int64   // Gas for simple ETH transfer
	CounterUpdateInterval time.Duration
	BlockTimeBlocks      int     // Number of blocks for block time calculation
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		CacheTTL:              5 * time.Minute,
		GasPriceBlocks:        200,
		SlowPercentile:        35.0,
		AveragePercentile:     60.0,
		FastPercentile:        90.0,
		SimpleTransactionGas:  21000,
		CounterUpdateInterval: 30 * time.Second,
		BlockTimeBlocks:       100,
	}
}

// Cache provides in-memory caching for statistics
type Cache struct {
	mu           sync.RWMutex
	gasPrices    *GasPrices
	counters     *Counters
	blockTime    *BlockTimeStats
	tokenStats   map[string]*TokenStats
	lastUpdated  time.Time
	ttl          time.Duration
}

// NewCache creates a new cache with specified TTL
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		tokenStats: make(map[string]*TokenStats),
		ttl:        ttl,
	}
}

// NewService creates a new statistics service
func NewService(db *sql.DB, config Config) *Service {
	return &Service{
		db:     db,
		cache:  NewCache(config.CacheTTL),
		config: config,
	}
}

// GetGasPrices returns current gas price estimates
func (s *Service) GetGasPrices(ctx context.Context) (*GasPrices, error) {
	s.cache.mu.RLock()
	if s.cache.gasPrices != nil && time.Since(s.cache.lastUpdated) < s.cache.ttl {
		prices := s.cache.gasPrices
		s.cache.mu.RUnlock()
		return prices, nil
	}
	s.cache.mu.RUnlock()

	return s.updateGasPrices(ctx)
}

// updateGasPrices calculates and caches gas prices
func (s *Service) updateGasPrices(ctx context.Context) (*GasPrices, error) {
	s.updateMutex.Lock()
	defer s.updateMutex.Unlock()

	// Double-check cache after acquiring lock
	s.cache.mu.RLock()
	if s.cache.gasPrices != nil && time.Since(s.cache.lastUpdated) < s.cache.ttl {
		prices := s.cache.gasPrices
		s.cache.mu.RUnlock()
		return prices, nil
	}
	s.cache.mu.RUnlock()

	// Get recent transactions with gas prices
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.gas_price, t.gas_used, b.timestamp
		FROM cchain_transactions t
		JOIN blocks b ON b.height = t.block_number
		WHERE t.gas_price IS NOT NULL AND t.gas_price != '0'
		ORDER BY t.block_number DESC
		LIMIT $1
	`, s.config.GasPriceBlocks*50) // Assume ~50 txs per block
	if err != nil {
		return nil, fmt.Errorf("query gas prices: %w", err)
	}
	defer rows.Close()

	type txGas struct {
		gasPrice  *big.Int
		gasUsed   uint64
		timestamp time.Time
	}

	var txGasData []txGas
	for rows.Next() {
		var gasPriceStr string
		var gasUsed uint64
		var timestamp time.Time
		if err := rows.Scan(&gasPriceStr, &gasUsed, &timestamp); err != nil {
			continue
		}
		gasPrice := new(big.Int)
		gasPrice.SetString(gasPriceStr, 10)
		if gasPrice.Sign() > 0 {
			txGasData = append(txGasData, txGas{
				gasPrice:  gasPrice,
				gasUsed:   gasUsed,
				timestamp: timestamp,
			})
		}
	}

	if len(txGasData) == 0 {
		return &GasPrices{
			Slow:      nil,
			Average:   nil,
			Fast:      nil,
			UpdatedAt: time.Now(),
		}, nil
	}

	// Sort by gas price
	sort.Slice(txGasData, func(i, j int) bool {
		return txGasData[i].gasPrice.Cmp(txGasData[j].gasPrice) < 0
	})

	// Calculate percentiles
	slowIdx := int(float64(len(txGasData)) * s.config.SlowPercentile / 100)
	avgIdx := int(float64(len(txGasData)) * s.config.AveragePercentile / 100)
	fastIdx := int(float64(len(txGasData)) * s.config.FastPercentile / 100)

	if slowIdx >= len(txGasData) {
		slowIdx = len(txGasData) - 1
	}
	if avgIdx >= len(txGasData) {
		avgIdx = len(txGasData) - 1
	}
	if fastIdx >= len(txGasData) {
		fastIdx = len(txGasData) - 1
	}

	// Get base fee from latest block
	var baseFeeStr sql.NullString
	err = s.db.QueryRowContext(ctx, `
		SELECT metadata->>'baseFee'
		FROM blocks
		ORDER BY height DESC
		LIMIT 1
	`).Scan(&baseFeeStr)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("query base fee: %w", err)
	}

	baseFee := new(big.Int)
	if baseFeeStr.Valid && baseFeeStr.String != "" {
		baseFee.SetString(baseFeeStr.String, 10)
	}

	// Get average block time
	blockTime, _ := s.GetAverageBlockTime(ctx)
	avgBlockTimeMs := float64(blockTime.AverageBlockTime.Milliseconds())

	// Create gas prices
	prices := &GasPrices{
		Slow:      createGasPrice(txGasData[slowIdx].gasPrice, baseFee, avgBlockTimeMs*2.5, s.config.SimpleTransactionGas),
		Average:   createGasPrice(txGasData[avgIdx].gasPrice, baseFee, avgBlockTimeMs*1.5, s.config.SimpleTransactionGas),
		Fast:      createGasPrice(txGasData[fastIdx].gasPrice, baseFee, avgBlockTimeMs*0.8, s.config.SimpleTransactionGas),
		UpdatedAt: time.Now(),
		BaseFee:   weiToGwei(baseFee),
	}

	// Calculate gas used ratio
	var gasUsed, gasLimit int64
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM((metadata->>'gasUsed')::bigint), 0),
		       COALESCE(SUM((metadata->>'gasLimit')::bigint), 1)
		FROM blocks
		WHERE height > (SELECT MAX(height) - 10 FROM blocks)
	`).Scan(&gasUsed, &gasLimit)
	if err == nil && gasLimit > 0 {
		prices.GasUsedRatio = float64(gasUsed) / float64(gasLimit)
	}

	// Update cache
	s.cache.mu.Lock()
	s.cache.gasPrices = prices
	s.cache.lastUpdated = time.Now()
	s.cache.mu.Unlock()

	return prices, nil
}

// createGasPrice creates a GasPrice from wei values
func createGasPrice(gasPrice, baseFee *big.Int, estimatedTimeMs float64, simpleGas int64) *GasPrice {
	priceGwei := weiToGweiFloat(gasPrice)
	baseFeeGwei := weiToGweiFloat(baseFee)
	priorityFee := priceGwei - baseFeeGwei
	if priorityFee < 0 {
		priorityFee = 0
	}

	// Estimate fiat price (placeholder - would need exchange rate integration)
	fiatPrice := priceGwei * float64(simpleGas) * 0.001 // Rough USD estimate

	return &GasPrice{
		Price:       priceGwei,
		PriorityFee: priorityFee,
		BaseFee:     baseFeeGwei,
		Time:        estimatedTimeMs,
		FiatPrice:   fiatPrice,
		Wei:         gasPrice.String(),
	}
}

// weiToGwei converts wei to gwei string
func weiToGwei(wei *big.Int) string {
	if wei == nil || wei.Sign() == 0 {
		return "0"
	}
	gwei := new(big.Float).SetInt(wei)
	gwei.Quo(gwei, big.NewFloat(1e9))
	return gwei.Text('f', 6)
}

// weiToGweiFloat converts wei to gwei float
func weiToGweiFloat(wei *big.Int) float64 {
	if wei == nil || wei.Sign() == 0 {
		return 0
	}
	gwei := new(big.Float).SetInt(wei)
	gwei.Quo(gwei, big.NewFloat(1e9))
	result, _ := gwei.Float64()
	return result
}

// GetCounters returns aggregate statistics
func (s *Service) GetCounters(ctx context.Context) (*Counters, error) {
	s.cache.mu.RLock()
	if s.cache.counters != nil && time.Since(s.cache.counters.UpdatedAt) < s.config.CounterUpdateInterval {
		counters := s.cache.counters
		s.cache.mu.RUnlock()
		return counters, nil
	}
	s.cache.mu.RUnlock()

	return s.updateCounters(ctx)
}

// updateCounters calculates and caches aggregate counters
func (s *Service) updateCounters(ctx context.Context) (*Counters, error) {
	s.updateMutex.Lock()
	defer s.updateMutex.Unlock()

	counters := &Counters{UpdatedAt: time.Now()}

	// Total blocks
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM blocks`).Scan(&counters.TotalBlocks)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("count blocks: %w", err)
	}

	// Total transactions
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cchain_transactions`).Scan(&counters.TotalTransactions)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("count transactions: %w", err)
	}

	// Total addresses
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cchain_addresses`).Scan(&counters.TotalAddresses)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("count addresses: %w", err)
	}

	// Total contracts
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cchain_addresses WHERE is_contract = true
	`).Scan(&counters.TotalContracts)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("count contracts: %w", err)
	}

	// Total tokens
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cchain_tokens`).Scan(&counters.TotalTokens)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("count tokens: %w", err)
	}

	// Total token transfers
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cchain_token_transfers`).Scan(&counters.TotalTokenTransfers)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("count token transfers: %w", err)
	}

	// TPS 24h
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)::float / 86400.0
		FROM cchain_transactions
		WHERE timestamp > NOW() - INTERVAL '24 hours'
	`).Scan(&counters.TPS24h)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("calculate tps: %w", err)
	}

	// Gas used 24h
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(gas_used), 0)
		FROM cchain_transactions
		WHERE timestamp > NOW() - INTERVAL '24 hours'
	`).Scan(&counters.GasUsed24h)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("sum gas used: %w", err)
	}

	// Average block time
	blockTime, err := s.GetAverageBlockTime(ctx)
	if err == nil {
		counters.AverageBlockTime = blockTime.AverageBlockTime.Seconds()
	}

	// Update cache
	s.cache.mu.Lock()
	s.cache.counters = counters
	s.cache.mu.Unlock()

	return counters, nil
}

// GetAverageBlockTime returns block time statistics
func (s *Service) GetAverageBlockTime(ctx context.Context) (*BlockTimeStats, error) {
	s.cache.mu.RLock()
	if s.cache.blockTime != nil && time.Since(s.cache.blockTime.UpdatedAt) < s.cache.ttl {
		stats := s.cache.blockTime
		s.cache.mu.RUnlock()
		return stats, nil
	}
	s.cache.mu.RUnlock()

	return s.updateBlockTime(ctx)
}

// updateBlockTime calculates block time statistics
func (s *Service) updateBlockTime(ctx context.Context) (*BlockTimeStats, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT timestamp
		FROM blocks
		ORDER BY height DESC
		LIMIT $1
	`, s.config.BlockTimeBlocks+1)
	if err != nil {
		return nil, fmt.Errorf("query blocks: %w", err)
	}
	defer rows.Close()

	var timestamps []time.Time
	for rows.Next() {
		var ts time.Time
		if err := rows.Scan(&ts); err != nil {
			continue
		}
		timestamps = append(timestamps, ts)
	}

	if len(timestamps) < 2 {
		return &BlockTimeStats{
			AverageBlockTime: 2 * time.Second, // Default estimate
			UpdatedAt:        time.Now(),
		}, nil
	}

	var totalDiff time.Duration
	var minDiff, maxDiff time.Duration
	minDiff = time.Hour // Start with large value
	count := 0

	for i := 0; i < len(timestamps)-1; i++ {
		diff := timestamps[i].Sub(timestamps[i+1])
		if diff < 0 {
			diff = -diff
		}
		if diff > 0 {
			totalDiff += diff
			if diff < minDiff {
				minDiff = diff
			}
			if diff > maxDiff {
				maxDiff = diff
			}
			count++
		}
	}

	avgDiff := time.Duration(0)
	if count > 0 {
		avgDiff = totalDiff / time.Duration(count)
	}

	stats := &BlockTimeStats{
		AverageBlockTime: avgDiff,
		MinBlockTime:     minDiff,
		MaxBlockTime:     maxDiff,
		BlocksAnalyzed:   count,
		UpdatedAt:        time.Now(),
	}

	s.cache.mu.Lock()
	s.cache.blockTime = stats
	s.cache.mu.Unlock()

	return stats, nil
}

// GetTokenStats returns statistics for a specific token
func (s *Service) GetTokenStats(ctx context.Context, tokenAddress string) (*TokenStats, error) {
	tokenAddress = normalizeAddress(tokenAddress)

	s.cache.mu.RLock()
	if stats, ok := s.cache.tokenStats[tokenAddress]; ok {
		if time.Since(stats.UpdatedAt) < s.cache.ttl {
			s.cache.mu.RUnlock()
			return stats, nil
		}
	}
	s.cache.mu.RUnlock()

	stats := &TokenStats{
		Address:   tokenAddress,
		UpdatedAt: time.Now(),
	}

	// Get holder count
	err := s.db.QueryRowContext(ctx, `
		SELECT holder_count FROM cchain_tokens WHERE address = $1
	`, tokenAddress).Scan(&stats.HolderCount)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("get holder count: %w", err)
	}

	// Get transfer count
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cchain_token_transfers WHERE token_address = $1
	`, tokenAddress).Scan(&stats.TransferCount)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("get transfer count: %w", err)
	}

	s.cache.mu.Lock()
	s.cache.tokenStats[tokenAddress] = stats
	s.cache.mu.Unlock()

	return stats, nil
}

// GetAddressStats returns statistics for a specific address
func (s *Service) GetAddressStats(ctx context.Context, address string) (*AddressStats, error) {
	address = normalizeAddress(address)

	stats := &AddressStats{Address: address}

	// Get transaction count
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cchain_transactions
		WHERE tx_from = $1 OR tx_to = $1
	`, address).Scan(&stats.TransactionCount)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("count transactions: %w", err)
	}

	// Get token transfer count
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cchain_token_transfers
		WHERE tx_from = $1 OR tx_to = $1
	`, address).Scan(&stats.TokenTransferCount)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("count token transfers: %w", err)
	}

	// Get internal tx count
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cchain_internal_transactions
		WHERE tx_from = $1 OR tx_to = $1
	`, address).Scan(&stats.InternalTxCount)
	if err != nil && err != sql.ErrNoRows {
		// Table might not exist, continue
	}

	// Get total gas used
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(gas_used), 0) FROM cchain_transactions
		WHERE tx_from = $1
	`, address).Scan(&stats.GasUsed)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("sum gas used: %w", err)
	}

	// Get balance
	err = s.db.QueryRowContext(ctx, `
		SELECT balance FROM cchain_addresses WHERE hash = $1
	`, address).Scan(&stats.Balance)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("get balance: %w", err)
	}

	return stats, nil
}

// GetDailyTransactionStats returns daily transaction counts
func (s *Service) GetDailyTransactionStats(ctx context.Context, days int) ([]DailyStats, error) {
	if days <= 0 {
		days = 30
	}
	if days > 365 {
		days = 365
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT DATE(timestamp) as date, COUNT(*) as count
		FROM cchain_transactions
		WHERE timestamp > NOW() - make_interval(days => $1)
		GROUP BY DATE(timestamp)
		ORDER BY date DESC
	`, days)
	if err != nil {
		return nil, fmt.Errorf("query daily stats: %w", err)
	}
	defer rows.Close()

	var stats []DailyStats
	for rows.Next() {
		var stat DailyStats
		if err := rows.Scan(&stat.Date, &stat.Count); err != nil {
			continue
		}
		stats = append(stats, stat)
	}

	return stats, nil
}

// DailyStats represents daily count statistics
type DailyStats struct {
	Date  time.Time `json:"date"`
	Count int64     `json:"count"`
}

// GetHourlyGasUsage returns hourly gas usage for the last 24 hours
func (s *Service) GetHourlyGasUsage(ctx context.Context) ([]HourlyStats, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DATE_TRUNC('hour', timestamp) as hour, COALESCE(SUM(gas_used), 0) as gas_used
		FROM cchain_transactions
		WHERE timestamp > NOW() - INTERVAL '24 hours'
		GROUP BY DATE_TRUNC('hour', timestamp)
		ORDER BY hour DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query hourly gas: %w", err)
	}
	defer rows.Close()

	var stats []HourlyStats
	for rows.Next() {
		var stat HourlyStats
		if err := rows.Scan(&stat.Hour, &stat.Value); err != nil {
			continue
		}
		stats = append(stats, stat)
	}

	return stats, nil
}

// HourlyStats represents hourly statistics
type HourlyStats struct {
	Hour  time.Time `json:"hour"`
	Value int64     `json:"value"`
}

// InvalidateCache clears all cached data
func (s *Service) InvalidateCache() {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()

	s.cache.gasPrices = nil
	s.cache.counters = nil
	s.cache.blockTime = nil
	s.cache.tokenStats = make(map[string]*TokenStats)
}

// StartBackgroundUpdater starts a background goroutine to update stats periodically
func (s *Service) StartBackgroundUpdater(ctx context.Context) {
	ticker := time.NewTicker(s.config.CounterUpdateInterval)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				s.updateCounters(ctx)
				s.updateGasPrices(ctx)
				s.updateBlockTime(ctx)
			}
		}
	}()
}

// normalizeAddress converts address to lowercase with 0x prefix
func normalizeAddress(address string) string {
	if len(address) >= 2 && (address[:2] == "0x" || address[:2] == "0X") {
		return "0x" + address[2:]
	}
	return "0x" + address
}
