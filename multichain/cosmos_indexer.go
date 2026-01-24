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

// CosmosIndexer indexes Cosmos SDK-based chains
type CosmosIndexer struct {
	mu sync.RWMutex

	config ChainConfig
	db     Database
	client *http.Client

	// State
	running       int32
	indexedHeight uint64
	latestHeight  uint64

	// Protocol indexers
	protocols map[ProtocolType]CosmosProtocolIndexer

	// Stats
	stats *IndexerStats

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// CosmosProtocolIndexer handles Cosmos protocol-specific indexing
type CosmosProtocolIndexer interface {
	Name() string
	MessageTypes() []string
	IndexMessage(ctx context.Context, msg *CosmosMessage, tx *CosmosTx) error
}

// CosmosBlock represents a Cosmos block
type CosmosBlock struct {
	BlockID BlockID       `json:"block_id"`
	Block   BlockData     `json:"block"`
	Txs     []CosmosTx    `json:"txs,omitempty"`
}

// BlockID represents block identification
type BlockID struct {
	Hash  string `json:"hash"`
	Parts struct {
		Total int    `json:"total"`
		Hash  string `json:"hash"`
	} `json:"parts"`
}

// BlockData represents block data
type BlockData struct {
	Header BlockHeader `json:"header"`
	Data   struct {
		Txs []string `json:"txs"` // Base64 encoded
	} `json:"data"`
	Evidence struct {
		Evidence []interface{} `json:"evidence"`
	} `json:"evidence"`
	LastCommit LastCommit `json:"last_commit"`
}

// BlockHeader represents block header
type BlockHeader struct {
	Version struct {
		Block string `json:"block"`
		App   string `json:"app"`
	} `json:"version"`
	ChainID         string    `json:"chain_id"`
	Height          string    `json:"height"`
	Time            time.Time `json:"time"`
	LastBlockID     BlockID   `json:"last_block_id"`
	LastCommitHash  string    `json:"last_commit_hash"`
	DataHash        string    `json:"data_hash"`
	ValidatorsHash  string    `json:"validators_hash"`
	ConsensusHash   string    `json:"consensus_hash"`
	AppHash         string    `json:"app_hash"`
	LastResultsHash string    `json:"last_results_hash"`
	EvidenceHash    string    `json:"evidence_hash"`
	ProposerAddress string    `json:"proposer_address"`
}

// LastCommit represents last commit info
type LastCommit struct {
	Height     string `json:"height"`
	Round      int    `json:"round"`
	BlockID    BlockID `json:"block_id"`
	Signatures []struct {
		BlockIDFlag      int       `json:"block_id_flag"`
		ValidatorAddress string    `json:"validator_address"`
		Timestamp        time.Time `json:"timestamp"`
		Signature        string    `json:"signature"`
	} `json:"signatures"`
}

// CosmosTx represents a Cosmos transaction
type CosmosTx struct {
	Hash     string          `json:"hash"`
	Height   uint64          `json:"height"`
	Index    uint32          `json:"index"`
	Code     uint32          `json:"code"` // 0 = success
	Data     string          `json:"data"`
	RawLog   string          `json:"raw_log"`
	Log      []TxLog         `json:"logs"`
	GasWanted uint64         `json:"gas_wanted"`
	GasUsed   uint64         `json:"gas_used"`
	Tx       TxBody          `json:"tx"`
	Timestamp time.Time      `json:"timestamp"`
	Events   []CosmosEvent   `json:"events"`
}

// TxBody represents the transaction body
type TxBody struct {
	Body struct {
		Messages                    []CosmosMessage `json:"messages"`
		Memo                        string          `json:"memo"`
		TimeoutHeight               string          `json:"timeout_height"`
		ExtensionOptions            []interface{}   `json:"extension_options"`
		NonCriticalExtensionOptions []interface{}   `json:"non_critical_extension_options"`
	} `json:"body"`
	AuthInfo struct {
		SignerInfos []SignerInfo `json:"signer_infos"`
		Fee         TxFee        `json:"fee"`
	} `json:"auth_info"`
	Signatures []string `json:"signatures"`
}

// CosmosMessage represents a Cosmos message
type CosmosMessage struct {
	Type  string                 `json:"@type"`
	Value map[string]interface{} `json:"-"` // Decoded message content
	Raw   json.RawMessage        `json:"-"` // Raw JSON for custom parsing
}

// SignerInfo represents signer information
type SignerInfo struct {
	PublicKey struct {
		Type string `json:"@type"`
		Key  string `json:"key"`
	} `json:"public_key"`
	ModeInfo struct {
		Single struct {
			Mode string `json:"mode"`
		} `json:"single"`
	} `json:"mode_info"`
	Sequence string `json:"sequence"`
}

// TxFee represents transaction fee
type TxFee struct {
	Amount   []Coin `json:"amount"`
	GasLimit string `json:"gas_limit"`
	Payer    string `json:"payer"`
	Granter  string `json:"granter"`
}

// Coin represents a coin
type Coin struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

// TxLog represents transaction log entry
type TxLog struct {
	MsgIndex uint32        `json:"msg_index"`
	Log      string        `json:"log"`
	Events   []CosmosEvent `json:"events"`
}

// CosmosEvent represents an event
type CosmosEvent struct {
	Type       string            `json:"type"`
	Attributes []EventAttribute  `json:"attributes"`
}

// EventAttribute represents event attribute
type EventAttribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Index bool   `json:"index,omitempty"`
}

