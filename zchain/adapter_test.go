// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package zchain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestConstants tests Z-Chain specific constants
func TestConstants(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  interface{}
	}{
		{"DefaultRPCEndpoint", DefaultRPCEndpoint, "http://localhost:9650/ext/bc/Z/rpc"},
		{"DefaultHTTPPort", DefaultHTTPPort, 4400},
		{"DefaultDatabase", DefaultDatabase, "explorer_zchain"},
		{"RPCMethod", RPCMethod, "zvm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.want {
				t.Errorf("got %v, want %v", tt.value, tt.want)
			}
		})
	}
}

// TestZKTransactionTypes tests transaction type constants
func TestZKTransactionTypes(t *testing.T) {
	types := []ZKTransactionType{
		TxShieldedTransfer,
		TxShield,
		TxUnshield,
		TxJoinSplit,
		TxMint,
		TxBurn,
	}
	expected := []string{"shielded_transfer", "shield", "unshield", "joinsplit", "mint", "burn"}

	for i, txType := range types {
		if string(txType) != expected[i] {
			t.Errorf("tx type %d: got %s, want %s", i, txType, expected[i])
		}
	}
}

// TestProofTypes tests proof type constants
func TestProofTypes(t *testing.T) {
	proofTypes := []ProofType{
		ProofGroth16,
		ProofPlonk,
		ProofSTARK,
		ProofBullet,
		ProofHalo2,
		ProofFRI,
	}
	expected := []string{"groth16", "plonk", "stark", "bulletproof", "halo2", "fri"}

	for i, pt := range proofTypes {
		if string(pt) != expected[i] {
			t.Errorf("proof type %d: got %s, want %s", i, pt, expected[i])
		}
	}
}

// TestZKProofJSON tests ZKProof JSON serialization
func TestZKProofJSON(t *testing.T) {
	proof := ZKProof{
		Type:         ProofGroth16,
		Data:         "0xabcdef1234567890",
		PublicInputs: []string{"0x111", "0x222", "0x333"},
		VerifyingKey: "0xvk123456",
	}

	data, err := json.Marshal(proof)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ZKProof
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Type != ProofGroth16 {
		t.Errorf("Type: got %s, want groth16", decoded.Type)
	}
	if decoded.Data != proof.Data {
		t.Errorf("Data: got %s, want %s", decoded.Data, proof.Data)
	}
	if len(decoded.PublicInputs) != 3 {
		t.Errorf("PublicInputs count: got %d, want 3", len(decoded.PublicInputs))
	}
	if decoded.VerifyingKey != proof.VerifyingKey {
		t.Errorf("VerifyingKey: got %s, want %s", decoded.VerifyingKey, proof.VerifyingKey)
	}
}

// TestNullifierJSON tests Nullifier JSON serialization
func TestNullifierJSON(t *testing.T) {
	nullifier := Nullifier{
		Hash:    "abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
		TxID:    "tx-123",
		Index:   0,
		SpentAt: 1000,
	}

	data, err := json.Marshal(nullifier)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Nullifier
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Hash != nullifier.Hash {
		t.Errorf("Hash: got %s, want %s", decoded.Hash, nullifier.Hash)
	}
	if decoded.TxID != nullifier.TxID {
		t.Errorf("TxID: got %s, want %s", decoded.TxID, nullifier.TxID)
	}
	if decoded.SpentAt != 1000 {
		t.Errorf("SpentAt: got %d, want 1000", decoded.SpentAt)
	}
}

// TestCommitmentJSON tests Commitment JSON serialization
func TestCommitmentJSON(t *testing.T) {
	commitment := Commitment{
		Hash:      "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		TxID:      "tx-456",
		Index:     1,
		CreatedAt: 500,
		Spent:     false,
	}

	data, err := json.Marshal(commitment)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Commitment
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Hash != commitment.Hash {
		t.Errorf("Hash: got %s, want %s", decoded.Hash, commitment.Hash)
	}
	if decoded.Spent {
		t.Error("Spent should be false")
	}
}

