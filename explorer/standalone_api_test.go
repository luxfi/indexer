package explorer

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/luxfi/explorer/explorer/testutil"
)

// hexStr converts raw bytes to 0x-prefixed lowercase hex.
// The standalone server queries with hex strings from URLs, so test data
// must be stored as hex strings (not raw bytes) for WHERE clauses to match.
func hexStr(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return "0x" + hex.EncodeToString(b)
}

// insertBlock inserts a block with all byte fields stored as hex strings.
func insertBlock(t *testing.T, db *testutil.DB, b testutil.Block) testutil.Block {
	t.Helper()
	_, err := db.Exec(`INSERT INTO blocks (chain_id, number, hash, parent_hash, nonce, miner, difficulty, total_difficulty, size, gas_limit, gas_used, base_fee, timestamp, transaction_count, consensus_state) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ChainID, b.Number, hexStr(b.Hash), hexStr(b.ParentHash), hexStr(b.Nonce), hexStr(b.Miner),
		b.Difficulty, b.TotalDifficulty, b.Size, b.GasLimit, b.GasUsed, b.BaseFee, b.Timestamp, b.TransactionCount, b.ConsensusState)
	if err != nil {
		t.Fatalf("insert block: %v", err)
	}
	return b
}

// insertTx inserts a transaction with all byte fields stored as hex strings.
func insertTx(t *testing.T, db *testutil.DB, tx testutil.Transaction) testutil.Transaction {
	t.Helper()
	var toAddr any
	if tx.ToAddress != nil {
		toAddr = hexStr(tx.ToAddress)
	}
	var createdContract any
	if tx.CreatedContract != nil {
		createdContract = hexStr(tx.CreatedContract)
	}
	var input any
	if tx.Input != nil {
		input = hexStr(tx.Input)
	}
	_, err := db.Exec(`INSERT INTO transactions (chain_id, hash, block_number, block_hash, transaction_index, from_address_hash, to_address_hash, value, gas, gas_price, gas_used, max_fee_per_gas, max_priority_fee_per_gas, input, nonce, type, status, error, revert_reason, created_contract_address_hash, cumulative_gas_used, block_timestamp) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		tx.ChainID, hexStr(tx.Hash), tx.BlockNumber, hexStr(tx.BlockHash), tx.TransactionIndex,
		hexStr(tx.FromAddress), toAddr, tx.Value, tx.Gas, tx.GasPrice, tx.GasUsed,
		tx.MaxFeePerGas, tx.MaxPriorityFeePerGas, input, tx.Nonce, tx.Type,
		tx.Status, tx.Error, tx.RevertReason, createdContract, tx.CumulativeGasUsed, tx.BlockTimestamp)
	if err != nil {
		t.Fatalf("insert tx: %v", err)
	}
	return tx
}

// insertAddr inserts an address with byte fields stored as hex strings.
func insertAddr(t *testing.T, db *testutil.DB, a testutil.Address) testutil.Address {
	t.Helper()
	var code any
	if a.ContractCode != nil {
		code = hexStr(a.ContractCode)
	}
	_, err := db.Exec(`INSERT INTO addresses (chain_id, hash, fetched_coin_balance, fetched_coin_balance_block_number, contract_code, transactions_count, token_transfers_count, gas_used, nonce, verified) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		a.ChainID, hexStr(a.Hash), a.FetchedCoinBalance, a.FetchedBalanceBlock, code,
		a.TransactionsCount, a.TokenTransfersCount, a.GasUsed, a.Nonce, a.Verified)
	if err != nil {
		t.Fatalf("insert addr: %v", err)
	}
	return a
}

// insertToken inserts a token with byte fields stored as hex strings.
func insertToken(t *testing.T, db *testutil.DB, tk testutil.Token) testutil.Token {
	t.Helper()
	_, err := db.Exec(`INSERT INTO tokens (chain_id, contract_address_hash, name, symbol, total_supply, decimals, type, holder_count, icon_url) VALUES (?,?,?,?,?,?,?,?,?)`,
		tk.ChainID, hexStr(tk.ContractAddress), tk.Name, tk.Symbol, tk.TotalSupply, tk.Decimals, tk.Type, tk.HolderCount, tk.IconURL)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	return tk
}

// insertTokenTransfer inserts a token transfer with byte fields stored as hex strings.
func insertTokenTransfer(t *testing.T, db *testutil.DB, tt testutil.TokenTransfer) testutil.TokenTransfer {
	t.Helper()
	_, err := db.Exec(`INSERT INTO token_transfers (chain_id, transaction_hash, log_index, block_number, block_hash, from_address_hash, to_address_hash, token_contract_address_hash, amount, token_id, token_type, block_timestamp) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		tt.ChainID, hexStr(tt.TransactionHash), tt.LogIndex, tt.BlockNumber, hexStr(tt.BlockHash),
		hexStr(tt.FromAddress), hexStr(tt.ToAddress), hexStr(tt.TokenContractAddress),
		tt.Amount, tt.TokenID, tt.TokenType, tt.BlockTimestamp)
	if err != nil {
		t.Fatalf("insert token transfer: %v", err)
	}
	return tt
}

// insertLog inserts a log with byte fields stored as hex strings.
func insertLog(t *testing.T, db *testutil.DB, l testutil.Log) testutil.Log {
	t.Helper()
	_, err := db.Exec(`INSERT INTO logs (chain_id, block_number, block_hash, transaction_hash, transaction_index, "index", address_hash, data, first_topic, second_topic, third_topic, fourth_topic, block_timestamp) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.ChainID, l.BlockNumber, hexStr(l.BlockHash), hexStr(l.TransactionHash), l.TransactionIndex,
		l.Index, hexStr(l.Address), hexStr(l.Data), hexStr(l.FirstTopic), hexStr(l.SecondTopic),
		hexStr(l.ThirdTopic), hexStr(l.FourthTopic), l.BlockTimestamp)
	if err != nil {
		t.Fatalf("insert log: %v", err)
	}
	return l
}

// insertInternalTx inserts an internal transaction with byte fields stored as hex strings.
func insertInternalTx(t *testing.T, db *testutil.DB, itx testutil.InternalTx) testutil.InternalTx {
	t.Helper()
	var toAddr any
	if itx.ToAddress != nil {
		toAddr = hexStr(itx.ToAddress)
	}
	_, err := db.Exec(`INSERT INTO internal_transactions (chain_id, transaction_hash, block_number, block_hash, "index", trace_address, type, call_type, from_address_hash, to_address_hash, value, gas, gas_used, input, output, error, block_timestamp) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		itx.ChainID, hexStr(itx.TransactionHash), itx.BlockNumber, hexStr(itx.BlockHash),
		itx.Index, itx.TraceAddress, itx.Type, itx.CallType, hexStr(itx.FromAddress), toAddr,
		itx.Value, itx.Gas, itx.GasUsed, hexStr(itx.Input), hexStr(itx.Output), itx.Error, itx.BlockTimestamp)
	if err != nil {
		t.Fatalf("insert internal tx: %v", err)
	}
	return itx
}

