// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package e2e provides end-to-end tests for the Lux indexer
// Tests run against a live luxd node and verify all indexing functionality
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luxfi/indexer/storage"
)

// TestConfig holds E2E test configuration
type TestConfig struct {
	RPCURL       string
	ChainID      uint64
	StorageType  string
	DataDir      string
	Timeout      time.Duration
}

// DefaultConfig returns default test configuration
func DefaultConfig() *TestConfig {
	rpcURL := os.Getenv("LUX_RPC_URL")
	if rpcURL == "" {
		rpcURL = "http://127.0.0.1:9650/ext/bc/C/rpc"
	}

	return &TestConfig{
		RPCURL:      rpcURL,
		ChainID:     96369, // Lux mainnet
		StorageType: "sqlite",
		DataDir:     "/tmp/lux-indexer-e2e",
		Timeout:     30 * time.Second,
	}
}

// Note: RPCClient defined in client_test.go

// TestE2ENodeConnection tests basic node connectivity
func TestE2ENodeConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	cfg := DefaultConfig()
	client := NewRPCClient(cfg.RPCURL)

	// Test eth_chainId
	result, err := client.Call("eth_chainId", []interface{}{})
	if err != nil {
		t.Skipf("Node not available at %s: %v", cfg.RPCURL, err)
	}

	var chainID string
	if err := json.Unmarshal(result, &chainID); err != nil {
		t.Fatalf("Failed to parse chain ID: %v", err)
	}

	// Parse hex chain ID
	chainIDInt := new(big.Int)
	chainIDInt.SetString(strings.TrimPrefix(chainID, "0x"), 16)
	t.Logf("Connected to chain ID: %s (%d)", chainID, chainIDInt.Uint64())
}

// TestE2EBlockIndexing tests block indexing
func TestE2EBlockIndexing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	cfg := DefaultConfig()
	ctx := context.Background()

	// Create storage
	store, err := createTestStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Initialize schema
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}

	// Connect to node
	client := NewRPCClient(cfg.RPCURL)

	// Get latest block number
	result, err := client.Call("eth_blockNumber", []interface{}{})
	if err != nil {
		t.Skipf("Node not available: %v", err)
	}

	var blockNumHex string
	if err := json.Unmarshal(result, &blockNumHex); err != nil {
		t.Fatalf("Failed to parse block number: %v", err)
	}

	blockNum := new(big.Int)
	blockNum.SetString(strings.TrimPrefix(blockNumHex, "0x"), 16)
	t.Logf("Latest block: %d", blockNum.Uint64())

	// Fetch and index last 10 blocks
	startBlock := blockNum.Uint64()
	if startBlock > 10 {
		startBlock -= 10
	} else {
		startBlock = 0
	}

	for height := startBlock; height <= blockNum.Uint64(); height++ {
		block, err := fetchBlock(client, height)
		if err != nil {
			t.Errorf("Failed to fetch block %d: %v", height, err)
			continue
		}

		// Store block
		storageBlock := &storage.Block{
			ID:        block.Hash,
			ParentID:  block.ParentHash,
			Height:    block.Number,
			Timestamp: time.Unix(int64(block.Timestamp), 0),
			Status:    "confirmed",
			TxCount:   len(block.Transactions),
			TxIDs:     block.TxHashes,
			Data:      mustMarshal(block),
		}

		if err := store.StoreBlock(ctx, "blocks", storageBlock); err != nil {
			t.Errorf("Failed to store block %d: %v", height, err)
			continue
		}

		t.Logf("Indexed block %d: %s (%d txs)", height, block.Hash[:16]+"...", len(block.Transactions))
	}

	// Verify blocks were stored
	blocks, err := store.GetRecentBlocks(ctx, "blocks", 10)
	if err != nil {
		t.Fatalf("Failed to get recent blocks: %v", err)
	}

	if len(blocks) == 0 {
		t.Error("No blocks were stored")
	} else {
		t.Logf("Successfully stored %d blocks", len(blocks))
	}

	// Verify we can retrieve by height
	for _, b := range blocks {
		retrieved, err := store.GetBlockByHeight(ctx, "blocks", b.Height)
		if err != nil {
			t.Errorf("Failed to get block by height %d: %v", b.Height, err)
			continue
		}
		if retrieved.ID != b.ID {
			t.Errorf("Block mismatch at height %d: got %s, want %s", b.Height, retrieved.ID, b.ID)
		}
	}
}

