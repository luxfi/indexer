// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package evm

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"
)

// --- Beacon Deposit Event Parsing ---

// BeaconDepositEvent represents a parsed DepositEvent from the beacon deposit contract
type BeaconDepositEvent struct {
	Pubkey                string `json:"pubkey"`
	WithdrawalCredentials string `json:"withdrawalCredentials"`
	Amount                uint64 `json:"amount"` // In Gwei
	Signature             string `json:"signature"`
	Index                 uint64 `json:"index"`
}

// TopicBeaconDeposit is the event signature for DepositEvent(bytes pubkey, bytes withdrawal_credentials, bytes amount, bytes signature, bytes index)
const TopicBeaconDeposit = "0x649bbc62d0e31342afea4e5cd82d4049e7e1ee912fc0889aa790803be39038c5"

// BeaconDepositContractAddress is the Ethereum 2.0 deposit contract
const BeaconDepositContractAddress = "0x00000000219ab540356cbb839cbe05303d7705fa"

func TestTopicBeaconDeposit(t *testing.T) {
	if len(TopicBeaconDeposit) != 66 {
		t.Errorf("TopicBeaconDeposit length = %d, want 66", len(TopicBeaconDeposit))
	}
	if TopicBeaconDeposit[:2] != "0x" {
		t.Error("TopicBeaconDeposit should start with 0x")
	}
	// Verify it matches the known keccak256 of DepositEvent
	if TopicBeaconDeposit != "0x649bbc62d0e31342afea4e5cd82d4049e7e1ee912fc0889aa790803be39038c5" {
		t.Error("TopicBeaconDeposit does not match expected keccak256")
	}
}

func TestBeaconDepositContractAddress(t *testing.T) {
	if len(BeaconDepositContractAddress) != 42 {
		t.Errorf("address length = %d, want 42", len(BeaconDepositContractAddress))
	}
	if BeaconDepositContractAddress[:2] != "0x" {
		t.Error("address should start with 0x")
	}
	lower := strings.ToLower(BeaconDepositContractAddress)
	if lower != BeaconDepositContractAddress {
		t.Error("address should be lowercase")
	}
}

// parseBeaconDepositLog parses a DepositEvent log into a BeaconDepositEvent.
// The deposit event has no indexed params; all data is ABI-encoded in log.Data.
// Layout: 5 dynamic fields (pubkey, withdrawal_credentials, amount, signature, index)
// each is offset(32) + length(32) + data (padded to 32).
func parseBeaconDepositLog(log Log) (*BeaconDepositEvent, error) {
	if len(log.Topics) < 1 || log.Topics[0] != TopicBeaconDeposit {
		return nil, fmt.Errorf("not a beacon deposit event")
	}

	data := strings.TrimPrefix(log.Data, "0x")
	if len(data) < 640 { // minimum 5 offsets + 5 length+data sections
		return nil, fmt.Errorf("deposit event data too short: %d", len(data))
	}

	// Parse ABI-encoded dynamic fields
	// Offsets at positions 0-4 (each 32 bytes = 64 hex chars)
	// pubkey: 48 bytes, withdrawal_credentials: 32 bytes, amount: 8 bytes LE, signature: 96 bytes, index: 8 bytes LE
	offsets := make([]uint64, 5)
	for i := 0; i < 5; i++ {
		offsets[i] = hexToUint64("0x" + data[i*64:(i+1)*64])
	}

	readField := func(offset uint64) (string, error) {
		start := offset * 2 // byte offset to hex offset
		if int(start+64) > len(data) {
			return "", fmt.Errorf("field at offset %d out of bounds", offset)
		}
		length := hexToUint64("0x" + data[start:start+64])
		fieldStart := start + 64
		fieldEnd := fieldStart + length*2
		if int(fieldEnd) > len(data) {
			return "", fmt.Errorf("field data at offset %d extends past end", offset)
		}
		return data[fieldStart:fieldEnd], nil
	}

	pubkeyHex, err := readField(offsets[0])
	if err != nil {
		return nil, fmt.Errorf("pubkey: %w", err)
	}

	wcHex, err := readField(offsets[1])
	if err != nil {
		return nil, fmt.Errorf("withdrawal_credentials: %w", err)
	}

	amountHex, err := readField(offsets[2])
	if err != nil {
		return nil, fmt.Errorf("amount: %w", err)
	}

	sigHex, err := readField(offsets[3])
	if err != nil {
		return nil, fmt.Errorf("signature: %w", err)
	}

	indexHex, err := readField(offsets[4])
	if err != nil {
		return nil, fmt.Errorf("index: %w", err)
	}

	// Amount and index are little-endian 8-byte values
	amountLE := parseLittleEndianUint64(amountHex)
	indexLE := parseLittleEndianUint64(indexHex)

	return &BeaconDepositEvent{
		Pubkey:                "0x" + pubkeyHex,
		WithdrawalCredentials: "0x" + wcHex,
		Amount:                amountLE,
		Signature:             "0x" + sigHex,
		Index:                 indexLE,
	}, nil
}

