// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package platform

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/luxfi/indexer/storage"
)

// str coerces a scanned SQL value (string or []byte) to a string for assertions.
func str(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// TestInitSchemaAndSyncValidatorsSQLite is the integration guard for the
// P-Chain adapter against the DEFAULT (SQLite) query backend. It proves:
//   - InitSchema's DDL (incl. the new bls_* columns) is SQLite-compatible.
//   - SyncValidators writes without the Postgres-only to_timestamp() — the bug
//     this test locks down: timestamps are now bound as time.Time.
//   - weight falls back into stake_amount when stakeAmount is omitted.
//   - the BLS signer (publicKey + proofOfPossession) is persisted.
//   - delegators are synced into pchain_delegators.
func TestInitSchemaAndSyncValidatorsSQLite(t *testing.T) {
	ctx := context.Background()

	// One validator carries stakeAmount + signer + a delegator; the second
	// carries only weight (exercises the stake_amount fallback).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"jsonrpc":"2.0","id":1,
			"result":{"validators":[
				{
					"txID":"TxA","nodeID":"NodeID-A",
					"startTime":"1600000000","endTime":"1700000000",
					"stakeAmount":"2000000000000000","potentialReward":"12345",
					"delegationFee":"20000","uptime":"0.998","connected":true,
					"signer":{"publicKey":"0xabc123","proofOfPossession":"0xdef456"},
					"delegators":[
						{"txID":"DelTx1","nodeID":"NodeID-A","startTime":"1600000050","endTime":"1699999950","stakeAmount":"500","potentialReward":"7","rewardOwner":"owner1"}
					]
				},
				{
					"txID":"TxB","nodeID":"NodeID-B",
					"startTime":"1600000000","endTime":"1700000000",
					"weight":"1500000000000000","potentialReward":"6789",
					"delegationFee":"20000","uptime":"0.5","connected":false
				}
			]}
		}`))
	}))
	defer server.Close()

	store, err := storage.NewUnified(storage.DefaultUnifiedConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("NewUnified: %v", err)
	}
	defer store.Close()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("store.Init: %v", err)
	}

	adapter := New(server.URL)

	// DDL must apply cleanly on SQLite.
	if err := adapter.InitSchema(ctx, store); err != nil {
		t.Fatalf("InitSchema on sqlite: %v", err)
	}

	// The regression assertion: this used to fail with "no such function:
	// to_timestamp" on SQLite.
	if err := adapter.SyncValidators(ctx, store); err != nil {
		t.Fatalf("SyncValidators on sqlite: %v", err)
	}

	// Validator A: signer + explicit stakeAmount.
	rowsA, err := store.Query(ctx, "SELECT node_id, stake_amount, bls_public_key, bls_proof_of_possession, start_time, connected FROM pchain_validators WHERE node_id = ?", "NodeID-A")
	if err != nil {
		t.Fatalf("query validator A: %v", err)
	}
	if len(rowsA) != 1 {
		t.Fatalf("validator A: want 1 row, got %d", len(rowsA))
	}
	a := rowsA[0]
	if got := str(a["stake_amount"]); got != "2000000000000000" {
		t.Errorf("validator A stake_amount = %q, want 2000000000000000", got)
	}
	if got := str(a["bls_public_key"]); got != "0xabc123" {
		t.Errorf("validator A bls_public_key = %q, want 0xabc123", got)
	}
	if got := str(a["bls_proof_of_possession"]); got != "0xdef456" {
		t.Errorf("validator A bls_proof_of_possession = %q, want 0xdef456", got)
	}
	if str(a["start_time"]) == "" {
		t.Errorf("validator A start_time is empty; timestamp bind failed")
	}

	// Validator B: weight fell back into stake_amount, no signer.
	rowsB, err := store.Query(ctx, "SELECT stake_amount, bls_public_key FROM pchain_validators WHERE node_id = ?", "NodeID-B")
	if err != nil {
		t.Fatalf("query validator B: %v", err)
	}
	if len(rowsB) != 1 {
		t.Fatalf("validator B: want 1 row, got %d", len(rowsB))
	}
	if got := str(rowsB[0]["stake_amount"]); got != "1500000000000000" {
		t.Errorf("validator B stake_amount = %q, want 1500000000000000 (weight fallback)", got)
	}
	if got := str(rowsB[0]["bls_public_key"]); got != "" {
		t.Errorf("validator B bls_public_key = %q, want empty", got)
	}

	// Delegator synced under NodeID-A.
	dels, err := store.Query(ctx, "SELECT tx_id, node_id FROM pchain_delegators WHERE node_id = ?", "NodeID-A")
	if err != nil {
		t.Fatalf("query delegators: %v", err)
	}
	if len(dels) != 1 {
		t.Fatalf("delegators for NodeID-A: want 1, got %d", len(dels))
	}
	if got := str(dels[0]["tx_id"]); got != "DelTx1" {
		t.Errorf("delegator tx_id = %q, want DelTx1", got)
	}

	// Idempotency: a second sync (the poll loop re-runs every tick) must upsert
	// cleanly, not error or duplicate.
	if err := adapter.SyncValidators(ctx, store); err != nil {
		t.Fatalf("second SyncValidators: %v", err)
	}
	cnt, err := store.Query(ctx, "SELECT COUNT(*) AS n FROM pchain_validators")
	if err != nil {
		t.Fatalf("count validators: %v", err)
	}
	if got := str(cnt[0]["n"]); got != "2" {
		t.Errorf("validator count after re-sync = %q, want 2", got)
	}
}
