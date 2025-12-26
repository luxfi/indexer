// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrInvalidParam = errors.New("invalid parameter")
)

// Repository handles database queries for the API
type Repository struct {
	db      *sql.DB
	chainID int64
}

// NewRepository creates a new repository
func NewRepository(db *sql.DB, chainID int64) *Repository {
	return &Repository{db: db, chainID: chainID}
}

// BlockFilters for block listing
type BlockFilters struct {
	Type string // block, reorg, uncle
}

// GetBlocks returns paginated blocks
func (r *Repository) GetBlocks(ctx context.Context, page, pageSize int, filters *BlockFilters) (*PaginatedResponse, error) {
	offset := page * pageSize

	query := `
		SELECT b.id, b.parent_id, b.height, b.timestamp, b.status, b.tx_count, b.data, b.metadata
		FROM cchain_blocks b
		ORDER BY b.height DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.QueryContext(ctx, query, pageSize+1, offset)
	if err != nil {
		return nil, fmt.Errorf("query blocks: %w", err)
	}
	defer rows.Close()

	var blocks []Block
	for rows.Next() {
		b, err := r.scanBlock(rows)
		if err != nil {
			continue
		}
		blocks = append(blocks, *b)
	}

	resp := &PaginatedResponse{Items: blocks}
	if len(blocks) > pageSize {
		blocks = blocks[:pageSize]
		resp.Items = blocks
		lastBlock := blocks[len(blocks)-1]
		resp.NextPageParams = &NextPageParams{
			BlockNumber: &lastBlock.Height,
			ItemsCount:  len(blocks),
		}
	}

	return resp, nil
}

// GetBlockByNumber returns a block by height
func (r *Repository) GetBlockByNumber(ctx context.Context, height uint64) (*Block, error) {
	query := `
		SELECT b.id, b.parent_id, b.height, b.timestamp, b.status, b.tx_count, b.data, b.metadata
		FROM cchain_blocks b
		WHERE b.height = $1
	`

	row := r.db.QueryRowContext(ctx, query, height)
	return r.scanBlockRow(row)
}

// GetBlockByHash returns a block by hash
func (r *Repository) GetBlockByHash(ctx context.Context, hash string) (*Block, error) {
	query := `
		SELECT b.id, b.parent_id, b.height, b.timestamp, b.status, b.tx_count, b.data, b.metadata
		FROM cchain_blocks b
		WHERE b.id = $1
	`

	row := r.db.QueryRowContext(ctx, query, strings.ToLower(hash))
	return r.scanBlockRow(row)
}

func (r *Repository) scanBlock(rows *sql.Rows) (*Block, error) {
	var id, parentID, status string
	var height uint64
	var timestamp time.Time
	var txCount int
	var data, metadata []byte

	if err := rows.Scan(&id, &parentID, &height, &timestamp, &status, &txCount, &data, &metadata); err != nil {
		return nil, err
	}

	return r.buildBlock(id, parentID, height, timestamp, status, txCount, data, metadata)
}

func (r *Repository) scanBlockRow(row *sql.Row) (*Block, error) {
	var id, parentID, status string
	var height uint64
	var timestamp time.Time
	var txCount int
	var data, metadata []byte

	if err := row.Scan(&id, &parentID, &height, &timestamp, &status, &txCount, &data, &metadata); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return r.buildBlock(id, parentID, height, timestamp, status, txCount, data, metadata)
}

func (r *Repository) buildBlock(id, parentID string, height uint64, timestamp time.Time, status string, txCount int, data, metadata []byte) (*Block, error) {
	block := &Block{
		Hash:             id,
		ParentHash:       parentID,
		Height:           height,
		Timestamp:        timestamp,
		TransactionCount: txCount,
		Type:             "block",
	}

	// Parse metadata for gas info
	if len(metadata) > 0 {
		var meta map[string]interface{}
		if err := json.Unmarshal(metadata, &meta); err == nil {
			if gasUsed, ok := meta["gasUsed"].(float64); ok {
				block.GasUsed = strconv.FormatUint(uint64(gasUsed), 10)
			}
			if gasLimit, ok := meta["gasLimit"].(float64); ok {
				block.GasLimit = strconv.FormatUint(uint64(gasLimit), 10)
			}
			if miner, ok := meta["miner"].(string); ok {
				block.Miner = &Address{Hash: miner}
			}
			if baseFee, ok := meta["baseFee"].(string); ok {
				block.BaseFeePerGas = baseFee
			}
			if size, ok := meta["size"].(float64); ok {
				block.Size = uint64(size)
			}
		}
	}

	// Get latest block for confirmations
	var latestHeight uint64
	r.db.QueryRowContext(context.Background(), "SELECT COALESCE(MAX(height), 0) FROM cchain_blocks").Scan(&latestHeight)
	if latestHeight > height {
		block.Confirmations = int(latestHeight - height)
	}

	return block, nil
}

// TransactionFilters for transaction listing
type TransactionFilters struct {
	Type   string // transaction type filter
	Method string // method filter
}

// GetTransactions returns paginated transactions
func (r *Repository) GetTransactions(ctx context.Context, page, pageSize int, filters *TransactionFilters) (*PaginatedResponse, error) {
	offset := page * pageSize

	query := `
		SELECT hash, block_hash, block_number, tx_from, tx_to, value, gas, gas_price,
		       gas_used, nonce, input, tx_index, tx_type, status, contract_address, timestamp
		FROM cchain_transactions
		ORDER BY block_number DESC, tx_index DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.QueryContext(ctx, query, pageSize+1, offset)
	if err != nil {
		return nil, fmt.Errorf("query transactions: %w", err)
	}
	defer rows.Close()

	var txs []Transaction
	for rows.Next() {
		tx, err := r.scanTransaction(rows)
		if err != nil {
			continue
		}
		txs = append(txs, *tx)
	}

	resp := &PaginatedResponse{Items: txs}
	if len(txs) > pageSize {
		txs = txs[:pageSize]
		resp.Items = txs
		lastTx := txs[len(txs)-1]
		idx := 0
		if lastTx.TransactionIndex != nil {
			idx = *lastTx.TransactionIndex
		}
		resp.NextPageParams = &NextPageParams{
			BlockNumber:      lastTx.BlockNumber,
			TransactionIndex: &idx,
			ItemsCount:       len(txs),
		}
	}

	return resp, nil
}

