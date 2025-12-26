// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package xchain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/luxfi/indexer/dag"
)

// TestNew tests adapter creation
func TestNew(t *testing.T) {
	endpoint := "http://localhost:9650/ext/bc/X"
	adapter := New(endpoint)

	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if adapter.rpcEndpoint != endpoint {
		t.Errorf("expected endpoint %s, got %s", endpoint, adapter.rpcEndpoint)
	}
	if adapter.client == nil {
		t.Error("expected non-nil http client")
	}
	if adapter.client.Timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", adapter.client.Timeout)
	}
}

// TestAdapterImplementsInterface verifies Adapter implements dag.Adapter
func TestAdapterImplementsInterface(t *testing.T) {
	var _ dag.Adapter = (*Adapter)(nil)
}

// TestCallRPC tests JSON-RPC call functionality
func TestCallRPC(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		params     interface{}
		response   string
		wantErr    bool
		errContain string
	}{
		{
			name:   "successful call",
			method: "xvm.getRecentVertices",
			params: map[string]int{"limit": 10},
			response: `{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {"vertices": []}
			}`,
			wantErr: false,
		},
		{
			name:   "rpc error response",
			method: "xvm.invalidMethod",
			params: nil,
			response: `{
				"jsonrpc": "2.0",
				"id": 1,
				"error": {"code": -32601, "message": "method not found"}
			}`,
			wantErr:    true,
			errContain: "method not found",
		},
		{
			name:       "invalid json response",
			method:     "xvm.test",
			params:     nil,
			response:   `invalid json`,
			wantErr:    true,
			errContain: "decode response",
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

				var req rpcRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("failed to decode request: %v", err)
				}
				if req.JSONRPC != "2.0" {
					t.Errorf("expected jsonrpc 2.0, got %s", req.JSONRPC)
				}
				if req.Method != tt.method {
					t.Errorf("expected method %s, got %s", tt.method, req.Method)
				}

				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			adapter := New(server.URL)
			result, err := adapter.callRPC(context.Background(), tt.method, tt.params)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errContain != "" && !contains(err.Error(), tt.errContain) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContain)
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

// TestCallRPCNetworkError tests network error handling
func TestCallRPCNetworkError(t *testing.T) {
	adapter := New("http://localhost:99999/invalid")
	_, err := adapter.callRPC(context.Background(), "xvm.test", nil)
	if err == nil {
		t.Error("expected network error")
	}
}

// TestCallRPCContextCancellation tests context cancellation
func TestCallRPCContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := adapter.callRPC(ctx, "xvm.test", nil)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

// TestParseVertex tests vertex parsing
func TestParseVertex(t *testing.T) {
	tests := []struct {
		name       string
		data       string
		wantID     string
		wantType   string
		wantStatus dag.Status
		wantErr    bool
	}{
		{
			name: "accepted vertex",
			data: `{
				"id": "vtx123",
				"parentIDs": ["vtx122", "vtx121"],
				"height": 100,
				"epoch": 5,
				"txs": ["tx1", "tx2"],
				"timestamp": 1700000000,
				"status": "Accepted",
				"chainID": "X"
			}`,
			wantID:     "vtx123",
			wantType:   "xchain_vertex",
			wantStatus: dag.StatusAccepted,
			wantErr:    false,
		},
		{
			name: "pending vertex",
			data: `{
				"id": "vtx124",
				"parentIDs": [],
				"height": 101,
				"epoch": 5,
				"txs": [],
				"timestamp": 1700000001,
				"status": "Processing",
				"chainID": "X"
			}`,
			wantID:     "vtx124",
			wantType:   "xchain_vertex",
			wantStatus: dag.StatusPending,
			wantErr:    false,
		},
		{
			name: "rejected vertex",
			data: `{
				"id": "vtx125",
				"parentIDs": ["vtx100"],
				"height": 50,
				"epoch": 2,
				"txs": ["tx3"],
				"timestamp": 1700000002,
				"status": "Rejected",
				"chainID": "X"
			}`,
			wantID:     "vtx125",
			wantType:   "xchain_vertex",
			wantStatus: dag.StatusRejected,
			wantErr:    false,
		},
		{
			name: "zero timestamp uses current time",
			data: `{
				"id": "vtx126",
				"parentIDs": [],
				"height": 102,
				"epoch": 5,
				"txs": [],
				"timestamp": 0,
				"status": "Accepted",
				"chainID": "X"
			}`,
			wantID:     "vtx126",
			wantType:   "xchain_vertex",
			wantStatus: dag.StatusAccepted,
			wantErr:    false,
		},
		{
			name:    "invalid json",
			data:    `{invalid json}`,
			wantErr: true,
		},
	}

	adapter := New("http://localhost:9650")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vertex, err := adapter.ParseVertex(json.RawMessage(tt.data))

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if vertex.ID != tt.wantID {
				t.Errorf("expected ID %s, got %s", tt.wantID, vertex.ID)
			}
			if vertex.Type != tt.wantType {
				t.Errorf("expected Type %s, got %s", tt.wantType, vertex.Type)
			}
			if vertex.Status != tt.wantStatus {
				t.Errorf("expected Status %v, got %v", tt.wantStatus, vertex.Status)
			}
		})
	}
}

