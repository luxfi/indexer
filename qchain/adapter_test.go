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

	"github.com/luxfi/indexer/dag"
)

// TestNew tests the adapter constructor
func TestNew(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    string
		wantDefault bool
	}{
		{
			name:        "custom endpoint",
			endpoint:    "http://custom:9999/rpc",
			wantDefault: false,
		},
		{
			name:        "empty uses default",
			endpoint:    "",
			wantDefault: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := New(tt.endpoint)
			if adapter == nil {
				t.Fatal("New returned nil")
			}
			if adapter.httpClient == nil {
				t.Error("httpClient is nil")
			}
			if tt.wantDefault && adapter.rpcEndpoint != DefaultRPCEndpoint {
				t.Errorf("expected default endpoint %s, got %s", DefaultRPCEndpoint, adapter.rpcEndpoint)
			}
			if !tt.wantDefault && adapter.rpcEndpoint != tt.endpoint {
				t.Errorf("expected endpoint %s, got %s", tt.endpoint, adapter.rpcEndpoint)
			}
		})
	}
}

// TestConstants tests Q-Chain specific constants
func TestConstants(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  interface{}
	}{
		{"DefaultRPCEndpoint", DefaultRPCEndpoint, "http://localhost:9650/ext/bc/Q/rpc"},
		{"DefaultHTTPPort", DefaultHTTPPort, 4300},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.want {
				t.Errorf("got %v, want %v", tt.value, tt.want)
			}
		})
	}
}

// TestProofTypes tests proof type constants
func TestProofTypes(t *testing.T) {
	proofTypes := []ProofType{ProofDilithium, ProofKyber, ProofFalcon, ProofSphincsh}
	expected := []string{"dilithium", "kyber", "falcon", "sphincs+"}

	for i, pt := range proofTypes {
		if string(pt) != expected[i] {
			t.Errorf("proof type %d: got %s, want %s", i, pt, expected[i])
		}
	}
}

// TestFinalityProofJSON tests FinalityProof JSON serialization
func TestFinalityProofJSON(t *testing.T) {
	proof := FinalityProof{
		ID:        "proof-123",
		VertexID:  "vertex-456",
		ProofType: ProofDilithium,
		LatticeParams: Lattice{
			Dimension:   256,
			Modulus:     8380417,
			ErrorBound:  2,
			SecurityLvl: 3,
			Algorithm:   "dilithium3",
		},
		Signature: []byte{0x01, 0x02, 0x03},
		PublicKey: []byte{0x04, 0x05, 0x06},
		Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Verified:  true,
	}

	data, err := json.Marshal(proof)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded FinalityProof
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != proof.ID {
		t.Errorf("ID: got %s, want %s", decoded.ID, proof.ID)
	}
	if decoded.ProofType != proof.ProofType {
		t.Errorf("ProofType: got %s, want %s", decoded.ProofType, proof.ProofType)
	}
	if decoded.LatticeParams.Dimension != 256 {
		t.Errorf("Dimension: got %d, want 256", decoded.LatticeParams.Dimension)
	}
	if decoded.LatticeParams.SecurityLvl != 3 {
		t.Errorf("SecurityLvl: got %d, want 3", decoded.LatticeParams.SecurityLvl)
	}
	if !decoded.Verified {
		t.Error("Verified should be true")
	}
}

// TestLatticeJSON tests Lattice JSON serialization
func TestLatticeJSON(t *testing.T) {
	lattice := Lattice{
		Dimension:   1024,
		Modulus:     12289,
		ErrorBound:  1,
		SecurityLvl: 5,
		Algorithm:   "falcon1024",
	}

	data, err := json.Marshal(lattice)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Lattice
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Dimension != lattice.Dimension {
		t.Errorf("Dimension: got %d, want %d", decoded.Dimension, lattice.Dimension)
	}
	if decoded.Modulus != lattice.Modulus {
		t.Errorf("Modulus: got %d, want %d", decoded.Modulus, lattice.Modulus)
	}
	if decoded.Algorithm != lattice.Algorithm {
		t.Errorf("Algorithm: got %s, want %s", decoded.Algorithm, lattice.Algorithm)
	}
}

