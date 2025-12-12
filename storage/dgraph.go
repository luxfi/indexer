// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/dgraph-io/dgo/v230"
	"github.com/dgraph-io/dgo/v230/protos/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// DgraphStore implements Store using Dgraph
type DgraphStore struct {
	client *dgo.Dgraph
	conn   *grpc.ClientConn
	cfg    Config
	mu     sync.RWMutex
	closed bool
}

// NewDgraph creates a new Dgraph storage backend
func NewDgraph(cfg Config) (*DgraphStore, error) {
	return &DgraphStore{
		cfg: cfg,
	}, nil
}

// Init initializes the Dgraph connection
func (s *DgraphStore) Init(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	url := s.cfg.URL
	if url == "" {
		url = "localhost:9080"
	}

	conn, err := grpc.DialContext(ctx, url,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to dgraph: %w", err)
	}

	s.conn = conn
	s.client = dgo.NewDgraphClient(api.NewDgraphClient(conn))

	return nil
}

// Close closes the Dgraph connection
func (s *DgraphStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// Ping checks if the database is accessible
func (s *DgraphStore) Ping(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed || s.client == nil {
		return ErrClosed
	}

	// Simple health check via alter with empty schema
	return s.client.Alter(ctx, &api.Operation{
		Schema: "",
	})
}

// InitSchema initializes the Dgraph schema
func (s *DgraphStore) InitSchema(ctx context.Context, schema Schema) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrClosed
	}

	// Define Dgraph schema for blocks, vertices, and edges
	dgraphSchema := `
		# Block schema
		block_id: string @index(exact) .
		block_parent_id: string @index(exact) .
		block_height: int @index(int) .
		block_timestamp: datetime @index(day) .
		block_status: string @index(exact) .
		block_tx_count: int .
		block_tx_ids: [string] .
		block_data: string .
		block_metadata: string .
		block_table: string @index(exact) .
		block_created_at: datetime .

		# Vertex schema
		vertex_id: string @index(exact) .
		vertex_type: string @index(exact) .
		vertex_parent_ids: [string] .
		vertex_height: int @index(int) .
		vertex_epoch: int .
		vertex_tx_ids: [string] .
		vertex_timestamp: datetime @index(day) .
		vertex_status: string @index(exact) .
		vertex_data: string .
		vertex_metadata: string .
		vertex_table: string @index(exact) .
		vertex_created_at: datetime .

		# Edge schema
		edge_source: string @index(exact) .
		edge_target: string @index(exact) .
		edge_type: string @index(exact) .
		edge_table: string @index(exact) .
		edge_created_at: datetime .

		# KV schema
		kv_key: string @index(exact) .
		kv_value: string .
		kv_table: string @index(exact) .

		# Stats schema
		stats_table: string @index(exact) .
		stats_data: string .

		type Block {
			block_id
			block_parent_id
			block_height
			block_timestamp
			block_status
			block_tx_count
			block_tx_ids
			block_data
			block_metadata
			block_table
			block_created_at
		}

		type Vertex {
			vertex_id
			vertex_type
			vertex_parent_ids
			vertex_height
			vertex_epoch
			vertex_tx_ids
			vertex_timestamp
			vertex_status
			vertex_data
			vertex_metadata
			vertex_table
			vertex_created_at
		}

		type Edge {
			edge_source
			edge_target
			edge_type
			edge_table
			edge_created_at
		}

		type KV {
			kv_key
			kv_value
			kv_table
		}

		type Stats {
			stats_table
			stats_data
		}
	`

	return s.client.Alter(ctx, &api.Operation{
		Schema: dgraphSchema,
	})
}

// MigrateSchema migrates the schema
func (s *DgraphStore) MigrateSchema(ctx context.Context, schema Schema) error {
	// For Dgraph, schema changes are additive, so we can just re-apply
	return s.InitSchema(ctx, schema)
}

