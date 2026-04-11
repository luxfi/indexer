// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package multichain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// ---- L2 Chain Config Parsing Tests ----

func TestOptimismChainConfigParsing(t *testing.T) {
	yaml := `
lux:
  lux_cchain:
    chain_id: 96369
    name: "Lux C-Chain"
    symbol: "LUX"
    type: evm
    rpc: "https://api.lux.network/ext/bc/C/rpc"
    enabled: true
ethereum:
  optimism:
    chain_id: 10
    name: "Optimism"
    symbol: "ETH"
    type: evm
    rpc: "https://mainnet.optimism.io"
    enabled: true
    batch_size: 200
    poll_interval: "2s"
    protocols:
      - type: uniswap_v3
        factory: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
        enabled: true
      - type: aave_v3
        address: "0x794a61358D6845594F94dc1DB02A252b5b4814aD"
        enabled: true
`
	// Write temp file and parse
	tmpFile := t.TempDir() + "/chains.yaml"
	if err := writeTestFile(tmpFile, yaml); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	config, err := LoadChainsConfig(tmpFile)
	if err != nil {
		t.Fatalf("LoadChainsConfig failed: %v", err)
	}

	// Find optimism config
	var opConfig *ChainConfig
	for i := range config.Chains {
		if config.Chains[i].ID == "optimism" {
			opConfig = &config.Chains[i]
			break
		}
	}

	if opConfig == nil {
		t.Fatal("optimism chain not found in config")
	}

	if opConfig.ChainID != 10 {
		t.Errorf("ChainID = %d, want 10", opConfig.ChainID)
	}
	if opConfig.Name != "Optimism" {
		t.Errorf("Name = %q, want Optimism", opConfig.Name)
	}
	if opConfig.Type != ChainTypeEVM {
		t.Errorf("Type = %q, want evm", opConfig.Type)
	}
	if opConfig.BatchSize != 200 {
		t.Errorf("BatchSize = %d, want 200", opConfig.BatchSize)
	}
	if opConfig.PollInterval != 2*time.Second {
		t.Errorf("PollInterval = %v, want 2s", opConfig.PollInterval)
	}
	if len(opConfig.Protocols) != 2 {
		t.Errorf("Protocols count = %d, want 2", len(opConfig.Protocols))
	}
}

func TestArbitrumChainConfigParsing(t *testing.T) {
	yaml := `
ethereum:
  arbitrum:
    chain_id: 42161
    name: "Arbitrum One"
    symbol: "ETH"
    type: evm
    rpc: "https://arb1.arbitrum.io/rpc"
    enabled: true
    start_block: 0
    batch_size: 500
    protocols:
      - type: gmx_v2
        address: "0x70d95587d40A2caf56bd97485aB3Eec10Bee6336"
        enabled: true
      - type: uniswap_v3
        factory: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
        enabled: true
      - type: curve
        enabled: false
`
	tmpFile := t.TempDir() + "/chains.yaml"
	if err := writeTestFile(tmpFile, yaml); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	config, err := LoadChainsConfig(tmpFile)
	if err != nil {
		t.Fatalf("LoadChainsConfig failed: %v", err)
	}

	var arbConfig *ChainConfig
	for i := range config.Chains {
		if config.Chains[i].ID == "arbitrum" {
			arbConfig = &config.Chains[i]
			break
		}
	}

	if arbConfig == nil {
		t.Fatal("arbitrum chain not found in config")
	}

	if arbConfig.ChainID != 42161 {
		t.Errorf("ChainID = %d, want 42161", arbConfig.ChainID)
	}
	if arbConfig.BatchSize != 500 {
		t.Errorf("BatchSize = %d, want 500", arbConfig.BatchSize)
	}
	// Curve is disabled, so only 2 protocols
	if len(arbConfig.Protocols) != 2 {
		t.Errorf("Protocols count = %d, want 2 (curve disabled)", len(arbConfig.Protocols))
	}
}

