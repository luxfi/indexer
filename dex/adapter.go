// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package dex provides the DEX L2 subnet adapter for the indexer.
// Handles orderbook, trades, liquidity pools, and swap operations.
// The DEX is a standalone L2 subnet (see ~/work/lux/dex), not a native chain.
package dex

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/luxfi/indexer/dag"
	"github.com/luxfi/indexer/storage"
)

const (
	// DefaultPort for DEX L2 indexer API
	DefaultPort = 4800
	// DefaultDatabase name
	DefaultDatabase = "explorer_dchain"
)

// OrderSide represents buy or sell
type OrderSide string

const (
	OrderSideBuy  OrderSide = "buy"
	OrderSideSell OrderSide = "sell"
)

// OrderType represents the order type
type OrderType string

const (
	OrderTypeLimit      OrderType = "limit"
	OrderTypeMarket     OrderType = "market"
	OrderTypeStopLoss   OrderType = "stop_loss"
	OrderTypeTakeProfit OrderType = "take_profit"
	OrderTypeStopLimit  OrderType = "stop_limit"
)

// OrderStatus represents order lifecycle state
type OrderStatus string

const (
	OrderStatusOpen      OrderStatus = "open"
	OrderStatusFilled    OrderStatus = "filled"
	OrderStatusCancelled OrderStatus = "cancelled"
	OrderStatusPartial   OrderStatus = "partial"
	OrderStatusExpired   OrderStatus = "expired"
)

