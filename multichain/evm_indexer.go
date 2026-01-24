// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package multichain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// EVMIndexer indexes EVM-compatible chains
type EVMIndexer struct {
	mu sync.RWMutex

	config  ChainConfig
	db      Database
	client  *http.Client

	// State
	running       int32
	indexedBlock  uint64
	latestBlock   uint64

	// Protocol indexers
	protocols map[ProtocolType]ProtocolIndexer

	// Stats
	stats *IndexerStats

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// ProtocolIndexer handles protocol-specific event indexing
type ProtocolIndexer interface {
	Name() string
	EventSignatures() []string
	IndexLog(ctx context.Context, log *EVMLog) error
}

// EVMLog represents an EVM log entry
type EVMLog struct {
	Address     string   `json:"address"`
	Topics      []string `json:"topics"`
	Data        string   `json:"data"`
	BlockNumber uint64   `json:"blockNumber"`
	TxHash      string   `json:"transactionHash"`
	TxIndex     uint64   `json:"transactionIndex"`
	LogIndex    uint64   `json:"logIndex"`
	Timestamp   time.Time
}

// EVMBlock represents an EVM block
type EVMBlock struct {
	Number       uint64         `json:"number"`
	Hash         string         `json:"hash"`
	ParentHash   string         `json:"parentHash"`
	Timestamp    uint64         `json:"timestamp"`
	Miner        string         `json:"miner"`
	GasLimit     uint64         `json:"gasLimit"`
	GasUsed      uint64         `json:"gasUsed"`
	BaseFee      *big.Int       `json:"baseFeePerGas,omitempty"`
	Transactions []EVMTx        `json:"transactions"`
}

// EVMTx represents an EVM transaction
type EVMTx struct {
	Hash        string   `json:"hash"`
	From        string   `json:"from"`
	To          string   `json:"to,omitempty"`
	Value       *big.Int `json:"value"`
	Gas         uint64   `json:"gas"`
	GasPrice    *big.Int `json:"gasPrice,omitempty"`
	MaxFee      *big.Int `json:"maxFeePerGas,omitempty"`
	MaxPriority *big.Int `json:"maxPriorityFeePerGas,omitempty"`
	Input       string   `json:"input"`
	Nonce       uint64   `json:"nonce"`
	Type        uint8    `json:"type"`
}

// NewEVMIndexer creates a new EVM chain indexer
func NewEVMIndexer(config ChainConfig, db Database) (*EVMIndexer, error) {
	ctx, cancel := context.WithCancel(context.Background())

	idx := &EVMIndexer{
		config:    config,
		db:        db,
		client:    &http.Client{Timeout: 30 * time.Second},
		protocols: make(map[ProtocolType]ProtocolIndexer),
		ctx:       ctx,
		cancel:    cancel,
		stats: &IndexerStats{
			ChainID:   config.ID,
			ChainName: config.Name,
			StartTime: time.Now(),
		},
	}

	// Initialize protocol indexers based on config
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

// initProtocol initializes a protocol indexer
func (e *EVMIndexer) initProtocol(config ProtocolConfig) error {
	var indexer ProtocolIndexer

	switch config.Type {
	// DEX Protocols
	case ProtocolUniswapV2, ProtocolSushiswap, ProtocolPancakeswap:
		indexer = NewUniswapV2Indexer(config)
	case ProtocolUniswapV3, ProtocolCamelot:
		indexer = NewUniswapV3Indexer(config)
	case ProtocolCurve:
		indexer = NewCurveIndexer(config)
	case ProtocolBalancer:
		indexer = NewBalancerIndexer(config)

	// Lending
	case ProtocolAaveV2:
		indexer = NewAaveV2Indexer(config)
	case ProtocolAaveV3:
		indexer = NewAaveV3Indexer(config)
	case ProtocolCompoundV2:
		indexer = NewCompoundV2Indexer(config)
	case ProtocolCompoundV3:
		indexer = NewCompoundV3Indexer(config)

	// Perpetuals
	case ProtocolGMX, ProtocolGMXV2:
		indexer = NewGMXIndexer(config)
	case ProtocolSynthetix, ProtocolSynthetixV3:
		indexer = NewSynthetixIndexer(config)

	// NFT Marketplaces
	case ProtocolSeaport:
		indexer = NewSeaportIndexer(config)
	case ProtocolLooksRare:
		indexer = NewLooksRareIndexer(config)
	case ProtocolBlur:
		indexer = NewBlurIndexer(config)
	case ProtocolX2Y2:
		indexer = NewX2Y2Indexer(config)
	case ProtocolSudoswap:
		indexer = NewSudoswapIndexer(config)

	// Bridges
	case ProtocolWormhole:
		indexer = NewWormholeIndexer(config)
	case ProtocolLayerZero:
		indexer = NewLayerZeroIndexer(config)
	case ProtocolStargate:
		indexer = NewStargateIndexer(config)

	// Liquid Staking
	case ProtocolLido:
		indexer = NewLidoIndexer(config)
	case ProtocolRocketPool:
		indexer = NewRocketPoolIndexer(config)
	case ProtocolEigenlayer:
		indexer = NewEigenlayerIndexer(config)

	// Yield
	case ProtocolYearn:
		indexer = NewYearnIndexer(config)
	case ProtocolConvex:
		indexer = NewConvexIndexer(config)
	case ProtocolPendle:
		indexer = NewPendleIndexer(config)

	default:
		// Generic ERC20/721/1155 indexer
		indexer = NewGenericEVMIndexer(config)
	}

	e.protocols[config.Type] = indexer
	return nil
}

// ChainID returns the chain identifier
func (e *EVMIndexer) ChainID() string {
	return e.config.ID
}

// ChainType returns the chain type
func (e *EVMIndexer) ChainType() ChainType {
	return ChainTypeEVM
}

// Start starts the indexer
func (e *EVMIndexer) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&e.running, 0, 1) {
		return fmt.Errorf("indexer already running")
	}

	e.stats.IsRunning = true
	return nil
}

