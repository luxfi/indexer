// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package main provides the CLI for the multi-chain parallel indexer.
// This indexer can index thousands of chains concurrently using Go routines.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/luxfi/indexer/multichain"
)

var version = "dev"

func main() {
	var (
		configFile  = flag.String("config", "", "Path to chains.yaml config file")
		dataDir     = flag.String("data", "", "Data directory (default: ~/.lux/indexer/multichain)")
		httpPort    = flag.Int("port", 5000, "HTTP API port for stats and management")
		maxChains   = flag.Int("max-chains", 100, "Maximum concurrent chains to index")
		showVersion = flag.Bool("version", false, "Show version and exit")
		listChains  = flag.Bool("list", false, "List configured chains and exit")
		dbURL       = flag.String("db", "", "PostgreSQL connection URL")
		redisURL    = flag.String("redis", "", "Redis URL for caching")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("lux-multichain-indexer %s\n", version)
		os.Exit(0)
	}

	// Set defaults
	if *dataDir == "" {
		if env := os.Getenv("DATA_DIR"); env != "" {
			*dataDir = env
		} else {
			home, _ := os.UserHomeDir()
			*dataDir = filepath.Join(home, ".lux", "indexer", "multichain")
		}
	}

	// Create data directory
	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Load configuration
	var config *multichain.Config
	var err error

	if *configFile != "" {
		config, err = multichain.LoadChainsConfig(*configFile)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
	} else {
		// Try default locations
		defaultPaths := []string{
			filepath.Join(*dataDir, "chains.yaml"),
			"config/chains.yaml",
			"/etc/lux/indexer/chains.yaml",
		}
		for _, p := range defaultPaths {
			if _, err := os.Stat(p); err == nil {
				config, err = multichain.LoadChainsConfig(p)
				if err != nil {
					log.Printf("Warning: Failed to load config from %s: %v", p, err)
					continue
				}
				log.Printf("Loaded config from %s", p)
				break
			}
		}
		if config == nil {
			log.Println("No config file found, using default configuration")
			config = multichain.DefaultConfig()
		}
	}

	// Override config with CLI flags
	config.MaxConcurrentChains = *maxChains
	if *dbURL != "" {
		config.DatabaseURL = *dbURL
	}
	if *redisURL != "" {
		config.RedisURL = *redisURL
	}

	if *listChains {
		printChainList(config)
		os.Exit(0)
	}

	// Create manager
	manager, err := multichain.NewManager(config)
	if err != nil {
		log.Fatalf("Failed to create manager: %v", err)
	}

	// Setup context and signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Shutdown signal received, stopping indexers...")
		cancel()
	}()

	// Start HTTP server for stats
	go startHTTPServer(*httpPort, manager)

	// Start indexing
	log.Printf("Starting multi-chain indexer with %d chains", len(config.Chains))
	log.Printf("HTTP API available at http://localhost:%d", *httpPort)

	if err := manager.Start(); err != nil {
		log.Fatalf("Failed to start manager: %v", err)
	}

	// Wait for shutdown
	<-ctx.Done()

	// Graceful shutdown
	log.Println("Stopping all indexers...")
	if err := manager.Stop(); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	log.Println("Multi-chain indexer stopped")
}

func printChainList(config *multichain.Config) {
	fmt.Printf("Configured chains (%d total):\n\n", len(config.Chains))

	byType := make(map[multichain.ChainType][]multichain.ChainConfig)
	for _, chain := range config.Chains {
		byType[chain.Type] = append(byType[chain.Type], chain)
	}

	typeNames := map[multichain.ChainType]string{
		multichain.ChainTypeEVM:       "EVM Chains",
		multichain.ChainTypeSolana:    "Solana",
		multichain.ChainTypeBitcoin:   "Bitcoin",
		multichain.ChainTypeCosmos:    "Cosmos SDK",
		multichain.ChainTypeMove:      "Move (Aptos/Sui)",
		multichain.ChainTypeNear:      "NEAR",
		multichain.ChainTypeTron:      "Tron",
		multichain.ChainTypeTon:       "TON",
		multichain.ChainTypeSubstrate: "Substrate (Polkadot)",
		// Lux Native Chains
		multichain.ChainTypeLuxAI:        "Lux A-Chain (AI/Oracle)",
		multichain.ChainTypeLuxBridge:    "Lux B-Chain (Bridge)",
		multichain.ChainTypeLuxThreshold: "Lux T-Chain (Threshold)",
		multichain.ChainTypeLuxZK:        "Lux Z-Chain (ZK)",
		multichain.ChainTypeLuxGraph:     "Lux G-Chain (Graph)",
		multichain.ChainTypeLuxIdentity:  "Lux I-Chain (Identity)",
		multichain.ChainTypeLuxKey:       "Lux K-Chain (Key)",
		multichain.ChainTypeLuxDEX:       "Lux D-Chain (DEX)",
	}

	for chainType, name := range typeNames {
		chains := byType[chainType]
		if len(chains) == 0 {
			continue
		}
		fmt.Printf("%s:\n", name)
		for _, c := range chains {
			status := "enabled"
			if !c.Enabled {
				status = "disabled"
			}
			fmt.Printf("  %-20s ChainID: %-8d %s\n", c.ID, c.ChainID, status)
		}
		fmt.Println()
	}
}

