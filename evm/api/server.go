// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// Config for the API server
type Config struct {
	HTTPPort    int
	ChainID     int64
	ChainName   string
	DatabaseURL string
	RPCEndpoint string
}

// Server provides REST, RPC, GraphQL and WebSocket APIs
type Server struct {
	config     Config
	db         *sql.DB
	repo       *Repository
	rpcHandler *RPCHandler
	wsHub      *WebSocketHub
	router     *mux.Router
}

// NewServer creates a new API server
func NewServer(cfg Config, db *sql.DB) *Server {
	repo := NewRepository(db, cfg.ChainID)
	wsHub := NewWebSocketHub()

	s := &Server{
		config:     cfg,
		db:         db,
		repo:       repo,
		rpcHandler: NewRPCHandler(repo),
		wsHub:      wsHub,
		router:     mux.NewRouter(),
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	// Health check
	s.router.HandleFunc("/health", s.handleHealth).Methods("GET")
	s.router.HandleFunc("/api/healthcheck", s.handleHealth).Methods("GET")

	// REST API v2 (Blockscout compatible)
	api := s.router.PathPrefix("/api/v2").Subrouter()

	// Blocks
	api.HandleFunc("/blocks", s.handleBlocks).Methods("GET")
	api.HandleFunc("/blocks/{block_hash_or_number}", s.handleBlock).Methods("GET")
	api.HandleFunc("/blocks/{block_hash_or_number}/transactions", s.handleBlockTransactions).Methods("GET")

	// Transactions
	api.HandleFunc("/transactions", s.handleTransactions).Methods("GET")
	api.HandleFunc("/transactions/{transaction_hash}", s.handleTransaction).Methods("GET")
	api.HandleFunc("/transactions/{transaction_hash}/token-transfers", s.handleTxTokenTransfers).Methods("GET")
	api.HandleFunc("/transactions/{transaction_hash}/internal-transactions", s.handleTxInternalTxs).Methods("GET")
	api.HandleFunc("/transactions/{transaction_hash}/logs", s.handleTxLogs).Methods("GET")
	api.HandleFunc("/transactions/{transaction_hash}/raw-trace", s.handleTxRawTrace).Methods("GET")
	api.HandleFunc("/transactions/{transaction_hash}/state-changes", s.handleTxStateChanges).Methods("GET")

	// Addresses
	api.HandleFunc("/addresses", s.handleAddresses).Methods("GET")
	api.HandleFunc("/addresses/{address_hash}", s.handleAddress).Methods("GET")
	api.HandleFunc("/addresses/{address_hash}/transactions", s.handleAddressTransactions).Methods("GET")
	api.HandleFunc("/addresses/{address_hash}/token-transfers", s.handleAddressTokenTransfers).Methods("GET")
	api.HandleFunc("/addresses/{address_hash}/internal-transactions", s.handleAddressInternalTxs).Methods("GET")
	api.HandleFunc("/addresses/{address_hash}/logs", s.handleAddressLogs).Methods("GET")
	api.HandleFunc("/addresses/{address_hash}/tokens", s.handleAddressTokens).Methods("GET")
	api.HandleFunc("/addresses/{address_hash}/coin-balance-history", s.handleAddressBalanceHistory).Methods("GET")
	api.HandleFunc("/addresses/{address_hash}/coin-balance-history-by-day", s.handleAddressBalanceByDay).Methods("GET")
	api.HandleFunc("/addresses/{address_hash}/counters", s.handleAddressCounters).Methods("GET")

	// Tokens
	api.HandleFunc("/tokens", s.handleTokens).Methods("GET")
	api.HandleFunc("/tokens/{address_hash}", s.handleToken).Methods("GET")
	api.HandleFunc("/tokens/{address_hash}/transfers", s.handleTokenTransfers).Methods("GET")
	api.HandleFunc("/tokens/{address_hash}/holders", s.handleTokenHolders).Methods("GET")
	api.HandleFunc("/tokens/{address_hash}/instances", s.handleTokenInstances).Methods("GET")
	api.HandleFunc("/tokens/{address_hash}/instances/{token_id}", s.handleTokenInstance).Methods("GET")

	// Smart Contracts
	api.HandleFunc("/smart-contracts", s.handleSmartContracts).Methods("GET")
	api.HandleFunc("/smart-contracts/{address_hash}", s.handleSmartContract).Methods("GET")
	api.HandleFunc("/smart-contracts/{address_hash}/methods-read", s.handleContractReadMethods).Methods("GET")
	api.HandleFunc("/smart-contracts/{address_hash}/methods-write", s.handleContractWriteMethods).Methods("GET")
	api.HandleFunc("/smart-contracts/{address_hash}/query-read-method", s.handleContractQuery).Methods("POST")

	// Search
	api.HandleFunc("/search", s.handleSearch).Methods("GET")
	api.HandleFunc("/search/check-redirect", s.handleSearchRedirect).Methods("GET")

	// Stats
	api.HandleFunc("/stats", s.handleStats).Methods("GET")
	api.HandleFunc("/stats/charts/transactions", s.handleTxChart).Methods("GET")
	api.HandleFunc("/stats/charts/market", s.handleMarketChart).Methods("GET")

	// Main page
	api.HandleFunc("/main-page/blocks", s.handleMainPageBlocks).Methods("GET")
	api.HandleFunc("/main-page/transactions", s.handleMainPageTransactions).Methods("GET")
	api.HandleFunc("/main-page/indexing-status", s.handleIndexingStatus).Methods("GET")

	// WebSocket
	api.HandleFunc("/blocks/subscribe", s.handleBlocksSubscribe)
	api.HandleFunc("/transactions/subscribe", s.handleTransactionsSubscribe)
	api.HandleFunc("/addresses/{address_hash}/subscribe", s.handleAddressSubscribe)

	// Etherscan-compatible RPC API
	s.router.HandleFunc("/api", s.handleRPC).Methods("GET", "POST")

	// GraphQL
	api.HandleFunc("/graphql", s.handleGraphQL).Methods("POST", "GET")
}

// CORS middleware
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Content-Type", "application/json")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Run starts the API server
func (s *Server) Run(ctx context.Context) error {
	go s.wsHub.Run(ctx)

	handler := corsMiddleware(s.router)
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.HTTPPort),
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Printf("[API] Server starting on port %d", s.config.HTTPPort)
	log.Printf("[API] REST API v2: http://localhost:%d/api/v2/", s.config.HTTPPort)
	log.Printf("[API] Etherscan RPC: http://localhost:%d/api", s.config.HTTPPort)
	log.Printf("[API] GraphQL: http://localhost:%d/api/v2/graphql", s.config.HTTPPort)

	return server.ListenAndServe()
}

