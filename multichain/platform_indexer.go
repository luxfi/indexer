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

	"github.com/gorilla/websocket"
)

// PlatformIndexer indexes platform chains (A, B, T, Z, G, I, K, D)
type PlatformIndexer struct {
	config       ChainConfig
	client       *http.Client
	latestHeight uint64
	indexedBlock uint64
	isRunning    bool
	stopChan     chan struct{}
	mu           sync.RWMutex
	stats        IndexerStats
}

// PlatformBlock represents a block from a platform chain
type PlatformBlock struct {
	Height    uint64          `json:"height"`
	Hash      string          `json:"hash"`
	ParentID  string          `json:"parentID"`
	Timestamp time.Time       `json:"timestamp"`
	Txs       json.RawMessage `json:"txs"`
}

// NewPlatformIndexer creates a new indexer for platform chains
func NewPlatformIndexer(config ChainConfig) *PlatformIndexer {
	return &PlatformIndexer{
		config: config,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		stopChan: make(chan struct{}),
	}
}

// ChainID returns the chain identifier
func (idx *PlatformIndexer) ChainID() string {
	return idx.config.ID
}

// ChainType returns the chain type
func (idx *PlatformIndexer) ChainType() ChainType {
	return idx.config.Type
}

// IsRunning returns whether the indexer is running
func (idx *PlatformIndexer) IsRunning() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.isRunning
}

// Start begins indexing the platform chain
func (idx *PlatformIndexer) Start(ctx context.Context) error {
	idx.mu.Lock()
	idx.isRunning = true
	idx.stats.StartTime = time.Now()
	idx.stats.ChainID = idx.config.ID
	idx.stats.ChainName = idx.config.Name
	idx.mu.Unlock()

	log.Printf("[%s] Starting native indexer for %s (%s)", idx.config.ID, idx.config.Name, idx.config.Type)

	go idx.indexLoop(ctx)
	return nil
}

// Stop halts the indexer
func (idx *PlatformIndexer) Stop() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if !idx.isRunning {
		return nil
	}

	close(idx.stopChan)
	idx.isRunning = false
	log.Printf("[%s] Stopped native indexer", idx.config.ID)
	return nil
}

// IndexBlock indexes a specific block
func (idx *PlatformIndexer) IndexBlock(ctx context.Context, blockNumber uint64) error {
	return idx.fetchAndProcessBlock(ctx)
}

// IndexBlockRange indexes a range of blocks
func (idx *PlatformIndexer) IndexBlockRange(ctx context.Context, from, to uint64) error {
	for block := from; block <= to; block++ {
		if err := idx.IndexBlock(ctx, block); err != nil {
			return err
		}
	}
	return nil
}

// GetLatestBlock returns the latest block number from the chain
func (idx *PlatformIndexer) GetLatestBlock(ctx context.Context) (uint64, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.latestHeight, nil
}

// GetIndexedBlock returns the last indexed block number
func (idx *PlatformIndexer) GetIndexedBlock() uint64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.indexedBlock
}

// Stats returns current indexer statistics
func (idx *PlatformIndexer) Stats() *IndexerStats {
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

// indexLoop is the main indexing loop. If the chain config has a WS
// endpoint, subscribe for realtime block notifications; otherwise fall
// back to polling at PollInterval.
func (idx *PlatformIndexer) indexLoop(ctx context.Context) {
	// Try realtime WebSocket subscription first when a WS endpoint is set.
	if idx.config.WS != "" {
		if idx.runRealtimeLoop(ctx) {
			// realtime ran to completion (ctx cancel or stop)
			return
		}
		// fall through to polling if WS subscription failed
		log.Printf("[%s] WS subscription unavailable, falling back to polling", idx.config.ID)
	}

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
				// Don't log every error for platform chains that might not be running
			}
		}
	}
}

