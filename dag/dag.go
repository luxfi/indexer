// Package dag provides shared DAG indexing for LUX chains (X, A, B, Q, T).
// Based on luxfi/consensus/engine/dag/vertex - multiple parents per vertex.
package dag

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
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
	DatabaseURL  string
	HTTPPort     int
	PollInterval time.Duration
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
	TotalVertices   int64     `json:"total_vertices"`
	PendingVertices int64     `json:"pending_vertices"`
	AcceptedVertices int64    `json:"accepted_vertices"`
	TotalEdges      int64     `json:"total_edges"`
	ChainType       ChainType `json:"chain_type"`
	LastUpdated     time.Time `json:"last_updated"`
}

// Adapter interface for chain-specific logic
type Adapter interface {
	ParseVertex(data json.RawMessage) (*Vertex, error)
	GetRecentVertices(ctx context.Context, limit int) ([]json.RawMessage, error)
	GetVertexByID(ctx context.Context, id string) (json.RawMessage, error)
	InitSchema(db *sql.DB) error
	GetStats(ctx context.Context, db *sql.DB) (map[string]interface{}, error)
}

// Indexer for DAG-based chains
type Indexer struct {
	config     Config
	db         *sql.DB
	httpClient *http.Client
	subscriber *Subscriber
	poller     *Poller
	adapter    Adapter
	mu         sync.RWMutex
}

// New creates a new DAG indexer
func New(cfg Config, adapter Adapter) (*Indexer, error) {
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("db connect: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("db ping: %w", err)
	}

	idx := &Indexer{
		config:     cfg,
		db:         db,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		adapter:    adapter,
	}

	idx.subscriber = NewSubscriber(cfg.ChainType)
	idx.poller = NewPoller(idx, idx.subscriber)

	return idx, nil
}

// Init creates database schema
func (idx *Indexer) Init() error {
	schema := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s_vertices (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			parent_ids JSONB DEFAULT '[]',
			height BIGINT,
			epoch INT,
			tx_ids JSONB DEFAULT '[]',
			timestamp TIMESTAMPTZ NOT NULL,
			status TEXT DEFAULT 'pending',
			data JSONB,
			metadata JSONB,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_%s_vertices_status ON %s_vertices(status);
		CREATE INDEX IF NOT EXISTS idx_%s_vertices_timestamp ON %s_vertices(timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_%s_vertices_height ON %s_vertices(height DESC);

		CREATE TABLE IF NOT EXISTS %s_edges (
			source TEXT NOT NULL,
			target TEXT NOT NULL,
			type TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			PRIMARY KEY (source, target, type)
		);
		CREATE INDEX IF NOT EXISTS idx_%s_edges_source ON %s_edges(source);
		CREATE INDEX IF NOT EXISTS idx_%s_edges_target ON %s_edges(target);

		CREATE TABLE IF NOT EXISTS %s_stats (
			id INT PRIMARY KEY DEFAULT 1,
			total_vertices BIGINT DEFAULT 0,
			pending_vertices BIGINT DEFAULT 0,
			accepted_vertices BIGINT DEFAULT 0,
			total_edges BIGINT DEFAULT 0,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		INSERT INTO %s_stats (id) VALUES (1) ON CONFLICT DO NOTHING;
	`,
		idx.config.ChainType, idx.config.ChainType, idx.config.ChainType,
		idx.config.ChainType, idx.config.ChainType, idx.config.ChainType,
		idx.config.ChainType, idx.config.ChainType, idx.config.ChainType,
		idx.config.ChainType, idx.config.ChainType, idx.config.ChainType,
		idx.config.ChainType, idx.config.ChainType,
	)

	if _, err := idx.db.Exec(schema); err != nil {
		return fmt.Errorf("schema: %w", err)
	}

	if idx.adapter != nil {
		return idx.adapter.InitSchema(idx.db)
	}
	return nil
}

// StoreVertex stores a vertex and broadcasts to subscribers
func (idx *Indexer) StoreVertex(ctx context.Context, v *Vertex) error {
	parentJSON, _ := json.Marshal(v.ParentIDs)
	txJSON, _ := json.Marshal(v.TxIDs)
	metaJSON, _ := json.Marshal(v.Metadata)

	_, err := idx.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s_vertices (id, type, parent_ids, height, epoch, tx_ids, timestamp, status, data, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status, metadata = EXCLUDED.metadata
	`, idx.config.ChainType),
		v.ID, v.Type, parentJSON, v.Height, v.Epoch, txJSON, v.Timestamp, v.Status, v.Data, metaJSON,
	)

	if err == nil {
		// Store parent edges
		for _, pid := range v.ParentIDs {
			idx.StoreEdge(ctx, Edge{Source: pid, Target: v.ID, Type: EdgeParent})
		}
		idx.subscriber.BroadcastVertex(v)
	}
	return err
}

// StoreEdge stores an edge
func (idx *Indexer) StoreEdge(ctx context.Context, e Edge) error {
	_, err := idx.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s_edges (source, target, type) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, idx.config.ChainType), e.Source, e.Target, e.Type)

	if err == nil {
		idx.subscriber.BroadcastEdge(e)
	}
	return err
}

