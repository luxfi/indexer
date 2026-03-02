// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mockServer creates a test server that returns the given status + body.
func mockServer(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
}

// mockServerFunc creates a test server driven by a handler function.
func mockServerFunc(fn http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(fn)
}

// floatClose returns true if a and b differ by less than epsilon.
func floatClose(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}

// ---------------------------------------------------------------------------
// CoinGecko — Coin Price
// ---------------------------------------------------------------------------

func TestCoinGecko_CoinPrice_Success(t *testing.T) {
	body := `{"lux-network":{"usd":12.5,"eur":11.2,"btc":0.00045}}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	prices, err := cg.CoinPrice(context.Background(), "lux-network", []string{"usd", "eur", "btc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices["usd"] != 12.5 {
		t.Errorf("usd = %f, want 12.5", prices["usd"])
	}
	if prices["eur"] != 11.2 {
		t.Errorf("eur = %f, want 11.2", prices["eur"])
	}
	if prices["btc"] != 0.00045 {
		t.Errorf("btc = %f, want 0.00045", prices["btc"])
	}
}

func TestCoinGecko_CoinPrice_SingleCurrency(t *testing.T) {
	body := `{"lux-network":{"usd":25.0}}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	prices, err := cg.CoinPrice(context.Background(), "lux-network", []string{"usd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices["usd"] != 25.0 {
		t.Errorf("usd = %f, want 25.0", prices["usd"])
	}
}

func TestCoinGecko_CoinPrice_CoinNotFound(t *testing.T) {
	body := `{}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	_, err := cg.CoinPrice(context.Background(), "nonexistent", []string{"usd"})
	if err == nil {
		t.Fatal("expected error for missing coin")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want 'not found'", err)
	}
}

func TestCoinGecko_CoinPrice_WithMarketData(t *testing.T) {
	body := `{"lux-network":{"usd":12.5,"usd_market_cap":500000000,"usd_24h_vol":10000000,"usd_24h_change":-2.5}}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	prices, err := cg.CoinPrice(context.Background(), "lux-network", []string{"usd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices["usd"] != 12.5 {
		t.Errorf("usd = %f, want 12.5", prices["usd"])
	}
}

// ---------------------------------------------------------------------------
// CoinGecko — Token Price
// ---------------------------------------------------------------------------

func TestCoinGecko_TokenPrice_Success(t *testing.T) {
	body := `{"0x1234abcd":{"usd":1.25,"eur":1.12}}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	prices, err := cg.TokenPrice(context.Background(), "lux", "0x1234abcd", []string{"usd", "eur"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices["usd"] != 1.25 {
		t.Errorf("usd = %f, want 1.25", prices["usd"])
	}
	if prices["eur"] != 1.12 {
		t.Errorf("eur = %f, want 1.12", prices["eur"])
	}
}

func TestCoinGecko_TokenPrice_UppercaseNormalized(t *testing.T) {
	body := `{"0xabcdef":{"usd":5.0}}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	prices, err := cg.TokenPrice(context.Background(), "lux", "0xABCDEF", []string{"usd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices["usd"] != 5.0 {
		t.Errorf("usd = %f, want 5.0", prices["usd"])
	}
}

func TestCoinGecko_TokenPrice_NotFound(t *testing.T) {
	body := `{}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	_, err := cg.TokenPrice(context.Background(), "lux", "0xdead", []string{"usd"})
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}

// ---------------------------------------------------------------------------
// CoinGecko — Market Chart
// ---------------------------------------------------------------------------

func TestCoinGecko_MarketChart_Success(t *testing.T) {
	ts1 := float64(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
	ts2 := float64(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC).UnixMilli())
	ts3 := float64(time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC).UnixMilli())
	body := fmt.Sprintf(`{"prices":[[%f,10.0],[%f,11.0],[%f,12.0]]}`, ts1, ts2, ts3)
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	prices, err := cg.MarketChart(context.Background(), "lux-network", "usd", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prices) != 3 {
		t.Fatalf("got %d prices, want 3", len(prices))
	}
	if prices[0].Price != 10.0 {
		t.Errorf("price[0] = %f, want 10.0", prices[0].Price)
	}
	if prices[2].Price != 12.0 {
		t.Errorf("price[2] = %f, want 12.0", prices[2].Price)
	}
}

func TestCoinGecko_MarketChart_EmptyPrices(t *testing.T) {
	body := `{"prices":[]}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	prices, err := cg.MarketChart(context.Background(), "lux-network", "usd", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prices) != 0 {
		t.Errorf("got %d prices, want 0", len(prices))
	}
}

func TestCoinGecko_MarketChart_MalformedEntry(t *testing.T) {
	// One entry has only 1 element; should be skipped.
	ts := float64(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
	body := fmt.Sprintf(`{"prices":[[%f],[%f,10.0]]}`, ts, ts)
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	prices, err := cg.MarketChart(context.Background(), "lux-network", "usd", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prices) != 1 {
		t.Errorf("got %d prices, want 1 (malformed entry skipped)", len(prices))
	}
}

// ---------------------------------------------------------------------------
// CoinGecko — Error Handling
// ---------------------------------------------------------------------------

func TestCoinGecko_RateLimit429(t *testing.T) {
	srv := mockServer(http.StatusTooManyRequests, `{"error":"rate limit"}`)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	_, err := cg.CoinPrice(context.Background(), "lux-network", []string{"usd"})
	if err == nil {
		t.Fatal("expected error on 429")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error = %v, want rate limited", err)
	}
}

func TestCoinGecko_ServerError500(t *testing.T) {
	srv := mockServer(http.StatusInternalServerError, `{"error":"internal"}`)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	_, err := cg.CoinPrice(context.Background(), "lux-network", []string{"usd"})
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want status 500", err)
	}
}

func TestCoinGecko_MalformedJSON(t *testing.T) {
	srv := mockServer(http.StatusOK, `{not valid json`)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	_, err := cg.CoinPrice(context.Background(), "lux-network", []string{"usd"})
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

func TestCoinGecko_EmptyBody(t *testing.T) {
	srv := mockServer(http.StatusOK, ``)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	_, err := cg.CoinPrice(context.Background(), "lux-network", []string{"usd"})
	if err == nil {
		t.Fatal("expected error on empty body")
	}
}

func TestCoinGecko_NullPriceValue(t *testing.T) {
	body := `{"lux-network":{"usd":null}}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	prices, err := cg.CoinPrice(context.Background(), "lux-network", []string{"usd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// null decodes to 0 in Go's json
	if prices["usd"] != 0 {
		t.Errorf("usd = %f, want 0 (null)", prices["usd"])
	}
}

func TestCoinGecko_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 50 * time.Millisecond}
	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL), WithCoinGeckoHTTPClient(client))
	_, err := cg.CoinPrice(context.Background(), "lux-network", []string{"usd"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestCoinGecko_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := cg.CoinPrice(ctx, "lux-network", []string{"usd"})
	if err == nil {
		t.Fatal("expected context canceled error")
	}
}

func TestCoinGecko_APIKeyHeader(t *testing.T) {
	var gotKey string
	srv := mockServerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-cg-pro-api-key")
		fmt.Fprint(w, `{"lux-network":{"usd":10.0}}`)
	})
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL), WithCoinGeckoAPIKey("test-key-123"))
	_, err := cg.CoinPrice(context.Background(), "lux-network", []string{"usd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotKey != "test-key-123" {
		t.Errorf("api key = %q, want %q", gotKey, "test-key-123")
	}
}

func TestCoinGecko_MarketChart_RateLimit(t *testing.T) {
	srv := mockServer(http.StatusTooManyRequests, ``)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	_, err := cg.MarketChart(context.Background(), "lux-network", "usd", 7)
	if err == nil {
		t.Fatal("expected 429 error")
	}
}

func TestCoinGecko_TokenPrice_RateLimit(t *testing.T) {
	srv := mockServer(http.StatusTooManyRequests, ``)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	_, err := cg.TokenPrice(context.Background(), "lux", "0xabc", []string{"usd"})
	if err == nil {
		t.Fatal("expected 429 error")
	}
}

func TestCoinGecko_TokenPrice_MalformedJSON(t *testing.T) {
	srv := mockServer(http.StatusOK, `{invalid`)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	_, err := cg.TokenPrice(context.Background(), "lux", "0xabc", []string{"usd"})
	if err == nil {
		t.Fatal("expected JSON error")
	}
}

func TestCoinGecko_MarketChart_MalformedJSON(t *testing.T) {
	srv := mockServer(http.StatusOK, `{invalid`)
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	_, err := cg.MarketChart(context.Background(), "lux-network", "usd", 7)
	if err == nil {
		t.Fatal("expected JSON error")
	}
}

func TestCoinGecko_Name(t *testing.T) {
	cg := NewCoinGecko()
	if cg.Name() != "coingecko" {
		t.Errorf("Name() = %q, want %q", cg.Name(), "coingecko")
	}
}

// ---------------------------------------------------------------------------
// CoinMarketCap — Coin Price
// ---------------------------------------------------------------------------

func TestCMC_CoinPrice_Success(t *testing.T) {
	body := `{
		"status":{"error_code":0,"error_message":""},
		"data":{
			"1":{"id":1,"name":"Lux","symbol":"LUX","slug":"lux-network",
				"quote":{"USD":{"price":12.5,"volume_24h":1000000,"market_cap":500000000,"percent_change_24h":-2.5,"percent_change_7d":5.0,"percent_change_30d":10.0}}}
		}
	}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cmc := NewCoinMarketCap(WithCMCBaseURL(srv.URL))
	prices, err := cmc.CoinPrice(context.Background(), "lux-network", []string{"usd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices["usd"] != 12.5 {
		t.Errorf("usd = %f, want 12.5", prices["usd"])
	}
}

func TestCMC_CoinPrice_APIError(t *testing.T) {
	body := `{"status":{"error_code":1002,"error_message":"API key invalid"},"data":{}}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cmc := NewCoinMarketCap(WithCMCBaseURL(srv.URL))
	_, err := cmc.CoinPrice(context.Background(), "lux-network", []string{"usd"})
	if err == nil {
		t.Fatal("expected API error")
	}
	if !strings.Contains(err.Error(), "API key invalid") {
		t.Errorf("error = %v, want API key message", err)
	}
}

func TestCMC_CoinPrice_RateLimit(t *testing.T) {
	srv := mockServer(http.StatusTooManyRequests, ``)
	defer srv.Close()

	cmc := NewCoinMarketCap(WithCMCBaseURL(srv.URL))
	_, err := cmc.CoinPrice(context.Background(), "lux-network", []string{"usd"})
	if err == nil {
		t.Fatal("expected 429 error")
	}
}

func TestCMC_CoinPrice_ServerError(t *testing.T) {
	srv := mockServer(http.StatusInternalServerError, ``)
	defer srv.Close()

	cmc := NewCoinMarketCap(WithCMCBaseURL(srv.URL))
	_, err := cmc.CoinPrice(context.Background(), "lux-network", []string{"usd"})
	if err == nil {
		t.Fatal("expected 500 error")
	}
}

func TestCMC_CoinPrice_MalformedJSON(t *testing.T) {
	srv := mockServer(http.StatusOK, `{bad json`)
	defer srv.Close()

	cmc := NewCoinMarketCap(WithCMCBaseURL(srv.URL))
	_, err := cmc.CoinPrice(context.Background(), "lux-network", []string{"usd"})
	if err == nil {
		t.Fatal("expected JSON error")
	}
}

func TestCMC_CoinPrice_MultiCurrency(t *testing.T) {
	body := `{
		"status":{"error_code":0,"error_message":""},
		"data":{
			"1":{"id":1,"name":"Lux","symbol":"LUX","slug":"lux-network",
				"quote":{
					"USD":{"price":12.5,"volume_24h":1000000,"market_cap":500000000,"percent_change_24h":0,"percent_change_7d":0,"percent_change_30d":0},
					"EUR":{"price":11.2,"volume_24h":900000,"market_cap":450000000,"percent_change_24h":0,"percent_change_7d":0,"percent_change_30d":0}
				}}
		}
	}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cmc := NewCoinMarketCap(WithCMCBaseURL(srv.URL))
	prices, err := cmc.CoinPrice(context.Background(), "lux-network", []string{"usd", "eur"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices["usd"] != 12.5 {
		t.Errorf("usd = %f, want 12.5", prices["usd"])
	}
	if prices["eur"] != 11.2 {
		t.Errorf("eur = %f, want 11.2", prices["eur"])
	}
}

func TestCMC_TokenPrice_Success(t *testing.T) {
	body := `{
		"status":{"error_code":0,"error_message":""},
		"data":{
			"1":{"id":1,"name":"Token","symbol":"TKN","slug":"token",
				"quote":{"USD":{"price":3.5,"volume_24h":100000,"market_cap":10000000,"percent_change_24h":0,"percent_change_7d":0,"percent_change_30d":0}}}
		}
	}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cmc := NewCoinMarketCap(WithCMCBaseURL(srv.URL))
	prices, err := cmc.TokenPrice(context.Background(), "lux", "0xdeadbeef", []string{"usd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices["usd"] != 3.5 {
		t.Errorf("usd = %f, want 3.5", prices["usd"])
	}
}

func TestCMC_MarketChart_NotSupported(t *testing.T) {
	cmc := NewCoinMarketCap()
	_, err := cmc.MarketChart(context.Background(), "lux-network", "usd", 7)
	if err == nil {
		t.Fatal("expected error for unsupported market chart")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("error = %v, want 'not available'", err)
	}
}

func TestCMC_APIKeyHeader(t *testing.T) {
	var gotKey string
	srv := mockServerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-CMC_PRO_API_KEY")
		fmt.Fprint(w, `{"status":{"error_code":0,"error_message":""},"data":{"1":{"id":1,"name":"Lux","symbol":"LUX","slug":"lux","quote":{"USD":{"price":10.0,"volume_24h":0,"market_cap":0,"percent_change_24h":0,"percent_change_7d":0,"percent_change_30d":0}}}}}`)
	})
	defer srv.Close()

	cmc := NewCoinMarketCap(WithCMCBaseURL(srv.URL), WithCMCAPIKey("cmc-key-456"))
	_, err := cmc.CoinPrice(context.Background(), "lux-network", []string{"usd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotKey != "cmc-key-456" {
		t.Errorf("api key = %q, want %q", gotKey, "cmc-key-456")
	}
}

func TestCMC_Name(t *testing.T) {
	cmc := NewCoinMarketCap()
	if cmc.Name() != "coinmarketcap" {
		t.Errorf("Name() = %q, want %q", cmc.Name(), "coinmarketcap")
	}
}

func TestCMC_TokenPrice_MalformedJSON(t *testing.T) {
	srv := mockServer(http.StatusOK, `{invalid`)
	defer srv.Close()

	cmc := NewCoinMarketCap(WithCMCBaseURL(srv.URL))
	_, err := cmc.TokenPrice(context.Background(), "lux", "0xabc", []string{"usd"})
	if err == nil {
		t.Fatal("expected JSON error")
	}
}

func TestCMC_TokenPrice_APIError(t *testing.T) {
	body := `{"status":{"error_code":400,"error_message":"address not found"},"data":{}}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cmc := NewCoinMarketCap(WithCMCBaseURL(srv.URL))
	_, err := cmc.TokenPrice(context.Background(), "lux", "0xbad", []string{"usd"})
	if err == nil {
		t.Fatal("expected API error")
	}
}

// ---------------------------------------------------------------------------
// Cache
// ---------------------------------------------------------------------------

func TestCache_CoinPrice_Hit(t *testing.T) {
	c := newCache(5 * time.Minute)
	c.setCoinPrice(map[string]float64{"usd": 10.0})
	prices, ok := c.getCoinPrice()
	if !ok {
		t.Fatal("expected cache hit")
	}
	if prices["usd"] != 10.0 {
		t.Errorf("usd = %f, want 10.0", prices["usd"])
	}
}

func TestCache_CoinPrice_Miss(t *testing.T) {
	c := newCache(5 * time.Minute)
	_, ok := c.getCoinPrice()
	if ok {
		t.Fatal("expected cache miss on empty cache")
	}
}

func TestCache_CoinPrice_Expired(t *testing.T) {
	c := newCache(1 * time.Millisecond)
	c.setCoinPrice(map[string]float64{"usd": 10.0})
	time.Sleep(5 * time.Millisecond)
	_, ok := c.getCoinPrice()
	if ok {
		t.Fatal("expected cache miss after expiry")
	}
}

func TestCache_TokenPrice_Hit(t *testing.T) {
	c := newCache(5 * time.Minute)
	c.setTokenPrice("0xabc", map[string]float64{"usd": 1.5})
	prices, ok := c.getTokenPrice("0xabc")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if prices["usd"] != 1.5 {
		t.Errorf("usd = %f, want 1.5", prices["usd"])
	}
}

func TestCache_TokenPrice_Miss(t *testing.T) {
	c := newCache(5 * time.Minute)
	_, ok := c.getTokenPrice("0xnonexistent")
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestCache_TokenPrice_Expired(t *testing.T) {
	c := newCache(1 * time.Millisecond)
	c.setTokenPrice("0xabc", map[string]float64{"usd": 1.5})
	time.Sleep(5 * time.Millisecond)
	_, ok := c.getTokenPrice("0xabc")
	if ok {
		t.Fatal("expected cache miss after expiry")
	}
}

func TestCache_MultipleTokens(t *testing.T) {
	c := newCache(5 * time.Minute)
	c.setTokenPrice("0xaaa", map[string]float64{"usd": 1.0})
	c.setTokenPrice("0xbbb", map[string]float64{"usd": 2.0})
	c.setTokenPrice("0xccc", map[string]float64{"usd": 3.0})

	p1, ok := c.getTokenPrice("0xaaa")
	if !ok || p1["usd"] != 1.0 {
		t.Errorf("0xaaa = %v, want 1.0", p1)
	}
	p2, ok := c.getTokenPrice("0xbbb")
	if !ok || p2["usd"] != 2.0 {
		t.Errorf("0xbbb = %v, want 2.0", p2)
	}
	p3, ok := c.getTokenPrice("0xccc")
	if !ok || p3["usd"] != 3.0 {
		t.Errorf("0xccc = %v, want 3.0", p3)
	}
}

func TestCache_ConcurrentAccess(t *testing.T) {
	c := newCache(5 * time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(v float64) {
			defer wg.Done()
			c.setCoinPrice(map[string]float64{"usd": v})
		}(float64(i))
		go func() {
			defer wg.Done()
			c.getCoinPrice()
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Service — cached fetching
// ---------------------------------------------------------------------------

func TestService_GetCoinPrice_CacheHit(t *testing.T) {
	var callCount int32
	srv := mockServerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		fmt.Fprint(w, `{"lux-network":{"usd":10.0}}`)
	})
	defer srv.Close()

	config := DefaultConfig()
	config.CacheTTL = 1 * time.Hour
	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	svc := NewService(cg, config)

	// First call hits the source.
	p1, err := svc.GetCoinPrice(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p1["usd"] != 10.0 {
		t.Errorf("usd = %f, want 10.0", p1["usd"])
	}

	// Second call uses cache.
	p2, err := svc.GetCoinPrice(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p2["usd"] != 10.0 {
		t.Errorf("usd = %f, want 10.0", p2["usd"])
	}

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("source called %d times, want 1 (cache hit)", callCount)
	}
}

func TestService_GetCoinPrice_CacheExpiry(t *testing.T) {
	var callCount int32
	srv := mockServerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		price := 10.0 * float64(n)
		fmt.Fprintf(w, `{"lux-network":{"usd":%f}}`, price)
	})
	defer srv.Close()

	config := DefaultConfig()
	config.CacheTTL = 1 * time.Millisecond
	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	svc := NewService(cg, config)

	_, err := svc.GetCoinPrice(context.Background())
	if err != nil {
		t.Fatalf("call 1: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	p2, err := svc.GetCoinPrice(context.Background())
	if err != nil {
		t.Fatalf("call 2: %v", err)
	}
	// Second call should get a new price.
	if p2["usd"] == 10.0 {
		t.Error("expected different price after cache expiry")
	}
	if atomic.LoadInt32(&callCount) < 2 {
		t.Errorf("source called %d times, want >= 2", callCount)
	}
}

func TestService_GetTokenPrice_CacheHit(t *testing.T) {
	var callCount int32
	srv := mockServerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		fmt.Fprint(w, `{"0xtoken":{"usd":5.0}}`)
	})
	defer srv.Close()

	config := DefaultConfig()
	config.CacheTTL = 1 * time.Hour
	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	svc := NewService(cg, config)

	_, err := svc.GetTokenPrice(context.Background(), "0xTOKEN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = svc.GetTokenPrice(context.Background(), "0xtoken")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("source called %d times, want 1", callCount)
	}
}

func TestService_GetMarketChart_DefaultDays(t *testing.T) {
	var gotPath string
	srv := mockServerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		fmt.Fprint(w, `{"prices":[]}`)
	})
	defer srv.Close()

	config := DefaultConfig()
	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	svc := NewService(cg, config)

	_, err := svc.GetMarketChart(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotPath, "days=30") {
		t.Errorf("path = %q, want days=30 default", gotPath)
	}
}

func TestService_GetMarketChart_MaxDays(t *testing.T) {
	var gotPath string
	srv := mockServerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		fmt.Fprint(w, `{"prices":[]}`)
	})
	defer srv.Close()

	config := DefaultConfig()
	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	svc := NewService(cg, config)

	_, err := svc.GetMarketChart(context.Background(), 9999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotPath, "days=365") {
		t.Errorf("path = %q, want days=365 capped", gotPath)
	}
}

// ---------------------------------------------------------------------------
// Aggregation
// ---------------------------------------------------------------------------

func TestAggregateDailyCandles(t *testing.T) {
	prices := []DailyPrice{
		{Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Price: 10.0},
		{Date: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Price: 12.0},
		{Date: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), Price: 11.0},
	}
	candles := AggregateDailyCandles(prices)
	if len(candles) != 3 {
		t.Fatalf("got %d candles, want 3", len(candles))
	}
	for i, c := range candles {
		if c.Open != prices[i].Price || c.Close != prices[i].Price {
			t.Errorf("candle[%d] open/close mismatch", i)
		}
	}
}

func TestAggregateDailyCandles_Empty(t *testing.T) {
	candles := AggregateDailyCandles(nil)
	if len(candles) != 0 {
		t.Errorf("got %d candles, want 0", len(candles))
	}
}

func TestAggregateWeekly(t *testing.T) {
	// Mon Jan 5 through Sun Jan 11, 2026
	prices := []DailyPrice{
		{Date: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), Price: 10.0},
		{Date: time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC), Price: 15.0},
		{Date: time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC), Price: 8.0},
		{Date: time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC), Price: 12.0},
		{Date: time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC), Price: 11.0},
		{Date: time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), Price: 14.0},
		{Date: time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC), Price: 13.0},
	}
	candles := AggregateWeekly(prices)
	if len(candles) != 1 {
		t.Fatalf("got %d candles, want 1 (all same week)", len(candles))
	}
	c := candles[0]
	if c.Open != 10.0 {
		t.Errorf("Open = %f, want 10.0", c.Open)
	}
	if c.High != 15.0 {
		t.Errorf("High = %f, want 15.0", c.High)
	}
	if c.Low != 8.0 {
		t.Errorf("Low = %f, want 8.0", c.Low)
	}
	if c.Close != 13.0 {
		t.Errorf("Close = %f, want 13.0", c.Close)
	}
}

