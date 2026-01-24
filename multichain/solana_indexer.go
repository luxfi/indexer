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

// SolanaIndexer indexes Solana blockchain
type SolanaIndexer struct {
	mu sync.RWMutex

	config ChainConfig
	db     Database
	client *http.Client

	// State
	running      int32
	indexedSlot  uint64
	latestSlot   uint64

	// Protocol indexers
	protocols map[ProtocolType]SolanaProtocolIndexer

	// Stats
	stats *IndexerStats

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// SolanaProtocolIndexer handles Solana protocol-specific indexing
type SolanaProtocolIndexer interface {
	Name() string
	ProgramIDs() []string
	IndexTransaction(ctx context.Context, tx *SolanaTransaction) error
}

// SolanaBlock represents a Solana block (slot)
type SolanaBlock struct {
	Slot              uint64               `json:"slot"`
	Blockhash         string               `json:"blockhash"`
	PreviousBlockhash string               `json:"previousBlockhash"`
	ParentSlot        uint64               `json:"parentSlot"`
	BlockTime         int64                `json:"blockTime"`
	Transactions      []SolanaTransaction  `json:"transactions"`
	Rewards           []SolanaReward       `json:"rewards"`
}

// SolanaTransaction represents a Solana transaction
type SolanaTransaction struct {
	Signature        string                   `json:"signature"`
	Slot             uint64                   `json:"slot"`
	BlockTime        int64                    `json:"blockTime"`
	Fee              uint64                   `json:"fee"`
	Status           string                   `json:"status"` // "success" or "failed"
	Err              interface{}              `json:"err"`
	Message          SolanaMessage            `json:"message"`
	Meta             SolanaMeta               `json:"meta"`
	InnerInstructions []SolanaInnerInstruction `json:"innerInstructions"`
}

// SolanaMessage represents transaction message
type SolanaMessage struct {
	AccountKeys     []string              `json:"accountKeys"`
	Header          SolanaHeader          `json:"header"`
	Instructions    []SolanaInstruction   `json:"instructions"`
	RecentBlockhash string                `json:"recentBlockhash"`
}

// SolanaHeader represents message header
type SolanaHeader struct {
	NumRequiredSignatures       int `json:"numRequiredSignatures"`
	NumReadonlySignedAccounts   int `json:"numReadonlySignedAccounts"`
	NumReadonlyUnsignedAccounts int `json:"numReadonlyUnsignedAccounts"`
}

// SolanaInstruction represents a Solana instruction
type SolanaInstruction struct {
	ProgramIDIndex int    `json:"programIdIndex"`
	Accounts       []int  `json:"accounts"`
	Data           string `json:"data"` // Base58 encoded
}

// SolanaInnerInstruction represents inner instructions
type SolanaInnerInstruction struct {
	Index        int                   `json:"index"`
	Instructions []SolanaInstruction   `json:"instructions"`
}

// SolanaMeta represents transaction metadata
type SolanaMeta struct {
	Err               interface{}           `json:"err"`
	Fee               uint64                `json:"fee"`
	PreBalances       []uint64              `json:"preBalances"`
	PostBalances      []uint64              `json:"postBalances"`
	PreTokenBalances  []SolanaTokenBalance  `json:"preTokenBalances"`
	PostTokenBalances []SolanaTokenBalance  `json:"postTokenBalances"`
	LogMessages       []string              `json:"logMessages"`
	ComputeUnitsUsed  uint64                `json:"computeUnitsConsumed"`
}

// SolanaTokenBalance represents token balance change
type SolanaTokenBalance struct {
	AccountIndex  int                      `json:"accountIndex"`
	Mint          string                   `json:"mint"`
	Owner         string                   `json:"owner"`
	ProgramID     string                   `json:"programId"`
	UITokenAmount SolanaTokenAmount        `json:"uiTokenAmount"`
}

// SolanaTokenAmount represents token amount
type SolanaTokenAmount struct {
	Amount         string  `json:"amount"`
	Decimals       int     `json:"decimals"`
	UIAmount       float64 `json:"uiAmount"`
	UIAmountString string  `json:"uiAmountString"`
}

// SolanaReward represents block rewards
type SolanaReward struct {
	Pubkey      string `json:"pubkey"`
	Lamports    int64  `json:"lamports"`
	PostBalance uint64 `json:"postBalance"`
	RewardType  string `json:"rewardType"`
	Commission  *int   `json:"commission,omitempty"`
}

// NewSolanaIndexer creates a new Solana indexer
func NewSolanaIndexer(config ChainConfig, db Database) (*SolanaIndexer, error) {
	ctx, cancel := context.WithCancel(context.Background())

	idx := &SolanaIndexer{
		config:    config,
		db:        db,
		client:    &http.Client{Timeout: 30 * time.Second},
		protocols: make(map[ProtocolType]SolanaProtocolIndexer),
		ctx:       ctx,
		cancel:    cancel,
		stats: &IndexerStats{
			ChainID:   config.ID,
			ChainName: config.Name,
			StartTime: time.Now(),
		},
	}

	// Initialize protocol indexers
	for _, proto := range config.Protocols {
		if !proto.Enabled {
			continue
		}
		if err := idx.initProtocol(proto); err != nil {
			return nil, fmt.Errorf("failed to init protocol %s: %w", proto.Type, err)
		}
	}

	return idx, nil
}

// initProtocol initializes a Solana protocol indexer
func (s *SolanaIndexer) initProtocol(config ProtocolConfig) error {
	var indexer SolanaProtocolIndexer

	switch config.Type {
	case ProtocolRaydium:
		indexer = NewRaydiumIndexer(config)
	case ProtocolOrca:
		indexer = NewOrcaIndexer(config)
	case ProtocolMarinade:
		indexer = NewMarinadeIndexer(config)
	case ProtocolJito:
		indexer = NewJitoIndexer(config)
	case ProtocolJupiter:
		indexer = NewJupiterIndexer(config)
	case ProtocolDrift:
		indexer = NewDriftIndexer(config)
	case ProtocolMango:
		indexer = NewMangoIndexer(config)
	case ProtocolPhoenix:
		indexer = NewPhoenixIndexer(config)
	case ProtocolMetaplex:
		indexer = NewMetaplexIndexer(config)
	case ProtocolMagicEden:
		indexer = NewMagicEdenSolanaIndexer(config)
	case ProtocolTensor:
		indexer = NewTensorIndexer(config)
	default:
		indexer = NewGenericSolanaIndexer(config)
	}

	s.protocols[config.Type] = indexer
	return nil
}

// ChainID returns the chain identifier
func (s *SolanaIndexer) ChainID() string {
	return s.config.ID
}

// ChainType returns the chain type
func (s *SolanaIndexer) ChainType() ChainType {
	return ChainTypeSolana
}

// Start starts the indexer
func (s *SolanaIndexer) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return fmt.Errorf("indexer already running")
	}
	s.stats.IsRunning = true
	return nil
}

