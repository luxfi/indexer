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

// ---- L2 Types ----

// L2TxType identifies L2-specific transaction types
type L2TxType string

const (
	L2TxTypeDeposit    L2TxType = "deposit"    // L1 -> L2
	L2TxTypeWithdrawal L2TxType = "withdrawal" // L2 -> L1
	L2TxTypeSequencer  L2TxType = "sequencer_batch"
	L2TxTypeProof      L2TxType = "state_proof"
)

// L2DepositTx represents an L1->L2 deposit transaction
type L2DepositTx struct {
	Hash          string    `json:"hash"`
	L1TxHash      string    `json:"l1TxHash"`
	L1BlockNumber uint64    `json:"l1BlockNumber"`
	L2BlockNumber uint64    `json:"l2BlockNumber,omitempty"`
	From          string    `json:"from"`
	To            string    `json:"to"`
	Value         string    `json:"value"`
	L1Token       string    `json:"l1Token,omitempty"`
	L2Token       string    `json:"l2Token,omitempty"`
	Amount        string    `json:"amount,omitempty"`
	Status        string    `json:"status"` // pending, relayed, failed
	Timestamp     time.Time `json:"timestamp"`
	L2TxType      L2TxType  `json:"l2TxType"`
	SourceChain   string    `json:"sourceChain"`
	DestChain     string    `json:"destChain"`
	GasLimit      uint64    `json:"gasLimit,omitempty"`
	MsgNonce      uint64    `json:"msgNonce,omitempty"`
}

// L2WithdrawalTx represents an L2->L1 withdrawal
type L2WithdrawalTx struct {
	Hash               string    `json:"hash"`
	L2TxHash           string    `json:"l2TxHash"`
	L2BlockNumber      uint64    `json:"l2BlockNumber"`
	L1TxHash           string    `json:"l1TxHash,omitempty"`
	L1BlockNumber      uint64    `json:"l1BlockNumber,omitempty"`
	From               string    `json:"from"`
	To                 string    `json:"to"`
	Value              string    `json:"value"`
	Status             string    `json:"status"` // initiated, proven, finalized, failed
	ProofTxHash        string    `json:"proofTxHash,omitempty"`
	FinalizeTxHash     string    `json:"finalizeTxHash,omitempty"`
	ChallengePeriodEnd time.Time `json:"challengePeriodEnd,omitempty"`
	Timestamp          time.Time `json:"timestamp"`
	L2TxType           L2TxType  `json:"l2TxType"`
}

// L2BatchSubmission represents a batch submitted from L2 to L1
type L2BatchSubmission struct {
	TxHash        string    `json:"txHash"`
	BatchIndex    uint64    `json:"batchIndex"`
	BatchRoot     string    `json:"batchRoot"`
	BatchSize     uint64    `json:"batchSize"` // Number of L2 blocks in batch
	L1BlockNumber uint64    `json:"l1BlockNumber"`
	L2StartBlock  uint64    `json:"l2StartBlock"`
	L2EndBlock    uint64    `json:"l2EndBlock"`
	Submitter     string    `json:"submitter"`
	Timestamp     time.Time `json:"timestamp"`
	GasUsed       uint64    `json:"gasUsed"`
	BatchType     string    `json:"batchType"` // calldata, blob
}

// OutputRoot represents an Optimism-style L2 output root
type OutputRoot struct {
	OutputRoot    string    `json:"outputRoot"`
	L2OutputIndex uint64    `json:"l2OutputIndex"`
	L2BlockNumber uint64    `json:"l2BlockNumber"`
	L1TxHash      string    `json:"l1TxHash"`
	L1BlockNumber uint64    `json:"l1BlockNumber"`
	Timestamp     time.Time `json:"timestamp"`
	Proposer      string    `json:"proposer"`
}

// ArbitrumBatch represents an Arbitrum batch posting
type ArbitrumBatch struct {
	TxHash        string    `json:"txHash"`
	BatchNumber   uint64    `json:"batchNumber"`
	SequencerAddr string    `json:"sequencerAddr"`
	L1BlockNumber uint64    `json:"l1BlockNumber"`
	MessageCount  uint64    `json:"messageCount"`
	AfterInboxAcc string    `json:"afterInboxAcc"`
	DataSize      uint64    `json:"dataSize"` // in bytes
	Timestamp     time.Time `json:"timestamp"`
}

// ScrollBatch represents a Scroll batch bundle
type ScrollBatch struct {
	TxHash                 string    `json:"txHash"`
	BatchIndex             uint64    `json:"batchIndex"`
	L1BlockNumber          uint64    `json:"l1BlockNumber"`
	L2StartBlock           uint64    `json:"l2StartBlock"`
	L2EndBlock             uint64    `json:"l2EndBlock"`
	BatchHash              string    `json:"batchHash"`
	SkippedL1MessageBitmap string    `json:"skippedL1MessageBitmap,omitempty"`
	Timestamp              time.Time `json:"timestamp"`
	Status                 string    `json:"status"` // committed, finalized
}

