// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package evm

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Blob constants
const (
	// BlobTxType is EIP-4844 type 3 transaction
	BlobTxType = 3
	// BlobGasPerBlob is the gas used per blob
	BlobGasPerBlob = 131072 // 2^17
	// MaxBlobsPerBlock is the target number of blobs per block
	MaxBlobsPerBlock = 6
	// BlobFieldElementSize is 32 bytes
	BlobFieldElementSize = 32
	// BlobSize is 128KB (4096 field elements * 32 bytes)
	BlobSize = 4096 * BlobFieldElementSize
)

// BlobIndexer handles EIP-4844 blob transaction indexing
type BlobIndexer struct {
	adapter    *Adapter
	db         *sql.DB
	storageURI string // External blob storage URI
}

// NewBlobIndexer creates a new blob indexer
func NewBlobIndexer(adapter *Adapter, db *sql.DB, storageURI string) *BlobIndexer {
	return &BlobIndexer{
		adapter:    adapter,
		db:         db,
		storageURI: storageURI,
	}
}

// InitSchema creates blob database tables
func (b *BlobIndexer) InitSchema() error {
	schema := `
		-- Blob transactions table
		CREATE TABLE IF NOT EXISTS blob_transactions (
			hash TEXT PRIMARY KEY,
			block_number BIGINT NOT NULL,
			block_hash TEXT NOT NULL,
			tx_from TEXT NOT NULL,
			tx_to TEXT,
			value TEXT NOT NULL DEFAULT '0',
			max_fee_per_blob_gas TEXT NOT NULL,
			blob_gas_used BIGINT NOT NULL,
			blob_gas_price TEXT NOT NULL,
			blob_versioned_hashes JSONB NOT NULL DEFAULT '[]',
			blob_count INT NOT NULL DEFAULT 0,
			transaction_index BIGINT NOT NULL,
			timestamp TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_blob_tx_block ON blob_transactions(block_number);
		CREATE INDEX IF NOT EXISTS idx_blob_tx_from ON blob_transactions(tx_from);
		CREATE INDEX IF NOT EXISTS idx_blob_tx_to ON blob_transactions(tx_to) WHERE tx_to IS NOT NULL;
		CREATE INDEX IF NOT EXISTS idx_blob_tx_timestamp ON blob_transactions(timestamp);
		CREATE INDEX IF NOT EXISTS idx_blob_tx_gas_price ON blob_transactions((CAST(blob_gas_price AS NUMERIC)));

		-- Blob data table
		CREATE TABLE IF NOT EXISTS blob_data (
			versioned_hash TEXT PRIMARY KEY,
			commitment TEXT NOT NULL,
			proof TEXT NOT NULL,
			data TEXT, -- Actual blob data (may be null if pruned)
			size BIGINT NOT NULL DEFAULT 131072,
			transaction_hash TEXT NOT NULL,
			block_number BIGINT NOT NULL,
			blob_index BIGINT NOT NULL,
			storage_uri TEXT, -- External storage link
			is_pruned BOOLEAN DEFAULT FALSE,
			timestamp TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_blob_data_tx ON blob_data(transaction_hash);
		CREATE INDEX IF NOT EXISTS idx_blob_data_block ON blob_data(block_number);
		CREATE INDEX IF NOT EXISTS idx_blob_data_pruned ON blob_data(is_pruned) WHERE is_pruned = TRUE;

		-- Block blob gas table (for tracking excess blob gas)
		CREATE TABLE IF NOT EXISTS blob_block_gas (
			block_number BIGINT PRIMARY KEY,
			block_hash TEXT NOT NULL,
			blob_gas_used BIGINT NOT NULL DEFAULT 0,
			excess_blob_gas BIGINT NOT NULL DEFAULT 0,
			blob_count INT NOT NULL DEFAULT 0,
			blob_tx_count INT NOT NULL DEFAULT 0,
			parent_beacon_root TEXT,
			timestamp TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_blob_block_gas_timestamp ON blob_block_gas(timestamp);

		-- Blob stats table
		CREATE TABLE IF NOT EXISTS blob_stats (
			id INT PRIMARY KEY DEFAULT 1,
			total_blob_transactions BIGINT DEFAULT 0,
			total_blobs BIGINT DEFAULT 0,
			total_blob_gas_used BIGINT DEFAULT 0,
			total_blob_gas_fees TEXT DEFAULT '0',
			avg_blobs_per_block FLOAT DEFAULT 0,
			avg_blob_gas_price TEXT DEFAULT '0',
			pruned_blobs BIGINT DEFAULT 0,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		INSERT INTO blob_stats (id) VALUES (1) ON CONFLICT DO NOTHING;
	`

	_, err := b.db.Exec(schema)
	return err
}