func TestMultipleL2ChainsConfig(t *testing.T) {
	yaml := `
ethereum:
  optimism:
    chain_id: 10
    name: "Optimism"
    symbol: "ETH"
    type: evm
    rpc: "https://mainnet.optimism.io"
    enabled: true
  base:
    chain_id: 8453
    name: "Base"
    symbol: "ETH"
    type: evm
    rpc: "https://mainnet.base.org"
    enabled: true
  arbitrum:
    chain_id: 42161
    name: "Arbitrum One"
    symbol: "ETH"
    type: evm
    rpc: "https://arb1.arbitrum.io/rpc"
    enabled: true
  scroll:
    chain_id: 534352
    name: "Scroll"
    symbol: "ETH"
    type: evm
    rpc: "https://rpc.scroll.io"
    enabled: true
  zksync:
    chain_id: 324
    name: "zkSync Era"
    symbol: "ETH"
    type: evm
    rpc: "https://mainnet.era.zksync.io"
    enabled: true
`
	tmpFile := t.TempDir() + "/chains.yaml"
	if err := writeTestFile(tmpFile, yaml); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	config, err := LoadChainsConfig(tmpFile)
	if err != nil {
		t.Fatalf("LoadChainsConfig failed: %v", err)
	}

	expectedChains := map[string]uint64{
		"optimism": 10,
		"base":     8453,
		"arbitrum": 42161,
		"scroll":   534352,
		"zksync":   324,
	}

	found := make(map[string]bool)
	for _, chain := range config.Chains {
		if expectedID, ok := expectedChains[chain.ID]; ok {
			found[chain.ID] = true
			if chain.ChainID != expectedID {
				t.Errorf("%s: ChainID = %d, want %d", chain.ID, chain.ChainID, expectedID)
			}
		}
	}

	for name := range expectedChains {
		if !found[name] {
			t.Errorf("chain %q not found in parsed config", name)
		}
	}
}

func TestDisabledL2ChainConfig(t *testing.T) {
	yaml := `
ethereum:
  optimism:
    chain_id: 10
    name: "Optimism"
    symbol: "ETH"
    type: evm
    rpc: "https://mainnet.optimism.io"
    enabled: false
  base:
    chain_id: 8453
    name: "Base"
    symbol: "ETH"
    type: evm
    rpc: "https://mainnet.base.org"
    enabled: true
`
	tmpFile := t.TempDir() + "/chains.yaml"
	if err := writeTestFile(tmpFile, yaml); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	config, err := LoadChainsConfig(tmpFile)
	if err != nil {
		t.Fatalf("LoadChainsConfig failed: %v", err)
	}

	for _, chain := range config.Chains {
		if chain.ID == "optimism" {
			t.Error("disabled optimism chain should not appear in config")
		}
	}
}

// ---- L2 Batch Detection Tests ----

// detectL2BatchFromCalldata checks if a transaction's calldata looks like an L2 batch submission
func detectL2BatchFromCalldata(input string) (string, bool) {
	if len(input) < 10 {
		return "", false
	}

	selector := input[:10]
	// Known L2 batch submission selectors
	batchSelectors := map[string]string{
		"0xd4f9283a": "optimism_appendSequencerBatch",
		"0x8f111f3c": "optimism_proposer_appendBatch",
		"0x3e5aa082": "arbitrum_addSequencerL2BatchFromOrigin",
		"0x6a7eb3ef": "arbitrum_addSequencerL2BatchFromBlobs",
		"0x1325aca0": "scroll_commitBatch",
		"0x4f099e3d": "scroll_finalizeBatchWithProof",
		"0x701f58c5": "zksync_commitBatches",
		"0x7739cbe7": "zksync_proveBatches",
		"0xc3d93e7c": "zksync_executeBatches",
	}

	if name, ok := batchSelectors[selector]; ok {
		return name, true
	}
	return "", false
}

