// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package defi

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// PerpsIndexer indexes perpetual futures trading events (GMX-style + LX DEX)
type PerpsIndexer struct {
	positions        map[string]*PerpPosition
	trades           []*PerpTrade
	liquidations     []*PerpLiquidation
	fundingUpdates   []*FundingUpdate
	vaultDeposits    []*VaultDeposit
	vaultWithdrawals []*VaultWithdrawal
	markets          map[string]*PerpMarket
	onTrade          func(*PerpTrade)
	onLiquidation    func(*PerpLiquidation)
	onPosition       func(*PerpPosition)
}

// PerpMarket represents a perpetual market
type PerpMarket struct {
	Address           string    `json:"address"`
	IndexToken        string    `json:"indexToken"`
	IndexSymbol       string    `json:"indexSymbol,omitempty"`
	LongToken         string    `json:"longToken"`
	ShortToken        string    `json:"shortToken"`
	MaxLeverage       float64   `json:"maxLeverage"`
	FundingRate       *big.Int  `json:"fundingRate"`
	OpenInterestLong  *big.Int  `json:"openInterestLong"`
	OpenInterestShort *big.Int  `json:"openInterestShort"`
	ReservedAmount    *big.Int  `json:"reservedAmount"`
	PoolAmount        *big.Int  `json:"poolAmount"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// PerpPosition represents a perpetual position
type PerpPosition struct {
	ID                string     `json:"id"` // account:collateralToken:indexToken:isLong
	Account           string     `json:"account"`
	CollateralToken   string     `json:"collateralToken"`
	IndexToken        string     `json:"indexToken"`
	IsLong            bool       `json:"isLong"`
	Size              *big.Int   `json:"size"`       // Position size in USD
	Collateral        *big.Int   `json:"collateral"` // Collateral amount
	AveragePrice      *big.Int   `json:"averagePrice"`
	EntryFundingRate  *big.Int   `json:"entryFundingRate"`
	ReserveAmount     *big.Int   `json:"reserveAmount"`
	RealisedPnL       *big.Int   `json:"realisedPnl"`
	LastIncreasedTime time.Time  `json:"lastIncreasedTime"`
	IsOpen            bool       `json:"isOpen"`
	Leverage          float64    `json:"leverage"`
	LiquidationPrice  *big.Float `json:"liquidationPrice,omitempty"`
	UnrealisedPnL     *big.Int   `json:"unrealisedPnl,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

// PerpTrade represents a perpetual trade (increase/decrease position)
type PerpTrade struct {
	ID              string    `json:"id"`
	TxHash          string    `json:"txHash"`
	BlockNumber     uint64    `json:"blockNumber"`
	LogIndex        uint64    `json:"logIndex"`
	Account         string    `json:"account"`
	CollateralToken string    `json:"collateralToken"`
	IndexToken      string    `json:"indexToken"`
	IsLong          bool      `json:"isLong"`
	TradeType       string    `json:"tradeType"` // "increase", "decrease", "swap"
	CollateralDelta *big.Int  `json:"collateralDelta"`
	SizeDelta       *big.Int  `json:"sizeDelta"`
	Price           *big.Int  `json:"price"`
	Fee             *big.Int  `json:"fee"`
	RealisedPnL     *big.Int  `json:"realisedPnl,omitempty"`
	Timestamp       time.Time `json:"timestamp"`
}

// PerpLiquidation represents a liquidation event
type PerpLiquidation struct {
	ID              string    `json:"id"`
	TxHash          string    `json:"txHash"`
	BlockNumber     uint64    `json:"blockNumber"`
	LogIndex        uint64    `json:"logIndex"`
	Account         string    `json:"account"`
	CollateralToken string    `json:"collateralToken"`
	IndexToken      string    `json:"indexToken"`
	IsLong          bool      `json:"isLong"`
	Size            *big.Int  `json:"size"`
	Collateral      *big.Int  `json:"collateral"`
	MarkPrice       *big.Int  `json:"markPrice"`
	Loss            *big.Int  `json:"loss"`
	Liquidator      string    `json:"liquidator"`
	Timestamp       time.Time `json:"timestamp"`
}

// FundingUpdate represents a funding rate update
type FundingUpdate struct {
	ID          string    `json:"id"`
	Token       string    `json:"token"`
	FundingRate *big.Int  `json:"fundingRate"`
	BlockNumber uint64    `json:"blockNumber"`
	Timestamp   time.Time `json:"timestamp"`
}

// VaultDeposit represents a vault deposit (for LP)
type VaultDeposit struct {
	ID          string    `json:"id"`
	TxHash      string    `json:"txHash"`
	BlockNumber uint64    `json:"blockNumber"`
	Account     string    `json:"account"`
	Token       string    `json:"token"`
	Amount      *big.Int  `json:"amount"`
	LPAmount    *big.Int  `json:"lpAmount"` // LP tokens minted
	Timestamp   time.Time `json:"timestamp"`
}

// VaultWithdrawal represents a vault withdrawal
type VaultWithdrawal struct {
	ID          string    `json:"id"`
	TxHash      string    `json:"txHash"`
	BlockNumber uint64    `json:"blockNumber"`
	Account     string    `json:"account"`
	Token       string    `json:"token"`
	Amount      *big.Int  `json:"amount"`
	LPAmount    *big.Int  `json:"lpAmount"` // LP tokens burned
	Timestamp   time.Time `json:"timestamp"`
}

// NewPerpsIndexer creates a new perpetuals indexer
func NewPerpsIndexer() *PerpsIndexer {
	return &PerpsIndexer{
		positions:        make(map[string]*PerpPosition),
		trades:           make([]*PerpTrade, 0),
		liquidations:     make([]*PerpLiquidation, 0),
		fundingUpdates:   make([]*FundingUpdate, 0),
		vaultDeposits:    make([]*VaultDeposit, 0),
		vaultWithdrawals: make([]*VaultWithdrawal, 0),
		markets:          make(map[string]*PerpMarket),
	}
}

// SetCallbacks sets event callbacks
func (p *PerpsIndexer) SetCallbacks(
	onTrade func(*PerpTrade),
	onLiquidation func(*PerpLiquidation),
	onPosition func(*PerpPosition),
) {
	p.onTrade = onTrade
	p.onLiquidation = onLiquidation
	p.onPosition = onPosition
}

// IndexLog processes a log entry for perps events
func (p *PerpsIndexer) IndexLog(log *LogEntry) error {
	if len(log.Topics) == 0 {
		return nil
	}

	topic0 := log.Topics[0]

	switch topic0 {
	case PerpsIncreasePositionSig:
		return p.indexIncreasePosition(log)
	case PerpsDecreasePositionSig:
		return p.indexDecreasePosition(log)
	case PerpsLiquidatePositionSig:
		return p.indexLiquidatePosition(log)
	case PerpsClosePositionSig:
		return p.indexClosePosition(log)
	case PerpsSwapSig:
		return p.indexSwap(log)
	}

	return nil
}

// indexIncreasePosition processes IncreasePosition event
func (p *PerpsIndexer) indexIncreasePosition(log *LogEntry) error {
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	if len(data) < 256 {
		return fmt.Errorf("invalid increase position data")
	}

	// IncreasePosition(bytes32 key, address account, address collateralToken, address indexToken,
	//                  uint256 collateralDelta, uint256 sizeDelta, bool isLong, uint256 price, uint256 fee)

	account := "0x" + hex.EncodeToString(data[12:32])
	collateralToken := "0x" + hex.EncodeToString(data[44:64])
	indexToken := "0x" + hex.EncodeToString(data[76:96])
	collateralDelta := new(big.Int).SetBytes(data[96:128])
	sizeDelta := new(big.Int).SetBytes(data[128:160])
	isLong := data[191] == 1
	price := new(big.Int).SetBytes(data[192:224])
	fee := new(big.Int).SetBytes(data[224:256])

	// Create or update position
	positionID := p.getPositionID(account, collateralToken, indexToken, isLong)
	position, exists := p.positions[positionID]

	if !exists {
		position = &PerpPosition{
			ID:              positionID,
			Account:         account,
			CollateralToken: collateralToken,
			IndexToken:      indexToken,
			IsLong:          isLong,
			Size:            big.NewInt(0),
			Collateral:      big.NewInt(0),
			AveragePrice:    big.NewInt(0),
			RealisedPnL:     big.NewInt(0),
			IsOpen:          true,
			CreatedAt:       log.Timestamp,
		}
		p.positions[positionID] = position
	}

	// Update position
	position.Size = new(big.Int).Add(position.Size, sizeDelta)
	position.Collateral = new(big.Int).Add(position.Collateral, collateralDelta)
	position.AveragePrice = calculateAveragePrice(position.AveragePrice, position.Size, sizeDelta, price)
	position.LastIncreasedTime = log.Timestamp
	position.UpdatedAt = log.Timestamp

	if position.Collateral.Sign() > 0 {
		sizeFloat := new(big.Float).SetInt(position.Size)
		collateralFloat := new(big.Float).SetInt(position.Collateral)
		leverage, _ := new(big.Float).Quo(sizeFloat, collateralFloat).Float64()
		position.Leverage = leverage
	}

	// Record trade
	trade := &PerpTrade{
		ID:              fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:          log.TxHash,
		BlockNumber:     log.BlockNumber,
		LogIndex:        log.LogIndex,
		Account:         account,
		CollateralToken: collateralToken,
		IndexToken:      indexToken,
		IsLong:          isLong,
		TradeType:       "increase",
		CollateralDelta: collateralDelta,
		SizeDelta:       sizeDelta,
		Price:           price,
		Fee:             fee,
		Timestamp:       log.Timestamp,
	}

	p.trades = append(p.trades, trade)

	if p.onTrade != nil {
		p.onTrade(trade)
	}

	if p.onPosition != nil {
		p.onPosition(position)
	}

	return nil
}

// indexDecreasePosition processes DecreasePosition event
func (p *PerpsIndexer) indexDecreasePosition(log *LogEntry) error {
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	if len(data) < 288 {
		return fmt.Errorf("invalid decrease position data")
	}

	account := "0x" + hex.EncodeToString(data[12:32])
	collateralToken := "0x" + hex.EncodeToString(data[44:64])
	indexToken := "0x" + hex.EncodeToString(data[76:96])
	collateralDelta := new(big.Int).SetBytes(data[96:128])
	sizeDelta := new(big.Int).SetBytes(data[128:160])
	isLong := data[191] == 1
	price := new(big.Int).SetBytes(data[192:224])
	fee := new(big.Int).SetBytes(data[224:256])
	realisedPnL := new(big.Int).SetBytes(data[256:288])

	// Update position
	positionID := p.getPositionID(account, collateralToken, indexToken, isLong)
	position, exists := p.positions[positionID]

	if exists {
		position.Size = new(big.Int).Sub(position.Size, sizeDelta)
		position.Collateral = new(big.Int).Sub(position.Collateral, collateralDelta)
		position.RealisedPnL = new(big.Int).Add(position.RealisedPnL, realisedPnL)
		position.UpdatedAt = log.Timestamp

		if position.Size.Sign() <= 0 {
			position.IsOpen = false
		}

		if p.onPosition != nil {
			p.onPosition(position)
		}
	}

	// Record trade
	trade := &PerpTrade{
		ID:              fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:          log.TxHash,
		BlockNumber:     log.BlockNumber,
		LogIndex:        log.LogIndex,
		Account:         account,
		CollateralToken: collateralToken,
		IndexToken:      indexToken,
		IsLong:          isLong,
		TradeType:       "decrease",
		CollateralDelta: collateralDelta,
		SizeDelta:       sizeDelta,
		Price:           price,
		Fee:             fee,
		RealisedPnL:     realisedPnL,
		Timestamp:       log.Timestamp,
	}

	p.trades = append(p.trades, trade)

	if p.onTrade != nil {
		p.onTrade(trade)
	}

	return nil
}

// indexLiquidatePosition processes LiquidatePosition event
func (p *PerpsIndexer) indexLiquidatePosition(log *LogEntry) error {
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	if len(data) < 256 {
		return fmt.Errorf("invalid liquidation data")
	}

	account := "0x" + hex.EncodeToString(data[12:32])
	collateralToken := "0x" + hex.EncodeToString(data[44:64])
	indexToken := "0x" + hex.EncodeToString(data[76:96])
	isLong := data[127] == 1
	size := new(big.Int).SetBytes(data[128:160])
	collateral := new(big.Int).SetBytes(data[160:192])
	markPrice := new(big.Int).SetBytes(data[192:224])
	loss := new(big.Int).SetBytes(data[224:256])

	// Close position
	positionID := p.getPositionID(account, collateralToken, indexToken, isLong)
	if position, exists := p.positions[positionID]; exists {
		position.IsOpen = false
		position.Size = big.NewInt(0)
		position.Collateral = big.NewInt(0)
		position.UpdatedAt = log.Timestamp

		if p.onPosition != nil {
			p.onPosition(position)
		}
	}

	liquidation := &PerpLiquidation{
		ID:              fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:          log.TxHash,
		BlockNumber:     log.BlockNumber,
		LogIndex:        log.LogIndex,
		Account:         account,
		CollateralToken: collateralToken,
		IndexToken:      indexToken,
		IsLong:          isLong,
		Size:            size,
		Collateral:      collateral,
		MarkPrice:       markPrice,
		Loss:            loss,
		Timestamp:       log.Timestamp,
	}

	p.liquidations = append(p.liquidations, liquidation)

	if p.onLiquidation != nil {
		p.onLiquidation(liquidation)
	}

	return nil
}

// indexClosePosition processes ClosePosition event
func (p *PerpsIndexer) indexClosePosition(log *LogEntry) error {
	// Similar to decrease with full size
	return p.indexDecreasePosition(log)
}

// indexSwap processes vault swap event
func (p *PerpsIndexer) indexSwap(log *LogEntry) error {
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	if len(data) < 192 {
		return fmt.Errorf("invalid swap data")
	}

	account := "0x" + hex.EncodeToString(data[12:32])
	tokenIn := "0x" + hex.EncodeToString(data[44:64])
	tokenOut := "0x" + hex.EncodeToString(data[76:96])
	amountIn := new(big.Int).SetBytes(data[96:128])
	amountOut := new(big.Int).SetBytes(data[128:160])
	fee := new(big.Int).SetBytes(data[160:192])

	trade := &PerpTrade{
		ID:              fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:          log.TxHash,
		BlockNumber:     log.BlockNumber,
		LogIndex:        log.LogIndex,
		Account:         account,
		CollateralToken: tokenIn,
		IndexToken:      tokenOut,
		TradeType:       "swap",
		CollateralDelta: amountIn,
		SizeDelta:       amountOut,
		Fee:             fee,
		Timestamp:       log.Timestamp,
	}

	p.trades = append(p.trades, trade)

	if p.onTrade != nil {
		p.onTrade(trade)
	}

	return nil
}

// getPositionID generates a unique position ID
func (p *PerpsIndexer) getPositionID(account, collateralToken, indexToken string, isLong bool) string {
	direction := "short"
	if isLong {
		direction = "long"
	}
	return fmt.Sprintf("%s:%s:%s:%s",
		strings.ToLower(account),
		strings.ToLower(collateralToken),
		strings.ToLower(indexToken),
		direction,
	)
}

// calculateAveragePrice calculates new average entry price
func calculateAveragePrice(prevAvgPrice, prevSize, sizeDelta, newPrice *big.Int) *big.Int {
	if prevSize.Sign() == 0 {
		return new(big.Int).Set(newPrice)
	}

	// avgPrice = (prevSize * prevAvgPrice + sizeDelta * newPrice) / (prevSize + sizeDelta)
	prev := new(big.Int).Mul(prevSize, prevAvgPrice)
	added := new(big.Int).Mul(sizeDelta, newPrice)
	total := new(big.Int).Add(prev, added)
	newSize := new(big.Int).Add(prevSize, sizeDelta)

	if newSize.Sign() == 0 {
		return big.NewInt(0)
	}

	return new(big.Int).Div(total, newSize)
}

// GetPosition returns a position by ID
func (p *PerpsIndexer) GetPosition(id string) *PerpPosition {
	return p.positions[id]
}

// GetPositionsByAccount returns all positions for an account
func (p *PerpsIndexer) GetPositionsByAccount(account string) []*PerpPosition {
	var result []*PerpPosition
	acc := strings.ToLower(account)
	for _, pos := range p.positions {
		if strings.ToLower(pos.Account) == acc {
			result = append(result, pos)
		}
	}
	return result
}

// GetOpenPositions returns all open positions
func (p *PerpsIndexer) GetOpenPositions() []*PerpPosition {
	var result []*PerpPosition
	for _, pos := range p.positions {
		if pos.IsOpen {
			result = append(result, pos)
		}
	}
	return result
}

// GetTrades returns trades with optional filtering
func (p *PerpsIndexer) GetTrades(account string, limit int) []*PerpTrade {
	if account == "" {
		if limit > 0 && limit < len(p.trades) {
			return p.trades[len(p.trades)-limit:]
		}
		return p.trades
	}

	var filtered []*PerpTrade
	acc := strings.ToLower(account)
	for _, t := range p.trades {
		if strings.ToLower(t.Account) == acc {
			filtered = append(filtered, t)
		}
	}

	if limit > 0 && limit < len(filtered) {
		return filtered[len(filtered)-limit:]
	}
	return filtered
}

// GetLiquidations returns liquidations
func (p *PerpsIndexer) GetLiquidations(limit int) []*PerpLiquidation {
	if limit > 0 && limit < len(p.liquidations) {
		return p.liquidations[len(p.liquidations)-limit:]
	}
	return p.liquidations
}

// PerpsStats represents perpetuals indexer statistics
type PerpsStats struct {
	TotalPositions    uint64    `json:"totalPositions"`
	OpenPositions     uint64    `json:"openPositions"`
	TotalTrades       uint64    `json:"totalTrades"`
	TotalLiquidations uint64    `json:"totalLiquidations"`
	OpenInterestLong  *big.Int  `json:"openInterestLong"`
	OpenInterestShort *big.Int  `json:"openInterestShort"`
	TotalVolume       *big.Int  `json:"totalVolume"`
	LastUpdated       time.Time `json:"lastUpdated"`
}

// GetStats returns aggregate stats
func (p *PerpsIndexer) GetStats() *PerpsStats {
	stats := &PerpsStats{
		TotalPositions:    uint64(len(p.positions)),
		OpenPositions:     uint64(len(p.GetOpenPositions())),
		TotalTrades:       uint64(len(p.trades)),
		TotalLiquidations: uint64(len(p.liquidations)),
		OpenInterestLong:  big.NewInt(0),
		OpenInterestShort: big.NewInt(0),
		TotalVolume:       big.NewInt(0),
		LastUpdated:       time.Now(),
	}

	for _, pos := range p.positions {
		if pos.IsOpen {
			if pos.IsLong {
				stats.OpenInterestLong.Add(stats.OpenInterestLong, pos.Size)
			} else {
				stats.OpenInterestShort.Add(stats.OpenInterestShort, pos.Size)
			}
		}
	}

	for _, trade := range p.trades {
		stats.TotalVolume.Add(stats.TotalVolume, trade.SizeDelta)
	}

	return stats
}
