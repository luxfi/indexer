// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package pchain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/luxfi/indexer/chain"
)

// TestNew tests adapter creation
func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		expected string
	}{
		{
			name:     "custom endpoint",
			endpoint: "http://localhost:9650/ext/bc/P",
			expected: "http://localhost:9650/ext/bc/P",
		},
		{
			name:     "empty endpoint uses default",
			endpoint: "",
			expected: DefaultRPCEndpoint,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := New(tt.endpoint)

			if adapter == nil {
				t.Fatal("expected non-nil adapter")
			}
			if adapter.rpcEndpoint != tt.expected {
				t.Errorf("expected endpoint %s, got %s", tt.expected, adapter.rpcEndpoint)
			}
			if adapter.httpClient == nil {
				t.Error("expected non-nil http client")
			}
		})
	}
}

// TestConstants tests package constants
func TestConstants(t *testing.T) {
	if DefaultRPCEndpoint != "http://localhost:9650/ext/bc/P" {
		t.Errorf("unexpected DefaultRPCEndpoint: %s", DefaultRPCEndpoint)
	}
	if DefaultHTTPPort != 4100 {
		t.Errorf("expected DefaultHTTPPort 4100, got %d", DefaultHTTPPort)
	}
}

// TestAdapterImplementsInterface verifies Adapter implements chain.Adapter
func TestAdapterImplementsInterface(t *testing.T) {
	var _ chain.Adapter = (*Adapter)(nil)
}

// TestParseBlock tests block parsing
func TestParseBlock(t *testing.T) {
	tests := []struct {
		name       string
		data       string
		wantID     string
		wantHeight uint64
		wantTxs    int
		wantErr    bool
	}{
		{
			name: "block with transactions",
			data: `{
				"id": "block123",
				"parentID": "block122",
				"height": 100,
				"timestamp": 1700000000,
				"txs": [
					{"id": "tx1", "type": "AddValidatorTx"},
					{"id": "tx2", "type": "AddDelegatorTx"}
				]
			}`,
			wantID:     "block123",
			wantHeight: 100,
			wantTxs:    2,
			wantErr:    false,
		},
		{
			name: "empty block",
			data: `{
				"id": "block124",
				"parentID": "block123",
				"height": 101,
				"timestamp": 1700000001,
				"txs": []
			}`,
			wantID:     "block124",
			wantHeight: 101,
			wantTxs:    0,
			wantErr:    false,
		},
		{
			name:    "invalid json",
			data:    `{invalid}`,
			wantErr: true,
		},
	}

	adapter := New("")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block, err := adapter.ParseBlock(json.RawMessage(tt.data))

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if block.ID != tt.wantID {
				t.Errorf("expected ID %s, got %s", tt.wantID, block.ID)
			}
			if block.Height != tt.wantHeight {
				t.Errorf("expected Height %d, got %d", tt.wantHeight, block.Height)
			}
			if block.TxCount != tt.wantTxs {
				t.Errorf("expected %d txs, got %d", tt.wantTxs, block.TxCount)
			}
		})
	}
}

