// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package multichain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// =============================================================================
// Move Indexer (Aptos, Sui)
// =============================================================================

// MoveIndexer indexes Move-based chains (Aptos, Sui)
type MoveIndexer struct {
	mu sync.RWMutex

	config  ChainConfig
	db      Database
	client  *http.Client
	variant string // "aptos" or "sui"

	running      int32
	indexedBlock uint64
	latestBlock  uint64

	stats  *IndexerStats
	ctx    context.Context
	cancel context.CancelFunc
}

// NewMoveIndexer creates a new Move chain indexer
func NewMoveIndexer(config ChainConfig, db Database) (*MoveIndexer, error) {
	ctx, cancel := context.WithCancel(context.Background())

	variant := "aptos"
	if config.ID == "sui" || config.ID == "sui_mainnet" {
		variant = "sui"
	}

	return &MoveIndexer{
		config:  config,
		db:      db,
		client:  &http.Client{Timeout: 30 * time.Second},
		variant: variant,
		ctx:     ctx,
		cancel:  cancel,
		stats: &IndexerStats{
			ChainID:   config.ID,
			ChainName: config.Name,
			StartTime: time.Now(),
		},
	}, nil
}

func (m *MoveIndexer) ChainID() string    { return m.config.ID }
func (m *MoveIndexer) ChainType() ChainType { return ChainTypeMove }

func (m *MoveIndexer) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&m.running, 0, 1) {
		return fmt.Errorf("indexer already running")
	}
	m.stats.IsRunning = true
	return nil
}

func (m *MoveIndexer) Stop() error {
	atomic.StoreInt32(&m.running, 0)
	m.cancel()
	m.stats.IsRunning = false
	return nil
}

func (m *MoveIndexer) IsRunning() bool {
	return atomic.LoadInt32(&m.running) == 1
}

func (m *MoveIndexer) GetLatestBlock(ctx context.Context) (uint64, error) {
	var height uint64
	var err error

	if m.variant == "aptos" {
		height, err = m.getAptosLatestBlock(ctx)
	} else {
		height, err = m.getSuiLatestCheckpoint(ctx)
	}

	if err == nil {
		m.mu.Lock()
		m.latestBlock = height
		m.mu.Unlock()
	}
	return height, err
}

func (m *MoveIndexer) getAptosLatestBlock(ctx context.Context) (uint64, error) {
	url := fmt.Sprintf("%s/v1", m.config.RPC)
	resp, err := m.httpGet(ctx, url)
	if err != nil {
		return 0, err
	}

	var result struct {
		BlockHeight string `json:"block_height"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return 0, err
	}

	var height uint64
	fmt.Sscanf(result.BlockHeight, "%d", &height)
	return height, nil
}

func (m *MoveIndexer) getSuiLatestCheckpoint(ctx context.Context) (uint64, error) {
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "sui_getLatestCheckpointSequenceNumber",
		"params":  []interface{}{},
	}

	data, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", m.config.RPC, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(jsonReader(data))

	resp, err := m.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Result string `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	var checkpoint uint64
	fmt.Sscanf(result.Result, "%d", &checkpoint)
	return checkpoint, nil
}

func (m *MoveIndexer) GetIndexedBlock() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.indexedBlock
}

func (m *MoveIndexer) IndexBlock(ctx context.Context, height uint64) error {
	// Implement Aptos/Sui block indexing
	m.mu.Lock()
	m.indexedBlock = height
	m.stats.BlocksProcessed++
	m.stats.LastBlockTime = time.Now()
	m.mu.Unlock()
	return nil
}

func (m *MoveIndexer) IndexBlockRange(ctx context.Context, from, to uint64) error {
	for height := from; height <= to; height++ {
		if err := m.IndexBlock(ctx, height); err != nil {
			m.stats.ErrorCount++
			continue
		}
	}
	return nil
}

func (m *MoveIndexer) Stats() *IndexerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return &IndexerStats{
		ChainID:         m.stats.ChainID,
		ChainName:       m.stats.ChainName,
		IsRunning:       m.stats.IsRunning,
		LatestBlock:     m.latestBlock,
		IndexedBlock:    m.indexedBlock,
		BlocksBehind:    m.latestBlock - m.indexedBlock,
		BlocksProcessed: m.stats.BlocksProcessed,
		TxsProcessed:    m.stats.TxsProcessed,
		EventsProcessed: m.stats.EventsProcessed,
		ErrorCount:      m.stats.ErrorCount,
		LastError:       m.stats.LastError,
		LastBlockTime:   m.stats.LastBlockTime,
		StartTime:       m.stats.StartTime,
		Uptime:          time.Since(m.stats.StartTime),
	}
}

func (m *MoveIndexer) httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// =============================================================================
// NEAR Indexer
// =============================================================================

