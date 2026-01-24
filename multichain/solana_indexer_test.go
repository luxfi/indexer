// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package multichain

import (
	"context"
	"testing"
)

func TestBase58Encode(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "empty",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "single zero",
			input:    []byte{0},
			expected: "1",
		},
		{
			name:     "leading zeros",
			input:    []byte{0, 0, 0, 1},
			expected: "1112",
		},
		{
			name:     "hello world",
			input:    []byte("Hello World"),
			expected: "JxF12TrwUP45BMd",
		},
		{
			name:     "solana pubkey bytes",
			input:    []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
			expected: "11111111111111111111111111111112",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := base58Encode(tt.input)
			if result != tt.expected {
				t.Errorf("base58Encode(%v) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBase58Decode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []byte
		wantErr  bool
	}{
		{
			name:     "empty",
			input:    "",
			expected: []byte{},
		},
		{
			name:     "single 1",
			input:    "1",
			expected: []byte{0},
		},
		{
			name:     "leading 1s",
			input:    "1112",
			expected: []byte{0, 0, 0, 1},
		},
		{
			name:     "hello world",
			input:    "JxF12TrwUP45BMd",
			expected: []byte("Hello World"),
		},
		{
			name:    "invalid char 0",
			input:   "0invalid",
			wantErr: true,
		},
		{
			name:    "invalid char O",
			input:   "O1234",
			wantErr: true,
		},
		{
			name:    "invalid char I",
			input:   "I1234",
			wantErr: true,
		},
		{
			name:    "invalid char l",
			input:   "l1234",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := base58Decode(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("base58Decode(%s) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("base58Decode(%s) error = %v", tt.input, err)
				return
			}
			if string(result) != string(tt.expected) {
				t.Errorf("base58Decode(%s) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBase58RoundTrip(t *testing.T) {
	tests := [][]byte{
		{},
		{0},
		{0, 0, 0},
		{1, 2, 3, 4, 5},
		{255, 255, 255},
		make([]byte, 32), // 32-byte pubkey with zeros
	}

	// Fill the last test with non-zero bytes
	for i := range tests[len(tests)-1] {
		tests[len(tests)-1][i] = byte(i)
	}

	for i, input := range tests {
		encoded := base58Encode(input)
		decoded, err := base58Decode(encoded)
		if err != nil {
			t.Errorf("test %d: roundtrip decode error: %v", i, err)
			continue
		}
		if string(decoded) != string(input) {
			t.Errorf("test %d: roundtrip failed: input=%v, encoded=%s, decoded=%v", i, input, encoded, decoded)
		}
	}
}

func TestBorshReader_ReadU8(t *testing.T) {
	reader := NewBorshReaderFromBytes([]byte{0x12, 0x34})

	v, err := reader.ReadU8()
	if err != nil {
		t.Fatalf("ReadU8 error: %v", err)
	}
	if v != 0x12 {
		t.Errorf("ReadU8 = %d, want %d", v, 0x12)
	}

	v, err = reader.ReadU8()
	if err != nil {
		t.Fatalf("ReadU8 error: %v", err)
	}
	if v != 0x34 {
		t.Errorf("ReadU8 = %d, want %d", v, 0x34)
	}

	// Should fail on empty
	_, err = reader.ReadU8()
	if err == nil {
		t.Error("expected buffer underflow error")
	}
}

func TestBorshReader_ReadU16(t *testing.T) {
	// Little-endian: 0x1234 = [0x34, 0x12]
	reader := NewBorshReaderFromBytes([]byte{0x34, 0x12})

	v, err := reader.ReadU16()
	if err != nil {
		t.Fatalf("ReadU16 error: %v", err)
	}
	if v != 0x1234 {
		t.Errorf("ReadU16 = %d, want %d", v, 0x1234)
	}
}

func TestBorshReader_ReadU32(t *testing.T) {
	// Little-endian: 0x12345678 = [0x78, 0x56, 0x34, 0x12]
	reader := NewBorshReaderFromBytes([]byte{0x78, 0x56, 0x34, 0x12})

	v, err := reader.ReadU32()
	if err != nil {
		t.Fatalf("ReadU32 error: %v", err)
	}
	if v != 0x12345678 {
		t.Errorf("ReadU32 = %d, want %d", v, 0x12345678)
	}
}

func TestBorshReader_ReadU64(t *testing.T) {
	// Little-endian: 1000000000 (1 SOL in lamports)
	reader := NewBorshReaderFromBytes([]byte{0x00, 0xca, 0x9a, 0x3b, 0x00, 0x00, 0x00, 0x00})

	v, err := reader.ReadU64()
	if err != nil {
		t.Fatalf("ReadU64 error: %v", err)
	}
	if v != 1000000000 {
		t.Errorf("ReadU64 = %d, want %d", v, 1000000000)
	}
}

func TestBorshReader_ReadString(t *testing.T) {
	// Borsh string: 4-byte length prefix + UTF-8 data
	// "Hello" = [5, 0, 0, 0, 'H', 'e', 'l', 'l', 'o']
	data := []byte{5, 0, 0, 0, 'H', 'e', 'l', 'l', 'o'}
	reader := NewBorshReaderFromBytes(data)

	s, err := reader.ReadString()
	if err != nil {
		t.Fatalf("ReadString error: %v", err)
	}
	if s != "Hello" {
		t.Errorf("ReadString = %q, want %q", s, "Hello")
	}
}

func TestBorshReader_ReadPubkey(t *testing.T) {
	// 32 bytes of sequential values
	data := make([]byte, 32)
	for i := range data {
		data[i] = byte(i)
	}
	reader := NewBorshReaderFromBytes(data)

	pubkey, err := reader.ReadPubkey()
	if err != nil {
		t.Fatalf("ReadPubkey error: %v", err)
	}

	// Verify we can decode it back
	decoded, err := base58Decode(pubkey)
	if err != nil {
		t.Fatalf("decode pubkey error: %v", err)
	}
	if len(decoded) != 32 {
		t.Errorf("decoded pubkey length = %d, want 32", len(decoded))
	}
	for i, b := range decoded {
		if b != byte(i) {
			t.Errorf("decoded[%d] = %d, want %d", i, b, i)
		}
	}
}

func TestBorshReader_ReadBool(t *testing.T) {
	reader := NewBorshReaderFromBytes([]byte{0, 1, 255})

	v, err := reader.ReadBool()
	if err != nil {
		t.Fatalf("ReadBool error: %v", err)
	}
	if v != false {
		t.Errorf("ReadBool = %v, want false", v)
	}

	v, err = reader.ReadBool()
	if err != nil {
		t.Fatalf("ReadBool error: %v", err)
	}
	if v != true {
		t.Errorf("ReadBool = %v, want true", v)
	}

	v, err = reader.ReadBool()
	if err != nil {
		t.Fatalf("ReadBool error: %v", err)
	}
	if v != true {
		t.Errorf("ReadBool = %v, want true (non-zero)", v)
	}
}

func TestBorshReader_ReadVec(t *testing.T) {
	// Vec of 3 u8s: [3, 0, 0, 0, 10, 20, 30]
	data := []byte{3, 0, 0, 0, 10, 20, 30}
	reader := NewBorshReaderFromBytes(data)

	length, err := reader.ReadVecLen()
	if err != nil {
		t.Fatalf("ReadVecLen error: %v", err)
	}
	if length != 3 {
		t.Errorf("ReadVecLen = %d, want 3", length)
	}

	for i := uint32(0); i < length; i++ {
		v, err := reader.ReadU8()
		if err != nil {
			t.Fatalf("ReadU8 error at %d: %v", i, err)
		}
		expected := byte((i + 1) * 10)
		if v != expected {
			t.Errorf("element %d = %d, want %d", i, v, expected)
		}
	}
}

func TestNewBorshReader_Base58(t *testing.T) {
	// Encode some test data
	testData := []byte{1, 2, 3, 4, 5}
	encoded := base58Encode(testData)

	reader, err := NewBorshReader(encoded)
	if err != nil {
		t.Fatalf("NewBorshReader error: %v", err)
	}

	for i, expected := range testData {
		v, err := reader.ReadU8()
		if err != nil {
			t.Fatalf("ReadU8 error at %d: %v", i, err)
		}
		if v != expected {
			t.Errorf("byte %d = %d, want %d", i, v, expected)
		}
	}
}

func TestTrimNullBytes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello", "Hello"},
		{"Hello\x00\x00\x00", "Hello"},
		{"\x00\x00\x00", ""},
		{"", ""},
		{"No nulls", "No nulls"},
		{"Middle\x00null", "Middle\x00null"}, // Only trims trailing
	}

	for _, tt := range tests {
		result := trimNullBytes(tt.input)
		if result != tt.expected {
			t.Errorf("trimNullBytes(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestSolanaNFTSale_Struct(t *testing.T) {
	sale := &SolanaNFTSale{
		Signature:      "5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW",
		Slot:           123456789,
		BlockTime:      1640000000,
		Marketplace:    "magic_eden",
		Mint:           "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		Buyer:          "11111111111111111111111111111111",
		Seller:         "22222222222222222222222222222222",
		Price:          1000000000, // 1 SOL
		RoyaltyAmount:  50000000,   // 0.05 SOL
		MarketplaceFee: 25000000,   // 0.025 SOL
	}

	if sale.Marketplace != "magic_eden" {
		t.Errorf("Marketplace = %s, want magic_eden", sale.Marketplace)
	}
	if sale.Price != 1000000000 {
		t.Errorf("Price = %d, want 1000000000", sale.Price)
	}
}

func TestMetaplexMetadata_Struct(t *testing.T) {
	metadata := &MetaplexMetadata{
		Mint:                 "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		UpdateAuthority:      "11111111111111111111111111111111",
		Name:                 "Test NFT",
		Symbol:               "TEST",
		URI:                  "https://arweave.net/abc123",
		SellerFeeBasisPoints: 500, // 5%
		Creators: []MetaplexCreator{
			{
				Address:  "22222222222222222222222222222222",
				Verified: true,
				Share:    100,
			},
		},
		PrimarySaleHappened: false,
		IsMutable:           true,
	}

	if metadata.SellerFeeBasisPoints != 500 {
		t.Errorf("SellerFeeBasisPoints = %d, want 500", metadata.SellerFeeBasisPoints)
	}
	if len(metadata.Creators) != 1 {
		t.Errorf("len(Creators) = %d, want 1", len(metadata.Creators))
	}
	if metadata.Creators[0].Share != 100 {
		t.Errorf("Creator share = %d, want 100", metadata.Creators[0].Share)
	}
}

func TestMetaplexIndexer_ProgramIDs(t *testing.T) {
	indexer := NewMetaplexIndexer(ProtocolConfig{}, nil)

	programIDs := indexer.ProgramIDs()
	expected := []string{
		"metaqbxxUerdq28cj1RbAWkYQm3ybzjb6a8bt518x1s",
		"p1exdMJcjVao65QdewkaZRUnU6VPSXhus9n2GzWfh98",
		"CoREENxT6tW1HoK8ypY1SxRMZTcVPm7R94rH4PZNhX7d",
		"BGUMAp9Gq7iTEuizy4pqaxsTyUCBK68MDfK752saRPUY",
	}

	if len(programIDs) != len(expected) {
		t.Errorf("len(programIDs) = %d, want %d", len(programIDs), len(expected))
	}

	for i, id := range programIDs {
		if id != expected[i] {
			t.Errorf("programID[%d] = %s, want %s", i, id, expected[i])
		}
	}
}

func TestMagicEdenIndexer_ProgramIDs(t *testing.T) {
	indexer := NewMagicEdenSolanaIndexer(ProtocolConfig{}, nil)

	programIDs := indexer.ProgramIDs()
	expected := []string{
		"M2mx93ekt1fmXSVkTrUL9xVFHkmME8HTUi5Cyc5aF7K",
		"MEisE1HzehtrDpAAT8PnLHjpSSkRYakotTuJRPjTpo8",
	}

	if len(programIDs) != len(expected) {
		t.Errorf("len(programIDs) = %d, want %d", len(programIDs), len(expected))
	}

	for i, id := range programIDs {
		if id != expected[i] {
			t.Errorf("programID[%d] = %s, want %s", i, id, expected[i])
		}
	}
}

func TestTensorIndexer_ProgramIDs(t *testing.T) {
	indexer := NewTensorIndexer(ProtocolConfig{}, nil)

	programIDs := indexer.ProgramIDs()
	expected := []string{
		"TSWAPaqyCSx2KABk68Shruf4rp7CxcNi8hAsbdwmHbN",
		"TCMPhJdwDryooaGtiocG1u3xcYbRpiJzb283XfCZsDp",
		"TBIDxNsM9DuLs4YCbmA7VuACbMZ5WyYv5JGQxpqLMVJ",
	}

	if len(programIDs) != len(expected) {
		t.Errorf("len(programIDs) = %d, want %d", len(programIDs), len(expected))
	}

	for i, id := range programIDs {
		if id != expected[i] {
			t.Errorf("programID[%d] = %s, want %s", i, id, expected[i])
		}
	}
}

func TestSolanaIndexer_ProcessTransaction_SkipsFailedTx(t *testing.T) {
	// Create a minimal indexer without protocols
	config := ChainConfig{
		ID:   "solana-test",
		Name: "Solana Test",
		RPC:  "http://localhost:8899",
	}
	indexer, err := NewSolanaIndexer(config, nil)
	if err != nil {
		t.Fatalf("NewSolanaIndexer error: %v", err)
	}

	// Create a failed transaction
	tx := &SolanaTransaction{
		Signature: "test-sig",
		Status:    "failed",
		Err:       "some error",
	}

	// Process should not panic and should skip failed tx
	indexer.processTransaction(context.Background(), tx)

	// Stats should not have changed
	if indexer.stats.EventsProcessed != 0 {
		t.Errorf("EventsProcessed = %d, want 0", indexer.stats.EventsProcessed)
	}
}

func TestSolanaIndexer_Name(t *testing.T) {
	tests := []struct {
		indexer  SolanaProtocolIndexer
		expected string
	}{
		{NewMetaplexIndexer(ProtocolConfig{}, nil), "metaplex"},
		{NewMagicEdenSolanaIndexer(ProtocolConfig{}, nil), "magic_eden_solana"},
		{NewTensorIndexer(ProtocolConfig{}, nil), "tensor"},
		{NewGenericSolanaIndexer(ProtocolConfig{}), "generic_solana"},
		{NewRaydiumIndexer(ProtocolConfig{}), "raydium"},
		{NewOrcaIndexer(ProtocolConfig{}), "orca"},
		{NewJupiterIndexer(ProtocolConfig{}), "jupiter"},
	}

	for _, tt := range tests {
		if tt.indexer.Name() != tt.expected {
			t.Errorf("%T.Name() = %s, want %s", tt.indexer, tt.indexer.Name(), tt.expected)
		}
	}
}