func TestAggregateWeekly_MultiWeek(t *testing.T) {
	prices := []DailyPrice{
		{Date: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), Price: 10.0}, // week 1 Mon
		{Date: time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC), Price: 12.0},
		{Date: time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC), Price: 20.0}, // week 2 Mon
		{Date: time.Date(2026, 1, 13, 0, 0, 0, 0, time.UTC), Price: 22.0},
	}
	candles := AggregateWeekly(prices)
	if len(candles) != 2 {
		t.Fatalf("got %d candles, want 2", len(candles))
	}
	if candles[0].Close != 12.0 {
		t.Errorf("week1 Close = %f, want 12.0", candles[0].Close)
	}
	if candles[1].Open != 20.0 {
		t.Errorf("week2 Open = %f, want 20.0", candles[1].Open)
	}
}

func TestAggregateWeekly_Empty(t *testing.T) {
	candles := AggregateWeekly(nil)
	if candles != nil {
		t.Errorf("got %v, want nil", candles)
	}
}

func TestAggregateMonthly(t *testing.T) {
	prices := []DailyPrice{
		{Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Price: 10.0},
		{Date: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), Price: 20.0},
		{Date: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), Price: 15.0},
		{Date: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Price: 18.0},
		{Date: time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC), Price: 22.0},
	}
	candles := AggregateMonthly(prices)
	if len(candles) != 2 {
		t.Fatalf("got %d candles, want 2", len(candles))
	}
	jan := candles[0]
	if jan.Open != 10.0 || jan.High != 20.0 || jan.Low != 10.0 || jan.Close != 15.0 {
		t.Errorf("Jan candle = %+v, unexpected values", jan)
	}
	feb := candles[1]
	if feb.Open != 18.0 || feb.Close != 22.0 {
		t.Errorf("Feb candle = %+v, unexpected values", feb)
	}
}

