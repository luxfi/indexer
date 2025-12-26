// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package bchain provides the B-Chain (Bridge) adapter for cross-chain bridge operations.
// B-Chain uses DAG consensus for fast finality of bridge transfers.
package bchain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/luxfi/indexer/dag"
	"github.com/luxfi/indexer/storage"
)

// Default configuration
const (
	DefaultPort     = 4600
	DefaultDatabase = "explorer_bchain"
	RPCMethod       = "bvm"
)

// BridgeStatus indicates the state of a cross-chain transfer
type BridgeStatus string

const (
	BridgeStatusPending   BridgeStatus = "pending"
	BridgeStatusLocked    BridgeStatus = "locked"
	BridgeStatusConfirmed BridgeStatus = "confirmed"
	BridgeStatusReleased  BridgeStatus = "released"
	BridgeStatusFailed    BridgeStatus = "failed"
)

// Transfer represents a cross-chain bridge transfer
type Transfer struct {
	ID            string       `json:"id"`
	SourceChain   string       `json:"sourceChain"`
	DestChain     string       `json:"destChain"`
	SourceTxID    string       `json:"sourceTxId"`
	DestTxID      string       `json:"destTxId,omitempty"`
	Sender        string       `json:"sender"`
	Recipient     string       `json:"recipient"`
	AssetID       string       `json:"assetId"`
	Amount        string       `json:"amount"`
	Fee           string       `json:"fee,omitempty"`
	Status        BridgeStatus `json:"status"`
	LockHeight    uint64       `json:"lockHeight,omitempty"`
	ReleaseHeight uint64       `json:"releaseHeight,omitempty"`
	ProofHash     string       `json:"proofHash,omitempty"`
	Timestamp     time.Time    `json:"timestamp"`
}

// Proof represents a bridge proof for cross-chain validation
type Proof struct {
	ID         string    `json:"id"`
	TransferID string    `json:"transferId"`
	ProofType  string    `json:"proofType"` // merkle, signature, zk
	ProofData  string    `json:"proofData"`
	Validators []string  `json:"validators,omitempty"`
	Signatures []string  `json:"signatures,omitempty"`
	Verified   bool      `json:"verified"`
	VerifiedAt time.Time `json:"verifiedAt,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// LockedAsset represents an asset locked in the bridge
type LockedAsset struct {
	AssetID     string    `json:"assetId"`
	SourceChain string    `json:"sourceChain"`
	Amount      string    `json:"amount"`
	LockTxID    string    `json:"lockTxId"`
	LockedAt    time.Time `json:"lockedAt"`
	UnlockedAt  time.Time `json:"unlockedAt,omitempty"`
}

// vertexData holds B-Chain specific vertex data
type vertexData struct {
	VertexID     string        `json:"vertexId"`
	Type         string        `json:"type"`
	ParentIDs    []string      `json:"parentIds"`
	Height       uint64        `json:"height"`
	Epoch        uint32        `json:"epoch"`
	Timestamp    int64         `json:"timestamp"`
	Status       string        `json:"status"`
	Transfers    []Transfer    `json:"transfers,omitempty"`
	Proofs       []Proof       `json:"proofs,omitempty"`
	LockedAssets []LockedAsset `json:"lockedAssets,omitempty"`
}

// Adapter implements dag.Adapter for B-Chain bridge operations
type Adapter struct {
	rpcEndpoint string
	httpClient  *http.Client
}

// New creates a new B-Chain adapter
func New(rpcEndpoint string) *Adapter {
	return &Adapter{
		rpcEndpoint: rpcEndpoint,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// ParseVertex parses B-Chain vertex data from RPC response
func (a *Adapter) ParseVertex(data json.RawMessage) (*dag.Vertex, error) {
	var vd vertexData
	if err := json.Unmarshal(data, &vd); err != nil {
		return nil, fmt.Errorf("parse vertex: %w", err)
	}

	// Extract transfer count for metadata
	metadata := map[string]interface{}{
		"transferCount":    len(vd.Transfers),
		"proofCount":       len(vd.Proofs),
		"lockedAssetCount": len(vd.LockedAssets),
	}

	// Collect unique chains involved
	chains := make(map[string]bool)
	for _, t := range vd.Transfers {
		chains[t.SourceChain] = true
		chains[t.DestChain] = true
	}
	chainList := make([]string, 0, len(chains))
	for c := range chains {
		chainList = append(chainList, c)
	}
	metadata["chainsInvolved"] = chainList

	return &dag.Vertex{
		ID:        vd.VertexID,
		Type:      vd.Type,
		ParentIDs: vd.ParentIDs,
		Height:    vd.Height,
		Epoch:     vd.Epoch,
		Timestamp: time.Unix(vd.Timestamp, 0),
		Status:    dag.Status(vd.Status),
		Data:      data,
		Metadata:  metadata,
	}, nil
}

// GetRecentVertices fetches recent vertices from the B-Chain RPC
func (a *Adapter) GetRecentVertices(ctx context.Context, limit int) ([]json.RawMessage, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "bvm.getRecentVertices",
		"params": map[string]interface{}{
			"limit": limit,
		},
	}

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

	var result struct {
		Result struct {
			Vertices []json.RawMessage `json:"vertices"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", result.Error.Code, result.Error.Message)
	}

	return result.Result.Vertices, nil
}

