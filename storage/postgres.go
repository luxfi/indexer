// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// PostgresStore implements Store using PostgreSQL
type PostgresStore struct {
	db     *sql.DB
	config Config
}

// NewPostgres creates a new PostgreSQL store
func NewPostgres(cfg Config) (*PostgresStore, error) {
	db, err := sql.Open("postgres", cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("postgres open: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &PostgresStore{
		db:     db,
		config: cfg,
	}, nil
}

func (s *PostgresStore) Init(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *PostgresStore) InitSchema(ctx context.Context, schema Schema) error {
	for _, table := range schema.Tables {
		if err := s.createTable(ctx, table); err != nil {
			return fmt.Errorf("create table %s: %w", table.Name, err)
		}
	}
	for _, idx := range schema.Indexes {
		if err := s.createIndex(ctx, idx); err != nil {
			return fmt.Errorf("create index %s: %w", idx.Name, err)
		}
	}
	return nil
}

func (s *PostgresStore) createTable(ctx context.Context, table Table) error {
	var cols []string
	var primaryKey string

	for _, col := range table.Columns {
		colDef := fmt.Sprintf("%s %s", col.Name, s.columnTypeToSQL(col.Type))
		if !col.Nullable {
			colDef += " NOT NULL"
		}
		if col.Default != "" {
			colDef += " DEFAULT " + col.Default
		}
		if col.Primary {
			primaryKey = col.Name
		}
		cols = append(cols, colDef)
	}

	if primaryKey != "" {
		cols = append(cols, fmt.Sprintf("PRIMARY KEY (%s)", primaryKey))
	}

	query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", table.Name, strings.Join(cols, ", "))
	_, err := s.db.ExecContext(ctx, query)
	return err
}

func (s *PostgresStore) columnTypeToSQL(t ColumnType) string {
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
		return "TIMESTAMPTZ"
	case TypeJSON:
		return "JSONB"
	case TypeBytes:
		return "BYTEA"
	default:
		return "TEXT"
	}
}

func (s *PostgresStore) createIndex(ctx context.Context, idx Index) error {
	unique := ""
	if idx.Unique {
		unique = "UNIQUE "
	}
	query := fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)",
		unique, idx.Name, idx.Table, strings.Join(idx.Columns, ", "))
	_, err := s.db.ExecContext(ctx, query)
	return err
}

func (s *PostgresStore) MigrateSchema(ctx context.Context, schema Schema) error {
	// For now, just ensure tables exist
	return s.InitSchema(ctx, schema)
}

func (s *PostgresStore) StoreBlock(ctx context.Context, table string, block *Block) error {
	txIDsJSON, _ := json.Marshal(block.TxIDs)

	query := fmt.Sprintf(`
		INSERT INTO %s (id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status, metadata = EXCLUDED.metadata
	`, table)

	_, err := s.db.ExecContext(ctx, query,
		block.ID, block.ParentID, block.Height, block.Timestamp, block.Status,
		block.TxCount, txIDsJSON, block.Data, block.Metadata, time.Now())
	return err
}

func (s *PostgresStore) GetBlock(ctx context.Context, table string, id string) (*Block, error) {
	query := fmt.Sprintf(`
		SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at
		FROM %s WHERE id = $1
	`, table)

	var b Block
	var txIDsJSON, dataJSON, metaJSON []byte
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&b.ID, &b.ParentID, &b.Height, &b.Timestamp, &b.Status,
		&b.TxCount, &txIDsJSON, &dataJSON, &metaJSON, &b.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	_ = json.Unmarshal(txIDsJSON, &b.TxIDs)
	b.Data = dataJSON
	b.Metadata = metaJSON
	return &b, nil
}

