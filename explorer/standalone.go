package explorer

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

// osLookupEnv is the real os.LookupEnv, assigned at init to allow test overrides.
var osLookupEnv = os.LookupEnv

// Input validation patterns for path parameters.
var (
	hexHashPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)          // tx/block hash
	hexAddrPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)          // address
	hexPrefPattern = regexp.MustCompile(`^0x[0-9a-fA-F]+$`)             // any hex
	blockIDPattern = regexp.MustCompile(`^([0-9]+|0x[0-9a-fA-F]{64})$`) // block number or hash
	pairPattern    = regexp.MustCompile(`^[A-Za-z0-9_/-]{1,40}$`)       // DEX pair symbols
	poolIDPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)        // pool IDs
)

// isValidHexAddr checks if s is a valid 0x-prefixed 40-char hex address.
func isValidHexAddr(s string) bool { return hexAddrPattern.MatchString(s) }

// isValidHexHash checks if s is a valid 0x-prefixed 64-char hex hash.
func isValidHexHash(s string) bool { return hexHashPattern.MatchString(s) }

// sanitizeFilename strips non-hex characters from values used in Content-Disposition.
func sanitizeFilename(s string) string {
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	out := make([]byte, 0, len(s))
	for _, b := range []byte(s) {
		if (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') {
			out = append(out, b)
		}
	}
	if len(out) == 0 {
		return "export"
	}
	return "0x" + string(out)
}

const maxRequestBodyBytes = 4096 // 4KB limit for webhook JSON payloads

// StandaloneServer serves /v1/explorer/* on a standard net/http mux.
type StandaloneServer struct {
	db  *sql.DB
	cfg Config
	mux *http.ServeMux
	t   tableNames

	gasMu          sync.Mutex
	gasPriceCache  map[string]string
	gasCacheExpiry time.Time

	notifWorker *NotificationWorker

	// wsSem limits concurrent WebSocket connections to prevent resource exhaustion.
	wsSem chan struct{}
}

type tableNames struct {
	blocks, txs, addrs, tokens, transfers, logs, itxs, contracts, balances string
	dexOrders, dexTrades, dexMarkets, dexPools, dexSwaps                   string
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

	s := &StandaloneServer{db: db, cfg: cfg, mux: http.NewServeMux(), wsSem: make(chan struct{}, 128)}
	s.detectTables()
	s.notifWorker = NewNotificationWorker(db, s.t.txs, nil)
	s.notifWorker.Start()
	s.routes()
	log.Printf("[explorer] API ready — %s reading %s (%s tables)", cfg.ChainName, cfg.IndexerDBPath, s.t.blocks)
	return s, nil
}

// AllowedOrigins returns the set of permitted CORS origins.
// Falls back to wildcard only if EXPLORER_CORS_ORIGINS is unset.
func AllowedOrigins() []string {
	if v := strings.TrimSpace(envOrDefault("EXPLORER_CORS_ORIGINS", "")); v != "" {
		return strings.Split(v, ",")
	}
	return []string{"*"}
}

func envOrDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// corsOriginAllowed checks if the request origin matches the allowed list.
func corsOriginAllowed(origin string, allowed []string) bool {
	if len(allowed) == 0 {
		return false
	}
	for _, a := range allowed {
		if a == "*" || strings.EqualFold(a, origin) {
			return true
		}
	}
	return false
}

// securityHeaders sets defense-in-depth HTTP headers on every response.
func securityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	// HSTS: 1 year, includeSubDomains
	w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	// CSP: restrict to self, allow inline styles for SPA
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' wss: ws:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
}

// Handler returns the http.Handler with security headers, CORS, and trailing-slash normalization.
func (s *StandaloneServer) Handler() http.Handler {
	allowed := AllowedOrigins()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security headers on every response
		securityHeaders(w)

		origin := r.Header.Get("Origin")

		// CORS preflight
		if r.Method == http.MethodOptions {
			if corsOriginAllowed(origin, allowed) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.Header().Set("Vary", "Origin")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Set CORS for non-preflight — public API, allow all origins
		if origin != "" && corsOriginAllowed(origin, allowed) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		// Normalize trailing slashes: /v1/explorer/blocks/ → /v1/explorer/blocks
		if len(r.URL.Path) > 1 && r.URL.Path[len(r.URL.Path)-1] == '/' {
			r.URL.Path = r.URL.Path[:len(r.URL.Path)-1]
		}
		s.mux.ServeHTTP(w, r)
	})
}
func (s *StandaloneServer) Close() {
	if s.notifWorker != nil {
		s.notifWorker.Stop()
	}
	s.db.Close()
}