// Order represents a DEX order
type Order struct {
	ID          string      `json:"id"`
	Owner       string      `json:"owner"`
	Symbol      string      `json:"symbol"`
	Side        OrderSide   `json:"side"`
	Type        OrderType   `json:"type"`
	Price       uint64      `json:"price"`
	Quantity    uint64      `json:"quantity"`
	FilledQty   uint64      `json:"filledQty"`
	TimeInForce string      `json:"timeInForce"`
	PostOnly    bool        `json:"postOnly"`
	ReduceOnly  bool        `json:"reduceOnly"`
	Status      OrderStatus `json:"status"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
}

// Trade represents a completed trade
type Trade struct {
	ID        string    `json:"id"`
	Symbol    string    `json:"symbol"`
	MakerID   string    `json:"makerId"`
	TakerID   string    `json:"takerId"`
	Maker     string    `json:"maker"`
	Taker     string    `json:"taker"`
	Side      OrderSide `json:"side"`
	Price     uint64    `json:"price"`
	Quantity  uint64    `json:"quantity"`
	Volume    uint64    `json:"volume"`
	Fee       uint64    `json:"fee"`
	Timestamp time.Time `json:"timestamp"`
}

// LiquidityPool represents an AMM pool
type LiquidityPool struct {
	ID        string    `json:"id"`
	Token0    string    `json:"token0"`
	Token1    string    `json:"token1"`
	Reserve0  string    `json:"reserve0"`
	Reserve1  string    `json:"reserve1"`
	LPSupply  string    `json:"lpSupply"`
	Fee       uint32    `json:"fee"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Swap represents a pool swap
type Swap struct {
	ID        string    `json:"id"`
	PoolID    string    `json:"poolId"`
	Sender    string    `json:"sender"`
	TokenIn   string    `json:"tokenIn"`
	TokenOut  string    `json:"tokenOut"`
	AmountIn  string    `json:"amountIn"`
	AmountOut string    `json:"amountOut"`
	Fee       string    `json:"fee"`
	Timestamp time.Time `json:"timestamp"`
}

// LiquidityEvent represents add/remove liquidity
type LiquidityEvent struct {
	ID        string    `json:"id"`
	PoolID    string    `json:"poolId"`
	Provider  string    `json:"provider"`
	Type      string    `json:"type"` // "add" or "remove"
	Amount0   string    `json:"amount0"`
	Amount1   string    `json:"amount1"`
	LPTokens  string    `json:"lpTokens"`
	Timestamp time.Time `json:"timestamp"`
}

// vertexData is the parsed vertex content
type vertexData struct {
	Orders          []Order          `json:"orders,omitempty"`
	Trades          []Trade          `json:"trades,omitempty"`
	Pools           []LiquidityPool  `json:"pools,omitempty"`
	Swaps           []Swap           `json:"swaps,omitempty"`
	LiquidityEvents []LiquidityEvent `json:"liquidityEvents,omitempty"`
}

// Adapter implements the DAG adapter interface for DEX L2
type Adapter struct {
	rpcEndpoint string
	httpClient  *http.Client
}

// New creates a new DEX L2 adapter
func New(rpcEndpoint string) *Adapter {
	return &Adapter{
		rpcEndpoint: rpcEndpoint,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ParseVertex parses DEX L2 vertex data
func (a *Adapter) ParseVertex(data json.RawMessage) (*dag.Vertex, error) {
	var raw struct {
		ID        string          `json:"id"`
		ParentIDs []string        `json:"parentIds"`
		Height    uint64          `json:"height"`
		Timestamp int64           `json:"timestamp"`
		Status    string          `json:"status"`
		Data      json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal vertex: %w", err)
	}

	vertex := &dag.Vertex{
		ID:        raw.ID,
		ParentIDs: raw.ParentIDs,
		Height:    raw.Height,
		Timestamp: time.Unix(0, raw.Timestamp),
		Status:    dag.Status(raw.Status),
		Data:      raw.Data,
		Type:      a.inferVertexType(raw.Data),
	}

	return vertex, nil
}

// inferVertexType determines the vertex type from its data
func (a *Adapter) inferVertexType(data json.RawMessage) string {
	var v vertexData
	if err := json.Unmarshal(data, &v); err != nil {
		return "unknown"
	}

	if len(v.Orders) > 0 {
		return "orders"
	}
	if len(v.Trades) > 0 {
		return "trades"
	}
	if len(v.Pools) > 0 {
		return "pools"
	}
	if len(v.Swaps) > 0 {
		return "swaps"
	}
	if len(v.LiquidityEvents) > 0 {
		return "liquidity"
	}
	return "generic"
}

// GetRecentVertices fetches recent vertices from DEX L2 RPC
func (a *Adapter) GetRecentVertices(ctx context.Context, limit int) ([]json.RawMessage, error) {
	return a.call(ctx, "dvm.getRecentVertices", map[string]interface{}{
		"limit": limit,
	})
}

// GetVertexByID fetches a specific vertex by ID
func (a *Adapter) GetVertexByID(ctx context.Context, id string) (json.RawMessage, error) {
	results, err := a.call(ctx, "dvm.getVertex", map[string]interface{}{
		"id": id,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("vertex not found: %s", id)
	}
	return results[0], nil
}

// call makes a JSON-RPC call to the DEX L2 node
func (a *Adapter) call(ctx context.Context, method string, params interface{}) ([]json.RawMessage, error) {
	reqBody, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.rpcEndpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", result.Error.Code, result.Error.Message)
	}

	// Try to decode as array
	var vertices []json.RawMessage
	if err := json.Unmarshal(result.Result, &vertices); err != nil {
		// Single result
		return []json.RawMessage{result.Result}, nil
	}
	return vertices, nil
}

// InitSchema creates DEX L2 specific database tables using unified storage
func (a *Adapter) InitSchema(ctx context.Context, store storage.Store) error {
	schema := storage.Schema{
		Name: "dex",
		Tables: []storage.Table{
			{
				Name: "dex_orders",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "owner", Type: storage.TypeText, Nullable: false},
					{Name: "symbol", Type: storage.TypeText, Nullable: false},
					{Name: "side", Type: storage.TypeText, Nullable: false},
					{Name: "type", Type: storage.TypeText, Nullable: false},
					{Name: "price", Type: storage.TypeBigInt, Nullable: false},
					{Name: "quantity", Type: storage.TypeBigInt, Nullable: false},
					{Name: "filled_qty", Type: storage.TypeBigInt, Default: "0"},
					{Name: "time_in_force", Type: storage.TypeText},
					{Name: "post_only", Type: storage.TypeBool, Default: "false"},
					{Name: "reduce_only", Type: storage.TypeBool, Default: "false"},
					{Name: "status", Type: storage.TypeText, Nullable: false},
					{Name: "created_at", Type: storage.TypeTimestamp, Nullable: false},
					{Name: "updated_at", Type: storage.TypeTimestamp, Nullable: false},
				},
			},
			{
				Name: "dex_trades",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "symbol", Type: storage.TypeText, Nullable: false},
					{Name: "maker_order_id", Type: storage.TypeText, Nullable: false},
					{Name: "taker_order_id", Type: storage.TypeText, Nullable: false},
					{Name: "maker", Type: storage.TypeText, Nullable: false},
					{Name: "taker", Type: storage.TypeText, Nullable: false},
					{Name: "side", Type: storage.TypeText, Nullable: false},
					{Name: "price", Type: storage.TypeBigInt, Nullable: false},
					{Name: "quantity", Type: storage.TypeBigInt, Nullable: false},
					{Name: "volume", Type: storage.TypeBigInt, Nullable: false},
					{Name: "fee", Type: storage.TypeBigInt, Default: "0"},
					{Name: "timestamp", Type: storage.TypeTimestamp, Nullable: false},
				},
			},
			{
				Name: "dex_pools",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "token0", Type: storage.TypeText, Nullable: false},
					{Name: "token1", Type: storage.TypeText, Nullable: false},
					{Name: "reserve0", Type: storage.TypeText, Nullable: false},
					{Name: "reserve1", Type: storage.TypeText, Nullable: false},
					{Name: "lp_supply", Type: storage.TypeText, Nullable: false},
					{Name: "fee", Type: storage.TypeInt, Default: "30"},
					{Name: "created_at", Type: storage.TypeTimestamp, Nullable: false},
					{Name: "updated_at", Type: storage.TypeTimestamp, Nullable: false},
				},
			},
			{
				Name: "dex_swaps",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "pool_id", Type: storage.TypeText, Nullable: false},
					{Name: "sender", Type: storage.TypeText, Nullable: false},
					{Name: "token_in", Type: storage.TypeText, Nullable: false},
					{Name: "token_out", Type: storage.TypeText, Nullable: false},
					{Name: "amount_in", Type: storage.TypeText, Nullable: false},
					{Name: "amount_out", Type: storage.TypeText, Nullable: false},
					{Name: "fee", Type: storage.TypeText, Nullable: false},
					{Name: "timestamp", Type: storage.TypeTimestamp, Nullable: false},
				},
			},
			{
				Name: "dex_liquidity_events",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "pool_id", Type: storage.TypeText, Nullable: false},
					{Name: "provider", Type: storage.TypeText, Nullable: false},
					{Name: "event_type", Type: storage.TypeText, Nullable: false},
					{Name: "amount0", Type: storage.TypeText, Nullable: false},
					{Name: "amount1", Type: storage.TypeText, Nullable: false},
					{Name: "lp_tokens", Type: storage.TypeText, Nullable: false},
					{Name: "timestamp", Type: storage.TypeTimestamp, Nullable: false},
				},
			},
			{
				Name: "dex_market_stats",
				Columns: []storage.Column{
					{Name: "symbol", Type: storage.TypeText, Primary: true},
					{Name: "last_price", Type: storage.TypeBigInt, Default: "0"},
					{Name: "high_24h", Type: storage.TypeBigInt, Default: "0"},
					{Name: "low_24h", Type: storage.TypeBigInt, Default: "0"},
					{Name: "volume_24h", Type: storage.TypeBigInt, Default: "0"},
					{Name: "trade_count_24h", Type: storage.TypeBigInt, Default: "0"},
					{Name: "open_interest", Type: storage.TypeBigInt, Default: "0"},
					{Name: "updated_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
			{
				Name: "dex_extended_stats",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeInt, Primary: true, Default: "1"},
					{Name: "total_orders", Type: storage.TypeBigInt, Default: "0"},
					{Name: "open_orders", Type: storage.TypeBigInt, Default: "0"},
					{Name: "total_trades", Type: storage.TypeBigInt, Default: "0"},
					{Name: "total_volume", Type: storage.TypeBigInt, Default: "0"},
					{Name: "total_pools", Type: storage.TypeBigInt, Default: "0"},
					{Name: "total_swaps", Type: storage.TypeBigInt, Default: "0"},
					{Name: "total_liquidity_events", Type: storage.TypeBigInt, Default: "0"},
					{Name: "unique_traders", Type: storage.TypeBigInt, Default: "0"},
					{Name: "updated_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
		},
		Indexes: []storage.Index{
			{Name: "idx_dex_orders_owner", Table: "dex_orders", Columns: []string{"owner"}},
			{Name: "idx_dex_orders_symbol", Table: "dex_orders", Columns: []string{"symbol"}},
			{Name: "idx_dex_orders_status", Table: "dex_orders", Columns: []string{"status"}},
			{Name: "idx_dex_orders_created", Table: "dex_orders", Columns: []string{"created_at"}},
			{Name: "idx_dex_trades_symbol", Table: "dex_trades", Columns: []string{"symbol"}},
			{Name: "idx_dex_trades_maker", Table: "dex_trades", Columns: []string{"maker"}},
			{Name: "idx_dex_trades_taker", Table: "dex_trades", Columns: []string{"taker"}},
			{Name: "idx_dex_trades_timestamp", Table: "dex_trades", Columns: []string{"timestamp"}},
			{Name: "idx_dex_pools_token0", Table: "dex_pools", Columns: []string{"token0"}},
			{Name: "idx_dex_pools_token1", Table: "dex_pools", Columns: []string{"token1"}},
			{Name: "idx_dex_swaps_pool", Table: "dex_swaps", Columns: []string{"pool_id"}},
			{Name: "idx_dex_swaps_sender", Table: "dex_swaps", Columns: []string{"sender"}},
			{Name: "idx_dex_swaps_timestamp", Table: "dex_swaps", Columns: []string{"timestamp"}},
			{Name: "idx_dex_liq_events_pool", Table: "dex_liquidity_events", Columns: []string{"pool_id"}},
			{Name: "idx_dex_liq_events_provider", Table: "dex_liquidity_events", Columns: []string{"provider"}},
		},
	}

	if err := store.InitSchema(ctx, schema); err != nil {
		return err
	}

	// Initialize stats row
	return store.Exec(ctx, "INSERT OR IGNORE INTO dex_extended_stats (id) VALUES (1)")
}

// GetStats returns DEX L2 specific statistics using unified storage
func (a *Adapter) GetStats(ctx context.Context, store storage.Store) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Get extended stats
	rows, err := store.Query(ctx, `
		SELECT total_orders, open_orders, total_trades, total_volume,
		       total_pools, total_swaps, total_liquidity_events, unique_traders, updated_at
		FROM dex_extended_stats WHERE id = 1
	`)
	if err != nil {
		return nil, err
	}

	if len(rows) > 0 {
		row := rows[0]
		stats["total_orders"] = row["total_orders"]
		stats["open_orders"] = row["open_orders"]
		stats["total_trades"] = row["total_trades"]
		stats["total_volume"] = row["total_volume"]
		stats["total_pools"] = row["total_pools"]
		stats["total_swaps"] = row["total_swaps"]
		stats["total_liquidity_events"] = row["total_liquidity_events"]
		stats["unique_traders"] = row["unique_traders"]
		stats["updated_at"] = row["updated_at"]
	}

	// Get top markets by volume
	marketRows, err := store.Query(ctx, `
		SELECT symbol, last_price, volume_24h, trade_count_24h
		FROM dex_market_stats
		ORDER BY volume_24h DESC
		LIMIT 10
	`)
	if err != nil {
		return nil, err
	}

	var markets []map[string]interface{}
	for _, row := range marketRows {
		markets = append(markets, map[string]interface{}{
			"symbol":      row["symbol"],
			"last_price":  row["last_price"],
			"volume_24h":  row["volume_24h"],
			"trade_count": row["trade_count_24h"],
		})
	}
	stats["top_markets"] = markets

	return stats, nil
}