func TestDetectL2BatchFromCalldata(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantOK   bool
	}{
		{
			name:     "Optimism appendSequencerBatch",
			input:    "0xd4f9283a" + strings.Repeat("00", 100),
			wantName: "optimism_appendSequencerBatch",
			wantOK:   true,
		},
		{
			name:     "Arbitrum addSequencerL2BatchFromOrigin",
			input:    "0x3e5aa082" + strings.Repeat("00", 100),
			wantName: "arbitrum_addSequencerL2BatchFromOrigin",
			wantOK:   true,
		},
		{
			name:     "Arbitrum addSequencerL2BatchFromBlobs",
			input:    "0x6a7eb3ef" + strings.Repeat("00", 100),
			wantName: "arbitrum_addSequencerL2BatchFromBlobs",
			wantOK:   true,
		},
		{
			name:     "Scroll commitBatch",
			input:    "0x1325aca0" + strings.Repeat("00", 100),
			wantName: "scroll_commitBatch",
			wantOK:   true,
		},
		{
			name:     "Scroll finalizeBatch",
			input:    "0x4f099e3d" + strings.Repeat("00", 100),
			wantName: "scroll_finalizeBatchWithProof",
			wantOK:   true,
		},
		{
			name:     "ZkSync commitBatches",
			input:    "0x701f58c5" + strings.Repeat("00", 100),
			wantName: "zksync_commitBatches",
			wantOK:   true,
		},
		{
			name:     "ZkSync proveBatches",
			input:    "0x7739cbe7" + strings.Repeat("00", 100),
			wantName: "zksync_proveBatches",
			wantOK:   true,
		},
		{
			name:     "ZkSync executeBatches",
			input:    "0xc3d93e7c" + strings.Repeat("00", 100),
			wantName: "zksync_executeBatches",
			wantOK:   true,
		},
		{
			name:   "ERC20 transfer (not batch)",
			input:  "0xa9059cbb" + strings.Repeat("00", 100),
			wantOK: false,
		},
		{
			name:   "short input",
			input:  "0x1234",
			wantOK: false,
		},
		{
			name:   "empty",
			input:  "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, ok := detectL2BatchFromCalldata(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}

// ---- Bridge Event Correlation Tests ----

// BridgeEventPair correlates a deposit on L1 with a mint on L2
type BridgeEventPair struct {
	L1DepositTxHash string    `json:"l1DepositTxHash"`
	L1BlockNumber   uint64    `json:"l1BlockNumber"`
	L2MintTxHash    string    `json:"l2MintTxHash,omitempty"`
	L2BlockNumber   uint64    `json:"l2BlockNumber,omitempty"`
	Token           string    `json:"token"`
	Amount          string    `json:"amount"`
	Sender          string    `json:"sender"`
	Recipient       string    `json:"recipient"`
	Status          string    `json:"status"` // pending, confirmed, failed
	CorrelationID   string    `json:"correlationId"`
}

func TestBridgeEventCorrelation(t *testing.T) {
	tests := []struct {
		name string
		pair BridgeEventPair
	}{
		{
			name: "ETH deposit confirmed",
			pair: BridgeEventPair{
				L1DepositTxHash: "0xl1deposit",
				L1BlockNumber:   18000000,
				L2MintTxHash:    "0xl2mint",
				L2BlockNumber:   100000,
				Token:           "0x0000000000000000000000000000000000000000",
				Amount:          "1000000000000000000",
				Sender:          "0xuser",
				Recipient:       "0xuser",
				Status:          "confirmed",
				CorrelationID:   "0x" + strings.Repeat("ab", 32),
			},
		},
		{
			name: "ERC20 deposit pending",
			pair: BridgeEventPair{
				L1DepositTxHash: "0xl1erc20deposit",
				L1BlockNumber:   18000001,
				Token:           "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
				Amount:          "1000000000",
				Sender:          "0xuser",
				Recipient:       "0xuser",
				Status:          "pending",
				CorrelationID:   "0x" + strings.Repeat("cd", 32),
			},
		},
		{
			name: "failed deposit",
			pair: BridgeEventPair{
				L1DepositTxHash: "0xl1failed",
				L1BlockNumber:   18000002,
				Token:           "0x0000000000000000000000000000000000000000",
				Amount:          "500000000000000000",
				Sender:          "0xuser",
				Recipient:       "0xuser",
				Status:          "failed",
				CorrelationID:   "0x" + strings.Repeat("ef", 32),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.pair)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var decoded BridgeEventPair
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if decoded.Status != tt.pair.Status {
				t.Errorf("Status = %q, want %q", decoded.Status, tt.pair.Status)
			}
			if decoded.CorrelationID != tt.pair.CorrelationID {
				t.Errorf("CorrelationID mismatch")
			}

			// If confirmed, L2 fields must be set
			if decoded.Status == "confirmed" {
				if decoded.L2MintTxHash == "" {
					t.Error("confirmed pair must have L2MintTxHash")
				}
				if decoded.L2BlockNumber == 0 {
					t.Error("confirmed pair must have L2BlockNumber")
				}
			}

			// Pending pairs should not have L2 fields
			if decoded.Status == "pending" {
				if decoded.L2MintTxHash != "" {
					t.Error("pending pair should not have L2MintTxHash")
				}
			}
		})
	}
}

// ---- Sequencer Batch Submission Tracking Tests ----

// SequencerBatchTracker tracks sequencer batch submissions over time
type SequencerBatchTracker struct {
	ChainID        string `json:"chainId"`
	LastBatchIndex uint64 `json:"lastBatchIndex"`
	TotalBatches   uint64 `json:"totalBatches"`
	TotalL2Blocks  uint64 `json:"totalL2Blocks"`
	AvgBatchSize   float64 `json:"avgBatchSize"`
	LastSubmission time.Time `json:"lastSubmission"`
}

func TestSequencerBatchTracking(t *testing.T) {
	tracker := SequencerBatchTracker{
		ChainID: "optimism",
	}

	// Simulate 10 batch submissions
	batches := make([]struct {
		index    uint64
		l2Start  uint64
		l2End    uint64
	}, 10)

	for i := range batches {
		batches[i].index = uint64(1000 + i)
		batches[i].l2Start = uint64(i * 100)
		batches[i].l2End = uint64((i+1)*100 - 1)
	}

	var totalBlocks uint64
	for _, b := range batches {
		batchSize := b.l2End - b.l2Start + 1
		totalBlocks += batchSize
		tracker.LastBatchIndex = b.index
		tracker.TotalBatches++
		tracker.TotalL2Blocks = totalBlocks
	}
	tracker.AvgBatchSize = float64(totalBlocks) / float64(tracker.TotalBatches)
	tracker.LastSubmission = time.Now()

	if tracker.TotalBatches != 10 {
		t.Errorf("TotalBatches = %d, want 10", tracker.TotalBatches)
	}
	if tracker.TotalL2Blocks != 1000 {
		t.Errorf("TotalL2Blocks = %d, want 1000", tracker.TotalL2Blocks)
	}
	if tracker.AvgBatchSize != 100 {
		t.Errorf("AvgBatchSize = %f, want 100", tracker.AvgBatchSize)
	}
	if tracker.LastBatchIndex != 1009 {
		t.Errorf("LastBatchIndex = %d, want 1009", tracker.LastBatchIndex)
	}
}

// ---- Challenge/Dispute Game Detection Tests ----

// DisputeGame represents an Optimism-style dispute game
type DisputeGame struct {
	GameIndex   uint64    `json:"gameIndex"`
	GameType    uint32    `json:"gameType"` // 0=Cannon, 1=Permissioned, 2=Alphabet
	RootClaim   string    `json:"rootClaim"`
	L2BlockNum  uint64    `json:"l2BlockNum"`
	Status      string    `json:"status"` // in_progress, challenger_wins, defender_wins
	Creator     string    `json:"creator"`
	Challenger  string    `json:"challenger,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	ResolvedAt  time.Time `json:"resolvedAt,omitempty"`
	L1TxHash    string    `json:"l1TxHash"`
}

func TestDisputeGameSerialization(t *testing.T) {
	tests := []struct {
		name string
		game DisputeGame
	}{
		{
			name: "in progress",
			game: DisputeGame{
				GameIndex:  100,
				GameType:   0,
				RootClaim:  "0x" + strings.Repeat("ab", 32),
				L2BlockNum: 10000000,
				Status:     "in_progress",
				Creator:    "0xproposer",
				CreatedAt:  time.Now().Truncate(time.Second),
				L1TxHash:   "0xl1tx",
			},
		},
		{
			name: "defender wins",
			game: DisputeGame{
				GameIndex:  101,
				GameType:   0,
				RootClaim:  "0x" + strings.Repeat("cd", 32),
				L2BlockNum: 10000100,
				Status:     "defender_wins",
				Creator:    "0xproposer",
				Challenger: "0xchallenger",
				CreatedAt:  time.Now().Add(-7 * 24 * time.Hour).Truncate(time.Second),
				ResolvedAt: time.Now().Truncate(time.Second),
				L1TxHash:   "0xl1tx2",
			},
		},
		{
			name: "challenger wins",
			game: DisputeGame{
				GameIndex:  102,
				GameType:   0,
				RootClaim:  "0x" + strings.Repeat("ef", 32),
				L2BlockNum: 10000200,
				Status:     "challenger_wins",
				Creator:    "0xproposer",
				Challenger: "0xchallenger",
				CreatedAt:  time.Now().Add(-7 * 24 * time.Hour).Truncate(time.Second),
				ResolvedAt: time.Now().Truncate(time.Second),
				L1TxHash:   "0xl1tx3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.game)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var decoded DisputeGame
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if decoded.GameIndex != tt.game.GameIndex {
				t.Errorf("GameIndex = %d, want %d", decoded.GameIndex, tt.game.GameIndex)
			}
			if decoded.Status != tt.game.Status {
				t.Errorf("Status = %q, want %q", decoded.Status, tt.game.Status)
			}

			// Resolved games must have challenger and resolution time
			if decoded.Status != "in_progress" {
				if decoded.Challenger == "" {
					t.Error("resolved game must have challenger")
				}
				if decoded.ResolvedAt.IsZero() {
					t.Error("resolved game must have resolution time")
				}
			}
		})
	}
}

func TestDisputeGameTypes(t *testing.T) {
	tests := []struct {
		gameType uint32
		name     string
	}{
		{0, "Cannon"},
		{1, "Permissioned"},
		{2, "Alphabet"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			game := DisputeGame{GameType: tt.gameType}
			var typeName string
			switch game.GameType {
			case 0:
				typeName = "Cannon"
			case 1:
				typeName = "Permissioned"
			case 2:
				typeName = "Alphabet"
			default:
				typeName = "Unknown"
			}
			if typeName != tt.name {
				t.Errorf("type = %q, want %q", typeName, tt.name)
			}
		})
	}
}

// ---- Mock RPC Server Tests ----

func TestMockL2RPCBlockNumber(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string        `json:"method"`
			Params []interface{} `json:"params"`
			ID     int           `json:"id"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		var resp interface{}
		switch req.Method {
		case "eth_blockNumber":
			resp = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  "0x989680", // 10000000
			}
		default:
			resp = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error":   map[string]interface{}{"code": -32601, "message": "method not found"},
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create indexer pointing at mock
	config := ChainConfig{
		ID:      "optimism-test",
		ChainID: 10,
		Name:    "Optimism Test",
		Type:    ChainTypeEVM,
		RPC:     server.URL,
		Enabled: true,
	}

	indexer, err := NewEVMIndexer(config, nil)
	if err != nil {
		t.Fatalf("NewEVMIndexer failed: %v", err)
	}

	block, err := indexer.GetLatestBlock(context.Background())
	if err != nil {
		t.Fatalf("GetLatestBlock failed: %v", err)
	}

	if block != 10000000 {
		t.Errorf("block = %d, want 10000000", block)
	}
}

func TestMockL2RPCGetBlock(t *testing.T) {
	blockResponse := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]interface{}{
			"number":     "0x989680",
			"hash":       "0x" + strings.Repeat("ab", 32),
			"parentHash": "0x" + strings.Repeat("cd", 32),
			"timestamp":  "0x65f5e100",
			"miner":      "0x4200000000000000000000000000000000000011",
			"gasLimit":   "0x1c9c380",
			"gasUsed":    "0xe4e1c0",
			"transactions": []interface{}{
				map[string]interface{}{
					"hash":  "0xtx1",
					"from":  "0xdeaddeaddeaddeaddeaddeaddeaddeaddead0001",
					"to":    "0x4200000000000000000000000000000000000015",
					"type":  "0x7e", // deposit tx
					"value": "0xde0b6b3a7640000",
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(blockResponse)
	}))
	defer server.Close()

	// Verify deposit tx type detection
	result := blockResponse["result"].(map[string]interface{})
	txs := result["transactions"].([]interface{})
	tx := txs[0].(map[string]interface{})
	txType := tx["type"].(string)

	if txType != "0x7e" {
		t.Errorf("tx type = %q, want 0x7e", txType)
	}

	// Verify the block's sequencer address (OP specific)
	miner := result["miner"].(string)
	if miner != "0x4200000000000000000000000000000000000011" {
		t.Errorf("miner = %q, want OP sequencer address", miner)
	}
}

