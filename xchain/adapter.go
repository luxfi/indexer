// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package xchain provides the X-Chain (Exchange) adapter for the DAG indexer.
// X-Chain handles asset exchange and UTXOs using DAG consensus.
package xchain

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

// Adapter implements dag.Adapter for X-Chain
type Adapter struct {
	rpcEndpoint string
	client      *http.Client
}

// New creates a new X-Chain adapter
func New(rpcEndpoint string) *Adapter {
	return &Adapter{
		rpcEndpoint: rpcEndpoint,
		client:      &http.Client{Timeout: 30 * time.Second},
	}
}

// UTXO represents an unspent transaction output
type UTXO struct {
	ID       string `json:"id"`
	TxID     string `json:"txId"`
	OutIndex uint32 `json:"outIndex"`
	AssetID  string `json:"assetId"`
	Amount   uint64 `json:"amount"`
	Address  string `json:"address"`
	Spent    bool   `json:"spent"`
	SpentBy  string `json:"spentBy,omitempty"`
}

// Asset represents an X-Chain asset
type Asset struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Symbol       string `json:"symbol"`
	Denomination uint8  `json:"denomination"`
	TotalSupply  uint64 `json:"totalSupply"`
}

// Transaction represents an X-Chain transaction
type Transaction struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"`
	Inputs    []Input  `json:"inputs"`
	Outputs   []Output `json:"outputs"`
	Memo      string   `json:"memo,omitempty"`
	Timestamp int64    `json:"timestamp"`
}

// Input represents a transaction input
type Input struct {
	TxID     string `json:"txId"`
	OutIndex uint32 `json:"outIndex"`
	AssetID  string `json:"assetId"`
	Amount   uint64 `json:"amount"`
	Address  string `json:"address"`
}

// Output represents a transaction output
type Output struct {
	AssetID   string   `json:"assetId"`
	Amount    uint64   `json:"amount"`
	Addresses []string `json:"addresses"`
	Threshold uint32   `json:"threshold"`
	Locktime  uint64   `json:"locktime"`
}

// VertexData holds X-Chain specific vertex data
type VertexData struct {
	Transactions []Transaction `json:"transactions"`
	ChainID      string        `json:"chainId"`
}

// rpcRequest for JSON-RPC calls
type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// rpcResponse for JSON-RPC responses
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// callRPC makes a JSON-RPC call to the X-Chain
func (a *Adapter) callRPC(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
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

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("rpc call: %w", err)
	}
	defer resp.Body.Close()

	var rpcResp rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// ParseVertex parses X-Chain vertex data into a dag.Vertex
func (a *Adapter) ParseVertex(data json.RawMessage) (*dag.Vertex, error) {
	var raw struct {
		ID        string          `json:"id"`
		ParentIDs []string        `json:"parentIDs"`
		Height    uint64          `json:"height"`
		Epoch     uint32          `json:"epoch"`
		TxIDs     []string        `json:"txs"`
		Timestamp int64           `json:"timestamp"`
		Status    string          `json:"status"`
		ChainID   string          `json:"chainID"`
		Bytes     json.RawMessage `json:"bytes,omitempty"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal vertex: %w", err)
	}

	status := dag.StatusPending
	switch raw.Status {
	case "Accepted":
		status = dag.StatusAccepted
	case "Rejected":
		status = dag.StatusRejected
	}

	ts := time.Unix(raw.Timestamp, 0)
	if raw.Timestamp == 0 {
		ts = time.Now()
	}

	return &dag.Vertex{
		ID:        raw.ID,
		Type:      "xchain_vertex",
		ParentIDs: raw.ParentIDs,
		Height:    raw.Height,
		Epoch:     raw.Epoch,
		TxIDs:     raw.TxIDs,
		Timestamp: ts,
		Status:    status,
		Data:      data,
		Metadata: map[string]interface{}{
			"chainId": raw.ChainID,
		},
	}, nil
}

// GetRecentVertices fetches recent vertices from the X-Chain
func (a *Adapter) GetRecentVertices(ctx context.Context, limit int) ([]json.RawMessage, error) {
	params := map[string]interface{}{
		"limit": limit,
	}

	result, err := a.callRPC(ctx, "xvm.getRecentVertices", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Vertices []json.RawMessage `json:"vertices"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal vertices: %w", err)
	}

	return resp.Vertices, nil
}

