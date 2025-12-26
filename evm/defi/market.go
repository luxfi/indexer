// Package defi provides DeFi protocol indexing for the Lux EVM indexer.
package defi

import (
	"math/big"
	"sort"
	"sync"
	"time"
)

// CandleInterval represents candle/OHLCV time intervals
type CandleInterval string

const (
	Candle1m  CandleInterval = "1m"
	Candle5m  CandleInterval = "5m"
	Candle15m CandleInterval = "15m"
	Candle30m CandleInterval = "30m"
	Candle1h  CandleInterval = "1h"
	Candle4h  CandleInterval = "4h"
	Candle1d  CandleInterval = "1d"
	Candle1w  CandleInterval = "1w"
)

// OHLCV represents Open-High-Low-Close-Volume candle data
type OHLCV struct {
	Market     string         `json:"market"`
	Interval   CandleInterval `json:"interval"`
	OpenTime   time.Time      `json:"openTime"`
	CloseTime  time.Time      `json:"closeTime"`
	Open       *big.Int       `json:"open"`
	High       *big.Int       `json:"high"`
	Low        *big.Int       `json:"low"`
	Close      *big.Int       `json:"close"`
	Volume     *big.Int       `json:"volume"`      // Base asset volume
	QuoteVolume *big.Int      `json:"quoteVolume"` // Quote asset volume
	Trades     uint64         `json:"trades"`
	BuyVolume  *big.Int       `json:"buyVolume"`
	SellVolume *big.Int       `json:"sellVolume"`
	VWAP       *big.Int       `json:"vwap"`        // Volume-weighted average price
	Finalized  bool           `json:"finalized"`
}

// MarketTicker represents current market ticker data
type MarketTicker struct {
	Market        string    `json:"market"`
	LastPrice     *big.Int  `json:"lastPrice"`
	BidPrice      *big.Int  `json:"bidPrice"`
	AskPrice      *big.Int  `json:"askPrice"`
	High24h       *big.Int  `json:"high24h"`
	Low24h        *big.Int  `json:"low24h"`
	Volume24h     *big.Int  `json:"volume24h"`
	QuoteVolume24h *big.Int `json:"quoteVolume24h"`
	OpenPrice24h  *big.Int  `json:"openPrice24h"`
	PriceChange   *big.Int  `json:"priceChange"`
	PriceChangePercent float64 `json:"priceChangePercent"`
	Trades24h     uint64    `json:"trades24h"`
	LastUpdated   time.Time `json:"lastUpdated"`
}

// TradeHistory represents a historical trade record
type TradeHistory struct {
	ID          string    `json:"id"`
	Market      string    `json:"market"`
	Price       *big.Int  `json:"price"`
	Size        *big.Int  `json:"size"`
	QuoteSize   *big.Int  `json:"quoteSize"`
	Side        OrderSide `json:"side"`
	Timestamp   time.Time `json:"timestamp"`
	TxHash      string    `json:"txHash"`
	BlockNumber uint64    `json:"blockNumber"`
	Maker       string    `json:"maker"`
	Taker       string    `json:"taker"`
}

// LiquiditySnapshot represents a point-in-time liquidity snapshot
type LiquiditySnapshot struct {
	Market       string    `json:"market"`
	Timestamp    time.Time `json:"timestamp"`
	TotalLiquidity *big.Int `json:"totalLiquidity"`
	Token0Locked *big.Int  `json:"token0Locked"`
	Token1Locked *big.Int  `json:"token1Locked"`
	LPTokens     *big.Int  `json:"lpTokens"`
	TVL          *big.Int  `json:"tvl"` // USD value
}

// VolumeStats represents volume statistics
type VolumeStats struct {
	Market       string              `json:"market"`
	Period       string              `json:"period"` // "1h", "24h", "7d", "30d"
	Volume       *big.Int            `json:"volume"`
	QuoteVolume  *big.Int            `json:"quoteVolume"`
	Trades       uint64              `json:"trades"`
	BuyVolume    *big.Int            `json:"buyVolume"`
	SellVolume   *big.Int            `json:"sellVolume"`
	UniqueTraders uint64             `json:"uniqueTraders"`
	AverageTradeSize *big.Int        `json:"averageTradeSize"`
	VolumeByHour map[int]*big.Int    `json:"volumeByHour,omitempty"`
	StartTime    time.Time           `json:"startTime"`
	EndTime      time.Time           `json:"endTime"`
}

