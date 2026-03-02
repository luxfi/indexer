package explorer

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Paginated response for explorer API v2.
type paginatedResponse struct {
	Items          any `json:"items"`
	NextPageParams any `json:"next_page_params"`
}

func emptyPage() paginatedResponse {
	return paginatedResponse{Items: []any{}, NextPageParams: nil}
}

// ---- Blocks ----

func (s *Service) handleListBlocks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	q := r.URL.Query()
	limit := intParam(q.Get("items_count"), 50)

	query := "SELECT * FROM blocks WHERE chain_id = ? ORDER BY number DESC LIMIT ?"
	args := []any{s.config.ChainID, limit + 1}

	if bn := q.Get("block_number"); bn != "" {
		query = "SELECT * FROM blocks WHERE chain_id = ? AND number < ? ORDER BY number DESC LIMIT ?"
		args = []any{s.config.ChainID, bn, limit + 1}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()

	blocks, err := scanMaps(rows)
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}

	var nextPage any
	if len(blocks) > limit {
		last := blocks[limit-1]
		nextPage = map[string]any{
			"block_number": last["number"],
			"items_count":  limit,
		}
		blocks = blocks[:limit]
	}

	items := make([]map[string]any, len(blocks))
	for i, b := range blocks {
		items[i] = formatBlock(b)
	}

	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nextPage})
}

func (s *Service) handleGetBlock(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	id := chi.URLParam(r, "block_hash_or_number")

	var query string
	var args []any
	if strings.HasPrefix(id, "0x") {
		query = "SELECT * FROM blocks WHERE chain_id = ? AND hash = ? LIMIT 1"
		args = []any{s.config.ChainID, hexToBytes(id)}
	} else {
		query = "SELECT * FROM blocks WHERE chain_id = ? AND number = ? LIMIT 1"
		args = []any{s.config.ChainID, id}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		notFoundError(w, "block not found")
		return
	}
	defer rows.Close()

	blocks, err := scanMaps(rows)
	if err != nil || len(blocks) == 0 {
		notFoundError(w, "block not found")
		return
	}

	writeJSON(w, http.StatusOK, formatBlock(blocks[0]))
}

func (s *Service) handleBlockTransactions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	id := chi.URLParam(r, "block_hash_or_number")
	limit := intParam(r.URL.Query().Get("items_count"), 50)

	var query string
	var args []any
	if strings.HasPrefix(id, "0x") {
		query = "SELECT * FROM transactions WHERE chain_id = ? AND block_hash = ? ORDER BY tx_index LIMIT ?"
		args = []any{s.config.ChainID, hexToBytes(id), limit}
	} else {
		query = "SELECT * FROM transactions WHERE chain_id = ? AND block_number = ? ORDER BY tx_index LIMIT ?"
		args = []any{s.config.ChainID, id, limit}
	}

	s.queryTxListCtx(w, r, ctx, query, args, limit)
}

// ---- Transactions ----

func (s *Service) handleListTransactions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	q := r.URL.Query()
	limit := intParam(q.Get("items_count"), 50)

	query := "SELECT * FROM transactions WHERE chain_id = ? ORDER BY block_number DESC, tx_index DESC LIMIT ?"
	args := []any{s.config.ChainID, limit + 1}

	s.queryTxListCtx(w, r, ctx, query, args, limit)
}

func (s *Service) handleGetTransaction(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	hash := chi.URLParam(r, "transaction_hash")

	rows, err := s.db.QueryContext(ctx, "SELECT * FROM transactions WHERE chain_id = ? AND hash = ? LIMIT 1",
		s.config.ChainID, hexToBytes(hash))
	if err != nil {
		notFoundError(w, "transaction not found")
		return
	}
	defer rows.Close()

	txs, err := scanMaps(rows)
	if err != nil || len(txs) == 0 {
		notFoundError(w, "transaction not found")
		return
	}

	writeJSON(w, http.StatusOK, formatTx(txs[0]))
}