// NewCosmosIndexer creates a new Cosmos indexer
func NewCosmosIndexer(config ChainConfig, db Database) (*CosmosIndexer, error) {
	ctx, cancel := context.WithCancel(context.Background())

	idx := &CosmosIndexer{
		config:    config,
		db:        db,
		client:    &http.Client{Timeout: 30 * time.Second},
		protocols: make(map[ProtocolType]CosmosProtocolIndexer),
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
		idx.initProtocol(proto)
	}

	return idx, nil
}

// initProtocol initializes a Cosmos protocol indexer
func (c *CosmosIndexer) initProtocol(config ProtocolConfig) {
	var indexer CosmosProtocolIndexer

	switch c.config.ID {
	case "osmosis":
		indexer = NewOsmosisIndexer(config)
	case "injective":
		indexer = NewInjectiveIndexer(config)
	case "dydx":
		indexer = NewDYDXCosmosIndexer(config)
	case "celestia":
		indexer = NewCelestiaIndexer(config)
	case "sei":
		indexer = NewSeiIndexer(config)
	case "neutron":
		indexer = NewNeutronIndexer(config)
	default:
		indexer = NewGenericCosmosIndexer(config)
	}

	c.protocols[config.Type] = indexer
}

// ChainID returns the chain identifier
func (c *CosmosIndexer) ChainID() string {
	return c.config.ID
}

// ChainType returns the chain type
func (c *CosmosIndexer) ChainType() ChainType {
	return ChainTypeCosmos
}

// Start starts the indexer
func (c *CosmosIndexer) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&c.running, 0, 1) {
		return fmt.Errorf("indexer already running")
	}
	c.stats.IsRunning = true
	return nil
}

// Stop stops the indexer
func (c *CosmosIndexer) Stop() error {
	atomic.StoreInt32(&c.running, 0)
	c.cancel()
	c.stats.IsRunning = false
	return nil
}

// IsRunning returns whether the indexer is running
func (c *CosmosIndexer) IsRunning() bool {
	return atomic.LoadInt32(&c.running) == 1
}

