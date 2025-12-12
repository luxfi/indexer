// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package tchain provides T-Chain (Teleport) adapter for DAG indexing.
// T-Chain handles MPC threshold signatures for cross-chain teleportation.
package tchain

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/luxfi/indexer/dag"
)

const (
	// DefaultRPCEndpoint for T-Chain on port 4700
	DefaultRPCEndpoint = "http://localhost:9630/ext/bc/T/rpc"
	// DefaultHTTPPort for T-Chain indexer API
	DefaultHTTPPort = 4700
	// DefaultDatabaseURL for T-Chain explorer
	DefaultDatabaseURL = "postgres://blockscout:blockscout@localhost:5432/explorer_tchain?sslmode=disable"
)

// SessionStatus for MPC signing sessions
type SessionStatus string

const (
	SessionPending   SessionStatus = "pending"
	SessionActive    SessionStatus = "active"
	SessionCompleted SessionStatus = "completed"
	SessionFailed    SessionStatus = "failed"
	SessionExpired   SessionStatus = "expired"
)

// KeyShareStatus for distributed key shares
type KeyShareStatus string

const (
	SharePending  KeyShareStatus = "pending"
	ShareActive   KeyShareStatus = "active"
	ShareRevoked  KeyShareStatus = "revoked"
	ShareRotated  KeyShareStatus = "rotated"
)

// SigningSession represents an MPC threshold signing session
type SigningSession struct {
	ID            string        `json:"id"`
	Threshold     uint32        `json:"threshold"`     // t of n threshold
	TotalShares   uint32        `json:"totalShares"`   // total participants
	MessageHash   string        `json:"messageHash"`   // hash being signed
	Participants  []string      `json:"participants"`  // node IDs participating
	Signatures    []string      `json:"signatures"`    // partial signatures collected
	FinalSig      string        `json:"finalSig,omitempty"` // combined signature
	Status        SessionStatus `json:"status"`
	CreatedAt     time.Time     `json:"createdAt"`
	CompletedAt   *time.Time    `json:"completedAt,omitempty"`
	ExpiresAt     time.Time     `json:"expiresAt"`
	SourceChain   string        `json:"sourceChain,omitempty"`
	DestChain     string        `json:"destChain,omitempty"`
}

// KeyShare represents a distributed key share for MPC
type KeyShare struct {
	ID          string         `json:"id"`
	PublicKey   string         `json:"publicKey"`
	ShareIndex  uint32         `json:"shareIndex"`
	NodeID      string         `json:"nodeId"`
	KeyGenID    string         `json:"keyGenId"`    // key generation ceremony ID
	Status      KeyShareStatus `json:"status"`
	CreatedAt   time.Time      `json:"createdAt"`
	RotatedAt   *time.Time     `json:"rotatedAt,omitempty"`
	ExpiresAt   *time.Time     `json:"expiresAt,omitempty"`
}

