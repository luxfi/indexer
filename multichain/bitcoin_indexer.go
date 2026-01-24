// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package multichain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// BitcoinIndexer indexes Bitcoin and Bitcoin-based chains
type BitcoinIndexer struct {
	mu sync.RWMutex

	config ChainConfig
	db     Database
	client *http.Client

	// State
	running      int32
	indexedHeight uint64
	latestHeight  uint64

	// Protocol indexers
	protocols map[ProtocolType]BitcoinProtocolIndexer

	// Stats
	stats *IndexerStats

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// BitcoinProtocolIndexer handles Bitcoin protocol-specific indexing
type BitcoinProtocolIndexer interface {
	Name() string
	IndexTransaction(ctx context.Context, tx *BitcoinTransaction, block *BitcoinBlock) error
}

// BitcoinBlock represents a Bitcoin block
type BitcoinBlock struct {
	Hash              string               `json:"hash"`
	Height            uint64               `json:"height"`
	Version           int32                `json:"version"`
	PreviousBlockHash string               `json:"previousblockhash"`
	MerkleRoot        string               `json:"merkleroot"`
	Time              int64                `json:"time"`
	Bits              string               `json:"bits"`
	Nonce             uint32               `json:"nonce"`
	Difficulty        float64              `json:"difficulty"`
	NumTx             int                  `json:"nTx"`
	Size              int                  `json:"size"`
	Weight            int                  `json:"weight"`
	Transactions      []BitcoinTransaction `json:"tx"`
}

// BitcoinTransaction represents a Bitcoin transaction
type BitcoinTransaction struct {
	TxID        string          `json:"txid"`
	Hash        string          `json:"hash"`
	Version     int32           `json:"version"`
	Size        int             `json:"size"`
	VSize       int             `json:"vsize"`
	Weight      int             `json:"weight"`
	LockTime    uint32          `json:"locktime"`
	Vin         []BitcoinVin    `json:"vin"`
	Vout        []BitcoinVout   `json:"vout"`
	Hex         string          `json:"hex"`
	BlockHash   string          `json:"blockhash,omitempty"`
	BlockHeight uint64          `json:"blockheight,omitempty"`
	BlockTime   int64           `json:"blocktime,omitempty"`
	Fee         float64         `json:"fee,omitempty"`
	// Ordinals/Inscriptions
	Inscriptions []Inscription  `json:"inscriptions,omitempty"`
	// Runes
	Runes        []RuneTransfer `json:"runes,omitempty"`
	// BRC-20
	BRC20        []BRC20Op      `json:"brc20,omitempty"`
}

// BitcoinVin represents a transaction input
type BitcoinVin struct {
	TxID        string   `json:"txid,omitempty"`
	Vout        uint32   `json:"vout,omitempty"`
	ScriptSig   Script   `json:"scriptSig,omitempty"`
	Sequence    uint32   `json:"sequence"`
	TxInWitness []string `json:"txinwitness,omitempty"`
	Coinbase    string   `json:"coinbase,omitempty"`
}

// BitcoinVout represents a transaction output
type BitcoinVout struct {
	Value        float64      `json:"value"`
	N            uint32       `json:"n"`
	ScriptPubKey ScriptPubKey `json:"scriptPubKey"`
}

// Script represents a script signature
type Script struct {
	Asm string `json:"asm"`
	Hex string `json:"hex"`
}

// ScriptPubKey represents the output script
type ScriptPubKey struct {
	Asm     string   `json:"asm"`
	Hex     string   `json:"hex"`
	Type    string   `json:"type"`
	Address string   `json:"address,omitempty"`
}

// Inscription represents an Ordinal inscription
type Inscription struct {
	ID            string `json:"id"`              // <txid>i<index>
	Number        uint64 `json:"number"`          // Inscription number
	ContentType   string `json:"content_type"`
	ContentLength uint64 `json:"content_length"`
	Content       []byte `json:"content,omitempty"`
	GenesisTxID   string `json:"genesis_txid"`
	GenesisHeight uint64 `json:"genesis_height"`
	Owner         string `json:"owner"`
	Sat           uint64 `json:"sat"`             // Satoshi ordinal
	SatRarity     string `json:"sat_rarity"`      // common, uncommon, rare, epic, legendary, mythic
	Timestamp     int64  `json:"timestamp"`
}

// RuneTransfer represents a Rune transfer
type RuneTransfer struct {
	RuneID    string `json:"rune_id"`     // <block>:<tx>
	RuneName  string `json:"rune_name"`
	Symbol    string `json:"symbol"`
	Amount    string `json:"amount"`      // uint128 as string
	From      string `json:"from"`
	To        string `json:"to"`
	Decimals  uint8  `json:"decimals"`
	Etching   bool   `json:"etching"`     // Is this an etching tx?
	Mint      bool   `json:"mint"`        // Is this a mint tx?
	Burn      bool   `json:"burn"`        // Is this a burn?
}

// BRC20Op represents a BRC-20 operation
type BRC20Op struct {
	Operation string `json:"op"`          // deploy, mint, transfer
	Ticker    string `json:"tick"`
	Amount    string `json:"amt,omitempty"`
	Max       string `json:"max,omitempty"`
	Limit     string `json:"lim,omitempty"`
	Decimals  uint8  `json:"dec,omitempty"`
	From      string `json:"from"`
	To        string `json:"to,omitempty"`
	Valid     bool   `json:"valid"`
}

// Stamp represents a Bitcoin Stamp
type Stamp struct {
	StampNumber uint64 `json:"stamp"`
	Creator     string `json:"creator"`
	CPid        string `json:"cpid"`
	BlockIndex  uint64 `json:"block_index"`
	Timestamp   int64  `json:"timestamp"`
	TxHash      string `json:"tx_hash"`
	SupplyTotal uint64 `json:"supply"`
	Divisible   bool   `json:"divisible"`
	Locked      bool   `json:"locked"`
}

// Atomical represents an Atomicals protocol item
type Atomical struct {
	AtomicalID   string `json:"atomical_id"`
	AtomicalNum  uint64 `json:"atomical_number"`
	Type         string `json:"type"` // NFT, FT, realm, container, subrealm
	Ticker       string `json:"ticker,omitempty"`
	Name         string `json:"name,omitempty"`
	Owner        string `json:"owner"`
	Value        uint64 `json:"value"`
	Bitworkc     string `json:"bitworkc,omitempty"` // Mining commitment
	Bitworkr     string `json:"bitworkr,omitempty"` // Mining reveal
	ParentRealm  string `json:"parent_realm,omitempty"`
}

// NewBitcoinIndexer creates a new Bitcoin indexer
func NewBitcoinIndexer(config ChainConfig, db Database) (*BitcoinIndexer, error) {
	ctx, cancel := context.WithCancel(context.Background())

	idx := &BitcoinIndexer{
		config:    config,
		db:        db,
		client:    &http.Client{Timeout: 60 * time.Second},
		protocols: make(map[ProtocolType]BitcoinProtocolIndexer),
		ctx:       ctx,
		cancel:    cancel,
		stats: &IndexerStats{
			ChainID:   config.ID,
			ChainName: config.Name,
			StartTime: time.Now(),
		},
	}

	// Initialize protocol indexers
	for _, proto := range config.Protocols {
		if !proto.Enabled {
			continue
		}
		if err := idx.initProtocol(proto); err != nil {
			return nil, fmt.Errorf("failed to init protocol %s: %w", proto.Type, err)
		}
	}

	return idx, nil
}

// initProtocol initializes a Bitcoin protocol indexer
func (b *BitcoinIndexer) initProtocol(config ProtocolConfig) error {
	var indexer BitcoinProtocolIndexer

	switch config.Type {
	case ProtocolOrdinals:
		indexer = NewOrdinalsIndexer(config)
	case ProtocolRunes:
		indexer = NewRunesIndexer(config)
	case ProtocolBRC20:
		indexer = NewBRC20Indexer(config)
	case ProtocolStamps:
		indexer = NewStampsIndexer(config)
	case ProtocolAtomicals:
		indexer = NewAtomicalsIndexer(config)
	default:
		indexer = NewGenericBitcoinIndexer(config)
	}

	b.protocols[config.Type] = indexer
	return nil
}

// ChainID returns the chain identifier
func (b *BitcoinIndexer) ChainID() string {
	return b.config.ID
}

// ChainType returns the chain type
func (b *BitcoinIndexer) ChainType() ChainType {
	return ChainTypeBitcoin
}

// Start starts the indexer
func (b *BitcoinIndexer) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&b.running, 0, 1) {
		return fmt.Errorf("indexer already running")
	}
	b.stats.IsRunning = true
	return nil
}

