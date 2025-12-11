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
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/luxfi/indexer/chain"
	"github.com/luxfi/indexer/dag"
)

var version = "dev"

func main() {
	var (
		chainType    = flag.String("chain", "", "Chain to index: xchain, achain, bchain, qchain, tchain, zchain (DAG) or pchain (linear)")
		rpcEndpoint  = flag.String("rpc", "", "RPC endpoint (e.g., http://localhost:9630/ext/bc/X)")
		databaseURL  = flag.String("db", "", "PostgreSQL connection URL")
		httpPort     = flag.Int("port", 0, "HTTP server port")
		pollInterval = flag.Duration("poll", 30*time.Second, "Poll interval for stats updates")
		showVersion  = flag.Bool("version", false, "Show version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("lux-indexer %s\n", version)
		os.Exit(0)
	}

	if *chainType == "" || *rpcEndpoint == "" || *databaseURL == "" || *httpPort == 0 {
		flag.Usage()
		log.Fatal("Missing required flags: -chain, -rpc, -db, -port")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
	case "xchain", "achain", "bchain", "qchain", "tchain", "zchain":
		runDAGIndexer(ctx, *chainType, *rpcEndpoint, *databaseURL, *httpPort, *pollInterval)
	// Linear chains
	case "pchain":
		runChainIndexer(ctx, *chainType, *rpcEndpoint, *databaseURL, *httpPort, *pollInterval)
	default:
		log.Fatalf("Unknown chain type: %s", *chainType)
	}
}

func runDAGIndexer(ctx context.Context, chainType, rpcEndpoint, databaseURL string, httpPort int, pollInterval time.Duration) {
	ct := dag.ChainType(chainType)
	cfg := dag.Config{
		ChainType:    ct,
		ChainName:    chainName(chainType),
		RPCEndpoint:  rpcEndpoint,
		RPCMethod:    rpcMethod(chainType),
		DatabaseURL:  databaseURL,
		HTTPPort:     httpPort,
		PollInterval: pollInterval,
	}

	// Use base adapter (chain-specific adapters can be added later)
	idx, err := dag.New(cfg, nil)
	if err != nil {
		log.Fatalf("[%s] Failed to create indexer: %v", chainType, err)
	}

	log.Printf("[%s] Starting DAG indexer on port %d", chainType, httpPort)
	if err := idx.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("[%s] Indexer error: %v", chainType, err)
	}
	log.Printf("[%s] Indexer stopped", chainType)
}

func runChainIndexer(ctx context.Context, chainType, rpcEndpoint, databaseURL string, httpPort int, pollInterval time.Duration) {
	ct := chain.ChainType(chainType)
	cfg := chain.Config{
		ChainType:    ct,
		ChainName:    chainName(chainType),
		RPCEndpoint:  rpcEndpoint,
		RPCMethod:    rpcMethod(chainType),
		DatabaseURL:  databaseURL,
		HTTPPort:     httpPort,
		PollInterval: pollInterval,
	}

	idx, err := chain.New(cfg, nil)
	if err != nil {
		log.Fatalf("[%s] Failed to create indexer: %v", chainType, err)
	}

	log.Printf("[%s] Starting linear chain indexer on port %d", chainType, httpPort)
	if err := idx.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("[%s] Indexer error: %v", chainType, err)
	}
	log.Printf("[%s] Indexer stopped", chainType)
}

func chainName(ct string) string {
	names := map[string]string{
		"xchain": "X-Chain (Exchange)",
		"achain": "A-Chain (AI)",
		"bchain": "B-Chain (Bridge)",
		"qchain": "Q-Chain (Quantum)",
		"tchain": "T-Chain (Teleport)",
		"zchain": "Z-Chain (Privacy)",
		"pchain": "P-Chain (Platform)",
	}
	return names[ct]
}

func rpcMethod(ct string) string {
	methods := map[string]string{
		"xchain": "xvm",
		"achain": "avm",
		"bchain": "bvm",
		"qchain": "qvm",
		"tchain": "tvm",
		"zchain": "zvm",
		"pchain": "pvm",
	}
	return methods[ct]
}
