// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package evm

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"
)

// TestChainIDConstants tests chain ID constants
func TestChainIDConstants(t *testing.T) {
	tests := []struct {
		name string
		got  uint64
		want uint64
	}{
		{"LuxMainnet", ChainIDLuxMainnet, 96369},
		{"LuxTestnet", ChainIDLuxTestnet, 96368},
		{"ZooMainnet", ChainIDZooMainnet, 200200},
		{"ZooTestnet", ChainIDZooTestnet, 200201},
		{"HanzoMainnet", ChainIDHanzoMainnet, 36963},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestEntryPointAddresses tests ERC-4337 entry point addresses
func TestEntryPointAddresses(t *testing.T) {
	if len(EntryPointV06) != 42 {
		t.Errorf("EntryPointV06 length = %d, want 42", len(EntryPointV06))
	}
	if len(EntryPointV07) != 42 {
		t.Errorf("EntryPointV07 length = %d, want 42", len(EntryPointV07))
	}
	if EntryPointV06[:2] != "0x" {
		t.Error("EntryPointV06 should start with 0x")
	}
	if EntryPointV07[:2] != "0x" {
		t.Error("EntryPointV07 should start with 0x")
	}
}

// TestUserOperationSerialization tests UserOperation JSON serialization
func TestUserOperationSerialization(t *testing.T) {
	op := UserOperation{
		Hash:                 "0x123abc",
		Sender:               "0xsender",
		Nonce:                "0x1",
		InitCode:             "0x",
		CallData:             "0xdata",
		CallGasLimit:         100000,
		VerificationGasLimit: 50000,
		PreVerificationGas:   21000,
		MaxFeePerGas:         "1000000000",
		MaxPriorityFeePerGas: "1000000",
		PaymasterAndData:     "0xpaymaster",
		Signature:            "0xsig",
		EntryPoint:           EntryPointV06,
		BlockNumber:          1000,
		BlockHash:            "0xblock",
		TransactionHash:      "0xtx",
		BundlerAddress:       "0xbundler",
		PaymasterAddress:     "0xpaymaster",
		FactoryAddress:       "0xfactory",
		Status:               1,
		ActualGasCost:        "500000",
		ActualGasUsed:        50000,
		Timestamp:            time.Now(),
		EntryPointVersion:    "v0.6",
	}

	data, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded UserOperation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Hash != op.Hash {
		t.Errorf("Hash = %q, want %q", decoded.Hash, op.Hash)
	}
	if decoded.CallGasLimit != op.CallGasLimit {
		t.Errorf("CallGasLimit = %d, want %d", decoded.CallGasLimit, op.CallGasLimit)
	}
	if decoded.EntryPointVersion != op.EntryPointVersion {
		t.Errorf("EntryPointVersion = %q, want %q", decoded.EntryPointVersion, op.EntryPointVersion)
	}
}

// TestMEVTransactionSerialization tests MEVTransaction JSON serialization
func TestMEVTransactionSerialization(t *testing.T) {
	tx := MEVTransaction{
		ID:               "mev-123",
		TransactionHash:  "0xtx",
		BlockNumber:      1000,
		MEVType:          MEVTypeSandwich,
		ExtractorAddress: "0xattacker",
		VictimAddress:    "0xvictim",
		Protocol:         "Uniswap",
		ProfitETH:        "1000000000000000000",
		ProfitUSD:        "2000.00",
		GasCostETH:       "100000000000000",
		RelatedTxHashes:  []string{"0xtx1", "0xtx2", "0xtx3"},
		Timestamp:        time.Now(),
		Confidence:       0.95,
	}

	data, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded MEVTransaction
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.MEVType != MEVTypeSandwich {
		t.Errorf("MEVType = %q, want %q", decoded.MEVType, MEVTypeSandwich)
	}
	if len(decoded.RelatedTxHashes) != 3 {
		t.Errorf("RelatedTxHashes length = %d, want 3", len(decoded.RelatedTxHashes))
	}
	if decoded.Confidence != 0.95 {
		t.Errorf("Confidence = %f, want 0.95", decoded.Confidence)
	}
}

// TestMEVTypes tests MEV type constants
func TestMEVTypes(t *testing.T) {
	types := []MEVType{
		MEVTypeSandwich,
		MEVTypeArbitrage,
		MEVTypeLiquidation,
		MEVTypeJIT,
		MEVTypeBackrun,
		MEVTypeFrontrun,
		MEVTypePrivate,
	}

	for _, mevType := range types {
		if mevType == "" {
			t.Error("MEV type should not be empty")
		}
	}
}

// TestBlobTransactionSerialization tests BlobTransaction JSON serialization
func TestBlobTransactionSerialization(t *testing.T) {
	tx := BlobTransaction{
		Hash:                "0xblobtx",
		BlockNumber:         1000,
		BlockHash:           "0xblock",
		From:                "0xfrom",
		To:                  "0xto",
		Value:               "0",
		MaxFeePerBlobGas:    "1000000000",
		BlobGasUsed:         131072,
		BlobGasPrice:        "100",
		BlobVersionedHashes: []string{"0xhash1", "0xhash2"},
		BlobCount:           2,
		TransactionIndex:    5,
		Timestamp:           time.Now(),
	}

	data, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded BlobTransaction
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.BlobCount != 2 {
		t.Errorf("BlobCount = %d, want 2", decoded.BlobCount)
	}
	if decoded.BlobGasUsed != 131072 {
		t.Errorf("BlobGasUsed = %d, want 131072", decoded.BlobGasUsed)
	}
	if len(decoded.BlobVersionedHashes) != 2 {
		t.Errorf("BlobVersionedHashes length = %d, want 2", len(decoded.BlobVersionedHashes))
	}
}

// TestWithdrawalSerialization tests Withdrawal JSON serialization
func TestWithdrawalSerialization(t *testing.T) {
	w := Withdrawal{
		Index:          100,
		ValidatorIndex: 50,
		Address:        "0xwithdraw",
		Amount:         "1000000000",
		BlockNumber:    1000,
		BlockHash:      "0xblock",
		Timestamp:      time.Now(),
	}

	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Withdrawal
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Index != 100 {
		t.Errorf("Index = %d, want 100", decoded.Index)
	}
	if decoded.ValidatorIndex != 50 {
		t.Errorf("ValidatorIndex = %d, want 50", decoded.ValidatorIndex)
	}
}

// TestEnhancedBlockSerialization tests EnhancedBlock JSON serialization
func TestEnhancedBlockSerialization(t *testing.T) {
	block := EnhancedBlock{
		Hash:             "0xblock",
		ParentHash:       "0xparent",
		Number:           1000,
		Timestamp:        time.Now(),
		Miner:            "0xminer",
		Difficulty:       "1000000",
		TotalDifficulty:  "100000000000",
		Size:             5000,
		GasLimit:         8000000,
		GasUsed:          5000000,
		BaseFeePerGas:    "1000000000",
		ExtraData:        "0x",
		StateRoot:        "0xstate",
		TransactionsRoot: "0xtxroot",
		ReceiptsRoot:     "0xreceipts",
		LogsBloom:        "0x00",
		TxCount:          100,
		UncleCount:       2,
		UncleHashes:      []string{"0xuncle1", "0xuncle2"},
		BlobGasUsed:      262144,
		ExcessBlobGas:    131072,
		ParentBeaconRoot: "0xbeacon",
		Withdrawals: []Withdrawal{
			{Index: 1, ValidatorIndex: 10, Address: "0xaddr", Amount: "1000000000"},
		},
		WithdrawalsRoot: "0xwithdrawals",
	}

	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded EnhancedBlock
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Number != 1000 {
		t.Errorf("Number = %d, want 1000", decoded.Number)
	}
	if decoded.BlobGasUsed != 262144 {
		t.Errorf("BlobGasUsed = %d, want 262144", decoded.BlobGasUsed)
	}
	if len(decoded.Withdrawals) != 1 {
		t.Errorf("Withdrawals length = %d, want 1", len(decoded.Withdrawals))
	}
}

// TestChainConfigSerialization tests ChainConfig JSON serialization
func TestChainConfigSerialization(t *testing.T) {
	config := ChainConfig{
		ChainID:         96369,
		Name:            "Lux C-Chain",
		Symbol:          "LUX",
		RPCEndpoint:     "https://api.lux.network/ext/bc/C/rpc",
		WSEndpoint:      "wss://api.lux.network/ext/bc/C/ws",
		ExplorerURL:     "https://explorer.lux.network",
		IsTestnet:       false,
		BlockTime:       2000,
		SupportsEIP1559: true,
		SupportsEIP4844: true,
		SupportsEIP4337: true,
		EntryPoints:     []string{EntryPointV06, EntryPointV07},
	}

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ChainConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ChainID != 96369 {
		t.Errorf("ChainID = %d, want 96369", decoded.ChainID)
	}
	if !decoded.SupportsEIP4844 {
		t.Error("SupportsEIP4844 should be true")
	}
	if len(decoded.EntryPoints) != 2 {
		t.Errorf("EntryPoints length = %d, want 2", len(decoded.EntryPoints))
	}
}

// TestCrossChainTransactionSerialization tests CrossChainTransaction JSON serialization
func TestCrossChainTransactionSerialization(t *testing.T) {
	tx := CrossChainTransaction{
		ID:              "xchain-123",
		SourceChainID:   96369,
		DestChainID:     200200,
		SourceTxHash:    "0xsource",
		DestTxHash:      "0xdest",
		BridgeProtocol:  "Warp",
		TokenAddress:    "0xtoken",
		Amount:          "1000000000000000000",
		Sender:          "0xsender",
		Recipient:       "0xrecipient",
		Status:          "completed",
		SourceTimestamp: time.Now().Add(-time.Hour),
		DestTimestamp:   time.Now(),
	}

	data, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded CrossChainTransaction
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.SourceChainID != 96369 {
		t.Errorf("SourceChainID = %d, want 96369", decoded.SourceChainID)
	}
	if decoded.DestChainID != 200200 {
		t.Errorf("DestChainID = %d, want 200200", decoded.DestChainID)
	}
	if decoded.BridgeProtocol != "Warp" {
		t.Errorf("BridgeProtocol = %q, want Warp", decoded.BridgeProtocol)
	}
}

// TestParseBigInt tests ParseBigInt helper
func TestParseBigInt(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"0x0", "0"},
		{"0x1", "1"},
		{"0xa", "10"},
		{"0x64", "100"},
		{"0xde0b6b3a7640000", "1000000000000000000"},
		{"", "0"},
		{"0x", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseBigInt(tt.input)
			if got.String() != tt.want {
				t.Errorf("ParseBigInt(%q) = %s, want %s", tt.input, got.String(), tt.want)
			}
		})
	}
}

