package explorer

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luxfi/explorer/explorer/testutil"
)

// testPlugin creates a plugin with a test database and returns an HTTP handler.
func testPlugin(t *testing.T) (*plugin, *testutil.DB) {
	t.Helper()
	tdb := testutil.NewTestDB(t)

	p := &plugin{
		config: Config{
			IndexerDBPath: tdb.Path,
			ChainID:       testutil.DefaultChainID,
			ChainName:     "Test Chain",
			CoinSymbol:    "COIN",
			CoinDecimals:  18,
		},
		db: tdb.DB,
	}
	return p, tdb
}

func doGet(p *plugin, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()

	// Route manually based on path
	mux := http.NewServeMux()
	registerTestRoutes(mux, p)
	mux.ServeHTTP(w, req)
	return w
}

// registerTestRoutes maps the plugin handlers to a standard mux for testing.
func registerTestRoutes(mux *http.ServeMux, p *plugin) {
	// We test handlers directly since Base router isn't available in unit tests.
	// For integration tests, use the full Base app.
}

func parseJSON(t *testing.T, body io.Reader) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.NewDecoder(body).Decode(&result); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	return result
}

func parsePaginated(t *testing.T, body io.Reader) ([]any, any) {
	t.Helper()
	result := parseJSON(t, body)
	items, _ := result["items"].([]any)
	return items, result["next_page_params"]
}

// =====================================================================
// BLOCK TESTS (ported from block_controller_test.exs)
// =====================================================================

func TestHandleListBlocks(t *testing.T) {
	p, tdb := testPlugin(t)
	blocks, _, _ := tdb.SeedChainData(t, 10)

	// Simulate the handler directly
	t.Run("returns blocks ordered by number desc", func(t *testing.T) {
		rows, err := p.db.Query("SELECT * FROM blocks WHERE chain_id = ? ORDER BY number DESC LIMIT 50", p.config.ChainID)
		if err != nil {
			t.Fatalf("query blocks: %v", err)
		}
		defer rows.Close()

		results, err := scanMaps(rows)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(results) != 10 {
			t.Errorf("expected 10 blocks, got %d", len(results))
		}

		// Verify descending order
		for i := 1; i < len(results); i++ {
			prev := results[i-1]["number"].(int64)
			curr := results[i]["number"].(int64)
			if prev <= curr {
				t.Errorf("blocks not in descending order: %d <= %d", prev, curr)
			}
		}
	})

	t.Run("formatBlock produces v2 response", func(t *testing.T) {
		row, err := p.db.Query("SELECT * FROM blocks WHERE chain_id = ? AND number = ? LIMIT 1", p.config.ChainID, blocks[0].Number)
		if err != nil {
			t.Fatal(err)
		}
		defer row.Close()

		maps, _ := scanMaps(row)
		if len(maps) == 0 {
			t.Fatal("block not found")
		}

		resp := formatBlock(maps[0])

		// Verify required fields exist
		for _, field := range []string{"height", "hash", "parent_hash", "miner", "size", "gas_limit", "gas_used", "timestamp", "tx_count", "type"} {
			if _, ok := resp[field]; !ok {
				t.Errorf("missing field: %s", field)
			}
		}

		// Miner should be {hash: "0x..."}
		miner, ok := resp["miner"].(map[string]any)
		if !ok {
			t.Errorf("miner should be an object, got %T", resp["miner"])
		} else if _, ok := miner["hash"]; !ok {
			t.Error("miner.hash missing")
		}

		// Height should match
		if resp["height"] != maps[0]["number"] {
			t.Errorf("height = %v, want %v", resp["height"], maps[0]["number"])
		}

		// Type should be "block"
		if resp["type"] != "block" {
			t.Errorf("type = %v, want block", resp["type"])
		}
	})
}

// =====================================================================
// TRANSACTION TESTS (ported from transaction_controller_test.exs)
// =====================================================================