// KeyGeneration represents a distributed key generation ceremony
type KeyGeneration struct {
	ID            string    `json:"id"`
	Threshold     uint32    `json:"threshold"`
	TotalShares   uint32    `json:"totalShares"`
	PublicKey     string    `json:"publicKey"`     // combined public key
	Participants  []string  `json:"participants"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
}

// TeleportMessage represents a cross-chain teleport message
type TeleportMessage struct {
	ID            string          `json:"id"`
	SourceChain   string          `json:"sourceChain"`
	DestChain     string          `json:"destChain"`
	Sender        string          `json:"sender"`
	Receiver      string          `json:"receiver"`
	Payload       json.RawMessage `json:"payload"`
	Nonce         uint64          `json:"nonce"`
	SessionID     string          `json:"sessionId,omitempty"`
	Status        string          `json:"status"`
	CreatedAt     time.Time       `json:"createdAt"`
	SignedAt      *time.Time      `json:"signedAt,omitempty"`
	DeliveredAt   *time.Time      `json:"deliveredAt,omitempty"`
}

// VertexData contains T-Chain specific vertex data
type VertexData struct {
	Session     *SigningSession  `json:"session,omitempty"`
	KeyShare    *KeyShare        `json:"keyShare,omitempty"`
	KeyGen      *KeyGeneration   `json:"keyGen,omitempty"`
	Message     *TeleportMessage `json:"message,omitempty"`
}

// Adapter implements dag.Adapter for T-Chain MPC operations
type Adapter struct {
	rpcEndpoint string
	httpClient  *http.Client
}

// Compile-time interface compliance check
var _ dag.Adapter = (*Adapter)(nil)

// NewAdapter creates a new T-Chain adapter
func New(rpcEndpoint string) *Adapter {
	if rpcEndpoint == "" {
		rpcEndpoint = DefaultRPCEndpoint
	}
	return &Adapter{
		rpcEndpoint: rpcEndpoint,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// DefaultConfig returns default configuration for T-Chain indexer
func DefaultConfig() dag.Config {
	return dag.Config{
		ChainType:    dag.ChainT,
		ChainName:    "T-Chain (Teleport)",
		RPCEndpoint:  DefaultRPCEndpoint,
		RPCMethod:    "tvm",
		DatabaseURL:  DefaultDatabaseURL,
		HTTPPort:     DefaultHTTPPort,
		PollInterval: 2 * time.Second,
	}
}

// ParseVertex parses T-Chain vertex from RPC response
func (a *Adapter) ParseVertex(data json.RawMessage) (*dag.Vertex, error) {
	var raw struct {
		ID        string          `json:"id"`
		Type      string          `json:"type"`
		ParentIDs []string        `json:"parentIds"`
		Height    uint64          `json:"height"`
		Epoch     uint32          `json:"epoch"`
		Timestamp int64           `json:"timestamp"`
		Status    string          `json:"status"`
		Data      json.RawMessage `json:"data"`
		// MPC-specific fields
		SessionID string `json:"sessionId,omitempty"`
		KeyGenID  string `json:"keyGenId,omitempty"`
		MessageID string `json:"messageId,omitempty"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse vertex: %w", err)
	}

	v := &dag.Vertex{
		ID:        raw.ID,
		Type:      raw.Type,
		ParentIDs: raw.ParentIDs,
		Height:    raw.Height,
		Epoch:     raw.Epoch,
		Timestamp: time.Unix(raw.Timestamp, 0),
		Status:    dag.Status(raw.Status),
		Data:      raw.Data,
		Metadata:  make(map[string]interface{}),
	}

	// Determine vertex type from content
	if v.Type == "" {
		v.Type = a.inferVertexType(raw.Data)
	}

	// Add MPC-specific metadata
	if raw.SessionID != "" {
		v.Metadata["sessionId"] = raw.SessionID
	}
	if raw.KeyGenID != "" {
		v.Metadata["keyGenId"] = raw.KeyGenID
	}
	if raw.MessageID != "" {
		v.Metadata["messageId"] = raw.MessageID
	}

	return v, nil
}

// inferVertexType determines vertex type from data content
func (a *Adapter) inferVertexType(data json.RawMessage) string {
	var probe map[string]interface{}
	if err := json.Unmarshal(data, &probe); err != nil {
		return "unknown"
	}

	if _, ok := probe["threshold"]; ok {
		if _, ok := probe["messageHash"]; ok {
			return "signing_session"
		}
		if _, ok := probe["publicKey"]; ok {
			return "key_generation"
		}
	}
	if _, ok := probe["shareIndex"]; ok {
		return "key_share"
	}
	if _, ok := probe["sourceChain"]; ok {
		return "teleport_message"
	}
	return "unknown"
}

// GetRecentVertices fetches recent vertices via RPC
func (a *Adapter) GetRecentVertices(ctx context.Context, limit int) ([]json.RawMessage, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tvm.getRecentVertices",
		"params": map[string]interface{}{
			"limit": limit,
		},
	}

	resp, err := a.rpcCall(ctx, req)
	if err != nil {
		return nil, err
	}

	var result struct {
		Vertices []json.RawMessage `json:"vertices"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse vertices: %w", err)
	}

	return result.Vertices, nil
}

// GetVertexByID fetches a specific vertex
func (a *Adapter) GetVertexByID(ctx context.Context, id string) (json.RawMessage, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tvm.getVertex",
		"params": map[string]interface{}{
			"id": id,
		},
	}

	resp, err := a.rpcCall(ctx, req)
	if err != nil {
		return nil, err
	}

	var result struct {
		Vertex json.RawMessage `json:"vertex"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse vertex: %w", err)
	}

	return result.Vertex, nil
}

