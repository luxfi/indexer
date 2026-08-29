package explorer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// The Lux Genesis collection as it stands on C-Chain: three items, minted to
// two addresses, ids 0, 1 and 2. Rows are exactly what the indexer wrote for
// blocks 1095748-1095750, ids in the 32-byte form the chain encoded them in.
//
// These tests pin the two things a collection page cannot exist without: a
// transfer that says which item moved, and a list of the items themselves.
func genesisDB(t *testing.T, withInstances bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "indexer.db")
	db, err := sql.Open("sqlite3", "file:"+path+"?_journal_mode=WAL")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	const nft = "0x9e04fc57c20b2ee45627c4aa280eb471f2ca6ea5"
	id := func(n int) string { return fmt.Sprintf("0x%064x", n) }

	stmts := []string{
		`CREATE TABLE evm_blocks (number INTEGER PRIMARY KEY, hash TEXT, parent_hash TEXT, nonce TEXT,
			miner TEXT, difficulty TEXT, total_difficulty TEXT, size INTEGER, gas_limit INTEGER,
			gas_used INTEGER, base_fee TEXT, timestamp TIMESTAMP, tx_count INTEGER)`,
		`CREATE TABLE evm_transactions (hash TEXT PRIMARY KEY, block_number INTEGER, timestamp TIMESTAMP)`,
		`CREATE TABLE evm_addresses (hash TEXT PRIMARY KEY, tx_count INTEGER DEFAULT 0)`,
		`CREATE TABLE evm_tokens (address TEXT PRIMARY KEY, name TEXT, symbol TEXT, decimals INTEGER,
			total_supply TEXT, token_type TEXT, holder_count INTEGER DEFAULT 0)`,
		`CREATE TABLE evm_token_transfers (id TEXT PRIMARY KEY, tx_hash TEXT, log_index INTEGER,
			block_number INTEGER, token_address TEXT, token_type TEXT, from_addr TEXT, to_addr TEXT,
			value TEXT, token_id TEXT, timestamp TIMESTAMP)`,
		`CREATE TABLE evm_token_balances (token_address TEXT, address TEXT, token_id TEXT DEFAULT '',
			value TEXT DEFAULT '0', token_type TEXT DEFAULT '', PRIMARY KEY (token_address, address, token_id))`,
		`CREATE TABLE evm_logs (id TEXT PRIMARY KEY, address TEXT, block_number INTEGER)`,
		fmt.Sprintf(`INSERT INTO evm_tokens VALUES ('%s','Lux Genesis','GENESIS',0,'3','ERC-721',3)`, nft),
	}
	if withInstances {
		stmts = append(stmts,
			`CREATE TABLE evm_token_instances (token_address TEXT, token_id TEXT, uri TEXT DEFAULT '',
				uri_state TEXT DEFAULT '', PRIMARY KEY (token_address, token_id))`,
			// Read from the contract: it concatenates its base onto a value
			// that is already a full URL. Recorded as the chain gave it.
			fmt.Sprintf(`INSERT INTO evm_token_instances VALUES
				('%s','%s','https://lux.town/nfts/https://lux.town/nfts/validator.mov','ok'),
				('%s','%s','https://lux.town/nfts/https://lux.town/nfts/validator.mov','ok')`,
				nft, id(0), nft, id(1)),
		)
	}
	mints := []struct {
		tx    string
		block int
		to    string
		id    int
	}{
		{"0xac5b05e51c38ebfec88a743a0f2815d5082da105a679035ecea73a07e80717e3", 1095748, "0x55be906ece0552797752e2894d9d7ef952414def", 0},
		{"0x10f128e73ab9a1c0fedc9b459870a7cc5e8f45c2ee2a384f13be5ed7e3dbbb8a", 1095749, "0x55be906ece0552797752e2894d9d7ef952414def", 1},
		{"0xb5a96c04e0b9f8520bc6992d1e98e409eee0ca82f8f28460d304843a02ccff8d", 1095750, "0x67bd7c7c3dbc53b8bde7d9888f90961d7af3d1a6", 2},
	}
	for _, m := range mints {
		stmts = append(stmts,
			fmt.Sprintf(`INSERT INTO evm_token_transfers VALUES ('%s-0','%s',0,%d,'%s','ERC-721',
				'0x0000000000000000000000000000000000000000','%s','1','%s',datetime('now'))`,
				m.tx, m.tx, m.block, nft, m.to, id(m.id)),
			fmt.Sprintf(`INSERT INTO evm_token_balances VALUES ('%s','%s','%s','1','ERC-721')`, nft, m.to, id(m.id)),
		)
	}

	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec %.60s: %v", s, err)
		}
	}
	return path
}