func TestHandleGetTransaction(t *testing.T) {
	p, tdb := testPlugin(t)
	_, txs, _ := tdb.SeedChainData(t, 3)

	t.Run("returns transaction by hash", func(t *testing.T) {
		tx := txs[0]
		rows, err := p.db.Query("SELECT * FROM transactions WHERE chain_id = ? AND hash = ? LIMIT 1",
			p.config.ChainID, tx.Hash)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		maps, _ := scanMaps(rows)
		if len(maps) == 0 {
			t.Fatal("transaction not found")
		}

		resp := formatTx(maps[0])

		// Required fields
		for _, field := range []string{"hash", "block_number", "from", "value", "gas_limit", "nonce", "position", "type", "status", "timestamp"} {
			if _, ok := resp[field]; !ok {
				t.Errorf("missing field: %s", field)
			}
		}

		// Status should be "ok" for successful transactions
		if resp["status"] != "ok" {
			t.Errorf("status = %v, want ok", resp["status"])
		}

		// From should be {hash: "0x..."}
		from, ok := resp["from"].(map[string]any)
		if !ok {
			t.Fatal("from should be an object")
		}
		if _, ok := from["hash"]; !ok {
			t.Error("from.hash missing")
		}
	})

	t.Run("transaction types", func(t *testing.T) {
		block := testutil.DefaultBlock()
		block = tdb.InsertBlock(t, block)

		// Legacy (type 0)
		tx0 := testutil.DefaultTransaction(block)
		tx0.Type = 0
		tx0 = tdb.InsertTransaction(t, tx0)

		// EIP-1559 (type 2)
		tx2 := testutil.DefaultEIP1559Transaction(block)
		tx2 = tdb.InsertTransaction(t, tx2)

		// Failed transaction
		txFail := testutil.DefaultFailedTransaction(block)
		txFail = tdb.InsertTransaction(t, txFail)

		// Contract creation
		txCreate := testutil.DefaultContractCreation(block)
		txCreate = tdb.InsertTransaction(t, txCreate)

		// Verify type 0
		r0 := queryTxResponse(t, p, tx0.Hash)
		if r0["type"] != int64(0) {
			t.Errorf("type 0: got type=%v", r0["type"])
		}

		// Verify EIP-1559
		r2 := queryTxResponse(t, p, tx2.Hash)
		if r2["type"] != int64(2) {
			t.Errorf("EIP-1559: got type=%v", r2["type"])
		}
		if fmtNum(r2["max_fee_per_gas"]) == "0" {
			t.Error("EIP-1559: max_fee_per_gas should be set")
		}

		// Verify failed
		rFail := queryTxResponse(t, p, txFail.Hash)
		if rFail["status"] != "error" {
			t.Errorf("failed tx: status=%v, want error", rFail["status"])
		}
		if rFail["result"] == "" || rFail["result"] == nil {
			t.Error("failed tx: result should contain error message")
		}

		// Verify contract creation
		rCreate := queryTxResponse(t, p, txCreate.Hash)
		if rCreate["created_contract"] == nil {
			t.Error("contract creation: created_contract should be set")
		}
	})
}

// =====================================================================
// ADDRESS TESTS (ported from address_controller_test.exs)
// =====================================================================

func TestHandleGetAddress(t *testing.T) {
	p, tdb := testPlugin(t)

	t.Run("returns address with balance", func(t *testing.T) {
		addr := testutil.DefaultAddress()
		addr = tdb.InsertAddress(t, addr)

		rows, err := p.db.Query("SELECT * FROM addresses WHERE chain_id = ? AND hash = ? LIMIT 1",
			p.config.ChainID, addr.Hash)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		maps, _ := scanMaps(rows)
		if len(maps) == 0 {
			t.Fatal("address not found")
		}

		resp := formatAddress(maps[0])

		if resp["coin_balance"] == nil || resp["coin_balance"] == "0" || resp["coin_balance"] == "" {
			t.Error("coin_balance should be set")
		}
		if resp["transactions_count"] == nil {
			t.Error("transactions_count should be set")
		}
	})

	t.Run("contract address detection", func(t *testing.T) {
		contract := testutil.DefaultContractAddress()
		contract = tdb.InsertAddress(t, contract)

		rows, err := p.db.Query("SELECT * FROM addresses WHERE chain_id = ? AND hash = ? LIMIT 1",
			p.config.ChainID, contract.Hash)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		maps, _ := scanMaps(rows)
		resp := formatAddress(maps[0])

		if resp["is_contract"] != true {
			t.Errorf("is_contract = %v, want true", resp["is_contract"])
		}
	})
}

// =====================================================================
// TOKEN TESTS (ported from token_controller_test.exs)
// =====================================================================

