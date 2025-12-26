// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package main provides the CLI for running LUX chain indexers.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/luxfi/indexer/achain"
	"github.com/luxfi/indexer/bchain"
	"github.com/luxfi/indexer/chain"
	"github.com/luxfi/indexer/dag"
	"github.com/luxfi/indexer/evm"
	"github.com/luxfi/indexer/pchain"
	"github.com/luxfi/indexer/qchain"
	"github.com/luxfi/indexer/storage"
	"github.com/luxfi/indexer/tchain"
	"github.com/luxfi/indexer/xchain"
	"github.com/luxfi/indexer/zchain"
)

var version = "dev"

func main() {
	var (
		chainType    = flag.String("chain", "", "Chain to index: xchain, achain, bchain, qchain, tchain, zchain (DAG), pchain (linear), or cchain (EVM)")
		rpcEndpoint  = flag.String("rpc", "", "RPC endpoint (e.g., http://localhost:9630/ext/bc/X)")
		dataDir      = flag.String("data", "", "Data directory for storage (default: ~/.lux/indexer/<chain>)")
		httpPort     = flag.Int("port", 0, "HTTP server port (default: chain-specific)")
		pollInterval = flag.Duration("poll", 30*time.Second, "Poll interval for stats updates")
		showVersion  = flag.Bool("version", false, "Show version and exit")
		listChains   = flag.Bool("list", false, "List available chains and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("lux-indexer %s\n", version)
		os.Exit(0)
	}

	if *listChains {
		printChainList()
		os.Exit(0)
	}

	if *chainType == "" {
		flag.Usage()
		fmt.Println("\nAvailable chains:")
		printChainList()
		os.Exit(1)
	}

	// Set defaults based on chain type, checking env vars first
	if *rpcEndpoint == "" {
		if env := os.Getenv("RPC_ENDPOINT"); env != "" {
			*rpcEndpoint = env
		} else {
			*rpcEndpoint = defaultRPC(*chainType)
		}
	}
	if *dataDir == "" {
		if env := os.Getenv("DATA_DIR"); env != "" {
			*dataDir = env
		} else {
			home, _ := os.UserHomeDir()
			*dataDir = filepath.Join(home, ".lux", "indexer", *chainType)
		}
	}
	if *httpPort == 0 {
		if env := os.Getenv("HTTP_PORT"); env != "" {
			if p, err := fmt.Sscanf(env, "%d", httpPort); err == nil && p == 1 {
				// port set from env
			} else {
				*httpPort = defaultPort(*chainType)
			}
		} else {
			*httpPort = defaultPort(*chainType)
		}
	}

	// Create data directory if needed
	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Create unified storage
	store, err := storage.NewUnified(storage.DefaultUnifiedConfig(*dataDir))
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Initialize storage
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := store.Init(ctx); err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Shutdown signal received")
		cancel()
	}()

	switch *chainType {
	// DAG chains
	case "xchain":
		runXChain(ctx, store, *rpcEndpoint, *httpPort, *pollInterval)
	case "achain":
		runAChain(ctx, store, *rpcEndpoint, *httpPort, *pollInterval)
	case "bchain":
		runBChain(ctx, store, *rpcEndpoint, *httpPort, *pollInterval)
	case "qchain":
		runQChain(ctx, store, *rpcEndpoint, *httpPort, *pollInterval)
	case "tchain":
		runTChain(ctx, store, *rpcEndpoint, *httpPort, *pollInterval)
	case "zchain":
		runZChain(ctx, store, *rpcEndpoint, *httpPort, *pollInterval)
	// Linear chains
	case "pchain":
		runPChain(ctx, store, *rpcEndpoint, *httpPort, *pollInterval)
	// EVM chain
	case "cchain":
		runCChain(ctx, store, *rpcEndpoint, *httpPort, *pollInterval)
	default:
		log.Fatalf("Unknown chain type: %s", *chainType)
	}
}

