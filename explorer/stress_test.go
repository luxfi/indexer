package explorer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luxfi/explorer/explorer/testutil"
)

// ==========================================================================
// 1. CONCURRENCY STRESS TESTS
// ==========================================================================

func TestConcurrent100Requests(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	const goroutines = 100
	const requestsPerG = 10

	endpoints := []string{
		"/v1/explorer/blocks",
		"/v1/explorer/transactions",
		"/v1/explorer/addresses",
		"/v1/explorer/tokens",
		"/v1/explorer/stats",
		"/v1/explorer/search?q=0",
		"/v1/explorer/blocks/0",
		"/v1/explorer/blocks?items_count=2",
		"/v1/explorer/transactions?items_count=3",
	}

	var wg sync.WaitGroup
	var failures atomic.Int64

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < requestsPerG; j++ {
				ep := endpoints[(id+j)%len(endpoints)]
				resp, err := http.Get(ts.URL + ep)
				if err != nil {
					failures.Add(1)
					t.Errorf("goroutine %d req %d: GET %s: %v", id, j, ep, err)
					continue
				}
				if resp.StatusCode != 200 {
					failures.Add(1)
					t.Errorf("goroutine %d req %d: GET %s: status %d", id, j, ep, resp.StatusCode)
				}
				var body map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					failures.Add(1)
					t.Errorf("goroutine %d req %d: GET %s: invalid JSON: %v", id, j, ep, err)
				}
				resp.Body.Close()
			}
		}(i)
	}

	wg.Wait()
	if f := failures.Load(); f > 0 {
		t.Errorf("%d/%d total requests failed", f, goroutines*requestsPerG)
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	ctx := t
	done := make(chan struct{})
	time.AfterFunc(3*time.Second, func() { close(done) })

	// Writer goroutine: insert blocks into DB.
	var written atomic.Int64
	go func() {
		for i := int64(1000); ; i++ {
			select {
			case <-done:
				return
			default:
			}
			b := testutil.DefaultBlock()
			b.Number = i
			insertBlock(ctx, tdb, b)
			written.Add(1)
		}
	}()

	// 10 reader goroutines.
	var wg sync.WaitGroup
	var reads atomic.Int64
	var readErrors atomic.Int64

	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				resp, err := http.Get(ts.URL + "/v1/explorer/blocks")
				if err != nil {
					readErrors.Add(1)
					continue
				}
				var body map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					readErrors.Add(1)
				}
				resp.Body.Close()
				reads.Add(1)
			}
		}()
	}

	wg.Wait()
	t.Logf("written=%d, reads=%d, read_errors=%d", written.Load(), reads.Load(), readErrors.Load())
	if readErrors.Load() > 0 {
		t.Errorf("%d read errors during concurrent read/write", readErrors.Load())
	}
}

func TestConcurrentMixedEndpoints(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	txHash := hexStr(sd.txs[0].Hash)
	addrHash := hexStr(sd.eoaAddr.Hash)
	tokenAddr := hexStr(sd.erc20Token.ContractAddress)
	contractAddr := hexStr(sd.contracts[0].AddressHash)

	endpoints := []string{
		"/v1/explorer/blocks",
		"/v1/explorer/blocks/0",
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
		"/v1/explorer/search?q=" + txHash,
		"/v1/explorer/stats",
	}

	var wg sync.WaitGroup
	var failures atomic.Int64

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for _, ep := range endpoints {
				resp, err := http.Get(ts.URL + ep)
				if err != nil {
					failures.Add(1)
					continue
				}
				if resp.StatusCode != 200 {
					failures.Add(1)
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}(i)
	}

	wg.Wait()
	if f := failures.Load(); f > 0 {
		t.Errorf("%d failures across %d goroutines * %d endpoints", f, 50, len(endpoints))
	}
}

// ==========================================================================
// 2. EDGE CASE TESTS
// ==========================================================================