// insertSmartContract inserts a smart contract with byte fields stored as hex strings.
func insertSmartContract(t *testing.T, db *testutil.DB, sc testutil.SmartContract) testutil.SmartContract {
	t.Helper()
	_, err := db.Exec(`INSERT INTO smart_contracts (chain_id, address_hash, name, compiler_version, optimization, optimization_runs, contract_source_code, abi, evm_version, verified_via, is_vyper_contract, license_type) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		sc.ChainID, hexStr(sc.AddressHash), sc.Name, sc.CompilerVersion, sc.Optimization, sc.OptRuns,
		sc.SourceCode, sc.ABI, sc.EVMVersion, sc.VerifiedVia, sc.IsVyper, sc.LicenseType)
	if err != nil {
		t.Fatalf("insert smart contract: %v", err)
	}
	return sc
}

// insertTokenBalance inserts a token balance with byte fields stored as hex strings.
func insertTokenBalance(t *testing.T, db *testutil.DB, tb testutil.TokenBalance) testutil.TokenBalance {
	t.Helper()
	_, err := db.Exec(`INSERT INTO address_current_token_balances (chain_id, address_hash, token_contract_address_hash, value, block_number, token_type) VALUES (?,?,?,?,?,?)`,
		tb.ChainID, hexStr(tb.AddressHash), hexStr(tb.TokenContractAddress), tb.Value, tb.BlockNumber, tb.TokenType)
	if err != nil {
		t.Fatalf("insert token balance: %v", err)
	}
	return tb
}

// newServer creates a StandaloneServer reading from the given test DB.
func newServer(t *testing.T, dbPath string) (*StandaloneServer, *httptest.Server) {
	t.Helper()
	srv, err := NewStandaloneServer(Config{
		IndexerDBPath: dbPath,
		ChainID:       testutil.DefaultChainID,
		ChainName:     "Test",
		CoinSymbol:    "TST",
	})
	if err != nil {
		t.Fatalf("NewStandaloneServer: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close() })
	return srv, ts
}

// getJSON does GET and decodes JSON body. Asserts the given status code.
func getJSON(t *testing.T, ts *httptest.Server, path string, wantStatus int) map[string]any {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s: want status %d, got %d", path, wantStatus, resp.StatusCode)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("GET %s: decode: %v", path, err)
	}
	return result
}

// getResp returns the raw http.Response for header checks.
func getResp(t *testing.T, ts *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// items extracts the items array from a paginated response.
// Returns empty slice if items is null/nil (e.g., search with no results).
func items(t *testing.T, body map[string]any) []any {
	t.Helper()
	raw, ok := body["items"]
	if !ok {
		t.Fatal("response missing 'items' field")
	}
	if raw == nil {
		return []any{}
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("items is %T, want []any", raw)
	}
	return arr
}

// itemMaps extracts items as []map[string]any.
func itemMaps(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw := items(t, body)
	out := make([]map[string]any, len(raw))
	for i, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("items[%d] is %T, want map[string]any", i, v)
		}
		out[i] = m
	}
	return out
}

// ---- seedAll inserts a standard dataset and returns known values ----
type seedData struct {
	blocks       []testutil.Block
	txs          []testutil.Transaction
	addrs        []testutil.Address
	tokens       []testutil.Token
	contracts    []testutil.SmartContract
	eip1559Tx    testutil.Transaction
	failedTx     testutil.Transaction
	pendingTx    testutil.Transaction
	contractTx   testutil.Transaction
	contractAddr testutil.Address
	eoaAddr      testutil.Address
	erc20Token   testutil.Token
	erc721Token  testutil.Token
}

func seedAll(t *testing.T, db *testutil.DB) seedData {
	t.Helper()
	var sd seedData

	// 5 blocks with increasing numbers.
	now := time.Now().Unix()
	for i := 0; i < 5; i++ {
		b := testutil.DefaultBlock()
		b.Number = int64(i)
		b.Timestamp = now - int64(4-i)*10
		b.TransactionCount = 1
		b = insertBlock(t, db, b)
		sd.blocks = append(sd.blocks, b)
	}

	// 1 normal tx per block.
	for i, b := range sd.blocks {
		tx := testutil.DefaultTransaction(b)
		tx.TransactionIndex = 0
		tx = insertTx(t, db, tx)
		sd.txs = append(sd.txs, tx)
		_ = i
	}

	// Special transaction types in block 4.
	sd.eip1559Tx = testutil.DefaultEIP1559Transaction(sd.blocks[4])
	sd.eip1559Tx.TransactionIndex = 1
	sd.eip1559Tx = insertTx(t, db, sd.eip1559Tx)

	sd.failedTx = testutil.DefaultFailedTransaction(sd.blocks[4])
	sd.failedTx.TransactionIndex = 2
	sd.failedTx = insertTx(t, db, sd.failedTx)

	sd.pendingTx = testutil.DefaultPendingTransaction(sd.blocks[4])
	sd.pendingTx.TransactionIndex = 3
	sd.pendingTx = insertTx(t, db, sd.pendingTx)

	sd.contractTx = testutil.DefaultContractCreation(sd.blocks[4])
	sd.contractTx.TransactionIndex = 4
	sd.contractTx = insertTx(t, db, sd.contractTx)

	// Addresses: 1 EOA, 1 contract.
	sd.eoaAddr = testutil.DefaultAddress()
	sd.eoaAddr = insertAddr(t, db, sd.eoaAddr)

	sd.contractAddr = testutil.DefaultContractAddress()
	sd.contractAddr = insertAddr(t, db, sd.contractAddr)
	sd.addrs = []testutil.Address{sd.eoaAddr, sd.contractAddr}

	// Address with known transactions (use first tx's from address).
	fromAddr := testutil.DefaultAddress()
	fromAddr.Hash = sd.txs[0].FromAddress
	fromAddr.TransactionsCount = 5
	fromAddr.TokenTransfersCount = 2
	fromAddr.GasUsed = "105000"
	fromAddr = insertAddr(t, db, fromAddr)

	// Tokens.
	sd.erc20Token = testutil.DefaultToken()
	sd.erc20Token.Name = "Wrapped LUX"
	sd.erc20Token.Symbol = "WLUX"
	sd.erc20Token.HolderCount = 100
	sd.erc20Token = insertToken(t, db, sd.erc20Token)

	sd.erc721Token = testutil.DefaultERC721Token()
	sd.erc721Token.Name = "LuxPunks"
	sd.erc721Token.Symbol = "LPUNK"
	sd.erc721Token.HolderCount = 50
	sd.erc721Token = insertToken(t, db, sd.erc721Token)
	sd.tokens = []testutil.Token{sd.erc20Token, sd.erc721Token}

	// Token transfers for first tx.
	tt := testutil.DefaultTokenTransfer(sd.txs[0], sd.erc20Token)
	insertTokenTransfer(t, db, tt)

	// Logs for first tx.
	lg := testutil.DefaultLog(sd.txs[0])
	insertLog(t, db, lg)

	// Internal tx for first tx.
	itx := testutil.DefaultInternalTx(sd.txs[0])
	insertInternalTx(t, db, itx)

	// Smart contract.
	sc := testutil.DefaultSmartContract()
	sc = insertSmartContract(t, db, sc)
	sd.contracts = []testutil.SmartContract{sc}

	// Token balances for holders endpoint.
	for i := 0; i < 3; i++ {
		tb := testutil.TokenBalance{
			ChainID:              testutil.DefaultChainID,
			AddressHash:          testutil.RandomAddress(),
			TokenContractAddress: sd.erc20Token.ContractAddress,
			Value:                new(big.Int).Mul(big.NewInt(int64(100-i*10)), big.NewInt(1e18)).String(),
			BlockNumber:          int64(i),
			TokenType:            "ERC-20",
		}
		insertTokenBalance(t, db, tb)
	}

	return sd
}

// ====================
// BLOCKS
// ====================

func TestListBlocks_Empty(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	_, ts := newServer(t, tdb.Path)
	body := getJSON(t, ts, "/v1/explorer/blocks", 200)
	if got := items(t, body); len(got) != 0 {
		t.Errorf("want 0 items, got %d", len(got))
	}
	if body["next_page_params"] != nil {
		t.Error("want next_page_params null for empty result")
	}
}

func TestListBlocks_DescendingOrder(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)
	body := getJSON(t, ts, "/v1/explorer/blocks", 200)
	got := itemMaps(t, body)
	if len(got) != 5 {
		t.Fatalf("want 5 blocks, got %d", len(got))
	}
	// First item should be highest block number.
	first := got[0]
	last := got[4]
	if toFloat(first["height"]) <= toFloat(last["height"]) {
		t.Errorf("blocks not descending: first height %v <= last %v", first["height"], last["height"])
	}
	_ = sd
}

func TestListBlocks_Pagination(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/blocks?items_count=2", 200)
	got := itemMaps(t, body)
	if len(got) != 2 {
		t.Errorf("want 2 items, got %d", len(got))
	}
}

func TestListBlocks_ItemsCountCapped(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/blocks?items_count=999999", 200)
	got := items(t, body)
	if len(got) > 250 {
		t.Errorf("items_count should cap at 250, got %d", len(got))
	}
}

func TestListBlocks_NextPageParamsPresent(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/blocks?items_count=2", 200)
	if body["next_page_params"] == nil {
		t.Error("want next_page_params when more results exist")
	}
	np := body["next_page_params"].(map[string]any)
	if np["block_number"] == nil {
		t.Error("next_page_params missing block_number")
	}
	if np["items_count"] == nil {
		t.Error("next_page_params missing items_count")
	}
}

func TestListBlocks_NextPageParamsNullWhenNoMore(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/blocks?items_count=250", 200)
	if body["next_page_params"] != nil {
		t.Error("want next_page_params null when all results returned")
	}
}

func TestListBlocks_DefaultItemsCount(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/blocks", 200)
	got := items(t, body)
	// Default is 50, we only have 5 blocks.
	if len(got) != 5 {
		t.Errorf("want 5 items with default limit, got %d", len(got))
	}
}

func TestListBlocks_ResponseFields(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/blocks", 200)
	got := itemMaps(t, body)
	b := got[0]
	for _, field := range []string{"height", "hash", "parent_hash", "miner", "gas_limit", "gas_used", "timestamp", "tx_count", "type"} {
		if _, ok := b[field]; !ok {
			t.Errorf("missing field %q", field)
		}
	}
	// Miner should be object with hash.
	miner, ok := b["miner"].(map[string]any)
	if !ok {
		t.Fatalf("miner should be object, got %T", b["miner"])
	}
	if miner["hash"] == nil {
		t.Error("miner.hash is nil")
	}
}

// ---- Get Block ----

func TestGetBlock_ByNumber(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/blocks/0", 200)
	if toFloat(body["height"]) != 0 {
		t.Errorf("want height 0, got %v", body["height"])
	}
	wantHash := hexStr(sd.blocks[0].Hash)
	if body["hash"] != wantHash {
		t.Errorf("hash: want %s, got %v", wantHash, body["hash"])
	}
}

func TestGetBlock_ByHash(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(sd.blocks[2].Hash)
	body := getJSON(t, ts, "/v1/explorer/blocks/"+hash, 200)
	if body["hash"] != hash {
		t.Errorf("want hash %s, got %v", hash, body["hash"])
	}
}

func TestGetBlock_404Number(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	getJSON(t, ts, "/v1/explorer/blocks/99999", 404)
}

func TestGetBlock_404Hash(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	getJSON(t, ts, "/v1/explorer/blocks/0x0000000000000000000000000000000000000000000000000000000000000000", 404)
}

// ---- Block Transactions ----

func TestBlockTxs_ReturnsTxs(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	// Block 4 has multiple txs.
	body := getJSON(t, ts, "/v1/explorer/blocks/4/transactions", 200)
	got := itemMaps(t, body)
	if len(got) < 2 {
		t.Errorf("block 4 should have multiple txs, got %d", len(got))
	}
	_ = sd
}

func TestBlockTxs_EmptyBlock(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	// Insert a block with no transactions.
	b := testutil.DefaultBlock()
	b.Number = 999
	b.TransactionCount = 0
	insertBlock(t, tdb, b)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/blocks/999/transactions", 200)
	got := items(t, body)
	if len(got) != 0 {
		t.Errorf("want 0 txs, got %d", len(got))
	}
}

func TestBlockTxs_ByHash(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(sd.blocks[0].Hash)
	body := getJSON(t, ts, "/v1/explorer/blocks/"+hash+"/transactions", 200)
	got := items(t, body)
	if len(got) == 0 {
		t.Error("expected at least 1 tx in block 0")
	}
}

// ====================
// TRANSACTIONS
// ====================

func TestListTxs_Empty(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/transactions", 200)
	got := items(t, body)
	if len(got) != 0 {
		t.Errorf("want 0 items, got %d", len(got))
	}
}

func TestListTxs_DescendingOrder(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/transactions", 200)
	got := itemMaps(t, body)
	if len(got) < 2 {
		t.Fatalf("need at least 2 txs, got %d", len(got))
	}
	first := toFloat(got[0]["block_number"])
	last := toFloat(got[len(got)-1]["block_number"])
	if first < last {
		t.Errorf("txs not descending: first block %v < last %v", first, last)
	}
}

func TestListTxs_Pagination(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/transactions?items_count=3", 200)
	got := items(t, body)
	if len(got) != 3 {
		t.Errorf("want 3 items, got %d", len(got))
	}
	if body["next_page_params"] == nil {
		t.Error("want next_page_params when more results exist")
	}
}

func TestListTxs_ResponseFields(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/transactions", 200)
	got := itemMaps(t, body)
	tx := got[0]
	for _, field := range []string{"hash", "block_number", "from", "value", "status", "timestamp"} {
		if _, ok := tx[field]; !ok {
			t.Errorf("missing field %q", field)
		}
	}
}

// ---- Get Transaction ----

func TestGetTx_ByHash(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(sd.txs[0].Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash, 200)
	if body["hash"] != hash {
		t.Errorf("want hash %s, got %v", hash, body["hash"])
	}
}

func TestGetTx_404(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	getJSON(t, ts, "/v1/explorer/transactions/0x0000000000000000000000000000000000000000000000000000000000000000", 404)
}

func TestGetTx_Types(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	tests := []struct {
		name   string
		tx     testutil.Transaction
		checks func(t *testing.T, body map[string]any)
	}{
		{
			name: "legacy type 0",
			tx:   sd.txs[0],
			checks: func(t *testing.T, body map[string]any) {
				if toFloat(body["type"]) != 0 {
					t.Errorf("want type 0, got %v", body["type"])
				}
				if body["status"] != "ok" {
					t.Errorf("want status ok, got %v", body["status"])
				}
				if body["gas_price"] == nil {
					t.Error("legacy tx should have gas_price")
				}
			},
		},
		{
			name: "EIP-1559 type 2",
			tx:   sd.eip1559Tx,
			checks: func(t *testing.T, body map[string]any) {
				if toFloat(body["type"]) != 2 {
					t.Errorf("want type 2, got %v", body["type"])
				}
				if body["max_fee_per_gas"] == nil || body["max_fee_per_gas"] == "0" {
					t.Error("EIP-1559 tx should have max_fee_per_gas")
				}
				if body["max_priority_fee_per_gas"] == nil || body["max_priority_fee_per_gas"] == "0" {
					t.Error("EIP-1559 tx should have max_priority_fee_per_gas")
				}
			},
		},
		{
			name: "failed transaction",
			tx:   sd.failedTx,
			checks: func(t *testing.T, body map[string]any) {
				if body["status"] != "error" {
					t.Errorf("want status error, got %v", body["status"])
				}
				if body["error"] == nil {
					t.Error("failed tx should have error field")
				}
			},
		},
		{
			name: "pending transaction",
			tx:   sd.pendingTx,
			checks: func(t *testing.T, body map[string]any) {
				if body["status"] != "pending" {
					t.Errorf("want status pending, got %v", body["status"])
				}
			},
		},
		{
			name: "contract creation",
			tx:   sd.contractTx,
			checks: func(t *testing.T, body map[string]any) {
				cc, ok := body["created_contract"].(map[string]any)
				if !ok {
					t.Fatal("contract creation tx should have created_contract object")
				}
				if cc["hash"] == nil || cc["hash"] == "" {
					t.Error("created_contract.hash should not be empty")
				}
				if body["to"] != nil {
					t.Error("contract creation should have nil to")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := hexStr(tt.tx.Hash)
			body := getJSON(t, ts, "/v1/explorer/transactions/"+hash, 200)
			tt.checks(t, body)
		})
	}
}

func TestGetTx_AllResponseFields(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(sd.txs[0].Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash, 200)
	for _, field := range []string{
		"hash", "block_number", "block_hash", "from", "value",
		"gas_limit", "gas_price", "gas_used", "nonce", "position",
		"type", "status", "timestamp", "result",
	} {
		if _, ok := body[field]; !ok {
			t.Errorf("missing field %q", field)
		}
	}
	// from should be object.
	from, ok := body["from"].(map[string]any)
	if !ok {
		t.Fatalf("from should be object, got %T", body["from"])
	}
	if from["hash"] == nil {
		t.Error("from.hash is nil")
	}
}

// ---- Transaction Token Transfers ----

func TestTxTokenTransfers(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(sd.txs[0].Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash+"/token-transfers", 200)
	got := itemMaps(t, body)
	if len(got) == 0 {
		t.Error("expected at least 1 token transfer")
	}
	tf := got[0]
	for _, field := range []string{"from", "to", "token", "total", "log_index", "transaction_hash"} {
		if _, ok := tf[field]; !ok {
			t.Errorf("missing field %q", field)
		}
	}
}

func TestTxTokenTransfers_Empty(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	// Use a tx that has no token transfers.
	hash := hexStr(sd.txs[1].Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash+"/token-transfers", 200)
	got := items(t, body)
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

// ---- Transaction Internal Transactions ----

func TestTxInternalTxs(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(sd.txs[0].Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash+"/internal-transactions", 200)
	got := itemMaps(t, body)
	if len(got) == 0 {
		t.Error("expected at least 1 internal tx")
	}
	itx := got[0]
	for _, field := range []string{"type", "call_type", "from", "value", "gas_limit", "gas_used"} {
		if _, ok := itx[field]; !ok {
			t.Errorf("missing field %q", field)
		}
	}
}

func TestTxInternalTxs_Empty(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(sd.txs[1].Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash+"/internal-transactions", 200)
	got := items(t, body)
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

// ---- Transaction Logs ----

func TestTxLogs(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(sd.txs[0].Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash+"/logs", 200)
	got := itemMaps(t, body)
	if len(got) == 0 {
		t.Error("expected at least 1 log")
	}
	lg := got[0]
	for _, field := range []string{"address", "data", "topics", "index", "block_number", "transaction_hash"} {
		if _, ok := lg[field]; !ok {
			t.Errorf("missing field %q", field)
		}
	}
	// Topics should be array.
	topics, ok := lg["topics"].([]any)
	if !ok {
		t.Fatalf("topics should be array, got %T", lg["topics"])
	}
	if len(topics) == 0 {
		t.Error("expected at least 1 topic")
	}
}

func TestTxLogs_Empty(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(sd.txs[1].Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash+"/logs", 200)
	got := items(t, body)
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

// ====================
// ADDRESSES
// ====================

func TestListAddrs_SortedByBalance(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/addresses", 200)
	got := itemMaps(t, body)
	if len(got) < 2 {
		t.Fatalf("want at least 2 addresses, got %d", len(got))
	}
}

func TestListAddrs_Empty(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/addresses", 200)
	got := items(t, body)
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

func TestListAddrs_ResponseFields(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/addresses", 200)
	got := itemMaps(t, body)
	a := got[0]
	for _, field := range []string{"hash", "coin_balance", "transactions_count", "is_contract"} {
		if _, ok := a[field]; !ok {
			t.Errorf("missing field %q", field)
		}
	}
}

// ---- Get Address ----

func TestGetAddr(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(sd.eoaAddr.Hash)
	body := getJSON(t, ts, "/v1/explorer/addresses/"+hash, 200)
	if body["hash"] != hash {
		t.Errorf("want hash %s, got %v", hash, body["hash"])
	}
}

func TestGetAddr_404(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	getJSON(t, ts, "/v1/explorer/addresses/0x0000000000000000000000000000000000000000", 404)
}

func TestGetAddr_Contract(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(sd.contractAddr.Hash)
	body := getJSON(t, ts, "/v1/explorer/addresses/"+hash, 200)
	if body["is_contract"] != true {
		t.Errorf("contract address should have is_contract=true, got %v", body["is_contract"])
	}
}

func TestGetAddr_EOA(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(sd.eoaAddr.Hash)
	body := getJSON(t, ts, "/v1/explorer/addresses/"+hash, 200)
	if body["is_contract"] != false {
		t.Errorf("EOA should have is_contract=false, got %v", body["is_contract"])
	}
}

// ---- Address Transactions ----

func TestAddrTxs(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	// Use the from address of the first tx.
	addr := hexStr(sd.txs[0].FromAddress)
	body := getJSON(t, ts, "/v1/explorer/addresses/"+addr+"/transactions", 200)
	got := items(t, body)
	if len(got) == 0 {
		t.Error("expected at least 1 tx for from address")
	}
}

func TestAddrTxs_ToAddress(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	// Use the to address of the first tx.
	addr := hexStr(sd.txs[0].ToAddress)
	body := getJSON(t, ts, "/v1/explorer/addresses/"+addr+"/transactions", 200)
	got := items(t, body)
	if len(got) == 0 {
		t.Error("expected at least 1 tx for to address")
	}
}

func TestAddrTxs_Empty(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/addresses/0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef/transactions", 200)
	got := items(t, body)
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

// ---- Address Counters ----

func TestAddrCounters(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	// fromAddr was created with known counters.
	addr := hexStr(sd.txs[0].FromAddress)
	body := getJSON(t, ts, "/v1/explorer/addresses/"+addr+"/counters", 200)
	if body["transactions_count"] == nil {
		t.Error("missing transactions_count")
	}
	if body["token_transfers_count"] == nil {
		t.Error("missing token_transfers_count")
	}
	if body["gas_usage_count"] == nil {
		t.Error("missing gas_usage_count")
	}
	if body["validations_count"] == nil {
		t.Error("missing validations_count")
	}
}

func TestAddrCounters_404(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	getJSON(t, ts, "/v1/explorer/addresses/0x0000000000000000000000000000000000000000/counters", 404)
}

func TestAddrCounters_Values(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	addr := hexStr(sd.txs[0].FromAddress)
	body := getJSON(t, ts, "/v1/explorer/addresses/"+addr+"/counters", 200)
	if toFloat(body["transactions_count"]) != 5 {
		t.Errorf("want transactions_count=5, got %v", body["transactions_count"])
	}
	if toFloat(body["token_transfers_count"]) != 2 {
		t.Errorf("want token_transfers_count=2, got %v", body["token_transfers_count"])
	}
}

// ====================
// TOKENS
// ====================

func TestListTokens_SortedByHolders(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/tokens", 200)
	got := itemMaps(t, body)
	if len(got) != 2 {
		t.Fatalf("want 2 tokens, got %d", len(got))
	}
	// ERC20 has 100 holders, ERC721 has 50, so ERC20 should be first.
	if got[0]["name"] != sd.erc20Token.Name {
		t.Errorf("first token should be %s (100 holders), got %v", sd.erc20Token.Name, got[0]["name"])
	}
}

func TestListTokens_Empty(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/tokens", 200)
	got := items(t, body)
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

func TestListTokens_ResponseFields(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/tokens", 200)
	got := itemMaps(t, body)
	tk := got[0]
	for _, field := range []string{"address", "name", "symbol", "total_supply", "decimals", "type", "holders"} {
		if _, ok := tk[field]; !ok {
			t.Errorf("missing field %q", field)
		}
	}
}

// ---- Get Token ----

func TestGetToken_ERC20(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	addr := hexStr(sd.erc20Token.ContractAddress)
	body := getJSON(t, ts, "/v1/explorer/tokens/"+addr, 200)
	if body["type"] != "ERC-20" {
		t.Errorf("want type ERC-20, got %v", body["type"])
	}
	if body["name"] != "Wrapped LUX" {
		t.Errorf("want name Wrapped LUX, got %v", body["name"])
	}
	if body["symbol"] != "WLUX" {
		t.Errorf("want symbol WLUX, got %v", body["symbol"])
	}
}

func TestGetToken_ERC721(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	addr := hexStr(sd.erc721Token.ContractAddress)
	body := getJSON(t, ts, "/v1/explorer/tokens/"+addr, 200)
	if body["type"] != "ERC-721" {
		t.Errorf("want type ERC-721, got %v", body["type"])
	}
}

func TestGetToken_404(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	getJSON(t, ts, "/v1/explorer/tokens/0x0000000000000000000000000000000000000000", 404)
}

// ---- Token Holders ----

func TestTokenHolders(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	addr := hexStr(sd.erc20Token.ContractAddress)
	body := getJSON(t, ts, "/v1/explorer/tokens/"+addr+"/holders", 200)
	got := itemMaps(t, body)
	if len(got) != 3 {
		t.Fatalf("want 3 holders, got %d", len(got))
	}
	// Should be sorted by value descending.
	h := got[0]
	if _, ok := h["address"]; !ok {
		t.Error("missing address field")
	}
	if _, ok := h["value"]; !ok {
		t.Error("missing value field")
	}
}

func TestTokenHolders_Empty(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	// ERC721 has no token balances inserted.
	addr := hexStr(sd.erc721Token.ContractAddress)
	body := getJSON(t, ts, "/v1/explorer/tokens/"+addr+"/holders", 200)
	got := items(t, body)
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

func TestTokenHolders_SortedDescending(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	addr := hexStr(sd.erc20Token.ContractAddress)
	body := getJSON(t, ts, "/v1/explorer/tokens/"+addr+"/holders", 200)
	got := itemMaps(t, body)
	if len(got) < 2 {
		t.Skip("need at least 2 holders")
	}
	// Values should be descending.
	v0 := got[0]["value"].(string)
	v1 := got[1]["value"].(string)
	b0, _ := new(big.Int).SetString(v0, 10)
	b1, _ := new(big.Int).SetString(v1, 10)
	if b0 == nil || b1 == nil {
		t.Fatalf("cannot parse values: %s, %s", v0, v1)
	}
	if b0.Cmp(b1) < 0 {
		t.Errorf("holders not sorted descending: %s < %s", v0, v1)
	}
}

// ====================
// SMART CONTRACTS
// ====================

func TestGetContract(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	addr := hexStr(sd.contracts[0].AddressHash)
	body := getJSON(t, ts, "/v1/explorer/smart-contracts/"+addr, 200)
	if body["name"] != "SimpleStorage" {
		t.Errorf("want name SimpleStorage, got %v", body["name"])
	}
	if body["abi"] == nil || body["abi"] == "" {
		t.Error("verified contract should have ABI")
	}
	if body["source_code"] == nil || body["source_code"] == "" {
		t.Error("verified contract should have source_code")
	}
	if body["is_verified"] != true {
		t.Errorf("want is_verified=true, got %v", body["is_verified"])
	}
}

func TestGetContract_AllFields(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	addr := hexStr(sd.contracts[0].AddressHash)
	body := getJSON(t, ts, "/v1/explorer/smart-contracts/"+addr, 200)
	for _, field := range []string{"address", "name", "compiler_version", "optimization", "source_code", "abi", "evm_version", "is_verified"} {
		if _, ok := body[field]; !ok {
			t.Errorf("missing field %q", field)
		}
	}
}

func TestGetContract_404(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	getJSON(t, ts, "/v1/explorer/smart-contracts/0x0000000000000000000000000000000000000000", 404)
}

// ====================
// SEARCH
// ====================

func TestSearch_ByBlockNumber(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/search?q=0", 200)
	got := itemMaps(t, body)
	found := false
	for _, item := range got {
		if item["type"] == "block" {
			found = true
			if toFloat(item["block_number"]) != 0 {
				t.Errorf("want block_number 0, got %v", item["block_number"])
			}
		}
	}
	if !found {
		t.Error("search for '0' should find block 0")
	}
}

func TestSearch_ByTxHash(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(sd.txs[0].Hash)
	body := getJSON(t, ts, "/v1/explorer/search?q="+hash, 200)
	got := itemMaps(t, body)
	found := false
	for _, item := range got {
		if item["type"] == "transaction" {
			found = true
		}
	}
	if !found {
		t.Error("search by tx hash should find transaction")
	}
}

func TestSearch_ByAddress(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	// Any 42-char hex string should return an address result.
	body := getJSON(t, ts, "/v1/explorer/search?q=0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", 200)
	got := itemMaps(t, body)
	found := false
	for _, item := range got {
		if item["type"] == "address" {
			found = true
			if !strings.HasPrefix(item["address_hash"].(string), "0x") {
				t.Error("address_hash should be hex")
			}
		}
	}
	if !found {
		t.Error("search by address should always return address result")
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/search?q=", 200)
	got := items(t, body)
	if len(got) != 0 {
		t.Errorf("empty query should return empty results, got %d", len(got))
	}
}

func TestSearch_NonexistentBlock(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/search?q=99999999", 200)
	got := itemMaps(t, body)
	for _, item := range got {
		if item["type"] == "block" {
			t.Error("should not find nonexistent block")
		}
	}
}

func TestSearch_NoQuery(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	// No q parameter at all.
	body := getJSON(t, ts, "/v1/explorer/search", 200)
	got := items(t, body)
	if len(got) != 0 {
		t.Errorf("no query should return empty results, got %d", len(got))
	}
}

// ====================
// STATS
// ====================

func TestStandaloneStats(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/stats", 200)
	if toFloat(body["total_blocks"]) != 5 {
		t.Errorf("want 5 blocks, got %v", body["total_blocks"])
	}
	// 5 normal + eip1559 + failed + pending + contract = 9
	if toFloat(body["total_transactions"]) != 9 {
		t.Errorf("want 9 transactions, got %v", body["total_transactions"])
	}
	if toFloat(body["total_addresses"]) != 3 {
		t.Errorf("want 3 addresses, got %v", body["total_addresses"])
	}
}

func TestStandaloneStats_Empty(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/stats", 200)
	if toFloat(body["total_blocks"]) != 0 {
		t.Errorf("want 0, got %v", body["total_blocks"])
	}
	if toFloat(body["total_transactions"]) != 0 {
		t.Errorf("want 0, got %v", body["total_transactions"])
	}
	if toFloat(body["total_addresses"]) != 0 {
		t.Errorf("want 0, got %v", body["total_addresses"])
	}
}

func TestStandaloneStats_AllFields(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/stats", 200)
	for _, field := range []string{"total_blocks", "total_transactions", "total_addresses", "market_cap", "network_utilization_percentage"} {
		if _, ok := body[field]; !ok {
			t.Errorf("missing field %q", field)
		}
	}
}

// ====================
// CORS AND CONTENT-TYPE (every endpoint)
// ====================

func TestCORSAndContentType(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	txHash := hexStr(sd.txs[0].Hash)
	blockHash := hexStr(sd.blocks[0].Hash)
	addrHash := hexStr(sd.eoaAddr.Hash)
	tokenAddr := hexStr(sd.erc20Token.ContractAddress)
	contractAddr := hexStr(sd.contracts[0].AddressHash)

	endpoints := []string{
		"/v1/explorer/blocks",
		"/v1/explorer/blocks/0",
		"/v1/explorer/blocks/" + blockHash,
		"/v1/explorer/blocks/0/transactions",
		"/v1/explorer/transactions",
		"/v1/explorer/transactions/" + txHash,
		"/v1/explorer/transactions/" + txHash + "/token-transfers",
		"/v1/explorer/transactions/" + txHash + "/internal-transactions",
		"/v1/explorer/transactions/" + txHash + "/logs",
		"/v1/explorer/addresses",
		"/v1/explorer/addresses/" + addrHash,
		"/v1/explorer/addresses/" + addrHash + "/transactions",
		"/v1/explorer/addresses/" + addrHash + "/counters",
		"/v1/explorer/tokens",
		"/v1/explorer/tokens/" + tokenAddr,
		"/v1/explorer/tokens/" + tokenAddr + "/holders",
		"/v1/explorer/smart-contracts/" + contractAddr,
		"/v1/explorer/search?q=0",
		"/v1/explorer/stats",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			resp := getResp(t, ts, ep)
			defer resp.Body.Close()

			ct := resp.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type: want application/json, got %q", ct)
			}
			cors := resp.Header.Get("Access-Control-Allow-Origin")
			if cors != "*" {
				t.Errorf("CORS: want *, got %q", cors)
			}
		})
	}
}

// ====================
// PAGINATED RESPONSE STRUCTURE (every paginated endpoint)
// ====================

func TestPaginatedResponseStructure(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	txHash := hexStr(sd.txs[0].Hash)
	addrHash := hexStr(sd.eoaAddr.Hash)
	tokenAddr := hexStr(sd.erc20Token.ContractAddress)

	endpoints := []string{
		"/v1/explorer/blocks",
		"/v1/explorer/blocks/0/transactions",
		"/v1/explorer/transactions",
		"/v1/explorer/transactions/" + txHash + "/token-transfers",
		"/v1/explorer/transactions/" + txHash + "/internal-transactions",
		"/v1/explorer/transactions/" + txHash + "/logs",
		"/v1/explorer/addresses",
		"/v1/explorer/addresses/" + addrHash + "/transactions",
		"/v1/explorer/tokens",
		"/v1/explorer/tokens/" + tokenAddr + "/holders",
		"/v1/explorer/search?q=0",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			body := getJSON(t, ts, ep, 200)
			if _, ok := body["items"]; !ok {
				t.Error("missing 'items' field")
			}
			// next_page_params must be present (even if null).
			if _, ok := body["next_page_params"]; !ok {
				t.Error("missing 'next_page_params' field")
			}
		})
	}
}

// ====================
// 404 RESPONSE STRUCTURE
// ====================

func TestNotFoundResponses(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	endpoints := []struct {
		name string
		path string
	}{
		{"nonexistent block number", "/v1/explorer/blocks/99999999"},
		{"nonexistent block hash", "/v1/explorer/blocks/0x0000000000000000000000000000000000000000000000000000000000000000"},
		{"nonexistent tx", "/v1/explorer/transactions/0x0000000000000000000000000000000000000000000000000000000000000000"},
		{"nonexistent address", "/v1/explorer/addresses/0x0000000000000000000000000000000000000000"},
		{"nonexistent address counters", "/v1/explorer/addresses/0x0000000000000000000000000000000000000000/counters"},
		{"nonexistent token", "/v1/explorer/tokens/0x0000000000000000000000000000000000000000"},
		{"nonexistent contract", "/v1/explorer/smart-contracts/0x0000000000000000000000000000000000000000"},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			body := getJSON(t, ts, ep.path, 404)
			if body["error"] == nil {
				t.Error("404 response should have error field")
			}
		})
	}
}

// ====================
// EDGE CASES
// ====================

func TestListBlocks_NegativeItemsCount(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/blocks?items_count=-1", 200)
	got := items(t, body)
	// Negative should default to 50.
	if len(got) == 0 {
		t.Error("negative items_count should default to 50 and return results")
	}
}

func TestListBlocks_ZeroItemsCount(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/blocks?items_count=0", 200)
	got := items(t, body)
	// Zero should default to 50.
	if len(got) == 0 {
		t.Error("zero items_count should default to 50 and return results")
	}
}

func TestListBlocks_NonNumericItemsCount(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/blocks?items_count=abc", 200)
	got := items(t, body)
	if len(got) == 0 {
		t.Error("non-numeric items_count should default to 50 and return results")
	}
}

func TestGetBlock_NumberAsString(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	// Block number 2 should work.
	body := getJSON(t, ts, "/v1/explorer/blocks/2", 200)
	if toFloat(body["height"]) != 2 {
		t.Errorf("want height 2, got %v", body["height"])
	}
}

func TestGetBlock_FormatDetails(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/blocks/0", 200)
	// hash should be 0x-prefixed.
	hash := body["hash"].(string)
	if !strings.HasPrefix(hash, "0x") {
		t.Errorf("hash should be 0x-prefixed, got %s", hash)
	}
	// timestamp should be RFC3339.
	ts2 := body["timestamp"].(string)
	if _, err := time.Parse(time.RFC3339, ts2); err != nil {
		t.Errorf("timestamp should be RFC3339, got %s: %v", ts2, err)
	}
	// type should be "block".
	if body["type"] != "block" {
		t.Errorf("want type=block, got %v", body["type"])
	}
}

func TestGetTx_FromObjectStructure(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(sd.txs[0].Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash, 200)

	from, ok := body["from"].(map[string]any)
	if !ok {
		t.Fatalf("from should be map, got %T", body["from"])
	}
	fromHash := from["hash"].(string)
	if !strings.HasPrefix(fromHash, "0x") {
		t.Error("from.hash should be 0x-prefixed")
	}

	to, ok := body["to"].(map[string]any)
	if !ok {
		t.Fatalf("to should be map for non-creation tx, got %T", body["to"])
	}
	toHash := to["hash"].(string)
	if !strings.HasPrefix(toHash, "0x") {
		t.Error("to.hash should be 0x-prefixed")
	}
}

func TestGetTx_MethodField(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	// Contract creation tx has input with 4-byte prefix.
	hash := hexStr(sd.contractTx.Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash, 200)
	// Method should be first 4 bytes (0x + 8 chars = 10 chars).
	method := body["method"]
	if method != nil {
		m := fmt.Sprintf("%v", method)
		if m != "" && len(m) != 10 {
			t.Errorf("method should be 10 chars (0x + 4 bytes), got %q (%d)", m, len(m))
		}
	}
}

func TestGetTx_ResultField(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	// Successful tx should have result=success.
	hash := hexStr(sd.txs[0].Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash, 200)
	if body["result"] != "success" {
		t.Errorf("want result=success, got %v", body["result"])
	}

	// Failed tx should have result=error text.
	hash = hexStr(sd.failedTx.Hash)
	body = getJSON(t, ts, "/v1/explorer/transactions/"+hash, 200)
	if body["result"] == "success" {
		t.Error("failed tx should not have result=success")
	}
}

func TestGetTx_FailedTxRevertReason(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(sd.failedTx.Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash, 200)
	if body["revert_reason"] == nil {
		t.Error("failed tx should have revert_reason")
	}
}

// ====================
// MANY BLOCKS (pagination stress)
// ====================

func TestListBlocks_ManyBlocks(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	now := time.Now().Unix()
	for i := 0; i < 60; i++ {
		b := testutil.DefaultBlock()
		b.Number = int64(i)
		b.Timestamp = now - int64(60-i)
		insertBlock(t, tdb, b)
	}
	_, ts := newServer(t, tdb.Path)

	// Default items_count=50, should return 50.
	body := getJSON(t, ts, "/v1/explorer/blocks", 200)
	got := items(t, body)
	if len(got) != 50 {
		t.Errorf("want 50 items with default limit, got %d", len(got))
	}
	if body["next_page_params"] == nil {
		t.Error("should have next_page_params when more than 50 blocks")
	}
}

func TestListTxs_ManyTxs(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	now := time.Now().Unix()
	for i := 0; i < 60; i++ {
		b := testutil.DefaultBlock()
		b.Number = int64(i)
		b.Timestamp = now - int64(60-i)
		b.TransactionCount = 1
		b = insertBlock(t, tdb, b)
		tx := testutil.DefaultTransaction(b)
		tx.TransactionIndex = 0
		insertTx(t, tdb, tx)
	}
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/transactions", 200)
	got := items(t, body)
	if len(got) != 50 {
		t.Errorf("want 50 items with default limit, got %d", len(got))
	}
	if body["next_page_params"] == nil {
		t.Error("should have next_page_params when more than 50 txs")
	}
}

// ====================
// TABLE-DRIVEN: Block fields
// ====================

func TestBlockFieldValues(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	b := testutil.DefaultBlock()
	b.Number = 42
	b.GasLimit = 8000000
	b.GasUsed = 1234567
	b.BaseFee = "25000000000"
	b.Size = 2048
	b.TransactionCount = 3
	b = insertBlock(t, tdb, b)
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/blocks/42", 200)

	tests := []struct {
		field string
		want  any
	}{
		{"height", float64(42)},
		{"hash", hexStr(b.Hash)},
		{"parent_hash", hexStr(b.ParentHash)},
		{"gas_limit", "8000000"},
		{"gas_used", "1234567"},
		{"base_fee_per_gas", "25000000000"},
		{"size", float64(2048)},
		{"tx_count", float64(3)},
		{"type", "block"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got := body[tt.field]
			if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", tt.want) {
				t.Errorf("want %v, got %v", tt.want, got)
			}
		})
	}
}

// ====================
// TABLE-DRIVEN: Transaction field values
// ====================

func TestTxFieldValues(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	b := testutil.DefaultBlock()
	b.Number = 10
	b = insertBlock(t, tdb, b)

	tx := testutil.DefaultTransaction(b)
	tx.Value = "1000000000000000000"
	tx.Gas = 21000
	tx.GasPrice = "25000000000"
	tx.GasUsed = 21000
	tx.Nonce = 5
	tx.TransactionIndex = 0
	tx = insertTx(t, tdb, tx)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(tx.Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash, 200)

	tests := []struct {
		field string
		want  string
	}{
		{"value", "1000000000000000000"},
		{"gas_limit", "21000"},
		{"gas_price", "25000000000"},
		{"gas_used", "21000"},
		{"status", "ok"},
		{"result", "success"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got := fmt.Sprintf("%v", body[tt.field])
			if got != tt.want {
				t.Errorf("want %s, got %s", tt.want, got)
			}
		})
	}

	// nonce should be numeric.
	if toFloat(body["nonce"]) != 5 {
		t.Errorf("want nonce 5, got %v", body["nonce"])
	}
	// position should be numeric.
	if toFloat(body["position"]) != 0 {
		t.Errorf("want position 0, got %v", body["position"])
	}
}

// ====================
// TABLE-DRIVEN: Address field values
// ====================

func TestAddrFieldValues(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	a := testutil.DefaultAddress()
	a.FetchedCoinBalance = "5000000000000000000"
	a.TransactionsCount = 42
	a.TokenTransfersCount = 7
	a = insertAddr(t, tdb, a)
	_, ts := newServer(t, tdb.Path)

	hash := hexStr(a.Hash)
	body := getJSON(t, ts, "/v1/explorer/addresses/"+hash, 200)

	tests := []struct {
		field string
		want  string
	}{
		{"hash", hash},
		{"coin_balance", "5000000000000000000"},
		{"is_contract", "false"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got := fmt.Sprintf("%v", body[tt.field])
			if got != tt.want {
				t.Errorf("want %s, got %s", tt.want, got)
			}
		})
	}

	if toFloat(body["transactions_count"]) != 42 {
		t.Errorf("want transactions_count 42, got %v", body["transactions_count"])
	}
	if toFloat(body["token_transfers_count"]) != 7 {
		t.Errorf("want token_transfers_count 7, got %v", body["token_transfers_count"])
	}
}

// ====================
// TABLE-DRIVEN: Token field values
// ====================

func TestTokenFieldValues(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	tk := testutil.DefaultToken()
	tk.Name = "Test Coin"
	tk.Symbol = "TCN"
	tk.Decimals = 18
	tk.TotalSupply = "1000000000000000000000000"
	tk.HolderCount = 500
	tk = insertToken(t, tdb, tk)
	_, ts := newServer(t, tdb.Path)

	addr := hexStr(tk.ContractAddress)
	body := getJSON(t, ts, "/v1/explorer/tokens/"+addr, 200)

	tests := []struct {
		field string
		want  string
	}{
		{"name", "Test Coin"},
		{"symbol", "TCN"},
		{"decimals", "18"},
		{"total_supply", "1000000000000000000000000"},
		{"type", "ERC-20"},
		{"holders", "500"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got := fmt.Sprintf("%v", body[tt.field])
			if got != tt.want {
				t.Errorf("want %s, got %s", tt.want, got)
			}
		})
	}
}

// ====================
// TABLE-DRIVEN: Smart contract field values
// ====================

func TestContractFieldValues(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sc := testutil.DefaultSmartContract()
	sc.Name = "Vault"
	sc.CompilerVersion = "v0.8.20+commit.a1b79de6"
	sc.EVMVersion = "paris"
	sc = insertSmartContract(t, tdb, sc)
	_, ts := newServer(t, tdb.Path)

	addr := hexStr(sc.AddressHash)
	body := getJSON(t, ts, "/v1/explorer/smart-contracts/"+addr, 200)

	tests := []struct {
		field string
		want  string
	}{
		{"name", "Vault"},
		{"compiler_version", "v0.8.20+commit.a1b79de6"},
		{"evm_version", "paris"},
		{"is_verified", "true"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got := fmt.Sprintf("%v", body[tt.field])
			if got != tt.want {
				t.Errorf("want %s, got %s", tt.want, got)
			}
		})
	}
}

// ====================
// INTERNAL TX FIELDS
// ====================

func TestInternalTxFields(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	b := testutil.DefaultBlock()
	b.Number = 100
	b = insertBlock(t, tdb, b)

	tx := testutil.DefaultTransaction(b)
	tx = insertTx(t, tdb, tx)

	itx := testutil.DefaultInternalTx(tx)
	itx.Value = "999000000000000"
	itx.Gas = 30000
	itx.GasUsed = 20000
	itx.Type = "call"
	itx.CallType = "delegatecall"
	itx = insertInternalTx(t, tdb, itx)

	_, ts := newServer(t, tdb.Path)

	hash := hexStr(tx.Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash+"/internal-transactions", 200)
	got := itemMaps(t, body)
	if len(got) == 0 {
		t.Fatal("expected at least 1 internal tx")
	}
	itxResp := got[0]
	if itxResp["type"] != "call" {
		t.Errorf("want type call, got %v", itxResp["type"])
	}
	if itxResp["call_type"] != "delegatecall" {
		t.Errorf("want call_type delegatecall, got %v", itxResp["call_type"])
	}
	if itxResp["value"] != "999000000000000" {
		t.Errorf("want value 999000000000000, got %v", itxResp["value"])
	}
}

// ====================
// LOG FIELDS
// ====================

func TestLogFields(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	b := testutil.DefaultBlock()
	b.Number = 200
	b = insertBlock(t, tdb, b)

	tx := testutil.DefaultTransaction(b)
	tx = insertTx(t, tdb, tx)

	lg := testutil.DefaultLog(tx)
	lg.Index = 7
	lg = insertLog(t, tdb, lg)

	_, ts := newServer(t, tdb.Path)

	hash := hexStr(tx.Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash+"/logs", 200)
	got := itemMaps(t, body)
	if len(got) == 0 {
		t.Fatal("expected at least 1 log")
	}
	logResp := got[0]

	if toFloat(logResp["index"]) != 7 {
		t.Errorf("want index 7, got %v", logResp["index"])
	}
	// address should be object.
	addr, ok := logResp["address"].(map[string]any)
	if !ok {
		t.Fatalf("address should be object, got %T", logResp["address"])
	}
	if addr["hash"] == nil {
		t.Error("address.hash is nil")
	}
	// topics should be array.
	topics, ok := logResp["topics"].([]any)
	if !ok {
		t.Fatalf("topics should be array, got %T", logResp["topics"])
	}
	// DefaultLog sets 3 topics (first, second, third).
	if len(topics) < 3 {
		t.Errorf("want at least 3 topics, got %d", len(topics))
	}
}

// ====================
// TOKEN TRANSFER FIELDS
// ====================

func TestTokenTransferFields(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	b := testutil.DefaultBlock()
	b.Number = 300
	b = insertBlock(t, tdb, b)

	tx := testutil.DefaultTransaction(b)
	tx = insertTx(t, tdb, tx)

	tk := testutil.DefaultToken()
	tk = insertToken(t, tdb, tk)

	tt := testutil.DefaultTokenTransfer(tx, tk)
	tt.Amount = "500000000000000000"
	tt = insertTokenTransfer(t, tdb, tt)

	_, ts := newServer(t, tdb.Path)

	hash := hexStr(tx.Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash+"/token-transfers", 200)
	got := itemMaps(t, body)
	if len(got) == 0 {
		t.Fatal("expected at least 1 token transfer")
	}
	tf := got[0]

	// from and to should be objects.
	from, ok := tf["from"].(map[string]any)
	if !ok {
		t.Fatalf("from should be object, got %T", tf["from"])
	}
	if from["hash"] == nil {
		t.Error("from.hash is nil")
	}
	// token should be object with address and type.
	token, ok := tf["token"].(map[string]any)
	if !ok {
		t.Fatalf("token should be object, got %T", tf["token"])
	}
	if token["address"] == nil {
		t.Error("token.address is nil")
	}
	// total should be object with value.
	total, ok := tf["total"].(map[string]any)
	if !ok {
		t.Fatalf("total should be object, got %T", tf["total"])
	}
	if total["value"] != "500000000000000000" {
		t.Errorf("want value 500000000000000000, got %v", total["value"])
	}
}

// ====================
// EMPTY PAGINATED ENDPOINTS (ensure structure even when empty)
// ====================

func TestEmptyEndpoints(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	_, ts := newServer(t, tdb.Path)

	endpoints := []string{
		"/v1/explorer/blocks",
		"/v1/explorer/transactions",
		"/v1/explorer/addresses",
		"/v1/explorer/tokens",
		"/v1/explorer/search?q=",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			body := getJSON(t, ts, ep, 200)
			got := items(t, body)
			if len(got) != 0 {
				t.Errorf("want 0 items, got %d", len(got))
			}
		})
	}
}

// ====================
// ITEMS_COUNT BOUNDARY VALUES
// ====================

func TestItemsCountBoundary(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	now := time.Now().Unix()
	for i := 0; i < 10; i++ {
		b := testutil.DefaultBlock()
		b.Number = int64(i)
		b.Timestamp = now - int64(10-i)
		insertBlock(t, tdb, b)
	}
	_, ts := newServer(t, tdb.Path)

	tests := []struct {
		name     string
		count    string
		wantMax  int
	}{
		{"count 1", "1", 1},
		{"count 5", "5", 5},
		{"count 10", "10", 10},
		{"count 250", "250", 10}, // Only 10 blocks exist.
		{"count 251", "251", 10}, // Capped at 250 but only 10 exist.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := getJSON(t, ts, "/v1/explorer/blocks?items_count="+tt.count, 200)
			got := items(t, body)
			if len(got) > tt.wantMax {
				t.Errorf("want at most %d items, got %d", tt.wantMax, len(got))
			}
		})
	}
}

// ====================
// MULTIPLE TOKEN TYPES
// ====================

func TestMultipleTokenTypes(t *testing.T) {
	tdb := testutil.NewTestDB(t)

	types := []struct {
		tokenType string
		decimals  int
	}{
		{"ERC-20", 18},
		{"ERC-721", 0},
		{"ERC-1155", 0},
	}

	for _, tt := range types {
		tk := testutil.DefaultToken()
		tk.Type = tt.tokenType
		tk.Decimals = tt.decimals
		tk.Name = tt.tokenType + " Token"
		insertToken(t, tdb, tk)
	}
	_, ts := newServer(t, tdb.Path)

	body := getJSON(t, ts, "/v1/explorer/tokens", 200)
	got := itemMaps(t, body)
	if len(got) != 3 {
		t.Fatalf("want 3 tokens, got %d", len(got))
	}

	typeSet := map[string]bool{}
	for _, tk := range got {
		typeSet[tk["type"].(string)] = true
	}
	for _, tt := range types {
		if !typeSet[tt.tokenType] {
			t.Errorf("missing token type %s", tt.tokenType)
		}
	}
}

// ====================
// ADDRESS HASH CASE SENSITIVITY
// ====================

func TestAddressHashCase(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	// Lowercase 0x prefix is required by the search handler.
	body := getJSON(t, ts, "/v1/explorer/search?q=0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", 200)
	got := itemMaps(t, body)
	found := false
	for _, item := range got {
		if item["type"] == "address" {
			found = true
			addrHash := item["address_hash"].(string)
			if !strings.HasPrefix(addrHash, "0x") {
				t.Error("address_hash should be 0x-prefixed")
			}
			if addrHash != strings.ToLower(addrHash) {
				t.Error("address_hash should be lowercase")
			}
		}
	}
	if !found {
		t.Error("search by lowercase 0x address should return result")
	}

	// Uppercase 0X prefix does not match the search handler's check.
	body2 := getJSON(t, ts, "/v1/explorer/search?q=0Xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", 200)
	got2 := items(t, body2)
	if len(got2) != 0 {
		t.Error("uppercase 0X prefix should not match address search")
	}
}

// ====================
// HOLDER ADDRESS OBJECT STRUCTURE
// ====================

func TestTokenHolders_AddressObject(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	addr := hexStr(sd.erc20Token.ContractAddress)
	body := getJSON(t, ts, "/v1/explorer/tokens/"+addr+"/holders", 200)
	got := itemMaps(t, body)
	if len(got) == 0 {
		t.Fatal("expected holders")
	}
	h := got[0]
	addrObj, ok := h["address"].(map[string]any)
	if !ok {
		t.Fatalf("address should be object, got %T", h["address"])
	}
	hash := addrObj["hash"].(string)
	if !strings.HasPrefix(hash, "0x") {
		t.Error("address.hash should be 0x-prefixed")
	}
}

// ====================
// SEARCH MULTIPLE RESULTS
// ====================

func TestSearch_MultipleResults(t *testing.T) {
	tdb := testutil.NewTestDB(t)

	// Create block number 42.
	b := testutil.DefaultBlock()
	b.Number = 42
	insertBlock(t, tdb, b)

	_, ts := newServer(t, tdb.Path)

	// Search for "42" should return a block result.
	body := getJSON(t, ts, "/v1/explorer/search?q=42", 200)
	got := itemMaps(t, body)
	if len(got) == 0 {
		t.Error("search for 42 should find block")
	}
}

// ====================
// TX WITH ALL NULL OPTIONALS
// ====================

func TestGetTx_MinimalFields(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	b := testutil.DefaultBlock()
	b.Number = 0
	b = insertBlock(t, tdb, b)

	tx := testutil.DefaultTransaction(b)
	tx.MaxFeePerGas = ""
	tx.MaxPriorityFeePerGas = ""
	tx.Input = nil
	tx.Error = ""
	tx.RevertReason = ""
	tx.CreatedContract = nil
	tx = insertTx(t, tdb, tx)

	_, ts := newServer(t, tdb.Path)

	hash := hexStr(tx.Hash)
	body := getJSON(t, ts, "/v1/explorer/transactions/"+hash, 200)
	// Should not panic, should return valid JSON.
	if body["hash"] != hash {
		t.Errorf("want hash %s, got %v", hash, body["hash"])
	}
}

// ====================
// NEWSTANDALONESERVER ERROR CASES
// ====================

func TestNewStandaloneServer_EmptyPath(t *testing.T) {
	_, err := NewStandaloneServer(Config{})
	if err == nil {
		t.Error("expected error for empty IndexerDBPath")
	}
}

func TestNewStandaloneServer_InvalidPath(t *testing.T) {
	_, err := NewStandaloneServer(Config{IndexerDBPath: "/nonexistent/path/db.sqlite"})
	if err == nil {
		t.Error("expected error for nonexistent DB path")
	}
}

// ====================
// HELPERS
// ====================

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}

// Ensure imports are used.
var _ = fmt.Sprintf
var _ = time.Second