func (s *Service) handleTxTokenTransfers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	hash := chi.URLParam(r, "transaction_hash")
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM token_transfers WHERE chain_id = ? AND transaction_hash = ? ORDER BY log_index",
		s.config.ChainID, hexToBytes(hash))
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()

	transfers, _ := scanMaps(rows)
	items := make([]map[string]any, len(transfers))
	for i, t := range transfers {
		items[i] = formatTokenTransfer(t)
	}
	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (s *Service) handleTxInternalTxs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	hash := chi.URLParam(r, "transaction_hash")
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM internal_transactions WHERE chain_id = ? AND transaction_hash = ? ORDER BY \"index\"",
		s.config.ChainID, hexToBytes(hash))
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()

	itxs, _ := scanMaps(rows)
	items := make([]map[string]any, len(itxs))
	for i, t := range itxs {
		items[i] = formatInternalTx(t)
	}
	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (s *Service) handleTxLogs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	hash := chi.URLParam(r, "transaction_hash")
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM logs WHERE chain_id = ? AND transaction_hash = ? ORDER BY \"index\"",
		s.config.ChainID, hexToBytes(hash))
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()

	logs, _ := scanMaps(rows)
	items := make([]map[string]any, len(logs))
	for i, l := range logs {
		items[i] = formatLog(l)
	}
	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (s *Service) handleTxRawTrace(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

// ---- Addresses ----

func (s *Service) handleListAddresses(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	limit := intParam(r.URL.Query().Get("items_count"), 50)
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM addresses WHERE chain_id = ? ORDER BY fetched_coin_balance DESC LIMIT ?",
		s.config.ChainID, limit)
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()

	addrs, _ := scanMaps(rows)
	items := make([]map[string]any, len(addrs))
	for i, a := range addrs {
		items[i] = formatAddress(a)
	}
	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (s *Service) handleGetAddress(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	hash := chi.URLParam(r, "address_hash")
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM addresses WHERE chain_id = ? AND hash = ? LIMIT 1",
		s.config.ChainID, hexToBytes(hash))
	if err != nil {
		notFoundError(w, "address not found")
		return
	}
	defer rows.Close()

	addrs, _ := scanMaps(rows)
	if len(addrs) == 0 {
		notFoundError(w, "address not found")
		return
	}
	writeJSON(w, http.StatusOK, formatAddress(addrs[0]))
}

func (s *Service) handleAddressTransactions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	hash := chi.URLParam(r, "address_hash")
	limit := intParam(r.URL.Query().Get("items_count"), 50)
	addr := hexToBytes(hash)

	rows, err := s.db.QueryContext(ctx,
		"SELECT * FROM transactions WHERE chain_id = ? AND (from_addr = ? OR to_addr = ?) ORDER BY block_number DESC, tx_index DESC LIMIT ?",
		s.config.ChainID, addr, addr, limit+1)
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()

	s.formatTxRows(w, rows, limit)
}