func TestMockL2RPCGetLogs(t *testing.T) {
	// Simulate fetching deposit logs from the OptimismPortal contract
	logsResponse := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"result": []interface{}{
			map[string]interface{}{
				"address":          "0xbEb5Fc579115071764c7423A4f12eDde41f106Ed",
				"topics":          []string{
					"0xb3813568d9991fc951961fcb4c784893574240a28925604d09fc577c55bb7c32", // TransactionDeposited
					"0x0000000000000000000000001234567890123456789012345678901234567890", // from
					"0x0000000000000000000000009876543210987654321098765432109876543210", // to
				},
				"data":             "0x" + strings.Repeat("00", 200),
				"blockNumber":      "0x112a880",
				"transactionHash":  "0x" + strings.Repeat("ab", 32),
				"logIndex":         "0x0",
				"transactionIndex": "0x0",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(logsResponse)
	}))
	defer server.Close()

	result := logsResponse["result"].([]interface{})
	if len(result) != 1 {
		t.Errorf("logs count = %d, want 1", len(result))
	}

	log := result[0].(map[string]interface{})
	topics := log["topics"].([]string)

	// Verify it's a TransactionDeposited event
	if topics[0] != "0xb3813568d9991fc951961fcb4c784893574240a28925604d09fc577c55bb7c32" {
		t.Error("first topic should be TransactionDeposited event signature")
	}

	// Verify contract address is OptimismPortal
	if log["address"] != "0xbEb5Fc579115071764c7423A4f12eDde41f106Ed" {
		t.Error("address should be OptimismPortal")
	}
}

