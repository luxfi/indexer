// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package evm

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Common DEX and lending protocol addresses
var (
	// Uniswap V2 Router
	UniswapV2Router = "0x7a250d5630b4cf539739df2c5dacb4c659f2488d"
	// Uniswap V3 Router
	UniswapV3Router = "0xe592427a0aece92de3edee1f18e0157c05861564"
	// Sushiswap Router
	SushiswapRouter = "0xd9e1ce17f2641f24ae83637ab66a2cca9c378b9f"

	// Aave V2 Lending Pool
	AaveV2LendingPool = "0x7d2768de32b0b80b7a3454c06bdac94a69ddc7a9"
	// Aave V3 Pool
	AaveV3Pool = "0x87870bca3f3fd6335c3f4ce8392d69350b4fa4e2"
	// Compound V2 Comptroller
	CompoundComptroller = "0x3d9819210a31b4961b30ef54be2aed79b9c9cd3b"

	// Event signatures
	TopicSwap     = "0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822" // Swap(address,uint256,uint256,uint256,uint256,address)
	TopicSwapV3   = "0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67" // Swap(address,address,int256,int256,uint160,uint128,int24)
	TopicSync     = "0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1" // Sync(uint112,uint112)
	TopicLiquidation = "0xe413a321e8681d831f4dbccbca790d2952b56f977908e45be37335533e005286" // LiquidationCall
)

// MEVIndexer handles MEV transaction detection and indexing
type MEVIndexer struct {
	adapter *Adapter
	db      *sql.DB
}

// NewMEVIndexer creates a new MEV indexer
func NewMEVIndexer(adapter *Adapter, db *sql.DB) *MEVIndexer {
	return &MEVIndexer{
		adapter: adapter,
		db:      db,
	}
}