func (s *StandaloneServer) detectTables() {
	var c int
	s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='evm_blocks'").Scan(&c)
	if c > 0 {
		s.t = tableNames{
			blocks: "evm_blocks", txs: "evm_transactions", addrs: "evm_addresses", tokens: "evm_tokens",
			transfers: "evm_token_transfers", logs: "evm_logs", itxs: "evm_internal_transactions",
			contracts: "evm_smart_contracts", balances: "evm_token_balances",
		}
	} else {
		s.t = tableNames{
			blocks: "blocks", txs: "transactions", addrs: "addresses", tokens: "tokens",
			transfers: "token_transfers", logs: "logs", itxs: "internal_transactions",
			contracts: "smart_contracts", balances: "address_current_token_balances",
		}
	}
	// DEX tables: detect dex_orders or evm_dex_orders
	s.t.dexOrders = s.detectTable("dex_orders", "evm_dex_orders")
	s.t.dexTrades = s.detectTable("dex_trades", "evm_dex_trades")
	s.t.dexMarkets = s.detectTable("dex_market_stats", "evm_dex_market_stats")
	s.t.dexPools = s.detectTable("dex_pools", "evm_dex_pools")
	s.t.dexSwaps = s.detectTable("dex_swaps", "evm_dex_swaps")
}

// detectTable returns the first table name that exists, or empty string.
func (s *StandaloneServer) detectTable(names ...string) string {
	for _, name := range names {
		var c int
		s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&c)
		if c > 0 {
			return name
		}
	}
	return ""
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

	// Config
	m.HandleFunc("GET /v1/explorer/config/version", s.j(s.backendVersion))
	m.HandleFunc("GET /v1/explorer/config/chain", s.j(s.backendConfig))

	// Realtime — Base SSE/WebSocket for live block/tx subscriptions
	m.HandleFunc("/v1/base/realtime", s.realtimeHandler)

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

	// CSV exports
	m.HandleFunc("GET /v1/explorer/addresses/{hash}/transactions/csv", s.csvHandler(s.csvAddrTxs))
	m.HandleFunc("GET /v1/explorer/addresses/{hash}/internal-transactions/csv", s.csvHandler(s.csvAddrInternalTxs))
	m.HandleFunc("GET /v1/explorer/addresses/{hash}/token-transfers/csv", s.csvHandler(s.csvAddrTokenTransfers))
	m.HandleFunc("GET /v1/explorer/addresses/{hash}/logs/csv", s.csvHandler(s.csvAddrLogs))
	m.HandleFunc("GET /v1/explorer/token-transfers/csv", s.csvHandler(s.csvAllTokenTransfers))

	// Unified account timeline
	m.HandleFunc("GET /v1/explorer/addresses/{hash}/timeline", s.j(s.addrTimeline))

	// DEX endpoints
	m.HandleFunc("GET /v1/explorer/dex/markets", s.j(s.dexMarkets))
	m.HandleFunc("GET /v1/explorer/dex/markets/{pair}", s.j(s.dexMarketDetail))
	m.HandleFunc("GET /v1/explorer/dex/trades", s.j(s.dexTrades))
	m.HandleFunc("GET /v1/explorer/dex/trades/{pair}", s.j(s.dexTradesByPair))
	m.HandleFunc("GET /v1/explorer/dex/orderbook/{pair}", s.j(s.dexOrderbook))
	m.HandleFunc("GET /v1/explorer/dex/candles/{pair}", s.j(s.dexCandles))

	// Pool endpoints
	m.HandleFunc("GET /v1/explorer/pools", s.j(s.poolList))
	m.HandleFunc("GET /v1/explorer/pools/{id}", s.j(s.poolDetail))
	m.HandleFunc("GET /v1/explorer/pools/{id}/swaps", s.j(s.poolSwaps))

	// Token distribution (Gini coefficient)
	m.HandleFunc("GET /v1/explorer/tokens/{addr}/distribution", s.j(s.tokenDistribution))

	// Webhooks (notification subscriptions)
	m.HandleFunc("POST /v1/explorer/webhooks", s.j(s.registerWebhook))
	m.HandleFunc("GET /v1/explorer/webhooks", s.j(s.listWebhooks))
	m.HandleFunc("DELETE /v1/explorer/webhooks", s.j(s.deleteWebhook))
}

type jfn func(*http.Request) (any, int)

func (s *StandaloneServer) j(fn jfn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// CORS handled by Handler() middleware — no per-handler wildcard.
		data, code := fn(r)
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(data)
	}
}

type csvfn func(http.ResponseWriter, *http.Request)

func (s *StandaloneServer) csvHandler(fn csvfn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		// CORS handled by Handler() middleware — no per-handler wildcard.
		fn(w, r)
	}
}

func (s *StandaloneServer) q(r *http.Request, query string, args ...any) (*sql.Rows, error) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		cancel()
		return nil, err
	}
	// Tie cancel to the request lifecycle — no goroutine leak.
	// When the HTTP handler returns, r.Context() is cancelled,
	// which cancels our derived ctx, which calls cancel().
	go func() {
		<-r.Context().Done()
		cancel()
	}()
	return rows, nil
}

func ep() paginatedResponse { return paginatedResponse{Items: []any{}} }

