// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package zchain provides the Z-Chain (Privacy) adapter for DAG indexing.
// Z-Chain uses DAG consensus for fast finality combined with ZK proofs for privacy.
package zchain

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
	// DefaultRPCEndpoint for Z-Chain
	DefaultRPCEndpoint = "http://localhost:9650/ext/bc/Z/rpc"
	// DefaultHTTPPort for Z-Chain indexer API
	DefaultHTTPPort = 4400
	// DefaultDatabase for Z-Chain explorer
	DefaultDatabase = "explorer_zchain"
	// RPCMethod prefix for Z-Chain
	RPCMethod = "zvm"
)

// ZKTransactionType identifies privacy transaction types
type ZKTransactionType string

const (
	TxShieldedTransfer ZKTransactionType = "shielded_transfer"
	TxShield           ZKTransactionType = "shield"   // Deposit: transparent -> shielded
	TxUnshield         ZKTransactionType = "unshield" // Withdraw: shielded -> transparent
	TxJoinSplit        ZKTransactionType = "joinsplit"
	TxMint             ZKTransactionType = "mint"
	TxBurn             ZKTransactionType = "burn"
)

// ProofType identifies ZK proof systems
type ProofType string

const (
	ProofGroth16 ProofType = "groth16"
	ProofPlonk   ProofType = "plonk"
	ProofSTARK   ProofType = "stark"
	ProofBullet  ProofType = "bulletproof"
	ProofHalo2   ProofType = "halo2"
	ProofFRI     ProofType = "fri" // Fast Reed-Solomon IOP
)

// ZKProof represents a zero-knowledge proof
type ZKProof struct {
	Type         ProofType `json:"type"`
	Data         string    `json:"data"` // Hex-encoded proof bytes
	PublicInputs []string  `json:"publicInputs,omitempty"`
	VerifyingKey string    `json:"verifyingKey,omitempty"`
}

// Nullifier represents a spent note marker (prevents double-spending)
type Nullifier struct {
	Hash    string `json:"hash"`
	TxID    string `json:"txId"`
	Index   int    `json:"index"`
	SpentAt int64  `json:"spentAt,omitempty"` // Vertex height
}

// Commitment represents a shielded note commitment
type Commitment struct {
	Hash      string `json:"hash"`
	TxID      string `json:"txId"`
	Index     int    `json:"index"`
	CreatedAt int64  `json:"createdAt,omitempty"` // Vertex height
	Spent     bool   `json:"spent"`
}

// ShieldedTransfer represents a private transfer
type ShieldedTransfer struct {
	TxID         string            `json:"txId"`
	Type         ZKTransactionType `json:"type"`
	Proof        ZKProof           `json:"proof"`
	Nullifiers   []Nullifier       `json:"nullifiers"`
	Commitments  []Commitment      `json:"commitments"`
	ValueBalance int64             `json:"valueBalance,omitempty"` // Net flow for shield/unshield
	Fee          uint64            `json:"fee"`
	Memo         string            `json:"memo,omitempty"` // Encrypted memo
	Timestamp    time.Time         `json:"timestamp"`
}

// VertexData holds Z-Chain specific vertex payload
type VertexData struct {
	Transfers     []ShieldedTransfer `json:"transfers,omitempty"`
	MerkleRoot    string             `json:"merkleRoot"`    // Commitment tree root
	NullifierRoot string             `json:"nullifierRoot"` // Nullifier set root
	Epoch         uint32             `json:"epoch"`
	Proposer      string             `json:"proposer,omitempty"`
}

// RPCRequest for JSON-RPC calls
type RPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params,omitempty"`
}

// RPCResponse from JSON-RPC calls
type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError in JSON-RPC response
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Adapter implements dag.Adapter for Z-Chain
type Adapter struct {
	rpcEndpoint string
	httpClient  *http.Client
}

