// Package main provides the standalone chain-indexer daemon.
//
// Indexes one or more chains (EVM and others) into per-chain SQLite stores
// and serves only the /v1/indexer/* HTTP API. No graph engine, no embedded
// frontend.
//
//	indexerd --config=chains.yaml
//	indexerd --rpc=http://localhost:9650/ext/bc/C/rpc  (single chain mode)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/luxfi/indexer/daemon"
)

var version = "dev"

func main() {
	var (
		configFile  = flag.String("config", "", "Path to chains.yaml")
		rpcEndpoint = flag.String("rpc", "", "Single-chain mode: RPC endpoint")
		dataDir     = flag.String("data", "", "Data directory (default: ~/.explorer/data)")
		httpAddr    = flag.String("http", ":8091", "HTTP listen address")
		chainName   = flag.String("chain-name", "", "Single-chain mode: display name")
		coinSymbol  = flag.String("coin", "ETH", "Single-chain mode: coin symbol")
		chainID     = flag.Int64("chain-id", 0, "Single-chain mode: chain ID")
		showVersion = flag.Bool("version", false, "Show version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("indexerd %s\n", version)
		os.Exit(0)
	}

	if *dataDir == "" {
		*dataDir = daemon.EnvOr("DATA_DIR", filepath.Join(daemon.HomeDir(), ".explorer", "data"))
	}
	if *httpAddr == ":8091" {
		if addr := os.Getenv("HTTP_ADDR"); addr != "" {
			*httpAddr = addr
		} else if p := os.Getenv("PORT"); p != "" {
			*httpAddr = ":" + p
		}
	}

	cfg := resolveConfig(*configFile, *rpcEndpoint, *chainName, *coinSymbol, *chainID, *dataDir)
	if cfg.DataDir == "" {
		cfg.DataDir = *dataDir
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = *httpAddr
	}

	enabled := cfg.EnabledChains()
	if len(enabled) == 0 {
		log.Fatal("No enabled chains")
	}
	log.Printf("indexerd %s — %d chains", version, len(enabled))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	wg := daemon.RunIndexers(ctx, cfg)

	mux := http.NewServeMux()
	daemon.MountHealth(mux)
	daemon.MountIndexerAPI(ctx, cfg, mux)

	go daemon.ListenAndServe(ctx, cfg.HTTPAddr, mux)

	wg.Wait()
	log.Println("indexerd stopped")
}

func resolveConfig(configFile, rpcFlag, chainName, coinSymbol string, chainID int64, dataDir string) daemon.Config {
	if configFile != "" {
		cfg, err := daemon.LoadConfig(configFile)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		return cfg
	}

	rpc := rpcFlag
	if rpc == "" {
		rpc = os.Getenv("RPC_ENDPOINT")
	}
	if rpc != "" {
		if chainName == "" {
			chainName = daemon.EnvOr("CHAIN_NAME", "EVM Chain")
		}
		if chainID == 0 {
			if env := os.Getenv("CHAIN_ID"); env != "" {
				fmt.Sscanf(env, "%d", &chainID)
			}
		}
		return daemon.Config{
			Chains: []daemon.ChainConfig{{
				Slug:       "default",
				Name:       chainName,
				ChainID:    chainID,
				Type:       "evm",
				RPC:        rpc,
				CoinSymbol: coinSymbol,
				Enabled:    true,
				Default:    true,
			}},
		}
	}

	if path := daemon.FindConfig(dataDir); path != "" {
		cfg, err := daemon.LoadConfig(path)
		if err != nil {
			log.Fatalf("Failed to load config %s: %v", path, err)
		}
		return cfg
	}

	fmt.Println("Usage:")
	fmt.Println("  indexerd --rpc=http://localhost:9650/ext/bc/C/rpc   (single chain)")
	fmt.Println("  indexerd --config=chains.yaml                        (multi-chain)")
	os.Exit(1)
	return daemon.Config{}
}