// PricePoint represents a simple price-time point
type PricePoint struct {
	Price     *big.Int  `json:"price"`
	Timestamp time.Time `json:"timestamp"`
}

// MarketDepth represents order book depth
type MarketDepth struct {
	Market    string            `json:"market"`
	Bids      [][2]*big.Int     `json:"bids"`  // [price, size]
	Asks      [][2]*big.Int     `json:"asks"`  // [price, size]
	BidDepth  *big.Int          `json:"bidDepth"`
	AskDepth  *big.Int          `json:"askDepth"`
	Spread    *big.Int          `json:"spread"`
	SpreadPercent float64       `json:"spreadPercent"`
	Timestamp time.Time         `json:"timestamp"`
}

// MarketHistoryIndexer manages market history data
type MarketHistoryIndexer struct {
	mu               sync.RWMutex
	candles          map[string]map[CandleInterval][]*OHLCV // market -> interval -> candles
	currentCandles   map[string]map[CandleInterval]*OHLCV   // Current (unfinalised) candles
	tickers          map[string]*MarketTicker
	trades           map[string][]*TradeHistory
	priceHistory     map[string][]*PricePoint
	liquidityHistory map[string][]*LiquiditySnapshot
	volumeStats      map[string]map[string]*VolumeStats // market -> period -> stats
	depth            map[string]*MarketDepth
	lastBlockTime    time.Time
	tradesByBlock    map[uint64][]*TradeHistory
}

// NewMarketHistoryIndexer creates a new market history indexer
func NewMarketHistoryIndexer() *MarketHistoryIndexer {
	return &MarketHistoryIndexer{
		candles:          make(map[string]map[CandleInterval][]*OHLCV),
		currentCandles:   make(map[string]map[CandleInterval]*OHLCV),
		tickers:          make(map[string]*MarketTicker),
		trades:           make(map[string][]*TradeHistory),
		priceHistory:     make(map[string][]*PricePoint),
		liquidityHistory: make(map[string][]*LiquiditySnapshot),
		volumeStats:      make(map[string]map[string]*VolumeStats),
		depth:            make(map[string]*MarketDepth),
		tradesByBlock:    make(map[uint64][]*TradeHistory),
	}
}

// RecordTrade records a trade for history tracking
func (m *MarketHistoryIndexer) RecordTrade(trade *TradeHistory) {
	m.mu.Lock()
	defer m.mu.Unlock()

	market := trade.Market

	// Store trade
	m.trades[market] = append(m.trades[market], trade)
	m.tradesByBlock[trade.BlockNumber] = append(m.tradesByBlock[trade.BlockNumber], trade)

	// Record price point
	m.priceHistory[market] = append(m.priceHistory[market], &PricePoint{
		Price:     trade.Price,
		Timestamp: trade.Timestamp,
	})

	// Update candles for all intervals
	intervals := []CandleInterval{Candle1m, Candle5m, Candle15m, Candle30m, Candle1h, Candle4h, Candle1d, Candle1w}
	for _, interval := range intervals {
		m.updateCandle(market, interval, trade)
	}

	// Update ticker
	m.updateTicker(market, trade)

	// Update volume stats
	m.updateVolumeStats(market, trade)
}

// updateCandle updates candle data for a specific interval
func (m *MarketHistoryIndexer) updateCandle(market string, interval CandleInterval, trade *TradeHistory) {
	// Ensure maps exist
	if m.candles[market] == nil {
		m.candles[market] = make(map[CandleInterval][]*OHLCV)
	}
	if m.currentCandles[market] == nil {
		m.currentCandles[market] = make(map[CandleInterval]*OHLCV)
	}

	// Get candle boundary
	openTime := getCandleOpenTime(trade.Timestamp, interval)
	closeTime := getCandleCloseTime(openTime, interval)

	// Check if current candle exists and is valid
	current := m.currentCandles[market][interval]

	if current == nil || !current.OpenTime.Equal(openTime) {
		// Finalize previous candle if exists
		if current != nil {
			current.Finalized = true
			m.candles[market][interval] = append(m.candles[market][interval], current)
		}

		// Create new candle
		current = &OHLCV{
			Market:      market,
			Interval:    interval,
			OpenTime:    openTime,
			CloseTime:   closeTime,
			Open:        trade.Price,
			High:        trade.Price,
			Low:         trade.Price,
			Close:       trade.Price,
			Volume:      big.NewInt(0),
			QuoteVolume: big.NewInt(0),
			BuyVolume:   big.NewInt(0),
			SellVolume:  big.NewInt(0),
			Trades:      0,
			Finalized:   false,
		}
		m.currentCandles[market][interval] = current
	}

	// Update candle
	if trade.Price.Cmp(current.High) > 0 {
		current.High = trade.Price
	}
	if trade.Price.Cmp(current.Low) < 0 {
		current.Low = trade.Price
	}
	current.Close = trade.Price
	current.Volume = new(big.Int).Add(current.Volume, trade.Size)
	current.QuoteVolume = new(big.Int).Add(current.QuoteVolume, trade.QuoteSize)
	current.Trades++

	if trade.Side == OrderSideBuy {
		current.BuyVolume = new(big.Int).Add(current.BuyVolume, trade.Size)
	} else {
		current.SellVolume = new(big.Int).Add(current.SellVolume, trade.Size)
	}

	// Calculate VWAP
	if current.Volume.Sign() > 0 {
		current.VWAP = new(big.Int).Div(current.QuoteVolume, current.Volume)
	}
}