// GetSigningSession fetches a signing session by ID
func (a *Adapter) GetSigningSession(ctx context.Context, sessionID string) (*SigningSession, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tvm.getSigningSession",
		"params": map[string]interface{}{
			"sessionId": sessionID,
		},
	}

	resp, err := a.rpcCall(ctx, req)
	if err != nil {
		return nil, err
	}

	var session SigningSession
	if err := json.Unmarshal(resp, &session); err != nil {
		return nil, fmt.Errorf("parse session: %w", err)
	}

	return &session, nil
}

// GetKeyGeneration fetches a key generation ceremony by ID
func (a *Adapter) GetKeyGeneration(ctx context.Context, keyGenID string) (*KeyGeneration, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tvm.getKeyGeneration",
		"params": map[string]interface{}{
			"keyGenId": keyGenID,
		},
	}

	resp, err := a.rpcCall(ctx, req)
	if err != nil {
		return nil, err
	}

	var keyGen KeyGeneration
	if err := json.Unmarshal(resp, &keyGen); err != nil {
		return nil, fmt.Errorf("parse keygen: %w", err)
	}

	return &keyGen, nil
}

// GetActiveKeyShares fetches active key shares for a public key
func (a *Adapter) GetActiveKeyShares(ctx context.Context, publicKey string) ([]KeyShare, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tvm.getActiveKeyShares",
		"params": map[string]interface{}{
			"publicKey": publicKey,
		},
	}

	resp, err := a.rpcCall(ctx, req)
	if err != nil {
		return nil, err
	}

	var result struct {
		KeyShares []KeyShare `json:"keyShares"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse key shares: %w", err)
	}

	return result.KeyShares, nil
}

// InitSchema creates T-Chain specific database tables
func (a *Adapter) InitSchema(db *sql.DB) error {
	schema := `
		-- Signing sessions table
		CREATE TABLE IF NOT EXISTS tchain_sessions (
			id TEXT PRIMARY KEY,
			threshold INT NOT NULL,
			total_shares INT NOT NULL,
			message_hash TEXT NOT NULL,
			participants JSONB DEFAULT '[]',
			signatures JSONB DEFAULT '[]',
			final_sig TEXT,
			status TEXT DEFAULT 'pending',
			source_chain TEXT,
			dest_chain TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			completed_at TIMESTAMPTZ,
			expires_at TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_tchain_sessions_status ON tchain_sessions(status);
		CREATE INDEX IF NOT EXISTS idx_tchain_sessions_message ON tchain_sessions(message_hash);
		CREATE INDEX IF NOT EXISTS idx_tchain_sessions_created ON tchain_sessions(created_at DESC);

		-- Key shares table
		CREATE TABLE IF NOT EXISTS tchain_key_shares (
			id TEXT PRIMARY KEY,
			public_key TEXT NOT NULL,
			share_index INT NOT NULL,
			node_id TEXT NOT NULL,
			keygen_id TEXT NOT NULL,
			status TEXT DEFAULT 'pending',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			rotated_at TIMESTAMPTZ,
			expires_at TIMESTAMPTZ
		);
		CREATE INDEX IF NOT EXISTS idx_tchain_key_shares_pubkey ON tchain_key_shares(public_key);
		CREATE INDEX IF NOT EXISTS idx_tchain_key_shares_node ON tchain_key_shares(node_id);
		CREATE INDEX IF NOT EXISTS idx_tchain_key_shares_status ON tchain_key_shares(status);

		-- Key generation ceremonies table
		CREATE TABLE IF NOT EXISTS tchain_key_generations (
			id TEXT PRIMARY KEY,
			threshold INT NOT NULL,
			total_shares INT NOT NULL,
			public_key TEXT,
			participants JSONB DEFAULT '[]',
			status TEXT DEFAULT 'pending',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			completed_at TIMESTAMPTZ
		);
		CREATE INDEX IF NOT EXISTS idx_tchain_key_gen_status ON tchain_key_generations(status);
		CREATE INDEX IF NOT EXISTS idx_tchain_key_gen_pubkey ON tchain_key_generations(public_key);

		-- Teleport messages table
		CREATE TABLE IF NOT EXISTS tchain_messages (
			id TEXT PRIMARY KEY,
			source_chain TEXT NOT NULL,
			dest_chain TEXT NOT NULL,
			sender TEXT NOT NULL,
			receiver TEXT NOT NULL,
			payload JSONB,
			nonce BIGINT NOT NULL,
			session_id TEXT,
			status TEXT DEFAULT 'pending',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			signed_at TIMESTAMPTZ,
			delivered_at TIMESTAMPTZ
		);
		CREATE INDEX IF NOT EXISTS idx_tchain_messages_source ON tchain_messages(source_chain);
		CREATE INDEX IF NOT EXISTS idx_tchain_messages_dest ON tchain_messages(dest_chain);
		CREATE INDEX IF NOT EXISTS idx_tchain_messages_status ON tchain_messages(status);
		CREATE INDEX IF NOT EXISTS idx_tchain_messages_session ON tchain_messages(session_id);

		-- MPC statistics table
		CREATE TABLE IF NOT EXISTS tchain_mpc_stats (
			id INT PRIMARY KEY DEFAULT 1,
			total_sessions BIGINT DEFAULT 0,
			active_sessions BIGINT DEFAULT 0,
			completed_sessions BIGINT DEFAULT 0,
			failed_sessions BIGINT DEFAULT 0,
			total_key_shares BIGINT DEFAULT 0,
			active_key_shares BIGINT DEFAULT 0,
			total_key_gens BIGINT DEFAULT 0,
			total_messages BIGINT DEFAULT 0,
			pending_messages BIGINT DEFAULT 0,
			delivered_messages BIGINT DEFAULT 0,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		INSERT INTO tchain_mpc_stats (id) VALUES (1) ON CONFLICT DO NOTHING;
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("init tchain schema: %w", err)
	}

	return nil
}

// GetStats returns T-Chain specific statistics
func (a *Adapter) GetStats(ctx context.Context, db *sql.DB) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Get MPC stats
	var totalSessions, activeSessions, completedSessions, failedSessions int64
	var totalKeyShares, activeKeyShares, totalKeyGens int64
	var totalMessages, pendingMessages, deliveredMessages int64

	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tchain_sessions").Scan(&totalSessions)
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tchain_sessions WHERE status='active'").Scan(&activeSessions)
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tchain_sessions WHERE status='completed'").Scan(&completedSessions)
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tchain_sessions WHERE status='failed'").Scan(&failedSessions)
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tchain_key_shares").Scan(&totalKeyShares)
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tchain_key_shares WHERE status='active'").Scan(&activeKeyShares)
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tchain_key_generations").Scan(&totalKeyGens)
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tchain_messages").Scan(&totalMessages)
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tchain_messages WHERE status='pending'").Scan(&pendingMessages)
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tchain_messages WHERE status='delivered'").Scan(&deliveredMessages)

	stats["mpc_stats"] = map[string]interface{}{
		"total_sessions":     totalSessions,
		"active_sessions":    activeSessions,
		"completed_sessions": completedSessions,
		"failed_sessions":    failedSessions,
	}

	stats["key_stats"] = map[string]interface{}{
		"total_key_shares":  totalKeyShares,
		"active_key_shares": activeKeyShares,
		"total_key_gens":    totalKeyGens,
	}

	stats["message_stats"] = map[string]interface{}{
		"total_messages":     totalMessages,
		"pending_messages":   pendingMessages,
		"delivered_messages": deliveredMessages,
	}

	// Calculate signing success rate
	if totalSessions > 0 {
		stats["signing_success_rate"] = float64(completedSessions) / float64(totalSessions) * 100.0
	}

	// Get average threshold
	var avgThreshold float64
	_ = db.QueryRowContext(ctx, "SELECT COALESCE(AVG(threshold), 0) FROM tchain_key_generations WHERE status='completed'").Scan(&avgThreshold)
	stats["avg_threshold"] = avgThreshold

	return stats, nil
}

