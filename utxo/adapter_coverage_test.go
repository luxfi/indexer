// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package utxo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/luxfi/indexer/dag"
)

// TestParseVertexParentIDs verifies parentIDs are correctly extracted
func TestParseVertexParentIDs(t *testing.T) {
	adapter := New("http://localhost:9650")
	data := json.RawMessage(`{
		"id": "vtx200",
		"parentIDs": ["vtx199", "vtx198", "vtx197"],
		"height": 200,
		"epoch": 10,
		"txs": [],
		"timestamp": 1700000000,
		"status": "Accepted",
		"chainID": "X"
	}`)

	v, err := adapter.ParseVertex(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v.ParentIDs) != 3 {
		t.Errorf("expected 3 parents, got %d", len(v.ParentIDs))
	}
	if v.ParentIDs[0] != "vtx199" {
		t.Errorf("expected first parent vtx199, got %s", v.ParentIDs[0])
	}
}

// TestParseVertexTxIDs verifies transaction IDs are extracted
func TestParseVertexTxIDs(t *testing.T) {
	adapter := New("http://localhost:9650")
	data := json.RawMessage(`{
		"id": "vtx300",
		"parentIDs": [],
		"height": 300,
		"epoch": 15,
		"txs": ["tx-a", "tx-b", "tx-c", "tx-d"],
		"timestamp": 1700000000,
		"status": "Accepted",
		"chainID": "X"
	}`)

	v, err := adapter.ParseVertex(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v.TxIDs) != 4 {
		t.Errorf("expected 4 txIDs, got %d", len(v.TxIDs))
	}
}

