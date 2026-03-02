// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package market provides coin/token price fetching and caching from external
// price oracles (CoinGecko, CoinMarketCap). It powers the /v1/explorer/stats
// coin_price field, token exchange_rate display, and chart market data.
package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Source fetches prices from an external API.
type Source interface {
	// CoinPrice returns the native coin price in the given fiat currencies.
	CoinPrice(ctx context.Context, coinID string, vsCurrencies []string) (map[string]float64, error)

	// TokenPrice returns the token price by contract address.
	TokenPrice(ctx context.Context, platform, contractAddress string, vsCurrencies []string) (map[string]float64, error)

	// MarketChart returns daily closing prices for the given number of days.
	MarketChart(ctx context.Context, coinID string, vsCurrency string, days int) ([]DailyPrice, error)

	// Name returns the source name for logging.
	Name() string
}

// DailyPrice is a single day's closing price from a market chart endpoint.
type DailyPrice struct {
	Date  time.Time `json:"date"`
	Price float64   `json:"price"`
}

// CoinData holds the full market snapshot for a coin.
type CoinData struct {
	Price            map[string]float64 `json:"price"`              // e.g. {"usd": 12.5, "eur": 11.2}
	MarketCap        float64            `json:"market_cap"`         // USD market cap
	Volume24h        float64            `json:"volume_24h"`         // USD 24h volume
	PriceChange24h   float64            `json:"price_change_24h"`   // percentage
	PriceChange7d    float64            `json:"price_change_7d"`    // percentage
	PriceChange30d   float64            `json:"price_change_30d"`   // percentage
	CirculatingSupply float64           `json:"circulating_supply"`
	LastUpdated      time.Time          `json:"last_updated"`
}

// TokenData holds exchange rate data for a token.
type TokenData struct {
	ContractAddress string             `json:"contract_address"`
	Price           map[string]float64 `json:"price"`
	MarketCap       float64            `json:"market_cap,omitempty"`
	Volume24h       float64            `json:"volume_24h,omitempty"`
	LastUpdated     time.Time          `json:"last_updated"`
}

// OHLCV is one candle of market data.
type OHLCV struct {
	Date   time.Time `json:"date"`
	Open   float64   `json:"open"`
	High   float64   `json:"high"`
	Low    float64   `json:"low"`
	Close  float64   `json:"close"`
	Volume float64   `json:"volume,omitempty"`
}

// Config holds market service configuration.
type Config struct {
	CacheTTL       time.Duration // How long to cache prices
	CoinID         string        // CoinGecko coin ID, e.g. "lux-network"
	Platform       string        // CoinGecko platform ID, e.g. "lux"
	DefaultFiat    []string      // Default fiat currencies, e.g. ["usd", "eur", "btc"]
	RequestTimeout time.Duration // HTTP request timeout
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		CacheTTL:       5 * time.Minute,
		CoinID:         "lux-network",
		Platform:       "lux",
		DefaultFiat:    []string{"usd"},
		RequestTimeout: 10 * time.Second,
	}
}

// Service orchestrates price fetching, caching, and aggregation.
type Service struct {
	source Source
	config Config
	cache  *cache
}

// NewService creates a market data service with the given source.
func NewService(source Source, config Config) *Service {
	return &Service{
		source: source,
		config: config,
		cache:  newCache(config.CacheTTL),
	}
}

// GetCoinPrice returns the cached native coin price, refreshing if stale.
func (s *Service) GetCoinPrice(ctx context.Context) (map[string]float64, error) {
	if prices, ok := s.cache.getCoinPrice(); ok {
		return prices, nil
	}
	prices, err := s.source.CoinPrice(ctx, s.config.CoinID, s.config.DefaultFiat)
	if err != nil {
		return nil, fmt.Errorf("fetch coin price: %w", err)
	}
	s.cache.setCoinPrice(prices)
	return prices, nil
}

