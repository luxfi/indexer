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
)

// TestParseVertexEmptyTransfers tests vertex with no transfers, proofs, or locked assets
func TestParseVertexEmptyTransfers(t *testing.T) {
	adapter := New("http://localhost:9650")
	data := json.RawMessage(`{
		"vertexId": "vtx-empty",
		"type": "bridge_vertex",
		"parentIds": [],
		"height": 1,
		"epoch": 1,
		"timestamp": 1700000000,
		"status": "Accepted",
		"transfers": [],
		"proofs": [],
		"lockedAssets": []
	}`)

	v, err := adapter.ParseVertex(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Metadata["transferCount"] != 0 {
		t.Errorf("expected 0 transfers, got %v", v.Metadata["transferCount"])
	}
	if v.Metadata["proofCount"] != 0 {
		t.Errorf("expected 0 proofs, got %v", v.Metadata["proofCount"])
	}
	chains := v.Metadata["chainsInvolved"].([]string)
	if len(chains) != 0 {
		t.Errorf("expected 0 chains, got %d", len(chains))
	}
}

// TestParseVertexSingleChain tests transfers within same chain pair
func TestParseVertexSingleChain(t *testing.T) {
	adapter := New("http://localhost:9650")
	data := json.RawMessage(`{
		"vertexId": "vtx-single",
		"type": "bridge",
		"parentIds": [],
		"height": 10,
		"epoch": 1,
		"timestamp": 1700000000,
		"status": "Accepted",
		"transfers": [
			{"id": "t1", "sourceChain": "C", "destChain": "X"},
			{"id": "t2", "sourceChain": "C", "destChain": "X"}
		],
		"proofs": [],
		"lockedAssets": []
	}`)

	v, err := adapter.ParseVertex(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Metadata["transferCount"] != 2 {
		t.Errorf("expected 2 transfers, got %v", v.Metadata["transferCount"])
	}
	chains := v.Metadata["chainsInvolved"].([]string)
	if len(chains) != 2 {
		t.Errorf("expected 2 unique chains (C and X), got %d", len(chains))
	}
}

// TestGetRecentVerticesInvalidJSON tests invalid JSON response
func TestGetRecentVerticesInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not valid json`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetRecentVertices(context.Background(), 10)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// TestGetVertexByIDRPCError tests RPC error from GetVertexByID
func TestGetVertexByIDRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"not found"}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetVertexByID(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected RPC error")
	}
}

// TestGetVertexByIDNetworkError tests network error from GetVertexByID
func TestGetVertexByIDNetworkError(t *testing.T) {
	adapter := New("http://localhost:99999/invalid")
	_, err := adapter.GetVertexByID(context.Background(), "vtx123")
	if err == nil {
		t.Error("expected network error")
	}
}

// TestTransferAllStatuses tests all bridge statuses in Transfer
func TestTransferAllStatuses(t *testing.T) {
	statuses := []BridgeStatus{
		BridgeStatusPending,
		BridgeStatusLocked,
		BridgeStatusConfirmed,
		BridgeStatusReleased,
		BridgeStatusFailed,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			transfer := Transfer{
				ID:          "t-" + string(status),
				SourceChain: "C",
				DestChain:   "X",
				Sender:      "0xsender",
				Recipient:   "X-lux1rcpt",
				AssetID:     "LUX",
				Amount:      "1000000000",
				Status:      status,
				Timestamp:   time.Now(),
			}

			data, err := json.Marshal(transfer)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var decoded Transfer
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if decoded.Status != status {
				t.Errorf("expected status %s, got %s", status, decoded.Status)
			}
		})
	}
}

// TestProofVerificationStates tests proof verified/unverified states
func TestProofVerificationStates(t *testing.T) {
	tests := []struct {
		name     string
		verified bool
	}{
		{"verified", true},
		{"unverified", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proof := Proof{
				ID:         "proof-" + tt.name,
				TransferID: "t1",
				ProofType:  "merkle",
				ProofData:  "0xdata",
				Verified:   tt.verified,
				CreatedAt:  time.Now(),
			}

			data, err := json.Marshal(proof)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var decoded Proof
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if decoded.Verified != tt.verified {
				t.Errorf("Verified: got %v, want %v", decoded.Verified, tt.verified)
			}
		})
	}
}

// TestProofTypes tests all proof types
func TestProofTypes(t *testing.T) {
	proofTypes := []string{"merkle", "signature", "zk"}

	for _, pt := range proofTypes {
		proof := Proof{
			ID:        "proof-" + pt,
			ProofType: pt,
			ProofData: "0x" + pt,
			CreatedAt: time.Now(),
		}

		data, err := json.Marshal(proof)
		if err != nil {
			t.Fatalf("marshal %s: %v", pt, err)
		}

		var decoded Proof
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal %s: %v", pt, err)
		}

		if decoded.ProofType != pt {
			t.Errorf("expected ProofType %s, got %s", pt, decoded.ProofType)
		}
	}
}

// TestLockedAssetUnlocked tests locked asset with unlock time
func TestLockedAssetUnlocked(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	asset := LockedAsset{
		AssetID:     "LUX",
		SourceChain: "C",
		Amount:      "1000",
		LockTxID:    "lock1",
		LockedAt:    now.Add(-1 * time.Hour),
		UnlockedAt:  now,
	}

	data, err := json.Marshal(asset)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded LockedAsset
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.UnlockedAt.IsZero() {
		t.Error("expected non-zero UnlockedAt")
	}
}

// TestGetRecentVerticesContextCancellation tests context cancellation
func TestGetRecentVerticesContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
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
