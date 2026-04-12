package explorer

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hanzoai/base/core"
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

func (p *plugin) handleListBlocks(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	q := e.Request.URL.Query()
	limit := intParam(q.Get("items_count"), 50)

	query := "SELECT * FROM blocks WHERE chain_id = ? ORDER BY number DESC LIMIT ?"
	args := []any{p.config.ChainID, limit + 1}

	if bn := q.Get("block_number"); bn != "" {
		query = "SELECT * FROM blocks WHERE chain_id = ? AND number < ? ORDER BY number DESC LIMIT ?"
		args = []any{p.config.ChainID, bn, limit + 1}
	}

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
	}
	defer rows.Close()

	blocks, err := scanMaps(rows)
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
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

	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nextPage})
}

func (p *plugin) handleGetBlock(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	id := e.Request.PathValue("block_hash_or_number")

	var query string
	var args []any
	if strings.HasPrefix(id, "0x") {
		query = "SELECT * FROM blocks WHERE chain_id = ? AND hash = ? LIMIT 1"
		args = []any{p.config.ChainID, hexToBytes(id)}
	} else {
		query = "SELECT * FROM blocks WHERE chain_id = ? AND number = ? LIMIT 1"
		args = []any{p.config.ChainID, id}
	}

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return e.NotFoundError("block not found", nil)
	}
	defer rows.Close()

	blocks, err := scanMaps(rows)
	if err != nil || len(blocks) == 0 {
		return e.NotFoundError("block not found", nil)
	}

	return e.JSON(http.StatusOK, formatBlock(blocks[0]))
}

func (p *plugin) handleBlockTransactions(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	id := e.Request.PathValue("block_hash_or_number")
	limit := intParam(e.Request.URL.Query().Get("items_count"), 50)

	var query string
	var args []any
	if strings.HasPrefix(id, "0x") {
		query = "SELECT * FROM transactions WHERE chain_id = ? AND block_hash = ? ORDER BY tx_index LIMIT ?"
		args = []any{p.config.ChainID, hexToBytes(id), limit}
	} else {
		query = "SELECT * FROM transactions WHERE chain_id = ? AND block_number = ? ORDER BY tx_index LIMIT ?"
		args = []any{p.config.ChainID, id, limit}
	}

	return p.queryTxListCtx(e, ctx, query, args, limit)
}

// ---- Transactions ----

func (p *plugin) handleListTransactions(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	q := e.Request.URL.Query()
	limit := intParam(q.Get("items_count"), 50)

	query := "SELECT * FROM transactions WHERE chain_id = ? ORDER BY block_number DESC, tx_index DESC LIMIT ?"
	args := []any{p.config.ChainID, limit + 1}

	return p.queryTxListCtx(e, ctx, query, args, limit)
}

func (p *plugin) handleGetTransaction(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	hash := e.Request.PathValue("transaction_hash")

	rows, err := p.db.QueryContext(ctx, "SELECT * FROM transactions WHERE chain_id = ? AND hash = ? LIMIT 1",
		p.config.ChainID, hexToBytes(hash))
	if err != nil {
		return e.NotFoundError("transaction not found", nil)
	}
	defer rows.Close()

	txs, err := scanMaps(rows)
	if err != nil || len(txs) == 0 {
		return e.NotFoundError("transaction not found", nil)
	}

	return e.JSON(http.StatusOK, formatTx(txs[0]))
}

func (p *plugin) handleTxTokenTransfers(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	hash := e.Request.PathValue("transaction_hash")
	rows, err := p.db.QueryContext(ctx, "SELECT * FROM token_transfers WHERE chain_id = ? AND transaction_hash = ? ORDER BY log_index",
		p.config.ChainID, hexToBytes(hash))
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
	}
	defer rows.Close()

	transfers, _ := scanMaps(rows)
	items := make([]map[string]any, len(transfers))
	for i, t := range transfers {
		items[i] = formatTokenTransfer(t)
	}
	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (p *plugin) handleTxInternalTxs(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	hash := e.Request.PathValue("transaction_hash")
	rows, err := p.db.QueryContext(ctx, "SELECT * FROM internal_transactions WHERE chain_id = ? AND transaction_hash = ? ORDER BY \"index\"",
		p.config.ChainID, hexToBytes(hash))
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
	}
	defer rows.Close()

	itxs, _ := scanMaps(rows)
	items := make([]map[string]any, len(itxs))
	for i, t := range itxs {
		items[i] = formatInternalTx(t)
	}
	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (p *plugin) handleTxLogs(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	hash := e.Request.PathValue("transaction_hash")
	rows, err := p.db.QueryContext(ctx, "SELECT * FROM logs WHERE chain_id = ? AND transaction_hash = ? ORDER BY \"index\"",
		p.config.ChainID, hexToBytes(hash))
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
	}
	defer rows.Close()

	logs, _ := scanMaps(rows)
	items := make([]map[string]any, len(logs))
	for i, l := range logs {
		items[i] = formatLog(l)
	}
	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (p *plugin) handleTxRawTrace(e *core.RequestEvent) error {
	return e.JSON(http.StatusOK, []any{})
}

