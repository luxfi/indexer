// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package qchain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/luxfi/explorer/dag"
)

// TestParseVertexNotFinalizedStatus tests vertex status when not finalized
func TestParseVertexNotFinalizedStatus(t *testing.T) {
	adapter := New("")

	vertexJSON := `{
		"id": "qv-notfinal",
		"parentIds": [],
		"height": 1,
		"epoch": 1,
		"timestamp": "2025-01-01T00:00:00Z",
		"status": "pending",
		"proofCount": 0,
		"finalized": false
	}`

	v, err := adapter.ParseVertex(json.RawMessage(vertexJSON))
	if err != nil {
		t.Fatalf("ParseVertex failed: %v", err)
	}
	// Not finalized, so status should remain pending
	if v.Status != dag.StatusPending {
		t.Errorf("Status: got %s, want pending (finalized=false)", v.Status)
	}
}

// TestParseVertexManyParents tests vertex with many parent IDs
func TestParseVertexManyParents(t *testing.T) {
	adapter := New("")

	vertexJSON := `{
		"id": "qv-many-parents",
		"parentIds": ["p1", "p2", "p3", "p4", "p5"],
		"height": 100,
		"epoch": 10,
		"timestamp": "2025-06-01T12:00:00Z",
		"status": "pending",
		"proofCount": 0,
		"finalized": false
	}`

	v, err := adapter.ParseVertex(json.RawMessage(vertexJSON))
	if err != nil {
		t.Fatalf("ParseVertex failed: %v", err)
	}
	if len(v.ParentIDs) != 5 {
		t.Errorf("ParentIDs count: got %d, want 5", len(v.ParentIDs))
	}
}

// TestParseVertexWithLegacyFinalityProofAlgorithms tests different proof types in finality proof
func TestParseVertexWithLegacyFinalityProofAlgorithms(t *testing.T) {
	adapter := New("")
	proofTypes := []ProofType{ProofDilithium, ProofKyber, ProofFalcon, ProofSphincsh}

	for _, pt := range proofTypes {
		t.Run(string(pt), func(t *testing.T) {
			vertexJSON := `{
				"id": "qv-` + string(pt) + `",
				"parentIds": [],
				"height": 1,
				"epoch": 1,
				"timestamp": "2025-01-01T00:00:00Z",
				"status": "pending",
				"finalityProof": {
					"id": "proof-` + string(pt) + `",
					"vertexId": "qv-` + string(pt) + `",
					"proofType": "` + string(pt) + `",
					"latticeParams": {"dimension": 256, "modulus": 8380417, "securityLvl": 3, "algorithm": "` + string(pt) + `"},
					"signature": "AQID",
					"publicKey": "BAUG",
					"timestamp": "2025-01-01T00:00:00Z",
					"verified": true
				},
				"proofCount": 1,
				"finalized": true
			}`

			v, err := adapter.ParseVertex(json.RawMessage(vertexJSON))
			if err != nil {
				t.Fatalf("ParseVertex failed: %v", err)
			}
			if v.Metadata["proof_type"] != pt {
				t.Errorf("proof_type: got %v, want %s", v.Metadata["proof_type"], pt)
			}
		})
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
		t.Error("expected error for invalid JSON response")
	}
}

// TestGetVertexByIDRPCError tests RPC error for GetVertexByID
func TestGetVertexByIDRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := rpcResponse{
			JSONRPC: "2.0",
			ID:      1,
			Error:   &rpcError{Code: -32000, Message: "vertex not found"},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetVertexByID(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestGetVertexByIDNetworkError tests network error for GetVertexByID
func TestGetVertexByIDNetworkError(t *testing.T) {
	adapter := New("http://invalid-host:99999/rpc")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := adapter.GetVertexByID(ctx, "vtx123")
	if err == nil {
		t.Error("expected network error")
	}
}

// TestVerifyProofRPCError tests RPC error from VerifyProof
func TestVerifyProofRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := rpcResponse{
			JSONRPC: "2.0",
			ID:      1,
			Error:   &rpcError{Code: -32000, Message: "verification failed"},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.VerifyProof(context.Background(), &FinalityProof{
		ID:        "test",
		ProofType: ProofDilithium,
		Signature: []byte{0x01},
		PublicKey: []byte{0x02},
	})
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestQuantumStampAllFields tests all QuantumStamp fields serialize
func TestQuantumStampAllFields(t *testing.T) {
	stamp := QuantumStamp{
		ID:          "stamp-full",
		VertexID:    "vtx-full",
		ChainID:     "P",
		BlockHeight: 999999,
		BlockHash:   []byte{0xaa, 0xbb, 0xcc},
		Entropy:     []byte{0x11, 0x22},
		KeyID:       "key-full",
		Signature:   []byte{0xff, 0xee},
		Timestamp:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Certified:   false,
	}

	data, err := json.Marshal(stamp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded QuantumStamp
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.BlockHeight != 999999 {
		t.Errorf("BlockHeight: got %d, want 999999", decoded.BlockHeight)
	}
	if decoded.Certified {
		t.Error("Certified should be false")
	}
	if decoded.ChainID != "P" {
		t.Errorf("ChainID: got %s, want P", decoded.ChainID)
	}
}

// TestRingtailKeyRevoked tests revoked key state
func TestRingtailKeyRevoked(t *testing.T) {
	key := RingtailKey{
		ID:          "key-rev",
		PublicKey:   []byte{0x01},
		KeyType:     ProofDilithium,
		Algorithm:   "dilithium3",
		SecurityLvl: 3,
		Owner:       "lux1test",
		ValidFrom:   time.Now().Add(-time.Hour),
		ValidUntil:  time.Now().Add(time.Hour),
		Revoked:     true,
		CreatedAt:   time.Now(),
	}

	if !key.Revoked {
		t.Error("key should be revoked")
	}

	data, err := json.Marshal(key)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded RingtailKey
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !decoded.Revoked {
		t.Error("decoded key should be revoked")
	}
}

// TestNewConfigValues tests NewConfig returns expected defaults
func TestNewConfigValues(t *testing.T) {
	config := NewConfig()
	if config.PollInterval != 5*time.Second {
		t.Errorf("PollInterval: got %v, want 5s", config.PollInterval)
	}
}

// TestFinalityProofZeroLattice tests FinalityProof with zero lattice params
func TestFinalityProofZeroLattice(t *testing.T) {
	proof := FinalityProof{
		ID:            "proof-zero",
		VertexID:      "vtx-zero",
		ProofType:     ProofKyber,
		LatticeParams: Lattice{},
		Timestamp:     time.Now(),
		Verified:      false,
	}

	data, err := json.Marshal(proof)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded FinalityProof
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.LatticeParams.Dimension != 0 {
		t.Errorf("Dimension: got %d, want 0", decoded.LatticeParams.Dimension)
	}
	if decoded.Verified {
		t.Error("Verified should be false")
	}
}
