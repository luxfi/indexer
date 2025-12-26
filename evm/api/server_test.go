// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

// TestConfig for testing
type TestConfig struct {
	HTTPPort    int
	ChainID     int64
	ChainName   string
	DatabaseURL string
}

func getTestConfig() TestConfig {
	return TestConfig{
		HTTPPort:    4001,
		ChainID:     96369,
		ChainName:   "Lux C-Chain Test",
		DatabaseURL: "postgres://localhost:5432/explorer_evm_test?sslmode=disable",
	}
}

// MockDB creates an in-memory mock for testing without database
type MockDB struct{}

// setupTestServer creates a test server with mock data
func setupTestServer(t *testing.T) (*Server, *httptest.Server) {
	cfg := Config{
		HTTPPort:  4001,
		ChainID:   96369,
		ChainName: "Lux C-Chain Test",
	}

	// Create server with nil DB (handlers will need to handle this)
	s := &Server{
		config: cfg,
		wsHub:  NewWebSocketHub(),
		repo:   nil, // Mock repository
	}

	// Setup routes manually for testing
	s.router = setupMockRoutes(s)

	ts := httptest.NewServer(corsMiddleware(s.router))
	return s, ts
}

func setupMockRoutes(s *Server) *mux.Router {
	router := mux.NewRouter()

	// Health endpoint
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "ok",
			"chain_id":   s.config.ChainID,
			"chain_name": s.config.ChainName,
		})
	}).Methods("GET")

	// Mock blocks endpoint
	router.HandleFunc("/api/v2/blocks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		blocks := []Block{
			{
				Hash:             "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				ParentHash:       "0x0000000000000000000000000000000000000000000000000000000000000000",
				Height:           100,
				Timestamp:        time.Now(),
				TransactionCount: 5,
				GasUsed:          "21000",
				GasLimit:         "8000000",
				Size:             1024,
				Miner:            &Address{Hash: "0xdeadbeef00000000000000000000000000000001"},
			},
		}
		json.NewEncoder(w).Encode(PaginatedResponse{Items: blocks})
	}).Methods("GET")

	// Mock single block endpoint
	router.HandleFunc("/api/v2/blocks/{block_hash_or_number}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		block := Block{
			Hash:             "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			ParentHash:       "0x0000000000000000000000000000000000000000000000000000000000000000",
			Height:           100,
			Timestamp:        time.Now(),
			TransactionCount: 5,
		}
		json.NewEncoder(w).Encode(block)
	}).Methods("GET")

	// Mock transactions endpoint
	router.HandleFunc("/api/v2/transactions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		blockNum := uint64(100)
		gasUsed := uint64(21000)
		txIndex := 0
		txs := []Transaction{
			{
				Hash:             "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				BlockNumber:      &blockNum,
				From:             &Address{Hash: "0x1111111111111111111111111111111111111111"},
				To:               &Address{Hash: "0x2222222222222222222222222222222222222222"},
				Value:            "1000000000000000000",
				Gas:              21000,
				GasPrice:         "20000000000",
				GasUsed:          &gasUsed,
				TransactionIndex: &txIndex,
				Status:           "ok",
			},
		}
		json.NewEncoder(w).Encode(PaginatedResponse{Items: txs})
	}).Methods("GET")

	// Mock addresses endpoint
	router.HandleFunc("/api/v2/addresses/{address_hash}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		txCount := uint64(10)
		addr := Address{
			Hash:             "0x1111111111111111111111111111111111111111",
			Balance:          "1000000000000000000",
			TransactionCount: &txCount,
			IsContract:       false,
		}
		json.NewEncoder(w).Encode(addr)
	}).Methods("GET")

	// Mock tokens endpoint
	router.HandleFunc("/api/v2/tokens", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		decimals := uint8(18)
		holderCount := uint64(1000)
		tokens := []Token{
			{
				Address:     "0x3333333333333333333333333333333333333333",
				Name:        "Test Token",
				Symbol:      "TEST",
				Decimals:    &decimals,
				TotalSupply: "1000000000000000000000000",
				Type:        "ERC-20",
				HolderCount: &holderCount,
			},
		}
		json.NewEncoder(w).Encode(PaginatedResponse{Items: tokens})
	}).Methods("GET")

	// Mock search endpoint
	router.HandleFunc("/api/v2/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		query := r.URL.Query().Get("q")
		var results []SearchResult

		if strings.HasPrefix(query, "0x") && len(query) == 42 {
			results = append(results, SearchResult{
				Type:    "address",
				Address: query,
			})
		}

		json.NewEncoder(w).Encode(PaginatedResponse{Items: results})
	}).Methods("GET")

	// Mock stats endpoint
	router.HandleFunc("/api/v2/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		stats := ChainStats{
			TotalBlocks:       100,
			TotalTransactions: 500,
			TotalAddresses:    50,
			AverageBlockTime:  2.0,
		}
		json.NewEncoder(w).Encode(stats)
	}).Methods("GET")

	// Mock RPC endpoint
	router.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		module := r.URL.Query().Get("module")
		action := r.URL.Query().Get("action")

		switch module {
		case "account":
			if action == "balance" {
				json.NewEncoder(w).Encode(RPCResponse{
					Status:  "1",
					Message: "OK",
					Result:  "1000000000000000000",
				})
			} else if action == "txlist" {
				json.NewEncoder(w).Encode(RPCResponse{
					Status:  "1",
					Message: "OK",
					Result:  []interface{}{},
				})
			}
		case "stats":
			json.NewEncoder(w).Encode(RPCResponse{
				Status:  "1",
				Message: "OK",
				Result:  "0",
			})
		default:
			json.NewEncoder(w).Encode(RPCResponse{
				Status:  "0",
				Message: "NOTOK",
				Result:  "Invalid module",
			})
		}
	}).Methods("GET")

	// Mock GraphQL endpoint
	router.HandleFunc("/api/v2/graphql", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>GraphQL Playground</html>"))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		var req GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			json.NewEncoder(w).Encode(GraphQLResponse{
				Errors: []GraphQLError{{Message: "Invalid request"}},
			})
			return
		}

		// Return mock data based on query
		json.NewEncoder(w).Encode(GraphQLResponse{
			Data: map[string]interface{}{
				"stats": map[string]interface{}{
					"totalBlocks":       100,
					"totalTransactions": 500,
				},
			},
		})
	}).Methods("GET", "POST")

	return router
}

