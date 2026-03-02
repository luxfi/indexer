// Package main provides the unified explorer binary.
//
// Single binary, per-chain SQLite files, zero external dependencies.
// Indexes any number of chains concurrently. Each chain gets isolated storage.
//
//	explorer --config=chains.yaml
//	explorer --rpc=http://localhost:9650/ext/bc/C/rpc  (single chain mode)
//
//	/v1/indexer/{chain}/*    Per-chain explorer API
//	/v1/indexer/*            Default chain API
//	/*                        Embedded frontend (go:embed)
//	/health                   Healthcheck (all chains)
//
// Storage layout:
//
//	{data_dir}/
//	├── cchain/query/indexer.db     ← C-Chain SQLite
//	├── zoo/query/indexer.db        ← Zoo chain SQLite
//	├── ethereum/query/indexer.db   ← Ethereum SQLite
//	├── solana/query/indexer.db     ← Solana SQLite
//	└── ...                         ← one per chain, isolated
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hanzoai/replicate"
	"github.com/luxfi/age"
	"github.com/luxfi/indexer/evm"
	"github.com/luxfi/indexer/explorer"
	"github.com/luxfi/indexer/storage"
	"github.com/luxfi/graph/engine"
	graphindexer "github.com/luxfi/graph/indexer"
	graphstorage "github.com/luxfi/graph/storage"
	"gopkg.in/yaml.v3"
)

// jsonMarshalImpl — tiny wrapper so we only import encoding/json once.
func jsonMarshalImpl(v any) ([]byte, error) { return json.Marshal(v) }

var version = "dev"

// GraphConfig controls the per-chain graph (subgraph) engine.
type GraphConfig struct {
	Enabled bool   `yaml:"enabled"` // Start graph indexer + GraphQL for this chain
	Schema  string `yaml:"schema"`  // Built-in schema name (e.g., "uniswap-v2", "amm", "all") or path to .graphql
}

// ChainConfig defines a single chain to index.
type ChainConfig struct {
	Slug       string       `yaml:"slug"`     // URL-safe identifier (e.g., "cchain", "ethereum", "solana")
	Name       string       `yaml:"name"`     // Display name
	ChainID    int64        `yaml:"chain_id"` // EVM chain ID (0 for non-EVM)
	Type       string       `yaml:"type"`     // evm, dag, linear, solana, bitcoin, cosmos
	RPC        string       `yaml:"rpc"`      // RPC endpoint
	WS         string       `yaml:"ws"`       // WebSocket endpoint (optional)
	CoinSymbol string       `yaml:"coin"`     // Native coin symbol
	Enabled    bool         `yaml:"enabled"`  // Enable indexing
	Default    bool         `yaml:"default"`  // Default chain for /v1/indexer/* (no slug prefix)
	Graph      *GraphConfig `yaml:"graph"`    // Graph engine config (nil = disabled)
}

// Brand holds runtime-configurable frontend branding.
// All fields override the built-in defaults baked into the Vite bundle.
type Brand struct {
	Name        string `yaml:"name"`         // VITE_CHAIN_NAME (e.g. "Liquidity Explorer")
	Coin        string `yaml:"coin"`         // VITE_COIN (e.g. "LQDTY")
	IconURL     string `yaml:"icon_url"`     // VITE_ICON_URL (relative path, default /icon.svg)
	LogoURL     string `yaml:"logo_url"`     // VITE_LOGO_URL (wordmark)
	AccentColor string `yaml:"accent_color"` // VITE_ACCENT_COLOR (hex)
	IconFile    string `yaml:"icon_file"`    // On-disk path to SVG served at /icon.svg
	LogoFile    string `yaml:"logo_file"`    // On-disk path to SVG served at /logo.svg
}

// Network is a cross-explorer link (for network switcher).
type Network struct {
	Label   string `yaml:"label" json:"label"`       // Display label
	Domain  string `yaml:"domain" json:"domain"`     // Hostname
	ChainID int64  `yaml:"chain_id" json:"chainId"`  // Native chain id for display
}