func TestEmptyDatabase_AllEndpoints(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	_, ts := newServer(t, tdb.Path)

	listEndpoints := []string{
		"/v1/explorer/blocks",
		"/v1/explorer/transactions",
		"/v1/explorer/addresses",
		"/v1/explorer/tokens",
		"/v1/explorer/search?q=test",
		"/v1/explorer/stats",
		"/v1/explorer/homepage/blocks",
		"/v1/explorer/homepage/transactions",
		"/v1/explorer/homepage/indexing-status",
		"/v1/explorer/config/backend-version",
		"/v1/explorer/config/backend",
		"/v1/explorer/stats/charts/transactions",
		"/v1/explorer/stats/charts/market",
	}

	for _, ep := range listEndpoints {
		t.Run(ep, func(t *testing.T) {
			resp, err := http.Get(ts.URL + ep)
			if err != nil {
				t.Fatalf("GET %s: %v", ep, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Errorf("GET %s: want 200, got %d", ep, resp.StatusCode)
			}
			ct := resp.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "application/json") && !strings.HasPrefix(ct, "text/csv") {
				t.Errorf("GET %s: Content-Type=%q", ep, ct)
			}
			// Must decode as valid JSON.
			var body any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Errorf("GET %s: invalid JSON: %v", ep, err)
			}
		})
	}
}

func TestEmptyDatabase_NotFoundEndpoints(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	_, ts := newServer(t, tdb.Path)

	notFoundEndpoints := []string{
		"/v1/explorer/blocks/0",
		"/v1/explorer/blocks/0x0000000000000000000000000000000000000000000000000000000000000000",
		"/v1/explorer/transactions/0x0000000000000000000000000000000000000000000000000000000000000000",
		"/v1/explorer/addresses/0x0000000000000000000000000000000000000000",
		"/v1/explorer/tokens/0x0000000000000000000000000000000000000000",
		"/v1/explorer/smart-contracts/0x0000000000000000000000000000000000000000",
	}

	for _, ep := range notFoundEndpoints {
		t.Run(ep, func(t *testing.T) {
			resp, err := http.Get(ts.URL + ep)
			if err != nil {
				t.Fatalf("GET %s: %v", ep, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 404 {
				t.Errorf("GET %s: want 404, got %d", ep, resp.StatusCode)
			}
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Errorf("GET %s: invalid JSON in 404: %v", ep, err)
			}
			if body["error"] == nil {
				t.Errorf("GET %s: 404 missing error field", ep)
			}
		})
	}
}

func TestMaxPagination(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	cases := []struct {
		name  string
		query string
	}{
		{"zero", "items_count=0"},
		{"negative", "items_count=-1"},
		{"huge", "items_count=999999"},
		{"max_int32", "items_count=2147483647"},
		{"nan", "items_count=NaN"},
		{"alpha", "items_count=abc"},
		{"float", "items_count=1.5"},
		{"empty", "items_count="},
		{"negative_big", "items_count=-999999"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/v1/explorer/blocks?" + tc.query)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Errorf("want 200, got %d", resp.StatusCode)
			}
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Errorf("invalid JSON: %v", err)
			}
		})
	}
}

func TestMalformedBlockID(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	malformed := []string{
		"0x",
		"0xZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ",
		"",
		"null",
		"-1",
		"9999999999999999999999999999999",
		"0x123", // too short
		"abc",
		"true",
		"undefined",
	}

	for _, id := range malformed {
		t.Run("block/"+id, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/v1/explorer/blocks/" + id)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			// Should be 200 or 404, never 500.
			if resp.StatusCode == 500 {
				t.Errorf("block/%s returned 500", id)
			}
			var body any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Errorf("block/%s: invalid JSON: %v", id, err)
			}
		})
	}
}

func TestMalformedTxHash(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	malformed := []string{
		"0x",
		"0xZZZ",
		"",
		"null",
		"-1",
		"0x" + strings.Repeat("f", 128), // too long
		"0x" + strings.Repeat("f", 10),  // too short
		"not-a-hash",
	}

	for _, h := range malformed {
		t.Run("tx/"+h, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/v1/explorer/transactions/" + h)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode == 500 {
				t.Errorf("tx/%s returned 500", h)
			}
			var body any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Errorf("tx/%s: invalid JSON: %v", h, err)
			}
		})
	}
}

func TestMalformedAddress(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	malformed := []string{
		"0x",
		"0xZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ",
		"",
		"null",
		"0x" + strings.Repeat("f", 100), // too long
		"0xdeaD",                        // too short, mixed case
	}

	subresources := []string{"", "/transactions", "/counters"}

	for _, addr := range malformed {
		for _, sub := range subresources {
			path := "/v1/explorer/addresses/" + addr + sub
			t.Run(path, func(t *testing.T) {
				resp, err := http.Get(ts.URL + path)
				if err != nil {
					t.Fatalf("GET: %v", err)
				}
				defer resp.Body.Close()
				if resp.StatusCode == 500 {
					t.Errorf("%s returned 500", path)
				}
				var body any
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Errorf("%s: invalid JSON: %v", path, err)
				}
			})
		}
	}
}

