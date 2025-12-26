// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package evm

import (
	"encoding/json"
	"testing"
	"time"
)

// TestNewMEVIndexer tests MEVIndexer creation
func TestNewMEVIndexer(t *testing.T) {
	adapter := &Adapter{}
	indexer := NewMEVIndexer(adapter, nil)

	if indexer == nil {
		t.Fatal("NewMEVIndexer returned nil")
	}
	if indexer.adapter != adapter {
		t.Error("adapter not set correctly")
	}
}

// TestMEVTypeConstants tests MEV type constants
func TestMEVTypeConstants(t *testing.T) {
	types := map[MEVType]string{
		MEVTypeSandwich:    "sandwich",
		MEVTypeArbitrage:   "arbitrage",
		MEVTypeLiquidation: "liquidation",
		MEVTypeJIT:         "jit_liquidity",
		MEVTypeBackrun:     "backrun",
		MEVTypeFrontrun:    "frontrun",
		MEVTypePrivate:     "private",
	}

	for mevType, expected := range types {
		if string(mevType) != expected {
			t.Errorf("MEVType %s = %s, want %s", mevType, string(mevType), expected)
		}
	}
}

// TestDEXAddressConstants tests DEX address constants
func TestDEXAddressConstants(t *testing.T) {
	// Verify addresses are valid hex format
	addresses := []string{UniswapV2Router, UniswapV3Router, SushiswapRouter}

	for _, addr := range addresses {
		if len(addr) != 42 {
			t.Errorf("address %s length = %d, want 42", addr, len(addr))
		}
		if addr[:2] != "0x" {
			t.Errorf("address %s should start with 0x", addr)
		}
	}
}

// TestLendingProtocolAddressConstants tests lending protocol address constants
func TestLendingProtocolAddressConstants(t *testing.T) {
	addresses := []string{AaveV2LendingPool, AaveV3Pool, CompoundComptroller}

	for _, addr := range addresses {
		if len(addr) != 42 {
			t.Errorf("address %s length = %d, want 42", addr, len(addr))
		}
		if addr[:2] != "0x" {
			t.Errorf("address %s should start with 0x", addr)
		}
	}
}

// TestMEVEventSignatures tests MEV event signature constants
func TestMEVEventSignatures(t *testing.T) {
	signatures := map[string]string{
		"TopicSwap":        TopicSwap,
		"TopicSwapV3":      TopicSwapV3,
		"TopicSync":        TopicSync,
		"TopicLiquidation": TopicLiquidation,
	}

	for name, sig := range signatures {
		if len(sig) != 66 {
			t.Errorf("%s length = %d, want 66", name, len(sig))
		}
		if sig[:2] != "0x" {
			t.Errorf("%s should start with 0x", name)
		}
	}
}

// TestSandwichAttackSerialization tests SandwichAttack JSON serialization
func TestSandwichAttackSerialization(t *testing.T) {
	sandwich := SandwichAttack{
		ID:                "sandwich-1000-0xtx",
		BlockNumber:       1000,
		FrontrunTxHash:    "0xfront",
		VictimTxHash:      "0xvictim",
		BackrunTxHash:     "0xback",
		AttackerAddress:   "0xattacker",
		VictimAddress:     "0xvictim",
		TokenAddress:      "0xtoken",
		PoolAddress:       "0xpool",
		VictimLossETH:     "1000000000000000000",
		AttackerProfitETH: "100000000000000000",
		Timestamp:         time.Now(),
	}

	data, err := json.Marshal(sandwich)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded SandwichAttack
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.BlockNumber != 1000 {
		t.Errorf("BlockNumber = %d, want 1000", decoded.BlockNumber)
	}
	if decoded.FrontrunTxHash != "0xfront" {
		t.Errorf("FrontrunTxHash = %s, want 0xfront", decoded.FrontrunTxHash)
	}
	if decoded.VictimTxHash != "0xvictim" {
		t.Errorf("VictimTxHash = %s, want 0xvictim", decoded.VictimTxHash)
	}
	if decoded.BackrunTxHash != "0xback" {
		t.Errorf("BackrunTxHash = %s, want 0xback", decoded.BackrunTxHash)
	}
}