// TestGetRecentVertices tests fetching recent vertices
func TestGetRecentVertices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method != "xvm.getRecentVertices" {
			t.Errorf("expected method xvm.getRecentVertices, got %s", req.Method)
		}

		params := req.Params.(map[string]interface{})
		limit := int(params["limit"].(float64))

		vertices := make([]json.RawMessage, 0, limit)
		for i := 0; i < limit && i < 3; i++ {
			vertices = append(vertices, json.RawMessage(`{"id":"vtx`+string(rune('1'+i))+`"}`))
		}

		resp := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]interface{}{"vertices": vertices},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := New(server.URL)
	vertices, err := adapter.GetRecentVertices(context.Background(), 5)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vertices) != 3 {
		t.Errorf("expected 3 vertices, got %d", len(vertices))
	}
}

// TestGetVertexByID tests fetching a specific vertex
func TestGetVertexByID(t *testing.T) {
	tests := []struct {
		name     string
		vertexID string
		response string
		wantErr  bool
	}{
		{
			name:     "found vertex",
			vertexID: "vtx123",
			response: `{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {"vertex": {"id": "vtx123", "status": "Accepted"}}
			}`,
			wantErr: false,
		},
		{
			name:     "vertex not found",
			vertexID: "vtx999",
			response: `{
				"jsonrpc": "2.0",
				"id": 1,
				"error": {"code": -32000, "message": "vertex not found"}
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req rpcRequest
				json.NewDecoder(r.Body).Decode(&req)

				if req.Method != "xvm.getVertex" {
					t.Errorf("expected method xvm.getVertex, got %s", req.Method)
				}

				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			adapter := New(server.URL)
			vertex, err := adapter.GetVertexByID(context.Background(), tt.vertexID)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if vertex == nil {
					t.Error("expected non-nil vertex")
				}
			}
		})
	}
}

// TestUTXOStructure tests UTXO struct fields
func TestUTXOStructure(t *testing.T) {
	utxo := UTXO{
		ID:       "tx1:0",
		TxID:     "tx1",
		OutIndex: 0,
		AssetID:  "LUX",
		Amount:   1000000000,
		Address:  "X-lux1abc...",
		Spent:    false,
		SpentBy:  "",
	}

	if utxo.ID != "tx1:0" {
		t.Errorf("expected ID tx1:0, got %s", utxo.ID)
	}
	if utxo.Amount != 1000000000 {
		t.Errorf("expected Amount 1000000000, got %d", utxo.Amount)
	}
}

// TestAssetStructure tests Asset struct fields
func TestAssetStructure(t *testing.T) {
	asset := Asset{
		ID:           "LUX",
		Name:         "Lux",
		Symbol:       "LUX",
		Denomination: 9,
		TotalSupply:  720000000000000000,
	}

	if asset.Symbol != "LUX" {
		t.Errorf("expected Symbol LUX, got %s", asset.Symbol)
	}
	if asset.Denomination != 9 {
		t.Errorf("expected Denomination 9, got %d", asset.Denomination)
	}
}

// TestTransactionStructure tests Transaction struct fields
func TestTransactionStructure(t *testing.T) {
	tx := Transaction{
		ID:   "tx123",
		Type: "BaseTx",
		Inputs: []Input{
			{TxID: "tx100", OutIndex: 0, AssetID: "LUX", Amount: 1000, Address: "X-lux1..."},
		},
		Outputs: []Output{
			{AssetID: "LUX", Amount: 900, Addresses: []string{"X-lux1..."}, Threshold: 1},
		},
		Memo:      "test memo",
		Timestamp: 1700000000,
	}

	if tx.Type != "BaseTx" {
		t.Errorf("expected Type BaseTx, got %s", tx.Type)
	}
	if len(tx.Inputs) != 1 {
		t.Errorf("expected 1 input, got %d", len(tx.Inputs))
	}
	if len(tx.Outputs) != 1 {
		t.Errorf("expected 1 output, got %d", len(tx.Outputs))
	}
}

// TestVertexDataStructure tests VertexData struct
func TestVertexDataStructure(t *testing.T) {
	data := VertexData{
		Transactions: []Transaction{
			{ID: "tx1", Type: "BaseTx"},
			{ID: "tx2", Type: "CreateAssetTx"},
		},
		ChainID: "X",
	}

	if len(data.Transactions) != 2 {
		t.Errorf("expected 2 transactions, got %d", len(data.Transactions))
	}
	if data.ChainID != "X" {
		t.Errorf("expected ChainID X, got %s", data.ChainID)
	}
}

// TestRPCRequestSerialization tests JSON serialization of RPC request
func TestRPCRequestSerialization(t *testing.T) {
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "xvm.test",
		Params:  map[string]string{"key": "value"},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded rpcRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", decoded.JSONRPC)
	}
	if decoded.Method != "xvm.test" {
		t.Errorf("expected method xvm.test, got %s", decoded.Method)
	}
}

// TestRPCErrorStructure tests RPC error structure
func TestRPCErrorStructure(t *testing.T) {
	errResp := `{"code": -32601, "message": "method not found"}`

	var rpcErr rpcError
	if err := json.Unmarshal([]byte(errResp), &rpcErr); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if rpcErr.Code != -32601 {
		t.Errorf("expected code -32601, got %d", rpcErr.Code)
	}
	if rpcErr.Message != "method not found" {
		t.Errorf("expected message 'method not found', got %s", rpcErr.Message)
	}
}

// TestInputOutputStructures tests Input and Output struct serialization
func TestInputOutputStructures(t *testing.T) {
	input := Input{
		TxID:     "tx1",
		OutIndex: 0,
		AssetID:  "LUX",
		Amount:   1000,
		Address:  "X-lux1abc",
	}

	output := Output{
		AssetID:   "LUX",
		Amount:    900,
		Addresses: []string{"X-lux1def", "X-lux1ghi"},
		Threshold: 2,
		Locktime:  1700000000,
	}

	// Test JSON serialization
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal input: %v", err)
	}

	outputJSON, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("failed to marshal output: %v", err)
	}

	var decodedInput Input
	if err := json.Unmarshal(inputJSON, &decodedInput); err != nil {
		t.Fatalf("failed to unmarshal input: %v", err)
	}

	var decodedOutput Output
	if err := json.Unmarshal(outputJSON, &decodedOutput); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if decodedInput.TxID != "tx1" {
		t.Errorf("expected TxID tx1, got %s", decodedInput.TxID)
	}
	if decodedOutput.Threshold != 2 {
		t.Errorf("expected Threshold 2, got %d", decodedOutput.Threshold)
	}
	if len(decodedOutput.Addresses) != 2 {
		t.Errorf("expected 2 addresses, got %d", len(decodedOutput.Addresses))
	}
}

// helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// BenchmarkParseVertex benchmarks vertex parsing
func BenchmarkParseVertex(b *testing.B) {
	adapter := New("http://localhost:9650")
	data := json.RawMessage(`{
		"id": "vtx123",
		"parentIDs": ["vtx122", "vtx121"],
		"height": 100,
		"epoch": 5,
		"txs": ["tx1", "tx2", "tx3"],
		"timestamp": 1700000000,
		"status": "Accepted",
		"chainID": "X"
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.ParseVertex(data)
	}
}

// BenchmarkCallRPC benchmarks RPC calls
func BenchmarkCallRPC(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"vertices":[]}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.callRPC(ctx, "xvm.getRecentVertices", map[string]int{"limit": 10})
	}
}
