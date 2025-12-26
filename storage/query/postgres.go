// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

//go:build postgres

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	_ "github.com/lib/pq"
)

func init() {
	registerBackend(BackendPostgres)
}

var registeredBackends []Backend

func registerBackend(b Backend) {
	registeredBackends = append(registeredBackends, b)
}

func availableBackends() []Backend {
	return registeredBackends
}

func newEngine(cfg Config) (Engine, error) {
	return NewPostgres(cfg)
}

// Postgres implements the Engine interface using PostgreSQL
type Postgres struct {
	db     *sql.DB
	mu     sync.RWMutex
	closed bool
}

// NewPostgres creates a new Postgres query engine
func NewPostgres(cfg Config) (*Postgres, error) {
	db, err := sql.Open("postgres", cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres: %w", err)
	}

	// Connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return &Postgres{db: db}, nil
}

// Backend returns the backend type
func (p *Postgres) Backend() Backend {
	return BackendPostgres
}

func (p *Postgres) Init(ctx context.Context) error {
	return nil
}

func (p *Postgres) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	return p.db.Close()
}

func (p *Postgres) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

func (p *Postgres) InitSchema(ctx context.Context, schema Schema) error {
	for _, table := range schema.Tables {
		if err := p.createTable(ctx, table); err != nil {
			return err
		}
	}
	for _, idx := range schema.Indexes {
		if err := p.createIndex(ctx, idx); err != nil {
			return err
		}
	}
	return nil
}

func (p *Postgres) createTable(ctx context.Context, table Table) error {
	var cols []string
	var primaryCols []string

	for _, col := range table.Columns {
		def := fmt.Sprintf("%s %s", col.Name, p.sqlType(col.Type))
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
	_, err := p.db.ExecContext(ctx, query)
	return err
}

func (p *Postgres) createIndex(ctx context.Context, idx Index) error {
	unique := ""
	if idx.Unique {
		unique = "UNIQUE "
	}
	query := fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)",
		unique, idx.Name, idx.Table, strings.Join(idx.Columns, ", "))
	_, err := p.db.ExecContext(ctx, query)
	return err
}

func (p *Postgres) sqlType(t ColumnType) string {
	switch t {
	case TypeText:
		return "TEXT"
	case TypeInt:
		return "INTEGER"
	case TypeBigInt:
		return "BIGINT"
	case TypeFloat:
		return "DOUBLE PRECISION"
	case TypeBool:
		return "BOOLEAN"
	case TypeTimestamp:
		return "TIMESTAMP WITH TIME ZONE"
	case TypeJSON:
		return "JSONB"
	case TypeBytes:
		return "BYTEA"
	default:
		return "TEXT"
	}
}

func (p *Postgres) Migrate(ctx context.Context, schema Schema) error {
	return p.InitSchema(ctx, schema)
}

func (p *Postgres) InsertBlock(ctx context.Context, table string, block *Block) error {
	txIDsJSON, _ := json.Marshal(block.TxIDs)
	query := fmt.Sprintf(`
		INSERT INTO %s (id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			parent_id = EXCLUDED.parent_id,
			height = EXCLUDED.height,
			timestamp = EXCLUDED.timestamp,
			status = EXCLUDED.status,
			tx_count = EXCLUDED.tx_count,
			tx_ids = EXCLUDED.tx_ids,
			data = EXCLUDED.data,
			metadata = EXCLUDED.metadata
	`, table)
	_, err := p.db.ExecContext(ctx, query,
		block.ID, block.ParentID, block.Height, block.Timestamp, block.Status,
		block.TxCount, string(txIDsJSON), string(block.Data), string(block.Metadata), block.CreatedAt,
	)
	return err
}

func (p *Postgres) GetBlock(ctx context.Context, table string, id string) (*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s WHERE id = $1`, table)
	row := p.db.QueryRowContext(ctx, query, id)
	return p.scanBlock(row)
}

func (p *Postgres) GetBlockByHeight(ctx context.Context, table string, height uint64) (*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s WHERE height = $1`, table)
	row := p.db.QueryRowContext(ctx, query, height)
	return p.scanBlock(row)
}

func (p *Postgres) GetRecentBlocks(ctx context.Context, table string, limit int) ([]*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s ORDER BY height DESC LIMIT $1`, table)
	rows, err := p.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return p.scanBlocks(rows)
}

func (p *Postgres) GetBlockRange(ctx context.Context, table string, start, end uint64) ([]*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s WHERE height >= $1 AND height <= $2 ORDER BY height`, table)
	rows, err := p.db.QueryContext(ctx, query, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return p.scanBlocks(rows)
}

