// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package evm

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sync"
	"time"
)

// PendingTransaction represents a transaction in the mempool
type PendingTransaction struct {
	Hash           string        `json:"hash"`
	From           string        `json:"from"`
	To             string        `json:"to"`
	Value          string        `json:"value"`
	Gas            uint64        `json:"gas"`
	GasPrice       string        `json:"gasPrice,omitempty"`
	MaxFeePerGas   string        `json:"maxFeePerGas,omitempty"`
	MaxPriorityFee string        `json:"maxPriorityFeePerGas,omitempty"`
	Nonce          uint64        `json:"nonce"`
	Input          string        `json:"input"`
	Type           uint8         `json:"type"`
	V              string        `json:"v,omitempty"`
	R              string        `json:"r,omitempty"`
	S              string        `json:"s,omitempty"`
	FirstSeenAt    time.Time     `json:"firstSeenAt"`
	LastSeenAt     time.Time     `json:"lastSeenAt"`
	FirstSeen      time.Time     `json:"firstSeen"`            // Alias for enhanced compatibility
	LastSeen       time.Time     `json:"lastSeen"`             // Alias for enhanced compatibility
	SeenCount      int           `json:"seenCount"`            // Times seen in mempool
	Status         string        `json:"status"`               // pending, replaced, dropped, confirmed
	ReplacedBy     string        `json:"replacedBy,omitempty"` // Hash of replacing tx
	GasEstimate    uint64        `json:"gasEstimate,omitempty"`
	DecodedInput   *DecodedInput `json:"decodedInput,omitempty"`
}

// DecodedInput represents decoded transaction input data
type DecodedInput struct {
	Method     string                 `json:"method"`
	MethodID   string                 `json:"methodId"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// PendingPoolStats represents mempool statistics
type PendingPoolStats struct {
	TotalCount      int       `json:"totalCount"`
	PendingCount    int       `json:"pendingCount"`
	QueuedCount     int       `json:"queuedCount"`
	TotalGasPrice   *big.Int  `json:"totalGasPrice"`
	AverageGasPrice *big.Int  `json:"averageGasPrice"`
	MinGasPrice     *big.Int  `json:"minGasPrice"`
	MaxGasPrice     *big.Int  `json:"maxGasPrice"`
	OldestTxTime    time.Time `json:"oldestTxTime"`
	NewestTxTime    time.Time `json:"newestTxTime"`
	TotalValue      *big.Int  `json:"totalValue"`
	UniqueFromAddrs int       `json:"uniqueFromAddresses"`
	UniqueToAddrs   int       `json:"uniqueToAddresses"`
	ContractCalls   int       `json:"contractCalls"`
	SimpleTransfers int       `json:"simpleTransfers"`
	TokenTransfers  int       `json:"tokenTransfers"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// PendingPoolMonitor monitors the pending transaction pool (mempool)
type PendingPoolMonitor struct {
	rpcURL        string
	httpClient    *httpClient
	pending       map[string]*PendingTransaction
	pendingMu     sync.RWMutex
	stats         *PendingPoolStats
	statsMu       sync.RWMutex
	pollInterval  time.Duration
	maxPendingAge time.Duration
	stopCh        chan struct{}
	onNewPending  func(*PendingTransaction)
	onRemoved     func(string) // tx hash removed (confirmed or dropped)
}

// httpClient is a simple HTTP client wrapper for RPC calls
type httpClient struct {
	endpoint string
}

// PendingPoolConfig configures the pending pool monitor
type PendingPoolConfig struct {
	RPCURL        string
	PollInterval  time.Duration
	MaxPendingAge time.Duration
	OnNewPending  func(*PendingTransaction)
	OnRemoved     func(string)
}

// NewPendingPoolMonitor creates a new pending transaction pool monitor
func NewPendingPoolMonitor(config PendingPoolConfig) *PendingPoolMonitor {
	if config.PollInterval == 0 {
		config.PollInterval = 2 * time.Second
	}
	if config.MaxPendingAge == 0 {
		config.MaxPendingAge = 30 * time.Minute
	}

	return &PendingPoolMonitor{
		rpcURL:        config.RPCURL,
		httpClient:    &httpClient{endpoint: config.RPCURL},
		pending:       make(map[string]*PendingTransaction),
		stats:         &PendingPoolStats{},
		pollInterval:  config.PollInterval,
		maxPendingAge: config.MaxPendingAge,
		stopCh:        make(chan struct{}),
		onNewPending:  config.OnNewPending,
		onRemoved:     config.OnRemoved,
	}
}

// Start begins monitoring the pending pool
func (m *PendingPoolMonitor) Start(ctx context.Context) error {
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.stopCh:
			return nil
		case <-ticker.C:
			if err := m.poll(ctx); err != nil {
				// Log error but continue polling
				fmt.Printf("pending pool poll error: %v\n", err)
			}
		}
	}
}