// StoreBlock stores a block in Dgraph
func (s *DgraphStore) StoreBlock(ctx context.Context, table string, block *Block) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrClosed
	}

	// First, check if block already exists
	query := fmt.Sprintf(`{
		block(func: eq(block_id, %q)) @filter(eq(block_table, %q)) {
			uid
		}
	}`, block.ID, table)

	resp, err := s.client.NewTxn().Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query existing block: %w", err)
	}

	type queryResult struct {
		Block []struct {
			UID string `json:"uid"`
		} `json:"block"`
	}
	var result queryResult
	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return fmt.Errorf("failed to unmarshal query result: %w", err)
	}

	// Prepare the block data
	blockData := map[string]interface{}{
		"dgraph.type":      "Block",
		"block_id":         block.ID,
		"block_parent_id":  block.ParentID,
		"block_height":     block.Height,
		"block_timestamp":  block.Timestamp,
		"block_status":     block.Status,
		"block_tx_count":   block.TxCount,
		"block_tx_ids":     block.TxIDs,
		"block_data":       string(block.Data),
		"block_metadata":   string(block.Metadata),
		"block_table":      table,
		"block_created_at": block.CreatedAt,
	}

	// If block exists, update it
	if len(result.Block) > 0 {
		blockData["uid"] = result.Block[0].UID
	}

	data, err := json.Marshal(blockData)
	if err != nil {
		return fmt.Errorf("failed to marshal block: %w", err)
	}

	mu := &api.Mutation{
		SetJson:   data,
		CommitNow: true,
	}

	_, err = s.client.NewTxn().Mutate(ctx, mu)
	return err
}

// GetBlock retrieves a block by ID
func (s *DgraphStore) GetBlock(ctx context.Context, table string, id string) (*Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	query := fmt.Sprintf(`{
		block(func: eq(block_id, %q)) @filter(eq(block_table, %q)) {
			block_id
			block_parent_id
			block_height
			block_timestamp
			block_status
			block_tx_count
			block_tx_ids
			block_data
			block_metadata
			block_created_at
		}
	}`, id, table)

	resp, err := s.client.NewTxn().Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query block: %w", err)
	}

	type blockResult struct {
		Block []struct {
			ID        string   `json:"block_id"`
			ParentID  string   `json:"block_parent_id"`
			Height    uint64   `json:"block_height"`
			Timestamp string   `json:"block_timestamp"`
			Status    string   `json:"block_status"`
			TxCount   int      `json:"block_tx_count"`
			TxIDs     []string `json:"block_tx_ids"`
			Data      string   `json:"block_data"`
			Metadata  string   `json:"block_metadata"`
			CreatedAt string   `json:"block_created_at"`
		} `json:"block"`
	}

	var result blockResult
	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	if len(result.Block) == 0 {
		return nil, ErrNotFound
	}

	b := result.Block[0]
	block := &Block{
		ID:       b.ID,
		ParentID: b.ParentID,
		Height:   b.Height,
		Status:   b.Status,
		TxCount:  b.TxCount,
		TxIDs:    b.TxIDs,
		Data:     json.RawMessage(b.Data),
		Metadata: json.RawMessage(b.Metadata),
	}

	return block, nil
}

// GetBlockByHeight retrieves a block by height
func (s *DgraphStore) GetBlockByHeight(ctx context.Context, table string, height uint64) (*Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	query := fmt.Sprintf(`{
		block(func: eq(block_height, %d)) @filter(eq(block_table, %q)) {
			block_id
			block_parent_id
			block_height
			block_timestamp
			block_status
			block_tx_count
			block_tx_ids
			block_data
			block_metadata
			block_created_at
		}
	}`, height, table)

	resp, err := s.client.NewTxn().Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query block: %w", err)
	}

	type blockResult struct {
		Block []struct {
			ID        string   `json:"block_id"`
			ParentID  string   `json:"block_parent_id"`
			Height    uint64   `json:"block_height"`
			Timestamp string   `json:"block_timestamp"`
			Status    string   `json:"block_status"`
			TxCount   int      `json:"block_tx_count"`
			TxIDs     []string `json:"block_tx_ids"`
			Data      string   `json:"block_data"`
			Metadata  string   `json:"block_metadata"`
			CreatedAt string   `json:"block_created_at"`
		} `json:"block"`
	}

	var result blockResult
	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	if len(result.Block) == 0 {
		return nil, ErrNotFound
	}

	b := result.Block[0]
	return &Block{
		ID:       b.ID,
		ParentID: b.ParentID,
		Height:   b.Height,
		Status:   b.Status,
		TxCount:  b.TxCount,
		TxIDs:    b.TxIDs,
		Data:     json.RawMessage(b.Data),
		Metadata: json.RawMessage(b.Metadata),
	}, nil
}