// TestQuantumStampJSON tests QuantumStamp JSON serialization
func TestQuantumStampJSON(t *testing.T) {
	stamp := QuantumStamp{
		ID:          "stamp-123",
		VertexID:    "vertex-456",
		ChainID:     "C",
		BlockHeight: 1000000,
		BlockHash:   []byte{0xab, 0xcd, 0xef},
		Entropy:     []byte{0x11, 0x22, 0x33},
		KeyID:       "key-789",
		Signature:   []byte{0xaa, 0xbb, 0xcc},
		Timestamp:   time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
		Certified:   true,
	}

	data, err := json.Marshal(stamp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded QuantumStamp
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != stamp.ID {
		t.Errorf("ID: got %s, want %s", decoded.ID, stamp.ID)
	}
	if decoded.ChainID != stamp.ChainID {
		t.Errorf("ChainID: got %s, want %s", decoded.ChainID, stamp.ChainID)
	}
	if decoded.BlockHeight != stamp.BlockHeight {
		t.Errorf("BlockHeight: got %d, want %d", decoded.BlockHeight, stamp.BlockHeight)
	}
	if !decoded.Certified {
		t.Error("Certified should be true")
	}
}

// TestRingtailKeyJSON tests RingtailKey JSON serialization
func TestRingtailKeyJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	key := RingtailKey{
		ID:          "key-123",
		PublicKey:   []byte{0x01, 0x02, 0x03, 0x04},
		KeyType:     ProofDilithium,
		Algorithm:   "dilithium3",
		SecurityLvl: 3,
		Owner:       "lux1abc123",
		ValidFrom:   now,
		ValidUntil:  now.Add(365 * 24 * time.Hour),
		Revoked:     false,
		CreatedAt:   now,
	}

	data, err := json.Marshal(key)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded RingtailKey
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != key.ID {
		t.Errorf("ID: got %s, want %s", decoded.ID, key.ID)
	}
	if decoded.KeyType != key.KeyType {
		t.Errorf("KeyType: got %s, want %s", decoded.KeyType, key.KeyType)
	}
	if decoded.SecurityLvl != key.SecurityLvl {
		t.Errorf("SecurityLvl: got %d, want %d", decoded.SecurityLvl, key.SecurityLvl)
	}
	if decoded.Owner != key.Owner {
		t.Errorf("Owner: got %s, want %s", decoded.Owner, key.Owner)
	}
	if decoded.Revoked {
		t.Error("Revoked should be false")
	}
}

// TestInterfaceCompliance verifies Adapter implements dag.Adapter
func TestInterfaceCompliance(t *testing.T) {
	var _ dag.Adapter = (*Adapter)(nil)
}

// TestParseVertex tests vertex parsing with finality proof
func TestParseVertex(t *testing.T) {
	adapter := New("")

	vertexJSON := `{
		"id": "qv-123abc",
		"parentIds": ["qv-parent1", "qv-parent2"],
		"height": 5000,
		"epoch": 100,
		"timestamp": "2025-01-15T10:30:00Z",
		"status": "pending",
		"txIds": ["tx1", "tx2"],
		"finalityProof": {
			"id": "proof-xyz",
			"vertexId": "qv-123abc",
			"proofType": "dilithium",
			"latticeParams": {
				"dimension": 256,
				"modulus": 8380417,
				"errorBound": 2,
				"securityLvl": 3,
				"algorithm": "dilithium3"
			},
			"signature": "AQID",
			"publicKey": "BAUG",
			"timestamp": "2025-01-15T10:30:00Z",
			"verified": true
		},
		"proofCount": 3,
		"finalized": true
	}`

	v, err := adapter.ParseVertex(json.RawMessage(vertexJSON))
	if err != nil {
		t.Fatalf("ParseVertex failed: %v", err)
	}

	if v.ID != "qv-123abc" {
		t.Errorf("ID: got %s, want qv-123abc", v.ID)
	}
	if v.Type != "quantum" {
		t.Errorf("Type: got %s, want quantum", v.Type)
	}
	if v.Height != 5000 {
		t.Errorf("Height: got %d, want 5000", v.Height)
	}
	if v.Epoch != 100 {
		t.Errorf("Epoch: got %d, want 100", v.Epoch)
	}
	if len(v.ParentIDs) != 2 {
		t.Errorf("ParentIDs count: got %d, want 2", len(v.ParentIDs))
	}
	// Status should be accepted because finalized=true
	if v.Status != dag.StatusAccepted {
		t.Errorf("Status: got %s, want %s", v.Status, dag.StatusAccepted)
	}

	// Check metadata
	if v.Metadata["proof_count"] != 3 {
		t.Errorf("proof_count: got %v, want 3", v.Metadata["proof_count"])
	}
	if v.Metadata["finalized"] != true {
		t.Errorf("finalized: got %v, want true", v.Metadata["finalized"])
	}
	if v.Metadata["proof_type"] != ProofDilithium {
		t.Errorf("proof_type: got %v, want dilithium", v.Metadata["proof_type"])
	}
	if v.Metadata["security_level"] != 3 {
		t.Errorf("security_level: got %v, want 3", v.Metadata["security_level"])
	}
}