// ProcessBlock processes a block for blob transactions
func (b *BlobIndexer) ProcessBlock(ctx context.Context, blockNumber uint64) error {
	// Get block with full transaction details
	blockData, err := b.adapter.GetBlockByHeight(ctx, blockNumber)
	if err != nil {
		return err
	}

	var block struct {
		Hash                  string `json:"hash"`
		Timestamp             string `json:"timestamp"`
		BlobGasUsed           string `json:"blobGasUsed"`
		ExcessBlobGas         string `json:"excessBlobGas"`
		ParentBeaconBlockRoot string `json:"parentBeaconBlockRoot"`
		Transactions          []struct {
			Hash                string   `json:"hash"`
			From                string   `json:"from"`
			To                  string   `json:"to"`
			Value               string   `json:"value"`
			Type                string   `json:"type"`
			MaxFeePerBlobGas    string   `json:"maxFeePerBlobGas"`
			BlobVersionedHashes []string `json:"blobVersionedHashes"`
			TransactionIndex    string   `json:"transactionIndex"`
		} `json:"transactions"`
	}

	if err := json.Unmarshal(blockData, &block); err != nil {
		return err
	}

	timestamp := time.Unix(int64(hexToUint64(block.Timestamp)), 0)
	blockBlobGasUsed := hexToUint64(block.BlobGasUsed)
	excessBlobGas := hexToUint64(block.ExcessBlobGas)

	var blobTxCount, totalBlobs int
	blobGasPrice := b.calculateBlobGasPrice(excessBlobGas)

	// Process blob transactions
	for _, tx := range block.Transactions {
		txType := hexToUint64(tx.Type)
		if txType != BlobTxType {
			continue
		}

		blobTxCount++
		blobCount := len(tx.BlobVersionedHashes)
		totalBlobs += blobCount

		blobTx := BlobTransaction{
			Hash:                tx.Hash,
			BlockNumber:         blockNumber,
			BlockHash:           block.Hash,
			From:                strings.ToLower(tx.From),
			To:                  strings.ToLower(tx.To),
			Value:               hexToBigInt(tx.Value).String(),
			MaxFeePerBlobGas:    hexToBigInt(tx.MaxFeePerBlobGas).String(),
			BlobGasUsed:         uint64(blobCount) * BlobGasPerBlob,
			BlobGasPrice:        blobGasPrice,
			BlobVersionedHashes: tx.BlobVersionedHashes,
			BlobCount:           blobCount,
			TransactionIndex:    hexToUint64(tx.TransactionIndex),
			Timestamp:           timestamp,
		}

		if err := b.StoreBlobTransaction(ctx, blobTx); err != nil {
			continue
		}

		// Process individual blobs
		for i, versionedHash := range tx.BlobVersionedHashes {
			blob := BlobData{
				VersionedHash:   versionedHash,
				TransactionHash: tx.Hash,
				BlockNumber:     blockNumber,
				BlobIndex:       uint64(i),
				Size:            BlobSize,
				Timestamp:       timestamp,
			}

			// Try to fetch blob data from beacon node if available
			commitment, proof, data := b.fetchBlobFromBeacon(ctx, versionedHash, blockNumber)
			if commitment != "" {
				blob.Commitment = commitment
				blob.Proof = proof
				blob.Data = data
			}

			// Set storage URI if we have external blob storage
			if b.storageURI != "" {
				blob.StorageURI = fmt.Sprintf("%s/%s", b.storageURI, versionedHash)
			}

			b.StoreBlobData(ctx, blob)
		}
	}

	// Store block blob gas info
	blockGas := struct {
		BlockNumber      uint64
		BlockHash        string
		BlobGasUsed      uint64
		ExcessBlobGas    uint64
		BlobCount        int
		BlobTxCount      int
		ParentBeaconRoot string
		Timestamp        time.Time
	}{
		BlockNumber:      blockNumber,
		BlockHash:        block.Hash,
		BlobGasUsed:      blockBlobGasUsed,
		ExcessBlobGas:    excessBlobGas,
		BlobCount:        totalBlobs,
		BlobTxCount:      blobTxCount,
		ParentBeaconRoot: block.ParentBeaconBlockRoot,
		Timestamp:        timestamp,
	}

	b.storeBlockBlobGas(ctx, blockGas)

	return nil
}

