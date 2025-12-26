// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package charts

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.DefaultHistoryDays != 30 {
		t.Errorf("DefaultHistoryDays = %d, want 30", config.DefaultHistoryDays)
	}
	if config.CacheTTL != 5*time.Minute {
		t.Errorf("CacheTTL = %v, want 5 minutes", config.CacheTTL)
	}
	if config.MaxDataPoints != 365 {
		t.Errorf("MaxDataPoints = %d, want 365", config.MaxDataPoints)
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
		{
			name:     "mixed case",
			input:    "0x742D35CC6634C0532925A3B844BC9E7595F2BD30",
			expected: "0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
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

func TestNewService(t *testing.T) {
	// Test with nil config
	service := NewService(nil, nil)
	if service == nil {
		t.Fatal("NewService returned nil")
	}
	if service.config.DefaultHistoryDays != 30 {
		t.Errorf("Default config not applied correctly")
	}

	// Test with custom config
	customConfig := &Config{
		DefaultHistoryDays: 60,
		CacheTTL:           10 * time.Minute,
		MaxDataPoints:      500,
	}
	service = NewService(nil, customConfig)
	if service.config.DefaultHistoryDays != 60 {
		t.Errorf("Custom config not applied correctly")
	}
}

func TestChartResponse_Struct(t *testing.T) {
	now := time.Now()
	response := &ChartResponse{
		ChartData: []DailyTransactionStats{
			{
				Date:             now,
				TransactionCount: 1000,
			},
		},
		UpdatedAt: now,
		Period:    "30 days",
		StartDate: now.AddDate(0, 0, -30),
		EndDate:   now,
	}

	if response.Period != "30 days" {
		t.Errorf("Period = %s, want '30 days'", response.Period)
	}
	if response.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
}

func TestDailyTransactionStats_Struct(t *testing.T) {
	stat := DailyTransactionStats{
		Date:                time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		TransactionCount:    5000,
		UniqueAddresses:     500,
		ContractDeployments: 10,
		TokenTransfers:      2000,
	}

	if stat.Date.IsZero() {
		t.Error("Date should be set")
	}
	if stat.TransactionCount <= 0 {
		t.Error("TransactionCount should be positive")
	}
}

func TestDailyGasStats_Struct(t *testing.T) {
	stat := DailyGasStats{
		Date:           time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		UtilizationPct: 75.5,
		BlockCount:     1000,
	}

	if stat.UtilizationPct < 0 || stat.UtilizationPct > 100 {
		t.Errorf("UtilizationPct = %f, should be between 0 and 100", stat.UtilizationPct)
	}
}

func TestDailyTokenTransferStats_Struct(t *testing.T) {
	stat := DailyTokenTransferStats{
		Date:          time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		TransferCount: 10000,
		UniqueTokens:  50,
		UniqueFrom:    500,
		UniqueTo:      600,
	}

	if stat.TransferCount <= 0 {
		t.Error("TransferCount should be positive")
	}
	if stat.UniqueTokens <= 0 {
		t.Error("UniqueTokens should be positive")
	}
}

func TestDailyAddressStats_Struct(t *testing.T) {
	stat := DailyAddressStats{
		Date:            time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		NewAddresses:    100,
		ActiveAddresses: 500,
		TotalAddresses:  10000,
	}

	if stat.TotalAddresses < stat.NewAddresses {
		t.Error("TotalAddresses should be >= NewAddresses")
	}
}

func TestDailyBlockStats_Struct(t *testing.T) {
	stat := DailyBlockStats{
		Date:             time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		BlockCount:       1000,
		AverageBlockTime: 2 * time.Second,
	}

	if stat.BlockCount <= 0 {
		t.Error("BlockCount should be positive")
	}
	if stat.AverageBlockTime <= 0 {
		t.Error("AverageBlockTime should be positive")
	}
}

func TestHourlyStats_Struct(t *testing.T) {
	stat := HourlyStats{
		Hour:  time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Value: 500,
	}

	if stat.Hour.IsZero() {
		t.Error("Hour should be set")
	}
	if stat.Value < 0 {
		t.Error("Value should not be negative")
	}
}

func TestMarketData_Struct(t *testing.T) {
	data := MarketData{
		Date:       time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		ClosePrice: 1250.50,
		MarketCap:  5000000000,
		Volume:     100000000,
		TVL:        500000000,
	}

	if data.ClosePrice <= 0 {
		t.Error("ClosePrice should be positive")
	}
	if data.MarketCap <= 0 {
		t.Error("MarketCap should be positive")
	}
}

func TestCache_InvalidateCache(t *testing.T) {
	config := DefaultConfig()
	service := NewService(nil, config)

	// Pre-populate cache
	now := time.Now()
	service.cache.mu.Lock()
	service.cache.txStats = []DailyTransactionStats{{Date: now, TransactionCount: 100}}
	service.cache.txStatsUpdated = now
	service.cache.gasStats = []DailyGasStats{{Date: now, BlockCount: 50}}
	service.cache.gasStatsUpdated = now
	service.cache.tokenStats = []DailyTokenTransferStats{{Date: now, TransferCount: 200}}
	service.cache.tokenStatsUpdated = now
	service.cache.addressStats = []DailyAddressStats{{Date: now, NewAddresses: 25}}
	service.cache.addressStatsUpdated = now
	service.cache.blockStats = []DailyBlockStats{{Date: now, BlockCount: 100}}
	service.cache.blockStatsUpdated = now
	service.cache.mu.Unlock()

	// Verify cache is populated
	service.cache.mu.RLock()
	if service.cache.txStats == nil {
		t.Error("txStats should be populated before invalidation")
	}
	service.cache.mu.RUnlock()

	// Invalidate
	service.InvalidateCache()

	// Verify cache is cleared
	service.cache.mu.RLock()
	defer service.cache.mu.RUnlock()

	if service.cache.txStats != nil {
		t.Error("txStats should be nil after invalidation")
	}
	if service.cache.gasStats != nil {
		t.Error("gasStats should be nil after invalidation")
	}
	if service.cache.tokenStats != nil {
		t.Error("tokenStats should be nil after invalidation")
	}
	if service.cache.addressStats != nil {
		t.Error("addressStats should be nil after invalidation")
	}
	if service.cache.blockStats != nil {
		t.Error("blockStats should be nil after invalidation")
	}
}

func TestDaysParameter_Bounds(t *testing.T) {
	// Test that days parameter is bounded correctly
	config := &Config{
		DefaultHistoryDays: 30,
		CacheTTL:           5 * time.Minute,
		MaxDataPoints:      100,
	}

	tests := []struct {
		input    int
		expected int
	}{
		{0, 30},     // Zero uses default
		{-5, 30},    // Negative uses default
		{50, 50},    // Within bounds
		{200, 100},  // Above max gets capped
	}

	for _, tt := range tests {
		days := tt.input
		if days <= 0 {
			days = config.DefaultHistoryDays
		}
		if days > config.MaxDataPoints {
			days = config.MaxDataPoints
		}
		if days != tt.expected {
			t.Errorf("boundDays(%d) = %d, want %d", tt.input, days, tt.expected)
		}
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

	t.Run("GetTransactionChart", func(t *testing.T) {
		response, err := service.GetTransactionChart(ctx, 7)
		if err != nil {
			t.Fatalf("GetTransactionChart failed: %v", err)
		}
		if response == nil {
			t.Fatal("GetTransactionChart returned nil")
		}
		if response.Period != "7 days" {
			t.Errorf("Period = %s, want '7 days'", response.Period)
		}
		if response.UpdatedAt.IsZero() {
			t.Error("UpdatedAt should be set")
		}
	})

	t.Run("GetGasUsageChart", func(t *testing.T) {
		response, err := service.GetGasUsageChart(ctx, 7)
		if err != nil {
			t.Fatalf("GetGasUsageChart failed: %v", err)
		}
		if response == nil {
			t.Fatal("GetGasUsageChart returned nil")
		}
	})

	t.Run("GetTokenTransferChart", func(t *testing.T) {
		response, err := service.GetTokenTransferChart(ctx, 7)
		if err != nil {
			t.Fatalf("GetTokenTransferChart failed: %v", err)
		}
		if response == nil {
			t.Fatal("GetTokenTransferChart returned nil")
		}
	})

	t.Run("GetAddressGrowthChart", func(t *testing.T) {
		response, err := service.GetAddressGrowthChart(ctx, 7)
		if err != nil {
			t.Fatalf("GetAddressGrowthChart failed: %v", err)
		}
		if response == nil {
			t.Fatal("GetAddressGrowthChart returned nil")
		}
	})

	t.Run("GetBlockChart", func(t *testing.T) {
		response, err := service.GetBlockChart(ctx, 7)
		if err != nil {
			t.Fatalf("GetBlockChart failed: %v", err)
		}
		if response == nil {
			t.Fatal("GetBlockChart returned nil")
		}
	})

	t.Run("GetContractDeploymentChart", func(t *testing.T) {
		response, err := service.GetContractDeploymentChart(ctx, 7)
		if err != nil {
			t.Fatalf("GetContractDeploymentChart failed: %v", err)
		}
		if response == nil {
			t.Fatal("GetContractDeploymentChart returned nil")
		}
	})

	t.Run("GetTokenHoldersChart", func(t *testing.T) {
		response, err := service.GetTokenHoldersChart(ctx, "0x0000000000000000000000000000000000000000", 7)
		if err != nil {
			t.Fatalf("GetTokenHoldersChart failed: %v", err)
		}
		if response == nil {
			t.Fatal("GetTokenHoldersChart returned nil")
		}
	})

	t.Run("GetHourlyTransactionChart", func(t *testing.T) {
		response, err := service.GetHourlyTransactionChart(ctx)
		if err != nil {
			t.Fatalf("GetHourlyTransactionChart failed: %v", err)
		}
		if response == nil {
			t.Fatal("GetHourlyTransactionChart returned nil")
		}
		if response.Period != "24 hours" {
			t.Errorf("Period = %s, want '24 hours'", response.Period)
		}
	})

	t.Run("GetHourlyGasChart", func(t *testing.T) {
		response, err := service.GetHourlyGasChart(ctx)
		if err != nil {
			t.Fatalf("GetHourlyGasChart failed: %v", err)
		}
		if response == nil {
			t.Fatal("GetHourlyGasChart returned nil")
		}
	})

	t.Run("CacheHitTest", func(t *testing.T) {
		// First call - should hit database
		_, err := service.GetTransactionChart(ctx, 7)
		if err != nil {
			t.Fatalf("First call failed: %v", err)
		}

		// Second call - should hit cache
		response, err := service.GetTransactionChart(ctx, 7)
		if err != nil {
			t.Fatalf("Second call failed: %v", err)
		}
		if response == nil {
			t.Fatal("Cached response is nil")
		}
	})

	t.Run("DaysBoundaryTest", func(t *testing.T) {
		// Test with default days (0)
		response, err := service.GetTransactionChart(ctx, 0)
		if err != nil {
			t.Fatalf("Default days call failed: %v", err)
		}
		if response.Period != "30 days" {
			t.Errorf("Period = %s, want '30 days'", response.Period)
		}

		// Test with max days exceeded
		service.config.MaxDataPoints = 50
		response, err = service.GetTransactionChart(ctx, 100)
		if err != nil {
			t.Fatalf("Max days call failed: %v", err)
		}
		if response.Period != "50 days" {
			t.Errorf("Period = %s, want '50 days'", response.Period)
		}
	})
}

func BenchmarkNormalizeAddress(b *testing.B) {
	addresses := []string{
		"0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
		"0X742D35CC6634C0532925A3B844BC9E7595F2BD30",
		"742d35cc6634c0532925a3b844bc9e7595f2bd30",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, addr := range addresses {
			normalizeAddress(addr)
		}
	}
}

func BenchmarkCacheLookup(b *testing.B) {
	config := DefaultConfig()
	service := NewService(nil, config)

	// Pre-populate cache
	now := time.Now()
	service.cache.mu.Lock()
	service.cache.txStats = make([]DailyTransactionStats, 30)
	for i := 0; i < 30; i++ {
		service.cache.txStats[i] = DailyTransactionStats{
			Date:             now.AddDate(0, 0, -i),
			TransactionCount: int64(1000 + i*100),
		}
	}
	service.cache.txStatsUpdated = now
	service.cache.mu.Unlock()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.cache.mu.RLock()
		_ = service.cache.txStats
		_ = time.Since(service.cache.txStatsUpdated) < service.config.CacheTTL
		service.cache.mu.RUnlock()
	}
}