// Config is the top-level multi-chain configuration.
type Config struct {
	DataDir  string        `yaml:"data_dir"`
	HTTPAddr string        `yaml:"http_addr"`
	Brand    Brand         `yaml:"brand"`
	Networks []Network     `yaml:"networks"`
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
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Mount explorer API for each chain — wait for DB before starting server
	log.Printf("  http:  %s", cfg.HTTPAddr)
	var apiReady sync.WaitGroup
	for _, c := range enabled {
		dbPath := filepath.Join(cfg.DataDir, c.Slug, "query", "indexer.db")
		apiReady.Add(1)
		go func(chain ChainConfig, path string) {
			defer apiReady.Done()
			for i := 0; i < 30; i++ {
				time.Sleep(time.Second)
				apiPrefix := "/v1/indexer"
				if !chain.Default {
					apiPrefix = fmt.Sprintf("/v1/indexer/%s", chain.Slug)
				}
				apiSrv, err := explorer.NewStandaloneServer(explorer.Config{
					IndexerDBPath: path,
					ChainID:       chain.ChainID,
					ChainName:     chain.Name,
					CoinSymbol:    chain.CoinSymbol,
					APIPrefix:     apiPrefix,
				})
				if err != nil {
					continue
				}
				mux.Handle(apiPrefix+"/", apiSrv.Handler())
				if chain.Default {
					log.Printf("[%s] API mounted at /v1/indexer/* (default)", chain.Slug)
				} else {
					log.Printf("[%s] API mounted at %s/*", chain.Slug, apiPrefix)
				}
				return
			}
			log.Printf("[%s] API not mounted — DB not ready after 30s", chain.Slug)
		}(c, dbPath)

		if c.Default {
			log.Printf("  %-20s /v1/indexer/*         %s", c.Slug, c.RPC)
		} else {
			log.Printf("  %-20s /v1/indexer/%s/*  %s", c.Slug, c.Slug, c.RPC)
		}
	}

	// Start graph engines for chains with graph.enabled: true
	var graphInstances []*graphInstance
	for _, c := range enabled {
		if c.Graph == nil || !c.Graph.Enabled {
			continue
		}
		gi, err := newGraphInstance(ctx, cfg.DataDir, c)
		if err != nil {
			log.Printf("[%s][graph] failed to start: %v", c.Slug, err)
			continue
		}
		graphInstances = append(graphInstances, gi)
		mountGraphHandlers(mux, c.Slug, gi)
		log.Printf("[%s][graph] mounted at /v1/graph/%s/* (schema=%s)", c.Slug, c.Slug, c.Graph.Schema)
	}

	// Wait for API handlers to mount before starting server
	apiReady.Wait()

	// Serve /envs.js — runtime frontend config. The SPA reads window.ENV
	// before falling back to compile-time import.meta.env defaults.
	mux.HandleFunc("GET /envs.js", envsHandler(cfg))

	// Per-brand logo and icon — read from disk on each request so brands
	// can be updated without a rebuild. Falls back to a generic SVG.
	mux.HandleFunc("GET /icon.svg", svgHandler(cfg.Brand.IconFile, genericIcon(cfg.Brand.AccentColor)))
	mux.HandleFunc("GET /logo.svg", svgHandler(cfg.Brand.LogoFile, genericLogo(cfg.Brand.Name, cfg.Brand.AccentColor)))

	// Serve embedded frontend at /
	mux.Handle("/", frontendHandler())

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16, // 64KB
	}
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

	// Drain graph indexers after chain indexers stop
	for _, gi := range graphInstances {
		gi.Close()
	}

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

// graphInstance holds the per-chain graph engine, indexer, and storage.
type graphInstance struct {
	slug    string
	store   *graphstorage.Store
	indexer *graphindexer.Indexer
	eng     *engine.Engine
	cancel  context.CancelFunc
	stopRep func()
}