// GetLatestBlock returns the latest block height
func (c *CosmosIndexer) GetLatestBlock(ctx context.Context) (uint64, error) {
	url := fmt.Sprintf("%s/cosmos/base/tendermint/v1beta1/blocks/latest", c.config.RPC)

	resp, err := c.httpGet(ctx, url)
	if err != nil {
		return 0, err
	}

	var result struct {
		Block struct {
			Header struct {
				Height string `json:"height"`
			} `json:"header"`
		} `json:"block"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return 0, err
	}

	var height uint64
	fmt.Sscanf(result.Block.Header.Height, "%d", &height)

	c.mu.Lock()
	c.latestHeight = height
	c.mu.Unlock()

	return height, nil
}

// GetIndexedBlock returns the last indexed block
func (c *CosmosIndexer) GetIndexedBlock() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.indexedHeight
}

// IndexBlock indexes a single block
func (c *CosmosIndexer) IndexBlock(ctx context.Context, height uint64) error {
	// Fetch block
	block, err := c.fetchBlock(ctx, height)
	if err != nil {
		return fmt.Errorf("failed to fetch block %d: %w", height, err)
	}

	// Fetch transactions for this block
	txs, err := c.fetchBlockTxs(ctx, height)
	if err != nil {
		return fmt.Errorf("failed to fetch txs for block %d: %w", height, err)
	}

	block.Txs = txs

	// Process transactions
	for _, tx := range txs {
		c.processTransaction(ctx, &tx)
	}

	c.mu.Lock()
	c.indexedHeight = height
	c.stats.BlocksProcessed++
	c.stats.TxsProcessed += uint64(len(txs))
	c.stats.LastBlockTime = time.Now()
	c.mu.Unlock()

	return nil
}

// IndexBlockRange indexes a range of blocks
func (c *CosmosIndexer) IndexBlockRange(ctx context.Context, from, to uint64) error {
	for height := from; height <= to; height++ {
		if err := c.IndexBlock(ctx, height); err != nil {
			c.stats.ErrorCount++
			c.stats.LastError = err.Error()
			continue
		}
	}
	return nil
}

// fetchBlock fetches a block from the API
func (c *CosmosIndexer) fetchBlock(ctx context.Context, height uint64) (*CosmosBlock, error) {
	url := fmt.Sprintf("%s/cosmos/base/tendermint/v1beta1/blocks/%d", c.config.RPC, height)

	resp, err := c.httpGet(ctx, url)
	if err != nil {
		return nil, err
	}

	var block CosmosBlock
	if err := json.Unmarshal(resp, &block); err != nil {
		return nil, err
	}

	return &block, nil
}

// fetchBlockTxs fetches transactions for a block
func (c *CosmosIndexer) fetchBlockTxs(ctx context.Context, height uint64) ([]CosmosTx, error) {
	url := fmt.Sprintf("%s/cosmos/tx/v1beta1/txs?events=tx.height=%d", c.config.RPC, height)

	resp, err := c.httpGet(ctx, url)
	if err != nil {
		return nil, err
	}

	var result struct {
		Txs         []CosmosTx `json:"tx_responses"`
		Pagination  struct {
			NextKey string `json:"next_key"`
			Total   string `json:"total"`
		} `json:"pagination"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}

	return result.Txs, nil
}

// processTransaction routes a transaction to protocol indexers
func (c *CosmosIndexer) processTransaction(ctx context.Context, tx *CosmosTx) {
	// Extract messages from transaction
	for _, msg := range tx.Tx.Body.Messages {
		for _, indexer := range c.protocols {
			indexer.IndexMessage(ctx, &msg, tx)
		}
	}
}

// httpGet makes an HTTP GET request
func (c *CosmosIndexer) httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// Stats returns indexer statistics
func (c *CosmosIndexer) Stats() *IndexerStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return &IndexerStats{
		ChainID:         c.stats.ChainID,
		ChainName:       c.stats.ChainName,
		IsRunning:       c.stats.IsRunning,
		LatestBlock:     c.latestHeight,
		IndexedBlock:    c.indexedHeight,
		BlocksBehind:    c.latestHeight - c.indexedHeight,
		BlocksProcessed: c.stats.BlocksProcessed,
		TxsProcessed:    c.stats.TxsProcessed,
		EventsProcessed: c.stats.EventsProcessed,
		ErrorCount:      c.stats.ErrorCount,
		LastError:       c.stats.LastError,
		LastBlockTime:   c.stats.LastBlockTime,
		StartTime:       c.stats.StartTime,
		Uptime:          time.Since(c.stats.StartTime),
	}
}

// =============================================================================
// Cosmos Protocol Indexer Stubs
// =============================================================================

type GenericCosmosIndexer struct{ config ProtocolConfig }
func NewGenericCosmosIndexer(config ProtocolConfig) *GenericCosmosIndexer { return &GenericCosmosIndexer{config: config} }
func (g *GenericCosmosIndexer) Name() string { return "generic_cosmos" }
func (g *GenericCosmosIndexer) MessageTypes() []string { return nil }
func (g *GenericCosmosIndexer) IndexMessage(ctx context.Context, msg *CosmosMessage, tx *CosmosTx) error { return nil }

