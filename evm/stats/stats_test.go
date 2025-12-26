// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package stats

import (
	"context"
	"database/sql"
	"math/big"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.CacheTTL != 5*time.Minute {
		t.Errorf("CacheTTL = %v, want 5 minutes", config.CacheTTL)
	}
	if config.GasPriceBlocks != 200 {
		t.Errorf("GasPriceBlocks = %d, want 200", config.GasPriceBlocks)
	}
	if config.SlowPercentile != 35.0 {
		t.Errorf("SlowPercentile = %f, want 35.0", config.SlowPercentile)
	}
	if config.AveragePercentile != 60.0 {
		t.Errorf("AveragePercentile = %f, want 60.0", config.AveragePercentile)
	}
	if config.FastPercentile != 90.0 {
		t.Errorf("FastPercentile = %f, want 90.0", config.FastPercentile)
	}
	if config.SimpleTransactionGas != 21000 {
		t.Errorf("SimpleTransactionGas = %d, want 21000", config.SimpleTransactionGas)
	}
	if config.CounterUpdateInterval != 30*time.Second {
		t.Errorf("CounterUpdateInterval = %v, want 30s", config.CounterUpdateInterval)
	}
	if config.BlockTimeBlocks != 100 {
		t.Errorf("BlockTimeBlocks = %d, want 100", config.BlockTimeBlocks)
	}
}

func TestWeiToGwei(t *testing.T) {
	tests := []struct {
		name     string
		wei      *big.Int
		expected string
	}{
		{
			name:     "nil",
			wei:      nil,
			expected: "0",
		},
		{
			name:     "zero",
			wei:      big.NewInt(0),
			expected: "0",
		},
		{
			name:     "1 gwei",
			wei:      big.NewInt(1000000000),
			expected: "1.000000",
		},
		{
			name:     "10 gwei",
			wei:      big.NewInt(10000000000),
			expected: "10.000000",
		},
		{
			name:     "0.5 gwei",
			wei:      big.NewInt(500000000),
			expected: "0.500000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := weiToGwei(tt.wei)
			if result != tt.expected {
				t.Errorf("weiToGwei(%v) = %s, want %s", tt.wei, result, tt.expected)
			}
		})
	}
}

func TestWeiToGweiFloat(t *testing.T) {
	tests := []struct {
		name     string
		wei      *big.Int
		expected float64
	}{
		{
			name:     "nil",
			wei:      nil,
			expected: 0,
		},
		{
			name:     "zero",
			wei:      big.NewInt(0),
			expected: 0,
		},
		{
			name:     "1 gwei",
			wei:      big.NewInt(1000000000),
			expected: 1.0,
		},
		{
			name:     "100 gwei",
			wei:      big.NewInt(100000000000),
			expected: 100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := weiToGweiFloat(tt.wei)
			if result != tt.expected {
				t.Errorf("weiToGweiFloat(%v) = %f, want %f", tt.wei, result, tt.expected)
			}
		})
	}
}

func TestNormalizeAddress(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase with prefix",
			input:    "0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
			expected: "0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
		},
		{
			name:     "uppercase prefix",
			input:    "0X742d35cc6634c0532925a3b844bc9e7595f2bd30",
			expected: "0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
		},
		{
			name:     "no prefix",
			input:    "742d35cc6634c0532925a3b844bc9e7595f2bd30",
			expected: "0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeAddress(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeAddress(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCreateGasPrice(t *testing.T) {
	gasPrice := big.NewInt(50000000000) // 50 gwei
	baseFee := big.NewInt(30000000000)  // 30 gwei
	estimatedTime := 2000.0             // 2 seconds
	simpleGas := int64(21000)

	result := createGasPrice(gasPrice, baseFee, estimatedTime, simpleGas)

	if result == nil {
		t.Fatal("createGasPrice returned nil")
	}

	if result.Price != 50.0 {
		t.Errorf("Price = %f, want 50.0", result.Price)
	}

	if result.BaseFee != 30.0 {
		t.Errorf("BaseFee = %f, want 30.0", result.BaseFee)
	}

	if result.PriorityFee != 20.0 {
		t.Errorf("PriorityFee = %f, want 20.0", result.PriorityFee)
	}

	if result.Time != estimatedTime {
		t.Errorf("Time = %f, want %f", result.Time, estimatedTime)
	}

	if result.Wei != "50000000000" {
		t.Errorf("Wei = %s, want 50000000000", result.Wei)
	}
}

