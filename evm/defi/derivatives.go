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

// Derivatives event signatures (keccak256)
// Source: ~/work/lux/standard/contracts/futures/Futures.sol, options/Options.sol
var (
	// Futures events (IFutures interface)
	FutureContractCreatedSig    = "0xb0de2ee05894e05bd01fb62bbf22384b51debfd1a0e3d40cf003ecec1accdf54" // ContractCreated(uint256,address,address,uint256,uint256,uint256,uint256,uint256)
	FuturePositionOpenedSig     = "0xe84b3bb3a65ff089d134b8148e7e7b29811f1e2d41d49d0ca08ecbbb027a3806" // PositionOpened(uint256,address,bool,uint256,uint256,uint256)
	FuturePositionClosedSig     = "0x9a6907c6cf8b4c5dcddd3c29093969be96c92aa4d163f2a561c7a49c61004e4f" // PositionClosed(uint256,address,uint256,uint256,int256)
	FutureDailySettlementSig    = "0x002569682551fe6e08cf09bd69e4c3d14da7816f773c17b1d76e50b648bde9c1" // DailySettlement(uint256,uint256,uint256)
	FuturePositionLiquidatedSig = "0x2052f3ea800a8541e0cf366b82f9c588d53a2de2a5adb5f1198079f721fde249" // PositionLiquidated(uint256,address,address,uint256,uint256)
	FutureFinalSettlementSig    = "0xf5de83102745507f9f4b93fa940cf2f58ec798b98da1dada890c933a867a6df8" // FinalSettlement(uint256,uint256,uint256)

	// Options events
	OptionSeriesCreatedSig    = "0x727bc414c30014514f8b0adc9ac9ff9b821f18cf70ed672317e91479941bd2f7" // SeriesCreated(uint256,address,address,uint256,uint256,uint8,uint8)
	OptionWrittenSig          = "0xd5fd99b18aab94ea2fbf75a74c92fccc5c07940e97b04dcbe531bf105c1cc2a6" // OptionsWritten(uint256,address,uint256,uint256)
	OptionExercisedSig        = "0xded41ab6df0aa12ad2217eab957fce80b8acd05bde48ba1169797c41a75ec007" // OptionsExercised(uint256,address,uint256,uint256)
	OptionSeriesSettledSig    = "0x142bbcdf6be41b73bec58380b4a9fdeaf778b97cd45a07664fc1d60390fc716f" // SeriesSettled(uint256,uint256,uint256)
	OptionCollateralClaimedSig = "0xea28f19c369f33d25e9c28fae79ce8e3461feb799b5d9beb660e82906b7cc1ef" // CollateralClaimed(uint256,address,uint256)
)

// DerivativesIndexer indexes futures and options events
type DerivativesIndexer struct {
	futures     map[uint64]*FutureContract // contractId -> future
	options     map[uint64]*OptionSeries   // seriesId -> option
	events      []*DerivativesEvent
	onFuture    func(*DerivativesEvent)
	onOption    func(*DerivativesEvent)
}