// TestArbitrageTxSerialization tests ArbitrageTx JSON serialization
func TestArbitrageTxSerialization(t *testing.T) {
	arb := ArbitrageTx{
		ID:                "arb-1000-0xtx",
		TransactionHash:   "0xtx",
		BlockNumber:       1000,
		ArbitragerAddress: "0xarbitrager",
		ProfitETH:         "500000000000000000",
		ProfitUSD:         "1000.00",
		PathLength:        3,
		Tokens:            []string{"0xtoken1", "0xtoken2", "0xtoken3"},
		Pools:             []string{"0xpool1", "0xpool2", "0xpool3"},
		IsAtomic:          true,
		Timestamp:         time.Now(),
	}

	data, err := json.Marshal(arb)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ArbitrageTx
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.PathLength != 3 {
		t.Errorf("PathLength = %d, want 3", decoded.PathLength)
	}
	if len(decoded.Tokens) != 3 {
		t.Errorf("Tokens length = %d, want 3", len(decoded.Tokens))
	}
	if len(decoded.Pools) != 3 {
		t.Errorf("Pools length = %d, want 3", len(decoded.Pools))
	}
	if !decoded.IsAtomic {
		t.Error("IsAtomic should be true")
	}
}

// TestLiquidationTxSerialization tests LiquidationTx JSON serialization
func TestLiquidationTxSerialization(t *testing.T) {
	liq := LiquidationTx{
		ID:                  "liq-1000-0xtx",
		TransactionHash:     "0xtx",
		BlockNumber:         1000,
		LiquidatorAddress:   "0xliquidator",
		BorrowerAddress:     "0xborrower",
		Protocol:            "Aave",
		CollateralToken:     "0xweth",
		DebtToken:           "0xusdc",
		CollateralSeized:    "1000000000000000000",
		DebtRepaid:          "2000000000",
		LiquidatorProfitETH: "100000000000000000",
		Timestamp:           time.Now(),
	}

	data, err := json.Marshal(liq)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded LiquidationTx
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Protocol != "Aave" {
		t.Errorf("Protocol = %s, want Aave", decoded.Protocol)
	}
	if decoded.CollateralToken != "0xweth" {
		t.Errorf("CollateralToken = %s, want 0xweth", decoded.CollateralToken)
	}
	if decoded.DebtToken != "0xusdc" {
		t.Errorf("DebtToken = %s, want 0xusdc", decoded.DebtToken)
	}
}

// TestMEVTransactionSerialization tests MEVTransaction JSON serialization
func TestMEVTransactionSerializationComplete(t *testing.T) {
	mev := MEVTransaction{
		ID:               "mev-1000-0xtx",
		TransactionHash:  "0xtx",
		BlockNumber:      1000,
		MEVType:          MEVTypeSandwich,
		ExtractorAddress: "0xextractor",
		VictimAddress:    "0xvictim",
		Protocol:         "UniswapV2",
		ProfitETH:        "1000000000000000000",
		ProfitUSD:        "2000.00",
		GasCostETH:       "100000000000000",
		RelatedTxHashes:  []string{"0xtx1", "0xtx2", "0xtx3"},
		Timestamp:        time.Now(),
		Confidence:       0.95,
	}

	data, err := json.Marshal(mev)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded MEVTransaction
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.MEVType != MEVTypeSandwich {
		t.Errorf("MEVType = %s, want %s", decoded.MEVType, MEVTypeSandwich)
	}
	if decoded.Confidence != 0.95 {
		t.Errorf("Confidence = %f, want 0.95", decoded.Confidence)
	}
	if len(decoded.RelatedTxHashes) != 3 {
		t.Errorf("RelatedTxHashes length = %d, want 3", len(decoded.RelatedTxHashes))
	}
}

// TestFindSwapEvent tests swap event detection
func TestFindSwapEvent(t *testing.T) {
	tests := []struct {
		name     string
		logs     []Log
		wantSwap bool
	}{
		{
			name: "V2 swap event",
			logs: []Log{
				{Topics: []string{TopicSwap}, Address: "0xpool"},
			},
			wantSwap: true,
		},
		{
			name: "V3 swap event",
			logs: []Log{
				{Topics: []string{TopicSwapV3}, Address: "0xpool"},
			},
			wantSwap: true,
		},
		{
			name: "no swap event",
			logs: []Log{
				{Topics: []string{TopicSync}, Address: "0xpool"},
			},
			wantSwap: false,
		},
		{
			name:     "empty logs",
			logs:     []Log{},
			wantSwap: false,
		},
		{
			name: "log with no topics",
			logs: []Log{
				{Topics: []string{}, Address: "0xpool"},
			},
			wantSwap: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findSwapEvent(tt.logs)
			if (result != nil) != tt.wantSwap {
				t.Errorf("findSwapEvent returned %v, wantSwap = %v", result != nil, tt.wantSwap)
			}
		})
	}
}