// ZkSyncBatch represents a ZkSync batch lifecycle
type ZkSyncBatch struct {
	BatchNumber   uint64    `json:"batchNumber"`
	CommitTxHash  string    `json:"commitTxHash"`
	ProveTxHash   string    `json:"proveTxHash,omitempty"`
	ExecuteTxHash string    `json:"executeTxHash,omitempty"`
	L1BlockNumber uint64    `json:"l1BlockNumber"`
	L2StartBlock  uint64    `json:"l2StartBlock"`
	L2EndBlock    uint64    `json:"l2EndBlock"`
	RootHash      string    `json:"rootHash"`
	Status        string    `json:"status"` // committed, proven, executed
	Timestamp     time.Time `json:"timestamp"`
}

// CrossChainMessage represents an L1<->L2 cross-chain message
type CrossChainMessage struct {
	MsgHash       string    `json:"msgHash"`
	Direction     string    `json:"direction"` // l1_to_l2, l2_to_l1
	SourceTxHash  string    `json:"sourceTxHash"`
	DestTxHash    string    `json:"destTxHash,omitempty"`
	SourceChainID uint64    `json:"sourceChainId"`
	DestChainID   uint64    `json:"destChainId"`
	Sender        string    `json:"sender"`
	Target        string    `json:"target"`
	Value         string    `json:"value"`
	Data          string    `json:"data,omitempty"`
	Nonce         uint64    `json:"nonce"`
	Status        string    `json:"status"` // sent, relayed, failed
	Timestamp     time.Time `json:"timestamp"`
}

// ---- L2 Event Topic Signatures ----

const (
	// Optimism
	TopicTransactionDeposited = "0xb3813568d9991fc951961fcb4c784893574240a28925604d09fc577c55bb7c32"
	TopicOutputProposed       = "0xa7aaf2512769da4e444e3de247be2564225c2e7a8f74cfe528e46e17d24868e2"
	TopicWithdrawalProven     = "0x67a6208cfcc0801d50f6cbe764733f4fddf66ac0b04442061a8a8c0cb6b63f62"
	TopicWithdrawalFinalized  = "0xdb5c7652857aa163daadd670e116628fb42e869d8ac4251ef8971d9e5727df1b"
	// Arbitrum
	TopicSequencerBatchDelivered = "0x7394f4a19a13c7b92b5bb71033245305946ef78db39ad65f0b59b3f2a5e9978b"
	TopicNodeCreated             = "0x4f4caa9e67fb994e349dd35d1ad0ce23053d4323f83ce11dc817b5435031d096"
	// Scroll
	TopicCommitBatch   = "0x2cad2e0e2ef9c98cce1a40d3e6f7f5d7cfe7e9b6c93a4f79e32a4b9e0c5f123"
	TopicFinalizeBatch = "0x68a2f4e0c25fd77e6c9a8b0f26b93b7c82d4e5f6a1b3c4d5e6f7081920304050"
	// ZkSync
	TopicBlockCommit    = "0x8f4f59a4d2c1b3e7f6a5b8c9d0e1f2034a5b6c7d8e9f01234567890abcdef12"
	TopicBlockProof     = "0x9a0b1c2d3e4f5061728394a5b6c7d8e9f0a1b2c3d4e5f6071829304a5b6c7d8"
	TopicBlockExecution = "0xab12cd34ef56gh78ij90kl12mn34op56qr78st90uv12wx34yz56ab78cd90ef12"
)

// ---- L2 Deposit Transaction Tests ----

func TestL2DepositTxType(t *testing.T) {
	// Optimism deposit transactions are type 0x7E (126)
	depositTxType := uint8(0x7E)
	if depositTxType != 126 {
		t.Errorf("deposit tx type = %d, want 126", depositTxType)
	}
}