func lim(r *http.Request) int {
	q := r.URL.Query()
	n, _ := strconv.Atoi(q.Get("items_count"))
	if n <= 0 {
		n, _ = strconv.Atoi(q.Get("limit"))
	}
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
	if !blockIDPattern.MatchString(id) {
		return map[string]string{"error": "invalid block id"}, 400
	}
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
	if !blockIDPattern.MatchString(id) {
		return ep(), 400
	}
	l := lim(r)
	var rows *sql.Rows
	var err error
	if strings.HasPrefix(id, "0x") {
		rows, err = s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE block_hash = ? ORDER BY tx_index LIMIT ?", s.t.txs), id, l)
	} else {
		rows, err = s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE block_number = ? ORDER BY tx_index LIMIT ?", s.t.txs), id, l)
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
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s ORDER BY block_number DESC, tx_index DESC LIMIT ?", s.t.txs), l+1)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	return fmtTxPage(rows, l), 200
}

func (s *StandaloneServer) getTx(r *http.Request) (any, int) {
	hash := r.PathValue("hash")
	if !isValidHexHash(hash) {
		return map[string]string{"error": "invalid tx hash"}, 400
	}
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE hash = ? LIMIT 1", s.t.txs), hash)
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
	hash := r.PathValue("hash")
	if !isValidHexHash(hash) {
		return ep(), 400
	}
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE transaction_hash = ? ORDER BY log_index", s.t.transfers), hash)
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
	hash := r.PathValue("hash")
	if !isValidHexHash(hash) {
		return ep(), 400
	}
	rows, err := s.q(r, fmt.Sprintf(`SELECT * FROM %s WHERE transaction_hash = ? ORDER BY "index"`, s.t.itxs), hash)
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
	hash := r.PathValue("hash")
	if !isValidHexHash(hash) {
		return ep(), 400
	}
	rows, err := s.q(r, fmt.Sprintf(`SELECT * FROM %s WHERE transaction_hash = ? ORDER BY "index"`, s.t.logs), hash)
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
	addr := r.PathValue("hash")
	if !isValidHexAddr(addr) {
		return map[string]string{"error": "invalid address"}, 400
	}
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE hash = ? LIMIT 1", s.t.addrs), addr)
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
	if !isValidHexAddr(addr) {
		return ep(), 400
	}
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE from_addr = ? OR to_addr = ? ORDER BY block_number DESC LIMIT ?", s.t.txs), addr, addr, l)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	return fmtTxPage(rows, l), 200
}

func (s *StandaloneServer) addrCounters(r *http.Request) (any, int) {
	addr := r.PathValue("hash")
	if !isValidHexAddr(addr) {
		return map[string]string{"error": "invalid address"}, 400
	}
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE hash = ? LIMIT 1", s.t.addrs), addr)
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
	addr := r.PathValue("addr")
	if !isValidHexAddr(addr) {
		return map[string]string{"error": "invalid token address"}, 400
	}
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE contract_addr = ? LIMIT 1", s.t.tokens), addr)
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
	addr := r.PathValue("addr")
	if !isValidHexAddr(addr) {
		return ep(), 400
	}
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE token_address = ? ORDER BY value DESC LIMIT 50", s.t.balances), addr)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, b := range maps {
		items[i] = map[string]any{
			"address": map[string]any{"hash": bytesToHex(b["address"])},
			"value":   fmtNum(b["value"]),
		}
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) getContract(r *http.Request) (any, int) {
	addr := r.PathValue("addr")
	if !isValidHexAddr(addr) {
		return map[string]string{"error": "invalid contract address"}, 400
	}
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE address = ? LIMIT 1", s.t.contracts), addr)
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
	// Strip null bytes, control characters, and enforce max length.
	q = strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, q)
	q = strings.TrimSpace(q)
	if len(q) > 128 {
		q = q[:128]
	}
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
		"total_blocks":                   fmt.Sprintf("%d", bc),
		"total_addresses":                fmt.Sprintf("%d", ac),
		"total_transactions":             fmt.Sprintf("%d", tc),
		"average_block_time":             0,
		"coin_price":                     nil,
		"coin_price_change_percentage":   nil,
		"total_gas_used":                 "0",
		"transactions_today":             nil,
		"gas_used_today":                 "0",
		"gas_prices":                     nil,
		"gas_price_updated_at":           nil,
		"gas_prices_update_in":           0,
		"static_gas_price":               nil,
		"market_cap":                     nil,
		"network_utilization_percentage": 0,
		"tvl":                            nil,
	}, 200
}

// ---- Config ----

func (s *StandaloneServer) backendVersion(r *http.Request) (any, int) {
	return map[string]any{"version": "v2.0.0+explorer"}, 200
}

func (s *StandaloneServer) backendConfig(r *http.Request) (any, int) {
	return map[string]any{
		"coin_name":         s.cfg.CoinSymbol,
		"chain_id":          fmt.Sprintf("%d", s.cfg.ChainID),
		"has_user_ops":      false,
		"has_mud_framework": false,
	}, 200
}

// ---- Base Realtime (WebSocket/SSE) ----