func TestSpecialCharactersInSearch(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	inputs := []string{
		"%",
		"_",
		"'",
		"\"",
		";",
		"--",
		"OR 1=1",
		"' OR '1'='1",
		"'; DROP TABLE blocks; --",
		"1; DROP TABLE blocks",
		"UNION SELECT * FROM transactions",
		"' UNION SELECT hash FROM blocks --",
		"1 OR 1=1",
		"0x' OR ''='",
		"admin'--",
		"\x00",
		"null",
		"undefined",
		"<script>alert(1)</script>",
		"{{template}}",
	}

	for _, q := range inputs {
		t.Run("search/"+q, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/v1/explorer/search?q=" + url.QueryEscape(q))
			if err != nil {
				t.Skipf("GET: %v", err) // some inputs may be invalid URLs
			}
			defer resp.Body.Close()
			if resp.StatusCode == 500 {
				t.Errorf("search with %q returned 500", q)
			}
			var body any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Errorf("search with %q: invalid JSON: %v", q, err)
			}
		})
	}
}

func TestUnicodeInSearch(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	inputs := []string{
		"\u4e16\u754c",        // Chinese
		"\u0410\u0411\u0412",  // Cyrillic
		"\u202e" + "reversed", // RTL override
		"\U0001F600",          // emoji
		strings.Repeat("\u00e9", 100),
		"\u0000", // null
		"\ufffd", // replacement char
	}

	for _, q := range inputs {
		t.Run("unicode", func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/v1/explorer/search?q=" + q)
			if err != nil {
				// Some of these are not valid URL chars, which is fine.
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode == 500 {
				t.Errorf("search with unicode returned 500")
			}
		})
	}
}

func TestLongSearchQuery(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	// 10KB query string.
	long := strings.Repeat("a", 10*1024)
	resp, err := http.Get(ts.URL + "/v1/explorer/search?q=" + long)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 500 {
		t.Error("long search query returned 500")
	}
	var body any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Errorf("long search: invalid JSON: %v", err)
	}
}

// ==========================================================================
// 3. MEMORY + RESOURCE TESTS
// ==========================================================================

func TestMemoryStability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory test in short mode")
	}

	tdb := testutil.NewTestDB(t)
	// Seed 500 blocks (enough to test stability without being slow).
	for i := 0; i < 500; i++ {
		b := testutil.DefaultBlock()
		b.Number = int64(i)
		b.Timestamp = time.Now().Unix() - int64(500-i)
		b.TransactionCount = 2
		insertBlock(t, tdb, b)

		for j := 0; j < 2; j++ {
			tx := testutil.DefaultTransaction(b)
			tx.TransactionIndex = j
			insertTx(t, tdb, tx)
		}
	}

	_, ts := newServer(t, tdb.Path)

	// Force GC, measure baseline.
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// 1000 requests.
	for i := 0; i < 1000; i++ {
		ep := "/v1/explorer/blocks"
		if i%3 == 0 {
			ep = "/v1/explorer/transactions"
		} else if i%3 == 1 {
			ep = "/v1/explorer/stats"
		}
		resp, err := http.Get(ts.URL + ep)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	// Check heap growth. Allow up to 100MB growth.
	growth := int64(m2.HeapAlloc) - int64(m1.HeapAlloc)
	t.Logf("heap: before=%dMB after=%dMB growth=%dMB",
		m1.HeapAlloc/1024/1024, m2.HeapAlloc/1024/1024, growth/1024/1024)

	if growth > 100*1024*1024 {
		t.Errorf("heap grew %dMB (>100MB) over 1000 requests", growth/1024/1024)
	}
}

func TestResponseSizeBounded(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	// Seed 300 blocks, each with 5 txs.
	for i := 0; i < 300; i++ {
		b := testutil.DefaultBlock()
		b.Number = int64(i)
		b.Timestamp = time.Now().Unix() - int64(300-i)
		b.TransactionCount = 5
		insertBlock(t, tdb, b)
		for j := 0; j < 5; j++ {
			tx := testutil.DefaultTransaction(b)
			tx.TransactionIndex = j
			insertTx(t, tdb, tx)
		}
	}

	_, ts := newServer(t, tdb.Path)

	endpoints := []string{
		"/v1/explorer/blocks",
		"/v1/explorer/blocks?items_count=250",
		"/v1/explorer/transactions",
		"/v1/explorer/transactions?items_count=250",
	}

	const maxSize = 10 * 1024 * 1024 // 10MB

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			resp, err := http.Get(ts.URL + ep)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if len(body) > maxSize {
				t.Errorf("response size %d exceeds %d bytes", len(body), maxSize)
			}
		})
	}
}

