// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package defi

import (
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestDerivativesIndexer_FutureCreated(t *testing.T) {
	idx := NewDerivativesIndexer()

	var called bool
	idx.SetCallbacks(func(e *DerivativesEvent) { called = true }, nil)

	// Data: quote(addr) + contractSize(uint256) + tickSize + initialMarginBps + maintenanceMarginBps + expiry
	data := "0x" +
		strings.Repeat("0", 24) + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + // quote
		strings.Repeat("0", 60) + "03e8" + // contractSize = 1000
		strings.Repeat("0", 64) + // tickSize
		strings.Repeat("0", 64) + // initialMarginBps
		strings.Repeat("0", 64) + // maintenanceMarginBps
		strings.Repeat("0", 56) + "67000000" // expiry (some future timestamp)

	if err := idx.IndexLog(&LogEntry{
		Address: "0xfutures",
		Topics: []string{
			FutureContractCreatedSig,
			"0x0000000000000000000000000000000000000000000000000000000000000001", // contractId=1
			"0x000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", // underlying
		},
		Data: data, TxHash: "0xtx1", BlockNumber: 100, Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("IndexLog failed: %v", err)
	}

	if !called {
		t.Fatal("onFuture callback not called")
	}

	fc := idx.GetFuture(1)
	if fc == nil {
		t.Fatal("future not found")
	}
	if fc.ContractSize.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("contractSize = %v, want 1000", fc.ContractSize)
	}
	if fc.IsSettled {
		t.Error("should not be settled")
	}
}

func TestDerivativesIndexer_FuturePositionLifecycle(t *testing.T) {
	idx := NewDerivativesIndexer()
	now := time.Now()

	contractID := "0x0000000000000000000000000000000000000000000000000000000000000001"

	// Create the contract first
	createData := "0x" +
		strings.Repeat("0", 24) + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		strings.Repeat("0", 60) + "03e8" +
		strings.Repeat("0", 64) +
		strings.Repeat("0", 64) +
		strings.Repeat("0", 64) +
		strings.Repeat("0", 56) + "67000000"

	idx.IndexLog(&LogEntry{
		Address: "0xfutures",
		Topics: []string{
			FutureContractCreatedSig,
			contractID,
			"0x000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		Data: createData, TxHash: "0xtx0", BlockNumber: 99, Timestamp: now,
	})

	// Open long position
	// Data: isLong(bool) + size(uint256) + price(uint256) + margin(uint256)
	openData := "0x" +
		strings.Repeat("0", 62) + "01" + // isLong = true
		strings.Repeat("0", 60) + "000a" + // size = 10
		strings.Repeat("0", 56) + "000003e8" + // price = 1000
		strings.Repeat("0", 60) + "0064" // margin = 100

	if err := idx.IndexLog(&LogEntry{
		Address: "0xfutures",
		Topics: []string{
			FuturePositionOpenedSig,
			contractID,
			"0x0000000000000000000000001111111111111111111111111111111111111111",
		},
		Data: openData, TxHash: "0xtx1", BlockNumber: 100, Timestamp: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("PositionOpened failed: %v", err)
	}

	fc := idx.GetFuture(1)
	if fc.LongOI.Cmp(big.NewInt(10)) != 0 {
		t.Errorf("longOI = %v, want 10", fc.LongOI)
	}

	// Close position with positive PnL
	// Data: size(uint256) + price(uint256) + pnl(int256)
	closeData := "0x" +
		strings.Repeat("0", 60) + "000a" + // size = 10
		strings.Repeat("0", 56) + "000004b0" + // price = 1200
		strings.Repeat("0", 60) + "00c8" // pnl = 200

	if err := idx.IndexLog(&LogEntry{
		Address: "0xfutures",
		Topics: []string{
			FuturePositionClosedSig,
			contractID,
			"0x0000000000000000000000001111111111111111111111111111111111111111",
		},
		Data: closeData, TxHash: "0xtx2", BlockNumber: 101, Timestamp: now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("PositionClosed failed: %v", err)
	}

	fc = idx.GetFuture(1)
	if fc.LastPrice.Cmp(big.NewInt(1200)) != 0 {
		t.Errorf("lastPrice = %v, want 1200", fc.LastPrice)
	}
}

func TestDerivativesIndexer_FinalSettlement(t *testing.T) {
	idx := NewDerivativesIndexer()

	contractID := "0x0000000000000000000000000000000000000000000000000000000000000001"

	// Create
	createData := "0x" +
		strings.Repeat("0", 24) + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		strings.Repeat("0", 60) + "03e8" +
		strings.Repeat("0", 64*4)

	idx.IndexLog(&LogEntry{
		Address: "0xfutures",
		Topics:  []string{FutureContractCreatedSig, contractID, "0x" + strings.Repeat("0", 64)},
		Data:    createData, TxHash: "0xtx0", BlockNumber: 99, Timestamp: time.Now(),
	})

	// Final settlement
	data := "0x" +
		strings.Repeat("0", 56) + "000004b0" + // finalPrice = 1200
		strings.Repeat("0", 56) + "67000000" // timestamp

	if err := idx.IndexLog(&LogEntry{
		Address: "0xfutures",
		Topics:  []string{FutureFinalSettlementSig, contractID},
		Data:    data, TxHash: "0xtx1", BlockNumber: 100, Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("FinalSettlement failed: %v", err)
	}

	fc := idx.GetFuture(1)
	if !fc.IsSettled {
		t.Error("should be settled")
	}
	if fc.SettlementPrice.Cmp(big.NewInt(1200)) != 0 {
		t.Errorf("settlementPrice = %v, want 1200", fc.SettlementPrice)
	}

	active := idx.GetActiveFutures()
	if len(active) != 0 {
		t.Errorf("activeFutures = %d, want 0", len(active))
	}
}

func TestDerivativesIndexer_OptionSeriesCreated(t *testing.T) {
	idx := NewDerivativesIndexer()

	var called bool
	idx.SetCallbacks(nil, func(e *DerivativesEvent) { called = true })

	// Data: strikePrice + expiry + optionType + settlementType
	data := "0x" +
		strings.Repeat("0", 56) + "000003e8" + // strikePrice = 1000
		strings.Repeat("0", 56) + "67000000" + // expiry
		strings.Repeat("0", 62) + "00" + // optionType = CALL
		strings.Repeat("0", 62) + "00" // settlementType = CASH

	if err := idx.IndexLog(&LogEntry{
		Address: "0xoptions",
		Topics: []string{
			OptionSeriesCreatedSig,
			"0x0000000000000000000000000000000000000000000000000000000000000001", // seriesId=1
			"0x000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", // underlying
			"0x000000000000000000000000cccccccccccccccccccccccccccccccccccccccc", // quote
		},
		Data: data, TxHash: "0xtx1", BlockNumber: 100, Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("IndexLog failed: %v", err)
	}

	if !called {
		t.Fatal("onOption callback not called")
	}

	opt := idx.GetOption(1)
	if opt == nil {
		t.Fatal("option not found")
	}
	if opt.StrikePrice.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("strikePrice = %v, want 1000", opt.StrikePrice)
	}
	if opt.OptionType != 0 {
		t.Errorf("optionType = %d, want 0 (CALL)", opt.OptionType)
	}
}

func TestDerivativesIndexer_OptionWriteAndExercise(t *testing.T) {
	idx := NewDerivativesIndexer()

	seriesID := "0x0000000000000000000000000000000000000000000000000000000000000001"

	// Create series
	createData := "0x" +
		strings.Repeat("0", 56) + "000003e8" +
		strings.Repeat("0", 56) + "67000000" +
		strings.Repeat("0", 62) + "00" +
		strings.Repeat("0", 62) + "00"

	idx.IndexLog(&LogEntry{
		Address: "0xoptions",
		Topics: []string{
			OptionSeriesCreatedSig, seriesID,
			"0x" + strings.Repeat("0", 64),
			"0x" + strings.Repeat("0", 64),
		},
		Data: createData, TxHash: "0xtx0", BlockNumber: 99, Timestamp: time.Now(),
	})

	// Write options: amount=50, collateral=5000
	writeData := "0x" +
		strings.Repeat("0", 60) + "0032" + // amount = 50
		strings.Repeat("0", 56) + "00001388" // collateral = 5000

	idx.IndexLog(&LogEntry{
		Address: "0xoptions",
		Topics: []string{
			OptionWrittenSig, seriesID,
			"0x0000000000000000000000001111111111111111111111111111111111111111",
		},
		Data: writeData, TxHash: "0xtx1", BlockNumber: 100, Timestamp: time.Now(),
	})

	opt := idx.GetOption(1)
	if opt.OpenInterest.Cmp(big.NewInt(50)) != 0 {
		t.Errorf("openInterest = %v, want 50", opt.OpenInterest)
	}

	// Exercise: amount=20, payout=4000
	exerciseData := "0x" +
		strings.Repeat("0", 60) + "0014" + // amount = 20
		strings.Repeat("0", 56) + "00000fa0" // payout = 4000

	idx.IndexLog(&LogEntry{
		Address: "0xoptions",
		Topics: []string{
			OptionExercisedSig, seriesID,
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		Data: exerciseData, TxHash: "0xtx2", BlockNumber: 101, Timestamp: time.Now(),
	})

	opt = idx.GetOption(1)
	if opt.OpenInterest.Cmp(big.NewInt(30)) != 0 {
		t.Errorf("openInterest after exercise = %v, want 30", opt.OpenInterest)
	}
}

func TestDerivativesIndexer_OptionSettled(t *testing.T) {
	idx := NewDerivativesIndexer()

	seriesID := "0x0000000000000000000000000000000000000000000000000000000000000001"

	// Create
	createData := "0x" + strings.Repeat("0", 64*4)
	idx.IndexLog(&LogEntry{
		Address: "0xoptions",
		Topics:  []string{OptionSeriesCreatedSig, seriesID, "0x" + strings.Repeat("0", 64), "0x" + strings.Repeat("0", 64)},
		Data:    createData, TxHash: "0xtx0", BlockNumber: 99, Timestamp: time.Now(),
	})

	// Settle
	settleData := "0x" +
		strings.Repeat("0", 56) + "000004b0" + // settlementPrice = 1200
		strings.Repeat("0", 56) + "67000000" // timestamp

	if err := idx.IndexLog(&LogEntry{
		Address: "0xoptions",
		Topics:  []string{OptionSeriesSettledSig, seriesID},
		Data:    settleData, TxHash: "0xtx1", BlockNumber: 100, Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("IndexLog failed: %v", err)
	}

	opt := idx.GetOption(1)
	if !opt.IsSettled {
		t.Error("should be settled")
	}
	if opt.SettlementPrice.Cmp(big.NewInt(1200)) != 0 {
		t.Errorf("settlementPrice = %v, want 1200", opt.SettlementPrice)
	}

	active := idx.GetActiveOptions()
	if len(active) != 0 {
		t.Errorf("activeOptions = %d, want 0", len(active))
	}
}

func TestDerivativesIndexer_Stats(t *testing.T) {
	idx := NewDerivativesIndexer()

	// Create 2 futures, settle 1
	for i := 1; i <= 2; i++ {
		cid := strings.Repeat("0", 62) + "0" + string(rune('0'+i))
		createData := "0x" +
			strings.Repeat("0", 24) + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
			strings.Repeat("0", 60) + "03e8" +
			strings.Repeat("0", 64*4)

		idx.IndexLog(&LogEntry{
			Address: "0xfutures",
			Topics:  []string{FutureContractCreatedSig, "0x" + cid, "0x" + strings.Repeat("0", 64)},
			Data:    createData, TxHash: "0xtx", BlockNumber: uint64(100 + i), Timestamp: time.Now(),
		})
	}

	// Settle first
	settleData := "0x" + strings.Repeat("0", 56) + "000003e8" + strings.Repeat("0", 56) + "67000000"
	idx.IndexLog(&LogEntry{
		Address: "0xfutures",
		Topics:  []string{FutureFinalSettlementSig, "0x" + strings.Repeat("0", 62) + "01"},
		Data:    settleData, TxHash: "0xtxs", BlockNumber: 200, Timestamp: time.Now(),
	})

	stats := idx.GetStats()
	if stats.TotalFutures != 2 {
		t.Errorf("TotalFutures = %d, want 2", stats.TotalFutures)
	}
	if stats.ActiveFutures != 1 {
		t.Errorf("ActiveFutures = %d, want 1", stats.ActiveFutures)
	}
}

func TestDerivativesIndexer_InvalidLogs(t *testing.T) {
	idx := NewDerivativesIndexer()

	// No topics
	if err := idx.IndexLog(&LogEntry{}); err != nil {
		t.Fatalf("empty log should not error: %v", err)
	}

	// Insufficient topics for futures created
	err := idx.IndexLog(&LogEntry{
		Topics: []string{FutureContractCreatedSig}, // needs 3
	})
	if err == nil {
		t.Error("should error on insufficient topics")
	}

	// Bad data for option written
	err = idx.IndexLog(&LogEntry{
		Topics: []string{
			OptionWrittenSig,
			"0x0000000000000000000000000000000000000000000000000000000000000001",
			"0x0000000000000000000000001111111111111111111111111111111111111111",
		},
		Data: "0xZZZZ",
	})
	if err == nil {
		t.Error("should error on bad hex data")
	}
}
