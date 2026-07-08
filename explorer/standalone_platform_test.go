// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package explorer

import (
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// newPlatformServer seeds a P-Chain (pchain_*) SQLite DB and returns a running
// StandaloneServer over it. The schema mirrors the columns written by the
// generic chain indexer (pchain_blocks) and platform.SyncValidators
// (pchain_validators / pchain_delegators).
func newPlatformServer(t *testing.T) *httptest.Server {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "indexer.db")

	db, err := sql.Open("sqlite3", "file:"+dbPath+"?_journal_mode=WAL")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ddl := `
		CREATE TABLE pchain_blocks (
			id TEXT PRIMARY KEY, parent_id TEXT, height BIGINT, timestamp TIMESTAMP,
			status TEXT, tx_count INT, tx_ids TEXT, data TEXT, metadata TEXT
		);
		CREATE TABLE pchain_validators (
			node_id TEXT PRIMARY KEY, start_time TIMESTAMP, end_time TIMESTAMP,
			stake_amount BIGINT, potential_reward BIGINT, delegation_fee REAL,
			uptime REAL, connected BOOLEAN, net_id TEXT, tx_id TEXT,
			bls_public_key TEXT, bls_proof_of_possession TEXT
		);
		CREATE TABLE pchain_delegators (
			tx_id TEXT PRIMARY KEY, node_id TEXT, stake_amount BIGINT
		);`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	// 2 linear blocks.
	db.Exec(`INSERT INTO pchain_blocks (id, parent_id, height, timestamp, status, tx_count, tx_ids) VALUES
		('bkA','bkGen',10,1600000000,'accepted',1,'["tx1"]'),
		('bkB','bkA',11,1600000010,'accepted',0,'[]')`)
	// 2 validators: A has a BLS signer + 2 delegators; B has neither.
	db.Exec(`INSERT INTO pchain_validators
		(node_id, start_time, end_time, stake_amount, potential_reward, delegation_fee, uptime, connected, net_id, tx_id, bls_public_key, bls_proof_of_possession) VALUES
		('NodeID-A',1600000000,1700000000,'2000000000000000','12345',20000,0.998,1,'primary','TxA','0xabc','0xdef'),
		('NodeID-B',1600000000,1700000000,'1500000000000000','6789',20000,0.5,0,'primary','TxB','','')`)
	db.Exec(`INSERT INTO pchain_delegators (tx_id, node_id, stake_amount) VALUES
		('DelTx1','NodeID-A','500'), ('DelTx2','NodeID-A','700')`)
	db.Close()

	srv, err := NewStandaloneServer(Config{
		IndexerDBPath: dbPath,
		ChainID:       1,
		ChainName:     "P-Chain",
		CoinSymbol:    "LUX",
	})
	if err != nil {
		t.Fatalf("NewStandaloneServer: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	if !srv.t.platform {
		t.Fatalf("detectTables did not select the platform variant")
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close() })
	return ts
}

func TestPlatformBlocks(t *testing.T) {
	ts := newPlatformServer(t)
	body := getJSON(t, ts, "/v1/explorer/blocks", 200)
	blocks := itemMaps(t, body)
	if len(blocks) != 2 {
		t.Fatalf("blocks: want 2, got %d", len(blocks))
	}
	// Newest-first by height.
	if h := toInt64(blocks[0]["height"]); h != 11 {
		t.Errorf("blocks[0].height = %d, want 11", h)
	}
	if blocks[0]["hash"] != "bkB" || blocks[0]["id"] != "bkB" {
		t.Errorf("blocks[0] id/hash = %v/%v, want bkB", blocks[0]["id"], blocks[0]["hash"])
	}
	// tx_ids decoded to a JSON array.
	txids, ok := blocks[1]["tx_ids"].([]any)
	if !ok || len(txids) != 1 || txids[0] != "tx1" {
		t.Errorf("blocks[1].tx_ids = %v, want [tx1]", blocks[1]["tx_ids"])
	}
	if s, _ := blocks[0]["timestamp"].(string); s == "" {
		t.Errorf("blocks[0].timestamp is empty")
	}
}

func TestPlatformBlockByHeightAndID(t *testing.T) {
	ts := newPlatformServer(t)
	byHeight := getJSON(t, ts, "/v1/explorer/blocks/11", 200)
	if toInt64(byHeight["height"]) != 11 {
		t.Errorf("by-height height = %v, want 11", byHeight["height"])
	}
	byID := getJSON(t, ts, "/v1/explorer/blocks/bkA", 200)
	if toInt64(byID["height"]) != 10 {
		t.Errorf("by-id height = %v, want 10", byID["height"])
	}
}

func TestPlatformValidators(t *testing.T) {
	ts := newPlatformServer(t)
	body := getJSON(t, ts, "/v1/explorer/validators", 200)
	vals := itemMaps(t, body)
	if len(vals) != 2 {
		t.Fatalf("validators: want 2, got %d", len(vals))
	}
	// Ordered by stake DESC: A (2e15) before B (1.5e15).
	a := vals[0]
	if a["node_id"] != "NodeID-A" {
		t.Fatalf("validators[0].node_id = %v, want NodeID-A", a["node_id"])
	}
	if a["weight"] != "2000000000000000" || a["stake"] != "2000000000000000" {
		t.Errorf("A weight/stake = %v/%v, want 2000000000000000", a["weight"], a["stake"])
	}
	if a["bls_public_key"] != "0xabc" || a["bls_proof_of_possession"] != "0xdef" {
		t.Errorf("A bls = %v/%v, want 0xabc/0xdef", a["bls_public_key"], a["bls_proof_of_possession"])
	}
	if dc := toInt64(a["delegator_count"]); dc != 2 {
		t.Errorf("A delegator_count = %d, want 2", dc)
	}
	if a["connected"] != true {
		t.Errorf("A connected = %v, want true", a["connected"])
	}
	if s, _ := a["start_time"].(string); s == "" {
		t.Errorf("A start_time is empty")
	}

	b := vals[1]
	if b["node_id"] != "NodeID-B" {
		t.Fatalf("validators[1].node_id = %v, want NodeID-B", b["node_id"])
	}
	// No signer captured => null, not "".
	if b["bls_public_key"] != nil {
		t.Errorf("B bls_public_key = %v, want null", b["bls_public_key"])
	}
	if dc := toInt64(b["delegator_count"]); dc != 0 {
		t.Errorf("B delegator_count = %d, want 0", dc)
	}
	if b["connected"] != false {
		t.Errorf("B connected = %v, want false", b["connected"])
	}
}
