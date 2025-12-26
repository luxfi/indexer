// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package evm

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

// Config for EVM indexer
type Config struct {
	ChainName    string
	ChainID      int64
	RPCEndpoint  string
	HTTPPort     int
	PollInterval time.Duration
}

// Indexer is the main EVM chain indexer
type Indexer struct {
	config     Config
	store      storage.Store
	adapter    *Adapter
	subscriber *Subscriber
	mu         sync.RWMutex
}

// EVMBlock represents a parsed EVM block
type EVMBlock struct {
	Number       uint64    `json:"number"`
	Hash         string    `json:"hash"`
	ParentHash   string    `json:"parentHash"`
	Nonce        string    `json:"nonce"`
	Miner        string    `json:"miner"`
	Difficulty   string    `json:"difficulty"`
	GasLimit     uint64    `json:"gasLimit"`
	GasUsed      uint64    `json:"gasUsed"`
	Timestamp    time.Time `json:"timestamp"`
	TxCount      int       `json:"txCount"`
	BaseFee      string    `json:"baseFeePerGas,omitempty"`
	Size         uint64    `json:"size"`
	Transactions []string  `json:"transactions"`
}

// NewIndexer creates a new EVM indexer with the unified storage
func NewIndexer(cfg Config, store storage.Store) (*Indexer, error) {
	if store == nil {
		return nil, fmt.Errorf("storage cannot be nil")
	}

	adapter := New(cfg.RPCEndpoint)

	idx := &Indexer{
		config:     cfg,
		store:      store,
		adapter:    adapter,
		subscriber: NewSubscriber(),
	}

	return idx, nil
}

