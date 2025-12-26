// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package achain

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
	endpoint := "http://localhost:9650/ext/bc/A"
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
	if DefaultPort != 4500 {
		t.Errorf("expected DefaultPort 4500, got %d", DefaultPort)
	}
	if DefaultDatabase != "explorer_achain" {
		t.Errorf("expected DefaultDatabase explorer_achain, got %s", DefaultDatabase)
	}
}

// TestAttestationTypes tests attestation type constants
func TestAttestationTypes(t *testing.T) {
	tests := []struct {
		attType  AttestationType
		expected string
	}{
		{AttestationCompute, "compute"},
		{AttestationModel, "model"},
		{AttestationInference, "inference"},
		{AttestationTraining, "training"},
	}

	for _, tt := range tests {
		if string(tt.attType) != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, tt.attType)
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
		name       string
		data       string
		wantID     string
		wantType   string
		wantErr    bool
		checkMeta  bool
		metaKey    string
		metaValue  interface{}
	}{
		{
			name: "basic vertex",
			data: `{
				"id": "vtx123",
				"type": "ai_vertex",
				"parentIds": ["vtx122"],
				"height": 100,
				"epoch": 5,
				"txIds": ["tx1"],
				"timestamp": 1700000000,
				"status": "Accepted",
				"data": {}
			}`,
			wantID:   "vtx123",
			wantType: "ai_vertex",
			wantErr:  false,
		},
		{
			name: "vertex with AI metadata",
			data: `{
				"id": "vtx124",
				"type": "attestation",
				"parentIds": [],
				"height": 101,
				"epoch": 5,
				"txIds": [],
				"timestamp": 1700000001,
				"status": "Pending",
				"data": {
					"attestationType": "compute",
					"modelHash": "sha256:abc123",
					"provider": "provider1",
					"computeUnits": 1000
				}
			}`,
			wantID:    "vtx124",
			wantType:  "attestation",
			wantErr:   false,
			checkMeta: true,
			metaKey:   "attestation_type",
			metaValue: "compute",
		},
		{
			name: "vertex with zero timestamp",
			data: `{
				"id": "vtx125",
				"type": "inference",
				"parentIds": [],
				"height": 102,
				"epoch": 5,
				"txIds": [],
				"timestamp": 0,
				"status": "Accepted",
				"data": {}
			}`,
			wantID:   "vtx125",
			wantType: "inference",
			wantErr:  false,
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

			if tt.checkMeta {
				if val, ok := vertex.Metadata[tt.metaKey]; !ok {
					t.Errorf("expected metadata key %s", tt.metaKey)
				} else if val != tt.metaValue {
					t.Errorf("expected metadata value %v, got %v", tt.metaValue, val)
				}
			}
		})
	}
}

