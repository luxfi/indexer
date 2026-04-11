package explorer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestStandaloneE2E tests the standalone server against a real indexer database.
// Set EXPLORER_TEST_DB to the path of a populated SQLite database.
// If not set, tests are skipped.
func TestStandaloneE2E(t *testing.T) {
	dbPath := os.Getenv("EXPLORER_TEST_DB")
	if dbPath == "" {
		// Try the default local path
		dbPath = "/tmp/explorer-data/default/query/indexer.db"
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			t.Skip("no indexer DB available — set EXPLORER_TEST_DB or run explorer locally first")
		}
	}

	srv, err := NewStandaloneServer(Config{
		IndexerDBPath: dbPath,
		ChainID:       96368,
		ChainName:     "Lux Testnet",
		CoinSymbol:    "LUX",
	})
	if err != nil {
		t.Fatalf("NewStandaloneServer: %v", err)
	}
	defer srv.Close()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// ---- Health ----
	t.Run("stats", func(t *testing.T) {
		resp := get(t, ts, "/v1/explorer/stats")
		if resp["total_blocks"] == nil {
			t.Error("total_blocks missing")
		}
		bc := toInt(resp["total_blocks"])
		t.Logf("stats: %d blocks, %d txs, %d addrs",
			bc, toInt(resp["total_transactions"]), toInt(resp["total_addresses"]))
		if bc == 0 {
			t.Error("expected at least 1 block")
		}
	})

	// ---- Blocks ----
	t.Run("list blocks", func(t *testing.T) {
		resp := getPage(t, ts, "/v1/explorer/blocks?items_count=5")
		items := resp["items"].([]any)
		if len(items) == 0 {
			t.Fatal("no blocks returned")
		}
		t.Logf("got %d blocks", len(items))

		first := items[0].(map[string]any)
		assertField(t, first, "height")
		assertField(t, first, "hash")
		assertField(t, first, "miner")
		assertField(t, first, "timestamp")
		assertField(t, first, "type")

		// Miner should be an object with hash
		miner := first["miner"]
		if m, ok := miner.(map[string]any); ok {
			assertField(t, m, "hash")
		} else {
			t.Errorf("miner should be {hash:...}, got %T", miner)
		}
	})

	t.Run("get block by number", func(t *testing.T) {
		resp := get(t, ts, "/v1/explorer/blocks/0")
		assertField(t, resp, "height")
		assertField(t, resp, "hash")
		if toInt(resp["height"]) != 0 {
			t.Errorf("expected block 0, got %v", resp["height"])
		}
	})

	t.Run("get block by hash", func(t *testing.T) {
		// Get block 0's hash first
		b0 := get(t, ts, "/v1/explorer/blocks/0")
		hash := b0["hash"].(string)
		if hash == "" {
			t.Skip("block 0 has no hash")
		}

		resp := get(t, ts, "/v1/explorer/blocks/"+hash)
		if resp["hash"] != hash {
			t.Errorf("hash mismatch: got %v, want %v", resp["hash"], hash)
		}
	})

	// ---- Transactions ----
	t.Run("list transactions", func(t *testing.T) {
		resp := getPage(t, ts, "/v1/explorer/transactions?items_count=3")
		items := resp["items"].([]any)
		t.Logf("got %d transactions", len(items))

		if len(items) > 0 {
			first := items[0].(map[string]any)
			assertField(t, first, "hash")
			assertField(t, first, "block_number")
			assertField(t, first, "from")
			assertField(t, first, "status")
			assertField(t, first, "timestamp")

			// From should be {hash:...}
			from := first["from"]
			if f, ok := from.(map[string]any); ok {
				assertField(t, f, "hash")
			}
		}
	})

	t.Run("get transaction by hash", func(t *testing.T) {
		page := getPage(t, ts, "/v1/explorer/transactions?items_count=1")
		items := page["items"].([]any)
		if len(items) == 0 {
			t.Skip("no transactions to test")
		}
		txHash := items[0].(map[string]any)["hash"].(string)

		resp := get(t, ts, "/v1/explorer/transactions/"+txHash)
		assertField(t, resp, "hash")
		assertField(t, resp, "value")
		assertField(t, resp, "gas_limit")
	})

	// ---- Search ----
	t.Run("search by block number", func(t *testing.T) {
		resp := getPage(t, ts, "/v1/explorer/search?q=0")
		items := resp["items"].([]any)
		found := false
		for _, item := range items {
			m := item.(map[string]any)
			if m["type"] == "block" {
				found = true
			}
		}
		if !found {
			t.Error("search for '0' should find block 0")
		}
	})

	// ---- Pagination ----
	t.Run("pagination respects items_count", func(t *testing.T) {
		resp := getPage(t, ts, "/v1/explorer/blocks?items_count=2")
		items := resp["items"].([]any)
		if len(items) > 2 {
			t.Errorf("expected at most 2 items, got %d", len(items))
		}
	})

	t.Run("items_count capped at 250", func(t *testing.T) {
		resp := getPage(t, ts, "/v1/explorer/blocks?items_count=999999")
		items := resp["items"].([]any)
		if len(items) > 250 {
			t.Errorf("expected at most 250 items, got %d", len(items))
		}
	})

	// ---- Block transactions ----
	t.Run("block transactions", func(t *testing.T) {
		resp := getPage(t, ts, "/v1/explorer/blocks/0/transactions")
		items := resp["items"].([]any)
		t.Logf("block 0 has %d transactions", len(items))
	})

	// ---- 404 handling ----
	t.Run("nonexistent block returns 404", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/v1/explorer/blocks/99999999")
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 404 {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("nonexistent tx returns 404", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/v1/explorer/transactions/0x0000000000000000000000000000000000000000000000000000000000000000")
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 404 {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	// ---- Response format ----
	t.Run("CORS headers present", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/v1/explorer/stats")
		if err != nil {
			t.Fatal(err)
		}
		if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
			t.Error("CORS header missing")
		}
		if resp.Header.Get("Content-Type") != "application/json" {
			t.Error("Content-Type should be application/json")
		}
	})

	t.Run("paginated response has items and next_page_params", func(t *testing.T) {
		resp := getPage(t, ts, "/v1/explorer/blocks?items_count=1")
		if _, ok := resp["items"]; !ok {
			t.Error("missing items field")
		}
		// next_page_params can be null or an object
		if _, ok := resp["next_page_params"]; !ok {
			t.Error("missing next_page_params field")
		}
	})
}

// ---- Test Helpers ----

func get(t *testing.T, ts *httptest.Server, path string) map[string]any {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s: status %d", path, resp.StatusCode)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

func getPage(t *testing.T, ts *httptest.Server, path string) map[string]any {
	t.Helper()
	return get(t, ts, path)
}

func assertField(t *testing.T, m map[string]any, field string) {
	t.Helper()
	if _, ok := m[field]; !ok {
		t.Errorf("missing field: %s (keys: %v)", field, keys(m))
	}
}

func keys(m map[string]any) []string {
	k := make([]string, 0, len(m))
	for key := range m {
		k = append(k, key)
	}
	return k
}

// toInt is defined in tokenscore.go (same package)

// Required for the format package to be importable
var _ = fmt.Sprintf
var _ = time.Second