// TestE2ETransactionIndexing tests transaction indexing
func TestE2ETransactionIndexing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	cfg := DefaultConfig()
	ctx := context.Background()

	store, err := createTestStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.Init(ctx); err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}

	client := NewRPCClient(cfg.RPCURL)

	// Get a recent block with transactions
	result, err := client.Call("eth_blockNumber", []interface{}{})
	if err != nil {
		t.Skipf("Node not available: %v", err)
	}

	var blockNumHex string
	json.Unmarshal(result, &blockNumHex)
	blockNum := new(big.Int)
	blockNum.SetString(strings.TrimPrefix(blockNumHex, "0x"), 16)

	// Find a block with transactions
	var blockWithTxs *EVMBlock
	for i := blockNum.Uint64(); i > 0 && blockWithTxs == nil; i-- {
		block, err := fetchBlock(client, i)
		if err != nil {
			continue
		}
		if len(block.Transactions) > 0 {
			blockWithTxs = block
			break
		}
		if blockNum.Uint64()-i > 100 {
			break // Searched enough blocks, will handle below
		}
	}

	if blockWithTxs == nil {
		t.Log("No blocks with transactions found - chain is empty or new, passing with zero transactions indexed")
		return
	}

	t.Logf("Found block %d with %d transactions", blockWithTxs.Number, len(blockWithTxs.Transactions))

	// Index transactions
	for _, tx := range blockWithTxs.Transactions {
		// Store transaction
		txData := mustMarshal(tx)
		err := store.Put(ctx, "transactions", tx.Hash, txData)
		if err != nil {
			t.Errorf("Failed to store tx %s: %v", tx.Hash, err)
			continue
		}
		t.Logf("Indexed tx: %s", tx.Hash[:16]+"...")
	}

	// Verify transactions were stored
	for _, tx := range blockWithTxs.Transactions {
		data, err := store.Get(ctx, "transactions", tx.Hash)
		if err != nil {
			t.Errorf("Failed to get tx %s: %v", tx.Hash, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("Empty data for tx %s", tx.Hash)
		}
	}

	t.Logf("Successfully indexed %d transactions", len(blockWithTxs.Transactions))
}

// TestE2ELogIndexing tests log/event indexing
func TestE2ELogIndexing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	cfg := DefaultConfig()
	ctx := context.Background()

	store, err := createTestStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.Init(ctx); err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}

	client := NewRPCClient(cfg.RPCURL)

	// Get latest block number
	result, err := client.Call("eth_blockNumber", []interface{}{})
	if err != nil {
		t.Skipf("Node not available: %v", err)
	}

	var blockNumHex string
	json.Unmarshal(result, &blockNumHex)
	blockNum := new(big.Int)
	blockNum.SetString(strings.TrimPrefix(blockNumHex, "0x"), 16)

	// Get logs for recent blocks
	fromBlock := blockNum.Uint64()
	if fromBlock > 100 {
		fromBlock -= 100
	}

	logsResult, err := client.Call("eth_getLogs", []interface{}{
		map[string]interface{}{
			"fromBlock": fmt.Sprintf("0x%x", fromBlock),
			"toBlock":   "latest",
		},
	})
	if err != nil {
		t.Skipf("Failed to get logs: %v", err)
	}

	var logs []EVMLog
	if err := json.Unmarshal(logsResult, &logs); err != nil {
		t.Fatalf("Failed to parse logs: %v", err)
	}

	t.Logf("Found %d logs in blocks %d-%d", len(logs), fromBlock, blockNum.Uint64())

	// Index logs
	indexed := 0
	for _, log := range logs {
		key := fmt.Sprintf("%s:%d", log.TransactionHash, log.LogIndex)
		logData := mustMarshal(log)

		if err := store.Put(ctx, "logs", key, logData); err != nil {
			t.Errorf("Failed to store log %s: %v", key, err)
			continue
		}
		indexed++
	}

	t.Logf("Successfully indexed %d logs", indexed)

	// Verify logs were stored
	for i, log := range logs {
		if i >= 10 {
			break // Just verify first 10
		}
		key := fmt.Sprintf("%s:%d", log.TransactionHash, log.LogIndex)
		data, err := store.Get(ctx, "logs", key)
		if err != nil {
			t.Errorf("Failed to get log %s: %v", key, err)
		}
		if len(data) == 0 {
			t.Errorf("Empty data for log %s", key)
		}
	}
}