func TestAggregateMonthly_Empty(t *testing.T) {
	candles := AggregateMonthly(nil)
	if candles != nil {
		t.Errorf("got %v, want nil", candles)
	}
}

func TestAggregateMonthly_SingleDay(t *testing.T) {
	prices := []DailyPrice{
		{Date: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC), Price: 42.0},
	}
	candles := AggregateMonthly(prices)
	if len(candles) != 1 {
		t.Fatalf("got %d candles, want 1", len(candles))
	}
	if candles[0].Open != 42.0 || candles[0].Close != 42.0 {
		t.Errorf("single day candle mismatch")
	}
}

// ---------------------------------------------------------------------------
// Gap Filling
// ---------------------------------------------------------------------------

func TestFillGaps_NoGaps(t *testing.T) {
	prices := []DailyPrice{
		{Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Price: 10.0},
		{Date: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Price: 11.0},
		{Date: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), Price: 12.0},
	}
	filled := FillGaps(prices)
	if len(filled) != 3 {
		t.Errorf("got %d entries, want 3", len(filled))
	}
}

func TestFillGaps_WithGaps(t *testing.T) {
	prices := []DailyPrice{
		{Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Price: 10.0},
		{Date: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC), Price: 14.0}, // 2-day gap
	}
	filled := FillGaps(prices)
	if len(filled) != 4 {
		t.Fatalf("got %d entries, want 4", len(filled))
	}
	// Gap days carry forward previous price.
	if filled[1].Price != 10.0 {
		t.Errorf("gap day 1 = %f, want 10.0 (carry forward)", filled[1].Price)
	}
	if filled[2].Price != 10.0 {
		t.Errorf("gap day 2 = %f, want 10.0 (carry forward)", filled[2].Price)
	}
	if filled[3].Price != 14.0 {
		t.Errorf("final day = %f, want 14.0", filled[3].Price)
	}
}