// Stop stops the indexer
func (s *SolanaIndexer) Stop() error {
	atomic.StoreInt32(&s.running, 0)
	s.cancel()
	s.stats.IsRunning = false
	return nil
}

// IsRunning returns whether the indexer is running
func (s *SolanaIndexer) IsRunning() bool {
	return atomic.LoadInt32(&s.running) == 1
}

// GetLatestBlock returns the latest slot
func (s *SolanaIndexer) GetLatestBlock(ctx context.Context) (uint64, error) {
	result, err := s.rpcCall(ctx, "getSlot", []interface{}{})
	if err != nil {
		return 0, err
	}

	slot, ok := result.(float64)
	if !ok {
		return 0, fmt.Errorf("invalid slot response")
	}

	s.mu.Lock()
	s.latestSlot = uint64(slot)
	s.mu.Unlock()

	return uint64(slot), nil
}

// GetIndexedBlock returns the last indexed slot
func (s *SolanaIndexer) GetIndexedBlock() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.indexedSlot
}

// IndexBlock indexes a single slot
func (s *SolanaIndexer) IndexBlock(ctx context.Context, slot uint64) error {
	block, err := s.fetchBlock(ctx, slot)
	if err != nil {
		return fmt.Errorf("failed to fetch slot %d: %w", slot, err)
	}

	// Process transactions
	for _, tx := range block.Transactions {
		s.processTransaction(ctx, &tx)
	}

	s.mu.Lock()
	s.indexedSlot = slot
	s.stats.BlocksProcessed++
	s.stats.TxsProcessed += uint64(len(block.Transactions))
	s.stats.LastBlockTime = time.Now()
	s.mu.Unlock()

	return nil
}

// IndexBlockRange indexes a range of slots
func (s *SolanaIndexer) IndexBlockRange(ctx context.Context, from, to uint64) error {
	for slot := from; slot <= to; slot++ {
		if err := s.IndexBlock(ctx, slot); err != nil {
			s.stats.ErrorCount++
			s.stats.LastError = err.Error()
			continue
		}
	}
	return nil
}

// fetchBlock fetches a block from RPC
func (s *SolanaIndexer) fetchBlock(ctx context.Context, slot uint64) (*SolanaBlock, error) {
	params := []interface{}{
		slot,
		map[string]interface{}{
			"encoding":                       "json",
			"transactionDetails":             "full",
			"rewards":                        true,
			"maxSupportedTransactionVersion": 0,
		},
	}

	result, err := s.rpcCall(ctx, "getBlock", params)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}

	var block SolanaBlock
	if err := json.Unmarshal(data, &block); err != nil {
		return nil, err
	}

	block.Slot = slot
	return &block, nil
}

