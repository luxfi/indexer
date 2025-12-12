// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package bchain provides the B-Chain (Bridge) adapter for cross-chain bridge operations.
// B-Chain uses DAG consensus for fast finality of bridge transfers.
package bchain

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/luxfi/indexer/dag"
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
func (a *Adapter) InitSchema(db *sql.DB) error {
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
			timestamp TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_bchain_transfers_vertex ON bchain_transfers(vertex_id);
		CREATE INDEX IF NOT EXISTS idx_bchain_transfers_source ON bchain_transfers(source_chain);
		CREATE INDEX IF NOT EXISTS idx_bchain_transfers_dest ON bchain_transfers(dest_chain);
		CREATE INDEX IF NOT EXISTS idx_bchain_transfers_sender ON bchain_transfers(sender);
		CREATE INDEX IF NOT EXISTS idx_bchain_transfers_recipient ON bchain_transfers(recipient);
		CREATE INDEX IF NOT EXISTS idx_bchain_transfers_asset ON bchain_transfers(asset_id);
		CREATE INDEX IF NOT EXISTS idx_bchain_transfers_status ON bchain_transfers(status);
		CREATE INDEX IF NOT EXISTS idx_bchain_transfers_timestamp ON bchain_transfers(timestamp DESC);

		-- Bridge proofs table
		CREATE TABLE IF NOT EXISTS bchain_proofs (
			id TEXT PRIMARY KEY,
			transfer_id TEXT NOT NULL REFERENCES bchain_transfers(id),
			proof_type TEXT NOT NULL,
			proof_data TEXT NOT NULL,
			validators JSONB DEFAULT '[]',
			signatures JSONB DEFAULT '[]',
			verified BOOLEAN DEFAULT FALSE,
			verified_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_bchain_proofs_transfer ON bchain_proofs(transfer_id);
		CREATE INDEX IF NOT EXISTS idx_bchain_proofs_type ON bchain_proofs(proof_type);
		CREATE INDEX IF NOT EXISTS idx_bchain_proofs_verified ON bchain_proofs(verified);

		-- Locked assets table
		CREATE TABLE IF NOT EXISTS bchain_locked_assets (
			id SERIAL PRIMARY KEY,
			asset_id TEXT NOT NULL,
			source_chain TEXT NOT NULL,
			amount TEXT NOT NULL,
			lock_tx_id TEXT NOT NULL,
			locked_at TIMESTAMPTZ NOT NULL,
			unlocked_at TIMESTAMPTZ,
			UNIQUE(asset_id, source_chain, lock_tx_id)
		);
		CREATE INDEX IF NOT EXISTS idx_bchain_locked_asset ON bchain_locked_assets(asset_id);
		CREATE INDEX IF NOT EXISTS idx_bchain_locked_chain ON bchain_locked_assets(source_chain);

		-- Chain statistics table
		CREATE TABLE IF NOT EXISTS bchain_chain_stats (
			chain_id TEXT PRIMARY KEY,
			total_transfers_in BIGINT DEFAULT 0,
			total_transfers_out BIGINT DEFAULT 0,
			total_volume_locked TEXT DEFAULT '0',
			active_transfers BIGINT DEFAULT 0,
			updated_at TIMESTAMPTZ DEFAULT NOW()
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
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		INSERT INTO bchain_bridge_stats (id) VALUES (1) ON CONFLICT DO NOTHING;
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("init bchain schema: %w", err)
	}

	return nil
}

// GetStats returns B-Chain specific statistics
func (a *Adapter) GetStats(ctx context.Context, db *sql.DB) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Bridge stats
	var bridgeStats struct {
		TotalTransfers     int64
		PendingTransfers   int64
		CompletedTransfers int64
		FailedTransfers    int64
		TotalProofs        int64
		VerifiedProofs     int64
		UniqueAssets       int64
		UniqueChains       int64
	}

	err := db.QueryRowContext(ctx, `
		SELECT total_transfers, pending_transfers, completed_transfers, failed_transfers,
		       total_proofs, verified_proofs, unique_assets, unique_chains
		FROM bchain_bridge_stats WHERE id = 1
	`).Scan(
		&bridgeStats.TotalTransfers,
		&bridgeStats.PendingTransfers,
		&bridgeStats.CompletedTransfers,
		&bridgeStats.FailedTransfers,
		&bridgeStats.TotalProofs,
		&bridgeStats.VerifiedProofs,
		&bridgeStats.UniqueAssets,
		&bridgeStats.UniqueChains,
	)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("get bridge stats: %w", err)
	}

	stats["bridge"] = map[string]interface{}{
		"total_transfers":     bridgeStats.TotalTransfers,
		"pending_transfers":   bridgeStats.PendingTransfers,
		"completed_transfers": bridgeStats.CompletedTransfers,
		"failed_transfers":    bridgeStats.FailedTransfers,
		"total_proofs":        bridgeStats.TotalProofs,
		"verified_proofs":     bridgeStats.VerifiedProofs,
		"unique_assets":       bridgeStats.UniqueAssets,
		"unique_chains":       bridgeStats.UniqueChains,
	}

	// Chain-specific stats
	rows, err := db.QueryContext(ctx, `
		SELECT chain_id, total_transfers_in, total_transfers_out, total_volume_locked, active_transfers
		FROM bchain_chain_stats
	`)
	if err != nil {
		return stats, nil // Return partial stats on error
	}
	defer rows.Close()

	chainStats := make(map[string]interface{})
	for rows.Next() {
		var chainID string
		var in, out, active int64
		var volume string
		if err := rows.Scan(&chainID, &in, &out, &volume, &active); err != nil {
			continue
		}
		chainStats[chainID] = map[string]interface{}{
			"transfers_in":     in,
			"transfers_out":    out,
			"volume_locked":    volume,
			"active_transfers": active,
		}
	}
	stats["chains"] = chainStats

	return stats, nil
}