func TestTokenEndpoints(t *testing.T) {
	p, tdb := testPlugin(t)

	t.Run("list ERC-20 tokens", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			tk := testutil.DefaultToken()
			tdb.InsertToken(t, tk)
		}

		rows, err := p.db.Query("SELECT * FROM tokens WHERE chain_id = ? ORDER BY holder_count DESC", p.config.ChainID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		maps, _ := scanMaps(rows)
		if len(maps) != 5 {
			t.Errorf("expected 5 tokens, got %d", len(maps))
		}

		// Verify explorer v2 format
		resp := formatToken(maps[0])
		for _, field := range []string{"address", "name", "symbol", "total_supply", "decimals", "type", "holders"} {
			if _, ok := resp[field]; !ok {
				t.Errorf("missing field: %s", field)
			}
		}
	})

	t.Run("ERC-721 token", func(t *testing.T) {
		tk := testutil.DefaultERC721Token()
		tk = tdb.InsertToken(t, tk)

		rows, err := p.db.Query("SELECT * FROM tokens WHERE chain_id = ? AND contract_address_hash = ?",
			p.config.ChainID, tk.ContractAddress)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		maps, _ := scanMaps(rows)
		resp := formatToken(maps[0])
		if resp["type"] != "ERC-721" {
			t.Errorf("type = %v, want ERC-721", resp["type"])
		}
	})
}

// =====================================================================
// TOKEN TRANSFER TESTS (ported from token_transfer_controller_test.exs)
// =====================================================================

func TestTokenTransfers(t *testing.T) {
	p, tdb := testPlugin(t)

	block := testutil.DefaultBlock()
	block = tdb.InsertBlock(t, block)

	tx := testutil.DefaultTransaction(block)
	tx = tdb.InsertTransaction(t, tx)

	token := testutil.DefaultToken()
	token = tdb.InsertToken(t, token)

	transfer := testutil.DefaultTokenTransfer(tx, token)
	transfer = tdb.InsertTokenTransfer(t, transfer)

	t.Run("transfer by transaction hash", func(t *testing.T) {
		rows, err := p.db.Query("SELECT * FROM token_transfers WHERE chain_id = ? AND transaction_hash = ?",
			p.config.ChainID, tx.Hash)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		maps, _ := scanMaps(rows)
		if len(maps) != 1 {
			t.Fatalf("expected 1 transfer, got %d", len(maps))
		}

		resp := formatTokenTransfer(maps[0])
		from, ok := resp["from"].(map[string]any)
		if !ok {
			t.Fatal("from should be an object")
		}
		if from["hash"] == "" {
			t.Error("from.hash should be set")
		}

		tokenObj, ok := resp["token"].(map[string]any)
		if !ok {
			t.Fatal("token should be an object")
		}
		if tokenObj["address"] == "" {
			t.Error("token.address should be set")
		}
	})
}

// =====================================================================
// LOG TESTS (ported from log domain tests)
// =====================================================================

func TestLogs(t *testing.T) {
	p, tdb := testPlugin(t)

	block := testutil.DefaultBlock()
	block = tdb.InsertBlock(t, block)

	tx := testutil.DefaultTransaction(block)
	tx = tdb.InsertTransaction(t, tx)

	log := testutil.DefaultLog(tx)
	log = tdb.InsertLog(t, log)

	t.Run("log by transaction hash", func(t *testing.T) {
		rows, err := p.db.Query("SELECT * FROM logs WHERE chain_id = ? AND transaction_hash = ?",
			p.config.ChainID, tx.Hash)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		maps, _ := scanMaps(rows)
		if len(maps) != 1 {
			t.Fatalf("expected 1 log, got %d", len(maps))
		}

		resp := formatLog(maps[0])

		// Topics should be an array of hex strings
		topics, ok := resp["topics"].([]string)
		if !ok {
			t.Fatal("topics should be []string")
		}
		if len(topics) < 1 {
			t.Error("should have at least 1 topic")
		}
		for _, topic := range topics {
			if !strings.HasPrefix(topic, "0x") {
				t.Errorf("topic should start with 0x, got %s", topic)
			}
		}

		// Address should be {hash: "0x..."}
		addr, ok := resp["address"].(map[string]any)
		if !ok {
			t.Fatal("address should be an object")
		}
		if !strings.HasPrefix(addr["hash"].(string), "0x") {
			t.Error("address.hash should start with 0x")
		}
	})
}