// updateTicker updates market ticker
func (m *MarketHistoryIndexer) updateTicker(market string, trade *TradeHistory) {
	ticker, ok := m.tickers[market]
	if !ok {
		ticker = &MarketTicker{
			Market:    market,
			High24h:   big.NewInt(0),
			Low24h:    big.NewInt(0),
			Volume24h: big.NewInt(0),
			QuoteVolume24h: big.NewInt(0),
		}
		m.tickers[market] = ticker
	}

	ticker.LastPrice = trade.Price
	ticker.LastUpdated = trade.Timestamp

	// Update 24h stats
	cutoff := trade.Timestamp.Add(-24 * time.Hour)

	if trades, ok := m.trades[market]; ok {
		high := big.NewInt(0)
		low := (*big.Int)(nil)
		volume := big.NewInt(0)
		quoteVolume := big.NewInt(0)
		var tradeCount uint64
		var openPrice *big.Int

		for _, t := range trades {
			if t.Timestamp.After(cutoff) {
				if openPrice == nil {
					openPrice = t.Price
				}
				if t.Price.Cmp(high) > 0 {
					high = t.Price
				}
				if low == nil || t.Price.Cmp(low) < 0 {
					low = t.Price
				}
				volume.Add(volume, t.Size)
				quoteVolume.Add(quoteVolume, t.QuoteSize)
				tradeCount++
			}
		}

		ticker.High24h = high
		if low != nil {
			ticker.Low24h = low
		}
		ticker.Volume24h = volume
		ticker.QuoteVolume24h = quoteVolume
		ticker.Trades24h = tradeCount

		if openPrice != nil {
			ticker.OpenPrice24h = openPrice
			ticker.PriceChange = new(big.Int).Sub(trade.Price, openPrice)
			if openPrice.Sign() > 0 {
				changePercent := new(big.Int).Mul(ticker.PriceChange, big.NewInt(10000))
				changePercent.Div(changePercent, openPrice)
				ticker.PriceChangePercent = float64(changePercent.Int64()) / 100.0
			}
		}
	}
}

// updateVolumeStats updates volume statistics
func (m *MarketHistoryIndexer) updateVolumeStats(market string, trade *TradeHistory) {
	if m.volumeStats[market] == nil {
		m.volumeStats[market] = make(map[string]*VolumeStats)
	}

	periods := []string{"1h", "24h", "7d", "30d"}
	durations := []time.Duration{time.Hour, 24 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour}

	for i, period := range periods {
		stats, ok := m.volumeStats[market][period]
		if !ok {
			stats = &VolumeStats{
				Market:      market,
				Period:      period,
				Volume:      big.NewInt(0),
				QuoteVolume: big.NewInt(0),
				BuyVolume:   big.NewInt(0),
				SellVolume:  big.NewInt(0),
				AverageTradeSize: big.NewInt(0),
				StartTime:   trade.Timestamp.Add(-durations[i]),
				EndTime:     trade.Timestamp,
			}
			m.volumeStats[market][period] = stats
		}

		// Update running totals
		stats.Volume = new(big.Int).Add(stats.Volume, trade.Size)
		stats.QuoteVolume = new(big.Int).Add(stats.QuoteVolume, trade.QuoteSize)
		stats.Trades++

		if trade.Side == OrderSideBuy {
			stats.BuyVolume = new(big.Int).Add(stats.BuyVolume, trade.Size)
		} else {
			stats.SellVolume = new(big.Int).Add(stats.SellVolume, trade.Size)
		}

		if stats.Trades > 0 {
			stats.AverageTradeSize = new(big.Int).Div(stats.Volume, big.NewInt(int64(stats.Trades)))
		}

		stats.EndTime = trade.Timestamp
	}
}

