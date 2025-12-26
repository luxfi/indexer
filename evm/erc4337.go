// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package evm

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ERC4337Indexer handles ERC-4337 Account Abstraction indexing
type ERC4337Indexer struct {
	adapter     *Adapter
	db          *sql.DB
	entryPoints []string
}

// NewERC4337Indexer creates a new ERC-4337 indexer
func NewERC4337Indexer(adapter *Adapter, db *sql.DB) *ERC4337Indexer {
	return &ERC4337Indexer{
		adapter:     adapter,
		db:          db,
		entryPoints: []string{EntryPointV06, EntryPointV07},
	}
}

// InitSchema creates ERC-4337 database tables
func (e *ERC4337Indexer) InitSchema() error {
	schema := `
		-- User Operations table
		CREATE TABLE IF NOT EXISTS erc4337_user_operations (
			hash TEXT PRIMARY KEY,
			sender TEXT NOT NULL,
			nonce TEXT NOT NULL,
			init_code TEXT,
			call_data TEXT,
			call_gas_limit BIGINT,
			verification_gas_limit BIGINT,
			pre_verification_gas BIGINT,
			max_fee_per_gas TEXT,
			max_priority_fee_per_gas TEXT,
			paymaster_and_data TEXT,
			signature TEXT,
			entry_point TEXT NOT NULL,
			block_number BIGINT NOT NULL,
			block_hash TEXT NOT NULL,
			transaction_hash TEXT NOT NULL,
			bundler_address TEXT,
			paymaster_address TEXT,
			factory_address TEXT,
			status SMALLINT DEFAULT 1,
			revert_reason TEXT,
			actual_gas_cost TEXT,
			actual_gas_used BIGINT,
			entry_point_version TEXT,
			timestamp TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_4337_ops_sender ON erc4337_user_operations(sender);
		CREATE INDEX IF NOT EXISTS idx_4337_ops_block ON erc4337_user_operations(block_number);
		CREATE INDEX IF NOT EXISTS idx_4337_ops_bundler ON erc4337_user_operations(bundler_address);
		CREATE INDEX IF NOT EXISTS idx_4337_ops_paymaster ON erc4337_user_operations(paymaster_address) WHERE paymaster_address IS NOT NULL;
		CREATE INDEX IF NOT EXISTS idx_4337_ops_factory ON erc4337_user_operations(factory_address) WHERE factory_address IS NOT NULL;
		CREATE INDEX IF NOT EXISTS idx_4337_ops_tx ON erc4337_user_operations(transaction_hash);
		CREATE INDEX IF NOT EXISTS idx_4337_ops_timestamp ON erc4337_user_operations(timestamp);

		-- Bundlers table
		CREATE TABLE IF NOT EXISTS erc4337_bundlers (
			address TEXT PRIMARY KEY,
			total_operations BIGINT DEFAULT 0,
			total_bundles BIGINT DEFAULT 0,
			total_gas_sponsored TEXT DEFAULT '0',
			success_rate FLOAT DEFAULT 1.0,
			first_seen_block BIGINT,
			last_seen_block BIGINT,
			first_seen_timestamp TIMESTAMPTZ,
			last_seen_timestamp TIMESTAMPTZ
		);
		CREATE INDEX IF NOT EXISTS idx_4337_bundlers_ops ON erc4337_bundlers(total_operations DESC);

		-- Paymasters table
		CREATE TABLE IF NOT EXISTS erc4337_paymasters (
			address TEXT PRIMARY KEY,
			total_operations BIGINT DEFAULT 0,
			total_gas_sponsored TEXT DEFAULT '0',
			total_eth_sponsored TEXT DEFAULT '0',
			unique_accounts BIGINT DEFAULT 0,
			first_seen_block BIGINT,
			last_seen_block BIGINT,
			first_seen_timestamp TIMESTAMPTZ,
			last_seen_timestamp TIMESTAMPTZ
		);
		CREATE INDEX IF NOT EXISTS idx_4337_paymasters_ops ON erc4337_paymasters(total_operations DESC);

		-- Account Factories table
		CREATE TABLE IF NOT EXISTS erc4337_factories (
			address TEXT PRIMARY KEY,
			total_accounts BIGINT DEFAULT 0,
			implementation_type TEXT,
			first_seen_block BIGINT,
			last_seen_block BIGINT,
			first_seen_timestamp TIMESTAMPTZ,
			last_seen_timestamp TIMESTAMPTZ
		);
		CREATE INDEX IF NOT EXISTS idx_4337_factories_accounts ON erc4337_factories(total_accounts DESC);

		-- Smart Accounts table
		CREATE TABLE IF NOT EXISTS erc4337_accounts (
			address TEXT PRIMARY KEY,
			factory_address TEXT,
			deployment_tx_hash TEXT,
			deployment_block BIGINT,
			total_operations BIGINT DEFAULT 0,
			total_gas_used TEXT DEFAULT '0',
			created_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ
		);
		CREATE INDEX IF NOT EXISTS idx_4337_accounts_factory ON erc4337_accounts(factory_address);
		CREATE INDEX IF NOT EXISTS idx_4337_accounts_ops ON erc4337_accounts(total_operations DESC);

		-- Bundles table
		CREATE TABLE IF NOT EXISTS erc4337_bundles (
			transaction_hash TEXT PRIMARY KEY,
			block_number BIGINT NOT NULL,
			block_hash TEXT NOT NULL,
			bundler_address TEXT NOT NULL,
			operation_count INT DEFAULT 0,
			operation_hashes JSONB DEFAULT '[]',
			total_gas_used BIGINT,
			gas_price TEXT,
			timestamp TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_4337_bundles_bundler ON erc4337_bundles(bundler_address);
		CREATE INDEX IF NOT EXISTS idx_4337_bundles_block ON erc4337_bundles(block_number);

		-- Stats table
		CREATE TABLE IF NOT EXISTS erc4337_stats (
			id INT PRIMARY KEY DEFAULT 1,
			total_user_operations BIGINT DEFAULT 0,
			total_bundlers BIGINT DEFAULT 0,
			total_paymasters BIGINT DEFAULT 0,
			total_factories BIGINT DEFAULT 0,
			total_smart_accounts BIGINT DEFAULT 0,
			total_bundles BIGINT DEFAULT 0,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		INSERT INTO erc4337_stats (id) VALUES (1) ON CONFLICT DO NOTHING;
	`

	_, err := e.db.Exec(schema)
	return err
}

