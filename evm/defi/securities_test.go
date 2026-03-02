// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package defi

import (
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestSecuritiesIndexer_WhitelistAddRemove(t *testing.T) {
	idx := NewSecuritiesIndexer()
	now := time.Now()

	account := "0x0000000000000000000000001111111111111111111111111111111111111111"

	// Add to whitelist
	idx.IndexLog(&LogEntry{
		Address: "0xregistry",
		Topics:  []string{SecWhitelistAddedSig, account},
		TxHash:  "0xtx1", BlockNumber: 100, Timestamp: now,
	})

	cs := idx.GetCompliance("0x1111111111111111111111111111111111111111")
	if cs == nil {
		t.Fatal("compliance not found")
	}
	if !cs.IsWhitelisted {
		t.Error("should be whitelisted")
	}

	// Remove from whitelist
	idx.IndexLog(&LogEntry{
		Address: "0xregistry",
		Topics:  []string{SecWhitelistRemovedSig, account},
		TxHash:  "0xtx2", BlockNumber: 101, Timestamp: now.Add(time.Minute),
	})

	cs = idx.GetCompliance("0x1111111111111111111111111111111111111111")
	if cs.IsWhitelisted {
		t.Error("should not be whitelisted after removal")
	}
}

func TestSecuritiesIndexer_AccreditationSet(t *testing.T) {
	idx := NewSecuritiesIndexer()

	account := "0x0000000000000000000000001111111111111111111111111111111111111111"
	// status = 2 (accredited)
	data := "0x0000000000000000000000000000000000000000000000000000000000000002"

	if err := idx.IndexLog(&LogEntry{
		Address: "0xregistry",
		Topics:  []string{SecAccreditationSetSig, account},
		Data:    data,
		TxHash:  "0xtx1", BlockNumber: 100, Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("IndexLog failed: %v", err)
	}

	cs := idx.GetCompliance("0x1111111111111111111111111111111111111111")
	if cs.Accreditation != 2 {
		t.Errorf("accreditation = %d, want 2", cs.Accreditation)
	}
}

func TestSecuritiesIndexer_DividendLifecycle(t *testing.T) {
	idx := NewSecuritiesIndexer()
	now := time.Now()

	// Create dividend round
	// Data: paymentToken(address) + totalAmount(uint256) + snapshotBlock(uint256)
	data := "0x" +
		strings.Repeat("0", 24) + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + // paymentToken
		strings.Repeat("0", 60) + "03e8" + // totalAmount = 1000
		strings.Repeat("0", 60) + "0064" // snapshotBlock = 100

	if err := idx.IndexLog(&LogEntry{
		Address: "0xdividend",
		Topics: []string{
			SecDividendCreatedSig,
			"0x0000000000000000000000000000000000000000000000000000000000000001", // roundId = 1
		},
		Data: data, TxHash: "0xtx1", BlockNumber: 100, Timestamp: now,
	}); err != nil {
		t.Fatalf("DividendCreated failed: %v", err)
	}

	div := idx.GetDividend(1)
	if div == nil {
		t.Fatal("dividend not found")
	}
	if div.TotalAmount.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("totalAmount = %v, want 1000", div.TotalAmount)
	}
	if div.SnapshotBlock != 100 {
		t.Errorf("snapshotBlock = %d, want 100", div.SnapshotBlock)
	}

	// Claim dividend
	claimData := "0x" + strings.Repeat("0", 60) + "0064" // amount = 100
	idx.IndexLog(&LogEntry{
		Address: "0xdividend",
		Topics: []string{
			SecDividendClaimedSig,
			"0x0000000000000000000000000000000000000000000000000000000000000001", // roundId
			"0x0000000000000000000000001111111111111111111111111111111111111111", // account
		},
		Data: claimData, TxHash: "0xtx2", BlockNumber: 101, Timestamp: now.Add(time.Minute),
	})

	div = idx.GetDividend(1)
	if div.ClaimedAmount.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("claimedAmount = %v, want 100", div.ClaimedAmount)
	}
	if div.Claimants != 1 {
		t.Errorf("claimants = %d, want 1", div.Claimants)
	}

	// Reclaim
	idx.IndexLog(&LogEntry{
		Address: "0xdividend",
		Topics: []string{
			SecDividendReclaimedSig,
			"0x0000000000000000000000000000000000000000000000000000000000000001",
		},
		Data:   "0x" + strings.Repeat("0", 60) + "0384", // 900 unclaimed
		TxHash: "0xtx3", BlockNumber: 102, Timestamp: now.Add(2 * time.Minute),
	})

	div = idx.GetDividend(1)
	if !div.IsReclaimed {
		t.Error("dividend should be reclaimed")
	}
}