// parseLittleEndianUint64 parses a hex string as a little-endian uint64
func parseLittleEndianUint64(hex string) uint64 {
	if len(hex) < 2 {
		return 0
	}
	var result uint64
	for i := 0; i < len(hex) && i < 16; i += 2 {
		b := hexToUint64("0x" + hex[i:i+2])
		result |= b << (uint(i/2) * 8)
	}
	return result
}

func TestParseBeaconDepositLog(t *testing.T) {
	// Construct a synthetic deposit event log
	// pubkey: 48 bytes (96 hex chars)
	// withdrawal_credentials: 32 bytes (64 hex chars)
	// amount: 8 bytes LE (16 hex chars) = 32000000000 gwei = 32 ETH
	// signature: 96 bytes (192 hex chars)
	// index: 8 bytes LE (16 hex chars) = validator index 42

	pubkey := strings.Repeat("ab", 48)     // 96 hex
	wc := strings.Repeat("cd", 32)         // 64 hex
	sig := strings.Repeat("ef", 96)        // 192 hex
	amountGwei := uint64(32000000000)      // 32 ETH in Gwei
	validatorIndex := uint64(42)

	amountLE := littleEndianHex(amountGwei)
	indexLE := littleEndianHex(validatorIndex)

	// ABI encode: 5 offsets, then 5 (length + padded data) sections
	// Each offset is 32 bytes. 5 offsets = 160 bytes.
	// Field 0 (pubkey): offset 160, length 48, data 48 bytes padded to 64
	// Field 1 (wc): offset 160+32+64=256, length 32, data 32 bytes padded to 32
	// Field 2 (amount): offset 256+32+32=320, length 8, data 8 bytes padded to 32
	// Field 3 (sig): offset 320+32+32=384, length 96, data 96 bytes padded to 96
	// Adjust: padding to 32 byte boundary
	// Field 4 (index): offset after sig, length 8, data 8 bytes padded to 32

	// Build each section
	padTo32 := func(hexData string) string {
		padLen := (32 - (len(hexData)/2)%32) % 32
		return hexData + strings.Repeat("00", padLen)
	}

	pubkeySection := fmt.Sprintf("%064x", 48) + padTo32(pubkey)
	wcSection := fmt.Sprintf("%064x", 32) + padTo32(wc)
	amountSection := fmt.Sprintf("%064x", 8) + padTo32(amountLE)
	sigSection := fmt.Sprintf("%064x", 96) + padTo32(sig)
	indexSection := fmt.Sprintf("%064x", 8) + padTo32(indexLE)

	// Calculate offsets (in bytes from start of data)
	offset0 := uint64(160)
	offset1 := offset0 + uint64(len(pubkeySection)/2)
	offset2 := offset1 + uint64(len(wcSection)/2)
	offset3 := offset2 + uint64(len(amountSection)/2)
	offset4 := offset3 + uint64(len(sigSection)/2)

	offsets := fmt.Sprintf("%064x%064x%064x%064x%064x", offset0, offset1, offset2, offset3, offset4)
	fullData := "0x" + offsets + pubkeySection + wcSection + amountSection + sigSection + indexSection

	log := Log{
		TxHash:      "0xdeadbeef",
		LogIndex:    0,
		BlockNumber: 100000,
		Address:     BeaconDepositContractAddress,
		Topics:      []string{TopicBeaconDeposit},
		Data:        fullData,
	}

	deposit, err := parseBeaconDepositLog(log)
	if err != nil {
		t.Fatalf("parseBeaconDepositLog failed: %v", err)
	}

	if deposit.Pubkey != "0x"+pubkey {
		t.Errorf("Pubkey = %s, want 0x%s", deposit.Pubkey[:10]+"...", pubkey[:10]+"...")
	}
	if deposit.WithdrawalCredentials != "0x"+wc {
		t.Errorf("WithdrawalCredentials mismatch")
	}
	if deposit.Amount != amountGwei {
		t.Errorf("Amount = %d, want %d", deposit.Amount, amountGwei)
	}
	if deposit.Signature != "0x"+sig {
		t.Errorf("Signature mismatch")
	}
	if deposit.Index != validatorIndex {
		t.Errorf("Index = %d, want %d", deposit.Index, validatorIndex)
	}
}

