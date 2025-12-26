// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package charts provides time-series data for blockchain visualization.
package charts

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// Config holds chart service configuration
type Config struct {
	// DefaultHistoryDays is the default number of days for historical data
	DefaultHistoryDays int
	// CacheTTL is how long to cache chart data
	CacheTTL time.Duration
	// MaxDataPoints is the maximum number of data points to return
	MaxDataPoints int
}

// DefaultConfig returns sensible defaults for chart configuration
func DefaultConfig() *Config {
	return &Config{
		DefaultHistoryDays: 30,
		CacheTTL:           5 * time.Minute,
		MaxDataPoints:      365,
	}
}

// DailyTransactionStats represents transaction statistics for a single day
type DailyTransactionStats struct {
	Date                time.Time `json:"date"`
	TransactionCount    int64     `json:"transaction_count"`
	GasUsed             *big.Int  `json:"gas_used"`
	TotalFee            *big.Int  `json:"total_fee"`
	AverageGasPrice     *big.Int  `json:"average_gas_price,omitempty"`
	UniqueAddresses     int64     `json:"unique_addresses,omitempty"`
	ContractDeployments int64     `json:"contract_deployments,omitempty"`
	TokenTransfers      int64     `json:"token_transfers,omitempty"`
}

// DailyGasStats represents gas usage statistics for a single day
type DailyGasStats struct {
	Date            time.Time `json:"date"`
	GasUsed         *big.Int  `json:"gas_used"`
	GasLimit        *big.Int  `json:"gas_limit"`
	UtilizationPct  float64   `json:"utilization_pct"`
	AverageGasPrice *big.Int  `json:"average_gas_price"`
	BlockCount      int64     `json:"block_count"`
}

// DailyTokenTransferStats represents token transfer statistics for a single day
type DailyTokenTransferStats struct {
	Date          time.Time `json:"date"`
	TransferCount int64     `json:"transfer_count"`
	UniqueTokens  int64     `json:"unique_tokens"`
	UniqueFrom    int64     `json:"unique_from"`
	UniqueTo      int64     `json:"unique_to"`
	TotalValue    *big.Int  `json:"total_value,omitempty"`
}

// DailyAddressStats represents address statistics for a single day
type DailyAddressStats struct {
	Date            time.Time `json:"date"`
	NewAddresses    int64     `json:"new_addresses"`
	ActiveAddresses int64     `json:"active_addresses"`
	TotalAddresses  int64     `json:"total_addresses"`
}

// DailyBlockStats represents block statistics for a single day
type DailyBlockStats struct {
	Date             time.Time     `json:"date"`
	BlockCount       int64         `json:"block_count"`
	AverageBlockTime time.Duration `json:"average_block_time"`
	TotalRewards     *big.Int      `json:"total_rewards"`
	AverageSize      int64         `json:"average_size"`
}

// MarketData represents market information for a single day
type MarketData struct {
	Date       time.Time `json:"date"`
	ClosePrice float64   `json:"closing_price"`
	MarketCap  float64   `json:"market_cap,omitempty"`
	Volume     float64   `json:"volume,omitempty"`
	TVL        float64   `json:"tvl,omitempty"`
}

// ChartResponse is the standard response format for chart data
type ChartResponse struct {
	ChartData interface{} `json:"chart_data"`
	UpdatedAt time.Time   `json:"updated_at"`
	Period    string      `json:"period,omitempty"`
	StartDate time.Time   `json:"start_date,omitempty"`
	EndDate   time.Time   `json:"end_date,omitempty"`
}

// Service provides chart data operations
type Service struct {
	db     *sql.DB
	config *Config
	cache  *chartCache
}

// chartCache holds cached chart data
type chartCache struct {
	mu                  sync.RWMutex
	txStats             []DailyTransactionStats
	txStatsUpdated      time.Time
	gasStats            []DailyGasStats
	gasStatsUpdated     time.Time
	tokenStats          []DailyTokenTransferStats
	tokenStatsUpdated   time.Time
	addressStats        []DailyAddressStats
	addressStatsUpdated time.Time
	blockStats          []DailyBlockStats
	blockStatsUpdated   time.Time
}

