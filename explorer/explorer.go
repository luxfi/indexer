// Package explorer provides a block explorer API layer for Hanzo Base.
// It reads chain data directly from an indexer's SQLite database (read-only)
// and exposes /v1/explorer/* endpoints for explorer frontends.
//
// Architecture:
//
//	indexer → writes SQLite (WAL mode, sole writer)
//	replicate → streams WAL to S3 (age PQ encrypted)
//	base + explorer plugin → reads indexer SQLite (read-only), serves /v1/explorer/*
//	frontend → consumes /v1/explorer/*
//
// Base's own DB handles user data (accounts, API keys, watchlists).
// The indexer's DB holds chain data (blocks, txs, tokens, contracts).
// White-label: no chain-specific branding — configure via Config.
package explorer

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
	_ "github.com/mattn/go-sqlite3"
)

// Config configures the explorer plugin.
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
}

// MustRegister registers the explorer plugin and panics on failure.
func MustRegister(app core.App, config Config) {
	if err := Register(app, config); err != nil {
		panic(err)
	}
}

// Register registers the explorer plugin.
func Register(app core.App, config Config) error {
	if config.CoinDecimals == 0 {
		config.CoinDecimals = 18
	}
	if config.IndexerDBPath == "" {
		return fmt.Errorf("explorer: IndexerDBPath is required")
	}
	if config.GChainEndpoint == "" {
		config.GChainEndpoint = os.Getenv("GCHAIN_ENDPOINT")
	}
	if config.GChainEndpoint == "" {
		config.GChainEndpoint = defaultGChainEndpoint
	}

	p := &plugin{app: app, config: config}

	// Initialize G-Chain GraphQL proxy.
	proxy, err := newGraphQLProxy(config.GChainEndpoint)
	if err != nil {
		return fmt.Errorf("explorer: %w", err)
	}
	p.gchainProxy = proxy

	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		if err := p.openIndexerDB(); err != nil {
			return err
		}
		p.notifWorker = NewNotificationWorker(p.db, "transactions", app.Logger())
		p.notifWorker.Start()
		return nil
	})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		p.registerRoutes(e.Router)
		return e.Next()
	})

	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		if p.notifWorker != nil {
			p.notifWorker.Stop()
		}
		p.closeIndexerDB()
		return e.Next()
	})

	return nil
}

type plugin struct {
	app         core.App
	config      Config
	db          *sql.DB // read-only connection to indexer's SQLite
	gchainProxy *graphqlProxy
	chainDBs    map[string]*sql.DB // pre-opened cross-chain DB connections
	notifWorker *NotificationWorker
}