// GetTransactionByHash returns a transaction by hash
func (r *Repository) GetTransactionByHash(ctx context.Context, hash string) (*Transaction, error) {
	query := `
		SELECT hash, block_hash, block_number, tx_from, tx_to, value, gas, gas_price,
		       gas_used, nonce, input, tx_index, tx_type, status, contract_address, timestamp
		FROM cchain_transactions
		WHERE hash = $1
	`

	row := r.db.QueryRowContext(ctx, query, strings.ToLower(hash))
	return r.scanTransactionRow(row)
}

// GetTransactionsByBlock returns transactions for a block
func (r *Repository) GetTransactionsByBlock(ctx context.Context, blockNumber uint64, page, pageSize int) (*PaginatedResponse, error) {
	offset := page * pageSize

	query := `
		SELECT hash, block_hash, block_number, tx_from, tx_to, value, gas, gas_price,
		       gas_used, nonce, input, tx_index, tx_type, status, contract_address, timestamp
		FROM cchain_transactions
		WHERE block_number = $1
		ORDER BY tx_index ASC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, query, blockNumber, pageSize+1, offset)
	if err != nil {
		return nil, fmt.Errorf("query transactions: %w", err)
	}
	defer rows.Close()

	var txs []Transaction
	for rows.Next() {
		tx, err := r.scanTransaction(rows)
		if err != nil {
			continue
		}
		txs = append(txs, *tx)
	}

	resp := &PaginatedResponse{Items: txs}
	if len(txs) > pageSize {
		txs = txs[:pageSize]
		resp.Items = txs
	}

	return resp, nil
}

func (r *Repository) scanTransaction(rows *sql.Rows) (*Transaction, error) {
	var hash, blockHash, from string
	var to, contractAddr sql.NullString
	var value, gasPrice string
	var blockNumber, gas, gasUsed, nonce uint64
	var txIndex int
	var txType, status uint8
	var input string
	var timestamp time.Time

	err := rows.Scan(&hash, &blockHash, &blockNumber, &from, &to, &value, &gas, &gasPrice,
		&gasUsed, &nonce, &input, &txIndex, &txType, &status, &contractAddr, &timestamp)
	if err != nil {
		return nil, err
	}

	return r.buildTransaction(hash, blockHash, blockNumber, from, to, value, gas, gasPrice,
		gasUsed, nonce, input, txIndex, txType, status, contractAddr, timestamp)
}

func (r *Repository) scanTransactionRow(row *sql.Row) (*Transaction, error) {
	var hash, blockHash, from string
	var to, contractAddr sql.NullString
	var value, gasPrice string
	var blockNumber, gas, gasUsed, nonce uint64
	var txIndex int
	var txType, status uint8
	var input string
	var timestamp time.Time

	err := row.Scan(&hash, &blockHash, &blockNumber, &from, &to, &value, &gas, &gasPrice,
		&gasUsed, &nonce, &input, &txIndex, &txType, &status, &contractAddr, &timestamp)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return r.buildTransaction(hash, blockHash, blockNumber, from, to, value, gas, gasPrice,
		gasUsed, nonce, input, txIndex, txType, status, contractAddr, timestamp)
}

func (r *Repository) buildTransaction(hash, blockHash string, blockNumber uint64, from string, to sql.NullString,
	value string, gas uint64, gasPrice string, gasUsed uint64, nonce uint64, input string,
	txIndex int, txType, status uint8, contractAddr sql.NullString, timestamp time.Time) (*Transaction, error) {

	tx := &Transaction{
		Hash:             hash,
		BlockHash:        blockHash,
		BlockNumber:      &blockNumber,
		Timestamp:        &timestamp,
		From:             &Address{Hash: from},
		Value:            value,
		Gas:              gas,
		GasPrice:         gasPrice,
		GasUsed:          &gasUsed,
		Nonce:            nonce,
		Input:            input,
		TransactionIndex: &txIndex,
		Position:         &txIndex,
		Type:             int(txType),
	}

	if to.Valid {
		tx.To = &Address{Hash: to.String}
	}

	if contractAddr.Valid {
		tx.CreatedContract = &Address{Hash: contractAddr.String, IsContract: true}
	}

	if status == 1 {
		tx.Status = "ok"
		tx.Result = "success"
	} else {
		tx.Status = "error"
		tx.Result = "revert"
	}

	// Extract method ID from input
	if len(input) >= 10 {
		tx.Method = input[:10]
	}

	return tx, nil
}

// GetAddress returns address details
func (r *Repository) GetAddress(ctx context.Context, hash string) (*Address, error) {
	hash = strings.ToLower(hash)

	query := `
		SELECT hash, balance, tx_count, is_contract, contract_code, contract_creator,
		       contract_tx_hash, created_at, updated_at
		FROM cchain_addresses
		WHERE hash = $1
	`

	var balance string
	var txCount uint64
	var isContract bool
	var code, creator, creationTx sql.NullString
	var createdAt, updatedAt time.Time

	err := r.db.QueryRowContext(ctx, query, hash).Scan(&hash, &balance, &txCount, &isContract,
		&code, &creator, &creationTx, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			// Return empty address for valid addresses not yet in DB
			return &Address{Hash: hash, Balance: "0", TransactionCount: new(uint64)}, nil
		}
		return nil, err
	}

	addr := &Address{
		Hash:             hash,
		Balance:          balance,
		TransactionCount: &txCount,
		IsContract:       isContract,
	}

	if creator.Valid {
		addr.Creator = &Address{Hash: creator.String}
	}
	if creationTx.Valid {
		addr.CreationTxHash = creationTx.String
	}

	return addr, nil
}

// GetAddressTransactions returns transactions for an address
func (r *Repository) GetAddressTransactions(ctx context.Context, address string, page, pageSize int) (*PaginatedResponse, error) {
	address = strings.ToLower(address)
	offset := page * pageSize

	query := `
		SELECT hash, block_hash, block_number, tx_from, tx_to, value, gas, gas_price,
		       gas_used, nonce, input, tx_index, tx_type, status, contract_address, timestamp
		FROM cchain_transactions
		WHERE tx_from = $1 OR tx_to = $1
		ORDER BY block_number DESC, tx_index DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, query, address, pageSize+1, offset)
	if err != nil {
		return nil, fmt.Errorf("query address transactions: %w", err)
	}
	defer rows.Close()

	var txs []Transaction
	for rows.Next() {
		tx, err := r.scanTransaction(rows)
		if err != nil {
			continue
		}
		txs = append(txs, *tx)
	}

	resp := &PaginatedResponse{Items: txs}
	if len(txs) > pageSize {
		txs = txs[:pageSize]
		resp.Items = txs
		lastTx := txs[len(txs)-1]
		idx := 0
		if lastTx.TransactionIndex != nil {
			idx = *lastTx.TransactionIndex
		}
		resp.NextPageParams = &NextPageParams{
			BlockNumber:      lastTx.BlockNumber,
			TransactionIndex: &idx,
			ItemsCount:       len(txs),
		}
	}

	return resp, nil
}