// NewService creates a new chart service
func NewService(db *sql.DB, config *Config) *Service {
	if config == nil {
		config = DefaultConfig()
	}
	return &Service{
		db:     db,
		config: config,
		cache:  &chartCache{},
	}
}

// GetTransactionChart returns daily transaction statistics
func (s *Service) GetTransactionChart(ctx context.Context, days int) (*ChartResponse, error) {
	if days <= 0 {
		days = s.config.DefaultHistoryDays
	}
	if days > s.config.MaxDataPoints {
		days = s.config.MaxDataPoints
	}

	// Check cache
	s.cache.mu.RLock()
	if s.cache.txStats != nil && time.Since(s.cache.txStatsUpdated) < s.config.CacheTTL {
		data := s.cache.txStats
		if len(data) > days {
			data = data[:days]
		}
		s.cache.mu.RUnlock()
		return &ChartResponse{
			ChartData: data,
			UpdatedAt: s.cache.txStatsUpdated,
			Period:    fmt.Sprintf("%d days", days),
		}, nil
	}
	s.cache.mu.RUnlock()

	// Query database
	today := time.Now().UTC().Truncate(24 * time.Hour)
	startDate := today.AddDate(0, 0, -days)

	query := `
		SELECT
			DATE(timestamp) as date,
			COUNT(*) as tx_count,
			COALESCE(SUM(gas_used), 0) as gas_used,
			COALESCE(SUM(gas_used * gas_price), 0) as total_fee,
			COALESCE(AVG(gas_price), 0) as avg_gas_price
		FROM cchain_transactions
		WHERE timestamp >= $1 AND timestamp < $2
		GROUP BY DATE(timestamp)
		ORDER BY date DESC
	`

	rows, err := s.db.QueryContext(ctx, query, startDate, today)
	if err != nil {
		return nil, fmt.Errorf("query transaction stats: %w", err)
	}
	defer rows.Close()

	var stats []DailyTransactionStats
	for rows.Next() {
		var stat DailyTransactionStats
		var gasUsedStr, totalFeeStr, avgGasPriceStr string

		if err := rows.Scan(&stat.Date, &stat.TransactionCount, &gasUsedStr, &totalFeeStr, &avgGasPriceStr); err != nil {
			return nil, fmt.Errorf("scan transaction stats: %w", err)
		}

		stat.GasUsed, _ = new(big.Int).SetString(gasUsedStr, 10)
		stat.TotalFee, _ = new(big.Int).SetString(totalFeeStr, 10)
		stat.AverageGasPrice, _ = new(big.Int).SetString(avgGasPriceStr, 10)

		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate transaction stats: %w", err)
	}

	// Update cache
	s.cache.mu.Lock()
	s.cache.txStats = stats
	s.cache.txStatsUpdated = time.Now()
	s.cache.mu.Unlock()

	return &ChartResponse{
		ChartData: stats,
		UpdatedAt: time.Now(),
		Period:    fmt.Sprintf("%d days", days),
		StartDate: startDate,
		EndDate:   today,
	}, nil
}