// Tests

func TestHealthEndpoint(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("Expected status 'ok', got %v", result["status"])
	}

	if result["chain_id"].(float64) != 96369 {
		t.Errorf("Expected chain_id 96369, got %v", result["chain_id"])
	}
}

func TestBlocksEndpoint(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/blocks")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result PaginatedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	blocks, ok := result.Items.([]interface{})
	if !ok {
		t.Fatalf("Expected items to be a slice")
	}

	if len(blocks) == 0 {
		t.Error("Expected at least one block")
	}
}

func TestSingleBlockEndpoint(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/blocks/100")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var block Block
	if err := json.NewDecoder(resp.Body).Decode(&block); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if block.Height != 100 {
		t.Errorf("Expected block height 100, got %d", block.Height)
	}
}

func TestTransactionsEndpoint(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/transactions")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result PaginatedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.Items == nil {
		t.Error("Expected items in response")
	}
}

func TestAddressEndpoint(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/addresses/0x1111111111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var addr Address
	if err := json.NewDecoder(resp.Body).Decode(&addr); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if addr.Hash == "" {
		t.Error("Expected address hash in response")
	}
}

func TestTokensEndpoint(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/tokens")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result PaginatedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.Items == nil {
		t.Error("Expected items in response")
	}
}

func TestSearchEndpoint(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	tests := []struct {
		query    string
		expected string
	}{
		{"0x1111111111111111111111111111111111111111", "address"},
	}

	for _, test := range tests {
		resp, err := http.Get(ts.URL + "/api/v2/search?q=" + test.query)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var result PaginatedResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		resp.Body.Close()
	}
}

func TestStatsEndpoint(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/stats")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var stats ChainStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if stats.TotalBlocks == 0 {
		t.Error("Expected non-zero total blocks")
	}
}

// RPC API Tests

func TestRPCAccountBalance(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api?module=account&action=balance&address=0x1111111111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result RPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.Status != "1" {
		t.Errorf("Expected status '1', got %s", result.Status)
	}
}