// Stop stops the monitor
func (m *PendingPoolMonitor) Stop() {
	close(m.stopCh)
}

// poll fetches current pending transactions from the node
func (m *PendingPoolMonitor) poll(ctx context.Context) error {
	// Get pending transactions using txpool_content
	content, err := m.getTxPoolContent(ctx)
	if err != nil {
		// Fall back to eth_pendingTransactions if txpool_content not available
		return m.pollWithPendingTransactions(ctx)
	}

	now := time.Now()
	seenHashes := make(map[string]bool)

	// Process pending transactions
	for _, txsByNonce := range content.Pending {
		for _, tx := range txsByNonce {
			seenHashes[tx.Hash] = true
			m.processPendingTx(tx, now)
		}
	}

	// Process queued transactions
	for _, txsByNonce := range content.Queued {
		for _, tx := range txsByNonce {
			seenHashes[tx.Hash] = true
			m.processPendingTx(tx, now)
		}
	}

	// Remove transactions that are no longer pending
	m.cleanupRemovedTxs(seenHashes, now)

	// Update stats
	m.updateStats(len(content.Pending), len(content.Queued))

	return nil
}

// pollWithPendingTransactions uses eth_pendingTransactions as fallback
func (m *PendingPoolMonitor) pollWithPendingTransactions(ctx context.Context) error {
	txs, err := m.getPendingTransactions(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	seenHashes := make(map[string]bool)

	for _, tx := range txs {
		seenHashes[tx.Hash] = true
		m.processPendingTx(tx, now)
	}

	m.cleanupRemovedTxs(seenHashes, now)
	m.updateStats(len(txs), 0)

	return nil
}

// processPendingTx processes a single pending transaction
func (m *PendingPoolMonitor) processPendingTx(tx *PendingTransaction, now time.Time) {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()

	existing, exists := m.pending[tx.Hash]
	if exists {
		existing.LastSeenAt = now
	} else {
		tx.FirstSeenAt = now
		tx.LastSeenAt = now
		m.pending[tx.Hash] = tx

		// Decode input if possible
		if len(tx.Input) > 10 {
			tx.DecodedInput = decodeTransactionInput(tx.Input)
		}

		// Notify callback
		if m.onNewPending != nil {
			go m.onNewPending(tx)
		}
	}
}

// cleanupRemovedTxs removes transactions no longer in the pool
func (m *PendingPoolMonitor) cleanupRemovedTxs(seenHashes map[string]bool, now time.Time) {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()

	for hash, tx := range m.pending {
		if !seenHashes[hash] || now.Sub(tx.FirstSeenAt) > m.maxPendingAge {
			delete(m.pending, hash)
			if m.onRemoved != nil {
				go m.onRemoved(hash)
			}
		}
	}
}

// updateStats calculates and updates pool statistics
func (m *PendingPoolMonitor) updateStats(pendingCount, queuedCount int) {
	m.pendingMu.RLock()
	defer m.pendingMu.RUnlock()

	stats := &PendingPoolStats{
		TotalCount:    len(m.pending),
		PendingCount:  pendingCount,
		QueuedCount:   queuedCount,
		TotalGasPrice: big.NewInt(0),
		TotalValue:    big.NewInt(0),
		UpdatedAt:     time.Now(),
	}

	fromAddrs := make(map[string]bool)
	toAddrs := make(map[string]bool)

	for _, tx := range m.pending {
		// Track gas prices
		gasPrice, _ := new(big.Int).SetString(tx.GasPrice, 10)
		if gasPrice != nil {
			stats.TotalGasPrice.Add(stats.TotalGasPrice, gasPrice)
			if stats.MinGasPrice == nil || gasPrice.Cmp(stats.MinGasPrice) < 0 {
				stats.MinGasPrice = new(big.Int).Set(gasPrice)
			}
			if stats.MaxGasPrice == nil || gasPrice.Cmp(stats.MaxGasPrice) > 0 {
				stats.MaxGasPrice = new(big.Int).Set(gasPrice)
			}
		}

		// Track value
		value, _ := new(big.Int).SetString(tx.Value, 10)
		if value != nil {
			stats.TotalValue.Add(stats.TotalValue, value)
		}

		// Track addresses
		fromAddrs[tx.From] = true
		if tx.To != "" {
			toAddrs[tx.To] = true
		}

		// Track transaction types
		if len(tx.Input) <= 2 || tx.Input == "0x" {
			stats.SimpleTransfers++
		} else if isTokenTransfer(tx.Input) {
			stats.TokenTransfers++
		} else {
			stats.ContractCalls++
		}

		// Track timestamps
		if stats.OldestTxTime.IsZero() || tx.FirstSeenAt.Before(stats.OldestTxTime) {
			stats.OldestTxTime = tx.FirstSeenAt
		}
		if stats.NewestTxTime.IsZero() || tx.FirstSeenAt.After(stats.NewestTxTime) {
			stats.NewestTxTime = tx.FirstSeenAt
		}
	}

	stats.UniqueFromAddrs = len(fromAddrs)
	stats.UniqueToAddrs = len(toAddrs)

	if stats.TotalCount > 0 {
		stats.AverageGasPrice = new(big.Int).Div(stats.TotalGasPrice, big.NewInt(int64(stats.TotalCount)))
	}

	m.statsMu.Lock()
	m.stats = stats
	m.statsMu.Unlock()
}

// GetPendingTransactions returns all current pending transactions
func (m *PendingPoolMonitor) GetPendingTransactions() []*PendingTransaction {
	m.pendingMu.RLock()
	defer m.pendingMu.RUnlock()

	result := make([]*PendingTransaction, 0, len(m.pending))
	for _, tx := range m.pending {
		result = append(result, tx)
	}
	return result
}

// GetPendingTransaction returns a specific pending transaction by hash
func (m *PendingPoolMonitor) GetPendingTransaction(hash string) *PendingTransaction {
	m.pendingMu.RLock()
	defer m.pendingMu.RUnlock()
	return m.pending[hash]
}

// GetStats returns current pool statistics
func (m *PendingPoolMonitor) GetStats() *PendingPoolStats {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()
	return m.stats
}

// GetPendingByAddress returns pending transactions for a specific address
func (m *PendingPoolMonitor) GetPendingByAddress(address string) []*PendingTransaction {
	m.pendingMu.RLock()
	defer m.pendingMu.RUnlock()

	var result []*PendingTransaction
	for _, tx := range m.pending {
		if tx.From == address || tx.To == address {
			result = append(result, tx)
		}
	}
	return result
}

// TxPoolContent represents the response from txpool_content
type TxPoolContent struct {
	Pending map[string]map[string]*PendingTransaction `json:"pending"`
	Queued  map[string]map[string]*PendingTransaction `json:"queued"`
}

// getTxPoolContent calls txpool_content RPC method
func (m *PendingPoolMonitor) getTxPoolContent(ctx context.Context) (*TxPoolContent, error) {
	result, err := m.rpcCall(ctx, "txpool_content", nil)
	if err != nil {
		return nil, err
	}

	var content TxPoolContent
	if err := json.Unmarshal(result, &content); err != nil {
		return nil, fmt.Errorf("failed to unmarshal txpool_content: %w", err)
	}

	return &content, nil
}

// getPendingTransactions calls eth_pendingTransactions RPC method
func (m *PendingPoolMonitor) getPendingTransactions(ctx context.Context) ([]*PendingTransaction, error) {
	result, err := m.rpcCall(ctx, "eth_pendingTransactions", nil)
	if err != nil {
		return nil, err
	}

	var txs []*PendingTransaction
	if err := json.Unmarshal(result, &txs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal pending transactions: %w", err)
	}

	return txs, nil
}

// rpcCall makes a JSON-RPC call
func (m *PendingPoolMonitor) rpcCall(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := newHTTPRequest(ctx, "POST", m.rpcURL, body)
	if err != nil {
		return nil, err
	}

	resp, err := doHTTPRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, err
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// decodeTransactionInput attempts to decode transaction input data
func decodeTransactionInput(input string) *DecodedInput {
	if len(input) < 10 {
		return nil
	}

	methodID := input[:10]
	decoded := &DecodedInput{
		MethodID: methodID,
	}

	// Common method signatures
	methodNames := map[string]string{
		"0xa9059cbb": "transfer",
		"0x23b872dd": "transferFrom",
		"0x095ea7b3": "approve",
		"0x40c10f19": "mint",
		"0x42966c68": "burn",
		"0x38ed1739": "swapExactTokensForTokens",
		"0x7ff36ab5": "swapExactETHForTokens",
		"0x18cbafe5": "swapExactTokensForETH",
		"0x8803dbee": "swapTokensForExactTokens",
		"0xfb3bdb41": "swapETHForExactTokens",
		"0x4a25d94a": "swapTokensForExactETH",
		"0xe8e33700": "addLiquidity",
		"0xf305d719": "addLiquidityETH",
		"0xbaa2abde": "removeLiquidity",
		"0x02751cec": "removeLiquidityETH",
		"0xc45a0155": "factory",
		"0x0902f1ac": "getReserves",
		"0x022c0d9f": "swap",
		"0x6a627842": "mint",
		"0x89afcb44": "burn",
		"0x128acb08": "swap", // V3
		"0x3850c7bd": "slot0",
		"0x1a686502": "collect",
		"0x3c8a7d8d": "mint", // V3
	}

	if name, ok := methodNames[methodID]; ok {
		decoded.Method = name
	}

	return decoded
}

// isTokenTransfer checks if input is a token transfer
func isTokenTransfer(input string) bool {
	if len(input) < 10 {
		return false
	}
	methodID := input[:10]
	tokenMethods := map[string]bool{
		"0xa9059cbb": true, // transfer
		"0x23b872dd": true, // transferFrom
	}
	return tokenMethods[methodID]
}

// Helper functions for HTTP requests (to avoid import cycles)
func newHTTPRequest(ctx context.Context, method, url string, body []byte) (*httpRequest, error) {
	return &httpRequest{ctx: ctx, method: method, url: url, body: body}, nil
}

type httpRequest struct {
	ctx    context.Context
	method string
	url    string
	body   []byte
}

func doHTTPRequest(req *httpRequest) (*httpResponse, error) {
	// This is a placeholder - in production, use actual http.Client
	return &httpResponse{}, fmt.Errorf("not implemented - use actual HTTP client")
}

type httpResponse struct {
	Body readCloser
}

type readCloser interface {
	Read(p []byte) (n int, err error)
	Close() error
}