// ==========================================================================
// 4. FUZZ TESTS
// ==========================================================================

func FuzzSearch(f *testing.F) {
	ts := setupFuzzServer(f)

	f.Add("block")
	f.Add("0x1234")
	f.Add("0x" + strings.Repeat("a", 64))
	f.Add("'; DROP TABLE blocks; --")
	f.Add("0")
	f.Add("")
	f.Add("99999999")
	f.Add("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	f.Add("\x00\x01\x02")
	f.Add(strings.Repeat("a", 1000))

	f.Fuzz(func(t *testing.T, query string) {
		resp, err := http.Get(ts.URL + "/v1/explorer/search?q=" + query)
		if err != nil {
			return // network error from malformed URL is acceptable
		}
		defer resp.Body.Close()
		if resp.StatusCode == 500 {
			t.Errorf("search returned 500 for query %q", query)
		}
	})
}

func FuzzBlockID(f *testing.F) {
	ts := setupFuzzServer(f)

	f.Add("0")
	f.Add("0x1234")
	f.Add("-1")
	f.Add("")
	f.Add("99999999")
	f.Add("null")
	f.Add("0x" + strings.Repeat("f", 64))

	f.Fuzz(func(t *testing.T, id string) {
		resp, err := http.Get(ts.URL + "/v1/explorer/blocks/" + id)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == 500 {
			t.Errorf("blocks/%s returned 500", id)
		}
		var body any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Errorf("blocks/%s: invalid JSON: %v", id, err)
		}
	})
}

func FuzzPagination(f *testing.F) {
	ts := setupFuzzServer(f)

	f.Add("50")
	f.Add("0")
	f.Add("-1")
	f.Add("abc")
	f.Add("999999999")
	f.Add("")
	f.Add("1.5")
	f.Add("2147483647")

	f.Fuzz(func(t *testing.T, limit string) {
		resp, err := http.Get(ts.URL + "/v1/explorer/blocks?items_count=" + limit)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == 500 {
			t.Errorf("blocks?items_count=%s returned 500", limit)
		}
		var body any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Errorf("blocks?items_count=%s: invalid JSON: %v", limit, err)
		}
	})
}

func FuzzTxHash(f *testing.F) {
	ts := setupFuzzServer(f)

	f.Add("0x" + strings.Repeat("0", 64))
	f.Add("0x")
	f.Add("")
	f.Add("0xdeadbeef")
	f.Add("not-a-hash")

	f.Fuzz(func(t *testing.T, hash string) {
		resp, err := http.Get(ts.URL + "/v1/explorer/transactions/" + hash)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == 500 {
			t.Errorf("tx/%s returned 500", hash)
		}
	})
}

func FuzzAddress(f *testing.F) {
	ts := setupFuzzServer(f)

	f.Add("0x" + strings.Repeat("0", 40))
	f.Add("0x")
	f.Add("")
	f.Add("not-an-address")

	f.Fuzz(func(t *testing.T, addr string) {
		resp, err := http.Get(ts.URL + "/v1/explorer/addresses/" + addr)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == 500 {
			t.Errorf("addr/%s returned 500", addr)
		}
	})
}

// ==========================================================================
// 5. API CONTRACT TESTS
// ==========================================================================

