// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package defi provides specialized indexing for DeFi protocols native to Lux Network.
// Supports AMM (Uniswap V2/V3 style), LSSVM (Sudoswap-style NFT AMM), Perpetuals (GMX-style),
// Synthetics (Alchemix-style), Staking, and cross-chain Bridge operations.
package defi

import (
	"math/big"
	"time"
)

// Common event signatures (keccak256 hashes)
var (
	// ERC20 Transfer
	TransferSig = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	// ERC20 Approval
	ApprovalSig = "0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925"

	// AMM V2 Events
	AMMV2SwapSig        = "0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822"
	AMMV2MintSig        = "0x4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f"
	AMMV2BurnSig        = "0xdccd412f0b1252819cb1fd330b93224ca42612892bb3f4f789976e6d81936496"
	AMMV2SyncSig        = "0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1"
	AMMV2PairCreatedSig = "0x0d3648bd0f6ba80134a33ba9275ac585d9d315f0ad8355cddefde31afa28d0e9"

	// AMM V3 Events
	AMMV3SwapSig        = "0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67"
	AMMV3MintSig        = "0x7a53080ba414158be7ec69b987b5fb7d07dee101fe85488f0853ae16239d0bde"
	AMMV3BurnSig        = "0x0c396cd989a39f4459b5fa1aed6a9a8dcdbc45908acfd67e028cd568da98982c"
	AMMV3CollectSig     = "0x70935338e69775456a85ddef226c395fb668b63fa0115f5f20610b388e6ca9c0"
	AMMV3FlashSig       = "0xbdbdb71d7860376ba52b25a5028beea23581364a40522f6bcfb86bb1f2dca633"
	AMMV3InitializeSig  = "0x98636036cb66a9c19a37435efc1e90142190214e8abeb821bdba3f2990dd4c95"
	AMMV3PoolCreatedSig = "0x783cca1c0412dd0d695e784568c96da2e9c22ff989357a2e8b1d9b2b4e6b7118"

	// LSSVM Events
	LSSVMSwapNFTInSig       = "0x3614eb567740a0ee3897c0e2b11ad6a5720d2e4438f9c8accf6c95c24af3a470"
	LSSVMSwapNFTOutSig      = "0x7e2b9ee89c5c4b23ae5693bbcc16e30be8cfa5b3c6e33d2ef33e7c4be1c7b374"
	LSSVMSpotPriceSig       = "0x9a3440b7c3b8a8b3e0cca8c5e5f5b3a5e7d9c1a3b5c7d9e1f3a5b7c9d1e3f5a7"
	LSSVMSpotPriceUpdateSig = "0x9a3440b7c3b8a8b3e0cca8c5e5f5b3a5e7d9c1a3b5c7d9e1f3a5b7c9d1e3f5a7" // Alias for SpotPrice
	LSSVMDeltaUpdateSig     = "0x2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c"
	LSSVMFeeUpdateSig       = "0x5c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d"
	LSSVMPairCreatedSig     = "0x4b3c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c"
	LSSVMTokenDepositSig    = "0x6d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e"
	LSSVMTokenWithdrawSig   = "0x7e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f"
	LSSVMNFTDepositSig      = "0x8f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a"
	LSSVMNFTWithdrawSig     = "0x9a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b"

	// Perps Events
	PerpsIncreasePositionSig  = "0x2fe68525253654c21998f35787a8d0f361905ef647c854092430ab65f2f15022"
	PerpsDecreasePositionSig  = "0x93d75d64d1f84fc6f430a64fc578bdd4c1e090e90ea2d51773e626d19de56d30"
	PerpsLiquidatePositionSig = "0x2e1f85a64a2f22cf2f0c7ada1c5a1e5a8f9c1b3d5e7f9a1b3c5d7e9f1a3b5c7d"
	PerpsClosePositionSig     = "0x3f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a"
	PerpsSwapSig              = "0x0874b2d545cb271cdbda4e093020c452328b24af12382ed62c4f00695b8f2f30"
	PerpsCollectMarginFeesSig = "0x4a2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a"
	PerpsUpdateFundingRateSig = "0x5b3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b"
	PerpsCreateOrderSig       = "0x6c4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c"
	PerpsCancelOrderSig       = "0x7d5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d"
	PerpsExecuteOrderSig      = "0x8e6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e"

	// Synths Events
	SynthsDepositSig     = "0x5548c837ab068cf56a2c2479df0882a4922fd203edb7517321831d95078c5f62"
	SynthsWithdrawSig    = "0x02f25270a4d87bea75db541cdfe559334a275b4a233520ed6c0a2429667cca94"
	SynthsMintSig        = "0xab8530f87dc9b59234c4623bf917212bb2536d647574c8e7e5da92c2ede0c9f8"
	SynthsBurnSig        = "0xcc16f5dbb4873280815c1ee09dbd06736cffcc184412cf7a71a0fdb75d397ca5"
	SynthsTransmuteSig   = "0x4a1a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a"
	SynthsLiquidateSig   = "0x9f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a"
	SynthsRepayDebtSig   = "0xa08b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b"
	SynthsHarvestSig     = "0xb19c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c"
	SynthsClaimSig       = "0xc2a0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0"
	SynthsExchangeSig    = "0xd3b1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1"
	SynthsYieldUpdateSig = "0xe4c2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2"

	// Staking Events
	StakingStakedSig             = "0x9e71bc8eea02a63969f509818f2dafb9254532904319f9dbda79b67bd34a5f3d"
	StakingUnstakedSig           = "0x0f5bb82176feb1b5e747e28471aa92156a04d9f3f48cb6e6e3d8d6b3f3f4e5a6"
	StakingRewardsSig            = "0xe2403640ba68fed3a2f88b7557551d1993f84b99bb10ff833f0cf8db0c5e0486"
	StakingCooldownSig           = "0x1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c"
	StakingClaimedSig            = "0xf5d3e4f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4"
	StakingCooldownStartedSig    = "0x06e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4"
	StakingCooldownCancelledSig  = "0x17f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5"
	StakingSlashedSig            = "0x28a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6"
	StakingRewardsDistributedSig = "0x39b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7"
	StakingPoolCreatedSig        = "0x4ac8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8"
	StakingDelegatedSig          = "0x5bd9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9"
	StakingUndelegatedSig        = "0x6ce0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0"

	// Bridge Events (basic - more specific signatures in bridge.go)
	BridgeDepositSig  = "0x5445f318f4f5fcfb66592e68e0cc5822aa15664039bd5f0ffde24c5a8142b1ac"
	BridgeWithdrawSig = "0xda0bc8b9b52da793ed5ee3297fcf8ae6f8a9cc9f74f7f0a58c94b3d36e5dce23"
	BridgeTransferSig = "0x2849b43074093a05ba57f9cf2dbc5a9b2a86e6a8c7e8f9d0a1b2c3d4e5f6a7b8"
	BridgeMessageSig  = "0x3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b"
	// Note: TokenBridge* and NativeBridge* signatures are defined in bridge.go
)

