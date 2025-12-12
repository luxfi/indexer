// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package storage provides a pluggable storage interface for the LUX indexer.
// Supported backends: PostgreSQL (default), BadgerDB, Dgraph
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Backend identifies the storage backend type
type Backend string

const (
	BackendPostgres Backend = "postgres"
	BackendBadger   Backend = "badger"
	BackendDgraph   Backend = "dgraph"
)

// Config for storage backend
type Config struct {
	Backend  Backend
	URL      string            // Connection URL (postgres://, badger://, dgraph://)
	Options  map[string]string // Backend-specific options
	DataDir  string            // For file-based backends (BadgerDB)
}

// Store is the main storage interface for indexed data
type Store interface {
	// Lifecycle
	Init(ctx context.Context) error
	Close() error
	Ping(ctx context.Context) error

	// Schema management
	InitSchema(ctx context.Context, schema Schema) error
	MigrateSchema(ctx context.Context, schema Schema) error

	// Block/Vertex storage (for chain/dag indexers)
	StoreBlock(ctx context.Context, table string, block *Block) error
	GetBlock(ctx context.Context, table string, id string) (*Block, error)
	GetBlockByHeight(ctx context.Context, table string, height uint64) (*Block, error)
	GetRecentBlocks(ctx context.Context, table string, limit int) ([]*Block, error)

	StoreVertex(ctx context.Context, table string, vertex *Vertex) error
	GetVertex(ctx context.Context, table string, id string) (*Vertex, error)
	GetRecentVertices(ctx context.Context, table string, limit int) ([]*Vertex, error)

	// Edge storage (for DAG)
	StoreEdge(ctx context.Context, table string, edge *Edge) error
	GetEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error)

	// Generic key-value storage
	Put(ctx context.Context, table string, key string, value []byte) error
	Get(ctx context.Context, table string, key string) ([]byte, error)
	Delete(ctx context.Context, table string, key string) error
	List(ctx context.Context, table string, prefix string, limit int) ([]KV, error)

	// Stats
	UpdateStats(ctx context.Context, table string, stats map[string]interface{}) error
	GetStats(ctx context.Context, table string) (map[string]interface{}, error)

	// Queries
	Query(ctx context.Context, query string, args ...interface{}) ([]map[string]interface{}, error)
	Exec(ctx context.Context, query string, args ...interface{}) error

	// Transaction support (optional, may return ErrNotSupported)
	Begin(ctx context.Context) (Transaction, error)
}

// Transaction represents a storage transaction
type Transaction interface {
	Store
	Commit() error
	Rollback() error
}

// Block represents a linear chain block for storage
type Block struct {
	ID        string          `json:"id"`
	ParentID  string          `json:"parent_id"`
	Height    uint64          `json:"height"`
	Timestamp time.Time       `json:"timestamp"`
	Status    string          `json:"status"`
	TxCount   int             `json:"tx_count"`
	TxIDs     []string        `json:"tx_ids"`
	Data      json.RawMessage `json:"data"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"created_at"`
}

// Vertex represents a DAG vertex for storage
type Vertex struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	ParentIDs []string        `json:"parent_ids"`
	Height    uint64          `json:"height"`
	Epoch     uint32          `json:"epoch"`
	TxIDs     []string        `json:"tx_ids"`
	Timestamp time.Time       `json:"timestamp"`
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"created_at"`
}

// Edge represents a DAG edge for storage
type Edge struct {
	Source    string    `json:"source"`
	Target    string    `json:"target"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

// KV represents a key-value pair
type KV struct {
	Key   string
	Value []byte
}

// Schema defines the database schema
type Schema struct {
	Name    string
	Tables  []Table
	Indexes []Index
}

// Table defines a database table
type Table struct {
	Name    string
	Columns []Column
}

// Column defines a table column
type Column struct {
	Name     string
	Type     ColumnType
	Nullable bool
	Default  string
	Primary  bool
}

// ColumnType represents a column data type
type ColumnType string

const (
	TypeText      ColumnType = "text"
	TypeInt       ColumnType = "int"
	TypeBigInt    ColumnType = "bigint"
	TypeFloat     ColumnType = "float"
	TypeBool      ColumnType = "bool"
	TypeTimestamp ColumnType = "timestamp"
	TypeJSON      ColumnType = "json"
	TypeBytes     ColumnType = "bytes"
)

// Index defines a database index
type Index struct {
	Name    string
	Table   string
	Columns []string
	Unique  bool
}

// Errors
var (
	ErrNotFound      = fmt.Errorf("not found")
	ErrAlreadyExists = fmt.Errorf("already exists")
	ErrNotSupported  = fmt.Errorf("not supported by this backend")
	ErrClosed        = fmt.Errorf("store is closed")
)

// New creates a new storage backend based on config
func New(cfg Config) (Store, error) {
	switch cfg.Backend {
	case BackendPostgres:
		return NewPostgres(cfg)
	case BackendBadger:
		return NewBadger(cfg)
	case BackendDgraph:
		return NewDgraph(cfg)
	default:
		return nil, fmt.Errorf("unknown storage backend: %s", cfg.Backend)
	}
}

// ParseBackend parses a backend string
func ParseBackend(s string) (Backend, error) {
	switch s {
	case "postgres", "postgresql", "pg":
		return BackendPostgres, nil
	case "badger", "badgerdb":
		return BackendBadger, nil
	case "dgraph":
		return BackendDgraph, nil
	default:
		return "", fmt.Errorf("unknown backend: %s", s)
	}
}
