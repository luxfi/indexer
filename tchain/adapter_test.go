// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package tchain

import (
	"context"
	"encoding/hex"
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

// TestConstants tests T-Chain specific constants
func TestConstants(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  interface{}
	}{
		{"DefaultRPCEndpoint", DefaultRPCEndpoint, "http://localhost:9650/ext/bc/T/rpc"},
		{"DefaultHTTPPort", DefaultHTTPPort, 4700},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.want {
				t.Errorf("got %v, want %v", tt.value, tt.want)
			}
		})
	}
}

// TestSessionStatus tests session status constants
func TestSessionStatus(t *testing.T) {
	statuses := []SessionStatus{
		SessionPending,
		SessionActive,
		SessionCompleted,
		SessionFailed,
		SessionExpired,
	}
	expected := []string{"pending", "active", "completed", "failed", "expired"}

	for i, status := range statuses {
		if string(status) != expected[i] {
			t.Errorf("status %d: got %s, want %s", i, status, expected[i])
		}
	}
}

// TestKeyShareStatus tests key share status constants
func TestKeyShareStatus(t *testing.T) {
	statuses := []KeyShareStatus{
		SharePending,
		ShareActive,
		ShareRevoked,
		ShareRotated,
	}
	expected := []string{"pending", "active", "revoked", "rotated"}

	for i, status := range statuses {
		if string(status) != expected[i] {
			t.Errorf("status %d: got %s, want %s", i, status, expected[i])
		}
	}
}

// TestSigningSessionJSON tests SigningSession JSON serialization
func TestSigningSessionJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	completed := now.Add(5 * time.Minute)

	session := SigningSession{
		ID:           "session-123",
		Threshold:    3,
		TotalShares:  5,
		MessageHash:  "0xabcdef1234567890",
		Participants: []string{"node1", "node2", "node3"},
		Signatures:   []string{"sig1", "sig2", "sig3"},
		FinalSig:     "final-signature",
		Status:       SessionCompleted,
		CreatedAt:    now,
		CompletedAt:  &completed,
		ExpiresAt:    now.Add(1 * time.Hour),
		SourceChain:  "C",
		DestChain:    "X",
	}

	data, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded SigningSession
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != session.ID {
		t.Errorf("ID: got %s, want %s", decoded.ID, session.ID)
	}
	if decoded.Threshold != session.Threshold {
		t.Errorf("Threshold: got %d, want %d", decoded.Threshold, session.Threshold)
	}
	if decoded.TotalShares != session.TotalShares {
		t.Errorf("TotalShares: got %d, want %d", decoded.TotalShares, session.TotalShares)
	}
	if len(decoded.Participants) != 3 {
		t.Errorf("Participants count: got %d, want 3", len(decoded.Participants))
	}
	if decoded.Status != SessionCompleted {
		t.Errorf("Status: got %s, want %s", decoded.Status, SessionCompleted)
	}
	if decoded.SourceChain != "C" {
		t.Errorf("SourceChain: got %s, want C", decoded.SourceChain)
	}
}

// TestKeyShareJSON tests KeyShare JSON serialization
func TestKeyShareJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	expires := now.Add(365 * 24 * time.Hour)

	share := KeyShare{
		ID:         "share-123",
		PublicKey:  "0x0402abcd...",
		ShareIndex: 2,
		NodeID:     "NodeID-abc123",
		KeyGenID:   "keygen-456",
		Status:     ShareActive,
		CreatedAt:  now,
		RotatedAt:  nil,
		ExpiresAt:  &expires,
	}

	data, err := json.Marshal(share)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded KeyShare
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != share.ID {
		t.Errorf("ID: got %s, want %s", decoded.ID, share.ID)
	}
	if decoded.ShareIndex != share.ShareIndex {
		t.Errorf("ShareIndex: got %d, want %d", decoded.ShareIndex, share.ShareIndex)
	}
	if decoded.Status != ShareActive {
		t.Errorf("Status: got %s, want %s", decoded.Status, ShareActive)
	}
}

// TestKeyGenerationJSON tests KeyGeneration JSON serialization
func TestKeyGenerationJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	completed := now.Add(30 * time.Second)

	keygen := KeyGeneration{
		ID:           "keygen-123",
		Threshold:    2,
		TotalShares:  3,
		PublicKey:    "0x04abcdef...",
		Participants: []string{"node1", "node2", "node3"},
		Status:       "completed",
		CreatedAt:    now,
		CompletedAt:  &completed,
	}

	data, err := json.Marshal(keygen)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded KeyGeneration
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != keygen.ID {
		t.Errorf("ID: got %s, want %s", decoded.ID, keygen.ID)
	}
	if decoded.Threshold != keygen.Threshold {
		t.Errorf("Threshold: got %d, want %d", decoded.Threshold, keygen.Threshold)
	}
	if decoded.Status != "completed" {
		t.Errorf("Status: got %s, want completed", decoded.Status)
	}
}