// GetTokenPrice returns the cached token price, refreshing if stale.
func (s *Service) GetTokenPrice(ctx context.Context, contractAddress string) (map[string]float64, error) {
	contractAddress = strings.ToLower(contractAddress)
	if prices, ok := s.cache.getTokenPrice(contractAddress); ok {
		return prices, nil
	}
	prices, err := s.source.TokenPrice(ctx, s.config.Platform, contractAddress, s.config.DefaultFiat)
	if err != nil {
		return nil, fmt.Errorf("fetch token price: %w", err)
	}
	s.cache.setTokenPrice(contractAddress, prices)
	return prices, nil
}

// GetMarketChart returns daily price history for the native coin.
func (s *Service) GetMarketChart(ctx context.Context, days int) ([]DailyPrice, error) {
	if days <= 0 {
		days = 30
	}
	if days > 365 {
		days = 365
	}
	vsCurrency := "usd"
	if len(s.config.DefaultFiat) > 0 {
		vsCurrency = s.config.DefaultFiat[0]
	}
	return s.source.MarketChart(ctx, s.config.CoinID, vsCurrency, days)
}

// AggregateDailyCandles converts daily prices into OHLCV candles.
// For daily data the open/high/low/close are all the closing price.
func AggregateDailyCandles(prices []DailyPrice) []OHLCV {
	candles := make([]OHLCV, 0, len(prices))
	for _, p := range prices {
		candles = append(candles, OHLCV{
			Date:  p.Date,
			Open:  p.Price,
			High:  p.Price,
			Low:   p.Price,
			Close: p.Price,
		})
	}
	return candles
}

// AggregateWeekly groups daily prices into weekly candles (Mon-Sun).
func AggregateWeekly(prices []DailyPrice) []OHLCV {
	if len(prices) == 0 {
		return nil
	}
	var candles []OHLCV
	var current *OHLCV
	var currentWeekStart time.Time

	for _, p := range prices {
		weekStart := weekStartDate(p.Date)
		if current == nil || !weekStart.Equal(currentWeekStart) {
			if current != nil {
				candles = append(candles, *current)
			}
			current = &OHLCV{
				Date: weekStart,
				Open: p.Price, High: p.Price, Low: p.Price, Close: p.Price,
			}
			currentWeekStart = weekStart
		} else {
			if p.Price > current.High {
				current.High = p.Price
			}
			if p.Price < current.Low {
				current.Low = p.Price
			}
			current.Close = p.Price
		}
	}
	if current != nil {
		candles = append(candles, *current)
	}
	return candles
}

// AggregateMonthly groups daily prices into monthly candles.
func AggregateMonthly(prices []DailyPrice) []OHLCV {
	if len(prices) == 0 {
		return nil
	}
	var candles []OHLCV
	var current *OHLCV
	var currentMonth time.Month
	var currentYear int

	for _, p := range prices {
		y, m, _ := p.Date.Date()
		if current == nil || m != currentMonth || y != currentYear {
			if current != nil {
				candles = append(candles, *current)
			}
			current = &OHLCV{
				Date: time.Date(y, m, 1, 0, 0, 0, 0, time.UTC),
				Open: p.Price, High: p.Price, Low: p.Price, Close: p.Price,
			}
			currentMonth = m
			currentYear = y
		} else {
			if p.Price > current.High {
				current.High = p.Price
			}
			if p.Price < current.Low {
				current.Low = p.Price
			}
			current.Close = p.Price
		}
	}
	if current != nil {
		candles = append(candles, *current)
	}
	return candles
}

// FillGaps fills missing dates in a sorted daily price series with the
// previous day's price (carry forward).
func FillGaps(prices []DailyPrice) []DailyPrice {
	if len(prices) < 2 {
		return prices
	}
	result := make([]DailyPrice, 0, len(prices))
	result = append(result, prices[0])

	for i := 1; i < len(prices); i++ {
		prev := prices[i-1]
		cur := prices[i]
		gap := cur.Date.Sub(prev.Date)
		days := int(gap.Hours() / 24)
		for d := 1; d < days; d++ {
			result = append(result, DailyPrice{
				Date:  prev.Date.AddDate(0, 0, d),
				Price: prev.Price,
			})
		}
		result = append(result, cur)
	}
	return result
}