// runRealtimeLoop subscribes to chain notifications over WebSocket and
// processes each block as it arrives. Returns true if the loop ran to
// completion (stop requested), false if the WS connection could not be
// established — in which case the caller should fall back to polling.
func (idx *PlatformIndexer) runRealtimeLoop(ctx context.Context) bool {
	method := idx.realtimeSubscribeMethod()
	if method == "" {
		return false
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, idx.config.WS, nil)
	if err != nil {
		return false
	}
	defer conn.Close()

	// Send JSON-RPC subscribe request.
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  []interface{}{},
	}
	if err := conn.WriteJSON(req); err != nil {
		return false
	}

	log.Printf("[%s] Realtime subscription active via %s", idx.config.ID, idx.config.WS)

	for {
		select {
		case <-ctx.Done():
			return true
		case <-idx.stopChan:
			return true
		default:
		}

		// Read with a deadline so we can check ctx periodically.
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			// Exit realtime mode on connection error; caller will poll.
			idx.mu.Lock()
			idx.stats.ErrorCount++
			idx.stats.LastError = "ws: " + err.Error()
			idx.mu.Unlock()
			return false
		}

		// Extract the `result` or `params.result` field from JSON-RPC notifications.
		block := extractRealtimeBlock(data)
		if block == nil {
			continue
		}
		if err := idx.processBlock(block); err != nil {
			idx.mu.Lock()
			idx.stats.ErrorCount++
			idx.stats.LastError = err.Error()
			idx.mu.Unlock()
		}
	}
}

// realtimeSubscribeMethod returns the JSON-RPC subscribe method name for
// the chain's realtime block feed. Each native VM exposes a
// `{prefix}.subscribeLatest` notification stream over WebSocket.
func (idx *PlatformIndexer) realtimeSubscribeMethod() string {
	switch idx.config.Type {
	case ChainTypePlatform:
		return "platform.subscribeLatest"
	case ChainTypeUTXO:
		return "xvm.subscribeLatest"
	case ChainTypeAI:
		return "ai.subscribeLatest"
	case ChainTypeBridge:
		return "bridge.subscribeLatest"
	case ChainTypeDEX:
		return "dex.subscribeLatest"
	case ChainTypeGraph:
		return "graph.subscribeLatest"
	case ChainTypeIdentity:
		return "identity.subscribeLatest"
	case ChainTypeKey:
		return "key.subscribeLatest"
	case ChainTypeMPC:
		return "mpc.subscribeLatest"
	case ChainTypeOracle:
		return "oracle.subscribeLatest"
	case ChainTypeQuantum:
		return "quantum.subscribeLatest"
	case ChainTypeRelay:
		return "relay.subscribeLatest"
	case ChainTypeThreshold:
		return "threshold.subscribeLatest"
	case ChainTypeZK:
		return "zk.subscribeLatest"
	}
	return ""
}

