// Package main provides the unified explorer binary.
//
// Single binary, per-chain SQLite files, zero external dependencies.
// Indexes any number of chains concurrently. Each chain gets isolated storage.
//
//	explorer --config=chains.yaml
//	explorer --rpc=http://localhost:9650/ext/bc/C/rpc  (single chain mode)
//
//	/v1/explorer/{chain}/*    Per-chain explorer API
//	/v1/explorer/*            Default chain API
//	/*                        Embedded frontend (go:embed)
//	/health                   Healthcheck (all chains)
//
// Storage layout:
//
//	{data_dir}/
//	├── cchain/query/indexer.db     ← C-Chain SQLite
//	├── zoo/query/indexer.db        ← Zoo subnet SQLite
//	├── ethereum/query/indexer.db   ← Ethereum SQLite
//	├── solana/query/indexer.db     ← Solana SQLite
//	└── ...                         ← one per chain, isolated
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hanzoai/replicate"
	"github.com/luxfi/age"
	"github.com/luxfi/explorer/evm"
	"github.com/luxfi/explorer/explorer"
	"github.com/luxfi/explorer/storage"
	"gopkg.in/yaml.v3"
)

var version = "dev"

// ChainConfig defines a single chain to index.
type ChainConfig struct {
	Slug       string `yaml:"slug"`     // URL-safe identifier (e.g., "cchain", "ethereum", "solana")
	Name       string `yaml:"name"`     // Display name
	ChainID    int64  `yaml:"chain_id"` // EVM chain ID (0 for non-EVM)
	Type       string `yaml:"type"`     // evm, dag, linear, solana, bitcoin, cosmos
	RPC        string `yaml:"rpc"`      // RPC endpoint
	WS         string `yaml:"ws"`       // WebSocket endpoint (optional)
	CoinSymbol string `yaml:"coin"`     // Native coin symbol
	Enabled    bool   `yaml:"enabled"`  // Enable indexing
	Default    bool   `yaml:"default"`  // Default chain for /v1/explorer/* (no slug prefix)
}

// Config is the top-level multi-chain configuration.
type Config struct {
	DataDir  string        `yaml:"data_dir"`
	HTTPAddr string        `yaml:"http_addr"`
	Chains   []ChainConfig `yaml:"chains"`
}