func TestNewCache(t *testing.T) {
	ttl := 10 * time.Minute
	cache := NewCache(ttl)

	if cache == nil {
		t.Fatal("NewCache returned nil")
	}

	if cache.ttl != ttl {
		t.Errorf("cache.ttl = %v, want %v", cache.ttl, ttl)
	}

	if cache.tokenStats == nil {
		t.Error("cache.tokenStats should be initialized")
	}
}

func TestNewService(t *testing.T) {
	config := DefaultConfig()
	service := NewService(nil, config)

	if service == nil {
		t.Fatal("NewService returned nil")
	}

	if service.cache == nil {
		t.Error("service.cache should be initialized")
	}

	if service.config.GasPriceBlocks != config.GasPriceBlocks {
		t.Errorf("service.config not properly set")
	}
}

func TestCache_InvalidateCache(t *testing.T) {
	config := DefaultConfig()
	service := NewService(nil, config)

	// Pre-populate cache
	service.cache.mu.Lock()
	service.cache.gasPrices = &GasPrices{UpdatedAt: time.Now()}
	service.cache.counters = &Counters{TotalBlocks: 100}
	service.cache.blockTime = &BlockTimeStats{AverageBlockTime: time.Second}
	service.cache.tokenStats["0x123"] = &TokenStats{HolderCount: 50}
	service.cache.mu.Unlock()

	// Invalidate
	service.InvalidateCache()

	service.cache.mu.RLock()
	defer service.cache.mu.RUnlock()

	if service.cache.gasPrices != nil {
		t.Error("gasPrices should be nil after invalidation")
	}
	if service.cache.counters != nil {
		t.Error("counters should be nil after invalidation")
	}
	if service.cache.blockTime != nil {
		t.Error("blockTime should be nil after invalidation")
	}
	if len(service.cache.tokenStats) != 0 {
		t.Error("tokenStats should be empty after invalidation")
	}
}

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("postgres", "postgres://localhost/explorer_evm_test?sslmode=disable")
	if err != nil {
		t.Skip("No test database available:", err)
	}
	if err := db.Ping(); err != nil {
		t.Skip("Cannot connect to test database:", err)
	}
	return db
}

func TestService_Integration(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	config := DefaultConfig()
	config.CacheTTL = 1 * time.Second // Short TTL for testing
	service := NewService(db, config)
	ctx := context.Background()

	t.Run("GetCounters", func(t *testing.T) {
		counters, err := service.GetCounters(ctx)
		if err != nil {
			t.Fatalf("GetCounters failed: %v", err)
		}
		if counters == nil {
			t.Fatal("GetCounters returned nil")
		}
		// Values can be 0 for empty test database
		if counters.UpdatedAt.IsZero() {
			t.Error("UpdatedAt should be set")
		}
	})

	t.Run("GetGasPrices", func(t *testing.T) {
		prices, err := service.GetGasPrices(ctx)
		if err != nil {
			t.Fatalf("GetGasPrices failed: %v", err)
		}
		if prices == nil {
			t.Fatal("GetGasPrices returned nil")
		}
		if prices.UpdatedAt.IsZero() {
			t.Error("UpdatedAt should be set")
		}
	})

	t.Run("GetAverageBlockTime", func(t *testing.T) {
		stats, err := service.GetAverageBlockTime(ctx)
		if err != nil {
			t.Fatalf("GetAverageBlockTime failed: %v", err)
		}
		if stats == nil {
			t.Fatal("GetAverageBlockTime returned nil")
		}
		if stats.UpdatedAt.IsZero() {
			t.Error("UpdatedAt should be set")
		}
	})

	t.Run("GetDailyTransactionStats", func(t *testing.T) {
		stats, err := service.GetDailyTransactionStats(ctx, 7)
		if err != nil {
			t.Fatalf("GetDailyTransactionStats failed: %v", err)
		}
		// Can be empty for test database
		_ = stats
	})

	t.Run("GetHourlyGasUsage", func(t *testing.T) {
		stats, err := service.GetHourlyGasUsage(ctx)
		if err != nil {
			t.Fatalf("GetHourlyGasUsage failed: %v", err)
		}
		// Can be empty for test database
		_ = stats
	})

	t.Run("GetAddressStats", func(t *testing.T) {
		stats, err := service.GetAddressStats(ctx, "0x0000000000000000000000000000000000000000")
		if err != nil {
			t.Fatalf("GetAddressStats failed: %v", err)
		}
		if stats == nil {
			t.Fatal("GetAddressStats returned nil")
		}
	})

	t.Run("GetTokenStats", func(t *testing.T) {
		stats, err := service.GetTokenStats(ctx, "0x0000000000000000000000000000000000000000")
		if err != nil {
			t.Fatalf("GetTokenStats failed: %v", err)
		}
		if stats == nil {
			t.Fatal("GetTokenStats returned nil")
		}
	})
}

