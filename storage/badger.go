// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/luxfi/database"
	"github.com/luxfi/database/badgerdb"
	"github.com/luxfi/database/prefixdb"
)

// BadgerStore implements Store using luxfi/database BadgerDB
type BadgerStore struct {
	db     database.Database
	cfg    Config
	mu     sync.RWMutex
	closed bool
}

// NewBadger creates a new BadgerDB storage backend using luxfi/database
func NewBadger(cfg Config) (*BadgerStore, error) {
	return &BadgerStore{
		cfg: cfg,
	}, nil
}

// Init initializes the BadgerDB connection
func (s *BadgerStore) Init(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dataDir := s.cfg.DataDir
	if dataDir == "" {
		dataDir = filepath.Join(".", "data", "badger")
	}

	// Build config bytes if options provided
	var configBytes []byte
	if len(s.cfg.Options) > 0 {
		configBytes, _ = json.Marshal(s.cfg.Options)
	}

	db, err := badgerdb.New(dataDir, configBytes, "indexer", nil)
	if err != nil {
		return fmt.Errorf("failed to open badger db: %w", err)
	}

	s.db = db
	return nil
}

// Close closes the BadgerDB connection
func (s *BadgerStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Ping checks if the database is accessible
func (s *BadgerStore) Ping(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed || s.db == nil {
		return ErrClosed
	}

	_, err := s.db.HealthCheck(ctx)
	return err
}

// InitSchema initializes the schema (no-op for BadgerDB as it's schemaless)
func (s *BadgerStore) InitSchema(ctx context.Context, schema Schema) error {
	// BadgerDB is schemaless, nothing to do
	return nil
}

// MigrateSchema migrates the schema (no-op for BadgerDB)
func (s *BadgerStore) MigrateSchema(ctx context.Context, schema Schema) error {
	// BadgerDB is schemaless, nothing to do
	return nil
}

// getPrefixDB returns a prefixed database for the given table
func (s *BadgerStore) getPrefixDB(table string) database.Database {
	return prefixdb.New([]byte(table+":"), s.db)
}

// key generates a key for the given id
func badgerKey(id string) []byte {
	return []byte(id)
}

// heightKey generates a key for height-based lookups
func badgerHeightKey(height uint64) []byte {
	return []byte(fmt.Sprintf("height:%020d", height))
}

// StoreBlock stores a block
func (s *BadgerStore) StoreBlock(ctx context.Context, table string, block *Block) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrClosed
	}

	data, err := json.Marshal(block)
	if err != nil {
		return fmt.Errorf("failed to marshal block: %w", err)
	}

	db := s.getPrefixDB(table)

	// Store by ID
	if err := db.Put(badgerKey(block.ID), data); err != nil {
		return err
	}

	// Store height -> ID mapping for height-based lookups
	return db.Put(badgerHeightKey(block.Height), []byte(block.ID))
}

// GetBlock retrieves a block by ID
func (s *BadgerStore) GetBlock(ctx context.Context, table string, id string) (*Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	db := s.getPrefixDB(table)
	data, err := db.Get(badgerKey(id))
	if err == database.ErrNotFound {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	var block Block
	if err := json.Unmarshal(data, &block); err != nil {
		return nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}

	return &block, nil
}

// GetBlockByHeight retrieves a block by height
func (s *BadgerStore) GetBlockByHeight(ctx context.Context, table string, height uint64) (*Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	db := s.getPrefixDB(table)

	// Get block ID from height index
	blockID, err := db.Get(badgerHeightKey(height))
	if err == database.ErrNotFound {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	// Get the block by ID (need to release lock temporarily)
	s.mu.RUnlock()
	defer s.mu.RLock()

	return s.GetBlock(ctx, table, string(blockID))
}

// GetRecentBlocks retrieves recent blocks
func (s *BadgerStore) GetRecentBlocks(ctx context.Context, table string, limit int) ([]*Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	db := s.getPrefixDB(table)
	iter := db.NewIterator()
	defer iter.Release()

	var blocks []*Block
	count := 0

	// Collect all blocks first (BadgerDB iterator doesn't support reverse)
	for iter.Next() && (limit == 0 || count < limit*10) {
		key := string(iter.Key())
		// Skip height index keys
		if len(key) > 7 && key[:7] == "height:" {
			continue
		}

		var block Block
		if err := json.Unmarshal(iter.Value(), &block); err != nil {
			continue
		}
		blocks = append(blocks, &block)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, err
	}

	// Sort by height descending and limit
	// Simple bubble sort for small datasets
	for i := 0; i < len(blocks)-1; i++ {
		for j := 0; j < len(blocks)-i-1; j++ {
			if blocks[j].Height < blocks[j+1].Height {
				blocks[j], blocks[j+1] = blocks[j+1], blocks[j]
			}
		}
	}

	if limit > 0 && len(blocks) > limit {
		blocks = blocks[:limit]
	}

	return blocks, nil
}

// StoreVertex stores a vertex
func (s *BadgerStore) StoreVertex(ctx context.Context, table string, vertex *Vertex) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrClosed
	}

	data, err := json.Marshal(vertex)
	if err != nil {
		return fmt.Errorf("failed to marshal vertex: %w", err)
	}

	db := s.getPrefixDB(table)
	return db.Put(badgerKey(vertex.ID), data)
}

