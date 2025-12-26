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

// EnhancedIndexer handles advanced block and transaction indexing
type EnhancedIndexer struct {
	adapter *Adapter
	db      *sql.DB
}

// NewEnhancedIndexer creates a new enhanced indexer
func NewEnhancedIndexer(adapter *Adapter, db *sql.DB) *EnhancedIndexer {
	return &EnhancedIndexer{
		adapter: adapter,
		db:      db,
	}
}

// InitSchema creates enhanced indexing database tables
func (e *EnhancedIndexer) InitSchema() error {
	schema := `
		-- Uncle blocks table
		CREATE TABLE IF NOT EXISTS block_uncles (
			hash TEXT PRIMARY KEY,
			block_number BIGINT NOT NULL,
			uncle_index BIGINT NOT NULL,
			parent_hash TEXT NOT NULL,
			miner TEXT NOT NULL,
			difficulty TEXT,
			gas_limit BIGINT,
			gas_used BIGINT,
			reward TEXT NOT NULL DEFAULT '0',
			timestamp TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_uncles_block ON block_uncles(block_number);
		CREATE INDEX IF NOT EXISTS idx_uncles_miner ON block_uncles(miner);

		-- Block rewards table
		CREATE TABLE IF NOT EXISTS block_rewards (
			block_number BIGINT PRIMARY KEY,
			block_hash TEXT NOT NULL,
			validator TEXT NOT NULL,
			base_reward TEXT NOT NULL DEFAULT '0',
			tx_fee_reward TEXT NOT NULL DEFAULT '0',
			uncle_reward TEXT NOT NULL DEFAULT '0',
			total_reward TEXT NOT NULL DEFAULT '0',
			burnt_fees TEXT NOT NULL DEFAULT '0',
			timestamp TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_rewards_validator ON block_rewards(validator);
		CREATE INDEX IF NOT EXISTS idx_rewards_timestamp ON block_rewards(timestamp);

		-- Withdrawals table (EIP-4895)
		CREATE TABLE IF NOT EXISTS withdrawals (
			id BIGSERIAL PRIMARY KEY,
			withdrawal_index BIGINT NOT NULL,
			validator_index BIGINT NOT NULL,
			address TEXT NOT NULL,
			amount TEXT NOT NULL,
			block_number BIGINT NOT NULL,
			block_hash TEXT NOT NULL,
			timestamp TIMESTAMPTZ NOT NULL,
			UNIQUE(block_number, withdrawal_index)
		);
		CREATE INDEX IF NOT EXISTS idx_withdrawals_block ON withdrawals(block_number);
		CREATE INDEX IF NOT EXISTS idx_withdrawals_validator ON withdrawals(validator_index);
		CREATE INDEX IF NOT EXISTS idx_withdrawals_address ON withdrawals(address);

		-- Pending transactions table
		CREATE TABLE IF NOT EXISTS pending_transactions (
			hash TEXT PRIMARY KEY,
			tx_from TEXT NOT NULL,
			tx_to TEXT,
			value TEXT NOT NULL DEFAULT '0',
			gas BIGINT NOT NULL,
			gas_price TEXT,
			max_fee_per_gas TEXT,
			max_priority_fee TEXT,
			nonce BIGINT NOT NULL,
			input TEXT,
			tx_type SMALLINT DEFAULT 0,
			first_seen TIMESTAMPTZ NOT NULL,
			last_seen TIMESTAMPTZ NOT NULL,
			seen_count INT DEFAULT 1,
			replaced_by TEXT,
			status TEXT DEFAULT 'pending'
		);
		CREATE INDEX IF NOT EXISTS idx_pending_from ON pending_transactions(tx_from);
		CREATE INDEX IF NOT EXISTS idx_pending_status ON pending_transactions(status);
		CREATE INDEX IF NOT EXISTS idx_pending_first_seen ON pending_transactions(first_seen);

		-- Replaced transactions table
		CREATE TABLE IF NOT EXISTS replaced_transactions (
			id BIGSERIAL PRIMARY KEY,
			original_hash TEXT NOT NULL,
			replacement_hash TEXT NOT NULL,
			block_number BIGINT,
			tx_from TEXT NOT NULL,
			nonce BIGINT NOT NULL,
			old_gas_price TEXT,
			new_gas_price TEXT,
			replacement_type TEXT NOT NULL,
			timestamp TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_replaced_original ON replaced_transactions(original_hash);
		CREATE INDEX IF NOT EXISTS idx_replaced_replacement ON replaced_transactions(replacement_hash);
		CREATE INDEX IF NOT EXISTS idx_replaced_from ON replaced_transactions(tx_from);

		-- Transaction revert reasons table
		CREATE TABLE IF NOT EXISTS tx_revert_reasons (
			transaction_hash TEXT PRIMARY KEY,
			revert_reason TEXT,
			decoded_reason TEXT,
			error_selector TEXT,
			error_name TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_revert_error ON tx_revert_reasons(error_selector) WHERE error_selector IS NOT NULL;

		-- Access lists table (EIP-2930)
		CREATE TABLE IF NOT EXISTS tx_access_lists (
			transaction_hash TEXT NOT NULL,
			entry_index INT NOT NULL,
			address TEXT NOT NULL,
			storage_keys JSONB NOT NULL DEFAULT '[]',
			PRIMARY KEY (transaction_hash, entry_index)
		);
		CREATE INDEX IF NOT EXISTS idx_access_list_address ON tx_access_lists(address);

		-- Enhanced blocks table (additional fields)
		CREATE TABLE IF NOT EXISTS enhanced_blocks (
			block_number BIGINT PRIMARY KEY,
			hash TEXT NOT NULL,
			parent_hash TEXT NOT NULL,
			miner TEXT NOT NULL,
			difficulty TEXT,
			total_difficulty TEXT,
			size BIGINT,
			gas_limit BIGINT,
			gas_used BIGINT,
			base_fee_per_gas TEXT,
			extra_data TEXT,
			state_root TEXT,
			transactions_root TEXT,
			receipts_root TEXT,
			logs_bloom TEXT,
			tx_count INT DEFAULT 0,
			uncle_count INT DEFAULT 0,
			uncle_hashes JSONB DEFAULT '[]',
			-- EIP-4844 fields
			blob_gas_used BIGINT,
			excess_blob_gas BIGINT,
			parent_beacon_root TEXT,
			-- EIP-4895 withdrawals
			withdrawals_root TEXT,
			timestamp TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_enhanced_blocks_hash ON enhanced_blocks(hash);
		CREATE INDEX IF NOT EXISTS idx_enhanced_blocks_miner ON enhanced_blocks(miner);
		CREATE INDEX IF NOT EXISTS idx_enhanced_blocks_timestamp ON enhanced_blocks(timestamp);

		-- Enhanced stats table
		CREATE TABLE IF NOT EXISTS enhanced_stats (
			id INT PRIMARY KEY DEFAULT 1,
			total_uncles BIGINT DEFAULT 0,
			total_withdrawals BIGINT DEFAULT 0,
			total_withdrawal_amount TEXT DEFAULT '0',
			total_block_rewards TEXT DEFAULT '0',
			total_burnt_fees TEXT DEFAULT '0',
			pending_tx_count BIGINT DEFAULT 0,
			replaced_tx_count BIGINT DEFAULT 0,
			reverted_tx_count BIGINT DEFAULT 0,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		INSERT INTO enhanced_stats (id) VALUES (1) ON CONFLICT DO NOTHING;
	`

	_, err := e.db.Exec(schema)
	return err
}