func TestGasPrices_Struct(t *testing.T) {
	prices := &GasPrices{
		Slow: &GasPrice{
			Price:       10,
			PriorityFee: 2,
			BaseFee:     8,
			Time:        5000,
		},
		Average: &GasPrice{
			Price:       20,
			PriorityFee: 5,
			BaseFee:     15,
			Time:        3000,
		},
		Fast: &GasPrice{
			Price:       50,
			PriorityFee: 20,
			BaseFee:     30,
			Time:        1000,
		},
		UpdatedAt: time.Now(),
		BaseFee:   "30.000000",
	}

	if prices.Slow.Price >= prices.Average.Price {
		t.Error("Slow price should be less than average")
	}
	if prices.Average.Price >= prices.Fast.Price {
		t.Error("Average price should be less than fast")
	}
	if prices.Slow.Time <= prices.Average.Time {
		t.Error("Slow time should be greater than average")
	}
	if prices.Average.Time <= prices.Fast.Time {
		t.Error("Average time should be greater than fast")
	}
}

func TestCounters_Struct(t *testing.T) {
	counters := &Counters{
		TotalBlocks:         1000000,
		TotalTransactions:   5000000,
		TotalAddresses:      100000,
		TotalContracts:      10000,
		TotalTokens:         500,
		TotalTokenTransfers: 2000000,
		AverageBlockTime:    2.0,
		TPS24h:              15.5,
		GasUsed24h:          1000000000,
		UpdatedAt:           time.Now(),
	}

	if counters.TotalContracts > counters.TotalAddresses {
		t.Error("Total contracts should not exceed total addresses")
	}
	if counters.TotalTokens > counters.TotalContracts {
		t.Error("Total tokens should not exceed total contracts")
	}
}

func TestDailyStats_Struct(t *testing.T) {
	stat := DailyStats{
		Date:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Count: 1000,
	}

	if stat.Date.IsZero() {
		t.Error("Date should be set")
	}
	if stat.Count <= 0 {
		t.Error("Count should be positive")
	}
}

func TestHourlyStats_Struct(t *testing.T) {
	stat := HourlyStats{
		Hour:  time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Value: 500000,
	}

	if stat.Hour.IsZero() {
		t.Error("Hour should be set")
	}
	if stat.Value <= 0 {
		t.Error("Value should be positive")
	}
}

func BenchmarkWeiToGwei(b *testing.B) {
	wei := big.NewInt(50000000000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		weiToGwei(wei)
	}
}

func BenchmarkWeiToGweiFloat(b *testing.B) {
	wei := big.NewInt(50000000000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		weiToGweiFloat(wei)
	}
}

func BenchmarkNormalizeAddress(b *testing.B) {
	addresses := []string{
		"0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
		"0X742d35cc6634c0532925a3b844bc9e7595f2bd30",
		"742d35cc6634c0532925a3b844bc9e7595f2bd30",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, addr := range addresses {
			normalizeAddress(addr)
		}
	}
}

func BenchmarkCreateGasPrice(b *testing.B) {
	gasPrice := big.NewInt(50000000000)
	baseFee := big.NewInt(30000000000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		createGasPrice(gasPrice, baseFee, 2000.0, 21000)
	}
}