// CalculateMarketCap returns price * circulating supply.
func CalculateMarketCap(price, circulatingSupply float64) float64 {
	return price * circulatingSupply
}

// PriceChangePercent calculates the percentage change between old and new.
func PriceChangePercent(oldPrice, newPrice float64) float64 {
	if oldPrice == 0 {
		return 0
	}
	return ((newPrice - oldPrice) / oldPrice) * 100
}

// IsStablecoin returns true if the USD price is approximately $1.00 (within 5%).
func IsStablecoin(usdPrice float64) bool {
	return math.Abs(usdPrice-1.0) < 0.05
}

// weekStartDate returns the Monday of the week containing t.
func weekStartDate(t time.Time) time.Time {
	t = t.UTC().Truncate(24 * time.Hour)
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return t.AddDate(0, 0, -(weekday - 1))
}

// ---------------------------------------------------------------------------
// Cache
// ---------------------------------------------------------------------------

type cache struct {
	mu          sync.RWMutex
	ttl         time.Duration
	coinPrice   map[string]float64
	coinUpdated time.Time
	tokens      map[string]tokenCacheEntry
}

type tokenCacheEntry struct {
	prices  map[string]float64
	updated time.Time
}

func newCache(ttl time.Duration) *cache {
	return &cache{
		ttl:    ttl,
		tokens: make(map[string]tokenCacheEntry),
	}
}

func (c *cache) getCoinPrice() (map[string]float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.coinPrice != nil && time.Since(c.coinUpdated) < c.ttl {
		return c.coinPrice, true
	}
	return nil, false
}

func (c *cache) setCoinPrice(prices map[string]float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.coinPrice = prices
	c.coinUpdated = time.Now()
}

func (c *cache) getTokenPrice(addr string) (map[string]float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.tokens[addr]
	if !ok || time.Since(entry.updated) >= c.ttl {
		return nil, false
	}
	return entry.prices, true
}

func (c *cache) setTokenPrice(addr string, prices map[string]float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens[addr] = tokenCacheEntry{prices: prices, updated: time.Now()}
}

// ---------------------------------------------------------------------------
// CoinGecko source
// ---------------------------------------------------------------------------

// CoinGecko implements Source using the CoinGecko API.
type CoinGecko struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// CoinGeckoOption configures a CoinGecko source.
type CoinGeckoOption func(*CoinGecko)

// WithCoinGeckoBaseURL overrides the default base URL (useful for testing).
func WithCoinGeckoBaseURL(u string) CoinGeckoOption {
	return func(cg *CoinGecko) { cg.baseURL = u }
}

// WithCoinGeckoAPIKey sets a pro API key.
func WithCoinGeckoAPIKey(key string) CoinGeckoOption {
	return func(cg *CoinGecko) { cg.apiKey = key }
}

// WithCoinGeckoHTTPClient sets a custom HTTP client.
func WithCoinGeckoHTTPClient(c *http.Client) CoinGeckoOption {
	return func(cg *CoinGecko) { cg.httpClient = c }
}

// NewCoinGecko creates a CoinGecko price source.
func NewCoinGecko(opts ...CoinGeckoOption) *CoinGecko {
	cg := &CoinGecko{
		baseURL:    "https://api.coingecko.com/api/v3",
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	for _, o := range opts {
		o(cg)
	}
	return cg
}

func (cg *CoinGecko) Name() string { return "coingecko" }

// CoinPrice calls /simple/price.
func (cg *CoinGecko) CoinPrice(ctx context.Context, coinID string, vsCurrencies []string) (map[string]float64, error) {
	u := fmt.Sprintf("%s/simple/price?ids=%s&vs_currencies=%s&include_market_cap=true&include_24hr_vol=true&include_24hr_change=true",
		cg.baseURL, url.QueryEscape(coinID), url.QueryEscape(strings.Join(vsCurrencies, ",")))

	body, err := cg.get(ctx, u)
	if err != nil {
		return nil, err
	}

	// {"lux-network": {"usd": 12.5, "usd_market_cap": ..., ...}}
	var result map[string]map[string]float64
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("coingecko: parse price response: %w", err)
	}

	coinData, ok := result[coinID]
	if !ok {
		return nil, fmt.Errorf("coingecko: coin %q not found in response", coinID)
	}

	prices := make(map[string]float64)
	for _, cur := range vsCurrencies {
		if v, ok := coinData[cur]; ok {
			prices[cur] = v
		}
	}
	return prices, nil
}