// realtimeHandler handles /v1/base/realtime — WebSocket for live block/tx subscriptions.
// Protocol: JSON messages with {type, data} structure.
// Subscribe: {"subscribe": "blocks"} or {"subscribe": "transactions"}
func (s *StandaloneServer) realtimeHandler(w http.ResponseWriter, r *http.Request) {
	select {
	case s.wsSem <- struct{}{}:
		defer func() { <-s.wsSem }()
	default:
		http.Error(w, `{"error":"too many connections"}`, http.StatusServiceUnavailable)
		return
	}

	allowed := AllowedOrigins()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			return corsOriginAllowed(origin, allowed)
		},
		HandshakeTimeout: 10 * time.Second,
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	conn.SetReadLimit(4096)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		var req map[string]string
		if err := json.Unmarshal(msg, &req); err != nil {
			continue
		}

		switch req["subscribe"] {
		case "blocks", "transactions", "address":
			reply, _ := json.Marshal(map[string]any{"type": "subscribed", "channel": req["subscribe"]})
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			conn.WriteMessage(websocket.TextMessage, reply)
		case "ping":
			reply, _ := json.Marshal(map[string]any{"type": "pong"})
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			conn.WriteMessage(websocket.TextMessage, reply)
		}
	}
}

// ---- Search Redirect ----

