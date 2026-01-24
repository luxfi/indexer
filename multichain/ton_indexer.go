// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package multichain

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// TON NFT operation codes (TEP-62)
const (
	TONOpTransfer          uint32 = 0x5fcc3d14 // NFT transfer
	TONOpOwnershipAssigned uint32 = 0x05138d91 // Ownership assigned
	TONOpGetStaticData     uint32 = 0x2fcb26a2 // Get static data
	TONOpReportStaticData  uint32 = 0x8b771735 // Report static data
	TONOpGetRoyaltyParams  uint32 = 0x693d3950 // Get royalty params (TEP-66)
	TONOpReportRoyalty     uint32 = 0xa8cb00ad // Report royalty params
)

// TON Marketplace addresses
const (
	GetGemsMarketplace    = "EQBYTuYbLf8INxFtD8tQeNk5ZLy-nAX9ahQbG_yl1qQ-GEMS"
	TONDiamondsMarketplace = "EQA8gwSB3bP-VmzPLTQOIaW-8dFOH_kVVvz7eyVm8MAvPYv-"
	FragmentMarketplace   = "EQAvlWFDxGF2lXm67y4yzC17wYKD9A0guwPkMs1gOsM__NOT"
)

// TONIndexer indexes TON blockchain
type TONIndexer struct {
	mu sync.RWMutex

	config ChainConfig
	db     Database
	client *http.Client

	// State
	running     int32
	indexedLt   uint64
	latestLt    uint64

	// NFT sale tracking
	nftSales []*TONNFTSale

	// Stats
	stats *IndexerStats

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// TONNFTSale represents an NFT sale on TON
type TONNFTSale struct {
	TxHash         string    `json:"txHash"`
	Lt             uint64    `json:"lt"`
	Utime          int64     `json:"utime"`
	Marketplace    string    `json:"marketplace"` // getgems, tondiamonds, fragment
	Collection     string    `json:"collection"`
	NFTAddress     string    `json:"nftAddress"`
	Seller         string    `json:"seller"`
	Buyer          string    `json:"buyer"`
	Price          *big.Int  `json:"price"` // in nanotons
	RoyaltyAmount  *big.Int  `json:"royaltyAmount"`
	MarketplaceFee *big.Int  `json:"marketplaceFee"`
	Timestamp      time.Time `json:"timestamp"`
}

// TONTransaction represents a TON transaction
type TONTransaction struct {
	Hash        string            `json:"hash"`
	Lt          uint64            `json:"lt"`
	Utime       int64             `json:"utime"`
	Fee         string            `json:"fee"`
	StorageFee  string            `json:"storage_fee"`
	OtherFee    string            `json:"other_fee"`
	InMsg       *TONMessage       `json:"in_msg"`
	OutMsgs     []*TONMessage     `json:"out_msgs"`
	Account     string            `json:"account"`
	Success     bool              `json:"success"`
	ExitCode    int               `json:"exit_code"`
	Description json.RawMessage   `json:"description"`
}

// TONMessage represents a TON message
type TONMessage struct {
	Hash        string `json:"hash"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Value       string `json:"value"`
	FwdFee      string `json:"fwd_fee"`
	IhrFee      string `json:"ihr_fee"`
	CreatedLt   uint64 `json:"created_lt"`
	BodyHash    string `json:"body_hash"`
	MsgData     *TONMsgData `json:"msg_data"`
	OpCode      uint32 `json:"op_code,omitempty"`
}

// TONMsgData represents message data
type TONMsgData struct {
	Type string `json:"@type"`
	Body string `json:"body,omitempty"` // base64 encoded
	Text string `json:"text,omitempty"`
}

// TONBlock represents a TON block
type TONBlock struct {
	Workchain int32  `json:"workchain"`
	Shard     int64  `json:"shard"`
	Seqno     uint32 `json:"seqno"`
	RootHash  string `json:"root_hash"`
	FileHash  string `json:"file_hash"`
}

// NewTonIndexer creates a new TON indexer
func NewTonIndexer(config ChainConfig, db Database) (*TONIndexer, error) {
	ctx, cancel := context.WithCancel(context.Background())

	idx := &TONIndexer{
		config:   config,
		db:       db,
		client:   &http.Client{Timeout: 30 * time.Second},
		nftSales: make([]*TONNFTSale, 0),
		ctx:      ctx,
		cancel:   cancel,
		stats: &IndexerStats{
			ChainID:   config.ID,
			ChainName: config.Name,
			StartTime: time.Now(),
		},
	}

	return idx, nil
}

// ChainID returns the chain identifier
func (t *TONIndexer) ChainID() string {
	return t.config.ID
}

// ChainType returns the chain type
func (t *TONIndexer) ChainType() ChainType {
	return ChainTypeTon
}

// Start starts the indexer
func (t *TONIndexer) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&t.running, 0, 1) {
		return fmt.Errorf("indexer already running")
	}
	t.stats.IsRunning = true
	return nil
}

// Stop stops the indexer
func (t *TONIndexer) Stop() error {
	atomic.StoreInt32(&t.running, 0)
	t.cancel()
	t.stats.IsRunning = false
	return nil
}

// IsRunning returns whether the indexer is running
func (t *TONIndexer) IsRunning() bool {
	return atomic.LoadInt32(&t.running) == 1
}

// GetLatestBlock returns the latest masterchain seqno
func (t *TONIndexer) GetLatestBlock(ctx context.Context) (uint64, error) {
	result, err := t.apiCall(ctx, "/getMasterchainInfo", nil)
	if err != nil {
		return 0, err
	}

	var info struct {
		Last struct {
			Seqno uint32 `json:"seqno"`
		} `json:"last"`
	}
	if err := json.Unmarshal(result, &info); err != nil {
		return 0, err
	}

	t.mu.Lock()
	t.latestLt = uint64(info.Last.Seqno)
	t.mu.Unlock()

	return uint64(info.Last.Seqno), nil
}

// GetIndexedBlock returns the last indexed seqno
func (t *TONIndexer) GetIndexedBlock() uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.indexedLt
}

// IndexBlock indexes transactions in a block
func (t *TONIndexer) IndexBlock(ctx context.Context, blockNumber uint64) error {
	// Get transactions for the block
	txs, err := t.getBlockTransactions(ctx, blockNumber)
	if err != nil {
		return err
	}

	// Process each transaction
	for _, tx := range txs {
		t.processTransaction(ctx, tx)
	}

	t.mu.Lock()
	t.indexedLt = blockNumber
	t.stats.BlocksProcessed++
	t.stats.TxsProcessed += uint64(len(txs))
	t.stats.LastBlockTime = time.Now()
	t.mu.Unlock()

	return nil
}

// IndexBlockRange indexes a range of blocks
func (t *TONIndexer) IndexBlockRange(ctx context.Context, from, to uint64) error {
	for seqno := from; seqno <= to; seqno++ {
		if err := t.IndexBlock(ctx, seqno); err != nil {
			t.stats.ErrorCount++
			t.stats.LastError = err.Error()
			continue
		}
	}
	return nil
}

// getBlockTransactions fetches transactions for a masterchain block
func (t *TONIndexer) getBlockTransactions(ctx context.Context, seqno uint64) ([]*TONTransaction, error) {
	params := map[string]interface{}{
		"seqno": seqno,
	}
	result, err := t.apiCall(ctx, "/getBlockTransactions", params)
	if err != nil {
		return nil, err
	}

	var response struct {
		Transactions []*TONTransaction `json:"transactions"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, err
	}

	return response.Transactions, nil
}

