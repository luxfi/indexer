// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

//go:build !postgres && !mysql && !mongo && !dgraph

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

func init() {
	registerBackend(BackendSQLite)
}

var registeredBackends []Backend

func registerBackend(b Backend) {
	registeredBackends = append(registeredBackends, b)
}

func availableBackends() []Backend {
	return registeredBackends
}

func newEngine(cfg Config) (Engine, error) {
	return NewSQLite(cfg)
}

// SQLite implements the Engine interface using SQLite
type SQLite struct {
	db     *sql.DB
	path   string
	mu     sync.RWMutex
	closed bool
}

// NewSQLite creates a new SQLite query engine
func NewSQLite(cfg Config) (*SQLite, error) {
	path := cfg.URL
	if path == "" {
		path = filepath.Join(cfg.DataDir, "indexer.db")
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Open database with WAL mode and other optimizations
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000&cache=shared", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	// Connection pool settings
	db.SetMaxOpenConns(1) // SQLite doesn't support multiple writers
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping sqlite: %w", err)
	}

	return &SQLite{
		db:   db,
		path: path,
	}, nil
}

// Backend returns the backend type
func (s *SQLite) Backend() Backend {
	return BackendSQLite
}

func (s *SQLite) Init(ctx context.Context) error {
	// Enable foreign keys
	_, err := s.db.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	return err
}

func (s *SQLite) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}

func (s *SQLite) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *SQLite) InitSchema(ctx context.Context, schema Schema) error {
	for _, table := range schema.Tables {
		if err := s.createTable(ctx, table); err != nil {
			return err
		}
	}
	for _, idx := range schema.Indexes {
		if err := s.createIndex(ctx, idx); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLite) createTable(ctx context.Context, table Table) error {
	var cols []string
	var primaryCols []string

	for _, col := range table.Columns {
		def := fmt.Sprintf("%s %s", col.Name, s.sqlType(col.Type))
		if !col.Nullable {
			def += " NOT NULL"
		}
		if col.Default != "" {
			def += " DEFAULT " + col.Default
		}
		if col.Primary {
			primaryCols = append(primaryCols, col.Name)
		}
		cols = append(cols, def)
	}

	if len(primaryCols) > 0 {
		cols = append(cols, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(primaryCols, ", ")))
	}

	query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n)", table.Name, strings.Join(cols, ",\n  "))
	_, err := s.db.ExecContext(ctx, query)
	return err
}

func (s *SQLite) createIndex(ctx context.Context, idx Index) error {
	unique := ""
	if idx.Unique {
		unique = "UNIQUE "
	}
	query := fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)",
		unique, idx.Name, idx.Table, strings.Join(idx.Columns, ", "))
	_, err := s.db.ExecContext(ctx, query)
	return err
}

func (s *SQLite) sqlType(t ColumnType) string {
	switch t {
	case TypeText:
		return "TEXT"
	case TypeInt:
		return "INTEGER"
	case TypeBigInt:
		return "INTEGER"
	case TypeFloat:
		return "REAL"
	case TypeBool:
		return "INTEGER"
	case TypeTimestamp:
		return "DATETIME"
	case TypeJSON:
		return "TEXT"
	case TypeBytes:
		return "BLOB"
	default:
		return "TEXT"
	}
}

func (s *SQLite) Migrate(ctx context.Context, schema Schema) error {
	// Simple migration: create missing tables and indexes
	return s.InitSchema(ctx, schema)
}

func (s *SQLite) InsertBlock(ctx context.Context, table string, block *Block) error {
	txIDsJSON, _ := json.Marshal(block.TxIDs)
	query := fmt.Sprintf(`
		INSERT OR REPLACE INTO %s (id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, table)
	_, err := s.db.ExecContext(ctx, query,
		block.ID, block.ParentID, block.Height, block.Timestamp, block.Status,
		block.TxCount, string(txIDsJSON), string(block.Data), string(block.Metadata), block.CreatedAt,
	)
	return err
}

func (s *SQLite) GetBlock(ctx context.Context, table string, id string) (*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s WHERE id = ?`, table)
	row := s.db.QueryRowContext(ctx, query, id)
	return s.scanBlock(row)
}

