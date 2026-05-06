package daemon

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hanzoai/replicate"
	"github.com/luxfi/age"
	"github.com/luxfi/indexer/evm"
	"github.com/luxfi/indexer/explorer"
	"github.com/luxfi/indexer/storage"
)

// RunIndexers spawns one chain-indexer goroutine per enabled chain in cfg.
// Each chain owns an isolated SQLite + ZapDB store rooted at
// {DataDir}/{slug}/. Returns a WaitGroup the caller can wait on for shutdown.
//
// SQLite WAL is replicated to S3 when REPLICATE_S3_ENDPOINT is set.
func RunIndexers(ctx context.Context, cfg Config) *sync.WaitGroup {
	var wg sync.WaitGroup
	for _, chain := range cfg.EnabledChains() {
		chain := chain
		wg.Add(1)
		go func() {
			defer wg.Done()
			runChain(ctx, cfg.DataDir, chain)
		}()
	}
	return &wg
}

// runChain starts indexing a single chain with its own isolated SQLite.
func runChain(ctx context.Context, baseDir string, chain ChainConfig) {
	chainDir := filepath.Join(baseDir, chain.Slug)
	if err := os.MkdirAll(chainDir, 0755); err != nil {
		log.Printf("[%s] failed to create data dir: %v", chain.Slug, err)
		return
	}

	store, err := storage.NewUnified(storage.DefaultUnifiedConfig(chainDir))
	if err != nil {
		log.Printf("[%s] failed to create storage: %v", chain.Slug, err)
		return
	}
	defer store.Close()

	sqliteDB := filepath.Join(chainDir, "query", "indexer.db")
	stopReplicate := startReplicate(sqliteDB, chain.Slug)
	defer stopReplicate()

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
		// Initialize the EVM schema *before* the indexing loop runs. The API
		// server polls for the schema and won't mount handlers until tables
		// exist — without this pre-init, the API for non-default chains gets
		// stuck behind a slow first RPC call and the frontend falls through
		// to the SPA placeholder.
		if err := idx.Init(ctx); err != nil {
			log.Printf("[%s] failed to init evm schema: %v", chain.Slug, err)
			return
		}
		log.Printf("[%s] evm schema initialized", chain.Slug)
		if err := idx.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[%s] indexer error: %v", chain.Slug, err)
		}

	case "dag", "linear", "solana", "bitcoin", "cosmos", "move", "near", "tron", "ton", "substrate":
		log.Printf("[%s] %s indexer running", chain.Slug, chain.Type)
		<-ctx.Done()

	default:
		log.Printf("[%s] unknown chain type: %s", chain.Slug, chain.Type)
	}
}