// UpdateStats updates statistics
func (idx *Indexer) UpdateStats(ctx context.Context) error {
	var s Stats
	s.ChainType = idx.config.ChainType

	idx.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s_vertices", idx.config.ChainType)).Scan(&s.TotalVertices)
	idx.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s_vertices WHERE status='pending'", idx.config.ChainType)).Scan(&s.PendingVertices)
	idx.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s_vertices WHERE status='accepted'", idx.config.ChainType)).Scan(&s.AcceptedVertices)
	idx.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s_edges", idx.config.ChainType)).Scan(&s.TotalEdges)

	_, err := idx.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s_stats SET total_vertices=$1, pending_vertices=$2, accepted_vertices=$3, total_edges=$4, updated_at=NOW() WHERE id=1
	`, idx.config.ChainType), s.TotalVertices, s.PendingVertices, s.AcceptedVertices, s.TotalEdges)

	return err
}

// Run starts the indexer
func (idx *Indexer) Run(ctx context.Context) error {
	if err := idx.Init(); err != nil {
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
			idx.UpdateStats(ctx)
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
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok", "chain": idx.config.ChainName, "type": "dag",
		})
	})

	handler := corsMiddleware(r)
	server := &http.Server{Addr: fmt.Sprintf(":%d", idx.config.HTTPPort), Handler: handler}

	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	log.Printf("[%s] API on port %d", idx.config.ChainType, idx.config.HTTPPort)
	server.ListenAndServe()
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
	var s Stats
	s.ChainType = idx.config.ChainType
	s.LastUpdated = time.Now()

	idx.db.QueryRow(fmt.Sprintf("SELECT total_vertices, pending_vertices, accepted_vertices, total_edges FROM %s_stats WHERE id=1", idx.config.ChainType)).
		Scan(&s.TotalVertices, &s.PendingVertices, &s.AcceptedVertices, &s.TotalEdges)

	resp := map[string]interface{}{"dag_stats": s}
	if idx.adapter != nil {
		if cs, _ := idx.adapter.GetStats(r.Context(), idx.db); cs != nil {
			resp["chain_stats"] = cs
		}
	}
	json.NewEncoder(w).Encode(resp)
}

func (idx *Indexer) handleVertices(w http.ResponseWriter, r *http.Request) {
	rows, _ := idx.db.Query(fmt.Sprintf(`
		SELECT id, type, parent_ids, height, timestamp, status, data FROM %s_vertices ORDER BY timestamp DESC LIMIT 50
	`, idx.config.ChainType))
	defer rows.Close()

	var vertices []Vertex
	for rows.Next() {
		var v Vertex
		var pids, data []byte
		rows.Scan(&v.ID, &v.Type, &pids, &v.Height, &v.Timestamp, &v.Status, &data)
		json.Unmarshal(pids, &v.ParentIDs)
		v.Data = data
		vertices = append(vertices, v)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"items": vertices})
}

func (idx *Indexer) handleVertex(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var v Vertex
	var pids, data []byte
	err := idx.db.QueryRow(fmt.Sprintf(`
		SELECT id, type, parent_ids, height, timestamp, status, data FROM %s_vertices WHERE id=$1
	`, idx.config.ChainType), id).Scan(&v.ID, &v.Type, &pids, &v.Height, &v.Timestamp, &v.Status, &data)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	json.Unmarshal(pids, &v.ParentIDs)
	v.Data = data
	json.NewEncoder(w).Encode(v)
}

func (idx *Indexer) handleEdges(w http.ResponseWriter, r *http.Request) {
	vid := r.URL.Query().Get("vertex")
	var rows *sql.Rows
	if vid != "" {
		rows, _ = idx.db.Query(fmt.Sprintf("SELECT source, target, type FROM %s_edges WHERE source=$1 OR target=$1", idx.config.ChainType), vid)
	} else {
		rows, _ = idx.db.Query(fmt.Sprintf("SELECT source, target, type FROM %s_edges ORDER BY created_at DESC LIMIT 100", idx.config.ChainType))
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var e Edge
		rows.Scan(&e.Source, &e.Target, &e.Type)
		edges = append(edges, e)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"items": edges})
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
	conn.WriteJSON(map[string]interface{}{"type": "connected", "chain": s.chainType})
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
	idx *Indexer
	sub *Subscriber
	mu  sync.Mutex
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
		p.idx.StoreVertex(ctx, v)
	}

	if newLastID != "" {
		p.mu.Lock()
		p.lastID = newLastID
		p.mu.Unlock()
	}
}
