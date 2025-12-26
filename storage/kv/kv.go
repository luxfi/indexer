// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package kv provides a unified key-value storage layer using github.com/luxfi/database.
// This allows the indexer to share the same database interface as the Lux node,
// enabling in-process mode where indexer can use the node's BadgerDB directly.
package kv

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/luxfi/database"
	"github.com/luxfi/database/badgerdb"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/prefixdb"
)

// Prefixes for different data types in the KV store
var (
	PrefixBlocks   = []byte("blk:")
	PrefixVertices = []byte("vtx:")
	PrefixEdges    = []byte("edg:")
	PrefixTxs      = []byte("tx:")
	PrefixState    = []byte("st:")
	PrefixMeta     = []byte("meta:")
	PrefixIndex    = []byte("idx:")
)

// Config for the KV store
type Config struct {
	// Path to the database directory (for file-based backends)
	Path string

	// InProcess enables sharing the node's database
	InProcess bool

	// NodeDB is the node's database (only used when InProcess is true)
	NodeDB database.Database

	// Prefix to use for indexer data (to avoid conflicts with node data)
	Prefix []byte
}

// Store wraps a luxfi/database.Database with indexer-specific functionality
type Store struct {
	db     database.Database
	prefix []byte
	owned  bool // whether we own the db and should close it

	// Prefixed databases for different data types
	blocks   database.Database
	vertices database.Database
	edges    database.Database
	txs      database.Database
	state    database.Database
	meta     database.Database
	indexes  database.Database

	mu     sync.RWMutex
	closed bool
}

// New creates a new KV store
func New(cfg Config) (*Store, error) {
	var db database.Database
	var owned bool

	if cfg.InProcess && cfg.NodeDB != nil {
		// Use the node's database with a prefix to avoid conflicts
		prefix := cfg.Prefix
		if len(prefix) == 0 {
			prefix = []byte("indexer:")
		}
		db = prefixdb.New(prefix, cfg.NodeDB)
		owned = false
	} else {
		// Create our own database
		var err error
		db, err = badgerdb.New(cfg.Path, nil, "", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to open badgerdb: %w", err)
		}
		owned = true
	}

	s := &Store{
		db:     db,
		prefix: cfg.Prefix,
		owned:  owned,
	}

	// Create prefixed databases for each data type
	s.blocks = prefixdb.New(PrefixBlocks, db)
	s.vertices = prefixdb.New(PrefixVertices, db)
	s.edges = prefixdb.New(PrefixEdges, db)
	s.txs = prefixdb.New(PrefixTxs, db)
	s.state = prefixdb.New(PrefixState, db)
	s.meta = prefixdb.New(PrefixMeta, db)
	s.indexes = prefixdb.New(PrefixIndex, db)

	return s, nil
}

// NewMemory creates an in-memory KV store (for testing)
func NewMemory() *Store {
	db := memdb.New()
	return &Store{
		db:       db,
		owned:    true,
		blocks:   prefixdb.New(PrefixBlocks, db),
		vertices: prefixdb.New(PrefixVertices, db),
		edges:    prefixdb.New(PrefixEdges, db),
		txs:      prefixdb.New(PrefixTxs, db),
		state:    prefixdb.New(PrefixState, db),
		meta:     prefixdb.New(PrefixMeta, db),
		indexes:  prefixdb.New(PrefixIndex, db),
	}
}

// Database returns the underlying database
func (s *Store) Database() database.Database {
	return s.db
}

// Blocks returns the blocks database
func (s *Store) Blocks() database.Database {
	return s.blocks
}

// Vertices returns the vertices database
func (s *Store) Vertices() database.Database {
	return s.vertices
}

// Edges returns the edges database
func (s *Store) Edges() database.Database {
	return s.edges
}

// Txs returns the transactions database
func (s *Store) Txs() database.Database {
	return s.txs
}

// State returns the state database
func (s *Store) State() database.Database {
	return s.state
}

// Meta returns the metadata database
func (s *Store) Meta() database.Database {
	return s.meta
}

// Indexes returns the indexes database
func (s *Store) Indexes() database.Database {
	return s.indexes
}

// Put stores a value
func (s *Store) Put(key, value []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return database.ErrClosed
	}
	return s.db.Put(key, value)
}

// Get retrieves a value
func (s *Store) Get(key []byte) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, database.ErrClosed
	}
	return s.db.Get(key)
}

// Has checks if a key exists
func (s *Store) Has(key []byte) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return false, database.ErrClosed
	}
	return s.db.Has(key)
}

// Delete removes a key
func (s *Store) Delete(key []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return database.ErrClosed
	}
	return s.db.Delete(key)
}

// NewBatch creates a new batch
func (s *Store) NewBatch() database.Batch {
	return s.db.NewBatch()
}

// NewIterator creates a new iterator
func (s *Store) NewIterator() database.Iterator {
	return s.db.NewIterator()
}

// NewIteratorWithPrefix creates an iterator with a prefix
func (s *Store) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return s.db.NewIteratorWithPrefix(prefix)
}

// Compact compacts the database
func (s *Store) Compact(start, limit []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return database.ErrClosed
	}
	return s.db.Compact(start, limit)
}

// HealthCheck performs a health check
func (s *Store) HealthCheck(ctx context.Context) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, database.ErrClosed
	}
	return s.db.HealthCheck(ctx)
}

// Close closes the store
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true

	// Only close if we own the database
	if s.owned {
		return s.db.Close()
	}
	return nil
}

// HeightKey creates a key for height-based lookups
func HeightKey(height uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, height)
	return key
}

// ParseHeightKey parses a height key
func ParseHeightKey(key []byte) (uint64, error) {
	if len(key) < 8 {
		return 0, fmt.Errorf("key too short: %d", len(key))
	}
	return binary.BigEndian.Uint64(key), nil
}

// CompositeKey creates a composite key from multiple parts
func CompositeKey(parts ...[]byte) []byte {
	size := 0
	for _, p := range parts {
		size += len(p) + 1 // +1 for separator
	}
	key := make([]byte, 0, size)
	for i, p := range parts {
		if i > 0 {
			key = append(key, ':')
		}
		key = append(key, p...)
	}
	return key
}