func TestFillGaps_SingleEntry(t *testing.T) {
	prices := []DailyPrice{
		{Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Price: 10.0},
	}
	filled := FillGaps(prices)
	if len(filled) != 1 {
		t.Errorf("got %d entries, want 1", len(filled))
	}
}

func TestFillGaps_Empty(t *testing.T) {
	filled := FillGaps(nil)
	if filled != nil {
		t.Errorf("got %v, want nil", filled)
	}
}

func TestFillGaps_WeekendGap(t *testing.T) {
	// Fri Jan 2 -> Mon Jan 5 (2-day weekend gap)
	prices := []DailyPrice{
		{Date: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Price: 10.0},
		{Date: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), Price: 12.0},
	}
	filled := FillGaps(prices)
	if len(filled) != 4 {
		t.Fatalf("got %d entries, want 4 (Fri + Sat + Sun + Mon)", len(filled))
	}
}

// ---------------------------------------------------------------------------
// Market Cap / Price Change / Stablecoin
// ---------------------------------------------------------------------------

func TestCalculateMarketCap(t *testing.T) {
	mc := CalculateMarketCap(12.5, 40000000)
	if mc != 500000000 {
		t.Errorf("market cap = %f, want 500000000", mc)
	}
}

func TestCalculateMarketCap_Zero(t *testing.T) {
	mc := CalculateMarketCap(0, 40000000)
	if mc != 0 {
		t.Errorf("market cap = %f, want 0", mc)
	}
}