// Router returns the HTTP router for testing
func (s *Server) Router() *mux.Router {
	return s.router
}

// BroadcastBlock sends a new block to all subscribers
func (s *Server) BroadcastBlock(block *Block) {
	s.wsHub.BroadcastBlock(block)
}

// BroadcastTransaction sends a new transaction to subscribers
func (s *Server) BroadcastTransaction(tx *Transaction) {
	s.wsHub.BroadcastTransaction(tx)
}

// Helper functions

func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, ErrorResponse{Message: message})
}

func getPagination(r *http.Request) (page, pageSize int) {
	page = 0
	pageSize = 50

	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n >= 0 {
			page = n
		}
	}

	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if n, err := strconv.Atoi(ps); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}

	// Also support items_count for Blockscout compatibility
	if ic := r.URL.Query().Get("items_count"); ic != "" {
		if n, err := strconv.Atoi(ic); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}

	return
}

func parseBlockParam(param string) (uint64, bool, error) {
	// Check if it's a hash (0x...)
	if strings.HasPrefix(param, "0x") && len(param) == 66 {
		return 0, true, nil
	}

	// Try to parse as number
	n, err := strconv.ParseUint(param, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("invalid block parameter")
	}
	return n, false, nil
}

// Handler implementations

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "ok",
		"chain_id":   s.config.ChainID,
		"chain_name": s.config.ChainName,
		"version":    "1.0.0",
	})
}

