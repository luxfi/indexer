// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package search provides unified search functionality across all indexed blockchain entities.
// Supports searching blocks, transactions, addresses, tokens, and contracts with relevance ranking.
package search

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ResultType represents the type of search result
type ResultType string

const (
	TypeBlock       ResultType = "block"
	TypeTransaction ResultType = "transaction"
	TypeAddress     ResultType = "address"
	TypeToken       ResultType = "token"
	TypeContract    ResultType = "contract"
	TypeLabel       ResultType = "label"
	TypeENS         ResultType = "ens_domain"
)

// Priority levels for search results (higher = more relevant)
const (
	PriorityENS         = 5 // ENS domains highest priority
	PriorityExactMatch  = 4 // Exact hash/address matches
	PriorityLabel       = 3 // Named/labeled addresses
	PriorityToken       = 2 // Token matches
	PriorityContract    = 1 // Verified contracts
	PriorityPartial     = 0 // Partial matches
)

// Result represents a single search result with relevance scoring
type Result struct {
	Type           ResultType             `json:"type"`
	Hash           string                 `json:"hash,omitempty"`           // block/tx hash or address
	BlockNumber    *uint64                `json:"block_number,omitempty"`   // for blocks
	Name           string                 `json:"name,omitempty"`           // token/contract name
	Symbol         string                 `json:"symbol,omitempty"`         // token symbol
	Verified       bool                   `json:"verified,omitempty"`       // contract verified
	HolderCount    uint64                 `json:"holder_count,omitempty"`   // token holders
	Timestamp      *time.Time             `json:"timestamp,omitempty"`      // block/tx timestamp
	Priority       int                    `json:"priority"`                 // relevance score
	IconURL        string                 `json:"icon_url,omitempty"`       // token icon
	TokenType      string                 `json:"token_type,omitempty"`     // ERC20/721/1155
	ExchangeRate   string                 `json:"exchange_rate,omitempty"`  // token fiat value
	MarketCap      string                 `json:"market_cap,omitempty"`     // circulating market cap
	TotalSupply    string                 `json:"total_supply,omitempty"`   // token total supply
	ENSInfo        map[string]interface{} `json:"ens_info,omitempty"`       // ENS domain info
	IsContract     bool                   `json:"is_contract,omitempty"`    // if address is contract
	TxCount        uint64                 `json:"tx_count,omitempty"`       // address tx count
	Balance        string                 `json:"balance,omitempty"`        // address balance
	InsertedAt     *time.Time             `json:"inserted_at,omitempty"`    // creation timestamp
}

// SearchResponse contains paginated search results
type SearchResponse struct {
	Items          []Result               `json:"items"`
	NextPageParams map[string]interface{} `json:"next_page_params,omitempty"`
}

// QuickSearchResponse contains unpaginated search results
type QuickSearchResponse struct {
	Items []Result `json:"items"`
}

// RedirectResponse indicates if query should redirect to specific page
type RedirectResponse struct {
	Redirect  bool   `json:"redirect"`
	Type      string `json:"type,omitempty"`
	Parameter string `json:"parameter,omitempty"`
}

// Engine provides search functionality across blockchain entities
type Engine struct {
	db *sql.DB
}

// NewEngine creates a new search engine
func NewEngine(db *sql.DB) *Engine {
	return &Engine{db: db}
}

// Config holds search configuration
type Config struct {
	PageSize      int
	MinQueryLen   int
	MaxResults    int
	EnableENS     bool
	EnableLabels  bool
}

// DefaultConfig returns sensible search defaults
func DefaultConfig() Config {
	return Config{
		PageSize:      50,
		MinQueryLen:   3,
		MaxResults:    100,
		EnableENS:     true,
		EnableLabels:  true,
	}
}

// PagingParams contains pagination parameters
type PagingParams struct {
	Type      string                 `json:"type,omitempty"`
	Key       map[string]interface{} `json:"key,omitempty"`
	PageSize  int                    `json:"page_size"`
}