// GetVertex retrieves a vertex by ID
func (s *BadgerStore) GetVertex(ctx context.Context, table string, id string) (*Vertex, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	db := s.getPrefixDB(table)
	data, err := db.Get(badgerKey(id))
	if err == database.ErrNotFound {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	var vertex Vertex
	if err := json.Unmarshal(data, &vertex); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vertex: %w", err)
	}

	return &vertex, nil
}

// GetRecentVertices retrieves recent vertices
func (s *BadgerStore) GetRecentVertices(ctx context.Context, table string, limit int) ([]*Vertex, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	db := s.getPrefixDB(table)
	iter := db.NewIterator()
	defer iter.Release()

	var vertices []*Vertex
	count := 0

	for iter.Next() && (limit == 0 || count < limit*10) {
		var vertex Vertex
		if err := json.Unmarshal(iter.Value(), &vertex); err != nil {
			continue
		}
		vertices = append(vertices, &vertex)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, err
	}

	// Sort by height descending and limit
	for i := 0; i < len(vertices)-1; i++ {
		for j := 0; j < len(vertices)-i-1; j++ {
			if vertices[j].Height < vertices[j+1].Height {
				vertices[j], vertices[j+1] = vertices[j+1], vertices[j]
			}
		}
	}

	if limit > 0 && len(vertices) > limit {
		vertices = vertices[:limit]
	}

	return vertices, nil
}

// StoreEdge stores an edge
func (s *BadgerStore) StoreEdge(ctx context.Context, table string, edge *Edge) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrClosed
	}

	data, err := json.Marshal(edge)
	if err != nil {
		return fmt.Errorf("failed to marshal edge: %w", err)
	}

	db := s.getPrefixDB(table)

	// Store edge by composite key
	edgeKey := fmt.Sprintf("%s:%s->%s", edge.Source, edge.Type, edge.Target)
	if err := db.Put([]byte(edgeKey), data); err != nil {
		return err
	}

	// Also index by source vertex for GetEdges
	indexKey := fmt.Sprintf("edges:%s:%s", edge.Source, edgeKey)
	return db.Put([]byte(indexKey), data)
}

// GetEdges retrieves edges for a vertex
func (s *BadgerStore) GetEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	db := s.getPrefixDB(table)
	prefix := []byte(fmt.Sprintf("edges:%s:", vertexID))
	iter := db.NewIteratorWithPrefix(prefix)
	defer iter.Release()

	var edges []*Edge
	for iter.Next() {
		var edge Edge
		if err := json.Unmarshal(iter.Value(), &edge); err != nil {
			continue
		}
		edges = append(edges, &edge)
	}

	if err := iter.Error(); err != nil {
		return nil, err
	}

	return edges, nil
}

// Put stores a key-value pair
func (s *BadgerStore) Put(ctx context.Context, table string, k string, value []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrClosed
	}

	db := s.getPrefixDB(table)
	return db.Put([]byte(k), value)
}

// Get retrieves a value by key
func (s *BadgerStore) Get(ctx context.Context, table string, k string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	db := s.getPrefixDB(table)
	data, err := db.Get([]byte(k))
	if err == database.ErrNotFound {
		return nil, ErrNotFound
	}
	return data, err
}

// Delete removes a key-value pair
func (s *BadgerStore) Delete(ctx context.Context, table string, k string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrClosed
	}

	db := s.getPrefixDB(table)
	return db.Delete([]byte(k))
}

// List retrieves key-value pairs with a prefix
func (s *BadgerStore) List(ctx context.Context, table string, prefix string, limit int) ([]KV, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	db := s.getPrefixDB(table)
	iter := db.NewIteratorWithPrefix([]byte(prefix))
	defer iter.Release()

	var results []KV
	count := 0

	for iter.Next() && (limit == 0 || count < limit) {
		results = append(results, KV{
			Key:   string(iter.Key()),
			Value: append([]byte{}, iter.Value()...),
		})
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, err
	}

	return results, nil
}

// UpdateStats updates statistics (stored as JSON)
func (s *BadgerStore) UpdateStats(ctx context.Context, table string, stats map[string]interface{}) error {
	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal stats: %w", err)
	}
	return s.Put(ctx, table, "_stats", data)
}