// GetAddressTokenTransfers returns token transfers for an address
func (r *Repository) GetAddressTokenTransfers(ctx context.Context, address string, page, pageSize int) (*PaginatedResponse, error) {
	address = strings.ToLower(address)
	offset := page * pageSize

	query := `
		SELECT t.id, t.tx_hash, t.log_index, t.block_number, t.token_address,
		       t.token_type, t.tx_from, t.tx_to, t.value, t.token_id, t.timestamp,
		       COALESCE(tk.name, ''), COALESCE(tk.symbol, ''), COALESCE(tk.decimals, 18)
		FROM cchain_token_transfers t
		LEFT JOIN cchain_tokens tk ON t.token_address = tk.address
		WHERE t.tx_from = $1 OR t.tx_to = $1
		ORDER BY t.block_number DESC, t.log_index DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, query, address, pageSize+1, offset)
	if err != nil {
		return nil, fmt.Errorf("query token transfers: %w", err)
	}
	defer rows.Close()

	var transfers []TokenTransfer
	for rows.Next() {
		var id, txHash, tokenAddr, tokenType, from, to, value string
		var tokenID sql.NullString
		var blockNumber uint64
		var logIndex int
		var timestamp time.Time
		var tokenName, tokenSymbol string
		var tokenDecimals uint8

		err := rows.Scan(&id, &txHash, &logIndex, &blockNumber, &tokenAddr, &tokenType,
			&from, &to, &value, &tokenID, &timestamp, &tokenName, &tokenSymbol, &tokenDecimals)
		if err != nil {
			continue
		}

		transfer := TokenTransfer{
			TxHash:      txHash,
			BlockNumber: blockNumber,
			LogIndex:    logIndex,
			Timestamp:   &timestamp,
			From:        &Address{Hash: from},
			To:          &Address{Hash: to},
			Token: &Token{
				Address:  tokenAddr,
				Name:     tokenName,
				Symbol:   tokenSymbol,
				Decimals: &tokenDecimals,
				Type:     tokenType,
			},
			Total: &TokenTotal{Value: value, Decimals: &tokenDecimals},
		}

		if tokenID.Valid {
			transfer.TokenID = tokenID.String
			transfer.Total.TokenID = tokenID.String
		}

		transfers = append(transfers, transfer)
	}

	resp := &PaginatedResponse{Items: transfers}
	if len(transfers) > pageSize {
		transfers = transfers[:pageSize]
		resp.Items = transfers
	}

	return resp, nil
}

// GetTokens returns paginated tokens
func (r *Repository) GetTokens(ctx context.Context, page, pageSize int, tokenType string) (*PaginatedResponse, error) {
	offset := page * pageSize

	query := `
		SELECT address, name, symbol, decimals, total_supply, token_type, holder_count, tx_count
		FROM cchain_tokens
		WHERE ($3 = '' OR token_type = $3)
		ORDER BY holder_count DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.QueryContext(ctx, query, pageSize+1, offset, tokenType)
	if err != nil {
		return nil, fmt.Errorf("query tokens: %w", err)
	}
	defer rows.Close()

	var tokens []Token
	for rows.Next() {
		var address, name, symbol, totalSupply, ttype string
		var decimals uint8
		var holderCount, txCount uint64

		if err := rows.Scan(&address, &name, &symbol, &decimals, &totalSupply, &ttype, &holderCount, &txCount); err != nil {
			continue
		}

		tokens = append(tokens, Token{
			Address:       address,
			Name:          name,
			Symbol:        symbol,
			Decimals:      &decimals,
			TotalSupply:   totalSupply,
			Type:          ttype,
			HolderCount:   &holderCount,
			TransferCount: &txCount,
		})
	}

	resp := &PaginatedResponse{Items: tokens}
	if len(tokens) > pageSize {
		tokens = tokens[:pageSize]
		resp.Items = tokens
	}

	return resp, nil
}