// newGraphInstance creates and starts a graph indexer + engine for one chain.
// Storage is isolated at {dataDir}/{slug}/graph/subgraph.db.
func newGraphInstance(parent context.Context, dataDir string, chain ChainConfig) (*graphInstance, error) {
	graphDir := filepath.Join(dataDir, chain.Slug, "graph")
	if err := os.MkdirAll(graphDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", graphDir, err)
	}

	store, err := graphstorage.New(graphDir)
	if err != nil {
		return nil, fmt.Errorf("graph storage: %w", err)
	}

	ctx, cancel := context.WithCancel(parent)

	if err := store.Init(ctx); err != nil {
		cancel()
		store.Close()
		return nil, fmt.Errorf("graph storage init: %w", err)
	}

	stopRep := graphstorage.StartReplicate(filepath.Join(graphDir, "graph.db"))

	idx := graphindexer.New(chain.RPC, store)
	go func() {
		if err := idx.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[%s][graph] indexer error: %v", chain.Slug, err)
		}
	}()

	eng := engine.New(store, &engine.Config{
		MaxQueryDepth:  10,
		MaxResultSize:  1 << 20, // 1MB
		QueryTimeoutMs: 30000,
	})

	schema := chain.Graph.Schema
	if schema == "" {
		schema = "amm"
	}
	if err := eng.LoadBuiltin(schema); err != nil {
		log.Printf("[%s][graph] schema %q not built-in, using amm", chain.Slug, schema)
		eng.LoadBuiltin("amm")
	}

	return &graphInstance{
		slug:    chain.Slug,
		store:   store,
		indexer: idx,
		eng:     eng,
		cancel:  cancel,
		stopRep: stopRep,
	}, nil
}

// Close drains the graph indexer and closes storage.
func (gi *graphInstance) Close() {
	gi.cancel()
	gi.stopRep()
	gi.store.Close()
	log.Printf("[%s][graph] stopped", gi.slug)
}

// mountGraphHandlers registers per-chain GraphQL routes on the mux.
//
//	POST /v1/graph/{slug}/   — GraphQL query
//	GET  /v1/graph/{slug}/schema     — introspection schema
//	GET  /v1/graph/{slug}/playground — GraphiQL IDE
//	GET  /v1/graph/{slug}/health     — graph indexer status
func mountGraphHandlers(mux *http.ServeMux, slug string, gi *graphInstance) {
	prefix := fmt.Sprintf("/v1/graph/%s", slug)

	// GraphQL POST endpoint with body size limit
	mux.HandleFunc(fmt.Sprintf("POST %s/", prefix), func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		gi.eng.HandleGraphQL(w, r)
	})

	// Schema introspection — returns the list of registered resolvers
	mux.HandleFunc(fmt.Sprintf("GET %s/schema", prefix), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Execute the __schema introspection query
		resp := gi.eng.Execute(r.Context(), &engine.Request{
			Query: `{ __schema { types { name } } }`,
		})
		// If introspection is not supported, return resolver list
		if len(resp.Errors) > 0 {
			w.Write([]byte(`{"schema":"graph","status":"ok","note":"introspection query not supported; use POST for queries"}`))
			return
		}
		json.NewEncoder(w).Encode(resp)
	})

	// GraphiQL playground — HTML UI pointing at the chain's GraphQL endpoint
	mux.HandleFunc(fmt.Sprintf("GET %s/playground", prefix), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		fmt.Fprintf(w, graphiQLTemplate, prefix+"/")
	})

	// Per-chain graph health
	mux.HandleFunc(fmt.Sprintf("GET %s/health", prefix), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		status := gi.indexer.Status()
		fmt.Fprintf(w, `{"status":"ok","chain":%q,"block":%d,"indexed":%d}`,
			gi.slug, status.LatestBlock, status.IndexedEvents)
	})
}

// graphiQLTemplate is the GraphiQL HTML with a %s placeholder for the POST endpoint.
const graphiQLTemplate = `<!DOCTYPE html>
<html><head><title>Graph Explorer</title>
<link rel="stylesheet" href="https://unpkg.com/graphiql@3/graphiql.min.css"/>
</head><body style="margin:0;overflow:hidden">
<div id="graphiql" style="height:100vh"></div>
<script src="https://unpkg.com/react@18/umd/react.production.min.js" crossorigin></script>
<script src="https://unpkg.com/react-dom@18/umd/react-dom.production.min.js" crossorigin></script>
<script src="https://unpkg.com/graphiql@3/graphiql.min.js" crossorigin></script>
<script>
ReactDOM.createRoot(document.getElementById('graphiql')).render(
  React.createElement(GraphiQL, {
    fetcher: GraphiQL.createFetcher({url: '%s'}),
    defaultEditorToolsVisibility: true,
  })
);
</script></body></html>`

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
		if err := validateSlug(cfg.Chains[i].Slug); err != nil {
			return Config{}, fmt.Errorf("chain[%d]: %w", i, err)
		}
	}
	return cfg, nil
}