// littleEndianHex encodes a uint64 as a little-endian hex string (16 chars)
func littleEndianHex(v uint64) string {
	var buf [8]byte
	for i := 0; i < 8; i++ {
		buf[i] = byte(v >> (uint(i) * 8))
	}
	return fmt.Sprintf("%02x%02x%02x%02x%02x%02x%02x%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5], buf[6], buf[7])
}

func TestParseBeaconDepositLog_WrongTopic(t *testing.T) {
	log := Log{
		Topics: []string{"0x0000000000000000000000000000000000000000000000000000000000000000"},
		Data:   "0x" + strings.Repeat("00", 640),
	}
	_, err := parseBeaconDepositLog(log)
	if err == nil {
		t.Error("expected error for wrong topic")
	}
}

func TestParseBeaconDepositLog_EmptyTopics(t *testing.T) {
	log := Log{
		Topics: []string{},
		Data:   "0x",
	}
	_, err := parseBeaconDepositLog(log)
	if err == nil {
		t.Error("expected error for empty topics")
	}
}

func TestParseBeaconDepositLog_ShortData(t *testing.T) {
	log := Log{
		Topics: []string{TopicBeaconDeposit},
		Data:   "0x" + strings.Repeat("00", 100),
	}
	_, err := parseBeaconDepositLog(log)
	if err == nil {
		t.Error("expected error for short data")
	}
}

func TestParseLittleEndianUint64(t *testing.T) {
	tests := []struct {
		name string
		hex  string
		want uint64
	}{
		{"zero", "0000000000000000", 0},
		{"one", "0100000000000000", 1},
		{"256", "0001000000000000", 256},
		{"32 ETH in Gwei", littleEndianHex(32000000000), 32000000000},
		{"max byte", "ff00000000000000", 255},
		{"validator 42", littleEndianHex(42), 42},
		{"validator 1000", littleEndianHex(1000), 1000},
		{"short", "01", 1},
		{"empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLittleEndianUint64(tt.hex)
			if got != tt.want {
				t.Errorf("parseLittleEndianUint64(%q) = %d, want %d", tt.hex, got, tt.want)
			}
		})
	}
}

func TestLittleEndianHex(t *testing.T) {
	tests := []struct {
		value uint64
	}{
		{0},
		{1},
		{255},
		{256},
		{32000000000},
		{42},
		{1000},
		{^uint64(0)}, // max uint64
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.value), func(t *testing.T) {
			hex := littleEndianHex(tt.value)
			if len(hex) != 16 {
				t.Errorf("length = %d, want 16", len(hex))
			}
			roundtrip := parseLittleEndianUint64(hex)
			if roundtrip != tt.value {
				t.Errorf("roundtrip = %d, want %d", roundtrip, tt.value)
			}
		})
	}
}

