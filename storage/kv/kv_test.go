// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package kv

import (
	"context"
	"testing"
)

func TestMemoryStore(t *testing.T) {
	store := NewMemory()
	defer store.Close()

	ctx := context.Background()

	t.Run("PutGet", func(t *testing.T) {
		key := []byte("test-key")
		value := []byte("test-value")

		// Put
		if err := store.Put(key, value); err != nil {
			t.Fatalf("Failed to put: %v", err)
		}

		// Get
		retrieved, err := store.Get(key)
		if err != nil {
			t.Fatalf("Failed to get: %v", err)
		}
		if string(retrieved) != string(value) {
			t.Errorf("Expected '%s', got '%s'", string(value), string(retrieved))
		}
	})

	t.Run("Has", func(t *testing.T) {
		key := []byte("has-key")
		value := []byte("has-value")

		// Has before put
		has, err := store.Has(key)
		if err != nil {
			t.Fatalf("Failed to check has: %v", err)
		}
		if has {
			t.Error("Expected key to not exist")
		}

		// Put
		if err := store.Put(key, value); err != nil {
			t.Fatalf("Failed to put: %v", err)
		}

		// Has after put
		has, err = store.Has(key)
		if err != nil {
			t.Fatalf("Failed to check has: %v", err)
		}
		if !has {
			t.Error("Expected key to exist")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		key := []byte("delete-key")
		value := []byte("delete-value")

		// Put
		if err := store.Put(key, value); err != nil {
			t.Fatalf("Failed to put: %v", err)
		}

		// Delete
		if err := store.Delete(key); err != nil {
			t.Fatalf("Failed to delete: %v", err)
		}

		// Check doesn't exist
		has, err := store.Has(key)
		if err != nil {
			t.Fatalf("Failed to check has: %v", err)
		}
		if has {
			t.Error("Expected key to not exist after delete")
		}
	})

	t.Run("PrefixedDatabases", func(t *testing.T) {
		// Store in blocks
		blockKey := []byte("block-1")
		blockValue := []byte(`{"id": "block-1"}`)
		if err := store.Blocks().Put(blockKey, blockValue); err != nil {
			t.Fatalf("Failed to put block: %v", err)
		}

		// Store in vertices
		vertexKey := []byte("vertex-1")
		vertexValue := []byte(`{"id": "vertex-1"}`)
		if err := store.Vertices().Put(vertexKey, vertexValue); err != nil {
			t.Fatalf("Failed to put vertex: %v", err)
		}

		// Verify isolation
		retrieved, err := store.Blocks().Get(blockKey)
		if err != nil {
			t.Fatalf("Failed to get block: %v", err)
		}
		if string(retrieved) != string(blockValue) {
			t.Error("Block value mismatch")
		}

		retrieved, err = store.Vertices().Get(vertexKey)
		if err != nil {
			t.Fatalf("Failed to get vertex: %v", err)
		}
		if string(retrieved) != string(vertexValue) {
			t.Error("Vertex value mismatch")
		}
	})

	t.Run("Batch", func(t *testing.T) {
		batch := store.NewBatch()

		// Add operations
		for i := 0; i < 10; i++ {
			key := []byte("batch-key-" + string(rune('0'+i)))
			value := []byte("batch-value-" + string(rune('0'+i)))
			if err := batch.Put(key, value); err != nil {
				t.Fatalf("Failed to put in batch: %v", err)
			}
		}

		// Write batch
		if err := batch.Write(); err != nil {
			t.Fatalf("Failed to write batch: %v", err)
		}

		// Verify
		for i := 0; i < 10; i++ {
			key := []byte("batch-key-" + string(rune('0'+i)))
			has, err := store.Has(key)
			if err != nil {
				t.Fatalf("Failed to check has: %v", err)
			}
			if !has {
				t.Errorf("Expected key %s to exist", string(key))
			}
		}
	})

	t.Run("HealthCheck", func(t *testing.T) {
		_, err := store.HealthCheck(ctx)
		if err != nil {
			t.Fatalf("Health check failed: %v", err)
		}
	})
}

func TestHeightKey(t *testing.T) {
	tests := []uint64{0, 1, 100, 1000000, ^uint64(0)}

	for _, height := range tests {
		key := HeightKey(height)
		parsed, err := ParseHeightKey(key)
		if err != nil {
			t.Fatalf("Failed to parse height key for %d: %v", height, err)
		}
		if parsed != height {
			t.Errorf("Expected %d, got %d", height, parsed)
		}
	}
}

func TestCompositeKey(t *testing.T) {
	key := CompositeKey([]byte("table"), []byte("key"))
	expected := "table:key"
	if string(key) != expected {
		t.Errorf("Expected '%s', got '%s'", expected, string(key))
	}

	key = CompositeKey([]byte("a"), []byte("b"), []byte("c"))
	expected = "a:b:c"
	if string(key) != expected {
		t.Errorf("Expected '%s', got '%s'", expected, string(key))
	}
}