// TestParseBlockMetadata tests transaction type extraction
func TestParseBlockMetadata(t *testing.T) {
	adapter := New("")

	data := `{
		"id": "block125",
		"parentID": "block124",
		"height": 102,
		"timestamp": 1700000000,
		"txs": [
			{"id": "tx1", "type": "AddValidatorTx"},
			{"id": "tx2", "type": "AddValidatorTx"},
			{"id": "tx3", "type": "AddDelegatorTx"},
			{"id": "tx4", "type": "CreateNetTx"}
		]
	}`

	block, err := adapter.ParseBlock(json.RawMessage(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	txTypes := block.Metadata["txTypes"].(map[string]int)
	if txTypes["AddValidatorTx"] != 2 {
		t.Errorf("expected 2 AddValidatorTx, got %d", txTypes["AddValidatorTx"])
	}
	if txTypes["AddDelegatorTx"] != 1 {
		t.Errorf("expected 1 AddDelegatorTx, got %d", txTypes["AddDelegatorTx"])
	}
	if txTypes["CreateNetTx"] != 1 {
		t.Errorf("expected 1 CreateNetTx, got %d", txTypes["CreateNetTx"])
	}
}

// TestRPCRequest tests JSON-RPC request functionality
func TestRPCRequest(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		response    string
		wantErr     bool
		errContains string
	}{
		{
			name:   "successful request",
			method: "platform.getHeight",
			response: `{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {"height": "100"}
			}`,
			wantErr: false,
		},
		{
			name:   "rpc error",
			method: "pvm.invalidMethod",
			response: `{
				"jsonrpc": "2.0",
				"id": 1,
				"error": {"code": -32601, "message": "method not found"}
			}`,
			wantErr:     true,
			errContains: "method not found",
		},
		{
			name:        "invalid json response",
			method:      "pvm.test",
			response:    `invalid json`,
			wantErr:     true,
			errContains: "unmarshal response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("expected application/json content type")
				}
				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			adapter := New(server.URL)
			result, err := adapter.rpcRequest(context.Background(), tt.method, nil)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Error("expected non-nil result")
				}
			}
		})
	}
}

// TestGetRecentBlocks tests fetching recent blocks
func TestGetRecentBlocks(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		method := req["method"].(string)

		switch method {
		case "platform.getHeight":
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"height":"5"}}`))
		case "platform.getBlockByHeight":
			callCount++
			params := req["params"].(map[string]interface{})
			height := params["height"].(string)
			w.Write([]byte(`{
				"jsonrpc":"2.0","id":1,
				"result":{"block":{"id":"block` + height + `","height":` + height + `,"txs":[]}}
			}`))
		default:
			t.Errorf("unexpected method: %s", method)
		}
	}))
	defer server.Close()

	adapter := New(server.URL)
	blocks, err := adapter.GetRecentBlocks(context.Background(), 3)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 3 {
		t.Errorf("expected 3 blocks, got %d", len(blocks))
	}
}

// TestGetBlockByID tests fetching a block by ID
func TestGetBlockByID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["method"] != "platform.getBlock" {
			t.Errorf("expected method pvm.getBlock, got %v", req["method"])
		}

		params := req["params"].(map[string]interface{})
		if params["blockID"] != "block123" {
			t.Errorf("expected blockID block123, got %v", params["blockID"])
		}

		w.Write([]byte(`{
			"jsonrpc":"2.0","id":1,
			"result":{"block":{"id":"block123","height":100,"txs":[]}}
		}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	block, err := adapter.GetBlockByID(context.Background(), "block123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block == nil {
		t.Error("expected non-nil block")
	}
}