func TestAllEndpointsReturnJSON(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	txHash := hexStr(sd.txs[0].Hash)
	addrHash := hexStr(sd.eoaAddr.Hash)
	tokenAddr := hexStr(sd.erc20Token.ContractAddress)
	contractAddr := hexStr(sd.contracts[0].AddressHash)

	// Every registered GET route that returns JSON.
	endpoints := []string{
		"/v1/explorer/blocks",
		"/v1/explorer/blocks/0",
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
		"/v1/explorer/addresses/" + addrHash + "/token-transfers",
		"/v1/explorer/addresses/" + addrHash + "/internal-transactions",
		"/v1/explorer/addresses/" + addrHash + "/logs",
		"/v1/explorer/addresses/" + addrHash + "/tokens",
		"/v1/explorer/addresses/" + addrHash + "/coin-balance-history",
		"/v1/explorer/addresses/" + addrHash + "/tabs-counters",
		"/v1/explorer/tokens",
		"/v1/explorer/tokens/" + tokenAddr,
		"/v1/explorer/tokens/" + tokenAddr + "/holders",
		"/v1/explorer/tokens/" + tokenAddr + "/transfers",
		"/v1/explorer/tokens/" + tokenAddr + "/instances",
		"/v1/explorer/tokens/" + tokenAddr + "/counters",
		"/v1/explorer/smart-contracts/" + contractAddr,
		"/v1/explorer/smart-contracts",
		"/v1/explorer/search?q=0",
		"/v1/explorer/search/quick?q=0",
		"/v1/explorer/search/check-redirect?q=" + txHash,
		"/v1/explorer/stats",
		"/v1/explorer/stats/charts/transactions",
		"/v1/explorer/stats/charts/market",
		"/v1/explorer/homepage/blocks",
		"/v1/explorer/homepage/transactions",
		"/v1/explorer/homepage/indexing-status",
		"/v1/explorer/config/backend-version",
		"/v1/explorer/config/backend",
		"/v1/explorer/token-transfers",
		"/v1/explorer/internal-transactions",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			resp, err := http.Get(ts.URL + ep)
			if err != nil {
				t.Fatalf("GET %s: %v", ep, err)
			}
			defer resp.Body.Close()

			// Must not be 500.
			if resp.StatusCode == 500 {
				t.Errorf("GET %s: returned 500", ep)
			}

			ct := resp.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "application/json") {
				t.Errorf("GET %s: Content-Type=%q, want application/json", ep, ct)
			}

			// CORS header present.
			cors := resp.Header.Get("Access-Control-Allow-Origin")
			if cors != "*" {
				t.Errorf("GET %s: CORS=%q, want *", ep, cors)
			}

			// Must be valid JSON.
			var body any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Errorf("GET %s: invalid JSON: %v", ep, err)
			}
		})
	}
}

func TestAll404sReturnJSON(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	notFoundPaths := []struct {
		name string
		path string
	}{
		{"block_number", "/v1/explorer/blocks/88888888"},
		{"block_hash", "/v1/explorer/blocks/0x" + strings.Repeat("a", 64)},
		{"tx_hash", "/v1/explorer/transactions/0x" + strings.Repeat("b", 64)},
		{"address", "/v1/explorer/addresses/0x" + strings.Repeat("c", 40)},
		{"addr_counters", "/v1/explorer/addresses/0x" + strings.Repeat("c", 40) + "/counters"},
		{"token", "/v1/explorer/tokens/0x" + strings.Repeat("d", 40)},
		{"contract", "/v1/explorer/smart-contracts/0x" + strings.Repeat("e", 40)},
	}

	for _, tc := range notFoundPaths {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tc.path)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != 404 {
				t.Errorf("want 404, got %d", resp.StatusCode)
			}

			ct := resp.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "application/json") {
				t.Errorf("404 Content-Type=%q, want application/json", ct)
			}

			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Errorf("404 not valid JSON: %v", err)
			}
			if body["error"] == nil {
				t.Error("404 body missing error field")
			}
		})
	}
}

func TestOptionsPreflightOnAllRoutes(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	routes := []string{
		"/v1/explorer/blocks",
		"/v1/explorer/transactions",
		"/v1/explorer/addresses",
		"/v1/explorer/tokens",
		"/v1/explorer/search?q=test",
		"/v1/explorer/stats",
		"/v1/explorer/smart-contracts",
	}

	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodOptions, ts.URL+route, nil)
			req.Header.Set("Origin", "https://explore.lux.network")
			req.Header.Set("Access-Control-Request-Method", "GET")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("OPTIONS: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != 204 {
				t.Errorf("OPTIONS %s: want 204, got %d", route, resp.StatusCode)
			}
			if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
				t.Errorf("OPTIONS %s: missing CORS Allow-Origin", route)
			}
			if resp.Header.Get("Access-Control-Allow-Methods") == "" {
				t.Errorf("OPTIONS %s: missing CORS Allow-Methods", route)
			}
		})
	}
}