// =====================================================================
// INTERNAL TRANSACTION TESTS
// =====================================================================

func TestInternalTransactions(t *testing.T) {
	p, tdb := testPlugin(t)

	block := testutil.DefaultBlock()
	block = tdb.InsertBlock(t, block)

	tx := testutil.DefaultTransaction(block)
	tx = tdb.InsertTransaction(t, tx)

	itx := testutil.DefaultInternalTx(tx)
	itx = tdb.InsertInternalTx(t, itx)

	t.Run("internal tx by transaction hash", func(t *testing.T) {
		rows, err := p.db.Query(`SELECT * FROM internal_transactions WHERE chain_id = ? AND transaction_hash = ?`,
			p.config.ChainID, tx.Hash)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		maps, _ := scanMaps(rows)
		if len(maps) != 1 {
			t.Fatalf("expected 1 internal tx, got %d", len(maps))
		}

		resp := formatInternalTx(maps[0])
		if resp["type"] != "call" {
			t.Errorf("type = %v, want call", resp["type"])
		}
		if resp["call_type"] != "call" {
			t.Errorf("call_type = %v, want call", resp["call_type"])
		}
		if resp["success"] != true {
			t.Errorf("success = %v, want true", resp["success"])
		}
	})

	t.Run("failed internal tx", func(t *testing.T) {
		failedItx := testutil.DefaultInternalTx(tx)
		failedItx.Error = "out of gas"
		failedItx = tdb.InsertInternalTx(t, failedItx)

		rows, err := p.db.Query(`SELECT * FROM internal_transactions WHERE chain_id = ? AND "index" = ?`,
			p.config.ChainID, failedItx.Index)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		maps, _ := scanMaps(rows)
		resp := formatInternalTx(maps[0])
		if resp["success"] != false {
			t.Errorf("success = %v, want false for failed itx", resp["success"])
		}
		if resp["error"] != "out of gas" {
			t.Errorf("error = %v, want 'out of gas'", resp["error"])
		}
	})
}

// =====================================================================
// SMART CONTRACT TESTS (ported from smart_contract_controller_test.exs)
// =====================================================================

func TestSmartContracts(t *testing.T) {
	p, tdb := testPlugin(t)

	sc := testutil.DefaultSmartContract()
	sc = tdb.InsertSmartContract(t, sc)

	t.Run("get verified contract", func(t *testing.T) {
		rows, err := p.db.Query("SELECT * FROM smart_contracts WHERE chain_id = ? AND address_hash = ?",
			p.config.ChainID, sc.AddressHash)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		maps, _ := scanMaps(rows)
		if len(maps) == 0 {
			t.Fatal("contract not found")
		}

		resp := formatContract(maps[0])
		if resp["name"] != "SimpleStorage" {
			t.Errorf("name = %v, want SimpleStorage", resp["name"])
		}
		if resp["is_verified"] != true {
			t.Errorf("is_verified = %v, want true", resp["is_verified"])
		}
		if resp["source_code"] == nil || resp["source_code"] == "" {
			t.Error("source_code should be set")
		}
		if resp["abi"] == nil {
			t.Error("abi should be set")
		}
	})
}

// =====================================================================
// SEARCH TESTS (ported from search_controller_test.exs)
// =====================================================================

func TestSearch(t *testing.T) {
	_, tdb := testPlugin(t)

	blocks, txs, _ := tdb.SeedChainData(t, 3)
	token := testutil.DefaultToken()
	token.Name = "Wrapped COIN"
	token.Symbol = "WCOIN"
	token = tdb.InsertToken(t, token)

	t.Run("search by block number", func(t *testing.T) {
		var count int
		tdb.QueryRow("SELECT COUNT(*) FROM blocks WHERE chain_id = ? AND number = ?",
			testutil.DefaultChainID, blocks[0].Number).Scan(&count)
		if count != 1 {
			t.Errorf("expected block %d to exist, count=%d", blocks[0].Number, count)
		}
	})

	t.Run("search by tx hash", func(t *testing.T) {
		var count int
		tdb.QueryRow("SELECT COUNT(*) FROM transactions WHERE chain_id = ? AND hash = ?",
			testutil.DefaultChainID, txs[0].Hash).Scan(&count)
		if count != 1 {
			t.Error("expected transaction to exist")
		}
	})

	t.Run("search by token name", func(t *testing.T) {
		rows, err := tdb.Query("SELECT * FROM tokens WHERE chain_id = ? AND name LIKE ?",
			testutil.DefaultChainID, "%Wrapped%")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		maps, _ := scanMaps(rows)
		if len(maps) != 1 {
			t.Errorf("expected 1 token matching 'Wrapped', got %d", len(maps))
		}
	})

	t.Run("search by token symbol", func(t *testing.T) {
		rows, err := tdb.Query("SELECT * FROM tokens WHERE chain_id = ? AND symbol LIKE ?",
			testutil.DefaultChainID, "%WCOIN%")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		maps, _ := scanMaps(rows)
		if len(maps) != 1 {
			t.Errorf("expected 1 token matching 'WCOIN', got %d", len(maps))
		}
	})
}