func (s *SQLite) GetBlockByHeight(ctx context.Context, table string, height uint64) (*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s WHERE height = ?`, table)
	row := s.db.QueryRowContext(ctx, query, height)
	return s.scanBlock(row)
}

func (s *SQLite) GetRecentBlocks(ctx context.Context, table string, limit int) ([]*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s ORDER BY height DESC LIMIT ?`, table)
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanBlocks(rows)
}

func (s *SQLite) GetBlockRange(ctx context.Context, table string, start, end uint64) ([]*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s WHERE height >= ? AND height <= ? ORDER BY height`, table)
	rows, err := s.db.QueryContext(ctx, query, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanBlocks(rows)
}

func (s *SQLite) scanBlock(row *sql.Row) (*Block, error) {
	var block Block
	var txIDsJSON, dataJSON, metaJSON string
	err := row.Scan(&block.ID, &block.ParentID, &block.Height, &block.Timestamp, &block.Status,
		&block.TxCount, &txIDsJSON, &dataJSON, &metaJSON, &block.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(txIDsJSON), &block.TxIDs)
	block.Data = json.RawMessage(dataJSON)
	block.Metadata = json.RawMessage(metaJSON)
	return &block, nil
}

func (s *SQLite) scanBlocks(rows *sql.Rows) ([]*Block, error) {
	var blocks []*Block
	for rows.Next() {
		var block Block
		var txIDsJSON, dataJSON, metaJSON string
		err := rows.Scan(&block.ID, &block.ParentID, &block.Height, &block.Timestamp, &block.Status,
			&block.TxCount, &txIDsJSON, &dataJSON, &metaJSON, &block.CreatedAt)
		if err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(txIDsJSON), &block.TxIDs)
		block.Data = json.RawMessage(dataJSON)
		block.Metadata = json.RawMessage(metaJSON)
		blocks = append(blocks, &block)
	}
	return blocks, rows.Err()
}

func (s *SQLite) InsertVertex(ctx context.Context, table string, vertex *Vertex) error {
	parentIDsJSON, _ := json.Marshal(vertex.ParentIDs)
	txIDsJSON, _ := json.Marshal(vertex.TxIDs)
	query := fmt.Sprintf(`
		INSERT OR REPLACE INTO %s (id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, table)
	_, err := s.db.ExecContext(ctx, query,
		vertex.ID, vertex.Type, string(parentIDsJSON), vertex.Height, vertex.Epoch,
		string(txIDsJSON), vertex.Timestamp, vertex.Status, string(vertex.Data), string(vertex.Metadata), vertex.CreatedAt,
	)
	return err
}

func (s *SQLite) GetVertex(ctx context.Context, table string, id string) (*Vertex, error) {
	query := fmt.Sprintf(`SELECT id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at FROM %s WHERE id = ?`, table)
	row := s.db.QueryRowContext(ctx, query, id)
	return s.scanVertex(row)
}

func (s *SQLite) GetRecentVertices(ctx context.Context, table string, limit int) ([]*Vertex, error) {
	query := fmt.Sprintf(`SELECT id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at FROM %s ORDER BY created_at DESC LIMIT ?`, table)
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanVertices(rows)
}

func (s *SQLite) GetVerticesByEpoch(ctx context.Context, table string, epoch uint32) ([]*Vertex, error) {
	query := fmt.Sprintf(`SELECT id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at FROM %s WHERE epoch = ?`, table)
	rows, err := s.db.QueryContext(ctx, query, epoch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanVertices(rows)
}

func (s *SQLite) scanVertex(row *sql.Row) (*Vertex, error) {
	var v Vertex
	var parentIDsJSON, txIDsJSON, dataJSON, metaJSON string
	err := row.Scan(&v.ID, &v.Type, &parentIDsJSON, &v.Height, &v.Epoch,
		&txIDsJSON, &v.Timestamp, &v.Status, &dataJSON, &metaJSON, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(parentIDsJSON), &v.ParentIDs)
	json.Unmarshal([]byte(txIDsJSON), &v.TxIDs)
	v.Data = json.RawMessage(dataJSON)
	v.Metadata = json.RawMessage(metaJSON)
	return &v, nil
}

func (s *SQLite) scanVertices(rows *sql.Rows) ([]*Vertex, error) {
	var vertices []*Vertex
	for rows.Next() {
		var v Vertex
		var parentIDsJSON, txIDsJSON, dataJSON, metaJSON string
		err := rows.Scan(&v.ID, &v.Type, &parentIDsJSON, &v.Height, &v.Epoch,
			&txIDsJSON, &v.Timestamp, &v.Status, &dataJSON, &metaJSON, &v.CreatedAt)
		if err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(parentIDsJSON), &v.ParentIDs)
		json.Unmarshal([]byte(txIDsJSON), &v.TxIDs)
		v.Data = json.RawMessage(dataJSON)
		v.Metadata = json.RawMessage(metaJSON)
		vertices = append(vertices, &v)
	}
	return vertices, rows.Err()
}