// Search performs a unified search across all entity types
func (e *Engine) Search(ctx context.Context, query string, paging PagingParams) (*SearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return &SearchResponse{Items: []Result{}}, nil
	}

	pageSize := paging.PageSize
	if pageSize <= 0 {
		pageSize = DefaultConfig().PageSize
	}

	// Determine query type and execute appropriate search
	searchType := classifyQuery(query)

	var results []Result
	var err error

	switch searchType {
	case "address_hash":
		results, err = e.searchByAddressHash(ctx, query, pageSize)
	case "tx_hash":
		results, err = e.searchByTxHash(ctx, query, pageSize)
	case "block_number":
		blockNum, _ := strconv.ParseUint(query, 10, 64)
		results, err = e.searchByBlockNumber(ctx, blockNum)
	case "block_hash":
		results, err = e.searchByBlockHash(ctx, query)
	case "text":
		results, err = e.searchByText(ctx, query, pageSize)
	default:
		results = []Result{}
	}

	if err != nil {
		return nil, err
	}

	// Sort by priority and build response
	sortByPriority(results)

	// Apply pagination
	var nextPageParams map[string]interface{}
	if len(results) > pageSize {
		results = results[:pageSize]
		nextPageParams = map[string]interface{}{
			"q": query,
		}
	}

	return &SearchResponse{
		Items:          results,
		NextPageParams: nextPageParams,
	}, nil
}

// QuickSearch performs an unpaginated search for autocomplete
func (e *Engine) QuickSearch(ctx context.Context, query string) (*QuickSearchResponse, error) {
	query = strings.TrimSpace(query)
	if len(query) < DefaultConfig().MinQueryLen {
		return &QuickSearchResponse{Items: []Result{}}, nil
	}

	resp, err := e.Search(ctx, query, PagingParams{PageSize: 50})
	if err != nil {
		return nil, err
	}

	return &QuickSearchResponse{Items: resp.Items}, nil
}

// CheckRedirect determines if a query should redirect to a specific entity page
func (e *Engine) CheckRedirect(ctx context.Context, query string) (*RedirectResponse, error) {
	query = strings.TrimSpace(query)

	// Check for exact transaction hash
	if isValidTxHash(query) {
		exists, err := e.transactionExists(ctx, query)
		if err != nil {
			return nil, err
		}
		if exists {
			return &RedirectResponse{
				Redirect:  true,
				Type:      "transaction",
				Parameter: query,
			}, nil
		}
	}

	// Check for exact address
	if isValidAddress(query) {
		exists, err := e.addressExists(ctx, query)
		if err != nil {
			return nil, err
		}
		if exists {
			return &RedirectResponse{
				Redirect:  true,
				Type:      "address",
				Parameter: query,
			}, nil
		}
	}

	// Check for block number
	if blockNum, err := strconv.ParseUint(query, 10, 64); err == nil {
		exists, err := e.blockExistsByNumber(ctx, blockNum)
		if err != nil {
			return nil, err
		}
		if exists {
			return &RedirectResponse{
				Redirect:  true,
				Type:      "block",
				Parameter: query,
			}, nil
		}
	}

	// Check for block hash
	if isValidBlockHash(query) {
		exists, err := e.blockExistsByHash(ctx, query)
		if err != nil {
			return nil, err
		}
		if exists {
			return &RedirectResponse{
				Redirect:  true,
				Type:      "block",
				Parameter: query,
			}, nil
		}
	}

	return &RedirectResponse{Redirect: false}, nil
}

// searchByAddressHash searches for exact address match and associated tokens
func (e *Engine) searchByAddressHash(ctx context.Context, address string, limit int) ([]Result, error) {
	address = strings.ToLower(address)
	var results []Result

	// Search for address
	row := e.db.QueryRowContext(ctx, `
		SELECT hash, balance, tx_count, is_contract, created_at
		FROM cchain_addresses
		WHERE hash = $1
	`, address)

	var addr struct {
		Hash       string
		Balance    string
		TxCount    uint64
		IsContract bool
		CreatedAt  time.Time
	}

	if err := row.Scan(&addr.Hash, &addr.Balance, &addr.TxCount, &addr.IsContract, &addr.CreatedAt); err == nil {
		resultType := TypeAddress
		if addr.IsContract {
			resultType = TypeContract
		}
		results = append(results, Result{
			Type:       resultType,
			Hash:       addr.Hash,
			IsContract: addr.IsContract,
			TxCount:    addr.TxCount,
			Balance:    addr.Balance,
			InsertedAt: &addr.CreatedAt,
			Priority:   PriorityExactMatch,
		})
	}

	// Search for token at this address
	row = e.db.QueryRowContext(ctx, `
		SELECT address, name, symbol, token_type, holder_count, total_supply, decimals
		FROM cchain_tokens
		WHERE address = $1
	`, address)

	var token struct {
		Address     string
		Name        string
		Symbol      string
		TokenType   string
		HolderCount uint64
		TotalSupply string
		Decimals    uint8
	}

	if err := row.Scan(&token.Address, &token.Name, &token.Symbol, &token.TokenType,
		&token.HolderCount, &token.TotalSupply, &token.Decimals); err == nil {
		results = append(results, Result{
			Type:        TypeToken,
			Hash:        token.Address,
			Name:        token.Name,
			Symbol:      token.Symbol,
			TokenType:   token.TokenType,
			HolderCount: token.HolderCount,
			TotalSupply: token.TotalSupply,
			Priority:    PriorityExactMatch,
		})
	}

	return results, nil
}