// Stop stops the indexer
func (b *BitcoinIndexer) Stop() error {
	atomic.StoreInt32(&b.running, 0)
	b.cancel()
	b.stats.IsRunning = false
	return nil
}

// IsRunning returns whether the indexer is running
func (b *BitcoinIndexer) IsRunning() bool {
	return atomic.LoadInt32(&b.running) == 1
}

// GetLatestBlock returns the latest block height
func (b *BitcoinIndexer) GetLatestBlock(ctx context.Context) (uint64, error) {
	result, err := b.rpcCall(ctx, "getblockcount", []interface{}{})
	if err != nil {
		return 0, err
	}

	height, ok := result.(float64)
	if !ok {
		return 0, fmt.Errorf("invalid block count response")
	}

	b.mu.Lock()
	b.latestHeight = uint64(height)
	b.mu.Unlock()

	return uint64(height), nil
}

// GetIndexedBlock returns the last indexed block
func (b *BitcoinIndexer) GetIndexedBlock() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.indexedHeight
}

// IndexBlock indexes a single block
func (b *BitcoinIndexer) IndexBlock(ctx context.Context, height uint64) error {
	// Get block hash
	hashResult, err := b.rpcCall(ctx, "getblockhash", []interface{}{height})
	if err != nil {
		return fmt.Errorf("failed to get block hash for height %d: %w", height, err)
	}

	blockHash, ok := hashResult.(string)
	if !ok {
		return fmt.Errorf("invalid block hash response")
	}

	// Get full block
	block, err := b.fetchBlock(ctx, blockHash)
	if err != nil {
		return fmt.Errorf("failed to fetch block %s: %w", blockHash, err)
	}
	block.Height = height

	// Process transactions through protocol indexers
	for i := range block.Transactions {
		tx := &block.Transactions[i]
		tx.BlockHash = block.Hash
		tx.BlockHeight = height
		tx.BlockTime = block.Time

		b.processTransaction(ctx, tx, block)
	}

	b.mu.Lock()
	b.indexedHeight = height
	b.stats.BlocksProcessed++
	b.stats.TxsProcessed += uint64(len(block.Transactions))
	b.stats.LastBlockTime = time.Now()
	b.mu.Unlock()

	return nil
}

