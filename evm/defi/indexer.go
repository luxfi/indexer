// Package defi provides comprehensive DeFi protocol indexing for the Lux EVM indexer.
//
// This package indexes all major DeFi protocols native to the Lux ecosystem:
//   - AMM V2/V3 (Uniswap-style constant product and concentrated liquidity)
//   - LSSVM (Sudoswap-style NFT AMM)
//   - Perpetuals (GMX-style perpetual futures)
//   - Synthetics (Alchemix-style self-repaying loans)
//   - Staking (with cooldown periods and receipt tokens)
//   - Bridge/Cross-chain (Warp, Teleporter, Token Bridge)
//   - Order Book (CLOB with margin, lending, positions)
//   - Market History (OHLCV candles, tickers, volume stats)
package defi

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"
)

// Note: LogEntry is defined in amm.go as the canonical definition

// DeFiIndexer is the unified indexer for all DeFi protocols
type DeFiIndexer struct {
	mu            sync.RWMutex
	chainID       ChainID
	amm           *AMMIndexer
	lssvm         *LSSVMIndexer
	perps         *PerpsIndexer
	synths        *SynthsIndexer
	staking       *StakingIndexer
	bridge        *BridgeIndexer
	orderbook     *OrderBookIndexer
	marketHistory *MarketHistoryIndexer
	nftMarketplace *NFTMarketplaceIndexer

	// Event routing
	eventHandlers map[string]func(*LogEntry) error

	// Stats
	stats *DeFiStats
}

// DeFiStats tracks indexer statistics
type DeFiStats struct {
	EventsProcessed  uint64            `json:"eventsProcessed"`
	EventsByType     map[string]uint64 `json:"eventsByType"`
	LastBlockIndexed uint64            `json:"lastBlockIndexed"`
	LastIndexTime    time.Time         `json:"lastIndexTime"`
	ErrorCount       uint64            `json:"errorCount"`
	StartTime        time.Time         `json:"startTime"`
}

// NewDeFiIndexer creates a new unified DeFi indexer
func NewDeFiIndexer(chainID ChainID) *DeFiIndexer {
	idx := &DeFiIndexer{
		chainID:        chainID,
		amm:            NewAMMIndexer(),
		lssvm:          NewLSSVMIndexer(),
		perps:          NewPerpsIndexer(),
		synths:         NewSynthsIndexer(),
		staking:        NewStakingIndexer(),
		bridge:         NewBridgeIndexer(chainID),
		orderbook:      NewOrderBookIndexer(),
		marketHistory:  NewMarketHistoryIndexer(),
		nftMarketplace: NewNFTMarketplaceIndexer(),
		eventHandlers:  make(map[string]func(*LogEntry) error),
		stats: &DeFiStats{
			EventsByType: make(map[string]uint64),
			StartTime:    time.Now(),
		},
	}

	// Register all event handlers
	idx.registerEventHandlers()

	return idx
}

// ammIndexLogWrapper wraps AMM's context-aware IndexLog to unified interface
func (d *DeFiIndexer) ammIndexLogWrapper(log *LogEntry) error {
	return d.amm.IndexLog(context.Background(), log)
}