type NearIndexer struct {
	mu sync.RWMutex

	config  ChainConfig
	db      Database
	client  *http.Client

	running      int32
	indexedBlock uint64
	latestBlock  uint64

	stats  *IndexerStats
	ctx    context.Context
	cancel context.CancelFunc
}

func NewNearIndexer(config ChainConfig, db Database) (*NearIndexer, error) {
	ctx, cancel := context.WithCancel(context.Background())
	return &NearIndexer{
		config: config,
		db:     db,
		client: &http.Client{Timeout: 30 * time.Second},
		ctx:    ctx,
		cancel: cancel,
		stats: &IndexerStats{
			ChainID:   config.ID,
			ChainName: config.Name,
			StartTime: time.Now(),
		},
	}, nil
}

func (n *NearIndexer) ChainID() string    { return n.config.ID }
func (n *NearIndexer) ChainType() ChainType { return ChainTypeNear }

func (n *NearIndexer) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&n.running, 0, 1) {
		return fmt.Errorf("indexer already running")
	}
	n.stats.IsRunning = true
	return nil
}

func (n *NearIndexer) Stop() error {
	atomic.StoreInt32(&n.running, 0)
	n.cancel()
	n.stats.IsRunning = false
	return nil
}

func (n *NearIndexer) IsRunning() bool { return atomic.LoadInt32(&n.running) == 1 }

func (n *NearIndexer) GetLatestBlock(ctx context.Context) (uint64, error) {
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "dontcare",
		"method":  "status",
		"params":  []interface{}{},
	}

	data, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", n.config.RPC, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(jsonReader(data))

	resp, err := n.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Result struct {
			SyncInfo struct {
				LatestBlockHeight uint64 `json:"latest_block_height"`
			} `json:"sync_info"`
		} `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	height := result.Result.SyncInfo.LatestBlockHeight
	n.mu.Lock()
	n.latestBlock = height
	n.mu.Unlock()
	return height, nil
}

func (n *NearIndexer) GetIndexedBlock() uint64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.indexedBlock
}

func (n *NearIndexer) IndexBlock(ctx context.Context, height uint64) error {
	n.mu.Lock()
	n.indexedBlock = height
	n.stats.BlocksProcessed++
	n.stats.LastBlockTime = time.Now()
	n.mu.Unlock()
	return nil
}

func (n *NearIndexer) IndexBlockRange(ctx context.Context, from, to uint64) error {
	for height := from; height <= to; height++ {
		if err := n.IndexBlock(ctx, height); err != nil {
			continue
		}
	}
	return nil
}

func (n *NearIndexer) Stats() *IndexerStats {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return &IndexerStats{
		ChainID:         n.stats.ChainID,
		ChainName:       n.stats.ChainName,
		IsRunning:       n.stats.IsRunning,
		LatestBlock:     n.latestBlock,
		IndexedBlock:    n.indexedBlock,
		BlocksBehind:    n.latestBlock - n.indexedBlock,
		BlocksProcessed: n.stats.BlocksProcessed,
		TxsProcessed:    n.stats.TxsProcessed,
		StartTime:       n.stats.StartTime,
		Uptime:          time.Since(n.stats.StartTime),
	}
}

// =============================================================================
// Tron Indexer
// =============================================================================

type TronIndexer struct {
	mu sync.RWMutex

	config  ChainConfig
	db      Database
	client  *http.Client

	running      int32
	indexedBlock uint64
	latestBlock  uint64

	stats  *IndexerStats
	ctx    context.Context
	cancel context.CancelFunc
}

func NewTronIndexer(config ChainConfig, db Database) (*TronIndexer, error) {
	ctx, cancel := context.WithCancel(context.Background())
	return &TronIndexer{
		config: config,
		db:     db,
		client: &http.Client{Timeout: 30 * time.Second},
		ctx:    ctx,
		cancel: cancel,
		stats: &IndexerStats{
			ChainID:   config.ID,
			ChainName: config.Name,
			StartTime: time.Now(),
		},
	}, nil
}

func (t *TronIndexer) ChainID() string    { return t.config.ID }
func (t *TronIndexer) ChainType() ChainType { return ChainTypeTron }

func (t *TronIndexer) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&t.running, 0, 1) {
		return fmt.Errorf("indexer already running")
	}
	t.stats.IsRunning = true
	return nil
}

func (t *TronIndexer) Stop() error {
	atomic.StoreInt32(&t.running, 0)
	t.cancel()
	t.stats.IsRunning = false
	return nil
}

func (t *TronIndexer) IsRunning() bool { return atomic.LoadInt32(&t.running) == 1 }

func (t *TronIndexer) GetLatestBlock(ctx context.Context) (uint64, error) {
	url := fmt.Sprintf("%s/wallet/getnowblock", t.config.RPC)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, nil)
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		BlockHeader struct {
			RawData struct {
				Number uint64 `json:"number"`
			} `json:"raw_data"`
		} `json:"block_header"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	height := result.BlockHeader.RawData.Number
	t.mu.Lock()
	t.latestBlock = height
	t.mu.Unlock()
	return height, nil
}

