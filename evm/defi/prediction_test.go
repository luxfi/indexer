// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package defi

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestPredictionIndexer_AssertionMade(t *testing.T) {
	idx := NewPredictionIndexer()

	var called bool
	idx.SetCallbacks(func(e *PredictionEvent) { called = true }, nil)

	// Build data: claim_offset(32) + asserter(32) + callback(32) + escalation(32) + caller(32) + expiry(32) + currency(32) + bond(32) + identifier(32)
	// We need at least 288 bytes (9 words)
	asserter := strings.Repeat("0", 24) + "aabbccddeeff11223344556677889900aabbccdd"
	bond := strings.Repeat("0", 56) + "de0b6b3a7640000" + "0" // 1e18 padded wrong, let's just use simple
	// Simple: 9 words of 64 hex chars each = 576 hex chars
	data := "0x" +
		strings.Repeat("0", 64) + // word 0: claim offset
		strings.Repeat("0", 24) + "aabbccddeeff11223344556677889900aabbccdd" + // word 1: asserter
		strings.Repeat("0", 64) + // word 2: callback
		strings.Repeat("0", 64) + // word 3: escalation
		strings.Repeat("0", 64) + // word 4: caller
		strings.Repeat("0", 64) + // word 5: expiry
		strings.Repeat("0", 64) + // word 6: currency
		strings.Repeat("0", 60) + "03e8" + // word 7: bond = 1000
		strings.Repeat("0", 64) // word 8: identifier

	_ = asserter
	_ = bond

	log := &LogEntry{
		Address: "0xoracle",
		Topics: []string{
			PredictionAssertionMadeSig,
			"0xassertionid0000000000000000000000000000000000000000000000000000",
			"0xdomainid000000000000000000000000000000000000000000000000000000",
		},
		Data:        data,
		BlockNumber: 100,
		TxHash:      "0xtx1",
		LogIndex:    0,
		Timestamp:   time.Now(),
	}

	if err := idx.IndexLog(log); err != nil {
		t.Fatalf("IndexLog failed: %v", err)
	}

	if !called {
		t.Fatal("onAssertion callback not called")
	}

	assertion := idx.GetAssertion("0xassertionid0000000000000000000000000000000000000000000000000000")
	if assertion == nil {
		t.Fatal("assertion not found")
	}
	if assertion.Bond == nil || assertion.Bond.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("bond = %v, want 1000", assertion.Bond)
	}
}

func TestPredictionIndexer_AssertionDisputed(t *testing.T) {
	idx := NewPredictionIndexer()

	assertionID := "0xassertionid0000000000000000000000000000000000000000000000000000"

	// First make an assertion
	data := "0x" + strings.Repeat("0", 64*9)
	idx.IndexLog(&LogEntry{
		Address: "0xoracle",
		Topics: []string{
			PredictionAssertionMadeSig,
			assertionID,
			"0x0000000000000000000000000000000000000000000000000000000000000000",
		},
		Data: data, TxHash: "0xtx1", BlockNumber: 100, Timestamp: time.Now(),
	})

	// Then dispute
	if err := idx.IndexLog(&LogEntry{
		Address: "0xoracle",
		Topics: []string{
			PredictionAssertionDisputedSig,
			assertionID,
			"0x000000000000000000000000aabbccddeeff11223344556677889900aabbccdd", // caller
			"0x0000000000000000000000001111111111111111111111111111111111111111", // disputer
		},
		TxHash: "0xtx2", BlockNumber: 101, Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("IndexLog failed: %v", err)
	}

	assertion := idx.GetAssertion(assertionID)
	if !assertion.IsDisputed {
		t.Error("assertion should be disputed")
	}
	if assertion.Disputer != "0x1111111111111111111111111111111111111111" {
		t.Errorf("disputer = %s, want 0x1111...1111", assertion.Disputer)
	}
}

func TestPredictionIndexer_QuestionLifecycle(t *testing.T) {
	idx := NewPredictionIndexer()
	now := time.Now()

	questionID := "0xquestionid000000000000000000000000000000000000000000000000000"

	// Initialize question - data: ancillaryData offset, rewardToken, reward, bond
	rewardToken := strings.Repeat("0", 24) + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	data := "0x" +
		strings.Repeat("0", 64) + // word 0: ancillary data offset
		rewardToken + // word 1: rewardToken
		strings.Repeat("0", 60) + "03e8" + // word 2: reward = 1000
		strings.Repeat("0", 60) + "07d0" // word 3: bond = 2000

	if err := idx.IndexLog(&LogEntry{
		Address: "0xresolver",
		Topics: []string{
			PredictionQuestionInitSig,
			questionID,
			"0x0000000000000000000000000000000000000000000000000000000000000001", // timestamp
			"0x000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", // creator
		},
		Data: data, TxHash: "0xtx1", BlockNumber: 100, Timestamp: now,
	}); err != nil {
		t.Fatalf("QuestionInitialized failed: %v", err)
	}

	q := idx.GetQuestion(questionID)
	if q == nil {
		t.Fatal("question not found")
	}
	if q.Bond.Cmp(big.NewInt(2000)) != 0 {
		t.Errorf("bond = %v, want 2000", q.Bond)
	}
	if q.Reward.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("reward = %v, want 1000", q.Reward)
	}

	// Flag
	idx.IndexLog(&LogEntry{
		Address: "0xresolver",
		Topics:  []string{PredictionQuestionFlaggedSig, questionID},
		TxHash:  "0xtx2", BlockNumber: 102, Timestamp: now.Add(time.Minute),
	})

	q = idx.GetQuestion(questionID)
	if !q.IsFlagged {
		t.Error("question should be flagged")
	}

	// Reset
	idx.IndexLog(&LogEntry{
		Address: "0xresolver",
		Topics:  []string{PredictionQuestionResetSig, questionID},
		TxHash:  "0xtx3", BlockNumber: 103, Timestamp: now.Add(2 * time.Minute),
	})

	q = idx.GetQuestion(questionID)
	if q.IsFlagged {
		t.Error("question should not be flagged after reset")
	}

	// Resolve - settledPrice = 1e18 (YES)
	priceHex := "0x" + strings.Repeat("0", 64)
	copy([]byte(priceHex)[2:], "0000000000000000000000000000000000000000000000000de0b6b3a7640000")
	idx.IndexLog(&LogEntry{
		Address: "0xresolver",
		Topics: []string{
			PredictionQuestionResolvedSig,
			questionID,
			"0x0000000000000000000000000000000000000000000000000de0b6b3a7640000", // 1e18
		},
		Data:   "0x" + strings.Repeat("0", 64), // payouts offset
		TxHash: "0xtx4", BlockNumber: 104, Timestamp: now.Add(3 * time.Minute),
	})

	q = idx.GetQuestion(questionID)
	if !q.IsResolved {
		t.Error("question should be resolved")
	}

	stats := idx.GetStats()
	if stats.TotalQuestions != 1 {
		t.Errorf("TotalQuestions = %d, want 1", stats.TotalQuestions)
	}
	if stats.ResolvedQuestions != 1 {
		t.Errorf("ResolvedQuestions = %d, want 1", stats.ResolvedQuestions)
	}
}