// TestParseVertexMetadataExtraction tests AI metadata extraction
func TestParseVertexMetadataExtraction(t *testing.T) {
	adapter := New("http://localhost:9650")

	data := `{
		"id": "vtx126",
		"type": "compute",
		"parentIds": [],
		"height": 103,
		"epoch": 5,
		"txIds": [],
		"timestamp": 1700000000,
		"status": "Accepted",
		"data": {
			"attestationType": "inference",
			"modelHash": "sha256:model123",
			"provider": "ai-provider-1",
			"computeUnits": 5000
		}
	}`

	vertex, err := adapter.ParseVertex(json.RawMessage(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedMeta := map[string]interface{}{
		"attestation_type": "inference",
		"model_hash":       "sha256:model123",
		"provider":         "ai-provider-1",
		"compute_units":    uint64(5000),
	}

	for key, expected := range expectedMeta {
		if val, ok := vertex.Metadata[key]; !ok {
			t.Errorf("missing metadata key: %s", key)
		} else if val != expected {
			t.Errorf("metadata %s: expected %v, got %v", key, expected, val)
		}
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
						{"id": "vtx1"},
						{"id": "vtx2"}
					]
				}
			}`,
			limit:     10,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "empty result",
			response: `{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {
					"vertices": []
				}
			}`,
			limit:     10,
			wantCount: 0,
			wantErr:   false,
		},
		{
			name: "rpc error",
			response: `{
				"jsonrpc": "2.0",
				"id": 1,
				"error": {
					"code": -32000,
					"message": "internal error"
				}
			}`,
			limit:       10,
			wantErr:     true,
			errContains: "internal error",
		},
		{
			name:        "invalid json",
			response:    `invalid`,
			limit:       10,
			wantErr:     true,
			errContains: "decode response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Errorf("expected POST, got %s", r.Method)
				}
				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			adapter := New(server.URL)
			vertices, err := adapter.GetRecentVertices(context.Background(), tt.limit)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
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

// TestGetRecentVerticesRequestValidation tests the request format
func TestGetRecentVerticesRequestValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["jsonrpc"] != "2.0" {
			t.Errorf("expected jsonrpc 2.0, got %v", req["jsonrpc"])
		}
		if req["method"] != "avm.getRecentVertices" {
			t.Errorf("expected method avm.getRecentVertices, got %v", req["method"])
		}

		params := req["params"].(map[string]interface{})
		if params["limit"] != float64(25) {
			t.Errorf("expected limit 25, got %v", params["limit"])
		}

		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"vertices":[]}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetRecentVertices(context.Background(), 25)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestGetVertexByID tests fetching a specific vertex
func TestGetVertexByID(t *testing.T) {
	tests := []struct {
		name        string
		vertexID    string
		response    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "found vertex",
			vertexID: "vtx123",
			response: `{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {"id": "vtx123", "status": "Accepted"}
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
			wantErr:     true,
			errContains: "vertex not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req map[string]interface{}
				json.NewDecoder(r.Body).Decode(&req)

				if req["method"] != "avm.getVertex" {
					t.Errorf("expected method avm.getVertex, got %v", req["method"])
				}

				params := req["params"].(map[string]interface{})
				if params["vertexID"] != tt.vertexID {
					t.Errorf("expected vertexID %s, got %v", tt.vertexID, params["vertexID"])
				}

				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			adapter := New(server.URL)
			result, err := adapter.GetVertexByID(context.Background(), tt.vertexID)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if result == nil {
					t.Error("expected non-nil result")
				}
			}
		})
	}
}