// StoreOrder stores an order
func (a *Adapter) StoreOrder(ctx context.Context, db *sql.DB, o Order) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO dex_orders
		(id, owner, symbol, side, type, price, quantity, filled_qty, time_in_force,
		 post_only, reduce_only, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (id) DO UPDATE SET
			filled_qty = EXCLUDED.filled_qty,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at
	`, o.ID, o.Owner, o.Symbol, o.Side, o.Type, o.Price, o.Quantity, o.FilledQty,
		o.TimeInForce, o.PostOnly, o.ReduceOnly, o.Status, o.CreatedAt, o.UpdatedAt)
	return err
}

// StoreTrade stores a trade
func (a *Adapter) StoreTrade(ctx context.Context, db *sql.DB, t Trade) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO dex_trades
		(id, symbol, maker_order_id, taker_order_id, maker, taker, side, price, quantity, volume, fee, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO NOTHING
	`, t.ID, t.Symbol, t.MakerID, t.TakerID, t.Maker, t.Taker, t.Side, t.Price, t.Quantity, t.Volume, t.Fee, t.Timestamp)

	if err != nil {
		return err
	}

	// Update market stats
	_, err = db.ExecContext(ctx, `
		INSERT INTO dex_market_stats (symbol, last_price, volume_24h, trade_count_24h, updated_at)
		VALUES ($1, $2, $3, 1, NOW())
		ON CONFLICT (symbol) DO UPDATE SET
			last_price = $2,
			volume_24h = dex_market_stats.volume_24h + $3,
			trade_count_24h = dex_market_stats.trade_count_24h + 1,
			updated_at = NOW()
	`, t.Symbol, t.Price, t.Volume)

	return err
}

