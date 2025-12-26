// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package evm

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Predefined chain configurations
var DefaultChains = []ChainConfig{
	{
		ChainID:         ChainIDLuxMainnet,
		Name:            "Lux C-Chain",
		Symbol:          "LUX",
		RPCEndpoint:     "https://api.lux.network/ext/bc/C/rpc",
		WSEndpoint:      "wss://api.lux.network/ext/bc/C/ws",
		ExplorerURL:     "https://explorer.lux.network",
		IsTestnet:       false,
		BlockTime:       2000,
		SupportsEIP1559: true,
		SupportsEIP4844: true,
		SupportsEIP4337: true,
		EntryPoints:     []string{EntryPointV06, EntryPointV07},
	},
	{
		ChainID:         ChainIDLuxTestnet,
		Name:            "Lux C-Chain Testnet",
		Symbol:          "LUX",
		RPCEndpoint:     "https://api.testnet.lux.network/ext/bc/C/rpc",
		WSEndpoint:      "wss://api.testnet.lux.network/ext/bc/C/ws",
		ExplorerURL:     "https://explorer.testnet.lux.network",
		IsTestnet:       true,
		BlockTime:       2000,
		SupportsEIP1559: true,
		SupportsEIP4844: true,
		SupportsEIP4337: true,
		EntryPoints:     []string{EntryPointV06, EntryPointV07},
	},
	{
		ChainID:         ChainIDZooMainnet,
		Name:            "Zoo Network",
		Symbol:          "ZOO",
		RPCEndpoint:     "https://api.zoo.network/ext/bc/zoo/rpc",
		WSEndpoint:      "wss://api.zoo.network/ext/bc/zoo/ws",
		ExplorerURL:     "https://explorer.zoo.network",
		IsTestnet:       false,
		BlockTime:       2000,
		SupportsEIP1559: true,
		SupportsEIP4844: true,
		SupportsEIP4337: true,
		EntryPoints:     []string{EntryPointV06, EntryPointV07},
	},
	{
		ChainID:         ChainIDZooTestnet,
		Name:            "Zoo Network Testnet",
		Symbol:          "ZOO",
		RPCEndpoint:     "https://api.testnet.zoo.network/ext/bc/zoo/rpc",
		WSEndpoint:      "wss://api.testnet.zoo.network/ext/bc/zoo/ws",
		ExplorerURL:     "https://explorer.testnet.zoo.network",
		IsTestnet:       true,
		BlockTime:       2000,
		SupportsEIP1559: true,
		SupportsEIP4844: true,
		SupportsEIP4337: true,
		EntryPoints:     []string{EntryPointV06, EntryPointV07},
	},
	{
		ChainID:         ChainIDHanzoMainnet,
		Name:            "Hanzo AI Chain",
		Symbol:          "AI",
		RPCEndpoint:     "https://api.hanzo.ai/ext/bc/hanzo/rpc",
		WSEndpoint:      "wss://api.hanzo.ai/ext/bc/hanzo/ws",
		ExplorerURL:     "https://explorer.hanzo.ai",
		IsTestnet:       false,
		BlockTime:       1000,
		SupportsEIP1559: true,
		SupportsEIP4844: true,
		SupportsEIP4337: true,
		EntryPoints:     []string{EntryPointV06, EntryPointV07},
	},
}

// MultiChainIndexer manages multiple chain adapters
type MultiChainIndexer struct {
	db       *sql.DB
	chains   map[uint64]*ChainAdapter
	configs  map[uint64]ChainConfig
	mu       sync.RWMutex
}

// ChainAdapter wraps an adapter with chain-specific configuration
type ChainAdapter struct {
	Adapter       *Adapter
	Config        ChainConfig
	ERC4337       *ERC4337Indexer
	MEV           *MEVIndexer
	Blob          *BlobIndexer
	Enhanced      *EnhancedIndexer
	lastBlock     uint64
	isRunning     bool
	stopChan      chan struct{}
}

