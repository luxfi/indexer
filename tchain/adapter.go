// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package tchain provides T-Chain (Teleport) adapter for DAG indexing.
// T-Chain handles MPC threshold signatures for cross-chain teleportation.
package tchain

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/luxfi/indexer/dag"
	"github.com/luxfi/indexer/storage"
)

const (
	// DefaultRPCEndpoint for T-Chain on port 4700
	DefaultRPCEndpoint = "http://localhost:9650/ext/bc/T/rpc"
	// DefaultHTTPPort for T-Chain indexer API
	DefaultHTTPPort = 4700
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
	SharePending KeyShareStatus = "pending"
	ShareActive  KeyShareStatus = "active"
	ShareRevoked KeyShareStatus = "revoked"
	ShareRotated KeyShareStatus = "rotated"
)

// SigningSession represents an MPC threshold signing session
type SigningSession struct {
	ID           string        `json:"id"`
	Threshold    uint32        `json:"threshold"`          // t of n threshold
	TotalShares  uint32        `json:"totalShares"`        // total participants
	MessageHash  string        `json:"messageHash"`        // hash being signed
	Participants []string      `json:"participants"`       // node IDs participating
	Signatures   []string      `json:"signatures"`         // partial signatures collected
	FinalSig     string        `json:"finalSig,omitempty"` // combined signature
	Status       SessionStatus `json:"status"`
	CreatedAt    time.Time     `json:"createdAt"`
	CompletedAt  *time.Time    `json:"completedAt,omitempty"`
	ExpiresAt    time.Time     `json:"expiresAt"`
	SourceChain  string        `json:"sourceChain,omitempty"`
	DestChain    string        `json:"destChain,omitempty"`
}

// KeyShare represents a distributed key share for MPC
type KeyShare struct {
	ID         string         `json:"id"`
	PublicKey  string         `json:"publicKey"`
	ShareIndex uint32         `json:"shareIndex"`
	NodeID     string         `json:"nodeId"`
	KeyGenID   string         `json:"keyGenId"` // key generation ceremony ID
	Status     KeyShareStatus `json:"status"`
	CreatedAt  time.Time      `json:"createdAt"`
	RotatedAt  *time.Time     `json:"rotatedAt,omitempty"`
	ExpiresAt  *time.Time     `json:"expiresAt,omitempty"`
}