// TokenPrice calls /simple/token_price/{platform}.
func (cg *CoinGecko) TokenPrice(ctx context.Context, platform, contractAddress string, vsCurrencies []string) (map[string]float64, error) {
	contractAddress = strings.ToLower(contractAddress)
	u := fmt.Sprintf("%s/simple/token_price/%s?contract_addresses=%s&vs_currencies=%s",
		cg.baseURL, url.QueryEscape(platform), url.QueryEscape(contractAddress),
		url.QueryEscape(strings.Join(vsCurrencies, ",")))

	body, err := cg.get(ctx, u)
	if err != nil {
		return nil, err
	}

	// {"0xaddr": {"usd": 1.23}}
	var result map[string]map[string]float64
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("coingecko: parse token price response: %w", err)
	}

	tokenData, ok := result[contractAddress]
	if !ok {
		return nil, fmt.Errorf("coingecko: token %q not found in response", contractAddress)
	}

	prices := make(map[string]float64)
	for _, cur := range vsCurrencies {
		if v, ok := tokenData[cur]; ok {
			prices[cur] = v
		}
	}
	return prices, nil
}

// MarketChart calls /coins/{id}/market_chart.
func (cg *CoinGecko) MarketChart(ctx context.Context, coinID string, vsCurrency string, days int) ([]DailyPrice, error) {
	u := fmt.Sprintf("%s/coins/%s/market_chart?vs_currency=%s&days=%d&interval=daily",
		cg.baseURL, url.QueryEscape(coinID), url.QueryEscape(vsCurrency), days)

	body, err := cg.get(ctx, u)
	if err != nil {
		return nil, err
	}

	var result struct {
		Prices [][]float64 `json:"prices"` // [[timestamp_ms, price], ...]
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("coingecko: parse market chart response: %w", err)
	}

	prices := make([]DailyPrice, 0, len(result.Prices))
	for _, p := range result.Prices {
		if len(p) < 2 {
			continue
		}
		prices = append(prices, DailyPrice{
			Date:  time.UnixMilli(int64(p[0])).UTC().Truncate(24 * time.Hour),
			Price: p[1],
		})
	}
	return prices, nil
}

func (cg *CoinGecko) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("coingecko: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if cg.apiKey != "" {
		req.Header.Set("x-cg-pro-api-key", cg.apiKey)
	}

	resp, err := cg.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("coingecko: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("coingecko: rate limited (429)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coingecko: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("coingecko: read body: %w", err)
	}
	return body, nil
}

// ---------------------------------------------------------------------------
// CoinMarketCap source
// ---------------------------------------------------------------------------

// CoinMarketCap implements Source using the CoinMarketCap API.
type CoinMarketCap struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// CoinMarketCapOption configures a CoinMarketCap source.
type CoinMarketCapOption func(*CoinMarketCap)

// WithCMCBaseURL overrides the default base URL.
func WithCMCBaseURL(u string) CoinMarketCapOption {
	return func(cmc *CoinMarketCap) { cmc.baseURL = u }
}

// WithCMCAPIKey sets the API key.
func WithCMCAPIKey(key string) CoinMarketCapOption {
	return func(cmc *CoinMarketCap) { cmc.apiKey = key }
}

// WithCMCHTTPClient sets a custom HTTP client.
func WithCMCHTTPClient(c *http.Client) CoinMarketCapOption {
	return func(cmc *CoinMarketCap) { cmc.httpClient = c }
}

