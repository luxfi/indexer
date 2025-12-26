// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package chain provides shared linear chain indexing for LUX chains (P).
// Based on luxfi/consensus/engine/chain/block - single parent per block.
package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/luxfi/indexer/storage"
)

// ChainType identifies the linear chain
type ChainType string

const (
	ChainP ChainType = "pchain" // Platform - validators, staking
	// Note: Z-Chain moved to DAG package - ZK uses DAG consensus for fast finality
)

// Config for chain indexer
type Config struct {
	ChainType    ChainType
	ChainName    string
	RPCEndpoint  string
	RPCMethod    string // pvm
	HTTPPort     int
	PollInterval time.Duration
	DataDir      string // For default storage
}

// Block represents a linear chain block (from luxfi/consensus)
// Single parent only (linear chain structure)
type Block struct {
	ID        string                 `json:"id"`
	ParentID  string                 `json:"parentId"` // Single parent (linear)
	Height    uint64                 `json:"height"`
	Timestamp time.Time              `json:"timestamp"`
	Status    Status                 `json:"status"`
	TxCount   int                    `json:"txCount,omitempty"`
	TxIDs     []string               `json:"txIds,omitempty"`
	Data      json.RawMessage        `json:"data,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Status of block consensus
type Status string

const (
	StatusPending   Status = "pending"
	StatusAccepted  Status = "accepted"
	StatusRejected  Status = "rejected"
	StatusFinalized Status = "finalized"
)

// Stats for linear chain
type Stats struct {
	TotalBlocks    int64     `json:"total_blocks"`
	LatestHeight   uint64    `json:"latest_height"`
	PendingBlocks  int64     `json:"pending_blocks"`
	AcceptedBlocks int64     `json:"accepted_blocks"`
	ChainType      ChainType `json:"chain_type"`
	LastUpdated    time.Time `json:"last_updated"`
}

// Adapter interface for chain-specific logic
type Adapter interface {
	ParseBlock(data json.RawMessage) (*Block, error)
	GetRecentBlocks(ctx context.Context, limit int) ([]json.RawMessage, error)
	GetBlockByID(ctx context.Context, id string) (json.RawMessage, error)
	GetBlockByHeight(ctx context.Context, height uint64) (json.RawMessage, error)
	InitSchema(ctx context.Context, store storage.Store) error
	GetStats(ctx context.Context, store storage.Store) (map[string]interface{}, error)
}

// Indexer for linear chains
type Indexer struct {
	config     Config
	store      storage.Store
	httpClient *http.Client
	subscriber *Subscriber
	poller     *Poller
	adapter    Adapter
	tableName  string
}

// New creates a new chain indexer with the given storage
func New(cfg Config, store storage.Store, adapter Adapter) (*Indexer, error) {
	if store == nil {
		return nil, fmt.Errorf("storage cannot be nil")
	}

	idx := &Indexer{
		config:     cfg,
		store:      store,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		adapter:    adapter,
		tableName:  string(cfg.ChainType),
	}

	idx.subscriber = NewSubscriber(cfg.ChainType)
	idx.poller = NewPoller(idx, idx.subscriber)

	return idx, nil
}

// Init creates database schema
func (idx *Indexer) Init(ctx context.Context) error {
	schema := storage.Schema{
		Name: idx.tableName,
		Tables: []storage.Table{
			{
				Name: idx.tableName + "_blocks",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "parent_id", Type: storage.TypeText},
					{Name: "height", Type: storage.TypeBigInt, Nullable: false},
					{Name: "timestamp", Type: storage.TypeTimestamp, Nullable: false},
					{Name: "status", Type: storage.TypeText, Default: "'pending'"},
					{Name: "tx_count", Type: storage.TypeInt, Default: "0"},
					{Name: "tx_ids", Type: storage.TypeJSON, Default: "'[]'"},
					{Name: "data", Type: storage.TypeJSON},
					{Name: "metadata", Type: storage.TypeJSON},
					{Name: "created_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
		},
		Indexes: []storage.Index{
			{Name: "idx_" + idx.tableName + "_blocks_height", Table: idx.tableName + "_blocks", Columns: []string{"height"}},
			{Name: "idx_" + idx.tableName + "_blocks_status", Table: idx.tableName + "_blocks", Columns: []string{"status"}},
			{Name: "idx_" + idx.tableName + "_blocks_timestamp", Table: idx.tableName + "_blocks", Columns: []string{"timestamp"}},
			{Name: "idx_" + idx.tableName + "_blocks_parent", Table: idx.tableName + "_blocks", Columns: []string{"parent_id"}},
		},
	}

	if err := idx.store.InitSchema(ctx, schema); err != nil {
		return fmt.Errorf("schema: %w", err)
	}

	if idx.adapter != nil {
		return idx.adapter.InitSchema(ctx, idx.store)
	}
	return nil
}

// StoreBlock stores a block and broadcasts to subscribers
func (idx *Indexer) StoreBlock(ctx context.Context, b *Block) error {
	metaJSON, _ := json.Marshal(b.Metadata)

	sb := &storage.Block{
		ID:        b.ID,
		ParentID:  b.ParentID,
		Height:    b.Height,
		Timestamp: b.Timestamp,
		Status:    string(b.Status),
		TxCount:   b.TxCount,
		TxIDs:     b.TxIDs,
		Data:      b.Data,
		Metadata:  metaJSON,
		CreatedAt: time.Now(),
	}

	if err := idx.store.StoreBlock(ctx, idx.tableName+"_blocks", sb); err != nil {
		return err
	}

	idx.subscriber.BroadcastBlock(b)
	return nil
}

// UpdateStats updates statistics
func (idx *Indexer) UpdateStats(ctx context.Context) error {
	var s Stats
	s.ChainType = idx.config.ChainType

	total, _ := idx.store.Count(ctx, idx.tableName+"_blocks", "1=1")
	pending, _ := idx.store.Count(ctx, idx.tableName+"_blocks", "status='pending'")
	accepted, _ := idx.store.Count(ctx, idx.tableName+"_blocks", "status='accepted'")

	// Get latest height
	rows, _ := idx.store.Query(ctx,
		fmt.Sprintf("SELECT COALESCE(MAX(height), 0) as max_height FROM %s_blocks", idx.tableName))
	var latestHeight uint64
	if len(rows) > 0 {
		if v, ok := rows[0]["max_height"]; ok {
			switch h := v.(type) {
			case int64:
				latestHeight = uint64(h)
			case float64:
				latestHeight = uint64(h)
			}
		}
	}

	s.TotalBlocks = total
	s.PendingBlocks = pending
	s.AcceptedBlocks = accepted
	s.LatestHeight = latestHeight
	s.LastUpdated = time.Now()

	statsMap := map[string]interface{}{
		"total_blocks":    s.TotalBlocks,
		"pending_blocks":  s.PendingBlocks,
		"accepted_blocks": s.AcceptedBlocks,
		"latest_height":   s.LatestHeight,
		"last_updated":    s.LastUpdated,
	}
	return idx.store.UpdateStats(ctx, idx.tableName, statsMap)
}

// Run starts the indexer
func (idx *Indexer) Run(ctx context.Context) error {
	if err := idx.Init(ctx); err != nil {
		return err
	}

	go idx.subscriber.Run(ctx)
	log.Printf("[%s] WebSocket streaming at /api/v2/blocks/subscribe", idx.config.ChainType)

	go idx.poller.Run(ctx)
	log.Printf("[%s] Poller started", idx.config.ChainType)

	go idx.startHTTP(ctx)

	ticker := time.NewTicker(idx.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_ = idx.UpdateStats(ctx)
		}
	}
}

func (idx *Indexer) startHTTP(ctx context.Context) {
	r := mux.NewRouter()
	api := r.PathPrefix("/api/v2").Subrouter()

	api.HandleFunc("/stats", idx.handleStats).Methods("GET")
	api.HandleFunc("/blocks", idx.handleBlocks).Methods("GET")
	api.HandleFunc("/blocks/{id}", idx.handleBlock).Methods("GET")
	api.HandleFunc("/blocks/height/{height}", idx.handleBlockByHeight).Methods("GET")
	api.HandleFunc("/blocks/subscribe", idx.subscriber.HandleWebSocket)

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok", "chain": idx.config.ChainName, "type": "linear",
		})
	})

	handler := corsMiddleware(r)
	server := &http.Server{Addr: fmt.Sprintf(":%d", idx.config.HTTPPort), Handler: handler}

	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

	log.Printf("[%s] API on port %d", idx.config.ChainType, idx.config.HTTPPort)
	_ = server.ListenAndServe()
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "OPTIONS" {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (idx *Indexer) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, _ := idx.store.GetStats(r.Context(), idx.tableName)
	if stats == nil {
		stats = make(map[string]interface{})
	}
	stats["chain_type"] = idx.config.ChainType

	resp := map[string]interface{}{"chain_stats": stats}
	if idx.adapter != nil {
		if cs, _ := idx.adapter.GetStats(r.Context(), idx.store); cs != nil {
			resp["extended_stats"] = cs
		}
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (idx *Indexer) handleBlocks(w http.ResponseWriter, r *http.Request) {
	sbs, err := idx.store.GetRecentBlocks(r.Context(), idx.tableName+"_blocks", 50)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}

	var blocks []Block
	for _, sb := range sbs {
		b := Block{
			ID:        sb.ID,
			ParentID:  sb.ParentID,
			Height:    sb.Height,
			Timestamp: sb.Timestamp,
			Status:    Status(sb.Status),
			TxCount:   sb.TxCount,
			TxIDs:     sb.TxIDs,
			Data:      sb.Data,
		}
		blocks = append(blocks, b)
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": blocks})
}

func (idx *Indexer) handleBlock(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	sb, err := idx.store.GetBlock(r.Context(), idx.tableName+"_blocks", id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	b := Block{
		ID:        sb.ID,
		ParentID:  sb.ParentID,
		Height:    sb.Height,
		Timestamp: sb.Timestamp,
		Status:    Status(sb.Status),
		TxCount:   sb.TxCount,
		TxIDs:     sb.TxIDs,
		Data:      sb.Data,
	}
	_ = json.NewEncoder(w).Encode(b)
}

func (idx *Indexer) handleBlockByHeight(w http.ResponseWriter, r *http.Request) {
	heightStr := mux.Vars(r)["height"]
	var height uint64
	if _, err := fmt.Sscanf(heightStr, "%d", &height); err != nil {
		http.Error(w, "invalid height", 400)
		return
	}

	sb, err := idx.store.GetBlockByHeight(r.Context(), idx.tableName+"_blocks", height)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	b := Block{
		ID:        sb.ID,
		ParentID:  sb.ParentID,
		Height:    sb.Height,
		Timestamp: sb.Timestamp,
		Status:    Status(sb.Status),
		TxCount:   sb.TxCount,
		TxIDs:     sb.TxIDs,
		Data:      sb.Data,
	}
	_ = json.NewEncoder(w).Encode(b)
}

// Store returns the underlying storage for adapters
func (idx *Indexer) Store() storage.Store {
	return idx.store
}

// Subscriber handles WebSocket for live block streaming
type Subscriber struct {
	chainType  ChainType
	clients    map[*websocket.Conn]bool
	broadcast  chan interface{}
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex
	upgrader   websocket.Upgrader
}

func NewSubscriber(ct ChainType) *Subscriber {
	return &Subscriber{
		chainType:  ct,
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan interface{}, 100),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		upgrader:   websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
	}
}

func (s *Subscriber) Run(ctx context.Context) {
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case c := <-s.register:
			s.mu.Lock()
			s.clients[c] = true
			s.mu.Unlock()
		case c := <-s.unregister:
			s.mu.Lock()
			delete(s.clients, c)
			c.Close()
			s.mu.Unlock()
		case msg := <-s.broadcast:
			s.mu.RLock()
			for c := range s.clients {
				if err := c.WriteJSON(msg); err != nil {
					go func(conn *websocket.Conn) { s.unregister <- conn }(c)
				}
			}
			s.mu.RUnlock()
		case <-heartbeat.C:
			s.broadcast <- map[string]string{"type": "heartbeat", "chain": string(s.chainType)}
		}
	}
}

func (s *Subscriber) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.register <- conn
	_ = conn.WriteJSON(map[string]interface{}{"type": "connected", "chain": s.chainType})
	go func() {
		defer func() { s.unregister <- conn }()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}()
}

func (s *Subscriber) BroadcastBlock(b *Block) {
	s.broadcast <- map[string]interface{}{"type": "block_added", "data": b}
}

func (s *Subscriber) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// Poller polls for new blocks
type Poller struct {
	idx        *Indexer
	sub        *Subscriber
	mu         sync.Mutex
	lastHeight uint64
}

func NewPoller(idx *Indexer, sub *Subscriber) *Poller {
	return &Poller{idx: idx, sub: sub}
}

func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if p.sub.ClientCount() > 0 && p.idx.adapter != nil {
				p.poll(ctx)
			}
		}
	}
}

func (p *Poller) poll(ctx context.Context) {
	raw, err := p.idx.adapter.GetRecentBlocks(ctx, 10)
	if err != nil {
		return
	}

	p.mu.Lock()
	lastHeight := p.lastHeight
	p.mu.Unlock()

	var newLastHeight uint64
	for _, r := range raw {
		b, err := p.idx.adapter.ParseBlock(r)
		if err != nil {
			continue
		}
		if newLastHeight == 0 || b.Height > newLastHeight {
			newLastHeight = b.Height
		}
		if b.Height <= lastHeight {
			continue
		}
		_ = p.idx.StoreBlock(ctx, b)
	}

	if newLastHeight > 0 {
		p.mu.Lock()
		p.lastHeight = newLastHeight
		p.mu.Unlock()
	}
}