// Init initializes the EVM indexer schema
func (idx *Indexer) Init(ctx context.Context) error {
	schema := storage.Schema{
		Name: "evm",
		Tables: []storage.Table{
			{
				Name: "evm_blocks",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "number", Type: storage.TypeBigInt, Nullable: false},
					{Name: "hash", Type: storage.TypeText, Nullable: false},
					{Name: "parent_hash", Type: storage.TypeText},
					{Name: "nonce", Type: storage.TypeText},
					{Name: "miner", Type: storage.TypeText},
					{Name: "difficulty", Type: storage.TypeText},
					{Name: "total_difficulty", Type: storage.TypeText},
					{Name: "gas_limit", Type: storage.TypeBigInt},
					{Name: "gas_used", Type: storage.TypeBigInt},
					{Name: "timestamp", Type: storage.TypeTimestamp, Nullable: false},
					{Name: "tx_count", Type: storage.TypeInt, Default: "0"},
					{Name: "base_fee", Type: storage.TypeText},
					{Name: "size", Type: storage.TypeBigInt},
					{Name: "created_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
			{
				Name: "evm_transactions",
				Columns: []storage.Column{
					{Name: "hash", Type: storage.TypeText, Primary: true},
					{Name: "block_hash", Type: storage.TypeText},
					{Name: "block_number", Type: storage.TypeBigInt},
					{Name: "tx_index", Type: storage.TypeInt},
					{Name: "from_addr", Type: storage.TypeText},
					{Name: "to_addr", Type: storage.TypeText},
					{Name: "value", Type: storage.TypeText},
					{Name: "gas", Type: storage.TypeBigInt},
					{Name: "gas_price", Type: storage.TypeText},
					{Name: "gas_used", Type: storage.TypeBigInt},
					{Name: "nonce", Type: storage.TypeBigInt},
					{Name: "input", Type: storage.TypeText},
					{Name: "status", Type: storage.TypeInt},
					{Name: "contract_addr", Type: storage.TypeText},
					{Name: "timestamp", Type: storage.TypeTimestamp},
					{Name: "created_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
			{
				Name: "evm_addresses",
				Columns: []storage.Column{
					{Name: "hash", Type: storage.TypeText, Primary: true},
					{Name: "balance", Type: storage.TypeText},
					{Name: "tx_count", Type: storage.TypeBigInt, Default: "0"},
					{Name: "is_contract", Type: storage.TypeBool, Default: "false"},
					{Name: "code", Type: storage.TypeText},
					{Name: "creator", Type: storage.TypeText},
					{Name: "creation_tx", Type: storage.TypeText},
					{Name: "created_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
					{Name: "updated_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
			{
				Name: "evm_tokens",
				Columns: []storage.Column{
					{Name: "address", Type: storage.TypeText, Primary: true},
					{Name: "name", Type: storage.TypeText},
					{Name: "symbol", Type: storage.TypeText},
					{Name: "decimals", Type: storage.TypeInt},
					{Name: "total_supply", Type: storage.TypeText},
					{Name: "token_type", Type: storage.TypeText},
					{Name: "holder_count", Type: storage.TypeBigInt, Default: "0"},
					{Name: "tx_count", Type: storage.TypeBigInt, Default: "0"},
					{Name: "created_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
					{Name: "updated_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
			{
				Name: "evm_token_transfers",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "tx_hash", Type: storage.TypeText},
					{Name: "log_index", Type: storage.TypeInt},
					{Name: "block_number", Type: storage.TypeBigInt},
					{Name: "token_address", Type: storage.TypeText},
					{Name: "token_type", Type: storage.TypeText},
					{Name: "from_addr", Type: storage.TypeText},
					{Name: "to_addr", Type: storage.TypeText},
					{Name: "value", Type: storage.TypeText},
					{Name: "token_id", Type: storage.TypeText},
					{Name: "timestamp", Type: storage.TypeTimestamp},
					{Name: "created_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
			{
				Name: "evm_logs",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "tx_hash", Type: storage.TypeText},
					{Name: "log_index", Type: storage.TypeInt},
					{Name: "block_number", Type: storage.TypeBigInt},
					{Name: "address", Type: storage.TypeText},
					{Name: "topic0", Type: storage.TypeText},
					{Name: "topic1", Type: storage.TypeText},
					{Name: "topic2", Type: storage.TypeText},
					{Name: "topic3", Type: storage.TypeText},
					{Name: "data", Type: storage.TypeText},
					{Name: "timestamp", Type: storage.TypeTimestamp},
					{Name: "created_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
		},
		Indexes: []storage.Index{
			{Name: "idx_evm_blocks_number", Table: "evm_blocks", Columns: []string{"number"}},
			{Name: "idx_evm_blocks_hash", Table: "evm_blocks", Columns: []string{"hash"}},
			{Name: "idx_evm_blocks_miner", Table: "evm_blocks", Columns: []string{"miner"}},
			{Name: "idx_evm_transactions_block", Table: "evm_transactions", Columns: []string{"block_number"}},
			{Name: "idx_evm_transactions_from", Table: "evm_transactions", Columns: []string{"from_addr"}},
			{Name: "idx_evm_transactions_to", Table: "evm_transactions", Columns: []string{"to_addr"}},
			{Name: "idx_evm_transactions_contract", Table: "evm_transactions", Columns: []string{"contract_addr"}},
			{Name: "idx_evm_addresses_contract", Table: "evm_addresses", Columns: []string{"is_contract"}},
			{Name: "idx_evm_tokens_type", Table: "evm_tokens", Columns: []string{"token_type"}},
			{Name: "idx_evm_token_transfers_token", Table: "evm_token_transfers", Columns: []string{"token_address"}},
			{Name: "idx_evm_token_transfers_from", Table: "evm_token_transfers", Columns: []string{"from_addr"}},
			{Name: "idx_evm_token_transfers_to", Table: "evm_token_transfers", Columns: []string{"to_addr"}},
			{Name: "idx_evm_logs_address", Table: "evm_logs", Columns: []string{"address"}},
			{Name: "idx_evm_logs_topic0", Table: "evm_logs", Columns: []string{"topic0"}},
		},
	}

	return idx.store.InitSchema(ctx, schema)
}

// Run starts the EVM indexer
func (idx *Indexer) Run(ctx context.Context) error {
	if err := idx.Init(ctx); err != nil {
		return fmt.Errorf("init: %w", err)
	}

	go idx.subscriber.Run(ctx)
	log.Printf("[evm] WebSocket streaming at /api/v2/blocks/subscribe")

	go idx.startHTTP(ctx)
	log.Printf("[evm] API on port %d", idx.config.HTTPPort)

	go idx.startIndexing(ctx)
	log.Printf("[evm] Block indexing started")

	ticker := time.NewTicker(idx.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_ = idx.updateStats(ctx)
		}
	}
}

func (idx *Indexer) startIndexing(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			idx.indexNewBlocks(ctx)
		}
	}
}

func (idx *Indexer) indexNewBlocks(ctx context.Context) {
	// Get current block number from RPC
	block, err := idx.adapter.GetLatestBlock(ctx)
	if err != nil {
		return
	}

	// Get last indexed block from storage
	var lastIndexed uint64
	rows, _ := idx.store.Query(ctx, "SELECT COALESCE(MAX(number), 0) as max_num FROM evm_blocks")
	if len(rows) > 0 {
		if v, ok := rows[0]["max_num"]; ok {
			switch h := v.(type) {
			case int64:
				lastIndexed = uint64(h)
			case float64:
				lastIndexed = uint64(h)
			}
		}
	}

	// Index new blocks (start from 0 if nothing indexed yet)
	startBlock := lastIndexed
	if lastIndexed > 0 {
		startBlock = lastIndexed + 1
	}
	for blockNum := startBlock; blockNum <= block.Number; blockNum++ {
		if err := idx.indexBlock(ctx, blockNum); err != nil {
			log.Printf("[evm] Failed to index block %d: %v", blockNum, err)
			return
		}
	}
}

func (idx *Indexer) indexBlock(ctx context.Context, blockNum uint64) error {
	block, err := idx.adapter.GetBlockByNumber(ctx, blockNum)
	if err != nil {
		return err
	}

	// Store block using EVM-specific schema with backend-portable SQL
	query := idx.upsertBlockSQL()
	args := idx.blockArgs(block)

	err = idx.store.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("store block: %w", err)
	}

	// Broadcast new block
	idx.subscriber.BroadcastBlock(block)

	return nil
}

// upsertBlockSQL returns the correct upsert SQL for the backend
func (idx *Indexer) upsertBlockSQL() string {
	switch idx.store.Backend() {
	case storage.BackendPostgres:
		return `
			INSERT INTO evm_blocks (id, number, hash, parent_hash, nonce, miner, difficulty,
				total_difficulty, gas_limit, gas_used, timestamp, tx_count, base_fee, size, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			ON CONFLICT (id) DO UPDATE SET
				number = EXCLUDED.number,
				hash = EXCLUDED.hash,
				parent_hash = EXCLUDED.parent_hash,
				nonce = EXCLUDED.nonce,
				miner = EXCLUDED.miner,
				difficulty = EXCLUDED.difficulty,
				total_difficulty = EXCLUDED.total_difficulty,
				gas_limit = EXCLUDED.gas_limit,
				gas_used = EXCLUDED.gas_used,
				timestamp = EXCLUDED.timestamp,
				tx_count = EXCLUDED.tx_count,
				base_fee = EXCLUDED.base_fee,
				size = EXCLUDED.size`
	default: // SQLite
		return `
			INSERT OR REPLACE INTO evm_blocks (id, number, hash, parent_hash, nonce, miner, difficulty,
				total_difficulty, gas_limit, gas_used, timestamp, tx_count, base_fee, size, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	}
}

// blockArgs returns the block arguments for upsert
func (idx *Indexer) blockArgs(block *EVMBlock) []interface{} {
	return []interface{}{
		block.Hash,              // id
		block.Number,            // number
		block.Hash,              // hash
		block.ParentHash,        // parent_hash
		block.Nonce,             // nonce
		block.Miner,             // miner
		block.Difficulty,        // difficulty
		"",                      // total_difficulty
		block.GasLimit,          // gas_limit
		block.GasUsed,           // gas_used
		block.Timestamp,         // timestamp
		len(block.Transactions), // tx_count
		block.BaseFee,           // base_fee
		block.Size,              // size
		time.Now(),              // created_at
	}
}

func (idx *Indexer) updateStats(ctx context.Context) error {
	var totalBlocks, totalTxs, totalAddresses int64

	rows, _ := idx.store.Query(ctx, "SELECT COUNT(*) as cnt FROM evm_blocks")
	if len(rows) > 0 {
		if v, ok := rows[0]["cnt"]; ok {
			totalBlocks = toInt64(v)
		}
	}

	rows, _ = idx.store.Query(ctx, "SELECT COUNT(*) as cnt FROM evm_transactions")
	if len(rows) > 0 {
		if v, ok := rows[0]["cnt"]; ok {
			totalTxs = toInt64(v)
		}
	}

	rows, _ = idx.store.Query(ctx, "SELECT COUNT(*) as cnt FROM evm_addresses")
	if len(rows) > 0 {
		if v, ok := rows[0]["cnt"]; ok {
			totalAddresses = toInt64(v)
		}
	}

	stats := map[string]interface{}{
		"total_blocks":    totalBlocks,
		"total_txs":       totalTxs,
		"total_addresses": totalAddresses,
		"last_updated":    time.Now(),
	}
	return idx.store.UpdateStats(ctx, "evm", stats)
}

func toInt64(v interface{}) int64 {
	switch h := v.(type) {
	case int64:
		return h
	case float64:
		return int64(h)
	case int:
		return int64(h)
	default:
		return 0
	}
}

func (idx *Indexer) startHTTP(ctx context.Context) {
	r := mux.NewRouter()
	api := r.PathPrefix("/api/v2").Subrouter()

	api.HandleFunc("/stats", idx.handleStats).Methods("GET")
	api.HandleFunc("/blocks", idx.handleBlocks).Methods("GET")
	api.HandleFunc("/blocks/{id}", idx.handleBlock).Methods("GET")
	api.HandleFunc("/transactions", idx.handleTransactions).Methods("GET")
	api.HandleFunc("/transactions/{hash}", idx.handleTransaction).Methods("GET")
	api.HandleFunc("/addresses/{hash}", idx.handleAddress).Methods("GET")
	api.HandleFunc("/blocks/subscribe", idx.subscriber.HandleWebSocket)
	api.HandleFunc("/events", idx.handleEvents).Methods("POST") // Real-time events from node

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok", "chain": idx.config.ChainName, "type": "evm",
		})
	})

	handler := corsMiddleware(r)
	server := &http.Server{Addr: fmt.Sprintf(":%d", idx.config.HTTPPort), Handler: handler}

	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

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
	stats, _ := idx.store.GetStats(r.Context(), "evm")
	if stats == nil {
		stats = make(map[string]interface{})
	}
	stats["chain_name"] = idx.config.ChainName
	stats["chain_id"] = idx.config.ChainID
	_ = json.NewEncoder(w).Encode(stats)
}

func (idx *Indexer) handleBlocks(w http.ResponseWriter, r *http.Request) {
	blocks, err := idx.store.GetRecentBlocks(r.Context(), "evm_blocks", 50)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": blocks})
}

func (idx *Indexer) handleBlock(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	block, err := idx.store.GetBlock(r.Context(), "evm_blocks", id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	_ = json.NewEncoder(w).Encode(block)
}

func (idx *Indexer) handleTransactions(w http.ResponseWriter, r *http.Request) {
	rows, err := idx.store.Query(r.Context(),
		"SELECT * FROM evm_transactions ORDER BY block_number DESC, tx_index DESC LIMIT 50")
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": rows})
}

func (idx *Indexer) handleTransaction(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["hash"]
	rows, err := idx.store.Query(r.Context(),
		"SELECT * FROM evm_transactions WHERE hash = ?", hash)
	if err != nil || len(rows) == 0 {
		http.Error(w, "not found", 404)
		return
	}
	_ = json.NewEncoder(w).Encode(rows[0])
}

func (idx *Indexer) handleAddress(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["hash"]
	rows, err := idx.store.Query(r.Context(),
		"SELECT * FROM evm_addresses WHERE hash = ?", hash)
	if err != nil || len(rows) == 0 {
		http.Error(w, "not found", 404)
		return
	}
	_ = json.NewEncoder(w).Encode(rows[0])
}

// NodeEvent represents an event from the node's hookdb
type NodeEvent struct {
	ChainID   string    `json:"chain_id"`
	Type      string    `json:"type"` // "put" or "delete"
	Prefix    string    `json:"prefix"`
	Key       string    `json:"key"`
	Value     []byte    `json:"value,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// handleEvents receives real-time events from the node's hookdb
func (idx *Indexer) handleEvents(w http.ResponseWriter, r *http.Request) {
	var events []NodeEvent
	if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}

	ctx := r.Context()
	processed := 0

	for _, event := range events {
		if event.Type != "put" {
			continue // Only process writes for now
		}

		// Detect block data by key prefix and process
		if idx.isBlockKey(event.Key) {
			if err := idx.processBlockEvent(ctx, event); err != nil {
				log.Printf("[evm] Failed to process block event: %v", err)
				continue
			}
			processed++
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"received":  len(events),
		"processed": processed,
	})
}

// isBlockKey checks if a key represents block data
func (idx *Indexer) isBlockKey(key string) bool {
	// Common block key prefixes in geth/coreth
	// B = block body, H = header hash, h = header, n = header number, b = block hash -> number
	if len(key) == 0 {
		return false
	}
	switch key[0] {
	case 'B', 'H', 'h', 'n', 'b':
		return true
	}
	// Also check for "LastBlock" or "LastAccepted" keys
	return key == "LastBlock" || key == "LastAccepted"
}

// processBlockEvent handles a block write event from the node
func (idx *Indexer) processBlockEvent(ctx context.Context, event NodeEvent) error {
	// The event contains the raw block data - we need to decode and index it
	// For now, trigger a re-index of the latest block
	// In the future, we can decode the RLP directly from event.Value

	block, err := idx.adapter.GetLatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("get latest block: %w", err)
	}

	// Index the block
	query := idx.upsertBlockSQL()
	args := idx.blockArgs(block)

	if err := idx.store.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("store block: %w", err)
	}

	// Broadcast to WebSocket subscribers
	idx.subscriber.BroadcastBlock(block)

	log.Printf("[evm] Indexed block %d from node event", block.Number)
	return nil
}

// Subscriber handles WebSocket for live block streaming
type Subscriber struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan interface{}
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex
	upgrader   websocket.Upgrader
}

func NewSubscriber() *Subscriber {
	return &Subscriber{
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
			s.broadcast <- map[string]string{"type": "heartbeat"}
		}
	}
}

func (s *Subscriber) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.register <- conn
	_ = conn.WriteJSON(map[string]interface{}{"type": "connected"})
	go func() {
		defer func() { s.unregister <- conn }()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}()
}

func (s *Subscriber) BroadcastBlock(block *EVMBlock) {
	s.broadcast <- map[string]interface{}{"type": "block", "data": block}
}
