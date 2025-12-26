// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package evm

import (
	"encoding/json"
	"testing"
	"time"
)

// TestNewBlobIndexer tests BlobIndexer creation
func TestNewBlobIndexer(t *testing.T) {
	adapter := &Adapter{}
	storageURI := "https://blob-storage.lux.network"
	indexer := NewBlobIndexer(adapter, nil, storageURI)

	if indexer == nil {
		t.Fatal("NewBlobIndexer returned nil")
	}
	if indexer.adapter != adapter {
		t.Error("adapter not set correctly")
	}
	if indexer.storageURI != storageURI {
		t.Errorf("storageURI = %s, want %s", indexer.storageURI, storageURI)
	}
}

// TestBlobConstants tests EIP-4844 constants
func TestBlobConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      uint64
		want     uint64
	}{
		{"BlobTxType", BlobTxType, 3},
		{"BlobGasPerBlob", BlobGasPerBlob, 131072},
		{"MaxBlobsPerBlock", MaxBlobsPerBlock, 6},
		{"BlobFieldElementSize", BlobFieldElementSize, 32},
		{"BlobSize", BlobSize, 131072},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != uint64(tt.want) {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}

	// Verify BlobSize calculation
	expectedSize := 4096 * BlobFieldElementSize
	if BlobSize != expectedSize {
		t.Errorf("BlobSize = %d, want %d (4096 * 32)", BlobSize, expectedSize)
	}
}

// TestBlobTransactionSerialization tests BlobTransaction JSON serialization
func TestBlobTransactionSerializationComplete(t *testing.T) {
	tx := BlobTransaction{
		Hash:                "0xblobtx123",
		BlockNumber:         1000,
		BlockHash:           "0xblock123",
		From:                "0xfrom",
		To:                  "0xto",
		Value:               "1000000000000000000",
		MaxFeePerBlobGas:    "1000000000",
		BlobGasUsed:         262144, // 2 blobs
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
	if decoded.BlobGasUsed != 262144 {
		t.Errorf("BlobGasUsed = %d, want 262144", decoded.BlobGasUsed)
	}
	if len(decoded.BlobVersionedHashes) != 2 {
		t.Errorf("BlobVersionedHashes length = %d, want 2", len(decoded.BlobVersionedHashes))
	}
	if decoded.TransactionIndex != 5 {
		t.Errorf("TransactionIndex = %d, want 5", decoded.TransactionIndex)
	}
}

// TestBlobDataSerialization tests BlobData JSON serialization
func TestBlobDataSerialization(t *testing.T) {
	blob := BlobData{
		VersionedHash:   "0x01hash123",
		Commitment:      "0xcommitment",
		Proof:           "0xproof",
		Data:            "0xblobdata...",
		Size:            BlobSize,
		TransactionHash: "0xtx123",
		BlockNumber:     1000,
		BlobIndex:       0,
		StorageURI:      "https://blob-storage.lux.network/0x01hash123",
		Timestamp:       time.Now(),
	}

	data, err := json.Marshal(blob)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded BlobData
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Size != BlobSize {
		t.Errorf("Size = %d, want %d", decoded.Size, BlobSize)
	}
	if decoded.BlobIndex != 0 {
		t.Errorf("BlobIndex = %d, want 0", decoded.BlobIndex)
	}
	if decoded.StorageURI == "" {
		t.Error("StorageURI should not be empty")
	}
}

// TestBlobGasCalculation tests blob gas calculation
func TestBlobGasCalculation(t *testing.T) {
	tests := []struct {
		blobCount int
		expected  uint64
	}{
		{1, BlobGasPerBlob},
		{2, 2 * BlobGasPerBlob},
		{3, 3 * BlobGasPerBlob},
		{6, 6 * BlobGasPerBlob}, // Max blobs per block
	}

	for _, tt := range tests {
		gasUsed := uint64(tt.blobCount) * BlobGasPerBlob
		if gasUsed != tt.expected {
			t.Errorf("gas for %d blobs = %d, want %d", tt.blobCount, gasUsed, tt.expected)
		}
	}
}

// TestVersionedHashFormat tests versioned hash format
func TestVersionedHashFormat(t *testing.T) {
	// EIP-4844 versioned hashes start with 0x01
	tests := []struct {
		hash  string
		valid bool
	}{
		{"0x01abcdef1234567890abcdef1234567890abcdef1234567890abcdef12345678", true},
		{"0x00abcdef1234567890abcdef1234567890abcdef1234567890abcdef12345678", false}, // Wrong version
		{"0x01", false}, // Too short
	}

	for _, tt := range tests {
		isValid := len(tt.hash) == 66 && tt.hash[:4] == "0x01"
		if isValid != tt.valid {
			t.Errorf("hash %s validity = %v, want %v", tt.hash[:10], isValid, tt.valid)
		}
	}
}

// TestCalculateBlobGasPrice tests blob gas price calculation
func TestCalculateBlobGasPrice(t *testing.T) {
	indexer := &BlobIndexer{}

	tests := []struct {
		excessBlobGas uint64
		description   string
	}{
		{0, "zero excess"},
		{131072, "one blob worth"},
		{262144, "two blobs worth"},
		{1000000, "high excess"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			price := indexer.calculateBlobGasPrice(tt.excessBlobGas)
			if price == "" {
				t.Error("price should not be empty")
			}
			// Price should be at least 1 (min blob base fee)
			if price == "0" {
				t.Error("price should be at least 1")
			}
		})
	}
}