// NewCoinMarketCap creates a CoinMarketCap price source.
func NewCoinMarketCap(opts ...CoinMarketCapOption) *CoinMarketCap {
	cmc := &CoinMarketCap{
		baseURL:    "https://pro-api.coinmarketcap.com/v1",
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	for _, o := range opts {
		o(cmc)
	}
	return cmc
}

func (cmc *CoinMarketCap) Name() string { return "coinmarketcap" }

// CoinPrice calls /cryptocurrency/quotes/latest.
func (cmc *CoinMarketCap) CoinPrice(ctx context.Context, coinID string, vsCurrencies []string) (map[string]float64, error) {
	convert := strings.ToUpper(strings.Join(vsCurrencies, ","))
	u := fmt.Sprintf("%s/cryptocurrency/quotes/latest?slug=%s&convert=%s",
		cmc.baseURL, url.QueryEscape(coinID), url.QueryEscape(convert))

	body, err := cmc.get(ctx, u)
	if err != nil {
		return nil, err
	}

	var result cmcQuotesResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("coinmarketcap: parse response: %w", err)
	}

	if result.Status.ErrorCode != 0 {
		return nil, fmt.Errorf("coinmarketcap: api error %d: %s", result.Status.ErrorCode, result.Status.ErrorMessage)
	}

	prices := make(map[string]float64)
	for _, coin := range result.Data {
		for cur, quote := range coin.Quote {
			prices[strings.ToLower(cur)] = quote.Price
		}
	}
	return prices, nil
}

// TokenPrice is not directly supported by CMC free API; returns an error.
func (cmc *CoinMarketCap) TokenPrice(ctx context.Context, platform, contractAddress string, vsCurrencies []string) (map[string]float64, error) {
	convert := strings.ToUpper(strings.Join(vsCurrencies, ","))
	u := fmt.Sprintf("%s/cryptocurrency/quotes/latest?address=%s&convert=%s",
		cmc.baseURL, url.QueryEscape(contractAddress), url.QueryEscape(convert))

	body, err := cmc.get(ctx, u)
	if err != nil {
		return nil, err
	}

	var result cmcQuotesResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("coinmarketcap: parse token response: %w", err)
	}

	if result.Status.ErrorCode != 0 {
		return nil, fmt.Errorf("coinmarketcap: api error %d: %s", result.Status.ErrorCode, result.Status.ErrorMessage)
	}

	prices := make(map[string]float64)
	for _, coin := range result.Data {
		for cur, quote := range coin.Quote {
			prices[strings.ToLower(cur)] = quote.Price
		}
	}
	return prices, nil
}

// MarketChart is not available on CMC free tier; returns error.
func (cmc *CoinMarketCap) MarketChart(_ context.Context, _ string, _ string, _ int) ([]DailyPrice, error) {
	return nil, fmt.Errorf("coinmarketcap: market chart not available (requires paid plan)")
}

func (cmc *CoinMarketCap) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("coinmarketcap: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if cmc.apiKey != "" {
		req.Header.Set("X-CMC_PRO_API_KEY", cmc.apiKey)
	}

	resp, err := cmc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("coinmarketcap: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("coinmarketcap: rate limited (429)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coinmarketcap: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("coinmarketcap: read body: %w", err)
	}
	return body, nil
}

// CMC response types
type cmcQuotesResponse struct {
	Status cmcStatus               `json:"status"`
	Data   map[string]cmcCoinEntry `json:"data"`
}

type cmcStatus struct {
	ErrorCode    int    `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

type cmcCoinEntry struct {
	ID     int                    `json:"id"`
	Name   string                 `json:"name"`
	Symbol string                 `json:"symbol"`
	Slug   string                 `json:"slug"`
	Quote  map[string]cmcQuoteVal `json:"quote"`
}

type cmcQuoteVal struct {
	Price            float64 `json:"price"`
	Volume24h        float64 `json:"volume_24h"`
	MarketCap        float64 `json:"market_cap"`
	PercentChange24h float64 `json:"percent_change_24h"`
	PercentChange7d  float64 `json:"percent_change_7d"`
	PercentChange30d float64 `json:"percent_change_30d"`
}