// ---- Addresses ----

func (p *plugin) handleListAddresses(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	limit := intParam(e.Request.URL.Query().Get("items_count"), 50)
	rows, err := p.db.QueryContext(ctx, "SELECT * FROM addresses WHERE chain_id = ? ORDER BY fetched_coin_balance DESC LIMIT ?",
		p.config.ChainID, limit)
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
	}
	defer rows.Close()

	addrs, _ := scanMaps(rows)
	items := make([]map[string]any, len(addrs))
	for i, a := range addrs {
		items[i] = formatAddress(a)
	}
	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (p *plugin) handleGetAddress(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	hash := e.Request.PathValue("address_hash")
	rows, err := p.db.QueryContext(ctx, "SELECT * FROM addresses WHERE chain_id = ? AND hash = ? LIMIT 1",
		p.config.ChainID, hexToBytes(hash))
	if err != nil {
		return e.NotFoundError("address not found", nil)
	}
	defer rows.Close()

	addrs, _ := scanMaps(rows)
	if len(addrs) == 0 {
		return e.NotFoundError("address not found", nil)
	}
	return e.JSON(http.StatusOK, formatAddress(addrs[0]))
}

func (p *plugin) handleAddressTransactions(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	hash := e.Request.PathValue("address_hash")
	limit := intParam(e.Request.URL.Query().Get("items_count"), 50)
	addr := hexToBytes(hash)

	rows, err := p.db.QueryContext(ctx,
		"SELECT * FROM transactions WHERE chain_id = ? AND (from_addr = ? OR to_addr = ?) ORDER BY block_number DESC, tx_index DESC LIMIT ?",
		p.config.ChainID, addr, addr, limit+1)
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
	}
	defer rows.Close()

	return p.formatTxRows(e, rows, limit)
}