// ProtocolType identifies the DeFi protocol type
type ProtocolType string

const (
	ProtocolAMMV2   ProtocolType = "amm_v2"
	ProtocolAMMV3   ProtocolType = "amm_v3"
	ProtocolLSSVM   ProtocolType = "lssvm"
	ProtocolPerps   ProtocolType = "perps"
	ProtocolSynths  ProtocolType = "synths"
	ProtocolStaking ProtocolType = "staking"
	ProtocolBridge  ProtocolType = "bridge"
	ProtocolLending ProtocolType = "lending"
)

// DeFiEvent represents a generic DeFi event
type DeFiEvent struct {
	ID              string       `json:"id"`
	Protocol        ProtocolType `json:"protocol"`
	EventType       string       `json:"eventType"`
	ContractAddress string       `json:"contractAddress"`
	TxHash          string       `json:"txHash"`
	BlockNumber     uint64       `json:"blockNumber"`
	LogIndex        uint64       `json:"logIndex"`
	Timestamp       time.Time    `json:"timestamp"`
	From            string       `json:"from,omitempty"`
	To              string       `json:"to,omitempty"`
	Data            interface{}  `json:"data"` // Protocol-specific data
}

// Pool represents a liquidity pool (AMM V2/V3, LSSVM, etc.)
type Pool struct {
	Address        string       `json:"address"`
	Protocol       ProtocolType `json:"protocol"`
	Token0         string       `json:"token0"`
	Token1         string       `json:"token1"`
	Token0Symbol   string       `json:"token0Symbol,omitempty"`
	Token1Symbol   string       `json:"token1Symbol,omitempty"`
	Token0Decimals uint8        `json:"token0Decimals,omitempty"`
	Token1Decimals uint8        `json:"token1Decimals,omitempty"`
	Fee            uint64       `json:"fee"`                   // Fee in basis points (e.g., 30 = 0.3%)
	TickSpacing    int32        `json:"tickSpacing,omitempty"` // V3 only
	Reserve0       *big.Int     `json:"reserve0"`
	Reserve1       *big.Int     `json:"reserve1"`
	TotalSupply    *big.Int     `json:"totalSupply,omitempty"`  // LP token supply
	SqrtPriceX96   *big.Int     `json:"sqrtPriceX96,omitempty"` // V3 only
	CurrentTick    int32        `json:"currentTick,omitempty"`  // V3 only
	Liquidity      *big.Int     `json:"liquidity,omitempty"`    // V3 only
	TVL            *big.Float   `json:"tvl,omitempty"`
	Volume24h      *big.Float   `json:"volume24h,omitempty"`
	Volume7d       *big.Float   `json:"volume7d,omitempty"`
	Fees24h        *big.Float   `json:"fees24h,omitempty"`
	APR            float64      `json:"apr,omitempty"`
	TxCount        uint64       `json:"txCount"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`
}