func (s *Service) handleAddressTokenTransfers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	hash := chi.URLParam(r, "address_hash")
	addr := hexToBytes(hash)

	rows, err := s.db.QueryContext(ctx,
		"SELECT * FROM token_transfers WHERE chain_id = ? AND (from_addr = ? OR to_addr = ?) ORDER BY block_number DESC, log_index DESC LIMIT 50",
		s.config.ChainID, addr, addr)
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()

	transfers, _ := scanMaps(rows)
	items := make([]map[string]any, len(transfers))
	for i, t := range transfers {
		items[i] = formatTokenTransfer(t)
	}
	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (s *Service) handleAddressInternalTxs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	hash := chi.URLParam(r, "address_hash")
	addr := hexToBytes(hash)

	rows, err := s.db.QueryContext(ctx,
		"SELECT * FROM internal_transactions WHERE chain_id = ? AND (from_addr = ? OR to_addr = ?) ORDER BY block_number DESC LIMIT 50",
		s.config.ChainID, addr, addr)
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()

	itxs, _ := scanMaps(rows)
	items := make([]map[string]any, len(itxs))
	for i, t := range itxs {
		items[i] = formatInternalTx(t)
	}
	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (s *Service) handleAddressLogs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	hash := chi.URLParam(r, "address_hash")
	rows, err := s.db.QueryContext(ctx,
		"SELECT * FROM logs WHERE chain_id = ? AND address_hash = ? ORDER BY block_number DESC LIMIT 50",
		s.config.ChainID, hexToBytes(hash))
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()

	logs, _ := scanMaps(rows)
	items := make([]map[string]any, len(logs))
	for i, l := range logs {
		items[i] = formatLog(l)
	}
	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (s *Service) handleAddressTokens(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	hash := chi.URLParam(r, "address_hash")
	rows, err := s.db.QueryContext(ctx,
		"SELECT * FROM address_current_token_balances WHERE chain_id = ? AND address_hash = ? ORDER BY value DESC LIMIT 100",
		s.config.ChainID, hexToBytes(hash))
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()

	bals, _ := scanMaps(rows)
	items := make([]map[string]any, len(bals))
	for i, b := range bals {
		items[i] = map[string]any{
			"token": map[string]any{
				"address": bytesToHex(b["token_contract_addr"]),
				"type":    b["token_type"],
			},
			"value":    fmt.Sprintf("%v", b["value"]),
			"token_id": b["token_id"],
		}
	}
	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (s *Service) handleAddressCoinBalanceHistory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	hash := chi.URLParam(r, "address_hash")
	rows, err := s.db.QueryContext(ctx,
		"SELECT * FROM address_coin_balances WHERE chain_id = ? AND address_hash = ? ORDER BY block_number DESC LIMIT 50",
		s.config.ChainID, hexToBytes(hash))
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()

	bals, _ := scanMaps(rows)
	items := make([]map[string]any, len(bals))
	for i, b := range bals {
		items[i] = map[string]any{
			"block_number": b["block_number"],
			"value":        fmt.Sprintf("%v", b["value"]),
			"delta":        fmt.Sprintf("%v", b["delta"]),
		}
	}
	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (s *Service) handleAddressCoinBalanceByDay(w http.ResponseWriter, r *http.Request) {
	s.handleAddressCoinBalanceHistory(w, r)
}

func (s *Service) handleAddressCounters(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	hash := chi.URLParam(r, "address_hash")
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM addresses WHERE chain_id = ? AND hash = ? LIMIT 1",
		s.config.ChainID, hexToBytes(hash))
	if err != nil {
		notFoundError(w, "address not found")
		return
	}
	defer rows.Close()

	addrs, _ := scanMaps(rows)
	if len(addrs) == 0 {
		notFoundError(w, "address not found")
		return
	}

	a := addrs[0]
	writeJSON(w, http.StatusOK, map[string]any{
		"transactions_count":    a["transactions_count"],
		"token_transfers_count": a["token_transfers_count"],
		"gas_usage_count":       fmt.Sprintf("%v", a["gas_used"]),
		"validations_count":     0,
	})
}

// ---- Tokens ----

func (s *Service) handleListTokens(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	limit := intParam(r.URL.Query().Get("items_count"), 50)
	q := r.URL.Query()

	query := "SELECT * FROM tokens WHERE chain_id = ?"
	args := []any{s.config.ChainID}

	if t := q.Get("type"); t != "" {
		query += " AND type = ?"
		args = append(args, t)
	}
	query += " ORDER BY holder_count DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()

	tokens, _ := scanMaps(rows)
	items := make([]map[string]any, len(tokens))
	for i, t := range tokens {
		items[i] = formatToken(t)
	}
	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (s *Service) handleGetToken(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	addr := chi.URLParam(r, "address_hash")
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM tokens WHERE chain_id = ? AND contract_addr = ? LIMIT 1",
		s.config.ChainID, hexToBytes(addr))
	if err != nil {
		notFoundError(w, "token not found")
		return
	}
	defer rows.Close()

	tokens, _ := scanMaps(rows)
	if len(tokens) == 0 {
		notFoundError(w, "token not found")
		return
	}
	writeJSON(w, http.StatusOK, formatToken(tokens[0]))
}

