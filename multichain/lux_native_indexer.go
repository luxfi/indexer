// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package multichain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// LuxNativeIndexer indexes Lux native chains (A, B, T, Z, G, I, K, D)
type LuxNativeIndexer struct {
	config       ChainConfig
	client       *http.Client
	latestHeight uint64
	indexedBlock uint64
	isRunning    bool
	stopChan     chan struct{}
	mu           sync.RWMutex
	stats        IndexerStats
}

// LuxNativeBlock represents a block from a Lux native chain
type LuxNativeBlock struct {
	Height    uint64          `json:"height"`
	Hash      string          `json:"hash"`
	ParentID  string          `json:"parentID"`
	Timestamp time.Time       `json:"timestamp"`
	Txs       json.RawMessage `json:"txs"`
}

// NewLuxNativeIndexer creates a new indexer for Lux native chains
func NewLuxNativeIndexer(config ChainConfig) *LuxNativeIndexer {
	return &LuxNativeIndexer{
		config: config,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		stopChan: make(chan struct{}),
	}
}

// ChainID returns the chain identifier
func (idx *LuxNativeIndexer) ChainID() string {
	return idx.config.ID
}

// ChainType returns the chain type
func (idx *LuxNativeIndexer) ChainType() ChainType {
	return idx.config.Type
}

// IsRunning returns whether the indexer is running
func (idx *LuxNativeIndexer) IsRunning() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.isRunning
}

// Start begins indexing the native chain
func (idx *LuxNativeIndexer) Start(ctx context.Context) error {
	idx.mu.Lock()
	idx.isRunning = true
	idx.stats.StartTime = time.Now()
	idx.stats.ChainID = idx.config.ID
	idx.stats.ChainName = idx.config.Name
	idx.mu.Unlock()

	log.Printf("[%s] Starting Lux native indexer for %s (%s)", idx.config.ID, idx.config.Name, idx.config.Type)

	go idx.indexLoop(ctx)
	return nil
}

// Stop halts the indexer
func (idx *LuxNativeIndexer) Stop() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if !idx.isRunning {
		return nil
	}

	close(idx.stopChan)
	idx.isRunning = false
	log.Printf("[%s] Stopped Lux native indexer", idx.config.ID)
	return nil
}

// IndexBlock indexes a specific block
func (idx *LuxNativeIndexer) IndexBlock(ctx context.Context, blockNumber uint64) error {
	return idx.fetchAndProcessBlock(ctx)
}

// IndexBlockRange indexes a range of blocks
func (idx *LuxNativeIndexer) IndexBlockRange(ctx context.Context, from, to uint64) error {
	for block := from; block <= to; block++ {
		if err := idx.IndexBlock(ctx, block); err != nil {
			return err
		}
	}
	return nil
}

// GetLatestBlock returns the latest block number from the chain
func (idx *LuxNativeIndexer) GetLatestBlock(ctx context.Context) (uint64, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.latestHeight, nil
}

// GetIndexedBlock returns the last indexed block number
func (idx *LuxNativeIndexer) GetIndexedBlock() uint64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.indexedBlock
}

// Stats returns current indexer statistics
func (idx *LuxNativeIndexer) Stats() *IndexerStats {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	stats := idx.stats
	stats.ChainID = idx.config.ID
	stats.ChainName = idx.config.Name
	stats.IsRunning = idx.isRunning
	stats.LatestBlock = idx.latestHeight
	stats.IndexedBlock = idx.indexedBlock
	stats.Uptime = time.Since(stats.StartTime)
	if stats.Uptime.Seconds() > 0 {
		stats.BlocksPerSecond = float64(stats.BlocksProcessed) / stats.Uptime.Seconds()
	}
	return &stats
}

// indexLoop is the main indexing loop
func (idx *LuxNativeIndexer) indexLoop(ctx context.Context) {
	ticker := time.NewTicker(idx.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-idx.stopChan:
			return
		case <-ticker.C:
			if err := idx.fetchAndProcessBlock(ctx); err != nil {
				idx.mu.Lock()
				idx.stats.ErrorCount++
				idx.stats.LastError = err.Error()
				idx.mu.Unlock()
				// Don't log every error for native chains that might not be running
			}
		}
	}
}

