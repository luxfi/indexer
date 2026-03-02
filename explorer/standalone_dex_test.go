package explorer

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/luxfi/indexer/explorer/testutil"
)

// seedDex inserts DEX test data: orders, trades, market stats, pools, swaps.
func seedDex(t *testing.T, db *testutil.DB) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	past := time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339)

	// Market stats
	db.Exec("INSERT INTO dex_market_stats (symbol, last_price, high_24h, low_24h, volume_24h, trade_count_24h) VALUES (?, ?, ?, ?, ?, ?)",
		"LUX/USDT", 42000, 43000, 41000, 500000, 120)
	db.Exec("INSERT INTO dex_market_stats (symbol, last_price, high_24h, low_24h, volume_24h, trade_count_24h) VALUES (?, ?, ?, ?, ?, ?)",
		"ETH/USDT", 3200, 3300, 3100, 300000, 80)

	// Orders (open bids and asks)
	for i := 0; i < 5; i++ {
		db.Exec("INSERT INTO dex_orders (id, owner, symbol, side, type, price, quantity, filled_qty, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)",
			fmt.Sprintf("bid-%d", i), "0xaaaa", "LUX/USDT", "buy", "limit", 41000+i*100, 10, 0, "open", now, now)
		db.Exec("INSERT INTO dex_orders (id, owner, symbol, side, type, price, quantity, filled_qty, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)",
			fmt.Sprintf("ask-%d", i), "0xbbbb", "LUX/USDT", "sell", "limit", 42500+i*100, 10, 0, "open", now, now)
	}
	// A filled order (should NOT appear in orderbook)
	db.Exec("INSERT INTO dex_orders (id, owner, symbol, side, type, price, quantity, filled_qty, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)",
		"filled-1", "0xcccc", "LUX/USDT", "buy", "limit", 40000, 10, 10, "filled", now, now)

	// Trades
	for i := 0; i < 3; i++ {
		ts := time.Now().Add(time.Duration(-i) * time.Minute).UTC().Format(time.RFC3339)
		db.Exec("INSERT INTO dex_trades (id, symbol, maker_order_id, taker_order_id, maker, taker, side, price, quantity, volume, fee, timestamp) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)",
			fmt.Sprintf("trade-%d", i), "LUX/USDT", fmt.Sprintf("bid-%d", i), fmt.Sprintf("ask-%d", i), "0xaaaa", "0xbbbb", "buy", 42000+i*100, 5, 210000+i*500, 100, ts)
	}
	// A trade for a different pair
	db.Exec("INSERT INTO dex_trades (id, symbol, maker_order_id, taker_order_id, maker, taker, side, price, quantity, volume, fee, timestamp) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)",
		"trade-eth-0", "ETH/USDT", "e-bid-0", "e-ask-0", "0xaaaa", "0xcccc", "sell", 3200, 2, 6400, 50, now)

	// Pools
	db.Exec("INSERT INTO dex_pools (id, token0, token1, reserve0, reserve1, lp_supply, fee, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)",
		"pool-1", "0x1111", "0x2222", "1000000", "500000", "700000", 30, past, now)
	db.Exec("INSERT INTO dex_pools (id, token0, token1, reserve0, reserve1, lp_supply, fee, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)",
		"pool-2", "0x3333", "0x4444", "200000", "100000", "150000", 100, past, now)

	// Swaps
	for i := 0; i < 4; i++ {
		ts := time.Now().Add(time.Duration(-i) * time.Minute).UTC().Format(time.RFC3339)
		db.Exec("INSERT INTO dex_swaps (id, pool_id, sender, token_in, token_out, amount_in, amount_out, fee, timestamp) VALUES (?,?,?,?,?,?,?,?,?)",
			fmt.Sprintf("swap-%d", i), "pool-1", "0xaaaa", "0x1111", "0x2222", "1000", "500", "3", ts)
	}
}