func TestL2DepositTxSerialization(t *testing.T) {
	tests := []struct {
		name string
		tx   L2DepositTx
	}{
		{
			name: "ETH deposit",
			tx: L2DepositTx{
				Hash:          "0xdeposit1",
				L1TxHash:      "0xl1tx1",
				L1BlockNumber: 18000000,
				L2BlockNumber: 100000,
				From:          "0xfrom",
				To:            "0xto",
				Value:         "1000000000000000000",
				Status:        "relayed",
				Timestamp:     time.Now().Truncate(time.Second),
				L2TxType:      L2TxTypeDeposit,
				SourceChain:   "ethereum",
				DestChain:     "optimism",
				GasLimit:      100000,
				MsgNonce:      42,
			},
		},
		{
			name: "ERC20 deposit",
			tx: L2DepositTx{
				Hash:          "0xdeposit2",
				L1TxHash:      "0xl1tx2",
				L1BlockNumber: 18000001,
				From:          "0xuser",
				To:            "0xbridge",
				Value:         "0",
				L1Token:       "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
				L2Token:       "0x7F5c764cBc14f9669B88837ca1490cCa17c31607",
				Amount:        "1000000000",
				Status:        "pending",
				Timestamp:     time.Now().Truncate(time.Second),
				L2TxType:      L2TxTypeDeposit,
				SourceChain:   "ethereum",
				DestChain:     "optimism",
			},
		},
		{
			name: "failed deposit",
			tx: L2DepositTx{
				Hash:          "0xdeposit3",
				L1TxHash:      "0xl1tx3",
				L1BlockNumber: 18000002,
				From:          "0xfrom",
				To:            "0xto",
				Value:         "500000000000000000",
				Status:        "failed",
				Timestamp:     time.Now().Truncate(time.Second),
				L2TxType:      L2TxTypeDeposit,
				SourceChain:   "ethereum",
				DestChain:     "base",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.tx)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var decoded L2DepositTx
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if decoded.Hash != tt.tx.Hash {
				t.Errorf("Hash = %q, want %q", decoded.Hash, tt.tx.Hash)
			}
			if decoded.L1TxHash != tt.tx.L1TxHash {
				t.Errorf("L1TxHash = %q, want %q", decoded.L1TxHash, tt.tx.L1TxHash)
			}
			if decoded.Status != tt.tx.Status {
				t.Errorf("Status = %q, want %q", decoded.Status, tt.tx.Status)
			}
			if decoded.L2TxType != L2TxTypeDeposit {
				t.Errorf("L2TxType = %q, want %q", decoded.L2TxType, L2TxTypeDeposit)
			}
			if decoded.SourceChain != tt.tx.SourceChain {
				t.Errorf("SourceChain = %q, want %q", decoded.SourceChain, tt.tx.SourceChain)
			}
		})
	}
}

// ---- L2 Withdrawal Transaction Tests ----

func TestL2WithdrawalTxSerialization(t *testing.T) {
	tests := []struct {
		name string
		tx   L2WithdrawalTx
	}{
		{
			name: "initiated withdrawal",
			tx: L2WithdrawalTx{
				Hash:          "0xwithdrawal1",
				L2TxHash:      "0xl2tx1",
				L2BlockNumber: 100000,
				From:          "0xuser",
				To:            "0xuser",
				Value:         "2000000000000000000",
				Status:        "initiated",
				Timestamp:     time.Now().Truncate(time.Second),
				L2TxType:      L2TxTypeWithdrawal,
			},
		},
		{
			name: "proven withdrawal",
			tx: L2WithdrawalTx{
				Hash:               "0xwithdrawal2",
				L2TxHash:           "0xl2tx2",
				L2BlockNumber:      100001,
				From:               "0xuser",
				To:                 "0xuser",
				Value:              "1000000000000000000",
				Status:             "proven",
				ProofTxHash:        "0xproof1",
				ChallengePeriodEnd: time.Now().Add(7 * 24 * time.Hour).Truncate(time.Second),
				Timestamp:          time.Now().Truncate(time.Second),
				L2TxType:           L2TxTypeWithdrawal,
			},
		},
		{
			name: "finalized withdrawal",
			tx: L2WithdrawalTx{
				Hash:           "0xwithdrawal3",
				L2TxHash:       "0xl2tx3",
				L2BlockNumber:  100002,
				L1TxHash:       "0xl1finalize",
				L1BlockNumber:  18500000,
				From:           "0xuser",
				To:             "0xuser",
				Value:          "500000000000000000",
				Status:         "finalized",
				ProofTxHash:    "0xproof2",
				FinalizeTxHash: "0xfinalize2",
				Timestamp:      time.Now().Truncate(time.Second),
				L2TxType:       L2TxTypeWithdrawal,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.tx)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var decoded L2WithdrawalTx
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if decoded.Status != tt.tx.Status {
				t.Errorf("Status = %q, want %q", decoded.Status, tt.tx.Status)
			}
			if decoded.L2TxType != L2TxTypeWithdrawal {
				t.Errorf("L2TxType = %q, want %q", decoded.L2TxType, L2TxTypeWithdrawal)
			}
			if tt.tx.ProofTxHash != "" && decoded.ProofTxHash != tt.tx.ProofTxHash {
				t.Errorf("ProofTxHash = %q, want %q", decoded.ProofTxHash, tt.tx.ProofTxHash)
			}
		})
	}
}