func (s *Service) handleTokenTransfers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	addr := chi.URLParam(r, "address_hash")
	rows, err := s.db.QueryContext(ctx,
		"SELECT * FROM token_transfers WHERE chain_id = ? AND token_contract_addr = ? ORDER BY block_number DESC LIMIT 50",
		s.config.ChainID, hexToBytes(addr))
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()

	transfers, _ := scanMaps(rows)
	items := make([]map[string]any, len(transfers))
	for i, t := range transfers {
		items[i] = formatTokenTransfer(t)
	}
	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (s *Service) handleTokenHolders(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	addr := chi.URLParam(r, "address_hash")
	rows, err := s.db.QueryContext(ctx,
		"SELECT * FROM address_current_token_balances WHERE chain_id = ? AND token_contract_addr = ? ORDER BY value DESC LIMIT 50",
		s.config.ChainID, hexToBytes(addr))
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()

	bals, _ := scanMaps(rows)
	items := make([]map[string]any, len(bals))
	for i, b := range bals {
		items[i] = map[string]any{
			"address": map[string]any{"hash": bytesToHex(b["address_hash"])},
			"value":   fmt.Sprintf("%v", b["value"]),
		}
	}
	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (s *Service) handleTokenInstances(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, emptyPage())
}

func (s *Service) handleTokenInstance(w http.ResponseWriter, r *http.Request) {
	notFoundError(w, "not found")
}

// ---- Smart Contracts ----

func (s *Service) handleListContracts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(ctx, "SELECT * FROM smart_contracts WHERE chain_id = ? ORDER BY inserted_at DESC LIMIT 50",
		s.config.ChainID)
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()

	contracts, _ := scanMaps(rows)
	items := make([]map[string]any, len(contracts))
	for i, c := range contracts {
		items[i] = formatContract(c)
	}
	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (s *Service) handleGetContract(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	addr := chi.URLParam(r, "address_hash")
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM smart_contracts WHERE chain_id = ? AND address_hash = ? LIMIT 1",
		s.config.ChainID, hexToBytes(addr))
	if err != nil {
		notFoundError(w, "contract not found")
		return
	}
	defer rows.Close()

	contracts, _ := scanMaps(rows)
	if len(contracts) == 0 {
		notFoundError(w, "contract not found")
		return
	}
	writeJSON(w, http.StatusOK, formatContract(contracts[0]))
}

func (s *Service) handleVerifyContract(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "queued"})
}

// ---- Search ----

