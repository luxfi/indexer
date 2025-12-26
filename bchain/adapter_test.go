// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package bchain

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
	endpoint := "http://localhost:9650/ext/bc/B"
	adapter := New(endpoint)

	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if adapter.rpcEndpoint != endpoint {
		t.Errorf("expected endpoint %s, got %s", endpoint, adapter.rpcEndpoint)
	}
	if adapter.httpClient == nil {
		t.Error("expected non-nil http client")
	}
	if adapter.httpClient.Timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", adapter.httpClient.Timeout)
	}
}

// TestConstants tests package constants
func TestConstants(t *testing.T) {
	if DefaultPort != 4600 {
		t.Errorf("expected DefaultPort 4600, got %d", DefaultPort)
	}
	if DefaultDatabase != "explorer_bchain" {
		t.Errorf("expected DefaultDatabase explorer_bchain, got %s", DefaultDatabase)
	}
	if RPCMethod != "bvm" {
		t.Errorf("expected RPCMethod bvm, got %s", RPCMethod)
	}
}

// TestBridgeStatusConstants tests bridge status constants
func TestBridgeStatusConstants(t *testing.T) {
	tests := []struct {
		status   BridgeStatus
		expected string
	}{
		{BridgeStatusPending, "pending"},
		{BridgeStatusLocked, "locked"},
		{BridgeStatusConfirmed, "confirmed"},
		{BridgeStatusReleased, "released"},
		{BridgeStatusFailed, "failed"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, tt.status)
		}
	}
}

// TestAdapterImplementsInterface verifies Adapter implements dag.Adapter
func TestAdapterImplementsInterface(t *testing.T) {
	var _ dag.Adapter = (*Adapter)(nil)
}

// TestParseVertex tests vertex parsing
func TestParseVertex(t *testing.T) {
	tests := []struct {
		name          string
		data          string
		wantID        string
		wantType      string
		wantTransfers int
		wantErr       bool
	}{
		{
			name: "vertex with transfers",
			data: `{
				"vertexId": "vtx123",
				"type": "bridge_vertex",
				"parentIds": ["vtx122"],
				"height": 100,
				"epoch": 5,
				"timestamp": 1700000000,
				"status": "Accepted",
				"transfers": [
					{"id": "t1", "sourceChain": "C", "destChain": "X", "amount": "1000"},
					{"id": "t2", "sourceChain": "X", "destChain": "P", "amount": "2000"}
				],
				"proofs": [],
				"lockedAssets": []
			}`,
			wantID:        "vtx123",
			wantType:      "bridge_vertex",
			wantTransfers: 2,
			wantErr:       false,
		},
		{
			name: "vertex with proofs",
			data: `{
				"vertexId": "vtx124",
				"type": "proof_vertex",
				"parentIds": [],
				"height": 101,
				"epoch": 5,
				"timestamp": 1700000001,
				"status": "Pending",
				"transfers": [],
				"proofs": [{"id": "p1", "verified": true}],
				"lockedAssets": [{"assetId": "LUX", "amount": "5000"}]
			}`,
			wantID:        "vtx124",
			wantType:      "proof_vertex",
			wantTransfers: 0,
			wantErr:       false,
		},
		{
			name:    "invalid json",
			data:    `{invalid}`,
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

			// Check metadata
			if tc, ok := vertex.Metadata["transferCount"].(int); ok {
				if tc != tt.wantTransfers {
					t.Errorf("expected %d transfers, got %d", tt.wantTransfers, tc)
				}
			}
		})
	}
}