// GetStats retrieves statistics
func (s *BadgerStore) GetStats(ctx context.Context, table string) (map[string]interface{}, error) {
	data, err := s.Get(ctx, table, "_stats")
	if err == ErrNotFound {
		return make(map[string]interface{}), nil
	}
	if err != nil {
		return nil, err
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(data, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stats: %w", err)
	}
	return stats, nil
}

// Query executes a query (not supported for BadgerDB)
func (s *BadgerStore) Query(ctx context.Context, query string, args ...interface{}) ([]map[string]interface{}, error) {
	return nil, ErrNotSupported
}

// Exec executes a command (not supported for BadgerDB)
func (s *BadgerStore) Exec(ctx context.Context, query string, args ...interface{}) error {
	return ErrNotSupported
}

// Begin starts a transaction
func (s *BadgerStore) Begin(ctx context.Context) (Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	// luxfi/database uses batch operations for transaction-like behavior
	return &BadgerTransaction{
		store: s,
		batch: s.db.NewBatch(),
	}, nil
}

// BadgerTransaction implements Transaction for BadgerDB using batches
type BadgerTransaction struct {
	store *BadgerStore
	batch database.Batch
}

// Commit commits the transaction
func (t *BadgerTransaction) Commit() error {
	return t.batch.Write()
}

// Rollback rolls back the transaction
func (t *BadgerTransaction) Rollback() error {
	t.batch.Reset()
	return nil
}

// Transaction methods delegate to the store
func (t *BadgerTransaction) Init(ctx context.Context) error {
	return nil
}

func (t *BadgerTransaction) Close() error {
	return nil
}

func (t *BadgerTransaction) Ping(ctx context.Context) error {
	return t.store.Ping(ctx)
}

func (t *BadgerTransaction) InitSchema(ctx context.Context, schema Schema) error {
	return nil
}

func (t *BadgerTransaction) MigrateSchema(ctx context.Context, schema Schema) error {
	return nil
}

func (t *BadgerTransaction) StoreBlock(ctx context.Context, table string, block *Block) error {
	data, err := json.Marshal(block)
	if err != nil {
		return err
	}
	prefix := []byte(table + ":")
	if err := t.batch.Put(append(prefix, badgerKey(block.ID)...), data); err != nil {
		return err
	}
	return t.batch.Put(append(prefix, badgerHeightKey(block.Height)...), []byte(block.ID))
}

func (t *BadgerTransaction) GetBlock(ctx context.Context, table string, id string) (*Block, error) {
	return t.store.GetBlock(ctx, table, id)
}

func (t *BadgerTransaction) GetBlockByHeight(ctx context.Context, table string, height uint64) (*Block, error) {
	return t.store.GetBlockByHeight(ctx, table, height)
}

func (t *BadgerTransaction) GetRecentBlocks(ctx context.Context, table string, limit int) ([]*Block, error) {
	return t.store.GetRecentBlocks(ctx, table, limit)
}

func (t *BadgerTransaction) StoreVertex(ctx context.Context, table string, vertex *Vertex) error {
	data, err := json.Marshal(vertex)
	if err != nil {
		return err
	}
	prefix := []byte(table + ":")
	return t.batch.Put(append(prefix, badgerKey(vertex.ID)...), data)
}

func (t *BadgerTransaction) GetVertex(ctx context.Context, table string, id string) (*Vertex, error) {
	return t.store.GetVertex(ctx, table, id)
}

func (t *BadgerTransaction) GetRecentVertices(ctx context.Context, table string, limit int) ([]*Vertex, error) {
	return t.store.GetRecentVertices(ctx, table, limit)
}

func (t *BadgerTransaction) StoreEdge(ctx context.Context, table string, edge *Edge) error {
	data, err := json.Marshal(edge)
	if err != nil {
		return err
	}
	prefix := []byte(table + ":")
	edgeKey := fmt.Sprintf("%s:%s->%s", edge.Source, edge.Type, edge.Target)
	if err := t.batch.Put(append(prefix, []byte(edgeKey)...), data); err != nil {
		return err
	}
	indexKey := fmt.Sprintf("edges:%s:%s", edge.Source, edgeKey)
	return t.batch.Put(append(prefix, []byte(indexKey)...), data)
}

func (t *BadgerTransaction) GetEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	return t.store.GetEdges(ctx, table, vertexID)
}

func (t *BadgerTransaction) Put(ctx context.Context, table string, k string, value []byte) error {
	prefix := []byte(table + ":")
	return t.batch.Put(append(prefix, []byte(k)...), value)
}

func (t *BadgerTransaction) Get(ctx context.Context, table string, k string) ([]byte, error) {
	return t.store.Get(ctx, table, k)
}

func (t *BadgerTransaction) Delete(ctx context.Context, table string, k string) error {
	prefix := []byte(table + ":")
	return t.batch.Delete(append(prefix, []byte(k)...))
}

func (t *BadgerTransaction) List(ctx context.Context, table string, prefix string, limit int) ([]KV, error) {
	return t.store.List(ctx, table, prefix, limit)
}

func (t *BadgerTransaction) UpdateStats(ctx context.Context, table string, stats map[string]interface{}) error {
	data, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	prefix := []byte(table + ":")
	return t.batch.Put(append(prefix, []byte("_stats")...), data)
}

func (t *BadgerTransaction) GetStats(ctx context.Context, table string) (map[string]interface{}, error) {
	return t.store.GetStats(ctx, table)
}

func (t *BadgerTransaction) Query(ctx context.Context, query string, args ...interface{}) ([]map[string]interface{}, error) {
	return nil, ErrNotSupported
}

func (t *BadgerTransaction) Exec(ctx context.Context, query string, args ...interface{}) error {
	return ErrNotSupported
}

func (t *BadgerTransaction) Begin(ctx context.Context) (Transaction, error) {
	return nil, ErrNotSupported // Nested transactions not supported
}