// FutureContract represents a futures contract specification
type FutureContract struct {
	ContractID      uint64    `json:"contractId"`
	Underlying      string    `json:"underlying"`
	Quote           string    `json:"quote"`
	ContractSize    *big.Int  `json:"contractSize"`
	Expiry          uint64    `json:"expiry"`
	InitialMargin   *big.Int  `json:"initialMargin"`
	LongOI          *big.Int  `json:"longOI"`
	ShortOI         *big.Int  `json:"shortOI"`
	LastPrice       *big.Int  `json:"lastPrice"`
	IsSettled       bool      `json:"isSettled"`
	SettlementPrice *big.Int  `json:"settlementPrice,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// OptionSeries represents an option series
type OptionSeries struct {
	SeriesID       uint64    `json:"seriesId"`
	Underlying     string    `json:"underlying"`
	Quote          string    `json:"quote"`
	StrikePrice    *big.Int  `json:"strikePrice"`
	Expiry         uint64    `json:"expiry"`
	OptionType     uint8     `json:"optionType"`     // 0=CALL, 1=PUT
	SettlementType uint8     `json:"settlementType"` // 0=CASH, 1=PHYSICAL
	OpenInterest   *big.Int  `json:"openInterest"`
	IsSettled      bool      `json:"isSettled"`
	SettlementPrice *big.Int `json:"settlementPrice,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// DerivativesEvent represents a derivatives event
type DerivativesEvent struct {
	ID          string    `json:"id"`
	TxHash      string    `json:"txHash"`
	BlockNumber uint64    `json:"blockNumber"`
	LogIndex    uint64    `json:"logIndex"`
	Contract    string    `json:"contract"`
	EventType   string    `json:"eventType"`
	ContractID  uint64    `json:"contractId,omitempty"`
	SeriesID    uint64    `json:"seriesId,omitempty"`
	Account     string    `json:"account,omitempty"`
	IsLong      bool      `json:"isLong,omitempty"`
	Size        *big.Int  `json:"size,omitempty"`
	Price       *big.Int  `json:"price,omitempty"`
	Payout      *big.Int  `json:"payout,omitempty"`
	PnL         *big.Int  `json:"pnl,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// NewDerivativesIndexer creates a new derivatives indexer
func NewDerivativesIndexer() *DerivativesIndexer {
	return &DerivativesIndexer{
		futures: make(map[uint64]*FutureContract),
		options: make(map[uint64]*OptionSeries),
		events:  make([]*DerivativesEvent, 0),
	}
}

// SetCallbacks sets event callbacks
func (d *DerivativesIndexer) SetCallbacks(onFuture, onOption func(*DerivativesEvent)) {
	d.onFuture = onFuture
	d.onOption = onOption
}

// IndexLog processes a log entry for derivatives events
func (d *DerivativesIndexer) IndexLog(log *LogEntry) error {
	if len(log.Topics) == 0 {
		return nil
	}

	switch log.Topics[0] {
	// Futures
	case FutureContractCreatedSig:
		return d.indexFutureCreated(log)
	case FuturePositionOpenedSig:
		return d.indexFuturePositionOpened(log)
	case FuturePositionClosedSig:
		return d.indexFuturePositionClosed(log)
	case FutureDailySettlementSig:
		return d.indexDailySettlement(log)
	case FuturePositionLiquidatedSig:
		return d.indexFuturePositionLiquidated(log)
	case FutureFinalSettlementSig:
		return d.indexFinalSettlement(log)

	// Options
	case OptionSeriesCreatedSig:
		return d.indexOptionSeriesCreated(log)
	case OptionWrittenSig:
		return d.indexOptionWritten(log)
	case OptionExercisedSig:
		return d.indexOptionExercised(log)
	case OptionSeriesSettledSig:
		return d.indexOptionSettled(log)
	case OptionCollateralClaimedSig:
		return d.indexOptionCollateralClaimed(log)
	}

	return nil
}

// indexFutureCreated processes ContractCreated(uint256 indexed contractId, address indexed underlying, address quote, uint256 contractSize, uint256 tickSize, uint256 initialMarginBps, uint256 maintenanceMarginBps, uint256 expiry)
func (d *DerivativesIndexer) indexFutureCreated(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid ContractCreated event: need 3 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 192 {
		return fmt.Errorf("invalid ContractCreated data: need 192 bytes, got %d", len(data))
	}

	contractID := new(big.Int).SetBytes(hexToBytes(log.Topics[1])).Uint64()
	underlying := topicToAddress(log.Topics[2])
	quote := "0x" + hex.EncodeToString(data[12:32])
	contractSize := new(big.Int).SetBytes(data[32:64])
	expiry := new(big.Int).SetBytes(data[160:192]).Uint64()

	evt := &DerivativesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "future_created",
		ContractID:  contractID,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	d.futures[contractID] = &FutureContract{
		ContractID:   contractID,
		Underlying:   underlying,
		Quote:        quote,
		ContractSize: contractSize,
		Expiry:       expiry,
		LongOI:       big.NewInt(0),
		ShortOI:      big.NewInt(0),
		LastPrice:    big.NewInt(0),
		CreatedAt:    log.Timestamp,
		UpdatedAt:    log.Timestamp,
	}

	if d.onFuture != nil {
		d.onFuture(evt)
	}
	return nil
}

// indexFuturePositionOpened processes PositionOpened(uint256 indexed contractId, address indexed trader, bool isLong, uint256 size, uint256 price, uint256 margin)
func (d *DerivativesIndexer) indexFuturePositionOpened(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid PositionOpened event: need 3 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 128 {
		return fmt.Errorf("invalid PositionOpened data: need 128 bytes, got %d", len(data))
	}

	contractID := new(big.Int).SetBytes(hexToBytes(log.Topics[1])).Uint64()
	account := topicToAddress(log.Topics[2])
	isLong := data[31] == 1
	size := new(big.Int).SetBytes(data[32:64])
	price := new(big.Int).SetBytes(data[64:96])

	evt := &DerivativesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "future_position_opened",
		ContractID:  contractID,
		Account:     account,
		IsLong:      isLong,
		Size:        size,
		Price:       price,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	if fc, ok := d.futures[contractID]; ok {
		if isLong {
			fc.LongOI = new(big.Int).Add(fc.LongOI, size)
		} else {
			fc.ShortOI = new(big.Int).Add(fc.ShortOI, size)
		}
		fc.LastPrice = price
		fc.UpdatedAt = log.Timestamp
	}

	if d.onFuture != nil {
		d.onFuture(evt)
	}
	return nil
}

// indexFuturePositionClosed processes PositionClosed(uint256 indexed contractId, address indexed trader, uint256 size, uint256 price, int256 pnl)
func (d *DerivativesIndexer) indexFuturePositionClosed(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid PositionClosed event: need 3 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 96 {
		return fmt.Errorf("invalid PositionClosed data: need 96 bytes, got %d", len(data))
	}

	contractID := new(big.Int).SetBytes(hexToBytes(log.Topics[1])).Uint64()
	account := topicToAddress(log.Topics[2])
	size := new(big.Int).SetBytes(data[0:32])
	price := new(big.Int).SetBytes(data[32:64])
	// PnL is signed int256
	pnlBytes := data[64:96]
	pnl := new(big.Int).SetBytes(pnlBytes)
	if pnlBytes[0]&0x80 != 0 {
		pnl.Sub(pnl, new(big.Int).Lsh(big.NewInt(1), 256))
	}

	evt := &DerivativesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "future_position_closed",
		ContractID:  contractID,
		Account:     account,
		Size:        size,
		Price:       price,
		PnL:         pnl,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	if fc, ok := d.futures[contractID]; ok {
		fc.LastPrice = price
		fc.UpdatedAt = log.Timestamp
	}

	if d.onFuture != nil {
		d.onFuture(evt)
	}
	return nil
}

// indexDailySettlement processes DailySettlement(uint256 indexed contractId, uint256 settlementPrice, uint256 timestamp)
func (d *DerivativesIndexer) indexDailySettlement(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid DailySettlement event: need 2 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 64 {
		return fmt.Errorf("invalid DailySettlement data: need 64 bytes, got %d", len(data))
	}

	contractID := new(big.Int).SetBytes(hexToBytes(log.Topics[1])).Uint64()
	price := new(big.Int).SetBytes(data[0:32])

	evt := &DerivativesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "daily_settlement",
		ContractID:  contractID,
		Price:       price,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	if fc, ok := d.futures[contractID]; ok {
		fc.LastPrice = price
		fc.UpdatedAt = log.Timestamp
	}

	if d.onFuture != nil {
		d.onFuture(evt)
	}
	return nil
}

// indexFuturePositionLiquidated processes PositionLiquidated(uint256 indexed contractId, address indexed trader, address indexed liquidator, uint256 marginSeized, uint256 penalty)
func (d *DerivativesIndexer) indexFuturePositionLiquidated(log *LogEntry) error {
	if len(log.Topics) < 4 {
		return fmt.Errorf("invalid PositionLiquidated event: need 4 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 64 {
		return fmt.Errorf("invalid PositionLiquidated data: need 64 bytes, got %d", len(data))
	}

	contractID := new(big.Int).SetBytes(hexToBytes(log.Topics[1])).Uint64()
	account := topicToAddress(log.Topics[2])
	marginSeized := new(big.Int).SetBytes(data[0:32])

	evt := &DerivativesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "future_liquidated",
		ContractID:  contractID,
		Account:     account,
		Payout:      marginSeized,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	if d.onFuture != nil {
		d.onFuture(evt)
	}
	return nil
}

// indexFinalSettlement processes FinalSettlement(uint256 indexed contractId, uint256 finalPrice, uint256 timestamp)
func (d *DerivativesIndexer) indexFinalSettlement(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid FinalSettlement event: need 2 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 64 {
		return fmt.Errorf("invalid FinalSettlement data: need 64 bytes, got %d", len(data))
	}

	contractID := new(big.Int).SetBytes(hexToBytes(log.Topics[1])).Uint64()
	price := new(big.Int).SetBytes(data[0:32])

	evt := &DerivativesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "final_settlement",
		ContractID:  contractID,
		Price:       price,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	if fc, ok := d.futures[contractID]; ok {
		fc.IsSettled = true
		fc.SettlementPrice = price
		fc.UpdatedAt = log.Timestamp
	}

	if d.onFuture != nil {
		d.onFuture(evt)
	}
	return nil
}

// indexOptionSeriesCreated processes SeriesCreated(uint256 indexed seriesId, address indexed underlying, address indexed quote, uint256 strikePrice, uint256 expiry, uint8 optionType, uint8 settlement)
func (d *DerivativesIndexer) indexOptionSeriesCreated(log *LogEntry) error {
	if len(log.Topics) < 4 {
		return fmt.Errorf("invalid SeriesCreated event: need 4 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 128 {
		return fmt.Errorf("invalid SeriesCreated data: need 128 bytes, got %d", len(data))
	}

	seriesID := new(big.Int).SetBytes(hexToBytes(log.Topics[1])).Uint64()
	underlying := topicToAddress(log.Topics[2])
	quote := topicToAddress(log.Topics[3])
	strikePrice := new(big.Int).SetBytes(data[0:32])
	expiry := new(big.Int).SetBytes(data[32:64]).Uint64()
	optionType := uint8(new(big.Int).SetBytes(data[64:96]).Uint64())
	settlementType := uint8(new(big.Int).SetBytes(data[96:128]).Uint64())

	evt := &DerivativesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "option_series_created",
		SeriesID:    seriesID,
		Price:       strikePrice,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	d.options[seriesID] = &OptionSeries{
		SeriesID:       seriesID,
		Underlying:     underlying,
		Quote:          quote,
		StrikePrice:    strikePrice,
		Expiry:         expiry,
		OptionType:     optionType,
		SettlementType: settlementType,
		OpenInterest:   big.NewInt(0),
		CreatedAt:      log.Timestamp,
		UpdatedAt:      log.Timestamp,
	}

	if d.onOption != nil {
		d.onOption(evt)
	}
	return nil
}

// indexOptionWritten processes OptionsWritten(uint256 indexed seriesId, address indexed writer, uint256 amount, uint256 collateral)
func (d *DerivativesIndexer) indexOptionWritten(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid OptionsWritten event: need 3 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 64 {
		return fmt.Errorf("invalid OptionsWritten data: need 64 bytes, got %d", len(data))
	}

	seriesID := new(big.Int).SetBytes(hexToBytes(log.Topics[1])).Uint64()
	account := topicToAddress(log.Topics[2])
	amount := new(big.Int).SetBytes(data[0:32])

	evt := &DerivativesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "option_written",
		SeriesID:    seriesID,
		Account:     account,
		Size:        amount,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	if os, ok := d.options[seriesID]; ok {
		os.OpenInterest = new(big.Int).Add(os.OpenInterest, amount)
		os.UpdatedAt = log.Timestamp
	}

	if d.onOption != nil {
		d.onOption(evt)
	}
	return nil
}

// indexOptionExercised processes OptionsExercised(uint256 indexed seriesId, address indexed holder, uint256 amount, uint256 payout)
func (d *DerivativesIndexer) indexOptionExercised(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid OptionsExercised event: need 3 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 64 {
		return fmt.Errorf("invalid OptionsExercised data: need 64 bytes, got %d", len(data))
	}

	seriesID := new(big.Int).SetBytes(hexToBytes(log.Topics[1])).Uint64()
	account := topicToAddress(log.Topics[2])
	amount := new(big.Int).SetBytes(data[0:32])
	payout := new(big.Int).SetBytes(data[32:64])

	evt := &DerivativesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "option_exercised",
		SeriesID:    seriesID,
		Account:     account,
		Size:        amount,
		Payout:      payout,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	if os, ok := d.options[seriesID]; ok {
		os.OpenInterest = new(big.Int).Sub(os.OpenInterest, amount)
		if os.OpenInterest.Sign() < 0 {
			os.OpenInterest = big.NewInt(0)
		}
		os.UpdatedAt = log.Timestamp
	}

	if d.onOption != nil {
		d.onOption(evt)
	}
	return nil
}

// indexOptionSettled processes SeriesSettled(uint256 indexed seriesId, uint256 settlementPrice, uint256 timestamp)
func (d *DerivativesIndexer) indexOptionSettled(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid SeriesSettled event: need 2 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 64 {
		return fmt.Errorf("invalid SeriesSettled data: need 64 bytes, got %d", len(data))
	}

	seriesID := new(big.Int).SetBytes(hexToBytes(log.Topics[1])).Uint64()
	price := new(big.Int).SetBytes(data[0:32])

	evt := &DerivativesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "option_settled",
		SeriesID:    seriesID,
		Price:       price,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	if os, ok := d.options[seriesID]; ok {
		os.IsSettled = true
		os.SettlementPrice = price
		os.UpdatedAt = log.Timestamp
	}

	if d.onOption != nil {
		d.onOption(evt)
	}
	return nil
}

// indexOptionCollateralClaimed processes CollateralClaimed(uint256 indexed seriesId, address indexed writer, uint256 amount)
func (d *DerivativesIndexer) indexOptionCollateralClaimed(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid CollateralClaimed event: need 3 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 32 {
		return fmt.Errorf("invalid CollateralClaimed data: need 32 bytes, got %d", len(data))
	}

	seriesID := new(big.Int).SetBytes(hexToBytes(log.Topics[1])).Uint64()
	account := topicToAddress(log.Topics[2])
	amount := new(big.Int).SetBytes(data[0:32])

	evt := &DerivativesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "option_collateral_claimed",
		SeriesID:    seriesID,
		Account:     account,
		Payout:      amount,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	if d.onOption != nil {
		d.onOption(evt)
	}
	return nil
}

// GetFuture returns a futures contract by ID
func (d *DerivativesIndexer) GetFuture(id uint64) *FutureContract {
	return d.futures[id]
}

// GetOption returns an option series by ID
func (d *DerivativesIndexer) GetOption(id uint64) *OptionSeries {
	return d.options[id]
}

// GetActiveFutures returns unsettled futures
func (d *DerivativesIndexer) GetActiveFutures() []*FutureContract {
	var result []*FutureContract
	for _, f := range d.futures {
		if !f.IsSettled {
			result = append(result, f)
		}
	}
	return result
}

// GetActiveOptions returns unsettled options
func (d *DerivativesIndexer) GetActiveOptions() []*OptionSeries {
	var result []*OptionSeries
	for _, o := range d.options {
		if !o.IsSettled {
			result = append(result, o)
		}
	}
	return result
}

// DerivativesStats represents derivatives indexer statistics
type DerivativesStats struct {
	TotalFutures   uint64    `json:"totalFutures"`
	ActiveFutures  uint64    `json:"activeFutures"`
	TotalOptions   uint64    `json:"totalOptions"`
	ActiveOptions  uint64    `json:"activeOptions"`
	TotalEvents    uint64    `json:"totalEvents"`
	LastUpdated    time.Time `json:"lastUpdated"`
}

// GetStats returns aggregate statistics
func (d *DerivativesIndexer) GetStats() *DerivativesStats {
	activeFutures := uint64(0)
	for _, f := range d.futures {
		if !f.IsSettled {
			activeFutures++
		}
	}
	activeOptions := uint64(0)
	for _, o := range d.options {
		if !o.IsSettled {
			activeOptions++
		}
	}
	return &DerivativesStats{
		TotalFutures:  uint64(len(d.futures)),
		ActiveFutures: activeFutures,
		TotalOptions:  uint64(len(d.options)),
		ActiveOptions: activeOptions,
		TotalEvents:   uint64(len(d.events)),
		LastUpdated:   time.Now(),
	}
}