// TestTeleportMessageJSON tests TeleportMessage JSON serialization
func TestTeleportMessageJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	signed := now.Add(2 * time.Second)
	delivered := now.Add(10 * time.Second)

	msg := TeleportMessage{
		ID:          "msg-123",
		SourceChain: "C",
		DestChain:   "X",
		Sender:      "0x1234...",
		Receiver:    "X-lux1abc...",
		Payload:     json.RawMessage(`{"type":"transfer","amount":"1000000"}`),
		Nonce:       42,
		SessionID:   "session-456",
		Status:      "delivered",
		CreatedAt:   now,
		SignedAt:    &signed,
		DeliveredAt: &delivered,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded TeleportMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != msg.ID {
		t.Errorf("ID: got %s, want %s", decoded.ID, msg.ID)
	}
	if decoded.SourceChain != "C" {
		t.Errorf("SourceChain: got %s, want C", decoded.SourceChain)
	}
	if decoded.Nonce != 42 {
		t.Errorf("Nonce: got %d, want 42", decoded.Nonce)
	}
}

// TestInterfaceCompliance verifies Adapter implements dag.Adapter
func TestInterfaceCompliance(t *testing.T) {
	var _ dag.Adapter = (*Adapter)(nil)
}

// TestParseVertex tests vertex parsing
func TestParseVertex(t *testing.T) {
	adapter := New("")

	vertexJSON := `{
		"id": "tv-123abc",
		"type": "signing_session",
		"parentIds": ["tv-parent1", "tv-parent2"],
		"height": 1000,
		"epoch": 50,
		"timestamp": 1705323000,
		"status": "accepted",
		"sessionId": "session-xyz",
		"data": {"threshold": 3, "messageHash": "0xabc123"}
	}`

	v, err := adapter.ParseVertex(json.RawMessage(vertexJSON))
	if err != nil {
		t.Fatalf("ParseVertex failed: %v", err)
	}

	if v.ID != "tv-123abc" {
		t.Errorf("ID: got %s, want tv-123abc", v.ID)
	}
	if v.Type != "signing_session" {
		t.Errorf("Type: got %s, want signing_session", v.Type)
	}
	if v.Height != 1000 {
		t.Errorf("Height: got %d, want 1000", v.Height)
	}
	if v.Epoch != 50 {
		t.Errorf("Epoch: got %d, want 50", v.Epoch)
	}
	if len(v.ParentIDs) != 2 {
		t.Errorf("ParentIDs count: got %d, want 2", len(v.ParentIDs))
	}
	if v.Metadata["sessionId"] != "session-xyz" {
		t.Errorf("sessionId: got %v, want session-xyz", v.Metadata["sessionId"])
	}
}

// TestInferVertexType tests vertex type inference
func TestInferVertexType(t *testing.T) {
	adapter := New("")

	tests := []struct {
		name     string
		data     string
		wantType string
	}{
		{
			name:     "signing session",
			data:     `{"threshold": 3, "messageHash": "0xabc"}`,
			wantType: "signing_session",
		},
		{
			name:     "key generation",
			data:     `{"threshold": 2, "publicKey": "0x04abc"}`,
			wantType: "key_generation",
		},
		{
			name:     "key share",
			data:     `{"shareIndex": 1, "nodeId": "node1"}`,
			wantType: "key_share",
		},
		{
			name:     "teleport message",
			data:     `{"sourceChain": "C", "destChain": "X"}`,
			wantType: "teleport_message",
		},
		{
			name:     "unknown",
			data:     `{"foo": "bar"}`,
			wantType: "unknown",
		},
		{
			name:     "invalid json",
			data:     `invalid`,
			wantType: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vertexType := adapter.inferVertexType(json.RawMessage(tt.data))
			if vertexType != tt.wantType {
				t.Errorf("got %s, want %s", vertexType, tt.wantType)
			}
		})
	}
}