// GetVertexByID fetches a specific vertex by ID
func (a *Adapter) GetVertexByID(ctx context.Context, id string) (json.RawMessage, error) {
	params := map[string]interface{}{
		"vertexID": id,
	}

	result, err := a.callRPC(ctx, "xvm.getVertex", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Vertex json.RawMessage `json:"vertex"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal vertex: %w", err)
	}

	return resp.Vertex, nil
}

// InitSchema creates X-Chain specific database tables
func (a *Adapter) InitSchema(db *sql.DB) error {
	schema := `
		-- X-Chain assets
		CREATE TABLE IF NOT EXISTS xchain_assets (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			symbol TEXT NOT NULL,
			denomination SMALLINT DEFAULT 0,
			total_supply BIGINT DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_xchain_assets_symbol ON xchain_assets(symbol);

		-- X-Chain UTXOs
		CREATE TABLE IF NOT EXISTS xchain_utxos (
			id TEXT PRIMARY KEY,
			tx_id TEXT NOT NULL,
			out_index INT NOT NULL,
			asset_id TEXT NOT NULL REFERENCES xchain_assets(id),
			amount BIGINT NOT NULL,
			address TEXT NOT NULL,
			spent BOOLEAN DEFAULT FALSE,
			spent_by TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			spent_at TIMESTAMPTZ
		);
		CREATE INDEX IF NOT EXISTS idx_xchain_utxos_address ON xchain_utxos(address);
		CREATE INDEX IF NOT EXISTS idx_xchain_utxos_asset ON xchain_utxos(asset_id);
		CREATE INDEX IF NOT EXISTS idx_xchain_utxos_unspent ON xchain_utxos(spent) WHERE NOT spent;
		CREATE INDEX IF NOT EXISTS idx_xchain_utxos_tx ON xchain_utxos(tx_id);

		-- X-Chain transactions
		CREATE TABLE IF NOT EXISTS xchain_transactions (
			id TEXT PRIMARY KEY,
			vertex_id TEXT,
			type TEXT NOT NULL,
			memo TEXT,
			timestamp TIMESTAMPTZ NOT NULL,
			inputs JSONB DEFAULT '[]',
			outputs JSONB DEFAULT '[]',
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_xchain_transactions_vertex ON xchain_transactions(vertex_id);
		CREATE INDEX IF NOT EXISTS idx_xchain_transactions_type ON xchain_transactions(type);
		CREATE INDEX IF NOT EXISTS idx_xchain_transactions_timestamp ON xchain_transactions(timestamp DESC);

		-- X-Chain address balances (materialized view alternative)
		CREATE TABLE IF NOT EXISTS xchain_balances (
			address TEXT NOT NULL,
			asset_id TEXT NOT NULL REFERENCES xchain_assets(id),
			balance BIGINT DEFAULT 0,
			utxo_count INT DEFAULT 0,
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			PRIMARY KEY (address, asset_id)
		);
		CREATE INDEX IF NOT EXISTS idx_xchain_balances_address ON xchain_balances(address);
		CREATE INDEX IF NOT EXISTS idx_xchain_balances_asset ON xchain_balances(asset_id);

		-- X-Chain statistics
		CREATE TABLE IF NOT EXISTS xchain_chain_stats (
			id INT PRIMARY KEY DEFAULT 1,
			total_assets INT DEFAULT 0,
			total_utxos BIGINT DEFAULT 0,
			unspent_utxos BIGINT DEFAULT 0,
			total_transactions BIGINT DEFAULT 0,
			total_addresses BIGINT DEFAULT 0,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		INSERT INTO xchain_chain_stats (id) VALUES (1) ON CONFLICT DO NOTHING;

		-- Insert LUX as default asset if not exists
		INSERT INTO xchain_assets (id, name, symbol, denomination, total_supply)
		VALUES ('LUX', 'Lux', 'LUX', 9, 0)
		ON CONFLICT DO NOTHING;
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("xchain schema: %w", err)
	}
	return nil
}

// GetStats returns X-Chain specific statistics
func (a *Adapter) GetStats(ctx context.Context, db *sql.DB) (map[string]interface{}, error) {
	// Update stats first
	_, _ = db.ExecContext(ctx, `
		UPDATE xchain_chain_stats SET
			total_assets = (SELECT COUNT(*) FROM xchain_assets),
			total_utxos = (SELECT COUNT(*) FROM xchain_utxos),
			unspent_utxos = (SELECT COUNT(*) FROM xchain_utxos WHERE NOT spent),
			total_transactions = (SELECT COUNT(*) FROM xchain_transactions),
			total_addresses = (SELECT COUNT(DISTINCT address) FROM xchain_utxos),
			updated_at = NOW()
		WHERE id = 1
	`)

	var stats struct {
		TotalAssets       int   `json:"total_assets"`
		TotalUTXOs        int64 `json:"total_utxos"`
		UnspentUTXOs      int64 `json:"unspent_utxos"`
		TotalTransactions int64 `json:"total_transactions"`
		TotalAddresses    int64 `json:"total_addresses"`
	}

	err := db.QueryRowContext(ctx, `
		SELECT total_assets, total_utxos, unspent_utxos, total_transactions, total_addresses
		FROM xchain_chain_stats WHERE id = 1
	`).Scan(&stats.TotalAssets, &stats.TotalUTXOs, &stats.UnspentUTXOs,
		&stats.TotalTransactions, &stats.TotalAddresses)

	if err != nil {
		return nil, fmt.Errorf("query stats: %w", err)
	}

	return map[string]interface{}{
		"total_assets":       stats.TotalAssets,
		"total_utxos":        stats.TotalUTXOs,
		"unspent_utxos":      stats.UnspentUTXOs,
		"total_transactions": stats.TotalTransactions,
		"total_addresses":    stats.TotalAddresses,
	}, nil
}

// StoreTransaction stores a transaction and updates UTXOs
func (a *Adapter) StoreTransaction(ctx context.Context, db *sql.DB, vertexID string, tx *Transaction) error {
	inputsJSON, _ := json.Marshal(tx.Inputs)
	outputsJSON, _ := json.Marshal(tx.Outputs)

	// Store transaction
	_, err := db.ExecContext(ctx, `
		INSERT INTO xchain_transactions (id, vertex_id, type, memo, timestamp, inputs, outputs)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO NOTHING
	`, tx.ID, vertexID, tx.Type, tx.Memo, time.Unix(tx.Timestamp, 0), inputsJSON, outputsJSON)
	if err != nil {
		return fmt.Errorf("store transaction: %w", err)
	}

	// Mark inputs as spent
	for _, in := range tx.Inputs {
		utxoID := fmt.Sprintf("%s:%d", in.TxID, in.OutIndex)
		_, _ = db.ExecContext(ctx, `
			UPDATE xchain_utxos SET spent = TRUE, spent_by = $1, spent_at = NOW()
			WHERE id = $2
		`, tx.ID, utxoID)
	}

	// Create new UTXOs from outputs
	for i, out := range tx.Outputs {
		if len(out.Addresses) == 0 {
			continue
		}
		utxoID := fmt.Sprintf("%s:%d", tx.ID, i)
		_, _ = db.ExecContext(ctx, `
			INSERT INTO xchain_utxos (id, tx_id, out_index, asset_id, amount, address)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (id) DO NOTHING
		`, utxoID, tx.ID, i, out.AssetID, out.Amount, out.Addresses[0])
	}

	return nil
}

// StoreAsset stores or updates an asset
func (a *Adapter) StoreAsset(ctx context.Context, db *sql.DB, asset *Asset) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO xchain_assets (id, name, symbol, denomination, total_supply)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			symbol = EXCLUDED.symbol,
			total_supply = EXCLUDED.total_supply
	`, asset.ID, asset.Name, asset.Symbol, asset.Denomination, asset.TotalSupply)
	return err
}

// GetUTXOs fetches UTXOs for an address
func (a *Adapter) GetUTXOs(ctx context.Context, db *sql.DB, address string, assetID string, spentFilter *bool) ([]UTXO, error) {
	query := `SELECT id, tx_id, out_index, asset_id, amount, address, spent, spent_by
		FROM xchain_utxos WHERE address = $1`
	args := []interface{}{address}

	if assetID != "" {
		query += " AND asset_id = $2"
		args = append(args, assetID)
	}

	if spentFilter != nil {
		if len(args) == 1 {
			query += " AND spent = $2"
		} else {
			query += " AND spent = $3"
		}
		args = append(args, *spentFilter)
	}

	query += " ORDER BY amount DESC"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query utxos: %w", err)
	}
	defer rows.Close()

	var utxos []UTXO
	for rows.Next() {
		var u UTXO
		var spentBy sql.NullString
		if err := rows.Scan(&u.ID, &u.TxID, &u.OutIndex, &u.AssetID, &u.Amount, &u.Address, &u.Spent, &spentBy); err != nil {
			continue
		}
		u.SpentBy = spentBy.String
		utxos = append(utxos, u)
	}

	return utxos, nil
}

// GetBalance calculates balance for an address
func (a *Adapter) GetBalance(ctx context.Context, db *sql.DB, address string, assetID string) (uint64, error) {
	var balance uint64
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount), 0) FROM xchain_utxos
		WHERE address = $1 AND asset_id = $2 AND NOT spent
	`, address, assetID).Scan(&balance)
	return balance, err
}

// Verify Adapter implements dag.Adapter interface
var _ dag.Adapter = (*Adapter)(nil)
