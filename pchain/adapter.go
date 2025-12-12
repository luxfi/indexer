// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package pchain provides the P-Chain (Platform) adapter for the LINEAR chain indexer.
// Handles validators, delegators, subnets, and staking operations.
package pchain

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/luxfi/indexer/chain"
)

const (
	DefaultRPCEndpoint = "http://localhost:9630/ext/bc/P"
	DefaultHTTPPort    = 4100
	DefaultDatabaseURL = "postgres://postgres:postgres@localhost:5432/explorer_pchain?sslmode=disable"
)

// Adapter implements chain.Adapter for P-Chain
type Adapter struct {
	rpcEndpoint string
	httpClient  *http.Client
}

// New creates a new P-Chain adapter
func New(rpcEndpoint string) *Adapter {
	if rpcEndpoint == "" {
		rpcEndpoint = DefaultRPCEndpoint
	}
	return &Adapter{
		rpcEndpoint: rpcEndpoint,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// rpcRequest makes a JSON-RPC call to the P-Chain
func (a *Adapter) rpcRequest(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.rpcEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rpc call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// PChainBlock represents a P-Chain block from RPC
type PChainBlock struct {
	ID        string    `json:"id"`
	ParentID  string    `json:"parentID"`
	Height    uint64    `json:"height"`
	Timestamp int64     `json:"timestamp"` // Unix timestamp
	Txs       []PChainTx `json:"txs"`
}

// PChainTx represents a P-Chain transaction
type PChainTx struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Inputs      json.RawMessage `json:"inputs,omitempty"`
	Outputs     json.RawMessage `json:"outputs,omitempty"`
	Credentials json.RawMessage `json:"credentials,omitempty"`
	// Validator-specific fields
	NodeID    string `json:"nodeID,omitempty"`
	StartTime int64  `json:"startTime,omitempty"`
	EndTime   int64  `json:"endTime,omitempty"`
	Weight    uint64 `json:"weight,omitempty"`
	NetID     string `json:"netID,omitempty"` // For subnet operations
}

// ParseBlock implements chain.Adapter
func (a *Adapter) ParseBlock(data json.RawMessage) (*chain.Block, error) {
	var pb PChainBlock
	if err := json.Unmarshal(data, &pb); err != nil {
		return nil, fmt.Errorf("parse pchain block: %w", err)
	}

	txIDs := make([]string, len(pb.Txs))
	for i, tx := range pb.Txs {
		txIDs[i] = tx.ID
	}

	metadata := map[string]interface{}{
		"txTypes": extractTxTypes(pb.Txs),
	}

	return &chain.Block{
		ID:        pb.ID,
		ParentID:  pb.ParentID,
		Height:    pb.Height,
		Timestamp: time.Unix(pb.Timestamp, 0),
		Status:    chain.StatusAccepted,
		TxCount:   len(pb.Txs),
		TxIDs:     txIDs,
		Data:      data,
		Metadata:  metadata,
	}, nil
}

func extractTxTypes(txs []PChainTx) map[string]int {
	types := make(map[string]int)
	for _, tx := range txs {
		types[tx.Type]++
	}
	return types
}

// GetRecentBlocks implements chain.Adapter
func (a *Adapter) GetRecentBlocks(ctx context.Context, limit int) ([]json.RawMessage, error) {
	// Get current height first
	heightResult, err := a.rpcRequest(ctx, "pvm.getHeight", map[string]interface{}{})
	if err != nil {
		return nil, fmt.Errorf("get height: %w", err)
	}

	var heightResp struct {
		Height string `json:"height"`
	}
	if err := json.Unmarshal(heightResult, &heightResp); err != nil {
		return nil, fmt.Errorf("parse height: %w", err)
	}

	var currentHeight uint64
	fmt.Sscanf(heightResp.Height, "%d", &currentHeight)

	blocks := make([]json.RawMessage, 0, limit)
	for i := 0; i < limit && currentHeight >= uint64(i); i++ {
		height := currentHeight - uint64(i)
		blockData, err := a.GetBlockByHeight(ctx, height)
		if err != nil {
			continue // Skip failed blocks
		}
		blocks = append(blocks, blockData)
	}

	return blocks, nil
}

// GetBlockByID implements chain.Adapter
func (a *Adapter) GetBlockByID(ctx context.Context, id string) (json.RawMessage, error) {
	result, err := a.rpcRequest(ctx, "pvm.getBlock", map[string]interface{}{
		"blockID":  id,
		"encoding": "json",
	})
	if err != nil {
		return nil, fmt.Errorf("get block by id: %w", err)
	}

	var blockResp struct {
		Block json.RawMessage `json:"block"`
	}
	if err := json.Unmarshal(result, &blockResp); err != nil {
		return nil, fmt.Errorf("parse block response: %w", err)
	}

	return blockResp.Block, nil
}

// GetBlockByHeight implements chain.Adapter
func (a *Adapter) GetBlockByHeight(ctx context.Context, height uint64) (json.RawMessage, error) {
	result, err := a.rpcRequest(ctx, "pvm.getBlockByHeight", map[string]interface{}{
		"height":   fmt.Sprintf("%d", height),
		"encoding": "json",
	})
	if err != nil {
		return nil, fmt.Errorf("get block at height %d: %w", height, err)
	}

	var blockResp struct {
		Block json.RawMessage `json:"block"`
	}
	if err := json.Unmarshal(result, &blockResp); err != nil {
		return nil, fmt.Errorf("parse block response: %w", err)
	}

	return blockResp.Block, nil
}

// InitSchema implements chain.Adapter - creates P-Chain specific tables
func (a *Adapter) InitSchema(db *sql.DB) error {
	schema := `
		-- Validators table
		CREATE TABLE IF NOT EXISTS pchain_validators (
			node_id TEXT PRIMARY KEY,
			start_time TIMESTAMPTZ NOT NULL,
			end_time TIMESTAMPTZ NOT NULL,
			stake_amount BIGINT NOT NULL,
			potential_reward BIGINT DEFAULT 0,
			delegation_fee NUMERIC(5,2) DEFAULT 0,
			uptime NUMERIC(5,2) DEFAULT 0,
			connected BOOLEAN DEFAULT false,
			net_id TEXT DEFAULT 'primary',
			tx_id TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_pchain_validators_net ON pchain_validators(net_id);
		CREATE INDEX IF NOT EXISTS idx_pchain_validators_end_time ON pchain_validators(end_time);
		CREATE INDEX IF NOT EXISTS idx_pchain_validators_stake ON pchain_validators(stake_amount DESC);

		-- Delegators table
		CREATE TABLE IF NOT EXISTS pchain_delegators (
			tx_id TEXT PRIMARY KEY,
			node_id TEXT NOT NULL REFERENCES pchain_validators(node_id),
			start_time TIMESTAMPTZ NOT NULL,
			end_time TIMESTAMPTZ NOT NULL,
			stake_amount BIGINT NOT NULL,
			potential_reward BIGINT DEFAULT 0,
			reward_owner TEXT,
			net_id TEXT DEFAULT 'primary',
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_pchain_delegators_node ON pchain_delegators(node_id);
		CREATE INDEX IF NOT EXISTS idx_pchain_delegators_end ON pchain_delegators(end_time);

		-- Subnets (Networks) table
		CREATE TABLE IF NOT EXISTS pchain_nets (
			net_id TEXT PRIMARY KEY,
			owner_addresses JSONB DEFAULT '[]',
			threshold INT DEFAULT 1,
			control_keys JSONB DEFAULT '[]',
			tx_id TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);

		-- Blockchains table
		CREATE TABLE IF NOT EXISTS pchain_chains (
			chain_id TEXT PRIMARY KEY,
			net_id TEXT REFERENCES pchain_nets(net_id),
			name TEXT NOT NULL,
			vm_id TEXT NOT NULL,
			genesis_data TEXT,
			tx_id TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_pchain_chains_net ON pchain_chains(net_id);

		-- Staking rewards table
		CREATE TABLE IF NOT EXISTS pchain_rewards (
			id SERIAL PRIMARY KEY,
			tx_id TEXT NOT NULL,
			node_id TEXT,
			delegator_tx_id TEXT,
			amount BIGINT NOT NULL,
			reward_type TEXT NOT NULL,
			claimed_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_pchain_rewards_node ON pchain_rewards(node_id);
		CREATE INDEX IF NOT EXISTS idx_pchain_rewards_time ON pchain_rewards(claimed_at DESC);

		-- P-Chain transactions table (extended)
		CREATE TABLE IF NOT EXISTS pchain_txs (
			tx_id TEXT PRIMARY KEY,
			block_id TEXT,
			tx_type TEXT NOT NULL,
			inputs JSONB DEFAULT '[]',
			outputs JSONB DEFAULT '[]',
			memo TEXT,
			fee BIGINT DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_pchain_txs_type ON pchain_txs(tx_type);
		CREATE INDEX IF NOT EXISTS idx_pchain_txs_block ON pchain_txs(block_id);

		-- Extended stats for P-Chain
		CREATE TABLE IF NOT EXISTS pchain_extended_stats (
			id INT PRIMARY KEY DEFAULT 1,
			total_validators INT DEFAULT 0,
			active_validators INT DEFAULT 0,
			total_delegators INT DEFAULT 0,
			total_stake BIGINT DEFAULT 0,
			total_delegated BIGINT DEFAULT 0,
			total_nets INT DEFAULT 0,
			total_chains INT DEFAULT 0,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		INSERT INTO pchain_extended_stats (id) VALUES (1) ON CONFLICT DO NOTHING;
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("pchain schema: %w", err)
	}
	return nil
}

// GetStats implements chain.Adapter - returns P-Chain specific statistics
func (a *Adapter) GetStats(ctx context.Context, db *sql.DB) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Get validator stats
	var totalValidators, activeValidators int
	var totalStake int64
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pchain_validators").Scan(&totalValidators)
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pchain_validators WHERE end_time > NOW()").Scan(&activeValidators)
	db.QueryRowContext(ctx, "SELECT COALESCE(SUM(stake_amount), 0) FROM pchain_validators WHERE end_time > NOW()").Scan(&totalStake)

	stats["validators"] = map[string]interface{}{
		"total":       totalValidators,
		"active":      activeValidators,
		"total_stake": totalStake,
	}

	// Get delegator stats
	var totalDelegators int
	var totalDelegated int64
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pchain_delegators WHERE end_time > NOW()").Scan(&totalDelegators)
	db.QueryRowContext(ctx, "SELECT COALESCE(SUM(stake_amount), 0) FROM pchain_delegators WHERE end_time > NOW()").Scan(&totalDelegated)

	stats["delegators"] = map[string]interface{}{
		"active":          totalDelegators,
		"total_delegated": totalDelegated,
	}

	// Get subnet/network stats
	var totalNets, totalChains int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pchain_nets").Scan(&totalNets)
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pchain_chains").Scan(&totalChains)

	stats["networks"] = map[string]interface{}{
		"total_nets":   totalNets,
		"total_chains": totalChains,
	}

	// Update extended stats
	db.ExecContext(ctx, `
		UPDATE pchain_extended_stats SET
			total_validators = $1,
			active_validators = $2,
			total_delegators = $3,
			total_stake = $4,
			total_delegated = $5,
			total_nets = $6,
			total_chains = $7,
			updated_at = NOW()
		WHERE id = 1
	`, totalValidators, activeValidators, totalDelegators, totalStake, totalDelegated, totalNets, totalChains)

	return stats, nil
}

// GetCurrentValidators fetches current validators from RPC
func (a *Adapter) GetCurrentValidators(ctx context.Context, netID string) ([]Validator, error) {
	params := map[string]interface{}{}
	if netID != "" && netID != "primary" {
		params["netID"] = netID
	}

	result, err := a.rpcRequest(ctx, "pvm.getCurrentValidators", params)
	if err != nil {
		return nil, fmt.Errorf("get current validators: %w", err)
	}

	var resp struct {
		Validators []Validator `json:"validators"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse validators: %w", err)
	}

	return resp.Validators, nil
}

// GetPendingValidators fetches pending validators from RPC
func (a *Adapter) GetPendingValidators(ctx context.Context, netID string) ([]Validator, error) {
	params := map[string]interface{}{}
	if netID != "" && netID != "primary" {
		params["netID"] = netID
	}

	result, err := a.rpcRequest(ctx, "pvm.getPendingValidators", params)
	if err != nil {
		return nil, fmt.Errorf("get pending validators: %w", err)
	}

	var resp struct {
		Validators []Validator `json:"validators"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse pending validators: %w", err)
	}

	return resp.Validators, nil
}

// Validator represents a P-Chain validator
type Validator struct {
	TxID            string  `json:"txID"`
	NodeID          string  `json:"nodeID"`
	StartTime       string  `json:"startTime"`
	EndTime         string  `json:"endTime"`
	StakeAmount     string  `json:"stakeAmount"`
	PotentialReward string  `json:"potentialReward"`
	DelegationFee   string  `json:"delegationFee"`
	Uptime          string  `json:"uptime"`
	Connected       bool    `json:"connected"`
	Delegators      []Delegator `json:"delegators,omitempty"`
}

// Delegator represents a P-Chain delegator
type Delegator struct {
	TxID            string `json:"txID"`
	NodeID          string `json:"nodeID"`
	StartTime       string `json:"startTime"`
	EndTime         string `json:"endTime"`
	StakeAmount     string `json:"stakeAmount"`
	PotentialReward string `json:"potentialReward"`
	RewardOwner     string `json:"rewardOwner,omitempty"`
}

// GetNets fetches all subnets/networks from RPC
func (a *Adapter) GetNets(ctx context.Context) ([]Net, error) {
	result, err := a.rpcRequest(ctx, "pvm.getNets", map[string]interface{}{})
	if err != nil {
		return nil, fmt.Errorf("get nets: %w", err)
	}

	var resp struct {
		Nets []Net `json:"nets"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse nets: %w", err)
	}

	return resp.Nets, nil
}

// Net represents a P-Chain subnet/network
type Net struct {
	ID          string   `json:"id"`
	ControlKeys []string `json:"controlKeys"`
	Threshold   string   `json:"threshold"`
}

// GetBlockchains fetches all blockchains from RPC
func (a *Adapter) GetBlockchains(ctx context.Context) ([]Blockchain, error) {
	result, err := a.rpcRequest(ctx, "pvm.getBlockchains", map[string]interface{}{})
	if err != nil {
		return nil, fmt.Errorf("get blockchains: %w", err)
	}

	var resp struct {
		Blockchains []Blockchain `json:"blockchains"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse blockchains: %w", err)
	}

	return resp.Blockchains, nil
}

// Blockchain represents a P-Chain blockchain
type Blockchain struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	NetID   string `json:"netID"`
	VMID    string `json:"vmID"`
}

// SyncValidators syncs validator data from RPC to database
func (a *Adapter) SyncValidators(ctx context.Context, db *sql.DB) error {
	validators, err := a.GetCurrentValidators(ctx, "")
	if err != nil {
		return err
	}

	for _, v := range validators {
		_, err := db.ExecContext(ctx, `
			INSERT INTO pchain_validators (node_id, start_time, end_time, stake_amount, potential_reward, delegation_fee, uptime, connected, tx_id, updated_at)
			VALUES ($1, to_timestamp($2::bigint), to_timestamp($3::bigint), $4, $5, $6, $7, $8, $9, NOW())
			ON CONFLICT (node_id) DO UPDATE SET
				end_time = EXCLUDED.end_time,
				stake_amount = EXCLUDED.stake_amount,
				potential_reward = EXCLUDED.potential_reward,
				uptime = EXCLUDED.uptime,
				connected = EXCLUDED.connected,
				updated_at = NOW()
		`, v.NodeID, v.StartTime, v.EndTime, v.StakeAmount, v.PotentialReward, v.DelegationFee, v.Uptime, v.Connected, v.TxID)
		if err != nil {
			return fmt.Errorf("upsert validator %s: %w", v.NodeID, err)
		}

		// Sync delegators for this validator
		for _, d := range v.Delegators {
			_, err := db.ExecContext(ctx, `
				INSERT INTO pchain_delegators (tx_id, node_id, start_time, end_time, stake_amount, potential_reward, reward_owner)
				VALUES ($1, $2, to_timestamp($3::bigint), to_timestamp($4::bigint), $5, $6, $7)
				ON CONFLICT (tx_id) DO UPDATE SET
					end_time = EXCLUDED.end_time,
					potential_reward = EXCLUDED.potential_reward
			`, d.TxID, d.NodeID, d.StartTime, d.EndTime, d.StakeAmount, d.PotentialReward, d.RewardOwner)
			if err != nil {
				return fmt.Errorf("upsert delegator %s: %w", d.TxID, err)
			}
		}
	}

	return nil
}

// NewConfig creates a default P-Chain indexer configuration
func NewConfig() chain.Config {
	return chain.Config{
		ChainType:    chain.ChainP,
		ChainName:    "P-Chain (Platform)",
		RPCEndpoint:  DefaultRPCEndpoint,
		RPCMethod:    "pvm",
		DatabaseURL:  DefaultDatabaseURL,
		HTTPPort:     DefaultHTTPPort,
		PollInterval: 5 * time.Second,
	}
}

// Ensure Adapter implements chain.Adapter
var _ chain.Adapter = (*Adapter)(nil)
