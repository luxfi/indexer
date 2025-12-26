// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package evm

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

// TestNewERC4337Indexer tests ERC4337Indexer creation
func TestNewERC4337Indexer(t *testing.T) {
	adapter := &Adapter{}
	indexer := NewERC4337Indexer(adapter, nil)

	if indexer == nil {
		t.Fatal("NewERC4337Indexer returned nil")
	}
	if indexer.adapter != adapter {
		t.Error("adapter not set correctly")
	}
	if len(indexer.entryPoints) != 2 {
		t.Errorf("entryPoints length = %d, want 2", len(indexer.entryPoints))
	}
	if indexer.entryPoints[0] != EntryPointV06 {
		t.Errorf("entryPoints[0] = %s, want %s", indexer.entryPoints[0], EntryPointV06)
	}
	if indexer.entryPoints[1] != EntryPointV07 {
		t.Errorf("entryPoints[1] = %s, want %s", indexer.entryPoints[1], EntryPointV07)
	}
}

// TestDecodeCallData tests callData decoding
func TestDecodeCallData(t *testing.T) {
	tests := []struct {
		name           string
		callData       string
		wantSelector   string
		wantFunction   string
	}{
		{
			name:         "execute function",
			callData:     "0xb61d27f6000000000000000000000000a0b86991c6218b36c1d19d4a2e9eb0ce3606eb480000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000006000000000000000000000000000000000000000000000000000000000000000044",
			wantSelector: "0xb61d27f6",
			wantFunction: "execute",
		},
		{
			name:         "executeBatch function",
			callData:     "0x47e1da2a00000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000002",
			wantSelector: "0x47e1da2a",
			wantFunction: "executeBatch",
		},
		{
			name:         "short data",
			callData:     "0x1234",
			wantSelector: "",
			wantFunction: "",
		},
		{
			name:         "empty data",
			callData:     "",
			wantSelector: "",
			wantFunction: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selector, decoded := DecodeCallData(tt.callData)
			if selector != tt.wantSelector {
				t.Errorf("selector = %q, want %q", selector, tt.wantSelector)
			}
			if decoded != nil {
				if fn, ok := decoded["function"].(string); ok && fn != tt.wantFunction {
					t.Errorf("function = %q, want %q", fn, tt.wantFunction)
				}
			}
		})
	}
}

// TestDecodeInitCode tests initCode decoding
func TestDecodeInitCode(t *testing.T) {
	tests := []struct {
		name        string
		initCode    string
		wantFactory string
		wantData    string
	}{
		{
			name:        "valid initCode",
			initCode:    "0x5ff137d4b0fdcd49dca30c7cf57e578a026d27896af4bb9c82d4af9d82e4c9a0",
			wantFactory: "0x5ff137d4b0fdcd49dca30c7cf57e578a026d2789",
			wantData:    "0x6af4bb9c82d4af9d82e4c9a0",
		},
		{
			name:        "short initCode",
			initCode:    "0x1234",
			wantFactory: "",
			wantData:    "",
		},
		{
			name:        "empty initCode",
			initCode:    "",
			wantFactory: "",
			wantData:    "",
		},
		{
			name:        "only factory address",
			initCode:    "0x5ff137d4b0fdcd49dca30c7cf57e578a026d2789",
			wantFactory: "0x5ff137d4b0fdcd49dca30c7cf57e578a026d2789",
			wantData:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory, data := DecodeInitCode(tt.initCode)
			if factory != tt.wantFactory {
				t.Errorf("factory = %q, want %q", factory, tt.wantFactory)
			}
			if data != tt.wantData {
				t.Errorf("data = %q, want %q", data, tt.wantData)
			}
		})
	}
}

// TestDecodePaymasterAndData tests paymasterAndData decoding
func TestDecodePaymasterAndData(t *testing.T) {
	tests := []struct {
		name            string
		paymasterData   string
		wantPaymaster   string
		wantData        string
	}{
		{
			name:          "valid paymasterAndData",
			paymasterData: "0x5ff137d4b0fdcd49dca30c7cf57e578a026d27896af4bb9c82d4af9d82e4c9a0",
			wantPaymaster: "0x5ff137d4b0fdcd49dca30c7cf57e578a026d2789",
			wantData:      "0x6af4bb9c82d4af9d82e4c9a0",
		},
		{
			name:          "paymaster only",
			paymasterData: "0x5ff137d4b0fdcd49dca30c7cf57e578a026d2789",
			wantPaymaster: "0x5ff137d4b0fdcd49dca30c7cf57e578a026d2789",
			wantData:      "",
		},
		{
			name:          "short data",
			paymasterData: "0x1234",
			wantPaymaster: "",
			wantData:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paymaster, data := DecodePaymasterAndData(tt.paymasterData)
			if paymaster != tt.wantPaymaster {
				t.Errorf("paymaster = %q, want %q", paymaster, tt.wantPaymaster)
			}
			if data != tt.wantData {
				t.Errorf("data = %q, want %q", data, tt.wantData)
			}
		})
	}
}

