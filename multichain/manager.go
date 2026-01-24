// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package multichain provides a massively parallel multi-chain indexer
// capable of indexing thousands of chains concurrently using goroutines.
package multichain

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// ChainType identifies the blockchain architecture
type ChainType string

const (
	ChainTypeEVM       ChainType = "evm"
	ChainTypeSolana    ChainType = "solana"
	ChainTypeBitcoin   ChainType = "bitcoin"
	ChainTypeCosmos    ChainType = "cosmos"
	ChainTypeMove      ChainType = "move"      // Aptos, Sui
	ChainTypeNear      ChainType = "near"
	ChainTypeTron      ChainType = "tron"
	ChainTypeTon       ChainType = "ton"
	ChainTypeFlow      ChainType = "flow"
	ChainTypeTezos     ChainType = "tezos"
	ChainTypeAlgorand  ChainType = "algorand"
	ChainTypeCardano   ChainType = "cardano"
	ChainTypeSubstrate ChainType = "substrate" // Polkadot, Kusama
	ChainTypeStacks    ChainType = "stacks"
	ChainTypeHedera    ChainType = "hedera"
	ChainTypeFilecoin  ChainType = "filecoin"
	ChainTypeICP       ChainType = "icp"
	ChainTypeMultiversX ChainType = "multiversx"

	// Lux Native Chains (non-EVM)
	ChainTypeLuxAI        ChainType = "lux_ai"        // A-Chain: AI compute/Oracle
	ChainTypeLuxBridge    ChainType = "lux_bridge"    // B-Chain: Cross-chain bridge
	ChainTypeLuxThreshold ChainType = "lux_threshold" // T-Chain: Threshold FHE
	ChainTypeLuxZK        ChainType = "lux_zk"        // Z-Chain: Zero-knowledge
	ChainTypeLuxGraph     ChainType = "lux_graph"     // G-Chain: GraphQL indexing
	ChainTypeLuxIdentity  ChainType = "lux_identity"  // I-Chain: Decentralized identity
	ChainTypeLuxKey       ChainType = "lux_key"       // K-Chain: Key management
	ChainTypeLuxDEX       ChainType = "lux_dex"       // D-Chain: DEX orderbook
)

// ProtocolType identifies DeFi protocol types
type ProtocolType string

