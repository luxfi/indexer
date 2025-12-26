// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package dag provides shared DAG indexing for LUX chains (X, A, B, Q, T, Z).
// Based on luxfi/consensus/engine/dag/vertex - multiple parents per vertex.
package dag

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

// ChainType identifies the DAG chain
type ChainType string

const (
	ChainX ChainType = "xchain" // Exchange - assets, UTXOs
	ChainA ChainType = "achain" // AI - attestations, compute
	ChainB ChainType = "bchain" // Bridge - cross-chain
	ChainQ ChainType = "qchain" // Quantum - finality proofs
	ChainT ChainType = "tchain" // Teleport - MPC signatures
	ChainZ ChainType = "zchain" // Privacy - ZK transactions (DAG for fast consensus, proofs for privacy)
)

// Config for DAG indexer
type Config struct {
	ChainType    ChainType
	ChainName    string
	RPCEndpoint  string
	RPCMethod    string // xvm, avm, bvm, qvm, tvm
	HTTPPort     int
	PollInterval time.Duration
	DataDir      string // For default storage, defaults to ~/.lux/indexer/<chain>
}

// Vertex represents a DAG vertex (from luxfi/consensus)
// Multiple parents allowed (DAG structure)
type Vertex struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	ParentIDs []string               `json:"parentIds"` // Multiple parents (DAG)
	Height    uint64                 `json:"height"`
	Epoch     uint32                 `json:"epoch,omitempty"`
	TxIDs     []string               `json:"txIds,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Status    Status                 `json:"status"`
	Data      json.RawMessage        `json:"data,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Edge represents a DAG edge (parent relationship)
type Edge struct {
	Source string   `json:"source"`
	Target string   `json:"target"`
	Type   EdgeType `json:"type"`
}

// Status of vertex consensus
type Status string

const (
	StatusPending  Status = "pending"
	StatusAccepted Status = "accepted"
	StatusRejected Status = "rejected"
)

// EdgeType categorizes edges
type EdgeType string

const (
	EdgeParent    EdgeType = "parent"    // DAG parent reference
	EdgeInput     EdgeType = "input"     // UTXO input
	EdgeOutput    EdgeType = "output"    // UTXO output
	EdgeReference EdgeType = "reference" // General reference
)

// Stats for DAG chain
type Stats struct {
	TotalVertices    int64     `json:"total_vertices"`
	PendingVertices  int64     `json:"pending_vertices"`
	AcceptedVertices int64     `json:"accepted_vertices"`
	TotalEdges       int64     `json:"total_edges"`
	ChainType        ChainType `json:"chain_type"`
	LastUpdated      time.Time `json:"last_updated"`
}

// Adapter interface for chain-specific logic
type Adapter interface {
	ParseVertex(data json.RawMessage) (*Vertex, error)
	GetRecentVertices(ctx context.Context, limit int) ([]json.RawMessage, error)
	GetVertexByID(ctx context.Context, id string) (json.RawMessage, error)
	InitSchema(ctx context.Context, store storage.Store) error
	GetStats(ctx context.Context, store storage.Store) (map[string]interface{}, error)
}

// Indexer for DAG-based chains
type Indexer struct {
	config     Config
	store      storage.Store
	httpClient *http.Client
	subscriber *Subscriber
	poller     *Poller
	adapter    Adapter
	tableName  string
}