// GetToken returns token details
func (r *Repository) GetToken(ctx context.Context, address string) (*Token, error) {
	address = strings.ToLower(address)

	query := `
		SELECT address, name, symbol, decimals, total_supply, token_type, holder_count, tx_count
		FROM cchain_tokens
		WHERE address = $1
	`

	var name, symbol, totalSupply, ttype string
	var decimals uint8
	var holderCount, txCount uint64

	err := r.db.QueryRowContext(ctx, query, address).Scan(&address, &name, &symbol, &decimals,
		&totalSupply, &ttype, &holderCount, &txCount)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &Token{
		Address:       address,
		Name:          name,
		Symbol:        symbol,
		Decimals:      &decimals,
		TotalSupply:   totalSupply,
		Type:          ttype,
		HolderCount:   &holderCount,
		TransferCount: &txCount,
	}, nil
}

// GetSmartContract returns verified contract details
func (r *Repository) GetSmartContract(ctx context.Context, address string) (*SmartContract, error) {
	address = strings.ToLower(address)

	// First check if address is a contract
	var isContract bool
	var code sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT is_contract, contract_code FROM cchain_addresses WHERE hash = $1
	`, address).Scan(&isContract, &code)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if !isContract {
		return nil, ErrNotFound
	}

	// Return basic contract info (full verification would require additional tables)
	contract := &SmartContract{
		Address:    address,
		IsVerified: false, // TODO: implement verification storage
	}

	return contract, nil
}

// Search performs universal search
func (r *Repository) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	var results []SearchResult
	lowerQuery := strings.ToLower(query)

	// Check if it's a block number
	if height, err := strconv.ParseUint(query, 10, 64); err == nil {
		block, err := r.GetBlockByNumber(ctx, height)
		if err == nil {
			results = append(results, SearchResult{
				Type:        "block",
				BlockNumber: &block.Height,
				Hash:        block.Hash,
			})
		}
	}

	// Check if it's a hash (tx or block)
	if len(query) == 66 && strings.HasPrefix(lowerQuery, "0x") {
		// Try transaction
		tx, err := r.GetTransactionByHash(ctx, query)
		if err == nil {
			results = append(results, SearchResult{
				Type: "transaction",
				Hash: tx.Hash,
			})
		}

		// Try block
		block, err := r.GetBlockByHash(ctx, query)
		if err == nil {
			results = append(results, SearchResult{
				Type:        "block",
				Hash:        block.Hash,
				BlockNumber: &block.Height,
			})
		}
	}

	// Check if it's an address
	if len(query) == 42 && strings.HasPrefix(lowerQuery, "0x") {
		addr, err := r.GetAddress(ctx, query)
		if err == nil {
			resultType := "address"
			if addr.IsContract {
				resultType = "contract"
			}
			results = append(results, SearchResult{
				Type:            resultType,
				Address:         addr.Hash,
				IsSmartContract: addr.IsContract,
			})
		}

		// Check if it's a token
		token, err := r.GetToken(ctx, query)
		if err == nil {
			results = append(results, SearchResult{
				Type:      "token",
				Address:   token.Address,
				Name:      token.Name,
				Symbol:    token.Symbol,
				TokenType: token.Type,
			})
		}
	}

	// Search tokens by name/symbol
	tokenRows, err := r.db.QueryContext(ctx, `
		SELECT address, name, symbol, token_type
		FROM cchain_tokens
		WHERE LOWER(name) LIKE $1 OR LOWER(symbol) LIKE $1
		LIMIT $2
	`, "%"+lowerQuery+"%", limit)
	if err == nil {
		defer tokenRows.Close()
		for tokenRows.Next() {
			var address, name, symbol, tokenType string
			if tokenRows.Scan(&address, &name, &symbol, &tokenType) == nil {
				results = append(results, SearchResult{
					Type:      "token",
					Address:   address,
					Name:      name,
					Symbol:    symbol,
					TokenType: tokenType,
				})
			}
		}
	}

	// Limit results
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// GetStats returns chain statistics
func (r *Repository) GetStats(ctx context.Context) (*ChainStats, error) {
	stats := &ChainStats{}

	// Get extended stats
	row := r.db.QueryRowContext(ctx, `
		SELECT total_transactions, total_addresses, total_contracts, total_tokens,
		       total_token_transfers, total_gas_used, avg_gas_price, avg_block_time, tps_24h
		FROM cchain_extended_stats WHERE id = 1
	`)

	var avgGasPrice string
	var avgBlockTime, tps24h float64

	err := row.Scan(&stats.TotalTransactions, &stats.TotalAddresses, &stats.TotalContracts,
		&stats.TotalTokens, &stats.TotalTokenTransfers, &stats.TotalGasUsed,
		&avgGasPrice, &avgBlockTime, &tps24h)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	stats.AverageBlockTime = avgBlockTime
	stats.GasPrice = avgGasPrice

	// Get block count
	r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cchain_blocks").Scan(&stats.TotalBlocks)

	// Calculate transactions today
	r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cchain_transactions
		WHERE timestamp > NOW() - INTERVAL '24 hours'
	`).Scan(&stats.TxnsToday)

	return stats, nil
}