// ProcessBlock scans a block for ERC-4337 activity
func (e *ERC4337Indexer) ProcessBlock(ctx context.Context, blockNumber uint64, blockHash string, timestamp time.Time) error {
	// Get all logs from entry points in this block
	for _, entryPoint := range e.entryPoints {
		logs, err := e.getEntryPointLogs(ctx, entryPoint, blockNumber)
		if err != nil {
			continue
		}

		if err := e.processEntryPointLogs(ctx, logs, entryPoint, blockNumber, blockHash, timestamp); err != nil {
			return err
		}
	}

	return nil
}

// getEntryPointLogs fetches logs from the entry point contract
func (e *ERC4337Indexer) getEntryPointLogs(ctx context.Context, entryPoint string, blockNumber uint64) ([]Log, error) {
	blockHex := fmt.Sprintf("0x%x", blockNumber)
	result, err := e.adapter.call(ctx, "eth_getLogs", []interface{}{
		map[string]interface{}{
			"address":   entryPoint,
			"fromBlock": blockHex,
			"toBlock":   blockHex,
			"topics":    []string{TopicUserOperationEvent},
		},
	})
	if err != nil {
		return nil, err
	}

	var rawLogs []struct {
		Address     string   `json:"address"`
		Topics      []string `json:"topics"`
		Data        string   `json:"data"`
		LogIndex    string   `json:"logIndex"`
		TxHash      string   `json:"transactionHash"`
		BlockHash   string   `json:"blockHash"`
		BlockNumber string   `json:"blockNumber"`
	}

	if err := json.Unmarshal(result, &rawLogs); err != nil {
		return nil, err
	}

	var logs []Log
	for _, rl := range rawLogs {
		logs = append(logs, Log{
			TxHash:      rl.TxHash,
			LogIndex:    hexToUint64(rl.LogIndex),
			BlockNumber: hexToUint64(rl.BlockNumber),
			Address:     strings.ToLower(rl.Address),
			Topics:      rl.Topics,
			Data:        rl.Data,
		})
	}

	return logs, nil
}