// New creates a new DAG indexer with the given storage
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
				Name: idx.tableName + "_vertices",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "type", Type: storage.TypeText, Nullable: false},
					{Name: "parent_ids", Type: storage.TypeJSON, Default: "'[]'"},
					{Name: "height", Type: storage.TypeBigInt},
					{Name: "epoch", Type: storage.TypeInt},
					{Name: "tx_ids", Type: storage.TypeJSON, Default: "'[]'"},
					{Name: "timestamp", Type: storage.TypeTimestamp, Nullable: false},
					{Name: "status", Type: storage.TypeText, Default: "'pending'"},
					{Name: "data", Type: storage.TypeJSON},
					{Name: "metadata", Type: storage.TypeJSON},
					{Name: "created_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
			{
				Name: idx.tableName + "_edges",
				Columns: []storage.Column{
					{Name: "source", Type: storage.TypeText, Nullable: false},
					{Name: "target", Type: storage.TypeText, Nullable: false},
					{Name: "type", Type: storage.TypeText, Nullable: false},
					{Name: "created_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
		},
		Indexes: []storage.Index{
			{Name: "idx_" + idx.tableName + "_vertices_status", Table: idx.tableName + "_vertices", Columns: []string{"status"}},
			{Name: "idx_" + idx.tableName + "_vertices_timestamp", Table: idx.tableName + "_vertices", Columns: []string{"timestamp"}},
			{Name: "idx_" + idx.tableName + "_vertices_height", Table: idx.tableName + "_vertices", Columns: []string{"height"}},
			{Name: "idx_" + idx.tableName + "_edges_source", Table: idx.tableName + "_edges", Columns: []string{"source"}},
			{Name: "idx_" + idx.tableName + "_edges_target", Table: idx.tableName + "_edges", Columns: []string{"target"}},
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

// StoreVertex stores a vertex and broadcasts to subscribers
func (idx *Indexer) StoreVertex(ctx context.Context, v *Vertex) error {
	metaJSON, _ := json.Marshal(v.Metadata)

	sv := &storage.Vertex{
		ID:        v.ID,
		Type:      v.Type,
		ParentIDs: v.ParentIDs,
		Height:    v.Height,
		Epoch:     v.Epoch,
		TxIDs:     v.TxIDs,
		Timestamp: v.Timestamp,
		Status:    string(v.Status),
		Data:      v.Data,
		Metadata:  metaJSON,
		CreatedAt: time.Now(),
	}

	if err := idx.store.StoreVertex(ctx, idx.tableName+"_vertices", sv); err != nil {
		return err
	}

	// Store parent edges
	for _, pid := range v.ParentIDs {
		_ = idx.StoreEdge(ctx, Edge{Source: pid, Target: v.ID, Type: EdgeParent})
	}
	idx.subscriber.BroadcastVertex(v)
	return nil
}

// StoreEdge stores an edge
func (idx *Indexer) StoreEdge(ctx context.Context, e Edge) error {
	se := &storage.Edge{
		Source:    e.Source,
		Target:    e.Target,
		Type:      string(e.Type),
		CreatedAt: time.Now(),
	}

	if err := idx.store.StoreEdge(ctx, idx.tableName+"_edges", se); err != nil {
		return err
	}
	idx.subscriber.BroadcastEdge(e)
	return nil
}

// UpdateStats updates statistics
func (idx *Indexer) UpdateStats(ctx context.Context) error {
	var s Stats
	s.ChainType = idx.config.ChainType

	total, _ := idx.store.Count(ctx, idx.tableName+"_vertices", "1=1")
	pending, _ := idx.store.Count(ctx, idx.tableName+"_vertices", "status='pending'")
	accepted, _ := idx.store.Count(ctx, idx.tableName+"_vertices", "status='accepted'")
	edges, _ := idx.store.Count(ctx, idx.tableName+"_edges", "1=1")

	s.TotalVertices = total
	s.PendingVertices = pending
	s.AcceptedVertices = accepted
	s.TotalEdges = edges
	s.LastUpdated = time.Now()

	statsMap := map[string]interface{}{
		"total_vertices":    s.TotalVertices,
		"pending_vertices":  s.PendingVertices,
		"accepted_vertices": s.AcceptedVertices,
		"total_edges":       s.TotalEdges,
		"last_updated":      s.LastUpdated,
	}
	return idx.store.UpdateStats(ctx, idx.tableName, statsMap)
}

// Run starts the indexer
func (idx *Indexer) Run(ctx context.Context) error {
	if err := idx.Init(ctx); err != nil {
		return err
	}

	go idx.subscriber.Run(ctx)
	log.Printf("[%s] WebSocket streaming at /api/v2/dag/subscribe", idx.config.ChainType)

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
	api.HandleFunc("/vertices", idx.handleVertices).Methods("GET")
	api.HandleFunc("/vertices/{id}", idx.handleVertex).Methods("GET")
	api.HandleFunc("/edges", idx.handleEdges).Methods("GET")
	api.HandleFunc("/dag/subscribe", idx.subscriber.HandleWebSocket)

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok", "chain": idx.config.ChainName, "type": "dag",
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

	resp := map[string]interface{}{"dag_stats": stats}
	if idx.adapter != nil {
		if cs, _ := idx.adapter.GetStats(r.Context(), idx.store); cs != nil {
			resp["chain_stats"] = cs
		}
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (idx *Indexer) handleVertices(w http.ResponseWriter, r *http.Request) {
	svs, err := idx.store.GetRecentVertices(r.Context(), idx.tableName+"_vertices", 50)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}

	var vertices []Vertex
	for _, sv := range svs {
		v := Vertex{
			ID:        sv.ID,
			Type:      sv.Type,
			ParentIDs: sv.ParentIDs,
			Height:    sv.Height,
			Epoch:     sv.Epoch,
			TxIDs:     sv.TxIDs,
			Timestamp: sv.Timestamp,
			Status:    Status(sv.Status),
			Data:      sv.Data,
		}
		vertices = append(vertices, v)
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": vertices})
}

func (idx *Indexer) handleVertex(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	sv, err := idx.store.GetVertex(r.Context(), idx.tableName+"_vertices", id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	v := Vertex{
		ID:        sv.ID,
		Type:      sv.Type,
		ParentIDs: sv.ParentIDs,
		Height:    sv.Height,
		Epoch:     sv.Epoch,
		TxIDs:     sv.TxIDs,
		Timestamp: sv.Timestamp,
		Status:    Status(sv.Status),
		Data:      sv.Data,
	}
	_ = json.NewEncoder(w).Encode(v)
}

func (idx *Indexer) handleEdges(w http.ResponseWriter, r *http.Request) {
	vid := r.URL.Query().Get("vertex")
	var edges []Edge

	if vid != "" {
		ses, err := idx.store.GetEdges(r.Context(), idx.tableName+"_edges", vid)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		for _, se := range ses {
			edges = append(edges, Edge{
				Source: se.Source,
				Target: se.Target,
				Type:   EdgeType(se.Type),
			})
		}
	} else {
		// List recent edges via query
		rows, err := idx.store.Query(r.Context(),
			fmt.Sprintf("SELECT source, target, type FROM %s_edges ORDER BY created_at DESC LIMIT 100", idx.tableName))
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		for _, row := range rows {
			edges = append(edges, Edge{
				Source: row["source"].(string),
				Target: row["target"].(string),
				Type:   EdgeType(row["type"].(string)),
			})
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": edges})
}

// Store returns the underlying storage for adapters
func (idx *Indexer) Store() storage.Store {
	return idx.store
}

// Subscriber handles WebSocket for live DAG streaming
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

func (s *Subscriber) BroadcastVertex(v *Vertex) {
	s.broadcast <- map[string]interface{}{"type": "vertex_added", "data": v}
}

func (s *Subscriber) BroadcastEdge(e Edge) {
	s.broadcast <- map[string]interface{}{"type": "edge_added", "data": e}
}

func (s *Subscriber) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// Poller polls for new vertices
type Poller struct {
	idx    *Indexer
	sub    *Subscriber
	mu     sync.Mutex
	lastID string
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
	raw, err := p.idx.adapter.GetRecentVertices(ctx, 10)
	if err != nil {
		return
	}

	p.mu.Lock()
	lastID := p.lastID
	p.mu.Unlock()

	var newLastID string
	for _, r := range raw {
		v, err := p.idx.adapter.ParseVertex(r)
		if err != nil {
			continue
		}
		if newLastID == "" {
			newLastID = v.ID
		}
		if v.ID == lastID {
			break
		}
		_ = p.idx.StoreVertex(ctx, v)
	}

	if newLastID != "" {
		p.mu.Lock()
		p.lastID = newLastID
		p.mu.Unlock()
	}
}
