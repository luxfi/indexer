// Copyright (c) 2025 Lux Industries Inc
// SPDX-License-Identifier: MIT

package evm

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// count answers a COUNT(*) query against the index.
func count(t *testing.T, idx *Indexer, q string, args ...any) int64 {
	t.Helper()
	rows, err := idx.store.Query(context.Background(), q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return toInt64(rows[0]["n"])
}

// legacy stores block h the way the indexer before v1.5.9 left it: the block
// row, counting its transactions, and none of them.
func legacy(t *testing.T, idx *Indexer, h uint64) {
	t.Helper()
	b, err := idx.adapter.GetBlockByNumber(context.Background(), h)
	if err != nil {
		t.Fatalf("read %d: %v", h, err)
	}
	if err := idx.store.Exec(context.Background(), idx.upsertBlockSQL(), idx.blockArgs(b)...); err != nil {
		t.Fatalf("store %d: %v", h, err)
	}
}

// whole reports what an index that matches the chain must hold, failing the
// test on the first difference.
func whole(t *testing.T, s *seats, idx *Indexer) (total int64) {
	t.Helper()
	for h := uint64(0); h <= s.tip; h++ {
		n := int64(s.txs(h))
		total += n
		if got := count(t, idx, "SELECT COUNT(*) AS n FROM evm_transactions WHERE block_number = ? AND block_hash = ?", int64(h), s.blockHash(h)); got != n {
			t.Fatalf("block %d holds %d transactions, the chain has %d", h, got, n)
		}
	}
	if got := count(t, idx, "SELECT COUNT(*) AS n FROM evm_transactions"); got != total {
		t.Fatalf("%d transaction rows, the chain has %d", got, total)
	}
	if got := count(t, idx, "SELECT COUNT(*) AS n FROM evm_transactions WHERE hash = '' OR from_addr = ''"); got != 0 {
		t.Fatalf("%d transaction rows without a hash or sender", got)
	}
	if got := count(t, idx, "SELECT COUNT(*) AS n FROM evm_blocks"); got != int64(s.tip+1) {
		t.Fatalf("%d block rows for %d heights", got, s.tip+1)
	}
	return total
}

// A transaction without a hash is not stored, and neither is its block: the
// height stays unheld, so the loop does not move past it.
func TestHashlessTransactionIsNotStored(t *testing.T) {
	s, idx := newSeats(t, 10, 0)
	s.hashless[4] = true
	ctx := context.Background()

	if _, err := idx.indexBlock(ctx, 4); err == nil {
		t.Fatal("stored a block whose transaction has no hash")
	}
	if n := count(t, idx, "SELECT COUNT(*) AS n FROM evm_transactions"); n != 0 {
		t.Fatalf("%d transaction rows from a block that was refused", n)
	}
	for pass := 0; pass < 3; pass++ {
		idx.indexNewBlocks(ctx)
	}
	if _, top, _ := held(t, idx); top != 3 {
		t.Fatalf("index reached %d past the refused block 4", top)
	}
}

// The Zoo shape: blocks held with none of their transactions, a hashless row
// filed at block 1, rows filed under a block hash the index does not hold, and
// a replaced block's row beside the real one. The audit reads each of those
// heights again and replaces what it held, and a second audit finds nothing.
func TestAuditReplacesWrongTransactions(t *testing.T) {
	const tip = 40
	s, idx := newSeats(t, tip, 0)
	s.txs = func(h uint64) int { return int(h % 3) } // 0, 1 or 2 per block
	ctx := context.Background()

	for h := uint64(0); h <= tip; h++ {
		if h >= 1 && h <= 20 {
			legacy(t, idx, h)
			continue
		}
		if _, err := idx.indexBlock(ctx, h); err != nil {
			t.Fatalf("seed %d: %v", h, err)
		}
	}
	now := time.Unix(1_700_000_001, 0)
	if err := idx.store.Exec(ctx, idx.upsertTxSQL(), "", "", 1, 0, "", "", "", 0, "", 0, 0, "", 0, "", now, now); err != nil {
		t.Fatalf("seed hashless row: %v", err)
	}
	if err := idx.store.Exec(ctx, "UPDATE evm_transactions SET block_hash = '0xdead' WHERE block_number = 32"); err != nil {
		t.Fatalf("seed misfiled rows: %v", err)
	}
	stale := &EVMBlock{Number: 35, Hash: "0xstale", Timestamp: now, Transactions: make([]Transaction, 2)}
	if err := idx.store.Exec(ctx, idx.upsertBlockSQL(), idx.blockArgs(stale)...); err != nil {
		t.Fatalf("seed replaced block: %v", err)
	}

	wrong, err := idx.mismatched(ctx, 0, tip+1)
	if err != nil {
		t.Fatalf("mismatched: %v", err)
	}
	// The 14 blocks in 1..20 that carry transactions, then 32 and 35.
	if len(wrong) != 16 || wrong[0] != 1 || wrong[len(wrong)-2] != 32 || wrong[len(wrong)-1] != 35 {
		t.Fatalf("mismatched = %v", wrong)
	}

	for pass := 0; idx.audit(ctx); pass++ {
		if pass > 10 {
			t.Fatal("audit never finished")
		}
	}
	total := whole(t, s, idx)
	if n := count(t, idx, "SELECT COUNT(*) AS n FROM evm_transactions WHERE status != 1 OR gas_used != 21000"); n != 0 {
		t.Fatalf("%d transactions without their receipt", n)
	}
	// Every transaction is from one sender, and each was counted once: rows
	// read again for 32 and 35 had been counted already.
	if n := count(t, idx, "SELECT tx_count AS n FROM evm_addresses WHERE hash = ?", sender); n != total {
		t.Fatalf("sender counted %d transactions, the chain has %d", n, total)
	}

	s.mu.Lock()
	before := s.requests["eth_getBlockByNumber"]
	s.mu.Unlock()
	idx.audited = 0
	if idx.audit(ctx) {
		t.Fatal("a whole index left work for another pass")
	}
	s.mu.Lock()
	after := s.requests["eth_getBlockByNumber"]
	s.mu.Unlock()
	if after != before {
		t.Fatalf("audit of a whole index read %d blocks", after-before)
	}
}

// A long repair is done auditBudget heights at a time, each pass starting where
// the last stopped, so the head is never held back for the whole of it.
func TestAuditRepairsInBudgetedPasses(t *testing.T) {
	const tip = 2*auditBudget + 50
	s, idx := newSeats(t, tip, 0)
	s.txs = func(h uint64) int {
		if h == 0 {
			return 0
		}
		return 1
	}
	ctx := context.Background()
	for h := uint64(0); h <= tip; h++ {
		legacy(t, idx, h)
	}

	passes := 1
	for more := idx.audit(ctx); more; more = idx.audit(ctx) {
		if want := uint64(passes*auditBudget + 1); idx.audited != want {
			t.Fatalf("after pass %d the audit resumes at %d, want %d", passes, idx.audited, want)
		}
		passes++
	}
	if passes != 3 {
		t.Fatalf("%d passes for %d heights, want 3", passes, tip)
	}
	whole(t, s, idx)
}

// A long history is compared auditWindow heights per pass, so the first audit of
// a million-block index is a walk of short reads, not one long one.
func TestAuditComparesInWindows(t *testing.T) {
	const tip = 2*auditWindow + 500
	short := map[uint64]bool{5: true, auditWindow + 2345: true, 2*auditWindow + 400: true}
	s, idx := newSeats(t, tip, 0)
	s.txs = func(h uint64) int {
		if short[h] {
			return 1
		}
		return 0
	}
	ctx := context.Background()
	for h := uint64(0); h <= tip; h++ {
		b := &EVMBlock{Number: h, Hash: s.blockHash(h), Timestamp: time.Unix(1_700_000_000+int64(h), 0), Transactions: make([]Transaction, s.txs(h))}
		if err := idx.store.Exec(ctx, idx.upsertBlockSQL(), idx.blockArgs(b)...); err != nil {
			t.Fatalf("seed %d: %v", h, err)
		}
	}

	var at []uint64
	for more := true; more; {
		more = idx.audit(ctx)
		at = append(at, idx.audited)
	}
	if want := []uint64{auditWindow, 2 * auditWindow, tip + 1}; fmt.Sprint(at) != fmt.Sprint(want) {
		t.Fatalf("audit passes ended at %v, want %v", at, want)
	}
	if n := s.requests["eth_getBlockByNumber"]; n != len(short) {
		t.Fatalf("read %d blocks, want only the %d short ones", n, len(short))
	}
	if n := count(t, idx, "SELECT COUNT(*) AS n FROM evm_transactions"); n != int64(len(short)) {
		t.Fatalf("%d transactions, want %d", n, len(short))
	}
}

// zood has no eth_getBlockReceipts. The indexer asks once, then reads each
// transaction's receipt, and a null among those is retried like a null block.
func TestReceiptsPerTransactionWithoutBlockReceipts(t *testing.T) {
	const tip = 30
	s, idx := newSeats(t, tip, 4)
	s.blockReceipts = false
	s.txs = func(h uint64) int { return int(h % 3) }
	ctx := context.Background()

	for pass := 0; pass < 300; pass++ {
		idx.indexNewBlocks(ctx)
		heights, top, _ := held(t, idx)
		if int64(len(heights)) != top+1 {
			t.Fatalf("pass %d: index reached %d holding %d heights", pass, top, len(heights))
		}
		if top == tip {
			break
		}
	}
	whole(t, s, idx)
	if n := count(t, idx, "SELECT COUNT(*) AS n FROM evm_transactions WHERE status != 1 OR gas_used != 21000"); n != 0 {
		t.Fatalf("%d transactions without their receipt", n)
	}
	if n := s.requests["eth_getBlockReceipts"]; n != 1 {
		t.Fatalf("asked for eth_getBlockReceipts %d times; the node said once it has none", n)
	}
	if s.nulls == 0 {
		t.Fatal("the fake never answered null; the test proved nothing")
	}
}

// A node that serves no receipts at all still yields its transactions, with
// every submitted field and no result.
func TestNoReceiptsStillIndexesTransactions(t *testing.T) {
	const tip = 12
	s, idx := newSeats(t, tip, 0)
	s.blockReceipts, s.txReceipts = false, false
	s.txs = func(h uint64) int { return int(h % 3) }

	idx.indexNewBlocks(context.Background())
	total := whole(t, s, idx)
	if n := count(t, idx, "SELECT COUNT(*) AS n FROM evm_transactions WHERE gas = 21000 AND from_addr = ?", sender); n != total {
		t.Fatalf("%d of %d transactions carry their submitted fields", n, total)
	}
}