// RecordLiquidity records a liquidity snapshot
func (m *MarketHistoryIndexer) RecordLiquidity(snapshot *LiquiditySnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.liquidityHistory[snapshot.Market] = append(m.liquidityHistory[snapshot.Market], snapshot)
}

// UpdateDepth updates order book depth
func (m *MarketHistoryIndexer) UpdateDepth(market string, bids, asks [][2]*big.Int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	depth := &MarketDepth{
		Market:    market,
		Bids:      bids,
		Asks:      asks,
		BidDepth:  big.NewInt(0),
		AskDepth:  big.NewInt(0),
		Timestamp: time.Now(),
	}

	// Calculate total depths
	for _, bid := range bids {
		depth.BidDepth = new(big.Int).Add(depth.BidDepth, bid[1])
	}
	for _, ask := range asks {
		depth.AskDepth = new(big.Int).Add(depth.AskDepth, ask[1])
	}

	// Calculate spread
	if len(bids) > 0 && len(asks) > 0 {
		depth.Spread = new(big.Int).Sub(asks[0][0], bids[0][0])
		if bids[0][0].Sign() > 0 {
			spreadPercent := new(big.Int).Mul(depth.Spread, big.NewInt(10000))
			spreadPercent.Div(spreadPercent, bids[0][0])
			depth.SpreadPercent = float64(spreadPercent.Int64()) / 100.0
		}
	}

	m.depth[market] = depth
}

// Query methods

// GetCandles returns candles for a market and interval
func (m *MarketHistoryIndexer) GetCandles(market string, interval CandleInterval, limit int) []*OHLCV {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*OHLCV

	// Get finalized candles
	if candles, ok := m.candles[market]; ok {
		if intervalCandles, ok := candles[interval]; ok {
			result = append(result, intervalCandles...)
		}
	}

	// Add current candle
	if current, ok := m.currentCandles[market]; ok {
		if currentCandle, ok := current[interval]; ok {
			result = append(result, currentCandle)
		}
	}

	// Apply limit
	if limit > 0 && len(result) > limit {
		result = result[len(result)-limit:]
	}

	return result
}

// GetCandlesInRange returns candles within a time range
func (m *MarketHistoryIndexer) GetCandlesInRange(market string, interval CandleInterval, start, end time.Time) []*OHLCV {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*OHLCV

	if candles, ok := m.candles[market]; ok {
		if intervalCandles, ok := candles[interval]; ok {
			for _, candle := range intervalCandles {
				if (candle.OpenTime.Equal(start) || candle.OpenTime.After(start)) &&
					(candle.CloseTime.Equal(end) || candle.CloseTime.Before(end)) {
					result = append(result, candle)
				}
			}
		}
	}

	return result
}

// GetTicker returns current ticker for a market
func (m *MarketHistoryIndexer) GetTicker(market string) (*MarketTicker, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ticker, ok := m.tickers[market]
	return ticker, ok
}

// GetAllTickers returns all market tickers
func (m *MarketHistoryIndexer) GetAllTickers() map[string]*MarketTicker {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*MarketTicker)
	for k, v := range m.tickers {
		result[k] = v
	}
	return result
}

// GetRecentTrades returns recent trades for a market
func (m *MarketHistoryIndexer) GetRecentTrades(market string, limit int) []*TradeHistory {
	m.mu.RLock()
	defer m.mu.RUnlock()

	trades := m.trades[market]
	if limit > 0 && len(trades) > limit {
		return trades[len(trades)-limit:]
	}
	return trades
}

// GetTradesInRange returns trades within a time range
func (m *MarketHistoryIndexer) GetTradesInRange(market string, start, end time.Time) []*TradeHistory {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*TradeHistory
	for _, trade := range m.trades[market] {
		if (trade.Timestamp.Equal(start) || trade.Timestamp.After(start)) &&
			(trade.Timestamp.Equal(end) || trade.Timestamp.Before(end)) {
			result = append(result, trade)
		}
	}
	return result
}

// GetPriceHistory returns price history for a market
func (m *MarketHistoryIndexer) GetPriceHistory(market string, limit int) []*PricePoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	prices := m.priceHistory[market]
	if limit > 0 && len(prices) > limit {
		return prices[len(prices)-limit:]
	}
	return prices
}