// NewMultiChainIndexer creates a new multi-chain indexer
func NewMultiChainIndexer(db *sql.DB) *MultiChainIndexer {
	return &MultiChainIndexer{
		db:      db,
		chains:  make(map[uint64]*ChainAdapter),
		configs: make(map[uint64]ChainConfig),
	}
}

// InitSchema creates multi-chain database tables
func (m *MultiChainIndexer) InitSchema() error {
	schema := `
		-- Chain configurations table
		CREATE TABLE IF NOT EXISTS chain_configs (
			chain_id BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			symbol TEXT NOT NULL,
			rpc_endpoint TEXT NOT NULL,
			ws_endpoint TEXT,
			explorer_url TEXT,
			is_testnet BOOLEAN DEFAULT FALSE,
			block_time_ms INT DEFAULT 2000,
			supports_eip1559 BOOLEAN DEFAULT TRUE,
			supports_eip4844 BOOLEAN DEFAULT FALSE,
			supports_eip4337 BOOLEAN DEFAULT FALSE,
			entry_points JSONB DEFAULT '[]',
			is_active BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);

		-- Cross-chain transactions table
		CREATE TABLE IF NOT EXISTS cross_chain_transactions (
			id TEXT PRIMARY KEY,
			source_chain_id BIGINT NOT NULL,
			dest_chain_id BIGINT NOT NULL,
			source_tx_hash TEXT NOT NULL,
			dest_tx_hash TEXT,
			bridge_protocol TEXT NOT NULL,
			token_address TEXT,
			amount TEXT,
			sender TEXT NOT NULL,
			recipient TEXT NOT NULL,
			status TEXT DEFAULT 'pending',
			source_timestamp TIMESTAMPTZ NOT NULL,
			dest_timestamp TIMESTAMPTZ
		);
		CREATE INDEX IF NOT EXISTS idx_xchain_source ON cross_chain_transactions(source_chain_id, source_tx_hash);
		CREATE INDEX IF NOT EXISTS idx_xchain_dest ON cross_chain_transactions(dest_chain_id, dest_tx_hash) WHERE dest_tx_hash IS NOT NULL;
		CREATE INDEX IF NOT EXISTS idx_xchain_sender ON cross_chain_transactions(sender);
		CREATE INDEX IF NOT EXISTS idx_xchain_recipient ON cross_chain_transactions(recipient);
		CREATE INDEX IF NOT EXISTS idx_xchain_status ON cross_chain_transactions(status);
		CREATE INDEX IF NOT EXISTS idx_xchain_bridge ON cross_chain_transactions(bridge_protocol);

		-- Chain sync status table
		CREATE TABLE IF NOT EXISTS chain_sync_status (
			chain_id BIGINT PRIMARY KEY,
			latest_indexed_block BIGINT DEFAULT 0,
			latest_chain_block BIGINT DEFAULT 0,
			sync_lag_blocks BIGINT DEFAULT 0,
			sync_lag_seconds FLOAT DEFAULT 0,
			blocks_per_second FLOAT DEFAULT 0,
			is_synced BOOLEAN DEFAULT FALSE,
			last_block_time TIMESTAMPTZ,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);

		-- Multi-chain stats table
		CREATE TABLE IF NOT EXISTS multichain_stats (
			id INT PRIMARY KEY DEFAULT 1,
			total_chains INT DEFAULT 0,
			active_chains INT DEFAULT 0,
			total_cross_chain_txs BIGINT DEFAULT 0,
			pending_cross_chain_txs BIGINT DEFAULT 0,
			completed_cross_chain_txs BIGINT DEFAULT 0,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		INSERT INTO multichain_stats (id) VALUES (1) ON CONFLICT DO NOTHING;
	`

	_, err := m.db.Exec(schema)
	return err
}