// GetRecentBlocks retrieves recent blocks
func (s *DgraphStore) GetRecentBlocks(ctx context.Context, table string, limit int) ([]*Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	query := fmt.Sprintf(`{
		blocks(func: eq(block_table, %q), orderdesc: block_height, first: %d) {
			block_id
			block_parent_id
			block_height
			block_timestamp
			block_status
			block_tx_count
			block_tx_ids
			block_data
			block_metadata
			block_created_at
		}
	}`, table, limit)

	resp, err := s.client.NewTxn().Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query blocks: %w", err)
	}

	type blocksResult struct {
		Blocks []struct {
			ID        string   `json:"block_id"`
			ParentID  string   `json:"block_parent_id"`
			Height    uint64   `json:"block_height"`
			Timestamp string   `json:"block_timestamp"`
			Status    string   `json:"block_status"`
			TxCount   int      `json:"block_tx_count"`
			TxIDs     []string `json:"block_tx_ids"`
			Data      string   `json:"block_data"`
			Metadata  string   `json:"block_metadata"`
			CreatedAt string   `json:"block_created_at"`
		} `json:"blocks"`
	}

	var result blocksResult
	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	blocks := make([]*Block, 0, len(result.Blocks))
	for _, b := range result.Blocks {
		blocks = append(blocks, &Block{
			ID:       b.ID,
			ParentID: b.ParentID,
			Height:   b.Height,
			Status:   b.Status,
			TxCount:  b.TxCount,
			TxIDs:    b.TxIDs,
			Data:     json.RawMessage(b.Data),
			Metadata: json.RawMessage(b.Metadata),
		})
	}

	return blocks, nil
}

// StoreVertex stores a vertex
func (s *DgraphStore) StoreVertex(ctx context.Context, table string, vertex *Vertex) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrClosed
	}

	vertexData := map[string]interface{}{
		"dgraph.type":       "Vertex",
		"vertex_id":         vertex.ID,
		"vertex_type":       vertex.Type,
		"vertex_parent_ids": vertex.ParentIDs,
		"vertex_height":     vertex.Height,
		"vertex_epoch":      vertex.Epoch,
		"vertex_tx_ids":     vertex.TxIDs,
		"vertex_timestamp":  vertex.Timestamp,
		"vertex_status":     vertex.Status,
		"vertex_data":       string(vertex.Data),
		"vertex_metadata":   string(vertex.Metadata),
		"vertex_table":      table,
		"vertex_created_at": vertex.CreatedAt,
	}

	data, err := json.Marshal(vertexData)
	if err != nil {
		return fmt.Errorf("failed to marshal vertex: %w", err)
	}

	mu := &api.Mutation{
		SetJson:   data,
		CommitNow: true,
	}

	_, err = s.client.NewTxn().Mutate(ctx, mu)
	return err
}

// GetVertex retrieves a vertex by ID
func (s *DgraphStore) GetVertex(ctx context.Context, table string, id string) (*Vertex, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	query := fmt.Sprintf(`{
		vertex(func: eq(vertex_id, %q)) @filter(eq(vertex_table, %q)) {
			vertex_id
			vertex_type
			vertex_parent_ids
			vertex_height
			vertex_epoch
			vertex_tx_ids
			vertex_timestamp
			vertex_status
			vertex_data
			vertex_metadata
			vertex_created_at
		}
	}`, id, table)

	resp, err := s.client.NewTxn().Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query vertex: %w", err)
	}

	type vertexResult struct {
		Vertex []struct {
			ID        string   `json:"vertex_id"`
			Type      string   `json:"vertex_type"`
			ParentIDs []string `json:"vertex_parent_ids"`
			Height    uint64   `json:"vertex_height"`
			Epoch     uint32   `json:"vertex_epoch"`
			TxIDs     []string `json:"vertex_tx_ids"`
			Timestamp string   `json:"vertex_timestamp"`
			Status    string   `json:"vertex_status"`
			Data      string   `json:"vertex_data"`
			Metadata  string   `json:"vertex_metadata"`
			CreatedAt string   `json:"vertex_created_at"`
		} `json:"vertex"`
	}

	var result vertexResult
	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	if len(result.Vertex) == 0 {
		return nil, ErrNotFound
	}

	v := result.Vertex[0]
	return &Vertex{
		ID:        v.ID,
		Type:      v.Type,
		ParentIDs: v.ParentIDs,
		Height:    v.Height,
		Epoch:     v.Epoch,
		TxIDs:     v.TxIDs,
		Status:    v.Status,
		Data:      json.RawMessage(v.Data),
		Metadata:  json.RawMessage(v.Metadata),
	}, nil
}