// IndexBlockRange indexes a range of blocks
func (b *BitcoinIndexer) IndexBlockRange(ctx context.Context, from, to uint64) error {
	for height := from; height <= to; height++ {
		if err := b.IndexBlock(ctx, height); err != nil {
			b.stats.ErrorCount++
			b.stats.LastError = err.Error()
			continue
		}
	}
	return nil
}

// fetchBlock fetches a block from RPC
func (b *BitcoinIndexer) fetchBlock(ctx context.Context, blockHash string) (*BitcoinBlock, error) {
	// Get block with full tx data (verbosity = 2)
	result, err := b.rpcCall(ctx, "getblock", []interface{}{blockHash, 2})
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}

	var block BitcoinBlock
	if err := json.Unmarshal(data, &block); err != nil {
		return nil, err
	}

	return &block, nil
}

// processTransaction routes a transaction to protocol indexers
func (b *BitcoinIndexer) processTransaction(ctx context.Context, tx *BitcoinTransaction, block *BitcoinBlock) {
	// Parse Ordinals inscriptions from witness data
	b.parseOrdinals(tx)

	// Parse Runes from OP_RETURN
	b.parseRunes(tx)

	// Parse BRC-20 from inscriptions
	b.parseBRC20(tx)

	// Route to protocol indexers
	for _, indexer := range b.protocols {
		indexer.IndexTransaction(ctx, tx, block)
	}
}

// parseOrdinals extracts Ordinal inscriptions from a transaction
func (b *BitcoinIndexer) parseOrdinals(tx *BitcoinTransaction) {
	// Check witness data for inscription envelope
	for _, vin := range tx.Vin {
		if len(vin.TxInWitness) < 2 {
			continue
		}

		// Look for inscription envelope pattern in witness
		// Inscriptions are in taproot witness scripts
		for _, witness := range vin.TxInWitness {
			inscription := parseInscriptionFromWitness(witness)
			if inscription != nil {
				inscription.GenesisTxID = tx.TxID
				tx.Inscriptions = append(tx.Inscriptions, *inscription)
			}
		}
	}
}