const (
	// DEX Protocols
	ProtocolUniswapV2    ProtocolType = "uniswap_v2"
	ProtocolUniswapV3    ProtocolType = "uniswap_v3"
	ProtocolSushiswap    ProtocolType = "sushiswap"
	ProtocolCurve        ProtocolType = "curve"
	ProtocolBalancer     ProtocolType = "balancer"
	ProtocolPancakeswap  ProtocolType = "pancakeswap"
	ProtocolTraderJoe    ProtocolType = "trader_joe"
	ProtocolCamelot      ProtocolType = "camelot"
	Protocol1inch        ProtocolType = "1inch"
	ProtocolParaswap     ProtocolType = "paraswap"
	ProtocolKyberswap    ProtocolType = "kyberswap"
	ProtocolDODO         ProtocolType = "dodo"
	ProtocolBancor       ProtocolType = "bancor"
	ProtocolVelodrome    ProtocolType = "velodrome"
	ProtocolAerodrome    ProtocolType = "aerodrome"
	ProtocolMaverick     ProtocolType = "maverick"

	// Lending/Borrowing
	ProtocolAaveV2       ProtocolType = "aave_v2"
	ProtocolAaveV3       ProtocolType = "aave_v3"
	ProtocolCompoundV2   ProtocolType = "compound_v2"
	ProtocolCompoundV3   ProtocolType = "compound_v3"
	ProtocolMakerDAO     ProtocolType = "makerdao"
	ProtocolMorpho       ProtocolType = "morpho"
	ProtocolEuler        ProtocolType = "euler"
	ProtocolRadiant      ProtocolType = "radiant"
	ProtocolBenqi        ProtocolType = "benqi"
	ProtocolVenus        ProtocolType = "venus"
	ProtocolSpark        ProtocolType = "spark"

	// Perpetuals/Derivatives
	ProtocolGMX          ProtocolType = "gmx"
	ProtocolGMXV2        ProtocolType = "gmx_v2"
	ProtocolGains        ProtocolType = "gains"
	ProtocolSynthetix    ProtocolType = "synthetix"
	ProtocolSynthetixV3  ProtocolType = "synthetix_v3"
	ProtocolPerpetual    ProtocolType = "perpetual"
	ProtocolDYDX         ProtocolType = "dydx"
	ProtocolKwenta       ProtocolType = "kwenta"
	ProtocolLevel        ProtocolType = "level"
	ProtocolMux          ProtocolType = "mux"
	ProtocolVela         ProtocolType = "vela"
	ProtocolVertex       ProtocolType = "vertex"
	ProtocolHyperliquid  ProtocolType = "hyperliquid"

	// Liquid Staking
	ProtocolLido         ProtocolType = "lido"
	ProtocolRocketPool   ProtocolType = "rocket_pool"
	ProtocolFrax         ProtocolType = "frax"
	ProtocolCoinbase     ProtocolType = "coinbase_staked"
	ProtocolStakeWise    ProtocolType = "stakewise"
	ProtocolSwell        ProtocolType = "swell"
	ProtocolEtherFi      ProtocolType = "etherfi"
	ProtocolKelp         ProtocolType = "kelp"
	ProtocolRenzo        ProtocolType = "renzo"
	ProtocolPuffer       ProtocolType = "puffer"
	ProtocolEigenlayer   ProtocolType = "eigenlayer"

	// Yield/Vaults
	ProtocolYearn        ProtocolType = "yearn"
	ProtocolConvex       ProtocolType = "convex"
	ProtocolBeefy        ProtocolType = "beefy"
	ProtocolSommelier    ProtocolType = "sommelier"
	ProtocolPendle       ProtocolType = "pendle"
	ProtocolOrigin       ProtocolType = "origin"
	ProtocolStakedao     ProtocolType = "stakedao"
	ProtocolConcentrator ProtocolType = "concentrator"

	// NFT Marketplaces
	ProtocolSeaport      ProtocolType = "seaport"
	ProtocolLooksRare    ProtocolType = "looksrare"
	ProtocolBlur         ProtocolType = "blur"
	ProtocolX2Y2         ProtocolType = "x2y2"
	ProtocolRarible      ProtocolType = "rarible"
	ProtocolZora         ProtocolType = "zora"
	ProtocolFoundation   ProtocolType = "foundation"
	ProtocolSuperRare    ProtocolType = "superrare"
	ProtocolMagicEden    ProtocolType = "magic_eden"
	ProtocolTensor       ProtocolType = "tensor"
	ProtocolSudoswap     ProtocolType = "sudoswap"
	ProtocolNFTX         ProtocolType = "nftx"
	ProtocolBendDAO      ProtocolType = "benddao"

	// Bridges
	ProtocolWormhole     ProtocolType = "wormhole"
	ProtocolLayerZero    ProtocolType = "layerzero"
	ProtocolStargate     ProtocolType = "stargate"
	ProtocolAxelar       ProtocolType = "axelar"
	ProtocolCeler        ProtocolType = "celer"
	ProtocolMultichain   ProtocolType = "multichain"
	ProtocolHop          ProtocolType = "hop"
	ProtocolAcross       ProtocolType = "across"
	ProtocolSynapse      ProtocolType = "synapse"
	ProtocolSocket       ProtocolType = "socket"
	ProtocolLifi         ProtocolType = "lifi"
	ProtocolDebridge     ProtocolType = "debridge"
	ProtocolCCTP         ProtocolType = "cctp"

	// Options/Structured Products
	ProtocolDopex        ProtocolType = "dopex"
	ProtocolLyra         ProtocolType = "lyra"
	ProtocolPremia       ProtocolType = "premia"
	ProtocolRibbon       ProtocolType = "ribbon"
	ProtocolThetanuts    ProtocolType = "thetanuts"

	// CDP/Stablecoins
	ProtocolLiquity      ProtocolType = "liquity"
	ProtocolPrisma       ProtocolType = "prisma"
	ProtocolGravita      ProtocolType = "gravita"
	ProtocolAbracadabra  ProtocolType = "abracadabra"
	ProtocolAngle        ProtocolType = "angle"
	ProtocolReflexer     ProtocolType = "reflexer"

	// RWA
	ProtocolCentrifuge   ProtocolType = "centrifuge"
	ProtocolMaple        ProtocolType = "maple"
	ProtocolGoldfinch    ProtocolType = "goldfinch"
	ProtocolOndo         ProtocolType = "ondo"

	// Insurance
	ProtocolNexusMutual  ProtocolType = "nexus_mutual"
	ProtocolInsurAce     ProtocolType = "insurace"

	// Prediction Markets
	ProtocolPolymarket   ProtocolType = "polymarket"
	ProtocolAugur        ProtocolType = "augur"

	// Bitcoin Protocols
	ProtocolOrdinals     ProtocolType = "ordinals"
	ProtocolRunes        ProtocolType = "runes"
	ProtocolBRC20        ProtocolType = "brc20"
	ProtocolStamps       ProtocolType = "stamps"
	ProtocolAtomicals    ProtocolType = "atomicals"

	// Solana Protocols
	ProtocolRaydium      ProtocolType = "raydium"
	ProtocolOrca         ProtocolType = "orca"
	ProtocolMarinade     ProtocolType = "marinade"
	ProtocolJito         ProtocolType = "jito"
	ProtocolJupiter      ProtocolType = "jupiter"
	ProtocolDrift        ProtocolType = "drift"
	ProtocolMango        ProtocolType = "mango"
	ProtocolPhoenix      ProtocolType = "phoenix"
	ProtocolMetaplex     ProtocolType = "metaplex"
)