func TestBeaconDepositEventSerialization(t *testing.T) {
	evt := BeaconDepositEvent{
		Pubkey:                "0x" + strings.Repeat("ab", 48),
		WithdrawalCredentials: "0x" + strings.Repeat("cd", 32),
		Amount:                32000000000,
		Signature:             "0x" + strings.Repeat("ef", 96),
		Index:                 42,
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded BeaconDepositEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Amount != 32000000000 {
		t.Errorf("Amount = %d, want 32000000000", decoded.Amount)
	}
	if decoded.Index != 42 {
		t.Errorf("Index = %d, want 42", decoded.Index)
	}
	if decoded.Pubkey != evt.Pubkey {
		t.Error("Pubkey mismatch")
	}
	if decoded.WithdrawalCredentials != evt.WithdrawalCredentials {
		t.Error("WithdrawalCredentials mismatch")
	}
	if decoded.Signature != evt.Signature {
		t.Error("Signature mismatch")
	}
}

// --- Withdrawal (EIP-4895) Tests ---

func TestWithdrawalSerialization(t *testing.T) {
	tests := []struct {
		name       string
		withdrawal Withdrawal
	}{
		{
			name: "standard withdrawal",
			withdrawal: Withdrawal{
				Index:          100,
				ValidatorIndex: 50,
				Address:        "0x1234567890123456789012345678901234567890",
				Amount:         "1000000000",
				BlockNumber:    1000,
				BlockHash:      "0xblock",
				Timestamp:      time.Now().Truncate(time.Second),
			},
		},
		{
			name: "zero index",
			withdrawal: Withdrawal{
				Index:          0,
				ValidatorIndex: 0,
				Address:        "0x0000000000000000000000000000000000000000",
				Amount:         "0",
				BlockNumber:    0,
				BlockHash:      "0x",
				Timestamp:      time.Time{},
			},
		},
		{
			name: "large values",
			withdrawal: Withdrawal{
				Index:          ^uint64(0),
				ValidatorIndex: 999999,
				Address:        "0xffffffffffffffffffffffffffffffffffffffff",
				Amount:         "115792089237316195423570985008687907853269984665640564039457584007913129639935",
				BlockNumber:    999999999,
				BlockHash:      "0x" + strings.Repeat("ff", 32),
				Timestamp:      time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.withdrawal)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var decoded Withdrawal
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if decoded.Index != tt.withdrawal.Index {
				t.Errorf("Index = %d, want %d", decoded.Index, tt.withdrawal.Index)
			}
			if decoded.ValidatorIndex != tt.withdrawal.ValidatorIndex {
				t.Errorf("ValidatorIndex = %d, want %d", decoded.ValidatorIndex, tt.withdrawal.ValidatorIndex)
			}
			if decoded.Address != tt.withdrawal.Address {
				t.Errorf("Address = %q, want %q", decoded.Address, tt.withdrawal.Address)
			}
			if decoded.Amount != tt.withdrawal.Amount {
				t.Errorf("Amount = %q, want %q", decoded.Amount, tt.withdrawal.Amount)
			}
			if decoded.BlockNumber != tt.withdrawal.BlockNumber {
				t.Errorf("BlockNumber = %d, want %d", decoded.BlockNumber, tt.withdrawal.BlockNumber)
			}
		})
	}
}

func TestWithdrawalAddressFormat(t *testing.T) {
	tests := []struct {
		name    string
		address string
		valid   bool
	}{
		{"standard", "0x1234567890123456789012345678901234567890", true},
		{"all zeros", "0x0000000000000000000000000000000000000000", true},
		{"all ff", "0xffffffffffffffffffffffffffffffffffffffff", true},
		{"no prefix", "1234567890123456789012345678901234567890", false},
		{"too short", "0x1234", false},
		{"too long", "0x12345678901234567890123456789012345678901", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := len(tt.address) == 42 && tt.address[:2] == "0x"
			if isValid != tt.valid {
				t.Errorf("address %q valid = %v, want %v", tt.address, isValid, tt.valid)
			}
		})
	}
}

