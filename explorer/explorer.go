// Package explorer provides a block explorer API layer.
// It reads chain data directly from an indexer's SQLite database (read-only)
// and exposes /v1/explorer/* endpoints for explorer frontends.
//
// Architecture:
//
//	indexer → writes SQLite (WAL mode, sole writer)
//	replicate → streams WAL to S3 (age PQ encrypted)
//	Service → reads indexer SQLite (read-only), serves /v1/explorer/*
//	frontend → consumes /v1/explorer/*
//
// The indexer's DB holds chain data (blocks, txs, tokens, contracts).
// White-label: no chain-specific branding — configure via Config.
package explorer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	_ "github.com/mattn/go-sqlite3"
)

// Config configures the explorer service.
type Config struct {
	// IndexerDBPath is the path to the indexer's SQLite database.
	// Example: ~/.lux/indexer/cchain/query/indexer.db
	IndexerDBPath string

	// ChainID is the chain this explorer instance serves (e.g., 96369 for mainnet C-Chain).
	ChainID int64

	// ChainName is the display name (e.g., "Lux C-Chain").
	ChainName string

	// CoinSymbol is the native coin symbol (e.g., "LUX").
	CoinSymbol string

	// CoinDecimals is the native coin decimals (default: 18).
	CoinDecimals int

	// GChainEndpoint is the G-Chain GraphQL endpoint on the node.
	// Default: http://localhost:9650/ext/bc/G/graphql
	// Set via GCHAIN_ENDPOINT env var.
	GChainEndpoint string

	// ChainDBPaths maps chain names to their SQLite DB paths for cross-chain search.
	// Example: {"C": "/data/cchain/indexer.db", "Zoo": "/data/zoo/indexer.db"}
	// When empty, cross-chain search only queries the local IndexerDBPath.
	ChainDBPaths map[string]string

	// APIPrefix is the URL prefix for all API routes (default: "/v1/explorer").
	APIPrefix string
}

// Service owns the state for explorer HTTP handlers.
// No DI framework — construct with NewService.
type Service struct {
	config      Config
	db          *sql.DB // read-only connection to indexer's SQLite
	gchainProxy *graphqlProxy
	chainDBs    map[string]*sql.DB // pre-opened cross-chain DB connections
	notifWorker *NotificationWorker
	logger      *slog.Logger
}

// NewService creates a Service. It opens the indexer DB and starts the notification worker.
// Call Close() when done.
func NewService(config Config, logger *slog.Logger) (*Service, error) {
	if config.CoinDecimals == 0 {
		config.CoinDecimals = 18
	}
	if config.IndexerDBPath == "" {
		return nil, fmt.Errorf("explorer: IndexerDBPath is required")
	}
	if config.GChainEndpoint == "" {
		config.GChainEndpoint = os.Getenv("GCHAIN_ENDPOINT")
	}
	if config.GChainEndpoint == "" {
		config.GChainEndpoint = defaultGChainEndpoint
	}
	if logger == nil {
		logger = slog.Default()
	}

	s := &Service{config: config, logger: logger}

	// Initialize G-Chain GraphQL proxy.
	proxy, err := newGraphQLProxy(config.GChainEndpoint)
	if err != nil {
		return nil, fmt.Errorf("explorer: %w", err)
	}
	s.gchainProxy = proxy

	if err := s.openIndexerDB(); err != nil {
		return nil, err
	}
	s.notifWorker = NewNotificationWorker(s.db, "transactions", logger)
	s.notifWorker.Start()

	return s, nil
}

// Close shuts down the notification worker and closes all DB connections.
func (s *Service) Close() {
	if s.notifWorker != nil {
		s.notifWorker.Stop()
	}
	s.closeIndexerDB()
}

// openIndexerDB opens a read-only connection to the indexer's SQLite database.
func (s *Service) openIndexerDB() error {
	dsn := fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL&_busy_timeout=5000&cache=shared", s.config.IndexerDBPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return fmt.Errorf("explorer: failed to open indexer db: %w", err)
	}

	// Read-only: allow many concurrent readers.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)

	if err := db.Ping(); err != nil {
		db.Close()
		return fmt.Errorf("explorer: indexer db not ready: %w", err)
	}

	s.db = db

	// Pre-open cross-chain DB connections for cross-chain search.
	if len(s.config.ChainDBPaths) > 0 {
		s.chainDBs = make(map[string]*sql.DB, len(s.config.ChainDBPaths))
		for chain, dbPath := range s.config.ChainDBPaths {
			cdb, err := sql.Open("sqlite3",
				fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL&_busy_timeout=5000&cache=shared", dbPath))
			if err != nil {
				s.logger.Warn("explorer: failed to open cross-chain db",
					slog.String("chain", chain), slog.String("error", err.Error()))
				continue
			}
			cdb.SetMaxOpenConns(2)
			s.chainDBs[chain] = cdb
		}
	}

	s.logger.Info("explorer: connected to indexer database",
		slog.String("path", s.config.IndexerDBPath),
		slog.Int64("chain_id", s.config.ChainID),
	)
	return nil
}

func (s *Service) closeIndexerDB() {
	for _, cdb := range s.chainDBs {
		cdb.Close()
	}
	if s.db != nil {
		s.db.Close()
	}
}