func TestSecuritiesIndexer_ForcedTransfer(t *testing.T) {
	idx := NewSecuritiesIndexer()

	var corporateCalled bool
	idx.SetCallbacks(nil, nil, func(e *SecuritiesEvent) { corporateCalled = true })

	// ForcedTransfer data: amount(32) + reason offset(32) + reason length(32) + reason data
	data := "0x" +
		strings.Repeat("0", 60) + "03e8" + // amount = 1000
		strings.Repeat("0", 64) // reason offset (we keep it minimal)

	if err := idx.IndexLog(&LogEntry{
		Address: "0xtoken",
		Topics: []string{
			SecForcedTransferSig,
			"0x0000000000000000000000001111111111111111111111111111111111111111", // from
			"0x0000000000000000000000002222222222222222222222222222222222222222", // to
		},
		Data: data, TxHash: "0xtx1", BlockNumber: 100, Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("IndexLog failed: %v", err)
	}

	if !corporateCalled {
		t.Fatal("onCorporate callback not called")
	}
}

func TestSecuritiesIndexer_TokensFrozen(t *testing.T) {
	idx := NewSecuritiesIndexer()

	data := "0x" + strings.Repeat("0", 60) + "03e8" // amount = 1000
	account := "0x0000000000000000000000001111111111111111111111111111111111111111"

	if err := idx.IndexLog(&LogEntry{
		Address: "0xtoken",
		Topics:  []string{SecTokensFrozenSig, account},
		Data:    data,
		TxHash:  "0xtx1", BlockNumber: 100, Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("IndexLog failed: %v", err)
	}

	cs := idx.GetCompliance("0x1111111111111111111111111111111111111111")
	if !cs.IsFrozen {
		t.Error("should be frozen")
	}
	if cs.FrozenAmount.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("frozenAmount = %v, want 1000", cs.FrozenAmount)
	}

	stats := idx.GetStats()
	if stats.FrozenAccounts != 1 {
		t.Errorf("FrozenAccounts = %d, want 1", stats.FrozenAccounts)
	}
}

func TestSecuritiesIndexer_TransferByPartition(t *testing.T) {
	idx := NewSecuritiesIndexer()

	data := "0x" + strings.Repeat("0", 60) + "03e8" // value = 1000
	partition := "0xpartition00000000000000000000000000000000000000000000000000000"

	if err := idx.IndexLog(&LogEntry{
		Address: "0xtoken",
		Topics: []string{
			SecTransferByPartitionSig,
			partition,
			"0x0000000000000000000000001111111111111111111111111111111111111111", // from
			"0x0000000000000000000000002222222222222222222222222222222222222222", // to
		},
		Data: data, TxHash: "0xtx1", BlockNumber: 100, Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("IndexLog failed: %v", err)
	}

	stats := idx.GetStats()
	if stats.TotalEvents != 1 {
		t.Errorf("TotalEvents = %d, want 1", stats.TotalEvents)
	}
}

func TestSecuritiesIndexer_InvalidLogs(t *testing.T) {
	idx := NewSecuritiesIndexer()

	// No topics
	if err := idx.IndexLog(&LogEntry{}); err != nil {
		t.Fatalf("empty log should not error: %v", err)
	}

	// Insufficient topics for whitelist
	err := idx.IndexLog(&LogEntry{
		Topics: []string{SecWhitelistAddedSig}, // needs 2
	})
	if err == nil {
		t.Error("should error on insufficient topics")
	}

	// Bad data for lockup
	err = idx.IndexLog(&LogEntry{
		Topics: []string{
			SecLockupSetSig,
			"0x0000000000000000000000001111111111111111111111111111111111111111",
		},
		Data: "0xZZZZ",
	})
	if err == nil {
		t.Error("should error on bad hex data")
	}
}