// TestParseVertexInferType tests type inference when type is empty
func TestParseVertexInferType(t *testing.T) {
	adapter := New("")

	vertexJSON := `{
		"id": "tv-infer",
		"type": "",
		"parentIds": [],
		"height": 1,
		"epoch": 1,
		"timestamp": 1705323000,
		"status": "pending",
		"data": {"threshold": 2, "messageHash": "0xdef456"}
	}`

	v, err := adapter.ParseVertex(json.RawMessage(vertexJSON))
	if err != nil {
		t.Fatalf("ParseVertex failed: %v", err)
	}

	if v.Type != "signing_session" {
		t.Errorf("Type: got %s, want signing_session", v.Type)
	}
}

// TestGetRecentVertices tests fetching recent vertices
func TestGetRecentVertices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req["method"] != "tvm.getRecentVertices" {
			t.Errorf("unexpected method: %v", req["method"])
		}

		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"vertices": []map[string]interface{}{
					{"id": "tv-1", "height": 100},
					{"id": "tv-2", "height": 101},
				},
			},
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
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"vertex": map[string]interface{}{"id": "tv-target", "height": 500},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	vertex, err := adapter.GetVertexByID(ctx, "tv-target")
	if err != nil {
		t.Fatalf("GetVertexByID failed: %v", err)
	}

	if vertex == nil {
		t.Fatal("expected non-nil vertex")
	}
}

// TestGetSigningSession tests fetching a signing session
func TestGetSigningSession(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"id":           "session-test",
				"threshold":    3,
				"totalShares":  5,
				"messageHash":  "0xabc",
				"participants": []string{"n1", "n2", "n3"},
				"status":       "active",
				"createdAt":    now.Format(time.RFC3339),
				"expiresAt":    now.Add(time.Hour).Format(time.RFC3339),
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	session, err := adapter.GetSigningSession(ctx, "session-test")
	if err != nil {
		t.Fatalf("GetSigningSession failed: %v", err)
	}

	if session.ID != "session-test" {
		t.Errorf("ID: got %s, want session-test", session.ID)
	}
	if session.Threshold != 3 {
		t.Errorf("Threshold: got %d, want 3", session.Threshold)
	}
}

// TestGetKeyGeneration tests fetching a key generation ceremony
func TestGetKeyGeneration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"id":           "keygen-test",
				"threshold":    2,
				"totalShares":  3,
				"publicKey":    "0x04abc...",
				"participants": []string{"n1", "n2", "n3"},
				"status":       "completed",
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	keygen, err := adapter.GetKeyGeneration(ctx, "keygen-test")
	if err != nil {
		t.Fatalf("GetKeyGeneration failed: %v", err)
	}

	if keygen.ID != "keygen-test" {
		t.Errorf("ID: got %s, want keygen-test", keygen.ID)
	}
	if keygen.Threshold != 2 {
		t.Errorf("Threshold: got %d, want 2", keygen.Threshold)
	}
}

// TestGetActiveKeyShares tests fetching active key shares
func TestGetActiveKeyShares(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"keyShares": []map[string]interface{}{
					{"id": "share-1", "shareIndex": 1, "status": "active"},
					{"id": "share-2", "shareIndex": 2, "status": "active"},
				},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	shares, err := adapter.GetActiveKeyShares(ctx, "0x04pubkey...")
	if err != nil {
		t.Fatalf("GetActiveKeyShares failed: %v", err)
	}

	if len(shares) != 2 {
		t.Errorf("expected 2 shares, got %d", len(shares))
	}
}

// TestRPCError tests handling of RPC errors
func TestRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]interface{}{
				"code":    -32600,
				"message": "Invalid request",
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

	if config.ChainType != dag.ChainT {
		t.Errorf("ChainType: got %s, want %s", config.ChainType, dag.ChainT)
	}
	if config.ChainName != "T-Chain (Teleport)" {
		t.Errorf("ChainName: got %s, want T-Chain (Teleport)", config.ChainName)
	}
	if config.RPCEndpoint != DefaultRPCEndpoint {
		t.Errorf("RPCEndpoint: got %s, want %s", config.RPCEndpoint, DefaultRPCEndpoint)
	}
	if config.HTTPPort != DefaultHTTPPort {
		t.Errorf("HTTPPort: got %d, want %d", config.HTTPPort, DefaultHTTPPort)
	}
	if config.RPCMethod != "tvm" {
		t.Errorf("RPCMethod: got %s, want tvm", config.RPCMethod)
	}
}