// calculateBlobGasPrice calculates the blob gas price using EIP-4844 formula
func (b *BlobIndexer) calculateBlobGasPrice(excessBlobGas uint64) string {
	// Base fee calculation using exponential formula
	// blob_base_fee = MIN_BLOB_BASE_FEE * e^(excess_blob_gas / BLOB_BASE_FEE_UPDATE_FRACTION)
	// Simplified: we use the excess blob gas directly for now
	// Real calculation would use big integer exponential

	minBlobBaseFee := uint64(1) // 1 wei minimum
	updateFraction := uint64(3338477) // BLOB_BASE_FEE_UPDATE_FRACTION

	if excessBlobGas == 0 {
		return fmt.Sprintf("%d", minBlobBaseFee)
	}

	// Simplified calculation
	// In production, use proper exponential calculation
	multiplier := excessBlobGas / updateFraction
	if multiplier > 20 {
		multiplier = 20 // Cap to prevent overflow
	}

	price := minBlobBaseFee
	for i := uint64(0); i < multiplier; i++ {
		price = price * 112 / 100 // ~e^(1/8) approximation
	}

	return fmt.Sprintf("%d", price)
}

// StoreBlobTransaction stores a blob transaction
func (b *BlobIndexer) StoreBlobTransaction(ctx context.Context, tx BlobTransaction) error {
	hashesJSON, _ := json.Marshal(tx.BlobVersionedHashes)
	_, err := b.db.ExecContext(ctx, `
		INSERT INTO blob_transactions
		(hash, block_number, block_hash, tx_from, tx_to, value, max_fee_per_blob_gas,
		 blob_gas_used, blob_gas_price, blob_versioned_hashes, blob_count, transaction_index, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (hash) DO UPDATE SET
			blob_gas_price = EXCLUDED.blob_gas_price
	`, tx.Hash, tx.BlockNumber, tx.BlockHash, tx.From, tx.To, tx.Value,
		tx.MaxFeePerBlobGas, tx.BlobGasUsed, tx.BlobGasPrice, hashesJSON,
		tx.BlobCount, tx.TransactionIndex, tx.Timestamp)
	return err
}

// StoreBlobData stores blob data
func (b *BlobIndexer) StoreBlobData(ctx context.Context, blob BlobData) error {
	_, err := b.db.ExecContext(ctx, `
		INSERT INTO blob_data
		(versioned_hash, commitment, proof, data, size, transaction_hash, block_number,
		 blob_index, storage_uri, is_pruned, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (versioned_hash) DO UPDATE SET
			storage_uri = COALESCE(EXCLUDED.storage_uri, blob_data.storage_uri),
			is_pruned = EXCLUDED.is_pruned
	`, blob.VersionedHash, blob.Commitment, blob.Proof, blob.Data, blob.Size,
		blob.TransactionHash, blob.BlockNumber, blob.BlobIndex, blob.StorageURI,
		blob.Data == "", blob.Timestamp)
	return err
}

// storeBlockBlobGas stores block-level blob gas information
func (b *BlobIndexer) storeBlockBlobGas(ctx context.Context, blockGas struct {
	BlockNumber      uint64
	BlockHash        string
	BlobGasUsed      uint64
	ExcessBlobGas    uint64
	BlobCount        int
	BlobTxCount      int
	ParentBeaconRoot string
	Timestamp        time.Time
}) error {
	_, err := b.db.ExecContext(ctx, `
		INSERT INTO blob_block_gas
		(block_number, block_hash, blob_gas_used, excess_blob_gas, blob_count, blob_tx_count, parent_beacon_root, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (block_number) DO UPDATE SET
			blob_gas_used = EXCLUDED.blob_gas_used,
			excess_blob_gas = EXCLUDED.excess_blob_gas,
			blob_count = EXCLUDED.blob_count,
			blob_tx_count = EXCLUDED.blob_tx_count
	`, blockGas.BlockNumber, blockGas.BlockHash, blockGas.BlobGasUsed,
		blockGas.ExcessBlobGas, blockGas.BlobCount, blockGas.BlobTxCount,
		blockGas.ParentBeaconRoot, blockGas.Timestamp)
	return err
}

// fetchBlobFromBeacon fetches blob data from the beacon node
func (b *BlobIndexer) fetchBlobFromBeacon(ctx context.Context, versionedHash string, blockNumber uint64) (commitment, proof, data string) {
	// This would call the beacon node's /eth/v1/beacon/blob_sidecars/{slot} endpoint
	// For now, return empty - would be implemented based on beacon node availability
	return "", "", ""
}