// GetGasUsageChart returns daily gas usage statistics
func (s *Service) GetGasUsageChart(ctx context.Context, days int) (*ChartResponse, error) {
	if days <= 0 {
		days = s.config.DefaultHistoryDays
	}
	if days > s.config.MaxDataPoints {
		days = s.config.MaxDataPoints
	}

	// Check cache
	s.cache.mu.RLock()
	if s.cache.gasStats != nil && time.Since(s.cache.gasStatsUpdated) < s.config.CacheTTL {
		data := s.cache.gasStats
		if len(data) > days {
			data = data[:days]
		}
		s.cache.mu.RUnlock()
		return &ChartResponse{
			ChartData: data,
			UpdatedAt: s.cache.gasStatsUpdated,
			Period:    fmt.Sprintf("%d days", days),
		}, nil
	}
	s.cache.mu.RUnlock()

	today := time.Now().UTC().Truncate(24 * time.Hour)
	startDate := today.AddDate(0, 0, -days)

	query := `
		SELECT
			DATE(timestamp) as date,
			COALESCE(SUM(gas_used), 0) as gas_used,
			COALESCE(SUM(gas_limit), 0) as gas_limit,
			COALESCE(AVG(gas_price), 0) as avg_gas_price,
			COUNT(DISTINCT block_number) as block_count
		FROM cchain_transactions
		WHERE timestamp >= $1 AND timestamp < $2
		GROUP BY DATE(timestamp)
		ORDER BY date DESC
	`

	rows, err := s.db.QueryContext(ctx, query, startDate, today)
	if err != nil {
		return nil, fmt.Errorf("query gas stats: %w", err)
	}
	defer rows.Close()

	var stats []DailyGasStats
	for rows.Next() {
		var stat DailyGasStats
		var gasUsedStr, gasLimitStr, avgGasPriceStr string

		if err := rows.Scan(&stat.Date, &gasUsedStr, &gasLimitStr, &avgGasPriceStr, &stat.BlockCount); err != nil {
			return nil, fmt.Errorf("scan gas stats: %w", err)
		}

		stat.GasUsed, _ = new(big.Int).SetString(gasUsedStr, 10)
		stat.GasLimit, _ = new(big.Int).SetString(gasLimitStr, 10)
		stat.AverageGasPrice, _ = new(big.Int).SetString(avgGasPriceStr, 10)

		// Calculate utilization percentage
		if stat.GasLimit != nil && stat.GasLimit.Sign() > 0 {
			gasUsedFloat := new(big.Float).SetInt(stat.GasUsed)
			gasLimitFloat := new(big.Float).SetInt(stat.GasLimit)
			ratio := new(big.Float).Quo(gasUsedFloat, gasLimitFloat)
			stat.UtilizationPct, _ = ratio.Mul(ratio, big.NewFloat(100)).Float64()
		}

		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate gas stats: %w", err)
	}

	// Update cache
	s.cache.mu.Lock()
	s.cache.gasStats = stats
	s.cache.gasStatsUpdated = time.Now()
	s.cache.mu.Unlock()

	return &ChartResponse{
		ChartData: stats,
		UpdatedAt: time.Now(),
		Period:    fmt.Sprintf("%d days", days),
		StartDate: startDate,
		EndDate:   today,
	}, nil
}

// GetTokenTransferChart returns daily token transfer statistics
func (s *Service) GetTokenTransferChart(ctx context.Context, days int) (*ChartResponse, error) {
	if days <= 0 {
		days = s.config.DefaultHistoryDays
	}
	if days > s.config.MaxDataPoints {
		days = s.config.MaxDataPoints
	}

	// Check cache
	s.cache.mu.RLock()
	if s.cache.tokenStats != nil && time.Since(s.cache.tokenStatsUpdated) < s.config.CacheTTL {
		data := s.cache.tokenStats
		if len(data) > days {
			data = data[:days]
		}
		s.cache.mu.RUnlock()
		return &ChartResponse{
			ChartData: data,
			UpdatedAt: s.cache.tokenStatsUpdated,
			Period:    fmt.Sprintf("%d days", days),
		}, nil
	}
	s.cache.mu.RUnlock()

	today := time.Now().UTC().Truncate(24 * time.Hour)
	startDate := today.AddDate(0, 0, -days)

	query := `
		SELECT
			DATE(timestamp) as date,
			COUNT(*) as transfer_count,
			COUNT(DISTINCT token_hash) as unique_tokens,
			COUNT(DISTINCT from_address) as unique_from,
			COUNT(DISTINCT to_address) as unique_to
		FROM cchain_token_transfers
		WHERE timestamp >= $1 AND timestamp < $2
		GROUP BY DATE(timestamp)
		ORDER BY date DESC
	`

	rows, err := s.db.QueryContext(ctx, query, startDate, today)
	if err != nil {
		return nil, fmt.Errorf("query token transfer stats: %w", err)
	}
	defer rows.Close()

	var stats []DailyTokenTransferStats
	for rows.Next() {
		var stat DailyTokenTransferStats
		if err := rows.Scan(&stat.Date, &stat.TransferCount, &stat.UniqueTokens, &stat.UniqueFrom, &stat.UniqueTo); err != nil {
			return nil, fmt.Errorf("scan token transfer stats: %w", err)
		}
		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate token transfer stats: %w", err)
	}

	// Update cache
	s.cache.mu.Lock()
	s.cache.tokenStats = stats
	s.cache.tokenStatsUpdated = time.Now()
	s.cache.mu.Unlock()

	return &ChartResponse{
		ChartData: stats,
		UpdatedAt: time.Now(),
		Period:    fmt.Sprintf("%d days", days),
		StartDate: startDate,
		EndDate:   today,
	}, nil
}

