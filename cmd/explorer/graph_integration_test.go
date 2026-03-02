package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luxfi/graph/engine"
	graphindexer "github.com/luxfi/graph/indexer"
	graphstorage "github.com/luxfi/graph/storage"
)

// TestGraphIntegration boots a graph instance with a fake RPC,
// submits a subgraph query, and asserts the GraphQL response shape.
func TestGraphIntegration(t *testing.T) {
	// Fake EVM RPC that returns block 0 and empty logs.
	fakeRPC := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
		}
		json.Unmarshal(body, &req)

		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "eth_blockNumber":
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":"0x0"}`)
		case "eth_getLogs":
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":[]}`)
		default:
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":null}`)
		}
	}))
	defer fakeRPC.Close()

	// Temp data dir
	dataDir := t.TempDir()
	graphDir := filepath.Join(dataDir, "testchain", "graph")
	if err := os.MkdirAll(graphDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create graph storage
	store, err := graphstorage.New(graphDir)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := store.Init(ctx); err != nil {
		t.Fatalf("storage init: %v", err)
	}

	// Seed test data
	store.SeedToken("0xabc", &graphstorage.SeedTokenData{
		Symbol: "TEST", Name: "Test Token", Decimals: 18,
	})
	store.SeedFactory("uniswap", &graphstorage.SeedFactoryData{
		PoolCount: 42, TxCount: 100,
		TotalVolumeUSD: "1000000", TotalValueLockedUSD: "500000",
	})

	// Start indexer (will poll fake RPC)
	idx := graphindexer.New(fakeRPC.URL, store)
	go idx.Run(ctx)

	// Create engine
	eng := engine.New(store, &engine.Config{
		MaxQueryDepth:  10,
		MaxResultSize:  1 << 20,
		QueryTimeoutMs: 5000,
	})
	eng.LoadBuiltin("amm")

	// Mount handlers on a test mux
	mux := http.NewServeMux()
	gi := &graphInstance{
		slug:    "testchain",
		store:   store,
		indexer: idx,
		eng:     eng,
		cancel:  cancel,
		stopRep: func() {},
	}
	mountGraphHandlers(mux, "testchain", gi)

	// Start test server
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	addr := fmt.Sprintf("http://%s", ln.Addr().String())

	// Wait briefly for server readiness
	time.Sleep(100 * time.Millisecond)

	// --- Test 1: POST GraphQL query ---
	t.Run("graphql_query", func(t *testing.T) {
		query := `{"query":"{ tokens(first: 10) { id symbol name decimals } }"}`
		resp, err := http.Post(addr+"/v1/graph/testchain/", "application/json", strings.NewReader(query))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status %d: %s", resp.StatusCode, body)
		}

		var gqlResp engine.Response
		if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if len(gqlResp.Errors) > 0 {
			t.Fatalf("graphql errors: %v", gqlResp.Errors)
		}
		if gqlResp.Data == nil {
			t.Fatal("expected data, got nil")
		}

		data, ok := gqlResp.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("data not a map: %T", gqlResp.Data)
		}
		tokens, ok := data["tokens"]
		if !ok {
			t.Fatal("missing 'tokens' in response data")
		}
		tokenList, ok := tokens.([]interface{})
		if !ok {
			t.Fatalf("tokens not a list: %T", tokens)
		}
		if len(tokenList) != 1 {
			t.Fatalf("expected 1 token, got %d", len(tokenList))
		}
		tok, ok := tokenList[0].(map[string]interface{})
		if !ok {
			t.Fatal("token not a map")
		}
		if tok["symbol"] != "TEST" {
			t.Errorf("expected symbol TEST, got %v", tok["symbol"])
		}
	})

	// --- Test 2: GET /schema ---
	t.Run("schema_endpoint", func(t *testing.T) {
		resp, err := http.Get(addr + "/v1/graph/testchain/schema")
		if err != nil {
			t.Fatalf("GET schema: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("schema status %d: %s", resp.StatusCode, body)
		}
		body, _ := io.ReadAll(resp.Body)
		if len(body) == 0 {
			t.Fatal("schema response empty")
		}
		// Verify it's valid JSON
		var v interface{}
		if err := json.Unmarshal(body, &v); err != nil {
			t.Fatalf("schema not valid JSON: %v", err)
		}
	})

	// --- Test 3: GET /playground ---
	t.Run("playground_endpoint", func(t *testing.T) {
		resp, err := http.Get(addr + "/v1/graph/testchain/playground")
		if err != nil {
			t.Fatalf("GET playground: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("playground status %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "graphiql") {
			t.Fatal("playground HTML missing graphiql reference")
		}
	})

	// --- Test 4: GET /health ---
	t.Run("health_endpoint", func(t *testing.T) {
		resp, err := http.Get(addr + "/v1/graph/testchain/health")
		if err != nil {
			t.Fatalf("GET health: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("health status %d", resp.StatusCode)
		}
		var health struct {
			Status string `json:"status"`
			Chain  string `json:"chain"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
			t.Fatalf("decode health: %v", err)
		}
		if health.Status != "ok" {
			t.Errorf("health status: %s", health.Status)
		}
		if health.Chain != "testchain" {
			t.Errorf("health chain: %s", health.Chain)
		}
	})

	// --- Test 5: Factory query (seeded data) ---
	t.Run("factory_query", func(t *testing.T) {
		query := `{"query":"{ factory(id: \"uniswap\") { id poolCount totalVolumeUSD } }"}`
		resp, err := http.Post(addr+"/v1/graph/testchain/", "application/json", strings.NewReader(query))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()

		var gqlResp engine.Response
		json.NewDecoder(resp.Body).Decode(&gqlResp)

		if len(gqlResp.Errors) > 0 {
			t.Fatalf("errors: %v", gqlResp.Errors)
		}
		data, ok := gqlResp.Data.(map[string]interface{})
		if !ok {
			t.Fatal("data not a map")
		}
		factory, ok := data["factory"].(map[string]interface{})
		if !ok {
			t.Fatalf("factory not a map: %T", data["factory"])
		}
		if factory["id"] != "uniswap" {
			t.Errorf("factory id: %v", factory["id"])
		}
	})
}

// TestGraphConfigParsing verifies the graph config is correctly parsed from YAML.
func TestGraphConfigParsing(t *testing.T) {
	yaml := `
data_dir: /tmp/test
http_addr: :9090
chains:
  - slug: testchain
    name: Test
    chain_id: 1337
    type: evm
    rpc: http://localhost:8545
    coin: ETH
    enabled: true
    graph:
      enabled: true
      schema: uniswap-v2
  - slug: nograph
    name: No Graph
    chain_id: 1338
    type: evm
    rpc: http://localhost:8546
    coin: ETH
    enabled: true
`
	tmpFile := filepath.Join(t.TempDir(), "chains.yaml")
	os.WriteFile(tmpFile, []byte(yaml), 0644)

	cfg, err := loadConfig(tmpFile)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if len(cfg.Chains) != 2 {
		t.Fatalf("expected 2 chains, got %d", len(cfg.Chains))
	}

	// Chain with graph enabled
	c := cfg.Chains[0]
	if c.Graph == nil {
		t.Fatal("expected graph config on testchain")
	}
	if !c.Graph.Enabled {
		t.Error("expected graph.enabled=true")
	}
	if c.Graph.Schema != "uniswap-v2" {
		t.Errorf("expected schema uniswap-v2, got %s", c.Graph.Schema)
	}

	// Chain without graph
	c2 := cfg.Chains[1]
	if c2.Graph != nil {
		t.Error("expected nil graph config on nograph chain")
	}
}
