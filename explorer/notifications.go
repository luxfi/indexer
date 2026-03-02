package explorer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hanzoai/base/core"
)

// WatchEntry describes a single webhook subscription for an address.
type WatchEntry struct {
	Address        string `json:"address"`
	NotifyIncoming bool   `json:"notify_incoming"`
	NotifyOutgoing bool   `json:"notify_outgoing"`
	WebhookURL     string `json:"url"`
}

// Notification is the payload POSTed to webhook URLs.
type Notification struct {
	Type      string `json:"type"`
	Address   string `json:"address"`
	TxHash    string `json:"tx_hash"`
	From      string `json:"from"`
	To        string `json:"to"`
	Value     string `json:"value"`
	Timestamp int64  `json:"timestamp"`
}

// NotificationWorker polls the indexer DB for new transactions matching watchlist entries.
type NotificationWorker struct {
	db       *sql.DB
	table    string // transactions table name
	interval time.Duration
	client   *http.Client
	logger   *slog.Logger

	mu        sync.RWMutex
	watchlist map[string][]WatchEntry // address (lowercase) -> entries

	lastBlock int64
	stop      chan struct{}
	done      chan struct{}
}

// NewNotificationWorker creates a stopped worker. Call Start() to begin polling.
func NewNotificationWorker(db *sql.DB, txTable string, logger *slog.Logger) *NotificationWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &NotificationWorker{
		db:        db,
		table:     txTable,
		interval:  10 * time.Second,
		client:    &http.Client{Timeout: 5 * time.Second},
		logger:    logger,
		watchlist: make(map[string][]WatchEntry),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// Register adds a webhook subscription. Thread-safe.
func (w *NotificationWorker) Register(entry WatchEntry) {
	addr := strings.ToLower(entry.Address)
	entry.Address = addr

	w.mu.Lock()
	defer w.mu.Unlock()
	w.watchlist[addr] = append(w.watchlist[addr], entry)
}

// Unregister removes all subscriptions for an address+URL pair.
func (w *NotificationWorker) Unregister(address, webhookURL string) {
	addr := strings.ToLower(address)

	w.mu.Lock()
	defer w.mu.Unlock()
	entries := w.watchlist[addr]
	filtered := entries[:0]
	for _, e := range entries {
		if e.WebhookURL != webhookURL {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == 0 {
		delete(w.watchlist, addr)
	} else {
		w.watchlist[addr] = filtered
	}
}

// Entries returns a snapshot of all watch entries. Thread-safe.
func (w *NotificationWorker) Entries() []WatchEntry {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var all []WatchEntry
	for _, entries := range w.watchlist {
		all = append(all, entries...)
	}
	return all
}

// Start begins the polling loop in a goroutine. Call Stop() to shut down.
func (w *NotificationWorker) Start() {
	// Seed lastBlock from current max.
	var maxBlock int64
	w.db.QueryRow(fmt.Sprintf("SELECT COALESCE(MAX(block_number), 0) FROM %s", w.table)).Scan(&maxBlock)
	w.lastBlock = maxBlock
	w.logger.Info("notification worker started", slog.Int64("from_block", maxBlock))

	go w.loop()
}

// Stop signals the worker to stop and waits for it to finish.
func (w *NotificationWorker) Stop() {
	close(w.stop)
	<-w.done
}

func (w *NotificationWorker) loop() {
	defer close(w.done)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.poll()
		}
	}
}

func (w *NotificationWorker) poll() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := w.db.QueryContext(ctx,
		fmt.Sprintf("SELECT hash, block_number, from_address_hash, to_address_hash, value, block_timestamp FROM %s WHERE block_number > ? ORDER BY block_number, transaction_index", w.table),
		w.lastBlock)
	if err != nil {
		w.logger.Warn("notification poll failed", slog.String("error", err.Error()))
		return
	}
	defer rows.Close()

	var maxBlock int64
	for rows.Next() {
		var hash, from, to any
		var value string
		var blockNum, ts int64
		if err := rows.Scan(&hash, &blockNum, &from, &to, &value, &ts); err != nil {
			continue
		}

		if blockNum > maxBlock {
			maxBlock = blockNum
		}

		fromLower := strings.ToLower(bytesToHex(from))
		toLower := strings.ToLower(bytesToHex(to))

		w.mu.RLock()
		// Check outgoing (from matches watchlist)
		if entries, ok := w.watchlist[fromLower]; ok {
			for _, e := range entries {
				if e.NotifyOutgoing {
					w.deliver(e.WebhookURL, Notification{
						Type:      "outgoing_tx",
						Address:   e.Address,
						TxHash:    bytesToHex(hash),
						From:      fromLower,
						To:        toLower,
						Value:     value,
						Timestamp: ts,
					})
				}
			}
		}
		// Check incoming (to matches watchlist)
		if entries, ok := w.watchlist[toLower]; ok {
			for _, e := range entries {
				if e.NotifyIncoming {
					w.deliver(e.WebhookURL, Notification{
						Type:      "incoming_tx",
						Address:   e.Address,
						TxHash:    bytesToHex(hash),
						From:      fromLower,
						To:        toLower,
						Value:     value,
						Timestamp: ts,
					})
				}
			}
		}
		w.mu.RUnlock()
	}

	if maxBlock > w.lastBlock {
		w.lastBlock = maxBlock
	}
}