// GetAddressGrowthChart returns daily address growth statistics
func (s *Service) GetAddressGrowthChart(ctx context.Context, days int) (*ChartResponse, error) {
	if days <= 0 {
		days = s.config.DefaultHistoryDays
	}
	if days > s.config.MaxDataPoints {
		days = s.config.MaxDataPoints
	}

	// Check cache
	s.cache.mu.RLock()
	if s.cache.addressStats != nil && time.Since(s.cache.addressStatsUpdated) < s.config.CacheTTL {
		data := s.cache.addressStats
		if len(data) > days {
			data = data[:days]
		}
		s.cache.mu.RUnlock()
		return &ChartResponse{
			ChartData: data,
			UpdatedAt: s.cache.addressStatsUpdated,
			Period:    fmt.Sprintf("%d days", days),
		}, nil
	}
	s.cache.mu.RUnlock()

	today := time.Now().UTC().Truncate(24 * time.Hour)
	startDate := today.AddDate(0, 0, -days)

	// Query new addresses per day (addresses first seen on that day)
	query := `
		SELECT
			DATE(first_seen) as date,
			COUNT(*) as new_addresses
		FROM cchain_addresses
		WHERE first_seen >= $1 AND first_seen < $2
		GROUP BY DATE(first_seen)
		ORDER BY date DESC
	`

	rows, err := s.db.QueryContext(ctx, query, startDate, today)
	if err != nil {
		return nil, fmt.Errorf("query address stats: %w", err)
	}
	defer rows.Close()

	var stats []DailyAddressStats
	var runningTotal int64

	// Get total addresses before start date
	var baseCount int64
	err = s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM cchain_addresses WHERE first_seen < $1", startDate).Scan(&baseCount)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("query base address count: %w", err)
	}
	runningTotal = baseCount

	for rows.Next() {
		var stat DailyAddressStats
		if err := rows.Scan(&stat.Date, &stat.NewAddresses); err != nil {
			return nil, fmt.Errorf("scan address stats: %w", err)
		}
		runningTotal += stat.NewAddresses
		stat.TotalAddresses = runningTotal
		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate address stats: %w", err)
	}

	// Update cache
	s.cache.mu.Lock()
	s.cache.addressStats = stats
	s.cache.addressStatsUpdated = time.Now()
	s.cache.mu.Unlock()

	return &ChartResponse{
		ChartData: stats,
		UpdatedAt: time.Now(),
		Period:    fmt.Sprintf("%d days", days),
		StartDate: startDate,
		EndDate:   today,
	}, nil
}