// Swap represents a swap event
type Swap struct {
	ID           string       `json:"id"`
	Protocol     ProtocolType `json:"protocol"`
	PoolAddress  string       `json:"poolAddress"`
	TxHash       string       `json:"txHash"`
	BlockNumber  uint64       `json:"blockNumber"`
	LogIndex     uint64       `json:"logIndex"`
	Sender       string       `json:"sender"`
	Recipient    string       `json:"recipient"`
	TokenIn      string       `json:"tokenIn"`
	TokenOut     string       `json:"tokenOut"`
	AmountIn     *big.Int     `json:"amountIn"`
	AmountOut    *big.Int     `json:"amountOut"`
	SqrtPriceX96 *big.Int     `json:"sqrtPriceX96,omitempty"` // V3 only
	Tick         int32        `json:"tick,omitempty"`         // V3 only
	Liquidity    *big.Int     `json:"liquidity,omitempty"`    // V3 only
	Fee          *big.Int     `json:"fee,omitempty"`
	PriceImpact  float64      `json:"priceImpact,omitempty"`
	Timestamp    time.Time    `json:"timestamp"`
}

// LiquidityEvent represents add/remove liquidity
type LiquidityEvent struct {
	ID          string       `json:"id"`
	Protocol    ProtocolType `json:"protocol"`
	EventType   string       `json:"eventType"` // "add" or "remove"
	PoolAddress string       `json:"poolAddress"`
	TxHash      string       `json:"txHash"`
	BlockNumber uint64       `json:"blockNumber"`
	LogIndex    uint64       `json:"logIndex"`
	Provider    string       `json:"provider"`
	Token0      string       `json:"token0"`
	Token1      string       `json:"token1"`
	Amount0     *big.Int     `json:"amount0"`
	Amount1     *big.Int     `json:"amount1"`
	LPTokens    *big.Int     `json:"lpTokens,omitempty"`  // V2 only
	TickLower   int32        `json:"tickLower,omitempty"` // V3 only
	TickUpper   int32        `json:"tickUpper,omitempty"` // V3 only
	Liquidity   *big.Int     `json:"liquidity,omitempty"` // V3 only
	Timestamp   time.Time    `json:"timestamp"`
}