// =====================================================================
// STATS TESTS (ported from stats_controller_test.exs)
// =====================================================================

func TestStats(t *testing.T) {
	_, tdb := testPlugin(t)
	tdb.SeedChainData(t, 5)

	t.Run("total counts", func(t *testing.T) {
		var blockCount, txCount, addrCount int
		tdb.QueryRow("SELECT COUNT(*) FROM blocks WHERE chain_id = ?", testutil.DefaultChainID).Scan(&blockCount)
		tdb.QueryRow("SELECT COUNT(*) FROM transactions WHERE chain_id = ?", testutil.DefaultChainID).Scan(&txCount)
		tdb.QueryRow("SELECT COUNT(*) FROM addresses WHERE chain_id = ?", testutil.DefaultChainID).Scan(&addrCount)

		if blockCount != 5 {
			t.Errorf("blocks = %d, want 5", blockCount)
		}
		if txCount != 10 { // 2 per block
			t.Errorf("transactions = %d, want 10", txCount)
		}
		if addrCount != 5 {
			t.Errorf("addresses = %d, want 5", addrCount)
		}
	})
}

// =====================================================================
// FORMAT TESTS (edge cases in response formatting)
// =====================================================================

func TestFormatHelpers(t *testing.T) {
	t.Run("bytesToHex", func(t *testing.T) {
		tests := []struct {
			input any
			want  string
		}{
			{[]byte{0xde, 0xad, 0xbe, 0xef}, "0xdeadbeef"},
			{[]byte{}, ""},
			{nil, "<nil>"},
			{"0xAlready", "0xalready"},
		}
		for _, tt := range tests {
			got := bytesToHex(tt.input)
			if got != tt.want {
				t.Errorf("bytesToHex(%v) = %q, want %q", tt.input, got, tt.want)
			}
		}
	})

	t.Run("hexToBytes", func(t *testing.T) {
		b := hexToBytes("0xdeadbeef")
		if hex.EncodeToString(b) != "deadbeef" {
			t.Errorf("hexToBytes(0xdeadbeef) = %x", b)
		}
	})

	t.Run("txStatusStr", func(t *testing.T) {
		if txStatusStr(int64(1)) != "ok" {
			t.Error("status 1 should be ok")
		}
		if txStatusStr(int64(0)) != "error" {
			t.Error("status 0 should be error")
		}
		if txStatusStr(nil) != "pending" {
			t.Error("nil status should be pending")
		}
	})

	t.Run("fmtTimestamp", func(t *testing.T) {
		ts := fmtTimestamp(int64(1700000000))
		if !strings.Contains(ts, "2023") {
			t.Errorf("fmtTimestamp(1700000000) = %s, should contain 2023", ts)
		}
		if fmtTimestamp(int64(0)) != "" {
			t.Error("fmtTimestamp(0) should be empty")
		}
	})

	t.Run("formatInternalTx pending has null success and error", func(t *testing.T) {
		// Simulate a pending internal tx (no block_number).
		pendingMap := map[string]any{
			"block_number":      nil,
			"index":             int64(0),
			"transaction_hash":  []byte{0x01, 0x02},
			"type":              "call",
			"call_type":         "call",
			"from_address_hash": []byte{0xaa},
			"to_address_hash":   []byte{0xbb},
			"value":             "1000",
			"gas":               int64(21000),
			"gas_used":          int64(15000),
			"input":             []byte{},
			"output":            []byte{},
			"error":             nil,
			"block_timestamp":   nil,
		}
		resp := formatInternalTx(pendingMap)
		if resp["success"] != nil {
			t.Errorf("pending internal tx success = %v, want nil", resp["success"])
		}
		if resp["error"] != nil {
			t.Errorf("pending internal tx error = %v, want nil", resp["error"])
		}
	})

	t.Run("formatInternalTx mined success=true when no error", func(t *testing.T) {
		minedMap := map[string]any{
			"block_number":      int64(100),
			"index":             int64(0),
			"transaction_hash":  []byte{0x01, 0x02},
			"type":              "call",
			"call_type":         "call",
			"from_address_hash": []byte{0xaa},
			"to_address_hash":   []byte{0xbb},
			"value":             "1000",
			"gas":               int64(21000),
			"gas_used":          int64(15000),
			"input":             []byte{},
			"output":            []byte{},
			"error":             nil,
			"block_timestamp":   int64(1700000000),
		}
		resp := formatInternalTx(minedMap)
		if resp["success"] != true {
			t.Errorf("mined internal tx success = %v, want true", resp["success"])
		}
		if resp["error"] != nil {
			t.Errorf("mined internal tx error = %v, want nil", resp["error"])
		}
	})

	t.Run("formatInternalTx mined success=false with error", func(t *testing.T) {
		failedMap := map[string]any{
			"block_number":      int64(100),
			"index":             int64(0),
			"transaction_hash":  []byte{0x01, 0x02},
			"type":              "call",
			"call_type":         "call",
			"from_address_hash": []byte{0xaa},
			"to_address_hash":   []byte{0xbb},
			"value":             "1000",
			"gas":               int64(21000),
			"gas_used":          int64(21000),
			"input":             []byte{},
			"output":            []byte{},
			"error":             "out of gas",
			"block_timestamp":   int64(1700000000),
		}
		resp := formatInternalTx(failedMap)
		if resp["success"] != false {
			t.Errorf("failed internal tx success = %v, want false", resp["success"])
		}
		if resp["error"] != "out of gas" {
			t.Errorf("failed internal tx error = %v, want 'out of gas'", resp["error"])
		}
	})
}

