// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package zk

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

// TestParseVertexInvalidTransfers tests vertex with malformed transfers
func TestParseVertexInvalidTransfers(t *testing.T) {
	adapter := New("")

	vertexJSON := `{
		"id": "zv-bad-tx",
		"parentIds": [],
		"height": 1,
		"epoch": 1,
		"timestamp": 1705323000,
		"status": "pending",
		"merkleRoot": "` + strings.Repeat("a", 64) + `",
		"nullifierRoot": "` + strings.Repeat("b", 64) + `",
		"transfers": "not-an-array"
	}`

	_, err := adapter.ParseVertex(json.RawMessage(vertexJSON))
	if err == nil {
		t.Error("expected error for invalid transfers field")
	}
}

// TestParseVertexNoTransfersField tests vertex without transfers key
func TestParseVertexNoTransfersField(t *testing.T) {
	adapter := New("")

	vertexJSON := `{
		"id": "zv-no-tx",
		"parentIds": [],
		"height": 1,
		"epoch": 1,
		"timestamp": 1705323000,
		"status": "accepted",
		"merkleRoot": "` + strings.Repeat("a", 64) + `",
		"nullifierRoot": "` + strings.Repeat("b", 64) + `"
	}`

	v, err := adapter.ParseVertex(json.RawMessage(vertexJSON))
	if err != nil {
		t.Fatalf("ParseVertex failed: %v", err)
	}
	if len(v.TxIDs) != 0 {
		t.Errorf("expected 0 txIDs, got %d", len(v.TxIDs))
	}
}

// TestParseVertexMultipleTransfers tests extracting multiple tx IDs
func TestParseVertexMultipleTransfers(t *testing.T) {
	adapter := New("")

	vertexJSON := `{
		"id": "zv-multi",
		"parentIds": [],
		"height": 100,
		"epoch": 5,
		"timestamp": 1705323000,
		"status": "accepted",
		"merkleRoot": "` + strings.Repeat("a", 64) + `",
		"nullifierRoot": "` + strings.Repeat("b", 64) + `",
		"proposer": "lux1prop",
		"transfers": [
			{"txId": "tx-1", "type": "shielded_transfer", "fee": 100},
			{"txId": "tx-2", "type": "shield", "fee": 200},
			{"txId": "tx-3", "type": "unshield", "fee": 300}
		]
	}`

	v, err := adapter.ParseVertex(json.RawMessage(vertexJSON))
	if err != nil {
		t.Fatalf("ParseVertex failed: %v", err)
	}
	if len(v.TxIDs) != 3 {
		t.Errorf("expected 3 txIDs, got %d", len(v.TxIDs))
	}
	if v.TxIDs[0] != "tx-1" {
		t.Errorf("first txID: got %s, want tx-1", v.TxIDs[0])
	}
	if v.Metadata["transferCount"] != 3 {
		t.Errorf("transferCount: got %v, want 3", v.Metadata["transferCount"])
	}
}

// TestParseVertexAllStatusValues tests all status mapping paths
func TestParseVertexAllStatusValues(t *testing.T) {
	adapter := New("")
	tests := []struct {
		input  string
		expect dag.Status
	}{
		{"accepted", dag.StatusAccepted},
		{"Accepted", dag.StatusAccepted},
		{"rejected", dag.StatusRejected},
		{"Rejected", dag.StatusRejected},
		{"pending", dag.StatusPending},
		{"Processing", dag.StatusPending},
		{"", dag.StatusPending},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			vertexJSON := `{
				"id": "zv-s",
				"parentIds": [],
				"height": 1,
				"epoch": 1,
				"timestamp": 1705323000,
				"status": "` + tt.input + `",
				"merkleRoot": "` + strings.Repeat("a", 64) + `",
				"nullifierRoot": "` + strings.Repeat("b", 64) + `"
			}`

			v, err := adapter.ParseVertex(json.RawMessage(vertexJSON))
			if err != nil {
				t.Fatalf("ParseVertex failed: %v", err)
			}
			if v.Status != tt.expect {
				t.Errorf("got %s, want %s", v.Status, tt.expect)
			}
		})
	}
}