// InitSchema creates MEV database tables
func (m *MEVIndexer) InitSchema() error {
	schema := `
		-- MEV transactions table
		CREATE TABLE IF NOT EXISTS mev_transactions (
			id TEXT PRIMARY KEY,
			transaction_hash TEXT NOT NULL UNIQUE,
			block_number BIGINT NOT NULL,
			mev_type TEXT NOT NULL,
			extractor_address TEXT NOT NULL,
			victim_address TEXT,
			protocol TEXT,
			profit_eth TEXT NOT NULL DEFAULT '0',
			profit_usd TEXT,
			gas_cost_eth TEXT NOT NULL DEFAULT '0',
			related_tx_hashes JSONB DEFAULT '[]',
			confidence FLOAT DEFAULT 0,
			timestamp TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_mev_tx_block ON mev_transactions(block_number);
		CREATE INDEX IF NOT EXISTS idx_mev_tx_type ON mev_transactions(mev_type);
		CREATE INDEX IF NOT EXISTS idx_mev_tx_extractor ON mev_transactions(extractor_address);
		CREATE INDEX IF NOT EXISTS idx_mev_tx_victim ON mev_transactions(victim_address) WHERE victim_address IS NOT NULL;
		CREATE INDEX IF NOT EXISTS idx_mev_tx_timestamp ON mev_transactions(timestamp);
		CREATE INDEX IF NOT EXISTS idx_mev_tx_profit ON mev_transactions((CAST(profit_eth AS NUMERIC)) DESC);

		-- Sandwich attacks table
		CREATE TABLE IF NOT EXISTS mev_sandwiches (
			id TEXT PRIMARY KEY,
			block_number BIGINT NOT NULL,
			frontrun_tx_hash TEXT NOT NULL,
			victim_tx_hash TEXT NOT NULL,
			backrun_tx_hash TEXT NOT NULL,
			attacker_address TEXT NOT NULL,
			victim_address TEXT NOT NULL,
			token_address TEXT NOT NULL,
			pool_address TEXT NOT NULL,
			victim_loss_eth TEXT NOT NULL DEFAULT '0',
			attacker_profit_eth TEXT NOT NULL DEFAULT '0',
			timestamp TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_mev_sandwich_block ON mev_sandwiches(block_number);
		CREATE INDEX IF NOT EXISTS idx_mev_sandwich_attacker ON mev_sandwiches(attacker_address);
		CREATE INDEX IF NOT EXISTS idx_mev_sandwich_victim ON mev_sandwiches(victim_address);
		CREATE INDEX IF NOT EXISTS idx_mev_sandwich_pool ON mev_sandwiches(pool_address);

		-- Arbitrage transactions table
		CREATE TABLE IF NOT EXISTS mev_arbitrages (
			id TEXT PRIMARY KEY,
			transaction_hash TEXT NOT NULL UNIQUE,
			block_number BIGINT NOT NULL,
			arbitrager_address TEXT NOT NULL,
			profit_eth TEXT NOT NULL DEFAULT '0',
			profit_usd TEXT,
			path_length INT DEFAULT 0,
			tokens JSONB DEFAULT '[]',
			pools JSONB DEFAULT '[]',
			is_atomic BOOLEAN DEFAULT FALSE,
			timestamp TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_mev_arb_block ON mev_arbitrages(block_number);
		CREATE INDEX IF NOT EXISTS idx_mev_arb_address ON mev_arbitrages(arbitrager_address);
		CREATE INDEX IF NOT EXISTS idx_mev_arb_profit ON mev_arbitrages((CAST(profit_eth AS NUMERIC)) DESC);

		-- Liquidations table
		CREATE TABLE IF NOT EXISTS mev_liquidations (
			id TEXT PRIMARY KEY,
			transaction_hash TEXT NOT NULL UNIQUE,
			block_number BIGINT NOT NULL,
			liquidator_address TEXT NOT NULL,
			borrower_address TEXT NOT NULL,
			protocol TEXT NOT NULL,
			collateral_token TEXT NOT NULL,
			debt_token TEXT NOT NULL,
			collateral_seized TEXT NOT NULL DEFAULT '0',
			debt_repaid TEXT NOT NULL DEFAULT '0',
			liquidator_profit_eth TEXT NOT NULL DEFAULT '0',
			timestamp TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_mev_liq_block ON mev_liquidations(block_number);
		CREATE INDEX IF NOT EXISTS idx_mev_liq_liquidator ON mev_liquidations(liquidator_address);
		CREATE INDEX IF NOT EXISTS idx_mev_liq_borrower ON mev_liquidations(borrower_address);
		CREATE INDEX IF NOT EXISTS idx_mev_liq_protocol ON mev_liquidations(protocol);

		-- Private/Flashbots transactions table
		CREATE TABLE IF NOT EXISTS mev_private_txs (
			transaction_hash TEXT PRIMARY KEY,
			block_number BIGINT NOT NULL,
			tx_from TEXT NOT NULL,
			tx_to TEXT,
			value TEXT,
			gas_price TEXT,
			is_flashbots BOOLEAN DEFAULT FALSE,
			bundle_index INT,
			timestamp TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_mev_private_block ON mev_private_txs(block_number);
		CREATE INDEX IF NOT EXISTS idx_mev_private_from ON mev_private_txs(tx_from);

		-- MEV stats table
		CREATE TABLE IF NOT EXISTS mev_stats (
			id INT PRIMARY KEY DEFAULT 1,
			total_mev_transactions BIGINT DEFAULT 0,
			total_sandwiches BIGINT DEFAULT 0,
			total_arbitrages BIGINT DEFAULT 0,
			total_liquidations BIGINT DEFAULT 0,
			total_private_txs BIGINT DEFAULT 0,
			total_profit_eth TEXT DEFAULT '0',
			total_victim_loss_eth TEXT DEFAULT '0',
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		INSERT INTO mev_stats (id) VALUES (1) ON CONFLICT DO NOTHING;
	`

	_, err := m.db.Exec(schema)
	return err
}

