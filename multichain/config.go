// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package multichain

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ChainsConfig represents the full chains.yaml configuration
type ChainsConfig struct {
	Lux      map[string]YAMLChainConfig `yaml:"lux"`
	Ethereum map[string]YAMLChainConfig `yaml:"ethereum"`
	Solana   map[string]YAMLChainConfig `yaml:"solana"`
	Bitcoin  map[string]YAMLChainConfig `yaml:"bitcoin"`
	Cosmos   map[string]YAMLChainConfig `yaml:"cosmos"`
	Move     map[string]YAMLChainConfig `yaml:"move"`
	Other    map[string]YAMLChainConfig `yaml:"other"`
}

// YAMLChainConfig represents a chain configuration from YAML
type YAMLChainConfig struct {
	ChainID        uint64                 `yaml:"chain_id"`
	Name           string                 `yaml:"name"`
	Symbol         string                 `yaml:"symbol"`
	Type           string                 `yaml:"type"`
	RPC            string                 `yaml:"rpc"`
	WS             string                 `yaml:"ws,omitempty"`
	API            string                 `yaml:"api,omitempty"`
	Explorer       string                 `yaml:"explorer,omitempty"`
	Enabled        bool                   `yaml:"enabled"`
	NFTMarketplaces []YAMLMarketplace     `yaml:"nft_marketplaces,omitempty"`
	Protocols      []YAMLProtocol         `yaml:"protocols,omitempty"`
	StartBlock     uint64                 `yaml:"start_block,omitempty"`
	BatchSize      int                    `yaml:"batch_size,omitempty"`
	PollInterval   string                 `yaml:"poll_interval,omitempty"`
}

// YAMLMarketplace represents an NFT marketplace config
type YAMLMarketplace struct {
	Protocol   string `yaml:"protocol"`
	Address    string `yaml:"address,omitempty"`
	StartBlock uint64 `yaml:"start_block,omitempty"`
}

// YAMLProtocol represents a protocol config
type YAMLProtocol struct {
	Type       string `yaml:"type"`
	Address    string `yaml:"address,omitempty"`
	Factory    string `yaml:"factory,omitempty"`
	StartBlock uint64 `yaml:"start_block,omitempty"`
	Enabled    bool   `yaml:"enabled"`
}

// LoadChainsConfig loads chains configuration from a YAML file
func LoadChainsConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	var chainsConfig ChainsConfig
	if err := yaml.Unmarshal([]byte(expanded), &chainsConfig); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	config := &Config{
		Chains:              make([]ChainConfig, 0),
		MaxConcurrentChains: 100, // Default
		DefaultBatchSize:    100,
		DefaultPollInterval: 5 * time.Second,
		RetryAttempts:       3,
		RetryDelay:          time.Second,
	}

	// Process each chain category
	for id, yc := range chainsConfig.Lux {
		if cc := convertYAMLConfig(id, yc); cc != nil {
			config.Chains = append(config.Chains, *cc)
		}
	}
	for id, yc := range chainsConfig.Ethereum {
		if cc := convertYAMLConfig(id, yc); cc != nil {
			config.Chains = append(config.Chains, *cc)
		}
	}
	for id, yc := range chainsConfig.Solana {
		if cc := convertYAMLConfig(id, yc); cc != nil {
			config.Chains = append(config.Chains, *cc)
		}
	}
	for id, yc := range chainsConfig.Bitcoin {
		if cc := convertYAMLConfig(id, yc); cc != nil {
			config.Chains = append(config.Chains, *cc)
		}
	}
	for id, yc := range chainsConfig.Cosmos {
		if cc := convertYAMLConfig(id, yc); cc != nil {
			config.Chains = append(config.Chains, *cc)
		}
	}
	for id, yc := range chainsConfig.Move {
		if cc := convertYAMLConfig(id, yc); cc != nil {
			config.Chains = append(config.Chains, *cc)
		}
	}
	for id, yc := range chainsConfig.Other {
		if cc := convertYAMLConfig(id, yc); cc != nil {
			config.Chains = append(config.Chains, *cc)
		}
	}

	return config, nil
}