// ProcessEnhancedBlock processes a block with all enhanced features
func (e *EnhancedIndexer) ProcessEnhancedBlock(ctx context.Context, blockNumber uint64) error {
	blockData, err := e.adapter.GetBlockByHeight(ctx, blockNumber)
	if err != nil {
		return err
	}

	var block struct {
		Hash                  string `json:"hash"`
		ParentHash            string `json:"parentHash"`
		Miner                 string `json:"miner"`
		Difficulty            string `json:"difficulty"`
		TotalDifficulty       string `json:"totalDifficulty"`
		Size                  string `json:"size"`
		GasLimit              string `json:"gasLimit"`
		GasUsed               string `json:"gasUsed"`
		BaseFeePerGas         string `json:"baseFeePerGas"`
		ExtraData             string `json:"extraData"`
		StateRoot             string `json:"stateRoot"`
		TransactionsRoot      string `json:"transactionsRoot"`
		ReceiptsRoot          string `json:"receiptsRoot"`
		LogsBloom             string `json:"logsBloom"`
		Timestamp             string `json:"timestamp"`
		Transactions          []json.RawMessage `json:"transactions"`
		Uncles                []string `json:"uncles"`
		Withdrawals           []struct {
			Index          string `json:"index"`
			ValidatorIndex string `json:"validatorIndex"`
			Address        string `json:"address"`
			Amount         string `json:"amount"`
		} `json:"withdrawals"`
		WithdrawalsRoot       string `json:"withdrawalsRoot"`
		BlobGasUsed           string `json:"blobGasUsed"`
		ExcessBlobGas         string `json:"excessBlobGas"`
		ParentBeaconBlockRoot string `json:"parentBeaconBlockRoot"`
	}

	if err := json.Unmarshal(blockData, &block); err != nil {
		return err
	}

	timestamp := time.Unix(int64(hexToUint64(block.Timestamp)), 0)

	// Store enhanced block
	enhanced := EnhancedBlock{
		Hash:             block.Hash,
		ParentHash:       block.ParentHash,
		Number:           blockNumber,
		Timestamp:        timestamp,
		Miner:            strings.ToLower(block.Miner),
		Difficulty:       hexToBigInt(block.Difficulty).String(),
		TotalDifficulty:  hexToBigInt(block.TotalDifficulty).String(),
		Size:             hexToUint64(block.Size),
		GasLimit:         hexToUint64(block.GasLimit),
		GasUsed:          hexToUint64(block.GasUsed),
		BaseFeePerGas:    hexToBigInt(block.BaseFeePerGas).String(),
		ExtraData:        block.ExtraData,
		StateRoot:        block.StateRoot,
		TransactionsRoot: block.TransactionsRoot,
		ReceiptsRoot:     block.ReceiptsRoot,
		LogsBloom:        block.LogsBloom,
		TxCount:          len(block.Transactions),
		UncleCount:       len(block.Uncles),
		UncleHashes:      block.Uncles,
		BlobGasUsed:      hexToUint64(block.BlobGasUsed),
		ExcessBlobGas:    hexToUint64(block.ExcessBlobGas),
		ParentBeaconRoot: block.ParentBeaconBlockRoot,
		WithdrawalsRoot:  block.WithdrawalsRoot,
	}

	if err := e.StoreEnhancedBlock(ctx, enhanced); err != nil {
		return err
	}

	// Process uncles
	for i, uncleHash := range block.Uncles {
		if err := e.processUncle(ctx, blockNumber, uint64(i), uncleHash, timestamp); err != nil {
			continue
		}
	}

	// Process withdrawals
	for _, w := range block.Withdrawals {
		withdrawal := Withdrawal{
			Index:          hexToUint64(w.Index),
			ValidatorIndex: hexToUint64(w.ValidatorIndex),
			Address:        strings.ToLower(w.Address),
			Amount:         hexToBigInt(w.Amount).String(),
			BlockNumber:    blockNumber,
			BlockHash:      block.Hash,
			Timestamp:      timestamp,
		}
		e.StoreWithdrawal(ctx, withdrawal)
	}

	// Calculate and store block reward
	if err := e.calculateBlockReward(ctx, blockNumber, block.Hash, block.Miner, block.GasUsed, block.BaseFeePerGas, len(block.Uncles), timestamp); err != nil {
		// Non-fatal
	}

	return nil
}