// ---- EVMIndexer L2 Integration Tests ----

func TestEVMIndexerCreation_L2Chains(t *testing.T) {
	l2Configs := []ChainConfig{
		{ID: "optimism", ChainID: 10, Name: "Optimism", Type: ChainTypeEVM, RPC: "https://mainnet.optimism.io", Enabled: true},
		{ID: "arbitrum", ChainID: 42161, Name: "Arbitrum", Type: ChainTypeEVM, RPC: "https://arb1.arbitrum.io/rpc", Enabled: true},
		{ID: "base", ChainID: 8453, Name: "Base", Type: ChainTypeEVM, RPC: "https://mainnet.base.org", Enabled: true},
		{ID: "scroll", ChainID: 534352, Name: "Scroll", Type: ChainTypeEVM, RPC: "https://rpc.scroll.io", Enabled: true},
		{ID: "zksync", ChainID: 324, Name: "zkSync Era", Type: ChainTypeEVM, RPC: "https://mainnet.era.zksync.io", Enabled: true},
	}

	for _, config := range l2Configs {
		t.Run(config.ID, func(t *testing.T) {
			indexer, err := NewEVMIndexer(config, nil)
			if err != nil {
				t.Fatalf("NewEVMIndexer failed: %v", err)
			}

			if indexer.ChainID() != config.ID {
				t.Errorf("ChainID = %q, want %q", indexer.ChainID(), config.ID)
			}
			if indexer.ChainType() != ChainTypeEVM {
				t.Errorf("ChainType = %q, want evm", indexer.ChainType())
			}
		})
	}
}

