// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package search

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestClassifyQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{
			name:     "ethereum address",
			query:    "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD30",
			expected: "address_hash",
		},
		{
			name:     "transaction hash",
			query:    "0x5c504ed432cb51138bcf09aa5e8a410dd4a1e204ef84bfed1be16dfba1b22060",
			expected: "tx_hash",
		},
		{
			name:     "block number",
			query:    "12345678",
			expected: "block_number",
		},
		{
			name:     "text search",
			query:    "USDC",
			expected: "text",
		},
		{
			name:     "partial text",
			query:    "wrapped eth",
			expected: "text",
		},
		{
			name:     "short hex",
			query:    "0x123",
			expected: "text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyQuery(tt.query)
			if result != tt.expected {
				t.Errorf("classifyQuery(%q) = %q, want %q", tt.query, result, tt.expected)
			}
		})
	}
}

func TestIsValidTxHash(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid tx hash lowercase",
			input:    "0x5c504ed432cb51138bcf09aa5e8a410dd4a1e204ef84bfed1be16dfba1b22060",
			expected: true,
		},
		{
			name:     "valid tx hash uppercase prefix",
			input:    "0X5c504ed432cb51138bcf09aa5e8a410dd4a1e204ef84bfed1be16dfba1b22060",
			expected: true,
		},
		{
			name:     "too short",
			input:    "0x5c504ed432cb51138bcf09aa5e8a410dd4a1e204",
			expected: false,
		},
		{
			name:     "too long",
			input:    "0x5c504ed432cb51138bcf09aa5e8a410dd4a1e204ef84bfed1be16dfba1b2206000",
			expected: false,
		},
		{
			name:     "no prefix",
			input:    "5c504ed432cb51138bcf09aa5e8a410dd4a1e204ef84bfed1be16dfba1b22060",
			expected: false,
		},
		{
			name:     "invalid hex",
			input:    "0xzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidTxHash(tt.input)
			if result != tt.expected {
				t.Errorf("isValidTxHash(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsValidAddress(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid address lowercase",
			input:    "0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
			expected: true,
		},
		{
			name:     "valid address mixed case",
			input:    "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD30",
			expected: true,
		},
		{
			name:     "too short",
			input:    "0x742d35cc6634c0532925a3b844bc9e7595f2",
			expected: false,
		},
		{
			name:     "too long",
			input:    "0x742d35cc6634c0532925a3b844bc9e7595f2bd3000",
			expected: false,
		},
		{
			name:     "no prefix",
			input:    "742d35cc6634c0532925a3b844bc9e7595f2bd30",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidAddress(tt.input)
			if result != tt.expected {
				t.Errorf("isValidAddress(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestPrepareSearchTerm(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single word",
			input:    "USDC",
			expected: "USDC:*",
		},
		{
			name:     "two words",
			input:    "Wrapped ETH",
			expected: "Wrapped:* & ETH:*",
		},
		{
			name:     "with special chars",
			input:    "USD-Coin",
			expected: "USD:* & Coin:*",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "numbers and letters",
			input:    "Token123",
			expected: "Token123:*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prepareSearchTerm(tt.input)
			if result != tt.expected {
				t.Errorf("prepareSearchTerm(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSortByPriority(t *testing.T) {
	now := time.Now()
	results := []Result{
		{Type: TypeToken, Name: "Low Priority", Priority: PriorityPartial, HolderCount: 100},
		{Type: TypeToken, Name: "High Priority", Priority: PriorityExactMatch, HolderCount: 50},
		{Type: TypeContract, Name: "Medium Priority", Priority: PriorityContract, HolderCount: 0},
		{Type: TypeToken, Name: "Same Priority High Holders", Priority: PriorityToken, HolderCount: 1000},
		{Type: TypeToken, Name: "Same Priority Low Holders", Priority: PriorityToken, HolderCount: 10},
		{Type: TypeAddress, Hash: "0x123", Priority: PriorityExactMatch, InsertedAt: &now},
	}

	sortByPriority(results)

	// Check order
	if results[0].Priority != PriorityExactMatch {
		t.Errorf("First result should have highest priority, got %d", results[0].Priority)
	}
	if results[len(results)-1].Priority != PriorityPartial {
		t.Errorf("Last result should have lowest priority, got %d", results[len(results)-1].Priority)
	}

	// Check secondary sort by holder count within same priority
	for i := 1; i < len(results); i++ {
		if results[i].Priority == results[i-1].Priority {
			if results[i].HolderCount > results[i-1].HolderCount {
				t.Errorf("Within same priority, higher holder count should come first")
			}
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.PageSize != 50 {
		t.Errorf("PageSize = %d, want 50", config.PageSize)
	}
	if config.MinQueryLen != 3 {
		t.Errorf("MinQueryLen = %d, want 3", config.MinQueryLen)
	}
	if config.MaxResults != 100 {
		t.Errorf("MaxResults = %d, want 100", config.MaxResults)
	}
	if !config.EnableENS {
		t.Error("EnableENS should be true by default")
	}
	if !config.EnableLabels {
		t.Error("EnableLabels should be true by default")
	}
}

// MockDB helper for testing with database
type mockDB struct {
	*sql.DB
}

func setupTestDB(t *testing.T) *sql.DB {
	// Skip if no database connection available
	db, err := sql.Open("postgres", "postgres://localhost/explorer_evm_test?sslmode=disable")
	if err != nil {
		t.Skip("No test database available:", err)
	}
	if err := db.Ping(); err != nil {
		t.Skip("Cannot connect to test database:", err)
	}
	return db
}

func TestEngine_Integration(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	engine := NewEngine(db)
	ctx := context.Background()

	// Test search with empty query
	t.Run("empty query", func(t *testing.T) {
		resp, err := engine.Search(ctx, "", PagingParams{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(resp.Items) != 0 {
			t.Errorf("Expected 0 results for empty query, got %d", len(resp.Items))
		}
	})

	// Test quick search with short query
	t.Run("short query quick search", func(t *testing.T) {
		resp, err := engine.QuickSearch(ctx, "ab")
		if err != nil {
			t.Fatalf("QuickSearch failed: %v", err)
		}
		if len(resp.Items) != 0 {
			t.Errorf("Expected 0 results for short query, got %d", len(resp.Items))
		}
	})

	// Test redirect check
	t.Run("check redirect no match", func(t *testing.T) {
		resp, err := engine.CheckRedirect(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("CheckRedirect failed: %v", err)
		}
		if resp.Redirect {
			t.Error("Expected no redirect for nonexistent query")
		}
	})
}

func TestResultTypes(t *testing.T) {
	// Verify all result types are defined
	types := []ResultType{TypeBlock, TypeTransaction, TypeAddress, TypeToken, TypeContract, TypeLabel, TypeENS}

	for _, typ := range types {
		if typ == "" {
			t.Error("Result type should not be empty")
		}
	}

	// Check string values
	if TypeBlock != "block" {
		t.Errorf("TypeBlock = %q, want 'block'", TypeBlock)
	}
	if TypeTransaction != "transaction" {
		t.Errorf("TypeTransaction = %q, want 'transaction'", TypeTransaction)
	}
	if TypeAddress != "address" {
		t.Errorf("TypeAddress = %q, want 'address'", TypeAddress)
	}
	if TypeToken != "token" {
		t.Errorf("TypeToken = %q, want 'token'", TypeToken)
	}
	if TypeContract != "contract" {
		t.Errorf("TypeContract = %q, want 'contract'", TypeContract)
	}
}

func TestPriorityLevels(t *testing.T) {
	// Ensure priority levels are properly ordered
	if PriorityENS <= PriorityExactMatch {
		t.Error("ENS priority should be highest")
	}
	if PriorityExactMatch <= PriorityLabel {
		t.Error("Exact match should be higher than label")
	}
	if PriorityLabel <= PriorityToken {
		t.Error("Label should be higher than token")
	}
	if PriorityToken <= PriorityContract {
		t.Error("Token should be higher than contract")
	}
	if PriorityContract <= PriorityPartial {
		t.Error("Contract should be higher than partial")
	}
	if PriorityPartial != 0 {
		t.Error("Partial priority should be 0")
	}
}

func BenchmarkClassifyQuery(b *testing.B) {
	queries := []string{
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD30",
		"0x5c504ed432cb51138bcf09aa5e8a410dd4a1e204ef84bfed1be16dfba1b22060",
		"12345678",
		"USDC",
		"Wrapped Ethereum",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, q := range queries {
			classifyQuery(q)
		}
	}
}

func BenchmarkPrepareSearchTerm(b *testing.B) {
	queries := []string{
		"USDC",
		"Wrapped Ethereum Token",
		"BTC-USD-LP",
		"Token123",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, q := range queries {
			prepareSearchTerm(q)
		}
	}
}

func BenchmarkSortByPriority(b *testing.B) {
	results := make([]Result, 100)
	for i := range results {
		results[i] = Result{
			Type:        TypeToken,
			Priority:    i % 6,
			HolderCount: uint64(i * 100),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Make a copy to avoid sorting already sorted slice
		temp := make([]Result, len(results))
		copy(temp, results)
		sortByPriority(temp)
	}
}