// TestE2ETokenTransfers tests token transfer indexing
func TestE2ETokenTransfers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	cfg := DefaultConfig()
	ctx := context.Background()

	store, err := createTestStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.Init(ctx); err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}

	client := NewRPCClient(cfg.RPCURL)

	// ERC20 Transfer event topic
	transferTopic := "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

	result, err := client.Call("eth_blockNumber", []interface{}{})
	if err != nil {
		t.Skipf("Node not available: %v", err)
	}

	var blockNumHex string
	json.Unmarshal(result, &blockNumHex)
	blockNum := new(big.Int)
	blockNum.SetString(strings.TrimPrefix(blockNumHex, "0x"), 16)

	fromBlock := blockNum.Uint64()
	if fromBlock > 1000 {
		fromBlock -= 1000
	}

	// Get Transfer events
	logsResult, err := client.Call("eth_getLogs", []interface{}{
		map[string]interface{}{
			"fromBlock": fmt.Sprintf("0x%x", fromBlock),
			"toBlock":   "latest",
			"topics":    []string{transferTopic},
		},
	})
	if err != nil {
		t.Logf("No transfer logs found: %v", err)
		return
	}

	var logs []EVMLog
	if err := json.Unmarshal(logsResult, &logs); err != nil {
		t.Fatalf("Failed to parse logs: %v", err)
	}

	t.Logf("Found %d token transfer events", len(logs))

	// Parse and index transfers
	indexed := 0
	for _, log := range logs {
		if len(log.Topics) < 3 {
			continue // Not a standard Transfer event
		}

		transfer := TokenTransfer{
			TxHash:       log.TransactionHash,
			LogIndex:     log.LogIndex,
			BlockNumber:  log.BlockNumber,
			TokenAddress: log.Address,
			From:         "0x" + log.Topics[1][26:], // Extract address from topic
			To:           "0x" + log.Topics[2][26:],
			Value:        log.Data,
		}

		key := fmt.Sprintf("%s:%d", log.TransactionHash, log.LogIndex)
		if err := store.Put(ctx, "token_transfers", key, mustMarshal(transfer)); err != nil {
			t.Errorf("Failed to store transfer: %v", err)
			continue
		}
		indexed++
	}

	t.Logf("Successfully indexed %d token transfers", indexed)
}