func (s *SQLite) InsertEdge(ctx context.Context, table string, edge *Edge) error {
	query := fmt.Sprintf(`INSERT OR REPLACE INTO %s (source, target, type, created_at) VALUES (?, ?, ?, ?)`, table)
	_, err := s.db.ExecContext(ctx, query, edge.Source, edge.Target, edge.Type, edge.CreatedAt)
	return err
}

func (s *SQLite) GetEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	query := fmt.Sprintf(`SELECT source, target, type, created_at FROM %s WHERE source = ?`, table)
	rows, err := s.db.QueryContext(ctx, query, vertexID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanEdges(rows)
}

func (s *SQLite) GetIncomingEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	query := fmt.Sprintf(`SELECT source, target, type, created_at FROM %s WHERE target = ?`, table)
	rows, err := s.db.QueryContext(ctx, query, vertexID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanEdges(rows)
}

func (s *SQLite) scanEdges(rows *sql.Rows) ([]*Edge, error) {
	var edges []*Edge
	for rows.Next() {
		var e Edge
		err := rows.Scan(&e.Source, &e.Target, &e.Type, &e.CreatedAt)
		if err != nil {
			return nil, err
		}
		edges = append(edges, &e)
	}
	return edges, rows.Err()
}

func (s *SQLite) Query(ctx context.Context, query string, args ...interface{}) ([]Row, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []Row
	for rows.Next() {
		values := make([]interface{}, len(cols))
		valuePtrs := make([]interface{}, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}
		row := make(Row)
		for i, col := range cols {
			row[col] = values[i]
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *SQLite) Exec(ctx context.Context, query string, args ...interface{}) (Result, error) {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return Result{}, err
	}
	affected, _ := res.RowsAffected()
	lastID, _ := res.LastInsertId()
	return Result{RowsAffected: affected, LastInsertID: lastID}, nil
}

func (s *SQLite) Count(ctx context.Context, table string, where string, args ...interface{}) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	if where != "" {
		query += " WHERE " + where
	}
	var count int64
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func (s *SQLite) Sum(ctx context.Context, table string, column, where string, args ...interface{}) (float64, error) {
	query := fmt.Sprintf("SELECT COALESCE(SUM(%s), 0) FROM %s", column, table)
	if where != "" {
		query += " WHERE " + where
	}
	var sum float64
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&sum)
	return sum, err
}

func (s *SQLite) Begin(ctx context.Context) (Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &sqliteTx{tx: tx, parent: s}, nil
}

type sqliteTx struct {
	tx     *sql.Tx
	parent *SQLite
}

func (t *sqliteTx) Backend() Backend                                                   { return BackendSQLite }
func (t *sqliteTx) Init(ctx context.Context) error                                     { return nil }
func (t *sqliteTx) Close() error                                                       { return t.Rollback() }
func (t *sqliteTx) Ping(ctx context.Context) error                                     { return nil }
func (t *sqliteTx) InitSchema(ctx context.Context, schema Schema) error                { return ErrNotSupported }
func (t *sqliteTx) Migrate(ctx context.Context, schema Schema) error                   { return ErrNotSupported }
func (t *sqliteTx) Begin(ctx context.Context) (Tx, error)                              { return nil, ErrNotSupported }
func (t *sqliteTx) Commit() error                                                      { return t.tx.Commit() }
func (t *sqliteTx) Rollback() error                                                    { return t.tx.Rollback() }