func (p *plugin) handleAddressTokenTransfers(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	hash := e.Request.PathValue("address_hash")
	addr := hexToBytes(hash)

	rows, err := p.db.QueryContext(ctx,
		"SELECT * FROM token_transfers WHERE chain_id = ? AND (from_addr = ? OR to_addr = ?) ORDER BY block_number DESC, log_index DESC LIMIT 50",
		p.config.ChainID, addr, addr)
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
	}
	defer rows.Close()

	transfers, _ := scanMaps(rows)
	items := make([]map[string]any, len(transfers))
	for i, t := range transfers {
		items[i] = formatTokenTransfer(t)
	}
	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (p *plugin) handleAddressInternalTxs(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	hash := e.Request.PathValue("address_hash")
	addr := hexToBytes(hash)

	rows, err := p.db.QueryContext(ctx,
		"SELECT * FROM internal_transactions WHERE chain_id = ? AND (from_addr = ? OR to_addr = ?) ORDER BY block_number DESC LIMIT 50",
		p.config.ChainID, addr, addr)
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
	}
	defer rows.Close()

	itxs, _ := scanMaps(rows)
	items := make([]map[string]any, len(itxs))
	for i, t := range itxs {
		items[i] = formatInternalTx(t)
	}
	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (p *plugin) handleAddressLogs(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	hash := e.Request.PathValue("address_hash")
	rows, err := p.db.QueryContext(ctx,
		"SELECT * FROM logs WHERE chain_id = ? AND address_hash = ? ORDER BY block_number DESC LIMIT 50",
		p.config.ChainID, hexToBytes(hash))
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
	}
	defer rows.Close()

	logs, _ := scanMaps(rows)
	items := make([]map[string]any, len(logs))
	for i, l := range logs {
		items[i] = formatLog(l)
	}
	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (p *plugin) handleAddressTokens(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	hash := e.Request.PathValue("address_hash")
	rows, err := p.db.QueryContext(ctx,
		"SELECT * FROM address_current_token_balances WHERE chain_id = ? AND address_hash = ? ORDER BY value DESC LIMIT 100",
		p.config.ChainID, hexToBytes(hash))
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
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
	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (p *plugin) handleAddressCoinBalanceHistory(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	hash := e.Request.PathValue("address_hash")
	rows, err := p.db.QueryContext(ctx,
		"SELECT * FROM address_coin_balances WHERE chain_id = ? AND address_hash = ? ORDER BY block_number DESC LIMIT 50",
		p.config.ChainID, hexToBytes(hash))
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
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
	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (p *plugin) handleAddressCoinBalanceByDay(e *core.RequestEvent) error {
	return p.handleAddressCoinBalanceHistory(e)
}

func (p *plugin) handleAddressCounters(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	hash := e.Request.PathValue("address_hash")
	rows, err := p.db.QueryContext(ctx, "SELECT * FROM addresses WHERE chain_id = ? AND hash = ? LIMIT 1",
		p.config.ChainID, hexToBytes(hash))
	if err != nil {
		return e.NotFoundError("address not found", nil)
	}
	defer rows.Close()

	addrs, _ := scanMaps(rows)
	if len(addrs) == 0 {
		return e.NotFoundError("address not found", nil)
	}

	a := addrs[0]
	return e.JSON(http.StatusOK, map[string]any{
		"transactions_count":    a["transactions_count"],
		"token_transfers_count": a["token_transfers_count"],
		"gas_usage_count":       fmt.Sprintf("%v", a["gas_used"]),
		"validations_count":     0,
	})
}

// ---- Tokens ----

func (p *plugin) handleListTokens(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	limit := intParam(e.Request.URL.Query().Get("items_count"), 50)
	q := e.Request.URL.Query()

	query := "SELECT * FROM tokens WHERE chain_id = ?"
	args := []any{p.config.ChainID}

	if t := q.Get("type"); t != "" {
		query += " AND type = ?"
		args = append(args, t)
	}
	query += " ORDER BY holder_count DESC LIMIT ?"
	args = append(args, limit)

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
	}
	defer rows.Close()

	tokens, _ := scanMaps(rows)
	items := make([]map[string]any, len(tokens))
	for i, t := range tokens {
		items[i] = formatToken(t)
	}
	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (p *plugin) handleGetToken(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	addr := e.Request.PathValue("address_hash")
	rows, err := p.db.QueryContext(ctx, "SELECT * FROM tokens WHERE chain_id = ? AND contract_addr = ? LIMIT 1",
		p.config.ChainID, hexToBytes(addr))
	if err != nil {
		return e.NotFoundError("token not found", nil)
	}
	defer rows.Close()

	tokens, _ := scanMaps(rows)
	if len(tokens) == 0 {
		return e.NotFoundError("token not found", nil)
	}
	return e.JSON(http.StatusOK, formatToken(tokens[0]))
}

func (p *plugin) handleTokenTransfers(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	addr := e.Request.PathValue("address_hash")
	rows, err := p.db.QueryContext(ctx,
		"SELECT * FROM token_transfers WHERE chain_id = ? AND token_contract_addr = ? ORDER BY block_number DESC LIMIT 50",
		p.config.ChainID, hexToBytes(addr))
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
	}
	defer rows.Close()

	transfers, _ := scanMaps(rows)
	items := make([]map[string]any, len(transfers))
	for i, t := range transfers {
		items[i] = formatTokenTransfer(t)
	}
	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (p *plugin) handleTokenHolders(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	addr := e.Request.PathValue("address_hash")
	rows, err := p.db.QueryContext(ctx,
		"SELECT * FROM address_current_token_balances WHERE chain_id = ? AND token_contract_addr = ? ORDER BY value DESC LIMIT 50",
		p.config.ChainID, hexToBytes(addr))
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
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
	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (p *plugin) handleTokenInstances(e *core.RequestEvent) error {
	return e.JSON(http.StatusOK, emptyPage())
}

func (p *plugin) handleTokenInstance(e *core.RequestEvent) error {
	return e.NotFoundError("not found", nil)
}

// ---- Smart Contracts ----

func (p *plugin) handleListContracts(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	rows, err := p.db.QueryContext(ctx, "SELECT * FROM smart_contracts WHERE chain_id = ? ORDER BY inserted_at DESC LIMIT 50",
		p.config.ChainID)
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
	}
	defer rows.Close()

	contracts, _ := scanMaps(rows)
	items := make([]map[string]any, len(contracts))
	for i, c := range contracts {
		items[i] = formatContract(c)
	}
	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (p *plugin) handleGetContract(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	addr := e.Request.PathValue("address_hash")
	rows, err := p.db.QueryContext(ctx, "SELECT * FROM smart_contracts WHERE chain_id = ? AND address_hash = ? LIMIT 1",
		p.config.ChainID, hexToBytes(addr))
	if err != nil {
		return e.NotFoundError("contract not found", nil)
	}
	defer rows.Close()

	contracts, _ := scanMaps(rows)
	if len(contracts) == 0 {
		return e.NotFoundError("contract not found", nil)
	}
	return e.JSON(http.StatusOK, formatContract(contracts[0]))
}

func (p *plugin) handleVerifyContract(e *core.RequestEvent) error {
	return e.JSON(http.StatusOK, map[string]any{"status": "queued"})
}

// ---- Search ----

func (p *plugin) handleSearch(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	q := e.Request.URL.Query().Get("q")
	if q == "" {
		return e.JSON(http.StatusOK, emptyPage())
	}

	var items []map[string]any

	// Transaction hash
	if strings.HasPrefix(q, "0x") && len(q) == 66 {
		if row := p.queryOneCtx(ctx, "transactions", "hash", hexToBytes(q)); row != nil {
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
		p.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM blocks WHERE chain_id = ? AND number = ?", p.config.ChainID, n).Scan(&count)
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
		rows, err := p.db.QueryContext(ctx,
			"SELECT * FROM tokens WHERE chain_id = ? AND (name LIKE ? ESCAPE '\\' OR symbol LIKE ? ESCAPE '\\') ORDER BY holder_count DESC LIMIT 5",
			p.config.ChainID, "%"+escaped+"%", "%"+escaped+"%")
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

	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nil})
}

func (p *plugin) handleSearchRedirect(e *core.RequestEvent) error {
	q := e.Request.URL.Query().Get("q")
	if strings.HasPrefix(q, "0x") && len(q) == 66 {
		return e.JSON(http.StatusOK, map[string]any{"redirect": true, "type": "transaction", "parameter": q})
	}
	if strings.HasPrefix(q, "0x") && len(q) == 42 {
		return e.JSON(http.StatusOK, map[string]any{"redirect": true, "type": "address", "parameter": q})
	}
	return e.JSON(http.StatusOK, map[string]any{"redirect": false})
}

// ---- Stats ----

func (p *plugin) handleStats(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	var blockCount, txCount, addrCount int
	p.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM blocks WHERE chain_id = ?", p.config.ChainID).Scan(&blockCount)
	p.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM transactions WHERE chain_id = ?", p.config.ChainID).Scan(&txCount)
	p.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM addresses WHERE chain_id = ?", p.config.ChainID).Scan(&addrCount)

	return e.JSON(http.StatusOK, map[string]any{
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

func (p *plugin) handleChartTransactions(e *core.RequestEvent) error {
	return e.JSON(http.StatusOK, map[string]any{"chart_data": []any{}})
}

func (p *plugin) handleChartMarket(e *core.RequestEvent) error {
	return e.JSON(http.StatusOK, map[string]any{"chart_data": []any{}})
}

func (p *plugin) handleHealth(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()

	return e.JSON(http.StatusOK, map[string]any{
		"healthy":    p.db.PingContext(ctx) == nil,
		"chain_id":   p.config.ChainID,
		"chain_name": p.config.ChainName,
	})
}

// ---- Query Helpers ----

func (p *plugin) queryOneCtx(ctx context.Context, table, col string, val any) map[string]any {
	rows, err := p.db.QueryContext(ctx,
		fmt.Sprintf("SELECT * FROM %s WHERE chain_id = ? AND %s = ? LIMIT 1", table, col),
		p.config.ChainID, val)
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

func (p *plugin) queryTxListCtx(e *core.RequestEvent, ctx context.Context, query string, args []any, limit int) error {
	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return e.JSON(http.StatusOK, emptyPage())
	}
	defer rows.Close()
	return p.formatTxRows(e, rows, limit)
}

func (p *plugin) formatTxRows(e *core.RequestEvent, rows *sql.Rows, limit int) error {
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

	return e.JSON(http.StatusOK, paginatedResponse{Items: items, NextPageParams: nextPage})
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