func (s *PostgresStore) GetBlockByHeight(ctx context.Context, table string, height uint64) (*Block, error) {
	query := fmt.Sprintf(`
		SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at
		FROM %s WHERE height = $1
	`, table)

	var b Block
	var txIDsJSON, dataJSON, metaJSON []byte
	err := s.db.QueryRowContext(ctx, query, height).Scan(
		&b.ID, &b.ParentID, &b.Height, &b.Timestamp, &b.Status,
		&b.TxCount, &txIDsJSON, &dataJSON, &metaJSON, &b.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	_ = json.Unmarshal(txIDsJSON, &b.TxIDs)
	b.Data = dataJSON
	b.Metadata = metaJSON
	return &b, nil
}

func (s *PostgresStore) GetRecentBlocks(ctx context.Context, table string, limit int) ([]*Block, error) {
	query := fmt.Sprintf(`
		SELECT id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at
		FROM %s ORDER BY height DESC LIMIT $1
	`, table)

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocks []*Block
	for rows.Next() {
		var b Block
		var txIDsJSON, dataJSON, metaJSON []byte
		if err := rows.Scan(&b.ID, &b.ParentID, &b.Height, &b.Timestamp, &b.Status,
			&b.TxCount, &txIDsJSON, &dataJSON, &metaJSON, &b.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(txIDsJSON, &b.TxIDs)
		b.Data = dataJSON
		b.Metadata = metaJSON
		blocks = append(blocks, &b)
	}
	return blocks, rows.Err()
}

func (s *PostgresStore) StoreVertex(ctx context.Context, table string, vertex *Vertex) error {
	parentIDsJSON, _ := json.Marshal(vertex.ParentIDs)
	txIDsJSON, _ := json.Marshal(vertex.TxIDs)

	query := fmt.Sprintf(`
		INSERT INTO %s (id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status, metadata = EXCLUDED.metadata
	`, table)

	_, err := s.db.ExecContext(ctx, query,
		vertex.ID, vertex.Type, parentIDsJSON, vertex.Height, vertex.Epoch,
		txIDsJSON, vertex.Timestamp, vertex.Status, vertex.Data, vertex.Metadata, time.Now())
	return err
}

func (s *PostgresStore) GetVertex(ctx context.Context, table string, id string) (*Vertex, error) {
	query := fmt.Sprintf(`
		SELECT id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at
		FROM %s WHERE id = $1
	`, table)

	var v Vertex
	var parentIDsJSON, txIDsJSON, dataJSON, metaJSON []byte
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&v.ID, &v.Type, &parentIDsJSON, &v.Height, &v.Epoch,
		&txIDsJSON, &v.Timestamp, &v.Status, &dataJSON, &metaJSON, &v.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	_ = json.Unmarshal(parentIDsJSON, &v.ParentIDs)
	_ = json.Unmarshal(txIDsJSON, &v.TxIDs)
	v.Data = dataJSON
	v.Metadata = metaJSON
	return &v, nil
}

func (s *PostgresStore) GetRecentVertices(ctx context.Context, table string, limit int) ([]*Vertex, error) {
	query := fmt.Sprintf(`
		SELECT id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at
		FROM %s ORDER BY timestamp DESC LIMIT $1
	`, table)

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vertices []*Vertex
	for rows.Next() {
		var v Vertex
		var parentIDsJSON, txIDsJSON, dataJSON, metaJSON []byte
		if err := rows.Scan(&v.ID, &v.Type, &parentIDsJSON, &v.Height, &v.Epoch,
			&txIDsJSON, &v.Timestamp, &v.Status, &dataJSON, &metaJSON, &v.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(parentIDsJSON, &v.ParentIDs)
		_ = json.Unmarshal(txIDsJSON, &v.TxIDs)
		v.Data = dataJSON
		v.Metadata = metaJSON
		vertices = append(vertices, &v)
	}
	return vertices, rows.Err()
}

func (s *PostgresStore) StoreEdge(ctx context.Context, table string, edge *Edge) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (source, target, type, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING
	`, table)

	_, err := s.db.ExecContext(ctx, query, edge.Source, edge.Target, edge.Type, time.Now())
	return err
}

func (s *PostgresStore) GetEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	query := fmt.Sprintf(`
		SELECT source, target, type, created_at
		FROM %s WHERE source = $1 OR target = $1
	`, table)

	rows, err := s.db.QueryContext(ctx, query, vertexID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []*Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.Source, &e.Target, &e.Type, &e.CreatedAt); err != nil {
			return nil, err
		}
		edges = append(edges, &e)
	}
	return edges, rows.Err()
}

func (s *PostgresStore) Put(ctx context.Context, table string, key string, value []byte) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (key, value, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
	`, table)

	_, err := s.db.ExecContext(ctx, query, key, value, time.Now())
	return err
}

func (s *PostgresStore) Get(ctx context.Context, table string, key string) ([]byte, error) {
	query := fmt.Sprintf("SELECT value FROM %s WHERE key = $1", table)

	var value []byte
	err := s.db.QueryRowContext(ctx, query, key).Scan(&value)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return value, err
}

func (s *PostgresStore) Delete(ctx context.Context, table string, key string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE key = $1", table)
	_, err := s.db.ExecContext(ctx, query, key)
	return err
}

func (s *PostgresStore) List(ctx context.Context, table string, prefix string, limit int) ([]KV, error) {
	query := fmt.Sprintf("SELECT key, value FROM %s WHERE key LIKE $1 LIMIT $2", table)

	rows, err := s.db.QueryContext(ctx, query, prefix+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []KV
	for rows.Next() {
		var kv KV
		if err := rows.Scan(&kv.Key, &kv.Value); err != nil {
			return nil, err
		}
		results = append(results, kv)
	}
	return results, rows.Err()
}

func (s *PostgresStore) UpdateStats(ctx context.Context, table string, stats map[string]interface{}) error {
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
		INSERT INTO %s_stats (id, stats, updated_at)
		VALUES (1, $1, $2)
		ON CONFLICT (id) DO UPDATE SET stats = EXCLUDED.stats, updated_at = EXCLUDED.updated_at
	`, table)

	_, err = s.db.ExecContext(ctx, query, statsJSON, time.Now())
	return err
}

func (s *PostgresStore) GetStats(ctx context.Context, table string) (map[string]interface{}, error) {
	query := fmt.Sprintf("SELECT stats FROM %s_stats WHERE id = 1", table)

	var statsJSON []byte
	err := s.db.QueryRowContext(ctx, query).Scan(&statsJSON)
	if err == sql.ErrNoRows {
		return make(map[string]interface{}), nil
	}
	if err != nil {
		return nil, err
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(statsJSON, &stats); err != nil {
		return nil, err
	}
	return stats, nil
}

func (s *PostgresStore) Query(ctx context.Context, query string, args ...interface{}) ([]map[string]interface{}, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(cols))
		valuePtrs := make([]interface{}, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		row := make(map[string]interface{})
		for i, col := range cols {
			row[col] = values[i]
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *PostgresStore) Exec(ctx context.Context, query string, args ...interface{}) error {
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *PostgresStore) Begin(ctx context.Context) (Transaction, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &PostgresTx{tx: tx, store: s}, nil
}

// PostgresTx implements Transaction for PostgreSQL
type PostgresTx struct {
	tx    *sql.Tx
	store *PostgresStore
}

func (t *PostgresTx) Commit() error {
	return t.tx.Commit()
}

func (t *PostgresTx) Rollback() error {
	return t.tx.Rollback()
}

// Implement Store interface methods for transaction
// (These wrap the transaction instead of the db)

func (t *PostgresTx) Init(ctx context.Context) error {
	return nil
}

func (t *PostgresTx) Close() error {
	return nil
}

func (t *PostgresTx) Ping(ctx context.Context) error {
	return nil
}

func (t *PostgresTx) InitSchema(ctx context.Context, schema Schema) error {
	return ErrNotSupported
}

func (t *PostgresTx) MigrateSchema(ctx context.Context, schema Schema) error {
	return ErrNotSupported
}

func (t *PostgresTx) StoreBlock(ctx context.Context, table string, block *Block) error {
	txIDsJSON, _ := json.Marshal(block.TxIDs)

	query := fmt.Sprintf(`
		INSERT INTO %s (id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status, metadata = EXCLUDED.metadata
	`, table)

	_, err := t.tx.ExecContext(ctx, query,
		block.ID, block.ParentID, block.Height, block.Timestamp, block.Status,
		block.TxCount, txIDsJSON, block.Data, block.Metadata, time.Now())
	return err
}

func (t *PostgresTx) GetBlock(ctx context.Context, table string, id string) (*Block, error) {
	return t.store.GetBlock(ctx, table, id) // Use store for reads
}

func (t *PostgresTx) GetBlockByHeight(ctx context.Context, table string, height uint64) (*Block, error) {
	return t.store.GetBlockByHeight(ctx, table, height)
}

func (t *PostgresTx) GetRecentBlocks(ctx context.Context, table string, limit int) ([]*Block, error) {
	return t.store.GetRecentBlocks(ctx, table, limit)
}

func (t *PostgresTx) StoreVertex(ctx context.Context, table string, vertex *Vertex) error {
	parentIDsJSON, _ := json.Marshal(vertex.ParentIDs)
	txIDsJSON, _ := json.Marshal(vertex.TxIDs)

	query := fmt.Sprintf(`
		INSERT INTO %s (id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status, metadata = EXCLUDED.metadata
	`, table)

	_, err := t.tx.ExecContext(ctx, query,
		vertex.ID, vertex.Type, parentIDsJSON, vertex.Height, vertex.Epoch,
		txIDsJSON, vertex.Timestamp, vertex.Status, vertex.Data, vertex.Metadata, time.Now())
	return err
}

func (t *PostgresTx) GetVertex(ctx context.Context, table string, id string) (*Vertex, error) {
	return t.store.GetVertex(ctx, table, id)
}

func (t *PostgresTx) GetRecentVertices(ctx context.Context, table string, limit int) ([]*Vertex, error) {
	return t.store.GetRecentVertices(ctx, table, limit)
}

func (t *PostgresTx) StoreEdge(ctx context.Context, table string, edge *Edge) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (source, target, type, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING
	`, table)

	_, err := t.tx.ExecContext(ctx, query, edge.Source, edge.Target, edge.Type, time.Now())
	return err
}

func (t *PostgresTx) GetEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	return t.store.GetEdges(ctx, table, vertexID)
}

func (t *PostgresTx) Put(ctx context.Context, table string, key string, value []byte) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (key, value, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
	`, table)

	_, err := t.tx.ExecContext(ctx, query, key, value, time.Now())
	return err
}

func (t *PostgresTx) Get(ctx context.Context, table string, key string) ([]byte, error) {
	return t.store.Get(ctx, table, key)
}

func (t *PostgresTx) Delete(ctx context.Context, table string, key string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE key = $1", table)
	_, err := t.tx.ExecContext(ctx, query, key)
	return err
}

func (t *PostgresTx) List(ctx context.Context, table string, prefix string, limit int) ([]KV, error) {
	return t.store.List(ctx, table, prefix, limit)
}

func (t *PostgresTx) UpdateStats(ctx context.Context, table string, stats map[string]interface{}) error {
	return t.store.UpdateStats(ctx, table, stats)
}

func (t *PostgresTx) GetStats(ctx context.Context, table string) (map[string]interface{}, error) {
	return t.store.GetStats(ctx, table)
}

func (t *PostgresTx) Query(ctx context.Context, query string, args ...interface{}) ([]map[string]interface{}, error) {
	return t.store.Query(ctx, query, args...)
}

func (t *PostgresTx) Exec(ctx context.Context, query string, args ...interface{}) error {
	_, err := t.tx.ExecContext(ctx, query, args...)
	return err
}

func (t *PostgresTx) Begin(ctx context.Context) (Transaction, error) {
	return nil, ErrNotSupported // Nested transactions not supported
}