// GetLogs returns event logs with filters
func (r *Repository) GetLogs(ctx context.Context, address string, fromBlock, toBlock uint64, topics []string, page, pageSize int) (*PaginatedResponse, error) {
	offset := page * pageSize

	args := []interface{}{pageSize + 1, offset}
	conditions := []string{}
	argIdx := 3

	if address != "" {
		conditions = append(conditions, fmt.Sprintf("address = $%d", argIdx))
		args = append(args, strings.ToLower(address))
		argIdx++
	}

	if fromBlock > 0 {
		conditions = append(conditions, fmt.Sprintf("block_number >= $%d", argIdx))
		args = append(args, fromBlock)
		argIdx++
	}

	if toBlock > 0 {
		conditions = append(conditions, fmt.Sprintf("block_number <= $%d", argIdx))
		args = append(args, toBlock)
		argIdx++
	}

	// Topic filters
	for i, topic := range topics {
		if topic != "" {
			conditions = append(conditions, fmt.Sprintf("topics->>%d = $%d", i, argIdx))
			args = append(args, topic)
			argIdx++
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT tx_hash, log_index, block_number, address, topics, data, removed
		FROM cchain_logs
		%s
		ORDER BY block_number DESC, log_index DESC
		LIMIT $1 OFFSET $2
	`, whereClause)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query logs: %w", err)
	}
	defer rows.Close()

	var logs []Log
	for rows.Next() {
		var txHash, address, data string
		var blockNumber uint64
		var logIndex int
		var topicsJSON []byte
		var removed bool

		if err := rows.Scan(&txHash, &logIndex, &blockNumber, &address, &topicsJSON, &data, &removed); err != nil {
			continue
		}

		var topics []string
		json.Unmarshal(topicsJSON, &topics)

		logs = append(logs, Log{
			TxHash:      txHash,
			BlockNumber: blockNumber,
			Index:       logIndex,
			Address:     &Address{Hash: address},
			Data:        data,
			Topics:      topics,
		})
	}

	resp := &PaginatedResponse{Items: logs}
	if len(logs) > pageSize {
		logs = logs[:pageSize]
		resp.Items = logs
	}

	return resp, nil
}

// GetInternalTransactions returns internal transactions for a tx hash
func (r *Repository) GetInternalTransactions(ctx context.Context, txHash string, page, pageSize int) (*PaginatedResponse, error) {
	offset := page * pageSize
	txHash = strings.ToLower(txHash)

	query := `
		SELECT id, tx_hash, block_number, trace_index, call_type, tx_from, tx_to,
		       value, gas, gas_used, input, output, error, timestamp
		FROM cchain_internal_transactions
		WHERE tx_hash = $1
		ORDER BY trace_index ASC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, query, txHash, pageSize+1, offset)
	if err != nil {
		return nil, fmt.Errorf("query internal transactions: %w", err)
	}
	defer rows.Close()

	var itxs []InternalTransaction
	for rows.Next() {
		var id, hash, from string
		var to, input, output, errMsg sql.NullString
		var value string
		var blockNumber, gas, gasUsed uint64
		var traceIndex int
		var callType string
		var timestamp time.Time

		err := rows.Scan(&id, &hash, &blockNumber, &traceIndex, &callType, &from, &to,
			&value, &gas, &gasUsed, &input, &output, &errMsg, &timestamp)
		if err != nil {
			continue
		}

		itx := InternalTransaction{
			TxHash:      hash,
			BlockNumber: blockNumber,
			Index:       traceIndex,
			Timestamp:   &timestamp,
			Type:        callType,
			CallType:    callType,
			From:        &Address{Hash: from},
			Value:       value,
			Gas:         gas,
			GasUsed:     &gasUsed,
			Success:     errMsg.String == "",
		}

		if to.Valid {
			itx.To = &Address{Hash: to.String}
		}
		if input.Valid {
			itx.Input = input.String
		}
		if output.Valid {
			itx.Output = output.String
		}
		if errMsg.Valid && errMsg.String != "" {
			itx.Error = errMsg.String
			itx.Success = false
		}

		itxs = append(itxs, itx)
	}

	resp := &PaginatedResponse{Items: itxs}
	if len(itxs) > pageSize {
		itxs = itxs[:pageSize]
		resp.Items = itxs
	}

	return resp, nil
}

// GetAddressBalance returns balance for Etherscan API
func (r *Repository) GetAddressBalance(ctx context.Context, address string) (string, error) {
	address = strings.ToLower(address)

	var balance string
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(balance, '0') FROM cchain_addresses WHERE hash = $1
	`, address).Scan(&balance)

	if err == sql.ErrNoRows {
		return "0", nil
	}
	return balance, err
}

// CoinBalanceEntry represents a single balance history entry
type CoinBalanceEntry struct {
	BlockNumber uint64    `json:"block_number"`
	BlockHash   string    `json:"block_hash,omitempty"`
	Delta       string    `json:"delta,omitempty"`
	Value       string    `json:"value"`
	Timestamp   time.Time `json:"block_timestamp"`
	TxHash      string    `json:"transaction_hash,omitempty"`
}

// DailyBalanceEntry represents balance aggregated by day
type DailyBalanceEntry struct {
	Date  string `json:"date"`
	Value string `json:"value"`
}

// GetAddressCoinBalanceHistory returns coin balance history for an address
func (r *Repository) GetAddressCoinBalanceHistory(ctx context.Context, address string, page, pageSize int) (*PaginatedResponse, error) {
	address = strings.ToLower(address)
	if !strings.HasPrefix(address, "0x") {
		address = "0x" + address
	}
	offset := page * pageSize

	// Query address_coin_balances table for history, join with blocks for block_hash and timestamp
	query := `
		SELECT 
			cb.block_number,
			COALESCE(encode(b.hash, 'hex'), '') as block_hash,
			COALESCE(cb.delta::text, '0') as delta,
			COALESCE(cb.value::text, '0') as value,
			to_timestamp(b.timestamp) as block_timestamp
		FROM address_coin_balances cb
		LEFT JOIN blocks b ON cb.chain_id = b.chain_id AND cb.block_number = b.number
		WHERE cb.chain_id = $1 AND cb.address_hash = decode($2, 'hex')
		ORDER BY cb.block_number DESC
		LIMIT $3 OFFSET $4
	`

	// Remove 0x prefix for decode
	addrHex := strings.TrimPrefix(address, "0x")

	rows, err := r.db.QueryContext(ctx, query, r.chainID, addrHex, pageSize+1, offset)
	if err != nil {
		return nil, fmt.Errorf("query coin balance history: %w", err)
	}
	defer rows.Close()

	var entries []CoinBalanceEntry
	for rows.Next() {
		var entry CoinBalanceEntry
		var blockHash string
		var timestamp time.Time

		err := rows.Scan(&entry.BlockNumber, &blockHash, &entry.Delta, &entry.Value, &timestamp)
		if err != nil {
			continue
		}

		if blockHash != "" {
			entry.BlockHash = "0x" + blockHash
		}
		entry.Timestamp = timestamp
		entries = append(entries, entry)
	}

	resp := &PaginatedResponse{Items: entries}
	if len(entries) > pageSize {
		entries = entries[:pageSize]
		resp.Items = entries
		lastEntry := entries[len(entries)-1]
		blockNum := lastEntry.BlockNumber
		resp.NextPageParams = &NextPageParams{
			BlockNumber: &blockNum,
			ItemsCount:  len(entries),
		}
	}

	return resp, nil
}

// GetAddressCoinBalanceByDay returns coin balance history aggregated by day
func (r *Repository) GetAddressCoinBalanceByDay(ctx context.Context, address string, page, pageSize int) (*PaginatedResponse, error) {
	address = strings.ToLower(address)
	if !strings.HasPrefix(address, "0x") {
		address = "0x" + address
	}
	offset := page * pageSize

	// Aggregate balance by day - get last balance of each day
	query := `
		WITH daily_balances AS (
			SELECT 
				DATE(to_timestamp(b.timestamp)) as date,
				cb.value,
				ROW_NUMBER() OVER (PARTITION BY DATE(to_timestamp(b.timestamp)) ORDER BY cb.block_number DESC) as rn
			FROM address_coin_balances cb
			JOIN blocks b ON cb.chain_id = b.chain_id AND cb.block_number = b.number
			WHERE cb.chain_id = $1 AND cb.address_hash = decode($2, 'hex')
		)
		SELECT date, COALESCE(value::text, '0')
		FROM daily_balances
		WHERE rn = 1
		ORDER BY date DESC
		LIMIT $3 OFFSET $4
	`

	addrHex := strings.TrimPrefix(address, "0x")

	rows, err := r.db.QueryContext(ctx, query, r.chainID, addrHex, pageSize+1, offset)
	if err != nil {
		return nil, fmt.Errorf("query coin balance by day: %w", err)
	}
	defer rows.Close()

	var entries []DailyBalanceEntry
	for rows.Next() {
		var entry DailyBalanceEntry
		var date time.Time

		err := rows.Scan(&date, &entry.Value)
		if err != nil {
			continue
		}

		entry.Date = date.Format("2006-01-02")
		entries = append(entries, entry)
	}

	resp := &PaginatedResponse{Items: entries}
	if len(entries) > pageSize {
		entries = entries[:pageSize]
		resp.Items = entries
	}

	return resp, nil
}