func TestWithdrawalAmountConversion(t *testing.T) {
	// Withdrawals are in Gwei. 1 ETH = 1e9 Gwei.
	tests := []struct {
		name      string
		gwei      string
		wantETH   string
	}{
		{"1 ETH", "1000000000", "1"},
		{"32 ETH", "32000000000", "32"},
		{"0.1 ETH", "100000000", "0"},      // integer division
		{"0 Gwei", "0", "0"},
		{"1 Gwei", "1", "0"},                // less than 1 ETH
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gwei := new(big.Int)
			gwei.SetString(tt.gwei, 10)
			eth := new(big.Int).Div(gwei, big.NewInt(1e9))
			if eth.String() != tt.wantETH {
				t.Errorf("gwei %s = %s ETH, want %s ETH", tt.gwei, eth.String(), tt.wantETH)
			}
		})
	}
}

// --- EnhancedBlock Withdrawal Tests ---

func TestEnhancedBlockWithWithdrawals(t *testing.T) {
	block := EnhancedBlock{
		Hash:        "0xblock",
		Number:      17034870, // Shanghai fork block (approx)
		Timestamp:   time.Now(),
		Miner:       "0xminer",
		GasLimit:    30000000,
		GasUsed:     15000000,
		Withdrawals: []Withdrawal{
			{Index: 0, ValidatorIndex: 100, Address: "0xaddr1", Amount: "1000000000"},
			{Index: 1, ValidatorIndex: 200, Address: "0xaddr2", Amount: "2000000000"},
			{Index: 2, ValidatorIndex: 300, Address: "0xaddr3", Amount: "500000000"},
		},
		WithdrawalsRoot: "0x" + strings.Repeat("ab", 32),
	}

	if len(block.Withdrawals) != 3 {
		t.Errorf("Withdrawals count = %d, want 3", len(block.Withdrawals))
	}

	// Verify sequential indices
	for i, w := range block.Withdrawals {
		if w.Index != uint64(i) {
			t.Errorf("Withdrawal[%d].Index = %d, want %d", i, w.Index, i)
		}
	}

	// Test total amount
	var total uint64
	for _, w := range block.Withdrawals {
		amount := new(big.Int)
		amount.SetString(w.Amount, 10)
		total += amount.Uint64()
	}
	if total != 3500000000 {
		t.Errorf("total withdrawal amount = %d, want 3500000000", total)
	}

	// Serialization
	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded EnhancedBlock
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(decoded.Withdrawals) != 3 {
		t.Errorf("decoded Withdrawals count = %d, want 3", len(decoded.Withdrawals))
	}
	if decoded.WithdrawalsRoot == "" {
		t.Error("WithdrawalsRoot should not be empty")
	}
}

func TestEnhancedBlockWithoutWithdrawals(t *testing.T) {
	// Pre-Shanghai blocks have no withdrawals
	block := EnhancedBlock{
		Hash:      "0xblock",
		Number:    15000000,
		Timestamp: time.Now(),
		Miner:     "0xminer",
		GasLimit:  30000000,
		GasUsed:   15000000,
	}

	if block.Withdrawals != nil {
		t.Error("pre-Shanghai block should have nil Withdrawals")
	}
	if block.WithdrawalsRoot != "" {
		t.Error("pre-Shanghai block should have empty WithdrawalsRoot")
	}
}

// --- Beacon Block Root Tests (EIP-4788) ---