func TestEVMIndexerWithProtocols(t *testing.T) {
	config := ChainConfig{
		ID:      "optimism",
		ChainID: 10,
		Name:    "Optimism",
		Type:    ChainTypeEVM,
		RPC:     "https://mainnet.optimism.io",
		Enabled: true,
		Protocols: []ProtocolConfig{
			{Type: ProtocolUniswapV3, Factory: "0x1F98431c8aD98523631AE4a59f267346ea31F984", Enabled: true},
			{Type: ProtocolVelodrome, Address: "0x9c12939390052919aF3155f41Bf4160Fd3666A6f", Enabled: true},
			{Type: ProtocolAaveV3, Address: "0x794a61358D6845594F94dc1DB02A252b5b4814aD", Enabled: true},
		},
	}

	indexer, err := NewEVMIndexer(config, nil)
	if err != nil {
		t.Fatalf("NewEVMIndexer failed: %v", err)
	}

	if indexer == nil {
		t.Fatal("indexer should not be nil")
	}
}

func TestEVMIndexerLifecycle(t *testing.T) {
	config := ChainConfig{
		ID:      "l2-test",
		ChainID: 10,
		Name:    "Test L2",
		Type:    ChainTypeEVM,
		RPC:     "http://localhost:8545",
		Enabled: true,
	}

	indexer, err := NewEVMIndexer(config, nil)
	if err != nil {
		t.Fatalf("NewEVMIndexer failed: %v", err)
	}

	// Initially not running
	if indexer.IsRunning() {
		t.Error("indexer should not be running initially")
	}

	// Start
	if err := indexer.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !indexer.IsRunning() {
		t.Error("indexer should be running after Start")
	}

	// Double start should fail
	if err := indexer.Start(context.Background()); err == nil {
		t.Error("double Start should return error")
	}

	// Stop
	if err := indexer.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if indexer.IsRunning() {
		t.Error("indexer should not be running after Stop")
	}
}

