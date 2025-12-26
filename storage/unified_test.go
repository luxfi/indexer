// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package storage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luxfi/indexer/storage/kv"
	"github.com/luxfi/indexer/storage/query"
)

func TestUnifiedStore(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "unified-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create unified store
	cfg := UnifiedConfig{
		KV: kv.Config{
			Path: filepath.Join(tmpDir, "kv"),
		},
		Query: query.Config{
			Backend: query.BackendSQLite,
			DataDir: filepath.Join(tmpDir, "query"),
		},
		DualWrite: true,
	}

	store, err := NewUnified(cfg)
	if err != nil {
		t.Fatalf("Failed to create unified store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Initialize
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}

	// Test ping
	if err := store.Ping(ctx); err != nil {
		t.Fatalf("Failed to ping store: %v", err)
	}

	t.Run("BlockStorage", func(t *testing.T) {
		block := &Block{
			ID:        "block-001",
			ParentID:  "genesis",
			Height:    1,
			Timestamp: time.Now().Truncate(time.Second),
			Status:    "accepted",
			TxCount:   5,
			TxIDs:     []string{"tx1", "tx2", "tx3", "tx4", "tx5"},
			Data:      json.RawMessage(`{"type": "test"}`),
			Metadata:  json.RawMessage(`{"version": 1}`),
			CreatedAt: time.Now().Truncate(time.Second),
		}

		// Store block
		if err := store.StoreBlock(ctx, "blocks", block); err != nil {
			t.Fatalf("Failed to store block: %v", err)
		}

		// Get block by ID
		retrieved, err := store.GetBlock(ctx, "blocks", block.ID)
		if err != nil {
			t.Fatalf("Failed to get block: %v", err)
		}
		if retrieved.ID != block.ID {
			t.Errorf("Expected block ID %s, got %s", block.ID, retrieved.ID)
		}
		if retrieved.Height != block.Height {
			t.Errorf("Expected height %d, got %d", block.Height, retrieved.Height)
		}

		// Get block by height
		byHeight, err := store.GetBlockByHeight(ctx, "blocks", 1)
		if err != nil {
			t.Fatalf("Failed to get block by height: %v", err)
		}
		if byHeight.ID != block.ID {
			t.Errorf("Expected block ID %s, got %s", block.ID, byHeight.ID)
		}

		// Get recent blocks
		recent, err := store.GetRecentBlocks(ctx, "blocks", 10)
		if err != nil {
			t.Fatalf("Failed to get recent blocks: %v", err)
		}
		if len(recent) != 1 {
			t.Errorf("Expected 1 recent block, got %d", len(recent))
		}
	})

	t.Run("VertexStorage", func(t *testing.T) {
		vertex := &Vertex{
			ID:        "vertex-001",
			Type:      "normal",
			ParentIDs: []string{"vertex-000"},
			Height:    1,
			Epoch:     1,
			TxIDs:     []string{"tx1"},
			Timestamp: time.Now().Truncate(time.Second),
			Status:    "accepted",
			Data:      json.RawMessage(`{"type": "test"}`),
			Metadata:  json.RawMessage(`{"version": 1}`),
			CreatedAt: time.Now().Truncate(time.Second),
		}

		// Store vertex
		if err := store.StoreVertex(ctx, "vertices", vertex); err != nil {
			t.Fatalf("Failed to store vertex: %v", err)
		}

		// Get vertex
		retrieved, err := store.GetVertex(ctx, "vertices", vertex.ID)
		if err != nil {
			t.Fatalf("Failed to get vertex: %v", err)
		}
		if retrieved.ID != vertex.ID {
			t.Errorf("Expected vertex ID %s, got %s", vertex.ID, retrieved.ID)
		}
		if retrieved.Epoch != vertex.Epoch {
			t.Errorf("Expected epoch %d, got %d", vertex.Epoch, retrieved.Epoch)
		}
	})

	t.Run("EdgeStorage", func(t *testing.T) {
		edge := &Edge{
			Source:    "vertex-001",
			Target:    "vertex-002",
			Type:      "parent",
			CreatedAt: time.Now().Truncate(time.Second),
		}

		// Store edge
		if err := store.StoreEdge(ctx, "edges", edge); err != nil {
			t.Fatalf("Failed to store edge: %v", err)
		}

		// Get edges
		edges, err := store.GetEdges(ctx, "edges", edge.Source)
		if err != nil {
			t.Fatalf("Failed to get edges: %v", err)
		}
		if len(edges) != 1 {
			t.Errorf("Expected 1 edge, got %d", len(edges))
		}
	})

	t.Run("KVOperations", func(t *testing.T) {
		// Put
		if err := store.Put(ctx, "state", "key1", []byte("value1")); err != nil {
			t.Fatalf("Failed to put: %v", err)
		}

		// Get
		val, err := store.Get(ctx, "state", "key1")
		if err != nil {
			t.Fatalf("Failed to get: %v", err)
		}
		if string(val) != "value1" {
			t.Errorf("Expected 'value1', got '%s'", string(val))
		}

		// Delete
		if err := store.Delete(ctx, "state", "key1"); err != nil {
			t.Fatalf("Failed to delete: %v", err)
		}

		// Get after delete should fail
		_, err = store.Get(ctx, "state", "key1")
		if err != ErrNotFound {
			t.Errorf("Expected ErrNotFound after delete, got %v", err)
		}
	})

	t.Run("Stats", func(t *testing.T) {
		stats := map[string]interface{}{
			"blocks":   100,
			"vertices": 200,
		}

		// Update stats
		if err := store.UpdateStats(ctx, "test", stats); err != nil {
			t.Fatalf("Failed to update stats: %v", err)
		}

		// Get stats
		retrieved, err := store.GetStats(ctx, "test")
		if err != nil {
			t.Fatalf("Failed to get stats: %v", err)
		}
		if retrieved["blocks"] != float64(100) { // JSON numbers are float64
			t.Errorf("Expected blocks=100, got %v", retrieved["blocks"])
		}
	})

	t.Run("Transaction", func(t *testing.T) {
		tx, err := store.Begin(ctx)
		if err != nil {
			t.Fatalf("Failed to begin transaction: %v", err)
		}

		block := &Block{
			ID:        "tx-block-001",
			ParentID:  "genesis",
			Height:    100,
			Timestamp: time.Now().Truncate(time.Second),
			Status:    "pending",
			TxCount:   1,
			TxIDs:     []string{"tx1"},
			CreatedAt: time.Now().Truncate(time.Second),
		}

		if err := tx.StoreBlock(ctx, "blocks", block); err != nil {
			tx.Rollback()
			t.Fatalf("Failed to store block in tx: %v", err)
		}

		if err := tx.Commit(); err != nil {
			t.Fatalf("Failed to commit tx: %v", err)
		}

		// Verify block was stored
		retrieved, err := store.GetBlock(ctx, "blocks", block.ID)
		if err != nil {
			t.Fatalf("Failed to get block after commit: %v", err)
		}
		if retrieved.ID != block.ID {
			t.Errorf("Expected block ID %s, got %s", block.ID, retrieved.ID)
		}
	})

	t.Run("SQLQuery", func(t *testing.T) {
		// Count blocks
		count, err := store.Count(ctx, "blocks", "")
		if err != nil {
			t.Fatalf("Failed to count blocks: %v", err)
		}
		if count < 1 {
			t.Errorf("Expected at least 1 block, got %d", count)
		}

		// Custom query
		results, err := store.QuerySQL(ctx, "SELECT id, height FROM blocks ORDER BY height LIMIT 5")
		if err != nil {
			t.Fatalf("Failed to query: %v", err)
		}
		if len(results) < 1 {
			t.Errorf("Expected at least 1 result, got %d", len(results))
		}
	})
}

func TestUnifiedStoreImplementsStore(t *testing.T) {
	// This test verifies that Unified implements the Store interface
	var _ Store = (*Unified)(nil)
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultUnifiedConfig("/tmp/test-indexer")

	if cfg.KV.Path != "/tmp/test-indexer/kv" {
		t.Errorf("Expected KV path /tmp/test-indexer/kv, got %s", cfg.KV.Path)
	}
	if cfg.Query.Backend != query.BackendSQLite {
		t.Errorf("Expected SQLite backend, got %s", cfg.Query.Backend)
	}
	if !cfg.DualWrite {
		t.Error("Expected DualWrite to be true")
	}
}