// GetVertexByID fetches a specific vertex by ID
func (a *Adapter) GetVertexByID(ctx context.Context, id string) (json.RawMessage, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "bvm.getVertex",
		"params": map[string]interface{}{
			"vertexId": id,
		},
	}

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

	var result struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", result.Error.Code, result.Error.Message)
	}

	return result.Result, nil
}

// InitSchema creates B-Chain specific database tables
func (a *Adapter) InitSchema(ctx context.Context, store storage.Store) error {
	schema := `
		-- Bridge transfers table
		CREATE TABLE IF NOT EXISTS bchain_transfers (
			id TEXT PRIMARY KEY,
			vertex_id TEXT NOT NULL,
			source_chain TEXT NOT NULL,
			dest_chain TEXT NOT NULL,
			source_tx_id TEXT,
			dest_tx_id TEXT,
			sender TEXT NOT NULL,
			recipient TEXT NOT NULL,
			asset_id TEXT NOT NULL,
			amount TEXT NOT NULL,
			fee TEXT,
			status TEXT DEFAULT 'pending',
			lock_height BIGINT,
			release_height BIGINT,
			proof_hash TEXT,
			timestamp TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_bchain_transfers_vertex ON bchain_transfers(vertex_id);
		CREATE INDEX IF NOT EXISTS idx_bchain_transfers_source ON bchain_transfers(source_chain);
		CREATE INDEX IF NOT EXISTS idx_bchain_transfers_dest ON bchain_transfers(dest_chain);
		CREATE INDEX IF NOT EXISTS idx_bchain_transfers_sender ON bchain_transfers(sender);
		CREATE INDEX IF NOT EXISTS idx_bchain_transfers_status ON bchain_transfers(status);

		-- Bridge proofs table
		CREATE TABLE IF NOT EXISTS bchain_proofs (
			id TEXT PRIMARY KEY,
			transfer_id TEXT NOT NULL,
			proof_type TEXT NOT NULL,
			proof_data TEXT NOT NULL,
			validators TEXT DEFAULT '[]',
			signatures TEXT DEFAULT '[]',
			verified BOOLEAN DEFAULT FALSE,
			verified_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_bchain_proofs_transfer ON bchain_proofs(transfer_id);
		CREATE INDEX IF NOT EXISTS idx_bchain_proofs_verified ON bchain_proofs(verified);

		-- Locked assets table
		CREATE TABLE IF NOT EXISTS bchain_locked_assets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			asset_id TEXT NOT NULL,
			source_chain TEXT NOT NULL,
			amount TEXT NOT NULL,
			lock_tx_id TEXT NOT NULL,
			locked_at TIMESTAMP NOT NULL,
			unlocked_at TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_bchain_locked_asset ON bchain_locked_assets(asset_id);

		-- Chain statistics table
		CREATE TABLE IF NOT EXISTS bchain_chain_stats (
			chain_id TEXT PRIMARY KEY,
			total_transfers_in BIGINT DEFAULT 0,
			total_transfers_out BIGINT DEFAULT 0,
			total_volume_locked TEXT DEFAULT '0',
			active_transfers BIGINT DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		-- Bridge statistics table
		CREATE TABLE IF NOT EXISTS bchain_bridge_stats (
			id INT PRIMARY KEY DEFAULT 1,
			total_transfers BIGINT DEFAULT 0,
			pending_transfers BIGINT DEFAULT 0,
			completed_transfers BIGINT DEFAULT 0,
			failed_transfers BIGINT DEFAULT 0,
			total_proofs BIGINT DEFAULT 0,
			verified_proofs BIGINT DEFAULT 0,
			unique_assets BIGINT DEFAULT 0,
			unique_chains BIGINT DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		INSERT OR IGNORE INTO bchain_bridge_stats (id) VALUES (1);
	`

	if err := store.Exec(ctx, schema); err != nil {
		return fmt.Errorf("init bchain schema: %w", err)
	}

	return nil
}