// ProcessBlock analyzes a block for MEV activity
func (m *MEVIndexer) ProcessBlock(ctx context.Context, blockNumber uint64) error {
	// Get block with transactions
	blockData, err := m.adapter.GetBlockByHeight(ctx, blockNumber)
	if err != nil {
		return err
	}

	var block struct {
		Hash         string `json:"hash"`
		Timestamp    string `json:"timestamp"`
		Transactions []struct {
			Hash     string `json:"hash"`
			From     string `json:"from"`
			To       string `json:"to"`
			Value    string `json:"value"`
			Gas      string `json:"gas"`
			GasPrice string `json:"gasPrice"`
			Input    string `json:"input"`
		} `json:"transactions"`
	}

	if err := json.Unmarshal(blockData, &block); err != nil {
		return err
	}

	timestamp := time.Unix(int64(hexToUint64(block.Timestamp)), 0)

	// Analyze transactions for MEV patterns
	for i, tx := range block.Transactions {
		// Check for sandwich patterns
		if i > 0 && i < len(block.Transactions)-1 {
			m.detectSandwich(ctx, block.Transactions[i-1].Hash, tx.Hash, block.Transactions[i+1].Hash, blockNumber, timestamp)
		}

		// Check for arbitrage
		m.detectArbitrage(ctx, tx.Hash, blockNumber, timestamp)

		// Check for liquidations
		m.detectLiquidation(ctx, tx.Hash, blockNumber, timestamp)

		// Check for private transactions (simple heuristic)
		m.detectPrivateTx(ctx, tx.Hash, tx.From, tx.To, tx.Value, tx.GasPrice, blockNumber, timestamp)
	}

	return nil
}

// detectSandwich detects potential sandwich attacks
func (m *MEVIndexer) detectSandwich(ctx context.Context, tx1Hash, tx2Hash, tx3Hash string, blockNumber uint64, timestamp time.Time) {
	// Get transaction receipts
	tx1, logs1, err := m.adapter.GetTransactionReceipt(ctx, tx1Hash)
	if err != nil || tx1.Status != 1 {
		return
	}

	tx2, logs2, err := m.adapter.GetTransactionReceipt(ctx, tx2Hash)
	if err != nil || tx2.Status != 1 {
		return
	}

	tx3, logs3, err := m.adapter.GetTransactionReceipt(ctx, tx3Hash)
	if err != nil || tx3.Status != 1 {
		return
	}

	// Check if tx1 and tx3 are from the same address (potential attacker)
	if tx1.From != tx3.From {
		return
	}

	// Check for swap events in all three transactions
	swap1 := findSwapEvent(logs1)
	swap2 := findSwapEvent(logs2)
	swap3 := findSwapEvent(logs3)

	if swap1 == nil || swap2 == nil || swap3 == nil {
		return
	}

	// Check if all swaps are on the same pool
	if swap1.Address != swap2.Address || swap2.Address != swap3.Address {
		return
	}

	// This looks like a sandwich attack
	sandwich := SandwichAttack{
		ID:              fmt.Sprintf("sandwich-%d-%s", blockNumber, tx2Hash),
		BlockNumber:     blockNumber,
		FrontrunTxHash:  tx1Hash,
		VictimTxHash:    tx2Hash,
		BackrunTxHash:   tx3Hash,
		AttackerAddress: tx1.From,
		VictimAddress:   tx2.From,
		PoolAddress:     swap1.Address,
		Timestamp:       timestamp,
	}

	// Calculate profit and loss (simplified)
	sandwich.AttackerProfitETH = "0" // Would need price oracle for accurate calculation
	sandwich.VictimLossETH = "0"

	m.StoreSandwich(ctx, sandwich)

	// Also store as MEV transaction
	m.StoreMEVTransaction(ctx, MEVTransaction{
		ID:               sandwich.ID,
		TransactionHash:  tx3Hash,
		BlockNumber:      blockNumber,
		MEVType:          MEVTypeSandwich,
		ExtractorAddress: tx1.From,
		VictimAddress:    tx2.From,
		Protocol:         detectProtocol(swap1.Address),
		ProfitETH:        sandwich.AttackerProfitETH,
		GasCostETH:       calculateGasCost(tx1, tx3),
		RelatedTxHashes:  []string{tx1Hash, tx2Hash, tx3Hash},
		Timestamp:        timestamp,
		Confidence:       0.8,
	})
}