// GetLiquidityHistory returns liquidity history for a market
func (m *MarketHistoryIndexer) GetLiquidityHistory(market string, limit int) []*LiquiditySnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshots := m.liquidityHistory[market]
	if limit > 0 && len(snapshots) > limit {
		return snapshots[len(snapshots)-limit:]
	}
	return snapshots
}

// GetVolumeStats returns volume statistics for a market
func (m *MarketHistoryIndexer) GetVolumeStats(market, period string) (*VolumeStats, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if stats, ok := m.volumeStats[market]; ok {
		if periodStats, ok := stats[period]; ok {
			return periodStats, true
		}
	}
	return nil, false
}

// GetDepth returns order book depth for a market
func (m *MarketHistoryIndexer) GetDepth(market string) (*MarketDepth, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	depth, ok := m.depth[market]
	return depth, ok
}

// GetMarketSummary returns a comprehensive market summary
func (m *MarketHistoryIndexer) GetMarketSummary(market string) *MarketSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	summary := &MarketSummary{
		Market:    market,
		Timestamp: time.Now(),
	}

	// Get ticker
	if ticker, ok := m.tickers[market]; ok {
		summary.Ticker = ticker
	}

	// Get 24h volume
	if stats, ok := m.volumeStats[market]; ok {
		if vol24h, ok := stats["24h"]; ok {
			summary.Volume24h = vol24h
		}
	}

	// Get depth
	if depth, ok := m.depth[market]; ok {
		summary.Depth = depth
	}

	// Get latest candles
	summary.Candle1h = m.getLatestCandle(market, Candle1h)
	summary.Candle1d = m.getLatestCandle(market, Candle1d)

	// Get latest liquidity
	if liq, ok := m.liquidityHistory[market]; ok && len(liq) > 0 {
		summary.Liquidity = liq[len(liq)-1]
	}

	return summary
}

func (m *MarketHistoryIndexer) getLatestCandle(market string, interval CandleInterval) *OHLCV {
	// Check current candle first
	if current, ok := m.currentCandles[market]; ok {
		if candle, ok := current[interval]; ok {
			return candle
		}
	}

	// Fall back to latest finalized
	if candles, ok := m.candles[market]; ok {
		if intervalCandles, ok := candles[interval]; ok && len(intervalCandles) > 0 {
			return intervalCandles[len(intervalCandles)-1]
		}
	}

	return nil
}

// GetTopMarkets returns top markets by volume
func (m *MarketHistoryIndexer) GetTopMarkets(limit int) []*MarketTicker {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tickers := make([]*MarketTicker, 0, len(m.tickers))
	for _, ticker := range m.tickers {
		tickers = append(tickers, ticker)
	}

	// Sort by 24h volume descending
	sort.Slice(tickers, func(i, j int) bool {
		return tickers[i].Volume24h.Cmp(tickers[j].Volume24h) > 0
	})

	if limit > 0 && len(tickers) > limit {
		return tickers[:limit]
	}
	return tickers
}

// GetTopGainers returns top gaining markets
func (m *MarketHistoryIndexer) GetTopGainers(limit int) []*MarketTicker {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tickers := make([]*MarketTicker, 0, len(m.tickers))
	for _, ticker := range m.tickers {
		if ticker.PriceChangePercent > 0 {
			tickers = append(tickers, ticker)
		}
	}

	sort.Slice(tickers, func(i, j int) bool {
		return tickers[i].PriceChangePercent > tickers[j].PriceChangePercent
	})

	if limit > 0 && len(tickers) > limit {
		return tickers[:limit]
	}
	return tickers
}

// GetTopLosers returns top losing markets
func (m *MarketHistoryIndexer) GetTopLosers(limit int) []*MarketTicker {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tickers := make([]*MarketTicker, 0, len(m.tickers))
	for _, ticker := range m.tickers {
		if ticker.PriceChangePercent < 0 {
			tickers = append(tickers, ticker)
		}
	}

	sort.Slice(tickers, func(i, j int) bool {
		return tickers[i].PriceChangePercent < tickers[j].PriceChangePercent
	})

	if limit > 0 && len(tickers) > limit {
		return tickers[:limit]
	}
	return tickers
}

// MarketSummary provides a comprehensive market overview
type MarketSummary struct {
	Market    string              `json:"market"`
	Ticker    *MarketTicker       `json:"ticker"`
	Volume24h *VolumeStats        `json:"volume24h"`
	Depth     *MarketDepth        `json:"depth"`
	Candle1h  *OHLCV              `json:"candle1h"`
	Candle1d  *OHLCV              `json:"candle1d"`
	Liquidity *LiquiditySnapshot  `json:"liquidity"`
	Timestamp time.Time           `json:"timestamp"`
}