// TestDetectFlashLoan tests flash loan detection
func TestDetectFlashLoan(t *testing.T) {
	tests := []struct {
		name      string
		logs      []Log
		wantFlash bool
	}{
		{
			name: "Aave flash loan",
			logs: []Log{
				{Topics: []string{"0x631042c832b07452973831137f2d73e395028b44b250dedc5abb0ee766e168ac"}},
			},
			wantFlash: true,
		},
		{
			name: "no flash loan",
			logs: []Log{
				{Topics: []string{TopicSwap}},
			},
			wantFlash: false,
		},
		{
			name:      "empty logs",
			logs:      []Log{},
			wantFlash: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectFlashLoan(tt.logs)
			if result != tt.wantFlash {
				t.Errorf("detectFlashLoan returned %v, want %v", result, tt.wantFlash)
			}
		})
	}
}

// TestDetectLendingProtocol tests lending protocol detection
func TestDetectLendingProtocol(t *testing.T) {
	tests := []struct {
		address  string
		expected string
	}{
		{AaveV2LendingPool, "Aave"},
		{AaveV3Pool, "Aave"},
		{CompoundComptroller, "Compound"},
		{"0x1234567890123456789012345678901234567890", "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.address[:10], func(t *testing.T) {
			result := detectLendingProtocol(tt.address)
			if result != tt.expected {
				t.Errorf("detectLendingProtocol(%s) = %s, want %s", tt.address, result, tt.expected)
			}
		})
	}
}

// TestDetectProtocol tests protocol detection by pool address
func TestDetectProtocol(t *testing.T) {
	result := detectProtocol("0xpool")
	if result == "" {
		t.Error("detectProtocol should not return empty string")
	}
}

// TestCalculateGasCost tests gas cost calculation
func TestCalculateGasCost(t *testing.T) {
	tx1 := &Transaction{
		GasUsed:  21000,
		GasPrice: "0x3b9aca00", // 1 gwei
	}
	tx3 := &Transaction{
		GasUsed:  21000,
		GasPrice: "0x3b9aca00", // 1 gwei
	}

	cost := calculateGasCost(tx1, tx3)
	// 42000 gas * 1 gwei = 42000000000000 wei
	expected := "42000000000000"
	if cost != expected {
		t.Errorf("calculateGasCost = %s, want %s", cost, expected)
	}
}

// TestCalculateGasCostSingle tests single tx gas cost calculation
func TestCalculateGasCostSingle(t *testing.T) {
	tx := &Transaction{
		GasUsed:  21000,
		GasPrice: "0x3b9aca00", // 1 gwei
	}

	cost := calculateGasCostSingle(tx)
	// 21000 gas * 1 gwei = 21000000000000 wei
	expected := "21000000000000"
	if cost != expected {
		t.Errorf("calculateGasCostSingle = %s, want %s", cost, expected)
	}
}

// Benchmarks

func BenchmarkFindSwapEvent(b *testing.B) {
	logs := []Log{
		{Topics: []string{TopicSync}, Address: "0xpool1"},
		{Topics: []string{TopicSwap}, Address: "0xpool2"},
		{Topics: []string{TopicLiquidation}, Address: "0xpool3"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		findSwapEvent(logs)
	}
}

func BenchmarkDetectFlashLoan(b *testing.B) {
	logs := []Log{
		{Topics: []string{TopicSwap}},
		{Topics: []string{TopicSwapV3}},
		{Topics: []string{TopicSync}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detectFlashLoan(logs)
	}
}

func BenchmarkMEVTransactionMarshalComplete(b *testing.B) {
	mev := MEVTransaction{
		ID:               "mev-1000-0xtx",
		TransactionHash:  "0xtx",
		BlockNumber:      1000,
		MEVType:          MEVTypeSandwich,
		ExtractorAddress: "0xextractor",
		VictimAddress:    "0xvictim",
		Protocol:         "UniswapV2",
		ProfitETH:        "1000000000000000000",
		ProfitUSD:        "2000.00",
		GasCostETH:       "100000000000000",
		RelatedTxHashes:  []string{"0xtx1", "0xtx2", "0xtx3"},
		Timestamp:        time.Now(),
		Confidence:       0.95,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(mev)
	}
}

func BenchmarkSandwichAttackMarshal(b *testing.B) {
	sandwich := SandwichAttack{
		ID:                "sandwich-1000-0xtx",
		BlockNumber:       1000,
		FrontrunTxHash:    "0xfront",
		VictimTxHash:      "0xvictim",
		BackrunTxHash:     "0xback",
		AttackerAddress:   "0xattacker",
		VictimAddress:     "0xvictim",
		TokenAddress:      "0xtoken",
		PoolAddress:       "0xpool",
		VictimLossETH:     "1000000000000000000",
		AttackerProfitETH: "100000000000000000",
		Timestamp:         time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(sandwich)
	}
}
