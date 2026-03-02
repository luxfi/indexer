package explorer

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

	_ "github.com/mattn/go-sqlite3"
)

// StandaloneServer serves /v1/explorer/* on a standard net/http mux.
type StandaloneServer struct {
	db  *sql.DB
	cfg Config
	mux *http.ServeMux
	t   tableNames
}

type tableNames struct {
	blocks, txs, addrs, tokens, transfers, logs, itxs, contracts, balances string
}

func NewStandaloneServer(cfg Config) (*StandaloneServer, error) {
	if cfg.IndexerDBPath == "" {
		return nil, fmt.Errorf("IndexerDBPath required")
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL&_busy_timeout=5000&cache=shared", cfg.IndexerDBPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	s := &StandaloneServer{db: db, cfg: cfg, mux: http.NewServeMux()}
	s.detectTables()
	s.routes()
	log.Printf("[explorer] API ready — %s reading %s (%s tables)", cfg.ChainName, cfg.IndexerDBPath, s.t.blocks)
	return s, nil
}

func (s *StandaloneServer) Handler() http.Handler { return s.mux }
func (s *StandaloneServer) Close()                { s.db.Close() }

func (s *StandaloneServer) detectTables() {
	var c int
	s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='evm_blocks'").Scan(&c)
	if c > 0 {
		s.t = tableNames{"evm_blocks", "evm_transactions", "evm_addresses", "evm_tokens",
			"evm_token_transfers", "evm_logs", "evm_internal_transactions", "evm_smart_contracts", "evm_token_balances"}
	} else {
		s.t = tableNames{"blocks", "transactions", "addresses", "tokens",
			"token_transfers", "logs", "internal_transactions", "smart_contracts", "address_current_token_balances"}
	}
}

func (s *StandaloneServer) routes() {
	m := s.mux
	m.HandleFunc("GET /v1/explorer/blocks", s.j(s.listBlocks))
	m.HandleFunc("GET /v1/explorer/blocks/{id}", s.j(s.getBlock))
	m.HandleFunc("GET /v1/explorer/blocks/{id}/transactions", s.j(s.blockTxs))
	m.HandleFunc("GET /v1/explorer/transactions", s.j(s.listTxs))
	m.HandleFunc("GET /v1/explorer/transactions/{hash}", s.j(s.getTx))
	m.HandleFunc("GET /v1/explorer/transactions/{hash}/token-transfers", s.j(s.txTransfers))
	m.HandleFunc("GET /v1/explorer/transactions/{hash}/internal-transactions", s.j(s.txInternal))
	m.HandleFunc("GET /v1/explorer/transactions/{hash}/logs", s.j(s.txLogs))
	m.HandleFunc("GET /v1/explorer/addresses", s.j(s.listAddrs))
	m.HandleFunc("GET /v1/explorer/addresses/{hash}", s.j(s.getAddr))
	m.HandleFunc("GET /v1/explorer/addresses/{hash}/transactions", s.j(s.addrTxs))
	m.HandleFunc("GET /v1/explorer/addresses/{hash}/counters", s.j(s.addrCounters))
	m.HandleFunc("GET /v1/explorer/tokens", s.j(s.listTokens))
	m.HandleFunc("GET /v1/explorer/tokens/{addr}", s.j(s.getToken))
	m.HandleFunc("GET /v1/explorer/tokens/{addr}/holders", s.j(s.tokenHolders))
	m.HandleFunc("GET /v1/explorer/smart-contracts/{addr}", s.j(s.getContract))
	m.HandleFunc("GET /v1/explorer/search", s.j(s.search))
	m.HandleFunc("GET /v1/explorer/search/quick", s.j(s.search))
	m.HandleFunc("GET /v1/explorer/search/check-redirect", s.j(s.searchRedirect))
	m.HandleFunc("GET /v1/explorer/stats", s.j(s.stats))
	m.HandleFunc("GET /v1/explorer/stats/charts/transactions", s.j(s.chartTxs))
	m.HandleFunc("GET /v1/explorer/stats/charts/market", s.j(s.chartMarket))

	// Homepage widgets
	m.HandleFunc("GET /v1/explorer/main-page/blocks", s.j(s.mainPageBlocks))
	m.HandleFunc("GET /v1/explorer/main-page/transactions", s.j(s.mainPageTxs))
	m.HandleFunc("GET /v1/explorer/main-page/indexing-status", s.j(s.indexingStatus))

	// Config
	m.HandleFunc("GET /v1/explorer/config/backend-version", s.j(s.backendVersion))
	m.HandleFunc("GET /v1/explorer/config/backend", s.j(s.backendConfig))

	// Address sub-resources
	m.HandleFunc("GET /v1/explorer/addresses/{hash}/token-transfers", s.j(s.addrTokenTransfers))
	m.HandleFunc("GET /v1/explorer/addresses/{hash}/internal-transactions", s.j(s.addrInternalTxs))
	m.HandleFunc("GET /v1/explorer/addresses/{hash}/logs", s.j(s.addrLogs))
	m.HandleFunc("GET /v1/explorer/addresses/{hash}/tokens", s.j(s.addrTokens))
	m.HandleFunc("GET /v1/explorer/addresses/{hash}/token-balances", s.j(s.addrTokens))
	m.HandleFunc("GET /v1/explorer/addresses/{hash}/coin-balance-history", s.j(s.addrCoinHistory))
	m.HandleFunc("GET /v1/explorer/addresses/{hash}/coin-balance-history-by-day", s.j(s.addrCoinHistory))
	m.HandleFunc("GET /v1/explorer/addresses/{hash}/tabs-counters", s.j(s.addrCounters))

	// Token sub-resources
	m.HandleFunc("GET /v1/explorer/tokens/{addr}/transfers", s.j(s.tokenTransfers))
	m.HandleFunc("GET /v1/explorer/tokens/{addr}/instances", s.j(s.emptyList))
	m.HandleFunc("GET /v1/explorer/tokens/{addr}/counters", s.j(s.tokenCounters))

	// Smart contract sub-resources
	m.HandleFunc("GET /v1/explorer/smart-contracts", s.j(s.listContracts))
	m.HandleFunc("GET /v1/explorer/smart-contracts/counters", s.j(s.contractCounters))

	// Token transfers list
	m.HandleFunc("GET /v1/explorer/token-transfers", s.j(s.allTokenTransfers))

	// Internal transactions list
	m.HandleFunc("GET /v1/explorer/internal-transactions", s.j(s.allInternalTxs))
}

type jfn func(*http.Request) (any, int)

func (s *StandaloneServer) j(fn jfn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		data, code := fn(r)
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(data)
	}
}