// convertYAMLConfig converts a YAML chain config to ChainConfig
func convertYAMLConfig(id string, yc YAMLChainConfig) *ChainConfig {
	if !yc.Enabled {
		return nil
	}

	chainType := parseChainType(yc.Type)

	pollInterval := 5 * time.Second
	if yc.PollInterval != "" {
		if d, err := time.ParseDuration(yc.PollInterval); err == nil {
			pollInterval = d
		}
	}

	batchSize := 100
	if yc.BatchSize > 0 {
		batchSize = yc.BatchSize
	}

	cc := &ChainConfig{
		ID:           id,
		ChainID:      yc.ChainID,
		Name:         yc.Name,
		Symbol:       yc.Symbol,
		Type:         chainType,
		RPC:          yc.RPC,
		WS:           yc.WS,
		API:          yc.API,
		Explorer:     yc.Explorer,
		Enabled:      true,
		StartBlock:   yc.StartBlock,
		BatchSize:    batchSize,
		PollInterval: pollInterval,
		Protocols:    make([]ProtocolConfig, 0),
	}

	// Convert NFT marketplaces to protocols
	for _, mp := range yc.NFTMarketplaces {
		proto := ProtocolConfig{
			Type:       parseProtocolType(mp.Protocol),
			Address:    mp.Address,
			StartBlock: mp.StartBlock,
			Enabled:    true,
		}
		cc.Protocols = append(cc.Protocols, proto)
	}

	// Convert protocols
	for _, p := range yc.Protocols {
		if !p.Enabled {
			continue
		}
		proto := ProtocolConfig{
			Type:       parseProtocolType(p.Type),
			Address:    p.Address,
			Factory:    p.Factory,
			StartBlock: p.StartBlock,
			Enabled:    true,
		}
		cc.Protocols = append(cc.Protocols, proto)
	}

	return cc
}

// parseChainType parses chain type from string
func parseChainType(s string) ChainType {
	switch strings.ToLower(s) {
	case "evm":
		return ChainTypeEVM
	case "solana":
		return ChainTypeSolana
	case "bitcoin":
		return ChainTypeBitcoin
	case "cosmos":
		return ChainTypeCosmos
	case "move":
		return ChainTypeMove
	case "near":
		return ChainTypeNear
	case "tron":
		return ChainTypeTron
	case "ton":
		return ChainTypeTon
	case "substrate", "polkadot":
		return ChainTypeSubstrate
	case "stacks":
		return ChainTypeStacks
	case "hedera":
		return ChainTypeHedera
	case "filecoin":
		return ChainTypeFilecoin
	case "icp":
		return ChainTypeICP
	case "multiversx":
		return ChainTypeMultiversX
	// Lux Native Chains
	case "lux_ai", "aivm", "achain", "a-chain":
		return ChainTypeLuxAI
	case "lux_bridge", "bridgevm", "bchain", "b-chain":
		return ChainTypeLuxBridge
	case "lux_threshold", "thresholdvm", "tchain", "t-chain":
		return ChainTypeLuxThreshold
	case "lux_zk", "zkvm", "zchain", "z-chain":
		return ChainTypeLuxZK
	case "lux_graph", "graphvm", "gchain", "g-chain":
		return ChainTypeLuxGraph
	case "lux_identity", "identityvm", "ichain", "i-chain":
		return ChainTypeLuxIdentity
	case "lux_key", "keyvm", "kchain", "k-chain":
		return ChainTypeLuxKey
	case "lux_dex", "dexvm", "dchain", "d-chain":
		return ChainTypeLuxDEX
	default:
		return ChainTypeEVM
	}
}

