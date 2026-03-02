// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package key

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestParseVertexNullData tests vertex with null data field
func TestParseVertexNullData(t *testing.T) {
	adapter := New("http://localhost:9650")

	input := json.RawMessage(`{
		"id": "vtx-null-data",
		"parentIds": [],
		"height": 1,
		"timestamp": 1700000000,
		"status": "pending",
		"data": null
	}`)

	v, err := adapter.ParseVertex(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// null data unmarshals to empty vertexData, which yields "generic"
	if v.Type != "generic" {
		t.Errorf("Type = %q, want generic", v.Type)
	}
}

// TestParseVertexPreservesParentIDs tests parent IDs are preserved
func TestParseVertexPreservesParentIDs(t *testing.T) {
	adapter := New("http://localhost:9650")

	input := json.RawMessage(`{
		"id": "vtx-parents",
		"parentIds": ["p1", "p2", "p3", "p4"],
		"height": 50,
		"timestamp": 1700000000,
		"status": "accepted",
		"data": {}
	}`)

	v, err := adapter.ParseVertex(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v.ParentIDs) != 4 {
		t.Errorf("ParentIDs count = %d, want 4", len(v.ParentIDs))
	}
}

// TestInferVertexTypePriority tests that keys take priority over operations
func TestInferVertexTypePriority(t *testing.T) {
	adapter := New("http://localhost:9650")

	// When both keys and operations present, keys should win
	data := json.RawMessage(`{
		"keys": [{"id": "k1"}],
		"keyOperations": [{"id": "op1"}]
	}`)

	result := adapter.inferVertexType(data)
	if result != "keys" {
		t.Errorf("inferVertexType = %q, want keys (keys should take priority)", result)
	}
}

// TestGetRecentVerticesRPCError tests RPC error response
func TestGetRecentVerticesRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"error":   map[string]interface{}{"code": -32000, "message": "backend error"},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetRecentVertices(context.Background(), 5)
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestGetRecentVerticesInvalidJSON tests invalid JSON response
func TestGetRecentVerticesInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetRecentVertices(context.Background(), 5)
	if err == nil {
		t.Error("expected error for invalid JSON response")
	}
}

// TestListKeysEmpty tests ListKeys returning empty
func TestListKeysEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  []interface{}{},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	keys, err := adapter.ListKeys(context.Background(), "", "", 0, 10)
	if err != nil {
		t.Fatalf("ListKeys failed: %v", err)
	}
	if keys != nil {
		t.Errorf("expected nil keys for empty result, got %v", keys)
	}
}

// TestListAlgorithmsEmpty tests ListAlgorithms returning empty
func TestListAlgorithmsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  []interface{}{},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	algs, err := adapter.ListAlgorithms(context.Background())
	if err != nil {
		t.Fatalf("ListAlgorithms failed: %v", err)
	}
	if algs != nil {
		t.Errorf("expected nil algorithms for empty result, got %v", algs)
	}
}

// TestListKeysRPCError tests RPC error from ListKeys
func TestListKeysRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"error":   map[string]interface{}{"code": -32000, "message": "unauthorized"},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.ListKeys(context.Background(), "", "", 0, 10)
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestKeyAllStatuses tests all key statuses
func TestKeyAllStatuses(t *testing.T) {
	statuses := []KeyStatus{
		KeyStatusActive,
		KeyStatusInactive,
		KeyStatusRotating,
		KeyStatusRevoked,
		KeyStatusPending,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			key := Key{
				ID:        "key-" + string(status),
				Name:      "test",
				Algorithm: "ml-kem-768",
				KeyType:   "encapsulation",
				Status:    status,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			data, err := json.Marshal(key)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var decoded Key
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if decoded.Status != status {
				t.Errorf("Status: got %s, want %s", decoded.Status, status)
			}
		})
	}
}

// TestKeyOperationFailure tests failed key operation
func TestKeyOperationFailure(t *testing.T) {
	op := KeyOperation{
		ID:           "op-fail",
		KeyID:        "key1",
		Operation:    "rotate",
		Initiator:    "0xadmin",
		Participants: []string{"p1", "p2"},
		Success:      false,
		Error:        "threshold not met",
		Timestamp:    time.Now(),
	}

	data, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded KeyOperation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Success {
		t.Error("Success should be false")
	}
	if decoded.Error != "threshold not met" {
		t.Errorf("Error: got %s, want 'threshold not met'", decoded.Error)
	}
}

// TestEncryptionRequestLargeData tests encryption request with large data size
func TestEncryptionRequestLargeData(t *testing.T) {
	req := EncryptionRequest{
		ID:        "enc-large",
		KeyID:     "key1",
		Requester: "0xuser",
		DataSize:  1024 * 1024 * 100, // 100MB
		Success:   true,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded EncryptionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.DataSize != 1024*1024*100 {
		t.Errorf("DataSize: got %d, want %d", decoded.DataSize, 1024*1024*100)
	}
}

// TestSignatureRequestFailure tests failed signature request
func TestSignatureRequestFailure(t *testing.T) {
	req := SignatureRequest{
		ID:          "sig-fail",
		KeyID:       "key1",
		Signers:     []string{"s1"},
		MessageHash: "0xhash",
		Algorithm:   "ml-dsa-65",
		Success:     false,
		Timestamp:   time.Now(),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SignatureRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Success {
		t.Error("Success should be false")
	}
}

// TestAlgorithmPostQuantumFlag tests post-quantum and classical algorithms
func TestAlgorithmPostQuantumFlag(t *testing.T) {
	tests := []struct {
		name        string
		postQuantum bool
	}{
		{"ml-kem-768", true},
		{"secp256k1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alg := Algorithm{
				Name:        tt.name,
				PostQuantum: tt.postQuantum,
			}

			if alg.PostQuantum != tt.postQuantum {
				t.Errorf("PostQuantum: got %v, want %v", alg.PostQuantum, tt.postQuantum)
			}
		})
	}
}

// TestCallSingleResult tests call returning single (non-array) result
func TestCallSingleResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"id":   "key-single",
				"name": "test-key",
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	key, err := adapter.GetKeyByID(context.Background(), "key-single")
	if err != nil {
		t.Fatalf("GetKeyByID failed: %v", err)
	}

	if key.ID != "key-single" {
		t.Errorf("ID = %q, want key-single", key.ID)
	}
}

// TestKeyEmptyTags tests key with empty tags
func TestKeyEmptyTags(t *testing.T) {
	key := Key{
		ID:        "key-no-tags",
		Name:      "test",
		Algorithm: "secp256k1",
		KeyType:   "signing",
		Tags:      nil,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	data, err := json.Marshal(key)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Key
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Tags != nil {
		t.Errorf("expected nil tags, got %v", decoded.Tags)
	}
}

// TestGetKeyByIDNetworkError tests network error from GetKeyByID
func TestGetKeyByIDNetworkError(t *testing.T) {
	adapter := New("http://localhost:99999/invalid")
	_, err := adapter.GetKeyByID(context.Background(), "key1")
	if err == nil {
		t.Error("expected network error")
	}
}

// TestListAlgorithmsNetworkError tests network error from ListAlgorithms
func TestListAlgorithmsNetworkError(t *testing.T) {
	adapter := New("http://localhost:99999/invalid")
	_, err := adapter.ListAlgorithms(context.Background())
	if err == nil {
		t.Error("expected network error")
	}
}