// Position represents a DeFi position (LP, perps, staking, etc.)
type Position struct {
	ID               string       `json:"id"`
	Protocol         ProtocolType `json:"protocol"`
	PositionType     string       `json:"positionType"` // "lp", "perp_long", "perp_short", "stake", etc.
	Owner            string       `json:"owner"`
	PoolAddress      string       `json:"poolAddress,omitempty"`
	Token0           string       `json:"token0,omitempty"`
	Token1           string       `json:"token1,omitempty"`
	Amount0          *big.Int     `json:"amount0,omitempty"`
	Amount1          *big.Int     `json:"amount1,omitempty"`
	LPTokens         *big.Int     `json:"lpTokens,omitempty"`
	TickLower        int32        `json:"tickLower,omitempty"`
	TickUpper        int32        `json:"tickUpper,omitempty"`
	Liquidity        *big.Int     `json:"liquidity,omitempty"`
	CollateralToken  string       `json:"collateralToken,omitempty"`
	CollateralAmount *big.Int     `json:"collateralAmount,omitempty"`
	SizeUSD          *big.Float   `json:"sizeUsd,omitempty"`
	Leverage         float64      `json:"leverage,omitempty"`
	EntryPrice       *big.Float   `json:"entryPrice,omitempty"`
	LiquidationPrice *big.Float   `json:"liquidationPrice,omitempty"`
	UnrealizedPnL    *big.Float   `json:"unrealizedPnl,omitempty"`
	IsOpen           bool         `json:"isOpen"`
	OpenedAt         time.Time    `json:"openedAt"`
	ClosedAt         *time.Time   `json:"closedAt,omitempty"`
	UpdatedAt        time.Time    `json:"updatedAt"`
}

// MarketData represents market data for a token or pool
type MarketData struct {
	Address        string     `json:"address"`
	Symbol         string     `json:"symbol"`
	Price          *big.Float `json:"price"`
	PriceChange24h float64    `json:"priceChange24h"`
	Volume24h      *big.Float `json:"volume24h"`
	MarketCap      *big.Float `json:"marketCap,omitempty"`
	Liquidity      *big.Float `json:"liquidity"`
	High24h        *big.Float `json:"high24h"`
	Low24h         *big.Float `json:"low24h"`
	TxCount24h     uint64     `json:"txCount24h"`
	Timestamp      time.Time  `json:"timestamp"`
}

// MarketHistory represents historical market data
type MarketHistory struct {
	Address   string     `json:"address"`
	Timestamp time.Time  `json:"timestamp"`
	Open      *big.Float `json:"open"`
	High      *big.Float `json:"high"`
	Low       *big.Float `json:"low"`
	Close     *big.Float `json:"close"`
	Volume    *big.Float `json:"volume"`
}

// Note: BridgeTransfer and AssetMovement types are defined in bridge.go
// with more detailed fields for comprehensive bridge/cross-chain indexing

// ProtocolStats represents aggregate protocol statistics
type ProtocolStats struct {
	Protocol       ProtocolType `json:"protocol"`
	TVL            *big.Float   `json:"tvl"`
	Volume24h      *big.Float   `json:"volume24h"`
	Volume7d       *big.Float   `json:"volume7d"`
	Fees24h        *big.Float   `json:"fees24h"`
	Fees7d         *big.Float   `json:"fees7d"`
	TxCount24h     uint64       `json:"txCount24h"`
	TxCount7d      uint64       `json:"txCount7d"`
	UniqueUsers24h uint64       `json:"uniqueUsers24h"`
	UniqueUsers7d  uint64       `json:"uniqueUsers7d"`
	PoolCount      uint64       `json:"poolCount"`
	UpdatedAt      time.Time    `json:"updatedAt"`
}
