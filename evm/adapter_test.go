// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package evm

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/luxfi/indexer/chain"
)

// TestNew tests the adapter constructor
func TestNew(t *testing.T) {
	endpoint := "http://localhost:9650/ext/bc/C/rpc"
	adapter := New(endpoint)

	if adapter == nil {
		t.Fatal("New returned nil adapter")
	}

	if adapter.rpcEndpoint != endpoint {
		t.Errorf("rpcEndpoint = %q, want %q", adapter.rpcEndpoint, endpoint)
	}

	if adapter.httpClient == nil {
		t.Error("httpClient should not be nil")
	}

	if adapter.httpClient.Timeout != 30*time.Second {
		t.Errorf("httpClient.Timeout = %v, want 30s", adapter.httpClient.Timeout)
	}
}

// TestConstants tests the package constants
func TestConstants(t *testing.T) {
	tests := []struct {
		name  string
		got   interface{}
		want  interface{}
	}{
		{"DefaultPort", DefaultPort, 4000},
		{"DefaultDatabase", DefaultDatabase, "explorer_evm"},
		{"ChainID", ChainID, 96369},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestEventSignatures tests the well-known event topic signatures
func TestEventSignatures(t *testing.T) {
	// These should match the Keccak256 hashes of the event signatures
	tests := []struct {
		name  string
		topic string
	}{
		{"TopicTransferERC20", TopicTransferERC20},
		{"TopicTransferERC721", TopicTransferERC721}, // Same as ERC20
		{"TopicTransferSingle", TopicTransferSingle},
		{"TopicTransferBatch", TopicTransferBatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.topic == "" {
				t.Errorf("%s should not be empty", tt.name)
			}
			if len(tt.topic) != 66 {
				t.Errorf("%s length = %d, want 66 (0x + 64 hex chars)", tt.name, len(tt.topic))
			}
			if tt.topic[:2] != "0x" {
				t.Errorf("%s should start with '0x'", tt.name)
			}
		})
	}

	// ERC20 and ERC721 should have same topic
	if TopicTransferERC20 != TopicTransferERC721 {
		t.Error("TopicTransferERC20 should equal TopicTransferERC721")
	}
}

// TestAdapterImplementsInterface verifies Adapter implements chain.Adapter
func TestAdapterImplementsInterface(t *testing.T) {
	var _ chain.Adapter = (*Adapter)(nil)
}

// TestParseBlock tests parsing EVM block data
func TestParseBlock(t *testing.T) {
	adapter := New("http://localhost:9650")

	tests := []struct {
		name      string
		input     json.RawMessage
		wantID    string
		wantHeight uint64
		wantTxCount int
		wantErr   bool
	}{
		{
			name: "valid block with tx hashes",
			input: json.RawMessage(`{
				"hash": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"parentHash": "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				"number": "0x64",
				"timestamp": "0x61234567",
				"transactions": ["0xtx1", "0xtx2", "0xtx3"],
				"gasUsed": "0x5208",
				"gasLimit": "0x7a1200",
				"miner": "0x0000000000000000000000000000000000000000",
				"baseFeePerGas": "0x174876e800",
				"size": "0x200"
			}`),
			wantID:    "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			wantHeight: 100,
			wantTxCount: 3,
			wantErr:   false,
		},
		{
			name: "valid block with tx objects",
			input: json.RawMessage(`{
				"hash": "0xabcd",
				"parentHash": "0xparent",
				"number": "0xa",
				"timestamp": "0x5f5e100",
				"transactions": [{"hash": "0xtx1"}, {"hash": "0xtx2"}],
				"gasUsed": "0x0",
				"gasLimit": "0x0",
				"miner": "0x0",
				"baseFeePerGas": "0x0",
				"size": "0x0"
			}`),
			wantID:    "0xabcd",
			wantHeight: 10,
			wantTxCount: 2,
			wantErr:   false,
		},
		{
			name: "block with no transactions",
			input: json.RawMessage(`{
				"hash": "0xempty",
				"parentHash": "0xparent",
				"number": "0x1",
				"timestamp": "0x1",
				"transactions": [],
				"gasUsed": "0x0",
				"gasLimit": "0x0"
			}`),
			wantID:    "0xempty",
			wantHeight: 1,
			wantTxCount: 0,
			wantErr:   false,
		},
		{
			name:    "invalid JSON",
			input:   json.RawMessage(`{invalid`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block, err := adapter.ParseBlock(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if block.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", block.ID, tt.wantID)
			}
			if block.Height != tt.wantHeight {
				t.Errorf("Height = %d, want %d", block.Height, tt.wantHeight)
			}
			if block.TxCount != tt.wantTxCount {
				t.Errorf("TxCount = %d, want %d", block.TxCount, tt.wantTxCount)
			}
			if block.Status != chain.StatusAccepted {
				t.Errorf("Status = %q, want %q", block.Status, chain.StatusAccepted)
			}
		})
	}
}

// TestParseBlockMetadata tests that metadata is correctly extracted
func TestParseBlockMetadata(t *testing.T) {
	adapter := New("http://localhost:9650")

	input := json.RawMessage(`{
		"hash": "0xblock",
		"parentHash": "0xparent",
		"number": "0x100",
		"timestamp": "0x60000000",
		"transactions": [],
		"gasUsed": "0x1000",
		"gasLimit": "0x2000",
		"miner": "0xminer123",
		"baseFeePerGas": "0x3b9aca00",
		"size": "0x500"
	}`)

	block, err := adapter.ParseBlock(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		key  string
		want interface{}
	}{
		{"gasUsed", uint64(0x1000)},
		{"gasLimit", uint64(0x2000)},
		{"miner", "0xminer123"},
		{"baseFee", "0x3b9aca00"},
		{"size", uint64(0x500)},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := block.Metadata[tt.key]
			if got != tt.want {
				t.Errorf("Metadata[%q] = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// TestGetRecentBlocks tests fetching recent blocks
func TestGetRecentBlocks(t *testing.T) {
	// Create a mock server
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		method := req["method"].(string)

		if method == "eth_blockNumber" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  "0x64", // Block 100
			})
			return
		}

		if method == "eth_getBlockByNumber" {
			callCount++
			params := req["params"].([]interface{})
			blockNum := params[0].(string)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"result": map[string]interface{}{
					"hash":         fmt.Sprintf("0xblock_%s", blockNum),
					"parentHash":   "0xparent",
					"number":       blockNum,
					"timestamp":    "0x60000000",
					"transactions": []string{},
					"gasUsed":      "0x0",
					"gasLimit":     "0x0",
				},
			})
		}
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	blocks, err := adapter.GetRecentBlocks(ctx, 5)
	if err != nil {
		t.Fatalf("GetRecentBlocks failed: %v", err)
	}

	if len(blocks) != 5 {
		t.Errorf("got %d blocks, want 5", len(blocks))
	}

	if callCount != 5 {
		t.Errorf("expected 5 block fetch calls, got %d", callCount)
	}
}

