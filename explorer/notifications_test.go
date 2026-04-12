package explorer

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luxfi/explorer/explorer/testutil"
)

func TestNotificationWorker_RegisterUnregister(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	w := NewNotificationWorker(tdb.DB, "transactions", nil)

	testAddr := "0xABCdef0123456789abcdef0123456789AbCdEf01"
	testAddrLower := "0xabcdef0123456789abcdef0123456789abcdef01"

	if err := w.Register(WatchEntry{
		Address:        testAddr,
		NotifyIncoming: true,
		WebhookURL:     "https://example.com/hook",
	}); err != nil {
		t.Fatal(err)
	}

	entries := w.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Address != testAddrLower {
		t.Errorf("address = %s, want %s (lowercase)", entries[0].Address, testAddrLower)
	}

	// Register another for same address.
	if err := w.Register(WatchEntry{
		Address:        testAddr,
		NotifyOutgoing: true,
		WebhookURL:     "https://other.com/hook",
	}); err != nil {
		t.Fatal(err)
	}
	if len(w.Entries()) != 2 {
		t.Fatalf("entries = %d, want 2", len(w.Entries()))
	}

	// Unregister one.
	w.Unregister(testAddrLower, "https://example.com/hook")
	entries = w.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 after unregister", len(entries))
	}
	if entries[0].WebhookURL != "https://other.com/***" { // URLs are redacted in Entries()
		t.Errorf("remaining url = %s", entries[0].WebhookURL)
	}

	// Unregister last.
	w.Unregister(testAddrLower, "https://other.com/hook")
	if len(w.Entries()) != 0 {
		t.Fatalf("entries = %d, want 0", len(w.Entries()))
	}
}

func TestNotificationWorker_StartStop(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	w := NewNotificationWorker(tdb.DB, "transactions", nil)
	w.interval = 50 * time.Millisecond
	w.Start()
	time.Sleep(100 * time.Millisecond)
	w.Stop() // Should not hang.
}

func TestNotificationWorker_DeliveryOnNewTx(t *testing.T) {
	tdb := testutil.NewTestDB(t)

	// Insert a block first for the foreign key.
	block := testutil.DefaultBlock()
	tdb.InsertBlock(t, block)

	// Insert a tx at block N.
	tx := testutil.DefaultTransaction(block)
	tdb.InsertTransaction(t, tx)

	// Capture webhook deliveries.
	var mu sync.Mutex
	var received []Notification
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var n Notification
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &n)
		mu.Lock()
		received = append(received, n)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	t.Cleanup(hook.Close)

	w := NewNotificationWorker(tdb.DB, "transactions", nil)
	w.allowInternal = true // allow localhost httptest server
	w.interval = 50 * time.Millisecond
	// Set lastBlock to before the tx's block so the poll picks it up.
	w.lastBlock = block.Number - 1

	fromHex := bytesToHex(tx.FromAddress)
	if err := w.Register(WatchEntry{
		Address:        fromHex,
		NotifyOutgoing: true,
		WebhookURL:     hook.URL,
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.Register(WatchEntry{
		Address:        bytesToHex(tx.ToAddress),
		NotifyIncoming: true,
		WebhookURL:     hook.URL,
	}); err != nil {
		t.Fatal(err)
	}

	// Run one poll cycle.
	w.poll()

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("received %d notifications, want 2", len(received))
	}

	// Verify types.
	types := map[string]bool{}
	for _, n := range received {
		types[n.Type] = true
	}
	if !types["outgoing_tx"] {
		t.Error("missing outgoing_tx notification")
	}
	if !types["incoming_tx"] {
		t.Error("missing incoming_tx notification")
	}
}

func TestNotificationWorker_NoMatchNoDelivery(t *testing.T) {
	tdb := testutil.NewTestDB(t)

	block := testutil.DefaultBlock()
	tdb.InsertBlock(t, block)
	tx := testutil.DefaultTransaction(block)
	tdb.InsertTransaction(t, tx)

	hookCalled := false
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hookCalled = true
		w.WriteHeader(200)
	}))
	t.Cleanup(hook.Close)

	w := NewNotificationWorker(tdb.DB, "transactions", nil)
	w.allowInternal = true // allow localhost httptest server
	w.lastBlock = block.Number - 1

	// Register for a completely different address.
	if err := w.Register(WatchEntry{
		Address:        "0x0000000000000000000000000000000000000000",
		NotifyIncoming: true,
		NotifyOutgoing: true,
		WebhookURL:     hook.URL,
	}); err != nil {
		t.Fatal(err)
	}

	w.poll()

	if hookCalled {
		t.Error("webhook should not have been called for non-matching address")
	}
}

func TestWebhookRegistrationEndpoint(t *testing.T) {
	t.Skip("TODO: fix schema mismatch between test factory and standalone server")
	tdb := testutil.NewTestDB(t)

	srv, err := NewStandaloneServer(Config{
		IndexerDBPath: tdb.Path,
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

	// POST /v1/explorer/webhooks
	testAddr := "0xDeaDbeeF01234567890AbCdEf0123456789aBcDe"
	testAddrLower := "0xdeadbeef01234567890abcdef0123456789abcde"
	body := `{"url": "https://example.com/hook", "address": "` + testAddr + `", "notify_incoming": true, "notify_outgoing": false}`
	resp, err := http.Post(ts.URL+"/v1/explorer/webhooks", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201; body: %s", resp.StatusCode, b)
	}

	// GET /v1/explorer/webhooks
	resp2, err := http.Get(ts.URL + "/v1/explorer/webhooks")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp2.Body.Close()

	var entries []WatchEntry
	json.NewDecoder(resp2.Body).Decode(&entries)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Address != testAddrLower {
		t.Errorf("address = %s, want %s", entries[0].Address, testAddrLower)
	}

	// POST with missing fields.
	badBody := `{"url": ""}`
	resp3, err := http.Post(ts.URL+"/v1/explorer/webhooks", "application/json", strings.NewReader(badBody))
	if err != nil {
		t.Fatalf("POST bad: %v", err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != 400 {
		t.Fatalf("bad request status = %d, want 400", resp3.StatusCode)
	}
}

func TestTokenResponseIncludesTrustScore(t *testing.T) {
	tdb := testutil.NewTestDB(t)

	token := testutil.DefaultToken()
	token.HolderCount = 500
	token.IconURL = "https://example.com/icon.png"
	tdb.InsertToken(t, token)

	srv, err := NewStandaloneServer(Config{
		IndexerDBPath: tdb.Path,
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

	resp, err := http.Get(ts.URL + "/v1/explorer/tokens")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	items, ok := result["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatal("no items in token list")
	}

	tok, ok := items[0].(map[string]any)
	if !ok {
		t.Fatal("item not a map")
	}

	score, ok := tok["trust_score"]
	if !ok {
		t.Fatal("trust_score field missing from token response")
	}

	// Token has 500 holders (>10, >100) + icon_url + total_supply > 0 = 20+15+15+10 = 60
	scoreNum, ok := score.(float64)
	if !ok {
		t.Fatalf("trust_score type = %T, want float64", score)
	}
	if scoreNum < 40 {
		t.Errorf("trust_score = %f, expected >= 40 for token with 500 holders + icon + supply", scoreNum)
	}
}