// TestFormatBigInt tests FormatBigInt helper
func TestFormatBigInt(t *testing.T) {
	tests := []struct {
		input *big.Int
		want  string
	}{
		{big.NewInt(0), "0x0"},
		{big.NewInt(1), "0x1"},
		{big.NewInt(10), "0xa"},
		{big.NewInt(100), "0x64"},
		{nil, "0x0"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := FormatBigInt(tt.input)
			if got != tt.want {
				t.Errorf("FormatBigInt() = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestTrimHexPrefix tests trimHexPrefix helper
func TestTrimHexPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"0x123", "123"},
		{"0X456", "456"},
		{"abc", "abc"},
		{"0x", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := trimHexPrefix(tt.input)
			if got != tt.want {
				t.Errorf("trimHexPrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// Benchmarks

func BenchmarkParseBigInt(b *testing.B) {
	inputs := []string{"0x0", "0x64", "0xde0b6b3a7640000"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, input := range inputs {
			ParseBigInt(input)
		}
	}
}

func BenchmarkUserOperationMarshal(b *testing.B) {
	op := UserOperation{
		Hash:         "0x123abc",
		Sender:       "0xsender",
		Nonce:        "0x1",
		CallData:     "0xdata",
		CallGasLimit: 100000,
		EntryPoint:   EntryPointV06,
		BlockNumber:  1000,
		Timestamp:    time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(op)
	}
}

func BenchmarkMEVTransactionMarshal(b *testing.B) {
	tx := MEVTransaction{
		ID:               "mev-123",
		TransactionHash:  "0xtx",
		BlockNumber:      1000,
		MEVType:          MEVTypeSandwich,
		ExtractorAddress: "0xattacker",
		VictimAddress:    "0xvictim",
		RelatedTxHashes:  []string{"0xtx1", "0xtx2", "0xtx3"},
		Timestamp:        time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(tx)
	}
}