func runXChain(ctx context.Context, store storage.Store, rpcEndpoint string, httpPort int, pollInterval time.Duration) {
	adapter := xchain.New(rpcEndpoint)
	cfg := dag.Config{
		ChainType:    dag.ChainX,
		ChainName:    "X-Chain (Exchange)",
		RPCEndpoint:  rpcEndpoint,
		RPCMethod:    "xvm",
		HTTPPort:     httpPort,
		PollInterval: pollInterval,
	}

	idx, err := dag.New(cfg, store, adapter)
	if err != nil {
		log.Fatalf("[xchain] Failed to create indexer: %v", err)
	}

	log.Printf("[xchain] Starting DAG indexer on port %d", httpPort)
	if err := idx.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("[xchain] Indexer error: %v", err)
	}
	log.Println("[xchain] Indexer stopped")
}

func runAChain(ctx context.Context, store storage.Store, rpcEndpoint string, httpPort int, pollInterval time.Duration) {
	adapter := achain.New(rpcEndpoint)
	cfg := dag.Config{
		ChainType:    dag.ChainA,
		ChainName:    "A-Chain (AI)",
		RPCEndpoint:  rpcEndpoint,
		RPCMethod:    "avm",
		HTTPPort:     httpPort,
		PollInterval: pollInterval,
	}

	idx, err := dag.New(cfg, store, adapter)
	if err != nil {
		log.Fatalf("[achain] Failed to create indexer: %v", err)
	}

	log.Printf("[achain] Starting DAG indexer on port %d", httpPort)
	if err := idx.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("[achain] Indexer error: %v", err)
	}
	log.Println("[achain] Indexer stopped")
}

func runBChain(ctx context.Context, store storage.Store, rpcEndpoint string, httpPort int, pollInterval time.Duration) {
	adapter := bchain.New(rpcEndpoint)
	cfg := dag.Config{
		ChainType:    dag.ChainB,
		ChainName:    "B-Chain (Bridge)",
		RPCEndpoint:  rpcEndpoint,
		RPCMethod:    "bvm",
		HTTPPort:     httpPort,
		PollInterval: pollInterval,
	}

	idx, err := dag.New(cfg, store, adapter)
	if err != nil {
		log.Fatalf("[bchain] Failed to create indexer: %v", err)
	}

	log.Printf("[bchain] Starting DAG indexer on port %d", httpPort)
	if err := idx.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("[bchain] Indexer error: %v", err)
	}
	log.Println("[bchain] Indexer stopped")
}

func runQChain(ctx context.Context, store storage.Store, rpcEndpoint string, httpPort int, pollInterval time.Duration) {
	adapter := qchain.New(rpcEndpoint)
	cfg := dag.Config{
		ChainType:    dag.ChainQ,
		ChainName:    "Q-Chain (Quantum)",
		RPCEndpoint:  rpcEndpoint,
		RPCMethod:    "qvm",
		HTTPPort:     httpPort,
		PollInterval: pollInterval,
	}

	idx, err := dag.New(cfg, store, adapter)
	if err != nil {
		log.Fatalf("[qchain] Failed to create indexer: %v", err)
	}

	log.Printf("[qchain] Starting DAG indexer on port %d", httpPort)
	if err := idx.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("[qchain] Indexer error: %v", err)
	}
	log.Println("[qchain] Indexer stopped")
}

func runTChain(ctx context.Context, store storage.Store, rpcEndpoint string, httpPort int, pollInterval time.Duration) {
	adapter := tchain.New(rpcEndpoint)
	cfg := dag.Config{
		ChainType:    dag.ChainT,
		ChainName:    "T-Chain (Teleport)",
		RPCEndpoint:  rpcEndpoint,
		RPCMethod:    "tvm",
		HTTPPort:     httpPort,
		PollInterval: pollInterval,
	}

	idx, err := dag.New(cfg, store, adapter)
	if err != nil {
		log.Fatalf("[tchain] Failed to create indexer: %v", err)
	}

	log.Printf("[tchain] Starting DAG indexer on port %d", httpPort)
	if err := idx.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("[tchain] Indexer error: %v", err)
	}
	log.Println("[tchain] Indexer stopped")
}