// GetBlockChart returns daily block statistics
func (s *Service) GetBlockChart(ctx context.Context, days int) (*ChartResponse, error) {
	if days <= 0 {
		days = s.config.DefaultHistoryDays
	}
	if days > s.config.MaxDataPoints {
		days = s.config.MaxDataPoints
	}

	// Check cache
	s.cache.mu.RLock()
	if s.cache.blockStats != nil && time.Since(s.cache.blockStatsUpdated) < s.config.CacheTTL {
		data := s.cache.blockStats
		if len(data) > days {
			data = data[:days]
		}
		s.cache.mu.RUnlock()
		return &ChartResponse{
			ChartData: data,
			UpdatedAt: s.cache.blockStatsUpdated,
			Period:    fmt.Sprintf("%d days", days),
		}, nil
	}
	s.cache.mu.RUnlock()

	today := time.Now().UTC().Truncate(24 * time.Hour)
	startDate := today.AddDate(0, 0, -days)

	query := `
		WITH block_times AS (
			SELECT
				DATE(timestamp) as date,
				block_number,
				timestamp,
				LAG(timestamp) OVER (ORDER BY block_number) as prev_timestamp
			FROM cchain_transactions
			WHERE timestamp >= $1 AND timestamp < $2
		)
		SELECT
			date,
			COUNT(DISTINCT block_number) as block_count,
			COALESCE(AVG(EXTRACT(EPOCH FROM (timestamp - prev_timestamp))), 0) as avg_block_time_secs
		FROM block_times
		WHERE prev_timestamp IS NOT NULL
		GROUP BY date
		ORDER BY date DESC
	`

	rows, err := s.db.QueryContext(ctx, query, startDate, today)
	if err != nil {
		return nil, fmt.Errorf("query block stats: %w", err)
	}
	defer rows.Close()

	var stats []DailyBlockStats
	for rows.Next() {
		var stat DailyBlockStats
		var avgBlockTimeSecs float64

		if err := rows.Scan(&stat.Date, &stat.BlockCount, &avgBlockTimeSecs); err != nil {
			return nil, fmt.Errorf("scan block stats: %w", err)
		}

		stat.AverageBlockTime = time.Duration(avgBlockTimeSecs * float64(time.Second))
		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate block stats: %w", err)
	}

	// Update cache
	s.cache.mu.Lock()
	s.cache.blockStats = stats
	s.cache.blockStatsUpdated = time.Now()
	s.cache.mu.Unlock()

	return &ChartResponse{
		ChartData: stats,
		UpdatedAt: time.Now(),
		Period:    fmt.Sprintf("%d days", days),
		StartDate: startDate,
		EndDate:   today,
	}, nil
}