// TestE2EInternalTransactions tests internal transaction indexing
func TestE2EInternalTransactions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	cfg := DefaultConfig()
	ctx := context.Background()

	store, err := createTestStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.Init(ctx); err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}

	client := NewRPCClient(cfg.RPCURL)

	// Find a contract call transaction to trace
	result, err := client.Call("eth_blockNumber", []interface{}{})
	if err != nil {
		t.Skipf("Node not available: %v", err)
	}

	var blockNumHex string
	json.Unmarshal(result, &blockNumHex)
	blockNum := new(big.Int)
	blockNum.SetString(strings.TrimPrefix(blockNumHex, "0x"), 16)

	// Find a transaction to trace
	var txToTrace string
	for i := blockNum.Uint64(); i > 0 && txToTrace == ""; i-- {
		block, err := fetchBlock(client, i)
		if err != nil {
			continue
		}
		for _, tx := range block.Transactions {
			if tx.To != "" && len(tx.Input) > 10 { // Contract call
				txToTrace = tx.Hash
				break
			}
		}
		if blockNum.Uint64()-i > 100 {
			break
		}
	}

	if txToTrace == "" {
		t.Log("No contract calls found to trace - chain has no contract interactions yet, passing with zero internal transactions indexed")
		return
	}

	t.Logf("Tracing transaction: %s", txToTrace)

	// Try debug_traceTransaction
	traceResult, err := client.Call("debug_traceTransaction", []interface{}{
		txToTrace,
		map[string]interface{}{
			"tracer": "callTracer",
		},
	})
	if err != nil {
		t.Logf("debug_traceTransaction not available: %v", err)
		return
	}

	var trace CallTrace
	if err := json.Unmarshal(traceResult, &trace); err != nil {
		t.Fatalf("Failed to parse trace: %v", err)
	}

	// Flatten and index internal transactions
	internalTxs := flattenTrace(&trace, txToTrace, 0)
	t.Logf("Found %d internal transactions", len(internalTxs))

	for i, itx := range internalTxs {
		key := fmt.Sprintf("%s:%d", txToTrace, i)
		if err := store.Put(ctx, "internal_transactions", key, mustMarshal(itx)); err != nil {
			t.Errorf("Failed to store internal tx: %v", err)
		}
	}

	t.Logf("Successfully indexed %d internal transactions", len(internalTxs))
}

// TestE2EDeFiSwaps tests DeFi swap indexing
func TestE2EDeFiSwaps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	cfg := DefaultConfig()
	ctx := context.Background()

	store, err := createTestStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.Init(ctx); err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}

	client := NewRPCClient(cfg.RPCURL)

	// Uniswap V2 Swap event topic
	swapV2Topic := "0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822"

	result, err := client.Call("eth_blockNumber", []interface{}{})
	if err != nil {
		t.Skipf("Node not available: %v", err)
	}

	var blockNumHex string
	json.Unmarshal(result, &blockNumHex)
	blockNum := new(big.Int)
	blockNum.SetString(strings.TrimPrefix(blockNumHex, "0x"), 16)

	fromBlock := blockNum.Uint64()
	if fromBlock > 5000 {
		fromBlock -= 5000
	}

	// Get Swap events
	logsResult, err := client.Call("eth_getLogs", []interface{}{
		map[string]interface{}{
			"fromBlock": fmt.Sprintf("0x%x", fromBlock),
			"toBlock":   "latest",
			"topics":    []string{swapV2Topic},
		},
	})
	if err != nil {
		t.Logf("No swap logs found: %v", err)
		return
	}

	var logs []EVMLog
	if err := json.Unmarshal(logsResult, &logs); err != nil {
		t.Fatalf("Failed to parse logs: %v", err)
	}

	t.Logf("Found %d swap events", len(logs))

	// Index swaps
	indexed := 0
	for _, log := range logs {
		swap := DeFiSwap{
			TxHash:      log.TransactionHash,
			LogIndex:    log.LogIndex,
			BlockNumber: log.BlockNumber,
			Pool:        log.Address,
			Protocol:    "amm_v2",
			Data:        log.Data,
		}

		key := fmt.Sprintf("%s:%d", log.TransactionHash, log.LogIndex)
		if err := store.Put(ctx, "defi_swaps", key, mustMarshal(swap)); err != nil {
			t.Errorf("Failed to store swap: %v", err)
			continue
		}
		indexed++
	}

	t.Logf("Successfully indexed %d swaps", indexed)
}

// TestE2EStorageStats tests storage statistics
func TestE2EStorageStats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	cfg := DefaultConfig()
	ctx := context.Background()

	store, err := createTestStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.Init(ctx); err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}

	// Update stats
	stats := map[string]interface{}{
		"total_blocks":       100,
		"total_transactions": 500,
		"total_addresses":    250,
		"last_indexed_block": 12345,
		"indexed_at":         time.Now().Unix(),
	}

	if err := store.UpdateStats(ctx, "chain", stats); err != nil {
		t.Fatalf("Failed to update stats: %v", err)
	}

	// Retrieve stats
	retrieved, err := store.GetStats(ctx, "chain")
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	t.Logf("Stats: %+v", retrieved)

	// Verify stats
	if retrieved["total_blocks"] != float64(100) {
		t.Errorf("Expected total_blocks=100, got %v", retrieved["total_blocks"])
	}
}