// parseRunes extracts Rune operations from a transaction
func (b *BitcoinIndexer) parseRunes(tx *BitcoinTransaction) {
	// Runes use OP_RETURN with specific protocol identifier
	for _, vout := range tx.Vout {
		if vout.ScriptPubKey.Type != "nulldata" {
			continue
		}

		// Check for Runes protocol marker (OP_RETURN OP_13)
		runeTransfer := parseRuneFromOpReturn(vout.ScriptPubKey.Hex)
		if runeTransfer != nil {
			tx.Runes = append(tx.Runes, *runeTransfer)
		}
	}
}

// parseBRC20 extracts BRC-20 operations from inscriptions
func (b *BitcoinIndexer) parseBRC20(tx *BitcoinTransaction) {
	for _, inscription := range tx.Inscriptions {
		if inscription.ContentType != "text/plain" && inscription.ContentType != "application/json" {
			continue
		}

		brc20Op := parseBRC20FromContent(inscription.Content)
		if brc20Op != nil {
			tx.BRC20 = append(tx.BRC20, *brc20Op)
		}
	}
}

// rpcCall makes a JSON-RPC call to Bitcoin Core
func (b *BitcoinIndexer) rpcCall(ctx context.Context, method string, params []interface{}) (interface{}, error) {
	body := map[string]interface{}{
		"jsonrpc": "1.0",
		"id":      "indexer",
		"method":  method,
		"params":  params,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", b.config.RPC, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(jsonReader(data))

	// Add basic auth if configured
	if b.config.API != "" {
		req.SetBasicAuth("rpc", b.config.API)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Result interface{} `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if result.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", result.Error.Code, result.Error.Message)
	}

	return result.Result, nil
}

// Stats returns indexer statistics
func (b *BitcoinIndexer) Stats() *IndexerStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return &IndexerStats{
		ChainID:         b.stats.ChainID,
		ChainName:       b.stats.ChainName,
		IsRunning:       b.stats.IsRunning,
		LatestBlock:     b.latestHeight,
		IndexedBlock:    b.indexedHeight,
		BlocksBehind:    b.latestHeight - b.indexedHeight,
		BlocksProcessed: b.stats.BlocksProcessed,
		TxsProcessed:    b.stats.TxsProcessed,
		EventsProcessed: b.stats.EventsProcessed,
		ErrorCount:      b.stats.ErrorCount,
		LastError:       b.stats.LastError,
		LastBlockTime:   b.stats.LastBlockTime,
		StartTime:       b.stats.StartTime,
		Uptime:          time.Since(b.stats.StartTime),
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

// parseInscriptionFromWitness extracts inscription from witness data
func parseInscriptionFromWitness(witness string) *Inscription {
	// Decode hex witness data
	data := hexToBytes(witness)
	if len(data) < 10 {
		return nil
	}

	// Look for inscription envelope pattern:
	// OP_FALSE (0x00) OP_IF (0x63) "ord" (0x036f7264) OP_1 (0x51) content_type OP_0 (0x00) content OP_ENDIF (0x68)

	// Find "ord" marker
	ordMarker := []byte{0x03, 0x6f, 0x72, 0x64} // length-prefixed "ord"
	ordIdx := -1
	for i := 0; i <= len(data)-len(ordMarker); i++ {
		if bytes.Equal(data[i:i+len(ordMarker)], ordMarker) {
			ordIdx = i
			break
		}
	}
	if ordIdx < 0 {
		return nil
	}

	// Parse content type and content
	pos := ordIdx + 4
	if pos >= len(data) {
		return nil
	}

	inscription := &Inscription{}

	// Parse fields until OP_ENDIF (0x68) or OP_0 (0x00)
	for pos < len(data) {
		op := data[pos]
		pos++

		switch op {
		case 0x01: // OP_1 - content type follows
			if pos >= len(data) {
				return nil
			}
			length := int(data[pos])
			pos++
			if pos+length > len(data) {
				return nil
			}
			inscription.ContentType = string(data[pos : pos+length])
			pos += length

		case 0x00: // OP_0 - content follows
			// Read content length (could be push opcode)
			if pos >= len(data) {
				break
			}

			// Handle different push opcodes
			contentLen := 0
			pushOp := data[pos]
			pos++

			if pushOp <= 0x4b { // Direct push 1-75 bytes
				contentLen = int(pushOp)
			} else if pushOp == 0x4c { // OP_PUSHDATA1
				if pos >= len(data) {
					break
				}
				contentLen = int(data[pos])
				pos++
			} else if pushOp == 0x4d { // OP_PUSHDATA2
				if pos+2 > len(data) {
					break
				}
				contentLen = int(data[pos]) | int(data[pos+1])<<8
				pos += 2
			} else if pushOp == 0x4e { // OP_PUSHDATA4
				if pos+4 > len(data) {
					break
				}
				contentLen = int(data[pos]) | int(data[pos+1])<<8 | int(data[pos+2])<<16 | int(data[pos+3])<<24
				pos += 4
			}

			if pos+contentLen <= len(data) {
				inscription.Content = data[pos : pos+contentLen]
				inscription.ContentLength = uint64(contentLen)
				pos += contentLen
			}

		case 0x68: // OP_ENDIF - end of envelope
			if inscription.ContentType != "" || len(inscription.Content) > 0 {
				return inscription
			}
			return nil
		}
	}

	if inscription.ContentType != "" || len(inscription.Content) > 0 {
		return inscription
	}
	return nil
}

// parseRuneFromOpReturn extracts Rune from OP_RETURN data
func parseRuneFromOpReturn(hex string) *RuneTransfer {
	data := hexToBytes(hex)
	if len(data) < 4 {
		return nil
	}

	// Runes protocol: OP_RETURN (0x6a) OP_13 (0x5d) <payload>
	// Find OP_RETURN OP_13 pattern
	if data[0] != 0x6a { // OP_RETURN
		return nil
	}

	// Check for Runes magic number (OP_13 = 0x5d)
	pos := 1
	if pos >= len(data) {
		return nil
	}

	// Skip push length byte if present
	if data[pos] <= 0x4b {
		pos++
	}

	if pos >= len(data) || data[pos] != 0x5d { // OP_13 = Runes identifier
		return nil
	}
	pos++

	// Parse Rune payload using LEB128 varint encoding
	rune := &RuneTransfer{}

	// Read edicts (transfers)
	if pos < len(data) {
		// First varint is the number of edicts or flags
		flags, n := decodeVarint(data[pos:])
		if n == 0 {
			return nil
		}
		pos += n

		// Check if this is an etching (bit 0 set)
		rune.Etching = (flags & 1) != 0

		// Check if this is a mint (bit 1 set)
		rune.Mint = (flags & 2) != 0

		// Parse rune ID if present
		if pos < len(data) {
			blockNum, n := decodeVarint(data[pos:])
			if n > 0 {
				pos += n
				txIdx, n := decodeVarint(data[pos:])
				if n > 0 {
					pos += n
					rune.RuneID = fmt.Sprintf("%d:%d", blockNum, txIdx)
				}
			}
		}

		// Parse amount if present
		if pos < len(data) {
			amount, n := decodeVarint(data[pos:])
			if n > 0 {
				rune.Amount = fmt.Sprintf("%d", amount)
			}
		}
	}

	if rune.RuneID != "" || rune.Etching || rune.Mint {
		return rune
	}
	return nil
}

// decodeVarint decodes a LEB128 varint from bytes
func decodeVarint(data []byte) (uint64, int) {
	var result uint64
	var shift uint
	for i, b := range data {
		if i >= 10 { // Max 10 bytes for uint64
			return 0, 0
		}
		result |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return result, i + 1
		}
		shift += 7
	}
	return 0, 0
}

// parseBRC20FromContent extracts BRC-20 operation from inscription content
func parseBRC20FromContent(content []byte) *BRC20Op {
	if len(content) == 0 {
		return nil
	}

	// BRC-20 format: {"p":"brc-20","op":"deploy","tick":"ordi","max":"21000000","lim":"1000"}
	var data map[string]interface{}
	if err := json.Unmarshal(content, &data); err != nil {
		return nil
	}

	// Check protocol identifier
	p, ok := data["p"].(string)
	if !ok || p != "brc-20" {
		return nil
	}

	op := &BRC20Op{}

	// Parse operation type
	if opType, ok := data["op"].(string); ok {
		op.Operation = opType
	} else {
		return nil
	}

	// Parse ticker (required)
	if tick, ok := data["tick"].(string); ok {
		op.Ticker = tick
	} else {
		return nil
	}

	// Parse optional fields based on operation type
	switch op.Operation {
	case "deploy":
		if max, ok := data["max"].(string); ok {
			op.Max = max
		}
		if lim, ok := data["lim"].(string); ok {
			op.Limit = lim
		}
		if dec, ok := data["dec"].(float64); ok {
			op.Decimals = uint8(dec)
		} else {
			op.Decimals = 18 // Default
		}
		op.Valid = op.Max != "" && op.Ticker != ""

	case "mint":
		if amt, ok := data["amt"].(string); ok {
			op.Amount = amt
		}
		op.Valid = op.Amount != "" && op.Ticker != ""

	case "transfer":
		if amt, ok := data["amt"].(string); ok {
			op.Amount = amt
		}
		op.Valid = op.Amount != "" && op.Ticker != ""
	}

	return op
}

// =============================================================================
// Bitcoin Protocol Indexer Stubs
// =============================================================================

type GenericBitcoinIndexer struct{ config ProtocolConfig }
func NewGenericBitcoinIndexer(config ProtocolConfig) *GenericBitcoinIndexer { return &GenericBitcoinIndexer{config: config} }
func (g *GenericBitcoinIndexer) Name() string { return "generic_bitcoin" }
func (g *GenericBitcoinIndexer) IndexTransaction(ctx context.Context, tx *BitcoinTransaction, block *BitcoinBlock) error { return nil }

// Ordinals Indexer
type OrdinalsIndexer struct{ config ProtocolConfig }
func NewOrdinalsIndexer(config ProtocolConfig) *OrdinalsIndexer { return &OrdinalsIndexer{config: config} }
func (o *OrdinalsIndexer) Name() string { return "ordinals" }
func (o *OrdinalsIndexer) IndexTransaction(ctx context.Context, tx *BitcoinTransaction, block *BitcoinBlock) error {
	// Index inscriptions
	for _, inscription := range tx.Inscriptions {
		_ = inscription // Store inscription
	}
	return nil
}

// Runes Indexer
type RunesIndexer struct{ config ProtocolConfig }
func NewRunesIndexer(config ProtocolConfig) *RunesIndexer { return &RunesIndexer{config: config} }
func (r *RunesIndexer) Name() string { return "runes" }
func (r *RunesIndexer) IndexTransaction(ctx context.Context, tx *BitcoinTransaction, block *BitcoinBlock) error {
	// Index rune transfers, etchings, mints
	for _, rune := range tx.Runes {
		_ = rune // Store rune operation
	}
	return nil
}

// BRC-20 Indexer
type BRC20Indexer struct{ config ProtocolConfig }
func NewBRC20Indexer(config ProtocolConfig) *BRC20Indexer { return &BRC20Indexer{config: config} }
func (b *BRC20Indexer) Name() string { return "brc20" }
func (b *BRC20Indexer) IndexTransaction(ctx context.Context, tx *BitcoinTransaction, block *BitcoinBlock) error {
	// Index BRC-20 operations
	for _, op := range tx.BRC20 {
		_ = op // Store BRC-20 operation
	}
	return nil
}

// Stamps Indexer
type StampsIndexer struct{ config ProtocolConfig }
func NewStampsIndexer(config ProtocolConfig) *StampsIndexer { return &StampsIndexer{config: config} }
func (s *StampsIndexer) Name() string { return "stamps" }
func (s *StampsIndexer) IndexTransaction(ctx context.Context, tx *BitcoinTransaction, block *BitcoinBlock) error {
	// Index Bitcoin Stamps (Counterparty-based)
	return nil
}

// Atomicals Indexer
type AtomicalsIndexer struct{ config ProtocolConfig }
func NewAtomicalsIndexer(config ProtocolConfig) *AtomicalsIndexer { return &AtomicalsIndexer{config: config} }
func (a *AtomicalsIndexer) Name() string { return "atomicals" }
func (a *AtomicalsIndexer) IndexTransaction(ctx context.Context, tx *BitcoinTransaction, block *BitcoinBlock) error {
	// Index Atomicals protocol items
	return nil
}