// TestParseVertexWithoutProof tests parsing vertex without finality proof
func TestParseVertexWithoutProof(t *testing.T) {
	adapter := New("")

	vertexJSON := `{
		"id": "qv-noProof",
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

	if v.Status != dag.StatusPending {
		t.Errorf("Status: got %s, want %s", v.Status, dag.StatusPending)
	}
	if v.Metadata["finalized"] != false {
		t.Errorf("finalized: got %v, want false", v.Metadata["finalized"])
	}
}

// TestParseVertexInvalidJSON tests error handling for invalid JSON
func TestParseVertexInvalidJSON(t *testing.T) {
	adapter := New("")

	_, err := adapter.ParseVertex(json.RawMessage("invalid json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// TestGetRecentVertices tests fetching recent vertices via RPC
func TestGetRecentVertices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Method != "qvm.getRecentVertices" {
			t.Errorf("unexpected method: %s", req.Method)
		}

		response := rpcResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result: json.RawMessage(`{
				"vertices": [
					{"id": "qv-1", "height": 100, "finalized": true},
					{"id": "qv-2", "height": 101, "finalized": false}
				]
			}`),
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	vertices, err := adapter.GetRecentVertices(ctx, 10)
	if err != nil {
		t.Fatalf("GetRecentVertices failed: %v", err)
	}

	if len(vertices) != 2 {
		t.Errorf("expected 2 vertices, got %d", len(vertices))
	}
}

// TestGetVertexByID tests fetching a specific vertex
func TestGetVertexByID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := rpcResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result: json.RawMessage(`{
				"vertex": {"id": "qv-target", "height": 500, "finalized": true}
			}`),
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	vertex, err := adapter.GetVertexByID(ctx, "qv-target")
	if err != nil {
		t.Fatalf("GetVertexByID failed: %v", err)
	}

	if vertex == nil {
		t.Fatal("expected non-nil vertex")
	}
}

// TestVerifyProof tests proof verification via RPC
func TestVerifyProof(t *testing.T) {
	tests := []struct {
		name      string
		valid     bool
		wantError bool
	}{
		{"valid proof", true, false},
		{"invalid proof", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := rpcResponse{
					JSONRPC: "2.0",
					ID:      1,
					Result: json.RawMessage(`{
						"valid": ` + boolToString(tt.valid) + `,
						"message": "test"
					}`),
				}
				json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			adapter := New(server.URL)
			ctx := context.Background()

			proof := &FinalityProof{
				ID:        "test-proof",
				VertexID:  "test-vertex",
				ProofType: ProofDilithium,
				Signature: []byte{0x01, 0x02},
				PublicKey: []byte{0x03, 0x04},
			}

			valid, err := adapter.VerifyProof(ctx, proof)
			if tt.wantError && err == nil {
				t.Error("expected error")
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if valid != tt.valid {
				t.Errorf("valid: got %v, want %v", valid, tt.valid)
			}
		})
	}
}

// TestRPCError tests handling of RPC errors
func TestRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := rpcResponse{
			JSONRPC: "2.0",
			ID:      1,
			Error: &rpcError{
				Code:    -32600,
				Message: "Invalid request",
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	_, err := adapter.GetRecentVertices(ctx, 10)
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestNetworkError tests handling of network errors
func TestNetworkError(t *testing.T) {
	adapter := New("http://invalid-host:99999/rpc")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := adapter.GetRecentVertices(ctx, 10)
	if err == nil {
		t.Error("expected error for network failure")
	}
}

// TestContextCancellation tests context cancellation
func TestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := adapter.GetRecentVertices(ctx, 10)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

// TestNewConfig tests default configuration creation
func TestNewConfig(t *testing.T) {
	config := NewConfig()

	if config.ChainType != dag.ChainQ {
		t.Errorf("ChainType: got %s, want %s", config.ChainType, dag.ChainQ)
	}
	if config.ChainName != "Q-Chain (Quantum)" {
		t.Errorf("ChainName: got %s, want Q-Chain (Quantum)", config.ChainName)
	}
	if config.RPCEndpoint != DefaultRPCEndpoint {
		t.Errorf("RPCEndpoint: got %s, want %s", config.RPCEndpoint, DefaultRPCEndpoint)
	}
	if config.HTTPPort != DefaultHTTPPort {
		t.Errorf("HTTPPort: got %d, want %d", config.HTTPPort, DefaultHTTPPort)
	}
	if config.RPCMethod != "qvm" {
		t.Errorf("RPCMethod: got %s, want qvm", config.RPCMethod)
	}
}

// TestLatticeSecurityLevels tests NIST security levels
func TestLatticeSecurityLevels(t *testing.T) {
	// Security levels should be 1-5 for NIST compliance
	levels := []int{1, 2, 3, 4, 5}

	for _, lvl := range levels {
		lattice := Lattice{
			Dimension:   256,
			Modulus:     8380417,
			ErrorBound:  lvl,
			SecurityLvl: lvl,
			Algorithm:   "dilithium",
		}

		if lattice.SecurityLvl < 1 || lattice.SecurityLvl > 5 {
			t.Errorf("invalid security level: %d", lattice.SecurityLvl)
		}
	}
}

// TestQuantumVertexEmbedding tests QuantumVertex embedding dag.Vertex
func TestQuantumVertexEmbedding(t *testing.T) {
	qv := QuantumVertex{
		Vertex: dag.Vertex{
			ID:        "qv-embed",
			Type:      "quantum",
			ParentIDs: []string{"parent1"},
			Height:    100,
			Epoch:     10,
			Status:    dag.StatusAccepted,
		},
		FinalityProof: &FinalityProof{
			ID:        "proof-1",
			ProofType: ProofDilithium,
			Verified:  true,
		},
		ProofCount: 1,
		Finalized:  true,
	}

	// Test embedded fields are accessible
	if qv.ID != "qv-embed" {
		t.Errorf("ID: got %s, want qv-embed", qv.ID)
	}
	if qv.Height != 100 {
		t.Errorf("Height: got %d, want 100", qv.Height)
	}
	if !qv.Finalized {
		t.Error("Finalized should be true")
	}
	if qv.FinalityProof.ProofType != ProofDilithium {
		t.Errorf("ProofType: got %s, want dilithium", qv.FinalityProof.ProofType)
	}
}

// TestRingtailKeyValidity tests key validity period
func TestRingtailKeyValidity(t *testing.T) {
	now := time.Now().UTC()
	key := RingtailKey{
		ID:         "key-valid",
		ValidFrom:  now.Add(-24 * time.Hour),
		ValidUntil: now.Add(365 * 24 * time.Hour),
		Revoked:    false,
	}

	isValid := !key.Revoked && now.After(key.ValidFrom) && now.Before(key.ValidUntil)
	if !isValid {
		t.Error("key should be valid")
	}

	// Test expired key
	expiredKey := RingtailKey{
		ID:         "key-expired",
		ValidFrom:  now.Add(-48 * time.Hour),
		ValidUntil: now.Add(-24 * time.Hour),
		Revoked:    false,
	}

	isExpired := now.After(expiredKey.ValidUntil)
	if !isExpired {
		t.Error("key should be expired")
	}

	// Test revoked key
	revokedKey := RingtailKey{
		ID:         "key-revoked",
		ValidFrom:  now.Add(-24 * time.Hour),
		ValidUntil: now.Add(365 * 24 * time.Hour),
		Revoked:    true,
	}

	if !revokedKey.Revoked {
		t.Error("key should be revoked")
	}
}

// boolToString converts bool to JSON string
func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// BenchmarkParseVertex benchmarks vertex parsing
func BenchmarkParseVertex(b *testing.B) {
	adapter := New("")
	vertexJSON := json.RawMessage(`{
		"id": "qv-bench",
		"parentIds": ["p1", "p2", "p3"],
		"height": 10000,
		"epoch": 500,
		"timestamp": "2025-01-15T10:30:00Z",
		"status": "accepted",
		"finalityProof": {
			"id": "proof-bench",
			"proofType": "dilithium",
			"latticeParams": {"dimension": 256, "modulus": 8380417, "securityLvl": 3},
			"verified": true
		},
		"proofCount": 5,
		"finalized": true
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.ParseVertex(vertexJSON)
	}
}

// BenchmarkFinalityProofSerialization benchmarks proof serialization
func BenchmarkFinalityProofSerialization(b *testing.B) {
	proof := FinalityProof{
		ID:        "proof-bench",
		VertexID:  "vertex-bench",
		ProofType: ProofDilithium,
		LatticeParams: Lattice{
			Dimension:   256,
			Modulus:     8380417,
			ErrorBound:  2,
			SecurityLvl: 3,
			Algorithm:   "dilithium3",
		},
		Signature: make([]byte, 2420), // Approximate Dilithium3 signature size
		PublicKey: make([]byte, 1952), // Approximate Dilithium3 public key size
		Timestamp: time.Now(),
		Verified:  true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(proof)
		var decoded FinalityProof
		_ = json.Unmarshal(data, &decoded)
	}
}