// TestComputeThreshold tests threshold computation
func TestComputeThreshold(t *testing.T) {
	tests := []struct {
		totalShares   uint32
		wantThreshold uint32
	}{
		{1, 1},              // Min case
		{2, 2},              // 2 * 2/3 + 1 = 2
		{3, 3},              // 3 * 2/3 + 1 = 3
		{4, 3},              // 4 * 2/3 + 1 = 3
		{5, 4},              // 5 * 2/3 + 1 = 4
		{6, 5},              // 6 * 2/3 + 1 = 5
		{7, 5},              // 7 * 2/3 + 1 = 5
		{9, 7},              // 9 * 2/3 + 1 = 7
		{10, 7},             // 10 * 2/3 + 1 = 7
		{100, 67},           // 100 * 2/3 + 1 = 67
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := ComputeThreshold(tt.totalShares)
			if got != tt.wantThreshold {
				t.Errorf("ComputeThreshold(%d) = %d, want %d", tt.totalShares, got, tt.wantThreshold)
			}
		})
	}
}

// TestValidateSignature tests signature validation
func TestValidateSignature(t *testing.T) {
	// Create valid hex-encoded test data
	publicKey := hex.EncodeToString(make([]byte, 33))  // 33 bytes -> 66 hex chars
	message := hex.EncodeToString([]byte("test message"))
	signature := hex.EncodeToString(make([]byte, 65))  // 65 bytes -> 130 hex chars

	valid, err := ValidateSignature(publicKey, message, signature)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !valid {
		t.Error("expected valid signature")
	}
}

// TestValidateSignatureInvalidHex tests signature validation with invalid hex
func TestValidateSignatureInvalidHex(t *testing.T) {
	tests := []struct {
		name      string
		publicKey string
		message   string
		signature string
	}{
		{"invalid public key", "not-hex", "0102", "0102"},
		{"invalid message", "0102", "not-hex", "0102"},
		{"invalid signature", "0102", "0102", "not-hex"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateSignature(tt.publicKey, tt.message, tt.signature)
			if err == nil {
				t.Error("expected error for invalid hex")
			}
		})
	}
}

// TestValidateSignatureTooShort tests signature validation with short keys
func TestValidateSignatureTooShort(t *testing.T) {
	shortKey := hex.EncodeToString(make([]byte, 10))     // Too short
	message := hex.EncodeToString([]byte("test"))
	shortSig := hex.EncodeToString(make([]byte, 32))     // Too short

	_, err := ValidateSignature(shortKey, message, shortSig)
	if err == nil {
		t.Error("expected error for short key/signature")
	}
}

// TestVertexData tests VertexData struct
func TestVertexData(t *testing.T) {
	now := time.Now().UTC()

	vd := VertexData{
		Session: &SigningSession{
			ID:        "session-1",
			Threshold: 2,
			Status:    SessionActive,
		},
		KeyShare: &KeyShare{
			ID:         "share-1",
			ShareIndex: 1,
			Status:     ShareActive,
		},
		KeyGen: &KeyGeneration{
			ID:        "keygen-1",
			Threshold: 2,
		},
		Message: &TeleportMessage{
			ID:          "msg-1",
			SourceChain: "C",
			DestChain:   "X",
			CreatedAt:   now,
		},
	}

	data, err := json.Marshal(vd)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded VertexData
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Session.ID != "session-1" {
		t.Errorf("Session.ID: got %s, want session-1", decoded.Session.ID)
	}
	if decoded.KeyShare.ShareIndex != 1 {
		t.Errorf("KeyShare.ShareIndex: got %d, want 1", decoded.KeyShare.ShareIndex)
	}
	if decoded.Message.SourceChain != "C" {
		t.Errorf("Message.SourceChain: got %s, want C", decoded.Message.SourceChain)
	}
}

// BenchmarkParseVertex benchmarks vertex parsing
func BenchmarkParseVertex(b *testing.B) {
	adapter := New("")
	vertexJSON := json.RawMessage(`{
		"id": "tv-bench",
		"type": "signing_session",
		"parentIds": ["p1", "p2"],
		"height": 10000,
		"epoch": 500,
		"timestamp": 1705323000,
		"status": "accepted",
		"sessionId": "session-bench",
		"data": {"threshold": 3, "messageHash": "0xbenchmark"}
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.ParseVertex(vertexJSON)
	}
}

// BenchmarkInferVertexType benchmarks type inference
func BenchmarkInferVertexType(b *testing.B) {
	adapter := New("")
	data := json.RawMessage(`{"threshold": 3, "messageHash": "0xabc"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = adapter.inferVertexType(data)
	}
}

// BenchmarkComputeThreshold benchmarks threshold computation
func BenchmarkComputeThreshold(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = ComputeThreshold(100)
	}
}
