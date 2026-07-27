package explorer

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// A zero that means "we could not fetch this" is the defect class these
// tests pin down. Every case here reproduces a figure explore.lux.network
// printed as 0 while the chain said otherwise.
//
// The fixtures in testutil build the Blockscout-legacy schema. Production
// runs the luxfi/indexer `evm_*` schema, which spells several columns
// differently — and that difference is precisely what the API got wrong,
// so these tests build the evm_* shape directly.

func newEVMDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "indexer.db")
	db, err := sql.Open("sqlite3", "file:"+path+"?_journal_mode=WAL")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE evm_blocks (number INTEGER PRIMARY KEY, hash TEXT, parent_hash TEXT, nonce TEXT,
			miner TEXT, difficulty TEXT, total_difficulty TEXT, size INTEGER, gas_limit INTEGER,
			gas_used INTEGER, base_fee TEXT, timestamp TIMESTAMP, tx_count INTEGER)`,
		`CREATE TABLE evm_transactions (hash TEXT PRIMARY KEY, block_hash TEXT, block_number INTEGER,
			tx_index INTEGER, from_addr TEXT, to_addr TEXT, value TEXT, gas INTEGER, gas_price TEXT,
			gas_used INTEGER, nonce INTEGER, input TEXT, status INTEGER, contract_addr TEXT, timestamp TIMESTAMP)`,
		// The column is tx_count here, transactions_count in Blockscout-legacy.
		`CREATE TABLE evm_addresses (hash TEXT PRIMARY KEY, balance TEXT DEFAULT '0', tx_count INTEGER DEFAULT 0,
			is_contract BOOLEAN DEFAULT false, code TEXT DEFAULT '', creator TEXT DEFAULT '', creation_tx TEXT DEFAULT '')`,
		`CREATE TABLE evm_tokens (address TEXT PRIMARY KEY, name TEXT, symbol TEXT, decimals INTEGER,
			total_supply TEXT, token_type TEXT, holder_count INTEGER DEFAULT 0, tx_count INTEGER DEFAULT 0)`,
		`CREATE TABLE evm_token_transfers (id TEXT PRIMARY KEY, tx_hash TEXT, log_index INTEGER,
			block_number INTEGER, token_address TEXT, token_type TEXT, from_addr TEXT, to_addr TEXT,
			value TEXT, token_id TEXT, timestamp TIMESTAMP)`,
		`CREATE TABLE evm_token_balances (token_address TEXT, address TEXT, token_id TEXT DEFAULT '',
			value TEXT DEFAULT '0', token_type TEXT DEFAULT '', PRIMARY KEY (token_address, address, token_id))`,
		`CREATE TABLE evm_logs (id TEXT PRIMARY KEY, tx_hash TEXT, log_index INTEGER, block_number INTEGER,
			address TEXT, topic0 TEXT, topic1 TEXT, topic2 TEXT, topic3 TEXT, data TEXT, timestamp TIMESTAMP)`,
		`CREATE TABLE evm_internal_transactions (id TEXT PRIMARY KEY, tx_hash TEXT, block_number INTEGER,
			from_addr TEXT, to_addr TEXT, value TEXT, gas INTEGER, gas_used INTEGER, type TEXT, timestamp TIMESTAMP)`,

		// One block, one tx, one token, three holders (one of them zeroed out).
		`INSERT INTO evm_blocks VALUES (1098193,'0xaa','0xbb','0x00',
			'0x0100000000000000000000000000000000000000','1','0',1175,12000000,73608,'25000000000',
			datetime('now','-30 seconds'),1)`,
		`INSERT INTO evm_blocks VALUES (1098192,'0xcc','0xdd','0x00',
			'0x0100000000000000000000000000000000000000','1','0',1175,12000000,41584,'25000000000',
			datetime('now','-33 seconds'),1)`,
		`INSERT INTO evm_transactions VALUES ('0xf1','0xaa',1098193,0,
			'0x8d5081153ae1cfb41f5c932fe0b6beb7e159cf84','0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e',
			'0',3000000,'50000000001',73608,1,'0x','1','',datetime('now','-30 seconds'))`,
		`INSERT INTO evm_addresses (hash, tx_count, is_contract) VALUES
			('0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e',146,1),
			('0x8d5081153ae1cfb41f5c932fe0b6beb7e159cf84',12,0)`,
		`INSERT INTO evm_tokens (address,name,symbol,decimals,total_supply,token_type) VALUES
			('0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e','Wrapped LUX','WLUX',18,'159126795518059496352559779','ERC-20')`,
		`INSERT INTO evm_token_balances (token_address,address,token_id,value) VALUES
			('0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e','0x2e317c5ce2c3e3aa720a3bb7f366f5959d940d4c','','70996491320794132137787284'),
			('0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e','0x9011e888251ab053b7bd1cdb598db4f9ded94714','','45179014363106477917069'),
			('0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e','0x0000000000000000000000000000000000009999','','0')`,
		`INSERT INTO evm_token_transfers (id,tx_hash,log_index,block_number,token_address,token_type,from_addr,to_addr,value,token_id,timestamp) VALUES
			('t1','0xf1',0,1098193,'0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e','ERC-20',
			 '0x2e317c5ce2c3e3aa720a3bb7f366f5959d940d4c','0x9011e888251ab053b7bd1cdb598db4f9ded94714','1','',datetime('now')),
			('t2','0xf1',1,1098193,'0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e','ERC-20',
			 '0x9011e888251ab053b7bd1cdb598db4f9ded94714','0x2e317c5ce2c3e3aa720a3bb7f366f5959d940d4c','2','',datetime('now'))`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("exec %.60s…: %v", q, err)
		}
	}
	return path
}

func newEVMServer(t *testing.T, rpc string) *httptest.Server {
	t.Helper()
	srv, err := NewStandaloneServer(Config{
		IndexerDBPath: newEVMDB(t),
		ChainID:       96369,
		ChainName:     "Lux C-Chain",
		CoinSymbol:    "LUX",
		RPCEndpoint:   rpc,
	})
	if err != nil {
		t.Fatalf("NewStandaloneServer: %v", err)
	}
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func getObj(t *testing.T, ts *httptest.Server, path string) map[string]any {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", path, resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("GET %s: decode: %v", path, err)
	}
	return out
}

func getList(t *testing.T, ts *httptest.Server, path string) []map[string]any {
	t.Helper()
	raw, ok := getObj(t, ts, path)["items"].([]any)
	if !ok {
		t.Fatalf("GET %s: no items array", path)
	}
	out := make([]map[string]any, len(raw))
	for i, v := range raw {
		out[i] = v.(map[string]any)
	}
	return out
}

// /addresses answered {"items":[]} in production while /stats reported 72
// addresses: the ORDER BY named a column the evm_* schema does not have and
// the error was swallowed into an empty page.
func TestListAddrs_NotEmptyOnEVMSchema(t *testing.T) {
	ts := newEVMServer(t, "")
	got := getList(t, ts, "/v1/explorer/addresses")
	if len(got) != 2 {
		t.Fatalf("want 2 addresses, got %d — an empty page here is the /addresses vs /stats contradiction", len(got))
	}
	if got[0]["hash"] != "0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e" {
		t.Errorf("want the busiest address first, got %v", got[0]["hash"])
	}
}

// Holders and transfers are counted from the rows that hold them. The
// evm_tokens.holder_count column is never written, so reading it printed
// "Holders 0" directly above a populated holder list.
func TestTokenCounters_CountedFromRows(t *testing.T) {
	ts := newEVMServer(t, "")
	body := getObj(t, ts, "/v1/explorer/tokens/0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e/counters")
	if body["token_holders_count"] != "2" {
		t.Errorf("want 2 holders (the zero-balance row does not count), got %v", body["token_holders_count"])
	}
	if body["transfers_count"] != "2" {
		t.Errorf("want 2 transfers, got %v", body["transfers_count"])
	}
}

func TestTokenList_HoldersNotZero(t *testing.T) {
	ts := newEVMServer(t, "")
	got := getList(t, ts, "/v1/explorer/tokens")
	if len(got) != 1 {
		t.Fatalf("want 1 token, got %d", len(got))
	}
	if got[0]["holders"] != "2" {
		t.Errorf("want holders=2, got %v", got[0]["holders"])
	}
}

// Lux credits the whole fee to the block coinbase; it does not burn the base
// fee. Reporting gas_used × base_fee as "burnt" made every block show Burnt
// fees == Txn fees, which the SPA renders as a 100% bar.
func TestBlocks_NoFabricatedBurn(t *testing.T) {
	ts := newEVMServer(t, "")
	got := getList(t, ts, "/v1/explorer/blocks")
	if len(got) == 0 {
		t.Fatal("want blocks")
	}
	if got[0]["burnt_fees"] != "0" {
		t.Errorf("want burnt_fees 0 — Lux burns nothing today; got %v", got[0]["burnt_fees"])
	}
}

// No stored balance means unknown, and unknown is null. It is emphatically
// not zero: WLUX reads "0" on a contract holding 159 million LUX.
func TestAddress_UnknownBalanceIsNull(t *testing.T) {
	ts := newEVMServer(t, "")
	body := getObj(t, ts, "/v1/explorer/addresses/0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e")
	if body["coin_balance"] != nil {
		t.Errorf("want null coin_balance with no node to ask, got %v", body["coin_balance"])
	}
}

// With a node to ask, the balance is the node's answer.
func TestAddress_BalanceComesFromChain(t *testing.T) {
	const wei = "159126795518059496352559779"
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch []struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Errorf("node: decode batch: %v", err)
			return
		}
		out := make([]map[string]any, 0, len(batch))
		for _, req := range batch {
			if req.Method != "eth_getBalance" {
				t.Errorf("node: unexpected method %q", req.Method)
			}
			out = append(out, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": "0x83a068debcb34161e8b2a3"})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer node.Close()

	ts := newEVMServer(t, node.URL)
	body := getObj(t, ts, "/v1/explorer/addresses/0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e")
	if body["coin_balance"] != wei {
		t.Errorf("want %s wei from the node, got %v", wei, body["coin_balance"])
	}
	if body["balance"] != wei {
		t.Errorf("balance alias should match coin_balance, got %v", body["balance"])
	}

	// And the whole list in one round trip.
	for _, a := range getList(t, ts, "/v1/explorer/addresses") {
		if a["coin_balance"] != wei {
			t.Errorf("list balance for %v: got %v", a["hash"], a["coin_balance"])
		}
	}
}

// average_block_time was structurally 0: subtracting two TEXT datetimes in
// SQLite yields 0, so the SPA printed blocks as arriving instantaneously.
func TestStats_BlockTimeAndTodayAreReal(t *testing.T) {
	ts := newEVMServer(t, "")
	body := getObj(t, ts, "/v1/explorer/stats")
	if bt, _ := body["average_block_time"].(float64); bt <= 0 {
		t.Errorf("want a positive average block time from two blocks 3s apart, got %v", body["average_block_time"])
	}
	if body["gas_used_today"] == "0" {
		t.Errorf("want today's gas from blocks minted seconds ago, got %v", body["gas_used_today"])
	}
	if body["transactions_today"] != "1" {
		t.Errorf("want 1 transaction today, got %v", body["transactions_today"])
	}
}

// detectColumn asks the database instead of guessing, so a name that is not
// there is reported as absent rather than exploding inside a swallowed query.
func TestDetectColumn(t *testing.T) {
	srv, err := NewStandaloneServer(Config{IndexerDBPath: newEVMDB(t), ChainID: 96369})
	if err != nil {
		t.Fatalf("NewStandaloneServer: %v", err)
	}
	defer srv.Close()

	for _, tc := range []struct {
		table string
		names []string
		want  string
	}{
		{"evm_addresses", []string{"tx_count", "transactions_count"}, "tx_count"},
		{"evm_addresses", []string{"transactions_count", "tx_count"}, "tx_count"},
		{"evm_addresses", []string{"nope"}, ""},
		{"", []string{"tx_count"}, ""},
		{"no_such_table", []string{"tx_count"}, ""},
	} {
		if got := srv.detectColumn(tc.table, tc.names...); got != tc.want {
			t.Errorf("detectColumn(%q, %v) = %q, want %q", tc.table, tc.names, got, tc.want)
		}
	}
	if srv.t.addrTxCol != "tx_count" {
		t.Errorf("addrTxCol = %q, want tx_count", srv.t.addrTxCol)
	}
	if srv.t.balTokenCol != "token_address" {
		t.Errorf("balTokenCol = %q, want token_address", srv.t.balTokenCol)
	}
}

