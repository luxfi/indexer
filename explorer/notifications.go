package explorer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxWatchlistEntries = 1000

// isInternalURL rejects URLs pointing to internal/private networks (SSRF prevention).
func isInternalURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	// Only allow https (never http for webhook delivery).
	if u.Scheme != "https" {
		return true
	}
	host := u.Hostname()
	if host == "" {
		return true
	}
	// Reject loopback, private, link-local IPs (IPv4 and IPv6).
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return true
		}
		// Reject IPv4-mapped IPv6 (::ffff:127.0.0.1, ::ffff:10.x.x.x, etc.)
		if ip4 := ip.To4(); ip4 != nil {
			if ip4.IsLoopback() || ip4.IsPrivate() || ip4.IsLinkLocalUnicast() {
				return true
			}
		}
	}
	// Reject K8s internal domains.
	lower := strings.ToLower(host)
	for _, suffix := range []string{".svc", ".cluster.local", ".internal", "localhost"} {
		if strings.HasSuffix(lower, suffix) || lower == strings.TrimPrefix(suffix, ".") {
			return true
		}
	}
	// Reject cloud metadata endpoints (GCP, AWS, Azure).
	if host == "169.254.169.254" || host == "metadata.google.internal" ||
		host == "169.254.170.2" || host == "fd00:ec2::254" {
		return true
	}
	// Reject URLs with userinfo (http://attacker@internal).
	if u.User != nil {
		return true
	}
	return false
}

// redactURL shows only the domain, hiding the path (prevents webhook URL enumeration).
func redactURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "***"
	}
	return u.Scheme + "://" + u.Host + "/***"
}

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

	mu            sync.RWMutex
	watchlist     map[string][]WatchEntry // address (lowercase) -> entries
	allowInternal bool                    // for testing only — bypasses SSRF protection

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
		db:       db,
		table:    txTable,
		interval: 10 * time.Second,
		client: &http.Client{
			Timeout: 5 * time.Second,
			// Prevent redirect-based SSRF: do not follow redirects.
			// An attacker could register https://evil.com which 302s to http://169.254.169.254.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		logger:    logger,
		watchlist: make(map[string][]WatchEntry),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// Register adds a webhook subscription. Thread-safe.
// Returns error if URL is internal, address is invalid, or watchlist is full.
// Set allowInternal=true for testing only.
func (w *NotificationWorker) Register(entry WatchEntry) error {
	if !w.allowInternal && isInternalURL(entry.WebhookURL) {
		return fmt.Errorf("internal URLs not allowed")
	}
	// Validate address format to prevent garbage entries and potential abuse.
	if !hexAddrPattern.MatchString(entry.Address) {
		return fmt.Errorf("invalid address format")
	}
	addr := strings.ToLower(entry.Address)
	entry.Address = addr

	w.mu.Lock()
	defer w.mu.Unlock()

	// Count total entries
	total := 0
	for _, entries := range w.watchlist {
		total += len(entries)
	}
	if total >= maxWatchlistEntries {
		return fmt.Errorf("watchlist full (max %d)", maxWatchlistEntries)
	}

	w.watchlist[addr] = append(w.watchlist[addr], entry)
	return nil
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
// Entries returns all webhook subscriptions with URLs redacted for security.
func (w *NotificationWorker) Entries() []WatchEntry {
	w.mu.RLock()
	defer w.mu.RUnlock()
	all := make([]WatchEntry, 0) // never nil — JSON encodes as [] not null
	for _, entries := range w.watchlist {
		for _, e := range entries {
			redacted := e
			redacted.WebhookURL = redactURL(e.WebhookURL)
			all = append(all, redacted)
		}
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

	// Auto-detect column names (different schemas use different names)
	fromCol, toCol, tsCol, idxCol := "from_addr", "to_addr", "timestamp", "tx_index"
	// Check if using the test/PG schema with _hash suffix
	var colCount int
	if err := w.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name='from_address_hash'", w.table)).Scan(&colCount); err == nil && colCount > 0 {
		fromCol, toCol = "from_address_hash", "to_address_hash"
	}
	if err := w.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name='block_timestamp'", w.table)).Scan(&colCount); err == nil && colCount > 0 {
		tsCol = "block_timestamp"
	}
	if err := w.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name='transaction_index'", w.table)).Scan(&colCount); err == nil && colCount > 0 {
		idxCol = "transaction_index"
	}

	rows, err := w.db.QueryContext(ctx,
		fmt.Sprintf("SELECT hash, block_number, %s, %s, value, %s FROM %s WHERE block_number > ? ORDER BY block_number, %s", fromCol, toCol, tsCol, w.table, idxCol),
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

func (w *NotificationWorker) deliver(rawURL string, n Notification) {
	// Re-check SSRF at delivery time to prevent DNS rebinding attacks.
	// An attacker can register https://evil.com (resolves to public IP at registration),
	// then change DNS to 169.254.169.254 before delivery.
	if !w.allowInternal && isInternalURL(rawURL) {
		w.logger.Warn("webhook blocked (internal URL at delivery time)",
			slog.String("url", redactURL(rawURL)))
		return
	}
	body, err := json.Marshal(n)
	if err != nil {
		return
	}
	resp, err := w.client.Post(rawURL, "application/json", bytes.NewReader(body))
	if err != nil {
		w.logger.Warn("webhook delivery failed",
			slog.String("url", redactURL(rawURL)), slog.String("tx", n.TxHash), slog.String("error", err.Error()))
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		w.logger.Warn("webhook returned error",
			slog.String("url", redactURL(rawURL)), slog.Int("status", resp.StatusCode))
	}
}

// ---- HTTP Handlers (Service methods) ----

// handleRegisterWebhook handles POST /v1/explorer/webhooks.
func (s *Service) handleRegisterWebhook(w http.ResponseWriter, r *http.Request) {
	var entry WatchEntry
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes)).Decode(&entry); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if entry.WebhookURL == "" || entry.Address == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url and address required"})
		return
	}
	if s.notifWorker == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "notifications not enabled"})
		return
	}
	if err := s.notifWorker.Register(entry); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "registered"})
}