// detectArbitrage detects arbitrage transactions
func (m *MEVIndexer) detectArbitrage(ctx context.Context, txHash string, blockNumber uint64, timestamp time.Time) {
	tx, logs, err := m.adapter.GetTransactionReceipt(ctx, txHash)
	if err != nil || tx.Status != 1 {
		return
	}

	// Count swap events
	var swaps []Log
	for _, log := range logs {
		if len(log.Topics) > 0 && (log.Topics[0] == TopicSwap || log.Topics[0] == TopicSwapV3) {
			swaps = append(swaps, log)
		}
	}

	// Arbitrage typically has 2+ swaps
	if len(swaps) < 2 {
		return
	}

	// Check for circular arbitrage (start and end with same token)
	// This is a simplified detection - real detection would track token flows
	var tokens []string
	var pools []string
	for _, swap := range swaps {
		pools = append(pools, swap.Address)
		// Would extract tokens from swap data
	}

	// Check if it's atomic (uses flash loan)
	isAtomic := detectFlashLoan(logs)

	// Check if the transaction starts and ends with the same ETH balance change
	// (circular arbitrage typically returns to starting token)
	arb := ArbitrageTx{
		ID:                fmt.Sprintf("arb-%d-%s", blockNumber, txHash),
		TransactionHash:   txHash,
		BlockNumber:       blockNumber,
		ArbitragerAddress: tx.From,
		PathLength:        len(swaps),
		Tokens:            tokens,
		Pools:             pools,
		IsAtomic:          isAtomic,
		Timestamp:         timestamp,
	}

	// Calculate profit (simplified)
	arb.ProfitETH = "0" // Would need actual calculation

	// Only store if confidence is high enough
	if len(swaps) >= 3 || isAtomic {
		m.StoreArbitrage(ctx, arb)

		m.StoreMEVTransaction(ctx, MEVTransaction{
			ID:               arb.ID,
			TransactionHash:  txHash,
			BlockNumber:      blockNumber,
			MEVType:          MEVTypeArbitrage,
			ExtractorAddress: tx.From,
			ProfitETH:        arb.ProfitETH,
			GasCostETH:       calculateGasCostSingle(tx),
			Timestamp:        timestamp,
			Confidence:       0.7,
		})
	}
}

// detectLiquidation detects liquidation transactions
func (m *MEVIndexer) detectLiquidation(ctx context.Context, txHash string, blockNumber uint64, timestamp time.Time) {
	tx, logs, err := m.adapter.GetTransactionReceipt(ctx, txHash)
	if err != nil || tx.Status != 1 {
		return
	}

	// Look for liquidation events
	for _, log := range logs {
		if len(log.Topics) == 0 {
			continue
		}

		// Check for Aave/Compound liquidation events
		if log.Topics[0] == TopicLiquidation {
			liq := LiquidationTx{
				ID:                fmt.Sprintf("liq-%d-%s", blockNumber, txHash),
				TransactionHash:   txHash,
				BlockNumber:       blockNumber,
				LiquidatorAddress: tx.From,
				Protocol:          detectLendingProtocol(log.Address),
				Timestamp:         timestamp,
			}

			// Parse liquidation event data
			if len(log.Topics) >= 4 {
				liq.CollateralToken = topicToAddress(log.Topics[1])
				liq.DebtToken = topicToAddress(log.Topics[2])
				liq.BorrowerAddress = topicToAddress(log.Topics[3])
			}

			// Parse amounts from data
			if len(log.Data) >= 130 {
				data := strings.TrimPrefix(log.Data, "0x")
				liq.DebtRepaid = hexToBigInt("0x" + data[0:64]).String()
				liq.CollateralSeized = hexToBigInt("0x" + data[64:128]).String()
			}

			m.StoreLiquidation(ctx, liq)

			m.StoreMEVTransaction(ctx, MEVTransaction{
				ID:               liq.ID,
				TransactionHash:  txHash,
				BlockNumber:      blockNumber,
				MEVType:          MEVTypeLiquidation,
				ExtractorAddress: tx.From,
				VictimAddress:    liq.BorrowerAddress,
				Protocol:         liq.Protocol,
				ProfitETH:        liq.LiquidatorProfitETH,
				GasCostETH:       calculateGasCostSingle(tx),
				Timestamp:        timestamp,
				Confidence:       0.95,
			})

			break
		}
	}
}