// GetContractDeploymentChart returns daily contract deployment statistics
func (s *Service) GetContractDeploymentChart(ctx context.Context, days int) (*ChartResponse, error) {
	if days <= 0 {
		days = s.config.DefaultHistoryDays
	}
	if days > s.config.MaxDataPoints {
		days = s.config.MaxDataPoints
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	startDate := today.AddDate(0, 0, -days)

	query := `
		SELECT
			DATE(first_seen) as date,
			COUNT(*) as deployments
		FROM cchain_addresses
		WHERE is_contract = true
		  AND first_seen >= $1
		  AND first_seen < $2
		GROUP BY DATE(first_seen)
		ORDER BY date DESC
	`

	rows, err := s.db.QueryContext(ctx, query, startDate, today)
	if err != nil {
		return nil, fmt.Errorf("query contract deployment stats: %w", err)
	}
	defer rows.Close()

	type ContractDeploymentStats struct {
		Date        time.Time `json:"date"`
		Deployments int64     `json:"deployments"`
	}

	var stats []ContractDeploymentStats
	for rows.Next() {
		var stat ContractDeploymentStats
		if err := rows.Scan(&stat.Date, &stat.Deployments); err != nil {
			return nil, fmt.Errorf("scan contract deployment stats: %w", err)
		}
		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate contract deployment stats: %w", err)
	}

	return &ChartResponse{
		ChartData: stats,
		UpdatedAt: time.Now(),
		Period:    fmt.Sprintf("%d days", days),
		StartDate: startDate,
		EndDate:   today,
	}, nil
}

// GetTokenHoldersChart returns token holder count over time
func (s *Service) GetTokenHoldersChart(ctx context.Context, tokenHash string, days int) (*ChartResponse, error) {
	if days <= 0 {
		days = s.config.DefaultHistoryDays
	}
	if days > s.config.MaxDataPoints {
		days = s.config.MaxDataPoints
	}

	tokenHash = normalizeAddress(tokenHash)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	startDate := today.AddDate(0, 0, -days)

	// This query counts unique holders by date based on transfer history
	// It's an approximation since we don't track historical balances
	query := `
		SELECT
			date,
			COUNT(DISTINCT holder) as holder_count
		FROM (
			SELECT DATE(timestamp) as date, to_address as holder FROM cchain_token_transfers
			WHERE token_hash = $1 AND timestamp >= $2 AND timestamp < $3
			UNION
			SELECT DATE(timestamp) as date, from_address as holder FROM cchain_token_transfers
			WHERE token_hash = $1 AND timestamp >= $2 AND timestamp < $3
		) sub
		GROUP BY date
		ORDER BY date DESC
	`

	rows, err := s.db.QueryContext(ctx, query, tokenHash, startDate, today)
	if err != nil {
		return nil, fmt.Errorf("query token holder stats: %w", err)
	}
	defer rows.Close()

	type TokenHolderStats struct {
		Date        time.Time `json:"date"`
		HolderCount int64     `json:"holder_count"`
	}

	var stats []TokenHolderStats
	for rows.Next() {
		var stat TokenHolderStats
		if err := rows.Scan(&stat.Date, &stat.HolderCount); err != nil {
			return nil, fmt.Errorf("scan token holder stats: %w", err)
		}
		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate token holder stats: %w", err)
	}

	return &ChartResponse{
		ChartData: stats,
		UpdatedAt: time.Now(),
		Period:    fmt.Sprintf("%d days", days),
		StartDate: startDate,
		EndDate:   today,
	}, nil
}

// HourlyStats represents hourly aggregated data
type HourlyStats struct {
	Hour  time.Time `json:"hour"`
	Value int64     `json:"value"`
}

// GetHourlyTransactionChart returns hourly transaction counts for the last 24 hours
func (s *Service) GetHourlyTransactionChart(ctx context.Context) (*ChartResponse, error) {
	now := time.Now().UTC()
	startTime := now.Add(-24 * time.Hour)

	query := `
		SELECT
			DATE_TRUNC('hour', timestamp) as hour,
			COUNT(*) as tx_count
		FROM cchain_transactions
		WHERE timestamp >= $1 AND timestamp < $2
		GROUP BY DATE_TRUNC('hour', timestamp)
		ORDER BY hour DESC
	`

	rows, err := s.db.QueryContext(ctx, query, startTime, now)
	if err != nil {
		return nil, fmt.Errorf("query hourly transaction stats: %w", err)
	}
	defer rows.Close()

	var stats []HourlyStats
	for rows.Next() {
		var stat HourlyStats
		if err := rows.Scan(&stat.Hour, &stat.Value); err != nil {
			return nil, fmt.Errorf("scan hourly transaction stats: %w", err)
		}
		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hourly transaction stats: %w", err)
	}

	return &ChartResponse{
		ChartData: stats,
		UpdatedAt: time.Now(),
		Period:    "24 hours",
		StartDate: startTime,
		EndDate:   now,
	}, nil
}

// GetHourlyGasChart returns hourly gas usage for the last 24 hours
func (s *Service) GetHourlyGasChart(ctx context.Context) (*ChartResponse, error) {
	now := time.Now().UTC()
	startTime := now.Add(-24 * time.Hour)

	query := `
		SELECT
			DATE_TRUNC('hour', timestamp) as hour,
			COALESCE(SUM(gas_used), 0) as gas_used
		FROM cchain_transactions
		WHERE timestamp >= $1 AND timestamp < $2
		GROUP BY DATE_TRUNC('hour', timestamp)
		ORDER BY hour DESC
	`

	rows, err := s.db.QueryContext(ctx, query, startTime, now)
	if err != nil {
		return nil, fmt.Errorf("query hourly gas stats: %w", err)
	}
	defer rows.Close()

	var stats []HourlyStats
	for rows.Next() {
		var stat HourlyStats
		if err := rows.Scan(&stat.Hour, &stat.Value); err != nil {
			return nil, fmt.Errorf("scan hourly gas stats: %w", err)
		}
		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hourly gas stats: %w", err)
	}

	return &ChartResponse{
		ChartData: stats,
		UpdatedAt: time.Now(),
		Period:    "24 hours",
		StartDate: startTime,
		EndDate:   now,
	}, nil
}

// InvalidateCache clears all cached chart data
func (s *Service) InvalidateCache() {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()

	s.cache.txStats = nil
	s.cache.gasStats = nil
	s.cache.tokenStats = nil
	s.cache.addressStats = nil
	s.cache.blockStats = nil
}

// normalizeAddress ensures address is lowercase with 0x prefix
func normalizeAddress(addr string) string {
	if len(addr) == 0 {
		return addr
	}
	// Remove 0x or 0X prefix if present
	if len(addr) >= 2 && (addr[:2] == "0x" || addr[:2] == "0X") {
		addr = addr[2:]
	}
	// Add lowercase 0x prefix
	return "0x" + strings.ToLower(addr)
}