// AddChain adds a chain to the multi-chain indexer
func (m *MultiChainIndexer) AddChain(ctx context.Context, config ChainConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store config in DB
	entryPointsJSON, _ := json.Marshal(config.EntryPoints)
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO chain_configs
		(chain_id, name, symbol, rpc_endpoint, ws_endpoint, explorer_url, is_testnet,
		 block_time_ms, supports_eip1559, supports_eip4844, supports_eip4337, entry_points)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (chain_id) DO UPDATE SET
			rpc_endpoint = EXCLUDED.rpc_endpoint,
			ws_endpoint = EXCLUDED.ws_endpoint,
			is_active = TRUE,
			updated_at = NOW()
	`, config.ChainID, config.Name, config.Symbol, config.RPCEndpoint, config.WSEndpoint,
		config.ExplorerURL, config.IsTestnet, config.BlockTime, config.SupportsEIP1559,
		config.SupportsEIP4844, config.SupportsEIP4337, entryPointsJSON)
	if err != nil {
		return err
	}

	// Create adapter
	adapter := New(config.RPCEndpoint)

	chainAdapter := &ChainAdapter{
		Adapter:   adapter,
		Config:    config,
		stopChan:  make(chan struct{}),
	}

	// Initialize sub-indexers based on chain capabilities
	if config.SupportsEIP4337 {
		chainAdapter.ERC4337 = NewERC4337Indexer(adapter, m.db)
	}
	chainAdapter.MEV = NewMEVIndexer(adapter, m.db)
	if config.SupportsEIP4844 {
		chainAdapter.Blob = NewBlobIndexer(adapter, m.db, "")
	}
	chainAdapter.Enhanced = NewEnhancedIndexer(adapter, m.db)

	m.chains[config.ChainID] = chainAdapter
	m.configs[config.ChainID] = config

	// Initialize sync status
	m.db.ExecContext(ctx, `
		INSERT INTO chain_sync_status (chain_id) VALUES ($1)
		ON CONFLICT (chain_id) DO NOTHING
	`, config.ChainID)

	return nil
}

// RemoveChain removes a chain from the indexer
func (m *MultiChainIndexer) RemoveChain(ctx context.Context, chainID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if chain, ok := m.chains[chainID]; ok {
		if chain.isRunning {
			close(chain.stopChan)
		}
		delete(m.chains, chainID)
		delete(m.configs, chainID)
	}

	_, err := m.db.ExecContext(ctx, `
		UPDATE chain_configs SET is_active = FALSE, updated_at = NOW()
		WHERE chain_id = $1
	`, chainID)

	return err
}

// GetChain returns a chain adapter by ID
func (m *MultiChainIndexer) GetChain(chainID uint64) (*ChainAdapter, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	chain, ok := m.chains[chainID]
	return chain, ok
}

// GetChains returns all chain adapters
func (m *MultiChainIndexer) GetChains() map[uint64]*ChainAdapter {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[uint64]*ChainAdapter)
	for k, v := range m.chains {
		result[k] = v
	}
	return result
}

// StartChain starts indexing for a specific chain
func (m *MultiChainIndexer) StartChain(ctx context.Context, chainID uint64) error {
	m.mu.Lock()
	chain, ok := m.chains[chainID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("chain %d not found", chainID)
	}
	if chain.isRunning {
		m.mu.Unlock()
		return nil
	}
	chain.isRunning = true
	chain.stopChan = make(chan struct{})
	m.mu.Unlock()

	go m.runChainIndexer(ctx, chainID)
	return nil
}

// StopChain stops indexing for a specific chain
func (m *MultiChainIndexer) StopChain(chainID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	chain, ok := m.chains[chainID]
	if !ok {
		return fmt.Errorf("chain %d not found", chainID)
	}
	if !chain.isRunning {
		return nil
	}

	close(chain.stopChan)
	chain.isRunning = false
	return nil
}

// runChainIndexer runs the indexer for a specific chain
func (m *MultiChainIndexer) runChainIndexer(ctx context.Context, chainID uint64) {
	chain, ok := m.GetChain(chainID)
	if !ok {
		return
	}

	// Get last indexed block
	var lastBlock uint64
	m.db.QueryRowContext(ctx, `
		SELECT latest_indexed_block FROM chain_sync_status WHERE chain_id = $1
	`, chainID).Scan(&lastBlock)
	chain.lastBlock = lastBlock

	pollInterval := time.Duration(chain.Config.BlockTime) * time.Millisecond
	if pollInterval < time.Second {
		pollInterval = time.Second
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-chain.stopChan:
			return
		case <-ticker.C:
			if err := m.indexChainBlocks(ctx, chainID); err != nil {
				// Log error but continue
				continue
			}
		}
	}
}

// indexChainBlocks indexes new blocks for a chain
func (m *MultiChainIndexer) indexChainBlocks(ctx context.Context, chainID uint64) error {
	chain, ok := m.GetChain(chainID)
	if !ok {
		return fmt.Errorf("chain %d not found", chainID)
	}

	// Get latest block from chain
	result, err := chain.Adapter.call(ctx, "eth_blockNumber", []interface{}{})
	if err != nil {
		return err
	}

	var latestHex string
	if err := json.Unmarshal(result, &latestHex); err != nil {
		return err
	}
	latestBlock := hexToUint64(latestHex)

	// Index missing blocks
	startBlock := chain.lastBlock + 1
	if startBlock == 1 {
		// Don't index from genesis, start from recent
		if latestBlock > 100 {
			startBlock = latestBlock - 100
		}
	}

	batchSize := uint64(10)
	if latestBlock-startBlock > batchSize {
		// Cap batch size
		latestBlock = startBlock + batchSize
	}

	for blockNum := startBlock; blockNum <= latestBlock; blockNum++ {
		if err := m.indexBlock(ctx, chainID, blockNum); err != nil {
			continue
		}
		chain.lastBlock = blockNum
	}

	// Update sync status
	syncLag := latestBlock - chain.lastBlock
	m.db.ExecContext(ctx, `
		UPDATE chain_sync_status SET
			latest_indexed_block = $2,
			latest_chain_block = $3,
			sync_lag_blocks = $4,
			is_synced = ($4 < 10),
			last_block_time = NOW(),
			updated_at = NOW()
		WHERE chain_id = $1
	`, chainID, chain.lastBlock, latestBlock, syncLag)

	return nil
}

// indexBlock indexes a single block with all features
func (m *MultiChainIndexer) indexBlock(ctx context.Context, chainID uint64, blockNumber uint64) error {
	chain, ok := m.GetChain(chainID)
	if !ok {
		return fmt.Errorf("chain %d not found", chainID)
	}

	// Get block data
	blockData, err := chain.Adapter.GetBlockByHeight(ctx, blockNumber)
	if err != nil {
		return err
	}

	// Parse and store base block
	block, err := chain.Adapter.ParseBlock(blockData)
	if err != nil {
		return err
	}

	// Process with all sub-indexers
	if chain.ERC4337 != nil && chain.Config.SupportsEIP4337 {
		chain.ERC4337.ProcessBlock(ctx, blockNumber, block.ID, block.Timestamp)
	}

	if chain.MEV != nil {
		chain.MEV.ProcessBlock(ctx, blockNumber)
	}

	if chain.Blob != nil && chain.Config.SupportsEIP4844 {
		chain.Blob.ProcessBlock(ctx, blockNumber)
	}

	if chain.Enhanced != nil {
		chain.Enhanced.ProcessEnhancedBlock(ctx, blockNumber)
	}

	return nil
}

// StoreCrossChainTransaction stores a cross-chain transaction
func (m *MultiChainIndexer) StoreCrossChainTransaction(ctx context.Context, tx CrossChainTransaction) error {
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO cross_chain_transactions
		(id, source_chain_id, dest_chain_id, source_tx_hash, dest_tx_hash, bridge_protocol,
		 token_address, amount, sender, recipient, status, source_timestamp, dest_timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (id) DO UPDATE SET
			dest_tx_hash = COALESCE(EXCLUDED.dest_tx_hash, cross_chain_transactions.dest_tx_hash),
			status = EXCLUDED.status,
			dest_timestamp = COALESCE(EXCLUDED.dest_timestamp, cross_chain_transactions.dest_timestamp)
	`, tx.ID, tx.SourceChainID, tx.DestChainID, tx.SourceTxHash, tx.DestTxHash,
		tx.BridgeProtocol, tx.TokenAddress, tx.Amount, tx.Sender, tx.Recipient,
		tx.Status, tx.SourceTimestamp, tx.DestTimestamp)
	return err
}