// registerEventHandlers sets up routing for all event signatures
func (d *DeFiIndexer) registerEventHandlers() {
	// AMM V2 events
	d.eventHandlers[AMMV2SwapSig] = d.ammIndexLogWrapper
	d.eventHandlers[AMMV2MintSig] = d.ammIndexLogWrapper
	d.eventHandlers[AMMV2BurnSig] = d.ammIndexLogWrapper
	d.eventHandlers[AMMV2SyncSig] = d.ammIndexLogWrapper
	d.eventHandlers[AMMV2PairCreatedSig] = d.ammIndexLogWrapper

	// AMM V3 events
	d.eventHandlers[AMMV3SwapSig] = d.ammIndexLogWrapper
	d.eventHandlers[AMMV3MintSig] = d.ammIndexLogWrapper
	d.eventHandlers[AMMV3BurnSig] = d.ammIndexLogWrapper
	d.eventHandlers[AMMV3CollectSig] = d.ammIndexLogWrapper
	d.eventHandlers[AMMV3FlashSig] = d.ammIndexLogWrapper
	d.eventHandlers[AMMV3PoolCreatedSig] = d.ammIndexLogWrapper

	// LSSVM events
	d.eventHandlers[LSSVMSwapNFTInSig] = d.lssvm.IndexLog
	d.eventHandlers[LSSVMSwapNFTOutSig] = d.lssvm.IndexLog
	d.eventHandlers[LSSVMSpotPriceUpdateSig] = d.lssvm.IndexLog
	d.eventHandlers[LSSVMDeltaUpdateSig] = d.lssvm.IndexLog
	d.eventHandlers[LSSVMFeeUpdateSig] = d.lssvm.IndexLog
	d.eventHandlers[LSSVMPairCreatedSig] = d.lssvm.IndexLog
	d.eventHandlers[LSSVMTokenDepositSig] = d.lssvm.IndexLog
	d.eventHandlers[LSSVMTokenWithdrawSig] = d.lssvm.IndexLog
	d.eventHandlers[LSSVMNFTDepositSig] = d.lssvm.IndexLog
	d.eventHandlers[LSSVMNFTWithdrawSig] = d.lssvm.IndexLog

	// Perps events
	d.eventHandlers[PerpsIncreasePositionSig] = d.perps.IndexLog
	d.eventHandlers[PerpsDecreasePositionSig] = d.perps.IndexLog
	d.eventHandlers[PerpsLiquidatePositionSig] = d.perps.IndexLog
	d.eventHandlers[PerpsClosePositionSig] = d.perps.IndexLog
	d.eventHandlers[PerpsCollectMarginFeesSig] = d.perps.IndexLog
	d.eventHandlers[PerpsUpdateFundingRateSig] = d.perps.IndexLog
	d.eventHandlers[PerpsSwapSig] = d.perps.IndexLog
	d.eventHandlers[PerpsCreateOrderSig] = d.perps.IndexLog
	d.eventHandlers[PerpsCancelOrderSig] = d.perps.IndexLog
	d.eventHandlers[PerpsExecuteOrderSig] = d.perps.IndexLog

	// Synths events
	d.eventHandlers[SynthsDepositSig] = d.synths.IndexLog
	d.eventHandlers[SynthsWithdrawSig] = d.synths.IndexLog
	d.eventHandlers[SynthsMintSig] = d.synths.IndexLog
	d.eventHandlers[SynthsBurnSig] = d.synths.IndexLog
	d.eventHandlers[SynthsLiquidateSig] = d.synths.IndexLog
	d.eventHandlers[SynthsRepayDebtSig] = d.synths.IndexLog
	d.eventHandlers[SynthsHarvestSig] = d.synths.IndexLog
	d.eventHandlers[SynthsTransmuteSig] = d.synths.IndexLog
	d.eventHandlers[SynthsClaimSig] = d.synths.IndexLog
	d.eventHandlers[SynthsExchangeSig] = d.synths.IndexLog
	d.eventHandlers[SynthsYieldUpdateSig] = d.synths.IndexLog

	// Staking events
	d.eventHandlers[StakingStakedSig] = d.staking.IndexLog
	d.eventHandlers[StakingUnstakedSig] = d.staking.IndexLog
	d.eventHandlers[StakingClaimedSig] = d.staking.IndexLog
	d.eventHandlers[StakingCooldownStartedSig] = d.staking.IndexLog
	d.eventHandlers[StakingCooldownCancelledSig] = d.staking.IndexLog
	d.eventHandlers[StakingSlashedSig] = d.staking.IndexLog
	d.eventHandlers[StakingRewardsDistributedSig] = d.staking.IndexLog
	d.eventHandlers[StakingPoolCreatedSig] = d.staking.IndexLog
	d.eventHandlers[StakingDelegatedSig] = d.staking.IndexLog
	d.eventHandlers[StakingUndelegatedSig] = d.staking.IndexLog

	// Bridge events
	d.eventHandlers[WarpMessageSentSig] = d.bridge.IndexLog
	d.eventHandlers[WarpMessageReceivedSig] = d.bridge.IndexLog
	d.eventHandlers[TokenBridgeDepositSig] = d.bridge.IndexLog
	d.eventHandlers[TokenBridgeWithdrawSig] = d.bridge.IndexLog
	d.eventHandlers[TokenBridgeMintSig] = d.bridge.IndexLog
	d.eventHandlers[TokenBridgeBurnSig] = d.bridge.IndexLog
	d.eventHandlers[NativeBridgeLockSig] = d.bridge.IndexLog
	d.eventHandlers[NativeBridgeUnlockSig] = d.bridge.IndexLog
	d.eventHandlers[CrossChainSwapInitSig] = d.bridge.IndexLog
	d.eventHandlers[CrossChainSwapCompleteSig] = d.bridge.IndexLog
	d.eventHandlers[CrossChainSwapRefundSig] = d.bridge.IndexLog
	d.eventHandlers[TeleporterSendSig] = d.bridge.IndexLog
	d.eventHandlers[TeleporterReceiveSig] = d.bridge.IndexLog

	// Order book events
	d.eventHandlers[OrderPlacedSig] = d.orderbook.IndexLog
	d.eventHandlers[OrderCancelledSig] = d.orderbook.IndexLog
	d.eventHandlers[OrderFilledSig] = d.orderbook.IndexLog
	d.eventHandlers[OrderPartialSig] = d.orderbook.IndexLog
	d.eventHandlers[TradeSig] = d.orderbook.IndexLog
	d.eventHandlers[TradeSettledSig] = d.orderbook.IndexLog
	d.eventHandlers[PositionOpenedSig] = d.orderbook.IndexLog
	d.eventHandlers[PositionClosedSig] = d.orderbook.IndexLog
	d.eventHandlers[PositionModifiedSig] = d.orderbook.IndexLog
	d.eventHandlers[PositionLiquidatedSig] = d.orderbook.IndexLog
	d.eventHandlers[MarginDepositedSig] = d.orderbook.IndexLog
	d.eventHandlers[MarginWithdrawnSig] = d.orderbook.IndexLog
	d.eventHandlers[MarginCallSig] = d.orderbook.IndexLog
	d.eventHandlers[LendingDepositSig] = d.orderbook.IndexLog
	d.eventHandlers[LendingWithdrawSig] = d.orderbook.IndexLog
	d.eventHandlers[LendingBorrowSig] = d.orderbook.IndexLog
	d.eventHandlers[LendingRepaySig] = d.orderbook.IndexLog
	d.eventHandlers[FundingRateSig] = d.orderbook.IndexLog
	d.eventHandlers[FundingPaymentSig] = d.orderbook.IndexLog

	// NFT Marketplace events (OpenSea Seaport, LooksRare, Blur, Rarible, X2Y2, Zora)
	d.eventHandlers[SeaportOrderFulfilledSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[SeaportOrderCancelledSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[SeaportCounterIncrementSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[LooksRareTakerBidSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[LooksRareTakerAskSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[LooksRareCancelAllSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[LooksRareV2TakerBidSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[LooksRareV2TakerAskSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[BlurOrdersMatchedSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[BlurOrderCancelledSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[BlurNonceIncrementedSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[RaribleMatchSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[RaribleCancelSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[X2Y2InventorySig] = d.nftMarketplace.IndexLog
	d.eventHandlers[X2Y2CancelSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[ZoraAskFilledSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[ZoraAskCreatedSig] = d.nftMarketplace.IndexLog
	d.eventHandlers[ZoraAskCancelledSig] = d.nftMarketplace.IndexLog
}

// IndexLog processes a single log entry
func (d *DeFiIndexer) IndexLog(log *LogEntry) error {
	if len(log.Topics) == 0 {
		return nil
	}

	topic0 := log.Topics[0]

	d.mu.Lock()
	d.stats.EventsProcessed++
	d.stats.EventsByType[topic0]++
	d.stats.LastBlockIndexed = log.BlockNumber
	d.stats.LastIndexTime = time.Now()
	d.mu.Unlock()

	// Route to appropriate handler
	if handler, ok := d.eventHandlers[topic0]; ok {
		if err := handler(log); err != nil {
			d.mu.Lock()
			d.stats.ErrorCount++
			d.mu.Unlock()
			return fmt.Errorf("handler error for %s: %w", topic0, err)
		}
	}

	// Also record to market history if it's a trade event
	if d.isTradeEvent(topic0) {
		d.recordTradeHistory(log)
	}

	return nil
}

// IndexLogs processes multiple log entries
func (d *DeFiIndexer) IndexLogs(logs []*LogEntry) error {
	for _, log := range logs {
		if err := d.IndexLog(log); err != nil {
			// Log error but continue processing
			continue
		}
	}
	return nil
}

// isTradeEvent checks if an event represents a trade
func (d *DeFiIndexer) isTradeEvent(topic0 string) bool {
	tradeEvents := map[string]bool{
		AMMV2SwapSig:       true,
		AMMV3SwapSig:       true,
		LSSVMSwapNFTInSig:  true,
		LSSVMSwapNFTOutSig: true,
		TradeSig:           true,
	}
	return tradeEvents[topic0]
}

// recordTradeHistory records trade to market history
func (d *DeFiIndexer) recordTradeHistory(log *LogEntry) {
	// Extract trade data based on event type
	topic0 := log.Topics[0]

	var trade *TradeHistory

	switch topic0 {
	case AMMV2SwapSig:
		trade = d.extractAMMV2Trade(log)
	case AMMV3SwapSig:
		trade = d.extractAMMV3Trade(log)
	case TradeSig:
		trade = d.extractOrderBookTrade(log)
	}

	if trade != nil {
		d.marketHistory.RecordTrade(trade)
	}
}

// extractAMMV2Trade extracts trade from AMM V2 swap
func (d *DeFiIndexer) extractAMMV2Trade(log *LogEntry) *TradeHistory {
	if len(log.Topics) < 3 || len(log.Data) < 256 {
		return nil
	}

	// Data: amount0In, amount1In, amount0Out, amount1Out
	amount0In := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))
	amount1In := new(big.Int).SetBytes(hexToBytes(log.Data[64:128]))
	amount0Out := new(big.Int).SetBytes(hexToBytes(log.Data[128:192]))
	amount1Out := new(big.Int).SetBytes(hexToBytes(log.Data[192:256]))

	var size, quoteSize *big.Int
	var side OrderSide

	if amount0In.Sign() > 0 {
		side = OrderSideSell
		size = amount0In
		quoteSize = amount1Out
	} else {
		side = OrderSideBuy
		size = amount0Out
		quoteSize = amount1In
	}

	price := big.NewInt(0)
	if size.Sign() > 0 {
		price = new(big.Int).Mul(quoteSize, big.NewInt(1e18))
		price.Div(price, size)
	}

	return &TradeHistory{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		Market:      log.Address,
		Price:       price,
		Size:        size,
		QuoteSize:   quoteSize,
		Side:        side,
		Timestamp:   log.Timestamp,
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		Taker:       "0x" + log.Topics[2][26:],
	}
}

// extractAMMV3Trade extracts trade from AMM V3 swap
func (d *DeFiIndexer) extractAMMV3Trade(log *LogEntry) *TradeHistory {
	if len(log.Topics) < 3 || len(log.Data) < 256 {
		return nil
	}

	// Data: amount0, amount1, sqrtPriceX96, liquidity, tick
	amount0Bytes := hexToBytes(log.Data[0:64])
	amount1Bytes := hexToBytes(log.Data[64:128])

	// Handle signed integers
	amount0 := new(big.Int).SetBytes(amount0Bytes)
	if amount0Bytes[0]&0x80 != 0 {
		amount0.Sub(amount0, new(big.Int).Lsh(big.NewInt(1), 256))
	}
	amount1 := new(big.Int).SetBytes(amount1Bytes)
	if amount1Bytes[0]&0x80 != 0 {
		amount1.Sub(amount1, new(big.Int).Lsh(big.NewInt(1), 256))
	}

	var size, quoteSize *big.Int
	var side OrderSide

	if amount0.Sign() > 0 {
		side = OrderSideSell
		size = amount0
		quoteSize = new(big.Int).Neg(amount1)
	} else {
		side = OrderSideBuy
		size = new(big.Int).Neg(amount0)
		quoteSize = amount1
	}

	price := big.NewInt(0)
	if size.Sign() > 0 {
		price = new(big.Int).Mul(quoteSize, big.NewInt(1e18))
		price.Div(price, size)
		if price.Sign() < 0 {
			price.Neg(price)
		}
	}

	return &TradeHistory{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		Market:      log.Address,
		Price:       price,
		Size:        size,
		QuoteSize:   quoteSize,
		Side:        side,
		Timestamp:   log.Timestamp,
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		Taker:       "0x" + log.Topics[2][26:],
	}
}

// extractOrderBookTrade extracts trade from order book
func (d *DeFiIndexer) extractOrderBookTrade(log *LogEntry) *TradeHistory {
	if len(log.Topics) < 4 || len(log.Data) < 192 {
		return nil
	}

	price := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))
	size := new(big.Int).SetBytes(hexToBytes(log.Data[64:128]))
	sideRaw := new(big.Int).SetBytes(hexToBytes(log.Data[128:192])).Uint64()

	side := OrderSideBuy
	if sideRaw == 1 {
		side = OrderSideSell
	}

	quoteSize := new(big.Int).Mul(price, size)
	quoteSize.Div(quoteSize, big.NewInt(1e18))

	return &TradeHistory{
		ID:          log.Topics[1],
		Market:      log.Address,
		Price:       price,
		Size:        size,
		QuoteSize:   quoteSize,
		Side:        side,
		Timestamp:   log.Timestamp,
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
	}
}

// Accessor methods

// AMM returns the AMM indexer
func (d *DeFiIndexer) AMM() *AMMIndexer {
	return d.amm
}

// LSSVM returns the LSSVM indexer
func (d *DeFiIndexer) LSSVM() *LSSVMIndexer {
	return d.lssvm
}

// Perps returns the perpetuals indexer
func (d *DeFiIndexer) Perps() *PerpsIndexer {
	return d.perps
}

// Synths returns the synthetics indexer
func (d *DeFiIndexer) Synths() *SynthsIndexer {
	return d.synths
}

// Staking returns the staking indexer
func (d *DeFiIndexer) Staking() *StakingIndexer {
	return d.staking
}

// Bridge returns the bridge indexer
func (d *DeFiIndexer) Bridge() *BridgeIndexer {
	return d.bridge
}

// OrderBook returns the order book indexer
func (d *DeFiIndexer) OrderBook() *OrderBookIndexer {
	return d.orderbook
}

// MarketHistory returns the market history indexer
func (d *DeFiIndexer) MarketHistory() *MarketHistoryIndexer {
	return d.marketHistory
}

// NFTMarketplace returns the NFT marketplace indexer
func (d *DeFiIndexer) NFTMarketplace() *NFTMarketplaceIndexer {
	return d.nftMarketplace
}

// Stats returns indexer statistics
func (d *DeFiIndexer) Stats() *DeFiStats {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.stats
}

// Start starts background monitoring
func (d *DeFiIndexer) Start(ctx context.Context) {
	// Start bridge pending monitor
	go d.bridge.StartPendingMonitor(ctx, 30*time.Second)

	// Start market history cleanup
	go d.startCleanup(ctx)
}

// startCleanup runs periodic cleanup
func (d *DeFiIndexer) startCleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.marketHistory.Cleanup(7 * 24 * time.Hour)
		}
	}
}

// GetOverview returns a comprehensive DeFi overview
func (d *DeFiIndexer) GetOverview() *DeFiOverview {
	return &DeFiOverview{
		ChainID:           d.chainID,
		Stats:             d.Stats(),
		AMMStats:          d.amm.GetStats(),
		LSSVMStats:        d.lssvm.GetStats(),
		PerpsStats:        d.perps.GetStats(),
		SynthsStats:       d.synths.GetStats(),
		StakingStats:      d.staking.GetStats(),
		BridgeStats:       d.bridge.GetStats(),
		MarketStats:       d.marketHistory.Stats(),
		TopMarkets:        d.marketHistory.GetTopMarkets(10),
		NFTMarketStats:    d.nftMarketplace.GetStats(),
		TopNFTCollections: d.nftMarketplace.GetTopCollections(10),
		Timestamp:         time.Now(),
	}
}

// DeFiOverview provides a comprehensive DeFi summary
type DeFiOverview struct {
	ChainID           ChainID                `json:"chainId"`
	Stats             *DeFiStats             `json:"stats"`
	AMMStats          *AMMStats              `json:"ammStats"`
	LSSVMStats        *LSSVMStats            `json:"lssvmStats"`
	PerpsStats        *PerpsStats            `json:"perpsStats"`
	SynthsStats       *SynthsStats           `json:"synthsStats"`
	StakingStats      *StakingStats          `json:"stakingStats"`
	BridgeStats       *BridgeStats           `json:"bridgeStats"`
	MarketStats       map[string]interface{} `json:"marketStats"`
	TopMarkets        []*MarketTicker        `json:"topMarkets"`
	NFTMarketStats    *NFTMarketplaceStats   `json:"nftMarketStats"`
	TopNFTCollections []*NFTCollection       `json:"topNftCollections"`
	Timestamp         time.Time              `json:"timestamp"`
}