func TestPredictionIndexer_AssertionSettled(t *testing.T) {
	idx := NewPredictionIndexer()

	assertionID := "0xassertionid0000000000000000000000000000000000000000000000000000"

	// Create assertion
	idx.IndexLog(&LogEntry{
		Address: "0xoracle",
		Topics: []string{
			PredictionAssertionMadeSig,
			assertionID,
			"0x0000000000000000000000000000000000000000000000000000000000000000",
		},
		Data: "0x" + strings.Repeat("0", 64*9), TxHash: "0xtx1", BlockNumber: 100, Timestamp: time.Now(),
	})

	// Settle with resolution=true
	// Data: disputed(bool) + resolution(bool) + caller(address)
	// resolution = true => last byte of word 1 = 0x01
	settleData := "0x" +
		strings.Repeat("0", 64) + // disputed = false
		strings.Repeat("0", 62) + "01" + // resolution = true
		strings.Repeat("0", 24) + "1111111111111111111111111111111111111111" // caller

	if err := idx.IndexLog(&LogEntry{
		Address: "0xoracle",
		Topics: []string{
			PredictionAssertionSettledSig,
			assertionID,
			"0x000000000000000000000000aabbccddeeff11223344556677889900aabbccdd", // bondRecipient
		},
		Data: settleData, TxHash: "0xtx2", BlockNumber: 101, Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("IndexLog failed: %v", err)
	}

	assertion := idx.GetAssertion(assertionID)
	if !assertion.IsSettled {
		t.Error("assertion should be settled")
	}
	if !assertion.Resolution {
		t.Error("resolution should be true")
	}
}

func TestPredictionIndexer_InvalidLogs(t *testing.T) {
	idx := NewPredictionIndexer()

	// No topics
	if err := idx.IndexLog(&LogEntry{}); err != nil {
		t.Fatalf("empty log should not error: %v", err)
	}

	// Insufficient topics for dispute
	err := idx.IndexLog(&LogEntry{
		Topics: []string{PredictionAssertionDisputedSig}, // needs 4
	})
	if err == nil {
		t.Error("should error on insufficient topics")
	}

	// Bad hex data for settled
	err = idx.IndexLog(&LogEntry{
		Topics: []string{
			PredictionAssertionSettledSig,
			"0x0000000000000000000000000000000000000000000000000000000000000001",
			"0x0000000000000000000000000000000000000000000000000000000000000002",
		},
		Data: "0xZZZZ", // invalid hex
	})
	if err == nil {
		t.Error("should error on bad hex data")
	}

	_ = hex.EncodeToString
}