// Helper functions

func getCandleOpenTime(t time.Time, interval CandleInterval) time.Time {
	switch interval {
	case Candle1m:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, t.Location())
	case Candle5m:
		minute := (t.Minute() / 5) * 5
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), minute, 0, 0, t.Location())
	case Candle15m:
		minute := (t.Minute() / 15) * 15
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), minute, 0, 0, t.Location())
	case Candle30m:
		minute := (t.Minute() / 30) * 30
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), minute, 0, 0, t.Location())
	case Candle1h:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
	case Candle4h:
		hour := (t.Hour() / 4) * 4
		return time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, t.Location())
	case Candle1d:
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	case Candle1w:
		// Start of week (Sunday)
		weekday := int(t.Weekday())
		return time.Date(t.Year(), t.Month(), t.Day()-weekday, 0, 0, 0, 0, t.Location())
	default:
		return t
	}
}

func getCandleCloseTime(openTime time.Time, interval CandleInterval) time.Time {
	switch interval {
	case Candle1m:
		return openTime.Add(time.Minute)
	case Candle5m:
		return openTime.Add(5 * time.Minute)
	case Candle15m:
		return openTime.Add(15 * time.Minute)
	case Candle30m:
		return openTime.Add(30 * time.Minute)
	case Candle1h:
		return openTime.Add(time.Hour)
	case Candle4h:
		return openTime.Add(4 * time.Hour)
	case Candle1d:
		return openTime.Add(24 * time.Hour)
	case Candle1w:
		return openTime.Add(7 * 24 * time.Hour)
	default:
		return openTime
	}
}

// Cleanup removes old data beyond retention period
func (m *MarketHistoryIndexer) Cleanup(retention time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-retention)

	// Cleanup trades
	for market, trades := range m.trades {
		var kept []*TradeHistory
		for _, trade := range trades {
			if trade.Timestamp.After(cutoff) {
				kept = append(kept, trade)
			}
		}
		m.trades[market] = kept
	}

	// Cleanup price history
	for market, prices := range m.priceHistory {
		var kept []*PricePoint
		for _, price := range prices {
			if price.Timestamp.After(cutoff) {
				kept = append(kept, price)
			}
		}
		m.priceHistory[market] = kept
	}

	// Cleanup candles (keep 1d and 1w longer)
	for market, intervals := range m.candles {
		for interval, candles := range intervals {
			var candleCutoff time.Time
			switch interval {
			case Candle1m, Candle5m:
				candleCutoff = cutoff
			case Candle15m, Candle30m, Candle1h:
				candleCutoff = time.Now().Add(-7 * 24 * time.Hour)
			case Candle4h:
				candleCutoff = time.Now().Add(-30 * 24 * time.Hour)
			case Candle1d, Candle1w:
				candleCutoff = time.Now().Add(-365 * 24 * time.Hour)
			default:
				candleCutoff = cutoff
			}

			var kept []*OHLCV
			for _, candle := range candles {
				if candle.OpenTime.After(candleCutoff) {
					kept = append(kept, candle)
				}
			}
			m.candles[market][interval] = kept
		}
	}

	// Cleanup liquidity history
	liquidityCutoff := time.Now().Add(-30 * 24 * time.Hour)
	for market, snapshots := range m.liquidityHistory {
		var kept []*LiquiditySnapshot
		for _, snapshot := range snapshots {
			if snapshot.Timestamp.After(liquidityCutoff) {
				kept = append(kept, snapshot)
			}
		}
		m.liquidityHistory[market] = kept
	}
}

// Stats returns indexer statistics
func (m *MarketHistoryIndexer) Stats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]interface{})

	// Count markets
	stats["markets"] = len(m.tickers)

	// Count total trades
	var totalTrades int
	for _, trades := range m.trades {
		totalTrades += len(trades)
	}
	stats["trades"] = totalTrades

	// Count candles
	var totalCandles int
	for _, intervals := range m.candles {
		for _, candles := range intervals {
			totalCandles += len(candles)
		}
	}
	stats["candles"] = totalCandles

	// Count liquidity snapshots
	var totalSnapshots int
	for _, snapshots := range m.liquidityHistory {
		totalSnapshots += len(snapshots)
	}
	stats["liquiditySnapshots"] = totalSnapshots

	return stats
}
