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
	// platformIDPattern matches a P-Chain block reference: a decimal height or
	// a CB58 (base58, no 0/O/I/l) block ID (typically ~49 chars, bounded at 64).
	// Queries are parameterized, so this is a sanity bound, not the injection
	// defense.
	platformIDPattern = regexp.MustCompile(`^([0-9]+|[1-9A-HJ-NP-Za-km-z]{1,64})$`)
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

// StandaloneServer serves "+p+"/* on a standard net/http mux.
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
	// tokenAddrCol is the column to filter on for "find token by address".
	// luxfi/indexer evm_tokens uses "address"; Blockscout-legacy tokens
	// table uses "contract_address". detectTables() sets this based on
	// which schema is live.
	tokenAddrCol string

	// addrTxCol is the per-address transaction count column: luxfi/indexer
	// evm_addresses spells it "tx_count", Blockscout-legacy addresses
	// "transactions_count".
	addrTxCol string

	// balTokenCol / transferTokenCol are the "which token" columns on the
	// balances and transfers tables. luxfi/indexer spells both
	// "token_address"; Blockscout-legacy spells them
	// "token_contract_address_hash". Empty means the table is absent or
	// its shape is unknown — the caller must then report unavailable
	// rather than count zero rows and call that an answer.
	balTokenCol, transferTokenCol string

	// platform (P-Chain / linear) variant. When platform is true the /blocks
	// and /main-page/blocks routes serve the P-Chain block handlers and the
	// /validators route is enabled. Set by detectTables when pchain_blocks
	// exists (created by the generic chain indexer's Init). The pchain_*
	// table names are the ones platform.InitSchema + the chain indexer create.
	platform                        bool
	pblocks, validators, delegators string
}

func NewStandaloneServer(cfg Config) (*StandaloneServer, error) {
	if cfg.IndexerDBPath == "" {
		return nil, fmt.Errorf("IndexerDBPath required")
	}
	if cfg.APIPrefix == "" {
		cfg.APIPrefix = "/v1/explorer"
	}
	// Read-mostly connection for the bulk of API queries. Open in RW mode
	// (not RO) so the verify endpoint can INSERT into evm_smart_contracts.
	// SQLite's WAL journal lets the indexer's writer + this connection's
	// occasional verify writes coexist without lockup; readers continue
	// concurrently via separate connections in the same pool.
	dsn := fmt.Sprintf("file:%s?mode=rw&_journal_mode=WAL&_busy_timeout=5000&cache=shared", cfg.IndexerDBPath)
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
	s.ensureContractsTable()
	s.notifWorker = NewNotificationWorker(db, s.t.txs, nil)
	s.notifWorker.Start()
	s.routes()
	primaryTable := s.t.blocks
	if s.t.platform {
		primaryTable = s.t.pblocks
	}
	log.Printf("[explorer] API ready — %s reading %s (%s tables)", cfg.ChainName, cfg.IndexerDBPath, primaryTable)
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

		// Normalize trailing slashes: "+p+"/blocks/ → "+p+"/blocks
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
			tokenAddrCol: "address",
		}
	} else {
		s.t = tableNames{
			blocks: "blocks", txs: "transactions", addrs: "addresses", tokens: "tokens",
			transfers: "token_transfers", logs: "logs", itxs: "internal_transactions",
			contracts: "smart_contracts", balances: "address_current_token_balances",
			tokenAddrCol: "contract_address",
		}
	}
	// DEX tables: detect dex_orders or evm_dex_orders
	s.t.dexOrders = s.detectTable("dex_orders", "evm_dex_orders")
	s.t.dexTrades = s.detectTable("dex_trades", "evm_dex_trades")
	s.t.dexMarkets = s.detectTable("dex_market_stats", "evm_dex_market_stats")
	s.t.dexPools = s.detectTable("dex_pools", "evm_dex_pools")
	s.t.dexSwaps = s.detectTable("dex_swaps", "evm_dex_swaps")

	// Platform (P-Chain / linear): a pchain_blocks table (from the generic
	// chain indexer) plus pchain_validators/pchain_delegators (from
	// platform.InitSchema). Set last so the wholesale s.t assignment above
	// doesn't clobber these. When platform is true, routes() serves the
	// P-Chain block/validator handlers; the EVM table fields stay set but
	// their tables are absent, so EVM-only routes simply return empty.
	if s.detectTable("pchain_blocks") != "" {
		s.t.platform = true
		s.t.pblocks = "pchain_blocks"
		s.t.validators = "pchain_validators"
		s.t.delegators = "pchain_delegators"
	}

	// The branches above name tables and columns by convention. Convention
	// is a guess; ask the database which of them are actually there, so a
	// wrong guess surfaces as "unavailable" instead of as a silent zero.
	s.t.addrTxCol = s.detectColumn(s.t.addrs, "tx_count", "transactions_count")
	if s.t.addrTxCol == "" {
		s.t.addrTxCol = "rowid"
	}
	s.t.balances = s.detectTable(s.t.balances)
	s.t.balTokenCol = s.detectColumn(s.t.balances, "token_address", "token_contract_address_hash")
	s.t.transferTokenCol = s.detectColumn(s.t.transfers, "token_address", "token_contract_address_hash")
	if c := s.detectColumn(s.t.tokens, "address", "contract_address", "contract_address_hash", "address_hash"); c != "" {
		s.t.tokenAddrCol = c
	}
}