// detectPrivateTx detects private/Flashbots transactions
func (m *MEVIndexer) detectPrivateTx(ctx context.Context, txHash, from, to, value, gasPrice string, blockNumber uint64, timestamp time.Time) {
	// Simple heuristic: very high gas price might indicate private tx
	gasPriceBI := hexToBigInt(gasPrice)

	// If gas price is significantly above base fee, might be private
	// This is a simplified heuristic - real detection would compare to block base fee
	threshold := new(big.Int)
	threshold.SetString("100000000000", 10) // 100 gwei

	if gasPriceBI.Cmp(threshold) > 0 {
		m.db.ExecContext(ctx, `
			INSERT INTO mev_private_txs (transaction_hash, block_number, tx_from, tx_to, value, gas_price, is_flashbots, timestamp)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (transaction_hash) DO NOTHING
		`, txHash, blockNumber, from, to, value, gasPrice, false, timestamp)
	}
}

// StoreMEVTransaction stores an MEV transaction
func (m *MEVIndexer) StoreMEVTransaction(ctx context.Context, tx MEVTransaction) error {
	relatedJSON, _ := json.Marshal(tx.RelatedTxHashes)
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO mev_transactions
		(id, transaction_hash, block_number, mev_type, extractor_address, victim_address,
		 protocol, profit_eth, profit_usd, gas_cost_eth, related_tx_hashes, confidence, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (id) DO UPDATE SET
			confidence = GREATEST(mev_transactions.confidence, EXCLUDED.confidence)
	`, tx.ID, tx.TransactionHash, tx.BlockNumber, tx.MEVType, tx.ExtractorAddress,
		tx.VictimAddress, tx.Protocol, tx.ProfitETH, tx.ProfitUSD, tx.GasCostETH,
		relatedJSON, tx.Confidence, tx.Timestamp)
	return err
}

// StoreSandwich stores a sandwich attack
func (m *MEVIndexer) StoreSandwich(ctx context.Context, s SandwichAttack) error {
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO mev_sandwiches
		(id, block_number, frontrun_tx_hash, victim_tx_hash, backrun_tx_hash,
		 attacker_address, victim_address, token_address, pool_address,
		 victim_loss_eth, attacker_profit_eth, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO NOTHING
	`, s.ID, s.BlockNumber, s.FrontrunTxHash, s.VictimTxHash, s.BackrunTxHash,
		s.AttackerAddress, s.VictimAddress, s.TokenAddress, s.PoolAddress,
		s.VictimLossETH, s.AttackerProfitETH, s.Timestamp)
	return err
}