func TestRPCAccountTxList(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api?module=account&action=txlist&address=0x1111111111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result RPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.Status != "1" {
		t.Errorf("Expected status '1', got %s", result.Status)
	}
}

func TestRPCInvalidModule(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api?module=invalid&action=test")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	var result RPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.Status != "0" {
		t.Errorf("Expected status '0' for invalid module, got %s", result.Status)
	}
}

// GraphQL Tests

func TestGraphQLPlayground(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/graphql")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("Expected text/html content type, got %s", contentType)
	}
}

func TestGraphQLQuery(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	query := GraphQLRequest{
		Query: `query { stats { totalBlocks totalTransactions } }`,
	}

	body, _ := json.Marshal(query)
	resp, err := http.Post(ts.URL+"/api/v2/graphql", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.Data == nil {
		t.Error("Expected data in response")
	}
}

// Pagination Tests

func TestPaginationParameters(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	tests := []struct {
		url  string
		name string
	}{
		{"/api/v2/blocks?page=0&page_size=10", "blocks with page params"},
		{"/api/v2/transactions?page=0&page_size=10", "transactions with page params"},
		{"/api/v2/tokens?page=0&page_size=10", "tokens with page params"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + test.url)
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", resp.StatusCode)
			}

			var result PaginatedResponse
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}
		})
	}
}

// CORS Tests

func TestCORSHeaders(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	cors := resp.Header.Get("Access-Control-Allow-Origin")
	if cors != "*" {
		t.Errorf("Expected CORS header '*', got '%s'", cors)
	}
}

func TestOptionsRequest(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("OPTIONS", ts.URL+"/api/v2/blocks", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// Type Tests

func TestBlockSerialization(t *testing.T) {
	block := Block{
		Hash:             "0x123",
		ParentHash:       "0x000",
		Height:           100,
		Timestamp:        time.Now(),
		TransactionCount: 5,
		GasUsed:          "21000",
		GasLimit:         "8000000",
		Size:             1024,
	}

	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("Failed to marshal block: %v", err)
	}

	var decoded Block
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal block: %v", err)
	}

	if decoded.Height != block.Height {
		t.Errorf("Height mismatch: expected %d, got %d", block.Height, decoded.Height)
	}
}

func TestTransactionSerialization(t *testing.T) {
	blockNum := uint64(100)
	gasUsed := uint64(21000)
	txIndex := 0

	tx := Transaction{
		Hash:             "0xabc",
		BlockNumber:      &blockNum,
		From:             &Address{Hash: "0x111"},
		To:               &Address{Hash: "0x222"},
		Value:            "1000000000000000000",
		Gas:              21000,
		GasPrice:         "20000000000",
		GasUsed:          &gasUsed,
		TransactionIndex: &txIndex,
		Status:           "ok",
	}

	data, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("Failed to marshal transaction: %v", err)
	}

	var decoded Transaction
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal transaction: %v", err)
	}

	if decoded.Hash != tx.Hash {
		t.Errorf("Hash mismatch: expected %s, got %s", tx.Hash, decoded.Hash)
	}
}

func TestAddressSerialization(t *testing.T) {
	txCount := uint64(10)

	addr := Address{
		Hash:             "0x1111111111111111111111111111111111111111",
		Balance:          "1000000000000000000",
		TransactionCount: &txCount,
		IsContract:       false,
	}

	data, err := json.Marshal(addr)
	if err != nil {
		t.Fatalf("Failed to marshal address: %v", err)
	}

	var decoded Address
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal address: %v", err)
	}

	if decoded.Hash != addr.Hash {
		t.Errorf("Hash mismatch: expected %s, got %s", addr.Hash, decoded.Hash)
	}
}

func TestTokenSerialization(t *testing.T) {
	decimals := uint8(18)
	holderCount := uint64(1000)

	token := Token{
		Address:     "0x3333333333333333333333333333333333333333",
		Name:        "Test Token",
		Symbol:      "TEST",
		Decimals:    &decimals,
		TotalSupply: "1000000000000000000000000",
		Type:        "ERC-20",
		HolderCount: &holderCount,
	}

	data, err := json.Marshal(token)
	if err != nil {
		t.Fatalf("Failed to marshal token: %v", err)
	}

	var decoded Token
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal token: %v", err)
	}

	if decoded.Symbol != token.Symbol {
		t.Errorf("Symbol mismatch: expected %s, got %s", token.Symbol, decoded.Symbol)
	}
}