func genesisServer(t *testing.T, withInstances bool) *httptest.Server {
	t.Helper()
	srv, err := NewStandaloneServer(Config{
		IndexerDBPath: genesisDB(t, withInstances),
		ChainID:       96369,
		ChainName:     "Lux C-Chain",
		CoinSymbol:    "LUX",
	})
	if err != nil {
		t.Fatalf("NewStandaloneServer: %v", err)
	}
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func fetch(t *testing.T, ts *httptest.Server, path string) (map[string]any, int) {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return out, resp.StatusCode
}

func rows(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("no items array in %v", body)
	}
	out := make([]map[string]any, len(raw))
	for i, v := range raw {
		out[i] = v.(map[string]any)
	}
	return out
}

const genesisAddr = "0x9e04fc57c20b2ee45627c4aa280eb471f2ca6ea5"

// Three ERC-721 movements are three different items. A response that gives
// them all the same payload is a feed of "NFT #?" rows.
func TestTransfersCarryTheItemID(t *testing.T) {
	ts := genesisServer(t, true)
	got := rows(t, mustGet(t, ts, "/v1/explorer/token-transfers"))
	if len(got) != 3 {
		t.Fatalf("got %d transfers, want 3", len(got))
	}
	seen := map[string]bool{}
	for _, row := range got {
		total, ok := row["total"].(map[string]any)
		if !ok {
			t.Fatalf("no total on %v", row)
		}
		id, ok := total["token_id"].(string)
		if !ok || id == "" {
			t.Fatalf("token_id missing from %v — the item id is being dropped", total)
		}
		if seen[id] {
			t.Errorf("id %q appears twice; every mint moved a different item", id)
		}
		seen[id] = true
		// An ERC-721 payload names the item, not an amount.
		if _, ok := total["value"]; ok {
			t.Errorf("ERC-721 total should carry no amount, got %v", total)
		}
	}
	for _, want := range []string{"0", "1", "2"} {
		if !seen[want] {
			t.Errorf("id %q missing; chain says the collection minted 0, 1 and 2", want)
		}
	}
}

// Every item of the collection, each owned by whoever received it last.
func TestInstancesListTheCollection(t *testing.T) {
	ts := genesisServer(t, true)
	got := rows(t, mustGet(t, ts, "/v1/explorer/tokens/"+genesisAddr+"/instances"))
	if len(got) != 3 {
		t.Fatalf("got %d items, want the 3 the chain minted", len(got))
	}
	wantOwner := map[string]string{
		"0": "0x55be906ece0552797752e2894d9d7ef952414def",
		"1": "0x55be906ece0552797752e2894d9d7ef952414def",
		"2": "0x67bd7c7c3dbc53b8bde7d9888f90961d7af3d1a6",
	}
	for _, it := range got {
		id, _ := it["id"].(string)
		owner, ok := it["owner"].(map[string]any)
		if !ok {
			t.Fatalf("item %q has no owner", id)
		}
		if got, want := owner["hash"], wantOwner[id]; got != want {
			t.Errorf("item %q owner %v, want %v", id, got, want)
		}
		if it["is_unique"] != true {
			t.Errorf("item %q: an ERC-721 id is held by one address", id)
		}
		// We store the URI and never fetch what is behind it, so there is
		// no image to name and claiming one would be inventing it.
		if it["image_url"] != nil || it["metadata"] != nil {
			t.Errorf("item %q asserts media it never fetched: %v", id, it)
		}
	}
}