// GetRecentVertices retrieves recent vertices
func (s *DgraphStore) GetRecentVertices(ctx context.Context, table string, limit int) ([]*Vertex, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	query := fmt.Sprintf(`{
		vertices(func: eq(vertex_table, %q), orderdesc: vertex_height, first: %d) {
			vertex_id
			vertex_type
			vertex_parent_ids
			vertex_height
			vertex_epoch
			vertex_tx_ids
			vertex_timestamp
			vertex_status
			vertex_data
			vertex_metadata
			vertex_created_at
		}
	}`, table, limit)

	resp, err := s.client.NewTxn().Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query vertices: %w", err)
	}

	type verticesResult struct {
		Vertices []struct {
			ID        string   `json:"vertex_id"`
			Type      string   `json:"vertex_type"`
			ParentIDs []string `json:"vertex_parent_ids"`
			Height    uint64   `json:"vertex_height"`
			Epoch     uint32   `json:"vertex_epoch"`
			TxIDs     []string `json:"vertex_tx_ids"`
			Timestamp string   `json:"vertex_timestamp"`
			Status    string   `json:"vertex_status"`
			Data      string   `json:"vertex_data"`
			Metadata  string   `json:"vertex_metadata"`
			CreatedAt string   `json:"vertex_created_at"`
		} `json:"vertices"`
	}

	var result verticesResult
	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	vertices := make([]*Vertex, 0, len(result.Vertices))
	for _, v := range result.Vertices {
		vertices = append(vertices, &Vertex{
			ID:        v.ID,
			Type:      v.Type,
			ParentIDs: v.ParentIDs,
			Height:    v.Height,
			Epoch:     v.Epoch,
			TxIDs:     v.TxIDs,
			Status:    v.Status,
			Data:      json.RawMessage(v.Data),
			Metadata:  json.RawMessage(v.Metadata),
		})
	}

	return vertices, nil
}

// StoreEdge stores an edge
func (s *DgraphStore) StoreEdge(ctx context.Context, table string, edge *Edge) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrClosed
	}

	edgeData := map[string]interface{}{
		"dgraph.type":     "Edge",
		"edge_source":     edge.Source,
		"edge_target":     edge.Target,
		"edge_type":       edge.Type,
		"edge_table":      table,
		"edge_created_at": edge.CreatedAt,
	}

	data, err := json.Marshal(edgeData)
	if err != nil {
		return fmt.Errorf("failed to marshal edge: %w", err)
	}

	mu := &api.Mutation{
		SetJson:   data,
		CommitNow: true,
	}

	_, err = s.client.NewTxn().Mutate(ctx, mu)
	return err
}

// GetEdges retrieves edges for a vertex
func (s *DgraphStore) GetEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	query := fmt.Sprintf(`{
		edges(func: eq(edge_source, %q)) @filter(eq(edge_table, %q)) {
			edge_source
			edge_target
			edge_type
			edge_created_at
		}
	}`, vertexID, table)

	resp, err := s.client.NewTxn().Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query edges: %w", err)
	}

	type edgesResult struct {
		Edges []struct {
			Source    string `json:"edge_source"`
			Target    string `json:"edge_target"`
			Type      string `json:"edge_type"`
			CreatedAt string `json:"edge_created_at"`
		} `json:"edges"`
	}

	var result edgesResult
	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	edges := make([]*Edge, 0, len(result.Edges))
	for _, e := range result.Edges {
		edges = append(edges, &Edge{
			Source: e.Source,
			Target: e.Target,
			Type:   e.Type,
		})
	}

	return edges, nil
}

// Put stores a key-value pair
func (s *DgraphStore) Put(ctx context.Context, table string, k string, value []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrClosed
	}

	kvData := map[string]interface{}{
		"dgraph.type": "KV",
		"kv_key":      k,
		"kv_value":    string(value),
		"kv_table":    table,
	}

	data, err := json.Marshal(kvData)
	if err != nil {
		return fmt.Errorf("failed to marshal kv: %w", err)
	}

	mu := &api.Mutation{
		SetJson:   data,
		CommitNow: true,
	}

	_, err = s.client.NewTxn().Mutate(ctx, mu)
	return err
}