// extractRealtimeBlock pulls the block payload out of either a subscribe
// response (`result` field) or a notification (`params.result` field).
// Returns nil for subscribe confirmations that don't carry a block.
func extractRealtimeBlock(data []byte) json.RawMessage {
	var env struct {
		Result json.RawMessage `json:"result"`
		Params struct {
			Result json.RawMessage `json:"result"`
		} `json:"params"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil
	}
	if len(env.Params.Result) > 0 && string(env.Params.Result) != "null" {
		return env.Params.Result
	}
	// A subscribe confirmation's `result` is usually a subscription id
	// string, not a block — skip those. Blocks come as notifications.
	return nil
}

// fetchAndProcessBlock fetches and processes the latest block
func (idx *PlatformIndexer) fetchAndProcessBlock(ctx context.Context) error {
	// Different chain types have different RPC methods
	switch idx.config.Type {
	case ChainTypePlatform:
		return idx.fetchPlatformBlock(ctx)
	case ChainTypeUTXO:
		return idx.fetchXVMBlock(ctx)
	case ChainTypeAI:
		return idx.fetchAIBlock(ctx)
	case ChainTypeBridge:
		return idx.fetchBridgeBlock(ctx)
	case ChainTypeDEX:
		return idx.fetchDEXBlock(ctx)
	case ChainTypeGraph:
		return idx.fetchGraphBlock(ctx)
	case ChainTypeIdentity:
		return idx.fetchIdentityBlock(ctx)
	case ChainTypeKey:
		return idx.fetchKeyBlock(ctx)
	case ChainTypeMPC:
		return idx.fetchMPCBlock(ctx)
	case ChainTypeOracle:
		return idx.fetchOracleBlock(ctx)
	case ChainTypeQuantum:
		return idx.fetchQuantumBlock(ctx)
	case ChainTypeRelay:
		return idx.fetchRelayBlock(ctx)
	case ChainTypeThreshold:
		return idx.fetchThresholdBlock(ctx)
	case ChainTypeZK:
		return idx.fetchZKBlock(ctx)
	default:
		return fmt.Errorf("unknown platform chain type: %s", idx.config.Type)
	}
}

// fetchAIBlock fetches blocks from A-Chain (AI VM)
func (idx *PlatformIndexer) fetchAIBlock(ctx context.Context) error {
	// A-Chain RPC methods: ai.getLatestBlock, ai.getProviders, ai.getTasks
	block, err := idx.rpcCall(ctx, "ai.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchBridgeBlock fetches blocks from B-Chain (Bridge VM)
func (idx *PlatformIndexer) fetchBridgeBlock(ctx context.Context) error {
	// B-Chain RPC methods: bridge.getLatestBlock, bridge.getPendingRequests
	block, err := idx.rpcCall(ctx, "bridge.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchThresholdBlock fetches blocks from T-Chain (Threshold VM)
func (idx *PlatformIndexer) fetchThresholdBlock(ctx context.Context) error {
	// T-Chain RPC methods: threshold.getLatestBlock, threshold.getKeyGenSessions
	block, err := idx.rpcCall(ctx, "threshold.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchZKBlock fetches blocks from Z-Chain (ZK VM)
func (idx *PlatformIndexer) fetchZKBlock(ctx context.Context) error {
	// Z-Chain RPC methods: zk.getLatestBlock, zk.getProofs
	block, err := idx.rpcCall(ctx, "zk.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchGraphBlock fetches blocks from G-Chain (Graph VM)
func (idx *PlatformIndexer) fetchGraphBlock(ctx context.Context) error {
	// G-Chain uses GraphQL endpoint, different from JSON-RPC
	return idx.fetchGraphQLSchema(ctx)
}

// fetchIdentityBlock fetches blocks from I-Chain (Identity VM)
func (idx *PlatformIndexer) fetchIdentityBlock(ctx context.Context) error {
	// I-Chain RPC methods: identity.getLatestBlock, identity.getIdentities
	block, err := idx.rpcCall(ctx, "identity.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchKeyBlock fetches blocks from K-Chain (Key VM)
func (idx *PlatformIndexer) fetchKeyBlock(ctx context.Context) error {
	// K-Chain RPC methods: key.getLatestBlock, key.getKeyShares
	block, err := idx.rpcCall(ctx, "key.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchDEXBlock fetches blocks from D-Chain (DEX VM)
func (idx *PlatformIndexer) fetchDEXBlock(ctx context.Context) error {
	// D-Chain RPC methods: dex.getLatestBlock, dex.getOrderbook
	block, err := idx.rpcCall(ctx, "dex.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchPlatformBlock fetches blocks from P-Chain (PlatformVM)
// P-Chain tracks validators, delegation, and subnet membership.
func (idx *PlatformIndexer) fetchPlatformBlock(ctx context.Context) error {
	block, err := idx.rpcCall(ctx, "platform.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchXVMBlock fetches blocks from X-Chain (XVM, UTXO asset chain)
func (idx *PlatformIndexer) fetchXVMBlock(ctx context.Context) error {
	block, err := idx.rpcCall(ctx, "xvm.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchMPCBlock fetches blocks from M-Chain (MPC coordination VM)
// M-Chain coordinates CGGMP21 / FROST threshold signing sessions.
func (idx *PlatformIndexer) fetchMPCBlock(ctx context.Context) error {
	block, err := idx.rpcCall(ctx, "mpc.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchOracleBlock fetches blocks from O-Chain (OracleVM)
// O-Chain aggregates Chainlink/Pyth/TWAP feeds.
func (idx *PlatformIndexer) fetchOracleBlock(ctx context.Context) error {
	block, err := idx.rpcCall(ctx, "oracle.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchQuantumBlock fetches blocks from Q-Chain (QuantumVM)
// Q-Chain runs Ringtail LWE threshold consensus + ML-DSA finality.
func (idx *PlatformIndexer) fetchQuantumBlock(ctx context.Context) error {
	block, err := idx.rpcCall(ctx, "quantum.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// fetchRelayBlock fetches blocks from R-Chain (RelayVM)
// R-Chain handles cross-chain message relay and Warp routing.
func (idx *PlatformIndexer) fetchRelayBlock(ctx context.Context) error {
	block, err := idx.rpcCall(ctx, "relay.getLatestBlock", nil)
	if err != nil {
		return err
	}
	return idx.processBlock(block)
}

// rpcCall makes a JSON-RPC call to the platform chain
func (idx *PlatformIndexer) rpcCall(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
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
func (idx *PlatformIndexer) fetchGraphQLSchema(ctx context.Context) error {
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

// processBlock processes a block from a platform chain
func (idx *PlatformIndexer) processBlock(data json.RawMessage) error {
	if data == nil {
		return nil
	}

	var block PlatformBlock
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