func (t *sqliteTx) InsertBlock(ctx context.Context, table string, block *Block) error {
	txIDsJSON, _ := json.Marshal(block.TxIDs)
	query := fmt.Sprintf(`
		INSERT OR REPLACE INTO %s (id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, table)
	_, err := t.tx.ExecContext(ctx, query,
		block.ID, block.ParentID, block.Height, block.Timestamp, block.Status,
		block.TxCount, string(txIDsJSON), string(block.Data), string(block.Metadata), block.CreatedAt,
	)
	return err
}

func (t *sqliteTx) GetBlock(ctx context.Context, table string, id string) (*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s WHERE id = ?`, table)
	row := t.tx.QueryRowContext(ctx, query, id)
	return t.parent.scanBlock(row)
}

func (t *sqliteTx) GetBlockByHeight(ctx context.Context, table string, height uint64) (*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s WHERE height = ?`, table)
	row := t.tx.QueryRowContext(ctx, query, height)
	return t.parent.scanBlock(row)
}

func (t *sqliteTx) GetRecentBlocks(ctx context.Context, table string, limit int) ([]*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s ORDER BY height DESC LIMIT ?`, table)
	rows, err := t.tx.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return t.parent.scanBlocks(rows)
}

func (t *sqliteTx) GetBlockRange(ctx context.Context, table string, start, end uint64) ([]*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s WHERE height >= ? AND height <= ? ORDER BY height`, table)
	rows, err := t.tx.QueryContext(ctx, query, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return t.parent.scanBlocks(rows)
}

func (t *sqliteTx) InsertVertex(ctx context.Context, table string, vertex *Vertex) error {
	parentIDsJSON, _ := json.Marshal(vertex.ParentIDs)
	txIDsJSON, _ := json.Marshal(vertex.TxIDs)
	query := fmt.Sprintf(`
		INSERT OR REPLACE INTO %s (id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, table)
	_, err := t.tx.ExecContext(ctx, query,
		vertex.ID, vertex.Type, string(parentIDsJSON), vertex.Height, vertex.Epoch,
		string(txIDsJSON), vertex.Timestamp, vertex.Status, string(vertex.Data), string(vertex.Metadata), vertex.CreatedAt,
	)
	return err
}

func (t *sqliteTx) GetVertex(ctx context.Context, table string, id string) (*Vertex, error) {
	query := fmt.Sprintf(`SELECT id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at FROM %s WHERE id = ?`, table)
	row := t.tx.QueryRowContext(ctx, query, id)
	return t.parent.scanVertex(row)
}

func (t *sqliteTx) GetRecentVertices(ctx context.Context, table string, limit int) ([]*Vertex, error) {
	query := fmt.Sprintf(`SELECT id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at FROM %s ORDER BY created_at DESC LIMIT ?`, table)
	rows, err := t.tx.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return t.parent.scanVertices(rows)
}