// Get retrieves a value by key
func (s *DgraphStore) Get(ctx context.Context, table string, k string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	query := fmt.Sprintf(`{
		kv(func: eq(kv_key, %q)) @filter(eq(kv_table, %q)) {
			kv_value
		}
	}`, k, table)

	resp, err := s.client.NewTxn().Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query kv: %w", err)
	}

	type kvResult struct {
		KV []struct {
			Value string `json:"kv_value"`
		} `json:"kv"`
	}

	var result kvResult
	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	if len(result.KV) == 0 {
		return nil, ErrNotFound
	}

	return []byte(result.KV[0].Value), nil
}

// Delete removes a key-value pair
func (s *DgraphStore) Delete(ctx context.Context, table string, k string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrClosed
	}

	// First find the uid
	query := fmt.Sprintf(`{
		kv(func: eq(kv_key, %q)) @filter(eq(kv_table, %q)) {
			uid
		}
	}`, k, table)

	resp, err := s.client.NewTxn().Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query kv: %w", err)
	}

	type kvResult struct {
		KV []struct {
			UID string `json:"uid"`
		} `json:"kv"`
	}

	var result kvResult
	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return fmt.Errorf("failed to unmarshal result: %w", err)
	}

	if len(result.KV) == 0 {
		return nil // Nothing to delete
	}

	// Delete the node
	deleteData := fmt.Sprintf(`{"uid": %q}`, result.KV[0].UID)
	mu := &api.Mutation{
		DeleteJson: []byte(deleteData),
		CommitNow:  true,
	}

	_, err = s.client.NewTxn().Mutate(ctx, mu)
	return err
}

// List retrieves key-value pairs with a prefix
func (s *DgraphStore) List(ctx context.Context, table string, prefix string, limit int) ([]KV, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	// Dgraph doesn't have prefix matching, so we filter by table and do client-side filtering
	query := fmt.Sprintf(`{
		kvs(func: eq(kv_table, %q), first: %d) {
			kv_key
			kv_value
		}
	}`, table, limit*10) // Over-fetch to account for filtering

	resp, err := s.client.NewTxn().Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query kvs: %w", err)
	}

	type kvsResult struct {
		KVs []struct {
			Key   string `json:"kv_key"`
			Value string `json:"kv_value"`
		} `json:"kvs"`
	}

	var result kvsResult
	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	kvs := make([]KV, 0)
	for _, kv := range result.KVs {
		if len(prefix) == 0 || len(kv.Key) >= len(prefix) && kv.Key[:len(prefix)] == prefix {
			kvs = append(kvs, KV{Key: kv.Key, Value: []byte(kv.Value)})
			if limit > 0 && len(kvs) >= limit {
				break
			}
		}
	}

	return kvs, nil
}

// UpdateStats updates statistics
func (s *DgraphStore) UpdateStats(ctx context.Context, table string, stats map[string]interface{}) error {
	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal stats: %w", err)
	}

	statsData := map[string]interface{}{
		"dgraph.type": "Stats",
		"stats_table": table,
		"stats_data":  string(data),
	}

	jsonData, err := json.Marshal(statsData)
	if err != nil {
		return fmt.Errorf("failed to marshal stats data: %w", err)
	}

	mu := &api.Mutation{
		SetJson:   jsonData,
		CommitNow: true,
	}

	_, err = s.client.NewTxn().Mutate(ctx, mu)
	return err
}

// GetStats retrieves statistics
func (s *DgraphStore) GetStats(ctx context.Context, table string) (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	query := fmt.Sprintf(`{
		stats(func: eq(stats_table, %q)) {
			stats_data
		}
	}`, table)

	resp, err := s.client.NewTxn().Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query stats: %w", err)
	}

	type statsResult struct {
		Stats []struct {
			Data string `json:"stats_data"`
		} `json:"stats"`
	}

	var result statsResult
	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	if len(result.Stats) == 0 {
		return make(map[string]interface{}), nil
	}

	var stats map[string]interface{}
	if err := json.Unmarshal([]byte(result.Stats[0].Data), &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stats: %w", err)
	}

	return stats, nil
}

// Query executes a DQL query
func (s *DgraphStore) Query(ctx context.Context, query string, args ...interface{}) ([]map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	resp, err := s.client.NewTxn().Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	var result map[string][]map[string]interface{}
	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	// Flatten all results into a single slice
	var rows []map[string]interface{}
	for _, v := range result {
		rows = append(rows, v...)
	}

	return rows, nil
}