// GetCrossChainTransaction retrieves a cross-chain transaction
func (m *MultiChainIndexer) GetCrossChainTransaction(ctx context.Context, id string) (*CrossChainTransaction, error) {
	var tx CrossChainTransaction
	var destTxHash, tokenAddress, amount sql.NullString
	var destTimestamp sql.NullTime

	err := m.db.QueryRowContext(ctx, `
		SELECT id, source_chain_id, dest_chain_id, source_tx_hash, dest_tx_hash, bridge_protocol,
			   token_address, amount, sender, recipient, status, source_timestamp, dest_timestamp
		FROM cross_chain_transactions WHERE id = $1
	`, id).Scan(&tx.ID, &tx.SourceChainID, &tx.DestChainID, &tx.SourceTxHash, &destTxHash,
		&tx.BridgeProtocol, &tokenAddress, &amount, &tx.Sender, &tx.Recipient,
		&tx.Status, &tx.SourceTimestamp, &destTimestamp)

	if err != nil {
		return nil, err
	}

	if destTxHash.Valid {
		tx.DestTxHash = destTxHash.String
	}
	if tokenAddress.Valid {
		tx.TokenAddress = tokenAddress.String
	}
	if amount.Valid {
		tx.Amount = amount.String
	}
	if destTimestamp.Valid {
		tx.DestTimestamp = destTimestamp.Time
	}

	return &tx, nil
}

