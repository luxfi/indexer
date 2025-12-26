// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package dex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/luxfi/indexer/dag"
)

// TestNew tests the adapter constructor
func TestNew(t *testing.T) {
	endpoint := "http://localhost:9650/ext/bc/DEX/rpc"
	adapter := New(endpoint)

	if adapter == nil {
		t.Fatal("New returned nil adapter")
	}

	if adapter.rpcEndpoint != endpoint {
		t.Errorf("rpcEndpoint = %q, want %q", adapter.rpcEndpoint, endpoint)
	}

	if adapter.httpClient == nil {
		t.Error("httpClient should not be nil")
	}

	if adapter.httpClient.Timeout != 30*time.Second {
		t.Errorf("httpClient.Timeout = %v, want 30s", adapter.httpClient.Timeout)
	}
}

// TestConstants tests the package constants
func TestConstants(t *testing.T) {
	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"DefaultPort", DefaultPort, 4800},
		{"DefaultDatabase", DefaultDatabase, "explorer_dchain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestOrderSideConstants tests order side constants
func TestOrderSideConstants(t *testing.T) {
	if OrderSideBuy != "buy" {
		t.Errorf("OrderSideBuy = %q, want buy", OrderSideBuy)
	}
	if OrderSideSell != "sell" {
		t.Errorf("OrderSideSell = %q, want sell", OrderSideSell)
	}
}

// TestOrderTypeConstants tests order type constants
func TestOrderTypeConstants(t *testing.T) {
	tests := []struct {
		name string
		got  OrderType
		want string
	}{
		{"OrderTypeLimit", OrderTypeLimit, "limit"},
		{"OrderTypeMarket", OrderTypeMarket, "market"},
		{"OrderTypeStopLoss", OrderTypeStopLoss, "stop_loss"},
		{"OrderTypeTakeProfit", OrderTypeTakeProfit, "take_profit"},
		{"OrderTypeStopLimit", OrderTypeStopLimit, "stop_limit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.got) != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestOrderStatusConstants tests order status constants
func TestOrderStatusConstants(t *testing.T) {
	tests := []struct {
		name string
		got  OrderStatus
		want string
	}{
		{"OrderStatusOpen", OrderStatusOpen, "open"},
		{"OrderStatusFilled", OrderStatusFilled, "filled"},
		{"OrderStatusCancelled", OrderStatusCancelled, "cancelled"},
		{"OrderStatusPartial", OrderStatusPartial, "partial"},
		{"OrderStatusExpired", OrderStatusExpired, "expired"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.got) != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestAdapterImplementsInterface verifies Adapter implements dag.Adapter
func TestAdapterImplementsInterface(t *testing.T) {
	var _ dag.Adapter = (*Adapter)(nil)
}

// TestParseVertex tests parsing DEX vertex data
func TestParseVertex(t *testing.T) {
	adapter := New("http://localhost:9650")

	tests := []struct {
		name     string
		input    json.RawMessage
		wantID   string
		wantType string
		wantErr  bool
	}{
		{
			name: "valid orders vertex",
			input: json.RawMessage(`{
				"id": "vertex123",
				"parentIds": ["parent1", "parent2"],
				"height": 100,
				"timestamp": 1700000000000000000,
				"status": "accepted",
				"data": {
					"orders": [{"id": "order1", "symbol": "LUX/USD"}]
				}
			}`),
			wantID:   "vertex123",
			wantType: "orders",
			wantErr:  false,
		},
		{
			name: "valid trades vertex",
			input: json.RawMessage(`{
				"id": "vertex456",
				"parentIds": [],
				"height": 50,
				"timestamp": 1700000000000000000,
				"status": "pending",
				"data": {
					"trades": [{"id": "trade1", "symbol": "ETH/USD"}]
				}
			}`),
			wantID:   "vertex456",
			wantType: "trades",
			wantErr:  false,
		},
		{
			name: "valid pools vertex",
			input: json.RawMessage(`{
				"id": "vertex789",
				"parentIds": ["p1"],
				"height": 25,
				"timestamp": 1700000000000000000,
				"status": "accepted",
				"data": {
					"pools": [{"id": "pool1", "token0": "LUX", "token1": "USD"}]
				}
			}`),
			wantID:   "vertex789",
			wantType: "pools",
			wantErr:  false,
		},
		{
			name: "valid swaps vertex",
			input: json.RawMessage(`{
				"id": "vertexabc",
				"parentIds": [],
				"height": 10,
				"timestamp": 1700000000000000000,
				"status": "accepted",
				"data": {
					"swaps": [{"id": "swap1", "poolId": "pool1"}]
				}
			}`),
			wantID:   "vertexabc",
			wantType: "swaps",
			wantErr:  false,
		},
		{
			name: "valid liquidity vertex",
			input: json.RawMessage(`{
				"id": "vertexdef",
				"parentIds": [],
				"height": 5,
				"timestamp": 1700000000000000000,
				"status": "accepted",
				"data": {
					"liquidityEvents": [{"id": "liq1", "type": "add"}]
				}
			}`),
			wantID:   "vertexdef",
			wantType: "liquidity",
			wantErr:  false,
		},
		{
			name: "empty data vertex",
			input: json.RawMessage(`{
				"id": "empty",
				"parentIds": [],
				"height": 1,
				"timestamp": 1700000000000000000,
				"status": "accepted",
				"data": {}
			}`),
			wantID:   "empty",
			wantType: "generic",
			wantErr:  false,
		},
		{
			name:    "invalid JSON",
			input:   json.RawMessage(`{invalid`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vertex, err := adapter.ParseVertex(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if vertex.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", vertex.ID, tt.wantID)
			}
			if vertex.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", vertex.Type, tt.wantType)
			}
		})
	}
}

// TestParseVertexParents tests parent parsing
func TestParseVertexParents(t *testing.T) {
	adapter := New("http://localhost:9650")

	input := json.RawMessage(`{
		"id": "child",
		"parentIds": ["parent1", "parent2", "parent3"],
		"height": 100,
		"timestamp": 1700000000000000000,
		"status": "accepted",
		"data": {}
	}`)

	vertex, err := adapter.ParseVertex(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(vertex.ParentIDs) != 3 {
		t.Errorf("ParentIDs length = %d, want 3", len(vertex.ParentIDs))
	}

	expected := []string{"parent1", "parent2", "parent3"}
	for i, p := range expected {
		if vertex.ParentIDs[i] != p {
			t.Errorf("ParentIDs[%d] = %q, want %q", i, vertex.ParentIDs[i], p)
		}
	}
}

// TestGetRecentVertices tests fetching recent vertices
func TestGetRecentVertices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		method := req["method"].(string)
		if method != "dvm.getRecentVertices" {
			t.Errorf("expected method dvm.getRecentVertices, got %s", method)
		}

		params := req["params"].(map[string]interface{})
		limit := int(params["limit"].(float64))
		if limit != 5 {
			t.Errorf("expected limit 5, got %d", limit)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": []map[string]interface{}{
				{"id": "v1", "height": 100},
				{"id": "v2", "height": 99},
				{"id": "v3", "height": 98},
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	vertices, err := adapter.GetRecentVertices(ctx, 5)
	if err != nil {
		t.Fatalf("GetRecentVertices failed: %v", err)
	}

	if len(vertices) != 3 {
		t.Errorf("got %d vertices, want 3", len(vertices))
	}
}

// TestGetVertexByID tests fetching a vertex by ID
func TestGetVertexByID(t *testing.T) {
	vertexID := "vertex123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		method := req["method"].(string)
		if method != "dvm.getVertex" {
			t.Errorf("expected method dvm.getVertex, got %s", method)
		}

		params := req["params"].(map[string]interface{})
		id := params["id"].(string)
		if id != vertexID {
			t.Errorf("expected id %s, got %s", vertexID, id)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"id":     vertexID,
				"height": 100,
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	result, err := adapter.GetVertexByID(ctx, vertexID)
	if err != nil {
		t.Fatalf("GetVertexByID failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if parsed["id"] != vertexID {
		t.Errorf("id = %v, want %s", parsed["id"], vertexID)
	}
}

// TestGetVertexByIDNotFound tests vertex not found case
func TestGetVertexByIDNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  []interface{}{}, // Empty array
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	_, err := adapter.GetVertexByID(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent vertex")
	}
}

// TestOrderStruct tests Order JSON serialization
func TestOrderStruct(t *testing.T) {
	order := Order{
		ID:          "order123",
		Owner:       "0xowner",
		Symbol:      "LUX/USD",
		Side:        OrderSideBuy,
		Type:        OrderTypeLimit,
		Price:       100000,
		Quantity:    1000,
		FilledQty:   500,
		TimeInForce: "GTC",
		PostOnly:    true,
		ReduceOnly:  false,
		Status:      OrderStatusPartial,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	data, err := json.Marshal(order)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Order
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != order.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, order.ID)
	}
	if decoded.Symbol != order.Symbol {
		t.Errorf("Symbol = %q, want %q", decoded.Symbol, order.Symbol)
	}
	if decoded.Side != order.Side {
		t.Errorf("Side = %q, want %q", decoded.Side, order.Side)
	}
	if decoded.Type != order.Type {
		t.Errorf("Type = %q, want %q", decoded.Type, order.Type)
	}
	if decoded.Price != order.Price {
		t.Errorf("Price = %d, want %d", decoded.Price, order.Price)
	}
	if decoded.PostOnly != order.PostOnly {
		t.Errorf("PostOnly = %v, want %v", decoded.PostOnly, order.PostOnly)
	}
	if decoded.Status != order.Status {
		t.Errorf("Status = %q, want %q", decoded.Status, order.Status)
	}
}

// TestTradeStruct tests Trade JSON serialization
func TestTradeStruct(t *testing.T) {
	trade := Trade{
		ID:        "trade123",
		Symbol:    "ETH/USD",
		MakerID:   "order1",
		TakerID:   "order2",
		Maker:     "0xmaker",
		Taker:     "0xtaker",
		Side:      OrderSideSell,
		Price:     200000,
		Quantity:  500,
		Volume:    100000000,
		Fee:       100,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(trade)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Trade
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != trade.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, trade.ID)
	}
	if decoded.Symbol != trade.Symbol {
		t.Errorf("Symbol = %q, want %q", decoded.Symbol, trade.Symbol)
	}
	if decoded.Maker != trade.Maker {
		t.Errorf("Maker = %q, want %q", decoded.Maker, trade.Maker)
	}
	if decoded.Taker != trade.Taker {
		t.Errorf("Taker = %q, want %q", decoded.Taker, trade.Taker)
	}
	if decoded.Volume != trade.Volume {
		t.Errorf("Volume = %d, want %d", decoded.Volume, trade.Volume)
	}
}

// TestLiquidityPoolStruct tests LiquidityPool JSON serialization
func TestLiquidityPoolStruct(t *testing.T) {
	pool := LiquidityPool{
		ID:        "pool123",
		Token0:    "LUX",
		Token1:    "USD",
		Reserve0:  "1000000000000000000000",
		Reserve1:  "2000000000000",
		LPSupply:  "500000000000000000",
		Fee:       30,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	data, err := json.Marshal(pool)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded LiquidityPool
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != pool.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, pool.ID)
	}
	if decoded.Token0 != pool.Token0 {
		t.Errorf("Token0 = %q, want %q", decoded.Token0, pool.Token0)
	}
	if decoded.Token1 != pool.Token1 {
		t.Errorf("Token1 = %q, want %q", decoded.Token1, pool.Token1)
	}
	if decoded.Reserve0 != pool.Reserve0 {
		t.Errorf("Reserve0 = %q, want %q", decoded.Reserve0, pool.Reserve0)
	}
	if decoded.Fee != pool.Fee {
		t.Errorf("Fee = %d, want %d", decoded.Fee, pool.Fee)
	}
}

// TestSwapStruct tests Swap JSON serialization
func TestSwapStruct(t *testing.T) {
	swap := Swap{
		ID:        "swap123",
		PoolID:    "pool1",
		Sender:    "0xsender",
		TokenIn:   "LUX",
		TokenOut:  "USD",
		AmountIn:  "1000000000000000000",
		AmountOut: "1950000000",
		Fee:       "50000000",
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(swap)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Swap
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != swap.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, swap.ID)
	}
	if decoded.PoolID != swap.PoolID {
		t.Errorf("PoolID = %q, want %q", decoded.PoolID, swap.PoolID)
	}
	if decoded.TokenIn != swap.TokenIn {
		t.Errorf("TokenIn = %q, want %q", decoded.TokenIn, swap.TokenIn)
	}
	if decoded.AmountIn != swap.AmountIn {
		t.Errorf("AmountIn = %q, want %q", decoded.AmountIn, swap.AmountIn)
	}
}

// TestLiquidityEventStruct tests LiquidityEvent JSON serialization
func TestLiquidityEventStruct(t *testing.T) {
	event := LiquidityEvent{
		ID:        "liq123",
		PoolID:    "pool1",
		Provider:  "0xprovider",
		Type:      "add",
		Amount0:   "1000000000000000000",
		Amount1:   "2000000000",
		LPTokens:  "1414213562373095",
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded LiquidityEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != event.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, event.ID)
	}
	if decoded.PoolID != event.PoolID {
		t.Errorf("PoolID = %q, want %q", decoded.PoolID, event.PoolID)
	}
	if decoded.Type != event.Type {
		t.Errorf("Type = %q, want %q", decoded.Type, event.Type)
	}
	if decoded.Provider != event.Provider {
		t.Errorf("Provider = %q, want %q", decoded.Provider, event.Provider)
	}
}

// TestInferVertexType tests vertex type inference
func TestInferVertexType(t *testing.T) {
	adapter := New("http://localhost:9650")

	tests := []struct {
		name     string
		data     json.RawMessage
		wantType string
	}{
		{
			name:     "orders type",
			data:     json.RawMessage(`{"orders": [{"id": "o1"}]}`),
			wantType: "orders",
		},
		{
			name:     "trades type",
			data:     json.RawMessage(`{"trades": [{"id": "t1"}]}`),
			wantType: "trades",
		},
		{
			name:     "pools type",
			data:     json.RawMessage(`{"pools": [{"id": "p1"}]}`),
			wantType: "pools",
		},
		{
			name:     "swaps type",
			data:     json.RawMessage(`{"swaps": [{"id": "s1"}]}`),
			wantType: "swaps",
		},
		{
			name:     "liquidity type",
			data:     json.RawMessage(`{"liquidityEvents": [{"id": "l1"}]}`),
			wantType: "liquidity",
		},
		{
			name:     "generic type",
			data:     json.RawMessage(`{}`),
			wantType: "generic",
		},
		{
			name:     "invalid JSON",
			data:     json.RawMessage(`{invalid`),
			wantType: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType := adapter.inferVertexType(tt.data)
			if gotType != tt.wantType {
				t.Errorf("inferVertexType() = %q, want %q", gotType, tt.wantType)
			}
		})
	}
}

// TestNetworkError tests error handling for network failures
func TestNetworkError(t *testing.T) {
	adapter := New("http://localhost:99999")
	ctx := context.Background()

	_, err := adapter.GetRecentVertices(ctx, 5)
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// TestContextCancellation tests context cancellation handling
func TestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := adapter.GetRecentVertices(ctx, 5)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

// TestRPCError tests RPC error response handling
func TestRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]interface{}{
				"code":    -32000,
				"message": "internal error",
			},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	_, err := adapter.GetVertexByID(ctx, "test")
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestVertexDataParsing tests parsing complete vertex data
func TestVertexDataParsing(t *testing.T) {
	adapter := New("http://localhost:9650")

	// Create a vertex with all data types
	input := json.RawMessage(`{
		"id": "complete_vertex",
		"parentIds": ["p1"],
		"height": 500,
		"timestamp": 1700000000000000000,
		"status": "accepted",
		"data": {
			"orders": [
				{
					"id": "o1",
					"owner": "0x123",
					"symbol": "LUX/USD",
					"side": "buy",
					"type": "limit",
					"price": 100000,
					"quantity": 1000,
					"status": "open"
				}
			],
			"trades": [
				{
					"id": "t1",
					"symbol": "LUX/USD",
					"maker": "0xmaker",
					"taker": "0xtaker",
					"price": 100000,
					"quantity": 500
				}
			]
		}
	}`)

	vertex, err := adapter.ParseVertex(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if vertex.ID != "complete_vertex" {
		t.Errorf("ID = %q, want complete_vertex", vertex.ID)
	}

	// Parse the data
	var data vertexData
	if err := json.Unmarshal(vertex.Data, &data); err != nil {
		t.Fatalf("failed to parse vertex data: %v", err)
	}

	if len(data.Orders) != 1 {
		t.Errorf("Orders count = %d, want 1", len(data.Orders))
	}
	if len(data.Trades) != 1 {
		t.Errorf("Trades count = %d, want 1", len(data.Trades))
	}

	if data.Orders[0].ID != "o1" {
		t.Errorf("Order ID = %q, want o1", data.Orders[0].ID)
	}
	if data.Trades[0].ID != "t1" {
		t.Errorf("Trade ID = %q, want t1", data.Trades[0].ID)
	}
}

// TestVertexTimestamp tests timestamp parsing
func TestVertexTimestamp(t *testing.T) {
	adapter := New("http://localhost:9650")

	// Unix nanoseconds: 1700000000000000000 = 2023-11-14 22:13:20 UTC
	input := json.RawMessage(`{
		"id": "ts_vertex",
		"parentIds": [],
		"height": 1,
		"timestamp": 1700000000000000000,
		"status": "accepted",
		"data": {}
	}`)

	vertex, err := adapter.ParseVertex(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedTime := time.Unix(0, 1700000000000000000)
	if !vertex.Timestamp.Equal(expectedTime) {
		t.Errorf("Timestamp = %v, want %v", vertex.Timestamp, expectedTime)
	}
}

// Benchmarks

func BenchmarkParseVertex(b *testing.B) {
	adapter := New("http://localhost:9650")
	input := json.RawMessage(`{
		"id": "benchmark_vertex",
		"parentIds": ["p1", "p2", "p3"],
		"height": 1000,
		"timestamp": 1700000000000000000,
		"status": "accepted",
		"data": {
			"orders": [
				{"id": "o1", "owner": "0x123", "symbol": "LUX/USD", "side": "buy", "type": "limit", "price": 100000, "quantity": 1000, "status": "open"},
				{"id": "o2", "owner": "0x456", "symbol": "ETH/USD", "side": "sell", "type": "market", "price": 200000, "quantity": 500, "status": "filled"}
			]
		}
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.ParseVertex(input)
	}
}

func BenchmarkInferVertexType(b *testing.B) {
	adapter := New("http://localhost:9650")
	inputs := []json.RawMessage{
		json.RawMessage(`{"orders": [{"id": "o1"}]}`),
		json.RawMessage(`{"trades": [{"id": "t1"}]}`),
		json.RawMessage(`{"pools": [{"id": "p1"}]}`),
		json.RawMessage(`{"swaps": [{"id": "s1"}]}`),
		json.RawMessage(`{"liquidityEvents": [{"id": "l1"}]}`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, input := range inputs {
			adapter.inferVertexType(input)
		}
	}
}

func BenchmarkCallRPC(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  []map[string]interface{}{{"id": "v1"}},
		})
	}))
	defer server.Close()

	adapter := New(server.URL)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.GetRecentVertices(ctx, 10)
	}
}