func startHTTPServer(port int, manager *multichain.Manager) {
	mux := http.NewServeMux()

	// Stats endpoint
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		stats := manager.Stats()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})

	// Health endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		stats := manager.Stats()
		health := map[string]interface{}{
			"status":       "ok",
			"activeChains": stats.ActiveChains,
			"totalChains":  stats.TotalChains,
			"uptime":       stats.Uptime.String(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(health)
	})

	// Chain stats endpoint
	mux.HandleFunc("/chains", func(w http.ResponseWriter, r *http.Request) {
		stats := manager.Stats()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats.ChainStats)
	})

	// Individual chain endpoint
	mux.HandleFunc("/chain/", func(w http.ResponseWriter, r *http.Request) {
		chainID := r.URL.Path[len("/chain/"):]
		indexer, ok := manager.GetIndexer(chainID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(indexer.Stats())
	})

	// Metrics endpoint (Prometheus format)
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		stats := manager.Stats()
		w.Header().Set("Content-Type", "text/plain")

		fmt.Fprintf(w, "# HELP indexer_total_chains Total number of configured chains\n")
		fmt.Fprintf(w, "# TYPE indexer_total_chains gauge\n")
		fmt.Fprintf(w, "indexer_total_chains %d\n", stats.TotalChains)

		fmt.Fprintf(w, "# HELP indexer_active_chains Number of actively indexing chains\n")
		fmt.Fprintf(w, "# TYPE indexer_active_chains gauge\n")
		fmt.Fprintf(w, "indexer_active_chains %d\n", stats.ActiveChains)

		fmt.Fprintf(w, "# HELP indexer_total_blocks_indexed Total blocks indexed across all chains\n")
		fmt.Fprintf(w, "# TYPE indexer_total_blocks_indexed counter\n")
		fmt.Fprintf(w, "indexer_total_blocks_indexed %d\n", stats.TotalBlocksIndexed)

		fmt.Fprintf(w, "# HELP indexer_total_txs_indexed Total transactions indexed\n")
		fmt.Fprintf(w, "# TYPE indexer_total_txs_indexed counter\n")
		fmt.Fprintf(w, "indexer_total_txs_indexed %d\n", stats.TotalTxsIndexed)

		fmt.Fprintf(w, "# HELP indexer_uptime_seconds Indexer uptime in seconds\n")
		fmt.Fprintf(w, "# TYPE indexer_uptime_seconds gauge\n")
		fmt.Fprintf(w, "indexer_uptime_seconds %.0f\n", stats.Uptime.Seconds())

		// Per-chain metrics
		for chainID, chainStats := range stats.ChainStats {
			fmt.Fprintf(w, "# HELP indexer_chain_blocks_behind{chain=\"%s\"} Blocks behind head\n", chainID)
			fmt.Fprintf(w, "indexer_chain_blocks_behind{chain=\"%s\"} %d\n", chainID, chainStats.BlocksBehind)
			fmt.Fprintf(w, "indexer_chain_blocks_processed{chain=\"%s\"} %d\n", chainID, chainStats.BlocksProcessed)
			fmt.Fprintf(w, "indexer_chain_errors{chain=\"%s\"} %d\n", chainID, chainStats.ErrorCount)
		}
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	log.Printf("Starting HTTP server on port %d", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("HTTP server error: %v", err)
	}
}