func TestPriceChangePercent(t *testing.T) {
	tests := []struct {
		name     string
		old, new float64
		want     float64
	}{
		{"up 10%", 100, 110, 10.0},
		{"down 25%", 100, 75, -25.0},
		{"no change", 100, 100, 0.0},
		{"double", 50, 100, 100.0},
		{"zero old", 0, 100, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PriceChangePercent(tt.old, tt.new)
			if !floatClose(got, tt.want, 0.01) {
				t.Errorf("PriceChangePercent(%f, %f) = %f, want %f", tt.old, tt.new, got, tt.want)
			}
		})
	}
}

func TestIsStablecoin(t *testing.T) {
	tests := []struct {
		name  string
		price float64
		want  bool
	}{
		{"USDC exactly 1.00", 1.00, true},
		{"USDT at 0.999", 0.999, true},
		{"DAI at 1.001", 1.001, true},
		{"close to boundary low", 0.951, true},
		{"close to boundary high", 1.049, true},
		{"below threshold", 0.94, false},
		{"above threshold", 1.06, false},
		{"volatile token", 12.5, false},
		{"zero", 0.0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsStablecoin(tt.price)
			if got != tt.want {
				t.Errorf("IsStablecoin(%f) = %v, want %v", tt.price, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Exchange Rate Tests
// ---------------------------------------------------------------------------

func TestExchangeRate_USDToken(t *testing.T) {
	body := `{"0xtoken":{"usd":25.0}}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	config := DefaultConfig()
	config.DefaultFiat = []string{"usd"}
	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	svc := NewService(cg, config)

	prices, err := svc.GetTokenPrice(context.Background(), "0xtoken")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices["usd"] != 25.0 {
		t.Errorf("usd = %f, want 25.0", prices["usd"])
	}
}

func TestExchangeRate_ETHPair(t *testing.T) {
	body := `{"lux-network":{"eth":0.005}}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	config := DefaultConfig()
	config.DefaultFiat = []string{"eth"}
	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	svc := NewService(cg, config)

	prices, err := svc.GetCoinPrice(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices["eth"] != 0.005 {
		t.Errorf("eth = %f, want 0.005", prices["eth"])
	}
}

func TestExchangeRate_BTCPair(t *testing.T) {
	body := `{"lux-network":{"btc":0.00045}}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	config := DefaultConfig()
	config.DefaultFiat = []string{"btc"}
	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	svc := NewService(cg, config)

	prices, err := svc.GetCoinPrice(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices["btc"] != 0.00045 {
		t.Errorf("btc = %f, want 0.00045", prices["btc"])
	}
}

func TestExchangeRate_ZeroValueToken(t *testing.T) {
	body := `{"0xdead":{"usd":0}}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	config := DefaultConfig()
	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	svc := NewService(cg, config)

	prices, err := svc.GetTokenPrice(context.Background(), "0xdead")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices["usd"] != 0 {
		t.Errorf("usd = %f, want 0", prices["usd"])
	}
}

// ---------------------------------------------------------------------------
// Market History Tests
// ---------------------------------------------------------------------------

func TestMarketHistory_DailyOHLCV(t *testing.T) {
	ts1 := float64(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
	ts2 := float64(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC).UnixMilli())
	body := fmt.Sprintf(`{"prices":[[%f,100.0],[%f,110.0]]}`, ts1, ts2)
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	config := DefaultConfig()
	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	svc := NewService(cg, config)

	daily, err := svc.GetMarketChart(context.Background(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	candles := AggregateDailyCandles(daily)
	if len(candles) != 2 {
		t.Fatalf("got %d candles, want 2", len(candles))
	}
	if candles[0].Close != 100.0 {
		t.Errorf("day1 close = %f, want 100.0", candles[0].Close)
	}
}

func TestMarketHistory_Volume24h(t *testing.T) {
	body := `{
		"status":{"error_code":0,"error_message":""},
		"data":{"1":{"id":1,"name":"Lux","symbol":"LUX","slug":"lux",
			"quote":{"USD":{"price":12.5,"volume_24h":5000000,"market_cap":500000000,"percent_change_24h":-2.5,"percent_change_7d":5.0,"percent_change_30d":10.0}}}}
	}`
	srv := mockServer(http.StatusOK, body)
	defer srv.Close()

	cmc := NewCoinMarketCap(WithCMCBaseURL(srv.URL))

	// Parse volume from raw response to verify.
	raw, _ := cmc.get(context.Background(), srv.URL+"/test")
	var resp cmcQuotesResponse
	json.Unmarshal(raw, &resp)
	for _, coin := range resp.Data {
		if q, ok := coin.Quote["USD"]; ok {
			if q.Volume24h != 5000000 {
				t.Errorf("volume = %f, want 5000000", q.Volume24h)
			}
		}
	}
}

func TestMarketHistory_PriceChange24h(t *testing.T) {
	change := PriceChangePercent(100, 97.5)
	if !floatClose(change, -2.5, 0.01) {
		t.Errorf("24h change = %f, want -2.5", change)
	}
}

func TestMarketHistory_PriceChange7d(t *testing.T) {
	change := PriceChangePercent(100, 105)
	if !floatClose(change, 5.0, 0.01) {
		t.Errorf("7d change = %f, want 5.0", change)
	}
}

func TestMarketHistory_PriceChange30d(t *testing.T) {
	change := PriceChangePercent(100, 110)
	if !floatClose(change, 10.0, 0.01) {
		t.Errorf("30d change = %f, want 10.0", change)
	}
}

func TestMarketHistory_MarketCapCalculation(t *testing.T) {
	mc := CalculateMarketCap(12.5, 40000000)
	if mc != 500000000 {
		t.Errorf("market cap = %f, want 500000000", mc)
	}
}

// ---------------------------------------------------------------------------
// Config / DefaultConfig
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	if config.CacheTTL != 5*time.Minute {
		t.Errorf("CacheTTL = %v, want 5m", config.CacheTTL)
	}
	if config.CoinID != "lux-network" {
		t.Errorf("CoinID = %q, want lux-network", config.CoinID)
	}
	if config.Platform != "lux" {
		t.Errorf("Platform = %q, want lux", config.Platform)
	}
	if len(config.DefaultFiat) != 1 || config.DefaultFiat[0] != "usd" {
		t.Errorf("DefaultFiat = %v, want [usd]", config.DefaultFiat)
	}
	if config.RequestTimeout != 10*time.Second {
		t.Errorf("RequestTimeout = %v, want 10s", config.RequestTimeout)
	}
}

func TestNewService(t *testing.T) {
	cg := NewCoinGecko()
	svc := NewService(cg, DefaultConfig())
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.source == nil {
		t.Error("source is nil")
	}
	if svc.cache == nil {
		t.Error("cache is nil")
	}
}

// ---------------------------------------------------------------------------
// Struct types
// ---------------------------------------------------------------------------

func TestCoinData_Struct(t *testing.T) {
	data := CoinData{
		Price:             map[string]float64{"usd": 12.5, "eur": 11.2},
		MarketCap:         500000000,
		Volume24h:         10000000,
		PriceChange24h:    -2.5,
		PriceChange7d:     5.0,
		PriceChange30d:    10.0,
		CirculatingSupply: 40000000,
		LastUpdated:       time.Now(),
	}
	if data.Price["usd"] != 12.5 {
		t.Errorf("usd = %f, want 12.5", data.Price["usd"])
	}
	if data.MarketCap <= 0 {
		t.Error("MarketCap should be positive")
	}
}

func TestTokenData_Struct(t *testing.T) {
	data := TokenData{
		ContractAddress: "0x1234",
		Price:           map[string]float64{"usd": 1.5},
		MarketCap:       10000000,
		Volume24h:       500000,
		LastUpdated:     time.Now(),
	}
	if data.ContractAddress != "0x1234" {
		t.Errorf("address = %q, want 0x1234", data.ContractAddress)
	}
}

func TestOHLCV_Struct(t *testing.T) {
	candle := OHLCV{
		Date:   time.Now(),
		Open:   10.0,
		High:   15.0,
		Low:    9.0,
		Close:  12.0,
		Volume: 1000000,
	}
	if candle.High < candle.Low {
		t.Error("High should be >= Low")
	}
	if candle.Open < candle.Low || candle.Open > candle.High {
		t.Error("Open should be between Low and High")
	}
}

func TestDailyPrice_Struct(t *testing.T) {
	p := DailyPrice{
		Date:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Price: 12.5,
	}
	if p.Date.IsZero() {
		t.Error("Date should be set")
	}
	if p.Price <= 0 {
		t.Error("Price should be positive")
	}
}

// ---------------------------------------------------------------------------
// weekStartDate helper
// ---------------------------------------------------------------------------

func TestWeekStartDate_Monday(t *testing.T) {
	// Jan 5 2026 is a Monday
	mon := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	got := weekStartDate(mon)
	if !got.Equal(mon) {
		t.Errorf("weekStartDate(Monday) = %v, want %v", got, mon)
	}
}

func TestWeekStartDate_Wednesday(t *testing.T) {
	wed := time.Date(2026, 1, 7, 12, 30, 0, 0, time.UTC)
	want := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	got := weekStartDate(wed)
	if !got.Equal(want) {
		t.Errorf("weekStartDate(Wednesday) = %v, want %v", got, want)
	}
}

func TestWeekStartDate_Sunday(t *testing.T) {
	sun := time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC)
	want := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	got := weekStartDate(sun)
	if !got.Equal(want) {
		t.Errorf("weekStartDate(Sunday) = %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Batch token price (via service with multiple tokens)
// ---------------------------------------------------------------------------

func TestService_BatchTokenPrices(t *testing.T) {
	tokens := map[string]float64{
		"0xaaaa": 1.0,
		"0xbbbb": 2.5,
		"0xcccc": 0.001,
	}
	srv := mockServerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return whichever token is in the URL.
		for addr, price := range tokens {
			if strings.Contains(r.URL.String(), addr) {
				fmt.Fprintf(w, `{"%s":{"usd":%f}}`, addr, price)
				return
			}
		}
		fmt.Fprint(w, `{}`)
	})
	defer srv.Close()

	config := DefaultConfig()
	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	svc := NewService(cg, config)

	for addr, wantPrice := range tokens {
		prices, err := svc.GetTokenPrice(context.Background(), addr)
		if err != nil {
			t.Fatalf("token %s: %v", addr, err)
		}
		if prices["usd"] != wantPrice {
			t.Errorf("token %s: usd = %f, want %f", addr, prices["usd"], wantPrice)
		}
	}
}

// ---------------------------------------------------------------------------
// CoinGecko request path verification
// ---------------------------------------------------------------------------

func TestCoinGecko_CoinPrice_RequestPath(t *testing.T) {
	var gotPath string
	srv := mockServerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		fmt.Fprint(w, `{"lux-network":{"usd":10.0}}`)
	})
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	_, _ = cg.CoinPrice(context.Background(), "lux-network", []string{"usd", "eur"})
	if !strings.Contains(gotPath, "ids=lux-network") {
		t.Errorf("path missing ids: %s", gotPath)
	}
	if !strings.Contains(gotPath, "vs_currencies=usd") {
		t.Errorf("path missing vs_currencies: %s", gotPath)
	}
}

func TestCoinGecko_TokenPrice_RequestPath(t *testing.T) {
	var gotPath string
	srv := mockServerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		fmt.Fprint(w, `{"0xabc":{"usd":1.0}}`)
	})
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	_, _ = cg.TokenPrice(context.Background(), "lux", "0xabc", []string{"usd"})
	if !strings.Contains(gotPath, "/simple/token_price/lux") {
		t.Errorf("path missing platform: %s", gotPath)
	}
	if !strings.Contains(gotPath, "contract_addresses=0xabc") {
		t.Errorf("path missing contract address: %s", gotPath)
	}
}

func TestCoinGecko_MarketChart_RequestPath(t *testing.T) {
	var gotPath string
	srv := mockServerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		fmt.Fprint(w, `{"prices":[]}`)
	})
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	_, _ = cg.MarketChart(context.Background(), "lux-network", "usd", 14)
	if !strings.Contains(gotPath, "/coins/lux-network/market_chart") {
		t.Errorf("path missing market_chart: %s", gotPath)
	}
	if !strings.Contains(gotPath, "days=14") {
		t.Errorf("path missing days: %s", gotPath)
	}
	if !strings.Contains(gotPath, "interval=daily") {
		t.Errorf("path missing interval=daily: %s", gotPath)
	}
}

// ---------------------------------------------------------------------------
// CoinGecko options
// ---------------------------------------------------------------------------

func TestCoinGeckoDefaults(t *testing.T) {
	cg := NewCoinGecko()
	if cg.baseURL != "https://api.coingecko.com/api/v3" {
		t.Errorf("baseURL = %q", cg.baseURL)
	}
	if cg.apiKey != "" {
		t.Errorf("apiKey = %q, want empty", cg.apiKey)
	}
	if cg.httpClient == nil {
		t.Error("httpClient is nil")
	}
}

func TestCMCDefaults(t *testing.T) {
	cmc := NewCoinMarketCap()
	if cmc.baseURL != "https://pro-api.coinmarketcap.com/v1" {
		t.Errorf("baseURL = %q", cmc.baseURL)
	}
	if cmc.apiKey != "" {
		t.Errorf("apiKey = %q, want empty", cmc.apiKey)
	}
}

// ---------------------------------------------------------------------------
// Source interface compliance
// ---------------------------------------------------------------------------

func TestCoinGecko_ImplementsSource(t *testing.T) {
	var _ Source = (*CoinGecko)(nil)
}

func TestCoinMarketCap_ImplementsSource(t *testing.T) {
	var _ Source = (*CoinMarketCap)(nil)
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestMarketChart_LargeDataset(t *testing.T) {
	var pricesJSON strings.Builder
	pricesJSON.WriteString(`{"prices":[`)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 365; i++ {
		if i > 0 {
			pricesJSON.WriteString(",")
		}
		ts := float64(base.AddDate(0, 0, i).UnixMilli())
		price := 10.0 + float64(i)*0.1
		fmt.Fprintf(&pricesJSON, "[%f,%f]", ts, price)
	}
	pricesJSON.WriteString(`]}`)

	srv := mockServer(http.StatusOK, pricesJSON.String())
	defer srv.Close()

	cg := NewCoinGecko(WithCoinGeckoBaseURL(srv.URL))
	prices, err := cg.MarketChart(context.Background(), "lux-network", "usd", 365)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prices) != 365 {
		t.Errorf("got %d prices, want 365", len(prices))
	}

	// Verify aggregation.
	weekly := AggregateWeekly(prices)
	if len(weekly) < 52 {
		t.Errorf("got %d weeks, want >= 52", len(weekly))
	}

	monthly := AggregateMonthly(prices)
	if len(monthly) != 12 {
		t.Errorf("got %d months, want 12", len(monthly))
	}
}

func TestFillGaps_LargeGap(t *testing.T) {
	prices := []DailyPrice{
		{Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Price: 10.0},
		{Date: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), Price: 20.0},
	}
	filled := FillGaps(prices)
	if len(filled) != 31 {
		t.Errorf("got %d entries, want 31", len(filled))
	}
	// All gap entries should carry forward 10.0.
	for i := 1; i < 30; i++ {
		if filled[i].Price != 10.0 {
			t.Errorf("day %d = %f, want 10.0", i, filled[i].Price)
			break
		}
	}
}

func TestPriceChangePercent_SmallValues(t *testing.T) {
	change := PriceChangePercent(0.001, 0.002)
	if !floatClose(change, 100.0, 0.01) {
		t.Errorf("change = %f, want 100.0", change)
	}
}

func TestIsStablecoin_EdgeCases(t *testing.T) {
	// Exactly at 5% boundary.
	if !IsStablecoin(0.951) {
		t.Error("0.951 should be stablecoin")
	}
	if !IsStablecoin(1.049) {
		t.Error("1.049 should be stablecoin")
	}
	if IsStablecoin(0.949) {
		t.Error("0.949 should NOT be stablecoin")
	}
	if IsStablecoin(1.051) {
		t.Error("1.051 should NOT be stablecoin")
	}
}