// ChainConfig holds configuration for a single chain
type ChainConfig struct {
	ID          string            `yaml:"id"`
	ChainID     uint64            `yaml:"chain_id"`
	Name        string            `yaml:"name"`
	Symbol      string            `yaml:"symbol"`
	Type        ChainType         `yaml:"type"`
	RPC         string            `yaml:"rpc"`
	WS          string            `yaml:"ws,omitempty"`
	API         string            `yaml:"api,omitempty"`
	Explorer    string            `yaml:"explorer,omitempty"`
	Enabled     bool              `yaml:"enabled"`
	Protocols   []ProtocolConfig  `yaml:"protocols,omitempty"`
	StartBlock  uint64            `yaml:"start_block,omitempty"`
	BatchSize   int               `yaml:"batch_size,omitempty"`
	PollInterval time.Duration    `yaml:"poll_interval,omitempty"`
}

// ProtocolConfig holds configuration for a protocol on a chain
type ProtocolConfig struct {
	Type       ProtocolType `yaml:"type"`
	Address    string       `yaml:"address,omitempty"`
	Factory    string       `yaml:"factory,omitempty"`
	StartBlock uint64       `yaml:"start_block,omitempty"`
	Enabled    bool         `yaml:"enabled"`
}

// ChainIndexer is the interface for chain-specific indexers
type ChainIndexer interface {
	// Chain info
	ChainID() string
	ChainType() ChainType

	// Lifecycle
	Start(ctx context.Context) error
	Stop() error
	IsRunning() bool

	// Indexing
	IndexBlock(ctx context.Context, blockNumber uint64) error
	IndexBlockRange(ctx context.Context, from, to uint64) error
	GetLatestBlock(ctx context.Context) (uint64, error)
	GetIndexedBlock() uint64

	// Stats
	Stats() *IndexerStats
}

// IndexerStats holds statistics for an indexer
type IndexerStats struct {
	ChainID          string        `json:"chainId"`
	ChainName        string        `json:"chainName"`
	IsRunning        bool          `json:"isRunning"`
	LatestBlock      uint64        `json:"latestBlock"`
	IndexedBlock     uint64        `json:"indexedBlock"`
	BlocksBehind     uint64        `json:"blocksBehind"`
	BlocksProcessed  uint64        `json:"blocksProcessed"`
	TxsProcessed     uint64        `json:"txsProcessed"`
	EventsProcessed  uint64        `json:"eventsProcessed"`
	ErrorCount       uint64        `json:"errorCount"`
	LastError        string        `json:"lastError,omitempty"`
	LastBlockTime    time.Time     `json:"lastBlockTime"`
	StartTime        time.Time     `json:"startTime"`
	Uptime           time.Duration `json:"uptime"`
	BlocksPerSecond  float64       `json:"blocksPerSecond"`
}