// TestGetVertexByIDNetworkError tests network error handling
func TestGetVertexByIDNetworkError(t *testing.T) {
	adapter := New("http://localhost:99999/invalid")
	_, err := adapter.GetVertexByID(context.Background(), "vtx123")
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

// TestComputeAttestationStructure tests ComputeAttestation struct
func TestComputeAttestationStructure(t *testing.T) {
	att := ComputeAttestation{
		ID:           "att123",
		Type:         AttestationCompute,
		ModelHash:    "sha256:abc",
		InputHash:    "sha256:input",
		OutputHash:   "sha256:output",
		ComputeUnits: 1000,
		Provider:     "provider1",
		Requester:    "requester1",
		Timestamp:    time.Now(),
		Status:       "completed",
		Metadata:     json.RawMessage(`{"gpu": "A100"}`),
	}

	if att.Type != AttestationCompute {
		t.Errorf("expected type compute, got %s", att.Type)
	}
	if att.ComputeUnits != 1000 {
		t.Errorf("expected 1000 compute units, got %d", att.ComputeUnits)
	}

	// Test JSON serialization
	data, err := json.Marshal(att)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ComputeAttestation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != "att123" {
		t.Errorf("expected ID att123, got %s", decoded.ID)
	}
}

// TestModelRegistrationStructure tests ModelRegistration struct
func TestModelRegistrationStructure(t *testing.T) {
	model := ModelRegistration{
		ID:         "model123",
		ModelHash:  "sha256:modelhash",
		Name:       "GPT-LUX",
		Version:    "1.0.0",
		Owner:      "owner1",
		Framework:  "pytorch",
		Parameters: 7000000000,
		Timestamp:  time.Now(),
		Metadata:   json.RawMessage(`{"architecture": "transformer"}`),
	}

	if model.Parameters != 7000000000 {
		t.Errorf("expected 7B parameters, got %d", model.Parameters)
	}

	data, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ModelRegistration
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Framework != "pytorch" {
		t.Errorf("expected framework pytorch, got %s", decoded.Framework)
	}
}

// TestInferenceResultStructure tests InferenceResult struct
func TestInferenceResultStructure(t *testing.T) {
	inf := InferenceResult{
		ID:         "inf123",
		ModelID:    "model123",
		InputHash:  "sha256:input",
		OutputHash: "sha256:output",
		Confidence: 0.95,
		Latency:    100 * time.Millisecond,
		Provider:   "provider1",
		Timestamp:  time.Now(),
		Metadata:   json.RawMessage(`{}`),
	}

	if inf.Confidence != 0.95 {
		t.Errorf("expected confidence 0.95, got %f", inf.Confidence)
	}
	if inf.Latency != 100*time.Millisecond {
		t.Errorf("expected latency 100ms, got %v", inf.Latency)
	}
}

// TestAIProviderStructure tests AIProvider struct
func TestAIProviderStructure(t *testing.T) {
	provider := AIProvider{
		ID:         "prov123",
		Address:    "0x1234...",
		Name:       "LUX AI Provider",
		Reputation: 4.5,
		Capacity:   1000000,
		Active:     true,
		Timestamp:  time.Now(),
		Metadata:   json.RawMessage(`{"gpus": 8}`),
	}

	if !provider.Active {
		t.Error("expected provider to be active")
	}
	if provider.Reputation != 4.5 {
		t.Errorf("expected reputation 4.5, got %f", provider.Reputation)
	}
}

// TestAIReceiptStructure tests AIReceipt struct
func TestAIReceiptStructure(t *testing.T) {
	receipt := AIReceipt{
		ID:            "rcpt123",
		AttestationID: "att123",
		Provider:      "provider1",
		Requester:     "requester1",
		Amount:        1000000000,
		Currency:      "AI",
		Status:        "completed",
		Timestamp:     time.Now(),
	}

	if receipt.Currency != "AI" {
		t.Errorf("expected currency AI, got %s", receipt.Currency)
	}
	if receipt.Amount != 1000000000 {
		t.Errorf("expected amount 1000000000, got %d", receipt.Amount)
	}
}

// TestTrainingJobStructure tests TrainingJob struct
func TestTrainingJobStructure(t *testing.T) {
	now := time.Now()
	job := TrainingJob{
		ID:           "job123",
		ModelID:      "model123",
		DatasetHash:  "sha256:dataset",
		Epochs:       100,
		BatchSize:    32,
		LearningRate: 0.001,
		Provider:     "provider1",
		Status:       "running",
		StartedAt:    now,
		CompletedAt:  time.Time{},
		Metadata:     json.RawMessage(`{"optimizer": "adam"}`),
	}

	if job.Epochs != 100 {
		t.Errorf("expected 100 epochs, got %d", job.Epochs)
	}
	if job.LearningRate != 0.001 {
		t.Errorf("expected learning rate 0.001, got %f", job.LearningRate)
	}

	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded TrainingJob
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.BatchSize != 32 {
		t.Errorf("expected batch size 32, got %d", decoded.BatchSize)
	}
}

// TestAllStructsJSONRoundTrip tests JSON serialization for all structs
func TestAllStructsJSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)

	testCases := []struct {
		name string
		val  interface{}
	}{
		{
			name: "ComputeAttestation",
			val: ComputeAttestation{
				ID: "att1", Type: AttestationCompute, ComputeUnits: 100,
				Timestamp: now, Status: "pending",
			},
		},
		{
			name: "ModelRegistration",
			val: ModelRegistration{
				ID: "mod1", ModelHash: "hash", Name: "test", Owner: "owner",
				Timestamp: now,
			},
		},
		{
			name: "InferenceResult",
			val: InferenceResult{
				ID: "inf1", ModelID: "mod1", InputHash: "in", OutputHash: "out",
				Timestamp: now,
			},
		},
		{
			name: "AIProvider",
			val: AIProvider{
				ID: "prov1", Address: "addr", Active: true, Timestamp: now,
			},
		},
		{
			name: "AIReceipt",
			val: AIReceipt{
				ID: "rcpt1", AttestationID: "att1", Provider: "p", Requester: "r",
				Amount: 100, Currency: "AI", Timestamp: now,
			},
		},
		{
			name: "TrainingJob",
			val: TrainingJob{
				ID: "job1", DatasetHash: "hash", Status: "pending",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.val)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			if len(data) == 0 {
				t.Error("expected non-empty JSON")
			}
		})
	}
}

// helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
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
		"type": "attestation",
		"parentIds": ["vtx122"],
		"height": 100,
		"epoch": 5,
		"txIds": ["tx1", "tx2"],
		"timestamp": 1700000000,
		"status": "Accepted",
		"data": {
			"attestationType": "compute",
			"modelHash": "sha256:abc",
			"provider": "provider1",
			"computeUnits": 1000
		}
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.ParseVertex(data)
	}
}

// BenchmarkGetRecentVertices benchmarks RPC calls
func BenchmarkGetRecentVertices(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"vertices":[{"id":"vtx1"},{"id":"vtx2"}]}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.GetRecentVertices(ctx, 10)
	}
}
