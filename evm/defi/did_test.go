// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package defi

import (
	"math/big"
	"testing"
	"time"
)

func TestDIDIndexer_IdentityClaimed(t *testing.T) {
	idx := NewDIDIndexer()

	var called bool
	idx.SetCallbacks(func(e *DIDEvent) { called = true }, nil, nil)

	log := &LogEntry{
		Address: "0xdidregistry",
		Topics: []string{
			DIDIdentityClaimedSig,
			"0xaabbccdd00000000000000000000000000000000000000000000000000000000", // did hash
			"0x0000000000000000000000000000000000000000000000000000000000000001", // nftId=1
			"0x000000000000000000000000aabbccddee112233445566778899001122334455", // owner
		},
		Data:        "",
		BlockNumber: 100,
		TxHash:      "0xtx1",
		LogIndex:    0,
		Timestamp:   time.Now(),
	}

	if err := idx.IndexLog(log); err != nil {
		t.Fatalf("IndexLog failed: %v", err)
	}

	if !called {
		t.Fatal("onClaim callback not called")
	}

	identity := idx.GetIdentity("0xaabbccdd00000000000000000000000000000000000000000000000000000000")
	if identity == nil {
		t.Fatal("identity not found")
	}
	if !identity.IsActive {
		t.Error("identity should be active")
	}
	if identity.NFTID.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("nftId = %v, want 1", identity.NFTID)
	}

	stats := idx.GetStats()
	if stats.TotalIdentities != 1 {
		t.Errorf("TotalIdentities = %d, want 1", stats.TotalIdentities)
	}
	if stats.ActiveIdentities != 1 {
		t.Errorf("ActiveIdentities = %d, want 1", stats.ActiveIdentities)
	}
}

func TestDIDIndexer_IdentityUnclaimed(t *testing.T) {
	idx := NewDIDIndexer()
	now := time.Now()

	didHash := "0xaabbccdd00000000000000000000000000000000000000000000000000000000"

	// First claim
	idx.IndexLog(&LogEntry{
		Address: "0xreg",
		Topics: []string{
			DIDIdentityClaimedSig,
			didHash,
			"0x0000000000000000000000000000000000000000000000000000000000000001",
			"0x0000000000000000000000001111111111111111111111111111111111111111",
		},
		TxHash: "0xtx1", BlockNumber: 100, Timestamp: now,
	})

	// Then unclaim
	if err := idx.IndexLog(&LogEntry{
		Address: "0xreg",
		Topics: []string{
			DIDIdentityUnclaimedSig,
			didHash,
			"0x0000000000000000000000000000000000000000000000000000000000000001",
		},
		TxHash: "0xtx2", BlockNumber: 101, Timestamp: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("IndexLog failed: %v", err)
	}

	identity := idx.GetIdentity(didHash)
	if identity == nil {
		t.Fatal("identity not found")
	}
	if identity.IsActive {
		t.Error("identity should be inactive after unclaim")
	}

	active := idx.GetActiveIdentities()
	if len(active) != 0 {
		t.Errorf("active identities = %d, want 0", len(active))
	}
}

func TestDIDIndexer_StakeUpdated(t *testing.T) {
	idx := NewDIDIndexer()
	now := time.Now()

	didHash := "0xaabbccdd00000000000000000000000000000000000000000000000000000000"

	// Claim first
	idx.IndexLog(&LogEntry{
		Address: "0xreg",
		Topics: []string{
			DIDIdentityClaimedSig,
			didHash,
			"0x0000000000000000000000000000000000000000000000000000000000000001",
			"0x0000000000000000000000001111111111111111111111111111111111111111",
		},
		TxHash: "0xtx1", BlockNumber: 100, Timestamp: now,
	})

	// Update stake: 1000 tokens (padded to 32 bytes)
	stakeData := "0x00000000000000000000000000000000000000000000003635c9adc5dea00000" // 1000e18
	if err := idx.IndexLog(&LogEntry{
		Address: "0xreg",
		Topics:  []string{DIDStakeUpdatedSig, didHash},
		Data:    stakeData,
		TxHash:  "0xtx2", BlockNumber: 101, Timestamp: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("IndexLog failed: %v", err)
	}

	identity := idx.GetIdentity(didHash)
	expected := new(big.Int)
	expected.SetString("1000000000000000000000", 10)
	if identity.StakedTokens.Cmp(expected) != 0 {
		t.Errorf("stake = %v, want %v", identity.StakedTokens, expected)
	}
}

func TestDIDIndexer_GenericUpdates(t *testing.T) {
	idx := NewDIDIndexer()
	now := time.Now()

	didHash := "0xaabbccdd00000000000000000000000000000000000000000000000000000000"

	// Claim first
	idx.IndexLog(&LogEntry{
		Address: "0xreg",
		Topics: []string{
			DIDIdentityClaimedSig,
			didHash,
			"0x0000000000000000000000000000000000000000000000000000000000000001",
			"0x0000000000000000000000001111111111111111111111111111111111111111",
		},
		TxHash: "0xtx1", BlockNumber: 100, Timestamp: now,
	})

	// NodesUpdated
	if err := idx.IndexLog(&LogEntry{
		Address: "0xreg",
		Topics:  []string{DIDNodesUpdatedSig, didHash},
		Data:    "0x",
		TxHash:  "0xtx2", BlockNumber: 101, Timestamp: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("NodesUpdated IndexLog failed: %v", err)
	}

	events := idx.GetEventsByDID(didHash)
	if len(events) != 2 { // claim + update
		t.Errorf("events = %d, want 2", len(events))
	}
}

func TestDIDIndexer_InvalidLog(t *testing.T) {
	idx := NewDIDIndexer()

	// No topics
	if err := idx.IndexLog(&LogEntry{}); err != nil {
		t.Fatalf("empty log should not error: %v", err)
	}

	// Insufficient topics
	err := idx.IndexLog(&LogEntry{
		Topics: []string{DIDIdentityClaimedSig}, // needs 4
	})
	if err == nil {
		t.Error("should error on insufficient topics")
	}
}
