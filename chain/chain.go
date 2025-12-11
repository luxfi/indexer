// Package chain provides shared linear chain indexing for LUX chains (P, Z).
// Based on luxfi/consensus/engine/chain/block - single parent per block.
package chain

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
	RPCMethod    string // pvm, zvm
	DatabaseURL  string
	HTTPPort     int
	PollInterval time.Duration
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
	InitSchema(db *sql.DB) error
	GetStats(ctx context.Context, db *sql.DB) (map[string]interface{}, error)
}

// Indexer for linear chains
type Indexer struct {
	config     Config
	db         *sql.DB
	httpClient *http.Client
	subscriber *Subscriber
	poller     *Poller
	adapter    Adapter
	mu         sync.RWMutex
}

// New creates a new chain indexer
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
		CREATE TABLE IF NOT EXISTS %s_blocks (
			id TEXT PRIMARY KEY,
			parent_id TEXT,
			height BIGINT NOT NULL,
			timestamp TIMESTAMPTZ NOT NULL,
			status TEXT DEFAULT 'pending',
			tx_count INT DEFAULT 0,
			tx_ids JSONB DEFAULT '[]',
			data JSONB,
			metadata JSONB,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_%s_blocks_height ON %s_blocks(height DESC);
		CREATE INDEX IF NOT EXISTS idx_%s_blocks_status ON %s_blocks(status);
		CREATE INDEX IF NOT EXISTS idx_%s_blocks_timestamp ON %s_blocks(timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_%s_blocks_parent ON %s_blocks(parent_id);

		CREATE TABLE IF NOT EXISTS %s_stats (
			id INT PRIMARY KEY DEFAULT 1,
			total_blocks BIGINT DEFAULT 0,
			latest_height BIGINT DEFAULT 0,
			pending_blocks BIGINT DEFAULT 0,
			accepted_blocks BIGINT DEFAULT 0,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		INSERT INTO %s_stats (id) VALUES (1) ON CONFLICT DO NOTHING;
	`,
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

// StoreBlock stores a block and broadcasts to subscribers
func (idx *Indexer) StoreBlock(ctx context.Context, b *Block) error {
	txJSON, _ := json.Marshal(b.TxIDs)
	metaJSON, _ := json.Marshal(b.Metadata)

	_, err := idx.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s_blocks (id, parent_id, height, timestamp, status, tx_count, tx_ids, data, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status, metadata = EXCLUDED.metadata
	`, idx.config.ChainType),
		b.ID, b.ParentID, b.Height, b.Timestamp, b.Status, b.TxCount, txJSON, b.Data, metaJSON,
	)

	if err == nil {
		idx.subscriber.BroadcastBlock(b)
	}
	return err
}

// UpdateStats updates statistics
func (idx *Indexer) UpdateStats(ctx context.Context) error {
	var s Stats
	s.ChainType = idx.config.ChainType

	idx.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s_blocks", idx.config.ChainType)).Scan(&s.TotalBlocks)
	idx.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COALESCE(MAX(height), 0) FROM %s_blocks", idx.config.ChainType)).Scan(&s.LatestHeight)
	idx.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s_blocks WHERE status='pending'", idx.config.ChainType)).Scan(&s.PendingBlocks)
	idx.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s_blocks WHERE status='accepted'", idx.config.ChainType)).Scan(&s.AcceptedBlocks)

	_, err := idx.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s_stats SET total_blocks=$1, latest_height=$2, pending_blocks=$3, accepted_blocks=$4, updated_at=NOW() WHERE id=1
	`, idx.config.ChainType), s.TotalBlocks, s.LatestHeight, s.PendingBlocks, s.AcceptedBlocks)

	return err
}

// Run starts the indexer
func (idx *Indexer) Run(ctx context.Context) error {
	if err := idx.Init(); err != nil {
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
			idx.UpdateStats(ctx)
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
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok", "chain": idx.config.ChainName, "type": "linear",
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

	idx.db.QueryRow(fmt.Sprintf("SELECT total_blocks, latest_height, pending_blocks, accepted_blocks FROM %s_stats WHERE id=1", idx.config.ChainType)).
		Scan(&s.TotalBlocks, &s.LatestHeight, &s.PendingBlocks, &s.AcceptedBlocks)

	resp := map[string]interface{}{"chain_stats": s}
	if idx.adapter != nil {
		if cs, _ := idx.adapter.GetStats(r.Context(), idx.db); cs != nil {
			resp["extended_stats"] = cs
		}
	}
	json.NewEncoder(w).Encode(resp)
}

func (idx *Indexer) handleBlocks(w http.ResponseWriter, r *http.Request) {
	rows, _ := idx.db.Query(fmt.Sprintf(`
		SELECT id, parent_id, height, timestamp, status, tx_count, data FROM %s_blocks ORDER BY height DESC LIMIT 50
	`, idx.config.ChainType))
	defer rows.Close()

	var blocks []Block
	for rows.Next() {
		var b Block
		var data []byte
		rows.Scan(&b.ID, &b.ParentID, &b.Height, &b.Timestamp, &b.Status, &b.TxCount, &data)
		b.Data = data
		blocks = append(blocks, b)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"items": blocks})
}

func (idx *Indexer) handleBlock(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var b Block
	var data []byte
	err := idx.db.QueryRow(fmt.Sprintf(`
		SELECT id, parent_id, height, timestamp, status, tx_count, data FROM %s_blocks WHERE id=$1
	`, idx.config.ChainType), id).Scan(&b.ID, &b.ParentID, &b.Height, &b.Timestamp, &b.Status, &b.TxCount, &data)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	b.Data = data
	json.NewEncoder(w).Encode(b)
}

func (idx *Indexer) handleBlockByHeight(w http.ResponseWriter, r *http.Request) {
	height := mux.Vars(r)["height"]
	var b Block
	var data []byte
	err := idx.db.QueryRow(fmt.Sprintf(`
		SELECT id, parent_id, height, timestamp, status, tx_count, data FROM %s_blocks WHERE height=$1
	`, idx.config.ChainType), height).Scan(&b.ID, &b.ParentID, &b.Height, &b.Timestamp, &b.Status, &b.TxCount, &data)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	b.Data = data
	json.NewEncoder(w).Encode(b)
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
		p.idx.StoreBlock(ctx, b)
	}

	if newLastHeight > 0 {
		p.mu.Lock()
		p.lastHeight = newLastHeight
		p.mu.Unlock()
	}
}