// New creates a new Z-Chain adapter
func New(rpcEndpoint string) *Adapter {
	if rpcEndpoint == "" {
		rpcEndpoint = DefaultRPCEndpoint
	}
	return &Adapter{
		rpcEndpoint: rpcEndpoint,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// ParseVertex parses RPC response into a DAG vertex
func (a *Adapter) ParseVertex(data json.RawMessage) (*dag.Vertex, error) {
	var raw struct {
		ID            string          `json:"id"`
		ParentIDs     []string        `json:"parentIds"`
		Height        uint64          `json:"height"`
		Epoch         uint32          `json:"epoch"`
		Timestamp     int64           `json:"timestamp"`
		Status        string          `json:"status"`
		MerkleRoot    string          `json:"merkleRoot"`
		NullifierRoot string          `json:"nullifierRoot"`
		Transfers     json.RawMessage `json:"transfers"`
		Proposer      string          `json:"proposer"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse vertex: %w", err)
	}

	// Parse transfers
	var transfers []ShieldedTransfer
	if len(raw.Transfers) > 0 {
		if err := json.Unmarshal(raw.Transfers, &transfers); err != nil {
			// Transfers may be empty or malformed; continue
			transfers = nil
		}
	}

	// Build vertex data
	vd := VertexData{
		Transfers:     transfers,
		MerkleRoot:    raw.MerkleRoot,
		NullifierRoot: raw.NullifierRoot,
		Epoch:         raw.Epoch,
		Proposer:      raw.Proposer,
	}
	vdBytes, _ := json.Marshal(vd)

	// Extract transaction IDs
	var txIDs []string
	for _, t := range transfers {
		txIDs = append(txIDs, t.TxID)
	}

	// Map status
	status := dag.StatusPending
	switch raw.Status {
	case "accepted", "Accepted":
		status = dag.StatusAccepted
	case "rejected", "Rejected":
		status = dag.StatusRejected
	}

	return &dag.Vertex{
		ID:        raw.ID,
		Type:      "zk_vertex",
		ParentIDs: raw.ParentIDs,
		Height:    raw.Height,
		Epoch:     raw.Epoch,
		TxIDs:     txIDs,
		Timestamp: time.Unix(raw.Timestamp, 0),
		Status:    status,
		Data:      vdBytes,
		Metadata: map[string]interface{}{
			"merkleRoot":    raw.MerkleRoot,
			"nullifierRoot": raw.NullifierRoot,
			"transferCount": len(transfers),
		},
	}, nil
}

// GetRecentVertices fetches recent vertices from RPC
func (a *Adapter) GetRecentVertices(ctx context.Context, limit int) ([]json.RawMessage, error) {
	req := RPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  fmt.Sprintf("%s.getRecentVertices", RPCMethod),
		Params:  []interface{}{limit},
	}

	resp, err := a.rpcCall(ctx, req)
	if err != nil {
		return nil, err
	}

	var vertices []json.RawMessage
	if err := json.Unmarshal(resp.Result, &vertices); err != nil {
		return nil, fmt.Errorf("parse vertices: %w", err)
	}
	return vertices, nil
}

// GetVertexByID fetches a specific vertex
func (a *Adapter) GetVertexByID(ctx context.Context, id string) (json.RawMessage, error) {
	req := RPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  fmt.Sprintf("%s.getVertex", RPCMethod),
		Params:  []interface{}{id},
	}

	resp, err := a.rpcCall(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Result, nil
}

// GetNullifier fetches nullifier status
func (a *Adapter) GetNullifier(ctx context.Context, hash string) (*Nullifier, error) {
	req := RPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  fmt.Sprintf("%s.getNullifier", RPCMethod),
		Params:  []interface{}{hash},
	}

	resp, err := a.rpcCall(ctx, req)
	if err != nil {
		return nil, err
	}

	var n Nullifier
	if err := json.Unmarshal(resp.Result, &n); err != nil {
		return nil, fmt.Errorf("parse nullifier: %w", err)
	}
	return &n, nil
}

// GetCommitment fetches commitment status
func (a *Adapter) GetCommitment(ctx context.Context, hash string) (*Commitment, error) {
	req := RPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  fmt.Sprintf("%s.getCommitment", RPCMethod),
		Params:  []interface{}{hash},
	}

	resp, err := a.rpcCall(ctx, req)
	if err != nil {
		return nil, err
	}

	var c Commitment
	if err := json.Unmarshal(resp.Result, &c); err != nil {
		return nil, fmt.Errorf("parse commitment: %w", err)
	}
	return &c, nil
}

// GetMerkleRoot fetches the current commitment tree root
func (a *Adapter) GetMerkleRoot(ctx context.Context) (string, error) {
	req := RPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  fmt.Sprintf("%s.getMerkleRoot", RPCMethod),
	}

	resp, err := a.rpcCall(ctx, req)
	if err != nil {
		return "", err
	}

	var root string
	if err := json.Unmarshal(resp.Result, &root); err != nil {
		return "", fmt.Errorf("parse merkle root: %w", err)
	}
	return root, nil
}

// VerifyProof verifies a ZK proof on-chain
func (a *Adapter) VerifyProof(ctx context.Context, proof ZKProof) (bool, error) {
	req := RPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  fmt.Sprintf("%s.verifyProof", RPCMethod),
		Params:  []interface{}{proof},
	}

	resp, err := a.rpcCall(ctx, req)
	if err != nil {
		return false, err
	}

	var valid bool
	if err := json.Unmarshal(resp.Result, &valid); err != nil {
		return false, fmt.Errorf("parse verify result: %w", err)
	}
	return valid, nil
}

// InitSchema creates Z-Chain specific database tables
func (a *Adapter) InitSchema(ctx context.Context, store storage.Store) error {
	// Create nullifiers table
	err := store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS zchain_nullifiers (
			hash TEXT PRIMARY KEY,
			tx_id TEXT NOT NULL,
			idx INTEGER NOT NULL,
			spent_at INTEGER,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create nullifiers table: %w", err)
	}

	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_zchain_nullifiers_tx ON zchain_nullifiers(tx_id)`)

	// Create commitments table
	err = store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS zchain_commitments (
			hash TEXT PRIMARY KEY,
			tx_id TEXT NOT NULL,
			idx INTEGER NOT NULL,
			created_at INTEGER,
			spent INTEGER DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create commitments table: %w", err)
	}

	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_zchain_commitments_tx ON zchain_commitments(tx_id)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_zchain_commitments_spent ON zchain_commitments(spent)`)

	// Create shielded transfers table
	err = store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS zchain_transfers (
			tx_id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			proof_type TEXT NOT NULL,
			proof_data TEXT NOT NULL,
			value_balance INTEGER DEFAULT 0,
			fee INTEGER NOT NULL,
			memo TEXT,
			timestamp TIMESTAMP NOT NULL,
			vertex_id TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create transfers table: %w", err)
	}

	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_zchain_transfers_type ON zchain_transfers(type)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_zchain_transfers_timestamp ON zchain_transfers(timestamp DESC)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_zchain_transfers_vertex ON zchain_transfers(vertex_id)`)

	// Create ZK proofs archive table
	err = store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS zchain_proofs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tx_id TEXT NOT NULL,
			proof_type TEXT NOT NULL,
			proof_data TEXT NOT NULL,
			public_inputs TEXT,
			verifying_key TEXT,
			verified INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create proofs table: %w", err)
	}

	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_zchain_proofs_tx ON zchain_proofs(tx_id)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_zchain_proofs_type ON zchain_proofs(proof_type)`)

	// Create merkle tree state table
	err = store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS zchain_merkle_state (
			id INTEGER PRIMARY KEY DEFAULT 1,
			root TEXT NOT NULL,
			height INTEGER NOT NULL,
			leaf_count INTEGER NOT NULL,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create merkle state table: %w", err)
	}

	_ = store.Exec(ctx, `INSERT OR IGNORE INTO zchain_merkle_state (id, root, height, leaf_count) VALUES (1, '', 0, 0)`)

	// Create extended stats table
	err = store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS zchain_extended_stats (
			id INTEGER PRIMARY KEY DEFAULT 1,
			total_nullifiers INTEGER DEFAULT 0,
			total_commitments INTEGER DEFAULT 0,
			total_transfers INTEGER DEFAULT 0,
			shielded_volume INTEGER DEFAULT 0,
			shield_count INTEGER DEFAULT 0,
			unshield_count INTEGER DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create extended stats table: %w", err)
	}

	_ = store.Exec(ctx, `INSERT OR IGNORE INTO zchain_extended_stats (id) VALUES (1)`)

	return nil
}

// GetStats returns Z-Chain specific statistics
func (a *Adapter) GetStats(ctx context.Context, store storage.Store) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Fetch extended stats
	var totalNullifiers, totalCommitments, totalTransfers int64
	var shieldedVolume, shieldCount, unshieldCount int64

	rows, err := store.Query(ctx, `
		SELECT total_nullifiers, total_commitments, total_transfers,
		       shielded_volume, shield_count, unshield_count
		FROM zchain_extended_stats WHERE id=1
	`)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		row := rows[0]
		if v, ok := row["total_nullifiers"].(int64); ok {
			totalNullifiers = v
		}
		if v, ok := row["total_commitments"].(int64); ok {
			totalCommitments = v
		}
		if v, ok := row["total_transfers"].(int64); ok {
			totalTransfers = v
		}
		if v, ok := row["shielded_volume"].(int64); ok {
			shieldedVolume = v
		}
		if v, ok := row["shield_count"].(int64); ok {
			shieldCount = v
		}
		if v, ok := row["unshield_count"].(int64); ok {
			unshieldCount = v
		}
	}

	// Fetch merkle state
	var merkleRoot string
	var merkleHeight, leafCount int64
	rows, _ = store.Query(ctx, `SELECT root, height, leaf_count FROM zchain_merkle_state WHERE id=1`)
	if len(rows) > 0 {
		row := rows[0]
		if v, ok := row["root"].(string); ok {
			merkleRoot = v
		}
		if v, ok := row["height"].(int64); ok {
			merkleHeight = v
		}
		if v, ok := row["leaf_count"].(int64); ok {
			leafCount = v
		}
	}

	// Count unspent commitments
	var unspentCommitments int64
	rows, _ = store.Query(ctx, `SELECT COUNT(*) as cnt FROM zchain_commitments WHERE spent=0`)
	if len(rows) > 0 {
		if v, ok := rows[0]["cnt"].(int64); ok {
			unspentCommitments = v
		}
	}

	stats["nullifiers"] = map[string]interface{}{
		"total": totalNullifiers,
	}
	stats["commitments"] = map[string]interface{}{
		"total":   totalCommitments,
		"unspent": unspentCommitments,
	}
	stats["transfers"] = map[string]interface{}{
		"total":          totalTransfers,
		"shieldedVolume": shieldedVolume,
		"shieldCount":    shieldCount,
		"unshieldCount":  unshieldCount,
	}
	stats["merkle"] = map[string]interface{}{
		"root":      merkleRoot,
		"height":    merkleHeight,
		"leafCount": leafCount,
	}

	return stats, nil
}

// StoreTransfer stores a shielded transfer and its components
func (a *Adapter) StoreTransfer(ctx context.Context, store storage.Store, t ShieldedTransfer, vertexID string) error {
	// Store transfer
	err := store.Exec(ctx, `
		INSERT OR IGNORE INTO zchain_transfers (tx_id, type, proof_type, proof_data, value_balance, fee, memo, timestamp, vertex_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, t.TxID, t.Type, t.Proof.Type, t.Proof.Data, t.ValueBalance, t.Fee, t.Memo, t.Timestamp, vertexID)
	if err != nil {
		return fmt.Errorf("store transfer: %w", err)
	}

	// Store proof
	publicInputsJSON, _ := json.Marshal(t.Proof.PublicInputs)
	err = store.Exec(ctx, `
		INSERT INTO zchain_proofs (tx_id, proof_type, proof_data, public_inputs, verifying_key, verified)
		VALUES (?, ?, ?, ?, ?, 1)
	`, t.TxID, t.Proof.Type, t.Proof.Data, string(publicInputsJSON), t.Proof.VerifyingKey)
	if err != nil {
		return fmt.Errorf("store proof: %w", err)
	}

	// Store nullifiers
	for _, n := range t.Nullifiers {
		err = store.Exec(ctx, `
			INSERT OR IGNORE INTO zchain_nullifiers (hash, tx_id, idx, spent_at)
			VALUES (?, ?, ?, ?)
		`, n.Hash, n.TxID, n.Index, n.SpentAt)
		if err != nil {
			return fmt.Errorf("store nullifier: %w", err)
		}
	}

	// Store commitments
	for _, c := range t.Commitments {
		spent := 0
		if c.Spent {
			spent = 1
		}
		err = store.Exec(ctx, `
			INSERT OR IGNORE INTO zchain_commitments (hash, tx_id, idx, created_at, spent)
			VALUES (?, ?, ?, ?, ?)
		`, c.Hash, c.TxID, c.Index, c.CreatedAt, spent)
		if err != nil {
			return fmt.Errorf("store commitment: %w", err)
		}
	}

	return nil
}