func (s *Server) handleBlocks(w http.ResponseWriter, r *http.Request) {
	page, pageSize := getPagination(r)
	blockType := r.URL.Query().Get("type")

	result, err := s.repo.GetBlocks(r.Context(), page, pageSize, &BlockFilters{Type: blockType})
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request) {
	param := mux.Vars(r)["block_hash_or_number"]

	height, isHash, err := parseBlockParam(param)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var block *Block
	if isHash {
		block, err = s.repo.GetBlockByHash(r.Context(), param)
	} else {
		block, err = s.repo.GetBlockByNumber(r.Context(), height)
	}

	if err == ErrNotFound {
		s.writeError(w, http.StatusNotFound, "block not found")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, block)
}

func (s *Server) handleBlockTransactions(w http.ResponseWriter, r *http.Request) {
	param := mux.Vars(r)["block_hash_or_number"]
	page, pageSize := getPagination(r)

	height, isHash, err := parseBlockParam(param)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if isHash {
		block, err := s.repo.GetBlockByHash(r.Context(), param)
		if err != nil {
			s.writeError(w, http.StatusNotFound, "block not found")
			return
		}
		height = block.Height
	}

	result, err := s.repo.GetTransactionsByBlock(r.Context(), height, page, pageSize)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTransactions(w http.ResponseWriter, r *http.Request) {
	page, pageSize := getPagination(r)

	result, err := s.repo.GetTransactions(r.Context(), page, pageSize, nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTransaction(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["transaction_hash"]

	tx, err := s.repo.GetTransactionByHash(r.Context(), hash)
	if err == ErrNotFound {
		s.writeError(w, http.StatusNotFound, "transaction not found")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, tx)
}

func (s *Server) handleTxTokenTransfers(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["transaction_hash"]
	page, pageSize := getPagination(r)

	// Query token transfers for this transaction
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT t.id, t.tx_hash, t.log_index, t.block_number, t.token_address,
		       t.token_type, t.tx_from, t.tx_to, t.value, t.token_id, t.timestamp,
		       COALESCE(tk.name, ''), COALESCE(tk.symbol, ''), COALESCE(tk.decimals, 18)
		FROM cchain_token_transfers t
		LEFT JOIN cchain_tokens tk ON t.token_address = tk.address
		WHERE t.tx_hash = $1
		ORDER BY t.log_index ASC
		LIMIT $2 OFFSET $3
	`, strings.ToLower(hash), pageSize+1, page*pageSize)

	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
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

		if err := rows.Scan(&id, &txHash, &logIndex, &blockNumber, &tokenAddr, &tokenType,
			&from, &to, &value, &tokenID, &timestamp, &tokenName, &tokenSymbol, &tokenDecimals); err != nil {
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
		}

		transfers = append(transfers, transfer)
	}

	resp := &PaginatedResponse{Items: transfers}
	if len(transfers) > pageSize {
		transfers = transfers[:pageSize]
		resp.Items = transfers
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleTxInternalTxs(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["transaction_hash"]
	page, pageSize := getPagination(r)

	result, err := s.repo.GetInternalTransactions(r.Context(), hash, page, pageSize)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTxLogs(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["transaction_hash"]
	page, pageSize := getPagination(r)

	result, err := s.repo.GetLogs(r.Context(), "", 0, 0, nil, page, pageSize)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Filter by tx hash
	if items, ok := result.Items.([]Log); ok {
		var filtered []Log
		for _, log := range items {
			if strings.EqualFold(log.TxHash, hash) {
				filtered = append(filtered, log)
			}
		}
		result.Items = filtered
	}

	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTxRawTrace(w http.ResponseWriter, r *http.Request) {
	// Return empty trace for now
	s.writeJSON(w, http.StatusOK, map[string]interface{}{})
}

func (s *Server) handleTxStateChanges(w http.ResponseWriter, r *http.Request) {
	// Return empty state changes for now
	s.writeJSON(w, http.StatusOK, &PaginatedResponse{Items: []interface{}{}})
}

func (s *Server) handleAddresses(w http.ResponseWriter, r *http.Request) {
	// List addresses with balances
	page, pageSize := getPagination(r)

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT hash, balance, tx_count, is_contract
		FROM cchain_addresses
		ORDER BY CAST(balance AS NUMERIC) DESC
		LIMIT $1 OFFSET $2
	`, pageSize+1, page*pageSize)

	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var addresses []Address
	for rows.Next() {
		var hash, balance string
		var txCount uint64
		var isContract bool

		if rows.Scan(&hash, &balance, &txCount, &isContract) == nil {
			addresses = append(addresses, Address{
				Hash:             hash,
				Balance:          balance,
				TransactionCount: &txCount,
				IsContract:       isContract,
			})
		}
	}

	resp := &PaginatedResponse{Items: addresses}
	if len(addresses) > pageSize {
		addresses = addresses[:pageSize]
		resp.Items = addresses
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAddress(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["address_hash"]

	addr, err := s.repo.GetAddress(r.Context(), hash)
	if err == ErrNotFound {
		s.writeError(w, http.StatusNotFound, "address not found")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, addr)
}

func (s *Server) handleAddressTransactions(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["address_hash"]
	page, pageSize := getPagination(r)

	result, err := s.repo.GetAddressTransactions(r.Context(), hash, page, pageSize)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAddressTokenTransfers(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["address_hash"]
	page, pageSize := getPagination(r)

	result, err := s.repo.GetAddressTokenTransfers(r.Context(), hash, page, pageSize)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAddressInternalTxs(w http.ResponseWriter, r *http.Request) {
	// Internal transactions for an address
	s.writeJSON(w, http.StatusOK, &PaginatedResponse{Items: []interface{}{}})
}

func (s *Server) handleAddressLogs(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["address_hash"]
	page, pageSize := getPagination(r)

	result, err := s.repo.GetLogs(r.Context(), hash, 0, 0, nil, page, pageSize)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAddressTokens(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["address_hash"]
	hash = strings.ToLower(hash)
	page, pageSize := getPagination(r)

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT tb.token_address, tb.balance, tb.token_id,
		       t.name, t.symbol, t.decimals, t.token_type
		FROM cchain_token_balances tb
		LEFT JOIN cchain_tokens t ON tb.token_address = t.address
		WHERE tb.holder_address = $1 AND CAST(tb.balance AS NUMERIC) > 0
		ORDER BY CAST(tb.balance AS NUMERIC) DESC
		LIMIT $2 OFFSET $3
	`, hash, pageSize+1, page*pageSize)

	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var balances []TokenBalance
	for rows.Next() {
		var tokenAddr, balance string
		var tokenID sql.NullString
		var name, symbol, tokenType sql.NullString
		var decimals sql.NullInt16

		if rows.Scan(&tokenAddr, &balance, &tokenID, &name, &symbol, &decimals, &tokenType) == nil {
			var d uint8 = 18
			if decimals.Valid {
				d = uint8(decimals.Int16)
			}
			tb := TokenBalance{
				Token: &Token{
					Address:  tokenAddr,
					Name:     name.String,
					Symbol:   symbol.String,
					Decimals: &d,
					Type:     tokenType.String,
				},
				Value: balance,
			}
			if tokenID.Valid {
				tb.TokenID = tokenID.String
			}
			balances = append(balances, tb)
		}
	}

	resp := &PaginatedResponse{Items: balances}
	if len(balances) > pageSize {
		balances = balances[:pageSize]
		resp.Items = balances
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAddressBalanceHistory(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["address_hash"]
	page, pageSize := getPagination(r)

	result, err := s.repo.GetAddressCoinBalanceHistory(r.Context(), hash, page, pageSize)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAddressBalanceByDay(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["address_hash"]
	page, pageSize := getPagination(r)

	result, err := s.repo.GetAddressCoinBalanceByDay(r.Context(), hash, page, pageSize)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAddressCounters(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["address_hash"]
	hash = strings.ToLower(hash)

	var txCount, tokenTransferCount int64
	s.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM cchain_transactions WHERE tx_from = $1 OR tx_to = $1
	`, hash).Scan(&txCount)

	s.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM cchain_token_transfers WHERE tx_from = $1 OR tx_to = $1
	`, hash).Scan(&tokenTransferCount)

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"transactions_count":    txCount,
		"token_transfers_count": tokenTransferCount,
		"validations_count":     0,
		"gas_usage_count":       0,
	})
}

func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	page, pageSize := getPagination(r)
	tokenType := r.URL.Query().Get("type")

	result, err := s.repo.GetTokens(r.Context(), page, pageSize, tokenType)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["address_hash"]

	token, err := s.repo.GetToken(r.Context(), hash)
	if err == ErrNotFound {
		s.writeError(w, http.StatusNotFound, "token not found")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, token)
}

func (s *Server) handleTokenTransfers(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["address_hash"]
	hash = strings.ToLower(hash)
	page, pageSize := getPagination(r)

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, tx_hash, log_index, block_number, tx_from, tx_to, value, token_id, timestamp
		FROM cchain_token_transfers
		WHERE token_address = $1
		ORDER BY block_number DESC, log_index DESC
		LIMIT $2 OFFSET $3
	`, hash, pageSize+1, page*pageSize)

	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var transfers []TokenTransfer
	for rows.Next() {
		var id, txHash, from, to, value string
		var tokenID sql.NullString
		var blockNumber uint64
		var logIndex int
		var timestamp time.Time

		if rows.Scan(&id, &txHash, &logIndex, &blockNumber, &from, &to, &value, &tokenID, &timestamp) == nil {
			transfer := TokenTransfer{
				TxHash:      txHash,
				BlockNumber: blockNumber,
				LogIndex:    logIndex,
				Timestamp:   &timestamp,
				From:        &Address{Hash: from},
				To:          &Address{Hash: to},
				Total:       &TokenTotal{Value: value},
			}
			if tokenID.Valid {
				transfer.TokenID = tokenID.String
			}
			transfers = append(transfers, transfer)
		}
	}

	resp := &PaginatedResponse{Items: transfers}
	if len(transfers) > pageSize {
		transfers = transfers[:pageSize]
		resp.Items = transfers
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleTokenHolders(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["address_hash"]
	hash = strings.ToLower(hash)
	page, pageSize := getPagination(r)

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT holder_address, balance
		FROM cchain_token_balances
		WHERE token_address = $1 AND CAST(balance AS NUMERIC) > 0
		ORDER BY CAST(balance AS NUMERIC) DESC
		LIMIT $2 OFFSET $3
	`, hash, pageSize+1, page*pageSize)

	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type Holder struct {
		Address *Address `json:"address"`
		Value   string   `json:"value"`
	}

	var holders []Holder
	for rows.Next() {
		var address, balance string
		if rows.Scan(&address, &balance) == nil {
			holders = append(holders, Holder{
				Address: &Address{Hash: address},
				Value:   balance,
			})
		}
	}

	resp := &PaginatedResponse{Items: holders}
	if len(holders) > pageSize {
		holders = holders[:pageSize]
		resp.Items = holders
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleTokenInstances(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, &PaginatedResponse{Items: []interface{}{}})
}

func (s *Server) handleTokenInstance(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]interface{}{})
}

func (s *Server) handleSmartContracts(w http.ResponseWriter, r *http.Request) {
	page, pageSize := getPagination(r)

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT hash, tx_count FROM cchain_addresses
		WHERE is_contract = TRUE
		ORDER BY tx_count DESC
		LIMIT $1 OFFSET $2
	`, pageSize+1, page*pageSize)

	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var contracts []SmartContract
	for rows.Next() {
		var address string
		var txCount int64
		if rows.Scan(&address, &txCount) == nil {
			contracts = append(contracts, SmartContract{
				Address: address,
			})
		}
	}

	resp := &PaginatedResponse{Items: contracts}
	if len(contracts) > pageSize {
		contracts = contracts[:pageSize]
		resp.Items = contracts
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSmartContract(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["address_hash"]

	contract, err := s.repo.GetSmartContract(r.Context(), hash)
	if err == ErrNotFound {
		s.writeError(w, http.StatusNotFound, "contract not found")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, contract)
}

func (s *Server) handleContractReadMethods(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, []interface{}{})
}

func (s *Server) handleContractWriteMethods(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, []interface{}{})
}

func (s *Server) handleContractQuery(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]interface{}{"result": nil})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		s.writeJSON(w, http.StatusOK, &PaginatedResponse{Items: []interface{}{}})
		return
	}

	results, err := s.repo.Search(r.Context(), query, 50)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, &PaginatedResponse{Items: results})
}

func (s *Server) handleSearchRedirect(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		s.writeJSON(w, http.StatusOK, map[string]interface{}{"redirect": false})
		return
	}

	results, _ := s.repo.Search(r.Context(), query, 1)
	if len(results) == 1 {
		result := results[0]
		var redirectURL string
		switch result.Type {
		case "block":
			redirectURL = fmt.Sprintf("/block/%d", *result.BlockNumber)
		case "transaction":
			redirectURL = fmt.Sprintf("/tx/%s", result.Hash)
		case "address", "contract":
			redirectURL = fmt.Sprintf("/address/%s", result.Address)
		case "token":
			redirectURL = fmt.Sprintf("/token/%s", result.Address)
		}
		if redirectURL != "" {
			s.writeJSON(w, http.StatusOK, map[string]interface{}{
				"redirect":    true,
				"type":        result.Type,
				"redirect_to": redirectURL,
			})
			return
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{"redirect": false})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.repo.GetStats(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleTxChart(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"chart_data":       []interface{}{},
		"available_supply": "0",
	})
}

func (s *Server) handleMarketChart(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"chart_data":       []interface{}{},
		"available_supply": "0",
	})
}