// Manager coordinates indexing across all chains
type Manager struct {
	mu sync.RWMutex

	// Configuration
	config *Config

	// Chain indexers
	indexers map[string]ChainIndexer

	// Shared resources
	db     Database
	cache  Cache
	search SearchEngine

	// Stats
	stats *ManagerStats

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Config holds the manager configuration
type Config struct {
	// Database settings
	DatabaseURL     string `yaml:"database_url"`
	MaxConnections  int    `yaml:"max_connections"`

	// Redis settings
	RedisURL        string `yaml:"redis_url"`

	// Search settings
	ElasticsearchURL string `yaml:"elasticsearch_url"`

	// Indexing settings
	MaxConcurrentChains int           `yaml:"max_concurrent_chains"`
	DefaultBatchSize    int           `yaml:"default_batch_size"`
	DefaultPollInterval time.Duration `yaml:"default_poll_interval"`
	RetryAttempts       int           `yaml:"retry_attempts"`
	RetryDelay          time.Duration `yaml:"retry_delay"`

	// Chains to index
	Chains []ChainConfig `yaml:"chains"`
}

// ManagerStats holds aggregate statistics
type ManagerStats struct {
	mu sync.RWMutex

	TotalChains        int           `json:"totalChains"`
	ActiveChains       int           `json:"activeChains"`
	TotalBlocksIndexed uint64        `json:"totalBlocksIndexed"`
	TotalTxsIndexed    uint64        `json:"totalTxsIndexed"`
	TotalEventsIndexed uint64        `json:"totalEventsIndexed"`
	StartTime          time.Time     `json:"startTime"`
	Uptime             time.Duration `json:"uptime"`
	ChainStats         map[string]*IndexerStats `json:"chainStats"`
}

// Database interface for storage
type Database interface {
	Store(ctx context.Context, table string, data interface{}) error
	Query(ctx context.Context, query string, args ...interface{}) (interface{}, error)
	Close() error
}

// Cache interface for caching
type Cache interface {
	Get(ctx context.Context, key string) (interface{}, error)
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// SearchEngine interface for search
type SearchEngine interface {
	Index(ctx context.Context, index string, id string, data interface{}) error
	Search(ctx context.Context, index string, query interface{}) (interface{}, error)
}

// NewManager creates a new multi-chain indexer manager
func NewManager(config *Config) (*Manager, error) {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		config:   config,
		indexers: make(map[string]ChainIndexer),
		ctx:      ctx,
		cancel:   cancel,
		stats: &ManagerStats{
			StartTime:  time.Now(),
			ChainStats: make(map[string]*IndexerStats),
		},
	}

	// Initialize chains
	for _, chainConfig := range config.Chains {
		if !chainConfig.Enabled {
			continue
		}

		indexer, err := m.createIndexer(chainConfig)
		if err != nil {
			log.Printf("Warning: Failed to create indexer for %s: %v", chainConfig.ID, err)
			continue
		}

		m.indexers[chainConfig.ID] = indexer
		m.stats.TotalChains++
	}

	return m, nil
}

// createIndexer creates the appropriate indexer for a chain type
func (m *Manager) createIndexer(config ChainConfig) (ChainIndexer, error) {
	switch config.Type {
	case ChainTypeEVM:
		return NewEVMIndexer(config, m.db)
	case ChainTypeSolana:
		return NewSolanaIndexer(config, m.db)
	case ChainTypeBitcoin:
		return NewBitcoinIndexer(config, m.db)
	case ChainTypeCosmos:
		return NewCosmosIndexer(config, m.db)
	case ChainTypeMove:
		return NewMoveIndexer(config, m.db)
	case ChainTypeNear:
		return NewNearIndexer(config, m.db)
	case ChainTypeTron:
		return NewTronIndexer(config, m.db)
	case ChainTypeTon:
		return NewTonIndexer(config, m.db)
	case ChainTypeSubstrate:
		return NewSubstrateIndexer(config, m.db)
	// Lux Native Chains
	case ChainTypeLuxAI, ChainTypeLuxBridge, ChainTypeLuxThreshold, ChainTypeLuxZK,
		ChainTypeLuxGraph, ChainTypeLuxIdentity, ChainTypeLuxKey, ChainTypeLuxDEX:
		return NewLuxNativeIndexer(config), nil
	default:
		return nil, fmt.Errorf("unsupported chain type: %s", config.Type)
	}
}

// Start starts all chain indexers concurrently
func (m *Manager) Start() error {
	log.Printf("Starting multi-chain indexer with %d chains", len(m.indexers))

	// Use semaphore to limit concurrent chain startups
	sem := make(chan struct{}, m.config.MaxConcurrentChains)

	for chainID, indexer := range m.indexers {
		sem <- struct{}{} // Acquire semaphore

		m.wg.Add(1)
		go func(id string, idx ChainIndexer) {
			defer m.wg.Done()
			defer func() { <-sem }() // Release semaphore

			log.Printf("Starting indexer for chain: %s", id)
			if err := idx.Start(m.ctx); err != nil {
				log.Printf("Error starting indexer for %s: %v", id, err)
				return
			}

			m.mu.Lock()
			m.stats.ActiveChains++
			m.mu.Unlock()

			// Run indexing loop
			m.runIndexer(id, idx)
		}(chainID, indexer)
	}

	// Start stats collector
	go m.collectStats()

	return nil
}

// runIndexer runs the indexing loop for a single chain
func (m *Manager) runIndexer(chainID string, indexer ChainIndexer) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			log.Printf("Stopping indexer for chain: %s", chainID)
			indexer.Stop()
			return
		case <-ticker.C:
			// Get latest block
			latest, err := indexer.GetLatestBlock(m.ctx)
			if err != nil {
				log.Printf("Error getting latest block for %s: %v", chainID, err)
				continue
			}

			indexed := indexer.GetIndexedBlock()
			if indexed >= latest {
				continue // Already caught up
			}

			// Index missing blocks in batches
			batchSize := uint64(100)
			for from := indexed + 1; from <= latest; from += batchSize {
				to := from + batchSize - 1
				if to > latest {
					to = latest
				}

				if err := indexer.IndexBlockRange(m.ctx, from, to); err != nil {
					log.Printf("Error indexing blocks %d-%d for %s: %v", from, to, chainID, err)
					break
				}
			}
		}
	}
}