// GetStats returns B-Chain specific statistics
func (a *Adapter) GetStats(ctx context.Context, store storage.Store) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Bridge stats
	rows, err := store.Query(ctx, `
		SELECT total_transfers, pending_transfers, completed_transfers, failed_transfers,
		       total_proofs, verified_proofs, unique_assets, unique_chains
		FROM bchain_bridge_stats WHERE id = 1
	`)
	if err == nil && len(rows) > 0 {
		stats["bridge"] = rows[0]
	}

	// Chain-specific stats
	chainRows, err := store.Query(ctx, `
		SELECT chain_id, total_transfers_in, total_transfers_out, total_volume_locked, active_transfers
		FROM bchain_chain_stats
	`)
	if err == nil {
		chainStats := make(map[string]interface{})
		for _, row := range chainRows {
			if chainID, ok := row["chain_id"].(string); ok {
				chainStats[chainID] = row
			}
		}
		stats["chains"] = chainStats
	}

	return stats, nil
}

// StoreTransfer stores a bridge transfer
func (a *Adapter) StoreTransfer(ctx context.Context, store storage.Store, vertexID string, t *Transfer) error {
	return store.Exec(ctx, `
		INSERT INTO bchain_transfers (
			id, vertex_id, source_chain, dest_chain, source_tx_id, dest_tx_id,
			sender, recipient, asset_id, amount, fee, status,
			lock_height, release_height, proof_hash, timestamp
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			dest_tx_id = EXCLUDED.dest_tx_id,
			release_height = EXCLUDED.release_height,
			proof_hash = EXCLUDED.proof_hash
	`,
		t.ID, vertexID, t.SourceChain, t.DestChain, t.SourceTxID, t.DestTxID,
		t.Sender, t.Recipient, t.AssetID, t.Amount, t.Fee, t.Status,
		t.LockHeight, t.ReleaseHeight, t.ProofHash, t.Timestamp,
	)
}

// StoreProof stores a bridge proof
func (a *Adapter) StoreProof(ctx context.Context, store storage.Store, p *Proof) error {
	validators, _ := json.Marshal(p.Validators)
	signatures, _ := json.Marshal(p.Signatures)

	return store.Exec(ctx, `
		INSERT INTO bchain_proofs (
			id, transfer_id, proof_type, proof_data, validators, signatures, verified, verified_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			verified = EXCLUDED.verified,
			verified_at = EXCLUDED.verified_at
	`,
		p.ID, p.TransferID, p.ProofType, p.ProofData, string(validators), string(signatures), p.Verified, p.VerifiedAt, p.CreatedAt,
	)
}