// StorePool stores a liquidity pool
func (a *Adapter) StorePool(ctx context.Context, db *sql.DB, p LiquidityPool) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO dex_pools (id, token0, token1, reserve0, reserve1, lp_supply, fee, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			reserve0 = EXCLUDED.reserve0,
			reserve1 = EXCLUDED.reserve1,
			lp_supply = EXCLUDED.lp_supply,
			updated_at = EXCLUDED.updated_at
	`, p.ID, p.Token0, p.Token1, p.Reserve0, p.Reserve1, p.LPSupply, p.Fee, p.CreatedAt, p.UpdatedAt)
	return err
}

// StoreSwap stores a swap
func (a *Adapter) StoreSwap(ctx context.Context, db *sql.DB, s Swap) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO dex_swaps (id, pool_id, sender, token_in, token_out, amount_in, amount_out, fee, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO NOTHING
	`, s.ID, s.PoolID, s.Sender, s.TokenIn, s.TokenOut, s.AmountIn, s.AmountOut, s.Fee, s.Timestamp)
	return err
}

// StoreLiquidityEvent stores a liquidity event
func (a *Adapter) StoreLiquidityEvent(ctx context.Context, db *sql.DB, e LiquidityEvent) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO dex_liquidity_events (id, pool_id, provider, event_type, amount0, amount1, lp_tokens, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO NOTHING
	`, e.ID, e.PoolID, e.Provider, e.Type, e.Amount0, e.Amount1, e.LPTokens, e.Timestamp)
	return err
}

// StoreVertex stores vertex data
func (a *Adapter) StoreVertex(ctx context.Context, db *sql.DB, v *dag.Vertex) error {
	var data vertexData
	if err := json.Unmarshal(v.Data, &data); err != nil {
		return fmt.Errorf("unmarshal vertex data: %w", err)
	}

	// Store orders
	for _, o := range data.Orders {
		if err := a.StoreOrder(ctx, db, o); err != nil {
			return fmt.Errorf("store order: %w", err)
		}
	}

	// Store trades
	for _, t := range data.Trades {
		if err := a.StoreTrade(ctx, db, t); err != nil {
			return fmt.Errorf("store trade: %w", err)
		}
	}

	// Store pools
	for _, p := range data.Pools {
		if err := a.StorePool(ctx, db, p); err != nil {
			return fmt.Errorf("store pool: %w", err)
		}
	}

	// Store swaps
	for _, s := range data.Swaps {
		if err := a.StoreSwap(ctx, db, s); err != nil {
			return fmt.Errorf("store swap: %w", err)
		}
	}

	// Store liquidity events
	for _, e := range data.LiquidityEvents {
		if err := a.StoreLiquidityEvent(ctx, db, e); err != nil {
			return fmt.Errorf("store liquidity event: %w", err)
		}
	}

	return nil
}

// UpdateExtendedStats updates DEX L2 specific statistics
func (a *Adapter) UpdateExtendedStats(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		UPDATE dex_extended_stats SET
			total_orders = (SELECT COUNT(*) FROM dex_orders),
			open_orders = (SELECT COUNT(*) FROM dex_orders WHERE status = 'open'),
			total_trades = (SELECT COUNT(*) FROM dex_trades),
			total_volume = COALESCE((SELECT SUM(volume) FROM dex_trades), 0),
			total_pools = (SELECT COUNT(*) FROM dex_pools),
			total_swaps = (SELECT COUNT(*) FROM dex_swaps),
			total_liquidity_events = (SELECT COUNT(*) FROM dex_liquidity_events),
			unique_traders = (SELECT COUNT(DISTINCT owner) FROM dex_orders),
			updated_at = NOW()
		WHERE id = 1
	`)
	return err
}