// =====================================================================
// INTEGRATION TEST: indexer writes → Base reads → API serves
// =====================================================================

func TestIntegrationPipeline(t *testing.T) {
	_, tdb := testPlugin(t)

	// Phase 1: Indexer writes chain data to SQLite
	blocks, txs, addrs := tdb.SeedChainData(t, 20)

	// Add tokens and transfers
	token := testutil.DefaultToken()
	token = tdb.InsertToken(t, token)

	for i := 0; i < 5; i++ {
		tt := testutil.DefaultTokenTransfer(txs[i], token)
		tdb.InsertTokenTransfer(t, tt)
	}

	// Add smart contracts
	sc := testutil.DefaultSmartContract()
	sc = tdb.InsertSmartContract(t, sc)

	// Add internal transactions
	for i := 0; i < 3; i++ {
		itx := testutil.DefaultInternalTx(txs[i])
		tdb.InsertInternalTx(t, itx)
	}

	// Phase 2: Open a second read-only connection (simulates Base)
	roDb, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL&cache=shared", tdb.Path))
	if err != nil {
		t.Fatalf("open read-only: %v", err)
	}
	defer roDb.Close()
	roDb.SetMaxOpenConns(8)

	// Phase 3: Verify all data is readable through the read-only connection
	t.Run("blocks readable via RO connection", func(t *testing.T) {
		var count int
		roDb.QueryRow("SELECT COUNT(*) FROM blocks WHERE chain_id = ?", testutil.DefaultChainID).Scan(&count)
		if count != 20 {
			t.Errorf("blocks via RO = %d, want 20", count)
		}
	})

	t.Run("transactions readable via RO connection", func(t *testing.T) {
		var count int
		roDb.QueryRow("SELECT COUNT(*) FROM transactions WHERE chain_id = ?", testutil.DefaultChainID).Scan(&count)
		if count != 40 { // 2 per block × 20 blocks
			t.Errorf("transactions via RO = %d, want 40", count)
		}
	})

	t.Run("tokens readable via RO connection", func(t *testing.T) {
		var count int
		roDb.QueryRow("SELECT COUNT(*) FROM tokens WHERE chain_id = ?", testutil.DefaultChainID).Scan(&count)
		if count != 1 {
			t.Errorf("tokens via RO = %d, want 1", count)
		}
	})

	t.Run("token transfers readable via RO connection", func(t *testing.T) {
		var count int
		roDb.QueryRow("SELECT COUNT(*) FROM token_transfers WHERE chain_id = ?", testutil.DefaultChainID).Scan(&count)
		if count != 5 {
			t.Errorf("token_transfers via RO = %d, want 5", count)
		}
	})

	t.Run("smart contracts readable via RO connection", func(t *testing.T) {
		var count int
		roDb.QueryRow("SELECT COUNT(*) FROM smart_contracts WHERE chain_id = ?", testutil.DefaultChainID).Scan(&count)
		if count != 1 {
			t.Errorf("smart_contracts via RO = %d, want 1", count)
		}
	})

	t.Run("internal transactions readable via RO connection", func(t *testing.T) {
		var count int
		roDb.QueryRow("SELECT COUNT(*) FROM internal_transactions WHERE chain_id = ?", testutil.DefaultChainID).Scan(&count)
		if count != 3 {
			t.Errorf("internal_transactions via RO = %d, want 3", count)
		}
	})

	t.Run("logs readable via RO connection", func(t *testing.T) {
		var count int
		roDb.QueryRow("SELECT COUNT(*) FROM logs WHERE chain_id = ?", testutil.DefaultChainID).Scan(&count)
		if count != 40 { // 1 per tx × 40 txs
			t.Errorf("logs via RO = %d, want 40", count)
		}
	})

	// Phase 4: Verify concurrent read doesn't block
	t.Run("concurrent reads work", func(t *testing.T) {
		done := make(chan bool, 8)
		for i := 0; i < 8; i++ {
			go func() {
				var c int
				roDb.QueryRow("SELECT COUNT(*) FROM blocks WHERE chain_id = ?", testutil.DefaultChainID).Scan(&c)
				done <- c == 20
			}()
		}
		for i := 0; i < 8; i++ {
			if !<-done {
				t.Error("concurrent read returned wrong count")
			}
		}
	})

	// Phase 5: Verify write-then-read consistency (simulates indexer writing new block)
	t.Run("new writes visible to RO readers", func(t *testing.T) {
		newBlock := testutil.DefaultBlock()
		newBlock.Number = 9999
		tdb.InsertBlock(t, newBlock)

		var count int
		roDb.QueryRow("SELECT COUNT(*) FROM blocks WHERE chain_id = ? AND number = 9999", testutil.DefaultChainID).Scan(&count)
		if count != 1 {
			t.Error("new block not visible to RO reader")
		}
	})

	_ = blocks
	_ = addrs
}