func (t *TronIndexer) GetIndexedBlock() uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.indexedBlock
}

func (t *TronIndexer) IndexBlock(ctx context.Context, height uint64) error {
	t.mu.Lock()
	t.indexedBlock = height
	t.stats.BlocksProcessed++
	t.stats.LastBlockTime = time.Now()
	t.mu.Unlock()
	return nil
}

func (t *TronIndexer) IndexBlockRange(ctx context.Context, from, to uint64) error {
	for height := from; height <= to; height++ {
		if err := t.IndexBlock(ctx, height); err != nil {
			continue
		}
	}
	return nil
}

func (t *TronIndexer) Stats() *IndexerStats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return &IndexerStats{
		ChainID:         t.stats.ChainID,
		ChainName:       t.stats.ChainName,
		IsRunning:       t.stats.IsRunning,
		LatestBlock:     t.latestBlock,
		IndexedBlock:    t.indexedBlock,
		BlocksBehind:    t.latestBlock - t.indexedBlock,
		BlocksProcessed: t.stats.BlocksProcessed,
		TxsProcessed:    t.stats.TxsProcessed,
		StartTime:       t.stats.StartTime,
		Uptime:          time.Since(t.stats.StartTime),
	}
}

// =============================================================================
// TON Indexer - See ton_indexer.go for full implementation
// =============================================================================

// =============================================================================
// Substrate Indexer (Polkadot, Kusama)
// =============================================================================

type SubstrateIndexer struct {
	mu sync.RWMutex

	config  ChainConfig
	db      Database
	client  *http.Client

	running      int32
	indexedBlock uint64
	latestBlock  uint64

	stats  *IndexerStats
	ctx    context.Context
	cancel context.CancelFunc
}

func NewSubstrateIndexer(config ChainConfig, db Database) (*SubstrateIndexer, error) {
	ctx, cancel := context.WithCancel(context.Background())
	return &SubstrateIndexer{
		config: config,
		db:     db,
		client: &http.Client{Timeout: 30 * time.Second},
		ctx:    ctx,
		cancel: cancel,
		stats: &IndexerStats{
			ChainID:   config.ID,
			ChainName: config.Name,
			StartTime: time.Now(),
		},
	}, nil
}

func (s *SubstrateIndexer) ChainID() string    { return s.config.ID }
func (s *SubstrateIndexer) ChainType() ChainType { return ChainTypeSubstrate }

func (s *SubstrateIndexer) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return fmt.Errorf("indexer already running")
	}
	s.stats.IsRunning = true
	return nil
}

func (s *SubstrateIndexer) Stop() error {
	atomic.StoreInt32(&s.running, 0)
	s.cancel()
	s.stats.IsRunning = false
	return nil
}

func (s *SubstrateIndexer) IsRunning() bool { return atomic.LoadInt32(&s.running) == 1 }

func (s *SubstrateIndexer) GetLatestBlock(ctx context.Context) (uint64, error) {
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "chain_getHeader",
		"params":  []interface{}{},
	}

	data, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.config.RPC, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(jsonReader(data))

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Result struct {
			Number string `json:"number"`
		} `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	var height uint64
	fmt.Sscanf(result.Result.Number, "0x%x", &height)

	s.mu.Lock()
	s.latestBlock = height
	s.mu.Unlock()
	return height, nil
}

func (s *SubstrateIndexer) GetIndexedBlock() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.indexedBlock
}

func (s *SubstrateIndexer) IndexBlock(ctx context.Context, height uint64) error {
	s.mu.Lock()
	s.indexedBlock = height
	s.stats.BlocksProcessed++
	s.stats.LastBlockTime = time.Now()
	s.mu.Unlock()
	return nil
}

func (s *SubstrateIndexer) IndexBlockRange(ctx context.Context, from, to uint64) error {
	for height := from; height <= to; height++ {
		if err := s.IndexBlock(ctx, height); err != nil {
			continue
		}
	}
	return nil
}

func (s *SubstrateIndexer) Stats() *IndexerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &IndexerStats{
		ChainID:         s.stats.ChainID,
		ChainName:       s.stats.ChainName,
		IsRunning:       s.stats.IsRunning,
		LatestBlock:     s.latestBlock,
		IndexedBlock:    s.indexedBlock,
		BlocksBehind:    s.latestBlock - s.indexedBlock,
		BlocksProcessed: s.stats.BlocksProcessed,
		TxsProcessed:    s.stats.TxsProcessed,
		StartTime:       s.stats.StartTime,
		Uptime:          time.Since(s.stats.StartTime),
	}
}
