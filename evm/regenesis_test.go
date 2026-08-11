// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package evm

import (
	"context"
	"errors"
	"testing"
)

// probe stands in for the RPC so a test can say what block 0 looks like without
// a network, and count how often it was asked.
type probe struct {
	hash  string
	err   error
	calls int
}

func (p *probe) at(context.Context) (string, error) {
	p.calls++
	return p.hash, p.err
}

func idxWith(p *probe, baseline string) *Indexer {
	return &Indexer{genesis: baseline, genesisAt: p.at}
}

// The zoo case: a chain rebuilt from genesis leaves the cursor stranded above
// the new head forever. Recognising it takes the full streak, then one probe.
func TestRelaunchedOnNewGenesis(t *testing.T) {
	p := &probe{hash: "0xnew"}
	idx := idxWith(p, "0xold")

	const head, cursor = 9564, 24606
	for i := 1; i < regenesisConfirmations; i++ {
		if idx.relaunched(context.Background(), head, cursor) {
			t.Fatalf("called it at poll %d; the streak is meant to take %d", i, regenesisConfirmations)
		}
	}
	if p.calls != 0 {
		t.Fatalf("probed %d times before the streak finished; the streak exists to avoid that", p.calls)
	}
	if !idx.relaunched(context.Background(), head, cursor) {
		t.Fatal("genesis changed and the head is far under the cursor, but the chain was not called relaunched")
	}
	if p.calls != 1 {
		t.Fatalf("probed %d times, want 1", p.calls)
	}
	if idx.genesis != "0xnew" {
		t.Fatalf("genesis on record is %q, want the new chain's", idx.genesis)
	}
}

// A backend lagging behind its peers looks exactly like a relaunch by height.
// The genesis hash is what tells them apart, and it must never erase this one.
func TestLagIsNotRelaunch(t *testing.T) {
	p := &probe{hash: "0xsame"}
	idx := idxWith(p, "0xsame")

	for i := 0; i < regenesisConfirmations*3; i++ {
		if idx.relaunched(context.Background(), 9564, 24606) {
			t.Fatalf("erased a chain whose genesis never changed (poll %d)", i+1)
		}
	}
	if idx.lowHead != 0 {
		t.Fatalf("streak left at %d; a proven-same chain should clear it", idx.lowHead)
	}
}

// Unreadable genesis is not evidence of anything. Keep the index.
func TestUnreadableGenesisKeepsIndex(t *testing.T) {
	p := &probe{err: errors.New("dial tcp: connection refused")}
	idx := idxWith(p, "0xold")

	for i := 0; i < regenesisConfirmations*2; i++ {
		if idx.relaunched(context.Background(), 9564, 24606) {
			t.Fatal("erased a chain on a failed probe")
		}
	}
}

// With nothing on record there is nothing to compare against, so the first
// answer is no — and the hash is kept so the next relaunch is caught.
func TestNoBaselineRecordsAndKeeps(t *testing.T) {
	p := &probe{hash: "0xwhatever"}
	idx := idxWith(p, "")

	for i := 0; i < regenesisConfirmations; i++ {
		if idx.relaunched(context.Background(), 9564, 24606) {
			t.Fatal("erased a chain with no genesis on record to compare against")
		}
	}
	if idx.genesis != "0xwhatever" {
		t.Fatalf("genesis on record is %q; it should have been noted for next time", idx.genesis)
	}
}

// A head that is merely behind, or a chain that has not yet outrun the reorg
// window, must not start the streak at all.
func TestShallowMovesAreNotRelaunch(t *testing.T) {
	cases := []struct {
		name         string
		head, cursor uint64
	}{
		{"reorg within the window", 24600, 24606},
		{"head exactly at the edge", 24606 - reorgDepth, 24606},
		{"young chain", 5, reorgDepth},
		{"head ahead of cursor", 30000, 24606},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &probe{hash: "0xnew"}
			idx := idxWith(p, "0xold")
			for i := 0; i < regenesisConfirmations*2; i++ {
				if idx.relaunched(context.Background(), c.head, c.cursor) {
					t.Fatalf("head %d vs cursor %d was called a relaunch", c.head, c.cursor)
				}
			}
			if p.calls != 0 {
				t.Fatalf("spent %d probes on a head that never left the reorg window", p.calls)
			}
		})
	}
}

// A dip that recovers must leave no suspicion behind, or unrelated dips would
// add up over hours into a reset.
func TestRecoveredHeadClearsStreak(t *testing.T) {
	p := &probe{hash: "0xnew"}
	idx := idxWith(p, "0xold")

	idx.relaunched(context.Background(), 9564, 24606)
	idx.relaunched(context.Background(), 9564, 24606)
	if idx.lowHead != 2 {
		t.Fatalf("streak is %d, want 2", idx.lowHead)
	}
	idx.relaunched(context.Background(), 24610, 24606) // caught up
	if idx.lowHead != 0 {
		t.Fatalf("streak is %d after the head recovered, want 0", idx.lowHead)
	}
	if idx.relaunched(context.Background(), 9564, 24606) {
		t.Fatal("one poll after a recovery was enough to erase the chain")
	}
}

// StartBlock moves the floor: an operator indexing from mid-chain has a cursor
// that is legitimately far above zero, and must not read as a relaunch.
func TestStartBlockRaisesTheFloor(t *testing.T) {
	p := &probe{hash: "0xnew"}
	idx := idxWith(p, "0xold")
	idx.config.StartBlock = 1_000_000

	for i := 0; i < regenesisConfirmations*2; i++ {
		if idx.relaunched(context.Background(), 999_000, 1_000_050) {
			t.Fatal("a cursor just above StartBlock was called a relaunch")
		}
	}
	if p.calls != 0 {
		t.Fatalf("spent %d probes inside the StartBlock window", p.calls)
	}
}