func TestIndexerStatsInitialization(t *testing.T) {
	config := ChainConfig{
		ID:      "stats-test",
		ChainID: 10,
		Name:    "Stats Test",
		Type:    ChainTypeEVM,
		RPC:     "http://localhost:8545",
		Enabled: true,
	}

	indexer, err := NewEVMIndexer(config, nil)
	if err != nil {
		t.Fatalf("NewEVMIndexer failed: %v", err)
	}

	stats := indexer.Stats()
	if stats == nil {
		t.Fatal("Stats should not be nil")
	}
	if stats.ChainID != "stats-test" {
		t.Errorf("ChainID = %q, want stats-test", stats.ChainID)
	}
	if stats.ChainName != "Stats Test" {
		t.Errorf("ChainName = %q, want Stats Test", stats.ChainName)
	}
	if stats.BlocksProcessed != 0 {
		t.Errorf("BlocksProcessed = %d, want 0", stats.BlocksProcessed)
	}
	if stats.StartTime.IsZero() {
		t.Error("StartTime should not be zero")
	}
}

// ---- DefaultConfig Tests ----

func TestDefaultConfigContainsL2s(t *testing.T) {
	config := DefaultConfig()

	expectedL2s := map[string]uint64{
		"arbitrum": 42161,
		"optimism": 10,
		"base":     8453,
	}

	found := make(map[string]bool)
	for _, chain := range config.Chains {
		if expectedID, ok := expectedL2s[chain.ID]; ok {
			found[chain.ID] = true
			if chain.ChainID != expectedID {
				t.Errorf("%s ChainID = %d, want %d", chain.ID, chain.ChainID, expectedID)
			}
			if chain.Type != ChainTypeEVM {
				t.Errorf("%s Type = %q, want evm", chain.ID, chain.Type)
			}
		}
	}

	for name := range expectedL2s {
		if !found[name] {
			t.Errorf("L2 chain %q missing from default config", name)
		}
	}
}