// GetBlobTransaction retrieves a blob transaction by hash
func (b *BlobIndexer) GetBlobTransaction(ctx context.Context, hash string) (*BlobTransaction, error) {
	var tx BlobTransaction
	var to sql.NullString
	var hashesJSON []byte

	err := b.db.QueryRowContext(ctx, `
		SELECT hash, block_number, block_hash, tx_from, tx_to, value, max_fee_per_blob_gas,
			   blob_gas_used, blob_gas_price, blob_versioned_hashes, blob_count, transaction_index, timestamp
		FROM blob_transactions WHERE hash = $1
	`, hash).Scan(
		&tx.Hash, &tx.BlockNumber, &tx.BlockHash, &tx.From, &to, &tx.Value,
		&tx.MaxFeePerBlobGas, &tx.BlobGasUsed, &tx.BlobGasPrice, &hashesJSON,
		&tx.BlobCount, &tx.TransactionIndex, &tx.Timestamp,
	)

	if err != nil {
		return nil, err
	}

	if to.Valid {
		tx.To = to.String
	}
	if len(hashesJSON) > 0 {
		json.Unmarshal(hashesJSON, &tx.BlobVersionedHashes)
	}

	return &tx, nil
}

// GetBlobData retrieves blob data by versioned hash
func (b *BlobIndexer) GetBlobData(ctx context.Context, versionedHash string) (*BlobData, error) {
	var blob BlobData
	var data, storageURI sql.NullString

	err := b.db.QueryRowContext(ctx, `
		SELECT versioned_hash, commitment, proof, data, size, transaction_hash,
			   block_number, blob_index, storage_uri, is_pruned, timestamp
		FROM blob_data WHERE versioned_hash = $1
	`, versionedHash).Scan(
		&blob.VersionedHash, &blob.Commitment, &blob.Proof, &data, &blob.Size,
		&blob.TransactionHash, &blob.BlockNumber, &blob.BlobIndex, &storageURI,
		nil, &blob.Timestamp,
	)

	if err != nil {
		return nil, err
	}

	if data.Valid {
		blob.Data = data.String
	}
	if storageURI.Valid {
		blob.StorageURI = storageURI.String
	}

	return &blob, nil
}