// processUncle fetches and stores uncle block data
func (e *EnhancedIndexer) processUncle(ctx context.Context, blockNumber, uncleIndex uint64, uncleHash string, timestamp time.Time) error {
	// Fetch uncle by block hash and index
	result, err := e.adapter.call(ctx, "eth_getUncleByBlockNumberAndIndex", []interface{}{
		fmt.Sprintf("0x%x", blockNumber),
		fmt.Sprintf("0x%x", uncleIndex),
	})
	if err != nil {
		return err
	}

	var uncleBlock struct {
		Hash       string `json:"hash"`
		ParentHash string `json:"parentHash"`
		Miner      string `json:"miner"`
		Difficulty string `json:"difficulty"`
		GasLimit   string `json:"gasLimit"`
		GasUsed    string `json:"gasUsed"`
		Timestamp  string `json:"timestamp"`
	}

	if err := json.Unmarshal(result, &uncleBlock); err != nil {
		return err
	}

	uncle := Uncle{
		Hash:        uncleBlock.Hash,
		BlockNumber: blockNumber,
		UncleIndex:  uncleIndex,
		ParentHash:  uncleBlock.ParentHash,
		Miner:       strings.ToLower(uncleBlock.Miner),
		Difficulty:  hexToBigInt(uncleBlock.Difficulty).String(),
		GasLimit:    hexToUint64(uncleBlock.GasLimit),
		GasUsed:     hexToUint64(uncleBlock.GasUsed),
		Timestamp:   time.Unix(int64(hexToUint64(uncleBlock.Timestamp)), 0),
	}

	// Calculate uncle reward (7/8 of base block reward for immediate uncle)
	// Base reward for Ethereum PoS is 0, but for PoW chains:
	// uncle_reward = base_reward * (8 - (block_number - uncle_number)) / 8
	uncle.Reward = "0" // For Lux PoS

	return e.StoreUncle(ctx, uncle)
}

