// Copyright (c) 2025 Lux Industries Inc
// SPDX-License-Identifier: MIT

package evm

import (
	"context"
	"testing"
)

// indexed runs the head loop until the index reaches the chain's tip.
func indexed(t *testing.T, s *seats, idx *Indexer) {
	t.Helper()
	for pass := 0; pass < 10; pass++ {
		idx.indexNewBlocks(context.Background())
		if _, top, _ := held(t, idx); top == int64(s.tip) {
			return
		}
	}
	t.Fatalf("index never reached tip %d", s.tip)
}

// matches fails the test unless the index holds exactly the chain as it now
// stands, heights 0 through tip.
func matches(t *testing.T, s *seats, idx *Indexer) {
	t.Helper()
	whole(t, s, idx)
	for h := uint64(0); h <= s.tip; h++ {
		if n := count(t, idx, "SELECT COUNT(*) AS n FROM evm_blocks WHERE number = ? AND hash = ?", int64(h), s.blockHash(h)); n != 1 {
			t.Fatalf("block %d is not the chain's %s", h, s.blockHash(h))
		}
	}
}

// Hanzo's planned restart: the same genesis, every later block replaced, and a
// new head far under the old cursor. Block 0 cannot tell the chains apart;
// block 1 can. The index is kept until the streak runs out, then emptied of
// everything the old chain left and rebuilt from the new one.
func TestSameGenesisRelaunchIsReindexed(t *testing.T) {
	const old, fresh = 400, 50
	s, idx := newSeats(t, old, 0)
	s.txs = func(h uint64) int { return int(h % 3) }
	indexed(t, s, idx)
	genesis := s.blockHash(0)

	s.mu.Lock()
	s.epoch = func(uint64) uint64 { return 1 }
	s.tip = fresh
	s.mu.Unlock()

	ctx := context.Background()
	for i := 1; i < regenesisConfirmations; i++ {
		idx.indexNewBlocks(ctx)
		if n := count(t, idx, "SELECT COUNT(*) AS n FROM evm_blocks"); n != old+1 {
			t.Fatalf("poll %d: the index was touched before the streak ran out (%d blocks)", i, n)
		}
	}
	idx.indexNewBlocks(ctx)

	matches(t, s, idx)
	if s.blockHash(0) != genesis {
		t.Fatal("the fake changed genesis; the test proved nothing")
	}
	// Nothing the old chain derived survives: every address count is the new
	// chain's alone.
	if n := count(t, idx, "SELECT tx_count AS n FROM evm_addresses WHERE hash = ?", sender); n != count(t, idx, "SELECT COUNT(*) AS n FROM evm_transactions") {
		t.Fatalf("sender counts %d transactions from both chains", n)
	}
}

// A node behind its peers reports a head far under the cursor for as long as
// it likes. It serves block 1 as the index holds it, so nothing is erased and
// the streak is cleared each time it runs out.
func TestSlowNodeIsNotRelaunch(t *testing.T) {
	const tip = 400
	s, idx := newSeats(t, tip, 0)
	s.txs = func(h uint64) int { return int(h % 3) }
	indexed(t, s, idx)

	s.mu.Lock()
	s.lag = 300
	s.mu.Unlock()
	for i := 0; i < regenesisConfirmations*4; i++ {
		idx.indexNewBlocks(context.Background())
	}
	matches(t, s, idx)
	if idx.lowHead >= regenesisConfirmations {
		t.Fatalf("streak at %d after the node proved to be on the same chain", idx.lowHead)
	}
	if s.requests["eth_getBlockByNumber"] == 0 {
		t.Fatal("never asked the chain for anything")
	}
}

// A null where block 1 should be is a node that does not hold it yet, not a
// new chain: the index is kept, and the question asked again next poll.
func TestUnreadableOldBlockKeepsIndex(t *testing.T) {
	const tip = 400
	s, idx := newSeats(t, tip, 0)
	indexed(t, s, idx)

	s.mu.Lock()
	s.lag = 300
	s.gone[1] = true
	s.mu.Unlock()
	for i := 0; i < regenesisConfirmations*3; i++ {
		idx.indexNewBlocks(context.Background())
	}
	if idx.lowHead < regenesisConfirmations {
		t.Fatalf("streak cleared to %d without an answer", idx.lowHead)
	}
	s.mu.Lock()
	s.lag = 0
	delete(s.gone, 1)
	s.mu.Unlock()
	matches(t, s, idx)
}

// A reorg rewrites a few heights near the head, within reorgDepth. It is not a
// relaunch: the head never drops far under the cursor, the index is not
// emptied, and indexing carries on above the cursor as it always has.
func TestShallowReorgIsNotRelaunch(t *testing.T) {
	const tip = 400
	s, idx := newSeats(t, tip, 0)
	indexed(t, s, idx)

	s.mu.Lock()
	s.epoch = func(h uint64) uint64 {
		if h > tip-10 {
			return 2
		}
		return 0
	}
	s.tip = tip - 5
	s.mu.Unlock()
	for i := 0; i < regenesisConfirmations*3; i++ {
		idx.indexNewBlocks(context.Background())
	}
	if n := count(t, idx, "SELECT COUNT(*) AS n FROM evm_blocks"); n != tip+1 {
		t.Fatalf("%d blocks after a shallow reorg, want the %d held before it", n, tip+1)
	}

	s.mu.Lock()
	s.tip = tip + 5
	s.mu.Unlock()
	idx.indexNewBlocks(context.Background())
	for h := uint64(0); h <= tip+5; h++ {
		if h > tip-10 && h <= tip {
			continue // the rewritten heights the loop had already passed
		}
		if n := count(t, idx, "SELECT COUNT(*) AS n FROM evm_blocks WHERE number = ? AND hash = ?", int64(h), s.blockHash(h)); n != 1 {
			t.Fatalf("block %d is not the chain's", h)
		}
	}
}