// Osmosis DEX
type OsmosisIndexer struct{ config ProtocolConfig }
func NewOsmosisIndexer(config ProtocolConfig) *OsmosisIndexer { return &OsmosisIndexer{config: config} }
func (o *OsmosisIndexer) Name() string { return "osmosis" }
func (o *OsmosisIndexer) MessageTypes() []string {
	return []string{
		"/osmosis.gamm.v1beta1.MsgSwapExactAmountIn",
		"/osmosis.gamm.v1beta1.MsgSwapExactAmountOut",
		"/osmosis.gamm.v1beta1.MsgJoinPool",
		"/osmosis.gamm.v1beta1.MsgExitPool",
		"/osmosis.concentratedliquidity.v1beta1.MsgCreatePosition",
		"/osmosis.superfluid.MsgSuperfluidDelegate",
	}
}
func (o *OsmosisIndexer) IndexMessage(ctx context.Context, msg *CosmosMessage, tx *CosmosTx) error { return nil }

// Injective DEX
type InjectiveIndexer struct{ config ProtocolConfig }
func NewInjectiveIndexer(config ProtocolConfig) *InjectiveIndexer { return &InjectiveIndexer{config: config} }
func (i *InjectiveIndexer) Name() string { return "injective" }
func (i *InjectiveIndexer) MessageTypes() []string {
	return []string{
		"/injective.exchange.v1beta1.MsgCreateSpotLimitOrder",
		"/injective.exchange.v1beta1.MsgCreateDerivativeLimitOrder",
		"/injective.exchange.v1beta1.MsgInstantSpotMarketLaunch",
		"/injective.exchange.v1beta1.MsgDeposit",
	}
}
func (i *InjectiveIndexer) IndexMessage(ctx context.Context, msg *CosmosMessage, tx *CosmosTx) error { return nil }

// dYdX Cosmos
type DYDXCosmosIndexer struct{ config ProtocolConfig }
func NewDYDXCosmosIndexer(config ProtocolConfig) *DYDXCosmosIndexer { return &DYDXCosmosIndexer{config: config} }
func (d *DYDXCosmosIndexer) Name() string { return "dydx_cosmos" }
func (d *DYDXCosmosIndexer) MessageTypes() []string {
	return []string{
		"/dydxprotocol.clob.MsgPlaceOrder",
		"/dydxprotocol.clob.MsgCancelOrder",
		"/dydxprotocol.sending.MsgCreateTransfer",
		"/dydxprotocol.subaccounts.MsgCreateSubaccount",
	}
}
func (d *DYDXCosmosIndexer) IndexMessage(ctx context.Context, msg *CosmosMessage, tx *CosmosTx) error { return nil }

// Celestia Data Availability
type CelestiaIndexer struct{ config ProtocolConfig }
func NewCelestiaIndexer(config ProtocolConfig) *CelestiaIndexer { return &CelestiaIndexer{config: config} }
func (c *CelestiaIndexer) Name() string { return "celestia" }
func (c *CelestiaIndexer) MessageTypes() []string {
	return []string{
		"/celestia.blob.v1.MsgPayForBlobs",
	}
}
func (c *CelestiaIndexer) IndexMessage(ctx context.Context, msg *CosmosMessage, tx *CosmosTx) error { return nil }

// Sei
type SeiIndexer struct{ config ProtocolConfig }
func NewSeiIndexer(config ProtocolConfig) *SeiIndexer { return &SeiIndexer{config: config} }
func (s *SeiIndexer) Name() string { return "sei" }
func (s *SeiIndexer) MessageTypes() []string {
	return []string{
		"/seiprotocol.seichain.dex.MsgPlaceOrders",
		"/seiprotocol.seichain.dex.MsgCancelOrders",
	}
}
func (s *SeiIndexer) IndexMessage(ctx context.Context, msg *CosmosMessage, tx *CosmosTx) error { return nil }

// Neutron
type NeutronIndexer struct{ config ProtocolConfig }
func NewNeutronIndexer(config ProtocolConfig) *NeutronIndexer { return &NeutronIndexer{config: config} }
func (n *NeutronIndexer) Name() string { return "neutron" }
func (n *NeutronIndexer) MessageTypes() []string {
	return []string{
		"/neutron.interchainqueries.MsgRegisterInterchainQuery",
		"/neutron.interchaintxs.v1.MsgSubmitTx",
	}
}
func (n *NeutronIndexer) IndexMessage(ctx context.Context, msg *CosmosMessage, tx *CosmosTx) error { return nil }
