// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package tchain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestParseVertexInvalidJSON tests error on invalid JSON
func TestParseVertexInvalidJSON(t *testing.T) {
	adapter := New("")
	_, err := adapter.ParseVertex(json.RawMessage(`{invalid json}`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// TestParseVertexAllMetadataFields tests all MPC metadata fields
func TestParseVertexAllMetadataFields(t *testing.T) {
	adapter := New("")

	vertexJSON := `{
		"id": "tv-full",
		"type": "signing_session",
		"parentIds": ["tv-p1"],
		"height": 500,
		"epoch": 25,
		"timestamp": 1705323000,
		"status": "accepted",
		"sessionId": "sess-abc",
		"keyGenId": "kg-xyz",
		"messageId": "msg-123",
		"data": {"threshold": 3, "messageHash": "0xabc"}
	}`

	v, err := adapter.ParseVertex(json.RawMessage(vertexJSON))
	if err != nil {
		t.Fatalf("ParseVertex failed: %v", err)
	}

	if v.Metadata["sessionId"] != "sess-abc" {
		t.Errorf("sessionId: got %v, want sess-abc", v.Metadata["sessionId"])
	}
	if v.Metadata["keyGenId"] != "kg-xyz" {
		t.Errorf("keyGenId: got %v, want kg-xyz", v.Metadata["keyGenId"])
	}
	if v.Metadata["messageId"] != "msg-123" {
		t.Errorf("messageId: got %v, want msg-123", v.Metadata["messageId"])
	}
}

// TestParseVertexEmptyMetadata tests vertex with no MPC-specific fields
func TestParseVertexEmptyMetadata(t *testing.T) {
	adapter := New("")

	vertexJSON := `{
		"id": "tv-empty",
		"type": "generic",
		"parentIds": [],
		"height": 1,
		"epoch": 1,
		"timestamp": 1705323000,
		"status": "pending",
		"data": {}
	}`

	v, err := adapter.ParseVertex(json.RawMessage(vertexJSON))
	if err != nil {
		t.Fatalf("ParseVertex failed: %v", err)
	}

	if _, ok := v.Metadata["sessionId"]; ok {
		t.Error("sessionId should not be in metadata when empty")
	}
}

// TestInferVertexTypeEdgeCases tests edge cases for type inference
func TestInferVertexTypeEdgeCases(t *testing.T) {
	adapter := New("")

	tests := []struct {
		name     string
		data     string
		wantType string
	}{
		{
			name:     "empty object",
			data:     `{}`,
			wantType: "unknown",
		},
		{
			name:     "threshold without messageHash or publicKey",
			data:     `{"threshold": 3}`,
			wantType: "unknown",
		},
		{
			name:     "null data",
			data:     `null`,
			wantType: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := adapter.inferVertexType(json.RawMessage(tt.data))
			if result != tt.wantType {
				t.Errorf("got %s, want %s", result, tt.wantType)
			}
		})
	}
}

// TestComputeThresholdEdgeCases tests threshold computation edge cases
func TestComputeThresholdEdgeCases(t *testing.T) {
	tests := []struct {
		total uint32
		want  uint32
	}{
		{0, 1},   // Zero shares
		{1, 1},   // Single share
		{2, 2},   // Minimum multi-party
		{3, 3},   // 3 * 2/3 + 1 = 3
		{21, 15}, // 21 * 2/3 + 1 = 15
	}

	for _, tt := range tests {
		got := ComputeThreshold(tt.total)
		if got != tt.want {
			t.Errorf("ComputeThreshold(%d) = %d, want %d", tt.total, got, tt.want)
		}
	}
}

// TestGetSigningSessionRPCError tests RPC error from GetSigningSession
func TestGetSigningSessionRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"error":   map[string]interface{}{"code": -32000, "message": "session not found"},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetSigningSession(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestGetKeyGenerationRPCError tests RPC error from GetKeyGeneration
func TestGetKeyGenerationRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"error":   map[string]interface{}{"code": -32000, "message": "keygen not found"},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetKeyGeneration(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestGetActiveKeySharesRPCError tests RPC error from GetActiveKeyShares
func TestGetActiveKeySharesRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"error":   map[string]interface{}{"code": -32000, "message": "key not found"},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetActiveKeyShares(context.Background(), "0xnonexistent")
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestSigningSessionNilCompletedAt tests session with nil CompletedAt
func TestSigningSessionNilCompletedAt(t *testing.T) {
	session := SigningSession{
		ID:          "sess-nil",
		Threshold:   2,
		TotalShares: 3,
		MessageHash: "0xhash",
		Status:      SessionPending,
		CreatedAt:   time.Now(),
		CompletedAt: nil,
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	data, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SigningSession
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.CompletedAt != nil {
		t.Error("CompletedAt should be nil")
	}
	if decoded.Status != SessionPending {
		t.Errorf("Status: got %s, want pending", decoded.Status)
	}
}

// TestKeyShareNilOptionalTimes tests KeyShare with nil optional times
func TestKeyShareNilOptionalTimes(t *testing.T) {
	share := KeyShare{
		ID:         "share-nil",
		PublicKey:  "0xpub",
		ShareIndex: 1,
		NodeID:     "node1",
		KeyGenID:   "kg1",
		Status:     SharePending,
		CreatedAt:  time.Now(),
		RotatedAt:  nil,
		ExpiresAt:  nil,
	}

	data, err := json.Marshal(share)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded KeyShare
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.RotatedAt != nil {
		t.Error("RotatedAt should be nil")
	}
	if decoded.ExpiresAt != nil {
		t.Error("ExpiresAt should be nil")
	}
}

// TestTeleportMessageNilOptionalTimes tests message with nil optional times
func TestTeleportMessageNilOptionalTimes(t *testing.T) {
	msg := TeleportMessage{
		ID:          "msg-nil",
		SourceChain: "C",
		DestChain:   "X",
		Sender:      "0xsender",
		Receiver:    "X-lux1rcpt",
		Payload:     json.RawMessage(`{}`),
		Nonce:       1,
		Status:      "pending",
		CreatedAt:   time.Now(),
		SignedAt:    nil,
		DeliveredAt: nil,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded TeleportMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.SignedAt != nil {
		t.Error("SignedAt should be nil")
	}
	if decoded.DeliveredAt != nil {
		t.Error("DeliveredAt should be nil")
	}
}

// TestVertexDataNilFields tests VertexData with all nil fields
func TestVertexDataNilFields(t *testing.T) {
	vd := VertexData{}

	data, err := json.Marshal(vd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded VertexData
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Session != nil {
		t.Error("Session should be nil")
	}
	if decoded.KeyShare != nil {
		t.Error("KeyShare should be nil")
	}
	if decoded.KeyGen != nil {
		t.Error("KeyGen should be nil")
	}
	if decoded.Message != nil {
		t.Error("Message should be nil")
	}
}