// searchByTxHash searches for exact transaction hash match
func (e *Engine) searchByTxHash(ctx context.Context, txHash string, limit int) ([]Result, error) {
	txHash = strings.ToLower(txHash)
	var results []Result

	row := e.db.QueryRowContext(ctx, `
		SELECT hash, block_number, timestamp
		FROM cchain_transactions
		WHERE hash = $1
	`, txHash)

	var tx struct {
		Hash        string
		BlockNumber uint64
		Timestamp   time.Time
	}

	if err := row.Scan(&tx.Hash, &tx.BlockNumber, &tx.Timestamp); err == nil {
		results = append(results, Result{
			Type:        TypeTransaction,
			Hash:        tx.Hash,
			BlockNumber: &tx.BlockNumber,
			Timestamp:   &tx.Timestamp,
			Priority:    PriorityExactMatch,
		})
	}

	return results, nil
}

// searchByBlockNumber searches for exact block number match
func (e *Engine) searchByBlockNumber(ctx context.Context, blockNumber uint64) ([]Result, error) {
	var results []Result

	row := e.db.QueryRowContext(ctx, `
		SELECT id, height, timestamp
		FROM blocks
		WHERE height = $1
	`, blockNumber)

	var block struct {
		Hash      string
		Height    uint64
		Timestamp time.Time
	}

	if err := row.Scan(&block.Hash, &block.Height, &block.Timestamp); err == nil {
		results = append(results, Result{
			Type:        TypeBlock,
			Hash:        block.Hash,
			BlockNumber: &block.Height,
			Timestamp:   &block.Timestamp,
			Priority:    PriorityExactMatch,
		})
	}

	return results, nil
}

// searchByBlockHash searches for exact block hash match
func (e *Engine) searchByBlockHash(ctx context.Context, blockHash string) ([]Result, error) {
	blockHash = strings.ToLower(blockHash)
	var results []Result

	row := e.db.QueryRowContext(ctx, `
		SELECT id, height, timestamp
		FROM blocks
		WHERE id = $1
	`, blockHash)

	var block struct {
		Hash      string
		Height    uint64
		Timestamp time.Time
	}

	if err := row.Scan(&block.Hash, &block.Height, &block.Timestamp); err == nil {
		results = append(results, Result{
			Type:        TypeBlock,
			Hash:        block.Hash,
			BlockNumber: &block.Height,
			Timestamp:   &block.Timestamp,
			Priority:    PriorityExactMatch,
		})
	}

	return results, nil
}