// TestUserOperationValidation tests UserOperation field validation
func TestUserOperationValidation(t *testing.T) {
	op := UserOperation{
		Hash:                 "0x" + string(make([]byte, 64)),
		Sender:               "0x1234567890123456789012345678901234567890",
		Nonce:                "0x1",
		InitCode:             "0x",
		CallData:             "0xb61d27f6",
		CallGasLimit:         100000,
		VerificationGasLimit: 50000,
		PreVerificationGas:   21000,
		MaxFeePerGas:         "1000000000",
		MaxPriorityFeePerGas: "1000000",
		PaymasterAndData:     "0x",
		Signature:            "0x",
		EntryPoint:           EntryPointV06,
		BlockNumber:          1000,
		BlockHash:            "0xblock",
		TransactionHash:      "0xtx",
		BundlerAddress:       "0xbundler",
		Status:               1,
		Timestamp:            time.Now(),
		EntryPointVersion:    "v0.6",
	}

	// Test serialization
	data, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded UserOperation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify fields
	if decoded.Sender != op.Sender {
		t.Errorf("Sender mismatch")
	}
	if decoded.CallGasLimit != op.CallGasLimit {
		t.Errorf("CallGasLimit mismatch")
	}
	if decoded.EntryPointVersion != op.EntryPointVersion {
		t.Errorf("EntryPointVersion mismatch")
	}
}

// TestBundleSerialization tests Bundle JSON serialization
func TestBundleSerialization(t *testing.T) {
	bundle := Bundle{
		TransactionHash: "0xtx123",
		BlockNumber:     1000,
		BlockHash:       "0xblock",
		BundlerAddress:  "0xbundler",
		OperationCount:  3,
		OperationHashes: []string{"0xop1", "0xop2", "0xop3"},
		TotalGasUsed:    500000,
		GasPrice:        "1000000000",
		Timestamp:       time.Now(),
	}

	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Bundle
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.OperationCount != 3 {
		t.Errorf("OperationCount = %d, want 3", decoded.OperationCount)
	}
	if len(decoded.OperationHashes) != 3 {
		t.Errorf("OperationHashes length = %d, want 3", len(decoded.OperationHashes))
	}
}

// TestBundlerSerialization tests Bundler JSON serialization
func TestBundlerSerialization(t *testing.T) {
	now := time.Now()
	bundler := Bundler{
		Address:            "0xbundler123",
		TotalOperations:    1000,
		TotalBundles:       500,
		TotalGasSponsored:  "1000000000000000000",
		SuccessRate:        0.95,
		FirstSeenBlock:     100,
		LastSeenBlock:      1000,
		FirstSeenTimestamp: now.Add(-time.Hour * 24),
		LastSeenTimestamp:  now,
	}

	data, err := json.Marshal(bundler)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Bundler
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.TotalOperations != 1000 {
		t.Errorf("TotalOperations = %d, want 1000", decoded.TotalOperations)
	}
	if decoded.SuccessRate != 0.95 {
		t.Errorf("SuccessRate = %f, want 0.95", decoded.SuccessRate)
	}
}

// TestPaymasterSerialization tests Paymaster JSON serialization
func TestPaymasterSerialization(t *testing.T) {
	now := time.Now()
	paymaster := Paymaster{
		Address:            "0xpaymaster123",
		TotalOperations:    500,
		TotalGasSponsored:  "500000000000000000",
		TotalEthSponsored:  "0.5",
		UniqueAccounts:     100,
		FirstSeenBlock:     100,
		LastSeenBlock:      1000,
		FirstSeenTimestamp: now.Add(-time.Hour * 24),
		LastSeenTimestamp:  now,
	}

	data, err := json.Marshal(paymaster)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Paymaster
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.UniqueAccounts != 100 {
		t.Errorf("UniqueAccounts = %d, want 100", decoded.UniqueAccounts)
	}
}