func TestAllPaginatedEndpointsHaveNextPageParams(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	sd := seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	txHash := hexStr(sd.txs[0].Hash)
	addrHash := hexStr(sd.txs[0].FromAddress)
	tokenAddr := hexStr(sd.erc20Token.ContractAddress)

	paginated := []string{
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

	for _, ep := range paginated {
		t.Run(ep, func(t *testing.T) {
			resp, err := http.Get(ts.URL + ep)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if _, ok := body["items"]; !ok {
				t.Error("missing items key")
			}
			// next_page_params key must exist (even if null).
			if _, ok := body["next_page_params"]; !ok {
				t.Error("missing next_page_params key")
			}
		})
	}
}

// ==========================================================================
// 6. ADDITIONAL EDGE CASES
// ==========================================================================

func TestTrailingSlashNormalization(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	// Trailing slash should return the same as without.
	resp1 := getJSON(t, ts, "/v1/explorer/blocks", 200)
	resp2 := getJSON(t, ts, "/v1/explorer/blocks/", 200)

	items1 := items(t, resp1)
	items2 := items(t, resp2)
	if len(items1) != len(items2) {
		t.Errorf("trailing slash mismatch: %d vs %d items", len(items1), len(items2))
	}
}

func TestHomepageWidgets(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	t.Run("homepage/blocks", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/v1/explorer/homepage/blocks")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body []any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if len(body) == 0 {
			t.Error("homepage blocks should return items")
		}
		if len(body) > 6 {
			t.Errorf("homepage blocks should return at most 6, got %d", len(body))
		}
	})

	t.Run("homepage/transactions", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/v1/explorer/homepage/transactions")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body []any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if len(body) == 0 {
			t.Error("homepage transactions should return items")
		}
		if len(body) > 6 {
			t.Errorf("homepage transactions should return at most 6, got %d", len(body))
		}
	})

	t.Run("indexing-status", func(t *testing.T) {
		body := getJSON(t, ts, "/v1/explorer/homepage/indexing-status", 200)
		if body["finished_indexing"] != true {
			t.Errorf("want finished_indexing=true, got %v", body["finished_indexing"])
		}
	})
}

func TestBackendConfig(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	_, ts := newServer(t, tdb.Path)

	t.Run("version", func(t *testing.T) {
		body := getJSON(t, ts, "/v1/explorer/config/backend-version", 200)
		if body["backend_version"] == nil {
			t.Error("missing backend_version")
		}
	})

	t.Run("config", func(t *testing.T) {
		body := getJSON(t, ts, "/v1/explorer/config/backend", 200)
		if body["coin_name"] != "TST" {
			t.Errorf("want coin_name=TST, got %v", body["coin_name"])
		}
		chainID := fmt.Sprintf("%v", body["chain_id"])
		if chainID != fmt.Sprintf("%d", testutil.DefaultChainID) {
			t.Errorf("want chain_id=%d, got %v", testutil.DefaultChainID, body["chain_id"])
		}
	})
}

func TestSearchRedirect(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)
	_, ts := newServer(t, tdb.Path)

	t.Run("tx_hash_redirect", func(t *testing.T) {
		hash := "0x" + strings.Repeat("a", 64)
		body := getJSON(t, ts, "/v1/explorer/search/check-redirect?q="+hash, 200)
		if body["redirect"] != true {
			t.Errorf("want redirect=true for tx hash")
		}
		if body["type"] != "transaction" {
			t.Errorf("want type=transaction, got %v", body["type"])
		}
	})

	t.Run("address_redirect", func(t *testing.T) {
		addr := "0x" + strings.Repeat("a", 40)
		body := getJSON(t, ts, "/v1/explorer/search/check-redirect?q="+addr, 200)
		if body["redirect"] != true {
			t.Errorf("want redirect=true for address")
		}
		if body["type"] != "address" {
			t.Errorf("want type=address, got %v", body["type"])
		}
	})

	t.Run("no_redirect", func(t *testing.T) {
		body := getJSON(t, ts, "/v1/explorer/search/check-redirect?q=hello", 200)
		if body["redirect"] != false {
			t.Errorf("want redirect=false for random string")
		}
	})
}

func TestChartEndpoints(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	_, ts := newServer(t, tdb.Path)

	t.Run("charts/transactions", func(t *testing.T) {
		body := getJSON(t, ts, "/v1/explorer/stats/charts/transactions", 200)
		if body["chart_data"] == nil {
			t.Error("missing chart_data")
		}
	})

	t.Run("charts/market", func(t *testing.T) {
		body := getJSON(t, ts, "/v1/explorer/stats/charts/market", 200)
		if body["chart_data"] == nil {
			t.Error("missing chart_data")
		}
	})
}