// StoreSession stores a signing session
func (a *Adapter) StoreSession(ctx context.Context, db *sql.DB, s *SigningSession) error {
	participants, _ := json.Marshal(s.Participants)
	signatures, _ := json.Marshal(s.Signatures)

	_, err := db.ExecContext(ctx, `
		INSERT INTO tchain_sessions (id, threshold, total_shares, message_hash, participants, signatures, final_sig, status, source_chain, dest_chain, created_at, completed_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (id) DO UPDATE SET 
			signatures = EXCLUDED.signatures,
			final_sig = EXCLUDED.final_sig,
			status = EXCLUDED.status,
			completed_at = EXCLUDED.completed_at
	`, s.ID, s.Threshold, s.TotalShares, s.MessageHash, participants, signatures, s.FinalSig, s.Status, s.SourceChain, s.DestChain, s.CreatedAt, s.CompletedAt, s.ExpiresAt)

	return err
}

// StoreKeyShare stores a key share
func (a *Adapter) StoreKeyShare(ctx context.Context, db *sql.DB, k *KeyShare) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO tchain_key_shares (id, public_key, share_index, node_id, keygen_id, status, created_at, rotated_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET 
			status = EXCLUDED.status,
			rotated_at = EXCLUDED.rotated_at
	`, k.ID, k.PublicKey, k.ShareIndex, k.NodeID, k.KeyGenID, k.Status, k.CreatedAt, k.RotatedAt, k.ExpiresAt)

	return err
}

// StoreKeyGeneration stores a key generation ceremony
func (a *Adapter) StoreKeyGeneration(ctx context.Context, db *sql.DB, kg *KeyGeneration) error {
	participants, _ := json.Marshal(kg.Participants)

	_, err := db.ExecContext(ctx, `
		INSERT INTO tchain_key_generations (id, threshold, total_shares, public_key, participants, status, created_at, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET 
			public_key = EXCLUDED.public_key,
			status = EXCLUDED.status,
			completed_at = EXCLUDED.completed_at
	`, kg.ID, kg.Threshold, kg.TotalShares, kg.PublicKey, participants, kg.Status, kg.CreatedAt, kg.CompletedAt)

	return err
}

// StoreMessage stores a teleport message
func (a *Adapter) StoreMessage(ctx context.Context, db *sql.DB, m *TeleportMessage) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO tchain_messages (id, source_chain, dest_chain, sender, receiver, payload, nonce, session_id, status, created_at, signed_at, delivered_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO UPDATE SET 
			session_id = EXCLUDED.session_id,
			status = EXCLUDED.status,
			signed_at = EXCLUDED.signed_at,
			delivered_at = EXCLUDED.delivered_at
	`, m.ID, m.SourceChain, m.DestChain, m.Sender, m.Receiver, m.Payload, m.Nonce, m.SessionID, m.Status, m.CreatedAt, m.SignedAt, m.DeliveredAt)

	return err
}

