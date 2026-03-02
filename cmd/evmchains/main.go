// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package main runs multiple EVM chain indexers in a single process.
// Each chain gets its own Blockscout-compatible API server on a dedicated port,
// while all chains share one PostgreSQL database using table prefixes.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"gopkg.in/yaml.v3"

	"github.com/luxfi/indexer/evm/api"
)

var version = "dev"

// ChainDef defines a single EVM chain to index.
type ChainDef struct {
	Slug     string `yaml:"slug"`
	Name     string `yaml:"name"`
	ChainID  int64  `yaml:"chain_id"`
	Port     int    `yaml:"port"`
	RPC      string `yaml:"rpc"`
	Enabled  bool   `yaml:"enabled"`
}

// Config is the top-level config for the multi-chain EVM indexer.
type Config struct {
	DatabaseURL string     `yaml:"database_url"`
	Chains      []ChainDef `yaml:"chains"`
}

func main() {
	var (
		configFile  = flag.String("config", "", "Path to evmchains.yaml config file")
		dbURL       = flag.String("db", "", "PostgreSQL connection URL (overrides config)")
		showVersion = flag.Bool("version", false, "Show version and exit")
		listChains  = flag.Bool("list", false, "List configured chains and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("lux-evmchains %s\n", version)
		os.Exit(0)
	}

	cfg, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if *listChains {
		printChains(cfg)
		os.Exit(0)
	}

	// CLI flag overrides config
	if *dbURL != "" {
		cfg.DatabaseURL = *dbURL
	}
	// Env var overrides config if not set by flag
	if cfg.DatabaseURL == "" {
		cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	}
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required (set via --db flag, config file, or env var)")
	}

	// Count enabled chains
	var enabled int
	for _, c := range cfg.Chains {
		if c.Enabled {
			enabled++
		}
	}
	if enabled == 0 {
		log.Fatal("No enabled chains in config")
	}

	// Connect to PostgreSQL (shared across all chains)
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(enabled * 5)
	db.SetMaxIdleConns(enabled * 2)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	log.Printf("Connected to PostgreSQL (max_conns=%d)", enabled*5)

	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, shutting down...", sig)
		cancel()
	}()

	// Start each enabled chain's API server
	var wg sync.WaitGroup
	for _, chain := range cfg.Chains {
		if !chain.Enabled {
			log.Printf("[%s] Skipping (disabled)", chain.Slug)
			continue
		}

		wg.Add(1)
		go func(c ChainDef) {
			defer wg.Done()
			runChain(ctx, db, c)
		}(chain)
	}

	// Start management HTTP server on port 9000
	go startManagementServer(ctx, cfg)

	log.Printf("Started %d EVM chain indexers", enabled)

	// Wait for shutdown
	<-ctx.Done()
	log.Println("Waiting for all chain servers to stop...")
	wg.Wait()
	log.Println("All chain servers stopped")
}

// runChain starts a Blockscout-compatible API server for a single chain.
func runChain(ctx context.Context, db *sql.DB, chain ChainDef) {
	log.Printf("[%s] Starting API server on port %d (chain_id=%d, rpc=%s)",
		chain.Slug, chain.Port, chain.ChainID, chain.RPC)

	// Ensure all required tables exist for this chain prefix
	if err := api.EnsureTables(db, chain.Slug); err != nil {
		log.Printf("[%s] Failed to ensure tables: %v", chain.Slug, err)
		return
	}

	cfg := api.Config{
		HTTPPort:    chain.Port,
		ChainID:     chain.ChainID,
		ChainName:   chain.Name,
		ChainSlug:   chain.Slug,
		RPCEndpoint: chain.RPC,
	}

	server := api.NewServer(cfg, db)

	if err := server.Run(ctx); err != nil && ctx.Err() == nil {
		log.Printf("[%s] API server error: %v", chain.Slug, err)
	}

	log.Printf("[%s] API server stopped", chain.Slug)
}

// loadConfig loads the YAML config, searching default paths if none given.
func loadConfig(path string) (*Config, error) {
	if path == "" {
		// Try default locations
		defaults := []string{
			"configs/evmchains.yaml",
			"config/evmchains.yaml",
			"/etc/lux/indexer/evmchains.yaml",
		}
		for _, p := range defaults {
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			}
		}
	}

	if path == "" {
		return defaultConfig(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	log.Printf("Loaded config from %s (%d chains)", path, len(cfg.Chains))
	return &cfg, nil
}

// defaultConfig returns the 5 Lux mainnet EVM chains.
func defaultConfig() *Config {
	return &Config{
		Chains: []ChainDef{
			{Slug: "cchain", Name: "Lux C-Chain", ChainID: 96369, Port: 4000, RPC: "http://luxd-headless.lux-mainnet.svc:9630/ext/bc/C/rpc", Enabled: true},
			{Slug: "zoo", Name: "Zoo", ChainID: 200200, Port: 5000, RPC: "http://luxd-headless.lux-mainnet.svc:9630/ext/bc/Zoo/rpc", Enabled: true},
			{Slug: "hanzo", Name: "Hanzo", ChainID: 36963, Port: 5100, RPC: "http://luxd-headless.lux-mainnet.svc:9630/ext/bc/Hanzo/rpc", Enabled: true},
			{Slug: "spc", Name: "SPC", ChainID: 36911, Port: 5200, RPC: "http://luxd-headless.lux-mainnet.svc:9630/ext/bc/SPC/rpc", Enabled: true},
			{Slug: "pars", Name: "Pars", ChainID: 494949, Port: 5300, RPC: "http://luxd-headless.lux-mainnet.svc:9630/ext/bc/Pars/rpc", Enabled: true},
		},
	}
}

func printChains(cfg *Config) {
	fmt.Printf("Configured EVM chains (%d total):\n\n", len(cfg.Chains))
	fmt.Printf("  %-10s %-20s %-10s %-6s %s\n", "SLUG", "NAME", "CHAIN_ID", "PORT", "STATUS")
	fmt.Printf("  %-10s %-20s %-10s %-6s %s\n", "----", "----", "--------", "----", "------")
	for _, c := range cfg.Chains {
		status := "enabled"
		if !c.Enabled {
			status = "disabled"
		}
		fmt.Printf("  %-10s %-20s %-10d %-6d %s\n", c.Slug, c.Name, c.ChainID, c.Port, status)
	}
}

// startManagementServer runs a simple HTTP server for health/stats.
func startManagementServer(ctx context.Context, cfg *Config) {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"version": version,
			"chains":  len(cfg.Chains),
		})
	})

	mux.HandleFunc("/chains", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var chains []map[string]interface{}
		for _, c := range cfg.Chains {
			chains = append(chains, map[string]interface{}{
				"slug":     c.Slug,
				"name":     c.Name,
				"chain_id": c.ChainID,
				"port":     c.Port,
				"enabled":  c.Enabled,
				"rpc":      c.RPC,
			})
		}
		json.NewEncoder(w).Encode(chains)
	})

	server := &http.Server{
		Addr:         ":9000",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Printf("Management API on :9000 (/health, /chains)")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("Management server error: %v", err)
	}
}
