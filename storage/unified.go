// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package storage provides unified storage with two layers:
//   - KV Layer: Fast key-value storage using github.com/luxfi/database
//   - Query Layer: SQL/Graph queries for indexed data
//
// Default backend is SQLite. Use build tags for other backends:
//
//	go build -tags postgres
//	go build -tags mysql
//	go build -tags mongo
//	go build -tags dgraph
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/luxfi/database"
	"github.com/luxfi/indexer/storage/kv"
	"github.com/luxfi/indexer/storage/query"
)

// UnifiedConfig configures the unified storage
type UnifiedConfig struct {
	// KV layer config
	KV kv.Config

	// Query layer config
	Query query.Config

	// Enable dual-write to both layers
	DualWrite bool
}

// Unified combines KV and Query layers into a single interface
type Unified struct {
	kv    *kv.Store
	query query.Engine

	dualWrite bool

	mu     sync.RWMutex
	closed bool
}

// NewUnified creates a new unified storage
func NewUnified(cfg UnifiedConfig) (*Unified, error) {
	// Create KV layer
	kvStore, err := kv.New(cfg.KV)
	if err != nil {
		return nil, fmt.Errorf("failed to create kv store: %w", err)
	}

	// Create Query layer
	queryEngine, err := query.New(cfg.Query)
	if err != nil {
		kvStore.Close()
		return nil, fmt.Errorf("failed to create query engine: %w", err)
	}

	return &Unified{
		kv:        kvStore,
		query:     queryEngine,
		dualWrite: cfg.DualWrite,
	}, nil
}