// handleListWebhooks handles GET /v1/explorer/webhooks.
func (s *Service) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	if s.notifWorker == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, s.notifWorker.Entries())
}

// handleDeleteWebhook handles DELETE /v1/explorer/webhooks.
func (s *Service) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address string `json:"address"`
		URL     string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if s.notifWorker == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	s.notifWorker.Unregister(req.Address, req.URL)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// registerWebhookStandalone handles POST /v1/explorer/webhooks (standalone mode).
func (ss *StandaloneServer) registerWebhook(r *http.Request) (any, int) {
	var entry WatchEntry
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes)).Decode(&entry); err != nil {
		return map[string]string{"error": "invalid JSON"}, 400
	}
	if entry.WebhookURL == "" || entry.Address == "" {
		return map[string]string{"error": "url and address required"}, 400
	}
	if ss.notifWorker == nil {
		return map[string]string{"error": "notifications not enabled"}, 503
	}
	if err := ss.notifWorker.Register(entry); err != nil {
		return map[string]string{"error": err.Error()}, 403
	}
	return map[string]string{"status": "registered"}, 201
}

// listWebhooksStandalone handles GET /v1/explorer/webhooks (standalone mode).
func (ss *StandaloneServer) listWebhooks(r *http.Request) (any, int) {
	if ss.notifWorker == nil {
		return []any{}, 200
	}
	return ss.notifWorker.Entries(), 200
}

// deleteWebhookStandalone handles DELETE /v1/explorer/webhooks (standalone mode).
func (ss *StandaloneServer) deleteWebhook(r *http.Request) (any, int) {
	var req struct {
		Address string `json:"address"`
		URL     string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes)).Decode(&req); err != nil {
		return map[string]string{"error": "invalid JSON"}, 400
	}
	if ss.notifWorker == nil {
		return map[string]string{"status": "ok"}, 200
	}
	ss.notifWorker.Unregister(req.Address, req.URL)
	return map[string]string{"status": "deleted"}, 200
}