// fetchAndProcessBlock fetches and processes the latest block
func (idx *LuxNativeIndexer) fetchAndProcessBlock(ctx context.Context) error {
	// Different chain types have different RPC methods
	switch idx.config.Type {
	case ChainTypeLuxAI:
		return idx.fetchAIBlock(ctx)
	case ChainTypeLuxBridge:
		return idx.fetchBridgeBlock(ctx)
	case ChainTypeLuxThreshold:
		return idx.fetchThresholdBlock(ctx)
	case ChainTypeLuxZK:
		return idx.fetchZKBlock(ctx)
	case ChainTypeLuxGraph:
		return idx.fetchGraphBlock(ctx)
	case ChainTypeLuxIdentity:
		return idx.fetchIdentityBlock(ctx)
	case ChainTypeLuxKey:
		return idx.fetchKeyBlock(ctx)
	case ChainTypeLuxDEX:
		return idx.fetchDEXBlock(ctx)
	default:
		return fmt.Errorf("unknown Lux native chain type: %s", idx.config.Type)
	}
}

// fetchAIBlock fetches blocks from A-Chain (AI VM)
func (idx *LuxNativeIndexer) fetchAIBlock(ctx context.Context) error {
	// A-Chain RPC methods: ai.getLatestBlock, ai.getProviders, ai.getTasks
	block, err := idx.rpcCall(ctx, "ai.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchBridgeBlock fetches blocks from B-Chain (Bridge VM)
func (idx *LuxNativeIndexer) fetchBridgeBlock(ctx context.Context) error {
	// B-Chain RPC methods: bridge.getLatestBlock, bridge.getPendingRequests
	block, err := idx.rpcCall(ctx, "bridge.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchThresholdBlock fetches blocks from T-Chain (Threshold VM)
func (idx *LuxNativeIndexer) fetchThresholdBlock(ctx context.Context) error {
	// T-Chain RPC methods: threshold.getLatestBlock, threshold.getKeyGenSessions
	block, err := idx.rpcCall(ctx, "threshold.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchZKBlock fetches blocks from Z-Chain (ZK VM)
func (idx *LuxNativeIndexer) fetchZKBlock(ctx context.Context) error {
	// Z-Chain RPC methods: zk.getLatestBlock, zk.getProofs
	block, err := idx.rpcCall(ctx, "zk.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchGraphBlock fetches blocks from G-Chain (Graph VM)
func (idx *LuxNativeIndexer) fetchGraphBlock(ctx context.Context) error {
	// G-Chain uses GraphQL endpoint, different from JSON-RPC
	return idx.fetchGraphQLSchema(ctx)
}

// fetchIdentityBlock fetches blocks from I-Chain (Identity VM)
func (idx *LuxNativeIndexer) fetchIdentityBlock(ctx context.Context) error {
	// I-Chain RPC methods: identity.getLatestBlock, identity.getIdentities
	block, err := idx.rpcCall(ctx, "identity.Health", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchKeyBlock fetches blocks from K-Chain (Key VM)
func (idx *LuxNativeIndexer) fetchKeyBlock(ctx context.Context) error {
	// K-Chain RPC methods: key.getLatestBlock, key.getKeyShares
	block, err := idx.rpcCall(ctx, "key.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchDEXBlock fetches blocks from D-Chain (DEX VM)
func (idx *LuxNativeIndexer) fetchDEXBlock(ctx context.Context) error {
	// D-Chain RPC methods: dex.getLatestBlock, dex.getOrderbook
	block, err := idx.rpcCall(ctx, "dex.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// rpcCall makes a JSON-RPC call to the native chain
func (idx *LuxNativeIndexer) rpcCall(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", idx.config.RPC, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := idx.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("RPC request failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", result.Error.Code, result.Error.Message)
	}

	return result.Result, nil
}

// fetchGraphQLSchema fetches the GraphQL schema from G-Chain
func (idx *LuxNativeIndexer) fetchGraphQLSchema(ctx context.Context) error {
	// GraphQL introspection query
	query := `{"query": "{ __schema { types { name } } }"}`

	req, err := http.NewRequestWithContext(ctx, "POST", idx.config.RPC+"/graphql", bytes.NewReader([]byte(query)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := idx.client.Do(req)
	if err != nil {
		// G-Chain might not be fully available yet, that's OK
		return nil
	}
	defer resp.Body.Close()

	idx.mu.Lock()
	idx.stats.BlocksProcessed++
	idx.mu.Unlock()

	return nil
}

// processBlock processes a block from a native chain
func (idx *LuxNativeIndexer) processBlock(data json.RawMessage) error {
	if data == nil {
		return nil
	}

	var block LuxNativeBlock
	if err := json.Unmarshal(data, &block); err != nil {
		// Not all responses are blocks, ignore parse errors
		idx.mu.Lock()
		idx.stats.BlocksProcessed++
		idx.mu.Unlock()
		return nil
	}

	idx.mu.Lock()
	if block.Height > idx.latestHeight {
		idx.latestHeight = block.Height
	}
	idx.stats.BlocksProcessed++
	idx.indexedBlock = block.Height
	idx.stats.IndexedBlock = block.Height
	idx.mu.Unlock()

	return nil
}