// An item we read a URI for, an item we did not, and the difference between
// them stated rather than blurred into one silent blank.
func TestURIStateSaysWhichKindOfNothing(t *testing.T) {
	ts := genesisServer(t, true)
	state := map[string]any{}
	uri := map[string]any{}
	for _, it := range rows(t, mustGet(t, ts, "/v1/explorer/tokens/"+genesisAddr+"/instances")) {
		id, _ := it["id"].(string)
		state[id] = it["uri_state"]
		uri[id] = it["uri"]
	}
	if state["0"] != "ok" || uri["0"] == nil {
		t.Errorf("item 0 was read from the contract: state %v uri %v", state["0"], uri["0"])
	}
	if state["2"] != "unread" {
		t.Errorf("item 2 was never asked about, want state unread, got %v", state["2"])
	}
	if uri["2"] != nil {
		t.Errorf("item 2 has no URI on record, got %v", uri["2"])
	}
}

// A store with no instances table still lists items; it just has no URI for
// any of them. Missing machinery is not the same as an empty collection.
func TestInstancesListWithoutTheURITable(t *testing.T) {
	ts := genesisServer(t, false)
	got := rows(t, mustGet(t, ts, "/v1/explorer/tokens/"+genesisAddr+"/instances"))
	if len(got) != 3 {
		t.Fatalf("got %d items, want 3", len(got))
	}
	for _, it := range got {
		if it["uri_state"] != "unread" {
			t.Errorf("item %v: want unread, got %v", it["id"], it["uri_state"])
		}
	}
}

// One item by its own id, which is the page a marketplace links to.
func TestOneItemByID(t *testing.T) {
	ts := genesisServer(t, true)
	body, code := fetch(t, ts, "/v1/explorer/tokens/"+genesisAddr+"/instances/2")
	if code != 200 {
		t.Fatalf("status %d, want 200", code)
	}
	if body["id"] != "2" {
		t.Errorf("id %v, want 2", body["id"])
	}
	owner, _ := body["owner"].(map[string]any)
	if owner["hash"] != "0x67bd7c7c3dbc53b8bde7d9888f90961d7af3d1a6" {
		t.Errorf("owner %v", owner)
	}
	if _, code := fetch(t, ts, "/v1/explorer/tokens/"+genesisAddr+"/instances/9"); code != 404 {
		t.Errorf("id 9 was never minted, want 404, got %d", code)
	}
	if _, code := fetch(t, ts, "/v1/explorer/tokens/"+genesisAddr+"/instances/nope"); code != 400 {
		t.Errorf("a non-numeric id is not a token id, want 400, got %d", code)
	}
}

// A portfolio names the items held, not the encoding they are stored in.
func TestPortfolioIDsAreDecimal(t *testing.T) {
	ts := genesisServer(t, true)
	got := rows(t, mustGet(t, ts, "/v1/explorer/addresses/0x55be906ece0552797752e2894d9d7ef952414def/tokens"))
	if len(got) != 2 {
		t.Fatalf("got %d holdings, want 2", len(got))
	}
	for _, h := range got {
		id, _ := h["token_id"].(string)
		if id != "0" && id != "1" {
			t.Errorf("token_id %q, want a decimal id", id)
		}
	}
}

func mustGet(t *testing.T, ts *httptest.Server, path string) map[string]any {
	t.Helper()
	body, code := fetch(t, ts, path)
	if code != 200 {
		t.Fatalf("GET %s: status %d", path, code)
	}
	return body
}