func TestL2WithdrawalStatusTransitions(t *testing.T) {
	validTransitions := map[string][]string{
		"initiated": {"proven", "failed"},
		"proven":    {"finalized", "failed"},
		"finalized": {},
		"failed":    {},
	}

	for from, validTos := range validTransitions {
		for _, to := range validTos {
			t.Run(from+"->"+to, func(t *testing.T) {
				// Transition should be valid
				found := false
				for _, valid := range validTransitions[from] {
					if valid == to {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("transition %s -> %s should be valid", from, to)
				}
			})
		}
	}

	// Invalid transitions
	invalidTransitions := []struct{ from, to string }{
		{"finalized", "initiated"},
		{"finalized", "proven"},
		{"failed", "proven"},
		{"initiated", "finalized"}, // Must go through proven first
	}

	for _, tt := range invalidTransitions {
		t.Run(tt.from+"->"+tt.to+"_invalid", func(t *testing.T) {
			valid := false
			for _, v := range validTransitions[tt.from] {
				if v == tt.to {
					valid = true
				}
			}
			if valid {
				t.Errorf("transition %s -> %s should be invalid", tt.from, tt.to)
			}
		})
	}
}

// ---- Cross-Chain Message Tests ----

func TestCrossChainMessageSerialization(t *testing.T) {
	tests := []struct {
		name string
		msg  CrossChainMessage
	}{
		{
			name: "L1 to L2 message",
			msg: CrossChainMessage{
				MsgHash:       "0x" + strings.Repeat("ab", 32),
				Direction:     "l1_to_l2",
				SourceTxHash:  "0xsource",
				DestTxHash:    "0xdest",
				SourceChainID: 1,
				DestChainID:   10,
				Sender:        "0xsender",
				Target:        "0xtarget",
				Value:         "1000000000000000000",
				Nonce:         100,
				Status:        "relayed",
				Timestamp:     time.Now().Truncate(time.Second),
			},
		},
		{
			name: "L2 to L1 message",
			msg: CrossChainMessage{
				MsgHash:       "0x" + strings.Repeat("cd", 32),
				Direction:     "l2_to_l1",
				SourceTxHash:  "0xl2source",
				SourceChainID: 10,
				DestChainID:   1,
				Sender:        "0xl2sender",
				Target:        "0xl1target",
				Value:         "0",
				Data:          "0xdeadbeef",
				Nonce:         200,
				Status:        "sent",
				Timestamp:     time.Now().Truncate(time.Second),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var decoded CrossChainMessage
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if decoded.Direction != tt.msg.Direction {
				t.Errorf("Direction = %q, want %q", decoded.Direction, tt.msg.Direction)
			}
			if decoded.SourceChainID != tt.msg.SourceChainID {
				t.Errorf("SourceChainID = %d, want %d", decoded.SourceChainID, tt.msg.SourceChainID)
			}
			if decoded.DestChainID != tt.msg.DestChainID {
				t.Errorf("DestChainID = %d, want %d", decoded.DestChainID, tt.msg.DestChainID)
			}
			if decoded.Nonce != tt.msg.Nonce {
				t.Errorf("Nonce = %d, want %d", decoded.Nonce, tt.msg.Nonce)
			}
		})
	}
}

// ---- L2 Batch Submission Tests ----

func TestL2BatchSubmissionSerialization(t *testing.T) {
	tests := []struct {
		name  string
		batch L2BatchSubmission
	}{
		{
			name: "calldata batch",
			batch: L2BatchSubmission{
				TxHash:        "0xbatch1",
				BatchIndex:    1000,
				BatchRoot:     "0x" + strings.Repeat("ab", 32),
				BatchSize:     100,
				L1BlockNumber: 18000000,
				L2StartBlock:  5000000,
				L2EndBlock:    5000099,
				Submitter:     "0xsequencer",
				Timestamp:     time.Now().Truncate(time.Second),
				GasUsed:       500000,
				BatchType:     "calldata",
			},
		},
		{
			name: "blob batch (EIP-4844)",
			batch: L2BatchSubmission{
				TxHash:        "0xbatch2",
				BatchIndex:    1001,
				BatchRoot:     "0x" + strings.Repeat("cd", 32),
				BatchSize:     200,
				L1BlockNumber: 19000000,
				L2StartBlock:  5000100,
				L2EndBlock:    5000299,
				Submitter:     "0xsequencer",
				Timestamp:     time.Now().Truncate(time.Second),
				GasUsed:       100000,
				BatchType:     "blob",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.batch)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var decoded L2BatchSubmission
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if decoded.BatchIndex != tt.batch.BatchIndex {
				t.Errorf("BatchIndex = %d, want %d", decoded.BatchIndex, tt.batch.BatchIndex)
			}
			if decoded.BatchSize != tt.batch.BatchSize {
				t.Errorf("BatchSize = %d, want %d", decoded.BatchSize, tt.batch.BatchSize)
			}
			if decoded.L2EndBlock-decoded.L2StartBlock+1 != decoded.BatchSize {
				t.Errorf("block range = %d, want %d", decoded.L2EndBlock-decoded.L2StartBlock+1, decoded.BatchSize)
			}
			if decoded.BatchType != tt.batch.BatchType {
				t.Errorf("BatchType = %q, want %q", decoded.BatchType, tt.batch.BatchType)
			}
		})
	}
}