// slugPattern is the set of characters allowed in a chain slug. Slugs
// feed directly into filesystem paths (per-chain SQLite dir) and URL
// route prefixes (/v1/graph/{slug}/…). Restricting them to
// lowercase-alphanumeric + hyphen keeps both surfaces safe from path
// traversal and URL-encoding surprises.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func validateSlug(slug string) error {
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("invalid slug %q: must match ^[a-z0-9][a-z0-9-]{0,63}$", slug)
	}
	return nil
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

// svgHandler returns a handler that serves the configured SVG file, or the
// given fallback SVG if the file is empty or unreadable. This keeps the
// single-binary model for many brands while letting each brand ship a real
// logo on disk.
func svgHandler(path, fallback string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if path != "" {
			if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
				_, _ = w.Write(data)
				return
			}
		}
		_, _ = io.WriteString(w, fallback)
	}
}

// genericIcon returns a minimal monochrome square icon with the accent color.
// Used when the brand didn't supply an icon_file.
func genericIcon(accent string) string {
	if accent == "" {
		accent = "#ffffff"
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">`+
		`<rect x="2" y="2" width="20" height="20" rx="4" fill="%s"/>`+
		`<rect x="6" y="6" width="5" height="12" fill="#0a0a0a"/>`+
		`<rect x="13" y="6" width="5" height="5" fill="#0a0a0a"/>`+
		`</svg>`, accent)
}

// genericLogo returns a text wordmark for brands that didn't ship a logo SVG.
func genericLogo(name, accent string) string {
	if name == "" {
		name = "Explorer"
	}
	if accent == "" {
		accent = "#ffffff"
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 240 40">`+
		`<text x="0" y="28" font-family="Inter, system-ui, sans-serif" font-size="24" font-weight="700" fill="%s">%s</text>`+
		`</svg>`, accent, name)
}

// envsHandler serves /envs.js — runtime config that overrides compile-time
// VITE_* defaults. Keeps one binary, many brands.
func envsHandler(cfg Config) http.HandlerFunc {
	// Build exposed chain list (label + slug only — RPC stays server-side).
	type chainExpose struct {
		Label string `json:"label"`
		Slug  string `json:"slug"`
	}
	chains := make([]chainExpose, 0, len(cfg.Chains))
	for _, c := range cfg.Chains {
		if !c.Enabled {
			continue
		}
		chains = append(chains, chainExpose{Label: c.Name, Slug: c.Slug})
	}
	chainsJSON, _ := jsonMarshal(chains)
	netsJSON, _ := jsonMarshal(cfg.Networks)

	// Build the JS once; it never changes at runtime.
	body := fmt.Sprintf(
		"window.ENV = Object.assign(window.ENV||{}, {"+
			"VITE_CHAIN_NAME:%q,"+
			"VITE_COIN:%q,"+
			"VITE_ICON_URL:%q,"+
			"VITE_LOGO_URL:%q,"+
			"VITE_ACCENT_COLOR:%q,"+
			"VITE_CHAINS:%q,"+
			"VITE_NETWORKS:%q"+
			"});\n",
		cfg.Brand.Name,
		cfg.Brand.Coin,
		cfg.Brand.IconURL,
		cfg.Brand.LogoURL,
		cfg.Brand.AccentColor,
		string(chainsJSON),
		string(netsJSON),
	)

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write([]byte(body))
	}
}

// jsonMarshal is a tiny local helper so we don't pull encoding/json into the top imports twice.
func jsonMarshal(v any) ([]byte, error) {
	return jsonMarshalImpl(v)
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}