// calculateBlockReward calculates and stores block reward
func (e *EnhancedIndexer) calculateBlockReward(ctx context.Context, blockNumber uint64, blockHash, miner, gasUsed, baseFee string, uncleCount int, timestamp time.Time) error {
	// For Lux C-Chain (PoS), block rewards come from:
	// 1. Base reward: 0 (no block subsidy in PoS)
	// 2. Transaction fees: sum of (gas_used * effective_gas_price - base_fee * gas_used)
	// 3. Uncle rewards: N/A for PoS

	gasUsedBI := hexToUint64(gasUsed)
	baseFeeBI := hexToBigInt(baseFee)

	// Calculate burnt fees (EIP-1559)
	burntFees := new(big.Int).Mul(baseFeeBI, big.NewInt(int64(gasUsedBI)))

	// Get transaction fees from transactions
	// This is simplified - would need to sum all tx fees
	txFeeReward := big.NewInt(0)

	reward := BlockReward{
		BlockNumber:   blockNumber,
		BlockHash:     blockHash,
		Validator:     strings.ToLower(miner),
		BaseReward:    "0", // PoS has no base reward
		TxFeeReward:   txFeeReward.String(),
		UncleReward:   "0", // PoS has no uncle rewards
		TotalReward:   txFeeReward.String(),
		BurntFees:     burntFees.String(),
		Timestamp:     timestamp,
	}

	return e.StoreBlockReward(ctx, reward)
}

// StoreEnhancedBlock stores an enhanced block
func (e *EnhancedIndexer) StoreEnhancedBlock(ctx context.Context, block EnhancedBlock) error {
	uncleHashesJSON, _ := json.Marshal(block.UncleHashes)
	_, err := e.db.ExecContext(ctx, `
		INSERT INTO enhanced_blocks
		(block_number, hash, parent_hash, miner, difficulty, total_difficulty, size, gas_limit,
		 gas_used, base_fee_per_gas, extra_data, state_root, transactions_root, receipts_root,
		 logs_bloom, tx_count, uncle_count, uncle_hashes, blob_gas_used, excess_blob_gas,
		 parent_beacon_root, withdrawals_root, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)
		ON CONFLICT (block_number) DO UPDATE SET
			uncle_count = EXCLUDED.uncle_count,
			uncle_hashes = EXCLUDED.uncle_hashes
	`, block.Number, block.Hash, block.ParentHash, block.Miner, block.Difficulty,
		block.TotalDifficulty, block.Size, block.GasLimit, block.GasUsed,
		block.BaseFeePerGas, block.ExtraData, block.StateRoot, block.TransactionsRoot,
		block.ReceiptsRoot, block.LogsBloom, block.TxCount, block.UncleCount,
		uncleHashesJSON, block.BlobGasUsed, block.ExcessBlobGas,
		block.ParentBeaconRoot, block.WithdrawalsRoot, block.Timestamp)
	return err
}