func TestEnhancedBlockParentBeaconRoot(t *testing.T) {
	block := EnhancedBlock{
		Hash:             "0xblock",
		Number:           19000000,
		ParentBeaconRoot: "0x" + strings.Repeat("be", 32),
	}

	if block.ParentBeaconRoot == "" {
		t.Error("ParentBeaconRoot should not be empty for Dencun blocks")
	}
	if len(block.ParentBeaconRoot) != 66 {
		t.Errorf("ParentBeaconRoot length = %d, want 66", len(block.ParentBeaconRoot))
	}
}

// --- Deposit Amount Validation Tests ---

func TestDepositAmountValidation(t *testing.T) {
	tests := []struct {
		name    string
		amount  uint64
		valid   bool // >= 1 ETH (1e9 gwei) for a valid deposit
	}{
		{"32 ETH (standard)", 32000000000, true},
		{"1 ETH (minimum)", 1000000000, true},
		{"0.5 ETH", 500000000, false},
		{"0 Gwei", 0, false},
		{"1 Gwei", 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := tt.amount >= 1000000000
			if isValid != tt.valid {
				t.Errorf("amount %d valid = %v, want %v", tt.amount, isValid, tt.valid)
			}
		})
	}
}

// --- Validator Index Tests ---

func TestValidatorIndexRange(t *testing.T) {
	tests := []struct {
		name  string
		index uint64
	}{
		{"genesis validator", 0},
		{"early validator", 42},
		{"post-merge", 500000},
		{"large index", 999999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := Withdrawal{
				ValidatorIndex: tt.index,
				Address:        "0x1234567890123456789012345678901234567890",
				Amount:         "1000000000",
			}
			if w.ValidatorIndex != tt.index {
				t.Errorf("ValidatorIndex = %d, want %d", w.ValidatorIndex, tt.index)
			}
		})
	}
}

// --- Withdrawal Credential Type Tests ---

func TestWithdrawalCredentialTypes(t *testing.T) {
	tests := []struct {
		name       string
		credential string
		credType   string
	}{
		{
			name:       "BLS withdrawal (0x00)",
			credential: "0x00" + strings.Repeat("ab", 31),
			credType:   "bls",
		},
		{
			name:       "ETH1 withdrawal (0x01)",
			credential: "0x01" + strings.Repeat("00", 11) + "1234567890123456789012345678901234567890",
			credType:   "eth1",
		},
		{
			name:       "Compounding (0x02)",
			credential: "0x02" + strings.Repeat("00", 11) + "1234567890123456789012345678901234567890",
			credType:   "compounding",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix := tt.credential[2:4]
			var credType string
			switch prefix {
			case "00":
				credType = "bls"
			case "01":
				credType = "eth1"
			case "02":
				credType = "compounding"
			default:
				credType = "unknown"
			}

			if credType != tt.credType {
				t.Errorf("credential type = %q, want %q", credType, tt.credType)
			}

			if len(tt.credential) != 66 {
				t.Errorf("credential length = %d, want 66", len(tt.credential))
			}
		})
	}
}

// --- Beacon Deposit Parsing Edge Cases ---

func TestBeaconDepositEvent_VariousAmounts(t *testing.T) {
	tests := []struct {
		name   string
		amount uint64
	}{
		{"minimum effective", 1000000000},       // 1 ETH
		{"standard 32 ETH", 32000000000},        // 32 ETH
		{"max effective", 2048000000000},         // 2048 ETH (theoretical)
		{"exact 1 gwei", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := BeaconDepositEvent{
				Pubkey:                "0x" + strings.Repeat("aa", 48),
				WithdrawalCredentials: "0x" + strings.Repeat("bb", 32),
				Amount:                tt.amount,
				Signature:             "0x" + strings.Repeat("cc", 96),
				Index:                 0,
			}

			data, err := json.Marshal(evt)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var decoded BeaconDepositEvent
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if decoded.Amount != tt.amount {
				t.Errorf("Amount = %d, want %d", decoded.Amount, tt.amount)
			}
		})
	}
}