// processEntryPointLogs processes UserOperationEvent logs
func (e *ERC4337Indexer) processEntryPointLogs(ctx context.Context, logs []Log, entryPoint string, blockNumber uint64, blockHash string, timestamp time.Time) error {
	// Group logs by transaction hash for bundle detection
	txLogs := make(map[string][]Log)
	for _, log := range logs {
		txLogs[log.TxHash] = append(txLogs[log.TxHash], log)
	}

	for txHash, logGroup := range txLogs {
		// Get transaction to identify bundler
		tx, _, err := e.adapter.GetTransactionReceipt(ctx, txHash)
		if err != nil {
			continue
		}

		bundlerAddress := strings.ToLower(tx.From)
		var opHashes []string

		for _, log := range logGroup {
			userOp, err := e.parseUserOperationEvent(log, entryPoint, txHash, blockNumber, blockHash, bundlerAddress, timestamp)
			if err != nil {
				continue
			}

			// Store user operation
			if err := e.StoreUserOperation(ctx, userOp); err != nil {
				continue
			}

			opHashes = append(opHashes, userOp.Hash)

			// Update related entities
			e.updateBundler(ctx, bundlerAddress, blockNumber, timestamp)
			if userOp.PaymasterAddress != "" {
				e.updatePaymaster(ctx, userOp.PaymasterAddress, userOp.ActualGasCost, blockNumber, timestamp)
			}
			if userOp.FactoryAddress != "" {
				e.updateFactory(ctx, userOp.FactoryAddress, blockNumber, timestamp)
			}
			e.updateSmartAccount(ctx, userOp.Sender, userOp.FactoryAddress, userOp.ActualGasUsed, blockNumber, txHash, timestamp)
		}

		// Store bundle if multiple ops
		if len(opHashes) > 0 {
			bundle := Bundle{
				TransactionHash: txHash,
				BlockNumber:     blockNumber,
				BlockHash:       blockHash,
				BundlerAddress:  bundlerAddress,
				OperationCount:  len(opHashes),
				OperationHashes: opHashes,
				TotalGasUsed:    tx.GasUsed,
				GasPrice:        tx.GasPrice,
				Timestamp:       timestamp,
			}
			e.StoreBundle(ctx, bundle)
		}
	}

	return nil
}

// parseUserOperationEvent parses a UserOperationEvent log
func (e *ERC4337Indexer) parseUserOperationEvent(log Log, entryPoint, txHash string, blockNumber uint64, blockHash, bundlerAddress string, timestamp time.Time) (*UserOperation, error) {
	if len(log.Topics) < 4 {
		return nil, fmt.Errorf("insufficient topics")
	}

	// UserOperationEvent topics:
	// [0] = event signature
	// [1] = userOpHash (indexed)
	// [2] = sender (indexed)
	// [3] = paymaster (indexed)
	userOpHash := log.Topics[1]
	sender := topicToAddress(log.Topics[2])
	paymaster := topicToAddress(log.Topics[3])

	// Data: nonce(32) + success(32) + actualGasCost(32) + actualGasUsed(32)
	data := strings.TrimPrefix(log.Data, "0x")
	if len(data) < 256 {
		return nil, fmt.Errorf("insufficient data")
	}

	nonce := "0x" + strings.TrimLeft(data[0:64], "0")
	if nonce == "0x" {
		nonce = "0x0"
	}
	successHex := data[64:128]
	success := successHex[63] == '1'
	actualGasCost := hexToBigInt("0x" + data[128:192]).String()
	actualGasUsed := hexToUint64("0x" + data[192:256])

	var status uint8 = 0
	if success {
		status = 1
	}

	// Determine entry point version
	version := "v0.6"
	if strings.ToLower(entryPoint) == EntryPointV07 {
		version = "v0.7"
	}

	return &UserOperation{
		Hash:              userOpHash,
		Sender:            sender,
		Nonce:             nonce,
		EntryPoint:        entryPoint,
		BlockNumber:       blockNumber,
		BlockHash:         blockHash,
		TransactionHash:   txHash,
		BundlerAddress:    bundlerAddress,
		PaymasterAddress:  paymaster,
		Status:            status,
		ActualGasCost:     actualGasCost,
		ActualGasUsed:     actualGasUsed,
		EntryPointVersion: version,
		Timestamp:         timestamp,
	}, nil
}