// TestGetBlockByHeight tests fetching a block by height
func TestGetBlockByHeight(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["method"] != "platform.getBlockByHeight" {
			t.Errorf("expected method pvm.getBlockByHeight, got %v", req["method"])
		}

		params := req["params"].(map[string]interface{})
		if params["height"] != "100" {
			t.Errorf("expected height 100, got %v", params["height"])
		}

		w.Write([]byte(`{
			"jsonrpc":"2.0","id":1,
			"result":{"block":{"id":"block100","height":100,"txs":[]}}
		}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	block, err := adapter.GetBlockByHeight(context.Background(), 100)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block == nil {
		t.Error("expected non-nil block")
	}
}

// TestGetCurrentValidators tests fetching current validators
func TestGetCurrentValidators(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["method"] != "platform.getCurrentValidators" {
			t.Errorf("expected method pvm.getCurrentValidators, got %v", req["method"])
		}

		w.Write([]byte(`{
			"jsonrpc":"2.0","id":1,
			"result":{
				"validators":[
					{"nodeID":"NodeID-1","stakeAmount":"2000000000000000","connected":true},
					{"nodeID":"NodeID-2","stakeAmount":"1500000000000000","connected":true}
				]
			}
		}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	validators, err := adapter.GetCurrentValidators(context.Background(), "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(validators) != 2 {
		t.Errorf("expected 2 validators, got %d", len(validators))
	}
	if validators[0].NodeID != "NodeID-1" {
		t.Errorf("expected NodeID-1, got %s", validators[0].NodeID)
	}
}

// TestGetPendingValidators tests fetching pending validators
func TestGetPendingValidators(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["method"] != "platform.getPendingValidators" {
			t.Errorf("expected method pvm.getPendingValidators, got %v", req["method"])
		}

		w.Write([]byte(`{
			"jsonrpc":"2.0","id":1,
			"result":{"validators":[{"nodeID":"NodeID-3","stakeAmount":"1000000000000000"}]}
		}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	validators, err := adapter.GetPendingValidators(context.Background(), "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(validators) != 1 {
		t.Errorf("expected 1 validator, got %d", len(validators))
	}
}

// TestGetNets tests fetching networks/subnets
func TestGetNets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["method"] != "platform.getBlockchains" {
			t.Errorf("expected method platform.getBlockchains, got %v", req["method"])
		}

		w.Write([]byte(`{
			"jsonrpc":"2.0","id":1,
			"result":{"blockchains":[{"id":"chain1","name":"C-Chain"},{"id":"chain2","name":"X-Chain"}]}
		}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	nets, err := adapter.GetNets(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nets) != 2 {
		t.Errorf("expected 2 nets, got %d", len(nets))
	}
}

// TestGetBlockchains tests fetching blockchains
func TestGetBlockchains(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["method"] != "platform.getBlockchains" {
			t.Errorf("expected method pvm.getBlockchains, got %v", req["method"])
		}

		w.Write([]byte(`{
			"jsonrpc":"2.0","id":1,
			"result":{
				"blockchains":[
					{"id":"chain1","name":"C-Chain","vmID":"evm"},
					{"id":"chain2","name":"X-Chain","vmID":"avm"}
				]
			}
		}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	chains, err := adapter.GetBlockchains(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chains) != 2 {
		t.Errorf("expected 2 chains, got %d", len(chains))
	}
	if chains[0].Name != "C-Chain" {
		t.Errorf("expected C-Chain, got %s", chains[0].Name)
	}
}

// TestValidatorStructure tests Validator struct
func TestValidatorStructure(t *testing.T) {
	validator := Validator{
		TxID:            "tx123",
		NodeID:          "NodeID-abc",
		StartTime:       "1700000000",
		EndTime:         "1731536000",
		StakeAmount:     "2000000000000000",
		PotentialReward: "100000000000",
		DelegationFee:   "2.0",
		Uptime:          "99.5",
		Connected:       true,
		Delegators: []Delegator{
			{TxID: "del1", NodeID: "NodeID-abc", StakeAmount: "500000000000000"},
		},
	}

	if !validator.Connected {
		t.Error("expected validator to be connected")
	}
	if len(validator.Delegators) != 1 {
		t.Errorf("expected 1 delegator, got %d", len(validator.Delegators))
	}

	data, err := json.Marshal(validator)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Validator
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.NodeID != "NodeID-abc" {
		t.Errorf("expected NodeID-abc, got %s", decoded.NodeID)
	}
}

// TestDelegatorStructure tests Delegator struct
func TestDelegatorStructure(t *testing.T) {
	delegator := Delegator{
		TxID:            "del123",
		NodeID:          "NodeID-abc",
		StartTime:       "1700000000",
		EndTime:         "1731536000",
		StakeAmount:     "500000000000000",
		PotentialReward: "25000000000",
		RewardOwner:     "P-lux1...",
	}

	data, err := json.Marshal(delegator)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Delegator
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.StakeAmount != "500000000000000" {
		t.Errorf("expected stake amount 500000000000000, got %s", decoded.StakeAmount)
	}
}

// TestNetStructure tests Net struct
func TestNetStructure(t *testing.T) {
	net := Net{
		ID:          "net123",
		ControlKeys: []string{"key1", "key2"},
		Threshold:   "2",
	}

	data, err := json.Marshal(net)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Net
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(decoded.ControlKeys) != 2 {
		t.Errorf("expected 2 control keys, got %d", len(decoded.ControlKeys))
	}
}

// TestBlockchainStructure tests Blockchain struct
func TestBlockchainStructure(t *testing.T) {
	blockchain := Blockchain{
		ID:    "chain123",
		Name:  "Test Chain",
		NetID: "net123",
		VMID:  "evm",
	}

	data, err := json.Marshal(blockchain)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Blockchain
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.VMID != "evm" {
		t.Errorf("expected VMID evm, got %s", decoded.VMID)
	}
}

// TestPChainBlockStructure tests PChainBlock struct
func TestPChainBlockStructure(t *testing.T) {
	block := PChainBlock{
		ID:        "block123",
		ParentID:  "block122",
		Height:    100,
		Timestamp: 1700000000,
		Txs: []PChainTx{
			{ID: "tx1", Type: "AddValidatorTx", NodeID: "NodeID-1"},
			{ID: "tx2", Type: "AddDelegatorTx", NodeID: "NodeID-1"},
		},
	}

	if len(block.Txs) != 2 {
		t.Errorf("expected 2 txs, got %d", len(block.Txs))
	}

	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded PChainBlock
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Height != 100 {
		t.Errorf("expected height 100, got %d", decoded.Height)
	}
}

// TestPChainTxStructure tests PChainTx struct
func TestPChainTxStructure(t *testing.T) {
	tx := PChainTx{
		ID:          "tx123",
		Type:        "AddValidatorTx",
		Inputs:      json.RawMessage(`[{"amount": "1000"}]`),
		Outputs:     json.RawMessage(`[{"amount": "900"}]`),
		Credentials: json.RawMessage(`[]`),
		NodeID:      "NodeID-abc",
		StartTime:   1700000000,
		EndTime:     1731536000,
		Weight:      2000000000000000,
		NetID:       "primary",
	}

	if tx.Type != "AddValidatorTx" {
		t.Errorf("expected type AddValidatorTx, got %s", tx.Type)
	}

	data, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded PChainTx
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Weight != 2000000000000000 {
		t.Errorf("expected weight 2000000000000000, got %d", decoded.Weight)
	}
}

// TestNewConfig tests default configuration
func TestNewConfig(t *testing.T) {
	config := NewConfig()

	if config.ChainType != chain.ChainP {
		t.Errorf("expected ChainP, got %v", config.ChainType)
	}
	if config.ChainName != "P-Chain (Platform)" {
		t.Errorf("expected P-Chain (Platform), got %s", config.ChainName)
	}
	if config.RPCEndpoint != DefaultRPCEndpoint {
		t.Errorf("expected default RPC endpoint")
	}
	if config.HTTPPort != DefaultHTTPPort {
		t.Errorf("expected port %d, got %d", DefaultHTTPPort, config.HTTPPort)
	}
	if config.PollInterval != 5*time.Second {
		t.Errorf("expected 5s poll interval")
	}
}

// TestNetworkError tests network error handling
func TestNetworkError(t *testing.T) {
	adapter := New("http://localhost:99999/invalid")

	_, err := adapter.GetBlockByID(context.Background(), "block123")
	if err == nil {
		t.Error("expected network error")
	}

	_, err = adapter.GetBlockByHeight(context.Background(), 100)
	if err == nil {
		t.Error("expected network error")
	}
}

// TestContextCancellation tests context cancellation
func TestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adapter.GetBlockByID(ctx, "block123")
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

// helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// BenchmarkParseBlock benchmarks block parsing
func BenchmarkParseBlock(b *testing.B) {
	adapter := New("")
	data := json.RawMessage(`{
		"id": "block123",
		"parentID": "block122",
		"height": 100,
		"timestamp": 1700000000,
		"txs": [
			{"id": "tx1", "type": "AddValidatorTx"},
			{"id": "tx2", "type": "AddDelegatorTx"},
			{"id": "tx3", "type": "CreateNetTx"}
		]
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.ParseBlock(data)
	}
}

// BenchmarkGetBlockByHeight benchmarks RPC calls
func BenchmarkGetBlockByHeight(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"block":{"id":"block100","height":100,"txs":[]}}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.GetBlockByHeight(ctx, 100)
	}
}