// StoreArbitrage stores an arbitrage transaction
func (m *MEVIndexer) StoreArbitrage(ctx context.Context, a ArbitrageTx) error {
	tokensJSON, _ := json.Marshal(a.Tokens)
	poolsJSON, _ := json.Marshal(a.Pools)
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO mev_arbitrages
		(id, transaction_hash, block_number, arbitrager_address, profit_eth, profit_usd,
		 path_length, tokens, pools, is_atomic, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO NOTHING
	`, a.ID, a.TransactionHash, a.BlockNumber, a.ArbitragerAddress, a.ProfitETH,
		a.ProfitUSD, a.PathLength, tokensJSON, poolsJSON, a.IsAtomic, a.Timestamp)
	return err
}

// StoreLiquidation stores a liquidation transaction
func (m *MEVIndexer) StoreLiquidation(ctx context.Context, l LiquidationTx) error {
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO mev_liquidations
		(id, transaction_hash, block_number, liquidator_address, borrower_address,
		 protocol, collateral_token, debt_token, collateral_seized, debt_repaid,
		 liquidator_profit_eth, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO NOTHING
	`, l.ID, l.TransactionHash, l.BlockNumber, l.LiquidatorAddress, l.BorrowerAddress,
		l.Protocol, l.CollateralToken, l.DebtToken, l.CollateralSeized, l.DebtRepaid,
		l.LiquidatorProfitETH, l.Timestamp)
	return err
}

// GetMEVTransactions retrieves MEV transactions with filters
func (m *MEVIndexer) GetMEVTransactions(ctx context.Context, mevType string, limit, offset int) ([]MEVTransaction, error) {
	query := `
		SELECT id, transaction_hash, block_number, mev_type, extractor_address, victim_address,
			   protocol, profit_eth, profit_usd, gas_cost_eth, related_tx_hashes, confidence, timestamp
		FROM mev_transactions
	`
	var args []interface{}
	argCount := 0

	if mevType != "" {
		argCount++
		query += fmt.Sprintf(" WHERE mev_type = $%d", argCount)
		args = append(args, mevType)
	}

	query += " ORDER BY block_number DESC, timestamp DESC"
	argCount++
	query += fmt.Sprintf(" LIMIT $%d", argCount)
	args = append(args, limit)
	argCount++
	query += fmt.Sprintf(" OFFSET $%d", argCount)
	args = append(args, offset)

	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txs []MEVTransaction
	for rows.Next() {
		var tx MEVTransaction
		var victim, protocol, profitUSD sql.NullString
		var relatedJSON []byte

		err := rows.Scan(
			&tx.ID, &tx.TransactionHash, &tx.BlockNumber, &tx.MEVType, &tx.ExtractorAddress,
			&victim, &protocol, &tx.ProfitETH, &profitUSD, &tx.GasCostETH,
			&relatedJSON, &tx.Confidence, &tx.Timestamp,
		)
		if err != nil {
			continue
		}

		if victim.Valid {
			tx.VictimAddress = victim.String
		}
		if protocol.Valid {
			tx.Protocol = protocol.String
		}
		if profitUSD.Valid {
			tx.ProfitUSD = profitUSD.String
		}
		if len(relatedJSON) > 0 {
			json.Unmarshal(relatedJSON, &tx.RelatedTxHashes)
		}

		txs = append(txs, tx)
	}

	return txs, nil
}

// GetSandwiches retrieves sandwich attacks
func (m *MEVIndexer) GetSandwiches(ctx context.Context, limit, offset int) ([]SandwichAttack, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, block_number, frontrun_tx_hash, victim_tx_hash, backrun_tx_hash,
			   attacker_address, victim_address, token_address, pool_address,
			   victim_loss_eth, attacker_profit_eth, timestamp
		FROM mev_sandwiches
		ORDER BY block_number DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sandwiches []SandwichAttack
	for rows.Next() {
		var s SandwichAttack
		err := rows.Scan(
			&s.ID, &s.BlockNumber, &s.FrontrunTxHash, &s.VictimTxHash, &s.BackrunTxHash,
			&s.AttackerAddress, &s.VictimAddress, &s.TokenAddress, &s.PoolAddress,
			&s.VictimLossETH, &s.AttackerProfitETH, &s.Timestamp,
		)
		if err != nil {
			continue
		}
		sandwiches = append(sandwiches, s)
	}

	return sandwiches, nil
}