func newDexServer(t *testing.T, db *testutil.DB) *httptest.Server {
	t.Helper()
	srv, err := NewStandaloneServer(Config{
		IndexerDBPath: db.Path,
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
	return ts
}

func TestDexMarkets(t *testing.T) {
	db := testutil.NewTestDB(t)
	seedDex(t, db)
	ts := newDexServer(t, db)

	body := getJSON(t, ts, "/v1/explorer/dex/markets", 200)
	got := items(t, body)
	if len(got) != 2 {
		t.Fatalf("want 2 markets, got %d", len(got))
	}
	// Ordered by volume_24h DESC: LUX/USDT first
	first := got[0].(map[string]any)
	if first["symbol"] != "LUX/USDT" {
		t.Fatalf("want first market LUX/USDT, got %v", first["symbol"])
	}
}

func TestDexMarketDetail(t *testing.T) {
	db := testutil.NewTestDB(t)
	seedDex(t, db)
	ts := newDexServer(t, db)

	body := getJSON(t, ts, "/v1/explorer/dex/markets/LUX%2FUSDT", 200)
	if body["symbol"] != "LUX/USDT" {
		t.Fatalf("want symbol LUX/USDT, got %v", body["symbol"])
	}

	// Not found
	resp := getResp(t, ts, "/v1/explorer/dex/markets/NOPE%2FUSDT")
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

func TestDexTrades(t *testing.T) {
	db := testutil.NewTestDB(t)
	seedDex(t, db)
	ts := newDexServer(t, db)

	// All trades
	body := getJSON(t, ts, "/v1/explorer/dex/trades", 200)
	got := items(t, body)
	if len(got) != 4 {
		t.Fatalf("want 4 trades, got %d", len(got))
	}

	// Filter by pair
	body = getJSON(t, ts, "/v1/explorer/dex/trades/LUX%2FUSDT", 200)
	got = items(t, body)
	if len(got) != 3 {
		t.Fatalf("want 3 LUX/USDT trades, got %d", len(got))
	}
}

func TestDexOrderbook(t *testing.T) {
	db := testutil.NewTestDB(t)
	seedDex(t, db)
	ts := newDexServer(t, db)

	body := getJSON(t, ts, "/v1/explorer/dex/orderbook/LUX%2FUSDT", 200)
	bids, ok := body["bids"].([]any)
	if !ok {
		t.Fatal("bids not an array")
	}
	asks, ok := body["asks"].([]any)
	if !ok {
		t.Fatal("asks not an array")
	}
	if len(bids) != 5 {
		t.Fatalf("want 5 bid levels, got %d", len(bids))
	}
	if len(asks) != 5 {
		t.Fatalf("want 5 ask levels, got %d", len(asks))
	}
	// Bids sorted DESC: highest price first
	firstBid := bids[0].(map[string]any)
	if toInt64(firstBid["price"]) != 41400 {
		t.Fatalf("want highest bid 41400, got %v", firstBid["price"])
	}
	// Asks sorted ASC: lowest price first
	firstAsk := asks[0].(map[string]any)
	if toInt64(firstAsk["price"]) != 42500 {
		t.Fatalf("want lowest ask 42500, got %v", firstAsk["price"])
	}
}

func TestDexCandles(t *testing.T) {
	db := testutil.NewTestDB(t)
	seedDex(t, db)
	ts := newDexServer(t, db)

	now := time.Now().Unix()
	from := now - 3600
	url := fmt.Sprintf("/v1/explorer/dex/candles/LUX%%2FUSDT?interval=1h&from=%d&to=%d", from, now+1)
	body := getJSON(t, ts, url, 200)
	got := items(t, body)
	// All 3 trades are within the same 1h bucket
	if len(got) != 1 {
		t.Fatalf("want 1 candle (all trades in same hour), got %d", len(got))
	}
	candle := got[0].(map[string]any)
	if candle["interval"] != "1h" {
		t.Fatalf("want interval 1h, got %v", candle["interval"])
	}
	if fmtNum(candle["trades"]) != "3" {
		t.Fatalf("want 3 trades in candle, got %v", candle["trades"])
	}
}

func TestPoolList(t *testing.T) {
	db := testutil.NewTestDB(t)
	seedDex(t, db)
	ts := newDexServer(t, db)

	body := getJSON(t, ts, "/v1/explorer/pools", 200)
	got := items(t, body)
	if len(got) != 2 {
		t.Fatalf("want 2 pools, got %d", len(got))
	}
	// Ordered by reserve0 DESC: pool-1 first
	first := got[0].(map[string]any)
	if first["id"] != "pool-1" {
		t.Fatalf("want first pool pool-1, got %v", first["id"])
	}
}

func TestPoolDetail(t *testing.T) {
	db := testutil.NewTestDB(t)
	seedDex(t, db)
	ts := newDexServer(t, db)

	body := getJSON(t, ts, "/v1/explorer/pools/pool-1", 200)
	if body["id"] != "pool-1" {
		t.Fatalf("want pool-1, got %v", body["id"])
	}
	// Should have recent_swaps attached
	swaps, ok := body["recent_swaps"].([]any)
	if !ok {
		t.Fatal("recent_swaps not an array")
	}
	if len(swaps) != 4 {
		t.Fatalf("want 4 recent swaps, got %d", len(swaps))
	}

	// Not found
	resp := getResp(t, ts, "/v1/explorer/pools/nonexistent")
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

func TestPoolSwaps(t *testing.T) {
	db := testutil.NewTestDB(t)
	seedDex(t, db)
	ts := newDexServer(t, db)

	body := getJSON(t, ts, "/v1/explorer/pools/pool-1/swaps", 200)
	got := items(t, body)
	if len(got) != 4 {
		t.Fatalf("want 4 swaps for pool-1, got %d", len(got))
	}

	// pool-2 has no swaps
	body = getJSON(t, ts, "/v1/explorer/pools/pool-2/swaps", 200)
	got = items(t, body)
	if len(got) != 0 {
		t.Fatalf("want 0 swaps for pool-2, got %d", len(got))
	}
}

func TestDexNoTablesGraceful(t *testing.T) {
	// Create a DB without DEX tables to verify graceful degradation.
	dir := t.TempDir()
	path := dir + "/bare.db"
	// Use a minimal schema (just blocks)
	bareDB := testutil.NewTestDB(t)

	// Drop DEX tables
	bareDB.Exec("DROP TABLE IF EXISTS dex_orders")
	bareDB.Exec("DROP TABLE IF EXISTS dex_trades")
	bareDB.Exec("DROP TABLE IF EXISTS dex_market_stats")
	bareDB.Exec("DROP TABLE IF EXISTS dex_pools")
	bareDB.Exec("DROP TABLE IF EXISTS dex_swaps")
	_ = path // not used, we use bareDB.Path

	srv, err := NewStandaloneServer(Config{
		IndexerDBPath: bareDB.Path,
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

	// All DEX endpoints should return empty, not error
	for _, path := range []string{
		"/v1/explorer/dex/markets",
		"/v1/explorer/dex/trades",
		"/v1/explorer/pools",
	} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s: want 200, got %d", path, resp.StatusCode)
		}
	}
}

func TestDexPagination(t *testing.T) {
	db := testutil.NewTestDB(t)
	seedDex(t, db)
	ts := newDexServer(t, db)

	// Request with items_count=2 to trigger pagination
	body := getJSON(t, ts, "/v1/explorer/dex/trades?items_count=2", 200)
	got := items(t, body)
	if len(got) != 2 {
		t.Fatalf("want 2 trades with limit, got %d", len(got))
	}
	np := body["next_page_params"]
	if np == nil {
		t.Fatal("expected next_page_params for paginated result")
	}
}
