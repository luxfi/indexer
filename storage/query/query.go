// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package query provides a query layer for indexed data.
// Default backend is SQLite. Use build tags for other backends:
//   - go build -tags postgres
//   - go build -tags mysql
//   - go build -tags mongo
//   - go build -tags dgraph
package query

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Backend identifies the query backend type
type Backend string

const (
	BackendSQLite   Backend = "sqlite"
	BackendPostgres Backend = "postgres"
	BackendMySQL    Backend = "mysql"
	BackendMongo    Backend = "mongo"
	BackendDgraph   Backend = "dgraph"
)

// Config for the query layer
type Config struct {
	Backend  Backend
	URL      string            // Connection URL
	Database string            // Database name (for MongoDB)
	DataDir  string            // For file-based backends
	Options  map[string]string // Backend-specific options
}

// Engine is the query layer interface
type Engine interface {
	// Backend returns the backend type (sqlite, postgres, etc.)
	Backend() Backend

	// Lifecycle
	Init(ctx context.Context) error
	Close() error
	Ping(ctx context.Context) error

	// Schema management
	InitSchema(ctx context.Context, schema Schema) error
	Migrate(ctx context.Context, schema Schema) error

	// Block queries
	InsertBlock(ctx context.Context, table string, block *Block) error
	GetBlock(ctx context.Context, table string, id string) (*Block, error)
	GetBlockByHeight(ctx context.Context, table string, height uint64) (*Block, error)
	GetRecentBlocks(ctx context.Context, table string, limit int) ([]*Block, error)
	GetBlockRange(ctx context.Context, table string, start, end uint64) ([]*Block, error)

	// Vertex queries
	InsertVertex(ctx context.Context, table string, vertex *Vertex) error
	GetVertex(ctx context.Context, table string, id string) (*Vertex, error)
	GetRecentVertices(ctx context.Context, table string, limit int) ([]*Vertex, error)
	GetVerticesByEpoch(ctx context.Context, table string, epoch uint32) ([]*Vertex, error)

	// Edge queries
	InsertEdge(ctx context.Context, table string, edge *Edge) error
	GetEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error)
	GetIncomingEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error)

	// Generic queries
	Query(ctx context.Context, query string, args ...interface{}) ([]Row, error)
	Exec(ctx context.Context, query string, args ...interface{}) (Result, error)

	// Aggregations
	Count(ctx context.Context, table string, where string, args ...interface{}) (int64, error)
	Sum(ctx context.Context, table string, column, where string, args ...interface{}) (float64, error)

	// Transaction support
	Begin(ctx context.Context) (Tx, error)
}

// Tx represents a query transaction
type Tx interface {
	Engine
	Commit() error
	Rollback() error
}

// Row represents a query result row
type Row map[string]interface{}

// Result represents an exec result
type Result struct {
	RowsAffected int64
	LastInsertID int64
}

// Block for query storage
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

// Vertex for query storage
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

// Edge for query storage
type Edge struct {
	Source    string    `json:"source"`
	Target    string    `json:"target"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
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
	ErrNotFound     = fmt.Errorf("not found")
	ErrExists       = fmt.Errorf("already exists")
	ErrNotSupported = fmt.Errorf("not supported")
	ErrClosed       = fmt.Errorf("engine closed")
)

// New creates a new query engine based on config
// This function is implemented in backend-specific files with build tags
func New(cfg Config) (Engine, error) {
	return newEngine(cfg)
}

// AvailableBackends returns the list of backends compiled into this build
func AvailableBackends() []Backend {
	return availableBackends()
}