// startReplicate streams a chain's SQLite WAL to S3, PQ-encrypted via luxfi/age.
// S3 path includes the chain slug for isolation: {prefix}/{slug}/.
// Returns a no-op stop function when REPLICATE_S3_ENDPOINT is not set.
func startReplicate(dbPath, slug string) func() {
	endpoint := os.Getenv("REPLICATE_S3_ENDPOINT")
	if endpoint == "" {
		return func() {}
	}

	bucket := EnvOr("REPLICATE_S3_BUCKET", "replicate")
	prefix := os.Getenv("REPLICATE_S3_PATH")
	if prefix == "" {
		prefix, _ = os.Hostname()
	}
	prefix = prefix + "/" + slug
	region := EnvOr("REPLICATE_S3_REGION", "us-central1")

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

// MountIndexerAPI mounts /v1/indexer/* HTTP handlers for every enabled chain.
// It blocks until every chain's per-chain SQLite is ready (DB file exists and,
// for EVM chains, the evm_blocks table is created). Concurrent — each chain
// is mounted in its own goroutine. Returns when all are mounted or ctx is done.
//
// Routes:
//   - /v1/indexer/*            (default chain)
//   - /v1/indexer/{slug}/*     (every chain, including the default chain alias)
//   - /v1/explorer/{slug}/*    (legacy alias for non-default chains)
func MountIndexerAPI(ctx context.Context, cfg Config, mux *http.ServeMux) {
	var apiReady sync.WaitGroup
	for _, c := range cfg.EnabledChains() {
		c := c
		dbPath := filepath.Join(cfg.DataDir, c.Slug, "query", "indexer.db")
		apiReady.Add(1)
		go func() {
			defer apiReady.Done()
			mountChainAPI(ctx, mux, c, dbPath)
		}()

		if c.Default {
			log.Printf("  %-20s /v1/indexer/*         %s", c.Slug, c.RPC)
		} else {
			log.Printf("  %-20s /v1/indexer/%s/*  %s", c.Slug, c.Slug, c.RPC)
		}
	}
	apiReady.Wait()
}

// mountChainAPI waits for the chain's DB + EVM schema to be ready, then mounts
// every variant of the explorer API handler the unified deploy expects.
func mountChainAPI(ctx context.Context, mux *http.ServeMux, chain ChainConfig, path string) {
	apiPrefix := "/v1/indexer"
	if !chain.Default {
		apiPrefix = fmt.Sprintf("/v1/indexer/%s", chain.Slug)
	}

	var lastErr error
	deadline := time.Now().Add(5 * time.Minute)
	for attempt := 0; ; attempt++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
		if _, err := os.Stat(path); err != nil {
			lastErr = fmt.Errorf("db file not yet created: %w", err)
			if time.Now().After(deadline) {
				log.Printf("[%s] API mount waiting on DB file %s (attempt %d): %v", chain.Slug, path, attempt, lastErr)
				deadline = time.Now().Add(5 * time.Minute)
			}
			continue
		}
		// EVM-typed chains use evm_blocks; non-EVM chains skip this check.
		// The standalone server caches table names on construction, so we wait
		// for the schema to settle before instantiating it.
		if chain.Type == "evm" {
			db, openErr := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", path))
			if openErr == nil {
				var c int
				err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='evm_blocks'").Scan(&c)
				db.Close()
				if err != nil || c == 0 {
					lastErr = fmt.Errorf("evm_blocks table not yet created (count=%d, err=%v)", c, err)
					if time.Now().After(deadline) {
						log.Printf("[%s] API mount waiting on evm_blocks (attempt %d): %v", chain.Slug, attempt, lastErr)
						deadline = time.Now().Add(5 * time.Minute)
					}
					continue
				}
			}
		}
		apiSrv, err := explorer.NewStandaloneServer(explorer.Config{
			IndexerDBPath: path,
			ChainID:       chain.ChainID,
			ChainName:     chain.Name,
			CoinSymbol:    chain.CoinSymbol,
			APIPrefix:     apiPrefix,
		})
		if err != nil {
			lastErr = err
			if time.Now().After(deadline) {
				log.Printf("[%s] API mount failed (attempt %d): %v", chain.Slug, attempt, err)
				deadline = time.Now().Add(5 * time.Minute)
			}
			continue
		}
		mux.Handle(apiPrefix+"/", apiSrv.Handler())

		// Legacy /v1/explorer/{slug}/* alias for older clients.
		legacyPrefix := strings.Replace(apiPrefix, "/v1/indexer", "/v1/explorer", 1)
		if legacyPrefix != apiPrefix {
			legacySrv, _ := explorer.NewStandaloneServer(explorer.Config{
				IndexerDBPath: path,
				ChainID:       chain.ChainID,
				ChainName:     chain.Name,
				CoinSymbol:    chain.CoinSymbol,
				APIPrefix:     legacyPrefix,
			})
			if legacySrv != nil {
				mux.Handle(legacyPrefix+"/", legacySrv.Handler())
			}
		}
		// Default chain also mounts under its slug so /v1/indexer/cchain/*
		// works alongside /v1/indexer/*. Hostname-based ingress can pin a
		// frontend to a specific chain.
		if chain.Default {
			slugSrv, _ := explorer.NewStandaloneServer(explorer.Config{
				IndexerDBPath: path,
				ChainID:       chain.ChainID,
				ChainName:     chain.Name,
				CoinSymbol:    chain.CoinSymbol,
				APIPrefix:     fmt.Sprintf("/v1/indexer/%s", chain.Slug),
			})
			if slugSrv != nil {
				mux.Handle(fmt.Sprintf("/v1/indexer/%s/", chain.Slug), slugSrv.Handler())
			}
			log.Printf("[%s] API mounted at /v1/indexer/* and /v1/indexer/%s/* (default)", chain.Slug, chain.Slug)
		} else {
			log.Printf("[%s] API mounted at %s/*", chain.Slug, apiPrefix)
		}
		return
	}
}