func TestBeaconDepositEvent_PubkeyLength(t *testing.T) {
	// BLS pubkey is 48 bytes = 96 hex chars + 0x prefix = 98
	pubkey := "0x" + strings.Repeat("ab", 48)
	if len(pubkey) != 98 {
		t.Errorf("pubkey length = %d, want 98", len(pubkey))
	}
}

func TestBeaconDepositEvent_SignatureLength(t *testing.T) {
	// BLS signature is 96 bytes = 192 hex chars + 0x prefix = 194
	sig := "0x" + strings.Repeat("ab", 96)
	if len(sig) != 194 {
		t.Errorf("signature length = %d, want 194", len(sig))
	}
}

// --- Multiple Withdrawals Per Block ---

func TestMultipleWithdrawalsPerBlock(t *testing.T) {
	// Shanghai allows up to 16 withdrawals per block
	maxWithdrawals := 16
	withdrawals := make([]Withdrawal, maxWithdrawals)

	for i := 0; i < maxWithdrawals; i++ {
		withdrawals[i] = Withdrawal{
			Index:          uint64(1000 + i),
			ValidatorIndex: uint64(i * 100),
			Address:        fmt.Sprintf("0x%040x", i),
			Amount:         fmt.Sprintf("%d", (i+1)*1000000000),
			BlockNumber:    17034870,
			BlockHash:      "0x" + strings.Repeat("ff", 32),
			Timestamp:      time.Now(),
		}
	}

	if len(withdrawals) != maxWithdrawals {
		t.Errorf("withdrawal count = %d, want %d", len(withdrawals), maxWithdrawals)
	}

	// Verify uniqueness of indices
	seen := make(map[uint64]bool)
	for _, w := range withdrawals {
		if seen[w.Index] {
			t.Errorf("duplicate withdrawal index: %d", w.Index)
		}
		seen[w.Index] = true
	}
}

// --- Benchmarks ---

func BenchmarkParseBeaconDepositLog(b *testing.B) {
	pubkey := strings.Repeat("ab", 48)
	wc := strings.Repeat("cd", 32)
	sig := strings.Repeat("ef", 96)
	amountLE := littleEndianHex(32000000000)
	indexLE := littleEndianHex(42)

	padTo32 := func(hexData string) string {
		padLen := (32 - (len(hexData)/2)%32) % 32
		return hexData + strings.Repeat("00", padLen)
	}

	pubkeySection := fmt.Sprintf("%064x", 48) + padTo32(pubkey)
	wcSection := fmt.Sprintf("%064x", 32) + padTo32(wc)
	amountSection := fmt.Sprintf("%064x", 8) + padTo32(amountLE)
	sigSection := fmt.Sprintf("%064x", 96) + padTo32(sig)
	indexSection := fmt.Sprintf("%064x", 8) + padTo32(indexLE)

	offset0 := uint64(160)
	offset1 := offset0 + uint64(len(pubkeySection)/2)
	offset2 := offset1 + uint64(len(wcSection)/2)
	offset3 := offset2 + uint64(len(amountSection)/2)
	offset4 := offset3 + uint64(len(sigSection)/2)

	offsets := fmt.Sprintf("%064x%064x%064x%064x%064x", offset0, offset1, offset2, offset3, offset4)
	fullData := "0x" + offsets + pubkeySection + wcSection + amountSection + sigSection + indexSection

	log := Log{
		Topics: []string{TopicBeaconDeposit},
		Data:   fullData,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseBeaconDepositLog(log)
	}
}

func BenchmarkParseLittleEndianUint64(b *testing.B) {
	hex := littleEndianHex(32000000000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseLittleEndianUint64(hex)
	}
}

func BenchmarkWithdrawalMarshal(b *testing.B) {
	w := Withdrawal{
		Index:          100,
		ValidatorIndex: 50,
		Address:        "0x1234567890123456789012345678901234567890",
		Amount:         "1000000000",
		BlockNumber:    1000,
		BlockHash:      "0xblock",
		Timestamp:      time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(w)
	}
}