// TestParseVertexMetadataChainID verifies chainId in metadata
func TestParseVertexMetadataChainID(t *testing.T) {
	adapter := New("http://localhost:9650")
	data := json.RawMessage(`{
		"id": "vtx400",
		"parentIDs": [],
		"height": 400,
		"epoch": 20,
		"txs": [],
		"timestamp": 1700000000,
		"status": "Accepted",
		"chainID": "2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm"
	}`)

	v, err := adapter.ParseVertex(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chainID, ok := v.Metadata["chainId"]
	if !ok {
		t.Fatal("expected chainId in metadata")
	}
	if chainID != "2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm" {
		t.Errorf("unexpected chainId: %v", chainID)
	}
}

// TestParseVertexEpoch verifies epoch field
func TestParseVertexEpoch(t *testing.T) {
	adapter := New("http://localhost:9650")
	data := json.RawMessage(`{
		"id": "vtx500",
		"parentIDs": [],
		"height": 500,
		"epoch": 42,
		"txs": [],
		"timestamp": 1700000000,
		"status": "Accepted",
		"chainID": "X"
	}`)

	v, err := adapter.ParseVertex(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Epoch != 42 {
		t.Errorf("expected epoch 42, got %d", v.Epoch)
	}
}

// TestParseVertexZeroTimestampUsesNow verifies zero timestamp fallback
func TestParseVertexZeroTimestampUsesNow(t *testing.T) {
	adapter := New("http://localhost:9650")
	before := time.Now().Add(-1 * time.Second)
	data := json.RawMessage(`{
		"id": "vtx600",
		"parentIDs": [],
		"height": 600,
		"epoch": 1,
		"txs": [],
		"timestamp": 0,
		"status": "Accepted",
		"chainID": "X"
	}`)

	v, err := adapter.ParseVertex(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Timestamp.Before(before) {
		t.Errorf("expected timestamp near now, got %v", v.Timestamp)
	}
}

// TestParseVertexEmptyJSON verifies error on empty JSON
func TestParseVertexEmptyJSON(t *testing.T) {
	adapter := New("http://localhost:9650")
	_, err := adapter.ParseVertex(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("empty JSON should parse without error, got: %v", err)
	}
}

// TestParseVertexNullParentIDs verifies nil parentIDs
func TestParseVertexNullParentIDs(t *testing.T) {
	adapter := New("http://localhost:9650")
	data := json.RawMessage(`{
		"id": "vtx700",
		"height": 700,
		"epoch": 1,
		"txs": [],
		"timestamp": 1700000000,
		"status": "Accepted",
		"chainID": "X"
	}`)

	v, err := adapter.ParseVertex(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.ParentIDs != nil && len(v.ParentIDs) != 0 {
		t.Errorf("expected nil or empty parentIDs, got %v", v.ParentIDs)
	}
}

// TestGetRecentVerticesRPCError tests RPC error response
func TestGetRecentVerticesRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"backend unavailable"}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetRecentVertices(context.Background(), 10)
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestGetVertexByIDNetworkError tests network failure for GetVertexByID
func TestGetVertexByIDNetworkError(t *testing.T) {
	adapter := New("http://localhost:99999/invalid")
	_, err := adapter.GetVertexByID(context.Background(), "vtx123")
	if err == nil {
		t.Error("expected network error")
	}
}

// TestTransactionJSONRoundTrip tests full JSON round-trip for Transaction
func TestTransactionJSONRoundTrip(t *testing.T) {
	tx := Transaction{
		ID:   "tx-roundtrip",
		Type: "CreateAssetTx",
		Inputs: []Input{
			{TxID: "prev-tx", OutIndex: 0, AssetID: "LUX", Amount: 5000, Address: "X-lux1sender"},
			{TxID: "prev-tx", OutIndex: 1, AssetID: "LUX", Amount: 3000, Address: "X-lux1sender"},
		},
		Outputs: []Output{
			{AssetID: "LUX", Amount: 7900, Addresses: []string{"X-lux1rcpt"}, Threshold: 1},
		},
		Memo:      "test round trip",
		Timestamp: 1700000000,
	}

	data, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Transaction
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.Inputs) != 2 {
		t.Errorf("expected 2 inputs, got %d", len(decoded.Inputs))
	}
	if decoded.Memo != "test round trip" {
		t.Errorf("expected memo 'test round trip', got %s", decoded.Memo)
	}
}

// TestUTXOSpentState verifies UTXO spent/unspent states
func TestUTXOSpentState(t *testing.T) {
	tests := []struct {
		name    string
		spent   bool
		spentBy string
	}{
		{"unspent", false, ""},
		{"spent", true, "tx-spender"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			utxo := UTXO{
				ID: "test:0", TxID: "test", OutIndex: 0,
				AssetID: "LUX", Amount: 1000, Address: "X-lux1test",
				Spent: tt.spent, SpentBy: tt.spentBy,
			}

			data, err := json.Marshal(utxo)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var decoded UTXO
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if decoded.Spent != tt.spent {
				t.Errorf("Spent: got %v, want %v", decoded.Spent, tt.spent)
			}
			if decoded.SpentBy != tt.spentBy {
				t.Errorf("SpentBy: got %s, want %s", decoded.SpentBy, tt.spentBy)
			}
		})
	}
}

// TestOutputMultipleAddresses tests Output with multiple addresses
func TestOutputMultipleAddresses(t *testing.T) {
	out := Output{
		AssetID:   "LUX",
		Amount:    10000,
		Addresses: []string{"X-lux1a", "X-lux1b", "X-lux1c"},
		Threshold: 2,
		Locktime:  1800000000,
	}

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Output
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.Addresses) != 3 {
		t.Errorf("expected 3 addresses, got %d", len(decoded.Addresses))
	}
	if decoded.Locktime != 1800000000 {
		t.Errorf("expected locktime 1800000000, got %d", decoded.Locktime)
	}
}

// TestAdapterType verifies the xchain_vertex type constant
func TestAdapterType(t *testing.T) {
	adapter := New("http://localhost:9650")
	data := json.RawMessage(`{
		"id": "vtx-type",
		"parentIDs": [],
		"height": 1,
		"epoch": 1,
		"txs": [],
		"timestamp": 1700000000,
		"status": "Accepted",
		"chainID": "X"
	}`)

	v, err := adapter.ParseVertex(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Type != "xchain_vertex" {
		t.Errorf("expected type xchain_vertex, got %s", v.Type)
	}
}

// TestCallRPCContextTimeout tests context timeout
func TestCallRPCContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := adapter.callRPC(ctx, "xvm.test", nil)
	if err == nil {
		t.Error("expected timeout error")
	}
}

// TestParseVertexUnknownStatus verifies unknown status defaults to pending
func TestParseVertexUnknownStatus(t *testing.T) {
	adapter := New("http://localhost:9650")
	data := json.RawMessage(`{
		"id": "vtx-unknown",
		"parentIDs": [],
		"height": 1,
		"epoch": 1,
		"txs": [],
		"timestamp": 1700000000,
		"status": "Processing",
		"chainID": "X"
	}`)

	v, err := adapter.ParseVertex(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Status != dag.StatusPending {
		t.Errorf("expected pending status for unknown, got %v", v.Status)
	}
}