func (s *StandaloneServer) q(r *http.Request, query string, args ...any) (*sql.Rows, error) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		cancel()
		return nil, err
	}
	// Cancel when rows are closed via the request context chain.
	// The 5s timeout protects against hung queries.
	go func() {
		<-ctx.Done()
		cancel()
	}()
	return rows, nil
}

func ep() paginatedResponse { return paginatedResponse{Items: []any{}} }

func lim(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("items_count"))
	if n <= 0 {
		n = 50
	}
	if n > 250 {
		n = 250
	}
	return n
}

// ---- Blocks ----

func (s *StandaloneServer) listBlocks(r *http.Request) (any, int) {
	l := lim(r)
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s ORDER BY number DESC LIMIT ?", s.t.blocks), l+1)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	var np any
	if len(maps) > l {
		np = map[string]any{"block_number": maps[l-1]["number"], "items_count": l}
		maps = maps[:l]
	}
	items := make([]map[string]any, len(maps))
	for i, b := range maps {
		items[i] = formatBlock(b)
	}
	return paginatedResponse{Items: items, NextPageParams: np}, 200
}

func (s *StandaloneServer) getBlock(r *http.Request) (any, int) {
	id := r.PathValue("id")
	var rows *sql.Rows
	var err error
	if strings.HasPrefix(id, "0x") {
		rows, err = s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE hash = ? LIMIT 1", s.t.blocks), id)
	} else {
		rows, err = s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE number = ? LIMIT 1", s.t.blocks), id)
	}
	if err != nil {
		return map[string]string{"error": "not found"}, 404
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	if len(maps) == 0 {
		return map[string]string{"error": "not found"}, 404
	}
	return formatBlock(maps[0]), 200
}

func (s *StandaloneServer) blockTxs(r *http.Request) (any, int) {
	id := r.PathValue("id")
	l := lim(r)
	var rows *sql.Rows
	var err error
	if strings.HasPrefix(id, "0x") {
		rows, err = s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE block_hash = ? ORDER BY transaction_index LIMIT ?", s.t.txs), id, l)
	} else {
		rows, err = s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE block_number = ? ORDER BY transaction_index LIMIT ?", s.t.txs), id, l)
	}
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	return fmtTxPage(rows, l), 200
}

// ---- Transactions ----

func (s *StandaloneServer) listTxs(r *http.Request) (any, int) {
	l := lim(r)
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s ORDER BY block_number DESC, transaction_index DESC LIMIT ?", s.t.txs), l+1)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	return fmtTxPage(rows, l), 200
}

func (s *StandaloneServer) getTx(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE hash = ? LIMIT 1", s.t.txs), r.PathValue("hash"))
	if err != nil {
		return map[string]string{"error": "not found"}, 404
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	if len(maps) == 0 {
		return map[string]string{"error": "not found"}, 404
	}
	return formatTx(maps[0]), 200
}

func (s *StandaloneServer) txTransfers(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE transaction_hash = ? ORDER BY log_index", s.t.transfers), r.PathValue("hash"))
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, t := range maps {
		items[i] = formatTokenTransfer(t)
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) txInternal(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf(`SELECT * FROM %s WHERE transaction_hash = ? ORDER BY "index"`, s.t.itxs), r.PathValue("hash"))
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, t := range maps {
		items[i] = formatInternalTx(t)
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) txLogs(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf(`SELECT * FROM %s WHERE transaction_hash = ? ORDER BY "index"`, s.t.logs), r.PathValue("hash"))
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, l := range maps {
		items[i] = formatLog(l)
	}
	return paginatedResponse{Items: items}, 200
}

// ---- Addresses ----

func (s *StandaloneServer) listAddrs(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s ORDER BY fetched_coin_balance DESC LIMIT ?", s.t.addrs), lim(r))
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, a := range maps {
		items[i] = formatAddress(a)
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) getAddr(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE hash = ? LIMIT 1", s.t.addrs), r.PathValue("hash"))
	if err != nil {
		return map[string]string{"error": "not found"}, 404
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	if len(maps) == 0 {
		return map[string]string{"error": "not found"}, 404
	}
	return formatAddress(maps[0]), 200
}

func (s *StandaloneServer) addrTxs(r *http.Request) (any, int) {
	l := lim(r)
	addr := r.PathValue("hash")
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE from_address_hash = ? OR to_address_hash = ? ORDER BY block_number DESC LIMIT ?", s.t.txs), addr, addr, l)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	return fmtTxPage(rows, l), 200
}

func (s *StandaloneServer) addrCounters(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE hash = ? LIMIT 1", s.t.addrs), r.PathValue("hash"))
	if err != nil {
		return map[string]string{"error": "not found"}, 404
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	if len(maps) == 0 {
		return map[string]string{"error": "not found"}, 404
	}
	a := maps[0]
	return map[string]any{
		"transactions_count":    a["transactions_count"],
		"token_transfers_count": a["token_transfers_count"],
		"gas_usage_count":       fmtNum(a["gas_used"]),
		"validations_count":     0,
	}, 200
}

// ---- Tokens ----

func (s *StandaloneServer) listTokens(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s ORDER BY holder_count DESC LIMIT ?", s.t.tokens), lim(r))
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, t := range maps {
		items[i] = formatToken(t)
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) getToken(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE contract_address_hash = ? LIMIT 1", s.t.tokens), r.PathValue("addr"))
	if err != nil {
		return map[string]string{"error": "not found"}, 404
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	if len(maps) == 0 {
		return map[string]string{"error": "not found"}, 404
	}
	return formatToken(maps[0]), 200
}

func (s *StandaloneServer) tokenHolders(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE token_contract_address_hash = ? ORDER BY value DESC LIMIT 50", s.t.balances), r.PathValue("addr"))
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, b := range maps {
		items[i] = map[string]any{
			"address": map[string]any{"hash": bytesToHex(b["address_hash"])},
			"value":   fmtNum(b["value"]),
		}
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) getContract(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE address_hash = ? LIMIT 1", s.t.contracts), r.PathValue("addr"))
	if err != nil {
		return map[string]string{"error": "not found"}, 404
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	if len(maps) == 0 {
		return map[string]string{"error": "not found"}, 404
	}
	return formatContract(maps[0]), 200
}

// ---- Search + Stats ----

func (s *StandaloneServer) search(r *http.Request) (any, int) {
	q := r.URL.Query().Get("q")
	if q == "" {
		return ep(), 200
	}
	items := make([]map[string]any, 0)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if strings.HasPrefix(q, "0x") && len(q) == 66 {
		var h string
		s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT hash FROM %s WHERE hash = ? LIMIT 1", s.t.txs), q).Scan(&h)
		if h != "" {
			items = append(items, map[string]any{"type": "transaction", "transaction_hash": h})
		}
	}
	if strings.HasPrefix(q, "0x") && len(q) == 42 {
		items = append(items, map[string]any{"type": "address", "address_hash": strings.ToLower(q)})
	}
	if n, err := strconv.ParseInt(q, 10, 64); err == nil {
		var c int
		s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE number = ?", s.t.blocks), n).Scan(&c)
		if c > 0 {
			items = append(items, map[string]any{"type": "block", "block_number": n})
		}
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) stats(r *http.Request) (any, int) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var bc, tc, ac int
	s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", s.t.blocks)).Scan(&bc)
	s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", s.t.txs)).Scan(&tc)
	s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", s.t.addrs)).Scan(&ac)
	return map[string]any{
		"total_blocks":                   bc,
		"total_transactions":             tc,
		"total_addresses":                ac,
		"coin_price":                     nil,
		"market_cap":                     "0",
		"network_utilization_percentage": 0,
	}, 200
}

// ---- Homepage Widgets ----

func (s *StandaloneServer) mainPageBlocks(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s ORDER BY number DESC LIMIT 6", s.t.blocks))
	if err != nil {
		return []any{}, 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, b := range maps {
		items[i] = formatBlock(b)
	}
	return items, 200
}

func (s *StandaloneServer) mainPageTxs(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s ORDER BY block_number DESC, transaction_index DESC LIMIT 6", s.t.txs))
	if err != nil {
		return []any{}, 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, t := range maps {
		items[i] = formatTx(t)
	}
	return items, 200
}

func (s *StandaloneServer) indexingStatus(r *http.Request) (any, int) {
	var maxBlock int64
	s.db.QueryRow(fmt.Sprintf("SELECT COALESCE(MAX(number), 0) FROM %s", s.t.blocks)).Scan(&maxBlock)
	return map[string]any{
		"finished_indexing":                   true,
		"finished_indexing_blocks":            true,
		"indexed_blocks_ratio":                "1.00",
		"indexed_internal_transactions_ratio": "1.00",
	}, 200
}

// ---- Config ----

func (s *StandaloneServer) backendVersion(r *http.Request) (any, int) {
	return map[string]any{"backend_version": "v2.0.0+explorer"}, 200
}

func (s *StandaloneServer) backendConfig(r *http.Request) (any, int) {
	return map[string]any{
		"coin_name":         s.cfg.CoinSymbol,
		"chain_id":          fmt.Sprintf("%d", s.cfg.ChainID),
		"has_user_ops":      false,
		"has_mud_framework": false,
	}, 200
}

// ---- Search Redirect ----

func (s *StandaloneServer) searchRedirect(r *http.Request) (any, int) {
	q := r.URL.Query().Get("q")
	if strings.HasPrefix(q, "0x") && len(q) == 66 {
		return map[string]any{"redirect": true, "type": "transaction", "parameter": q}, 200
	}
	if strings.HasPrefix(q, "0x") && len(q) == 42 {
		return map[string]any{"redirect": true, "type": "address", "parameter": q}, 200
	}
	return map[string]any{"redirect": false}, 200
}

// ---- Charts ----

func (s *StandaloneServer) chartTxs(r *http.Request) (any, int) {
	return map[string]any{"chart_data": []any{}}, 200
}

func (s *StandaloneServer) chartMarket(r *http.Request) (any, int) {
	return map[string]any{"chart_data": []any{}}, 200
}

// ---- Additional Address Endpoints ----

func (s *StandaloneServer) addrTokenTransfers(r *http.Request) (any, int) {
	addr := r.PathValue("hash")
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE from_address_hash = ? OR to_address_hash = ? ORDER BY block_number DESC LIMIT 50", s.t.transfers), addr, addr)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, t := range maps {
		items[i] = formatTokenTransfer(t)
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) addrInternalTxs(r *http.Request) (any, int) {
	addr := r.PathValue("hash")
	rows, err := s.q(r, fmt.Sprintf(`SELECT * FROM %s WHERE from_address_hash = ? OR to_address_hash = ? ORDER BY block_number DESC LIMIT 50`, s.t.itxs), addr, addr)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, t := range maps {
		items[i] = formatInternalTx(t)
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) addrLogs(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE address_hash = ? ORDER BY block_number DESC LIMIT 50", s.t.logs), r.PathValue("hash"))
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, l := range maps {
		items[i] = formatLog(l)
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) addrTokens(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE address_hash = ? ORDER BY value DESC LIMIT 100", s.t.balances), r.PathValue("hash"))
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, b := range maps {
		items[i] = map[string]any{
			"token":    map[string]any{"address": bytesToHex(b["token_contract_address_hash"]), "type": b["token_type"]},
			"value":    fmtNum(b["value"]),
			"token_id": b["token_id"],
		}
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) addrCoinHistory(r *http.Request) (any, int) {
	return paginatedResponse{Items: []any{}}, 200
}

// ---- Token Sub-resources ----

func (s *StandaloneServer) tokenTransfers(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE token_contract_address_hash = ? ORDER BY block_number DESC LIMIT 50", s.t.transfers), r.PathValue("addr"))
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, t := range maps {
		items[i] = formatTokenTransfer(t)
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) tokenCounters(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE contract_address_hash = ? LIMIT 1", s.t.tokens), r.PathValue("addr"))
	if err != nil {
		return map[string]any{"token_holders_count": "0", "transfers_count": "0"}, 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	if len(maps) == 0 {
		return map[string]any{"token_holders_count": "0", "transfers_count": "0"}, 200
	}
	return map[string]any{
		"token_holders_count": fmtNum(maps[0]["holder_count"]),
		"transfers_count":     "0",
	}, 200
}

// ---- Smart Contract List & Counters ----

func (s *StandaloneServer) listContracts(r *http.Request) (any, int) {
	l := lim(r)
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s ORDER BY rowid DESC LIMIT ?", s.t.contracts), l)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, c := range maps {
		items[i] = formatContract(c)
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) contractCounters(r *http.Request) (any, int) {
	var total, verified int
	s.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", s.t.contracts)).Scan(&verified)
	return map[string]any{
		"smart_contracts":                  fmt.Sprintf("%d", total),
		"verified_smart_contracts":         fmt.Sprintf("%d", verified),
		"new_smart_contracts_24h":          "0",
		"new_verified_smart_contracts_24h": "0",
	}, 200
}

// ---- Global Lists ----

func (s *StandaloneServer) allTokenTransfers(r *http.Request) (any, int) {
	l := lim(r)
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s ORDER BY block_number DESC LIMIT ?", s.t.transfers), l)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, t := range maps {
		items[i] = formatTokenTransfer(t)
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) allInternalTxs(r *http.Request) (any, int) {
	l := lim(r)
	rows, err := s.q(r, fmt.Sprintf(`SELECT * FROM %s ORDER BY block_number DESC LIMIT ?`, s.t.itxs), l)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, t := range maps {
		items[i] = formatInternalTx(t)
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) emptyList(r *http.Request) (any, int) {
	return ep(), 200
}

// ---- Helpers ----

func fmtTxPage(rows *sql.Rows, limit int) paginatedResponse {
	maps, _ := scanMaps(rows)
	var np any
	if len(maps) > limit {
		last := maps[limit-1]
		np = map[string]any{"block_number": last["block_number"], "index": last["transaction_index"], "items_count": limit}
		maps = maps[:limit]
	}
	items := make([]map[string]any, len(maps))
	for i, t := range maps {
		items[i] = formatTx(t)
	}
	return paginatedResponse{Items: items, NextPageParams: np}
}