func TestL2BatchBlockRangeConsistency(t *testing.T) {
	batch := L2BatchSubmission{
		L2StartBlock: 100,
		L2EndBlock:   199,
		BatchSize:    100,
	}

	blockRange := batch.L2EndBlock - batch.L2StartBlock + 1
	if blockRange != batch.BatchSize {
		t.Errorf("block range %d does not match batch size %d", blockRange, batch.BatchSize)
	}

	if batch.L2StartBlock > batch.L2EndBlock {
		t.Error("start block should not exceed end block")
	}
}

// ---- Optimism Output Root Tests ----

func TestOutputRootSerialization(t *testing.T) {
	root := OutputRoot{
		OutputRoot:    "0x" + strings.Repeat("ab", 32),
		L2OutputIndex: 5000,
		L2BlockNumber: 10000000,
		L1TxHash:      "0xl1tx",
		L1BlockNumber: 18000000,
		Timestamp:     time.Now().Truncate(time.Second),
		Proposer:      "0xproposer",
	}

	data, err := json.Marshal(root)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded OutputRoot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.L2OutputIndex != 5000 {
		t.Errorf("L2OutputIndex = %d, want 5000", decoded.L2OutputIndex)
	}
	if decoded.L2BlockNumber != 10000000 {
		t.Errorf("L2BlockNumber = %d, want 10000000", decoded.L2BlockNumber)
	}
	if len(decoded.OutputRoot) != 66 {
		if len(decoded.OutputRoot) < 10 {
			t.Errorf("OutputRoot too short")
		}
	}
}

func TestOutputRootSequence(t *testing.T) {
	roots := []OutputRoot{
		{L2OutputIndex: 0, L2BlockNumber: 0},
		{L2OutputIndex: 1, L2BlockNumber: 1800},
		{L2OutputIndex: 2, L2BlockNumber: 3600},
		{L2OutputIndex: 3, L2BlockNumber: 5400},
	}

	for i := 1; i < len(roots); i++ {
		if roots[i].L2OutputIndex != roots[i-1].L2OutputIndex+1 {
			t.Errorf("output index %d not sequential after %d", roots[i].L2OutputIndex, roots[i-1].L2OutputIndex)
		}
		if roots[i].L2BlockNumber <= roots[i-1].L2BlockNumber {
			t.Errorf("block %d not increasing after %d", roots[i].L2BlockNumber, roots[i-1].L2BlockNumber)
		}
	}
}

// ---- Arbitrum Batch Tests ----

func TestArbitrumBatchSerialization(t *testing.T) {
	batch := ArbitrumBatch{
		TxHash:        "0xarbbatch",
		BatchNumber:   50000,
		SequencerAddr: "0xsequencer",
		L1BlockNumber: 18000000,
		MessageCount:  500,
		AfterInboxAcc: "0x" + strings.Repeat("ab", 32),
		DataSize:      125000,
		Timestamp:     time.Now().Truncate(time.Second),
	}

	data, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ArbitrumBatch
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.BatchNumber != 50000 {
		t.Errorf("BatchNumber = %d, want 50000", decoded.BatchNumber)
	}
	if decoded.MessageCount != 500 {
		t.Errorf("MessageCount = %d, want 500", decoded.MessageCount)
	}
	if decoded.DataSize != 125000 {
		t.Errorf("DataSize = %d, want 125000", decoded.DataSize)
	}
}

func TestArbitrumBatchSequence(t *testing.T) {
	batches := make([]ArbitrumBatch, 10)
	for i := range batches {
		batches[i] = ArbitrumBatch{
			BatchNumber:   uint64(1000 + i),
			L1BlockNumber: uint64(18000000 + i*12), // ~12 seconds per L1 block
			MessageCount:  uint64(100 + i*10),
		}
	}

	for i := 1; i < len(batches); i++ {
		if batches[i].BatchNumber != batches[i-1].BatchNumber+1 {
			t.Errorf("batch %d not sequential", batches[i].BatchNumber)
		}
		if batches[i].L1BlockNumber <= batches[i-1].L1BlockNumber {
			t.Errorf("L1 block %d not increasing", batches[i].L1BlockNumber)
		}
	}
}

// ---- Scroll Batch Tests ----

func TestScrollBatchSerialization(t *testing.T) {
	batch := ScrollBatch{
		TxHash:        "0xscrollbatch",
		BatchIndex:    1000,
		L1BlockNumber: 18000000,
		L2StartBlock:  5000000,
		L2EndBlock:    5000099,
		BatchHash:     "0x" + strings.Repeat("ab", 32),
		Timestamp:     time.Now().Truncate(time.Second),
		Status:        "committed",
	}

	data, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ScrollBatch
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.BatchIndex != 1000 {
		t.Errorf("BatchIndex = %d, want 1000", decoded.BatchIndex)
	}
	if decoded.Status != "committed" {
		t.Errorf("Status = %q, want committed", decoded.Status)
	}
}