// processTransaction routes a transaction to protocol indexers
func (s *SolanaIndexer) processTransaction(ctx context.Context, tx *SolanaTransaction) {
	for _, indexer := range s.protocols {
		indexer.IndexTransaction(ctx, tx)
	}
}

// rpcCall makes a JSON-RPC call to Solana
func (s *SolanaIndexer) rpcCall(ctx context.Context, method string, params []interface{}) (interface{}, error) {
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.config.RPC, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(jsonReader(data))

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Result interface{} `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if result.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", result.Error.Code, result.Error.Message)
	}

	return result.Result, nil
}

// Stats returns indexer statistics
func (s *SolanaIndexer) Stats() *IndexerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &IndexerStats{
		ChainID:         s.stats.ChainID,
		ChainName:       s.stats.ChainName,
		IsRunning:       s.stats.IsRunning,
		LatestBlock:     s.latestSlot,
		IndexedBlock:    s.indexedSlot,
		BlocksBehind:    s.latestSlot - s.indexedSlot,
		BlocksProcessed: s.stats.BlocksProcessed,
		TxsProcessed:    s.stats.TxsProcessed,
		EventsProcessed: s.stats.EventsProcessed,
		ErrorCount:      s.stats.ErrorCount,
		LastError:       s.stats.LastError,
		LastBlockTime:   s.stats.LastBlockTime,
		StartTime:       s.stats.StartTime,
		Uptime:          time.Since(s.stats.StartTime),
	}
}

// =============================================================================
// Solana Protocol Indexer Stubs
// =============================================================================

type GenericSolanaIndexer struct{ config ProtocolConfig }
func NewGenericSolanaIndexer(config ProtocolConfig) *GenericSolanaIndexer { return &GenericSolanaIndexer{config: config} }
func (g *GenericSolanaIndexer) Name() string { return "generic_solana" }
func (g *GenericSolanaIndexer) ProgramIDs() []string { return nil }
func (g *GenericSolanaIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Raydium AMM (largest Solana DEX)
type RaydiumIndexer struct{ config ProtocolConfig }
func NewRaydiumIndexer(config ProtocolConfig) *RaydiumIndexer { return &RaydiumIndexer{config: config} }
func (r *RaydiumIndexer) Name() string { return "raydium" }
func (r *RaydiumIndexer) ProgramIDs() []string {
	return []string{
		"675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8", // AMM V4
		"CAMMCzo5YL8w4VFF8KVHrK22GGUsp5VTaW7grrKgrWqK", // CLMM
		"routeUGWgWzqBWFcrCfv8tritsqukccJPu3q5GPP3xS",  // Router
	}
}
func (r *RaydiumIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Orca AMM
type OrcaIndexer struct{ config ProtocolConfig }
func NewOrcaIndexer(config ProtocolConfig) *OrcaIndexer { return &OrcaIndexer{config: config} }
func (o *OrcaIndexer) Name() string { return "orca" }
func (o *OrcaIndexer) ProgramIDs() []string {
	return []string{
		"whirLbMiicVdio4qvUfM5KAg6Ct8VwpYzGff3uctyCc",  // Whirlpool
		"9W959DqEETiGZocYWCQPaJ6sBmUzgfxXfqGeTEdp3aQP", // Legacy
	}
}
func (o *OrcaIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Marinade Liquid Staking
type MarinadeIndexer struct{ config ProtocolConfig }
func NewMarinadeIndexer(config ProtocolConfig) *MarinadeIndexer { return &MarinadeIndexer{config: config} }
func (m *MarinadeIndexer) Name() string { return "marinade" }
func (m *MarinadeIndexer) ProgramIDs() []string {
	return []string{
		"MarBmsSgKXdrN1egZf5sqe1TMai9K1rChYNDJgjq7aD", // Marinade Finance
	}
}
func (m *MarinadeIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Jito MEV/Staking
type JitoIndexer struct{ config ProtocolConfig }
func NewJitoIndexer(config ProtocolConfig) *JitoIndexer { return &JitoIndexer{config: config} }
func (j *JitoIndexer) Name() string { return "jito" }
func (j *JitoIndexer) ProgramIDs() []string {
	return []string{
		"J1toso1uCk3RLmjorhTtrVwY9HJ7X8V9yYac6Y7kGCPn", // JitoSOL
		"Jito4APyf642JPZPx3hGc6WWJ8zPKtRbRs4P815Awbb",  // Tip Payment
	}
}
func (j *JitoIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Jupiter Aggregator
type JupiterIndexer struct{ config ProtocolConfig }
func NewJupiterIndexer(config ProtocolConfig) *JupiterIndexer { return &JupiterIndexer{config: config} }
func (j *JupiterIndexer) Name() string { return "jupiter" }
func (j *JupiterIndexer) ProgramIDs() []string {
	return []string{
		"JUP6LkbZbjS1jKKwapdHNy74zcZ3tLUZoi5QNyVTaV4",  // Jupiter V6
		"JUP4Fb2cqiRUcaTHdrPC8h2gNsA2ETXiPDD33WcGuJB",  // Jupiter V4
	}
}
func (j *JupiterIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Drift Perpetuals
type DriftIndexer struct{ config ProtocolConfig }
func NewDriftIndexer(config ProtocolConfig) *DriftIndexer { return &DriftIndexer{config: config} }
func (d *DriftIndexer) Name() string { return "drift" }
func (d *DriftIndexer) ProgramIDs() []string {
	return []string{
		"dRiftyHA39MWEi3m9aunc5MzRF1JYuBsbn6VPcn33UH", // Drift V2
	}
}
func (d *DriftIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Mango Markets
type MangoIndexer struct{ config ProtocolConfig }
func NewMangoIndexer(config ProtocolConfig) *MangoIndexer { return &MangoIndexer{config: config} }
func (m *MangoIndexer) Name() string { return "mango" }
func (m *MangoIndexer) ProgramIDs() []string {
	return []string{
		"4MangoMjqJ2firMokCjjGgoK8d4MXcrgL7XJaL3w6fVg", // Mango V4
	}
}
func (m *MangoIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Phoenix DEX (CLOB)
type PhoenixIndexer struct{ config ProtocolConfig }
func NewPhoenixIndexer(config ProtocolConfig) *PhoenixIndexer { return &PhoenixIndexer{config: config} }
func (p *PhoenixIndexer) Name() string { return "phoenix" }
func (p *PhoenixIndexer) ProgramIDs() []string {
	return []string{
		"PhoeNiXZ8ByJGLkxNfZRnkUfjvmuYqLR89jjFHGqdXY", // Phoenix V1
	}
}
func (p *PhoenixIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Metaplex NFT
type MetaplexIndexer struct{ config ProtocolConfig }
func NewMetaplexIndexer(config ProtocolConfig) *MetaplexIndexer { return &MetaplexIndexer{config: config} }
func (m *MetaplexIndexer) Name() string { return "metaplex" }
func (m *MetaplexIndexer) ProgramIDs() []string {
	return []string{
		"metaqbxxUerdq28cj1RbAWkYQm3ybzjb6a8bt518x1s",  // Token Metadata
		"p1exdMJcjVao65QdewkaZRUnU6VPSXhus9n2GzWfh98",  // Auction House
		"CoREENxT6tW1HoK8ypY1SxRMZTcVPm7R94rH4PZNhX7d", // Core
		"BGUMAp9Gq7iTEuizy4pqaxsTyUCBK68MDfK752saRPUY", // Bubblegum (cNFTs)
	}
}
func (m *MetaplexIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Magic Eden Solana
type MagicEdenSolanaIndexer struct{ config ProtocolConfig }
func NewMagicEdenSolanaIndexer(config ProtocolConfig) *MagicEdenSolanaIndexer { return &MagicEdenSolanaIndexer{config: config} }
func (m *MagicEdenSolanaIndexer) Name() string { return "magic_eden_solana" }
func (m *MagicEdenSolanaIndexer) ProgramIDs() []string {
	return []string{
		"M2mx93ekt1fmXSVkTrUL9xVFHkmME8HTUi5Cyc5aF7K",  // Magic Eden V2
		"MEisE1HzehtrDpAAT8PnLHjpSSkRYakotTuJRPjTpo8",  // Magic Eden V1
	}
}
func (m *MagicEdenSolanaIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Tensor NFT Marketplace
type TensorIndexer struct{ config ProtocolConfig }
func NewTensorIndexer(config ProtocolConfig) *TensorIndexer { return &TensorIndexer{config: config} }
func (t *TensorIndexer) Name() string { return "tensor" }
func (t *TensorIndexer) ProgramIDs() []string {
	return []string{
		"TSWAPaqyCSx2KABk68Shruf4rp7CxcNi8hAsbdwmHbN",  // Tensor Swap
		"TCMPhJdwDryooaGtiocG1u3xcYbRpiJzb283XfCZsDp",  // Tensor cNFT
		"TBIDxNsM9DuLs4YCbmA7VuACbMZ5WyYv5JGQxpqLMVJ",  // Tensor Bid
	}
}
func (t *TensorIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }
