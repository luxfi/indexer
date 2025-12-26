// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package kchain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/luxfi/indexer/dag"
)

// TestNew tests the adapter constructor
func TestNew(t *testing.T) {
	endpoint := "http://localhost:9650/ext/bc/K/rpc"
	adapter := New(endpoint)

	if adapter == nil {
		t.Fatal("New returned nil adapter")
	}

	if adapter.rpcEndpoint != endpoint {
		t.Errorf("rpcEndpoint = %q, want %q", adapter.rpcEndpoint, endpoint)
	}

	if adapter.httpClient == nil {
		t.Error("httpClient should not be nil")
	}

	if adapter.httpClient.Timeout != 30*time.Second {
		t.Errorf("httpClient.Timeout = %v, want 30s", adapter.httpClient.Timeout)
	}
}

// TestConstants tests the package constants
func TestConstants(t *testing.T) {
	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"DefaultPort", DefaultPort, 4900},
		{"DefaultDatabase", DefaultDatabase, "explorer_kchain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestKeyStatusConstants tests key status constants
func TestKeyStatusConstants(t *testing.T) {
	tests := []struct {
		name string
		got  KeyStatus
		want string
	}{
		{"KeyStatusActive", KeyStatusActive, "active"},
		{"KeyStatusInactive", KeyStatusInactive, "inactive"},
		{"KeyStatusRotating", KeyStatusRotating, "rotating"},
		{"KeyStatusRevoked", KeyStatusRevoked, "revoked"},
		{"KeyStatusPending", KeyStatusPending, "pending"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.got) != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestAlgorithmTypeConstants tests algorithm type constants
func TestAlgorithmTypeConstants(t *testing.T) {
	tests := []struct {
		name string
		got  AlgorithmType
		want string
	}{
		{"AlgorithmTypeKeyExchange", AlgorithmTypeKeyExchange, "key-exchange"},
		{"AlgorithmTypeSigning", AlgorithmTypeSigning, "signing"},
		{"AlgorithmTypeEncryption", AlgorithmTypeEncryption, "encryption"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.got) != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestAdapterImplementsInterface verifies Adapter implements dag.Adapter
func TestAdapterImplementsInterface(t *testing.T) {
	var _ dag.Adapter = (*Adapter)(nil)
}

// TestParseVertex tests parsing K-Chain vertex data
func TestParseVertex(t *testing.T) {
	adapter := New("http://localhost:9650")

	tests := []struct {
		name     string
		input    json.RawMessage
		wantID   string
		wantType string
		wantErr  bool
	}{
		{
			name: "valid keys vertex",
			input: json.RawMessage(`{
				"id": "vertex123",
				"parentIds": ["parent1"],
				"height": 100,
				"timestamp": 1700000000000000000,
				"status": "accepted",
				"data": {
					"keys": [{"id": "key1", "algorithm": "ml-kem-768"}]
				}
			}`),
			wantID:   "vertex123",
			wantType: "keys",
			wantErr:  false,
		},
		{
			name: "valid key operations vertex",
			input: json.RawMessage(`{
				"id": "vertex456",
				"parentIds": [],
				"height": 50,
				"timestamp": 1700000000000000000,
				"status": "pending",
				"data": {
					"keyOperations": [{"id": "op1", "operation": "create"}]
				}
			}`),
			wantID:   "vertex456",
			wantType: "key_operations",
			wantErr:  false,
		},
		{
			name: "valid encryption vertex",
			input: json.RawMessage(`{
				"id": "vertex789",
				"parentIds": ["p1"],
				"height": 25,
				"timestamp": 1700000000000000000,
				"status": "accepted",
				"data": {
					"encryptionRequests": [{"id": "enc1", "keyId": "k1"}]
				}
			}`),
			wantID:   "vertex789",
			wantType: "encryption",
			wantErr:  false,
		},
		{
			name: "valid signatures vertex",
			input: json.RawMessage(`{
				"id": "vertexabc",
				"parentIds": [],
				"height": 10,
				"timestamp": 1700000000000000000,
				"status": "accepted",
				"data": {
					"signatureRequests": [{"id": "sig1", "keyId": "k1"}]
				}
			}`),
			wantID:   "vertexabc",
			wantType: "signatures",
			wantErr:  false,
		},
		{
			name: "empty data vertex",
			input: json.RawMessage(`{
				"id": "empty",
				"parentIds": [],
				"height": 1,
				"timestamp": 1700000000000000000,
				"status": "accepted",
				"data": {}
			}`),
			wantID:   "empty",
			wantType: "generic",
			wantErr:  false,
		},
		{
			name:    "invalid JSON",
			input:   json.RawMessage(`{invalid`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vertex, err := adapter.ParseVertex(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if vertex.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", vertex.ID, tt.wantID)
			}
			if vertex.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", vertex.Type, tt.wantType)
			}
		})
	}
}

// TestGetRecentVertices tests fetching recent vertices
func TestGetRecentVertices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		method := req["method"].(string)
		if method != "kvm.getRecentVertices" {
			t.Errorf("expected method kvm.getRecentVertices, got %s", method)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": []map[string]interface{}{
				{"id": "v1", "height": 100},
				{"id": "v2", "height": 99},
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	vertices, err := adapter.GetRecentVertices(ctx, 5)
	if err != nil {
		t.Fatalf("GetRecentVertices failed: %v", err)
	}

	if len(vertices) != 2 {
		t.Errorf("got %d vertices, want 2", len(vertices))
	}
}

// TestGetVertexByID tests fetching a vertex by ID
func TestGetVertexByID(t *testing.T) {
	vertexID := "vertex123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		method := req["method"].(string)
		if method != "kvm.getVertex" {
			t.Errorf("expected method kvm.getVertex, got %s", method)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"id":     vertexID,
				"height": 100,
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	result, err := adapter.GetVertexByID(ctx, vertexID)
	if err != nil {
		t.Fatalf("GetVertexByID failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestListKeys tests listing keys
func TestListKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["method"] != "kvm.listKeys" {
			t.Errorf("expected method kvm.listKeys, got %v", req["method"])
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"keys": []map[string]interface{}{
					{"id": "key1", "name": "my-key", "algorithm": "ml-kem-768", "status": "active"},
					{"id": "key2", "name": "my-key2", "algorithm": "ml-dsa-65", "status": "active"},
				},
				"total": 2,
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	keys, err := adapter.ListKeys(ctx, "", "", 0, 10)
	if err != nil {
		t.Fatalf("ListKeys failed: %v", err)
	}

	if len(keys) != 2 {
		t.Errorf("got %d keys, want 2", len(keys))
	}

	if keys[0].Algorithm != "ml-kem-768" {
		t.Errorf("first key algorithm = %q, want ml-kem-768", keys[0].Algorithm)
	}
}

// TestGetKeyByID tests fetching a key by ID
func TestGetKeyByID(t *testing.T) {
	keyID := "key123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["method"] != "kvm.getKeyByID" {
			t.Errorf("expected method kvm.getKeyByID, got %v", req["method"])
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"id":        keyID,
				"name":      "my-key",
				"algorithm": "ml-kem-768",
				"status":    "active",
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	key, err := adapter.GetKeyByID(ctx, keyID)
	if err != nil {
		t.Fatalf("GetKeyByID failed: %v", err)
	}

	if key.ID != keyID {
		t.Errorf("ID = %q, want %q", key.ID, keyID)
	}
	if key.Algorithm != "ml-kem-768" {
		t.Errorf("Algorithm = %q, want ml-kem-768", key.Algorithm)
	}
}

// TestListAlgorithms tests listing supported algorithms
func TestListAlgorithms(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["method"] != "kvm.listAlgorithms" {
			t.Errorf("expected method kvm.listAlgorithms, got %v", req["method"])
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"algorithms": []map[string]interface{}{
					{
						"name":             "ml-kem-768",
						"type":             "key-exchange",
						"securityLevel":    192,
						"postQuantum":      true,
						"thresholdSupport": false,
					},
					{
						"name":             "frost-secp256k1",
						"type":             "signing",
						"securityLevel":    128,
						"postQuantum":      false,
						"thresholdSupport": true,
					},
				},
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	algorithms, err := adapter.ListAlgorithms(ctx)
	if err != nil {
		t.Fatalf("ListAlgorithms failed: %v", err)
	}

	if len(algorithms) != 2 {
		t.Errorf("got %d algorithms, want 2", len(algorithms))
	}

	if algorithms[0].Name != "ml-kem-768" {
		t.Errorf("first algorithm name = %q, want ml-kem-768", algorithms[0].Name)
	}
	if !algorithms[0].PostQuantum {
		t.Error("ml-kem-768 should be PostQuantum")
	}
	if !algorithms[1].ThresholdSupport {
		t.Error("frost-secp256k1 should have ThresholdSupport")
	}
}

// TestKeyStruct tests Key JSON serialization
func TestKeyStruct(t *testing.T) {
	key := Key{
		ID:          "key123",
		Name:        "my-key",
		Algorithm:   "ml-kem-768",
		KeyType:     "encapsulation",
		PublicKey:   "0xpublic",
		Threshold:   3,
		TotalShares: 5,
		Status:      KeyStatusActive,
		Tags:        []string{"production", "pq"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	data, err := json.Marshal(key)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Key
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != key.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, key.ID)
	}
	if decoded.Algorithm != key.Algorithm {
		t.Errorf("Algorithm = %q, want %q", decoded.Algorithm, key.Algorithm)
	}
	if decoded.Threshold != key.Threshold {
		t.Errorf("Threshold = %d, want %d", decoded.Threshold, key.Threshold)
	}
	if decoded.Status != key.Status {
		t.Errorf("Status = %q, want %q", decoded.Status, key.Status)
	}
	if len(decoded.Tags) != len(key.Tags) {
		t.Errorf("Tags length = %d, want %d", len(decoded.Tags), len(key.Tags))
	}
}

// TestKeyOperationStruct tests KeyOperation JSON serialization
func TestKeyOperationStruct(t *testing.T) {
	op := KeyOperation{
		ID:           "op123",
		KeyID:        "key1",
		Operation:    "create",
		Initiator:    "0xadmin",
		Participants: []string{"0xp1", "0xp2", "0xp3"},
		Success:      true,
		Error:        "",
		Timestamp:    time.Now(),
	}

	data, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded KeyOperation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != op.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, op.ID)
	}
	if decoded.Operation != op.Operation {
		t.Errorf("Operation = %q, want %q", decoded.Operation, op.Operation)
	}
	if len(decoded.Participants) != len(op.Participants) {
		t.Errorf("Participants length = %d, want %d", len(decoded.Participants), len(op.Participants))
	}
}

// TestEncryptionRequestStruct tests EncryptionRequest JSON serialization
func TestEncryptionRequestStruct(t *testing.T) {
	req := EncryptionRequest{
		ID:        "enc123",
		KeyID:     "key1",
		Requester: "0xuser",
		DataSize:  1024,
		Success:   true,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded EncryptionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != req.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, req.ID)
	}
	if decoded.DataSize != req.DataSize {
		t.Errorf("DataSize = %d, want %d", decoded.DataSize, req.DataSize)
	}
}

// TestSignatureRequestStruct tests SignatureRequest JSON serialization
func TestSignatureRequestStruct(t *testing.T) {
	req := SignatureRequest{
		ID:          "sig123",
		KeyID:       "key1",
		Signers:     []string{"0xs1", "0xs2", "0xs3"},
		MessageHash: "0xhash",
		Algorithm:   "frost-secp256k1",
		Success:     true,
		Timestamp:   time.Now(),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded SignatureRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != req.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, req.ID)
	}
	if decoded.Algorithm != req.Algorithm {
		t.Errorf("Algorithm = %q, want %q", decoded.Algorithm, req.Algorithm)
	}
	if len(decoded.Signers) != len(req.Signers) {
		t.Errorf("Signers length = %d, want %d", len(decoded.Signers), len(req.Signers))
	}
}

// TestAlgorithmStruct tests Algorithm JSON serialization
func TestAlgorithmStruct(t *testing.T) {
	alg := Algorithm{
		Name:             "ml-dsa-65",
		Type:             "signing",
		SecurityLevel:    192,
		KeySize:          0,
		SignatureSize:    3309,
		PostQuantum:      true,
		ThresholdSupport: false,
		Description:      "ML-DSA-65 post-quantum digital signature",
		Standards:        []string{"NIST FIPS 204"},
	}

	data, err := json.Marshal(alg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Algorithm
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Name != alg.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, alg.Name)
	}
	if decoded.SecurityLevel != alg.SecurityLevel {
		t.Errorf("SecurityLevel = %d, want %d", decoded.SecurityLevel, alg.SecurityLevel)
	}
	if decoded.PostQuantum != alg.PostQuantum {
		t.Errorf("PostQuantum = %v, want %v", decoded.PostQuantum, alg.PostQuantum)
	}
	if len(decoded.Standards) != len(alg.Standards) {
		t.Errorf("Standards length = %d, want %d", len(decoded.Standards), len(alg.Standards))
	}
}

// TestInferVertexType tests vertex type inference
func TestInferVertexType(t *testing.T) {
	adapter := New("http://localhost:9650")

	tests := []struct {
		name     string
		data     json.RawMessage
		wantType string
	}{
		{
			name:     "keys type",
			data:     json.RawMessage(`{"keys": [{"id": "k1"}]}`),
			wantType: "keys",
		},
		{
			name:     "key_operations type",
			data:     json.RawMessage(`{"keyOperations": [{"id": "op1"}]}`),
			wantType: "key_operations",
		},
		{
			name:     "encryption type",
			data:     json.RawMessage(`{"encryptionRequests": [{"id": "e1"}]}`),
			wantType: "encryption",
		},
		{
			name:     "signatures type",
			data:     json.RawMessage(`{"signatureRequests": [{"id": "s1"}]}`),
			wantType: "signatures",
		},
		{
			name:     "generic type",
			data:     json.RawMessage(`{}`),
			wantType: "generic",
		},
		{
			name:     "invalid JSON",
			data:     json.RawMessage(`{invalid`),
			wantType: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType := adapter.inferVertexType(tt.data)
			if gotType != tt.wantType {
				t.Errorf("inferVertexType() = %q, want %q", gotType, tt.wantType)
			}
		})
	}
}

// TestNetworkError tests error handling for network failures
func TestNetworkError(t *testing.T) {
	adapter := New("http://localhost:99999")
	ctx := context.Background()

	_, err := adapter.GetRecentVertices(ctx, 5)
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// TestContextCancellation tests context cancellation handling
func TestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := adapter.GetRecentVertices(ctx, 5)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

// TestRPCError tests RPC error response handling
func TestRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]interface{}{
				"code":    -32000,
				"message": "internal error",
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	_, err := adapter.GetVertexByID(ctx, "test")
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestVertexNotFound tests vertex not found case
func TestVertexNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  []interface{}{},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	_, err := adapter.GetVertexByID(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent vertex")
	}
}

// TestKeyNotFound tests key not found case
func TestKeyNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  []interface{}{},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	_, err := adapter.GetKeyByID(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
}

// TestVertexDataParsing tests parsing complete vertex data
func TestVertexDataParsing(t *testing.T) {
	adapter := New("http://localhost:9650")

	input := json.RawMessage(`{
		"id": "complete_vertex",
		"parentIds": ["p1"],
		"height": 500,
		"timestamp": 1700000000000000000,
		"status": "accepted",
		"data": {
			"keys": [
				{
					"id": "k1",
					"name": "test-key",
					"algorithm": "ml-kem-768",
					"status": "active",
					"threshold": 2,
					"totalShares": 3
				}
			],
			"keyOperations": [
				{
					"id": "op1",
					"keyId": "k1",
					"operation": "create",
					"success": true
				}
			]
		}
	}`)

	vertex, err := adapter.ParseVertex(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if vertex.ID != "complete_vertex" {
		t.Errorf("ID = %q, want complete_vertex", vertex.ID)
	}

	var data vertexData
	if err := json.Unmarshal(vertex.Data, &data); err != nil {
		t.Fatalf("failed to parse vertex data: %v", err)
	}

	if len(data.Keys) != 1 {
		t.Errorf("Keys count = %d, want 1", len(data.Keys))
	}
	if len(data.KeyOperations) != 1 {
		t.Errorf("KeyOperations count = %d, want 1", len(data.KeyOperations))
	}

	if data.Keys[0].Algorithm != "ml-kem-768" {
		t.Errorf("Key algorithm = %q, want ml-kem-768", data.Keys[0].Algorithm)
	}
	if data.KeyOperations[0].Operation != "create" {
		t.Errorf("Operation = %q, want create", data.KeyOperations[0].Operation)
	}
}

// TestPostQuantumAlgorithms tests parsing PQ algorithm configurations
func TestPostQuantumAlgorithms(t *testing.T) {
	pqAlgorithms := []string{
		"ml-kem-512", "ml-kem-768", "ml-kem-1024",
		"ml-dsa-44", "ml-dsa-65", "ml-dsa-87",
		"ringtail",
	}

	for _, alg := range pqAlgorithms {
		t.Run(alg, func(t *testing.T) {
			key := Key{
				ID:        "k1",
				Algorithm: alg,
				Status:    KeyStatusActive,
			}

			data, err := json.Marshal(key)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var decoded Key
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if decoded.Algorithm != alg {
				t.Errorf("Algorithm = %q, want %q", decoded.Algorithm, alg)
			}
		})
	}
}

// TestThresholdKeyConfiguration tests threshold key configuration
func TestThresholdKeyConfiguration(t *testing.T) {
	tests := []struct {
		threshold   int
		totalShares int
		valid       bool
	}{
		{1, 1, true},  // 1-of-1
		{2, 3, true},  // 2-of-3
		{3, 5, true},  // 3-of-5
		{5, 7, true},  // 5-of-7
		{10, 15, true}, // 10-of-15
	}

	for _, tt := range tests {
		name := fmt.Sprintf("%d-of-%d", tt.threshold, tt.totalShares)
		t.Run(name, func(t *testing.T) {
			key := Key{
				ID:          "k1",
				Algorithm:   "frost-secp256k1",
				Threshold:   tt.threshold,
				TotalShares: tt.totalShares,
				Status:      KeyStatusActive,
			}

			if key.Threshold > key.TotalShares {
				t.Error("threshold should not exceed totalShares")
			}
		})
	}
}

// Benchmarks

func BenchmarkParseVertex(b *testing.B) {
	adapter := New("http://localhost:9650")
	input := json.RawMessage(`{
		"id": "benchmark_vertex",
		"parentIds": ["p1", "p2"],
		"height": 1000,
		"timestamp": 1700000000000000000,
		"status": "accepted",
		"data": {
			"keys": [
				{"id": "k1", "name": "key1", "algorithm": "ml-kem-768", "status": "active"},
				{"id": "k2", "name": "key2", "algorithm": "ml-dsa-65", "status": "active"}
			],
			"keyOperations": [
				{"id": "op1", "keyId": "k1", "operation": "create", "success": true}
			]
		}
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.ParseVertex(input)
	}
}

func BenchmarkInferVertexType(b *testing.B) {
	adapter := New("http://localhost:9650")
	inputs := []json.RawMessage{
		json.RawMessage(`{"keys": [{"id": "k1"}]}`),
		json.RawMessage(`{"keyOperations": [{"id": "op1"}]}`),
		json.RawMessage(`{"encryptionRequests": [{"id": "e1"}]}`),
		json.RawMessage(`{"signatureRequests": [{"id": "s1"}]}`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, input := range inputs {
			adapter.inferVertexType(input)
		}
	}
}

func BenchmarkCallRPC(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  []map[string]interface{}{{"id": "v1"}},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.GetRecentVertices(ctx, 10)
	}
}