// openIndexerDB opens a read-only connection to the indexer's SQLite database.
func (p *plugin) openIndexerDB() error {
	dsn := fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL&_busy_timeout=5000&cache=shared", p.config.IndexerDBPath)
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

	p.db = db

	// Pre-open cross-chain DB connections for cross-chain search.
	if len(p.config.ChainDBPaths) > 0 {
		p.chainDBs = make(map[string]*sql.DB, len(p.config.ChainDBPaths))
		for chain, dbPath := range p.config.ChainDBPaths {
			cdb, err := sql.Open("sqlite3",
				fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL&_busy_timeout=5000&cache=shared", dbPath))
			if err != nil {
				p.app.Logger().Warn("explorer: failed to open cross-chain db",
					slog.String("chain", chain), slog.String("error", err.Error()))
				continue
			}
			cdb.SetMaxOpenConns(2)
			p.chainDBs[chain] = cdb
		}
	}

	p.app.Logger().Info("explorer: connected to indexer database",
		slog.String("path", p.config.IndexerDBPath),
		slog.Int64("chain_id", p.config.ChainID),
	)
	return nil
}

func (p *plugin) closeIndexerDB() {
	for _, cdb := range p.chainDBs {
		cdb.Close()
	}
	if p.db != nil {
		p.db.Close()
	}
}

func (p *plugin) registerRoutes(r *router.Router[*core.RequestEvent]) {
	v2 := r.Group("/v1/explorer")

	// Blocks
	v2.GET("/blocks", p.handleListBlocks)
	v2.GET("/blocks/{block_hash_or_number}", p.handleGetBlock)
	v2.GET("/blocks/{block_hash_or_number}/transactions", p.handleBlockTransactions)

	// Transactions
	v2.GET("/transactions", p.handleListTransactions)
	v2.GET("/transactions/{transaction_hash}", p.handleGetTransaction)
	v2.GET("/transactions/{transaction_hash}/token-transfers", p.handleTxTokenTransfers)
	v2.GET("/transactions/{transaction_hash}/internal-transactions", p.handleTxInternalTxs)
	v2.GET("/transactions/{transaction_hash}/logs", p.handleTxLogs)
	v2.GET("/transactions/{transaction_hash}/raw-trace", p.handleTxRawTrace)

	// Addresses
	v2.GET("/addresses", p.handleListAddresses)
	v2.GET("/addresses/{address_hash}", p.handleGetAddress)
	v2.GET("/addresses/{address_hash}/transactions", p.handleAddressTransactions)
	v2.GET("/addresses/{address_hash}/token-transfers", p.handleAddressTokenTransfers)
	v2.GET("/addresses/{address_hash}/internal-transactions", p.handleAddressInternalTxs)
	v2.GET("/addresses/{address_hash}/logs", p.handleAddressLogs)
	v2.GET("/addresses/{address_hash}/tokens", p.handleAddressTokens)
	v2.GET("/addresses/{address_hash}/coin-balance-history", p.handleAddressCoinBalanceHistory)
	v2.GET("/addresses/{address_hash}/coin-balance-history-by-day", p.handleAddressCoinBalanceByDay)
	v2.GET("/addresses/{address_hash}/counters", p.handleAddressCounters)

	// Tokens
	v2.GET("/tokens", p.handleListTokens)
	v2.GET("/tokens/{address_hash}", p.handleGetToken)
	v2.GET("/tokens/{address_hash}/transfers", p.handleTokenTransfers)
	v2.GET("/tokens/{address_hash}/holders", p.handleTokenHolders)
	v2.GET("/tokens/{address_hash}/instances", p.handleTokenInstances)
	v2.GET("/tokens/{address_hash}/instances/{id}", p.handleTokenInstance)

	// Smart Contracts
	v2.GET("/smart-contracts", p.handleListContracts)
	v2.GET("/smart-contracts/{address_hash}", p.handleGetContract)
	v2.POST("/smart-contracts/{address_hash}/verify", p.handleVerifyContract)

	// Search
	v2.GET("/search", p.handleSearch)
	v2.GET("/search/check-redirect", p.handleSearchRedirect)

	// Stats
	v2.GET("/stats", p.handleStats)
	v2.GET("/stats/charts/transactions", p.handleChartTransactions)
	v2.GET("/stats/charts/market", p.handleChartMarket)

	// GraphQL
	v2.Any("/graphql", p.handleFederatedGraphQL)            // G-Chain proxy (consensus-backed federated)
	v2.Any("/local/graphql", p.handleLocalGraphQL)          // local SQLite (per-chain, fast)
	v2.GET("/search/cross-chain", p.handleCrossChainSearch) // parallel multi-chain SQLite search

	// Token distribution (Gini coefficient)
	v2.GET("/tokens/{address_hash}/distribution", p.handleTokenDistribution)

	// Webhooks (notification subscriptions)
	v2.POST("/webhooks", p.handleRegisterWebhook)
	v2.GET("/webhooks", p.handleListWebhooks)
	v2.DELETE("/webhooks", p.handleDeleteWebhook)

	// Health
	r.GET("/health", p.handleHealth)

	p.app.Logger().Info("explorer plugin registered",
		slog.Int64("chain_id", p.config.ChainID),
		slog.String("chain", p.config.ChainName),
		slog.String("gchain_endpoint", p.config.GChainEndpoint),
		slog.Int("endpoints", 37),
	)
}