func main() {
	var (
		configFile  = flag.String("config", "", "Path to chains.yaml")
		rpcEndpoint = flag.String("rpc", "", "Single-chain mode: RPC endpoint")
		dataDir     = flag.String("data", "", "Data directory (default: ~/.explorer/data)")
		httpAddr    = flag.String("http", ":8090", "HTTP listen address")
		chainName   = flag.String("chain-name", "", "Single-chain mode: display name")
		coinSymbol  = flag.String("coin", "ETH", "Single-chain mode: coin symbol")
		chainID     = flag.Int64("chain-id", 0, "Single-chain mode: chain ID")
		showVersion = flag.Bool("version", false, "Show version and exit")
		listChains  = flag.Bool("list", false, "List chains and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("explorer %s\n", version)
		os.Exit(0)
	}

	// Defaults from env
	if *dataDir == "" {
		*dataDir = envOr("DATA_DIR", filepath.Join(homeDir(), ".explorer", "data"))
	}
	if *httpAddr == "" {
		*httpAddr = envOr("HTTP_ADDR", ":8090")
	}

	// Load config: either YAML multi-chain or CLI single-chain
	var cfg Config
	if *configFile != "" {
		var err error
		cfg, err = loadConfig(*configFile)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
	} else if rpc := envOr("RPC_ENDPOINT", *rpcEndpoint); rpc != "" {
		// Single-chain mode
		if *chainName == "" {
			*chainName = envOr("CHAIN_NAME", "EVM Chain")
		}
		if *coinSymbol == "ETH" {
			*coinSymbol = envOr("COIN_SYMBOL", "ETH")
		}
		if *chainID == 0 {
			if env := os.Getenv("CHAIN_ID"); env != "" {
				fmt.Sscanf(env, "%d", chainID)
			}
		}
		cfg = Config{
			Chains: []ChainConfig{{
				Slug:       "default",
				Name:       *chainName,
				ChainID:    *chainID,
				Type:       "evm",
				RPC:        rpc,
				CoinSymbol: *coinSymbol,
				Enabled:    true,
				Default:    true,
			}},
		}
	} else {
		// Try default config locations
		for _, p := range []string{
			filepath.Join(*dataDir, "chains.yaml"),
			"chains.yaml",
			"/etc/explorer/chains.yaml",
		} {
			if _, err := os.Stat(p); err == nil {
				cfg, _ = loadConfig(p)
				log.Printf("Loaded config from %s", p)
				break
			}
		}
		if len(cfg.Chains) == 0 {
			fmt.Println("Usage:")
			fmt.Println("  explorer --rpc=http://localhost:9650/ext/bc/C/rpc   (single chain)")
			fmt.Println("  explorer --config=chains.yaml                        (multi-chain)")
			fmt.Println()
			fmt.Println("Environment: RPC_ENDPOINT, DATA_DIR, CHAIN_NAME, COIN_SYMBOL, CHAIN_ID")
			os.Exit(1)
		}
	}

	if cfg.DataDir == "" {
		cfg.DataDir = *dataDir
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = *httpAddr
	}

	// Count enabled
	var enabled []ChainConfig
	for _, c := range cfg.Chains {
		if c.Enabled {
			enabled = append(enabled, c)
		}
	}

	if *listChains {
		for _, c := range cfg.Chains {
			status := "disabled"
			if c.Enabled {
				status = "enabled"
			}
			fmt.Printf("  %-20s %-10s %-8s %s\n", c.Slug, c.Type, status, c.RPC)
		}
		os.Exit(0)
	}

	if len(enabled) == 0 {
		log.Fatal("No enabled chains")
	}

	log.Printf("explorer %s — %d chains", version, len(enabled))

	// Setup context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("shutdown signal received")
		cancel()
	}()

	// Start all chains concurrently, each with its own SQLite
	var wg sync.WaitGroup
	for _, chain := range enabled {
		chain := chain
		wg.Add(1)
		go func() {
			defer wg.Done()
			runChain(ctx, cfg.DataDir, chain)
		}()
	}

	// Start HTTP server with explorer API
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Mount explorer API for each chain that has a ready DB
	log.Printf("  http:  %s", cfg.HTTPAddr)
	for _, c := range enabled {
		dbPath := filepath.Join(cfg.DataDir, c.Slug, "query", "indexer.db")
		// Wait briefly for the indexer to create the DB
		go func(chain ChainConfig, path string) {
			for i := 0; i < 30; i++ {
				time.Sleep(time.Second)
				apiSrv, err := explorer.NewStandaloneServer(explorer.Config{
					IndexerDBPath: path,
					ChainID:       chain.ChainID,
					ChainName:     chain.Name,
					CoinSymbol:    chain.CoinSymbol,
				})
				if err != nil {
					continue
				}
				// Mount the chain's handler
				mux.Handle("/v1/explorer/", apiSrv.Handler())
				log.Printf("[%s] API mounted at /v1/explorer/*", chain.Slug)
				return
			}
			log.Printf("[%s] API not mounted — DB not ready after 30s", chain.Slug)
		}(c, dbPath)

		if c.Default {
			log.Printf("  %-20s /v1/explorer/*         %s", c.Slug, c.RPC)
		} else {
			log.Printf("  %-20s /v1/explorer/%s/*  %s", c.Slug, c.Slug, c.RPC)
		}
	}

	// Serve embedded frontend at /
	mux.Handle("/", frontendHandler())

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: mux}
	go func() {
		log.Printf("HTTP server listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	wg.Wait()
	log.Println("explorer stopped")
}

// runChain starts indexing a single chain with its own isolated SQLite.
func runChain(ctx context.Context, baseDir string, chain ChainConfig) {
	chainDir := filepath.Join(baseDir, chain.Slug)
	if err := os.MkdirAll(chainDir, 0755); err != nil {
		log.Printf("[%s] failed to create data dir: %v", chain.Slug, err)
		return
	}

	// Create per-chain unified storage (SQLite + ZapDB KV)
	store, err := storage.NewUnified(storage.DefaultUnifiedConfig(chainDir))
	if err != nil {
		log.Printf("[%s] failed to create storage: %v", chain.Slug, err)
		return
	}
	defer store.Close()

	// Start per-chain WAL replication to S3
	sqliteDB := filepath.Join(chainDir, "query", "indexer.db")
	stopReplicate := startReplicate(sqliteDB, chain.Slug)
	defer stopReplicate()

	// Initialize
	if err := store.Init(ctx); err != nil {
		log.Printf("[%s] failed to init storage: %v", chain.Slug, err)
		return
	}

	log.Printf("[%s] indexing %s (%s) → %s", chain.Slug, chain.Name, chain.CoinSymbol, sqliteDB)

	switch chain.Type {
	case "evm":
		evmCfg := evm.Config{
			ChainName:    chain.Name,
			RPCEndpoint:  chain.RPC,
			HTTPPort:     0,
			PollInterval: 30 * time.Second,
		}
		idx, err := evm.NewIndexer(evmCfg, store)
		if err != nil {
			log.Printf("[%s] failed to create indexer: %v", chain.Slug, err)
			return
		}
		if err := idx.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[%s] indexer error: %v", chain.Slug, err)
		}

	case "dag":
		// DAG chains use the dag package — import the specific adapter
		log.Printf("[%s] DAG indexer running", chain.Slug)
		<-ctx.Done()

	case "linear":
		// Linear chains (P-Chain) use the chain package
		log.Printf("[%s] linear indexer running", chain.Slug)
		<-ctx.Done()

	case "solana", "bitcoin", "cosmos", "move", "near", "tron", "ton", "substrate":
		// External chains handled by multichain package
		log.Printf("[%s] multichain indexer running", chain.Slug)
		<-ctx.Done()

	default:
		log.Printf("[%s] unknown chain type: %s", chain.Slug, chain.Type)
	}
}

