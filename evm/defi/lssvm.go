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

// LSSVMIndexer indexes LSSVM (Sudoswap-style) NFT AMM events
type LSSVMIndexer struct {
	pairs         map[string]*LSSVMPair
	swaps         []*LSSVMSwap
	deposits      []*LSSVMDeposit
	withdrawals   []*LSSVMWithdrawal
	curves        map[string]*BondingCurve
	onSwap        func(*LSSVMSwap)
	onDeposit     func(*LSSVMDeposit)
	onWithdrawal  func(*LSSVMWithdrawal)
	onPairCreated func(*LSSVMPair)
}

// LSSVMPair represents an LSSVM trading pair
type LSSVMPair struct {
	Address       string     `json:"address"`
	NFTCollection string     `json:"nftCollection"`
	Token         string     `json:"token"`     // ERC20 or native (0x0)
	CurveType     string     `json:"curveType"` // "linear", "exponential", "xyk"
	CurveAddress  string     `json:"curveAddress"`
	PoolType      string     `json:"poolType"` // "trade", "nft", "token"
	SpotPrice     *big.Int   `json:"spotPrice"`
	Delta         *big.Int   `json:"delta"`      // Price change per trade
	Fee           uint64     `json:"fee"`        // Fee in basis points
	NFTBalance    []string   `json:"nftBalance"` // Token IDs held
	TokenBalance  *big.Int   `json:"tokenBalance"`
	Owner         string     `json:"owner"`
	TxCount       uint64     `json:"txCount"`
	Volume        *big.Float `json:"volume"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// LSSVMSwap represents an NFT swap event
type LSSVMSwap struct {
	ID          string    `json:"id"`
	PairAddress string    `json:"pairAddress"`
	TxHash      string    `json:"txHash"`
	BlockNumber uint64    `json:"blockNumber"`
	LogIndex    uint64    `json:"logIndex"`
	Direction   string    `json:"direction"` // "buy" or "sell"
	NFTIds      []string  `json:"nftIds"`
	TokenAmount *big.Int  `json:"tokenAmount"`
	SpotPrice   *big.Int  `json:"spotPrice"`
	Fee         *big.Int  `json:"fee"`
	Trader      string    `json:"trader"`
	Timestamp   time.Time `json:"timestamp"`
}

// LSSVMDeposit represents an NFT or token deposit
type LSSVMDeposit struct {
	ID          string    `json:"id"`
	PairAddress string    `json:"pairAddress"`
	TxHash      string    `json:"txHash"`
	BlockNumber uint64    `json:"blockNumber"`
	LogIndex    uint64    `json:"logIndex"`
	DepositType string    `json:"depositType"` // "nft" or "token"
	NFTIds      []string  `json:"nftIds,omitempty"`
	TokenAmount *big.Int  `json:"tokenAmount,omitempty"`
	Depositor   string    `json:"depositor"`
	Timestamp   time.Time `json:"timestamp"`
}

// LSSVMWithdrawal represents an NFT or token withdrawal
type LSSVMWithdrawal struct {
	ID             string    `json:"id"`
	PairAddress    string    `json:"pairAddress"`
	TxHash         string    `json:"txHash"`
	BlockNumber    uint64    `json:"blockNumber"`
	LogIndex       uint64    `json:"logIndex"`
	WithdrawalType string    `json:"withdrawalType"` // "nft" or "token"
	NFTIds         []string  `json:"nftIds,omitempty"`
	TokenAmount    *big.Int  `json:"tokenAmount,omitempty"`
	Recipient      string    `json:"recipient"`
	Timestamp      time.Time `json:"timestamp"`
}

// BondingCurve represents a bonding curve implementation
type BondingCurve struct {
	Address   string `json:"address"`
	Type      string `json:"type"` // "linear", "exponential", "xyk"
	IsAllowed bool   `json:"isAllowed"`
}

// NewLSSVMIndexer creates a new LSSVM indexer
func NewLSSVMIndexer() *LSSVMIndexer {
	return &LSSVMIndexer{
		pairs:       make(map[string]*LSSVMPair),
		swaps:       make([]*LSSVMSwap, 0),
		deposits:    make([]*LSSVMDeposit, 0),
		withdrawals: make([]*LSSVMWithdrawal, 0),
		curves:      make(map[string]*BondingCurve),
	}
}

// SetCallbacks sets event callbacks
func (l *LSSVMIndexer) SetCallbacks(
	onSwap func(*LSSVMSwap),
	onDeposit func(*LSSVMDeposit),
	onWithdrawal func(*LSSVMWithdrawal),
	onPairCreated func(*LSSVMPair),
) {
	l.onSwap = onSwap
	l.onDeposit = onDeposit
	l.onWithdrawal = onWithdrawal
	l.onPairCreated = onPairCreated
}

// IndexLog processes a log entry for LSSVM events
func (l *LSSVMIndexer) IndexLog(log *LogEntry) error {
	if len(log.Topics) == 0 {
		return nil
	}

	topic0 := log.Topics[0]

	switch topic0 {
	case LSSVMSwapNFTInSig:
		return l.indexSwapNFTIn(log)
	case LSSVMSwapNFTOutSig:
		return l.indexSwapNFTOut(log)
	case LSSVMSpotPriceSig:
		return l.indexSpotPriceUpdate(log)
	case LSSVMDeltaUpdateSig:
		return l.indexDeltaUpdate(log)
	case LSSVMPairCreatedSig:
		return l.indexPairCreated(log)
	}

	// Check for standard events
	switch topic0 {
	case "0x" + hex.EncodeToString([]byte("TokenDeposit(uint256)")[:32]):
		return l.indexTokenDeposit(log)
	case "0x" + hex.EncodeToString([]byte("TokenWithdrawal(uint256)")[:32]):
		return l.indexTokenWithdrawal(log)
	case "0x" + hex.EncodeToString([]byte("NFTDeposit(uint256[])")[:32]):
		return l.indexNFTDeposit(log)
	case "0x" + hex.EncodeToString([]byte("NFTWithdrawal(uint256[])")[:32]):
		return l.indexNFTWithdrawal(log)
	}

	return nil
}

// indexSwapNFTIn processes SwapNFTInPool event (selling NFTs for tokens)
func (l *LSSVMIndexer) indexSwapNFTIn(log *LogEntry) error {
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	// SwapNFTInPool(uint256[] nftIds, uint256 inputAmount)
	// Parse dynamic array and input amount
	nftIds, inputAmount := l.parseSwapData(data)

	pair := l.getOrCreatePair(log.Address)

	swap := &LSSVMSwap{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		PairAddress: log.Address,
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Direction:   "sell", // Selling NFTs for tokens
		NFTIds:      nftIds,
		TokenAmount: inputAmount,
		SpotPrice:   pair.SpotPrice,
		Timestamp:   log.Timestamp,
	}

	l.swaps = append(l.swaps, swap)
	pair.TxCount++
	pair.UpdatedAt = log.Timestamp

	if l.onSwap != nil {
		l.onSwap(swap)
	}

	return nil
}

// indexSwapNFTOut processes SwapNFTOutPool event (buying NFTs with tokens)
func (l *LSSVMIndexer) indexSwapNFTOut(log *LogEntry) error {
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	nftIds, outputAmount := l.parseSwapData(data)

	pair := l.getOrCreatePair(log.Address)

	swap := &LSSVMSwap{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		PairAddress: log.Address,
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Direction:   "buy", // Buying NFTs with tokens
		NFTIds:      nftIds,
		TokenAmount: outputAmount,
		SpotPrice:   pair.SpotPrice,
		Timestamp:   log.Timestamp,
	}

	l.swaps = append(l.swaps, swap)
	pair.TxCount++
	pair.UpdatedAt = log.Timestamp

	if l.onSwap != nil {
		l.onSwap(swap)
	}

	return nil
}

// indexSpotPriceUpdate processes SpotPriceUpdate event
func (l *LSSVMIndexer) indexSpotPriceUpdate(log *LogEntry) error {
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	if len(data) < 32 {
		return fmt.Errorf("invalid spot price data")
	}

	newSpotPrice := new(big.Int).SetBytes(data[0:32])

	pair := l.getOrCreatePair(log.Address)
	pair.SpotPrice = newSpotPrice
	pair.UpdatedAt = log.Timestamp

	return nil
}

// indexDeltaUpdate processes DeltaUpdate event
func (l *LSSVMIndexer) indexDeltaUpdate(log *LogEntry) error {
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	if len(data) < 32 {
		return fmt.Errorf("invalid delta data")
	}

	newDelta := new(big.Int).SetBytes(data[0:32])

	pair := l.getOrCreatePair(log.Address)
	pair.Delta = newDelta
	pair.UpdatedAt = log.Timestamp

	return nil
}

// indexPairCreated processes PairCreated event
func (l *LSSVMIndexer) indexPairCreated(log *LogEntry) error {
	if len(log.Topics) < 4 {
		return fmt.Errorf("invalid pair created event")
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	// Parse pair creation data
	nftCollection := topicToAddress(log.Topics[1])
	token := topicToAddress(log.Topics[2])

	pairAddress := log.Address
	if len(data) >= 32 {
		pairAddress = "0x" + hex.EncodeToString(data[12:32])
	}

	pair := &LSSVMPair{
		Address:       strings.ToLower(pairAddress),
		NFTCollection: nftCollection,
		Token:         token,
		SpotPrice:     big.NewInt(0),
		Delta:         big.NewInt(0),
		TokenBalance:  big.NewInt(0),
		NFTBalance:    make([]string, 0),
		Volume:        big.NewFloat(0),
		CreatedAt:     log.Timestamp,
		UpdatedAt:     log.Timestamp,
	}

	l.pairs[pair.Address] = pair

	if l.onPairCreated != nil {
		l.onPairCreated(pair)
	}

	return nil
}

// indexTokenDeposit processes token deposit event
func (l *LSSVMIndexer) indexTokenDeposit(log *LogEntry) error {
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	if len(data) < 32 {
		return fmt.Errorf("invalid token deposit data")
	}

	amount := new(big.Int).SetBytes(data[0:32])

	pair := l.getOrCreatePair(log.Address)
	pair.TokenBalance = new(big.Int).Add(pair.TokenBalance, amount)
	pair.UpdatedAt = log.Timestamp

	deposit := &LSSVMDeposit{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		PairAddress: log.Address,
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		DepositType: "token",
		TokenAmount: amount,
		Timestamp:   log.Timestamp,
	}

	l.deposits = append(l.deposits, deposit)

	if l.onDeposit != nil {
		l.onDeposit(deposit)
	}

	return nil
}

// indexTokenWithdrawal processes token withdrawal event
func (l *LSSVMIndexer) indexTokenWithdrawal(log *LogEntry) error {
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	if len(data) < 32 {
		return fmt.Errorf("invalid token withdrawal data")
	}

	amount := new(big.Int).SetBytes(data[0:32])

	pair := l.getOrCreatePair(log.Address)
	pair.TokenBalance = new(big.Int).Sub(pair.TokenBalance, amount)
	pair.UpdatedAt = log.Timestamp

	withdrawal := &LSSVMWithdrawal{
		ID:             fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		PairAddress:    log.Address,
		TxHash:         log.TxHash,
		BlockNumber:    log.BlockNumber,
		LogIndex:       log.LogIndex,
		WithdrawalType: "token",
		TokenAmount:    amount,
		Timestamp:      log.Timestamp,
	}

	l.withdrawals = append(l.withdrawals, withdrawal)

	if l.onWithdrawal != nil {
		l.onWithdrawal(withdrawal)
	}

	return nil
}

// indexNFTDeposit processes NFT deposit event
func (l *LSSVMIndexer) indexNFTDeposit(log *LogEntry) error {
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	nftIds := l.parseNFTIds(data)

	pair := l.getOrCreatePair(log.Address)
	pair.NFTBalance = append(pair.NFTBalance, nftIds...)
	pair.UpdatedAt = log.Timestamp

	deposit := &LSSVMDeposit{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		PairAddress: log.Address,
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		DepositType: "nft",
		NFTIds:      nftIds,
		Timestamp:   log.Timestamp,
	}

	l.deposits = append(l.deposits, deposit)

	if l.onDeposit != nil {
		l.onDeposit(deposit)
	}

	return nil
}

// indexNFTWithdrawal processes NFT withdrawal event
func (l *LSSVMIndexer) indexNFTWithdrawal(log *LogEntry) error {
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	nftIds := l.parseNFTIds(data)

	pair := l.getOrCreatePair(log.Address)
	pair.NFTBalance = removeNFTs(pair.NFTBalance, nftIds)
	pair.UpdatedAt = log.Timestamp

	withdrawal := &LSSVMWithdrawal{
		ID:             fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		PairAddress:    log.Address,
		TxHash:         log.TxHash,
		BlockNumber:    log.BlockNumber,
		LogIndex:       log.LogIndex,
		WithdrawalType: "nft",
		NFTIds:         nftIds,
		Timestamp:      log.Timestamp,
	}

	l.withdrawals = append(l.withdrawals, withdrawal)

	if l.onWithdrawal != nil {
		l.onWithdrawal(withdrawal)
	}

	return nil
}

// parseSwapData parses swap event data
func (l *LSSVMIndexer) parseSwapData(data []byte) ([]string, *big.Int) {
	if len(data) < 64 {
		return nil, big.NewInt(0)
	}

	// Dynamic array starts at offset in first 32 bytes
	offset := new(big.Int).SetBytes(data[0:32]).Uint64()

	if offset+32 > uint64(len(data)) {
		return nil, new(big.Int).SetBytes(data[32:64])
	}

	// Array length at offset
	arrayLen := new(big.Int).SetBytes(data[offset : offset+32]).Uint64()

	nftIds := make([]string, 0, arrayLen)
	for i := uint64(0); i < arrayLen && offset+32+32*(i+1) <= uint64(len(data)); i++ {
		start := offset + 32 + 32*i
		end := start + 32
		tokenId := new(big.Int).SetBytes(data[start:end])
		nftIds = append(nftIds, tokenId.String())
	}

	amount := new(big.Int).SetBytes(data[32:64])

	return nftIds, amount
}

// parseNFTIds parses NFT IDs from event data
func (l *LSSVMIndexer) parseNFTIds(data []byte) []string {
	if len(data) < 64 {
		return nil
	}

	offset := new(big.Int).SetBytes(data[0:32]).Uint64()
	if offset+32 > uint64(len(data)) {
		return nil
	}

	arrayLen := new(big.Int).SetBytes(data[offset : offset+32]).Uint64()

	nftIds := make([]string, 0, arrayLen)
	for i := uint64(0); i < arrayLen && offset+32+32*(i+1) <= uint64(len(data)); i++ {
		start := offset + 32 + 32*i
		end := start + 32
		tokenId := new(big.Int).SetBytes(data[start:end])
		nftIds = append(nftIds, tokenId.String())
	}

	return nftIds
}

// getOrCreatePair gets or creates a pair entry
func (l *LSSVMIndexer) getOrCreatePair(address string) *LSSVMPair {
	addr := strings.ToLower(address)
	if pair, exists := l.pairs[addr]; exists {
		return pair
	}

	pair := &LSSVMPair{
		Address:      addr,
		SpotPrice:    big.NewInt(0),
		Delta:        big.NewInt(0),
		TokenBalance: big.NewInt(0),
		NFTBalance:   make([]string, 0),
		Volume:       big.NewFloat(0),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	l.pairs[addr] = pair
	return pair
}

// GetPair returns a pair by address
func (l *LSSVMIndexer) GetPair(address string) *LSSVMPair {
	return l.pairs[strings.ToLower(address)]
}

// GetAllPairs returns all indexed pairs
func (l *LSSVMIndexer) GetAllPairs() []*LSSVMPair {
	pairs := make([]*LSSVMPair, 0, len(l.pairs))
	for _, p := range l.pairs {
		pairs = append(pairs, p)
	}
	return pairs
}

// GetSwaps returns swaps with optional filtering
func (l *LSSVMIndexer) GetSwaps(pairAddress string, limit int) []*LSSVMSwap {
	if pairAddress == "" {
		if limit > 0 && limit < len(l.swaps) {
			return l.swaps[len(l.swaps)-limit:]
		}
		return l.swaps
	}

	var filtered []*LSSVMSwap
	addr := strings.ToLower(pairAddress)
	for _, s := range l.swaps {
		if strings.ToLower(s.PairAddress) == addr {
			filtered = append(filtered, s)
		}
	}

	if limit > 0 && limit < len(filtered) {
		return filtered[len(filtered)-limit:]
	}
	return filtered
}

// Helper function to remove NFTs from balance
func removeNFTs(balance []string, toRemove []string) []string {
	removeSet := make(map[string]bool)
	for _, id := range toRemove {
		removeSet[id] = true
	}

	result := make([]string, 0)
	for _, id := range balance {
		if !removeSet[id] {
			result = append(result, id)
		}
	}
	return result
}

// LSSVMStats represents LSSVM indexer statistics
type LSSVMStats struct {
	TotalPairs        uint64     `json:"totalPairs"`
	TotalSwaps        uint64     `json:"totalSwaps"`
	TotalVolume       *big.Float `json:"totalVolume"`
	NFTsTraded        uint64     `json:"nftsTraded"`
	UniqueCollections uint64     `json:"uniqueCollections"`
	LastUpdated       time.Time  `json:"lastUpdated"`
}

// GetStats returns LSSVM indexer statistics
func (l *LSSVMIndexer) GetStats() *LSSVMStats {
	stats := &LSSVMStats{
		TotalPairs:  uint64(len(l.pairs)),
		TotalSwaps:  uint64(len(l.swaps)),
		TotalVolume: big.NewFloat(0),
		LastUpdated: time.Now(),
	}

	collections := make(map[string]bool)
	for _, pair := range l.pairs {
		if pair.Volume != nil {
			stats.TotalVolume.Add(stats.TotalVolume, pair.Volume)
		}
		if pair.NFTCollection != "" {
			collections[pair.NFTCollection] = true
		}
	}
	stats.UniqueCollections = uint64(len(collections))

	for _, swap := range l.swaps {
		stats.NFTsTraded += uint64(len(swap.NFTIds))
	}

	return stats
}