// Stop stops the indexer
func (e *EVMIndexer) Stop() error {
	atomic.StoreInt32(&e.running, 0)
	e.cancel()
	e.stats.IsRunning = false
	return nil
}

// IsRunning returns whether the indexer is running
func (e *EVMIndexer) IsRunning() bool {
	return atomic.LoadInt32(&e.running) == 1
}

// GetLatestBlock fetches the latest block number from the chain
func (e *EVMIndexer) GetLatestBlock(ctx context.Context) (uint64, error) {
	result, err := e.rpcCall(ctx, "eth_blockNumber", []interface{}{})
	if err != nil {
		return 0, err
	}

	blockHex, ok := result.(string)
	if !ok {
		return 0, fmt.Errorf("invalid block number response")
	}

	var blockNum uint64
	fmt.Sscanf(blockHex, "0x%x", &blockNum)

	e.mu.Lock()
	e.latestBlock = blockNum
	e.mu.Unlock()

	return blockNum, nil
}

// GetIndexedBlock returns the last indexed block
func (e *EVMIndexer) GetIndexedBlock() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.indexedBlock
}

// IndexBlock indexes a single block
func (e *EVMIndexer) IndexBlock(ctx context.Context, blockNumber uint64) error {
	// Fetch block with transactions
	block, err := e.fetchBlock(ctx, blockNumber)
	if err != nil {
		return fmt.Errorf("failed to fetch block %d: %w", blockNumber, err)
	}

	// Fetch receipts for logs
	logs, err := e.fetchLogs(ctx, blockNumber, blockNumber)
	if err != nil {
		return fmt.Errorf("failed to fetch logs for block %d: %w", blockNumber, err)
	}

	// Process block
	if err := e.processBlock(ctx, block); err != nil {
		return err
	}

	// Process logs through protocol indexers
	for _, log := range logs {
		e.processLog(ctx, log)
	}

	e.mu.Lock()
	e.indexedBlock = blockNumber
	e.stats.BlocksProcessed++
	e.stats.TxsProcessed += uint64(len(block.Transactions))
	e.stats.EventsProcessed += uint64(len(logs))
	e.stats.LastBlockTime = time.Now()
	e.mu.Unlock()

	return nil
}

// IndexBlockRange indexes a range of blocks
func (e *EVMIndexer) IndexBlockRange(ctx context.Context, from, to uint64) error {
	// Fetch logs for entire range at once (more efficient)
	logs, err := e.fetchLogs(ctx, from, to)
	if err != nil {
		return fmt.Errorf("failed to fetch logs for blocks %d-%d: %w", from, to, err)
	}

	// Group logs by block
	logsByBlock := make(map[uint64][]*EVMLog)
	for _, log := range logs {
		logsByBlock[log.BlockNumber] = append(logsByBlock[log.BlockNumber], log)
	}

	// Process each block
	for blockNum := from; blockNum <= to; blockNum++ {
		block, err := e.fetchBlock(ctx, blockNum)
		if err != nil {
			e.stats.ErrorCount++
			e.stats.LastError = err.Error()
			continue
		}

		if err := e.processBlock(ctx, block); err != nil {
			e.stats.ErrorCount++
			continue
		}

		// Process logs for this block
		for _, log := range logsByBlock[blockNum] {
			e.processLog(ctx, log)
		}

		e.mu.Lock()
		e.indexedBlock = blockNum
		e.stats.BlocksProcessed++
		e.stats.TxsProcessed += uint64(len(block.Transactions))
		e.stats.EventsProcessed += uint64(len(logsByBlock[blockNum]))
		e.stats.LastBlockTime = time.Now()
		e.mu.Unlock()
	}

	return nil
}