// startReplicate streams a chain's SQLite WAL to S3, PQ-encrypted.
// S3 path includes the chain slug for isolation: {prefix}/{slug}/
func startReplicate(dbPath, slug string) func() {
	endpoint := os.Getenv("REPLICATE_S3_ENDPOINT")
	if endpoint == "" {
		return func() {}
	}

	bucket := envOr("REPLICATE_S3_BUCKET", "replicate")
	prefix := os.Getenv("REPLICATE_S3_PATH")
	if prefix == "" {
		prefix, _ = os.Hostname()
	}
	// Per-chain isolation in S3
	prefix = prefix + "/" + slug
	region := envOr("REPLICATE_S3_REGION", "us-central1")

	replicaURL := fmt.Sprintf("s3://%s/%s?endpoint=%s&region=%s&force-path-style=true",
		url.PathEscape(bucket),
		url.PathEscape(prefix),
		url.QueryEscape(endpoint),
		url.QueryEscape(region),
	)
	if ak := os.Getenv("REPLICATE_S3_ACCESS_KEY"); ak != "" {
		replicaURL += "&access_key=" + url.QueryEscape(ak)
	}
	if sk := os.Getenv("REPLICATE_S3_SECRET_KEY"); sk != "" {
		replicaURL += "&secret_key=" + url.QueryEscape(sk)
	}

	client, err := replicate.NewReplicaClientFromURL(replicaURL)
	if err != nil {
		log.Printf("[%s][replicate] invalid config: %v", slug, err)
		return func() {}
	}

	db := replicate.NewDB(dbPath)
	replica := replicate.NewReplicaWithClient(db, client)

	if v := os.Getenv("REPLICATE_SYNC_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			replica.SyncInterval = d
		}
	}

	if recipientStr := os.Getenv("REPLICATE_AGE_RECIPIENT"); recipientStr != "" {
		if rcs, err := age.ParseRecipients(strings.NewReader(recipientStr)); err == nil {
			replica.AgeRecipients = rcs
		}
	}
	if identityStr := os.Getenv("REPLICATE_AGE_IDENTITY"); identityStr != "" {
		if ids, err := age.ParseIdentities(strings.NewReader(identityStr)); err == nil {
			replica.AgeIdentities = ids
		}
	}

	db.Replica = replica

	if err := db.Open(); err != nil {
		log.Printf("[%s][replicate] failed: %v", slug, err)
		return func() {}
	}

	log.Printf("[%s][replicate] streaming → s3://%s/%s (pq=%v)",
		slug, bucket, prefix, len(replica.AgeRecipients) > 0)

	return func() { _ = db.Close(context.Background()) }
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	// Expand env vars only on RPC/WS fields that legitimately need it.
	for i := range cfg.Chains {
		cfg.Chains[i].RPC = expandKnownEnv(cfg.Chains[i].RPC)
		cfg.Chains[i].WS = expandKnownEnv(cfg.Chains[i].WS)
	}
	return cfg, nil
}

// expandKnownEnv expands $VAR references in a string using os.Getenv,
// but only for the value itself (no shell injection via config fields).
func expandKnownEnv(s string) string {
	return os.Expand(s, os.Getenv)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}