// processTransaction processes a transaction for NFT events
func (t *TONIndexer) processTransaction(ctx context.Context, tx *TONTransaction) {
	if tx.InMsg == nil {
		return
	}

	// Check if this is an NFT-related transaction
	opCode := t.extractOpCode(tx.InMsg)
	if opCode == 0 {
		return
	}

	switch opCode {
	case TONOpTransfer:
		t.processNFTTransfer(tx)
	case TONOpOwnershipAssigned:
		t.processOwnershipAssigned(tx)
	}

	// Check marketplace addresses for sales
	t.checkMarketplaceSale(tx)
}

// extractOpCode extracts the operation code from a message
func (t *TONIndexer) extractOpCode(msg *TONMessage) uint32 {
	if msg.MsgData == nil || msg.MsgData.Body == "" {
		return 0
	}

	data, err := base64.StdEncoding.DecodeString(msg.MsgData.Body)
	if err != nil || len(data) < 4 {
		return 0
	}

	return binary.BigEndian.Uint32(data[:4])
}

// processNFTTransfer processes an NFT transfer message
func (t *TONIndexer) processNFTTransfer(tx *TONTransaction) {
	if tx.InMsg == nil || tx.InMsg.MsgData == nil {
		return
	}

	data, err := base64.StdEncoding.DecodeString(tx.InMsg.MsgData.Body)
	if err != nil || len(data) < 36 { // op (4) + query_id (8) + new_owner (33+)
		return
	}

	// Parse transfer data
	// op: uint32, query_id: uint64, new_owner: MsgAddress
	// For now just track the event
	t.mu.Lock()
	t.stats.EventsProcessed++
	t.mu.Unlock()
}

// processOwnershipAssigned processes ownership assigned notification
func (t *TONIndexer) processOwnershipAssigned(tx *TONTransaction) {
	if tx.InMsg == nil || tx.InMsg.MsgData == nil {
		return
	}

	t.mu.Lock()
	t.stats.EventsProcessed++
	t.mu.Unlock()
}

