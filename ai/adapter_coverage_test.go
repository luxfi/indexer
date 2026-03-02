// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestParseVertexEmptyData tests parsing vertex with empty data object
func TestParseVertexEmptyData(t *testing.T) {
	adapter := New("http://localhost:9650")
	data := json.RawMessage(`{
		"id": "vtx-empty-data",
		"type": "generic",
		"parentIds": [],
		"height": 1,
		"epoch": 1,
		"txIds": [],
		"timestamp": 1700000000,
		"status": "Accepted",
		"data": {}
	}`)

	v, err := adapter.ParseVertex(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v.Metadata) != 0 {
		t.Errorf("expected empty metadata for empty data, got %d keys", len(v.Metadata))
	}
}

// TestParseVertexPartialMetadata tests that only non-empty fields populate metadata
func TestParseVertexPartialMetadata(t *testing.T) {
	adapter := New("http://localhost:9650")
	data := json.RawMessage(`{
		"id": "vtx-partial",
		"type": "compute",
		"parentIds": [],
		"height": 200,
		"epoch": 10,
		"txIds": [],
		"timestamp": 1700000000,
		"status": "Accepted",
		"data": {
			"modelHash": "sha256:onlymodel",
			"computeUnits": 0,
			"attestationType": "",
			"provider": ""
		}
	}`)

	v, err := adapter.ParseVertex(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := v.Metadata["model_hash"]; !ok {
		t.Error("expected model_hash in metadata")
	}
	if _, ok := v.Metadata["attestation_type"]; ok {
		t.Error("empty attestation_type should not be in metadata")
	}
	if _, ok := v.Metadata["compute_units"]; ok {
		t.Error("zero compute_units should not be in metadata")
	}
}

// TestParseVertexStatusDirect tests status is passed through directly
func TestParseVertexStatusDirect(t *testing.T) {
	adapter := New("http://localhost:9650")
	tests := []struct {
		status string
	}{
		{"Accepted"},
		{"Pending"},
		{"Rejected"},
		{"custom_status"},
	}

	for _, tt := range tests {
		data := json.RawMessage(`{
			"id": "vtx-s",
			"type": "test",
			"parentIds": [],
			"height": 1,
			"epoch": 1,
			"txIds": [],
			"timestamp": 1700000000,
			"status": "` + tt.status + `",
			"data": {}
		}`)

		v, err := adapter.ParseVertex(data)
		if err != nil {
			t.Fatalf("unexpected error for status %s: %v", tt.status, err)
		}
		if string(v.Status) != tt.status {
			t.Errorf("expected status %s, got %s", tt.status, v.Status)
		}
	}
}

// TestParseVertexZeroTimestamp tests zero timestamp defaults to now
func TestParseVertexZeroTimestamp(t *testing.T) {
	adapter := New("http://localhost:9650")
	before := time.Now().Add(-1 * time.Second)
	data := json.RawMessage(`{
		"id": "vtx-zero-ts",
		"type": "test",
		"parentIds": [],
		"height": 1,
		"epoch": 1,
		"txIds": [],
		"timestamp": 0,
		"status": "Pending",
		"data": {}
	}`)

	v, err := adapter.ParseVertex(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Timestamp.Before(before) {
		t.Errorf("expected timestamp near now for zero ts, got %v", v.Timestamp)
	}
}

// TestParseVertexNullData tests parsing vertex with no data field
func TestParseVertexNullData(t *testing.T) {
	adapter := New("http://localhost:9650")
	data := json.RawMessage(`{
		"id": "vtx-null-data",
		"type": "test",
		"parentIds": [],
		"height": 1,
		"epoch": 1,
		"txIds": [],
		"timestamp": 1700000000,
		"status": "Accepted"
	}`)

	v, err := adapter.ParseVertex(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.ID != "vtx-null-data" {
		t.Errorf("expected ID vtx-null-data, got %s", v.ID)
	}
}

// TestGetRecentVerticesNetworkError tests network error
func TestGetRecentVerticesNetworkError(t *testing.T) {
	adapter := New("http://localhost:99999/invalid")
	_, err := adapter.GetRecentVertices(context.Background(), 10)
	if err == nil {
		t.Error("expected network error")
	}
}

// TestGetVertexByIDContextCancellation tests context cancellation on GetVertexByID
func TestGetVertexByIDContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adapter.GetVertexByID(ctx, "vtx123")
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

// TestComputeAttestationAllTypes tests all attestation types serialize correctly
func TestComputeAttestationAllTypes(t *testing.T) {
	types := []AttestationType{
		AttestationCompute,
		AttestationModel,
		AttestationInference,
		AttestationTraining,
	}

	for _, at := range types {
		t.Run(string(at), func(t *testing.T) {
			att := ComputeAttestation{
				ID:           "att-" + string(at),
				Type:         at,
				ComputeUnits: 100,
				Provider:     "test-provider",
				Requester:    "test-requester",
				Timestamp:    time.Now().Truncate(time.Second),
				Status:       "completed",
			}

			data, err := json.Marshal(att)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var decoded ComputeAttestation
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if decoded.Type != at {
				t.Errorf("expected type %s, got %s", at, decoded.Type)
			}
		})
	}
}

// TestTrainingJobZeroValues tests training job with zero-value fields
func TestTrainingJobZeroValues(t *testing.T) {
	job := TrainingJob{
		ID:          "job-zero",
		DatasetHash: "sha256:empty",
		Status:      "pending",
	}

	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded TrainingJob
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Epochs != 0 {
		t.Errorf("expected 0 epochs, got %d", decoded.Epochs)
	}
	if decoded.LearningRate != 0 {
		t.Errorf("expected 0 learning rate, got %f", decoded.LearningRate)
	}
}

// TestAIProviderInactive tests inactive provider state
func TestAIProviderInactive(t *testing.T) {
	provider := AIProvider{
		ID:         "prov-inactive",
		Address:    "0x0000",
		Active:     false,
		Reputation: 0.0,
		Capacity:   0,
		Timestamp:  time.Now(),
	}

	if provider.Active {
		t.Error("expected inactive provider")
	}
	if provider.Reputation != 0.0 {
		t.Errorf("expected 0 reputation, got %f", provider.Reputation)
	}
}

// TestGetRecentVerticesInvalidJSON tests handling of invalid JSON response
func TestGetRecentVerticesInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetRecentVertices(context.Background(), 10)
	if err == nil {
		t.Error("expected error for invalid JSON response")
	}
}