// rpcCall makes an RPC call to T-Chain
func (a *Adapter) rpcCall(ctx context.Context, req map[string]interface{}) (json.RawMessage, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.rpcEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("rpc call: %w", err)
	}
	defer resp.Body.Close()

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// ValidateSignature validates a threshold signature
func ValidateSignature(publicKey, message, signature string) (bool, error) {
	// Decode hex strings
	pubKeyBytes, err := hex.DecodeString(publicKey)
	if err != nil {
		return false, fmt.Errorf("decode public key: %w", err)
	}
	msgBytes, err := hex.DecodeString(message)
	if err != nil {
		return false, fmt.Errorf("decode message: %w", err)
	}
	sigBytes, err := hex.DecodeString(signature)
	if err != nil {
		return false, fmt.Errorf("decode signature: %w", err)
	}

	// Basic length validation (actual crypto verification would use luxfi/crypto)
	if len(pubKeyBytes) < 32 || len(sigBytes) < 64 {
		return false, fmt.Errorf("invalid key or signature length")
	}

	// Placeholder - actual verification uses threshold signature scheme
	_ = msgBytes
	return true, nil
}

// ComputeThreshold computes required threshold for given parameters
// Uses t-of-n threshold where t = (n * 2 / 3) + 1 for Byzantine fault tolerance
func ComputeThreshold(totalShares uint32) uint32 {
	if totalShares < 2 {
		return 1
	}
	return (totalShares * 2 / 3) + 1
}