// StoreUserOperation stores a user operation
func (e *ERC4337Indexer) StoreUserOperation(ctx context.Context, op *UserOperation) error {
	_, err := e.db.ExecContext(ctx, `
		INSERT INTO erc4337_user_operations
		(hash, sender, nonce, init_code, call_data, call_gas_limit, verification_gas_limit,
		 pre_verification_gas, max_fee_per_gas, max_priority_fee_per_gas, paymaster_and_data,
		 signature, entry_point, block_number, block_hash, transaction_hash, bundler_address,
		 paymaster_address, factory_address, status, revert_reason, actual_gas_cost,
		 actual_gas_used, entry_point_version, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25)
		ON CONFLICT (hash) DO UPDATE SET
			status = EXCLUDED.status,
			actual_gas_cost = EXCLUDED.actual_gas_cost,
			actual_gas_used = EXCLUDED.actual_gas_used
	`, op.Hash, op.Sender, op.Nonce, op.InitCode, op.CallData, op.CallGasLimit,
		op.VerificationGasLimit, op.PreVerificationGas, op.MaxFeePerGas,
		op.MaxPriorityFeePerGas, op.PaymasterAndData, op.Signature, op.EntryPoint,
		op.BlockNumber, op.BlockHash, op.TransactionHash, op.BundlerAddress,
		op.PaymasterAddress, op.FactoryAddress, op.Status, op.RevertReason,
		op.ActualGasCost, op.ActualGasUsed, op.EntryPointVersion, op.Timestamp)
	return err
}

// StoreBundle stores a bundle
func (e *ERC4337Indexer) StoreBundle(ctx context.Context, b Bundle) error {
	opHashesJSON, _ := json.Marshal(b.OperationHashes)
	_, err := e.db.ExecContext(ctx, `
		INSERT INTO erc4337_bundles
		(transaction_hash, block_number, block_hash, bundler_address, operation_count,
		 operation_hashes, total_gas_used, gas_price, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (transaction_hash) DO UPDATE SET
			operation_count = EXCLUDED.operation_count,
			operation_hashes = EXCLUDED.operation_hashes
	`, b.TransactionHash, b.BlockNumber, b.BlockHash, b.BundlerAddress,
		b.OperationCount, opHashesJSON, b.TotalGasUsed, b.GasPrice, b.Timestamp)
	return err
}

// updateBundler updates bundler statistics
func (e *ERC4337Indexer) updateBundler(ctx context.Context, address string, blockNumber uint64, timestamp time.Time) {
	e.db.ExecContext(ctx, `
		INSERT INTO erc4337_bundlers (address, total_operations, total_bundles, first_seen_block, last_seen_block, first_seen_timestamp, last_seen_timestamp)
		VALUES ($1, 1, 1, $2, $2, $3, $3)
		ON CONFLICT (address) DO UPDATE SET
			total_operations = erc4337_bundlers.total_operations + 1,
			last_seen_block = $2,
			last_seen_timestamp = $3
	`, address, blockNumber, timestamp)
}

// updatePaymaster updates paymaster statistics
func (e *ERC4337Indexer) updatePaymaster(ctx context.Context, address, gasCost string, blockNumber uint64, timestamp time.Time) {
	e.db.ExecContext(ctx, `
		INSERT INTO erc4337_paymasters (address, total_operations, total_gas_sponsored, first_seen_block, last_seen_block, first_seen_timestamp, last_seen_timestamp)
		VALUES ($1, 1, $2, $3, $3, $4, $4)
		ON CONFLICT (address) DO UPDATE SET
			total_operations = erc4337_paymasters.total_operations + 1,
			total_gas_sponsored = (CAST(erc4337_paymasters.total_gas_sponsored AS NUMERIC) + CAST($2 AS NUMERIC))::TEXT,
			last_seen_block = $3,
			last_seen_timestamp = $4
	`, address, gasCost, blockNumber, timestamp)
}

// updateFactory updates factory statistics
func (e *ERC4337Indexer) updateFactory(ctx context.Context, address string, blockNumber uint64, timestamp time.Time) {
	e.db.ExecContext(ctx, `
		INSERT INTO erc4337_factories (address, total_accounts, first_seen_block, last_seen_block, first_seen_timestamp, last_seen_timestamp)
		VALUES ($1, 1, $2, $2, $3, $3)
		ON CONFLICT (address) DO UPDATE SET
			total_accounts = erc4337_factories.total_accounts + 1,
			last_seen_block = $2,
			last_seen_timestamp = $3
	`, address, blockNumber, timestamp)
}