// StoreUncle stores an uncle block
func (e *EnhancedIndexer) StoreUncle(ctx context.Context, uncle Uncle) error {
	_, err := e.db.ExecContext(ctx, `
		INSERT INTO block_uncles
		(hash, block_number, uncle_index, parent_hash, miner, difficulty, gas_limit, gas_used, reward, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (hash) DO NOTHING
	`, uncle.Hash, uncle.BlockNumber, uncle.UncleIndex, uncle.ParentHash, uncle.Miner,
		uncle.Difficulty, uncle.GasLimit, uncle.GasUsed, uncle.Reward, uncle.Timestamp)
	return err
}

// StoreBlockReward stores a block reward
func (e *EnhancedIndexer) StoreBlockReward(ctx context.Context, reward BlockReward) error {
	_, err := e.db.ExecContext(ctx, `
		INSERT INTO block_rewards
		(block_number, block_hash, validator, base_reward, tx_fee_reward, uncle_reward, total_reward, burnt_fees, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (block_number) DO UPDATE SET
			total_reward = EXCLUDED.total_reward,
			burnt_fees = EXCLUDED.burnt_fees
	`, reward.BlockNumber, reward.BlockHash, reward.Validator, reward.BaseReward,
		reward.TxFeeReward, reward.UncleReward, reward.TotalReward, reward.BurntFees, reward.Timestamp)
	return err
}

// StoreWithdrawal stores a withdrawal
func (e *EnhancedIndexer) StoreWithdrawal(ctx context.Context, w Withdrawal) error {
	_, err := e.db.ExecContext(ctx, `
		INSERT INTO withdrawals
		(withdrawal_index, validator_index, address, amount, block_number, block_hash, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (block_number, withdrawal_index) DO NOTHING
	`, w.Index, w.ValidatorIndex, w.Address, w.Amount, w.BlockNumber, w.BlockHash, w.Timestamp)
	return err
}

// StorePendingTransaction stores or updates a pending transaction
func (e *EnhancedIndexer) StorePendingTransaction(ctx context.Context, tx PendingTransaction) error {
	_, err := e.db.ExecContext(ctx, `
		INSERT INTO pending_transactions
		(hash, tx_from, tx_to, value, gas, gas_price, max_fee_per_gas, max_priority_fee,
		 nonce, input, tx_type, first_seen, last_seen, seen_count, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (hash) DO UPDATE SET
			last_seen = EXCLUDED.last_seen,
			seen_count = pending_transactions.seen_count + 1,
			status = EXCLUDED.status,
			replaced_by = EXCLUDED.replaced_by
	`, tx.Hash, tx.From, tx.To, tx.Value, tx.Gas, tx.GasPrice, tx.MaxFeePerGas,
		tx.MaxPriorityFee, tx.Nonce, tx.Input, tx.Type, tx.FirstSeen, tx.LastSeen,
		tx.SeenCount, tx.Status)
	return err
}

// MarkPendingTxConfirmed marks a pending transaction as confirmed
func (e *EnhancedIndexer) MarkPendingTxConfirmed(ctx context.Context, hash string) error {
	_, err := e.db.ExecContext(ctx, `
		UPDATE pending_transactions SET status = 'confirmed' WHERE hash = $1
	`, hash)
	return err
}