// UpdateStats updates MEV statistics
func (m *MEVIndexer) UpdateStats(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, `
		UPDATE mev_stats SET
			total_mev_transactions = (SELECT COUNT(*) FROM mev_transactions),
			total_sandwiches = (SELECT COUNT(*) FROM mev_sandwiches),
			total_arbitrages = (SELECT COUNT(*) FROM mev_arbitrages),
			total_liquidations = (SELECT COUNT(*) FROM mev_liquidations),
			total_private_txs = (SELECT COUNT(*) FROM mev_private_txs),
			total_profit_eth = COALESCE((SELECT SUM(CAST(profit_eth AS NUMERIC)) FROM mev_transactions), 0)::TEXT,
			total_victim_loss_eth = COALESCE((SELECT SUM(CAST(victim_loss_eth AS NUMERIC)) FROM mev_sandwiches), 0)::TEXT,
			updated_at = NOW()
		WHERE id = 1
	`)
	return err
}

// GetStats retrieves MEV statistics
func (m *MEVIndexer) GetStats(ctx context.Context) (map[string]interface{}, error) {
	var totalTxs, totalSandwiches, totalArbs, totalLiqs, totalPrivate int64
	var totalProfit, totalVictimLoss string
	var updatedAt time.Time

	err := m.db.QueryRowContext(ctx, `
		SELECT total_mev_transactions, total_sandwiches, total_arbitrages, total_liquidations,
			   total_private_txs, total_profit_eth, total_victim_loss_eth, updated_at
		FROM mev_stats WHERE id = 1
	`).Scan(&totalTxs, &totalSandwiches, &totalArbs, &totalLiqs, &totalPrivate,
		&totalProfit, &totalVictimLoss, &updatedAt)

	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	return map[string]interface{}{
		"total_mev_transactions": totalTxs,
		"total_sandwiches":       totalSandwiches,
		"total_arbitrages":       totalArbs,
		"total_liquidations":     totalLiqs,
		"total_private_txs":      totalPrivate,
		"total_profit_eth":       totalProfit,
		"total_victim_loss_eth":  totalVictimLoss,
		"updated_at":             updatedAt,
	}, nil
}

// Helper functions

func findSwapEvent(logs []Log) *Log {
	for _, log := range logs {
		if len(log.Topics) > 0 && (log.Topics[0] == TopicSwap || log.Topics[0] == TopicSwapV3) {
			return &log
		}
	}
	return nil
}

func detectFlashLoan(logs []Log) bool {
	// Look for flash loan signatures
	flashLoanTopics := []string{
		"0x631042c832b07452973831137f2d73e395028b44b250dedc5abb0ee766e168ac", // Aave FlashLoan
		"0x5c29b9b7b0c0d3b0f9f5c0f0c0d3b0f9f5c0f0c0d3b0f9f5c0f0c0d3b0f9f5c0", // Balancer FlashLoan
	}

	for _, log := range logs {
		if len(log.Topics) > 0 {
			for _, topic := range flashLoanTopics {
				if log.Topics[0] == topic {
					return true
				}
			}
		}
	}
	return false
}

func detectProtocol(poolAddress string) string {
	// Would have a mapping of known pool addresses to protocols
	// Simplified version
	return "Unknown DEX"
}

func detectLendingProtocol(address string) string {
	addr := strings.ToLower(address)
	if addr == AaveV2LendingPool || addr == AaveV3Pool {
		return "Aave"
	}
	if addr == CompoundComptroller {
		return "Compound"
	}
	return "Unknown"
}

func calculateGasCost(tx1, tx3 *Transaction) string {
	gas1 := new(big.Int).SetUint64(tx1.GasUsed)
	gas3 := new(big.Int).SetUint64(tx3.GasUsed)
	gasPrice := hexToBigInt(tx1.GasPrice)

	totalGas := new(big.Int).Add(gas1, gas3)
	cost := new(big.Int).Mul(totalGas, gasPrice)

	return cost.String()
}

func calculateGasCostSingle(tx *Transaction) string {
	gas := new(big.Int).SetUint64(tx.GasUsed)
	gasPrice := hexToBigInt(tx.GasPrice)
	cost := new(big.Int).Mul(gas, gasPrice)
	return cost.String()
}