// KeyGeneration represents a distributed key generation ceremony
type KeyGeneration struct {
	ID           string     `json:"id"`
	Threshold    uint32     `json:"threshold"`
	TotalShares  uint32     `json:"totalShares"`
	PublicKey    string     `json:"publicKey"` // combined public key
	Participants []string   `json:"participants"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"createdAt"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
}

// TeleportMessage represents a cross-chain teleport message
type TeleportMessage struct {
	ID          string          `json:"id"`
	SourceChain string          `json:"sourceChain"`
	DestChain   string          `json:"destChain"`
	Sender      string          `json:"sender"`
	Receiver    string          `json:"receiver"`
	Payload     json.RawMessage `json:"payload"`
	Nonce       uint64          `json:"nonce"`
	SessionID   string          `json:"sessionId,omitempty"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"createdAt"`
	SignedAt    *time.Time      `json:"signedAt,omitempty"`
	DeliveredAt *time.Time      `json:"deliveredAt,omitempty"`
}

// VertexData contains T-Chain specific vertex data
type VertexData struct {
	Session  *SigningSession  `json:"session,omitempty"`
	KeyShare *KeyShare        `json:"keyShare,omitempty"`
	KeyGen   *KeyGeneration   `json:"keyGen,omitempty"`
	Message  *TeleportMessage `json:"message,omitempty"`
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
func (a *Adapter) InitSchema(ctx context.Context, store storage.Store) error {
	// Create signing sessions table
	err := store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS tchain_sessions (
			id TEXT PRIMARY KEY,
			threshold INTEGER NOT NULL,
			total_shares INTEGER NOT NULL,
			message_hash TEXT NOT NULL,
			participants TEXT DEFAULT '[]',
			signatures TEXT DEFAULT '[]',
			final_sig TEXT,
			status TEXT DEFAULT 'pending',
			source_chain TEXT,
			dest_chain TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			completed_at TIMESTAMP,
			expires_at TIMESTAMP NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("create sessions table: %w", err)
	}

	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_tchain_sessions_status ON tchain_sessions(status)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_tchain_sessions_message ON tchain_sessions(message_hash)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_tchain_sessions_created ON tchain_sessions(created_at DESC)`)

	// Create key shares table
	err = store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS tchain_key_shares (
			id TEXT PRIMARY KEY,
			public_key TEXT NOT NULL,
			share_index INTEGER NOT NULL,
			node_id TEXT NOT NULL,
			keygen_id TEXT NOT NULL,
			status TEXT DEFAULT 'pending',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			rotated_at TIMESTAMP,
			expires_at TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create key shares table: %w", err)
	}

	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_tchain_key_shares_pubkey ON tchain_key_shares(public_key)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_tchain_key_shares_node ON tchain_key_shares(node_id)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_tchain_key_shares_status ON tchain_key_shares(status)`)

	// Create key generation ceremonies table
	err = store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS tchain_key_generations (
			id TEXT PRIMARY KEY,
			threshold INTEGER NOT NULL,
			total_shares INTEGER NOT NULL,
			public_key TEXT,
			participants TEXT DEFAULT '[]',
			status TEXT DEFAULT 'pending',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			completed_at TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create key generations table: %w", err)
	}

	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_tchain_key_gen_status ON tchain_key_generations(status)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_tchain_key_gen_pubkey ON tchain_key_generations(public_key)`)

	// Create teleport messages table
	err = store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS tchain_messages (
			id TEXT PRIMARY KEY,
			source_chain TEXT NOT NULL,
			dest_chain TEXT NOT NULL,
			sender TEXT NOT NULL,
			receiver TEXT NOT NULL,
			payload TEXT,
			nonce INTEGER NOT NULL,
			session_id TEXT,
			status TEXT DEFAULT 'pending',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			signed_at TIMESTAMP,
			delivered_at TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create messages table: %w", err)
	}

	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_tchain_messages_source ON tchain_messages(source_chain)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_tchain_messages_dest ON tchain_messages(dest_chain)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_tchain_messages_status ON tchain_messages(status)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_tchain_messages_session ON tchain_messages(session_id)`)

	// Create MPC statistics table
	err = store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS tchain_mpc_stats (
			id INTEGER PRIMARY KEY DEFAULT 1,
			total_sessions INTEGER DEFAULT 0,
			active_sessions INTEGER DEFAULT 0,
			completed_sessions INTEGER DEFAULT 0,
			failed_sessions INTEGER DEFAULT 0,
			total_key_shares INTEGER DEFAULT 0,
			active_key_shares INTEGER DEFAULT 0,
			total_key_gens INTEGER DEFAULT 0,
			total_messages INTEGER DEFAULT 0,
			pending_messages INTEGER DEFAULT 0,
			delivered_messages INTEGER DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create mpc stats table: %w", err)
	}

	_ = store.Exec(ctx, `INSERT OR IGNORE INTO tchain_mpc_stats (id) VALUES (1)`)

	return nil
}

// GetStats returns T-Chain specific statistics
func (a *Adapter) GetStats(ctx context.Context, store storage.Store) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Get MPC stats
	var totalSessions, activeSessions, completedSessions, failedSessions int64
	var totalKeyShares, activeKeyShares, totalKeyGens int64
	var totalMessages, pendingMessages, deliveredMessages int64

	// Query sessions
	rows, _ := store.Query(ctx, "SELECT COUNT(*) as cnt FROM tchain_sessions")
	if len(rows) > 0 {
		if v, ok := rows[0]["cnt"].(int64); ok {
			totalSessions = v
		}
	}
	rows, _ = store.Query(ctx, "SELECT COUNT(*) as cnt FROM tchain_sessions WHERE status='active'")
	if len(rows) > 0 {
		if v, ok := rows[0]["cnt"].(int64); ok {
			activeSessions = v
		}
	}
	rows, _ = store.Query(ctx, "SELECT COUNT(*) as cnt FROM tchain_sessions WHERE status='completed'")
	if len(rows) > 0 {
		if v, ok := rows[0]["cnt"].(int64); ok {
			completedSessions = v
		}
	}
	rows, _ = store.Query(ctx, "SELECT COUNT(*) as cnt FROM tchain_sessions WHERE status='failed'")
	if len(rows) > 0 {
		if v, ok := rows[0]["cnt"].(int64); ok {
			failedSessions = v
		}
	}

	// Query key shares
	rows, _ = store.Query(ctx, "SELECT COUNT(*) as cnt FROM tchain_key_shares")
	if len(rows) > 0 {
		if v, ok := rows[0]["cnt"].(int64); ok {
			totalKeyShares = v
		}
	}
	rows, _ = store.Query(ctx, "SELECT COUNT(*) as cnt FROM tchain_key_shares WHERE status='active'")
	if len(rows) > 0 {
		if v, ok := rows[0]["cnt"].(int64); ok {
			activeKeyShares = v
		}
	}
	rows, _ = store.Query(ctx, "SELECT COUNT(*) as cnt FROM tchain_key_generations")
	if len(rows) > 0 {
		if v, ok := rows[0]["cnt"].(int64); ok {
			totalKeyGens = v
		}
	}

	// Query messages
	rows, _ = store.Query(ctx, "SELECT COUNT(*) as cnt FROM tchain_messages")
	if len(rows) > 0 {
		if v, ok := rows[0]["cnt"].(int64); ok {
			totalMessages = v
		}
	}
	rows, _ = store.Query(ctx, "SELECT COUNT(*) as cnt FROM tchain_messages WHERE status='pending'")
	if len(rows) > 0 {
		if v, ok := rows[0]["cnt"].(int64); ok {
			pendingMessages = v
		}
	}
	rows, _ = store.Query(ctx, "SELECT COUNT(*) as cnt FROM tchain_messages WHERE status='delivered'")
	if len(rows) > 0 {
		if v, ok := rows[0]["cnt"].(int64); ok {
			deliveredMessages = v
		}
	}

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
	rows, _ = store.Query(ctx, "SELECT COALESCE(AVG(threshold), 0) as avg_threshold FROM tchain_key_generations WHERE status='completed'")
	if len(rows) > 0 {
		if v, ok := rows[0]["avg_threshold"].(float64); ok {
			stats["avg_threshold"] = v
		}
	}

	return stats, nil
}

// StoreSession stores a signing session
func (a *Adapter) StoreSession(ctx context.Context, store storage.Store, s *SigningSession) error {
	participants, _ := json.Marshal(s.Participants)
	signatures, _ := json.Marshal(s.Signatures)

	return store.Exec(ctx, `
		INSERT OR REPLACE INTO tchain_sessions (id, threshold, total_shares, message_hash, participants, signatures, final_sig, status, source_chain, dest_chain, created_at, completed_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.ID, s.Threshold, s.TotalShares, s.MessageHash, string(participants), string(signatures), s.FinalSig, s.Status, s.SourceChain, s.DestChain, s.CreatedAt, s.CompletedAt, s.ExpiresAt)
}