func TestNewStandaloneServer_EmptyConfig(t *testing.T) {
	_, err := NewStandaloneServer(Config{})
	if err == nil {
		t.Error("expected error for empty config")
	}
}

func TestConcurrentServerCreation(t *testing.T) {
	// Creating multiple servers on the same DB should not crash.
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv, err := NewStandaloneServer(Config{
				IndexerDBPath: tdb.Path,
				ChainID:       testutil.DefaultChainID,
				ChainName:     "Test",
				CoinSymbol:    "TST",
			})
			if err != nil {
				t.Errorf("NewStandaloneServer: %v", err)
				return
			}
			srv.Close()
		}()
	}
	wg.Wait()
}

func TestRapidServerLifecycle(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	seedAll(t, tdb)

	// Open/close rapidly -- should not leak or panic.
	for i := 0; i < 20; i++ {
		srv, err := NewStandaloneServer(Config{
			IndexerDBPath: tdb.Path,
			ChainID:       testutil.DefaultChainID,
			ChainName:     "Test",
			CoinSymbol:    "TST",
		})
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		ts := httptest.NewServer(srv.Handler())
		resp, err := http.Get(ts.URL + "/v1/explorer/stats")
		if err != nil {
			t.Fatalf("iteration %d: GET: %v", i, err)
		}
		resp.Body.Close()
		ts.Close()
		srv.Close()
	}
}

// ==========================================================================
// FUZZ HELPERS
// ==========================================================================