// TestCalculateBlobGasPriceZeroExcess tests price at zero excess
func TestCalculateBlobGasPriceZeroExcess(t *testing.T) {
	indexer := &BlobIndexer{}
	price := indexer.calculateBlobGasPrice(0)

	// At zero excess, price should be minimum (1 wei)
	if price != "1" {
		t.Errorf("price at zero excess = %s, want 1", price)
	}
}

// TestBlobGasUsedPerBlob tests gas used per blob calculation
func TestBlobGasUsedPerBlob(t *testing.T) {
	// Each blob uses exactly 2^17 = 131072 gas
	if BlobGasPerBlob != 131072 {
		t.Errorf("BlobGasPerBlob = %d, want 131072", BlobGasPerBlob)
	}

	// 2^17 calculation
	expected := uint64(1 << 17)
	if BlobGasPerBlob != expected {
		t.Errorf("BlobGasPerBlob = %d, want 2^17 = %d", BlobGasPerBlob, expected)
	}
}

// TestBlobSizeCalculation tests blob size
func TestBlobSizeCalculation(t *testing.T) {
	// Blob size = 4096 field elements * 32 bytes
	expected := 4096 * 32
	if BlobSize != expected {
		t.Errorf("BlobSize = %d, want %d", BlobSize, expected)
	}

	// Should be 128KB
	if BlobSize != 131072 {
		t.Errorf("BlobSize = %d, want 131072 (128KB)", BlobSize)
	}
}

// TestMaxBlobsPerBlockTarget tests max blobs per block
func TestMaxBlobsPerBlockTarget(t *testing.T) {
	// EIP-4844 targets 3 blobs per block, max 6
	if MaxBlobsPerBlock != 6 {
		t.Errorf("MaxBlobsPerBlock = %d, want 6", MaxBlobsPerBlock)
	}

	// Max blob gas per block = 6 * 131072 = 786432
	maxBlobGas := MaxBlobsPerBlock * BlobGasPerBlob
	expectedMax := uint64(786432)
	if uint64(maxBlobGas) != expectedMax {
		t.Errorf("max blob gas = %d, want %d", maxBlobGas, expectedMax)
	}
}