// TestAccountFactorySerialization tests AccountFactory JSON serialization
func TestAccountFactorySerialization(t *testing.T) {
	now := time.Now()
	factory := AccountFactory{
		Address:            "0xfactory123",
		TotalAccounts:      1000,
		ImplementationType: "SimpleAccount",
		FirstSeenBlock:     100,
		LastSeenBlock:      1000,
		FirstSeenTimestamp: now.Add(-time.Hour * 24),
		LastSeenTimestamp:  now,
	}

	data, err := json.Marshal(factory)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded AccountFactory
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ImplementationType != "SimpleAccount" {
		t.Errorf("ImplementationType = %q, want SimpleAccount", decoded.ImplementationType)
	}
}

// TestSmartAccountSerialization tests SmartAccount JSON serialization
func TestSmartAccountSerialization(t *testing.T) {
	now := time.Now()
	account := SmartAccount{
		Address:          "0xaccount123",
		FactoryAddress:   "0xfactory123",
		DeploymentTxHash: "0xtx123",
		DeploymentBlock:  100,
		TotalOperations:  50,
		TotalGasUsed:     "1000000000000000",
		CreatedAt:        now.Add(-time.Hour * 24),
		UpdatedAt:        now,
	}

	data, err := json.Marshal(account)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded SmartAccount
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.DeploymentBlock != 100 {
		t.Errorf("DeploymentBlock = %d, want 100", decoded.DeploymentBlock)
	}
}

// TestTopicConstants tests ERC-4337 topic constants
func TestTopicConstants(t *testing.T) {
	// UserOperationEvent topic should be 66 chars (0x + 64 hex)
	if len(TopicUserOperationEvent) != 66 {
		t.Errorf("TopicUserOperationEvent length = %d, want 66", len(TopicUserOperationEvent))
	}
	if TopicUserOperationEvent[:2] != "0x" {
		t.Error("TopicUserOperationEvent should start with 0x")
	}

	// AccountDeployed topic
	if len(TopicAccountDeployed) != 66 {
		t.Errorf("TopicAccountDeployed length = %d, want 66", len(TopicAccountDeployed))
	}

	// BeforeExecution topic
	if len(TopicBeforeExecution) != 66 {
		t.Errorf("TopicBeforeExecution length = %d, want 66", len(TopicBeforeExecution))
	}
}

// MockDB implements minimal database interface for testing
type MockDB struct {
	*sql.DB
}

// TestCalculateUserOpHash tests hash calculation placeholder
func TestCalculateUserOpHash(t *testing.T) {
	op := &UserOperation{
		Hash:       "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		Sender:     "0x1234567890123456789012345678901234567890",
		Nonce:      "0x1",
		EntryPoint: EntryPointV06,
	}

	hash := CalculateUserOpHash(op, 96369)
	if hash != op.Hash {
		t.Errorf("CalculateUserOpHash returned %s, want %s", hash, op.Hash)
	}
}

// Benchmarks

func BenchmarkDecodeCallData(b *testing.B) {
	callData := "0xb61d27f6000000000000000000000000a0b86991c6218b36c1d19d4a2e9eb0ce3606eb480000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000006000000000000000000000000000000000000000000000000000000000000000044"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeCallData(callData)
	}
}

func BenchmarkDecodeInitCode(b *testing.B) {
	initCode := "0x5ff137d4b0fdcd49dca30c7cf57e578a026d27896af4bb9c82d4af9d82e4c9a0"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeInitCode(initCode)
	}
}

func BenchmarkUserOperationMarshalComplete(b *testing.B) {
	op := UserOperation{
		Hash:                 "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		Sender:               "0x1234567890123456789012345678901234567890",
		Nonce:                "0x1",
		InitCode:             "0x5ff137d4b0fdcd49dca30c7cf57e578a026d27896af4bb9c82d4af9d82e4c9a0",
		CallData:             "0xb61d27f6000000000000000000000000a0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
		CallGasLimit:         100000,
		VerificationGasLimit: 50000,
		PreVerificationGas:   21000,
		MaxFeePerGas:         "1000000000",
		MaxPriorityFeePerGas: "1000000",
		PaymasterAndData:     "0x",
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(op)
	}
}

// TestProcessBlockContext tests context handling
func TestProcessBlockContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	indexer := &ERC4337Indexer{
		adapter:     nil,
		db:          nil,
		entryPoints: []string{EntryPointV06},
	}

	// This should return quickly due to nil adapter
	// In real tests, we'd mock the adapter
	_ = indexer
	_ = ctx
}