// TestParseVertexMetadata tests metadata extraction
func TestParseVertexMetadata(t *testing.T) {
	adapter := New("http://localhost:9650")

	data := `{
		"vertexId": "vtx125",
		"type": "multi_chain",
		"parentIds": [],
		"height": 102,
		"epoch": 5,
		"timestamp": 1700000000,
		"status": "Accepted",
		"transfers": [
			{"id": "t1", "sourceChain": "C-Chain", "destChain": "X-Chain"},
			{"id": "t2", "sourceChain": "X-Chain", "destChain": "P-Chain"},
			{"id": "t3", "sourceChain": "C-Chain", "destChain": "P-Chain"}
		],
		"proofs": [{"id": "p1"}, {"id": "p2"}],
		"lockedAssets": [{"assetId": "LUX"}]
	}`

	vertex, err := adapter.ParseVertex(json.RawMessage(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check transfer count
	if tc := vertex.Metadata["transferCount"]; tc != 3 {
		t.Errorf("expected 3 transfers, got %v", tc)
	}

	// Check proof count
	if pc := vertex.Metadata["proofCount"]; pc != 2 {
		t.Errorf("expected 2 proofs, got %v", pc)
	}

	// Check locked asset count
	if lac := vertex.Metadata["lockedAssetCount"]; lac != 1 {
		t.Errorf("expected 1 locked asset, got %v", lac)
	}

	// Check chains involved
	chains := vertex.Metadata["chainsInvolved"].([]string)
	if len(chains) != 3 {
		t.Errorf("expected 3 unique chains, got %d", len(chains))
	}
}

// TestGetRecentVertices tests fetching recent vertices
func TestGetRecentVertices(t *testing.T) {
	tests := []struct {
		name        string
		response    string
		limit       int
		wantCount   int
		wantErr     bool
		errContains string
	}{
		{
			name: "successful fetch",
			response: `{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {
					"vertices": [
						{"vertexId": "vtx1"},
						{"vertexId": "vtx2"}
					]
				}
			}`,
			limit:     10,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "rpc error",
			response: `{
				"jsonrpc": "2.0",
				"id": 1,
				"error": {"code": -32000, "message": "internal error"}
			}`,
			limit:       10,
			wantErr:     true,
			errContains: "internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req map[string]interface{}
				json.NewDecoder(r.Body).Decode(&req)

				if req["method"] != "bvm.getRecentVertices" {
					t.Errorf("expected method bvm.getRecentVertices, got %v", req["method"])
				}

				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			adapter := New(server.URL)
			vertices, err := adapter.GetRecentVertices(context.Background(), tt.limit)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(vertices) != tt.wantCount {
				t.Errorf("expected %d vertices, got %d", tt.wantCount, len(vertices))
			}
		})
	}
}

// TestGetVertexByID tests fetching a specific vertex
func TestGetVertexByID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["method"] != "bvm.getVertex" {
			t.Errorf("expected method bvm.getVertex, got %v", req["method"])
		}

		params := req["params"].(map[string]interface{})
		if params["vertexId"] != "vtx123" {
			t.Errorf("expected vertexId vtx123, got %v", params["vertexId"])
		}

		w.Write([]byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"result": {"vertexId": "vtx123", "status": "Accepted"}
		}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	result, err := adapter.GetVertexByID(context.Background(), "vtx123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

// TestTransferStructure tests Transfer struct
func TestTransferStructure(t *testing.T) {
	transfer := Transfer{
		ID:            "txf123",
		SourceChain:   "C-Chain",
		DestChain:     "X-Chain",
		SourceTxID:    "stx123",
		DestTxID:      "dtx123",
		Sender:        "0x1234...",
		Recipient:     "X-lux1...",
		AssetID:       "LUX",
		Amount:        "1000000000",
		Fee:           "1000000",
		Status:        BridgeStatusConfirmed,
		LockHeight:    100,
		ReleaseHeight: 105,
		ProofHash:     "0xproof...",
		Timestamp:     time.Now(),
	}

	if transfer.Status != BridgeStatusConfirmed {
		t.Errorf("expected status confirmed, got %s", transfer.Status)
	}

	// Test JSON serialization
	data, err := json.Marshal(transfer)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Transfer
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.SourceChain != "C-Chain" {
		t.Errorf("expected SourceChain C-Chain, got %s", decoded.SourceChain)
	}
}

// TestProofStructure tests Proof struct
func TestProofStructure(t *testing.T) {
	now := time.Now()
	proof := Proof{
		ID:         "proof123",
		TransferID: "txf123",
		ProofType:  "merkle",
		ProofData:  "0xproofdata...",
		Validators: []string{"val1", "val2", "val3"},
		Signatures: []string{"sig1", "sig2", "sig3"},
		Verified:   true,
		VerifiedAt: now,
		CreatedAt:  now,
	}

	if !proof.Verified {
		t.Error("expected proof to be verified")
	}
	if len(proof.Validators) != 3 {
		t.Errorf("expected 3 validators, got %d", len(proof.Validators))
	}

	data, err := json.Marshal(proof)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Proof
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ProofType != "merkle" {
		t.Errorf("expected ProofType merkle, got %s", decoded.ProofType)
	}
}

// TestLockedAssetStructure tests LockedAsset struct
func TestLockedAssetStructure(t *testing.T) {
	now := time.Now()
	asset := LockedAsset{
		AssetID:     "LUX",
		SourceChain: "C-Chain",
		Amount:      "5000000000",
		LockTxID:    "lock123",
		LockedAt:    now,
		UnlockedAt:  time.Time{},
	}

	if asset.AssetID != "LUX" {
		t.Errorf("expected AssetID LUX, got %s", asset.AssetID)
	}

	data, err := json.Marshal(asset)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded LockedAsset
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Amount != "5000000000" {
		t.Errorf("expected Amount 5000000000, got %s", decoded.Amount)
	}
}

// TestVertexDataStructure tests vertexData struct
func TestVertexDataStructure(t *testing.T) {
	vd := vertexData{
		VertexID:  "vtx123",
		Type:      "bridge",
		ParentIDs: []string{"vtx122"},
		Height:    100,
		Epoch:     5,
		Timestamp: 1700000000,
		Status:    "Accepted",
		Transfers: []Transfer{
			{ID: "t1", SourceChain: "C", DestChain: "X"},
		},
		Proofs: []Proof{
			{ID: "p1", ProofType: "merkle"},
		},
		LockedAssets: []LockedAsset{
			{AssetID: "LUX", Amount: "1000"},
		},
	}

	if len(vd.Transfers) != 1 {
		t.Errorf("expected 1 transfer, got %d", len(vd.Transfers))
	}
	if len(vd.Proofs) != 1 {
		t.Errorf("expected 1 proof, got %d", len(vd.Proofs))
	}
	if len(vd.LockedAssets) != 1 {
		t.Errorf("expected 1 locked asset, got %d", len(vd.LockedAssets))
	}
}

// TestNetworkError tests network error handling
func TestNetworkError(t *testing.T) {
	adapter := New("http://localhost:99999/invalid")

	_, err := adapter.GetRecentVertices(context.Background(), 10)
	if err == nil {
		t.Error("expected network error")
	}

	_, err = adapter.GetVertexByID(context.Background(), "vtx123")
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

	_, err := adapter.GetRecentVertices(ctx, 10)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

// TestAllBridgeStatuses tests all bridge status values
func TestAllBridgeStatuses(t *testing.T) {
	statuses := []BridgeStatus{
		BridgeStatusPending,
		BridgeStatusLocked,
		BridgeStatusConfirmed,
		BridgeStatusReleased,
		BridgeStatusFailed,
	}

	for _, status := range statuses {
		transfer := Transfer{Status: status}
		data, err := json.Marshal(transfer)
		if err != nil {
			t.Fatalf("failed to marshal transfer with status %s: %v", status, err)
		}

		var decoded Transfer
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal transfer with status %s: %v", status, err)
		}

		if decoded.Status != status {
			t.Errorf("expected status %s, got %s", status, decoded.Status)
		}
	}
}

// BenchmarkParseVertex benchmarks vertex parsing
func BenchmarkParseVertex(b *testing.B) {
	adapter := New("http://localhost:9650")
	data := json.RawMessage(`{
		"vertexId": "vtx123",
		"type": "bridge_vertex",
		"parentIds": ["vtx122", "vtx121"],
		"height": 100,
		"epoch": 5,
		"timestamp": 1700000000,
		"status": "Accepted",
		"transfers": [
			{"id": "t1", "sourceChain": "C", "destChain": "X", "amount": "1000"},
			{"id": "t2", "sourceChain": "X", "destChain": "P", "amount": "2000"}
		],
		"proofs": [{"id": "p1", "verified": true}],
		"lockedAssets": [{"assetId": "LUX", "amount": "5000"}]
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.ParseVertex(data)
	}
}

// BenchmarkGetRecentVertices benchmarks RPC calls
func BenchmarkGetRecentVertices(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"vertices":[{"vertexId":"vtx1"}]}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.GetRecentVertices(ctx, 10)
	}
}