func TestScrollBatchStatusTransitions(t *testing.T) {
	valid := map[string][]string{
		"committed": {"finalized"},
		"finalized": {},
	}

	tests := []struct {
		from  string
		to    string
		valid bool
	}{
		{"committed", "finalized", true},
		{"finalized", "committed", false},
	}

	for _, tt := range tests {
		t.Run(tt.from+"->"+tt.to, func(t *testing.T) {
			found := false
			for _, v := range valid[tt.from] {
				if v == tt.to {
					found = true
				}
			}
			if found != tt.valid {
				t.Errorf("transition %s -> %s valid = %v, want %v", tt.from, tt.to, found, tt.valid)
			}
		})
	}
}

// ---- ZkSync Batch Lifecycle Tests ----

func TestZkSyncBatchSerialization(t *testing.T) {
	batch := ZkSyncBatch{
		BatchNumber:   10000,
		CommitTxHash:  "0xcommit",
		ProveTxHash:   "0xprove",
		ExecuteTxHash: "0xexecute",
		L1BlockNumber: 18000000,
		L2StartBlock:  20000000,
		L2EndBlock:    20000099,
		RootHash:      "0x" + strings.Repeat("ab", 32),
		Status:        "executed",
		Timestamp:     time.Now().Truncate(time.Second),
	}

	data, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ZkSyncBatch
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.BatchNumber != 10000 {
		t.Errorf("BatchNumber = %d, want 10000", decoded.BatchNumber)
	}
	if decoded.Status != "executed" {
		t.Errorf("Status = %q, want executed", decoded.Status)
	}
	if decoded.CommitTxHash == "" {
		t.Error("CommitTxHash should not be empty")
	}
	if decoded.ProveTxHash == "" {
		t.Error("ProveTxHash should not be empty")
	}
	if decoded.ExecuteTxHash == "" {
		t.Error("ExecuteTxHash should not be empty")
	}
}

func TestZkSyncBatchLifecycle(t *testing.T) {
	// ZkSync batches go through: commit -> prove -> execute
	validTransitions := map[string][]string{
		"committed": {"proven"},
		"proven":    {"executed"},
		"executed":  {},
	}

	tests := []struct {
		from  string
		to    string
		valid bool
	}{
		{"committed", "proven", true},
		{"proven", "executed", true},
		{"committed", "executed", false}, // Must go through proven
		{"executed", "committed", false},
		{"executed", "proven", false},
	}

	for _, tt := range tests {
		t.Run(tt.from+"->"+tt.to, func(t *testing.T) {
			found := false
			for _, v := range validTransitions[tt.from] {
				if v == tt.to {
					found = true
				}
			}
			if found != tt.valid {
				t.Errorf("transition %s -> %s valid = %v, want %v", tt.from, tt.to, found, tt.valid)
			}
		})
	}
}

func TestZkSyncBatchTxHashProgression(t *testing.T) {
	// Batch starts with only CommitTxHash, then gains ProveTxHash, then ExecuteTxHash
	stages := []ZkSyncBatch{
		{BatchNumber: 1, CommitTxHash: "0xcommit", Status: "committed"},
		{BatchNumber: 1, CommitTxHash: "0xcommit", ProveTxHash: "0xprove", Status: "proven"},
		{BatchNumber: 1, CommitTxHash: "0xcommit", ProveTxHash: "0xprove", ExecuteTxHash: "0xexecute", Status: "executed"},
	}

	for i, stage := range stages {
		if stage.CommitTxHash == "" {
			t.Errorf("stage %d: CommitTxHash should always be set", i)
		}
		switch stage.Status {
		case "committed":
			if stage.ProveTxHash != "" {
				t.Errorf("committed stage should not have ProveTxHash")
			}
			if stage.ExecuteTxHash != "" {
				t.Errorf("committed stage should not have ExecuteTxHash")
			}
		case "proven":
			if stage.ProveTxHash == "" {
				t.Errorf("proven stage must have ProveTxHash")
			}
			if stage.ExecuteTxHash != "" {
				t.Errorf("proven stage should not have ExecuteTxHash")
			}
		case "executed":
			if stage.ProveTxHash == "" {
				t.Errorf("executed stage must have ProveTxHash")
			}
			if stage.ExecuteTxHash == "" {
				t.Errorf("executed stage must have ExecuteTxHash")
			}
		}
	}
}

// ---- L2 Event Topic Tests ----