// TestShieldedTransferJSON tests ShieldedTransfer JSON serialization
func TestShieldedTransferJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	transfer := ShieldedTransfer{
		TxID: "tx-shielded-123",
		Type: TxShieldedTransfer,
		Proof: ZKProof{
			Type:         ProofGroth16,
			Data:         "0xproof",
			PublicInputs: []string{"0x1"},
		},
		Nullifiers: []Nullifier{
			{Hash: strings.Repeat("a", 64), TxID: "tx-shielded-123", Index: 0},
		},
		Commitments: []Commitment{
			{Hash: strings.Repeat("b", 64), TxID: "tx-shielded-123", Index: 0},
			{Hash: strings.Repeat("c", 64), TxID: "tx-shielded-123", Index: 1},
		},
		ValueBalance: 0,
		Fee:          1000,
		Memo:         "encrypted-memo",
		Timestamp:    now,
	}

	data, err := json.Marshal(transfer)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ShieldedTransfer
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.TxID != transfer.TxID {
		t.Errorf("TxID: got %s, want %s", decoded.TxID, transfer.TxID)
	}
	if decoded.Type != TxShieldedTransfer {
		t.Errorf("Type: got %s, want shielded_transfer", decoded.Type)
	}
	if len(decoded.Nullifiers) != 1 {
		t.Errorf("Nullifiers count: got %d, want 1", len(decoded.Nullifiers))
	}
	if len(decoded.Commitments) != 2 {
		t.Errorf("Commitments count: got %d, want 2", len(decoded.Commitments))
	}
	if decoded.Fee != 1000 {
		t.Errorf("Fee: got %d, want 1000", decoded.Fee)
	}
}

// TestShieldUnshieldTransfers tests shield/unshield transactions with value balance
func TestShieldUnshieldTransfers(t *testing.T) {
	tests := []struct {
		name         string
		txType       ZKTransactionType
		valueBalance int64
	}{
		{"shield (deposit)", TxShield, 1000000},    // Positive: transparent -> shielded
		{"unshield (withdraw)", TxUnshield, -500000}, // Negative: shielded -> transparent
		{"shielded transfer", TxShieldedTransfer, 0}, // Zero: fully shielded
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transfer := ShieldedTransfer{
				TxID:         "tx-" + string(tt.txType),
				Type:         tt.txType,
				ValueBalance: tt.valueBalance,
				Fee:          100,
				Timestamp:    time.Now(),
			}

			if transfer.ValueBalance != tt.valueBalance {
				t.Errorf("ValueBalance: got %d, want %d", transfer.ValueBalance, tt.valueBalance)
			}
		})
	}
}

// TestVertexDataJSON tests VertexData JSON serialization
func TestVertexDataJSON(t *testing.T) {
	vd := VertexData{
		Transfers: []ShieldedTransfer{
			{TxID: "tx-1", Type: TxShieldedTransfer},
			{TxID: "tx-2", Type: TxShield},
		},
		MerkleRoot:    strings.Repeat("m", 64),
		NullifierRoot: strings.Repeat("n", 64),
		Epoch:         100,
		Proposer:      "lux1abc...",
	}

	data, err := json.Marshal(vd)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded VertexData
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(decoded.Transfers) != 2 {
		t.Errorf("Transfers count: got %d, want 2", len(decoded.Transfers))
	}
	if decoded.Epoch != 100 {
		t.Errorf("Epoch: got %d, want 100", decoded.Epoch)
	}
}

// TestInterfaceCompliance verifies Adapter implements dag.Adapter
func TestInterfaceCompliance(t *testing.T) {
	// This is a compile-time check
	var _ dag.Adapter = (*Adapter)(nil)
}