func (p *Postgres) scanBlock(row *sql.Row) (*Block, error) {
	var block Block
	var txIDsJSON, dataJSON, metaJSON []byte
	err := row.Scan(&block.ID, &block.ParentID, &block.Height, &block.Timestamp, &block.Status,
		&block.TxCount, &txIDsJSON, &dataJSON, &metaJSON, &block.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal(txIDsJSON, &block.TxIDs)
	block.Data = json.RawMessage(dataJSON)
	block.Metadata = json.RawMessage(metaJSON)
	return &block, nil
}

func (p *Postgres) scanBlocks(rows *sql.Rows) ([]*Block, error) {
	var blocks []*Block
	for rows.Next() {
		var block Block
		var txIDsJSON, dataJSON, metaJSON []byte
		err := rows.Scan(&block.ID, &block.ParentID, &block.Height, &block.Timestamp, &block.Status,
			&block.TxCount, &txIDsJSON, &dataJSON, &metaJSON, &block.CreatedAt)
		if err != nil {
			return nil, err
		}
		json.Unmarshal(txIDsJSON, &block.TxIDs)
		block.Data = json.RawMessage(dataJSON)
		block.Metadata = json.RawMessage(metaJSON)
		blocks = append(blocks, &block)
	}
	return blocks, rows.Err()
}

func (p *Postgres) InsertVertex(ctx context.Context, table string, vertex *Vertex) error {
	parentIDsJSON, _ := json.Marshal(vertex.ParentIDs)
	txIDsJSON, _ := json.Marshal(vertex.TxIDs)
	query := fmt.Sprintf(`
		INSERT INTO %s (id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET
			type = EXCLUDED.type,
			parent_ids = EXCLUDED.parent_ids,
			height = EXCLUDED.height,
			epoch = EXCLUDED.epoch,
			tx_ids = EXCLUDED.tx_ids,
			timestamp = EXCLUDED.timestamp,
			status = EXCLUDED.status,
			data = EXCLUDED.data,
			metadata = EXCLUDED.metadata
	`, table)
	_, err := p.db.ExecContext(ctx, query,
		vertex.ID, vertex.Type, string(parentIDsJSON), vertex.Height, vertex.Epoch,
		string(txIDsJSON), vertex.Timestamp, vertex.Status, string(vertex.Data), string(vertex.Metadata), vertex.CreatedAt,
	)
	return err
}

func (p *Postgres) GetVertex(ctx context.Context, table string, id string) (*Vertex, error) {
	query := fmt.Sprintf(`SELECT id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at FROM %s WHERE id = $1`, table)
	row := p.db.QueryRowContext(ctx, query, id)
	return p.scanVertex(row)
}

func (p *Postgres) GetRecentVertices(ctx context.Context, table string, limit int) ([]*Vertex, error) {
	query := fmt.Sprintf(`SELECT id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at FROM %s ORDER BY created_at DESC LIMIT $1`, table)
	rows, err := p.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return p.scanVertices(rows)
}

func (p *Postgres) GetVerticesByEpoch(ctx context.Context, table string, epoch uint32) ([]*Vertex, error) {
	query := fmt.Sprintf(`SELECT id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at FROM %s WHERE epoch = $1`, table)
	rows, err := p.db.QueryContext(ctx, query, epoch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return p.scanVertices(rows)
}

func (p *Postgres) scanVertex(row *sql.Row) (*Vertex, error) {
	var v Vertex
	var parentIDsJSON, txIDsJSON, dataJSON, metaJSON []byte
	err := row.Scan(&v.ID, &v.Type, &parentIDsJSON, &v.Height, &v.Epoch,
		&txIDsJSON, &v.Timestamp, &v.Status, &dataJSON, &metaJSON, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal(parentIDsJSON, &v.ParentIDs)
	json.Unmarshal(txIDsJSON, &v.TxIDs)
	v.Data = json.RawMessage(dataJSON)
	v.Metadata = json.RawMessage(metaJSON)
	return &v, nil
}

func (p *Postgres) scanVertices(rows *sql.Rows) ([]*Vertex, error) {
	var vertices []*Vertex
	for rows.Next() {
		var v Vertex
		var parentIDsJSON, txIDsJSON, dataJSON, metaJSON []byte
		err := rows.Scan(&v.ID, &v.Type, &parentIDsJSON, &v.Height, &v.Epoch,
			&txIDsJSON, &v.Timestamp, &v.Status, &dataJSON, &metaJSON, &v.CreatedAt)
		if err != nil {
			return nil, err
		}
		json.Unmarshal(parentIDsJSON, &v.ParentIDs)
		json.Unmarshal(txIDsJSON, &v.TxIDs)
		v.Data = json.RawMessage(dataJSON)
		v.Metadata = json.RawMessage(metaJSON)
		vertices = append(vertices, &v)
	}
	return vertices, rows.Err()
}

func (p *Postgres) InsertEdge(ctx context.Context, table string, edge *Edge) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (source, target, type, created_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (source, target) DO UPDATE SET type = EXCLUDED.type
	`, table)
	_, err := p.db.ExecContext(ctx, query, edge.Source, edge.Target, edge.Type, edge.CreatedAt)
	return err
}

func (p *Postgres) GetEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	query := fmt.Sprintf(`SELECT source, target, type, created_at FROM %s WHERE source = $1`, table)
	rows, err := p.db.QueryContext(ctx, query, vertexID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return p.scanEdges(rows)
}

func (p *Postgres) GetIncomingEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	query := fmt.Sprintf(`SELECT source, target, type, created_at FROM %s WHERE target = $1`, table)
	rows, err := p.db.QueryContext(ctx, query, vertexID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return p.scanEdges(rows)
}

func (p *Postgres) scanEdges(rows *sql.Rows) ([]*Edge, error) {
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

func (p *Postgres) Query(ctx context.Context, query string, args ...interface{}) ([]Row, error) {
	rows, err := p.db.QueryContext(ctx, query, args...)
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

func (p *Postgres) Exec(ctx context.Context, query string, args ...interface{}) (Result, error) {
	res, err := p.db.ExecContext(ctx, query, args...)
	if err != nil {
		return Result{}, err
	}
	affected, _ := res.RowsAffected()
	return Result{RowsAffected: affected}, nil
}

func (p *Postgres) Count(ctx context.Context, table string, where string, args ...interface{}) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	if where != "" {
		query += " WHERE " + where
	}
	var count int64
	err := p.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func (p *Postgres) Sum(ctx context.Context, table string, column, where string, args ...interface{}) (float64, error) {
	query := fmt.Sprintf("SELECT COALESCE(SUM(%s), 0) FROM %s", column, table)
	if where != "" {
		query += " WHERE " + where
	}
	var sum float64
	err := p.db.QueryRowContext(ctx, query, args...).Scan(&sum)
	return sum, err
}

func (p *Postgres) Begin(ctx context.Context) (Tx, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &postgresTx{tx: tx, parent: p}, nil
}

type postgresTx struct {
	tx     *sql.Tx
	parent *Postgres
}

func (t *postgresTx) Backend() Backend                                    { return BackendPostgres }
func (t *postgresTx) Init(ctx context.Context) error                      { return nil }
func (t *postgresTx) Close() error                                        { return t.Rollback() }
func (t *postgresTx) Ping(ctx context.Context) error                      { return nil }
func (t *postgresTx) InitSchema(ctx context.Context, schema Schema) error { return ErrNotSupported }
func (t *postgresTx) Migrate(ctx context.Context, schema Schema) error    { return ErrNotSupported }
func (t *postgresTx) Begin(ctx context.Context) (Tx, error)               { return nil, ErrNotSupported }
func (t *postgresTx) Commit() error                                       { return t.tx.Commit() }
func (t *postgresTx) Rollback() error                                     { return t.tx.Rollback() }

func (t *postgresTx) InsertBlock(ctx context.Context, table string, block *Block) error {
	txIDsJSON, _ := json.Marshal(block.TxIDs)
	query := fmt.Sprintf(`
		INSERT INTO %s (id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			parent_id = EXCLUDED.parent_id,
			height = EXCLUDED.height,
			timestamp = EXCLUDED.timestamp,
			status = EXCLUDED.status,
			tx_count = EXCLUDED.tx_count,
			tx_ids = EXCLUDED.tx_ids,
			data = EXCLUDED.data,
			metadata = EXCLUDED.metadata
	`, table)
	_, err := t.tx.ExecContext(ctx, query,
		block.ID, block.ParentID, block.Height, block.Timestamp, block.Status,
		block.TxCount, string(txIDsJSON), string(block.Data), string(block.Metadata), block.CreatedAt,
	)
	return err
}

func (t *postgresTx) GetBlock(ctx context.Context, table string, id string) (*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s WHERE id = $1`, table)
	row := t.tx.QueryRowContext(ctx, query, id)
	return t.parent.scanBlock(row)
}

func (t *postgresTx) GetBlockByHeight(ctx context.Context, table string, height uint64) (*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s WHERE height = $1`, table)
	row := t.tx.QueryRowContext(ctx, query, height)
	return t.parent.scanBlock(row)
}

func (t *postgresTx) GetRecentBlocks(ctx context.Context, table string, limit int) ([]*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s ORDER BY height DESC LIMIT $1`, table)
	rows, err := t.tx.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return t.parent.scanBlocks(rows)
}

func (t *postgresTx) GetBlockRange(ctx context.Context, table string, start, end uint64) ([]*Block, error) {
	query := fmt.Sprintf(`SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at FROM %s WHERE height >= $1 AND height <= $2 ORDER BY height`, table)
	rows, err := t.tx.QueryContext(ctx, query, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return t.parent.scanBlocks(rows)
}

func (t *postgresTx) InsertVertex(ctx context.Context, table string, vertex *Vertex) error {
	parentIDsJSON, _ := json.Marshal(vertex.ParentIDs)
	txIDsJSON, _ := json.Marshal(vertex.TxIDs)
	query := fmt.Sprintf(`
		INSERT INTO %s (id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET
			type = EXCLUDED.type,
			parent_ids = EXCLUDED.parent_ids,
			height = EXCLUDED.height,
			epoch = EXCLUDED.epoch,
			tx_ids = EXCLUDED.tx_ids,
			timestamp = EXCLUDED.timestamp,
			status = EXCLUDED.status,
			data = EXCLUDED.data,
			metadata = EXCLUDED.metadata
	`, table)
	_, err := t.tx.ExecContext(ctx, query,
		vertex.ID, vertex.Type, string(parentIDsJSON), vertex.Height, vertex.Epoch,
		string(txIDsJSON), vertex.Timestamp, vertex.Status, string(vertex.Data), string(vertex.Metadata), vertex.CreatedAt,
	)
	return err
}

func (t *postgresTx) GetVertex(ctx context.Context, table string, id string) (*Vertex, error) {
	query := fmt.Sprintf(`SELECT id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at FROM %s WHERE id = $1`, table)
	row := t.tx.QueryRowContext(ctx, query, id)
	return t.parent.scanVertex(row)
}

func (t *postgresTx) GetRecentVertices(ctx context.Context, table string, limit int) ([]*Vertex, error) {
	query := fmt.Sprintf(`SELECT id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at FROM %s ORDER BY created_at DESC LIMIT $1`, table)
	rows, err := t.tx.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return t.parent.scanVertices(rows)
}

func (t *postgresTx) GetVerticesByEpoch(ctx context.Context, table string, epoch uint32) ([]*Vertex, error) {
	query := fmt.Sprintf(`SELECT id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at FROM %s WHERE epoch = $1`, table)
	rows, err := t.tx.QueryContext(ctx, query, epoch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return t.parent.scanVertices(rows)
}

func (t *postgresTx) InsertEdge(ctx context.Context, table string, edge *Edge) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (source, target, type, created_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (source, target) DO UPDATE SET type = EXCLUDED.type
	`, table)
	_, err := t.tx.ExecContext(ctx, query, edge.Source, edge.Target, edge.Type, edge.CreatedAt)
	return err
}

func (t *postgresTx) GetEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	query := fmt.Sprintf(`SELECT source, target, type, created_at FROM %s WHERE source = $1`, table)
	rows, err := t.tx.QueryContext(ctx, query, vertexID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return t.parent.scanEdges(rows)
}

func (t *postgresTx) GetIncomingEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	query := fmt.Sprintf(`SELECT source, target, type, created_at FROM %s WHERE target = $1`, table)
	rows, err := t.tx.QueryContext(ctx, query, vertexID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return t.parent.scanEdges(rows)
}

func (t *postgresTx) Query(ctx context.Context, query string, args ...interface{}) ([]Row, error) {
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

func (t *postgresTx) Exec(ctx context.Context, query string, args ...interface{}) (Result, error) {
	res, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return Result{}, err
	}
	affected, _ := res.RowsAffected()
	return Result{RowsAffected: affected}, nil
}

func (t *postgresTx) Count(ctx context.Context, table string, where string, args ...interface{}) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	if where != "" {
		query += " WHERE " + where
	}
	var count int64
	err := t.tx.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func (t *postgresTx) Sum(ctx context.Context, table string, column, where string, args ...interface{}) (float64, error) {
	query := fmt.Sprintf("SELECT COALESCE(SUM(%s), 0) FROM %s", column, table)
	if where != "" {
		query += " WHERE " + where
	}
	var sum float64
	err := t.tx.QueryRowContext(ctx, query, args...).Scan(&sum)
	return sum, err
}

var _ Engine = (*Postgres)(nil)
var _ Tx = (*postgresTx)(nil)
