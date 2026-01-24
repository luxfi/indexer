// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package xchain provides the X-Chain (Exchange) adapter for the DAG indexer.
// X-Chain handles asset exchange and UTXOs using DAG consensus.
package xchain

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
func (a *Adapter) InitSchema(ctx context.Context, store storage.Store) error {
	schema := `
		-- X-Chain assets
		CREATE TABLE IF NOT EXISTS xchain_assets (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			symbol TEXT NOT NULL,
			denomination SMALLINT DEFAULT 0,
			total_supply BIGINT DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_xchain_assets_symbol ON xchain_assets(symbol);

		-- X-Chain UTXOs
		CREATE TABLE IF NOT EXISTS xchain_utxos (
			id TEXT PRIMARY KEY,
			tx_id TEXT NOT NULL,
			out_index INT NOT NULL,
			asset_id TEXT NOT NULL,
			amount BIGINT NOT NULL,
			address TEXT NOT NULL,
			spent BOOLEAN DEFAULT FALSE,
			spent_by TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			spent_at TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_xchain_utxos_address ON xchain_utxos(address);
		CREATE INDEX IF NOT EXISTS idx_xchain_utxos_asset ON xchain_utxos(asset_id);
		CREATE INDEX IF NOT EXISTS idx_xchain_utxos_tx ON xchain_utxos(tx_id);

		-- X-Chain transactions
		CREATE TABLE IF NOT EXISTS xchain_transactions (
			id TEXT PRIMARY KEY,
			vertex_id TEXT,
			type TEXT NOT NULL,
			memo TEXT,
			timestamp TIMESTAMP NOT NULL,
			inputs TEXT DEFAULT '[]',
			outputs TEXT DEFAULT '[]',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_xchain_transactions_vertex ON xchain_transactions(vertex_id);
		CREATE INDEX IF NOT EXISTS idx_xchain_transactions_type ON xchain_transactions(type);

		-- X-Chain address balances
		CREATE TABLE IF NOT EXISTS xchain_balances (
			address TEXT NOT NULL,
			asset_id TEXT NOT NULL,
			balance BIGINT DEFAULT 0,
			utxo_count INT DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (address, asset_id)
		);
		CREATE INDEX IF NOT EXISTS idx_xchain_balances_address ON xchain_balances(address);

		-- X-Chain statistics
		CREATE TABLE IF NOT EXISTS xchain_chain_stats (
			id INT PRIMARY KEY DEFAULT 1,
			total_assets INT DEFAULT 0,
			total_utxos BIGINT DEFAULT 0,
			unspent_utxos BIGINT DEFAULT 0,
			total_transactions BIGINT DEFAULT 0,
			total_addresses BIGINT DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO xchain_chain_stats (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

		-- Insert LUX as default asset if not exists
		INSERT INTO xchain_assets (id, name, symbol, denomination, total_supply)
		VALUES ('LUX', 'Lux', 'LUX', 9, 0) ON CONFLICT (id) DO NOTHING;
	`

	if err := store.Exec(ctx, schema); err != nil {
		return fmt.Errorf("xchain schema: %w", err)
	}
	return nil
}

// GetStats returns X-Chain specific statistics
func (a *Adapter) GetStats(ctx context.Context, store storage.Store) (map[string]interface{}, error) {
	// Update stats first
	_ = store.Exec(ctx, `
		UPDATE xchain_chain_stats SET
			total_assets = (SELECT COUNT(*) FROM xchain_assets),
			total_utxos = (SELECT COUNT(*) FROM xchain_utxos),
			unspent_utxos = (SELECT COUNT(*) FROM xchain_utxos WHERE NOT spent),
			total_transactions = (SELECT COUNT(*) FROM xchain_transactions),
			total_addresses = (SELECT COUNT(DISTINCT address) FROM xchain_utxos),
			updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`)

	rows, err := store.Query(ctx, `
		SELECT total_assets, total_utxos, unspent_utxos, total_transactions, total_addresses
		FROM xchain_chain_stats WHERE id = 1
	`)
	if err != nil || len(rows) == 0 {
		return map[string]interface{}{}, nil
	}

	return rows[0], nil
}

