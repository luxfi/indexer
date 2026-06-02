// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package evm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strings"
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

	// StartBlock is the lowest block to index when there is no existing
	// stored state (i.e. on a fresh DB). Set this when running against a
	// long-history chain — most useful for mainnet-fork devnets where the
	// fork inherits millions of historical blocks but the operator only
	// cares about the post-fork (locally-mined) activity. Once the indexer
	// has any rows in evm_blocks, this field is ignored and indexing
	// resumes from MAX(number)+1.
	StartBlock uint64
}

// Indexer is the main EVM chain indexer
type Indexer struct {
	config      Config
	store       storage.Store
	adapter     *Adapter
	subscriber  *Subscriber
	mu          sync.RWMutex
	lastLogTime time.Time
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

// Subscriber returns the indexer's internal WebSocket subscriber so an
// embedder (e.g. luxfi/explorer) can install a Subscriber.OnBroadcast
// callback to bridge block events into its own pub/sub fabric.
//
// Returns nil if the indexer was constructed without an internal
// subscriber (currently always non-nil — NewIndexer always allocates
// one — but the contract permits nil so callers must guard).
func (idx *Indexer) Subscriber() *Subscriber {
	return idx.subscriber
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
					{Name: "balance", Type: storage.TypeText, Default: "'0'"},
					{Name: "tx_count", Type: storage.TypeBigInt, Default: "0"},
					{Name: "is_contract", Type: storage.TypeBool, Default: "false"},
					{Name: "code", Type: storage.TypeText, Default: "''"},
					{Name: "creator", Type: storage.TypeText, Default: "''"},
					{Name: "creation_tx", Type: storage.TypeText, Default: "''"},
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
			{
				// Per-(token, holder) balance state. Maintained on every
				// detected Transfer log via applyERC20BalanceDelta /
				// applyERC721BalanceDelta. token_id is "" for ERC-20
				// (one row per holder per token) or the hex tokenId for
				// ERC-721 (one row per NFT, owner stored in `address`).
				Name: "evm_token_balances",
				Columns: []storage.Column{
					// Composite PK (token_address, address, token_id) is
					// required so the upsert's ON CONFLICT clause has a
					// constraint to match against — without it, every
					// balance write was silently lost on duplicate
					// (token, holder) inserts.
					{Name: "token_address", Type: storage.TypeText, Nullable: false, Primary: true},
					{Name: "address", Type: storage.TypeText, Nullable: false, Primary: true},
					{Name: "token_id", Type: storage.TypeText, Default: "''", Primary: true},
					{Name: "value", Type: storage.TypeText, Default: "'0'"},
					{Name: "token_type", Type: storage.TypeText, Default: "''"},
					{Name: "updated_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
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
			{Name: "idx_evm_token_balances_token", Table: "evm_token_balances", Columns: []string{"token_address"}},
			{Name: "idx_evm_token_balances_addr", Table: "evm_token_balances", Columns: []string{"address"}},
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
	log.Printf("[evm] WebSocket streaming at /v1/explorer/blocks/subscribe")

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
	log.Printf("[evm] Indexing goroutine started, RPC: %s", idx.adapter.rpcEndpoint)

	// Backfill addresses from already-indexed transactions — this repairs
	// state from older indexer versions that couldn't write to evm_addresses.
	if n, err := idx.backfillAddresses(ctx); err == nil && n > 0 {
		log.Printf("[evm] Backfilled %d addresses from existing transactions", n)
	}

	// Index immediately on startup before entering ticker loop
	idx.indexNewBlocks(ctx)

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
		if idx.lastLogTime.Add(30 * time.Second).Before(time.Now()) {
			log.Printf("[evm] Failed to get latest block: %v", err)
			idx.lastLogTime = time.Now()
		}
		return
	}

	// Get last indexed block from storage
	var lastIndexed uint64
	rows, queryErr := idx.store.Query(ctx, "SELECT COALESCE(MAX(number), 0) as max_num FROM evm_blocks")
	if queryErr != nil {
		log.Printf("[evm] Failed to query last indexed block: %v", queryErr)
		return
	}
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

	// Index new blocks. When there is existing state, resume from MAX+1.
	// Otherwise, honor an operator-supplied StartBlock (useful for
	// mainnet-fork devnets where genesis-from-0 backfill is wasteful), or
	// fall back to 0 for a true fresh-genesis chain.
	startBlock := lastIndexed
	if lastIndexed > 0 {
		startBlock = lastIndexed + 1
	} else if idx.config.StartBlock > 0 {
		startBlock = idx.config.StartBlock
	}

	if startBlock <= block.Number && idx.lastLogTime.Add(30*time.Second).Before(time.Now()) {
		log.Printf("[evm] Indexing blocks %d to %d (latest: %d)", startBlock, block.Number, block.Number)
		idx.lastLogTime = time.Now()
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

	// Index transactions: fetch receipts for each tx hash. Track each
	// address's per-block tx count and known contract status so the
	// downstream upsert can increment counters and resolve is_contract
	// (the old code passed isContract:false for every from/to and only
	// flagged the contract-creation address — but a SimplePool created
	// via `new` inside a script is a regular CALL to the creator's
	// factory, not a top-level creation tx, so the indexer never knew
	// it was a contract).
	addrTxCount := make(map[string]uint64)
	addrIsContract := make(map[string]bool)
	// Track tokens discovered in this block so we can insert a placeholder
	// row in evm_tokens with the metadata best-effort resolved via eth_call
	// (name/symbol/decimals).
	tokensSeen := make(map[string]string) // address → token_type
	for i, txHash := range block.Transactions {
		tx, logs, err := idx.adapter.GetTransactionReceipt(ctx, txHash)
		if err != nil || tx == nil {
			continue
		}
		status := 0
		if tx.Status != nil {
			status = *tx.Status
		}
		txQ := idx.upsertTxSQL()
		txArgs := []interface{}{
			tx.Hash, tx.BlockHash, int64(block.Number), i,
			tx.From, tx.To, tx.Value,
			int64(tx.Gas), tx.GasPrice, int64(tx.GasUsed),
			int64(tx.Nonce), tx.Input, status, tx.ContractAddress,
			block.Timestamp, time.Now(),
		}
		if err := idx.store.Exec(ctx, txQ, txArgs...); err != nil {
			continue
		}

		// Index logs + extract token transfers. Receipts include the full
		// log list already, so this is "free" — no separate eth_getLogs.
		for _, l := range logs {
			logID := fmt.Sprintf("%s-%d", l.TxHash, l.LogIndex)
			topic0, topic1, topic2, topic3 := "", "", "", ""
			if len(l.Topics) > 0 {
				topic0 = l.Topics[0]
			}
			if len(l.Topics) > 1 {
				topic1 = l.Topics[1]
			}
			if len(l.Topics) > 2 {
				topic2 = l.Topics[2]
			}
			if len(l.Topics) > 3 {
				topic3 = l.Topics[3]
			}
			_ = idx.store.Exec(ctx, idx.upsertLogSQL(),
				logID, l.TxHash, int64(l.LogIndex), int64(block.Number),
				l.Address, topic0, topic1, topic2, topic3, l.Data,
				block.Timestamp, time.Now())

			// Topic-0 of ERC-20 Transfer is identical to ERC-721 Transfer;
			// distinguish on topic count (ERC-20 has 3 topics, ERC-721 has 4).
			if topic0 == TopicTransferERC20 {
				from := topicToAddr(topic1)
				to := topicToAddr(topic2)
				tokenLo := strings.ToLower(l.Address)
				if len(l.Topics) == 3 {
					// ERC-20: value is the data field (uint256 hex)
					_ = idx.store.Exec(ctx, idx.upsertTokenTransferSQL(),
						logID, l.TxHash, int64(l.LogIndex), int64(block.Number),
						l.Address, "ERC-20", from, to, l.Data, "",
						block.Timestamp, time.Now())
					tokensSeen[tokenLo] = "ERC-20"
					// Update per-holder balance state. l.Data is the
					// 32-byte uint256 value. We move it from `from` to
					// `to`, skipping the zero-address (mint source +
					// burn sink — those aren't real holders).
					idx.applyERC20BalanceDelta(ctx, tokenLo, from, to, l.Data)
				} else if len(l.Topics) == 4 {
					// ERC-721: token_id in topic3, value is "1"
					_ = idx.store.Exec(ctx, idx.upsertTokenTransferSQL(),
						logID, l.TxHash, int64(l.LogIndex), int64(block.Number),
						l.Address, "ERC-721", from, to, "1", topic3,
						block.Timestamp, time.Now())
					tokensSeen[tokenLo] = "ERC-721"
					// For ERC-721 the entire token_id moves: delete
					// from the old owner, insert for the new owner.
					idx.applyERC721BalanceDelta(ctx, tokenLo, from, to, topic3)
				}
			}
		}

		// Each tx touches up to 3 distinct addresses. Dedup per-tx so a
		// self-send doesn't double-count.
		touched := make(map[string]struct{}, 3)
		if tx.From != "" {
			touched[tx.From] = struct{}{}
		}
		if tx.To != "" {
			touched[tx.To] = struct{}{}
		}
		if tx.ContractAddress != "" {
			touched[tx.ContractAddress] = struct{}{}
			addrIsContract[tx.ContractAddress] = true
		}
		for a := range touched {
			addrTxCount[a]++
		}
	}

	// Resolve token metadata (name/symbol/decimals) via eth_call for any
	// token we saw a transfer for and haven't already cached. Best-effort:
	// failures emit a row with empty metadata so the explorer at least
	// shows the address + transfer count.
	for tokenAddr, tType := range tokensSeen {
		addrIsContract[tokenAddr] = true
		name, symbol, decimals, totalSupply := idx.resolveTokenMeta(ctx, tokenAddr, tType)
		_ = idx.store.Exec(ctx, idx.upsertTokenSQL(),
			tokenAddr, name, symbol, decimals, totalSupply, tType,
			time.Now(), time.Now())
	}

	// Resolve is_contract for everything we haven't already pinned as
	// a contract via tx.ContractAddress. Anvil-local RPCs return code
	// for any address in O(1); cost is bounded by unique addresses per
	// block (~5 typical). Failures are non-fatal — fall back to false.
	for addr := range addrTxCount {
		if addrIsContract[addr] {
			continue
		}
		code, err := idx.adapter.GetCode(ctx, addr)
		if err == nil && code != "" && code != "0x" {
			addrIsContract[addr] = true
		}
	}

	// Upsert discovered addresses with tx_count increment + is_contract
	// OR-merge so contract status, once true, stays true. Parameter order
	// must match the SQL: (hash, tx_count, is_contract, created_at, updated_at).
	now := time.Now()
	for addr, dCount := range addrTxCount {
		_ = idx.store.Exec(ctx, idx.upsertAddrSQL(),
			addr, int64(dCount), addrIsContract[addr], now, now)
	}

	// Broadcast new block
	idx.subscriber.BroadcastBlock(block)

	return nil
}

// backfillAddresses upserts every from/to/contract address found in
// evm_transactions into evm_addresses, counting how many distinct txs
// touched each address and verifying contract status via eth_getCode for
// any address not already flagged. Safe to re-run: the upsert is an
// increment of tx_count, but if the caller has already seeded the table
// with per-block deltas, running this again would double-count. Intended
// for first-time bootstrap on an empty addresses table; the
// per-block upsert in Run() is the steady-state path.
func (idx *Indexer) backfillAddresses(ctx context.Context) (int, error) {
	addrTxCount := make(map[string]uint64)
	addrIsContract := make(map[string]bool)
	rows, err := idx.store.Query(ctx, `SELECT from_addr, to_addr, contract_addr FROM evm_transactions`)
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		touched := make(map[string]struct{}, 3)
		if v, ok := r["from_addr"].(string); ok && v != "" {
			touched[v] = struct{}{}
		}
		if v, ok := r["to_addr"].(string); ok && v != "" {
			touched[v] = struct{}{}
		}
		if v, ok := r["contract_addr"].(string); ok && v != "" {
			touched[v] = struct{}{}
			addrIsContract[v] = true
		}
		for a := range touched {
			addrTxCount[a]++
		}
	}
	for a := range addrTxCount {
		if addrIsContract[a] {
			continue
		}
		code, err := idx.adapter.GetCode(ctx, a)
		if err == nil && code != "" && code != "0x" {
			addrIsContract[a] = true
		}
	}
	now := time.Now()
	q := idx.upsertAddrSQL()
	for a, c := range addrTxCount {
		_ = idx.store.Exec(ctx, q, a, int64(c), addrIsContract[a], now, now)
	}
	return len(addrTxCount), nil
}

// upsertAddrSQL returns an INSERT … ON CONFLICT … DO UPDATE that:
//   - on first sight: writes hash, is_contract, tx_count delta, timestamps.
//   - on subsequent sight: increments tx_count by the delta and OR-merges
//     is_contract (once true, stays true).
//
// The legacy form was INSERT OR IGNORE / ON CONFLICT DO NOTHING, which
// meant tx_count was permanently 0 for every address ever seen and
// is_contract could never be retro-corrected if an address was first
// observed as plain `to` and only later resolved as a contract.
//
// Caller passes: (addr, isContract, txCountDelta, createdAt, updatedAt).
func (idx *Indexer) upsertAddrSQL() string {
	switch idx.store.Backend() {
	case storage.BackendPostgres:
		return `INSERT INTO evm_addresses
			(hash, balance, tx_count, is_contract, code, creator, creation_tx, created_at, updated_at)
			VALUES ($1,'0',$3,$2,'','','',$4,$5)
			ON CONFLICT (hash) DO UPDATE SET
				tx_count    = evm_addresses.tx_count + EXCLUDED.tx_count,
				is_contract = evm_addresses.is_contract OR EXCLUDED.is_contract,
				updated_at  = EXCLUDED.updated_at`
	default:
		// SQLite: `OR` between integers works as bitwise/logical for the 0/1
		// boolean column. MAX() also works and is more obviously a merge.
		return `INSERT INTO evm_addresses
			(hash, balance, tx_count, is_contract, code, creator, creation_tx, created_at, updated_at)
			VALUES (?,'0',?,?,'','','',?,?)
			ON CONFLICT (hash) DO UPDATE SET
				tx_count    = evm_addresses.tx_count + excluded.tx_count,
				is_contract = MAX(evm_addresses.is_contract, excluded.is_contract),
				updated_at  = excluded.updated_at`
	}
}

// upsertTxSQL returns the correct upsert SQL for transactions
func (idx *Indexer) upsertTxSQL() string {
	switch idx.store.Backend() {
	case storage.BackendPostgres:
		return `INSERT INTO evm_transactions (hash, block_hash, block_number, tx_index,
			from_addr, to_addr, value, gas, gas_price, gas_used, nonce, input, status, contract_addr, timestamp, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
			ON CONFLICT (hash) DO NOTHING`
	default:
		return `INSERT OR IGNORE INTO evm_transactions (hash, block_hash, block_number, tx_index,
			from_addr, to_addr, value, gas, gas_price, gas_used, nonce, input, status, contract_addr, timestamp, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	}
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
	api := r.PathPrefix("/v1/explorer").Subrouter()

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
	// Decode event and index the latest block.

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

// Subscriber handles WebSocket for live block streaming.
//
// External pub/sub hook (2026-05-21):
//
// `OnBroadcast` is an optional callback fired on every
// BroadcastBlock / BroadcastTransaction. It lets an enclosing process
// — e.g. luxfi/explorer which embeds this indexer as a library — re-
// publish the same events to its own pub/sub fabric (SSE channels,
// gRPC streams, NATS, etc.) without touching the indexer's own
// WebSocket flow.
//
// The callback is invoked synchronously from the indexing goroutine,
// so implementations must NOT block on slow consumers (use a buffered
// channel + drop-on-full, the way luxfi/explorer's SSE registry does).
//
// Zero-value (nil) is a no-op: existing WebSocket subscribers see no
// change, and the indexer keeps working standalone.
type Subscriber struct {
	clients     map[*websocket.Conn]bool
	broadcast   chan interface{}
	register    chan *websocket.Conn
	unregister  chan *websocket.Conn
	mu          sync.RWMutex
	upgrader    websocket.Upgrader
	maxClients  int
	clientSem   chan struct{}
	OnBroadcast func(eventType string, data any) // optional, see header
}

func NewSubscriber() *Subscriber {
	return &Subscriber{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan interface{}, 100),
		register:   make(chan *websocket.Conn, 16),
		unregister: make(chan *websocket.Conn, 16),
		upgrader:   websocket.Upgrader{HandshakeTimeout: 10 * time.Second},
		maxClients: 1024,
		clientSem:  make(chan struct{}, 1024),
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
	select {
	case s.clientSem <- struct{}{}:
	default:
		http.Error(w, `{"error":"too many connections"}`, http.StatusServiceUnavailable)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		<-s.clientSem
		return
	}

	conn.SetReadLimit(65536)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	s.register <- conn
	_ = conn.WriteJSON(map[string]interface{}{"type": "connected"})
	go func() {
		defer func() {
			s.unregister <- conn
			<-s.clientSem
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		}
	}()
}

func (s *Subscriber) BroadcastBlock(block *EVMBlock) {
	s.broadcast <- map[string]interface{}{"type": "block", "data": block}
	if s.OnBroadcast != nil {
		// `"blocks"` (plural) is the channel name the explorer SPA
		// subscribes to via SSE/WS. Keeping the wire-shape consistent
		// across embedders.
		s.OnBroadcast("blocks", block)
	}
}

// upsertLogSQL returns the correct upsert SQL for evm_logs.
// Param order: (id, tx_hash, log_index, block_number, address,
//
//	topic0, topic1, topic2, topic3, data, timestamp, created_at)
func (idx *Indexer) upsertLogSQL() string {
	switch idx.store.Backend() {
	case storage.BackendPostgres:
		return `INSERT INTO evm_logs
			(id, tx_hash, log_index, block_number, address, topic0, topic1, topic2, topic3, data, timestamp, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT (id) DO NOTHING`
	default:
		return `INSERT OR IGNORE INTO evm_logs
			(id, tx_hash, log_index, block_number, address, topic0, topic1, topic2, topic3, data, timestamp, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`
	}
}

// upsertTokenTransferSQL returns the correct upsert SQL for evm_token_transfers.
// Param order: (id, tx_hash, log_index, block_number, token_address, token_type,
//
//	from_addr, to_addr, value, token_id, timestamp, created_at)
func (idx *Indexer) upsertTokenTransferSQL() string {
	switch idx.store.Backend() {
	case storage.BackendPostgres:
		return `INSERT INTO evm_token_transfers
			(id, tx_hash, log_index, block_number, token_address, token_type, from_addr, to_addr, value, token_id, timestamp, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT (id) DO NOTHING`
	default:
		return `INSERT OR IGNORE INTO evm_token_transfers
			(id, tx_hash, log_index, block_number, token_address, token_type, from_addr, to_addr, value, token_id, timestamp, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`
	}
}

// upsertTokenSQL returns the correct upsert SQL for evm_tokens.
// Param order: (address, name, symbol, decimals, total_supply, token_type,
//
//	created_at, updated_at). tx_count + holder_count are
//	maintained separately and default to 0 on first insert.
func (idx *Indexer) upsertTokenSQL() string {
	switch idx.store.Backend() {
	case storage.BackendPostgres:
		return `INSERT INTO evm_tokens
			(address, name, symbol, decimals, total_supply, token_type, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (address) DO UPDATE SET
				name         = COALESCE(NULLIF(EXCLUDED.name,    ''), evm_tokens.name),
				symbol       = COALESCE(NULLIF(EXCLUDED.symbol,  ''), evm_tokens.symbol),
				decimals     = COALESCE(NULLIF(EXCLUDED.decimals, 0), evm_tokens.decimals),
				total_supply = COALESCE(NULLIF(EXCLUDED.total_supply, ''), evm_tokens.total_supply),
				token_type   = COALESCE(NULLIF(EXCLUDED.token_type,   ''), evm_tokens.token_type),
				updated_at   = EXCLUDED.updated_at`
	default:
		return `INSERT INTO evm_tokens
			(address, name, symbol, decimals, total_supply, token_type, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?)
			ON CONFLICT (address) DO UPDATE SET
				name         = CASE WHEN excluded.name         != '' THEN excluded.name         ELSE evm_tokens.name         END,
				symbol       = CASE WHEN excluded.symbol       != '' THEN excluded.symbol       ELSE evm_tokens.symbol       END,
				decimals     = CASE WHEN excluded.decimals     != 0  THEN excluded.decimals     ELSE evm_tokens.decimals     END,
				total_supply = CASE WHEN excluded.total_supply != '' THEN excluded.total_supply ELSE evm_tokens.total_supply END,
				token_type   = CASE WHEN excluded.token_type   != '' THEN excluded.token_type   ELSE evm_tokens.token_type   END,
				updated_at   = excluded.updated_at`
	}
}

// resolveTokenMeta does a best-effort eth_call name/symbol/decimals/totalSupply
// resolution. Empty strings + 0 decimals for the metadata when the contract
// doesn't implement the ERC-20 interface (or the call reverts). For ERC-721 we
// don't fetch totalSupply (the standard's totalSupply is optional + expensive).
func (idx *Indexer) resolveTokenMeta(ctx context.Context, addr, tokenType string) (name, symbol string, decimals uint8, totalSupply string) {
	info, err := idx.adapter.GetTokenInfo(ctx, addr)
	if err != nil || info == nil {
		return "", "", 0, "0"
	}
	totalSupply = info.TotalSupply
	if totalSupply == "" {
		totalSupply = "0"
	}
	return info.Name, info.Symbol, info.Decimals, totalSupply
}

// topicToAddr extracts the trailing 20-byte address from a 32-byte indexed
// topic. ERC-20 Transfer indexes `from` + `to` as topics in their
// uint256-padded form: a topic looks like 0x0000…<20-byte addr>. Returns
// "" for empty / malformed input.
func topicToAddr(topic string) string {
	t := strings.TrimPrefix(topic, "0x")
	if len(t) < 40 {
		return ""
	}
	return "0x" + strings.ToLower(t[len(t)-40:])
}

// upsertBalanceSQL returns the correct upsert SQL for evm_token_balances.
//
// Schema: (token_address, address, token_id, value, token_type, updated_at)
// Primary key: (token_address, address, token_id). token_id is "" for
// ERC-20 holdings, the actual hex tokenId for ERC-721.
func (idx *Indexer) upsertBalanceSQL() string {
	switch idx.store.Backend() {
	case storage.BackendPostgres:
		return `INSERT INTO evm_token_balances
			(token_address, address, token_id, value, token_type, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (token_address, address, token_id) DO UPDATE SET
				value      = EXCLUDED.value,
				token_type = COALESCE(NULLIF(EXCLUDED.token_type,''), evm_token_balances.token_type),
				updated_at = EXCLUDED.updated_at`
	default:
		return `INSERT INTO evm_token_balances
			(token_address, address, token_id, value, token_type, updated_at)
			VALUES (?,?,?,?,?,?)
			ON CONFLICT (token_address, address, token_id) DO UPDATE SET
				value      = excluded.value,
				token_type = CASE WHEN excluded.token_type != '' THEN excluded.token_type ELSE evm_token_balances.token_type END,
				updated_at = excluded.updated_at`
	}
}

// readBalance loads the current `value` for (token, holder, tokenId) as a
// *big.Int. Returns big.NewInt(0) when no row exists.
func (idx *Indexer) readBalance(ctx context.Context, token, holder, tokenID string) *big.Int {
	rows, err := idx.store.Query(ctx,
		"SELECT value FROM evm_token_balances WHERE token_address = ? AND address = ? AND token_id = ? LIMIT 1",
		token, holder, tokenID,
	)
	if err != nil || len(rows) == 0 {
		return big.NewInt(0)
	}
	v, _ := rows[0]["value"].(string)
	if v == "" {
		return big.NewInt(0)
	}
	n, ok := new(big.Int).SetString(v, 10)
	if !ok {
		return big.NewInt(0)
	}
	return n
}

// applyERC20BalanceDelta moves `valueHex` (32-byte uint256, hex with "0x"
// prefix optional) from `fromAddr` to `toAddr` in evm_token_balances. The
// zero address is excluded (it's the conventional mint source + burn sink,
// not a real holder).
func (idx *Indexer) applyERC20BalanceDelta(ctx context.Context, token, fromAddr, toAddr, valueHex string) {
	v, ok := new(big.Int).SetString(strings.TrimPrefix(valueHex, "0x"), 16)
	if !ok || v.Sign() == 0 {
		return
	}
	now := time.Now()
	zero := "0x0000000000000000000000000000000000000000"
	if fromAddr != "" && fromAddr != zero {
		cur := idx.readBalance(ctx, token, fromAddr, "")
		newV := new(big.Int).Sub(cur, v)
		if newV.Sign() < 0 {
			newV = big.NewInt(0)
		}
		_ = idx.store.Exec(ctx, idx.upsertBalanceSQL(),
			token, fromAddr, "", newV.String(), "ERC-20", now)
	}
	if toAddr != "" && toAddr != zero {
		cur := idx.readBalance(ctx, token, toAddr, "")
		newV := new(big.Int).Add(cur, v)
		_ = idx.store.Exec(ctx, idx.upsertBalanceSQL(),
			token, toAddr, "", newV.String(), "ERC-20", now)
	}
}

// applyERC721BalanceDelta moves a single NFT (tokenId from topic3) between
// owners. Each ERC-721 row represents "this address owns tokenId X" with
// value="1". On transfer we remove the source row and write a destination
// row.
func (idx *Indexer) applyERC721BalanceDelta(ctx context.Context, token, fromAddr, toAddr, tokenIDHex string) {
	now := time.Now()
	zero := "0x0000000000000000000000000000000000000000"
	if fromAddr != "" && fromAddr != zero {
		_ = idx.store.Exec(ctx,
			"DELETE FROM evm_token_balances WHERE token_address = ? AND address = ? AND token_id = ?",
			token, fromAddr, tokenIDHex,
		)
	}
	if toAddr != "" && toAddr != zero {
		_ = idx.store.Exec(ctx, idx.upsertBalanceSQL(),
			token, toAddr, tokenIDHex, "1", "ERC-721", now)
	}
}