// TestBlobTransactionFields tests all BlobTransaction fields
func TestBlobTransactionFields(t *testing.T) {
	now := time.Now()
	tx := BlobTransaction{
		Hash:                "0xhash",
		BlockNumber:         100,
		BlockHash:           "0xblockhash",
		From:                "0xfrom",
		To:                  "0xto",
		Value:               "0",
		MaxFeePerBlobGas:    "1000000000",
		BlobGasUsed:         131072,
		BlobGasPrice:        "1",
		BlobVersionedHashes: []string{"0x01hash"},
		BlobCount:           1,
		TransactionIndex:    0,
		Timestamp:           now,
	}

	// Verify all fields are set
	if tx.Hash == "" {
		t.Error("Hash should not be empty")
	}
	if tx.BlockNumber == 0 {
		t.Error("BlockNumber should not be 0")
	}
	if tx.From == "" {
		t.Error("From should not be empty")
	}
	if tx.MaxFeePerBlobGas == "" {
		t.Error("MaxFeePerBlobGas should not be empty")
	}
	if tx.BlobGasUsed == 0 {
		t.Error("BlobGasUsed should not be 0")
	}
	if tx.BlobGasPrice == "" {
		t.Error("BlobGasPrice should not be empty")
	}
	if len(tx.BlobVersionedHashes) == 0 {
		t.Error("BlobVersionedHashes should not be empty")
	}
	if tx.BlobCount == 0 {
		t.Error("BlobCount should not be 0")
	}
	if tx.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

// TestBlobDataFields tests all BlobData fields
func TestBlobDataFields(t *testing.T) {
	now := time.Now()
	blob := BlobData{
		VersionedHash:   "0x01hash",
		Commitment:      "0xcommitment",
		Proof:           "0xproof",
		Data:            "0xdata",
		Size:            BlobSize,
		TransactionHash: "0xtx",
		BlockNumber:     100,
		BlobIndex:       0,
		StorageURI:      "https://storage/hash",
		Timestamp:       now,
	}

	if blob.VersionedHash == "" {
		t.Error("VersionedHash should not be empty")
	}
	if blob.Size != BlobSize {
		t.Errorf("Size = %d, want %d", blob.Size, BlobSize)
	}
	if blob.TransactionHash == "" {
		t.Error("TransactionHash should not be empty")
	}
}

// Benchmarks

func BenchmarkBlobTransactionMarshal(b *testing.B) {
	tx := BlobTransaction{
		Hash:                "0xblobtx123",
		BlockNumber:         1000,
		BlockHash:           "0xblock123",
		From:                "0xfrom",
		To:                  "0xto",
		Value:               "1000000000000000000",
		MaxFeePerBlobGas:    "1000000000",
		BlobGasUsed:         262144,
		BlobGasPrice:        "100",
		BlobVersionedHashes: []string{"0xhash1", "0xhash2"},
		BlobCount:           2,
		TransactionIndex:    5,
		Timestamp:           time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(tx)
	}
}

func BenchmarkBlobDataMarshal(b *testing.B) {
	blob := BlobData{
		VersionedHash:   "0x01hash123",
		Commitment:      "0xcommitment",
		Proof:           "0xproof",
		Data:            "0xblobdata",
		Size:            BlobSize,
		TransactionHash: "0xtx123",
		BlockNumber:     1000,
		BlobIndex:       0,
		StorageURI:      "https://blob-storage.lux.network/0x01hash123",
		Timestamp:       time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(blob)
	}
}

func BenchmarkCalculateBlobGasPrice(b *testing.B) {
	indexer := &BlobIndexer{}
	excessValues := []uint64{0, 131072, 262144, 1000000}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, excess := range excessValues {
			indexer.calculateBlobGasPrice(excess)
		}
	}
}

func BenchmarkBlobGasCalculation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for blobCount := 1; blobCount <= MaxBlobsPerBlock; blobCount++ {
			_ = uint64(blobCount) * BlobGasPerBlob
		}
	}
}