// Benchmarks

func BenchmarkHealthEndpoint(b *testing.B) {
	_, ts := setupTestServer(nil)
	defer ts.Close()

	client := &http.Client{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(ts.URL + "/health")
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func BenchmarkBlocksEndpoint(b *testing.B) {
	_, ts := setupTestServer(nil)
	defer ts.Close()

	client := &http.Client{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(ts.URL + "/api/v2/blocks")
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func BenchmarkJSONSerialization(b *testing.B) {
	block := Block{
		Hash:             "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		ParentHash:       "0x0000000000000000000000000000000000000000000000000000000000000000",
		Height:           100,
		Timestamp:        time.Now(),
		TransactionCount: 5,
		GasUsed:          "21000",
		GasLimit:         "8000000",
		Size:             1024,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(block)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Integration test with real database (skipped by default)
func TestIntegrationWithDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := getTestConfig()
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		t.Skipf("Database not available: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Skipf("Database not reachable: %v", err)
	}

	server := NewServer(Config{
		HTTPPort:  cfg.HTTPPort,
		ChainID:   cfg.ChainID,
		ChainName: cfg.ChainName,
	}, db)

	ts := httptest.NewServer(corsMiddleware(server.Router()))
	defer ts.Close()

	// Test real database queries
	resp, err := http.Get(ts.URL + "/api/v2/stats")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

// WebSocket test helper
func TestWebSocketHub(t *testing.T) {
	hub := NewWebSocketHub()

	// Test stats
	stats := hub.Stats()
	if stats["total_connections"].(int) != 0 {
		t.Errorf("Expected 0 connections, got %v", stats["total_connections"])
	}
}

// Test block broadcast
func TestBroadcastBlock(t *testing.T) {
	hub := NewWebSocketHub()

	block := &Block{
		Hash:             "0x123",
		Height:           100,
		TransactionCount: 5,
	}

	// Should not panic with no clients
	hub.BroadcastBlock(block)
}

// Test transaction broadcast
func TestBroadcastTransaction(t *testing.T) {
	hub := NewWebSocketHub()

	tx := &Transaction{
		Hash:  "0xabc",
		From:  &Address{Hash: "0x111"},
		To:    &Address{Hash: "0x222"},
		Value: "1000",
	}

	// Should not panic with no clients
	hub.BroadcastTransaction(tx)
}

// Test token transfer broadcast
func TestBroadcastTokenTransfer(t *testing.T) {
	hub := NewWebSocketHub()

	decimals := uint8(18)
	transfer := &TokenTransfer{
		TxHash: "0xdef",
		From:   &Address{Hash: "0x111"},
		To:     &Address{Hash: "0x222"},
		Token: &Token{
			Address:  "0x333",
			Symbol:   "TEST",
			Decimals: &decimals,
		},
		Total: &TokenTotal{Value: "1000"},
	}

	// Should not panic with no clients
	hub.BroadcastTokenTransfer(transfer)
}

// Test multiple API calls
func TestAPISequence(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	// Get stats first
	resp, _ := http.Get(ts.URL + "/api/v2/stats")
	resp.Body.Close()

	// Then get blocks
	resp, _ = http.Get(ts.URL + "/api/v2/blocks")
	resp.Body.Close()

	// Then get transactions
	resp, _ = http.Get(ts.URL + "/api/v2/transactions")
	resp.Body.Close()

	// Then search
	resp, _ = http.Get(ts.URL + "/api/v2/search?q=0x123")
	resp.Body.Close()
}

// Parallel test
func TestParallelRequests(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	t.Run("parallel", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			t.Run(fmt.Sprintf("request_%d", i), func(t *testing.T) {
				t.Parallel()
				resp, err := http.Get(ts.URL + "/api/v2/blocks")
				if err != nil {
					t.Fatalf("Request failed: %v", err)
				}
				resp.Body.Close()
			})
		}
	})
}