// TestGetBlockByID tests fetching a block by hash
func TestGetBlockByID(t *testing.T) {
	blockHash := "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		method := req["method"].(string)
		if method != "eth_getBlockByHash" {
			t.Errorf("expected method eth_getBlockByHash, got %s", method)
		}

		params := req["params"].([]interface{})
		hash := params[0].(string)
		if hash != blockHash {
			t.Errorf("expected hash %s, got %s", blockHash, hash)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"hash":         blockHash,
				"parentHash":   "0xparent",
				"number":       "0x64",
				"timestamp":    "0x60000000",
				"transactions": []string{},
				"gasUsed":      "0x0",
				"gasLimit":     "0x0",
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	result, err := adapter.GetBlockByID(ctx, blockHash)
	if err != nil {
		t.Fatalf("GetBlockByID failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestGetBlockByHeight tests fetching a block by number
func TestGetBlockByHeight(t *testing.T) {
	height := uint64(100)
	expectedHex := "0x64"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		method := req["method"].(string)
		if method != "eth_getBlockByNumber" {
			t.Errorf("expected method eth_getBlockByNumber, got %s", method)
		}

		params := req["params"].([]interface{})
		blockNum := params[0].(string)
		if blockNum != expectedHex {
			t.Errorf("expected block number %s, got %s", expectedHex, blockNum)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"hash":         "0xblock100",
				"parentHash":   "0xparent",
				"number":       expectedHex,
				"timestamp":    "0x60000000",
				"transactions": []string{},
				"gasUsed":      "0x0",
				"gasLimit":     "0x0",
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	result, err := adapter.GetBlockByHeight(ctx, height)
	if err != nil {
		t.Fatalf("GetBlockByHeight failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestGetTransactionReceipt tests fetching a transaction receipt
func TestGetTransactionReceipt(t *testing.T) {
	txHash := "0xtx123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"transactionHash":   txHash,
				"blockHash":         "0xblock",
				"blockNumber":       "0x64",
				"from":              "0xfrom123",
				"to":                "0xto456",
				"gasUsed":           "0x5208",
				"status":            "0x1",
				"contractAddress":   "",
				"transactionIndex":  "0x0",
				"logs": []map[string]interface{}{
					{
						"address":  "0xtoken",
						"topics":   []string{TopicTransferERC20, "0xtopic1", "0xtopic2"},
						"data":     "0xdata",
						"logIndex": "0x0",
						"removed":  false,
					},
				},
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	tx, logs, err := adapter.GetTransactionReceipt(ctx, txHash)
	if err != nil {
		t.Fatalf("GetTransactionReceipt failed: %v", err)
	}

	if tx.Hash != txHash {
		t.Errorf("Hash = %q, want %q", tx.Hash, txHash)
	}
	if tx.BlockNumber != 100 {
		t.Errorf("BlockNumber = %d, want 100", tx.BlockNumber)
	}
	if tx.From != "0xfrom123" {
		t.Errorf("From = %q, want 0xfrom123", tx.From)
	}
	if tx.To != "0xto456" {
		t.Errorf("To = %q, want 0xto456", tx.To)
	}
	if tx.Status != 1 {
		t.Errorf("Status = %d, want 1", tx.Status)
	}
	if tx.GasUsed != 21000 {
		t.Errorf("GasUsed = %d, want 21000", tx.GasUsed)
	}

	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].Address != "0xtoken" {
		t.Errorf("Log address = %q, want 0xtoken", logs[0].Address)
	}
}

// TestGetBalance tests fetching an address balance
func TestGetBalance(t *testing.T) {
	address := "0xaddress123"
	expectedBalance := "0xde0b6b3a7640000" // 1 ETH in wei

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["method"] != "eth_getBalance" {
			t.Errorf("expected eth_getBalance, got %s", req["method"])
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  expectedBalance,
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	balance, err := adapter.GetBalance(ctx, address)
	if err != nil {
		t.Fatalf("GetBalance failed: %v", err)
	}

	expected := new(big.Int)
	expected.SetString("1000000000000000000", 10) // 1 ETH

	if balance.Cmp(expected) != 0 {
		t.Errorf("Balance = %s, want %s", balance.String(), expected.String())
	}
}

// TestGetCode tests fetching contract code
func TestGetCode(t *testing.T) {
	address := "0xcontract123"
	contractCode := "0x608060405234801561001057600080fd5b50"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["method"] != "eth_getCode" {
			t.Errorf("expected eth_getCode, got %s", req["method"])
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  contractCode,
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	code, err := adapter.GetCode(ctx, address)
	if err != nil {
		t.Fatalf("GetCode failed: %v", err)
	}

	if code != contractCode {
		t.Errorf("Code = %q, want %q", code, contractCode)
	}
}

// TestGetTokenInfo tests fetching ERC20 token metadata
func TestGetTokenInfo(t *testing.T) {
	tokenAddress := "0xtoken123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		params := req["params"].([]interface{})
		callParams := params[0].(map[string]interface{})
		data := callParams["data"].(string)

		var result string
		switch data {
		case "0x06fdde03": // name()
			// "TestToken" encoded
			result = "0x0000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000954657374546f6b656e0000000000000000000000000000000000000000000000"
		case "0x95d89b41": // symbol()
			// "TST" encoded
			result = "0x00000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000003545354000000000000000000000000000000000000000000000000000000000000"
		case "0x313ce567": // decimals()
			result = "0x12" // 18
		case "0x18160ddd": // totalSupply()
			result = "0x0000000000000000000000000000000000000000000000000de0b6b3a7640000" // 1e18
		default:
			result = "0x"
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  result,
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	token, err := adapter.GetTokenInfo(ctx, tokenAddress)
	if err != nil {
		t.Fatalf("GetTokenInfo failed: %v", err)
	}

	if token.Address != tokenAddress {
		t.Errorf("Address = %q, want %q", token.Address, tokenAddress)
	}
	if token.Name != "TestToken" {
		t.Errorf("Name = %q, want TestToken", token.Name)
	}
	if token.Symbol != "TST" {
		t.Errorf("Symbol = %q, want TST", token.Symbol)
	}
	if token.Decimals != 18 {
		t.Errorf("Decimals = %d, want 18", token.Decimals)
	}
	if token.TokenType != "ERC20" {
		t.Errorf("TokenType = %q, want ERC20", token.TokenType)
	}
}

// TestTransactionStruct tests Transaction JSON serialization
func TestTransactionStruct(t *testing.T) {
	tx := Transaction{
		Hash:             "0xhash123",
		BlockHash:        "0xblock456",
		BlockNumber:      100,
		From:             "0xfrom",
		To:               "0xto",
		Value:            "1000000000000000000",
		Gas:              21000,
		GasPrice:         "1000000000",
		GasUsed:          21000,
		Nonce:            5,
		Input:            "0x",
		TransactionIndex: 0,
		Type:             2,
		Status:           1,
		ContractAddress:  "",
		Timestamp:        time.Now(),
	}

	data, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Transaction
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Hash != tx.Hash {
		t.Errorf("Hash = %q, want %q", decoded.Hash, tx.Hash)
	}
	if decoded.BlockNumber != tx.BlockNumber {
		t.Errorf("BlockNumber = %d, want %d", decoded.BlockNumber, tx.BlockNumber)
	}
	if decoded.Value != tx.Value {
		t.Errorf("Value = %q, want %q", decoded.Value, tx.Value)
	}
}

// TestAddressStruct tests Address JSON serialization
func TestAddressStruct(t *testing.T) {
	addr := Address{
		Hash:            "0xaddress123",
		Balance:         "1000000000000000000",
		TxCount:         10,
		IsContract:      true,
		ContractCode:    "0x608060",
		ContractCreator: "0xcreator",
		ContractTxHash:  "0xtx",
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	data, err := json.Marshal(addr)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Address
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Hash != addr.Hash {
		t.Errorf("Hash = %q, want %q", decoded.Hash, addr.Hash)
	}
	if decoded.IsContract != addr.IsContract {
		t.Errorf("IsContract = %v, want %v", decoded.IsContract, addr.IsContract)
	}
}

// TestTokenTransferStruct tests TokenTransfer JSON serialization
func TestTokenTransferStruct(t *testing.T) {
	transfer := TokenTransfer{
		ID:           "0xtx-0",
		TxHash:       "0xtx",
		LogIndex:     0,
		BlockNumber:  100,
		TokenAddress: "0xtoken",
		TokenType:    "ERC20",
		From:         "0xfrom",
		To:           "0xto",
		Value:        "1000",
		TokenID:      "",
		Timestamp:    time.Now(),
	}

	data, err := json.Marshal(transfer)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded TokenTransfer
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != transfer.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, transfer.ID)
	}
	if decoded.TokenType != transfer.TokenType {
		t.Errorf("TokenType = %q, want %q", decoded.TokenType, transfer.TokenType)
	}
}

// TestTokenStruct tests Token JSON serialization
func TestTokenStruct(t *testing.T) {
	token := Token{
		Address:     "0xtoken",
		Name:        "Test Token",
		Symbol:      "TST",
		Decimals:    18,
		TotalSupply: "1000000000000000000000000",
		TokenType:   "ERC20",
		HolderCount: 1000,
		TxCount:     5000,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	data, err := json.Marshal(token)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Token
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Address != token.Address {
		t.Errorf("Address = %q, want %q", decoded.Address, token.Address)
	}
	if decoded.Decimals != token.Decimals {
		t.Errorf("Decimals = %d, want %d", decoded.Decimals, token.Decimals)
	}
}

// TestLogStruct tests Log JSON serialization
func TestLogStruct(t *testing.T) {
	log := Log{
		TxHash:      "0xtx",
		LogIndex:    0,
		BlockNumber: 100,
		Address:     "0xcontract",
		Topics:      []string{"0xtopic0", "0xtopic1"},
		Data:        "0xdata",
		Removed:     false,
	}

	data, err := json.Marshal(log)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Log
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.TxHash != log.TxHash {
		t.Errorf("TxHash = %q, want %q", decoded.TxHash, log.TxHash)
	}
	if len(decoded.Topics) != len(log.Topics) {
		t.Errorf("Topics length = %d, want %d", len(decoded.Topics), len(log.Topics))
	}
}

// TestInternalTransactionStruct tests InternalTransaction JSON serialization
func TestInternalTransactionStruct(t *testing.T) {
	itx := InternalTransaction{
		ID:          "0xtx-0",
		TxHash:      "0xtx",
		BlockNumber: 100,
		TraceIndex:  0,
		CallType:    "call",
		From:        "0xfrom",
		To:          "0xto",
		Value:       "1000",
		Gas:         100000,
		GasUsed:     50000,
		Input:       "0x",
		Output:      "0x",
		Error:       "",
		Timestamp:   time.Now(),
	}

	data, err := json.Marshal(itx)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded InternalTransaction
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != itx.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, itx.ID)
	}
	if decoded.CallType != itx.CallType {
		t.Errorf("CallType = %q, want %q", decoded.CallType, itx.CallType)
	}
}

// TestHexToUint64 tests the hexToUint64 helper
func TestHexToUint64(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
	}{
		{"0x0", 0},
		{"0x1", 1},
		{"0xa", 10},
		{"0x64", 100},
		{"0x100", 256},
		{"0xffffffff", 4294967295},
		{"", 0},
		{"0x", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := hexToUint64(tt.input)
			if got != tt.want {
				t.Errorf("hexToUint64(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestHexToBigInt tests the hexToBigInt helper
func TestHexToBigInt(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"0x0", "0"},
		{"0x1", "1"},
		{"0x64", "100"},
		{"0xde0b6b3a7640000", "1000000000000000000"},
		{"", "0"},
		{"0x", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := hexToBigInt(tt.input)
			if got.String() != tt.want {
				t.Errorf("hexToBigInt(%q) = %s, want %s", tt.input, got.String(), tt.want)
			}
		})
	}
}

// TestTopicToAddress tests the topicToAddress helper
func TestTopicToAddress(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			"0x000000000000000000000000abcdefabcdefabcdefabcdefabcdefabcdefabcd",
			"0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		},
		{
			"0x000000000000000000000000ABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD",
			"0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		},
		{"0x1234", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := topicToAddress(tt.input)
			if got != tt.want {
				t.Errorf("topicToAddress(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestAddressToTopic tests the addressToTopic helper
func TestAddressToTopic(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			"0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
			"0x000000000000000000000000abcdefabcdefabcdefabcdefabcdefabcdefabcd",
		},
		{
			"0xABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD",
			"0x000000000000000000000000abcdefabcdefabcdefabcdefabcdefabcdefabcd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := addressToTopic(tt.input)
			if got != tt.want {
				t.Errorf("addressToTopic(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestDecodeString tests the ABI string decoding helper
func TestDecodeString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "TestToken",
			input: "0x0000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000954657374546f6b656e0000000000000000000000000000000000000000000000",
			want:  "TestToken",
		},
		{
			name:  "TST",
			input: "0x00000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000003545354000000000000000000000000000000000000000000000000000000000000",
			want:  "TST",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "too short",
			input: "0x1234",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeString(tt.input)
			if got != tt.want {
				t.Errorf("decodeString() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestNetworkError tests error handling for network failures
func TestNetworkError(t *testing.T) {
	adapter := New("http://localhost:99999")
	ctx := context.Background()

	_, err := adapter.GetRecentBlocks(ctx, 5)
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// TestContextCancellation tests context cancellation handling
func TestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := adapter.GetRecentBlocks(ctx, 5)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

// TestRPCError tests RPC error response handling
func TestRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]interface{}{
				"code":    -32000,
				"message": "block not found",
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	_, err := adapter.GetBlockByID(ctx, "0xnonexistent")
	if err == nil {
		t.Error("expected error for RPC error response")
	}

	if err != nil && !contains(err.Error(), "block not found") {
		t.Errorf("error should contain 'block not found', got: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestParseTokenTransfersERC20 tests parsing ERC20 transfer logs
func TestParseTokenTransfersERC20(t *testing.T) {
	log := Log{
		TxHash:      "0xtx123",
		LogIndex:    0,
		BlockNumber: 100,
		Address:     "0xtoken",
		Topics: []string{
			TopicTransferERC20,
			"0x000000000000000000000000from1234567890abcdef1234567890abcdef1234",
			"0x000000000000000000000000to1234567890abcdef1234567890abcdefabcd",
		},
		Data:    "0x0000000000000000000000000000000000000000000000000de0b6b3a7640000",
		Removed: false,
	}

	transfers := parseTokenTransfers(log, time.Now())

	if len(transfers) != 1 {
		t.Fatalf("expected 1 transfer, got %d", len(transfers))
	}

	transfer := transfers[0]
	if transfer.TokenType != "ERC20" {
		t.Errorf("TokenType = %q, want ERC20", transfer.TokenType)
	}
	if transfer.From != "0xfrom1234567890abcdef1234567890abcdef1234" {
		t.Errorf("From = %q", transfer.From)
	}
	if transfer.Value != "1000000000000000000" {
		t.Errorf("Value = %q, want 1000000000000000000", transfer.Value)
	}
}

// TestParseTokenTransfersERC721 tests parsing ERC721 transfer logs
func TestParseTokenTransfersERC721(t *testing.T) {
	log := Log{
		TxHash:      "0xtx123",
		LogIndex:    0,
		BlockNumber: 100,
		Address:     "0xnft",
		Topics: []string{
			TopicTransferERC721,
			"0x000000000000000000000000from1234567890abcdef1234567890abcdef1234",
			"0x000000000000000000000000to1234567890abcdef1234567890abcdefabcd",
			"0x0000000000000000000000000000000000000000000000000000000000000001",
		},
		Data:    "0x",
		Removed: false,
	}

	transfers := parseTokenTransfers(log, time.Now())

	if len(transfers) != 1 {
		t.Fatalf("expected 1 transfer, got %d", len(transfers))
	}

	transfer := transfers[0]
	if transfer.TokenType != "ERC721" {
		t.Errorf("TokenType = %q, want ERC721", transfer.TokenType)
	}
	if transfer.Value != "1" {
		t.Errorf("Value = %q, want 1", transfer.Value)
	}
	if transfer.TokenID != "0x0000000000000000000000000000000000000000000000000000000000000001" {
		t.Errorf("TokenID = %q", transfer.TokenID)
	}
}

// TestParseTokenTransfersEmpty tests parsing logs with no transfers
func TestParseTokenTransfersEmpty(t *testing.T) {
	// Log with no topics
	log := Log{
		TxHash:      "0xtx",
		LogIndex:    0,
		BlockNumber: 100,
		Address:     "0xcontract",
		Topics:      []string{},
		Data:        "0x",
	}

	transfers := parseTokenTransfers(log, time.Now())
	if len(transfers) != 0 {
		t.Errorf("expected 0 transfers, got %d", len(transfers))
	}
}

// TestParseTokenTransfersUnknownEvent tests parsing unknown event logs
func TestParseTokenTransfersUnknownEvent(t *testing.T) {
	log := Log{
		TxHash:      "0xtx",
		LogIndex:    0,
		BlockNumber: 100,
		Address:     "0xcontract",
		Topics:      []string{"0xunknowneventtopichash0000000000000000000000000000000000000000"},
		Data:        "0x",
	}

	transfers := parseTokenTransfers(log, time.Now())
	if len(transfers) != 0 {
		t.Errorf("expected 0 transfers for unknown event, got %d", len(transfers))
	}
}

// Benchmarks

func BenchmarkParseBlock(b *testing.B) {
	adapter := New("http://localhost:9650")
	input := json.RawMessage(`{
		"hash": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		"parentHash": "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		"number": "0x64",
		"timestamp": "0x61234567",
		"transactions": ["0xtx1", "0xtx2", "0xtx3", "0xtx4", "0xtx5"],
		"gasUsed": "0x5208",
		"gasLimit": "0x7a1200",
		"miner": "0x0000000000000000000000000000000000000000",
		"baseFeePerGas": "0x174876e800",
		"size": "0x200"
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.ParseBlock(input)
	}
}

func BenchmarkHexToUint64(b *testing.B) {
	inputs := []string{"0x0", "0x64", "0xffffffff", "0xde0b6b3a7640000"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, input := range inputs {
			hexToUint64(input)
		}
	}
}

func BenchmarkParseTokenTransfers(b *testing.B) {
	log := Log{
		TxHash:      "0xtx123",
		LogIndex:    0,
		BlockNumber: 100,
		Address:     "0xtoken",
		Topics: []string{
			TopicTransferERC20,
			"0x000000000000000000000000from1234567890abcdef1234567890abcdef1234",
			"0x000000000000000000000000to1234567890abcdef1234567890abcdefabcd",
		},
		Data:    "0x0000000000000000000000000000000000000000000000000de0b6b3a7640000",
		Removed: false,
	}
	timestamp := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseTokenTransfers(log, timestamp)
	}
}

func BenchmarkDecodeString(b *testing.B) {
	input := "0x0000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000954657374546f6b656e0000000000000000000000000000000000000000000000"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeString(input)
	}
}

// ============================================================================
// Phase 1 Tests: Internal Transaction Tracing
// ============================================================================

// TestFlattenCallFrame tests the call tree flattening with trace addresses
func TestFlattenCallFrame(t *testing.T) {
	timestamp := time.Now()
	txHash := "0xtx123"
	blockNumber := uint64(100)

	tests := []struct {
		name           string
		frame          *CallFrame
		wantCount      int
		wantAddresses  [][]int
		wantCallTypes  []string
	}{
		{
			name: "single call",
			frame: &CallFrame{
				Type:    "CALL",
				From:    "0xfrom",
				To:      "0xto",
				Value:   "0x100",
				Gas:     "0x5208",
				GasUsed: "0x5000",
				Input:   "0x",
				Output:  "0x",
			},
			wantCount:     1,
			wantAddresses: [][]int{{}},
			wantCallTypes: []string{"call"},
		},
		{
			name: "nested calls",
			frame: &CallFrame{
				Type:    "CALL",
				From:    "0xa",
				To:      "0xb",
				Value:   "0x0",
				Gas:     "0x10000",
				GasUsed: "0x8000",
				Calls: []*CallFrame{
					{
						Type:    "CALL",
						From:    "0xb",
						To:      "0xc",
						Value:   "0x0",
						Gas:     "0x5000",
						GasUsed: "0x3000",
					},
					{
						Type:    "DELEGATECALL",
						From:    "0xb",
						To:      "0xd",
						Value:   "0x0",
						Gas:     "0x3000",
						GasUsed: "0x2000",
					},
				},
			},
			wantCount:     3,
			wantAddresses: [][]int{{}, {0}, {1}},
			wantCallTypes: []string{"call", "call", "delegatecall"},
		},
		{
			name: "deeply nested calls",
			frame: &CallFrame{
				Type:    "CALL",
				From:    "0xa",
				To:      "0xb",
				Gas:     "0x10000",
				GasUsed: "0x8000",
				Calls: []*CallFrame{
					{
						Type:    "CALL",
						From:    "0xb",
						To:      "0xc",
						Gas:     "0x5000",
						GasUsed: "0x3000",
						Calls: []*CallFrame{
							{
								Type:    "STATICCALL",
								From:    "0xc",
								To:      "0xd",
								Gas:     "0x2000",
								GasUsed: "0x1000",
							},
						},
					},
				},
			},
			wantCount:     3,
			wantAddresses: [][]int{{}, {0}, {0, 0}},
			wantCallTypes: []string{"call", "call", "staticcall"},
		},
		{
			name: "create operation",
			frame: &CallFrame{
				Type:    "CREATE",
				From:    "0xdeployer",
				To:      "0xnewcontract",
				Value:   "0x0",
				Gas:     "0x100000",
				GasUsed: "0x50000",
				Input:   "0x608060405234801561001057600080fd5b50",
				Output:  "0x608060405234801561001057600080fd5b50",
			},
			wantCount:     1,
			wantAddresses: [][]int{{}},
			wantCallTypes: []string{"create"},
		},
		{
			name: "call with error",
			frame: &CallFrame{
				Type:    "CALL",
				From:    "0xa",
				To:      "0xb",
				Gas:     "0x5208",
				GasUsed: "0x5208",
				Error:   "execution reverted",
			},
			wantCount:     1,
			wantAddresses: [][]int{{}},
			wantCallTypes: []string{"call"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var traces []InternalTransaction
			result := flattenCallFrame(tt.frame, txHash, blockNumber, timestamp, []int{}, &traces, 0)

			if len(result) != tt.wantCount {
				t.Errorf("flattenCallFrame() returned %d traces, want %d", len(result), tt.wantCount)
				return
			}

			for i, itx := range result {
				// Check trace address
				if !intSlicesEqual(itx.TraceAddress, tt.wantAddresses[i]) {
					t.Errorf("trace[%d].TraceAddress = %v, want %v", i, itx.TraceAddress, tt.wantAddresses[i])
				}

				// Check call type
				if itx.CallType != tt.wantCallTypes[i] {
					t.Errorf("trace[%d].CallType = %q, want %q", i, itx.CallType, tt.wantCallTypes[i])
				}

				// Check transaction hash
				if itx.TxHash != txHash {
					t.Errorf("trace[%d].TxHash = %q, want %q", i, itx.TxHash, txHash)
				}

				// Check block number
				if itx.BlockNumber != blockNumber {
					t.Errorf("trace[%d].BlockNumber = %d, want %d", i, itx.BlockNumber, blockNumber)
				}
			}
		})
	}
}

// intSlicesEqual compares two int slices for equality
func intSlicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestNormalizeCallType tests call type normalization
func TestNormalizeCallType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"CALL", "call"},
		{"Call", "call"},
		{"call", "call"},
		{"DELEGATECALL", "delegatecall"},
		{"STATICCALL", "staticcall"},
		{"CALLCODE", "callcode"},
		{"CREATE", "create"},
		{"CREATE2", "create2"},
		{"SELFDESTRUCT", "selfdestruct"},
		{"SUICIDE", "selfdestruct"},
		{"suicide", "selfdestruct"},
		{"unknown", "unknown"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeCallType(tt.input)
			if got != tt.want {
				t.Errorf("normalizeCallType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestNormalizeValue tests value normalization
func TestNormalizeValue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "0x0"},
		{"0x0", "0x0"},
		{"0x100", "0x100"},
		{"0xde0b6b3a7640000", "0xde0b6b3a7640000"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeValue(tt.input)
			if got != tt.want {
				t.Errorf("normalizeValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestIntSliceToPostgresArray tests PostgreSQL array formatting
func TestIntSliceToPostgresArray(t *testing.T) {
	tests := []struct {
		input []int
		want  string
	}{
		{[]int{}, "{}"},
		{[]int{0}, "{0}"},
		{[]int{0, 1}, "{0,1}"},
		{[]int{0, 1, 2}, "{0,1,2}"},
		{[]int{1, 2, 3, 4, 5}, "{1,2,3,4,5}"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := intSliceToPostgresArray(tt.input)
			if got != tt.want {
				t.Errorf("intSliceToPostgresArray(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestPostgresArrayToIntSlice tests PostgreSQL array parsing
func TestPostgresArrayToIntSlice(t *testing.T) {
	tests := []struct {
		input string
		want  []int
	}{
		{"{}", []int{}},
		{"{0}", []int{0}},
		{"{0,1}", []int{0, 1}},
		{"{0,1,2}", []int{0, 1, 2}},
		{"{1, 2, 3}", []int{1, 2, 3}},
		{"", []int{}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := postgresArrayToIntSlice(tt.input)
			if !intSlicesEqual(got, tt.want) {
				t.Errorf("postgresArrayToIntSlice(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestTraceAddressToString tests trace address string formatting
func TestTraceAddressToString(t *testing.T) {
	tests := []struct {
		input []int
		want  string
	}{
		{[]int{}, "[]"},
		{[]int{0}, "[0]"},
		{[]int{0, 1}, "[0,1]"},
		{[]int{0, 1, 2}, "[0,1,2]"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := TraceAddressToString(tt.input)
			if got != tt.want {
				t.Errorf("TraceAddressToString(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestSortInternalTransactionsByTraceAddress tests sorting by trace address
func TestSortInternalTransactionsByTraceAddress(t *testing.T) {
	itxs := []InternalTransaction{
		{ID: "1", TraceAddress: []int{1}},
		{ID: "2", TraceAddress: []int{0, 1}},
		{ID: "3", TraceAddress: []int{}},
		{ID: "4", TraceAddress: []int{0}},
		{ID: "5", TraceAddress: []int{0, 0}},
	}

	SortInternalTransactionsByTraceAddress(itxs)

	expected := []string{"3", "4", "5", "2", "1"}
	for i, itx := range itxs {
		if itx.ID != expected[i] {
			t.Errorf("sorted[%d].ID = %q, want %q", i, itx.ID, expected[i])
		}
	}
}

// TestInternalTransactionWithTraceAddress tests InternalTransaction struct with TraceAddress
func TestInternalTransactionWithTraceAddress(t *testing.T) {
	itx := InternalTransaction{
		ID:                     "0xtx-0",
		TxHash:                 "0xtx",
		BlockNumber:            100,
		TraceIndex:             0,
		TraceAddress:           []int{0, 1, 2},
		CallType:               "call",
		From:                   "0xfrom",
		To:                     "0xto",
		Value:                  "0x100",
		Gas:                    100000,
		GasUsed:                50000,
		Input:                  "0x",
		Output:                 "0x",
		Error:                  "",
		CreatedContractAddress: "",
		CreatedContractCode:    "",
		Init:                   "",
		Timestamp:              time.Now(),
	}

	// Test JSON serialization
	data, err := json.Marshal(itx)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded InternalTransaction
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if !intSlicesEqual(decoded.TraceAddress, itx.TraceAddress) {
		t.Errorf("TraceAddress = %v, want %v", decoded.TraceAddress, itx.TraceAddress)
	}
	if decoded.CallType != itx.CallType {
		t.Errorf("CallType = %q, want %q", decoded.CallType, itx.CallType)
	}
}

// ============================================================================
// Phase 1 Tests: Token Balance Tracking
// ============================================================================

// TestTokenBalanceStruct tests TokenBalance JSON serialization
func TestTokenBalanceStruct(t *testing.T) {
	tb := TokenBalance{
		TokenAddress:  "0xtoken",
		HolderAddress: "0xholder",
		Balance:       "1000000000000000000",
		TokenID:       "123",
		BlockNumber:   100,
		UpdatedAt:     time.Now(),
	}

	data, err := json.Marshal(tb)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded TokenBalance
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.TokenAddress != tb.TokenAddress {
		t.Errorf("TokenAddress = %q, want %q", decoded.TokenAddress, tb.TokenAddress)
	}
	if decoded.HolderAddress != tb.HolderAddress {
		t.Errorf("HolderAddress = %q, want %q", decoded.HolderAddress, tb.HolderAddress)
	}
	if decoded.Balance != tb.Balance {
		t.Errorf("Balance = %q, want %q", decoded.Balance, tb.Balance)
	}
	if decoded.TokenID != tb.TokenID {
		t.Errorf("TokenID = %q, want %q", decoded.TokenID, tb.TokenID)
	}
}

// TestGetTokenBalance tests fetching ERC20 token balance via RPC
func TestGetTokenBalance(t *testing.T) {
	tokenAddress := "0xtoken123"
	holderAddress := "0xholder456"
	expectedBalance := "0x0000000000000000000000000000000000000000000000000de0b6b3a7640000" // 1e18

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["method"] != "eth_call" {
			t.Errorf("expected eth_call, got %s", req["method"])
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  expectedBalance,
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	balance, err := adapter.GetTokenBalance(ctx, tokenAddress, holderAddress, 0)
	if err != nil {
		t.Fatalf("GetTokenBalance failed: %v", err)
	}

	expected := new(big.Int)
	expected.SetString("1000000000000000000", 10)

	if balance.Cmp(expected) != 0 {
		t.Errorf("Balance = %s, want %s", balance.String(), expected.String())
	}
}

// TestGetERC721Owner tests fetching ERC721 owner
func TestGetERC721Owner(t *testing.T) {
	tokenAddress := "0xnft123"
	tokenID := big.NewInt(42)
	expectedOwner := "0x000000000000000000000000abcdef1234567890abcdef1234567890abcdef12"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  expectedOwner,
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	owner, err := adapter.GetERC721Owner(ctx, tokenAddress, tokenID)
	if err != nil {
		t.Fatalf("GetERC721Owner failed: %v", err)
	}

	if owner != "0xabcdef1234567890abcdef1234567890abcdef12" {
		t.Errorf("Owner = %q, want 0xabcdef1234567890abcdef1234567890abcdef12", owner)
	}
}

// TestGetERC1155Balance tests fetching ERC1155 balance
func TestGetERC1155Balance(t *testing.T) {
	tokenAddress := "0xerc1155"
	holderAddress := "0xholder"
	tokenID := big.NewInt(1)
	expectedBalance := "0x0000000000000000000000000000000000000000000000000000000000000064" // 100

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  expectedBalance,
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	balance, err := adapter.GetERC1155Balance(ctx, tokenAddress, holderAddress, tokenID)
	if err != nil {
		t.Fatalf("GetERC1155Balance failed: %v", err)
	}

	if balance.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("Balance = %s, want 100", balance.String())
	}
}

// ============================================================================
// Phase 1 Tests: Address Coin Balance Tracking
// ============================================================================

// TestAddressCoinBalanceStruct tests AddressCoinBalance JSON serialization
func TestAddressCoinBalanceStruct(t *testing.T) {
	acb := AddressCoinBalance{
		AddressHash:    "0xaddress",
		BlockNumber:    100,
		Value:          "1000000000000000000",
		ValueFetchedAt: time.Now(),
	}

	data, err := json.Marshal(acb)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded AddressCoinBalance
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.AddressHash != acb.AddressHash {
		t.Errorf("AddressHash = %q, want %q", decoded.AddressHash, acb.AddressHash)
	}
	if decoded.BlockNumber != acb.BlockNumber {
		t.Errorf("BlockNumber = %d, want %d", decoded.BlockNumber, acb.BlockNumber)
	}
	if decoded.Value != acb.Value {
		t.Errorf("Value = %q, want %q", decoded.Value, acb.Value)
	}
}

// TestGetBalanceAtBlock tests fetching balance at specific block
func TestGetBalanceAtBlock(t *testing.T) {
	address := "0xaddress123"
	blockNumber := uint64(100)
	expectedBalance := "0xde0b6b3a7640000" // 1 LUX in wei

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["method"] != "eth_getBalance" {
			t.Errorf("expected eth_getBalance, got %s", req["method"])
		}

		params := req["params"].([]interface{})
		blockTag := params[1].(string)
		if blockTag != "0x64" {
			t.Errorf("expected block tag 0x64, got %s", blockTag)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  expectedBalance,
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	balance, err := adapter.GetBalanceAtBlock(ctx, address, blockNumber)
	if err != nil {
		t.Fatalf("GetBalanceAtBlock failed: %v", err)
	}

	expected := new(big.Int)
	expected.SetString("1000000000000000000", 10)

	if balance.Cmp(expected) != 0 {
		t.Errorf("Balance = %s, want %s", balance.String(), expected.String())
	}
}

// TestGetBalanceAtBlockLatest tests fetching balance at latest block
func TestGetBalanceAtBlockLatest(t *testing.T) {
	address := "0xaddress123"
	expectedBalance := "0xde0b6b3a7640000"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		params := req["params"].([]interface{})
		blockTag := params[1].(string)
		if blockTag != "latest" {
			t.Errorf("expected block tag 'latest', got %s", blockTag)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  expectedBalance,
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	// Block number 0 should use "latest"
	balance, err := adapter.GetBalanceAtBlock(ctx, address, 0)
	if err != nil {
		t.Fatalf("GetBalanceAtBlock failed: %v", err)
	}

	if balance == nil {
		t.Error("Balance should not be nil")
	}
}

// ============================================================================
// Phase 1 Tests: Tracer Options
// ============================================================================

// TestAdapterOptions tests adapter configuration options
func TestAdapterOptions(t *testing.T) {
	endpoint := "http://localhost:9650"

	// Test WithTracerType
	adapter := New(endpoint, WithTracerType(TracerParity))
	if adapter.tracerType != TracerParity {
		t.Errorf("tracerType = %v, want TracerParity", adapter.tracerType)
	}

	// Test WithTraceTimeout
	adapter = New(endpoint, WithTraceTimeout("60s"))
	if adapter.traceTimeout != "60s" {
		t.Errorf("traceTimeout = %q, want 60s", adapter.traceTimeout)
	}

	// Test multiple options
	adapter = New(endpoint,
		WithTracerType(TracerJS),
		WithTraceTimeout("30s"),
	)
	if adapter.tracerType != TracerJS {
		t.Errorf("tracerType = %v, want TracerJS", adapter.tracerType)
	}
	if adapter.traceTimeout != "30s" {
		t.Errorf("traceTimeout = %q, want 30s", adapter.traceTimeout)
	}
}

// TestTracerTypeConstants tests tracer type constants
func TestTracerTypeConstants(t *testing.T) {
	tests := []struct {
		tracer TracerType
		want   string
	}{
		{TracerCallTracer, "callTracer"},
		{TracerJS, "js"},
		{TracerParity, "parity"},
	}

	for _, tt := range tests {
		t.Run(string(tt.tracer), func(t *testing.T) {
			if string(tt.tracer) != tt.want {
				t.Errorf("TracerType = %q, want %q", tt.tracer, tt.want)
			}
		})
	}
}

// ============================================================================
// Phase 1 Tests: TraceTransaction RPC Mocking
// ============================================================================

// TestTraceTransactionCallTracer tests debug_traceTransaction with callTracer
func TestTraceTransactionCallTracer(t *testing.T) {
	txHash := "0xtx123"
	blockNumber := uint64(100)
	timestamp := time.Now()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["method"] != "debug_traceTransaction" {
			t.Errorf("expected debug_traceTransaction, got %s", req["method"])
		}

		// Return a call tree with nested calls
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"type":    "CALL",
				"from":    "0xa",
				"to":      "0xb",
				"value":   "0x100",
				"gas":     "0x10000",
				"gasUsed": "0x8000",
				"input":   "0x",
				"output":  "0x",
				"calls": []map[string]interface{}{
					{
						"type":    "CALL",
						"from":    "0xb",
						"to":      "0xc",
						"value":   "0x0",
						"gas":     "0x5000",
						"gasUsed": "0x3000",
						"input":   "0x",
						"output":  "0x",
					},
				},
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL, WithTracerType(TracerCallTracer))
	ctx := context.Background()

	traces, err := adapter.TraceTransaction(ctx, txHash, blockNumber, timestamp)
	if err != nil {
		t.Fatalf("TraceTransaction failed: %v", err)
	}

	if len(traces) != 2 {
		t.Errorf("expected 2 traces, got %d", len(traces))
		return
	}

	// Check root call
	if traces[0].CallType != "call" {
		t.Errorf("traces[0].CallType = %q, want call", traces[0].CallType)
	}
	if !intSlicesEqual(traces[0].TraceAddress, []int{}) {
		t.Errorf("traces[0].TraceAddress = %v, want []", traces[0].TraceAddress)
	}

	// Check nested call
	if traces[1].CallType != "call" {
		t.Errorf("traces[1].CallType = %q, want call", traces[1].CallType)
	}
	if !intSlicesEqual(traces[1].TraceAddress, []int{0}) {
		t.Errorf("traces[1].TraceAddress = %v, want [0]", traces[1].TraceAddress)
	}
}

// TestTraceTransactionCreate tests tracing contract creation
func TestTraceTransactionCreate(t *testing.T) {
	txHash := "0xtxcreate"
	blockNumber := uint64(100)
	timestamp := time.Now()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"type":    "CREATE",
				"from":    "0xdeployer",
				"to":      "0xnewcontract",
				"value":   "0x0",
				"gas":     "0x100000",
				"gasUsed": "0x50000",
				"input":   "0x608060405234801561001057600080fd5b50",
				"output":  "0x608060",
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL, WithTracerType(TracerCallTracer))
	ctx := context.Background()

	traces, err := adapter.TraceTransaction(ctx, txHash, blockNumber, timestamp)
	if err != nil {
		t.Fatalf("TraceTransaction failed: %v", err)
	}

	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}

	trace := traces[0]
	if trace.CallType != "create" {
		t.Errorf("CallType = %q, want create", trace.CallType)
	}
	if trace.CreatedContractAddress != "0xnewcontract" {
		t.Errorf("CreatedContractAddress = %q, want 0xnewcontract", trace.CreatedContractAddress)
	}
	if trace.Init != "0x608060405234801561001057600080fd5b50" {
		t.Errorf("Init = %q", trace.Init)
	}
}

// ============================================================================
// Phase 1 Tests: ERC1155 TransferSingle Parsing
// ============================================================================

// TestParseTokenTransfersERC1155Single tests parsing ERC1155 TransferSingle logs
func TestParseTokenTransfersERC1155Single(t *testing.T) {
	// Use properly formatted 40-char addresses (without 0x prefix)
	// Topics are 32 bytes = 64 hex chars: 24 zeros padding + 40 char address
	fromAddr := "1234567890abcdef1234567890abcdef12345678"
	toAddr := "abcdef1234567890abcdef1234567890abcdef12"
	operatorAddr := "9999999999999999999999999999999999999999"

	log := Log{
		TxHash:      "0xtx123",
		LogIndex:    0,
		BlockNumber: 100,
		Address:     "0xerc1155token",
		Topics: []string{
			TopicTransferSingle,
			"0x000000000000000000000000" + operatorAddr, // operator (topic[1])
			"0x000000000000000000000000" + fromAddr,     // from (topic[2])
			"0x000000000000000000000000" + toAddr,       // to (topic[3])
		},
		// Data contains tokenId (32 bytes) + value (32 bytes)
		Data:    "0x00000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000064",
		Removed: false,
	}

	transfers := parseTokenTransfers(log, time.Now())

	if len(transfers) != 1 {
		t.Fatalf("expected 1 transfer, got %d", len(transfers))
	}

	transfer := transfers[0]
	if transfer.TokenType != "ERC1155" {
		t.Errorf("TokenType = %q, want ERC1155", transfer.TokenType)
	}
	if transfer.TokenAddress != "0xerc1155token" {
		t.Errorf("TokenAddress = %q, want 0xerc1155token", transfer.TokenAddress)
	}
	expectedFrom := "0x" + fromAddr
	if transfer.From != expectedFrom {
		t.Errorf("From = %q, want %q", transfer.From, expectedFrom)
	}
	expectedTo := "0x" + toAddr
	if transfer.To != expectedTo {
		t.Errorf("To = %q, want %q", transfer.To, expectedTo)
	}
}

// ============================================================================
// Phase 1 Benchmarks
// ============================================================================

func BenchmarkFlattenCallFrame(b *testing.B) {
	timestamp := time.Now()
	txHash := "0xtx123"
	blockNumber := uint64(100)

	// Create a moderately complex call tree
	frame := &CallFrame{
		Type:    "CALL",
		From:    "0xa",
		To:      "0xb",
		Gas:     "0x100000",
		GasUsed: "0x80000",
		Calls: []*CallFrame{
			{
				Type:    "CALL",
				From:    "0xb",
				To:      "0xc",
				Gas:     "0x50000",
				GasUsed: "0x30000",
				Calls: []*CallFrame{
					{Type: "STATICCALL", From: "0xc", To: "0xd", Gas: "0x10000", GasUsed: "0x5000"},
					{Type: "STATICCALL", From: "0xc", To: "0xe", Gas: "0x10000", GasUsed: "0x5000"},
				},
			},
			{
				Type:    "DELEGATECALL",
				From:    "0xb",
				To:      "0xf",
				Gas:     "0x30000",
				GasUsed: "0x20000",
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var traces []InternalTransaction
		flattenCallFrame(frame, txHash, blockNumber, timestamp, []int{}, &traces, 0)
	}
}

func BenchmarkNormalizeCallType(b *testing.B) {
	types := []string{"CALL", "DELEGATECALL", "STATICCALL", "CREATE", "CREATE2", "SELFDESTRUCT"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, t := range types {
			normalizeCallType(t)
		}
	}
}

func BenchmarkIntSliceToPostgresArray(b *testing.B) {
	arrays := [][]int{
		{},
		{0},
		{0, 1, 2},
		{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, arr := range arrays {
			intSliceToPostgresArray(arr)
		}
	}
}

func BenchmarkPostgresArrayToIntSlice(b *testing.B) {
	arrays := []string{"{}", "{0}", "{0,1,2}", "{0,1,2,3,4,5,6,7,8,9}"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, arr := range arrays {
			postgresArrayToIntSlice(arr)
		}
	}
}

func BenchmarkSortInternalTransactionsByTraceAddress(b *testing.B) {
	// Create a shuffled slice of internal transactions
	itxs := make([]InternalTransaction, 100)
	for i := range itxs {
		depth := i % 5
		addr := make([]int, depth)
		for j := 0; j < depth; j++ {
			addr[j] = i % (j + 1)
		}
		itxs[i] = InternalTransaction{ID: fmt.Sprintf("%d", i), TraceAddress: addr}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Make a copy to sort
		cpy := make([]InternalTransaction, len(itxs))
		copy(cpy, itxs)
		SortInternalTransactionsByTraceAddress(cpy)
	}
}
