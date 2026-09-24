// Copyright (c) 2025 Lux Industries Inc
// SPDX-License-Identifier: MIT

package evm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luxfi/indexer/storage"
)

// seats stands in for an RPC Service spread over several nodes, some of them
// behind. Every node agrees on the tip it reports, but a request for a block or
// a receipt lands on a lagging node every `every`-th time and is answered with
// null, as a node answers for a block it does not hold. A node can also lack
// eth_getBlockReceipts or eth_getTransactionReceipt altogether, as zood does,
// and answer MethodNotFound. The chain itself can be replaced above genesis:
// epoch numbers the chain each height belongs to, so a relaunch from the same
// genesis moves every height above 0 to a new epoch and a reorg moves only the
// heights it rewrote. lag makes "latest" answer from a node that far behind.
type seats struct {
	mu            sync.Mutex
	tip           uint64
	every         int
	calls         int
	nulls         int
	txs           func(h uint64) int // transactions in block h
	blockReceipts bool               // serves eth_getBlockReceipts
	txReceipts    bool               // serves eth_getTransactionReceipt
	hashless      map[uint64]bool    // blocks whose transactions come without a hash
	epoch         func(h uint64) uint64
	lag           uint64
	gone          map[uint64]bool // heights answered null by number
	requests      map[string]int
}