// =====================================================================
// ENVIRONMENT-BASED INTEGRATION TEST
// =====================================================================

func TestWithRealIndexerDB(t *testing.T) {
	dbPath := filepath.Join(os.Getenv("HOME"), ".lux", "indexer", "cchain", "query", "indexer.db")
	if envDB := os.Getenv("EXPLORER_TEST_DB"); envDB != "" {
		dbPath = envDB
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skip("no indexer database found at", dbPath)
	}

	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL&cache=shared", dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	t.Run("schema exists", func(t *testing.T) {
		tables := []string{"blocks", "transactions", "logs", "addresses", "tokens"}
		for _, table := range tables {
			var count int
			err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
			if err != nil {
				t.Errorf("table %s: %v", table, err)
			} else {
				t.Logf("table %s: %d rows", table, count)
			}
		}
	})

	t.Run("blocks are indexed", func(t *testing.T) {
		var maxBlock int64
		db.QueryRow("SELECT COALESCE(MAX(number), 0) FROM blocks").Scan(&maxBlock)
		t.Logf("max block: %d", maxBlock)
		if maxBlock == 0 {
			t.Skip("no blocks indexed yet")
		}
	})
}

// ---- Helpers ----

func queryTxResponse(t *testing.T, p *plugin, hash []byte) map[string]any {
	t.Helper()
	rows, err := p.db.Query("SELECT * FROM transactions WHERE chain_id = ? AND hash = ? LIMIT 1",
		p.config.ChainID, hash)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	maps, _ := scanMaps(rows)
	if len(maps) == 0 {
		t.Fatal("tx not found")
	}
	return formatTx(maps[0])
}