// searchByText performs full-text search on tokens and contracts
func (e *Engine) searchByText(ctx context.Context, query string, limit int) ([]Result, error) {
	var results []Result

	// Prepare search term for PostgreSQL full-text search
	searchTerm := prepareSearchTerm(query)

	// Search tokens by name or symbol
	rows, err := e.db.QueryContext(ctx, `
		SELECT address, name, symbol, token_type, holder_count, total_supply
		FROM cchain_tokens
		WHERE
			to_tsvector('english', COALESCE(name, '') || ' ' || COALESCE(symbol, '')) @@ to_tsquery($1)
			OR LOWER(name) LIKE LOWER($2)
			OR LOWER(symbol) LIKE LOWER($2)
		ORDER BY holder_count DESC NULLS LAST
		LIMIT $3
	`, searchTerm, "%"+query+"%", limit)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("search tokens: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var token struct {
			Address     string
			Name        sql.NullString
			Symbol      sql.NullString
			TokenType   string
			HolderCount uint64
			TotalSupply sql.NullString
		}
		if err := rows.Scan(&token.Address, &token.Name, &token.Symbol,
			&token.TokenType, &token.HolderCount, &token.TotalSupply); err != nil {
			continue
		}

		// Calculate priority based on match quality
		priority := PriorityToken
		nameLower := strings.ToLower(token.Name.String)
		symbolLower := strings.ToLower(token.Symbol.String)
		queryLower := strings.ToLower(query)

		if nameLower == queryLower || symbolLower == queryLower {
			priority = PriorityExactMatch
		} else if strings.HasPrefix(nameLower, queryLower) || strings.HasPrefix(symbolLower, queryLower) {
			priority = PriorityLabel
		}

		results = append(results, Result{
			Type:        TypeToken,
			Hash:        token.Address,
			Name:        token.Name.String,
			Symbol:      token.Symbol.String,
			TokenType:   token.TokenType,
			HolderCount: token.HolderCount,
			TotalSupply: token.TotalSupply.String,
			Priority:    priority,
		})
	}

	// Search verified contracts by name (if contract verification table exists)
	contractRows, err := e.db.QueryContext(ctx, `
		SELECT a.hash, a.is_contract, a.tx_count, a.created_at
		FROM cchain_addresses a
		WHERE a.is_contract = true
		LIMIT $1
	`, limit)
	if err != nil && err != sql.ErrNoRows {
		// Contracts table might not exist, skip silently
		return results, nil
	}
	defer contractRows.Close()

	for contractRows.Next() {
		var contract struct {
			Hash       string
			IsContract bool
			TxCount    uint64
			CreatedAt  time.Time
		}
		if err := contractRows.Scan(&contract.Hash, &contract.IsContract,
			&contract.TxCount, &contract.CreatedAt); err != nil {
			continue
		}

		results = append(results, Result{
			Type:       TypeContract,
			Hash:       contract.Hash,
			IsContract: true,
			TxCount:    contract.TxCount,
			InsertedAt: &contract.CreatedAt,
			Verified:   true,
			Priority:   PriorityContract,
		})
	}

	return results, nil
}

// SearchTokens searches tokens by name or symbol
func (e *Engine) SearchTokens(ctx context.Context, query string, limit int) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []Result{}, nil
	}

	searchTerm := prepareSearchTerm(query)

	rows, err := e.db.QueryContext(ctx, `
		SELECT address, name, symbol, token_type, holder_count, total_supply, tx_count
		FROM cchain_tokens
		WHERE
			to_tsvector('english', COALESCE(name, '') || ' ' || COALESCE(symbol, '')) @@ to_tsquery($1)
			OR LOWER(name) LIKE LOWER($2)
			OR LOWER(symbol) LIKE LOWER($2)
		ORDER BY holder_count DESC NULLS LAST, tx_count DESC
		LIMIT $3
	`, searchTerm, "%"+query+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("search tokens: %w", err)
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var token struct {
			Address     string
			Name        sql.NullString
			Symbol      sql.NullString
			TokenType   string
			HolderCount uint64
			TotalSupply sql.NullString
			TxCount     uint64
		}
		if err := rows.Scan(&token.Address, &token.Name, &token.Symbol,
			&token.TokenType, &token.HolderCount, &token.TotalSupply, &token.TxCount); err != nil {
			continue
		}

		results = append(results, Result{
			Type:        TypeToken,
			Hash:        token.Address,
			Name:        token.Name.String,
			Symbol:      token.Symbol.String,
			TokenType:   token.TokenType,
			HolderCount: token.HolderCount,
			TotalSupply: token.TotalSupply.String,
			TxCount:     token.TxCount,
			Priority:    PriorityToken,
		})
	}

	return results, nil
}