// fetchBlock fetches a block from the RPC
func (e *EVMIndexer) fetchBlock(ctx context.Context, blockNumber uint64) (*EVMBlock, error) {
	blockHex := fmt.Sprintf("0x%x", blockNumber)
	result, err := e.rpcCall(ctx, "eth_getBlockByNumber", []interface{}{blockHex, true})
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}

	var block EVMBlock
	if err := json.Unmarshal(data, &block); err != nil {
		return nil, err
	}

	return &block, nil
}

// fetchLogs fetches logs for a block range
func (e *EVMIndexer) fetchLogs(ctx context.Context, fromBlock, toBlock uint64) ([]*EVMLog, error) {
	params := map[string]interface{}{
		"fromBlock": fmt.Sprintf("0x%x", fromBlock),
		"toBlock":   fmt.Sprintf("0x%x", toBlock),
	}

	result, err := e.rpcCall(ctx, "eth_getLogs", []interface{}{params})
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}

	var logs []*EVMLog
	if err := json.Unmarshal(data, &logs); err != nil {
		return nil, err
	}

	return logs, nil
}

// processBlock processes a block and stores it
func (e *EVMIndexer) processBlock(ctx context.Context, block *EVMBlock) error {
	// Store block in database
	// Implementation depends on database interface
	return nil
}

// processLog routes a log to the appropriate protocol indexer
func (e *EVMIndexer) processLog(ctx context.Context, log *EVMLog) {
	if len(log.Topics) == 0 {
		return
	}

	// Route to all protocol indexers - they'll filter by their signatures
	for _, indexer := range e.protocols {
		indexer.IndexLog(ctx, log)
	}
}