// MarkPendingTxReplaced marks a pending transaction as replaced
func (e *EnhancedIndexer) MarkPendingTxReplaced(ctx context.Context, originalHash, replacementHash string) error {
	_, err := e.db.ExecContext(ctx, `
		UPDATE pending_transactions SET status = 'replaced', replaced_by = $2 WHERE hash = $1
	`, originalHash, replacementHash)
	return err
}

// StoreReplacedTransaction stores a replaced transaction record
func (e *EnhancedIndexer) StoreReplacedTransaction(ctx context.Context, r ReplacedTransaction) error {
	_, err := e.db.ExecContext(ctx, `
		INSERT INTO replaced_transactions
		(original_hash, replacement_hash, block_number, tx_from, nonce, old_gas_price, new_gas_price, replacement_type, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, r.OriginalHash, r.ReplacementHash, r.BlockNumber, r.From, r.Nonce,
		r.OldGasPrice, r.NewGasPrice, r.ReplacementType, r.Timestamp)
	return err
}

// StoreRevertReason stores a transaction revert reason
func (e *EnhancedIndexer) StoreRevertReason(ctx context.Context, r TransactionRevertReason) error {
	_, err := e.db.ExecContext(ctx, `
		INSERT INTO tx_revert_reasons
		(transaction_hash, revert_reason, decoded_reason, error_selector, error_name)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (transaction_hash) DO UPDATE SET
			decoded_reason = EXCLUDED.decoded_reason,
			error_name = EXCLUDED.error_name
	`, r.TransactionHash, r.RevertReason, r.DecodedReason, r.ErrorSelector, r.ErrorName)
	return err
}

// StoreAccessList stores transaction access list entries
func (e *EnhancedIndexer) StoreAccessList(ctx context.Context, txHash string, accessList []AccessListEntry) error {
	for i, entry := range accessList {
		keysJSON, _ := json.Marshal(entry.StorageKeys)
		_, err := e.db.ExecContext(ctx, `
			INSERT INTO tx_access_lists (transaction_hash, entry_index, address, storage_keys)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (transaction_hash, entry_index) DO NOTHING
		`, txHash, i, entry.Address, keysJSON)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetRevertReason retrieves and decodes revert reason for a failed transaction
func (e *EnhancedIndexer) GetRevertReason(ctx context.Context, txHash string) (*TransactionRevertReason, error) {
	// Try to get from cache first
	var r TransactionRevertReason
	err := e.db.QueryRowContext(ctx, `
		SELECT transaction_hash, revert_reason, decoded_reason, error_selector, error_name
		FROM tx_revert_reasons WHERE transaction_hash = $1
	`, txHash).Scan(&r.TransactionHash, &r.RevertReason, &r.DecodedReason, &r.ErrorSelector, &r.ErrorName)

	if err == nil {
		return &r, nil
	}

	if err != sql.ErrNoRows {
		return nil, err
	}

	// Not in cache, try to fetch from node
	result, err := e.adapter.call(ctx, "eth_call", []interface{}{
		map[string]interface{}{
			"data": txHash,
		},
		"latest",
	})
	if err != nil {
		// Parse revert reason from error
		r = e.parseRevertReason(err.Error())
		r.TransactionHash = txHash
		e.StoreRevertReason(ctx, r)
		return &r, nil
	}

	// Check if result contains revert data
	var revertData string
	json.Unmarshal(result, &revertData)
	r = e.parseRevertReason(revertData)
	r.TransactionHash = txHash
	e.StoreRevertReason(ctx, r)

	return &r, nil
}

// parseRevertReason parses and decodes a revert reason
func (e *EnhancedIndexer) parseRevertReason(data string) TransactionRevertReason {
	r := TransactionRevertReason{
		RevertReason: data,
	}

	data = strings.TrimPrefix(data, "0x")
	if len(data) < 8 {
		return r
	}

	selector := "0x" + data[:8]
	r.ErrorSelector = selector

	// Common error selectors
	switch selector {
	case "0x08c379a0": // Error(string)
		r.ErrorName = "Error"
		if len(data) >= 136 {
			// Decode string from ABI
			r.DecodedReason = decodeString("0x" + data[8:])
		}
	case "0x4e487b71": // Panic(uint256)
		r.ErrorName = "Panic"
		if len(data) >= 72 {
			panicCode := hexToUint64("0x" + data[8:72])
			r.DecodedReason = decodePanicCode(panicCode)
		}
	case "0xfb8f41b2": // InsufficientBalance
		r.ErrorName = "InsufficientBalance"
	case "0xe450d38c": // InsufficientAllowance
		r.ErrorName = "InsufficientAllowance"
	default:
		r.ErrorName = "CustomError"
	}

	return r
}

// decodePanicCode decodes Solidity panic codes
func decodePanicCode(code uint64) string {
	switch code {
	case 0x00:
		return "Generic compiler inserted panic"
	case 0x01:
		return "Assert condition failed"
	case 0x11:
		return "Arithmetic overflow/underflow"
	case 0x12:
		return "Division by zero"
	case 0x21:
		return "Invalid enum value"
	case 0x22:
		return "Storage byte array that is incorrectly encoded"
	case 0x31:
		return "Pop on empty array"
	case 0x32:
		return "Array access out of bounds"
	case 0x41:
		return "Too much memory allocated"
	case 0x51:
		return "Called invalid internal function"
	default:
		return fmt.Sprintf("Panic code: 0x%x", code)
	}
}

// GetWithdrawals retrieves withdrawals for a block
func (e *EnhancedIndexer) GetWithdrawals(ctx context.Context, blockNumber uint64) ([]Withdrawal, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT withdrawal_index, validator_index, address, amount, block_number, block_hash, timestamp
		FROM withdrawals WHERE block_number = $1
		ORDER BY withdrawal_index
	`, blockNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var withdrawals []Withdrawal
	for rows.Next() {
		var w Withdrawal
		err := rows.Scan(&w.Index, &w.ValidatorIndex, &w.Address, &w.Amount, &w.BlockNumber, &w.BlockHash, &w.Timestamp)
		if err != nil {
			continue
		}
		withdrawals = append(withdrawals, w)
	}

	return withdrawals, nil
}

// GetWithdrawalsByValidator retrieves withdrawals for a validator
func (e *EnhancedIndexer) GetWithdrawalsByValidator(ctx context.Context, validatorIndex uint64, limit, offset int) ([]Withdrawal, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT withdrawal_index, validator_index, address, amount, block_number, block_hash, timestamp
		FROM withdrawals WHERE validator_index = $1
		ORDER BY block_number DESC
		LIMIT $2 OFFSET $3
	`, validatorIndex, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var withdrawals []Withdrawal
	for rows.Next() {
		var w Withdrawal
		err := rows.Scan(&w.Index, &w.ValidatorIndex, &w.Address, &w.Amount, &w.BlockNumber, &w.BlockHash, &w.Timestamp)
		if err != nil {
			continue
		}
		withdrawals = append(withdrawals, w)
	}

	return withdrawals, nil
}

// GetBlockReward retrieves block reward information
func (e *EnhancedIndexer) GetBlockReward(ctx context.Context, blockNumber uint64) (*BlockReward, error) {
	var r BlockReward
	err := e.db.QueryRowContext(ctx, `
		SELECT block_number, block_hash, validator, base_reward, tx_fee_reward, uncle_reward, total_reward, burnt_fees, timestamp
		FROM block_rewards WHERE block_number = $1
	`, blockNumber).Scan(&r.BlockNumber, &r.BlockHash, &r.Validator, &r.BaseReward,
		&r.TxFeeReward, &r.UncleReward, &r.TotalReward, &r.BurntFees, &r.Timestamp)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetPendingTransactions retrieves pending transactions
func (e *EnhancedIndexer) GetPendingTransactions(ctx context.Context, limit, offset int) ([]PendingTransaction, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT hash, tx_from, tx_to, value, gas, gas_price, max_fee_per_gas, max_priority_fee,
			   nonce, input, tx_type, first_seen, last_seen, seen_count, replaced_by, status
		FROM pending_transactions
		WHERE status = 'pending'
		ORDER BY first_seen DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txs []PendingTransaction
	for rows.Next() {
		var tx PendingTransaction
		var to, gasPrice, maxFee, maxPriority, input, replacedBy sql.NullString
		err := rows.Scan(&tx.Hash, &tx.From, &to, &tx.Value, &tx.Gas, &gasPrice,
			&maxFee, &maxPriority, &tx.Nonce, &input, &tx.Type, &tx.FirstSeen,
			&tx.LastSeen, &tx.SeenCount, &replacedBy, &tx.Status)
		if err != nil {
			continue
		}
		if to.Valid {
			tx.To = to.String
		}
		if gasPrice.Valid {
			tx.GasPrice = gasPrice.String
		}
		if maxFee.Valid {
			tx.MaxFeePerGas = maxFee.String
		}
		if maxPriority.Valid {
			tx.MaxPriorityFee = maxPriority.String
		}
		if input.Valid {
			tx.Input = input.String
		}
		if replacedBy.Valid {
			tx.ReplacedBy = replacedBy.String
		}
		txs = append(txs, tx)
	}

	return txs, nil
}

// CleanupOldPendingTxs removes stale pending transactions
func (e *EnhancedIndexer) CleanupOldPendingTxs(ctx context.Context, maxAgeHours int) (int64, error) {
	result, err := e.db.ExecContext(ctx, `
		UPDATE pending_transactions
		SET status = 'dropped'
		WHERE status = 'pending' AND last_seen < NOW() - INTERVAL '%d hours'
	`, maxAgeHours)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// UpdateStats updates enhanced statistics
func (e *EnhancedIndexer) UpdateStats(ctx context.Context) error {
	_, err := e.db.ExecContext(ctx, `
		UPDATE enhanced_stats SET
			total_uncles = (SELECT COUNT(*) FROM block_uncles),
			total_withdrawals = (SELECT COUNT(*) FROM withdrawals),
			total_withdrawal_amount = COALESCE((SELECT SUM(CAST(amount AS NUMERIC)) FROM withdrawals), 0)::TEXT,
			total_block_rewards = COALESCE((SELECT SUM(CAST(total_reward AS NUMERIC)) FROM block_rewards), 0)::TEXT,
			total_burnt_fees = COALESCE((SELECT SUM(CAST(burnt_fees AS NUMERIC)) FROM block_rewards), 0)::TEXT,
			pending_tx_count = (SELECT COUNT(*) FROM pending_transactions WHERE status = 'pending'),
			replaced_tx_count = (SELECT COUNT(*) FROM replaced_transactions),
			reverted_tx_count = (SELECT COUNT(*) FROM tx_revert_reasons),
			updated_at = NOW()
		WHERE id = 1
	`)
	return err
}

// GetStats retrieves enhanced statistics
func (e *EnhancedIndexer) GetStats(ctx context.Context) (map[string]interface{}, error) {
	var totalUncles, totalWithdrawals, pendingTxs, replacedTxs, revertedTxs int64
	var totalWithdrawalAmount, totalRewards, totalBurnt string
	var updatedAt time.Time

	err := e.db.QueryRowContext(ctx, `
		SELECT total_uncles, total_withdrawals, total_withdrawal_amount, total_block_rewards,
			   total_burnt_fees, pending_tx_count, replaced_tx_count, reverted_tx_count, updated_at
		FROM enhanced_stats WHERE id = 1
	`).Scan(&totalUncles, &totalWithdrawals, &totalWithdrawalAmount, &totalRewards,
		&totalBurnt, &pendingTxs, &replacedTxs, &revertedTxs, &updatedAt)

	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	return map[string]interface{}{
		"total_uncles":            totalUncles,
		"total_withdrawals":       totalWithdrawals,
		"total_withdrawal_amount": totalWithdrawalAmount,
		"total_block_rewards":     totalRewards,
		"total_burnt_fees":        totalBurnt,
		"pending_tx_count":        pendingTxs,
		"replaced_tx_count":       replacedTxs,
		"reverted_tx_count":       revertedTxs,
		"updated_at":              updatedAt,
	}, nil
}