// StoreTransfer stores a bridge transfer
func (a *Adapter) StoreTransfer(ctx context.Context, db *sql.DB, vertexID string, t *Transfer) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO bchain_transfers (
			id, vertex_id, source_chain, dest_chain, source_tx_id, dest_tx_id,
			sender, recipient, asset_id, amount, fee, status,
			lock_height, release_height, proof_hash, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
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
	return err
}

// StoreProof stores a bridge proof
func (a *Adapter) StoreProof(ctx context.Context, db *sql.DB, p *Proof) error {
	validators, _ := json.Marshal(p.Validators)
	signatures, _ := json.Marshal(p.Signatures)

	_, err := db.ExecContext(ctx, `
		INSERT INTO bchain_proofs (
			id, transfer_id, proof_type, proof_data, validators, signatures, verified, verified_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			verified = EXCLUDED.verified,
			verified_at = EXCLUDED.verified_at
	`,
		p.ID, p.TransferID, p.ProofType, p.ProofData, validators, signatures, p.Verified, p.VerifiedAt, p.CreatedAt,
	)
	return err
}

// UpdateBridgeStats updates aggregate bridge statistics
func (a *Adapter) UpdateBridgeStats(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
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
			updated_at = NOW()
		WHERE id = 1
	`)
	return err
}

// GetTransfersByChain retrieves transfers for a specific chain
func (a *Adapter) GetTransfersByChain(ctx context.Context, db *sql.DB, chainID string, direction string, limit int) ([]Transfer, error) {
	var query string
	switch direction {
	case "in":
		query = `SELECT id, source_chain, dest_chain, source_tx_id, dest_tx_id, sender, recipient,
		         asset_id, amount, fee, status, lock_height, release_height, proof_hash, timestamp
		         FROM bchain_transfers WHERE dest_chain = $1 ORDER BY timestamp DESC LIMIT $2`
	case "out":
		query = `SELECT id, source_chain, dest_chain, source_tx_id, dest_tx_id, sender, recipient,
		         asset_id, amount, fee, status, lock_height, release_height, proof_hash, timestamp
		         FROM bchain_transfers WHERE source_chain = $1 ORDER BY timestamp DESC LIMIT $2`
	default:
		query = `SELECT id, source_chain, dest_chain, source_tx_id, dest_tx_id, sender, recipient,
		         asset_id, amount, fee, status, lock_height, release_height, proof_hash, timestamp
		         FROM bchain_transfers WHERE source_chain = $1 OR dest_chain = $1 ORDER BY timestamp DESC LIMIT $2`
	}

	rows, err := db.QueryContext(ctx, query, chainID, limit)
	if err != nil {
		return nil, fmt.Errorf("query transfers: %w", err)
	}
	defer rows.Close()

	var transfers []Transfer
	for rows.Next() {
		var t Transfer
		if err := rows.Scan(
			&t.ID, &t.SourceChain, &t.DestChain, &t.SourceTxID, &t.DestTxID,
			&t.Sender, &t.Recipient, &t.AssetID, &t.Amount, &t.Fee,
			&t.Status, &t.LockHeight, &t.ReleaseHeight, &t.ProofHash, &t.Timestamp,
		); err != nil {
			continue
		}
		transfers = append(transfers, t)
	}

	return transfers, nil
}

// Verify interface compliance at compile time
var _ dag.Adapter = (*Adapter)(nil)