// GetCrossChainTransactionsByAddress retrieves cross-chain transactions for an address
func (m *MultiChainIndexer) GetCrossChainTransactionsByAddress(ctx context.Context, address string, limit, offset int) ([]CrossChainTransaction, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, source_chain_id, dest_chain_id, source_tx_hash, dest_tx_hash, bridge_protocol,
			   token_address, amount, sender, recipient, status, source_timestamp, dest_timestamp
		FROM cross_chain_transactions
		WHERE sender = $1 OR recipient = $1
		ORDER BY source_timestamp DESC
		LIMIT $2 OFFSET $3
	`, strings.ToLower(address), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txs []CrossChainTransaction
	for rows.Next() {
		var tx CrossChainTransaction
		var destTxHash, tokenAddress, amount sql.NullString
		var destTimestamp sql.NullTime

		err := rows.Scan(&tx.ID, &tx.SourceChainID, &tx.DestChainID, &tx.SourceTxHash, &destTxHash,
			&tx.BridgeProtocol, &tokenAddress, &amount, &tx.Sender, &tx.Recipient,
			&tx.Status, &tx.SourceTimestamp, &destTimestamp)
		if err != nil {
			continue
		}

		if destTxHash.Valid {
			tx.DestTxHash = destTxHash.String
		}
		if tokenAddress.Valid {
			tx.TokenAddress = tokenAddress.String
		}
		if amount.Valid {
			tx.Amount = amount.String
		}
		if destTimestamp.Valid {
			tx.DestTimestamp = destTimestamp.Time
		}

		txs = append(txs, tx)
	}

	return txs, nil
}

// GetChainSyncStatus retrieves sync status for all chains
func (m *MultiChainIndexer) GetChainSyncStatus(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT c.chain_id, c.name, c.symbol, s.latest_indexed_block, s.latest_chain_block,
			   s.sync_lag_blocks, s.sync_lag_seconds, s.blocks_per_second, s.is_synced,
			   s.last_block_time, s.updated_at
		FROM chain_configs c
		LEFT JOIN chain_sync_status s ON c.chain_id = s.chain_id
		WHERE c.is_active = TRUE
		ORDER BY c.chain_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var chainID int64
		var name, symbol string
		var latestIndexed, latestChain, lagBlocks sql.NullInt64
		var lagSeconds, bps sql.NullFloat64
		var isSynced sql.NullBool
		var lastBlockTime, updatedAt sql.NullTime

		err := rows.Scan(&chainID, &name, &symbol, &latestIndexed, &latestChain,
			&lagBlocks, &lagSeconds, &bps, &isSynced, &lastBlockTime, &updatedAt)
		if err != nil {
			continue
		}

		status := map[string]interface{}{
			"chain_id": chainID,
			"name":     name,
			"symbol":   symbol,
		}

		if latestIndexed.Valid {
			status["latest_indexed_block"] = latestIndexed.Int64
		}
		if latestChain.Valid {
			status["latest_chain_block"] = latestChain.Int64
		}
		if lagBlocks.Valid {
			status["sync_lag_blocks"] = lagBlocks.Int64
		}
		if lagSeconds.Valid {
			status["sync_lag_seconds"] = lagSeconds.Float64
		}
		if bps.Valid {
			status["blocks_per_second"] = bps.Float64
		}
		if isSynced.Valid {
			status["is_synced"] = isSynced.Bool
		}
		if lastBlockTime.Valid {
			status["last_block_time"] = lastBlockTime.Time
		}
		if updatedAt.Valid {
			status["updated_at"] = updatedAt.Time
		}

		result = append(result, status)
	}

	return result, nil
}

// UpdateStats updates multi-chain statistics
func (m *MultiChainIndexer) UpdateStats(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, `
		UPDATE multichain_stats SET
			total_chains = (SELECT COUNT(*) FROM chain_configs),
			active_chains = (SELECT COUNT(*) FROM chain_configs WHERE is_active = TRUE),
			total_cross_chain_txs = (SELECT COUNT(*) FROM cross_chain_transactions),
			pending_cross_chain_txs = (SELECT COUNT(*) FROM cross_chain_transactions WHERE status = 'pending'),
			completed_cross_chain_txs = (SELECT COUNT(*) FROM cross_chain_transactions WHERE status = 'completed'),
			updated_at = NOW()
		WHERE id = 1
	`)
	return err
}

// GetStats retrieves multi-chain statistics
func (m *MultiChainIndexer) GetStats(ctx context.Context) (map[string]interface{}, error) {
	var totalChains, activeChains int
	var totalXChain, pendingXChain, completedXChain int64
	var updatedAt time.Time

	err := m.db.QueryRowContext(ctx, `
		SELECT total_chains, active_chains, total_cross_chain_txs, pending_cross_chain_txs,
			   completed_cross_chain_txs, updated_at
		FROM multichain_stats WHERE id = 1
	`).Scan(&totalChains, &activeChains, &totalXChain, &pendingXChain, &completedXChain, &updatedAt)

	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// Get per-chain stats
	chainStats, _ := m.GetChainSyncStatus(ctx)

	return map[string]interface{}{
		"total_chains":             totalChains,
		"active_chains":            activeChains,
		"total_cross_chain_txs":    totalXChain,
		"pending_cross_chain_txs":  pendingXChain,
		"completed_cross_chain_txs": completedXChain,
		"chain_status":             chainStats,
		"updated_at":               updatedAt,
	}, nil
}

// LoadDefaultChains loads and activates default chain configurations
func (m *MultiChainIndexer) LoadDefaultChains(ctx context.Context) error {
	for _, config := range DefaultChains {
		if err := m.AddChain(ctx, config); err != nil {
			return err
		}
	}
	return nil
}

// StartAllChains starts indexing for all configured chains
func (m *MultiChainIndexer) StartAllChains(ctx context.Context) error {
	m.mu.RLock()
	chainIDs := make([]uint64, 0, len(m.chains))
	for id := range m.chains {
		chainIDs = append(chainIDs, id)
	}
	m.mu.RUnlock()

	for _, chainID := range chainIDs {
		if err := m.StartChain(ctx, chainID); err != nil {
			return err
		}
	}
	return nil
}

// StopAllChains stops indexing for all chains
func (m *MultiChainIndexer) StopAllChains() {
	m.mu.RLock()
	chainIDs := make([]uint64, 0, len(m.chains))
	for id := range m.chains {
		chainIDs = append(chainIDs, id)
	}
	m.mu.RUnlock()

	for _, chainID := range chainIDs {
		m.StopChain(chainID)
	}
}