// setupFuzzServer creates a temporary DB with seed data and a server for fuzz tests.
// Uses database/sql directly since testutil.NewTestDB requires *testing.T.
func setupFuzzServer(f *testing.F) *httptest.Server {
	f.Helper()
	dir := f.TempDir()
	dbPath := dir + "/fuzz.db"

	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_journal_mode=WAL&_synchronous=NORMAL", dbPath))
	if err != nil {
		f.Fatalf("open fuzz db: %v", err)
	}

	// Create schema inline (minimal tables needed for fuzz targets).
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS blocks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chain_id INTEGER NOT NULL, number INTEGER NOT NULL, hash BLOB NOT NULL,
			parent_hash BLOB NOT NULL, nonce BLOB, miner BLOB NOT NULL,
			difficulty TEXT, total_difficulty TEXT, size INTEGER NOT NULL DEFAULT 0,
			gas_limit INTEGER NOT NULL DEFAULT 0, gas_used INTEGER NOT NULL DEFAULT 0,
			base_fee TEXT, timestamp INTEGER NOT NULL, transaction_count INTEGER NOT NULL DEFAULT 0,
			consensus_state TEXT DEFAULT 'finalized', UNIQUE(chain_id, number));
		CREATE INDEX IF NOT EXISTS idx_blocks_hash ON blocks(hash);
		CREATE TABLE IF NOT EXISTS transactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chain_id INTEGER NOT NULL, hash BLOB NOT NULL, block_number INTEGER NOT NULL,
			block_hash BLOB NOT NULL, transaction_index INTEGER NOT NULL,
			from_address_hash BLOB NOT NULL, to_address_hash BLOB,
			value TEXT NOT NULL DEFAULT '0', gas INTEGER NOT NULL DEFAULT 0,
			gas_price TEXT, gas_used INTEGER, max_fee_per_gas TEXT,
			max_priority_fee_per_gas TEXT, input BLOB, nonce INTEGER NOT NULL DEFAULT 0,
			type INTEGER NOT NULL DEFAULT 0, status INTEGER, error TEXT, revert_reason TEXT,
			created_contract_address_hash BLOB, cumulative_gas_used INTEGER,
			block_timestamp INTEGER NOT NULL, UNIQUE(chain_id, hash));
		CREATE INDEX IF NOT EXISTS idx_txs_block ON transactions(chain_id, block_number);
		CREATE TABLE IF NOT EXISTS addresses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chain_id INTEGER NOT NULL, hash BLOB NOT NULL,
			fetched_coin_balance TEXT, fetched_coin_balance_block_number INTEGER,
			contract_code BLOB, transactions_count INTEGER DEFAULT 0,
			token_transfers_count INTEGER DEFAULT 0, gas_used TEXT DEFAULT '0',
			nonce INTEGER, decompiled INTEGER DEFAULT 0, verified INTEGER DEFAULT 0,
			UNIQUE(chain_id, hash));
		CREATE TABLE IF NOT EXISTS tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chain_id INTEGER NOT NULL, contract_address_hash BLOB NOT NULL,
			name TEXT, symbol TEXT, total_supply TEXT, decimals INTEGER,
			type TEXT NOT NULL, holder_count INTEGER DEFAULT 0, icon_url TEXT,
			UNIQUE(chain_id, contract_address_hash));
		CREATE TABLE IF NOT EXISTS token_transfers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chain_id INTEGER NOT NULL, transaction_hash BLOB NOT NULL,
			log_index INTEGER NOT NULL, block_number INTEGER NOT NULL,
			block_hash BLOB NOT NULL, from_address_hash BLOB NOT NULL,
			to_address_hash BLOB NOT NULL, token_contract_address_hash BLOB NOT NULL,
			amount TEXT, token_id TEXT, token_type TEXT, block_timestamp INTEGER NOT NULL);
		CREATE TABLE IF NOT EXISTS logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chain_id INTEGER NOT NULL, block_number INTEGER NOT NULL,
			block_hash BLOB NOT NULL, transaction_hash BLOB NOT NULL,
			transaction_index INTEGER NOT NULL, "index" INTEGER NOT NULL,
			address_hash BLOB NOT NULL, data BLOB, first_topic BLOB,
			second_topic BLOB, third_topic BLOB, fourth_topic BLOB,
			type TEXT, block_timestamp INTEGER NOT NULL);
		CREATE TABLE IF NOT EXISTS internal_transactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chain_id INTEGER NOT NULL, transaction_hash BLOB NOT NULL,
			block_number INTEGER NOT NULL, block_hash BLOB NOT NULL,
			"index" INTEGER NOT NULL, trace_address TEXT NOT NULL DEFAULT '{}',
			type TEXT NOT NULL, call_type TEXT, from_address_hash BLOB NOT NULL,
			to_address_hash BLOB, value TEXT NOT NULL DEFAULT '0',
			gas INTEGER, gas_used INTEGER, input BLOB, output BLOB,
			error TEXT, block_timestamp INTEGER NOT NULL);
		CREATE TABLE IF NOT EXISTS smart_contracts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chain_id INTEGER NOT NULL, address_hash BLOB NOT NULL,
			name TEXT NOT NULL, compiler_version TEXT NOT NULL,
			optimization INTEGER DEFAULT 0, optimization_runs INTEGER,
			contract_source_code TEXT, abi TEXT, evm_version TEXT,
			verified_via TEXT, is_vyper_contract INTEGER DEFAULT 0,
			license_type TEXT, UNIQUE(chain_id, address_hash));
		CREATE TABLE IF NOT EXISTS address_current_token_balances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chain_id INTEGER NOT NULL, address_hash BLOB NOT NULL,
			token_contract_address_hash BLOB NOT NULL, value TEXT,
			block_number INTEGER NOT NULL, token_type TEXT);
	`)
	if err != nil {
		f.Fatalf("create schema: %v", err)
	}

	// Seed 5 blocks + 5 txs.
	now := time.Now().Unix()
	for i := 0; i < 5; i++ {
		bHash := testutil.RandomHexHash()
		miner := testutil.RandomHexAddress()
		db.Exec(`INSERT INTO blocks (chain_id,number,hash,parent_hash,miner,timestamp,transaction_count,consensus_state) VALUES (?,?,?,?,?,?,?,?)`,
			testutil.DefaultChainID, i, bHash, testutil.RandomHexHash(), miner, now-int64(4-i)*10, 1, "finalized")

		txHash := testutil.RandomHexHash()
		db.Exec(`INSERT INTO transactions (chain_id,hash,block_number,block_hash,transaction_index,from_address_hash,to_address_hash,value,gas,gas_price,gas_used,nonce,type,status,block_timestamp) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			testutil.DefaultChainID, txHash, i, bHash, 0, testutil.RandomHexAddress(), testutil.RandomHexAddress(), "0", 21000, "25000000000", 21000, i, 0, 1, now-int64(4-i)*10)
	}
	db.Close()

	srv, err := NewStandaloneServer(Config{
		IndexerDBPath: dbPath,
		ChainID:       testutil.DefaultChainID,
		ChainName:     "Fuzz",
		CoinSymbol:    "FZZ",
	})
	if err != nil {
		f.Fatalf("NewStandaloneServer: %v", err)
	}
	f.Cleanup(func() { srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	f.Cleanup(func() { ts.Close() })
	return ts
}