// TestParseVertex tests vertex parsing with transfers
func TestParseVertex(t *testing.T) {
	adapter := New("")

	vertexJSON := `{
		"id": "zv-123abc",
		"parentIds": ["zv-parent1", "zv-parent2"],
		"height": 5000,
		"epoch": 100,
		"timestamp": 1705323000,
		"status": "accepted",
		"merkleRoot": "` + strings.Repeat("a", 64) + `",
		"nullifierRoot": "` + strings.Repeat("b", 64) + `",
		"proposer": "lux1proposer",
		"transfers": [
			{"txId": "tx-1", "type": "shielded_transfer", "fee": 100}
		]
	}`

	v, err := adapter.ParseVertex(json.RawMessage(vertexJSON))
	if err != nil {
		t.Fatalf("ParseVertex failed: %v", err)
	}

	if v.ID != "zv-123abc" {
		t.Errorf("ID: got %s, want zv-123abc", v.ID)
	}
	if v.Type != "zk_vertex" {
		t.Errorf("Type: got %s, want zk_vertex", v.Type)
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
	if v.Status != dag.StatusAccepted {
		t.Errorf("Status: got %s, want accepted", v.Status)
	}
	if len(v.TxIDs) != 1 {
		t.Errorf("TxIDs count: got %d, want 1", len(v.TxIDs))
	}

	// Check metadata
	if v.Metadata["transferCount"] != 1 {
		t.Errorf("transferCount: got %v, want 1", v.Metadata["transferCount"])
	}
}

// TestParseVertexStatusMapping tests status mapping
func TestParseVertexStatusMapping(t *testing.T) {
	adapter := New("")

	tests := []struct {
		inputStatus  string
		wantStatus   dag.Status
	}{
		{"accepted", dag.StatusAccepted},
		{"Accepted", dag.StatusAccepted},
		{"rejected", dag.StatusRejected},
		{"Rejected", dag.StatusRejected},
		{"pending", dag.StatusPending},
		{"unknown", dag.StatusPending},
	}

	for _, tt := range tests {
		t.Run(tt.inputStatus, func(t *testing.T) {
			vertexJSON := `{
				"id": "zv-status",
				"parentIds": [],
				"height": 1,
				"epoch": 1,
				"timestamp": 1705323000,
				"status": "` + tt.inputStatus + `",
				"merkleRoot": "` + strings.Repeat("a", 64) + `",
				"nullifierRoot": "` + strings.Repeat("b", 64) + `"
			}`

			v, err := adapter.ParseVertex(json.RawMessage(vertexJSON))
			if err != nil {
				t.Fatalf("ParseVertex failed: %v", err)
			}

			if v.Status != tt.wantStatus {
				t.Errorf("Status: got %s, want %s", v.Status, tt.wantStatus)
			}
		})
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

// TestParseVertexEmptyTransfers tests parsing with empty transfers
func TestParseVertexEmptyTransfers(t *testing.T) {
	adapter := New("")

	vertexJSON := `{
		"id": "zv-empty",
		"parentIds": [],
		"height": 1,
		"epoch": 1,
		"timestamp": 1705323000,
		"status": "pending",
		"merkleRoot": "` + strings.Repeat("a", 64) + `",
		"nullifierRoot": "` + strings.Repeat("b", 64) + `",
		"transfers": []
	}`

	v, err := adapter.ParseVertex(json.RawMessage(vertexJSON))
	if err != nil {
		t.Fatalf("ParseVertex failed: %v", err)
	}

	if len(v.TxIDs) != 0 {
		t.Errorf("TxIDs should be empty, got %d", len(v.TxIDs))
	}
}

// TestGetRecentVertices tests fetching recent vertices
func TestGetRecentVertices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Method != "zvm.getRecentVertices" {
			t.Errorf("unexpected method: %s", req.Method)
		}

		response := RPCResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result:  json.RawMessage(`[{"id": "zv-1"}, {"id": "zv-2"}]`),
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
		response := RPCResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result:  json.RawMessage(`{"id": "zv-target", "height": 500}`),
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	vertex, err := adapter.GetVertexByID(ctx, "zv-target")
	if err != nil {
		t.Fatalf("GetVertexByID failed: %v", err)
	}

	if vertex == nil {
		t.Fatal("expected non-nil vertex")
	}
}

// TestGetNullifier tests fetching nullifier status
func TestGetNullifier(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := RPCResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result:  json.RawMessage(`{"hash": "` + strings.Repeat("a", 64) + `", "txId": "tx-1", "index": 0, "spentAt": 1000}`),
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	nullifier, err := adapter.GetNullifier(ctx, strings.Repeat("a", 64))
	if err != nil {
		t.Fatalf("GetNullifier failed: %v", err)
	}

	if nullifier.SpentAt != 1000 {
		t.Errorf("SpentAt: got %d, want 1000", nullifier.SpentAt)
	}
}

// TestGetCommitment tests fetching commitment status
func TestGetCommitment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := RPCResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result:  json.RawMessage(`{"hash": "` + strings.Repeat("b", 64) + `", "txId": "tx-2", "index": 1, "spent": false}`),
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	commitment, err := adapter.GetCommitment(ctx, strings.Repeat("b", 64))
	if err != nil {
		t.Fatalf("GetCommitment failed: %v", err)
	}

	if commitment.Spent {
		t.Error("Spent should be false")
	}
}

// TestGetMerkleRoot tests fetching merkle root
func TestGetMerkleRoot(t *testing.T) {
	expectedRoot := strings.Repeat("c", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := RPCResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result:  json.RawMessage(`"` + expectedRoot + `"`),
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	root, err := adapter.GetMerkleRoot(ctx)
	if err != nil {
		t.Fatalf("GetMerkleRoot failed: %v", err)
	}

	if root != expectedRoot {
		t.Errorf("root: got %s, want %s", root, expectedRoot)
	}
}

// TestVerifyProof tests proof verification
func TestVerifyProof(t *testing.T) {
	tests := []struct {
		name      string
		valid     bool
	}{
		{"valid proof", true},
		{"invalid proof", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				result := "true"
				if !tt.valid {
					result = "false"
				}
				response := RPCResponse{
					JSONRPC: "2.0",
					ID:      1,
					Result:  json.RawMessage(result),
				}
				json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			adapter := New(server.URL)
			ctx := context.Background()

			proof := ZKProof{
				Type: ProofGroth16,
				Data: "0xproof",
			}

			valid, err := adapter.VerifyProof(ctx, proof)
			if err != nil {
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
		response := RPCResponse{
			JSONRPC: "2.0",
			ID:      1,
			Error: &RPCError{
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

// TestDefaultConfig tests default configuration
func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.ChainType != dag.ChainZ {
		t.Errorf("ChainType: got %s, want %s", config.ChainType, dag.ChainZ)
	}
	if config.ChainName != "Z-Chain (Privacy)" {
		t.Errorf("ChainName: got %s, want Z-Chain (Privacy)", config.ChainName)
	}
	if config.RPCEndpoint != DefaultRPCEndpoint {
		t.Errorf("RPCEndpoint: got %s, want %s", config.RPCEndpoint, DefaultRPCEndpoint)
	}
	if config.HTTPPort != DefaultHTTPPort {
		t.Errorf("HTTPPort: got %d, want %d", config.HTTPPort, DefaultHTTPPort)
	}
	if config.RPCMethod != RPCMethod {
		t.Errorf("RPCMethod: got %s, want %s", config.RPCMethod, RPCMethod)
	}
}

// TestValidateNullifierHash tests nullifier hash validation
func TestValidateNullifierHash(t *testing.T) {
	tests := []struct {
		name    string
		hash    string
		wantErr bool
	}{
		{"valid hash", strings.Repeat("a", 64), false},
		{"valid mixed case", strings.Repeat("aB", 32), false},
		{"too short", strings.Repeat("a", 63), true},
		{"too long", strings.Repeat("a", 65), true},
		{"empty", "", true},
		{"invalid hex", strings.Repeat("g", 64), true},
		{"with 0x prefix", "0x" + strings.Repeat("a", 64), true}, // Should fail - 66 chars
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNullifierHash(tt.hash)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateNullifierHash(%s) error = %v, wantErr %v", tt.hash, err, tt.wantErr)
			}
		})
	}
}

// TestValidateCommitmentHash tests commitment hash validation
func TestValidateCommitmentHash(t *testing.T) {
	tests := []struct {
		name    string
		hash    string
		wantErr bool
	}{
		{"valid hash", strings.Repeat("b", 64), false},
		{"valid numeric", strings.Repeat("12", 32), false},
		{"too short", strings.Repeat("b", 32), true},
		{"too long", strings.Repeat("b", 128), true},
		{"empty", "", true},
		{"invalid hex", "xyz" + strings.Repeat("0", 61), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommitmentHash(tt.hash)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommitmentHash(%s) error = %v, wantErr %v", tt.hash, err, tt.wantErr)
			}
		})
	}
}

// TestRPCRequestResponse tests RPC request/response serialization
func TestRPCRequestResponse(t *testing.T) {
	req := RPCRequest{
		JSONRPC: "2.0",
		ID:      42,
		Method:  "zvm.test",
		Params:  []interface{}{"param1", 123},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	var decoded RPCRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	if decoded.Method != "zvm.test" {
		t.Errorf("Method: got %s, want zvm.test", decoded.Method)
	}
	if decoded.ID != 42 {
		t.Errorf("ID: got %d, want 42", decoded.ID)
	}
}

// BenchmarkParseVertex benchmarks vertex parsing
func BenchmarkParseVertex(b *testing.B) {
	adapter := New("")
	vertexJSON := json.RawMessage(`{
		"id": "zv-bench",
		"parentIds": ["p1", "p2", "p3"],
		"height": 10000,
		"epoch": 500,
		"timestamp": 1705323000,
		"status": "accepted",
		"merkleRoot": "` + strings.Repeat("a", 64) + `",
		"nullifierRoot": "` + strings.Repeat("b", 64) + `",
		"transfers": [
			{"txId": "tx-1", "type": "shielded_transfer", "fee": 100},
			{"txId": "tx-2", "type": "shield", "fee": 200, "valueBalance": 1000000}
		]
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.ParseVertex(vertexJSON)
	}
}

// BenchmarkValidateNullifierHash benchmarks hash validation
func BenchmarkValidateNullifierHash(b *testing.B) {
	hash := strings.Repeat("a", 64)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateNullifierHash(hash)
	}
}

// BenchmarkShieldedTransferSerialization benchmarks transfer serialization
func BenchmarkShieldedTransferSerialization(b *testing.B) {
	transfer := ShieldedTransfer{
		TxID: "tx-bench",
		Type: TxShieldedTransfer,
		Proof: ZKProof{
			Type:         ProofGroth16,
			Data:         strings.Repeat("a", 512),
			PublicInputs: []string{"0x1", "0x2", "0x3"},
		},
		Nullifiers: []Nullifier{
			{Hash: strings.Repeat("a", 64), TxID: "tx-bench", Index: 0},
			{Hash: strings.Repeat("b", 64), TxID: "tx-bench", Index: 1},
		},
		Commitments: []Commitment{
			{Hash: strings.Repeat("c", 64), TxID: "tx-bench", Index: 0},
			{Hash: strings.Repeat("d", 64), TxID: "tx-bench", Index: 1},
		},
		Fee:       1000,
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(transfer)
		var decoded ShieldedTransfer
		_ = json.Unmarshal(data, &decoded)
	}
}