func (s *Service) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}

	var items []map[string]any

	// Transaction hash
	if strings.HasPrefix(q, "0x") && len(q) == 66 {
		if row := s.queryOneCtx(ctx, "transactions", "hash", hexToBytes(q)); row != nil {
			items = append(items, map[string]any{
				"type":             "transaction",
				"transaction_hash": bytesToHex(row["hash"]),
			})
		}
	}

	// Address
	if strings.HasPrefix(q, "0x") && len(q) == 42 {
		items = append(items, map[string]any{
			"type":         "address",
			"address_hash": strings.ToLower(q),
		})
	}

	// Block number
	if n, err := strconv.ParseInt(q, 10, 64); err == nil {
		var count int
		s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM blocks WHERE chain_id = ? AND number = ?", s.config.ChainID, n).Scan(&count)
		if count > 0 {
			items = append(items, map[string]any{
				"type":         "block",
				"block_number": n,
			})
		}
	}

	// Token name/symbol search
	if !strings.HasPrefix(q, "0x") {
		escaped := strings.NewReplacer("%", "\\%", "_", "\\_").Replace(q)
		rows, err := s.db.QueryContext(ctx,
			"SELECT * FROM tokens WHERE chain_id = ? AND (name LIKE ? ESCAPE '\\' OR symbol LIKE ? ESCAPE '\\') ORDER BY holder_count DESC LIMIT 5",
			s.config.ChainID, "%"+escaped+"%", "%"+escaped+"%")
		if err == nil {
			defer rows.Close()
			tokens, _ := scanMaps(rows)
			for _, t := range tokens {
				items = append(items, map[string]any{
					"type":         "token",
					"name":         t["name"],
					"symbol":       t["symbol"],
					"address_hash": bytesToHex(t["contract_addr"]),
					"token_type":   t["type"],
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (s *Service) handleSearchRedirect(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if strings.HasPrefix(q, "0x") && len(q) == 66 {
		writeJSON(w, http.StatusOK, map[string]any{"redirect": true, "type": "transaction", "parameter": q})
		return
	}
	if strings.HasPrefix(q, "0x") && len(q) == 42 {
		writeJSON(w, http.StatusOK, map[string]any{"redirect": true, "type": "address", "parameter": q})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"redirect": false})
}

// ---- Stats ----

func (s *Service) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var blockCount, txCount, addrCount int
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM blocks WHERE chain_id = ?", s.config.ChainID).Scan(&blockCount)
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM transactions WHERE chain_id = ?", s.config.ChainID).Scan(&txCount)
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM addresses WHERE chain_id = ?", s.config.ChainID).Scan(&addrCount)

	writeJSON(w, http.StatusOK, map[string]any{
		"total_blocks":                   blockCount,
		"total_transactions":             txCount,
		"total_addresses":                addrCount,
		"coin_price":                     nil,
		"coin_price_change_percentage":   nil,
		"total_gas_used":                 "0",
		"average_block_time":             0,
		"market_cap":                     "0",
		"network_utilization_percentage": 0,
		"coin_image":                     nil,
	})
}

func (s *Service) handleChartTransactions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"chart_data": []any{}})
}

func (s *Service) handleChartMarket(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"chart_data": []any{}})
}

func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	writeJSON(w, http.StatusOK, map[string]any{
		"healthy":    s.db.PingContext(ctx) == nil,
		"chain_id":   s.config.ChainID,
		"chain_name": s.config.ChainName,
	})
}

// ---- Query Helpers ----

func (s *Service) queryOneCtx(ctx context.Context, table, col string, val any) map[string]any {
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf("SELECT * FROM %s WHERE chain_id = ? AND %s = ? LIMIT 1", table, col),
		s.config.ChainID, val)
	if err != nil {
		return nil
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	if len(maps) == 0 {
		return nil
	}
	return maps[0]
}

func (s *Service) queryTxListCtx(w http.ResponseWriter, r *http.Request, ctx context.Context, query string, args []any, limit int) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		writeJSON(w, http.StatusOK, emptyPage())
		return
	}
	defer rows.Close()
	s.formatTxRows(w, rows, limit)
}

func (s *Service) formatTxRows(w http.ResponseWriter, rows *sql.Rows, limit int) {
	txs, _ := scanMaps(rows)

	var nextPage any
	if len(txs) > limit {
		last := txs[limit-1]
		nextPage = map[string]any{
			"block_number": last["block_number"],
			"index":        last["tx_index"],
			"items_count":  limit,
		}
		txs = txs[:limit]
	}

	items := make([]map[string]any, len(txs))
	for i, t := range txs {
		items[i] = formatTx(t)
	}

	writeJSON(w, http.StatusOK, paginatedResponse{Items: items, NextPageParams: nextPage})
}

func intParam(s string, fallback int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		if n > 250 {
			n = 250
		}
		return n
	}
	return fallback
}