// GetBlobsByTransaction retrieves all blobs for a transaction
func (b *BlobIndexer) GetBlobsByTransaction(ctx context.Context, txHash string) ([]BlobData, error) {
	rows, err := b.db.QueryContext(ctx, `
		SELECT versioned_hash, commitment, proof, data, size, transaction_hash,
			   block_number, blob_index, storage_uri, is_pruned, timestamp
		FROM blob_data WHERE transaction_hash = $1
		ORDER BY blob_index
	`, txHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blobs []BlobData
	for rows.Next() {
		var blob BlobData
		var data, storageURI sql.NullString
		var isPruned bool

		err := rows.Scan(
			&blob.VersionedHash, &blob.Commitment, &blob.Proof, &data, &blob.Size,
			&blob.TransactionHash, &blob.BlockNumber, &blob.BlobIndex, &storageURI,
			&isPruned, &blob.Timestamp,
		)
		if err != nil {
			continue
		}

		if data.Valid {
			blob.Data = data.String
		}
		if storageURI.Valid {
			blob.StorageURI = storageURI.String
		}

		blobs = append(blobs, blob)
	}

	return blobs, nil
}

// GetBlobTransactions retrieves blob transactions with pagination
func (b *BlobIndexer) GetBlobTransactions(ctx context.Context, limit, offset int) ([]BlobTransaction, error) {
	rows, err := b.db.QueryContext(ctx, `
		SELECT hash, block_number, block_hash, tx_from, tx_to, value, max_fee_per_blob_gas,
			   blob_gas_used, blob_gas_price, blob_versioned_hashes, blob_count, transaction_index, timestamp
		FROM blob_transactions
		ORDER BY block_number DESC, transaction_index DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txs []BlobTransaction
	for rows.Next() {
		var tx BlobTransaction
		var to sql.NullString
		var hashesJSON []byte

		err := rows.Scan(
			&tx.Hash, &tx.BlockNumber, &tx.BlockHash, &tx.From, &to, &tx.Value,
			&tx.MaxFeePerBlobGas, &tx.BlobGasUsed, &tx.BlobGasPrice, &hashesJSON,
			&tx.BlobCount, &tx.TransactionIndex, &tx.Timestamp,
		)
		if err != nil {
			continue
		}

		if to.Valid {
			tx.To = to.String
		}
		if len(hashesJSON) > 0 {
			json.Unmarshal(hashesJSON, &tx.BlobVersionedHashes)
		}

		txs = append(txs, tx)
	}

	return txs, nil
}

// GetBlockBlobGas retrieves blob gas info for a block
func (b *BlobIndexer) GetBlockBlobGas(ctx context.Context, blockNumber uint64) (map[string]interface{}, error) {
	var blobGasUsed, excessBlobGas uint64
	var blobCount, blobTxCount int
	var blockHash, parentBeaconRoot sql.NullString
	var timestamp time.Time

	err := b.db.QueryRowContext(ctx, `
		SELECT block_hash, blob_gas_used, excess_blob_gas, blob_count, blob_tx_count,
			   parent_beacon_root, timestamp
		FROM blob_block_gas WHERE block_number = $1
	`, blockNumber).Scan(&blockHash, &blobGasUsed, &excessBlobGas, &blobCount,
		&blobTxCount, &parentBeaconRoot, &timestamp)

	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"block_number":       blockNumber,
		"blob_gas_used":      blobGasUsed,
		"excess_blob_gas":    excessBlobGas,
		"blob_count":         blobCount,
		"blob_tx_count":      blobTxCount,
		"timestamp":          timestamp,
	}

	if blockHash.Valid {
		result["block_hash"] = blockHash.String
	}
	if parentBeaconRoot.Valid {
		result["parent_beacon_root"] = parentBeaconRoot.String
	}

	return result, nil
}

// PruneOldBlobs removes blob data older than retention period
func (b *BlobIndexer) PruneOldBlobs(ctx context.Context, retentionDays int) (int64, error) {
	// Mark blobs as pruned and remove data
	result, err := b.db.ExecContext(ctx, `
		UPDATE blob_data
		SET data = NULL, is_pruned = TRUE
		WHERE timestamp < NOW() - INTERVAL '%d days' AND is_pruned = FALSE
	`, retentionDays)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// UpdateStats updates blob statistics
func (b *BlobIndexer) UpdateStats(ctx context.Context) error {
	_, err := b.db.ExecContext(ctx, `
		UPDATE blob_stats SET
			total_blob_transactions = (SELECT COUNT(*) FROM blob_transactions),
			total_blobs = (SELECT COUNT(*) FROM blob_data),
			total_blob_gas_used = COALESCE((SELECT SUM(blob_gas_used) FROM blob_transactions), 0),
			total_blob_gas_fees = COALESCE((SELECT SUM(CAST(blob_gas_price AS NUMERIC) * blob_gas_used) FROM blob_transactions), 0)::TEXT,
			avg_blobs_per_block = COALESCE((SELECT AVG(blob_count) FROM blob_block_gas WHERE blob_count > 0), 0),
			avg_blob_gas_price = COALESCE((SELECT AVG(CAST(blob_gas_price AS NUMERIC)) FROM blob_transactions), 0)::TEXT,
			pruned_blobs = (SELECT COUNT(*) FROM blob_data WHERE is_pruned = TRUE),
			updated_at = NOW()
		WHERE id = 1
	`)
	return err
}

// GetStats retrieves blob statistics
func (b *BlobIndexer) GetStats(ctx context.Context) (map[string]interface{}, error) {
	var totalTxs, totalBlobs, totalGasUsed, prunedBlobs int64
	var totalFees, avgGasPrice string
	var avgBlobsPerBlock float64
	var updatedAt time.Time

	err := b.db.QueryRowContext(ctx, `
		SELECT total_blob_transactions, total_blobs, total_blob_gas_used, total_blob_gas_fees,
			   avg_blobs_per_block, avg_blob_gas_price, pruned_blobs, updated_at
		FROM blob_stats WHERE id = 1
	`).Scan(&totalTxs, &totalBlobs, &totalGasUsed, &totalFees,
		&avgBlobsPerBlock, &avgGasPrice, &prunedBlobs, &updatedAt)

	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	return map[string]interface{}{
		"total_blob_transactions": totalTxs,
		"total_blobs":             totalBlobs,
		"total_blob_gas_used":     totalGasUsed,
		"total_blob_gas_fees":     totalFees,
		"avg_blobs_per_block":     avgBlobsPerBlock,
		"avg_blob_gas_price":      avgGasPrice,
		"pruned_blobs":            prunedBlobs,
		"updated_at":              updatedAt,
	}, nil
}