// parseProtocolType parses protocol type from string
func parseProtocolType(s string) ProtocolType {
	switch strings.ToLower(s) {
	// NFT Marketplaces
	case "seaport", "opensea":
		return ProtocolSeaport
	case "looksrare", "looksrare_v2":
		return ProtocolLooksRare
	case "blur":
		return ProtocolBlur
	case "x2y2":
		return ProtocolX2Y2
	case "rarible":
		return ProtocolRarible
	case "zora":
		return ProtocolZora
	case "magic_eden", "magiceden":
		return ProtocolMagicEden
	case "tensor":
		return ProtocolTensor
	case "sudoswap":
		return ProtocolSudoswap

	// DEX
	case "uniswap_v2", "uniswapv2":
		return ProtocolUniswapV2
	case "uniswap_v3", "uniswapv3":
		return ProtocolUniswapV3
	case "sushiswap":
		return ProtocolSushiswap
	case "curve":
		return ProtocolCurve
	case "balancer":
		return ProtocolBalancer
	case "pancakeswap":
		return ProtocolPancakeswap

	// Lending
	case "aave_v2", "aavev2":
		return ProtocolAaveV2
	case "aave_v3", "aavev3":
		return ProtocolAaveV3
	case "compound_v2", "compoundv2":
		return ProtocolCompoundV2
	case "compound_v3", "compoundv3":
		return ProtocolCompoundV3

	// Perpetuals
	case "gmx":
		return ProtocolGMX
	case "gmx_v2", "gmxv2":
		return ProtocolGMXV2
	case "synthetix":
		return ProtocolSynthetix
	case "dydx":
		return ProtocolDYDX

	// Liquid Staking
	case "lido":
		return ProtocolLido
	case "rocket_pool", "rocketpool":
		return ProtocolRocketPool
	case "eigenlayer":
		return ProtocolEigenlayer

	// Bridges
	case "wormhole":
		return ProtocolWormhole
	case "layerzero":
		return ProtocolLayerZero
	case "stargate":
		return ProtocolStargate

	// Bitcoin
	case "ordinals":
		return ProtocolOrdinals
	case "runes":
		return ProtocolRunes
	case "brc20", "brc-20":
		return ProtocolBRC20
	case "stamps":
		return ProtocolStamps
	case "atomicals":
		return ProtocolAtomicals

	// Solana
	case "raydium":
		return ProtocolRaydium
	case "orca":
		return ProtocolOrca
	case "marinade":
		return ProtocolMarinade
	case "jito":
		return ProtocolJito
	case "jupiter":
		return ProtocolJupiter
	case "drift":
		return ProtocolDrift
	case "mango":
		return ProtocolMango
	case "phoenix":
		return ProtocolPhoenix
	case "metaplex":
		return ProtocolMetaplex

	default:
		return ProtocolType(s)
	}
}

// DefaultConfig returns a default configuration with major chains enabled
func DefaultConfig() *Config {
	return &Config{
		MaxConcurrentChains: 100,
		DefaultBatchSize:    100,
		DefaultPollInterval: 5 * time.Second,
		RetryAttempts:       3,
		RetryDelay:          time.Second,
		Chains: []ChainConfig{
			// Lux
			{ID: "lux_cchain", ChainID: 96369, Name: "Lux C-Chain", Symbol: "LUX", Type: ChainTypeEVM, RPC: "https://api.lux.network/ext/bc/C/rpc", Enabled: true},
			{ID: "lux_testnet", ChainID: 96368, Name: "Lux Testnet", Symbol: "LUX", Type: ChainTypeEVM, RPC: "https://api.testnet.lux.network/ext/bc/C/rpc", Enabled: true},

			// Ethereum Ecosystem
			{ID: "ethereum", ChainID: 1, Name: "Ethereum", Symbol: "ETH", Type: ChainTypeEVM, RPC: "https://eth.llamarpc.com", Enabled: true},
			{ID: "polygon", ChainID: 137, Name: "Polygon", Symbol: "MATIC", Type: ChainTypeEVM, RPC: "https://polygon.llamarpc.com", Enabled: true},
			{ID: "arbitrum", ChainID: 42161, Name: "Arbitrum One", Symbol: "ETH", Type: ChainTypeEVM, RPC: "https://arb1.arbitrum.io/rpc", Enabled: true},
			{ID: "optimism", ChainID: 10, Name: "Optimism", Symbol: "ETH", Type: ChainTypeEVM, RPC: "https://mainnet.optimism.io", Enabled: true},
			{ID: "base", ChainID: 8453, Name: "Base", Symbol: "ETH", Type: ChainTypeEVM, RPC: "https://mainnet.base.org", Enabled: true},
			{ID: "bsc", ChainID: 56, Name: "BNB Chain", Symbol: "BNB", Type: ChainTypeEVM, RPC: "https://bsc-dataseed1.binance.org", Enabled: true},
			{ID: "avalanche", ChainID: 43114, Name: "Avalanche C-Chain", Symbol: "AVAX", Type: ChainTypeEVM, RPC: "https://api.avax.network/ext/bc/C/rpc", Enabled: true},

			// Solana
			{ID: "solana", ChainID: 0, Name: "Solana", Symbol: "SOL", Type: ChainTypeSolana, RPC: "https://api.mainnet-beta.solana.com", Enabled: true},

			// Bitcoin
			{ID: "bitcoin", ChainID: 0, Name: "Bitcoin", Symbol: "BTC", Type: ChainTypeBitcoin, RPC: "http://localhost:8332", Enabled: false}, // Requires local node
		},
	}
}