// UpdateBridgeStats updates aggregate bridge statistics
func (a *Adapter) UpdateBridgeStats(ctx context.Context, store storage.Store) error {
	return store.Exec(ctx, `
		UPDATE bchain_bridge_stats SET
			total_transfers = (SELECT COUNT(*) FROM bchain_transfers),
			pending_transfers = (SELECT COUNT(*) FROM bchain_transfers WHERE status = 'pending' OR status = 'locked'),
			completed_transfers = (SELECT COUNT(*) FROM bchain_transfers WHERE status = 'released'),
			failed_transfers = (SELECT COUNT(*) FROM bchain_transfers WHERE status = 'failed'),
			total_proofs = (SELECT COUNT(*) FROM bchain_proofs),
			verified_proofs = (SELECT COUNT(*) FROM bchain_proofs WHERE verified = TRUE),
			unique_assets = (SELECT COUNT(DISTINCT asset_id) FROM bchain_transfers),
			unique_chains = (
				SELECT COUNT(DISTINCT chain) FROM (
					SELECT source_chain AS chain FROM bchain_transfers
					UNION
					SELECT dest_chain AS chain FROM bchain_transfers
				) chains
			),
			updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`)
}

// GetTransfersByChain retrieves transfers for a specific chain
func (a *Adapter) GetTransfersByChain(ctx context.Context, store storage.Store, chainID string, direction string, limit int) ([]Transfer, error) {
	var q string
	switch direction {
	case "in":
		q = `SELECT id, source_chain, dest_chain, source_tx_id, dest_tx_id, sender, recipient,
		         asset_id, amount, fee, status, lock_height, release_height, proof_hash, timestamp
		         FROM bchain_transfers WHERE dest_chain = ? ORDER BY timestamp DESC LIMIT ?`
	case "out":
		q = `SELECT id, source_chain, dest_chain, source_tx_id, dest_tx_id, sender, recipient,
		         asset_id, amount, fee, status, lock_height, release_height, proof_hash, timestamp
		         FROM bchain_transfers WHERE source_chain = ? ORDER BY timestamp DESC LIMIT ?`
	default:
		q = `SELECT id, source_chain, dest_chain, source_tx_id, dest_tx_id, sender, recipient,
		         asset_id, amount, fee, status, lock_height, release_height, proof_hash, timestamp
		         FROM bchain_transfers WHERE source_chain = ? OR dest_chain = ? ORDER BY timestamp DESC LIMIT ?`
	}

	var rows []map[string]interface{}
	var err error
	if direction == "in" || direction == "out" {
		rows, err = store.Query(ctx, q, chainID, limit)
	} else {
		rows, err = store.Query(ctx, q, chainID, chainID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("query transfers: %w", err)
	}

	transfers := make([]Transfer, 0, len(rows))
	for _, row := range rows {
		t := Transfer{
			ID:          fmt.Sprintf("%v", row["id"]),
			SourceChain: fmt.Sprintf("%v", row["source_chain"]),
			DestChain:   fmt.Sprintf("%v", row["dest_chain"]),
			SourceTxID:  fmt.Sprintf("%v", row["source_tx_id"]),
			DestTxID:    fmt.Sprintf("%v", row["dest_tx_id"]),
			Sender:      fmt.Sprintf("%v", row["sender"]),
			Recipient:   fmt.Sprintf("%v", row["recipient"]),
			AssetID:     fmt.Sprintf("%v", row["asset_id"]),
			Amount:      fmt.Sprintf("%v", row["amount"]),
			Fee:         fmt.Sprintf("%v", row["fee"]),
			Status:      BridgeStatus(fmt.Sprintf("%v", row["status"])),
			ProofHash:   fmt.Sprintf("%v", row["proof_hash"]),
		}
		if v, ok := row["lock_height"].(int64); ok {
			t.LockHeight = uint64(v)
		}
		if v, ok := row["release_height"].(int64); ok {
			t.ReleaseHeight = uint64(v)
		}
		if v, ok := row["timestamp"].(time.Time); ok {
			t.Timestamp = v
		}
		transfers = append(transfers, t)
	}

	return transfers, nil
}

// Verify interface compliance at compile time
var _ dag.Adapter = (*Adapter)(nil)