// NewUnifiedWithDB creates unified storage using an existing database
// This enables in-process mode where the indexer shares the node's database
func NewUnifiedWithDB(db database.Database, queryCfg query.Config) (*Unified, error) {
	kvStore, err := kv.New(kv.Config{
		InProcess: true,
		NodeDB:    db,
		Prefix:    []byte("indexer:"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create kv store: %w", err)
	}

	queryEngine, err := query.New(queryCfg)
	if err != nil {
		kvStore.Close()
		return nil, fmt.Errorf("failed to create query engine: %w", err)
	}

	return &Unified{
		kv:        kvStore,
		query:     queryEngine,
		dualWrite: true,
	}, nil
}

// Init initializes both layers
func (u *Unified) Init(ctx context.Context) error {
	if err := u.query.Init(ctx); err != nil {
		return err
	}
	return u.query.InitSchema(ctx, query.DefaultSchema())
}

// Close closes both layers
func (u *Unified) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return nil
	}
	u.closed = true

	var errs []error
	if err := u.query.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := u.kv.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

// Ping checks both layers
func (u *Unified) Ping(ctx context.Context) error {
	if _, err := u.kv.HealthCheck(ctx); err != nil {
		return err
	}
	return u.query.Ping(ctx)
}

// KV returns the KV layer for direct access
func (u *Unified) KV() *kv.Store {
	return u.kv
}

// QueryEngine returns the Query layer for direct access
func (u *Unified) QueryEngine() query.Engine {
	return u.query
}

// Database returns the underlying luxfi/database.Database
func (u *Unified) Database() database.Database {
	return u.kv.Database()
}

// Backend returns the query backend type
func (u *Unified) Backend() Backend {
	return Backend(u.query.Backend())
}

// StoreBlock stores a block in both layers
func (u *Unified) StoreBlock(ctx context.Context, table string, block *Block) error {
	// Store in KV layer (fast lookup by ID)
	blockBytes, err := json.Marshal(block)
	if err != nil {
		return err
	}
	if err := u.kv.Blocks().Put([]byte(block.ID), blockBytes); err != nil {
		return err
	}

	// Store height -> ID mapping
	heightKey := kv.HeightKey(block.Height)
	if err := u.kv.Blocks().Put(kv.CompositeKey([]byte("height"), heightKey), []byte(block.ID)); err != nil {
		return err
	}

	// Store in Query layer (indexed queries)
	if u.dualWrite {
		qb := &query.Block{
			ID:        block.ID,
			ParentID:  block.ParentID,
			Height:    block.Height,
			Timestamp: block.Timestamp,
			Status:    block.Status,
			TxCount:   block.TxCount,
			TxIDs:     block.TxIDs,
			Data:      block.Data,
			Metadata:  block.Metadata,
			CreatedAt: block.CreatedAt,
		}
		return u.query.InsertBlock(ctx, table, qb)
	}
	return nil
}

// GetBlock retrieves a block (tries KV first, falls back to Query)
func (u *Unified) GetBlock(ctx context.Context, table string, id string) (*Block, error) {
	// Try KV layer first (fastest)
	blockBytes, err := u.kv.Blocks().Get([]byte(id))
	if err == nil {
		var block Block
		if err := json.Unmarshal(blockBytes, &block); err == nil {
			return &block, nil
		}
	}

	// Fall back to Query layer
	qb, err := u.query.GetBlock(ctx, table, id)
	if err != nil {
		if err == query.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &Block{
		ID:        qb.ID,
		ParentID:  qb.ParentID,
		Height:    qb.Height,
		Timestamp: qb.Timestamp,
		Status:    qb.Status,
		TxCount:   qb.TxCount,
		TxIDs:     qb.TxIDs,
		Data:      qb.Data,
		Metadata:  qb.Metadata,
		CreatedAt: qb.CreatedAt,
	}, nil
}

// GetBlockByHeight retrieves a block by height
func (u *Unified) GetBlockByHeight(ctx context.Context, table string, height uint64) (*Block, error) {
	// Try KV layer first
	heightKey := kv.HeightKey(height)
	idBytes, err := u.kv.Blocks().Get(kv.CompositeKey([]byte("height"), heightKey))
	if err == nil {
		return u.GetBlock(ctx, table, string(idBytes))
	}

	// Fall back to Query layer
	qb, err := u.query.GetBlockByHeight(ctx, table, height)
	if err != nil {
		if err == query.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &Block{
		ID:        qb.ID,
		ParentID:  qb.ParentID,
		Height:    qb.Height,
		Timestamp: qb.Timestamp,
		Status:    qb.Status,
		TxCount:   qb.TxCount,
		TxIDs:     qb.TxIDs,
		Data:      qb.Data,
		Metadata:  qb.Metadata,
		CreatedAt: qb.CreatedAt,
	}, nil
}

// GetRecentBlocks retrieves recent blocks (query layer only)
func (u *Unified) GetRecentBlocks(ctx context.Context, table string, limit int) ([]*Block, error) {
	qBlocks, err := u.query.GetRecentBlocks(ctx, table, limit)
	if err != nil {
		return nil, err
	}

	blocks := make([]*Block, len(qBlocks))
	for i, qb := range qBlocks {
		blocks[i] = &Block{
			ID:        qb.ID,
			ParentID:  qb.ParentID,
			Height:    qb.Height,
			Timestamp: qb.Timestamp,
			Status:    qb.Status,
			TxCount:   qb.TxCount,
			TxIDs:     qb.TxIDs,
			Data:      qb.Data,
			Metadata:  qb.Metadata,
			CreatedAt: qb.CreatedAt,
		}
	}
	return blocks, nil
}

// StoreVertex stores a vertex in both layers
func (u *Unified) StoreVertex(ctx context.Context, table string, vertex *Vertex) error {
	// Store in KV layer
	vertexBytes, err := json.Marshal(vertex)
	if err != nil {
		return err
	}
	if err := u.kv.Vertices().Put([]byte(vertex.ID), vertexBytes); err != nil {
		return err
	}

	// Store in Query layer
	if u.dualWrite {
		qv := &query.Vertex{
			ID:        vertex.ID,
			Type:      vertex.Type,
			ParentIDs: vertex.ParentIDs,
			Height:    vertex.Height,
			Epoch:     vertex.Epoch,
			TxIDs:     vertex.TxIDs,
			Timestamp: vertex.Timestamp,
			Status:    vertex.Status,
			Data:      vertex.Data,
			Metadata:  vertex.Metadata,
			CreatedAt: vertex.CreatedAt,
		}
		return u.query.InsertVertex(ctx, table, qv)
	}
	return nil
}

// GetVertex retrieves a vertex
func (u *Unified) GetVertex(ctx context.Context, table string, id string) (*Vertex, error) {
	// Try KV layer first
	vertexBytes, err := u.kv.Vertices().Get([]byte(id))
	if err == nil {
		var vertex Vertex
		if err := json.Unmarshal(vertexBytes, &vertex); err == nil {
			return &vertex, nil
		}
	}

	// Fall back to Query layer
	qv, err := u.query.GetVertex(ctx, table, id)
	if err != nil {
		if err == query.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &Vertex{
		ID:        qv.ID,
		Type:      qv.Type,
		ParentIDs: qv.ParentIDs,
		Height:    qv.Height,
		Epoch:     qv.Epoch,
		TxIDs:     qv.TxIDs,
		Timestamp: qv.Timestamp,
		Status:    qv.Status,
		Data:      qv.Data,
		Metadata:  qv.Metadata,
		CreatedAt: qv.CreatedAt,
	}, nil
}

// GetRecentVertices retrieves recent vertices
func (u *Unified) GetRecentVertices(ctx context.Context, table string, limit int) ([]*Vertex, error) {
	qVertices, err := u.query.GetRecentVertices(ctx, table, limit)
	if err != nil {
		return nil, err
	}

	vertices := make([]*Vertex, len(qVertices))
	for i, qv := range qVertices {
		vertices[i] = &Vertex{
			ID:        qv.ID,
			Type:      qv.Type,
			ParentIDs: qv.ParentIDs,
			Height:    qv.Height,
			Epoch:     qv.Epoch,
			TxIDs:     qv.TxIDs,
			Timestamp: qv.Timestamp,
			Status:    qv.Status,
			Data:      qv.Data,
			Metadata:  qv.Metadata,
			CreatedAt: qv.CreatedAt,
		}
	}
	return vertices, nil
}

// StoreEdge stores an edge
func (u *Unified) StoreEdge(ctx context.Context, table string, edge *Edge) error {
	// Store in KV layer
	edgeKey := kv.CompositeKey([]byte(edge.Source), []byte(edge.Target))
	edgeBytes, err := json.Marshal(edge)
	if err != nil {
		return err
	}
	if err := u.kv.Edges().Put(edgeKey, edgeBytes); err != nil {
		return err
	}

	// Store in Query layer
	if u.dualWrite {
		qe := &query.Edge{
			Source:    edge.Source,
			Target:    edge.Target,
			Type:      edge.Type,
			CreatedAt: edge.CreatedAt,
		}
		return u.query.InsertEdge(ctx, table, qe)
	}
	return nil
}

// GetEdges retrieves edges from a vertex
func (u *Unified) GetEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	qEdges, err := u.query.GetEdges(ctx, table, vertexID)
	if err != nil {
		return nil, err
	}

	edges := make([]*Edge, len(qEdges))
	for i, qe := range qEdges {
		edges[i] = &Edge{
			Source:    qe.Source,
			Target:    qe.Target,
			Type:      qe.Type,
			CreatedAt: qe.CreatedAt,
		}
	}
	return edges, nil
}

// Put stores a raw key-value in the KV layer
func (u *Unified) Put(ctx context.Context, table string, key string, value []byte) error {
	return u.kv.State().Put(kv.CompositeKey([]byte(table), []byte(key)), value)
}

// Get retrieves a raw key-value from the KV layer
func (u *Unified) Get(ctx context.Context, table string, key string) ([]byte, error) {
	val, err := u.kv.State().Get(kv.CompositeKey([]byte(table), []byte(key)))
	if err == database.ErrNotFound {
		return nil, ErrNotFound
	}
	return val, err
}

// Delete removes a key from the KV layer
func (u *Unified) Delete(ctx context.Context, table string, key string) error {
	return u.kv.State().Delete(kv.CompositeKey([]byte(table), []byte(key)))
}

// List lists keys from the KV layer
func (u *Unified) List(ctx context.Context, table string, prefix string, limit int) ([]KV, error) {
	fullPrefix := kv.CompositeKey([]byte(table), []byte(prefix))
	iter := u.kv.State().NewIteratorWithPrefix(fullPrefix)
	defer iter.Release()

	var results []KV
	for iter.Next() && (limit <= 0 || len(results) < limit) {
		// Remove the table prefix from the key
		key := iter.Key()[len(table)+1:]
		results = append(results, KV{
			Key:   string(key),
			Value: iter.Value(),
		})
	}
	return results, iter.Error()
}

// UpdateStats updates stats in the KV layer
func (u *Unified) UpdateStats(ctx context.Context, table string, stats map[string]interface{}) error {
	statsBytes, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	return u.kv.Meta().Put([]byte("stats:"+table), statsBytes)
}

// GetStats retrieves stats from the KV layer
func (u *Unified) GetStats(ctx context.Context, table string) (map[string]interface{}, error) {
	statsBytes, err := u.kv.Meta().Get([]byte("stats:" + table))
	if err == database.ErrNotFound {
		return make(map[string]interface{}), nil
	}
	if err != nil {
		return nil, err
	}
	var stats map[string]interface{}
	if err := json.Unmarshal(statsBytes, &stats); err != nil {
		return nil, err
	}
	return stats, nil
}

// Query runs a SQL/Graph query
func (u *Unified) QuerySQL(ctx context.Context, q string, args ...interface{}) ([]map[string]interface{}, error) {
	rows, err := u.query.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	results := make([]map[string]interface{}, len(rows))
	for i, row := range rows {
		results[i] = map[string]interface{}(row)
	}
	return results, nil
}

// Exec executes a SQL command
func (u *Unified) ExecSQL(ctx context.Context, q string, args ...interface{}) error {
	_, err := u.query.Exec(ctx, q, args...)
	return err
}

// Count counts records
func (u *Unified) Count(ctx context.Context, table string, where string, args ...interface{}) (int64, error) {
	return u.query.Count(ctx, table, where, args...)
}

// Begin starts a transaction that satisfies the Store interface
func (u *Unified) Begin(ctx context.Context) (Transaction, error) {
	tx, err := u.query.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &unifiedStoreTx{u: u, tx: &UnifiedTx{kv: u.kv, query: tx}}, nil
}

// BeginTx starts a transaction returning the typed UnifiedTx
func (u *Unified) BeginTx(ctx context.Context) (*UnifiedTx, error) {
	tx, err := u.query.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &UnifiedTx{
		kv:    u.kv,
		query: tx,
	}, nil
}

// UnifiedTx is a transaction over the unified store
type UnifiedTx struct {
	kv    *kv.Store
	query query.Tx
}

func (t *UnifiedTx) Commit() error   { return t.query.Commit() }
func (t *UnifiedTx) Rollback() error { return t.query.Rollback() }

func (t *UnifiedTx) StoreBlock(ctx context.Context, table string, block *Block) error {
	qb := &query.Block{
		ID:        block.ID,
		ParentID:  block.ParentID,
		Height:    block.Height,
		Timestamp: block.Timestamp,
		Status:    block.Status,
		TxCount:   block.TxCount,
		TxIDs:     block.TxIDs,
		Data:      block.Data,
		Metadata:  block.Metadata,
		CreatedAt: block.CreatedAt,
	}
	return t.query.InsertBlock(ctx, table, qb)
}

func (t *UnifiedTx) StoreVertex(ctx context.Context, table string, vertex *Vertex) error {
	qv := &query.Vertex{
		ID:        vertex.ID,
		Type:      vertex.Type,
		ParentIDs: vertex.ParentIDs,
		Height:    vertex.Height,
		Epoch:     vertex.Epoch,
		TxIDs:     vertex.TxIDs,
		Timestamp: vertex.Timestamp,
		Status:    vertex.Status,
		Data:      vertex.Data,
		Metadata:  vertex.Metadata,
		CreatedAt: vertex.CreatedAt,
	}
	return t.query.InsertVertex(ctx, table, qv)
}

// Example usage for in-process mode:
//
//	// In the node's VM initialization:
//	nodeDB := badgerdb.New(path, nil, nil)
//
//	// Create unified storage sharing the node's database
//	storage, err := storage.NewUnifiedWithDB(nodeDB, query.Config{
//	    Backend: query.BackendSQLite,
//	    DataDir: "/path/to/indexer",
//	})
//
//	// The indexer now shares the node's database for KV operations
//	// while maintaining its own SQLite for indexed queries

// Wrapper methods to satisfy the Store interface

func (u *Unified) InitSchema(ctx context.Context, schema Schema) error {
	qs := query.Schema{
		Name:    schema.Name,
		Tables:  make([]query.Table, len(schema.Tables)),
		Indexes: make([]query.Index, len(schema.Indexes)),
	}
	for i, t := range schema.Tables {
		cols := make([]query.Column, len(t.Columns))
		for j, c := range t.Columns {
			cols[j] = query.Column{
				Name:     c.Name,
				Type:     query.ColumnType(c.Type),
				Nullable: c.Nullable,
				Default:  c.Default,
				Primary:  c.Primary,
			}
		}
		qs.Tables[i] = query.Table{Name: t.Name, Columns: cols}
	}
	for i, idx := range schema.Indexes {
		qs.Indexes[i] = query.Index{
			Name:    idx.Name,
			Table:   idx.Table,
			Columns: idx.Columns,
			Unique:  idx.Unique,
		}
	}
	return u.query.InitSchema(ctx, qs)
}

func (u *Unified) MigrateSchema(ctx context.Context, schema Schema) error {
	qs := query.Schema{Name: schema.Name}
	return u.query.Migrate(ctx, qs)
}

func (u *Unified) Query(ctx context.Context, q string, args ...interface{}) ([]map[string]interface{}, error) {
	return u.QuerySQL(ctx, q, args...)
}

func (u *Unified) Exec(ctx context.Context, q string, args ...interface{}) error {
	return u.ExecSQL(ctx, q, args...)
}

// BeginStore begins a transaction that implements the Store interface
// Deprecated: Use Begin instead
func (u *Unified) BeginStore(ctx context.Context) (Transaction, error) {
	return u.Begin(ctx)
}

type unifiedStoreTx struct {
	u  *Unified
	tx *UnifiedTx
}

func (t *unifiedStoreTx) Backend() Backend               { return t.u.Backend() }
func (t *unifiedStoreTx) Init(ctx context.Context) error { return nil }
func (t *unifiedStoreTx) Close() error                   { return t.Rollback() }
func (t *unifiedStoreTx) Ping(ctx context.Context) error { return nil }
func (t *unifiedStoreTx) Commit() error                  { return t.tx.Commit() }
func (t *unifiedStoreTx) Rollback() error                { return t.tx.Rollback() }

func (t *unifiedStoreTx) InitSchema(ctx context.Context, schema Schema) error {
	return ErrNotSupported
}

func (t *unifiedStoreTx) MigrateSchema(ctx context.Context, schema Schema) error {
	return ErrNotSupported
}

func (t *unifiedStoreTx) StoreBlock(ctx context.Context, table string, block *Block) error {
	return t.tx.StoreBlock(ctx, table, block)
}

func (t *unifiedStoreTx) GetBlock(ctx context.Context, table string, id string) (*Block, error) {
	return t.u.GetBlock(ctx, table, id)
}

func (t *unifiedStoreTx) GetBlockByHeight(ctx context.Context, table string, height uint64) (*Block, error) {
	return t.u.GetBlockByHeight(ctx, table, height)
}

func (t *unifiedStoreTx) GetRecentBlocks(ctx context.Context, table string, limit int) ([]*Block, error) {
	return t.u.GetRecentBlocks(ctx, table, limit)
}

func (t *unifiedStoreTx) StoreVertex(ctx context.Context, table string, vertex *Vertex) error {
	return t.tx.StoreVertex(ctx, table, vertex)
}

func (t *unifiedStoreTx) GetVertex(ctx context.Context, table string, id string) (*Vertex, error) {
	return t.u.GetVertex(ctx, table, id)
}

func (t *unifiedStoreTx) GetRecentVertices(ctx context.Context, table string, limit int) ([]*Vertex, error) {
	return t.u.GetRecentVertices(ctx, table, limit)
}

func (t *unifiedStoreTx) StoreEdge(ctx context.Context, table string, edge *Edge) error {
	return t.u.StoreEdge(ctx, table, edge)
}

func (t *unifiedStoreTx) GetEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	return t.u.GetEdges(ctx, table, vertexID)
}

func (t *unifiedStoreTx) Put(ctx context.Context, table string, key string, value []byte) error {
	return t.u.Put(ctx, table, key, value)
}

func (t *unifiedStoreTx) Get(ctx context.Context, table string, key string) ([]byte, error) {
	return t.u.Get(ctx, table, key)
}

func (t *unifiedStoreTx) Delete(ctx context.Context, table string, key string) error {
	return t.u.Delete(ctx, table, key)
}

func (t *unifiedStoreTx) List(ctx context.Context, table string, prefix string, limit int) ([]KV, error) {
	return t.u.List(ctx, table, prefix, limit)
}

func (t *unifiedStoreTx) UpdateStats(ctx context.Context, table string, stats map[string]interface{}) error {
	return t.u.UpdateStats(ctx, table, stats)
}

func (t *unifiedStoreTx) GetStats(ctx context.Context, table string) (map[string]interface{}, error) {
	return t.u.GetStats(ctx, table)
}

func (t *unifiedStoreTx) Query(ctx context.Context, q string, args ...interface{}) ([]map[string]interface{}, error) {
	return t.u.QuerySQL(ctx, q, args...)
}

func (t *unifiedStoreTx) Exec(ctx context.Context, q string, args ...interface{}) error {
	return t.u.Exec(ctx, q, args...)
}

func (t *unifiedStoreTx) Begin(ctx context.Context) (Transaction, error) {
	return nil, ErrNotSupported
}

func (t *unifiedStoreTx) Count(ctx context.Context, table string, where string, args ...interface{}) (int64, error) {
	return t.u.Count(ctx, table, where, args...)
}

var _ Transaction = (*unifiedStoreTx)(nil)

// DefaultUnifiedConfig returns the default configuration
func DefaultUnifiedConfig(dataDir string) UnifiedConfig {
	return UnifiedConfig{
		KV: kv.Config{
			Path: dataDir + "/kv",
		},
		Query: query.Config{
			Backend: query.BackendSQLite,
			DataDir: dataDir + "/query",
		},
		DualWrite: true,
	}
}

// InProcessConfig returns config for in-process mode with a node database
func InProcessConfig(nodeDB database.Database, dataDir string) UnifiedConfig {
	return UnifiedConfig{
		KV: kv.Config{
			InProcess: true,
			NodeDB:    nodeDB,
			Prefix:    []byte("indexer:"),
		},
		Query: query.Config{
			Backend: query.BackendSQLite,
			DataDir: dataDir,
		},
		DualWrite: true,
	}
}
