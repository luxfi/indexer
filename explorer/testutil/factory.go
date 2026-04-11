// Package testutil provides test data factories for explorer tests.
// Go test data factory for explorer tests.
package testutil

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps a test SQLite database with the indexer schema.
type DB struct {
	*sql.DB
	Path string
}

var seq uint64

func nextSeq() uint64 { return atomic.AddUint64(&seq, 1) }

// NewTestDB creates a temporary SQLite database with the indexer schema.
func NewTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "indexer.db")

	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_journal_mode=WAL&_synchronous=NORMAL", path))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := createSchema(db); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	return &DB{DB: db, Path: path}
}

func createSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS blocks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chain_id INTEGER NOT NULL,
		number INTEGER NOT NULL,
		hash BLOB NOT NULL,
		parent_hash BLOB NOT NULL,
		nonce BLOB,
		miner BLOB NOT NULL,
		difficulty TEXT,
		total_difficulty TEXT,
		size INTEGER NOT NULL DEFAULT 0,
		gas_limit INTEGER NOT NULL DEFAULT 0,
		gas_used INTEGER NOT NULL DEFAULT 0,
		base_fee TEXT,
		timestamp INTEGER NOT NULL,
		transaction_count INTEGER NOT NULL DEFAULT 0,
		ext_data_hash BLOB,
		ext_data_gas_used INTEGER,
		block_gas_cost TEXT,
		extra_data BLOB,
		logs_bloom BLOB,
		state_root BLOB,
		transactions_root BLOB,
		receipts_root BLOB,
		indexed_at TEXT DEFAULT (datetime('now')),
		consensus_state TEXT DEFAULT 'finalized',
		is_empty INTEGER DEFAULT 0,
		UNIQUE(chain_id, number)
	);
	CREATE INDEX IF NOT EXISTS idx_blocks_hash ON blocks(hash);
	CREATE INDEX IF NOT EXISTS idx_blocks_timestamp ON blocks(timestamp);

	CREATE TABLE IF NOT EXISTS transactions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chain_id INTEGER NOT NULL,
		hash BLOB NOT NULL,
		block_number INTEGER NOT NULL,
		block_hash BLOB NOT NULL,
		transaction_index INTEGER NOT NULL,
		from_address_hash BLOB NOT NULL,
		to_address_hash BLOB,
		value TEXT NOT NULL DEFAULT '0',
		gas INTEGER NOT NULL DEFAULT 0,
		gas_price TEXT,
		gas_used INTEGER,
		max_fee_per_gas TEXT,
		max_priority_fee_per_gas TEXT,
		max_fee_per_blob_gas TEXT,
		blob_gas_used INTEGER,
		blob_gas_price TEXT,
		input BLOB,
		nonce INTEGER NOT NULL DEFAULT 0,
		type INTEGER NOT NULL DEFAULT 0,
		status INTEGER,
		error TEXT,
		revert_reason TEXT,
		created_contract_address_hash BLOB,
		created_contract_code_indexed_at TEXT,
		cumulative_gas_used INTEGER,
		block_timestamp INTEGER NOT NULL,
		inserted_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now')),
		UNIQUE(chain_id, hash)
	);
	CREATE INDEX IF NOT EXISTS idx_txs_block ON transactions(chain_id, block_number);
	CREATE INDEX IF NOT EXISTS idx_txs_from ON transactions(from_address_hash);
	CREATE INDEX IF NOT EXISTS idx_txs_to ON transactions(to_address_hash);

	CREATE TABLE IF NOT EXISTS internal_transactions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chain_id INTEGER NOT NULL,
		transaction_hash BLOB NOT NULL,
		block_number INTEGER NOT NULL,
		block_hash BLOB NOT NULL,
		"index" INTEGER NOT NULL,
		trace_address TEXT NOT NULL DEFAULT '{}',
		type TEXT NOT NULL,
		call_type TEXT,
		from_address_hash BLOB NOT NULL,
		to_address_hash BLOB,
		created_contract_address_hash BLOB,
		created_contract_code BLOB,
		value TEXT NOT NULL DEFAULT '0',
		gas INTEGER,
		gas_used INTEGER,
		input BLOB,
		output BLOB,
		init BLOB,
		error TEXT,
		block_timestamp INTEGER NOT NULL,
		inserted_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_itxs_tx ON internal_transactions(transaction_hash);

	CREATE TABLE IF NOT EXISTS logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chain_id INTEGER NOT NULL,
		block_number INTEGER NOT NULL,
		block_hash BLOB NOT NULL,
		transaction_hash BLOB NOT NULL,
		transaction_index INTEGER NOT NULL,
		"index" INTEGER NOT NULL,
		address_hash BLOB NOT NULL,
		data BLOB,
		first_topic BLOB,
		second_topic BLOB,
		third_topic BLOB,
		fourth_topic BLOB,
		type TEXT,
		block_timestamp INTEGER NOT NULL,
		inserted_at TEXT DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_logs_tx ON logs(transaction_hash);
	CREATE INDEX IF NOT EXISTS idx_logs_addr ON logs(address_hash);

	CREATE TABLE IF NOT EXISTS addresses (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chain_id INTEGER NOT NULL,
		hash BLOB NOT NULL,
		fetched_coin_balance TEXT,
		fetched_coin_balance_block_number INTEGER,
		contract_code BLOB,
		transactions_count INTEGER DEFAULT 0,
		token_transfers_count INTEGER DEFAULT 0,
		gas_used TEXT DEFAULT '0',
		nonce INTEGER,
		decompiled INTEGER DEFAULT 0,
		verified INTEGER DEFAULT 0,
		inserted_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now')),
		UNIQUE(chain_id, hash)
	);

	CREATE TABLE IF NOT EXISTS smart_contracts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chain_id INTEGER NOT NULL,
		address_hash BLOB NOT NULL,
		name TEXT NOT NULL,
		compiler_version TEXT NOT NULL,
		optimization INTEGER DEFAULT 0,
		optimization_runs INTEGER,
		contract_source_code TEXT,
		abi TEXT,
		constructor_arguments TEXT,
		evm_version TEXT,
		file_path TEXT,
		external_libraries TEXT DEFAULT '[]',
		secondary_sources TEXT DEFAULT '[]',
		verified_via TEXT,
		partially_verified INTEGER DEFAULT 0,
		is_vyper_contract INTEGER DEFAULT 0,
		is_changed_bytecode INTEGER DEFAULT 0,
		license_type TEXT,
		inserted_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now')),
		UNIQUE(chain_id, address_hash)
	);

	CREATE TABLE IF NOT EXISTS tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chain_id INTEGER NOT NULL,
		contract_address_hash BLOB NOT NULL,
		name TEXT,
		symbol TEXT,
		total_supply TEXT,
		decimals INTEGER,
		type TEXT NOT NULL,
		holder_count INTEGER DEFAULT 0,
		fiat_value TEXT,
		circulating_market_cap TEXT,
		total_supply_updated_at_block INTEGER,
		cataloged INTEGER DEFAULT 0,
		skip_metadata INTEGER DEFAULT 0,
		icon_url TEXT,
		inserted_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now')),
		UNIQUE(chain_id, contract_address_hash)
	);

	CREATE TABLE IF NOT EXISTS token_transfers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chain_id INTEGER NOT NULL,
		transaction_hash BLOB NOT NULL,
		log_index INTEGER NOT NULL,
		block_number INTEGER NOT NULL,
		block_hash BLOB NOT NULL,
		from_address_hash BLOB NOT NULL,
		to_address_hash BLOB NOT NULL,
		token_contract_address_hash BLOB NOT NULL,
		amount TEXT,
		token_id TEXT,
		token_ids TEXT,
		amounts TEXT,
		token_type TEXT,
		block_timestamp INTEGER NOT NULL,
		inserted_at TEXT DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_tt_tx ON token_transfers(transaction_hash);
	CREATE INDEX IF NOT EXISTS idx_tt_token ON token_transfers(token_contract_address_hash);

	CREATE TABLE IF NOT EXISTS address_current_token_balances (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chain_id INTEGER NOT NULL,
		address_hash BLOB NOT NULL,
		token_contract_address_hash BLOB NOT NULL,
		value TEXT,
		value_fetched_at TEXT,
		block_number INTEGER NOT NULL,
		token_id TEXT,
		token_type TEXT,
		old_value TEXT,
		inserted_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS address_coin_balances (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chain_id INTEGER NOT NULL,
		address_hash BLOB NOT NULL,
		block_number INTEGER NOT NULL,
		value TEXT,
		value_fetched_at TEXT,
		delta TEXT,
		inserted_at TEXT DEFAULT (datetime('now'))
	);
	`
	_, err := db.Exec(schema)
	return err
}

// ---- Random Data Generators ----

func RandomHash() []byte {
	b := make([]byte, 32)
	rand.Read(b)
	return b
}

func RandomAddress() []byte {
	b := make([]byte, 20)
	rand.Read(b)
	return b
}

func RandomHexHash() string {
	return "0x" + hex.EncodeToString(RandomHash())
}

func RandomHexAddress() string {
	return "0x" + hex.EncodeToString(RandomAddress())
}

// ---- Factory Types ----

// Block mirrors the indexer's block row.
type Block struct {
	ChainID          int64
	Number           int64
	Hash             []byte
	ParentHash       []byte
	Nonce            []byte
	Miner            []byte
	Difficulty       string
	TotalDifficulty  string
	Size             int
	GasLimit         int64
	GasUsed          int64
	BaseFee          string
	Timestamp        int64
	TransactionCount int
	ConsensusState   string
}

// Transaction mirrors the indexer's transaction row.
type Transaction struct {
	ChainID              int64
	Hash                 []byte
	BlockNumber          int64
	BlockHash            []byte
	TransactionIndex     int
	FromAddress          []byte
	ToAddress            []byte
	Value                string
	Gas                  int64
	GasPrice             string
	GasUsed              int64
	MaxFeePerGas         string
	MaxPriorityFeePerGas string
	Input                []byte
	Nonce                int64
	Type                 int
	Status               *int  // nil = pending
	Error                string
	RevertReason         string
	CreatedContract      []byte
	CumulativeGasUsed    int64
	BlockTimestamp        int64
}

// Address mirrors the indexer's address row.
type Address struct {
	ChainID              int64
	Hash                 []byte
	FetchedCoinBalance   string
	FetchedBalanceBlock  int64
	ContractCode         []byte
	TransactionsCount    int
	TokenTransfersCount  int
	GasUsed              string
	Nonce                int64
	Verified             bool
}

// Token mirrors the indexer's token row.
type Token struct {
	ChainID         int64
	ContractAddress []byte
	Name            string
	Symbol          string
	TotalSupply     string
	Decimals        int
	Type            string
	HolderCount     int
	IconURL         string
}

// TokenTransfer mirrors the indexer's token_transfer row.
type TokenTransfer struct {
	ChainID              int64
	TransactionHash      []byte
	LogIndex             int
	BlockNumber          int64
	BlockHash            []byte
	FromAddress          []byte
	ToAddress            []byte
	TokenContractAddress []byte
	Amount               string
	TokenID              string
	TokenType            string
	BlockTimestamp        int64
}

// Log mirrors the indexer's log row.
type Log struct {
	ChainID          int64
	BlockNumber      int64
	BlockHash        []byte
	TransactionHash  []byte
	TransactionIndex int
	Index            int
	Address          []byte
	Data             []byte
	FirstTopic       []byte
	SecondTopic      []byte
	ThirdTopic       []byte
	FourthTopic      []byte
	BlockTimestamp    int64
}

// InternalTransaction mirrors the indexer's internal_transaction row.
type InternalTx struct {
	ChainID         int64
	TransactionHash []byte
	BlockNumber     int64
	BlockHash       []byte
	Index           int
	TraceAddress    string
	Type            string
	CallType        string
	FromAddress     []byte
	ToAddress       []byte
	Value           string
	Gas             int64
	GasUsed         int64
	Input           []byte
	Output          []byte
	Error           string
	BlockTimestamp   int64
}

// SmartContract mirrors the indexer's smart_contract row.
type SmartContract struct {
	ChainID         int64
	AddressHash     []byte
	Name            string
	CompilerVersion string
	Optimization    bool
	OptRuns         int
	SourceCode      string
	ABI             string
	EVMVersion      string
	VerifiedVia     string
	IsVyper         bool
	LicenseType     string
}

// ---- Default Factory Functions ----

const DefaultChainID int64 = 96369

func DefaultBlock() Block {
	n := int64(nextSeq())
	ts := time.Now().Unix() - (1000 - n)
	return Block{
		ChainID:          DefaultChainID,
		Number:           n,
		Hash:             RandomHash(),
		ParentHash:       RandomHash(),
		Nonce:            []byte{0, 0, 0, 0, 0, 0, 0, byte(n)},
		Miner:            RandomAddress(),
		Difficulty:       fmt.Sprintf("%d", 100+n),
		TotalDifficulty:  fmt.Sprintf("%d", 100000+n),
		Size:             1024 + int(n*100),
		GasLimit:         8000000,
		GasUsed:          21000 * n,
		BaseFee:          "25000000000",
		Timestamp:        ts,
		TransactionCount: 0,
		ConsensusState:   "finalized",
	}
}

func DefaultTransaction(block Block) Transaction {
	n := int64(nextSeq())
	status := 1
	return Transaction{
		ChainID:          block.ChainID,
		Hash:             RandomHash(),
		BlockNumber:      block.Number,
		BlockHash:        block.Hash,
		TransactionIndex: int(n % 100),
		FromAddress:      RandomAddress(),
		ToAddress:        RandomAddress(),
		Value:            new(big.Int).Mul(big.NewInt(n), big.NewInt(1e15)).String(),
		Gas:              21000,
		GasPrice:         "25000000000",
		GasUsed:          21000,
		Nonce:            n,
		Type:             0,
		Status:           &status,
		BlockTimestamp:    block.Timestamp,
	}
}

func DefaultEIP1559Transaction(block Block) Transaction {
	tx := DefaultTransaction(block)
	tx.Type = 2
	tx.MaxFeePerGas = "50000000000"
	tx.MaxPriorityFeePerGas = "2000000000"
	tx.GasPrice = ""
	return tx
}

func DefaultPendingTransaction(block Block) Transaction {
	tx := DefaultTransaction(block)
	tx.Status = nil
	return tx
}

func DefaultFailedTransaction(block Block) Transaction {
	tx := DefaultTransaction(block)
	status := 0
	tx.Status = &status
	tx.Error = "execution reverted"
	tx.RevertReason = "ERC20: transfer amount exceeds balance"
	return tx
}

func DefaultContractCreation(block Block) Transaction {
	tx := DefaultTransaction(block)
	tx.ToAddress = nil
	tx.CreatedContract = RandomAddress()
	tx.Input = []byte{0x60, 0x80, 0x60, 0x40} // contract bytecode prefix
	return tx
}

func DefaultAddress() Address {
	balance := new(big.Int).Mul(big.NewInt(int64(nextSeq())), big.NewInt(1e18))
	return Address{
		ChainID:            DefaultChainID,
		Hash:               RandomAddress(),
		FetchedCoinBalance: balance.String(),
		FetchedBalanceBlock: int64(nextSeq()),
		TransactionsCount:   int(nextSeq() % 100),
		TokenTransfersCount: int(nextSeq() % 50),
		GasUsed:            "0",
		Nonce:              int64(nextSeq() % 100),
	}
}

func DefaultContractAddress() Address {
	addr := DefaultAddress()
	addr.ContractCode = []byte{0x60, 0x80, 0x60, 0x40, 0x52}
	return addr
}

func DefaultToken() Token {
	n := nextSeq()
	return Token{
		ChainID:         DefaultChainID,
		ContractAddress: RandomAddress(),
		Name:            fmt.Sprintf("Token %d", n),
		Symbol:          fmt.Sprintf("TKN%d", n),
		TotalSupply:     new(big.Int).Mul(big.NewInt(1e6), big.NewInt(1e18)).String(),
		Decimals:        18,
		Type:            "ERC-20",
		HolderCount:     int(n % 1000),
	}
}

func DefaultERC721Token() Token {
	t := DefaultToken()
	t.Type = "ERC-721"
	t.Decimals = 0
	t.TotalSupply = fmt.Sprintf("%d", nextSeq()%10000)
	return t
}

func DefaultTokenTransfer(tx Transaction, token Token) TokenTransfer {
	return TokenTransfer{
		ChainID:              tx.ChainID,
		TransactionHash:      tx.Hash,
		LogIndex:             int(nextSeq() % 100),
		BlockNumber:          tx.BlockNumber,
		BlockHash:            tx.BlockHash,
		FromAddress:          tx.FromAddress,
		ToAddress:            tx.ToAddress,
		TokenContractAddress: token.ContractAddress,
		Amount:               new(big.Int).Mul(big.NewInt(100), big.NewInt(1e18)).String(),
		TokenType:            token.Type,
		BlockTimestamp:        tx.BlockTimestamp,
	}
}

func DefaultLog(tx Transaction) Log {
	// Transfer(address,address,uint256) topic
	topic0, _ := hex.DecodeString("ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	return Log{
		ChainID:          tx.ChainID,
		BlockNumber:      tx.BlockNumber,
		BlockHash:        tx.BlockHash,
		TransactionHash:  tx.Hash,
		TransactionIndex: tx.TransactionIndex,
		Index:            int(nextSeq() % 100),
		Address:          RandomAddress(),
		Data:             RandomHash(),
		FirstTopic:       topic0,
		SecondTopic:      padAddress(tx.FromAddress),
		ThirdTopic:       padAddress(tx.ToAddress),
		BlockTimestamp:    tx.BlockTimestamp,
	}
}

func DefaultInternalTx(tx Transaction) InternalTx {
	return InternalTx{
		ChainID:         tx.ChainID,
		TransactionHash: tx.Hash,
		BlockNumber:     tx.BlockNumber,
		BlockHash:       tx.BlockHash,
		Index:           int(nextSeq()),
		TraceAddress:    "{}",
		Type:            "call",
		CallType:        "call",
		FromAddress:     tx.FromAddress,
		ToAddress:       RandomAddress(),
		Value:           "1000000000000000",
		Gas:             21000,
		GasUsed:         15000,
		Input:           []byte{},
		Output:          []byte{},
		BlockTimestamp:   tx.BlockTimestamp,
	}
}

func DefaultSmartContract() SmartContract {
	return SmartContract{
		ChainID:         DefaultChainID,
		AddressHash:     RandomAddress(),
		Name:            "SimpleStorage",
		CompilerVersion: "v0.8.10+commit.fc410830",
		Optimization:    false,
		OptRuns:         200,
		SourceCode:      "pragma solidity ^0.8.10;\ncontract SimpleStorage { uint storedData; function set(uint x) public { storedData = x; } function get() public view returns (uint) { return storedData; } }",
		ABI:             `[{"inputs":[{"name":"x","type":"uint256"}],"name":"set","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"get","outputs":[{"name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`,
		EVMVersion:      "london",
		VerifiedVia:     "manual",
		LicenseType:     "MIT",
	}
}

// ---- Insert Functions ----

func (db *DB) InsertBlock(t *testing.T, b Block) Block {
	t.Helper()
	_, err := db.Exec(`INSERT INTO blocks (chain_id, number, hash, parent_hash, nonce, miner, difficulty, total_difficulty, size, gas_limit, gas_used, base_fee, timestamp, transaction_count, consensus_state) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ChainID, b.Number, b.Hash, b.ParentHash, b.Nonce, b.Miner, b.Difficulty, b.TotalDifficulty, b.Size, b.GasLimit, b.GasUsed, b.BaseFee, b.Timestamp, b.TransactionCount, b.ConsensusState)
	if err != nil {
		t.Fatalf("insert block: %v", err)
	}
	return b
}

func (db *DB) InsertTransaction(t *testing.T, tx Transaction) Transaction {
	t.Helper()
	_, err := db.Exec(`INSERT INTO transactions (chain_id, hash, block_number, block_hash, transaction_index, from_address_hash, to_address_hash, value, gas, gas_price, gas_used, max_fee_per_gas, max_priority_fee_per_gas, input, nonce, type, status, error, revert_reason, created_contract_address_hash, cumulative_gas_used, block_timestamp) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		tx.ChainID, tx.Hash, tx.BlockNumber, tx.BlockHash, tx.TransactionIndex, tx.FromAddress, tx.ToAddress, tx.Value, tx.Gas, tx.GasPrice, tx.GasUsed, tx.MaxFeePerGas, tx.MaxPriorityFeePerGas, tx.Input, tx.Nonce, tx.Type, tx.Status, tx.Error, tx.RevertReason, tx.CreatedContract, tx.CumulativeGasUsed, tx.BlockTimestamp)
	if err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
	return tx
}

func (db *DB) InsertAddress(t *testing.T, a Address) Address {
	t.Helper()
	_, err := db.Exec(`INSERT INTO addresses (chain_id, hash, fetched_coin_balance, fetched_coin_balance_block_number, contract_code, transactions_count, token_transfers_count, gas_used, nonce, verified) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		a.ChainID, a.Hash, a.FetchedCoinBalance, a.FetchedBalanceBlock, a.ContractCode, a.TransactionsCount, a.TokenTransfersCount, a.GasUsed, a.Nonce, a.Verified)
	if err != nil {
		t.Fatalf("insert address: %v", err)
	}
	return a
}

func (db *DB) InsertToken(t *testing.T, tk Token) Token {
	t.Helper()
	_, err := db.Exec(`INSERT INTO tokens (chain_id, contract_address_hash, name, symbol, total_supply, decimals, type, holder_count, icon_url) VALUES (?,?,?,?,?,?,?,?,?)`,
		tk.ChainID, tk.ContractAddress, tk.Name, tk.Symbol, tk.TotalSupply, tk.Decimals, tk.Type, tk.HolderCount, tk.IconURL)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	return tk
}

func (db *DB) InsertTokenTransfer(t *testing.T, tt TokenTransfer) TokenTransfer {
	t.Helper()
	_, err := db.Exec(`INSERT INTO token_transfers (chain_id, transaction_hash, log_index, block_number, block_hash, from_address_hash, to_address_hash, token_contract_address_hash, amount, token_id, token_type, block_timestamp) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		tt.ChainID, tt.TransactionHash, tt.LogIndex, tt.BlockNumber, tt.BlockHash, tt.FromAddress, tt.ToAddress, tt.TokenContractAddress, tt.Amount, tt.TokenID, tt.TokenType, tt.BlockTimestamp)
	if err != nil {
		t.Fatalf("insert token transfer: %v", err)
	}
	return tt
}

func (db *DB) InsertLog(t *testing.T, l Log) Log {
	t.Helper()
	_, err := db.Exec(`INSERT INTO logs (chain_id, block_number, block_hash, transaction_hash, transaction_index, "index", address_hash, data, first_topic, second_topic, third_topic, fourth_topic, block_timestamp) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.ChainID, l.BlockNumber, l.BlockHash, l.TransactionHash, l.TransactionIndex, l.Index, l.Address, l.Data, l.FirstTopic, l.SecondTopic, l.ThirdTopic, l.FourthTopic, l.BlockTimestamp)
	if err != nil {
		t.Fatalf("insert log: %v", err)
	}
	return l
}

func (db *DB) InsertInternalTx(t *testing.T, itx InternalTx) InternalTx {
	t.Helper()
	_, err := db.Exec(`INSERT INTO internal_transactions (chain_id, transaction_hash, block_number, block_hash, "index", trace_address, type, call_type, from_address_hash, to_address_hash, value, gas, gas_used, input, output, error, block_timestamp) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		itx.ChainID, itx.TransactionHash, itx.BlockNumber, itx.BlockHash, itx.Index, itx.TraceAddress, itx.Type, itx.CallType, itx.FromAddress, itx.ToAddress, itx.Value, itx.Gas, itx.GasUsed, itx.Input, itx.Output, itx.Error, itx.BlockTimestamp)
	if err != nil {
		t.Fatalf("insert internal tx: %v", err)
	}
	return itx
}

func (db *DB) InsertSmartContract(t *testing.T, sc SmartContract) SmartContract {
	t.Helper()
	_, err := db.Exec(`INSERT INTO smart_contracts (chain_id, address_hash, name, compiler_version, optimization, optimization_runs, contract_source_code, abi, evm_version, verified_via, is_vyper_contract, license_type) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		sc.ChainID, sc.AddressHash, sc.Name, sc.CompilerVersion, sc.Optimization, sc.OptRuns, sc.SourceCode, sc.ABI, sc.EVMVersion, sc.VerifiedVia, sc.IsVyper, sc.LicenseType)
	if err != nil {
		t.Fatalf("insert smart contract: %v", err)
	}
	return sc
}

// ---- Seed Helpers ----

// SeedChainData inserts a realistic set of chain data for testing.
// Returns the blocks, transactions, and addresses created.
func (db *DB) SeedChainData(t *testing.T, numBlocks int) ([]Block, []Transaction, []Address) {
	t.Helper()
	var blocks []Block
	var txs []Transaction
	var addrs []Address

	for i := 0; i < numBlocks; i++ {
		b := DefaultBlock()
		b.TransactionCount = 2
		b = db.InsertBlock(t, b)
		blocks = append(blocks, b)

		// 2 transactions per block
		for j := 0; j < 2; j++ {
			tx := DefaultTransaction(b)
			tx.TransactionIndex = j
			tx = db.InsertTransaction(t, tx)
			txs = append(txs, tx)

			// 1 log per transaction
			l := DefaultLog(tx)
			db.InsertLog(t, l)
		}
	}

	// Create some addresses
	for i := 0; i < 5; i++ {
		a := DefaultAddress()
		a = db.InsertAddress(t, a)
		addrs = append(addrs, a)
	}

	return blocks, txs, addrs
}

func padAddress(addr []byte) []byte {
	if len(addr) == 0 {
		return nil
	}
	padded := make([]byte, 32)
	copy(padded[12:], addr)
	return padded
}

// EnvironmentDBPath returns EXPLORER_TEST_DB if set (for integration with a real indexer DB).
func EnvironmentDBPath() string {
	return os.Getenv("EXPLORER_TEST_DB")
}