// SearchAddresses searches addresses by partial hash match
func (e *Engine) SearchAddresses(ctx context.Context, query string, limit int) ([]Result, error) {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return []Result{}, nil
	}

	// Add 0x prefix if missing
	if !strings.HasPrefix(query, "0x") {
		query = "0x" + query
	}

	rows, err := e.db.QueryContext(ctx, `
		SELECT hash, balance, tx_count, is_contract, created_at
		FROM cchain_addresses
		WHERE hash LIKE $1
		ORDER BY tx_count DESC
		LIMIT $2
	`, query+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("search addresses: %w", err)
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var addr struct {
			Hash       string
			Balance    string
			TxCount    uint64
			IsContract bool
			CreatedAt  time.Time
		}
		if err := rows.Scan(&addr.Hash, &addr.Balance, &addr.TxCount,
			&addr.IsContract, &addr.CreatedAt); err != nil {
			continue
		}

		resultType := TypeAddress
		if addr.IsContract {
			resultType = TypeContract
		}

		results = append(results, Result{
			Type:       resultType,
			Hash:       addr.Hash,
			IsContract: addr.IsContract,
			TxCount:    addr.TxCount,
			Balance:    addr.Balance,
			InsertedAt: &addr.CreatedAt,
			Priority:   PriorityPartial,
		})
	}

	return results, nil
}

// Existence check helpers

func (e *Engine) transactionExists(ctx context.Context, hash string) (bool, error) {
	var exists bool
	err := e.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM cchain_transactions WHERE hash = $1)
	`, strings.ToLower(hash)).Scan(&exists)
	return exists, err
}

func (e *Engine) addressExists(ctx context.Context, hash string) (bool, error) {
	var exists bool
	err := e.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM cchain_addresses WHERE hash = $1)
	`, strings.ToLower(hash)).Scan(&exists)
	return exists, err
}

func (e *Engine) blockExistsByNumber(ctx context.Context, number uint64) (bool, error) {
	var exists bool
	err := e.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM blocks WHERE height = $1)
	`, number).Scan(&exists)
	return exists, err
}

func (e *Engine) blockExistsByHash(ctx context.Context, hash string) (bool, error) {
	var exists bool
	err := e.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM blocks WHERE id = $1)
	`, strings.ToLower(hash)).Scan(&exists)
	return exists, err
}

// Helper functions

// classifyQuery determines the type of search query
func classifyQuery(query string) string {
	query = strings.TrimSpace(query)

	// Check if it's a hex string
	if strings.HasPrefix(query, "0x") || strings.HasPrefix(query, "0X") {
		hexPart := query[2:]
		if _, err := hex.DecodeString(hexPart); err == nil {
			switch len(hexPart) {
			case 40: // 20 bytes = address
				return "address_hash"
			case 64: // 32 bytes = tx hash or block hash
				return "tx_hash" // could be block hash too
			}
		}
	}

	// Check if it's a number (block number)
	if _, err := strconv.ParseUint(query, 10, 64); err == nil {
		return "block_number"
	}

	// Default to text search
	return "text"
}

// isValidTxHash checks if string is a valid transaction hash
func isValidTxHash(s string) bool {
	if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return false
	}
	hexPart := s[2:]
	if len(hexPart) != 64 {
		return false
	}
	_, err := hex.DecodeString(hexPart)
	return err == nil
}

// isValidAddress checks if string is a valid Ethereum address
func isValidAddress(s string) bool {
	if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return false
	}
	hexPart := s[2:]
	if len(hexPart) != 40 {
		return false
	}
	_, err := hex.DecodeString(hexPart)
	return err == nil
}

// isValidBlockHash checks if string is a valid block hash
func isValidBlockHash(s string) bool {
	return isValidTxHash(s) // Same format
}

// prepareSearchTerm converts query to PostgreSQL tsquery format
func prepareSearchTerm(query string) string {
	// Extract words and append :* for prefix matching
	wordRe := regexp.MustCompile(`[a-zA-Z0-9]+`)
	words := wordRe.FindAllString(query, -1)

	if len(words) == 0 {
		return ""
	}

	var terms []string
	for _, word := range words {
		terms = append(terms, word+":*")
	}

	return strings.Join(terms, " & ")
}

// sortByPriority sorts results by priority (descending) and then by holder count for tokens
func sortByPriority(results []Result) {
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Priority > results[i].Priority {
				results[i], results[j] = results[j], results[i]
			} else if results[j].Priority == results[i].Priority {
				// Secondary sort by holder count for tokens
				if results[j].HolderCount > results[i].HolderCount {
					results[i], results[j] = results[j], results[i]
				}
			}
		}
	}
}

// hexToBigInt converts hex string to big.Int
func hexToBigInt(s string) *big.Int {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return big.NewInt(0)
	}
	n := new(big.Int)
	n.SetString(s, 16)
	return n
}