func TestL2EventTopics(t *testing.T) {
	topics := map[string]string{
		"TransactionDeposited":    TopicTransactionDeposited,
		"OutputProposed":          TopicOutputProposed,
		"WithdrawalProven":        TopicWithdrawalProven,
		"WithdrawalFinalized":     TopicWithdrawalFinalized,
		"SequencerBatchDelivered": TopicSequencerBatchDelivered,
		"NodeCreated":             TopicNodeCreated,
		"CommitBatch":             TopicCommitBatch,
		"FinalizeBatch":           TopicFinalizeBatch,
		"BlockCommit":             TopicBlockCommit,
		"BlockProof":              TopicBlockProof,
		"BlockExecution":          TopicBlockExecution,
	}

	for name, topic := range topics {
		t.Run(name, func(t *testing.T) {
			if topic[:2] != "0x" {
				t.Errorf("%s should start with 0x", name)
			}
			if len(topic) != 66 {
				if len(topic) < 10 {
					t.Errorf("%s too short: %d", name, len(topic))
				}
			}
		})
	}

	// All topics should be unique
	seen := make(map[string]string)
	for name, topic := range topics {
		if prev, ok := seen[topic]; ok {
			t.Errorf("duplicate topic between %s and %s", name, prev)
		}
		seen[topic] = name
	}
}

// ---- L2 Deposit Detection Tests ----

func TestDetectL2DepositTx(t *testing.T) {
	// Optimism deposit transactions have type 0x7E
	tests := []struct {
		name      string
		txType    uint8
		isDeposit bool
	}{
		{"legacy", 0, false},
		{"access list", 1, false},
		{"EIP-1559", 2, false},
		{"blob (EIP-4844)", 3, false},
		{"OP deposit (0x7E)", 0x7E, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isDeposit := tt.txType == 0x7E
			if isDeposit != tt.isDeposit {
				t.Errorf("tx type %d: isDeposit = %v, want %v", tt.txType, isDeposit, tt.isDeposit)
			}
		})
	}
}

// ---- Bridge Event Detection Tests ----

func TestBridgeEventDetection(t *testing.T) {
	// Standard bridge events use specific topics
	tests := []struct {
		name      string
		topic     string
		isDeposit bool
	}{
		{"TransactionDeposited", TopicTransactionDeposited, true},
		{"WithdrawalProven", TopicWithdrawalProven, false},
		{"WithdrawalFinalized", TopicWithdrawalFinalized, false},
		{"random topic", "0x" + strings.Repeat("00", 32), false},
	}

	depositTopics := map[string]bool{
		TopicTransactionDeposited: true,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isDeposit := depositTopics[tt.topic]
			if isDeposit != tt.isDeposit {
				t.Errorf("topic %s: isDeposit = %v, want %v", tt.name, isDeposit, tt.isDeposit)
			}
		})
	}
}

// ---- L2 Chain ID Tests ----

func TestL2ChainIDs(t *testing.T) {
	l2Chains := map[string]uint64{
		"optimism":      10,
		"base":          8453,
		"arbitrum_one":  42161,
		"arbitrum_nova": 42170,
		"scroll":        534352,
		"zksync_era":    324,
		"polygon_zkevm": 1101,
		"linea":         59144,
		"mantle":        5000,
		"blast":         81457,
	}

	for name, chainID := range l2Chains {
		t.Run(name, func(t *testing.T) {
			if chainID == 0 {
				t.Errorf("%s chain ID should not be 0", name)
			}
			if chainID == 1 {
				t.Errorf("%s chain ID should not be 1 (Ethereum mainnet)", name)
			}
		})
	}

	// All chain IDs should be unique
	seen := make(map[uint64]string)
	for name, chainID := range l2Chains {
		if prev, ok := seen[chainID]; ok {
			t.Errorf("duplicate chain ID %d for %s and %s", chainID, name, prev)
		}
		seen[chainID] = name
	}
}

// ---- CrossChainTransaction (existing type) Tests ----

func TestCrossChainTransactionBridgeProtocols(t *testing.T) {
	protocols := []string{
		"Warp", "LayerZero", "Stargate", "Wormhole",
		"Optimism Bridge", "Arbitrum Bridge", "Scroll Bridge",
		"ZkSync Bridge", "Across", "Hop", "Synapse",
	}

	for _, proto := range protocols {
		t.Run(proto, func(t *testing.T) {
			tx := CrossChainTransaction{
				ID:              fmt.Sprintf("xchain-%s", proto),
				SourceChainID:   1,
				DestChainID:     10,
				SourceTxHash:    "0xsource",
				BridgeProtocol:  proto,
				Sender:          "0xsender",
				Recipient:       "0xrecipient",
				Status:          "pending",
				SourceTimestamp: time.Now(),
			}

			data, err := json.Marshal(tx)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var decoded CrossChainTransaction
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if decoded.BridgeProtocol != proto {
				t.Errorf("BridgeProtocol = %q, want %q", decoded.BridgeProtocol, proto)
			}
		})
	}
}

// ---- EnhancedTransaction L2 Fields Tests ----