func (s *seats) serve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     int               `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests[req.Method]++

	reply := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": nil}
	var arg string
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params[0], &arg)
	}
	lagging := func() bool {
		s.calls++
		if s.every > 0 && s.calls%s.every == 0 {
			s.nulls++
			return true
		}
		return false
	}
	switch req.Method {
	case "eth_getBlockByNumber":
		h := s.tip - s.lag
		if arg != "latest" {
			h = hexToUint64(arg)
			if lagging() || s.gone[h] {
				break
			}
		}
		if h <= s.tip {
			reply["result"] = s.block(h)
		}
	case "eth_getBlockReceipts":
		if !s.blockReceipts {
			delete(reply, "result")
			reply["error"] = map[string]any{"code": -32601, "message": "the method eth_getBlockReceipts does not exist/is not available"}
			break
		}
		if h := hexToUint64(arg); !lagging() && h <= s.tip {
			out := []any{}
			for i := 0; i < s.txs(h); i++ {
				out = append(out, s.receipt(h, i))
			}
			reply["result"] = out
		}
	case "eth_getTransactionReceipt":
		if !s.txReceipts {
			delete(reply, "result")
			reply["error"] = map[string]any{"code": -32601, "message": "the method eth_getTransactionReceipt does not exist/is not available"}
			break
		}
		n, err := strconv.ParseUint(strings.TrimPrefix(arg, "0x"), 16, 64)
		if err == nil && n >= txBase && !lagging() {
			n -= txBase
			e, h, i := n>>40, (n&(1<<40-1))/16, int(n%16)
			if h <= s.tip && i < s.txs(h) && e == s.epoch(h) {
				reply["result"] = s.receipt(h, i)
			}
		}
	case "eth_getCode":
		reply["result"] = "0x"
	default:
		reply["result"] = "0x0"
	}
	_ = json.NewEncoder(w).Encode(reply)
}

const (
	sender    = "0x00000000000000000000000000000000000000aa"
	recipient = "0x00000000000000000000000000000000000000bb"
	txBase    = 0xabc000
)

// blockHash is block h of the chain as it stands. Block 0 belongs to no epoch:
// every chain here shares one genesis.
func (s *seats) blockHash(h uint64) string {
	if h == 0 {
		return fmt.Sprintf("0x%064x", 1)
	}
	return fmt.Sprintf("0x%064x", h+1+s.epoch(h)<<40)
}

func (s *seats) txHash(h uint64, i int) string {
	return fmt.Sprintf("0x%064x", txBase+s.epoch(h)<<40+h*16+uint64(i))
}

func (s *seats) block(h uint64) map[string]any {
	txs := []any{}
	for i := 0; i < s.txs(h); i++ {
		tx := map[string]any{
			"hash": s.txHash(h, i), "blockHash": s.blockHash(h), "blockNumber": fmt.Sprintf("0x%x", h),
			"from": sender, "to": recipient,
			"value": "0x1", "gas": "0x5208", "gasPrice": "0x1", "nonce": fmt.Sprintf("0x%x", i), "input": "0x",
			"transactionIndex": fmt.Sprintf("0x%x", i),
		}
		if s.hashless[h] {
			delete(tx, "hash")
		}
		txs = append(txs, tx)
	}
	parent := "0x" + fmt.Sprintf("%064x", 0)
	if h > 0 {
		parent = s.blockHash(h - 1)
	}
	return map[string]any{
		"number": fmt.Sprintf("0x%x", h), "hash": s.blockHash(h), "parentHash": parent,
		"timestamp": fmt.Sprintf("0x%x", 1_700_000_000+h), "gasLimit": "0xb71b00", "gasUsed": "0x0",
		"miner": "0x0000000000000000000000000000000000000000", "transactions": txs,
	}
}

func (s *seats) receipt(h uint64, i int) map[string]any {
	return map[string]any{
		"transactionHash": s.txHash(h, i), "blockHash": s.blockHash(h), "blockNumber": fmt.Sprintf("0x%x", h),
		"from": sender, "to": recipient, "transactionIndex": fmt.Sprintf("0x%x", i),
		"status": "0x1", "gasUsed": "0x5208", "logs": []any{},
	}
}

func newSeats(t *testing.T, tip uint64, every int) (*seats, *Indexer) {
	t.Helper()
	s := &seats{
		tip: tip, every: every, requests: map[string]int{},
		txs: func(h uint64) int {
			if h%3 == 1 {
				return 1
			}
			return 0
		},
		blockReceipts: true, txReceipts: true, hashless: map[uint64]bool{},
		epoch: func(uint64) uint64 { return 0 }, gone: map[uint64]bool{},
	}
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)

	store, err := storage.NewUnified(storage.DefaultUnifiedConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("store init: %v", err)
	}
	idx, err := NewIndexer(Config{ChainName: "test", RPCEndpoint: srv.URL, PollInterval: time.Second}, store)
	if err != nil {
		t.Fatalf("indexer: %v", err)
	}
	if err := idx.Init(ctx); err != nil {
		t.Fatalf("schema: %v", err)
	}
	// The head loop announces every block it indexes; drain the stream as Run
	// does, or the loop blocks once its buffer is full.
	live, stop := context.WithCancel(ctx)
	t.Cleanup(stop)
	go idx.subscriber.Run(live)
	return s, idx
}

// held reads what the index holds: its distinct heights, its highest one, and
// how many rows carry no hash.
func held(t *testing.T, idx *Indexer) (heights map[uint64]bool, top int64, blank int64) {
	t.Helper()
	ctx := context.Background()
	rows, err := idx.store.Query(ctx, "SELECT DISTINCT number FROM evm_blocks WHERE hash != ''")
	if err != nil {
		t.Fatalf("heights: %v", err)
	}
	heights = map[uint64]bool{}
	top = -1
	for _, r := range rows {
		n := toInt64(r["number"])
		heights[uint64(n)] = true
		top = max(top, n)
	}
	rows, err = idx.store.Query(ctx, "SELECT COUNT(*) AS n FROM evm_blocks WHERE hash = ''")
	if err != nil {
		t.Fatalf("blank rows: %v", err)
	}
	return heights, top, toInt64(rows[0]["n"])
}

func TestNullBlockIsNotServed(t *testing.T) {
	s, idx := newSeats(t, 10, 1) // every block request answered with null
	_, err := idx.adapter.GetBlockByNumber(context.Background(), 5)
	if !errors.Is(err, ErrNoBlock) {
		t.Fatalf("null block: err = %v, want ErrNoBlock", err)
	}
	_, err = idx.adapter.BlockReceipts(context.Background(), 5)
	if !errors.Is(err, ErrNoBlock) {
		t.Fatalf("null receipts: err = %v, want ErrNoBlock", err)
	}
	if s.nulls != 2 {
		t.Fatalf("answered %d nulls, want 2", s.nulls)
	}
}

// The live defect: a lagging seat answered null for a height under the tip, the
// null parsed as an empty block, and the loop moved on. Every pass must leave
// the index holding exactly [0, MAX] — never a height above one it lacks.
func TestNullBlocksAreRetriedNeverSkipped(t *testing.T) {
	const tip = 60
	s, idx := newSeats(t, tip, 3)
	ctx := context.Background()

	for pass := 0; pass < 200; pass++ {
		idx.indexNewBlocks(ctx)
		heights, top, blank := held(t, idx)
		if blank != 0 {
			t.Fatalf("pass %d: %d rows with no hash; a null was stored as a block", pass, blank)
		}
		for h := int64(0); h <= top; h++ {
			if !heights[uint64(h)] {
				t.Fatalf("pass %d: index reached %d without height %d", pass, top, h)
			}
		}
		if top == tip {
			break
		}
	}
	heights, top, _ := held(t, idx)
	if top != tip || len(heights) != tip+1 {
		t.Fatalf("index holds %d heights up to %d, want %d up to %d", len(heights), top, tip+1, tip)
	}
	if s.nulls == 0 {
		t.Fatal("the fake never answered null; the test proved nothing")
	}

	// Every block with a transaction has its receipt: none was indexed from a
	// seat that did not hold it.
	rows, err := idx.store.Query(ctx, "SELECT COUNT(*) AS n FROM evm_transactions WHERE gas_used = 0")
	if err != nil {
		t.Fatalf("receipts: %v", err)
	}
	if n := toInt64(rows[0]["n"]); n != 0 {
		t.Fatalf("%d transactions stored without their receipt", n)
	}
	rows, err = idx.store.Query(ctx, "SELECT COUNT(*) AS n FROM evm_transactions")
	if err != nil {
		t.Fatalf("transactions: %v", err)
	}
	if n, want := toInt64(rows[0]["n"]), int64((tip+2)/3); n != want {
		t.Fatalf("%d transactions, want %d", n, want)
	}
}

// What the old indexer left behind — holes under MAX, a leading hole at
// genesis, and a blank row where a null was stored — is filled by the audit,
// with the RPC still answering null intermittently.
func TestAuditFillsHoles(t *testing.T) {
	const tip = 80
	s, idx := newSeats(t, tip, 0)
	ctx := context.Background()

	missing := map[uint64]bool{0: true, 1: true, 7: true, 20: true, 21: true, 22: true, 50: true, 79: true}
	for h := uint64(0); h <= tip; h++ {
		if missing[h] {
			continue
		}
		if _, err := idx.indexBlock(ctx, h); err != nil {
			t.Fatalf("seed %d: %v", h, err)
		}
	}
	runs, next, err := idx.holes(ctx, 0)
	if err != nil {
		t.Fatalf("holes: %v", err)
	}
	want := [][2]uint64{{0, 1}, {7, 7}, {20, 22}, {50, 50}, {79, 79}}
	if fmt.Sprint(runs) != fmt.Sprint(want) || next != tip+1 {
		t.Fatalf("holes = %v next %d, want %v next %d", runs, next, want, tip+1)
	}
	// The row a null used to become: no hash, height 0.
	if err := idx.store.Exec(ctx, idx.upsertBlockSQL(), idx.blockArgs(&EVMBlock{Timestamp: time.Unix(0, 0)})...); err != nil {
		t.Fatalf("seed blank row: %v", err)
	}

	s.mu.Lock()
	s.every = 3
	s.mu.Unlock()
	for pass := 0; pass < 50 && idx.audited != tip+1; pass++ {
		idx.audit(ctx)
	}
	heights, top, blank := held(t, idx)
	if blank != 0 {
		t.Fatalf("%d blank rows survived the audit", blank)
	}
	if top != tip || len(heights) != tip+1 {
		t.Fatalf("after audit: %d heights up to %d, want %d up to %d", len(heights), top, tip+1, tip)
	}
	if idx.audited != tip+1 {
		t.Fatalf("audited = %d, want %d", idx.audited, tip+1)
	}
	if s.nulls == 0 {
		t.Fatal("the fake never answered null during the audit; the test proved nothing")
	}

	// A whole index costs the audit no block reads, even from genesis: the
	// count proves there is nothing to find.
	s.mu.Lock()
	before := s.requests["eth_getBlockByNumber"]
	s.mu.Unlock()
	idx.audited = 0
	idx.audit(ctx)
	s.mu.Lock()
	after := s.requests["eth_getBlockByNumber"]
	s.mu.Unlock()
	if after != before {
		t.Fatalf("audit of a whole index read %d blocks", after-before)
	}
	if idx.audited != tip+1 {
		t.Fatalf("re-audit left audited = %d, want %d", idx.audited, tip+1)
	}
}