func (t *sqliteTx) GetVerticesByEpoch(ctx context.Context, table string, epoch uint32) ([]*Vertex, error) {
	query := fmt.Sprintf(`SELECT id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at FROM %s WHERE epoch = ?`, table)
	rows, err := t.tx.QueryContext(ctx, query, epoch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return t.parent.scanVertices(rows)
}

func (t *sqliteTx) InsertEdge(ctx context.Context, table string, edge *Edge) error {
	query := fmt.Sprintf(`INSERT OR REPLACE INTO %s (source, target, type, created_at) VALUES (?, ?, ?, ?)`, table)
	_, err := t.tx.ExecContext(ctx, query, edge.Source, edge.Target, edge.Type, edge.CreatedAt)
	return err
}

func (t *sqliteTx) GetEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	query := fmt.Sprintf(`SELECT source, target, type, created_at FROM %s WHERE source = ?`, table)
	rows, err := t.tx.QueryContext(ctx, query, vertexID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return t.parent.scanEdges(rows)
}

func (t *sqliteTx) GetIncomingEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	query := fmt.Sprintf(`SELECT source, target, type, created_at FROM %s WHERE target = ?`, table)
	rows, err := t.tx.QueryContext(ctx, query, vertexID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return t.parent.scanEdges(rows)
}

func (t *sqliteTx) Query(ctx context.Context, query string, args ...interface{}) ([]Row, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []Row
	for rows.Next() {
		values := make([]interface{}, len(cols))
		valuePtrs := make([]interface{}, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}
		row := make(Row)
		for i, col := range cols {
			row[col] = values[i]
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func (t *sqliteTx) Exec(ctx context.Context, query string, args ...interface{}) (Result, error) {
	res, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return Result{}, err
	}
	affected, _ := res.RowsAffected()
	lastID, _ := res.LastInsertId()
	return Result{RowsAffected: affected, LastInsertID: lastID}, nil
}

func (t *sqliteTx) Count(ctx context.Context, table string, where string, args ...interface{}) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	if where != "" {
		query += " WHERE " + where
	}
	var count int64
	err := t.tx.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func (t *sqliteTx) Sum(ctx context.Context, table string, column, where string, args ...interface{}) (float64, error) {
	query := fmt.Sprintf("SELECT COALESCE(SUM(%s), 0) FROM %s", column, table)
	if where != "" {
		query += " WHERE " + where
	}
	var sum float64
	err := t.tx.QueryRowContext(ctx, query, args...).Scan(&sum)
	return sum, err
}

// Ensure SQLite implements Engine
var _ Engine = (*SQLite)(nil)
var _ Tx = (*sqliteTx)(nil)

// BlocksSchema returns the default blocks table schema
func BlocksSchema() Table {
	return Table{
		Name: "blocks",
		Columns: []Column{
			{Name: "id", Type: TypeText, Primary: true},
			{Name: "parent_id", Type: TypeText},
			{Name: "height", Type: TypeBigInt},
			{Name: "timestamp", Type: TypeTimestamp},
			{Name: "status", Type: TypeText},
			{Name: "tx_count", Type: TypeInt},
			{Name: "tx_ids", Type: TypeJSON},
			{Name: "data", Type: TypeJSON},
			{Name: "metadata", Type: TypeJSON},
			{Name: "created_at", Type: TypeTimestamp},
		},
	}
}

// VerticesSchema returns the default vertices table schema
func VerticesSchema() Table {
	return Table{
		Name: "vertices",
		Columns: []Column{
			{Name: "id", Type: TypeText, Primary: true},
			{Name: "type", Type: TypeText},
			{Name: "parent_ids", Type: TypeJSON},
			{Name: "height", Type: TypeBigInt},
			{Name: "epoch", Type: TypeInt},
			{Name: "tx_ids", Type: TypeJSON},
			{Name: "timestamp", Type: TypeTimestamp},
			{Name: "status", Type: TypeText},
			{Name: "data", Type: TypeJSON},
			{Name: "metadata", Type: TypeJSON},
			{Name: "created_at", Type: TypeTimestamp},
		},
	}
}

// EdgesSchema returns the default edges table schema
func EdgesSchema() Table {
	return Table{
		Name: "edges",
		Columns: []Column{
			{Name: "source", Type: TypeText},
			{Name: "target", Type: TypeText},
			{Name: "type", Type: TypeText},
			{Name: "created_at", Type: TypeTimestamp},
		},
	}
}

// DefaultIndexes returns the default index definitions
func DefaultIndexes() []Index {
	return []Index{
		{Name: "idx_blocks_height", Table: "blocks", Columns: []string{"height"}},
		{Name: "idx_blocks_timestamp", Table: "blocks", Columns: []string{"timestamp"}},
		{Name: "idx_vertices_epoch", Table: "vertices", Columns: []string{"epoch"}},
		{Name: "idx_vertices_height", Table: "vertices", Columns: []string{"height"}},
		{Name: "idx_edges_source", Table: "edges", Columns: []string{"source"}},
		{Name: "idx_edges_target", Table: "edges", Columns: []string{"target"}},
	}
}

// DefaultSchema returns the default complete schema
func DefaultSchema() Schema {
	return Schema{
		Name:    "indexer",
		Tables:  []Table{BlocksSchema(), VerticesSchema(), EdgesSchema()},
		Indexes: DefaultIndexes(),
	}
}