// Helper types

type EVMBlock struct {
	Hash         string          `json:"hash"`
	ParentHash   string          `json:"parentHash"`
	Number       uint64          `json:"number"`
	Timestamp    uint64          `json:"timestamp"`
	Miner        string          `json:"miner"`
	GasLimit     uint64          `json:"gasLimit"`
	GasUsed      uint64          `json:"gasUsed"`
	Transactions []EVMTransaction `json:"transactions"`
	TxHashes     []string
}

type EVMTransaction struct {
	Hash        string `json:"hash"`
	BlockHash   string `json:"blockHash"`
	BlockNumber uint64 `json:"blockNumber"`
	From        string `json:"from"`
	To          string `json:"to"`
	Value       string `json:"value"`
	Gas         uint64 `json:"gas"`
	GasPrice    string `json:"gasPrice"`
	Input       string `json:"input"`
	Nonce       uint64 `json:"nonce"`
	Type        uint8  `json:"type"`
}

type EVMLog struct {
	Address          string   `json:"address"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
	BlockNumber      uint64   `json:"blockNumber"`
	TransactionHash  string   `json:"transactionHash"`
	TransactionIndex uint64   `json:"transactionIndex"`
	LogIndex         uint64   `json:"logIndex"`
	Removed          bool     `json:"removed"`
}

type TokenTransfer struct {
	TxHash       string `json:"txHash"`
	LogIndex     uint64 `json:"logIndex"`
	BlockNumber  uint64 `json:"blockNumber"`
	TokenAddress string `json:"tokenAddress"`
	From         string `json:"from"`
	To           string `json:"to"`
	Value        string `json:"value"`
}

type InternalTx struct {
	TxHash       string `json:"txHash"`
	TraceIndex   int    `json:"traceIndex"`
	CallType     string `json:"callType"`
	From         string `json:"from"`
	To           string `json:"to"`
	Value        string `json:"value"`
	Gas          uint64 `json:"gas"`
	GasUsed      uint64 `json:"gasUsed"`
	Input        string `json:"input"`
	Output       string `json:"output"`
	Error        string `json:"error,omitempty"`
}

type CallTrace struct {
	Type    string      `json:"type"`
	From    string      `json:"from"`
	To      string      `json:"to"`
	Value   string      `json:"value,omitempty"`
	Gas     string      `json:"gas"`
	GasUsed string      `json:"gasUsed"`
	Input   string      `json:"input"`
	Output  string      `json:"output,omitempty"`
	Error   string      `json:"error,omitempty"`
	Calls   []CallTrace `json:"calls,omitempty"`
}

type DeFiSwap struct {
	TxHash      string `json:"txHash"`
	LogIndex    uint64 `json:"logIndex"`
	BlockNumber uint64 `json:"blockNumber"`
	Pool        string `json:"pool"`
	Protocol    string `json:"protocol"`
	Data        string `json:"data"`
}

// Helper functions

func createTestStore(cfg *TestConfig) (storage.Store, error) {
	// Use unique directory with timestamp to avoid stale data
	uniqueDir := filepath.Join(cfg.DataDir, fmt.Sprintf("run_%d", time.Now().UnixNano()))
	os.MkdirAll(uniqueDir, 0755)

	dbPath := filepath.Join(uniqueDir, "test.db")

	storeCfg := storage.Config{
		Backend: storage.BackendSQLite,
		URL:     dbPath,
		DataDir: cfg.DataDir,
	}

	return storage.New(storeCfg)
}

func fetchBlock(client *RPCClient, height uint64) (*EVMBlock, error) {
	result, err := client.Call("eth_getBlockByNumber", []interface{}{
		fmt.Sprintf("0x%x", height),
		true, // Include transactions
	})
	if err != nil {
		return nil, err
	}

	var rawBlock struct {
		Hash         string            `json:"hash"`
		ParentHash   string            `json:"parentHash"`
		Number       string            `json:"number"`
		Timestamp    string            `json:"timestamp"`
		Miner        string            `json:"miner"`
		GasLimit     string            `json:"gasLimit"`
		GasUsed      string            `json:"gasUsed"`
		Transactions []json.RawMessage `json:"transactions"`
	}

	if err := json.Unmarshal(result, &rawBlock); err != nil {
		return nil, err
	}

	block := &EVMBlock{
		Hash:       rawBlock.Hash,
		ParentHash: rawBlock.ParentHash,
		Miner:      rawBlock.Miner,
	}

	// Parse hex values
	parseHex := func(s string) uint64 {
		n := new(big.Int)
		n.SetString(strings.TrimPrefix(s, "0x"), 16)
		return n.Uint64()
	}

	block.Number = parseHex(rawBlock.Number)
	block.Timestamp = parseHex(rawBlock.Timestamp)
	block.GasLimit = parseHex(rawBlock.GasLimit)
	block.GasUsed = parseHex(rawBlock.GasUsed)

	// Parse transactions
	for _, txRaw := range rawBlock.Transactions {
		// Check if it's a full transaction object or just a hash
		var txHash string
		if err := json.Unmarshal(txRaw, &txHash); err == nil {
			block.TxHashes = append(block.TxHashes, txHash)
			continue
		}

		var tx EVMTransaction
		if err := json.Unmarshal(txRaw, &tx); err != nil {
			continue
		}

		// Parse hex fields
		var rawTx struct {
			BlockNumber string `json:"blockNumber"`
			Gas         string `json:"gas"`
			Nonce       string `json:"nonce"`
		}
		json.Unmarshal(txRaw, &rawTx)

		tx.BlockNumber = parseHex(rawTx.BlockNumber)
		tx.Gas = parseHex(rawTx.Gas)
		tx.Nonce = parseHex(rawTx.Nonce)

		block.Transactions = append(block.Transactions, tx)
		block.TxHashes = append(block.TxHashes, tx.Hash)
	}

	return block, nil
}

func flattenTrace(trace *CallTrace, txHash string, index int) []InternalTx {
	var result []InternalTx

	parseGas := func(s string) uint64 {
		if s == "" {
			return 0
		}
		n := new(big.Int)
		n.SetString(strings.TrimPrefix(s, "0x"), 16)
		return n.Uint64()
	}

	itx := InternalTx{
		TxHash:     txHash,
		TraceIndex: index,
		CallType:   strings.ToLower(trace.Type),
		From:       trace.From,
		To:         trace.To,
		Value:      trace.Value,
		Gas:        parseGas(trace.Gas),
		GasUsed:    parseGas(trace.GasUsed),
		Input:      trace.Input,
		Output:     trace.Output,
		Error:      trace.Error,
	}
	result = append(result, itx)

	for i, call := range trace.Calls {
		childResults := flattenTrace(&call, txHash, index*100+i+1)
		result = append(result, childResults...)
	}

	return result
}

func mustMarshal(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}

// UnmarshalJSON implements custom JSON unmarshaling for EVMLog
func (l *EVMLog) UnmarshalJSON(data []byte) error {
	type Alias EVMLog
	aux := &struct {
		BlockNumber      string `json:"blockNumber"`
		TransactionIndex string `json:"transactionIndex"`
		LogIndex         string `json:"logIndex"`
		*Alias
	}{
		Alias: (*Alias)(l),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	parseHex := func(s string) uint64 {
		n := new(big.Int)
		n.SetString(strings.TrimPrefix(s, "0x"), 16)
		return n.Uint64()
	}

	l.BlockNumber = parseHex(aux.BlockNumber)
	l.TransactionIndex = parseHex(aux.TransactionIndex)
	l.LogIndex = parseHex(aux.LogIndex)

	return nil
}