// updateSmartAccount updates smart account data
func (e *ERC4337Indexer) updateSmartAccount(ctx context.Context, address, factory string, gasUsed, blockNumber uint64, txHash string, timestamp time.Time) {
	e.db.ExecContext(ctx, `
		INSERT INTO erc4337_accounts (address, factory_address, deployment_tx_hash, deployment_block, total_operations, total_gas_used, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 1, $5, $6, $6)
		ON CONFLICT (address) DO UPDATE SET
			total_operations = erc4337_accounts.total_operations + 1,
			total_gas_used = (CAST(erc4337_accounts.total_gas_used AS NUMERIC) + $5)::TEXT,
			updated_at = $6
	`, address, factory, txHash, blockNumber, gasUsed, timestamp)
}

// GetUserOperation retrieves a user operation by hash
func (e *ERC4337Indexer) GetUserOperation(ctx context.Context, hash string) (*UserOperation, error) {
	var op UserOperation
	var initCode, callData, paymasterData, signature, paymaster, factory, revertReason sql.NullString

	err := e.db.QueryRowContext(ctx, `
		SELECT hash, sender, nonce, init_code, call_data, call_gas_limit, verification_gas_limit,
			   pre_verification_gas, max_fee_per_gas, max_priority_fee_per_gas, paymaster_and_data,
			   signature, entry_point, block_number, block_hash, transaction_hash, bundler_address,
			   paymaster_address, factory_address, status, revert_reason, actual_gas_cost,
			   actual_gas_used, entry_point_version, timestamp
		FROM erc4337_user_operations WHERE hash = $1
	`, hash).Scan(
		&op.Hash, &op.Sender, &op.Nonce, &initCode, &callData, &op.CallGasLimit,
		&op.VerificationGasLimit, &op.PreVerificationGas, &op.MaxFeePerGas,
		&op.MaxPriorityFeePerGas, &paymasterData, &signature, &op.EntryPoint,
		&op.BlockNumber, &op.BlockHash, &op.TransactionHash, &op.BundlerAddress,
		&paymaster, &factory, &op.Status, &revertReason, &op.ActualGasCost,
		&op.ActualGasUsed, &op.EntryPointVersion, &op.Timestamp,
	)

	if err != nil {
		return nil, err
	}

	if initCode.Valid {
		op.InitCode = initCode.String
	}
	if callData.Valid {
		op.CallData = callData.String
	}
	if paymasterData.Valid {
		op.PaymasterAndData = paymasterData.String
	}
	if signature.Valid {
		op.Signature = signature.String
	}
	if paymaster.Valid {
		op.PaymasterAddress = paymaster.String
	}
	if factory.Valid {
		op.FactoryAddress = factory.String
	}
	if revertReason.Valid {
		op.RevertReason = revertReason.String
	}

	return &op, nil
}