// StoreTransaction stores a transaction and updates UTXOs
func (a *Adapter) StoreTransaction(ctx context.Context, store storage.Store, vertexID string, tx *Transaction) error {
	inputsJSON, _ := json.Marshal(tx.Inputs)
	outputsJSON, _ := json.Marshal(tx.Outputs)

	// Store transaction
	err := store.Exec(ctx, `
		INSERT INTO xchain_transactions (id, vertex_id, type, memo, timestamp, inputs, outputs)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO NOTHING
	`, tx.ID, vertexID, tx.Type, tx.Memo, time.Unix(tx.Timestamp, 0), string(inputsJSON), string(outputsJSON))
	if err != nil {
		return fmt.Errorf("store transaction: %w", err)
	}

	// Mark inputs as spent
	for _, in := range tx.Inputs {
		utxoID := fmt.Sprintf("%s:%d", in.TxID, in.OutIndex)
		_ = store.Exec(ctx, `
			UPDATE xchain_utxos SET spent = TRUE, spent_by = ?, spent_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, tx.ID, utxoID)
	}

	// Create new UTXOs from outputs
	for i, out := range tx.Outputs {
		if len(out.Addresses) == 0 {
			continue
		}
		utxoID := fmt.Sprintf("%s:%d", tx.ID, i)
		_ = store.Exec(ctx, `
			INSERT INTO xchain_utxos (id, tx_id, out_index, asset_id, amount, address)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (id) DO NOTHING
		`, utxoID, tx.ID, i, out.AssetID, out.Amount, out.Addresses[0])
	}

	return nil
}

// StoreAsset stores or updates an asset
func (a *Adapter) StoreAsset(ctx context.Context, store storage.Store, asset *Asset) error {
	return store.Exec(ctx, `
		INSERT INTO xchain_assets (id, name, symbol, denomination, total_supply)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			symbol = EXCLUDED.symbol,
			total_supply = EXCLUDED.total_supply
	`, asset.ID, asset.Name, asset.Symbol, asset.Denomination, asset.TotalSupply)
}

// GetUTXOs fetches UTXOs for an address
func (a *Adapter) GetUTXOs(ctx context.Context, store storage.Store, address string, assetID string, spentFilter *bool) ([]UTXO, error) {
	q := `SELECT id, tx_id, out_index, asset_id, amount, address, spent, spent_by
		FROM xchain_utxos WHERE address = ?`
	args := []interface{}{address}

	if assetID != "" {
		q += " AND asset_id = ?"
		args = append(args, assetID)
	}

	if spentFilter != nil {
		q += " AND spent = ?"
		args = append(args, *spentFilter)
	}

	q += " ORDER BY amount DESC"

	rows, err := store.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query utxos: %w", err)
	}

	var utxos []UTXO
	for _, row := range rows {
		u := UTXO{
			ID:      fmt.Sprintf("%v", row["id"]),
			TxID:    fmt.Sprintf("%v", row["tx_id"]),
			AssetID: fmt.Sprintf("%v", row["asset_id"]),
			Address: fmt.Sprintf("%v", row["address"]),
		}
		if v, ok := row["out_index"].(int64); ok {
			u.OutIndex = uint32(v)
		}
		if v, ok := row["amount"].(int64); ok {
			u.Amount = uint64(v)
		}
		if v, ok := row["spent"].(bool); ok {
			u.Spent = v
		}
		if v, ok := row["spent_by"].(string); ok {
			u.SpentBy = v
		}
		utxos = append(utxos, u)
	}

	return utxos, nil
}

// GetBalance calculates balance for an address
func (a *Adapter) GetBalance(ctx context.Context, store storage.Store, address string, assetID string) (uint64, error) {
	rows, err := store.Query(ctx, `
		SELECT COALESCE(SUM(amount), 0) as balance FROM xchain_utxos
		WHERE address = ? AND asset_id = ? AND NOT spent
	`, address, assetID)
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	if v, ok := rows[0]["balance"].(int64); ok {
		return uint64(v), nil
	}
	return 0, nil
}

// Verify Adapter implements dag.Adapter interface
var _ dag.Adapter = (*Adapter)(nil)