// collectStats periodically collects statistics from all indexers
func (m *Manager) collectStats() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			m.stats.Uptime = time.Since(m.stats.StartTime)

			var totalBlocks, totalTxs, totalEvents uint64
			for id, indexer := range m.indexers {
				stats := indexer.Stats()
				m.stats.ChainStats[id] = stats
				totalBlocks += stats.BlocksProcessed
				totalTxs += stats.TxsProcessed
				totalEvents += stats.EventsProcessed
			}

			m.stats.TotalBlocksIndexed = totalBlocks
			m.stats.TotalTxsIndexed = totalTxs
			m.stats.TotalEventsIndexed = totalEvents
			m.mu.Unlock()
		}
	}
}

// Stop stops all indexers
func (m *Manager) Stop() error {
	log.Println("Stopping multi-chain indexer manager")
	m.cancel()
	m.wg.Wait()
	return nil
}

// Stats returns aggregate statistics
func (m *Manager) Stats() *ManagerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Clone stats
	stats := &ManagerStats{
		TotalChains:        m.stats.TotalChains,
		ActiveChains:       m.stats.ActiveChains,
		TotalBlocksIndexed: m.stats.TotalBlocksIndexed,
		TotalTxsIndexed:    m.stats.TotalTxsIndexed,
		TotalEventsIndexed: m.stats.TotalEventsIndexed,
		StartTime:          m.stats.StartTime,
		Uptime:             m.stats.Uptime,
		ChainStats:         make(map[string]*IndexerStats),
	}

	for k, v := range m.stats.ChainStats {
		stats.ChainStats[k] = v
	}

	return stats
}

// GetIndexer returns a specific chain indexer
func (m *Manager) GetIndexer(chainID string) (ChainIndexer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	idx, ok := m.indexers[chainID]
	return idx, ok
}

// AddChain dynamically adds a new chain
func (m *Manager) AddChain(config ChainConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.indexers[config.ID]; exists {
		return fmt.Errorf("chain %s already exists", config.ID)
	}

	indexer, err := m.createIndexer(config)
	if err != nil {
		return err
	}

	m.indexers[config.ID] = indexer
	m.stats.TotalChains++

	// Start the indexer
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if err := indexer.Start(m.ctx); err != nil {
			log.Printf("Error starting indexer for %s: %v", config.ID, err)
			return
		}
		m.mu.Lock()
		m.stats.ActiveChains++
		m.mu.Unlock()
		m.runIndexer(config.ID, indexer)
	}()

	return nil
}

// RemoveChain dynamically removes a chain
func (m *Manager) RemoveChain(chainID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	indexer, exists := m.indexers[chainID]
	if !exists {
		return fmt.Errorf("chain %s not found", chainID)
	}

	indexer.Stop()
	delete(m.indexers, chainID)
	delete(m.stats.ChainStats, chainID)
	m.stats.TotalChains--
	m.stats.ActiveChains--

	return nil
}