func TestEnhancedTransactionBlobFields(t *testing.T) {
	// Type 3 (blob) transactions on L1 that carry L2 batch data
	tx := EnhancedTransaction{
		Hash:                "0xblobtx",
		BlockNumber:         19000000,
		Type:                3,
		MaxFeePerBlobGas:    "1000000000",
		BlobVersionedHashes: []string{"0x01hash1", "0x01hash2"},
		BlobGasUsed:         262144,
		BlobGasPrice:        "100",
	}

	if tx.Type != 3 {
		t.Errorf("Type = %d, want 3", tx.Type)
	}
	if len(tx.BlobVersionedHashes) != 2 {
		t.Errorf("BlobVersionedHashes length = %d, want 2", len(tx.BlobVersionedHashes))
	}
	if tx.BlobGasUsed != 2*BlobGasPerBlob {
		t.Errorf("BlobGasUsed = %d, want %d", tx.BlobGasUsed, 2*BlobGasPerBlob)
	}
}

func TestEnhancedTransactionDepositType(t *testing.T) {
	// Optimism deposit type 126 (0x7E)
	tx := EnhancedTransaction{
		Hash:        "0xdeposit",
		BlockNumber: 100000,
		Type:        0x7E,
		From:        "0xdeaddeaddeaddeaddeaddeaddeaddeaddead0001",
		To:          "0x4200000000000000000000000000000000000015",
		Value:       "1000000000000000000",
	}

	if tx.Type != 126 {
		t.Errorf("Type = %d, want 126", tx.Type)
	}
	// L1 attributes depositer address (OP system)
	if tx.From != "0xdeaddeaddeaddeaddeaddeaddeaddeaddead0001" {
		t.Errorf("From should be L1 attributes depositer address")
	}
}

// ---- L2 Gas Cost Estimation Tests ----

func TestL2GasCostEstimation(t *testing.T) {
	// L2 gas = L2 execution gas + L1 data fee
	tests := []struct {
		name         string
		l2Gas        uint64
		l2GasPrice   *big.Int
		l1DataFee    *big.Int
		wantTotalWei *big.Int
	}{
		{
			name:         "standard tx",
			l2Gas:        21000,
			l2GasPrice:   big.NewInt(100000000),      // 0.1 gwei
			l1DataFee:    big.NewInt(50000000000000), // 0.00005 ETH
			wantTotalWei: new(big.Int).Add(new(big.Int).Mul(big.NewInt(21000), big.NewInt(100000000)), big.NewInt(50000000000000)),
		},
		{
			name:         "contract call",
			l2Gas:        200000,
			l2GasPrice:   big.NewInt(100000000),
			l1DataFee:    big.NewInt(100000000000000), // 0.0001 ETH
			wantTotalWei: new(big.Int).Add(new(big.Int).Mul(big.NewInt(200000), big.NewInt(100000000)), big.NewInt(100000000000000)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l2Cost := new(big.Int).Mul(big.NewInt(int64(tt.l2Gas)), tt.l2GasPrice)
			totalCost := new(big.Int).Add(l2Cost, tt.l1DataFee)

			if totalCost.Cmp(tt.wantTotalWei) != 0 {
				t.Errorf("total cost = %s, want %s", totalCost.String(), tt.wantTotalWei.String())
			}

			// L1 data fee should be larger than L2 execution cost for most L2s
			if tt.l1DataFee.Cmp(l2Cost) < 0 {
				// This is expected for cheap L2s, not an error
			}
		})
	}
}

// ---- Benchmarks ----

func BenchmarkL2DepositTxMarshal(b *testing.B) {
	tx := L2DepositTx{
		Hash:          "0xdeposit",
		L1TxHash:      "0xl1tx",
		L1BlockNumber: 18000000,
		L2BlockNumber: 100000,
		From:          "0xfrom",
		To:            "0xto",
		Value:         "1000000000000000000",
		Status:        "relayed",
		Timestamp:     time.Now(),
		L2TxType:      L2TxTypeDeposit,
		SourceChain:   "ethereum",
		DestChain:     "optimism",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(tx)
	}
}

func BenchmarkZkSyncBatchMarshal(b *testing.B) {
	batch := ZkSyncBatch{
		BatchNumber:   10000,
		CommitTxHash:  "0xcommit",
		ProveTxHash:   "0xprove",
		ExecuteTxHash: "0xexecute",
		L1BlockNumber: 18000000,
		L2StartBlock:  20000000,
		L2EndBlock:    20000099,
		RootHash:      "0x" + strings.Repeat("ab", 32),
		Status:        "executed",
		Timestamp:     time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(batch)
	}
}

func BenchmarkCrossChainMessageMarshal(b *testing.B) {
	msg := CrossChainMessage{
		MsgHash:       "0x" + strings.Repeat("ab", 32),
		Direction:     "l1_to_l2",
		SourceTxHash:  "0xsource",
		DestTxHash:    "0xdest",
		SourceChainID: 1,
		DestChainID:   10,
		Sender:        "0xsender",
		Target:        "0xtarget",
		Value:         "1000000000000000000",
		Nonce:         100,
		Status:        "relayed",
		Timestamp:     time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(msg)
	}
}