// UpdateExtendedStats updates Z-Chain specific statistics
func (a *Adapter) UpdateExtendedStats(ctx context.Context, store storage.Store) error {
	return store.Exec(ctx, `
		UPDATE zchain_extended_stats SET
			total_nullifiers = (SELECT COUNT(*) FROM zchain_nullifiers),
			total_commitments = (SELECT COUNT(*) FROM zchain_commitments),
			total_transfers = (SELECT COUNT(*) FROM zchain_transfers),
			shield_count = (SELECT COUNT(*) FROM zchain_transfers WHERE type='shield'),
			unshield_count = (SELECT COUNT(*) FROM zchain_transfers WHERE type='unshield'),
			shielded_volume = COALESCE((SELECT SUM(ABS(value_balance)) FROM zchain_transfers), 0),
			updated_at = CURRENT_TIMESTAMP
		WHERE id=1
	`)
}

// rpcCall executes a JSON-RPC request
func (a *Adapter) rpcCall(ctx context.Context, req RPCRequest) (*RPCResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.rpcEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("rpc call: %w", err)
	}
	defer httpResp.Body.Close()

	var resp RPCResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return &resp, nil
}

// DefaultConfig returns the default configuration for Z-Chain indexer
func DefaultConfig() dag.Config {
	return dag.Config{
		ChainType:    dag.ChainZ,
		ChainName:    "Z-Chain (Privacy)",
		RPCEndpoint:  DefaultRPCEndpoint,
		RPCMethod:    RPCMethod,
		HTTPPort:     DefaultHTTPPort,
		PollInterval: 5 * time.Second,
	}
}

// ValidateNullifierHash validates a nullifier hash format
func ValidateNullifierHash(hash string) error {
	if len(hash) != 64 {
		return fmt.Errorf("invalid nullifier hash length: expected 64, got %d", len(hash))
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return fmt.Errorf("invalid nullifier hash encoding: %w", err)
	}
	return nil
}

// ValidateCommitmentHash validates a commitment hash format
func ValidateCommitmentHash(hash string) error {
	if len(hash) != 64 {
		return fmt.Errorf("invalid commitment hash length: expected 64, got %d", len(hash))
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return fmt.Errorf("invalid commitment hash encoding: %w", err)
	}
	return nil
}

// Compile-time interface compliance check
var _ dag.Adapter = (*Adapter)(nil)