// detectColumn returns the first of names that exists on table, or "".
// Guessing wrong is not a compile error and not a runtime error either —
// SQLite raises "no such column", the caller swallows it, and the endpoint
// answers an empty page forever. Ask instead.
func (s *StandaloneServer) detectColumn(table string, names ...string) string {
	if table == "" {
		return ""
	}
	have := map[string]struct{}{}
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return ""
		}
		have[name] = struct{}{}
	}
	for _, n := range names {
		if _, ok := have[n]; ok {
			return n
		}
	}
	return ""
}

// ensureContractsTable creates evm_smart_contracts if it doesn't yet exist.
// The luxfi/indexer evm pipeline (evm/indexer.go) doesn't allocate this
// table — it's verifier-driven, populated when the operator POSTs source
// code via /smart-contracts/{addr}/verify. Schema mirrors the Postgres
// migration shape (migrations/001_initial_schema.sql) trimmed for SQLite.
func (s *StandaloneServer) ensureContractsTable() {
	if s.t.contracts == "" {
		return
	}
	// CREATE IF NOT EXISTS is idempotent — running it on every API-pod
	// boot is cheap and ensures verify endpoints work even on a fresh DB
	// that pre-dates this feature.
	_, _ = s.db.Exec(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			address               TEXT PRIMARY KEY,
			name                  TEXT NOT NULL,
			compiler_version      TEXT NOT NULL,
			optimization          INTEGER DEFAULT 0,
			optimization_runs     INTEGER DEFAULT 200,
			contract_source_code  TEXT,
			abi                   TEXT,
			constructor_arguments TEXT,
			evm_version           TEXT DEFAULT 'paris',
			file_path             TEXT DEFAULT '',
			external_libraries    TEXT DEFAULT '[]',
			secondary_sources     TEXT DEFAULT '[]',
			verified_via          TEXT DEFAULT 'manual',
			partially_verified    INTEGER DEFAULT 0,
			is_vyper_contract     INTEGER DEFAULT 0,
			is_changed_bytecode   INTEGER DEFAULT 0,
			license_type          TEXT DEFAULT 'UNLICENSED',
			inserted_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at            TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`, s.t.contracts))
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
	p := s.cfg.APIPrefix // e.g. "/v1/explorer"

	// ================================================================
	// Universal endpoints — work for any chain type
	// ================================================================
	m.HandleFunc("GET "+p+"/stats", s.j(s.stats))
	m.HandleFunc("GET "+p+"/stats/charts/transactions", s.j(s.chartTxs))
	m.HandleFunc("GET "+p+"/stats/charts/market", s.j(s.chartMarket))
	m.HandleFunc("GET "+p+"/search", s.j(s.search))
	m.HandleFunc("GET "+p+"/search/quick", s.j(s.search))
	m.HandleFunc("GET "+p+"/search/check-redirect", s.j(s.searchRedirect))
	m.HandleFunc("GET "+p+"/config/version", s.j(s.backendVersion))
	m.HandleFunc("GET "+p+"/config/chain", s.j(s.backendConfig))

	// ================================================================
	// EVM endpoints — blocks, transactions, addresses, tokens, contracts
	// Used by: C-Chain, Zoo, Hanzo, SPC, Pars, and any EVM chain
	// ================================================================
	if s.t.platform {
		// P-Chain block explorer: linear blocks keyed by height/CB58 id, plus
		// the validator set synced from platform.getCurrentValidators.
		m.HandleFunc("GET "+p+"/blocks", s.j(s.platformBlocks))
		m.HandleFunc("GET "+p+"/blocks/{id}", s.j(s.platformBlock))
		m.HandleFunc("GET "+p+"/validators", s.j(s.platformValidators))
	} else {
		m.HandleFunc("GET "+p+"/blocks", s.j(s.listBlocks))
		m.HandleFunc("GET "+p+"/blocks/{id}", s.j(s.getBlock))
		m.HandleFunc("GET "+p+"/blocks/{id}/transactions", s.j(s.blockTxs))
	}
	m.HandleFunc("GET "+p+"/transactions", s.j(s.listTxs))
	m.HandleFunc("GET "+p+"/transactions/{hash}", s.j(s.getTx))
	m.HandleFunc("GET "+p+"/transactions/{hash}/token-transfers", s.j(s.txTransfers))
	m.HandleFunc("GET "+p+"/transactions/{hash}/internal-transactions", s.j(s.txInternal))
	m.HandleFunc("GET "+p+"/transactions/{hash}/logs", s.j(s.txLogs))
	m.HandleFunc("GET "+p+"/addresses", s.j(s.listAddrs))
	m.HandleFunc("GET "+p+"/addresses/{hash}", s.j(s.getAddr))
	m.HandleFunc("GET "+p+"/addresses/{hash}/transactions", s.j(s.addrTxs))
	m.HandleFunc("GET "+p+"/addresses/{hash}/counters", s.j(s.addrCounters))
	m.HandleFunc("GET "+p+"/addresses/{hash}/token-transfers", s.j(s.addrTokenTransfers))
	m.HandleFunc("GET "+p+"/addresses/{hash}/internal-transactions", s.j(s.addrInternalTxs))
	m.HandleFunc("GET "+p+"/addresses/{hash}/logs", s.j(s.addrLogs))
	m.HandleFunc("GET "+p+"/addresses/{hash}/tokens", s.j(s.addrTokens))
	m.HandleFunc("GET "+p+"/addresses/{hash}/token-balances", s.j(s.addrTokens))
	m.HandleFunc("GET "+p+"/addresses/{hash}/coin-balance-history", s.j(s.addrCoinHistory))
	m.HandleFunc("GET "+p+"/addresses/{hash}/timeline", s.j(s.addrTimeline))
	m.HandleFunc("GET "+p+"/addresses/{hash}/tabs-counters", s.j(s.addrCounters))
	m.HandleFunc("GET "+p+"/tokens", s.j(s.listTokens))
	m.HandleFunc("GET "+p+"/tokens/{addr}", s.j(s.getToken))
	m.HandleFunc("GET "+p+"/tokens/{addr}/holders", s.j(s.tokenHolders))
	m.HandleFunc("GET "+p+"/tokens/{addr}/transfers", s.j(s.tokenTransfers))
	m.HandleFunc("GET "+p+"/tokens/{addr}/instances", s.j(s.emptyList))
	m.HandleFunc("GET "+p+"/tokens/{addr}/counters", s.j(s.tokenCounters))
	m.HandleFunc("GET "+p+"/tokens/{addr}/distribution", s.j(s.tokenDistribution))
	m.HandleFunc("GET "+p+"/smart-contracts", s.j(s.listContracts))
	m.HandleFunc("GET "+p+"/smart-contracts/{addr}", s.j(s.getContract))
	m.HandleFunc("GET "+p+"/smart-contracts/counters", s.j(s.contractCounters))
	m.HandleFunc("POST "+p+"/smart-contracts/{addr}/verify", s.j(s.verifyContract))
	m.HandleFunc("GET "+p+"/token-transfers", s.j(s.allTokenTransfers))
	m.HandleFunc("GET "+p+"/internal-transactions", s.j(s.allInternalTxs))

	// CSV exports
	m.HandleFunc("GET "+p+"/addresses/{hash}/transactions/csv", s.csvHandler(s.csvAddrTxs))
	m.HandleFunc("GET "+p+"/addresses/{hash}/token-transfers/csv", s.csvHandler(s.csvAddrTokenTransfers))
	m.HandleFunc("GET "+p+"/addresses/{hash}/internal-transactions/csv", s.csvHandler(s.csvAddrInternalTxs))
	m.HandleFunc("GET "+p+"/addresses/{hash}/logs/csv", s.csvHandler(s.csvAddrLogs))
	m.HandleFunc("GET "+p+"/token-transfers/csv", s.csvHandler(s.csvAllTokenTransfers))

	// ================================================================
	// DEX endpoints — orderbook, trades, markets, pools
	// Used by: D-Chain (native CLOB), any chain with AMM contracts
	// ================================================================
	m.HandleFunc("GET "+p+"/dex/markets", s.j(s.dexMarkets))
	m.HandleFunc("GET "+p+"/dex/markets/{pair}", s.j(s.dexMarketDetail))
	m.HandleFunc("GET "+p+"/dex/trades", s.j(s.dexTrades))
	m.HandleFunc("GET "+p+"/dex/trades/{pair}", s.j(s.dexTradesByPair))
	m.HandleFunc("GET "+p+"/dex/orderbook/{pair}", s.j(s.dexOrderbook))
	m.HandleFunc("GET "+p+"/dex/candles/{pair}", s.j(s.dexCandles))
	m.HandleFunc("GET "+p+"/pools", s.j(s.poolList))
	m.HandleFunc("GET "+p+"/pools/{id}", s.j(s.poolDetail))
	m.HandleFunc("GET "+p+"/pools/{id}/swaps", s.j(s.poolSwaps))

	// ================================================================
	// Homepage widgets
	// ================================================================
	if s.t.platform {
		m.HandleFunc("GET "+p+"/main-page/blocks", s.j(s.platformMainPageBlocks))
	} else {
		m.HandleFunc("GET "+p+"/main-page/blocks", s.j(s.mainPageBlocks))
	}
	m.HandleFunc("GET "+p+"/main-page/transactions", s.j(s.mainPageTxs))
	m.HandleFunc("GET "+p+"/main-page/indexing-status", s.j(s.indexingStatus))
	m.HandleFunc("GET "+p+"/config/backend-version", s.j(s.backendVersion))
	m.HandleFunc("GET "+p+"/config/backend", s.j(s.backendConfig))

	// ================================================================
	// Notifications — webhook subscriptions for address activity
	// ================================================================
	m.HandleFunc("POST "+p+"/webhooks", s.j(s.registerWebhook))
	m.HandleFunc("GET "+p+"/webhooks", s.j(s.listWebhooks))
	m.HandleFunc("DELETE "+p+"/webhooks", s.j(s.deleteWebhook))

	// ================================================================
	// Realtime — Base WebSocket for live subscriptions
	// ================================================================
	m.HandleFunc("/v1/base/realtime", s.realtimeHandler)
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
		rows, err = s.q(r, s.txSelectAll("WHERE block_hash = ? ORDER BY tx_index LIMIT ?"), id, l)
	} else {
		rows, err = s.q(r, s.txSelectAll("WHERE block_number = ? ORDER BY tx_index LIMIT ?"), id, l)
	}
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	return fmtTxPage(rows, l), 200
}

// ---- Transactions ----

// txSelectAll builds a "SELECT *" over the txs table that also pulls each
// row's block base_fee (aliased block_base_fee) via a correlated subquery.
// txFeeObj uses it to compute a real fee (gas_used × base_fee) for rows whose
// gas_price was never ingested — no re-index required.
func (s *StandaloneServer) txSelectAll(suffix string) string {
	return fmt.Sprintf("SELECT *, (SELECT base_fee FROM %s WHERE number = block_number) AS block_base_fee FROM %s %s", s.t.blocks, s.t.txs, suffix)
}

func (s *StandaloneServer) listTxs(r *http.Request) (any, int) {
	l := lim(r)
	rows, err := s.q(r, s.txSelectAll("ORDER BY block_number DESC, tx_index DESC LIMIT ?"), l+1)
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
	rows, err := s.q(r, s.txSelectAll("WHERE hash = ? LIMIT 1"), hash)
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
	// evm_token_transfers uses `tx_hash`; Blockscout-legacy uses
	// `transaction_hash`. Try the modern column first, fall back if the
	// query errors (most likely "no such column").
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE tx_hash = ? ORDER BY log_index", s.t.transfers), hash)
	if err != nil {
		rows, err = s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE transaction_hash = ? ORDER BY log_index", s.t.transfers), hash)
	}
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
	// evm_logs uses tx_hash + log_index; Blockscout-legacy uses
	// transaction_hash + index. Try the modern column first.
	rows, err := s.q(r, fmt.Sprintf(`SELECT * FROM %s WHERE tx_hash = ? ORDER BY log_index`, s.t.logs), hash)
	if err != nil {
		rows, err = s.q(r, fmt.Sprintf(`SELECT * FROM %s WHERE transaction_hash = ? ORDER BY "index"`, s.t.logs), hash)
	}
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
	// Sort by whichever tx-count column this schema actually has:
	// luxfi/indexer evm_addresses spells it `tx_count`, Blockscout-legacy
	// `addresses` spells it `transactions_count`. Naming one of the two
	// unconditionally made the ORDER BY reference a column that does not
	// exist, and the error was swallowed into an empty page below — so
	// /addresses answered {"items":[]} while /stats reported 72 addresses.
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s ORDER BY %s DESC LIMIT ?", s.t.addrs, s.t.addrTxCol), lim(r))
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, a := range maps {
		items[i] = formatAddress(a)
	}
	s.withNativeBalance(r.Context(), items)
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
	item := formatAddress(maps[0])
	s.withNativeBalance(r.Context(), []map[string]any{item})
	return item, 200
}

func (s *StandaloneServer) addrTxs(r *http.Request) (any, int) {
	l := lim(r)
	addr := r.PathValue("hash")
	if !isValidHexAddr(addr) {
		return ep(), 400
	}
	rows, err := s.q(r, s.txSelectAll("WHERE from_addr = ? OR to_addr = ? ORDER BY block_number DESC LIMIT ?"), addr, addr, l)
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
	// Counters tolerate the legacy `transactions_count` vs trimmed `tx_count`
	// schema drift and always emit a number (never JSON null) — the
	// Blockscout-derived SPA does `.toLocaleString()` without a guard.
	txC := a["transactions_count"]
	if txC == nil {
		txC = a["tx_count"]
	}
	if txC == nil {
		txC = int64(0)
	}
	ttC := a["token_transfers_count"]
	if ttC == nil {
		ttC = int64(0)
	}
	return map[string]any{
		"transactions_count":    txC,
		"token_transfers_count": ttC,
		"gas_usage_count":       fmtNum(a["gas_used"]),
		"validations_count":     0,
	}, 200
}

// ---- Tokens ----

// tokenSelect selects every token column plus `holder_count_live`, the
// holder count counted from the balance rows themselves. The stored
// `holder_count` column is never written by the indexer, so both the token
// list's ordering and its "Holders" figure were derived from a column of
// zeros. Counting in the same statement keeps it one query, not N+1.
func (s *StandaloneServer) tokenSelect(suffix string) string {
	if s.t.balTokenCol == "" {
		return fmt.Sprintf("SELECT t.* FROM %s t %s", s.t.tokens, suffix)
	}
	return fmt.Sprintf(
		`SELECT t.*, (SELECT COUNT(*) FROM %s b
			WHERE LOWER(b.%s) = LOWER(t.%s) AND b.value != '0' AND b.value != '')
			AS holder_count_live
		 FROM %s t %s`, s.t.balances, s.t.balTokenCol, s.t.tokenAddrCol, s.t.tokens, suffix)
}

// holderOrder is the column tokens are ranked by: the counted holders when
// we can count them, otherwise the stored column.
func (s *StandaloneServer) holderOrder() string {
	if s.t.balTokenCol == "" {
		return "holder_count"
	}
	return "holder_count_live"
}

func (s *StandaloneServer) listTokens(r *http.Request) (any, int) {
	rows, err := s.q(r, s.tokenSelect(fmt.Sprintf("ORDER BY %s DESC LIMIT ?", s.holderOrder())), lim(r))
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
	rows, err := s.q(r, s.tokenSelect(fmt.Sprintf("WHERE t.%s = ? LIMIT 1", s.t.tokenAddrCol)), addr)
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
	addr = strings.ToLower(addr)
	if s.t.balTokenCol == "" {
		return ep(), 200
	}
	// Filter out zero-balance rows + sort by value DESC. CAST to REAL
	// for ordering since `value` is a TEXT column holding decimal strings.
	q := fmt.Sprintf(`SELECT %s AS address, value FROM %s
		WHERE LOWER(%s) = ? AND value != '0' AND value != ''
		ORDER BY CAST(value AS REAL) DESC LIMIT 50`,
		s.detectColumn(s.t.balances, "address", "address_hash"), s.t.balances, s.t.balTokenCol)
	rows, err := s.q(r, q, addr)
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
		// No verified-contract row: return 200 with an "unverified" shape
		// rather than 404. Blockscout-derived SPAs (incl. downstream-tenant
		// explorer) fetch this endpoint unconditionally on every address
		// page and the 404 shows up as noise in devtools even for EOAs.
		// The SPA's contract-tab gate is `i.is_contract && c` — so returning
		// is_verified=false on an EOA is structurally identical to the 404
		// case (no contract panel is rendered) but keeps the network log
		// clean.
		return map[string]any{
			"is_verified":        false,
			"is_self_destructed": false,
		}, 200
	}
	return formatContract(maps[0]), 200
}

// verifyContract accepts a JSON or form-encoded body with source + metadata,
// INSERTs (or UPSERTs) into evm_smart_contracts, and returns the resulting
// formatContract shape so the SPA can immediately render the verified page.
//
// Trust model: this endpoint is "manual" verification — the submitter is
// the deployer (or someone holding the deployer's private key); they're
// claiming the source matches the on-chain bytecode. There's no recompile
// + bytecode-compare step here. Future iterations can hook in solc or
// Sourcify; the schema already has `verified_via` to distinguish.
//
// Expected JSON body shape:
//
//	{
//	  "name":                  "VccGrowthFund",
//	  "compiler_version":      "v0.8.31+commit.bb7f4f8d",
//	  "optimization":          true,
//	  "optimization_runs":     200,
//	  "evm_version":           "paris",
//	  "license_type":          "MIT",
//	  "constructor_arguments": "0x000...",
//	  "abi":                   "[...]",
//	  "contract_source_code":  "// SPDX-License-Identifier: MIT\\npragma solidity ^0.8.28;\\n..."
//	}
func (s *StandaloneServer) verifyContract(r *http.Request) (any, int) {
	if s.t.contracts == "" {
		return map[string]string{"error": "contracts table not configured for this chain"}, 501
	}
	addr := r.PathValue("addr")
	if !isValidHexAddr(addr) {
		return map[string]string{"error": "invalid contract address"}, 400
	}
	addr = strings.ToLower(addr)

	var body struct {
		Name                 string `json:"name"`
		CompilerVersion      string `json:"compiler_version"`
		Optimization         *bool  `json:"optimization"`
		OptimizationRuns     *int64 `json:"optimization_runs"`
		EVMVersion           string `json:"evm_version"`
		LicenseType          string `json:"license_type"`
		ConstructorArguments string `json:"constructor_arguments"`
		ABI                  any    `json:"abi"`
		ContractSourceCode   string `json:"contract_source_code"`
		IsVyperContract      *bool  `json:"is_vyper_contract"`
		ExternalLibraries    any    `json:"external_libraries"`
		SecondarySources     any    `json:"secondary_sources"`
		FilePath             string `json:"file_path"`
		VerifiedVia          string `json:"verified_via"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return map[string]string{"error": "invalid JSON body: " + err.Error()}, 400
	}
	if body.Name == "" || body.CompilerVersion == "" {
		return map[string]string{"error": "name + compiler_version are required"}, 400
	}

	// abi + external_libraries + secondary_sources are stored as JSON-text
	// columns; marshal whatever the caller sent so the API GET can pass
	// them straight back to the SPA.
	abiJSON, _ := json.Marshal(body.ABI)
	extLibsJSON, _ := json.Marshal(body.ExternalLibraries)
	secSrcJSON, _ := json.Marshal(body.SecondarySources)

	optimization := 0
	if body.Optimization != nil && *body.Optimization {
		optimization = 1
	}
	var optRuns int64 = 200
	if body.OptimizationRuns != nil {
		optRuns = *body.OptimizationRuns
	}
	if body.EVMVersion == "" {
		body.EVMVersion = "paris"
	}
	if body.LicenseType == "" {
		body.LicenseType = "UNLICENSED"
	}
	if body.VerifiedVia == "" {
		body.VerifiedVia = "manual"
	}
	isVyper := 0
	if body.IsVyperContract != nil && *body.IsVyperContract {
		isVyper = 1
	}

	now := time.Now().UTC()
	q := fmt.Sprintf(`
		INSERT INTO %s
			(address, name, compiler_version, optimization, optimization_runs,
			 contract_source_code, abi, constructor_arguments, evm_version,
			 file_path, external_libraries, secondary_sources, verified_via,
			 partially_verified, is_vyper_contract, is_changed_bytecode,
			 license_type, inserted_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,0,?,?,?)
		ON CONFLICT (address) DO UPDATE SET
			name                  = excluded.name,
			compiler_version      = excluded.compiler_version,
			optimization          = excluded.optimization,
			optimization_runs     = excluded.optimization_runs,
			contract_source_code  = excluded.contract_source_code,
			abi                   = excluded.abi,
			constructor_arguments = excluded.constructor_arguments,
			evm_version           = excluded.evm_version,
			file_path             = excluded.file_path,
			external_libraries    = excluded.external_libraries,
			secondary_sources     = excluded.secondary_sources,
			verified_via          = excluded.verified_via,
			is_vyper_contract     = excluded.is_vyper_contract,
			license_type          = excluded.license_type,
			updated_at            = excluded.updated_at
	`, s.t.contracts)

	if _, err := s.db.ExecContext(r.Context(), q,
		addr, body.Name, body.CompilerVersion, optimization, optRuns,
		body.ContractSourceCode, string(abiJSON), body.ConstructorArguments, body.EVMVersion,
		body.FilePath, string(extLibsJSON), string(secSrcJSON), body.VerifiedVia,
		isVyper, body.LicenseType, now, now,
	); err != nil {
		return map[string]string{"error": "insert: " + err.Error()}, 500
	}

	// Echo back the row we just wrote via the standard formatter so the
	// SPA can flip the page state immediately without re-fetching.
	rows, err := s.q(r, fmt.Sprintf("SELECT * FROM %s WHERE address = ? LIMIT 1", s.t.contracts), addr)
	if err != nil {
		return map[string]any{"status": "verified", "address": addr}, 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	if len(maps) == 0 {
		return map[string]any{"status": "verified", "address": addr}, 200
	}
	out := formatContract(maps[0])
	out["status"] = "verified"
	return out, 200
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
	// Search the tokens table by name/symbol substring (case-insensitive)
	// when the query isn't a hash/address/block-number — covers the common
	// case of typing "USDL" or "VCC" into the search bar to jump to the
	// token-detail page. Up to 5 token hits, ordered by holder count.
	if !strings.HasPrefix(q, "0x") && s.t.tokens != "" {
		escaped := strings.NewReplacer("%", `\%`, "_", `\_`).Replace(q)
		like := "%" + strings.ToLower(escaped) + "%"
		rows, err := s.q(r, fmt.Sprintf(
			`SELECT %s AS addr_col, name, symbol FROM %s
			 WHERE LOWER(name) LIKE ? ESCAPE '\' OR LOWER(symbol) LIKE ? ESCAPE '\'
			 ORDER BY holder_count DESC, name LIMIT 5`,
			s.t.tokenAddrCol, s.t.tokens), like, like)
		if err == nil {
			defer rows.Close()
			maps, _ := scanMaps(rows)
			for _, t := range maps {
				addr := bytesToHex(t["addr_col"])
				items = append(items, map[string]any{
					"type":         "token",
					"address":      addr,
					"address_hash": addr,
					"name":         t["name"],
					"symbol":       t["symbol"],
				})
			}
		}
	}
	return paginatedResponse{Items: items}, 200
}

func (s *StandaloneServer) stats(r *http.Request) (any, int) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var bc, tc, ac int
	var totalGas, avgBlockTime float64
	var gasUsedToday int64
	s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", s.t.blocks)).Scan(&bc)
	s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", s.t.txs)).Scan(&tc)
	s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", s.t.addrs)).Scan(&ac)
	s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COALESCE(SUM(gas_used), 0) FROM %s", s.t.blocks)).Scan(&totalGas)
	// Average block time over the last 50 blocks. `timestamp` is a TEXT
	// datetime column, so subtracting two of them yields 0 in SQLite — the
	// figure was structurally zero, and the SPA printed "0s" as if blocks
	// arrived instantaneously. strftime('%s') converts to epoch seconds
	// first. The 60s ceiling drops the gaps across an indexer restart.
	s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COALESCE(AVG(dt), 0) FROM (
			SELECT CAST(strftime('%%s', timestamp) AS INTEGER)
			     - LAG(CAST(strftime('%%s', timestamp) AS INTEGER)) OVER (ORDER BY number) AS dt
			FROM %s ORDER BY number DESC LIMIT 50
		) WHERE dt > 0 AND dt < 60`, s.t.blocks)).Scan(&avgBlockTime)
	// Gas used in the last 24h. Same trap: a lexical comparison between the
	// stored timestamp format and datetime('now') matched nothing, so this
	// reported 0 gas on a chain that had been producing blocks all day.
	s.db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COALESCE(SUM(gas_used), 0) FROM %s
		 WHERE CAST(strftime('%%s', timestamp) AS INTEGER) > CAST(strftime('%%s', 'now', '-1 day') AS INTEGER)`,
		s.t.blocks)).Scan(&gasUsedToday)
	// Transactions in the same window, from the same clock.
	var txsToday int64
	haveTxsToday := false
	if s.t.txs != "" {
		haveTxsToday = s.db.QueryRowContext(ctx, fmt.Sprintf(
			`SELECT COUNT(*) FROM %s
			 WHERE CAST(strftime('%%s', timestamp) AS INTEGER) > CAST(strftime('%%s', 'now', '-1 day') AS INTEGER)`,
			s.t.txs)).Scan(&txsToday) == nil
	}
	// Gas prices: try tx gas_price first, fall back to block base_fee
	var slowGas, avgGas, fastGas float64
	s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COALESCE(MIN(CAST(gas_price AS REAL)), 0),
		       COALESCE(AVG(CAST(gas_price AS REAL)), 0),
		       COALESCE(MAX(CAST(gas_price AS REAL)), 0)
		FROM (SELECT gas_price FROM %s WHERE gas_price != '' AND gas_price != '0' ORDER BY block_number DESC LIMIT 100)`, s.t.txs)).Scan(&slowGas, &avgGas, &fastGas)
	// If no tx gas prices, compute from block base_fee (stored as hex)
	if avgGas == 0 {
		rows, _ := s.db.QueryContext(ctx, fmt.Sprintf(
			"SELECT base_fee FROM %s WHERE base_fee != '' ORDER BY number DESC LIMIT 50", s.t.blocks))
		if rows != nil {
			var fees []float64
			for rows.Next() {
				var raw string
				rows.Scan(&raw)
				if v, err := strconv.ParseInt(strings.TrimPrefix(raw, "0x"), 16, 64); err == nil && v > 0 {
					fees = append(fees, float64(v))
				}
			}
			rows.Close()
			if len(fees) > 0 {
				sort.Float64s(fees)
				slowGas = fees[0]
				fastGas = fees[len(fees)-1]
				var sum float64
				for _, f := range fees {
					sum += f
				}
				avgGas = sum / float64(len(fees))
			}
		}
	}

	var gasPrices any
	if avgGas > 0 {
		toGwei := func(wei float64) float64 { return wei / 1e9 }
		gasPrices = map[string]any{
			"slow":    map[string]any{"price": toGwei(slowGas), "time": 30000, "base_fee": toGwei(slowGas), "priority_fee": 0, "fiat_price": nil},
			"average": map[string]any{"price": toGwei(avgGas), "time": 15000, "base_fee": toGwei(avgGas), "priority_fee": 0, "fiat_price": nil},
			"fast":    map[string]any{"price": toGwei(fastGas), "time": 5000, "base_fee": toGwei(fastGas), "priority_fee": 0, "fiat_price": nil},
		}
	}

	// Network utilization (gas used / gas limit from latest block)
	var utilization float64
	s.db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT CASE WHEN gas_limit > 0 THEN CAST(gas_used AS REAL) / gas_limit * 100 ELSE 0 END FROM %s ORDER BY number DESC LIMIT 1", s.t.blocks)).Scan(&utilization)

	return map[string]any{
		"total_blocks":                   fmt.Sprintf("%d", bc),
		"total_addresses":                fmt.Sprintf("%d", ac),
		"total_transactions":             fmt.Sprintf("%d", tc),
		"average_block_time":             avgBlockTime,
		"coin_price":                     nil,
		"coin_price_change_percentage":   nil,
		"total_gas_used":                 fmt.Sprintf("%.0f", totalGas),
		"transactions_today":             countOrNull(txsToday, haveTxsToday),
		"gas_used_today":                 fmt.Sprintf("%d", gasUsedToday),
		"gas_prices":                     gasPrices,
		"gas_price_updated_at":           time.Now().UTC().Format(time.RFC3339),
		"gas_prices_update_in":           30,
		"static_gas_price":               nil,
		"market_cap":                     nil,
		"network_utilization_percentage": utilization,
		"tvl":                            nil,
	}, 200
}

// ---- Homepage Widgets ----

func (s *StandaloneServer) mainPageBlocks(r *http.Request) (any, int) {
	if s.t.blocks == "" {
		return []any{}, 200
	}
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
	if s.t.txs == "" {
		return []any{}, 200
	}
	rows, err := s.q(r, s.txSelectAll("ORDER BY block_number DESC LIMIT 6"))
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
	return map[string]any{
		"finished_indexing":                   true,
		"finished_indexing_blocks":            true,
		"indexed_blocks_ratio":                "1.00",
		"indexed_internal_transactions_ratio": "1.00",
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
	addr = strings.ToLower(addr)
	// LEFT JOIN evm_tokens so the SPA gets symbol/decimals/name in one
	// call instead of N follow-up /tokens/{addr} lookups. Filter out
	// zero-balance rows (debits that hit zero leave the row in place).
	q := fmt.Sprintf(`
		SELECT b.token_address AS token_address, b.address AS address,
		       b.token_id AS token_id, b.value AS value, b.token_type AS token_type,
		       t.name AS name, t.symbol AS symbol, t.decimals AS decimals
		FROM %s b LEFT JOIN %s t ON LOWER(t.%s) = LOWER(b.token_address)
		WHERE LOWER(b.address) = ? AND b.value != '0' AND b.value != ''
		ORDER BY CAST(b.value AS REAL) DESC LIMIT 100`,
		s.t.balances, s.t.tokens, s.t.tokenAddrCol)
	rows, err := s.q(r, q, addr)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, b := range maps {
		items[i] = map[string]any{
			"token": map[string]any{
				"address":  bytesToHex(b["token_address"]),
				"type":     b["token_type"],
				"name":     b["name"],
				"symbol":   b["symbol"],
				"decimals": fmtNum(b["decimals"]),
			},
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

// tokenCounters counts the token's holders and transfers from the rows that
// hold them. The evm_tokens.holder_count column exists but nothing ever
// writes it, so reading it reported "Holders 0" directly above a populated
// holder list; transfers_count was a literal "0" next to a populated
// transfer list. A count nobody maintains is not a count.
func (s *StandaloneServer) tokenCounters(r *http.Request) (any, int) {
	addr := r.PathValue("addr")
	if !isValidHexAddr(addr) {
		return map[string]any{"token_holders_count": nil, "transfers_count": nil}, 400
	}
	ctx := r.Context()
	return map[string]any{
		"token_holders_count": countOrNull(s.tokenHolderCount(ctx, addr)),
		"transfers_count":     countOrNull(s.tokenTransferCount(ctx, addr)),
	}, 200
}

// countOrNull renders a count that could be taken, or null for one that
// could not. Never zero — a zero here would read as "this token has no
// holders" when it means "we failed to look".
func countOrNull(n int64, ok bool) any {
	if !ok {
		return nil
	}
	return strconv.FormatInt(n, 10)
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
		rows, err := s.q(r, s.txSelectAll("WHERE from_addr = ? OR to_addr = ? ORDER BY block_number DESC LIMIT 50"), addr, addr)
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

// ================================================================
// Platform (P-Chain / linear) — blocks + validators
//
// Reads the pchain_blocks table (written by the generic chain indexer) and
// the pchain_validators/pchain_delegators tables (written by
// platform.SyncValidators). Registered only when detectTables sets
// s.t.platform; the EVM routes are untouched.
// ================================================================

const platformBlockCols = "id, parent_id, height, timestamp, status, tx_count, tx_ids"

// platformBlocks serves GET {prefix}/blocks — linear blocks newest-first.
func (s *StandaloneServer) platformBlocks(r *http.Request) (any, int) {
	l := lim(r)
	rows, err := s.q(r, fmt.Sprintf("SELECT %s FROM %s ORDER BY height DESC LIMIT ?", platformBlockCols, s.t.pblocks), l+1)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	var np any
	if len(maps) > l {
		np = map[string]any{"height": maps[l-1]["height"], "items_count": l}
		maps = maps[:l]
	}
	items := make([]map[string]any, len(maps))
	for i, b := range maps {
		items[i] = formatPlatformBlock(b)
	}
	return paginatedResponse{Items: items, NextPageParams: np}, 200
}

// platformBlock serves GET {prefix}/blocks/{id} where id is a height or CB58 id.
func (s *StandaloneServer) platformBlock(r *http.Request) (any, int) {
	id := r.PathValue("id")
	if !platformIDPattern.MatchString(id) {
		return map[string]string{"error": "invalid block id"}, 400
	}
	var (
		rows *sql.Rows
		err  error
	)
	if _, e := strconv.ParseInt(id, 10, 64); e == nil {
		rows, err = s.q(r, fmt.Sprintf("SELECT %s FROM %s WHERE height = ? LIMIT 1", platformBlockCols, s.t.pblocks), id)
	} else {
		rows, err = s.q(r, fmt.Sprintf("SELECT %s FROM %s WHERE id = ? LIMIT 1", platformBlockCols, s.t.pblocks), id)
	}
	if err != nil {
		return map[string]string{"error": "not found"}, 404
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	if len(maps) == 0 {
		return map[string]string{"error": "not found"}, 404
	}
	return formatPlatformBlock(maps[0]), 200
}

// platformMainPageBlocks serves GET {prefix}/main-page/blocks — top 6 as a raw
// array (matching the EVM widget contract).
func (s *StandaloneServer) platformMainPageBlocks(r *http.Request) (any, int) {
	rows, err := s.q(r, fmt.Sprintf("SELECT %s FROM %s ORDER BY height DESC LIMIT 6", platformBlockCols, s.t.pblocks))
	if err != nil {
		return []any{}, 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, b := range maps {
		items[i] = formatPlatformBlock(b)
	}
	return items, 200
}

// platformValidators serves GET {prefix}/validators — the current validator set
// ordered by stake, with a per-validator delegator count derived from
// pchain_delegators.
func (s *StandaloneServer) platformValidators(r *http.Request) (any, int) {
	l := lim(r)
	q := fmt.Sprintf(`SELECT v.node_id, v.start_time, v.end_time, v.stake_amount,
		v.potential_reward, v.delegation_fee, v.uptime, v.connected, v.net_id, v.tx_id,
		v.bls_public_key, v.bls_proof_of_possession,
		(SELECT COUNT(*) FROM %s d WHERE d.node_id = v.node_id) AS delegator_count
	FROM %s v
	ORDER BY CAST(v.stake_amount AS REAL) DESC
	LIMIT ?`, s.t.delegators, s.t.validators)
	rows, err := s.q(r, q, l)
	if err != nil {
		return ep(), 200
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	items := make([]map[string]any, len(maps))
	for i, v := range maps {
		items[i] = formatValidator(v)
	}
	return paginatedResponse{Items: items}, 200
}

// formatPlatformBlock shapes a pchain_blocks row for the API.
func formatPlatformBlock(b map[string]any) map[string]any {
	id := platformStr(b["id"])
	parent := platformStr(b["parent_id"])
	return map[string]any{
		"height":      toInt64(b["height"]),
		"id":          id,
		"hash":        id, // SPA block widgets key on "hash"
		"parent_id":   parent,
		"parent_hash": parent, // SPA compatibility
		"timestamp":   fmtTimestamp(b["timestamp"]),
		"status":      platformStr(b["status"]),
		"tx_count":    toInt64(b["tx_count"]),
		"tx_ids":      parsePlatformTxIDs(b["tx_ids"]),
	}
}

// formatValidator shapes a pchain_validators row (with delegator_count) for the
// API. Stake/reward stay strings to preserve full uint64 precision; uptime and
// delegation_fee are small reals passed through as numbers. BLS fields are null
// until the adapter captures a signer for that validator.
func formatValidator(v map[string]any) map[string]any {
	stake := fmtNum(v["stake_amount"])
	return map[string]any{
		"node_id":                 platformStr(v["node_id"]),
		"weight":                  stake,
		"stake":                   stake,
		"stake_amount":            stake,
		"start_time":              fmtTimestamp(v["start_time"]),
		"end_time":                fmtTimestamp(v["end_time"]),
		"uptime":                  v["uptime"],
		"potential_reward":        fmtNum(v["potential_reward"]),
		"delegation_fee":          v["delegation_fee"],
		"connected":               asBool(v["connected"]),
		"net_id":                  platformStr(v["net_id"]),
		"tx_id":                   platformStr(v["tx_id"]),
		"bls_public_key":          nullableStr(v["bls_public_key"]),
		"bls_proof_of_possession": nullableStr(v["bls_proof_of_possession"]),
		"delegator_count":         toInt64(v["delegator_count"]),
	}
}

// platformStr coerces a scanned column (string, []byte, or nil) to a string.
// Unlike bytesToHex it does not hex-encode — P-Chain ids are CB58 text.
func platformStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// nullableStr returns nil for empty/absent values so the JSON emits null rather
// than "" — used for the optional BLS signer fields.
func nullableStr(v any) any {
	if s := platformStr(v); s != "" {
		return s
	}
	return nil
}

// asBool coerces SQLite's 0/1 (int64), a real bool, or "1"/"true" to a bool.
func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	case float64:
		return t != 0
	case string:
		return t == "1" || strings.EqualFold(t, "true")
	default:
		return false
	}
}

// parsePlatformTxIDs decodes the JSON-text tx_ids column into a string slice,
// always returning a non-nil slice so the JSON is [] not null.
func parsePlatformTxIDs(v any) []string {
	raw := platformStr(v)
	if raw == "" {
		return []string{}
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return []string{}
	}
	return ids
}