func (w *NotificationWorker) deliver(url string, n Notification) {
	body, err := json.Marshal(n)
	if err != nil {
		return
	}
	resp, err := w.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		w.logger.Warn("webhook delivery failed",
			slog.String("url", url), slog.String("tx", n.TxHash), slog.String("error", err.Error()))
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		w.logger.Warn("webhook returned error",
			slog.String("url", url), slog.Int("status", resp.StatusCode))
	}
}

// ---- HTTP Handlers ----

// handleRegisterWebhook handles POST /v1/explorer/webhooks (plugin mode).
func (p *plugin) handleRegisterWebhook(e *core.RequestEvent) error {
	var entry WatchEntry
	if err := json.NewDecoder(e.Request.Body).Decode(&entry); err != nil {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
	}
	if entry.WebhookURL == "" || entry.Address == "" {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "url and address required"})
	}
	if p.notifWorker == nil {
		return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": "notifications not enabled"})
	}
	p.notifWorker.Register(entry)
	return e.JSON(http.StatusCreated, map[string]string{"status": "registered"})
}

// handleListWebhooks handles GET /v1/explorer/webhooks (plugin mode).
func (p *plugin) handleListWebhooks(e *core.RequestEvent) error {
	if p.notifWorker == nil {
		return e.JSON(http.StatusOK, []any{})
	}
	return e.JSON(http.StatusOK, p.notifWorker.Entries())
}

// handleDeleteWebhook handles DELETE /v1/explorer/webhooks (plugin mode).
func (p *plugin) handleDeleteWebhook(e *core.RequestEvent) error {
	var req struct {
		Address string `json:"address"`
		URL     string `json:"url"`
	}
	if err := json.NewDecoder(e.Request.Body).Decode(&req); err != nil {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
	}
	if p.notifWorker == nil {
		return e.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}
	p.notifWorker.Unregister(req.Address, req.URL)
	return e.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// registerWebhookStandalone handles POST /v1/explorer/webhooks (standalone mode).
func (s *StandaloneServer) registerWebhook(r *http.Request) (any, int) {
	var entry WatchEntry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		return map[string]string{"error": "invalid JSON"}, 400
	}
	if entry.WebhookURL == "" || entry.Address == "" {
		return map[string]string{"error": "url and address required"}, 400
	}
	if s.notifWorker == nil {
		return map[string]string{"error": "notifications not enabled"}, 503
	}
	s.notifWorker.Register(entry)
	return map[string]string{"status": "registered"}, 201
}

// listWebhooksStandalone handles GET /v1/explorer/webhooks (standalone mode).
func (s *StandaloneServer) listWebhooks(r *http.Request) (any, int) {
	if s.notifWorker == nil {
		return []any{}, 200
	}
	return s.notifWorker.Entries(), 200
}

// deleteWebhookStandalone handles DELETE /v1/explorer/webhooks (standalone mode).
func (s *StandaloneServer) deleteWebhook(r *http.Request) (any, int) {
	var req struct {
		Address string `json:"address"`
		URL     string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return map[string]string{"error": "invalid JSON"}, 400
	}
	if s.notifWorker == nil {
		return map[string]string{"status": "ok"}, 200
	}
	s.notifWorker.Unregister(req.Address, req.URL)
	return map[string]string{"status": "deleted"}, 200
}