// TestValidateNullifierHashEdgeCases tests more nullifier hash validations
func TestValidateNullifierHashEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		hash    string
		wantErr bool
	}{
		{"valid lowercase", strings.Repeat("ab", 32), false},
		{"valid uppercase", strings.Repeat("AB", 32), false},
		{"valid digits", strings.Repeat("01", 32), false},
		{"spaces", strings.Repeat(" ", 64), true},
		{"special chars", strings.Repeat("@!", 32), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNullifierHash(tt.hash)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateCommitmentHashEdgeCases tests more commitment hash validations
func TestValidateCommitmentHashEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		hash    string
		wantErr bool
	}{
		{"valid mixed", "aAbBcCdDeEfF00112233445566778899aabbccddeeff00112233445566778899", false},
		{"63 chars", strings.Repeat("a", 63), true},
		{"65 chars", strings.Repeat("a", 65), true},
		{"valid 64 hex", strings.Repeat("0f", 32), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommitmentHash(tt.hash)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestGetNullifierRPCError tests RPC error from GetNullifier
func TestGetNullifierRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := RPCResponse{
			JSONRPC: "2.0",
			ID:      1,
			Error:   &RPCError{Code: -32000, Message: "nullifier not found"},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetNullifier(context.Background(), strings.Repeat("a", 64))
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestGetCommitmentRPCError tests RPC error from GetCommitment
func TestGetCommitmentRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := RPCResponse{
			JSONRPC: "2.0",
			ID:      1,
			Error:   &RPCError{Code: -32000, Message: "commitment not found"},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetCommitment(context.Background(), strings.Repeat("b", 64))
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestGetMerkleRootRPCError tests RPC error from GetMerkleRoot
func TestGetMerkleRootRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := RPCResponse{
			JSONRPC: "2.0",
			ID:      1,
			Error:   &RPCError{Code: -32000, Message: "chain unavailable"},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetMerkleRoot(context.Background())
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestVerifyProofRPCError tests RPC error from VerifyProof
func TestVerifyProofRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := RPCResponse{
			JSONRPC: "2.0",
			ID:      1,
			Error:   &RPCError{Code: -32000, Message: "proof invalid"},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.VerifyProof(context.Background(), ZKProof{Type: ProofGroth16, Data: "0x"})
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestShieldedTransferAllTypes tests all ZK transaction types
func TestShieldedTransferAllTypes(t *testing.T) {
	types := []ZKTransactionType{
		TxShieldedTransfer,
		TxShield,
		TxUnshield,
		TxJoinSplit,
		TxMint,
		TxBurn,
	}

	for _, txType := range types {
		t.Run(string(txType), func(t *testing.T) {
			transfer := ShieldedTransfer{
				TxID:      "tx-" + string(txType),
				Type:      txType,
				Proof:     ZKProof{Type: ProofGroth16, Data: "0xproof"},
				Fee:       100,
				Timestamp: time.Now(),
			}

			data, err := json.Marshal(transfer)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var decoded ShieldedTransfer
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if decoded.Type != txType {
				t.Errorf("Type: got %s, want %s", decoded.Type, txType)
			}
		})
	}
}

// TestRPCRequestMethodFormat tests RPC method format uses zvm prefix
func TestRPCRequestMethodFormat(t *testing.T) {
	var capturedMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		capturedMethod = req.Method
		response := RPCResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result:  json.RawMessage(`[]`),
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	adapter.GetRecentVertices(context.Background(), 10)

	if capturedMethod != "zvm.getRecentVertices" {
		t.Errorf("expected method zvm.getRecentVertices, got %s", capturedMethod)
	}
}