// RegisterRoutes mounts all explorer endpoints on the given chi.Router.
func RegisterRoutes(mux chi.Router, svc *Service) {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	// Blocks
	r.Get("/blocks", svc.handleListBlocks)
	r.Get("/blocks/{block_hash_or_number}", svc.handleGetBlock)
	r.Get("/blocks/{block_hash_or_number}/transactions", svc.handleBlockTransactions)

	// Transactions
	r.Get("/transactions", svc.handleListTransactions)
	r.Get("/transactions/{transaction_hash}", svc.handleGetTransaction)
	r.Get("/transactions/{transaction_hash}/token-transfers", svc.handleTxTokenTransfers)
	r.Get("/transactions/{transaction_hash}/internal-transactions", svc.handleTxInternalTxs)
	r.Get("/transactions/{transaction_hash}/logs", svc.handleTxLogs)
	r.Get("/transactions/{transaction_hash}/raw-trace", svc.handleTxRawTrace)

	// Addresses
	r.Get("/addresses", svc.handleListAddresses)
	r.Get("/addresses/{address_hash}", svc.handleGetAddress)
	r.Get("/addresses/{address_hash}/transactions", svc.handleAddressTransactions)
	r.Get("/addresses/{address_hash}/token-transfers", svc.handleAddressTokenTransfers)
	r.Get("/addresses/{address_hash}/internal-transactions", svc.handleAddressInternalTxs)
	r.Get("/addresses/{address_hash}/logs", svc.handleAddressLogs)
	r.Get("/addresses/{address_hash}/tokens", svc.handleAddressTokens)
	r.Get("/addresses/{address_hash}/coin-balance-history", svc.handleAddressCoinBalanceHistory)
	r.Get("/addresses/{address_hash}/coin-balance-history-by-day", svc.handleAddressCoinBalanceByDay)
	r.Get("/addresses/{address_hash}/counters", svc.handleAddressCounters)

	// Tokens
	r.Get("/tokens", svc.handleListTokens)
	r.Get("/tokens/{address_hash}", svc.handleGetToken)
	r.Get("/tokens/{address_hash}/transfers", svc.handleTokenTransfers)
	r.Get("/tokens/{address_hash}/holders", svc.handleTokenHolders)
	r.Get("/tokens/{address_hash}/instances", svc.handleTokenInstances)
	r.Get("/tokens/{address_hash}/instances/{id}", svc.handleTokenInstance)

	// Smart Contracts
	r.Get("/smart-contracts", svc.handleListContracts)
	r.Get("/smart-contracts/{address_hash}", svc.handleGetContract)
	r.Post("/smart-contracts/{address_hash}/verify", svc.handleVerifyContract)

	// Search
	r.Get("/search", svc.handleSearch)
	r.Get("/search/check-redirect", svc.handleSearchRedirect)

	// Stats
	r.Get("/stats", svc.handleStats)
	r.Get("/stats/charts/transactions", svc.handleChartTransactions)
	r.Get("/stats/charts/market", svc.handleChartMarket)

	// GraphQL — GET serves playground, POST executes queries. Reject other methods.
	r.Get("/graphql", svc.handleFederatedGraphQL)
	r.Post("/graphql", svc.handleFederatedGraphQL)
	r.Get("/local/graphql", svc.handleLocalGraphQL)
	r.Post("/local/graphql", svc.handleLocalGraphQL)
	r.Get("/search/cross-chain", svc.handleCrossChainSearch)

	// Token distribution (Gini coefficient)
	r.Get("/tokens/{address_hash}/distribution", svc.handleTokenDistribution)

	// Webhooks (notification subscriptions)
	r.Post("/webhooks", svc.handleRegisterWebhook)
	r.Get("/webhooks", svc.handleListWebhooks)
	r.Delete("/webhooks", svc.handleDeleteWebhook)

	mux.Mount("/v1/explorer", r)

	// Health at root
	mux.Get("/health", svc.handleHealth)

	svc.logger.Info("explorer routes registered",
		slog.Int64("chain_id", svc.config.ChainID),
		slog.String("chain", svc.config.ChainName),
		slog.String("gchain_endpoint", svc.config.GChainEndpoint),
		slog.Int("endpoints", 37),
	)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// writeHTML writes an HTML response with the given status code.
func writeHTML(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	w.Write([]byte(body))
}

// apiError writes a JSON error matching the old base/core ApiError shape:
// {"status": <code>, "message": "<Msg.>", "data": {}}
func apiError(w http.ResponseWriter, code int, msg string) {
	// Sentence-case to match old inflector.Sentenize behavior.
	if len(msg) > 0 {
		msg = strings.ToUpper(msg[:1]) + msg[1:]
		if msg[len(msg)-1] != '.' {
			msg += "."
		}
	}
	writeJSON(w, code, map[string]any{
		"status":  code,
		"message": msg,
		"data":    map[string]any{},
	})
}

// notFoundError writes a standard JSON 404 response.
func notFoundError(w http.ResponseWriter, msg string) {
	apiError(w, http.StatusNotFound, msg)
}

// badRequestError writes a standard JSON 400 response.
func badRequestError(w http.ResponseWriter, msg string) {
	apiError(w, http.StatusBadRequest, msg)
}