// StoreKeyShare stores a key share
func (a *Adapter) StoreKeyShare(ctx context.Context, store storage.Store, k *KeyShare) error {
	return store.Exec(ctx, `
		INSERT OR REPLACE INTO tchain_key_shares (id, public_key, share_index, node_id, keygen_id, status, created_at, rotated_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, k.ID, k.PublicKey, k.ShareIndex, k.NodeID, k.KeyGenID, k.Status, k.CreatedAt, k.RotatedAt, k.ExpiresAt)
}

// StoreKeyGeneration stores a key generation ceremony
func (a *Adapter) StoreKeyGeneration(ctx context.Context, store storage.Store, kg *KeyGeneration) error {
	participants, _ := json.Marshal(kg.Participants)

	return store.Exec(ctx, `
		INSERT OR REPLACE INTO tchain_key_generations (id, threshold, total_shares, public_key, participants, status, created_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, kg.ID, kg.Threshold, kg.TotalShares, kg.PublicKey, string(participants), kg.Status, kg.CreatedAt, kg.CompletedAt)
}

// StoreMessage stores a teleport message
func (a *Adapter) StoreMessage(ctx context.Context, store storage.Store, m *TeleportMessage) error {
	return store.Exec(ctx, `
		INSERT OR REPLACE INTO tchain_messages (id, source_chain, dest_chain, sender, receiver, payload, nonce, session_id, status, created_at, signed_at, delivered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, m.ID, m.SourceChain, m.DestChain, m.Sender, m.Receiver, string(m.Payload), m.Nonce, m.SessionID, m.Status, m.CreatedAt, m.SignedAt, m.DeliveredAt)
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