func (s *Server) handleMainPageBlocks(w http.ResponseWriter, r *http.Request) {
	result, err := s.repo.GetBlocks(r.Context(), 0, 6, nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result.Items)
}

func (s *Server) handleMainPageTransactions(w http.ResponseWriter, r *http.Request) {
	result, err := s.repo.GetTransactions(r.Context(), 0, 6, nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result.Items)
}

func (s *Server) handleIndexingStatus(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"finished_indexing":                   true,
		"finished_indexing_blocks":            true,
		"indexed_blocks_ratio":                "1.00",
		"indexed_internal_transactions_ratio": "1.00",
	})
}

// WebSocket handlers

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *Server) handleBlocksSubscribe(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	s.wsHub.RegisterBlockClient(conn)
}

func (s *Server) handleTransactionsSubscribe(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	s.wsHub.RegisterTransactionClient(conn)
}

func (s *Server) handleAddressSubscribe(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	address := strings.ToLower(mux.Vars(r)["address_hash"])
	s.wsHub.RegisterAddressClient(conn, address)
}

// RPC handler (Etherscan compatible)
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	s.rpcHandler.Handle(w, r)
}

// GraphQL handler
func (s *Server) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	// For now, return a simple response
	// Full GraphQL implementation would use gqlgen
	if r.Method == "GET" {
		// Return GraphQL playground
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
  <title>GraphQL Playground</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/graphql-playground-react/build/static/css/index.css" />
  <script src="https://cdn.jsdelivr.net/npm/graphql-playground-react/build/static/js/middleware.js"></script>
</head>
<body>
  <div id="root"></div>
  <script>
    window.addEventListener('load', function() {
      GraphQLPlayground.init(document.getElementById('root'), { endpoint: '/api/v2/graphql' })
    })
  </script>
</body>
</html>`))
		return
	}

	// Parse GraphQL query
	var req struct {
		Query     string                 `json:"query"`
		Variables map[string]interface{} `json:"variables"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Simple query handling - full implementation would use gqlgen
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{
			"__typename": "Query",
		},
	})
}