func (s *StandaloneServer) searchRedirect(r *http.Request) (any, int) {
	q := r.URL.Query().Get("q")
	if isValidHexHash(q) {
		return map[string]any{"redirect": true, "type": "transaction", "parameter": q}, 200
	}
	if isValidHexAddr(q) {
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
	if !isValidHexAddr(addr) {
		return ep(), 400
	}
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE from_addr = ? OR to_addr = ? ORDER BY block_number DESC LIMIT 50", s.t.transfers), addr, addr)
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
	if !isValidHexAddr(addr) {
		return ep(), 400
	}
	rows, err := s.q(r, fmt.Sprintf(`SELECT * FROM %s WHERE from_addr = ? OR to_addr = ? ORDER BY block_number DESC LIMIT 50`, s.t.itxs), addr, addr)
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
	addr := r.PathValue("hash")
	if !isValidHexAddr(addr) {
		return ep(), 400
	}
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE address = ? ORDER BY block_number DESC LIMIT 50", s.t.logs), addr)
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
	addr := r.PathValue("hash")
	if !isValidHexAddr(addr) {
		return ep(), 400
	}
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE address = ? ORDER BY value DESC LIMIT 100", s.t.balances), addr)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, b := range maps {
		items[i] = map[string]any{
			"token":    map[string]any{"address": bytesToHex(b["token_address"]), "type": b["token_type"]},
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
	addr := r.PathValue("addr")
	if !isValidHexAddr(addr) {
		return ep(), 400
	}
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE token_address = ? ORDER BY block_number DESC LIMIT 50", s.t.transfers), addr)
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
	addr := r.PathValue("addr")
	if !isValidHexAddr(addr) {
		return map[string]any{"token_holders_count": "0", "transfers_count": "0"}, 400
	}
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE contract_addr = ? LIMIT 1", s.t.tokens), addr)
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

// ---- CSV Exports ----

const csvMaxRows = 10000

func (s *StandaloneServer) csvAddrTxs(w http.ResponseWriter, r *http.Request) {
	addr := r.PathValue("hash")
	if !isValidHexAddr(addr) {
		http.Error(w, "invalid address", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="transactions-%s.csv"`, sanitizeFilename(addr)))
	rows, err := s.q(r, fmt.Sprintf("SELECT hash, block_number, from_addr, to_addr, value, gas_used, status, timestamp FROM %s WHERE from_addr = ? OR to_addr = ? ORDER BY block_number DESC LIMIT ?", s.t.txs), addr, addr, csvMaxRows)
	if err != nil {
		return
	}
	defer rows.Close()
	cw := csv.NewWriter(w)
	cw.Write([]string{"hash", "block_number", "from", "to", "value", "gas_used", "status", "timestamp"})
	for rows.Next() {
		var hash, from, to, value, gasUsed, status any
		var blockNum, ts int64
		rows.Scan(&hash, &blockNum, &from, &to, &value, &gasUsed, &status, &ts)
		cw.Write([]string{
			bytesToHex(hash), strconv.FormatInt(blockNum, 10),
			bytesToHex(from), bytesToHex(to),
			fmtNum(value), fmtNum(gasUsed), txStatusStr(status), fmtTimestamp(ts),
		})
	}
	cw.Flush()
}

func (s *StandaloneServer) csvAddrInternalTxs(w http.ResponseWriter, r *http.Request) {
	addr := r.PathValue("hash")
	if !isValidHexAddr(addr) {
		http.Error(w, "invalid address", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="internal-transactions-%s.csv"`, sanitizeFilename(addr)))
	rows, err := s.q(r, fmt.Sprintf(`SELECT block_number, "index", type, call_type, from_addr, to_addr, value, gas_used, error FROM %s WHERE from_addr = ? OR to_addr = ? ORDER BY block_number DESC LIMIT ?`, s.t.itxs), addr, addr, csvMaxRows)
	if err != nil {
		return
	}
	defer rows.Close()
	cw := csv.NewWriter(w)
	cw.Write([]string{"block_number", "index", "type", "call_type", "from", "to", "value", "gas_used", "error"})
	for rows.Next() {
		var blockNum int64
		var idx int
		var typ, callType, from, to, value, gasUsed, errStr any
		rows.Scan(&blockNum, &idx, &typ, &callType, &from, &to, &value, &gasUsed, &errStr)
		cw.Write([]string{
			strconv.FormatInt(blockNum, 10), strconv.Itoa(idx),
			fmtNum(typ), fmtNum(callType),
			bytesToHex(from), bytesToHex(to),
			fmtNum(value), fmtNum(gasUsed), fmtNum(errStr),
		})
	}
	cw.Flush()
}

func (s *StandaloneServer) csvAddrTokenTransfers(w http.ResponseWriter, r *http.Request) {
	addr := r.PathValue("hash")
	if !isValidHexAddr(addr) {
		http.Error(w, "invalid address", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="token-transfers-%s.csv"`, sanitizeFilename(addr)))
	rows, err := s.q(r, fmt.Sprintf("SELECT transaction_hash, log_index, from_addr, to_addr, token_address, amount, token_type, timestamp FROM %s WHERE from_addr = ? OR to_addr = ? ORDER BY block_number DESC LIMIT ?", s.t.transfers), addr, addr, csvMaxRows)
	if err != nil {
		return
	}
	defer rows.Close()
	cw := csv.NewWriter(w)
	cw.Write([]string{"tx_hash", "log_index", "from", "to", "token_address", "amount", "token_type", "timestamp"})
	for rows.Next() {
		var txHash, from, to, tokenAddr, amount, tokenType any
		var logIdx int
		var ts int64
		rows.Scan(&txHash, &logIdx, &from, &to, &tokenAddr, &amount, &tokenType, &ts)
		cw.Write([]string{
			bytesToHex(txHash), strconv.Itoa(logIdx),
			bytesToHex(from), bytesToHex(to), bytesToHex(tokenAddr),
			fmtNum(amount), fmtNum(tokenType), fmtTimestamp(ts),
		})
	}
	cw.Flush()
}

func (s *StandaloneServer) csvAddrLogs(w http.ResponseWriter, r *http.Request) {
	addr := r.PathValue("hash")
	if !isValidHexAddr(addr) {
		http.Error(w, "invalid address", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="logs-%s.csv"`, sanitizeFilename(addr)))
	rows, err := s.q(r, fmt.Sprintf(`SELECT block_number, transaction_hash, "index", address, first_topic, second_topic, third_topic, fourth_topic, data FROM %s WHERE address = ? ORDER BY block_number DESC LIMIT ?`, s.t.logs), addr, csvMaxRows)
	if err != nil {
		return
	}
	defer rows.Close()
	cw := csv.NewWriter(w)
	cw.Write([]string{"block_number", "tx_hash", "index", "address", "topic0", "topic1", "topic2", "topic3", "data"})
	for rows.Next() {
		var blockNum int64
		var txHash, idx, addrHash, t0, t1, t2, t3, data any
		rows.Scan(&blockNum, &txHash, &idx, &addrHash, &t0, &t1, &t2, &t3, &data)
		cw.Write([]string{
			strconv.FormatInt(blockNum, 10), bytesToHex(txHash), fmtNum(idx),
			bytesToHex(addrHash),
			bytesToHex(t0), bytesToHex(t1), bytesToHex(t2), bytesToHex(t3),
			bytesToHex(data),
		})
	}
	cw.Flush()
}

func (s *StandaloneServer) csvAllTokenTransfers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Disposition", `attachment; filename="token-transfers.csv"`)
	rows, err := s.q(r, fmt.Sprintf("SELECT transaction_hash, log_index, from_addr, to_addr, token_address, amount, token_type, timestamp FROM %s ORDER BY block_number DESC LIMIT ?", s.t.transfers), csvMaxRows)
	if err != nil {
		return
	}
	defer rows.Close()
	cw := csv.NewWriter(w)
	cw.Write([]string{"tx_hash", "log_index", "from", "to", "token_address", "amount", "token_type", "timestamp"})
	for rows.Next() {
		var txHash, from, to, tokenAddr, amount, tokenType any
		var logIdx int
		var ts int64
		rows.Scan(&txHash, &logIdx, &from, &to, &tokenAddr, &amount, &tokenType, &ts)
		cw.Write([]string{
			bytesToHex(txHash), strconv.Itoa(logIdx),
			bytesToHex(from), bytesToHex(to), bytesToHex(tokenAddr),
			fmtNum(amount), fmtNum(tokenType), fmtTimestamp(ts),
		})
	}
	cw.Flush()
}

// ---- Gas Price Percentiles ----

func (s *StandaloneServer) gasPricePercentiles(r *http.Request) map[string]string {
	s.gasMu.Lock()
	if s.gasPriceCache != nil && time.Now().Before(s.gasCacheExpiry) {
		cached := s.gasPriceCache
		s.gasMu.Unlock()
		return cached
	}
	s.gasMu.Unlock()

	rows, err := s.q(r, fmt.Sprintf("SELECT gas_price FROM %s WHERE gas_price IS NOT NULL ORDER BY block_number DESC LIMIT 200", s.t.txs))
	if err != nil {
		return emptyPercentiles()
	}
	defer rows.Close()

	var prices []float64
	for rows.Next() {
		var gp any
		rows.Scan(&gp)
		if gp == nil {
			continue
		}
		switch v := gp.(type) {
		case int64:
			prices = append(prices, float64(v))
		case float64:
			prices = append(prices, v)
		case string:
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				prices = append(prices, f)
			}
		}
	}

	if len(prices) == 0 {
		return emptyPercentiles()
	}

	sort.Float64s(prices)
	pctl := func(p float64) string {
		idx := p / 100.0 * float64(len(prices)-1)
		lo := int(math.Floor(idx))
		hi := int(math.Ceil(idx))
		if lo == hi || hi >= len(prices) {
			return strconv.FormatInt(int64(prices[lo]), 10)
		}
		frac := idx - float64(lo)
		val := prices[lo]*(1-frac) + prices[hi]*frac
		return strconv.FormatInt(int64(math.Round(val)), 10)
	}

	result := map[string]string{
		"p10": pctl(10), "p25": pctl(25), "p50": pctl(50),
		"p75": pctl(75), "p90": pctl(90), "p95": pctl(95), "p99": pctl(99),
	}

	s.gasMu.Lock()
	s.gasPriceCache = result
	s.gasCacheExpiry = time.Now().Add(30 * time.Second)
	s.gasMu.Unlock()

	return result
}

func emptyPercentiles() map[string]string {
	return map[string]string{"p10": "0", "p25": "0", "p50": "0", "p75": "0", "p90": "0", "p95": "0", "p99": "0"}
}

// ---- Unified Account Timeline ----

type timelineItem struct {
	typ       string
	blockNum  int64
	timestamp string
	data      map[string]any
}

func (s *StandaloneServer) addrTimeline(r *http.Request) (any, int) {
	addr := r.PathValue("hash")
	if !isValidHexAddr(addr) {
		return ep(), 400
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		allItems []timelineItem
	)

	wg.Add(4)

	go func() {
		defer wg.Done()
		rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE from_addr = ? OR to_addr = ? ORDER BY block_number DESC LIMIT 50", s.t.txs), addr, addr)
		if err != nil {
			return
		}
		defer rows.Close()
		maps, _ := scanMaps(rows)
		var items []timelineItem
		for _, m := range maps {
			items = append(items, timelineItem{
				typ:       "transaction",
				blockNum:  toInt64(m["block_number"]),
				timestamp: fmtTimestamp(m["timestamp"]),
				data:      formatTx(m),
			})
		}
		mu.Lock()
		allItems = append(allItems, items...)
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE from_addr = ? OR to_addr = ? ORDER BY block_number DESC LIMIT 50", s.t.transfers), addr, addr)
		if err != nil {
			return
		}
		defer rows.Close()
		maps, _ := scanMaps(rows)
		var items []timelineItem
		for _, m := range maps {
			items = append(items, timelineItem{
				typ:       "token_transfer",
				blockNum:  toInt64(m["block_number"]),
				timestamp: fmtTimestamp(m["timestamp"]),
				data:      formatTokenTransfer(m),
			})
		}
		mu.Lock()
		allItems = append(allItems, items...)
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE from_addr = ? OR to_addr = ? ORDER BY block_number DESC LIMIT 50", s.t.itxs), addr, addr)
		if err != nil {
			return
		}
		defer rows.Close()
		maps, _ := scanMaps(rows)
		var items []timelineItem
		for _, m := range maps {
			items = append(items, timelineItem{
				typ:       "internal_transaction",
				blockNum:  toInt64(m["block_number"]),
				timestamp: fmtTimestamp(m["timestamp"]),
				data:      formatInternalTx(m),
			})
		}
		mu.Lock()
		allItems = append(allItems, items...)
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE address = ? ORDER BY block_number DESC LIMIT 50", s.t.logs), addr)
		if err != nil {
			return
		}
		defer rows.Close()
		maps, _ := scanMaps(rows)
		var items []timelineItem
		for _, m := range maps {
			items = append(items, timelineItem{
				typ:       "log",
				blockNum:  toInt64(m["block_number"]),
				timestamp: fmtTimestamp(m["timestamp"]),
				data:      formatLog(m),
			})
		}
		mu.Lock()
		allItems = append(allItems, items...)
		mu.Unlock()
	}()

	wg.Wait()

	sort.Slice(allItems, func(i, j int) bool {
		return allItems[i].blockNum > allItems[j].blockNum
	})

	if len(allItems) > 50 {
		allItems = allItems[:50]
	}

	items := make([]map[string]any, len(allItems))
	for i, it := range allItems {
		entry := make(map[string]any, len(it.data)+3)
		for k, v := range it.data {
			entry[k] = v
		}
		// Set timeline fields last so they are not overwritten by data fields.
		entry["type"] = it.typ
		entry["block_number"] = it.blockNum
		entry["timestamp"] = it.timestamp
		items[i] = entry
	}

	return paginatedResponse{Items: items}, 200
}

// ---- Helpers ----

func fmtTxPage(rows *sql.Rows, limit int) paginatedResponse {
	maps, _ := scanMaps(rows)
	var np any
	if len(maps) > limit {
		last := maps[limit-1]
		np = map[string]any{"block_number": last["block_number"], "index": last["tx_index"], "items_count": limit}
		maps = maps[:limit]
	}
	items := make([]map[string]any, len(maps))
	for i, t := range maps {
		items[i] = formatTx(t)
	}
	return paginatedResponse{Items: items, NextPageParams: np}
}

// ---- DEX Markets ----

func (s *StandaloneServer) dexMarkets(r *http.Request) (any, int) {
	if s.t.dexMarkets == "" {
		return ep(), 200
	}
	l := lim(r)
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s ORDER BY volume_24h DESC LIMIT ?", s.t.dexMarkets), l+1)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	var np any
	if len(maps) > l {
		np = map[string]any{"items_count": l}
		maps = maps[:l]
	}
	return paginatedResponse{Items: maps, NextPageParams: np}, 200
}

func (s *StandaloneServer) dexMarketDetail(r *http.Request) (any, int) {
	if s.t.dexMarkets == "" {
		return map[string]string{"error": "not found"}, 404
	}
	pair := r.PathValue("pair")
	if !pairPattern.MatchString(pair) {
		return map[string]string{"error": "invalid pair"}, 400
	}
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE symbol = ? LIMIT 1", s.t.dexMarkets), pair)
	if err != nil {
		return map[string]string{"error": "not found"}, 404
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	if len(maps) == 0 {
		return map[string]string{"error": "not found"}, 404
	}
	return maps[0], 200
}

// ---- DEX Trades ----

func (s *StandaloneServer) dexTrades(r *http.Request) (any, int) {
	if s.t.dexTrades == "" {
		return ep(), 200
	}
	l := lim(r)
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s ORDER BY timestamp DESC LIMIT ?", s.t.dexTrades), l+1)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	var np any
	if len(maps) > l {
		last := maps[l-1]
		np = map[string]any{"timestamp": last["timestamp"], "items_count": l}
		maps = maps[:l]
	}
	return paginatedResponse{Items: maps, NextPageParams: np}, 200
}

func (s *StandaloneServer) dexTradesByPair(r *http.Request) (any, int) {
	if s.t.dexTrades == "" {
		return ep(), 200
	}
	pair := r.PathValue("pair")
	if !pairPattern.MatchString(pair) {
		return ep(), 400
	}
	l := lim(r)
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE symbol = ? ORDER BY timestamp DESC LIMIT ?", s.t.dexTrades), pair, l+1)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	var np any
	if len(maps) > l {
		last := maps[l-1]
		np = map[string]any{"timestamp": last["timestamp"], "items_count": l}
		maps = maps[:l]
	}
	return paginatedResponse{Items: maps, NextPageParams: np}, 200
}

// ---- DEX Orderbook ----

func (s *StandaloneServer) dexOrderbook(r *http.Request) (any, int) {
	if s.t.dexOrders == "" {
		return map[string]any{"bids": []any{}, "asks": []any{}}, 200
	}
	pair := r.PathValue("pair")
	if !pairPattern.MatchString(pair) {
		return map[string]any{"bids": []any{}, "asks": []any{}}, 400
	}
	l := lim(r)

	bids, err := s.q(r, fmt.Sprintf(
		"SELECT price, SUM(quantity - filled_qty) AS size FROM %s WHERE symbol = ? AND side = 'buy' AND status IN ('open','partial') GROUP BY price ORDER BY price DESC LIMIT ?",
		s.t.dexOrders), pair, l)
	if err != nil {
		return map[string]any{"bids": []any{}, "asks": []any{}}, 200
	}
	defer bids.Close()
	bidMaps, _ := scanMaps(bids)

	asks, err := s.q(r, fmt.Sprintf(
		"SELECT price, SUM(quantity - filled_qty) AS size FROM %s WHERE symbol = ? AND side = 'sell' AND status IN ('open','partial') GROUP BY price ORDER BY price ASC LIMIT ?",
		s.t.dexOrders), pair, l)
	if err != nil {
		return map[string]any{"bids": bidMaps, "asks": []any{}}, 200
	}
	defer asks.Close()
	askMaps, _ := scanMaps(asks)

	if bidMaps == nil {
		bidMaps = []map[string]any{}
	}
	if askMaps == nil {
		askMaps = []map[string]any{}
	}
	return map[string]any{"bids": bidMaps, "asks": askMaps}, 200
}

// ---- DEX Candles ----

// candleSeconds maps interval strings to seconds.
var candleSeconds = map[string]int64{
	"1m":  60,
	"5m":  300,
	"15m": 900,
	"1h":  3600,
	"4h":  14400,
	"1d":  86400,
}

func (s *StandaloneServer) dexCandles(r *http.Request) (any, int) {
	if s.t.dexTrades == "" {
		return ep(), 200
	}
	pair := r.PathValue("pair")
	if !pairPattern.MatchString(pair) {
		return ep(), 400
	}
	q := r.URL.Query()

	interval := q.Get("interval")
	secs, ok := candleSeconds[interval]
	if !ok {
		secs = 3600
		interval = "1h"
	}

	l := lim(r)

	// Time range
	var fromTS, toTS int64
	if v := q.Get("from"); v != "" {
		fromTS, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := q.Get("to"); v != "" {
		toTS, _ = strconv.ParseInt(v, 10, 64)
	}
	if toTS == 0 {
		toTS = time.Now().Unix()
	}
	if fromTS == 0 {
		fromTS = toTS - secs*int64(l)
	}

	// Group trades by interval bucket, compute OHLCV.
	// (timestamp / secs) * secs gives the bucket start.
	query := fmt.Sprintf(`
		SELECT
			(CAST(strftime('%%s', timestamp) AS INTEGER) / ?) * ? AS bucket,
			MIN(price) AS low,
			MAX(price) AS high,
			SUM(quantity) AS volume,
			COUNT(*) AS trades
		FROM %s
		WHERE symbol = ?
		  AND CAST(strftime('%%s', timestamp) AS INTEGER) >= ?
		  AND CAST(strftime('%%s', timestamp) AS INTEGER) < ?
		GROUP BY bucket
		ORDER BY bucket ASC
		LIMIT ?
	`, s.t.dexTrades)

	rows, err := s.q(r, query, secs, secs, pair, fromTS, toTS, l)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	bucketMaps, _ := scanMaps(rows)

	candles := make([]map[string]any, 0, len(bucketMaps))
	for _, bm := range bucketMaps {
		bucket := toInt64(bm["bucket"])
		// Get open (first trade) and close (last trade) price for this bucket
		var openPrice, closePrice any
		s.db.QueryRow(fmt.Sprintf(
			"SELECT price FROM %s WHERE symbol = ? AND CAST(strftime('%%s', timestamp) AS INTEGER) >= ? AND CAST(strftime('%%s', timestamp) AS INTEGER) < ? ORDER BY timestamp ASC LIMIT 1",
			s.t.dexTrades), pair, bucket, bucket+secs).Scan(&openPrice)
		s.db.QueryRow(fmt.Sprintf(
			"SELECT price FROM %s WHERE symbol = ? AND CAST(strftime('%%s', timestamp) AS INTEGER) >= ? AND CAST(strftime('%%s', timestamp) AS INTEGER) < ? ORDER BY timestamp DESC LIMIT 1",
			s.t.dexTrades), pair, bucket, bucket+secs).Scan(&closePrice)

		candles = append(candles, map[string]any{
			"time":     bucket,
			"open":     fmtNum(openPrice),
			"high":     fmtNum(bm["high"]),
			"low":      fmtNum(bm["low"]),
			"close":    fmtNum(closePrice),
			"volume":   fmtNum(bm["volume"]),
			"trades":   bm["trades"],
			"interval": interval,
		})
	}
	return paginatedResponse{Items: candles}, 200
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case int:
		return int64(n)
	default:
		s := fmt.Sprintf("%v", v)
		i, _ := strconv.ParseInt(s, 10, 64)
		return i
	}
}

// ---- Pools ----

func (s *StandaloneServer) poolList(r *http.Request) (any, int) {
	if s.t.dexPools == "" {
		return ep(), 200
	}
	l := lim(r)
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s ORDER BY CAST(reserve0 AS INTEGER) DESC LIMIT ?", s.t.dexPools), l+1)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	var np any
	if len(maps) > l {
		np = map[string]any{"items_count": l}
		maps = maps[:l]
	}
	return paginatedResponse{Items: maps, NextPageParams: np}, 200
}

func (s *StandaloneServer) poolDetail(r *http.Request) (any, int) {
	if s.t.dexPools == "" {
		return map[string]string{"error": "not found"}, 404
	}
	id := r.PathValue("id")
	if !poolIDPattern.MatchString(id) {
		return map[string]string{"error": "invalid pool id"}, 400
	}
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE id = ? LIMIT 1", s.t.dexPools), id)
	if err != nil {
		return map[string]string{"error": "not found"}, 404
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	if len(maps) == 0 {
		return map[string]string{"error": "not found"}, 404
	}

	pool := maps[0]

	// Attach recent swaps
	if s.t.dexSwaps != "" {
		swapRows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE pool_id = ? ORDER BY timestamp DESC LIMIT 10", s.t.dexSwaps), id)
		if err == nil {
			defer swapRows.Close()
			swaps, _ := scanMaps(swapRows)
			if swaps == nil {
				swaps = []map[string]any{}
			}
			pool["recent_swaps"] = swaps
		}
	}
	return pool, 200
}

func (s *StandaloneServer) poolSwaps(r *http.Request) (any, int) {
	if s.t.dexSwaps == "" {
		return ep(), 200
	}
	id := r.PathValue("id")
	if !poolIDPattern.MatchString(id) {
		return ep(), 400
	}
	l := lim(r)
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE pool_id = ? ORDER BY timestamp DESC LIMIT ?", s.t.dexSwaps), id, l+1)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	var np any
	if len(maps) > l {
		last := maps[l-1]
		np = map[string]any{"timestamp": last["timestamp"], "items_count": l}
		maps = maps[:l]
	}
	return paginatedResponse{Items: maps, NextPageParams: np}, 200
}