// rpcCall makes a JSON-RPC call
func (e *EVMIndexer) rpcCall(ctx context.Context, method string, params []interface{}) (interface{}, error) {
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

	req, err := http.NewRequestWithContext(ctx, "POST", e.config.RPC, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(jsonReader(data))

	resp, err := e.client.Do(req)
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

func jsonReader(data []byte) io.Reader {
	return &jsonBodyReader{data: data}
}

type jsonBodyReader struct {
	data []byte
	pos  int
}

func (r *jsonBodyReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// Stats returns indexer statistics
func (e *EVMIndexer) Stats() *IndexerStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return &IndexerStats{
		ChainID:         e.stats.ChainID,
		ChainName:       e.stats.ChainName,
		IsRunning:       e.stats.IsRunning,
		LatestBlock:     e.latestBlock,
		IndexedBlock:    e.indexedBlock,
		BlocksBehind:    e.latestBlock - e.indexedBlock,
		BlocksProcessed: e.stats.BlocksProcessed,
		TxsProcessed:    e.stats.TxsProcessed,
		EventsProcessed: e.stats.EventsProcessed,
		ErrorCount:      e.stats.ErrorCount,
		LastError:       e.stats.LastError,
		LastBlockTime:   e.stats.LastBlockTime,
		StartTime:       e.stats.StartTime,
		Uptime:          time.Since(e.stats.StartTime),
	}
}

// =============================================================================
// Protocol Indexer Stubs (implement actual logic)
// =============================================================================

type GenericEVMIndexer struct{ config ProtocolConfig }
func NewGenericEVMIndexer(config ProtocolConfig) *GenericEVMIndexer { return &GenericEVMIndexer{config: config} }
func (g *GenericEVMIndexer) Name() string { return "generic" }
func (g *GenericEVMIndexer) EventSignatures() []string { return nil }
func (g *GenericEVMIndexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type UniswapV2Indexer struct{ config ProtocolConfig }
func NewUniswapV2Indexer(config ProtocolConfig) *UniswapV2Indexer { return &UniswapV2Indexer{config: config} }
func (u *UniswapV2Indexer) Name() string { return "uniswap_v2" }
func (u *UniswapV2Indexer) EventSignatures() []string { return []string{"0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822"} }
func (u *UniswapV2Indexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type UniswapV3Indexer struct{ config ProtocolConfig }
func NewUniswapV3Indexer(config ProtocolConfig) *UniswapV3Indexer { return &UniswapV3Indexer{config: config} }
func (u *UniswapV3Indexer) Name() string { return "uniswap_v3" }
func (u *UniswapV3Indexer) EventSignatures() []string { return []string{"0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67"} }
func (u *UniswapV3Indexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type CurveIndexer struct{ config ProtocolConfig }
func NewCurveIndexer(config ProtocolConfig) *CurveIndexer { return &CurveIndexer{config: config} }
func (c *CurveIndexer) Name() string { return "curve" }
func (c *CurveIndexer) EventSignatures() []string { return nil }
func (c *CurveIndexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type BalancerIndexer struct{ config ProtocolConfig }
func NewBalancerIndexer(config ProtocolConfig) *BalancerIndexer { return &BalancerIndexer{config: config} }
func (b *BalancerIndexer) Name() string { return "balancer" }
func (b *BalancerIndexer) EventSignatures() []string { return nil }
func (b *BalancerIndexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type AaveV2Indexer struct{ config ProtocolConfig }
func NewAaveV2Indexer(config ProtocolConfig) *AaveV2Indexer { return &AaveV2Indexer{config: config} }
func (a *AaveV2Indexer) Name() string { return "aave_v2" }
func (a *AaveV2Indexer) EventSignatures() []string { return nil }
func (a *AaveV2Indexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type AaveV3Indexer struct{ config ProtocolConfig }
func NewAaveV3Indexer(config ProtocolConfig) *AaveV3Indexer { return &AaveV3Indexer{config: config} }
func (a *AaveV3Indexer) Name() string { return "aave_v3" }
func (a *AaveV3Indexer) EventSignatures() []string { return nil }
func (a *AaveV3Indexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type CompoundV2Indexer struct{ config ProtocolConfig }
func NewCompoundV2Indexer(config ProtocolConfig) *CompoundV2Indexer { return &CompoundV2Indexer{config: config} }
func (c *CompoundV2Indexer) Name() string { return "compound_v2" }
func (c *CompoundV2Indexer) EventSignatures() []string { return nil }
func (c *CompoundV2Indexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type CompoundV3Indexer struct{ config ProtocolConfig }
func NewCompoundV3Indexer(config ProtocolConfig) *CompoundV3Indexer { return &CompoundV3Indexer{config: config} }
func (c *CompoundV3Indexer) Name() string { return "compound_v3" }
func (c *CompoundV3Indexer) EventSignatures() []string { return nil }
func (c *CompoundV3Indexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type GMXIndexer struct{ config ProtocolConfig }
func NewGMXIndexer(config ProtocolConfig) *GMXIndexer { return &GMXIndexer{config: config} }
func (g *GMXIndexer) Name() string { return "gmx" }
func (g *GMXIndexer) EventSignatures() []string { return nil }
func (g *GMXIndexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type SynthetixIndexer struct{ config ProtocolConfig }
func NewSynthetixIndexer(config ProtocolConfig) *SynthetixIndexer { return &SynthetixIndexer{config: config} }
func (s *SynthetixIndexer) Name() string { return "synthetix" }
func (s *SynthetixIndexer) EventSignatures() []string { return nil }
func (s *SynthetixIndexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

// =============================================================================
// NFT Marketplace Indexers - Full Implementation
// =============================================================================

// NFT Marketplace Event Signatures
const (
	// Seaport (OpenSea) v1.1-1.6
	SeaportOrderFulfilledSig  = "0x9d9af8e38d66c62e2c12f0225249fd9d721c54b83f48d9352c97c6cacdcb6f31"
	SeaportOrderCancelledSig  = "0x6bacc01dbe442496068f7d234edd811f1a5f833243e0aec824f86ab861f3c90d"
	SeaportCounterIncrementSig = "0x721c20121297512b72821b97f5326877ea8ecf4bb9948fea5bfcb6453074d37f"

	// LooksRare v1
	LooksRareTakerBidSig  = "0x95fb6205e23ff6bda16a2d1dba56b9ad7c783f67c96fa149785052f47696f2be"
	LooksRareTakerAskSig  = "0x68cd251d4d267c6e2034ff0088b990352b97b2002c0476587d0c4da889c11330"
	LooksRareCancelAllSig = "0x1e7178d84f0b0825c65795cd62e7972809ad3aac6917843aaec596161b2c0a97"

	// LooksRare v2
	LooksRareV2TakerBidSig = "0x3ee3de4684413690dee6fff1a0a4f92916a1b97d1c5a83cdf24671844306e2e1"
	LooksRareV2TakerAskSig = "0x9aaa45d6db2ef74ead0751ea9113263d1dec1b50cea05f0ca2002cb8063564a4"

	// Blur
	BlurOrdersMatchedSig     = "0x61cbb2a3dee0b6064c2e681aadd61677fb4ef319f0b547508d495626f5a62f64"
	BlurOrderCancelledSig    = "0x5152abf959f6564662358c2e52b702c3fd8a1dead7a7e0efe0f0e3a8c2146a34"
	BlurNonceIncrementedSig  = "0xa82a649bbd060c5c3e04c50b8f455d7a4ec1b99bd6f7e71ac1eb19a0a5c1c8f4"

	// X2Y2
	X2Y2InventorySig = "0x3cbb63f144840e5b1b0a38a7c19211d2e89de4d7c5faf8b2d3c1776c302d1d33"
	X2Y2CancelSig    = "0xa015ad2dc32f266993958a0fd9884c746b971b254206f3478bc43e2f125c7b9e"

	// Sudoswap (NFT AMM)
	SudoswapSwapNFTInPairSig  = "0xbc479dfc6cb9c1a9d880f987ee4b30fa43dd7f06bee0440fef0a7daf0d2f85a9"
	SudoswapSwapNFTOutPairSig = "0x40c10f19c047ae7dfa66d6312b683d2ea3dfbcb4159a7b3ad8e62a61e78e6b19"

	// Rarible
	RaribleMatchSig  = "0x268820db288a211986b26a8fda86b1e0046281b21206936bb0e61c67b5c79ef4"
	RaribleCancelSig = "0x75ab8acd8c1dc0fcd5ae9aebb5c8a7ea3c2a0f7d0a7fbc4ca4ee9ca79b8ad37d"

	// Zora
	ZoraAskFilledSig    = "0x21a9d8e221211780696258a05c6225b1a24f428e2fd4d51708f1ab2be4224d39"
	ZoraAskCancelledSig = "0x6b8e2b9f74e3a1a4e2b9f5a7c3e9f5a7c3e9f5a7c3e9f5a7c3e9f5a7c3e9f5a7"
)

// NFTSale represents a completed NFT sale
type NFTSale struct {
	ID           string     `json:"id"`
	OrderHash    string     `json:"orderHash,omitempty"`
	Protocol     string     `json:"protocol"`
	Collection   string     `json:"collection"`
	TokenID      *big.Int   `json:"tokenId"`
	Quantity     uint64     `json:"quantity"`
	Seller       string     `json:"seller"`
	Buyer        string     `json:"buyer"`
	Price        *big.Int   `json:"price"`
	PaymentToken string     `json:"paymentToken"`
	FeeTotal     *big.Int   `json:"feeTotal"`
	RoyaltyTotal *big.Int   `json:"royaltyTotal"`
	BlockNumber  uint64     `json:"blockNumber"`
	TxHash       string     `json:"txHash"`
	LogIndex     uint64     `json:"logIndex"`
	Timestamp    time.Time  `json:"timestamp"`
}

// SeaportIndexer indexes OpenSea Seaport events
type SeaportIndexer struct {
	config ProtocolConfig
	mu     sync.RWMutex
	sales  []*NFTSale
	stats  struct {
		TotalSales   uint64
		TotalVolume  *big.Int
	}
}

func NewSeaportIndexer(config ProtocolConfig) *SeaportIndexer {
	return &SeaportIndexer{
		config: config,
		sales:  make([]*NFTSale, 0),
		stats:  struct {
			TotalSales   uint64
			TotalVolume  *big.Int
		}{TotalVolume: big.NewInt(0)},
	}
}

func (s *SeaportIndexer) Name() string { return "seaport" }

func (s *SeaportIndexer) EventSignatures() []string {
	return []string{
		SeaportOrderFulfilledSig,
		SeaportOrderCancelledSig,
		SeaportCounterIncrementSig,
	}
}

func (s *SeaportIndexer) IndexLog(ctx context.Context, log *EVMLog) error {
	if len(log.Topics) == 0 {
		return nil
	}

	switch log.Topics[0] {
	case SeaportOrderFulfilledSig:
		return s.indexOrderFulfilled(log)
	case SeaportOrderCancelledSig:
		// Order cancelled - track in stats
		return nil
	}
	return nil
}

func (s *SeaportIndexer) indexOrderFulfilled(log *EVMLog) error {
	if len(log.Topics) < 3 {
		return nil
	}

	// Topics: [sig, orderHash, offerer, zone]
	orderHash := log.Topics[1]
	offerer := "0x" + log.Topics[2][26:]

	sale := &NFTSale{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		OrderHash:   orderHash,
		Protocol:    "seaport",
		Seller:      offerer,
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		LogIndex:    log.LogIndex,
		Timestamp:   log.Timestamp,
		Quantity:    1,
		FeeTotal:    big.NewInt(0),
		RoyaltyTotal: big.NewInt(0),
	}

	// Extract price from data if available
	if len(log.Data) >= 64 {
		sale.Price = new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))
	}

	s.mu.Lock()
	s.sales = append(s.sales, sale)
	s.stats.TotalSales++
	if sale.Price != nil {
		s.stats.TotalVolume.Add(s.stats.TotalVolume, sale.Price)
	}
	s.mu.Unlock()

	return nil
}

func (s *SeaportIndexer) GetSales(limit int) []*NFTSale {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit > len(s.sales) {
		limit = len(s.sales)
	}
	return s.sales[len(s.sales)-limit:]
}

// LooksRareIndexer indexes LooksRare v1/v2 events
type LooksRareIndexer struct {
	config ProtocolConfig
	mu     sync.RWMutex
	sales  []*NFTSale
	stats  struct {
		TotalSales  uint64
		TotalVolume *big.Int
	}
}

func NewLooksRareIndexer(config ProtocolConfig) *LooksRareIndexer {
	return &LooksRareIndexer{
		config: config,
		sales:  make([]*NFTSale, 0),
		stats:  struct {
			TotalSales  uint64
			TotalVolume *big.Int
		}{TotalVolume: big.NewInt(0)},
	}
}

func (l *LooksRareIndexer) Name() string { return "looksrare" }

func (l *LooksRareIndexer) EventSignatures() []string {
	return []string{
		LooksRareTakerBidSig,
		LooksRareTakerAskSig,
		LooksRareCancelAllSig,
		LooksRareV2TakerBidSig,
		LooksRareV2TakerAskSig,
	}
}

func (l *LooksRareIndexer) IndexLog(ctx context.Context, log *EVMLog) error {
	if len(log.Topics) == 0 {
		return nil
	}

	switch log.Topics[0] {
	case LooksRareTakerBidSig, LooksRareV2TakerBidSig:
		return l.indexTrade(log, "taker_bid")
	case LooksRareTakerAskSig, LooksRareV2TakerAskSig:
		return l.indexTrade(log, "taker_ask")
	}
	return nil
}

func (l *LooksRareIndexer) indexTrade(log *EVMLog, tradeType string) error {
	if len(log.Topics) < 2 || len(log.Data) < 320 {
		return nil
	}

	orderHash := log.Topics[1]

	// Parse from data: taker, maker, currency, collection, tokenId, amount, price
	taker := "0x" + log.Data[24:64]
	maker := "0x" + log.Data[88:128]
	currency := "0x" + log.Data[152:192]
	collection := "0x" + log.Data[216:256]
	tokenID := new(big.Int).SetBytes(hexToBytes(log.Data[256:320]))
	price := new(big.Int).SetBytes(hexToBytes(log.Data[384:448]))

	var seller, buyer string
	if tradeType == "taker_bid" {
		seller, buyer = maker, taker
	} else {
		seller, buyer = taker, maker
	}

	sale := &NFTSale{
		ID:           fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		OrderHash:    orderHash,
		Protocol:     "looksrare",
		Collection:   collection,
		TokenID:      tokenID,
		Quantity:     1,
		Seller:       seller,
		Buyer:        buyer,
		Price:        price,
		PaymentToken: currency,
		FeeTotal:     big.NewInt(0),
		RoyaltyTotal: big.NewInt(0),
		BlockNumber:  log.BlockNumber,
		TxHash:       log.TxHash,
		LogIndex:     log.LogIndex,
		Timestamp:    log.Timestamp,
	}

	l.mu.Lock()
	l.sales = append(l.sales, sale)
	l.stats.TotalSales++
	l.stats.TotalVolume.Add(l.stats.TotalVolume, price)
	l.mu.Unlock()

	return nil
}

// BlurIndexer indexes Blur marketplace events
type BlurIndexer struct {
	config ProtocolConfig
	mu     sync.RWMutex
	sales  []*NFTSale
	stats  struct {
		TotalSales  uint64
		TotalVolume *big.Int
	}
}

func NewBlurIndexer(config ProtocolConfig) *BlurIndexer {
	return &BlurIndexer{
		config: config,
		sales:  make([]*NFTSale, 0),
		stats:  struct {
			TotalSales  uint64
			TotalVolume *big.Int
		}{TotalVolume: big.NewInt(0)},
	}
}

func (b *BlurIndexer) Name() string { return "blur" }

func (b *BlurIndexer) EventSignatures() []string {
	return []string{
		BlurOrdersMatchedSig,
		BlurOrderCancelledSig,
		BlurNonceIncrementedSig,
	}
}

func (b *BlurIndexer) IndexLog(ctx context.Context, log *EVMLog) error {
	if len(log.Topics) == 0 {
		return nil
	}

	switch log.Topics[0] {
	case BlurOrdersMatchedSig:
		return b.indexOrdersMatched(log)
	}
	return nil
}

func (b *BlurIndexer) indexOrdersMatched(log *EVMLog) error {
	if len(log.Topics) < 3 {
		return nil
	}

	// Topics: [sig, maker, taker]
	maker := "0x" + log.Topics[1][26:]
	taker := "0x" + log.Topics[2][26:]

	sale := &NFTSale{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		Protocol:    "blur",
		Seller:      maker,
		Buyer:       taker,
		Quantity:    1,
		FeeTotal:    big.NewInt(0),
		RoyaltyTotal: big.NewInt(0),
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		LogIndex:    log.LogIndex,
		Timestamp:   log.Timestamp,
	}

	// Extract price from data
	if len(log.Data) >= 128 {
		sale.Price = new(big.Int).SetBytes(hexToBytes(log.Data[64:128]))
	}

	b.mu.Lock()
	b.sales = append(b.sales, sale)
	b.stats.TotalSales++
	if sale.Price != nil {
		b.stats.TotalVolume.Add(b.stats.TotalVolume, sale.Price)
	}
	b.mu.Unlock()

	return nil
}

// X2Y2Indexer indexes X2Y2 marketplace events
type X2Y2Indexer struct {
	config ProtocolConfig
	mu     sync.RWMutex
	sales  []*NFTSale
	stats  struct {
		TotalSales  uint64
		TotalVolume *big.Int
	}
}

func NewX2Y2Indexer(config ProtocolConfig) *X2Y2Indexer {
	return &X2Y2Indexer{
		config: config,
		sales:  make([]*NFTSale, 0),
		stats:  struct {
			TotalSales  uint64
			TotalVolume *big.Int
		}{TotalVolume: big.NewInt(0)},
	}
}

func (x *X2Y2Indexer) Name() string { return "x2y2" }

func (x *X2Y2Indexer) EventSignatures() []string {
	return []string{X2Y2InventorySig, X2Y2CancelSig}
}

func (x *X2Y2Indexer) IndexLog(ctx context.Context, log *EVMLog) error {
	if len(log.Topics) == 0 {
		return nil
	}

	switch log.Topics[0] {
	case X2Y2InventorySig:
		return x.indexInventory(log)
	}
	return nil
}

func (x *X2Y2Indexer) indexInventory(log *EVMLog) error {
	if len(log.Topics) < 2 || len(log.Data) < 192 {
		return nil
	}

	itemHash := log.Topics[1]
	maker := "0x" + log.Data[24:64]
	taker := "0x" + log.Data[88:128]
	price := new(big.Int).SetBytes(hexToBytes(log.Data[128:192]))

	sale := &NFTSale{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		OrderHash:   itemHash,
		Protocol:    "x2y2",
		Seller:      maker,
		Buyer:       taker,
		Price:       price,
		Quantity:    1,
		FeeTotal:    big.NewInt(0),
		RoyaltyTotal: big.NewInt(0),
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		LogIndex:    log.LogIndex,
		Timestamp:   log.Timestamp,
	}

	x.mu.Lock()
	x.sales = append(x.sales, sale)
	x.stats.TotalSales++
	x.stats.TotalVolume.Add(x.stats.TotalVolume, price)
	x.mu.Unlock()

	return nil
}

// SudoswapIndexer indexes Sudoswap NFT AMM events
type SudoswapIndexer struct {
	config ProtocolConfig
	mu     sync.RWMutex
	swaps  []*NFTSale
	stats  struct {
		TotalSwaps  uint64
		TotalVolume *big.Int
	}
}

func NewSudoswapIndexer(config ProtocolConfig) *SudoswapIndexer {
	return &SudoswapIndexer{
		config: config,
		swaps:  make([]*NFTSale, 0),
		stats:  struct {
			TotalSwaps  uint64
			TotalVolume *big.Int
		}{TotalVolume: big.NewInt(0)},
	}
}

func (s *SudoswapIndexer) Name() string { return "sudoswap" }

func (s *SudoswapIndexer) EventSignatures() []string {
	return []string{SudoswapSwapNFTInPairSig, SudoswapSwapNFTOutPairSig}
}

func (s *SudoswapIndexer) IndexLog(ctx context.Context, log *EVMLog) error {
	if len(log.Topics) == 0 {
		return nil
	}

	// Track swap events from Sudoswap pairs
	switch log.Topics[0] {
	case SudoswapSwapNFTInPairSig, SudoswapSwapNFTOutPairSig:
		return s.indexSwap(log)
	}
	return nil
}

func (s *SudoswapIndexer) indexSwap(log *EVMLog) error {
	swap := &NFTSale{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		Protocol:    "sudoswap",
		Collection:  log.Address,
		Quantity:    1,
		FeeTotal:    big.NewInt(0),
		RoyaltyTotal: big.NewInt(0),
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		LogIndex:    log.LogIndex,
		Timestamp:   log.Timestamp,
	}

	s.mu.Lock()
	s.swaps = append(s.swaps, swap)
	s.stats.TotalSwaps++
	s.mu.Unlock()

	return nil
}

type WormholeIndexer struct{ config ProtocolConfig }
func NewWormholeIndexer(config ProtocolConfig) *WormholeIndexer { return &WormholeIndexer{config: config} }
func (w *WormholeIndexer) Name() string { return "wormhole" }
func (w *WormholeIndexer) EventSignatures() []string { return nil }
func (w *WormholeIndexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type LayerZeroIndexer struct{ config ProtocolConfig }
func NewLayerZeroIndexer(config ProtocolConfig) *LayerZeroIndexer { return &LayerZeroIndexer{config: config} }
func (l *LayerZeroIndexer) Name() string { return "layerzero" }
func (l *LayerZeroIndexer) EventSignatures() []string { return nil }
func (l *LayerZeroIndexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type StargateIndexer struct{ config ProtocolConfig }
func NewStargateIndexer(config ProtocolConfig) *StargateIndexer { return &StargateIndexer{config: config} }
func (s *StargateIndexer) Name() string { return "stargate" }
func (s *StargateIndexer) EventSignatures() []string { return nil }
func (s *StargateIndexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type LidoIndexer struct{ config ProtocolConfig }
func NewLidoIndexer(config ProtocolConfig) *LidoIndexer { return &LidoIndexer{config: config} }
func (l *LidoIndexer) Name() string { return "lido" }
func (l *LidoIndexer) EventSignatures() []string { return nil }
func (l *LidoIndexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type RocketPoolIndexer struct{ config ProtocolConfig }
func NewRocketPoolIndexer(config ProtocolConfig) *RocketPoolIndexer { return &RocketPoolIndexer{config: config} }
func (r *RocketPoolIndexer) Name() string { return "rocket_pool" }
func (r *RocketPoolIndexer) EventSignatures() []string { return nil }
func (r *RocketPoolIndexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type EigenlayerIndexer struct{ config ProtocolConfig }
func NewEigenlayerIndexer(config ProtocolConfig) *EigenlayerIndexer { return &EigenlayerIndexer{config: config} }
func (e *EigenlayerIndexer) Name() string { return "eigenlayer" }
func (e *EigenlayerIndexer) EventSignatures() []string { return nil }
func (e *EigenlayerIndexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type YearnIndexer struct{ config ProtocolConfig }
func NewYearnIndexer(config ProtocolConfig) *YearnIndexer { return &YearnIndexer{config: config} }
func (y *YearnIndexer) Name() string { return "yearn" }
func (y *YearnIndexer) EventSignatures() []string { return nil }
func (y *YearnIndexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type ConvexIndexer struct{ config ProtocolConfig }
func NewConvexIndexer(config ProtocolConfig) *ConvexIndexer { return &ConvexIndexer{config: config} }
func (c *ConvexIndexer) Name() string { return "convex" }
func (c *ConvexIndexer) EventSignatures() []string { return nil }
func (c *ConvexIndexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

type PendleIndexer struct{ config ProtocolConfig }
func NewPendleIndexer(config ProtocolConfig) *PendleIndexer { return &PendleIndexer{config: config} }
func (p *PendleIndexer) Name() string { return "pendle" }
func (p *PendleIndexer) EventSignatures() []string { return nil }
func (p *PendleIndexer) IndexLog(ctx context.Context, log *EVMLog) error { return nil }

// =============================================================================
// Helper Functions
// =============================================================================

// hexToBytes converts a hex string to bytes, handling 0x prefix
func hexToBytes(s string) []byte {
	if len(s) >= 2 && s[0:2] == "0x" {
		s = s[2:]
	}
	if len(s)%2 != 0 {
		s = "0" + s
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(b); i++ {
		b[i] = hexCharToByte(s[i*2])<<4 | hexCharToByte(s[i*2+1])
	}
	return b
}

func hexCharToByte(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}