// Exec executes a DQL mutation
func (s *DgraphStore) Exec(ctx context.Context, query string, args ...interface{}) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrClosed
	}

	mu := &api.Mutation{
		SetNquads: []byte(query),
		CommitNow: true,
	}

	_, err := s.client.NewTxn().Mutate(ctx, mu)
	return err
}

// Begin starts a transaction
func (s *DgraphStore) Begin(ctx context.Context) (Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrClosed
	}

	return &DgraphTransaction{
		store: s,
		txn:   s.client.NewTxn(),
	}, nil
}

// DgraphTransaction implements Transaction for Dgraph
type DgraphTransaction struct {
	store *DgraphStore
	txn   *dgo.Txn
}

// Commit commits the transaction
func (t *DgraphTransaction) Commit() error {
	return t.txn.Commit(context.Background())
}

// Rollback rolls back the transaction
func (t *DgraphTransaction) Rollback() error {
	return t.txn.Discard(context.Background())
}

// All other methods delegate to the store
func (t *DgraphTransaction) Init(ctx context.Context) error                                { return nil }
func (t *DgraphTransaction) Close() error                                                  { return nil }
func (t *DgraphTransaction) Ping(ctx context.Context) error                                { return t.store.Ping(ctx) }
func (t *DgraphTransaction) InitSchema(ctx context.Context, schema Schema) error           { return nil }
func (t *DgraphTransaction) MigrateSchema(ctx context.Context, schema Schema) error        { return nil }
func (t *DgraphTransaction) StoreBlock(ctx context.Context, table string, block *Block) error {
	return t.store.StoreBlock(ctx, table, block)
}
func (t *DgraphTransaction) GetBlock(ctx context.Context, table string, id string) (*Block, error) {
	return t.store.GetBlock(ctx, table, id)
}
func (t *DgraphTransaction) GetBlockByHeight(ctx context.Context, table string, height uint64) (*Block, error) {
	return t.store.GetBlockByHeight(ctx, table, height)
}
func (t *DgraphTransaction) GetRecentBlocks(ctx context.Context, table string, limit int) ([]*Block, error) {
	return t.store.GetRecentBlocks(ctx, table, limit)
}
func (t *DgraphTransaction) StoreVertex(ctx context.Context, table string, vertex *Vertex) error {
	return t.store.StoreVertex(ctx, table, vertex)
}
func (t *DgraphTransaction) GetVertex(ctx context.Context, table string, id string) (*Vertex, error) {
	return t.store.GetVertex(ctx, table, id)
}
func (t *DgraphTransaction) GetRecentVertices(ctx context.Context, table string, limit int) ([]*Vertex, error) {
	return t.store.GetRecentVertices(ctx, table, limit)
}
func (t *DgraphTransaction) StoreEdge(ctx context.Context, table string, edge *Edge) error {
	return t.store.StoreEdge(ctx, table, edge)
}
func (t *DgraphTransaction) GetEdges(ctx context.Context, table string, vertexID string) ([]*Edge, error) {
	return t.store.GetEdges(ctx, table, vertexID)
}
func (t *DgraphTransaction) Put(ctx context.Context, table string, k string, value []byte) error {
	return t.store.Put(ctx, table, k, value)
}
func (t *DgraphTransaction) Get(ctx context.Context, table string, k string) ([]byte, error) {
	return t.store.Get(ctx, table, k)
}
func (t *DgraphTransaction) Delete(ctx context.Context, table string, k string) error {
	return t.store.Delete(ctx, table, k)
}
func (t *DgraphTransaction) List(ctx context.Context, table string, prefix string, limit int) ([]KV, error) {
	return t.store.List(ctx, table, prefix, limit)
}
func (t *DgraphTransaction) UpdateStats(ctx context.Context, table string, stats map[string]interface{}) error {
	return t.store.UpdateStats(ctx, table, stats)
}
func (t *DgraphTransaction) GetStats(ctx context.Context, table string) (map[string]interface{}, error) {
	return t.store.GetStats(ctx, table)
}
func (t *DgraphTransaction) Query(ctx context.Context, query string, args ...interface{}) ([]map[string]interface{}, error) {
	return t.store.Query(ctx, query, args...)
}
func (t *DgraphTransaction) Exec(ctx context.Context, query string, args ...interface{}) error {
	return t.store.Exec(ctx, query, args...)
}
func (t *DgraphTransaction) Begin(ctx context.Context) (Transaction, error) {
	return nil, ErrNotSupported
}