// GetUserOperationsBySender retrieves user operations for a sender
func (e *ERC4337Indexer) GetUserOperationsBySender(ctx context.Context, sender string, limit, offset int) ([]UserOperation, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT hash, sender, nonce, entry_point, block_number, block_hash, transaction_hash,
			   bundler_address, paymaster_address, factory_address, status, actual_gas_cost,
			   actual_gas_used, entry_point_version, timestamp
		FROM erc4337_user_operations
		WHERE sender = $1
		ORDER BY block_number DESC, timestamp DESC
		LIMIT $2 OFFSET $3
	`, strings.ToLower(sender), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ops []UserOperation
	for rows.Next() {
		var op UserOperation
		var paymaster, factory sql.NullString
		err := rows.Scan(
			&op.Hash, &op.Sender, &op.Nonce, &op.EntryPoint, &op.BlockNumber,
			&op.BlockHash, &op.TransactionHash, &op.BundlerAddress, &paymaster,
			&factory, &op.Status, &op.ActualGasCost, &op.ActualGasUsed,
			&op.EntryPointVersion, &op.Timestamp,
		)
		if err != nil {
			continue
		}
		if paymaster.Valid {
			op.PaymasterAddress = paymaster.String
		}
		if factory.Valid {
			op.FactoryAddress = factory.String
		}
		ops = append(ops, op)
	}

	return ops, nil
}

// GetBundlers retrieves bundlers with pagination
func (e *ERC4337Indexer) GetBundlers(ctx context.Context, limit, offset int) ([]Bundler, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT address, total_operations, total_bundles, total_gas_sponsored, success_rate,
			   first_seen_block, last_seen_block, first_seen_timestamp, last_seen_timestamp
		FROM erc4337_bundlers
		ORDER BY total_operations DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bundlers []Bundler
	for rows.Next() {
		var b Bundler
		err := rows.Scan(
			&b.Address, &b.TotalOperations, &b.TotalBundles, &b.TotalGasSponsored,
			&b.SuccessRate, &b.FirstSeenBlock, &b.LastSeenBlock,
			&b.FirstSeenTimestamp, &b.LastSeenTimestamp,
		)
		if err != nil {
			continue
		}
		bundlers = append(bundlers, b)
	}

	return bundlers, nil
}

// GetPaymasters retrieves paymasters with pagination
func (e *ERC4337Indexer) GetPaymasters(ctx context.Context, limit, offset int) ([]Paymaster, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT address, total_operations, total_gas_sponsored, total_eth_sponsored, unique_accounts,
			   first_seen_block, last_seen_block, first_seen_timestamp, last_seen_timestamp
		FROM erc4337_paymasters
		ORDER BY total_operations DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paymasters []Paymaster
	for rows.Next() {
		var p Paymaster
		err := rows.Scan(
			&p.Address, &p.TotalOperations, &p.TotalGasSponsored, &p.TotalEthSponsored,
			&p.UniqueAccounts, &p.FirstSeenBlock, &p.LastSeenBlock,
			&p.FirstSeenTimestamp, &p.LastSeenTimestamp,
		)
		if err != nil {
			continue
		}
		paymasters = append(paymasters, p)
	}

	return paymasters, nil
}

// GetFactories retrieves account factories with pagination
func (e *ERC4337Indexer) GetFactories(ctx context.Context, limit, offset int) ([]AccountFactory, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT address, total_accounts, implementation_type, first_seen_block, last_seen_block,
			   first_seen_timestamp, last_seen_timestamp
		FROM erc4337_factories
		ORDER BY total_accounts DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var factories []AccountFactory
	for rows.Next() {
		var f AccountFactory
		var implType sql.NullString
		err := rows.Scan(
			&f.Address, &f.TotalAccounts, &implType, &f.FirstSeenBlock, &f.LastSeenBlock,
			&f.FirstSeenTimestamp, &f.LastSeenTimestamp,
		)
		if err != nil {
			continue
		}
		if implType.Valid {
			f.ImplementationType = implType.String
		}
		factories = append(factories, f)
	}

	return factories, nil
}

// GetSmartAccounts retrieves smart accounts with pagination
func (e *ERC4337Indexer) GetSmartAccounts(ctx context.Context, limit, offset int) ([]SmartAccount, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT address, factory_address, deployment_tx_hash, deployment_block, total_operations,
			   total_gas_used, created_at, updated_at
		FROM erc4337_accounts
		ORDER BY total_operations DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []SmartAccount
	for rows.Next() {
		var a SmartAccount
		var factory, txHash sql.NullString
		err := rows.Scan(
			&a.Address, &factory, &txHash, &a.DeploymentBlock, &a.TotalOperations,
			&a.TotalGasUsed, &a.CreatedAt, &a.UpdatedAt,
		)
		if err != nil {
			continue
		}
		if factory.Valid {
			a.FactoryAddress = factory.String
		}
		if txHash.Valid {
			a.DeploymentTxHash = txHash.String
		}
		accounts = append(accounts, a)
	}

	return accounts, nil
}

// UpdateStats updates ERC-4337 statistics
func (e *ERC4337Indexer) UpdateStats(ctx context.Context) error {
	_, err := e.db.ExecContext(ctx, `
		UPDATE erc4337_stats SET
			total_user_operations = (SELECT COUNT(*) FROM erc4337_user_operations),
			total_bundlers = (SELECT COUNT(*) FROM erc4337_bundlers),
			total_paymasters = (SELECT COUNT(*) FROM erc4337_paymasters),
			total_factories = (SELECT COUNT(*) FROM erc4337_factories),
			total_smart_accounts = (SELECT COUNT(*) FROM erc4337_accounts),
			total_bundles = (SELECT COUNT(*) FROM erc4337_bundles),
			updated_at = NOW()
		WHERE id = 1
	`)
	return err
}

// GetStats retrieves ERC-4337 statistics
func (e *ERC4337Indexer) GetStats(ctx context.Context) (map[string]interface{}, error) {
	var totalOps, totalBundlers, totalPaymasters, totalFactories, totalAccounts, totalBundles int64
	var updatedAt time.Time

	err := e.db.QueryRowContext(ctx, `
		SELECT total_user_operations, total_bundlers, total_paymasters, total_factories,
			   total_smart_accounts, total_bundles, updated_at
		FROM erc4337_stats WHERE id = 1
	`).Scan(&totalOps, &totalBundlers, &totalPaymasters, &totalFactories, &totalAccounts, &totalBundles, &updatedAt)

	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	return map[string]interface{}{
		"total_user_operations": totalOps,
		"total_bundlers":        totalBundlers,
		"total_paymasters":      totalPaymasters,
		"total_factories":       totalFactories,
		"total_smart_accounts":  totalAccounts,
		"total_bundles":         totalBundles,
		"updated_at":            updatedAt,
	}, nil
}

// DecodeCallData decodes the callData of a user operation
func DecodeCallData(callData string) (selector string, decoded map[string]interface{}) {
	data := strings.TrimPrefix(callData, "0x")
	if len(data) < 8 {
		return "", nil
	}

	selector = "0x" + data[:8]
	decoded = make(map[string]interface{})

	// Common function selectors
	switch selector {
	case "0xb61d27f6": // execute(address,uint256,bytes)
		if len(data) >= 136 {
			decoded["function"] = "execute"
			decoded["target"] = "0x" + data[32:72]
			decoded["value"] = hexToBigInt("0x" + data[72:136]).String()
			if len(data) > 200 {
				decoded["data"] = "0x" + data[200:]
			}
		}
	case "0x47e1da2a": // executeBatch(address[],uint256[],bytes[])
		decoded["function"] = "executeBatch"
	case "0x51945447": // execute(address,uint256,bytes,uint8)
		decoded["function"] = "executeWithOperation"
	}

	return selector, decoded
}

// DecodeInitCode decodes the initCode to extract factory and initialization data
func DecodeInitCode(initCode string) (factory string, initData string) {
	data := strings.TrimPrefix(initCode, "0x")
	if len(data) < 40 {
		return "", ""
	}

	factory = "0x" + data[:40]
	if len(data) > 40 {
		initData = "0x" + data[40:]
	}

	return factory, initData
}

// DecodePaymasterAndData decodes paymaster and data
func DecodePaymasterAndData(paymasterAndData string) (paymaster string, data string) {
	d := strings.TrimPrefix(paymasterAndData, "0x")
	if len(d) < 40 {
		return "", ""
	}

	paymaster = "0x" + d[:40]
	if len(d) > 40 {
		data = "0x" + d[40:]
	}

	return paymaster, data
}

// CalculateUserOpHash calculates the hash of a user operation
func CalculateUserOpHash(op *UserOperation, chainID uint64) string {
	// This would require proper ABI encoding and keccak256
	// For now, return the hash from the event
	return op.Hash
}

// detectAccountFactory tries to detect the factory from init code or contract code
func (e *ERC4337Indexer) detectAccountFactory(ctx context.Context, sender string) (factory string, implType string) {
	// Get contract creation transaction
	code, err := e.adapter.GetCode(ctx, sender)
	if err != nil || code == "0x" {
		return "", ""
	}

	// Detect common implementation types by bytecode patterns
	codeLower := strings.ToLower(code)

	// Safe implementation
	if strings.Contains(codeLower, "d9db270c") { // Safe's masterCopy
		return "", "Safe"
	}

	// Kernel
	if strings.Contains(codeLower, "kernel") {
		return "", "Kernel"
	}

	// SimpleAccount (common Alchemy/StackUp)
	if len(code) < 1000 { // SimpleAccount bytecode is relatively small
		return "", "SimpleAccount"
	}

	return "", "Unknown"
}

// Helper to decode hex string
func decodeHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	return hex.DecodeString(s)
}