func TestDefaultConfigDefaults(t *testing.T) {
	config := DefaultConfig()

	if config.MaxConcurrentChains != 100 {
		t.Errorf("MaxConcurrentChains = %d, want 100", config.MaxConcurrentChains)
	}
	if config.DefaultBatchSize != 100 {
		t.Errorf("DefaultBatchSize = %d, want 100", config.DefaultBatchSize)
	}
	if config.DefaultPollInterval != 5*time.Second {
		t.Errorf("DefaultPollInterval = %v, want 5s", config.DefaultPollInterval)
	}
	if config.RetryAttempts != 3 {
		t.Errorf("RetryAttempts = %d, want 3", config.RetryAttempts)
	}
}

// ---- Chain Type Parsing Tests ----

func TestParseChainType_L2Variants(t *testing.T) {
	tests := []struct {
		input string
		want  ChainType
	}{
		{"evm", ChainTypeEVM},
		{"EVM", ChainTypeEVM},
		{"Evm", ChainTypeEVM},
		{"solana", ChainTypeSolana},
		{"bitcoin", ChainTypeBitcoin},
		{"cosmos", ChainTypeCosmos},
		{"move", ChainTypeMove},
		{"ton", ChainTypeTon},
		{"substrate", ChainTypeSubstrate},
		{"polkadot", ChainTypeSubstrate},
		// Unknown defaults to EVM (L2 chains are EVM)
		{"optimism_evm", ChainTypeEVM},
		{"arbitrum_nitro", ChainTypeEVM},
		{"unknown", ChainTypeEVM},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseChainType(tt.input)
			if got != tt.want {
				t.Errorf("parseChainType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---- Protocol Parsing Tests ----

func TestParseProtocolType_L2Protocols(t *testing.T) {
	tests := []struct {
		input string
		want  ProtocolType
	}{
		{"uniswap_v3", ProtocolUniswapV3},
		{"uniswapv3", ProtocolUniswapV3},
		{"aave_v3", ProtocolAaveV3},
		{"aavev3", ProtocolAaveV3},
		{"gmx", ProtocolGMX},
		{"gmx_v2", ProtocolGMXV2},
		{"velodrome", ProtocolVelodrome},
		{"aerodrome", ProtocolAerodrome},
		{"curve", ProtocolCurve},
		{"balancer", ProtocolBalancer},
		{"wormhole", ProtocolWormhole},
		{"layerzero", ProtocolLayerZero},
		{"stargate", ProtocolStargate},
		{"lido", ProtocolLido},
		{"eigenlayer", ProtocolEigenlayer},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseProtocolType(tt.input)
			if got != tt.want {
				t.Errorf("parseProtocolType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---- Benchmarks ----

func BenchmarkDetectL2Batch(b *testing.B) {
	input := "0xd4f9283a" + strings.Repeat("00", 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detectL2BatchFromCalldata(input)
	}
}

func BenchmarkBridgeEventPairMarshal(b *testing.B) {
	pair := BridgeEventPair{
		L1DepositTxHash: "0xl1deposit",
		L1BlockNumber:   18000000,
		L2MintTxHash:    "0xl2mint",
		L2BlockNumber:   100000,
		Token:           "0x0000000000000000000000000000000000000000",
		Amount:          "1000000000000000000",
		Sender:          "0xuser",
		Recipient:       "0xuser",
		Status:          "confirmed",
		CorrelationID:   "0x" + strings.Repeat("ab", 32),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(pair)
	}
}

func BenchmarkDisputeGameMarshal(b *testing.B) {
	game := DisputeGame{
		GameIndex:  100,
		GameType:   0,
		RootClaim:  "0x" + strings.Repeat("ab", 32),
		L2BlockNum: 10000000,
		Status:     "in_progress",
		Creator:    "0xproposer",
		CreatedAt:  time.Now(),
		L1TxHash:   "0xl1tx",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(game)
	}
}

// ---- Helper ----

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