// checkMarketplaceSale checks if transaction is a marketplace sale
func (t *TONIndexer) checkMarketplaceSale(tx *TONTransaction) {
	if tx.InMsg == nil {
		return
	}

	// Check if source or destination is a known marketplace
	marketplace := ""
	switch tx.InMsg.Source {
	case GetGemsMarketplace:
		marketplace = "getgems"
	case TONDiamondsMarketplace:
		marketplace = "tondiamonds"
	case FragmentMarketplace:
		marketplace = "fragment"
	}

	if marketplace == "" {
		switch tx.InMsg.Destination {
		case GetGemsMarketplace:
			marketplace = "getgems"
		case TONDiamondsMarketplace:
			marketplace = "tondiamonds"
		case FragmentMarketplace:
			marketplace = "fragment"
		}
	}

	if marketplace == "" {
		return
	}

	// Parse value as price
	price := new(big.Int)
	price.SetString(tx.InMsg.Value, 10)

	sale := &TONNFTSale{
		TxHash:         tx.Hash,
		Lt:             tx.Lt,
		Utime:          tx.Utime,
		Marketplace:    marketplace,
		NFTAddress:     tx.Account,
		Seller:         tx.InMsg.Source,
		Buyer:          tx.InMsg.Destination,
		Price:          price,
		RoyaltyAmount:  big.NewInt(0),
		MarketplaceFee: big.NewInt(0),
		Timestamp:      time.Unix(tx.Utime, 0),
	}

	t.mu.Lock()
	t.nftSales = append(t.nftSales, sale)
	t.stats.EventsProcessed++
	t.mu.Unlock()
}

// apiCall makes an API call to TON Center or similar API
func (t *TONIndexer) apiCall(ctx context.Context, endpoint string, params map[string]interface{}) (json.RawMessage, error) {
	url := t.config.RPC + endpoint
	if t.config.API != "" {
		url = t.config.API + endpoint
	}

	// Build query params
	if len(params) > 0 {
		url += "?"
		first := true
		for k, v := range params {
			if !first {
				url += "&"
			}
			url += fmt.Sprintf("%s=%v", k, v)
			first = false
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error,omitempty"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if !result.OK {
		return nil, fmt.Errorf("TON API error: %s", result.Error)
	}

	return result.Result, nil
}

// Stats returns indexer statistics
func (t *TONIndexer) Stats() *IndexerStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return &IndexerStats{
		ChainID:         t.stats.ChainID,
		ChainName:       t.stats.ChainName,
		IsRunning:       t.stats.IsRunning,
		LatestBlock:     t.latestLt,
		IndexedBlock:    t.indexedLt,
		BlocksBehind:    t.latestLt - t.indexedLt,
		BlocksProcessed: t.stats.BlocksProcessed,
		TxsProcessed:    t.stats.TxsProcessed,
		EventsProcessed: t.stats.EventsProcessed,
		ErrorCount:      t.stats.ErrorCount,
		LastError:       t.stats.LastError,
		LastBlockTime:   t.stats.LastBlockTime,
		StartTime:       t.stats.StartTime,
		Uptime:          time.Since(t.stats.StartTime),
	}
}

// GetNFTSales returns recent NFT sales
func (t *TONIndexer) GetNFTSales(limit int) []*TONNFTSale {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if limit > len(t.nftSales) {
		limit = len(t.nftSales)
	}
	return t.nftSales[len(t.nftSales)-limit:]
}

// =============================================================================
// TON Cell/BOC Parsing Helpers
// =============================================================================

// TONCell represents a TON Cell
type TONCell struct {
	Data     []byte
	BitLen   int
	Refs     []*TONCell
}

// parseBOC parses a Bag of Cells from base64 encoded data
func parseBOC(b64Data string) (*TONCell, error) {
	data, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return nil, fmt.Errorf("base64 decode error: %w", err)
	}

	if len(data) < 5 {
		return nil, fmt.Errorf("BOC too short")
	}

	// BOC magic bytes: b5ee9c72
	if data[0] != 0xb5 || data[1] != 0xee || data[2] != 0x9c || data[3] != 0x72 {
		return nil, fmt.Errorf("invalid BOC magic")
	}

	// Simplified BOC parsing - real implementation would be more complex
	// For now return a basic cell with the raw data
	return &TONCell{
		Data:   data[4:],
		BitLen: (len(data) - 4) * 8,
	}, nil
}

// parseTONAddress parses a TON address from cell data
func parseTONAddress(data []byte, offset int) (string, int, error) {
	if offset >= len(data) {
		return "", offset, fmt.Errorf("offset out of bounds")
	}

	// TON address format: 2 bits addr_type + workchain (if external) + hash
	// Standard address: workchain (int8) + 256 bits hash
	if offset+33 > len(data) {
		return "", offset, fmt.Errorf("insufficient data for address")
	}

	workchain := int8(data[offset])
	hash := data[offset+1 : offset+33]

	// Format as user-friendly address (simplified)
	addr := fmt.Sprintf("%d:%x", workchain, hash)
	return addr, offset + 33, nil
}

// parseCoins parses a VarUInteger 16 (coins) from cell data
func parseCoins(data []byte, offset int) (*big.Int, int, error) {
	if offset >= len(data) {
		return nil, offset, fmt.Errorf("offset out of bounds")
	}

	// First 4 bits indicate length of the value in bytes
	lenByte := data[offset]
	length := int(lenByte >> 4)

	if length == 0 {
		return big.NewInt(0), offset + 1, nil
	}

	if offset+1+length > len(data) {
		return nil, offset, fmt.Errorf("insufficient data for coins")
	}

	value := new(big.Int).SetBytes(data[offset+1 : offset+1+length])
	return value, offset + 1 + length, nil
}