func runZChain(ctx context.Context, store storage.Store, rpcEndpoint string, httpPort int, pollInterval time.Duration) {
	adapter := zchain.New(rpcEndpoint)
	cfg := dag.Config{
		ChainType:    dag.ChainZ,
		ChainName:    "Z-Chain (Privacy)",
		RPCEndpoint:  rpcEndpoint,
		RPCMethod:    "zvm",
		HTTPPort:     httpPort,
		PollInterval: pollInterval,
	}

	idx, err := dag.New(cfg, store, adapter)
	if err != nil {
		log.Fatalf("[zchain] Failed to create indexer: %v", err)
	}

	log.Printf("[zchain] Starting DAG indexer on port %d", httpPort)
	if err := idx.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("[zchain] Indexer error: %v", err)
	}
	log.Println("[zchain] Indexer stopped")
}

func runPChain(ctx context.Context, store storage.Store, rpcEndpoint string, httpPort int, pollInterval time.Duration) {
	adapter := pchain.New(rpcEndpoint)
	cfg := chain.Config{
		ChainType:    chain.ChainP,
		ChainName:    "P-Chain (Platform)",
		RPCEndpoint:  rpcEndpoint,
		RPCMethod:    "pvm",
		HTTPPort:     httpPort,
		PollInterval: pollInterval,
	}

	idx, err := chain.New(cfg, store, adapter)
	if err != nil {
		log.Fatalf("[pchain] Failed to create indexer: %v", err)
	}

	log.Printf("[pchain] Starting linear chain indexer on port %d", httpPort)
	if err := idx.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("[pchain] Indexer error: %v", err)
	}
	log.Println("[pchain] Indexer stopped")
}

func runCChain(ctx context.Context, store storage.Store, rpcEndpoint string, httpPort int, pollInterval time.Duration) {
	cfg := evm.Config{
		ChainName:    "C-Chain (Contract)",
		RPCEndpoint:  rpcEndpoint,
		HTTPPort:     httpPort,
		PollInterval: pollInterval,
	}

	idx, err := evm.NewIndexer(cfg, store)
	if err != nil {
		log.Fatalf("[cchain] Failed to create indexer: %v", err)
	}

	log.Printf("[cchain] Starting EVM indexer on port %d", httpPort)
	if err := idx.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("[cchain] Indexer error: %v", err)
	}
	log.Println("[cchain] Indexer stopped")
}

func printChainList() {
	fmt.Println("  DAG-based chains (fast consensus):")
	fmt.Println("    xchain  - X-Chain (Exchange)  - Port 4200 - Asset exchange, UTXOs")
	fmt.Println("    achain  - A-Chain (AI)        - Port 4500 - AI compute, attestations")
	fmt.Println("    bchain  - B-Chain (Bridge)    - Port 4600 - Cross-chain bridge")
	fmt.Println("    qchain  - Q-Chain (Quantum)   - Port 4300 - Quantum finality proofs")
	fmt.Println("    tchain  - T-Chain (Teleport)  - Port 4700 - MPC threshold signatures")
	fmt.Println("    zchain  - Z-Chain (Privacy)   - Port 4400 - ZK transactions")
	fmt.Println()
	fmt.Println("  Linear chains (strict ordering):")
	fmt.Println("    pchain  - P-Chain (Platform)  - Port 4100 - Validators, staking")
	fmt.Println()
	fmt.Println("  EVM chain:")
	fmt.Println("    cchain  - C-Chain (Contract)  - Port 4000 - Smart contracts, DeFi")
}

func defaultPort(ct string) int {
	ports := map[string]int{
		"cchain": 4000,
		"pchain": 4100,
		"xchain": 4200,
		"qchain": 4300,
		"zchain": 4400,
		"achain": 4500,
		"bchain": 4600,
		"tchain": 4700,
	}
	return ports[ct]
}

func defaultRPC(ct string) string {
	endpoints := map[string]string{
		"pchain": "http://localhost:9650/ext/bc/P",
		"xchain": "http://localhost:9650/ext/bc/X",
		"cchain": "http://localhost:9650/ext/bc/C/rpc",
		"qchain": "http://localhost:9650/ext/bc/Q",
		"zchain": "http://localhost:9650/ext/bc/Z",
		"achain": "http://localhost:9650/ext/bc/A",
		"bchain": "http://localhost:9650/ext/bc/B",
		"tchain": "http://localhost:9650/ext/bc/T",
	}
	return endpoints[ct]
}
