// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package multichain

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// =============================================================================
// Base58 Encoding/Decoding (Bitcoin alphabet)
// =============================================================================

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

var base58AlphabetIdx [256]int

func init() {
	for i := range base58AlphabetIdx {
		base58AlphabetIdx[i] = -1
	}
	for i, c := range base58Alphabet {
		base58AlphabetIdx[c] = i
	}
}

// base58Decode decodes a base58-encoded string
func base58Decode(s string) ([]byte, error) {
	if len(s) == 0 {
		return []byte{}, nil
	}

	// Count leading '1's (zeros in output)
	zeros := 0
	for _, c := range s {
		if c != '1' {
			break
		}
		zeros++
	}

	// Allocate enough space for the result
	// A base58 digit represents log(58)/log(256) = 0.73 bytes
	result := make([]byte, 0, len(s))

	for _, c := range s {
		idx := base58AlphabetIdx[c]
		if idx == -1 {
			return nil, fmt.Errorf("invalid base58 character: %c", c)
		}

		carry := idx
		for i := len(result) - 1; i >= 0; i-- {
			carry += 58 * int(result[i])
			result[i] = byte(carry & 0xff)
			carry >>= 8
		}

		for carry > 0 {
			result = append([]byte{byte(carry & 0xff)}, result...)
			carry >>= 8
		}
	}

	// Add leading zeros
	for i := 0; i < zeros; i++ {
		result = append([]byte{0}, result...)
	}

	return result, nil
}

// base58Encode encodes bytes to base58 string
func base58Encode(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	// Count leading zeros
	zeros := 0
	for _, b := range data {
		if b != 0 {
			break
		}
		zeros++
	}

	// Allocate enough space for the result
	result := make([]byte, 0, len(data)*138/100+1)

	for _, b := range data {
		carry := int(b)
		for i := len(result) - 1; i >= 0; i-- {
			carry += 256 * int(result[i])
			result[i] = byte(carry % 58)
			carry /= 58
		}

		for carry > 0 {
			result = append([]byte{byte(carry % 58)}, result...)
			carry /= 58
		}
	}

	// Convert to alphabet
	for i, b := range result {
		result[i] = base58Alphabet[b]
	}

	// Add leading '1's for zeros
	prefix := make([]byte, zeros)
	for i := range prefix {
		prefix[i] = '1'
	}

	return string(append(prefix, result...))
}

// SolanaIndexer indexes Solana blockchain
type SolanaIndexer struct {
	mu sync.RWMutex

	config ChainConfig
	db     Database
	client *http.Client

	// State
	running     int32
	indexedSlot uint64
	latestSlot  uint64

	// Protocol indexers
	protocols map[ProtocolType]SolanaProtocolIndexer

	// Program ID to protocol mapping for fast lookup
	programToProtocol map[string]SolanaProtocolIndexer

	// Stats
	stats *IndexerStats

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// SolanaProtocolIndexer handles Solana protocol-specific indexing
type SolanaProtocolIndexer interface {
	Name() string
	ProgramIDs() []string
	IndexTransaction(ctx context.Context, tx *SolanaTransaction) error
}

// SolanaBlock represents a Solana block (slot)
type SolanaBlock struct {
	Slot              uint64              `json:"slot"`
	Blockhash         string              `json:"blockhash"`
	PreviousBlockhash string              `json:"previousBlockhash"`
	ParentSlot        uint64              `json:"parentSlot"`
	BlockTime         int64               `json:"blockTime"`
	Transactions      []SolanaTransaction `json:"transactions"`
	Rewards           []SolanaReward      `json:"rewards"`
}

// SolanaTransaction represents a Solana transaction
type SolanaTransaction struct {
	Signature         string                    `json:"signature"`
	Slot              uint64                    `json:"slot"`
	BlockTime         int64                     `json:"blockTime"`
	Fee               uint64                    `json:"fee"`
	Status            string                    `json:"status"` // "success" or "failed"
	Err               interface{}               `json:"err"`
	Message           SolanaMessage             `json:"message"`
	Meta              SolanaMeta                `json:"meta"`
	InnerInstructions []SolanaInnerInstruction  `json:"innerInstructions"`
}

// SolanaMessage represents transaction message
type SolanaMessage struct {
	AccountKeys     []string            `json:"accountKeys"`
	Header          SolanaHeader        `json:"header"`
	Instructions    []SolanaInstruction `json:"instructions"`
	RecentBlockhash string              `json:"recentBlockhash"`
}

// SolanaHeader represents message header
type SolanaHeader struct {
	NumRequiredSignatures       int `json:"numRequiredSignatures"`
	NumReadonlySignedAccounts   int `json:"numReadonlySignedAccounts"`
	NumReadonlyUnsignedAccounts int `json:"numReadonlyUnsignedAccounts"`
}

// SolanaInstruction represents a Solana instruction
type SolanaInstruction struct {
	ProgramIDIndex int    `json:"programIdIndex"`
	Accounts       []int  `json:"accounts"`
	Data           string `json:"data"` // Base58 encoded
}

// SolanaInnerInstruction represents inner instructions
type SolanaInnerInstruction struct {
	Index        int                 `json:"index"`
	Instructions []SolanaInstruction `json:"instructions"`
}

// SolanaMeta represents transaction metadata
type SolanaMeta struct {
	Err               interface{}          `json:"err"`
	Fee               uint64               `json:"fee"`
	PreBalances       []uint64             `json:"preBalances"`
	PostBalances      []uint64             `json:"postBalances"`
	PreTokenBalances  []SolanaTokenBalance `json:"preTokenBalances"`
	PostTokenBalances []SolanaTokenBalance `json:"postTokenBalances"`
	LogMessages       []string             `json:"logMessages"`
	ComputeUnitsUsed  uint64               `json:"computeUnitsConsumed"`
}

// SolanaTokenBalance represents token balance change
type SolanaTokenBalance struct {
	AccountIndex  int               `json:"accountIndex"`
	Mint          string            `json:"mint"`
	Owner         string            `json:"owner"`
	ProgramID     string            `json:"programId"`
	UITokenAmount SolanaTokenAmount `json:"uiTokenAmount"`
}

// SolanaTokenAmount represents token amount
type SolanaTokenAmount struct {
	Amount         string  `json:"amount"`
	Decimals       int     `json:"decimals"`
	UIAmount       float64 `json:"uiAmount"`
	UIAmountString string  `json:"uiAmountString"`
}

// SolanaReward represents block rewards
type SolanaReward struct {
	Pubkey      string `json:"pubkey"`
	Lamports    int64  `json:"lamports"`
	PostBalance uint64 `json:"postBalance"`
	RewardType  string `json:"rewardType"`
	Commission  *int   `json:"commission,omitempty"`
}

// =============================================================================
// NFT Sale Types
// =============================================================================

// SolanaNFTSale represents an NFT sale on Solana
type SolanaNFTSale struct {
	Signature      string `json:"signature"`
	Slot           uint64 `json:"slot"`
	BlockTime      int64  `json:"blockTime"`
	Marketplace    string `json:"marketplace"` // metaplex, magic_eden, tensor
	Mint           string `json:"mint"`
	Buyer          string `json:"buyer"`
	Seller         string `json:"seller"`
	Price          uint64 `json:"price"` // in lamports
	RoyaltyAmount  uint64 `json:"royaltyAmount"`
	MarketplaceFee uint64 `json:"marketplaceFee"`
}

// MetaplexMetadata represents Metaplex token metadata
type MetaplexMetadata struct {
	Mint                 string            `json:"mint"`
	UpdateAuthority      string            `json:"updateAuthority"`
	Name                 string            `json:"name"`
	Symbol               string            `json:"symbol"`
	URI                  string            `json:"uri"`
	SellerFeeBasisPoints uint16            `json:"sellerFeeBasisPoints"`
	Creators             []MetaplexCreator `json:"creators"`
	PrimarySaleHappened  bool              `json:"primarySaleHappened"`
	IsMutable            bool              `json:"isMutable"`
}

// MetaplexCreator represents a creator in Metaplex metadata
type MetaplexCreator struct {
	Address  string `json:"address"`
	Verified bool   `json:"verified"`
	Share    uint8  `json:"share"` // percentage (0-100)
}

// =============================================================================
// Borsh Deserialization
// =============================================================================

// BorshReader provides Borsh deserialization for Solana instruction data
type BorshReader struct {
	data []byte
	pos  int
}

// NewBorshReader creates a new Borsh reader from base58-encoded data
func NewBorshReader(base58Data string) (*BorshReader, error) {
	data, err := base58Decode(base58Data)
	if err != nil {
		return nil, fmt.Errorf("base58 decode failed: %w", err)
	}
	return &BorshReader{data: data, pos: 0}, nil
}

// NewBorshReaderFromBytes creates a new Borsh reader from raw bytes
func NewBorshReaderFromBytes(data []byte) *BorshReader {
	return &BorshReader{data: data, pos: 0}
}

// Remaining returns the number of unread bytes
func (r *BorshReader) Remaining() int {
	return len(r.data) - r.pos
}

// ReadU8 reads a single byte
func (r *BorshReader) ReadU8() (uint8, error) {
	if r.pos >= len(r.data) {
		return 0, fmt.Errorf("buffer underflow: need 1 byte, have %d", r.Remaining())
	}
	v := r.data[r.pos]
	r.pos++
	return v, nil
}

// ReadU16 reads a little-endian uint16
func (r *BorshReader) ReadU16() (uint16, error) {
	if r.pos+2 > len(r.data) {
		return 0, fmt.Errorf("buffer underflow: need 2 bytes, have %d", r.Remaining())
	}
	v := binary.LittleEndian.Uint16(r.data[r.pos:])
	r.pos += 2
	return v, nil
}

// ReadU32 reads a little-endian uint32
func (r *BorshReader) ReadU32() (uint32, error) {
	if r.pos+4 > len(r.data) {
		return 0, fmt.Errorf("buffer underflow: need 4 bytes, have %d", r.Remaining())
	}
	v := binary.LittleEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return v, nil
}

// ReadU64 reads a little-endian uint64
func (r *BorshReader) ReadU64() (uint64, error) {
	if r.pos+8 > len(r.data) {
		return 0, fmt.Errorf("buffer underflow: need 8 bytes, have %d", r.Remaining())
	}
	v := binary.LittleEndian.Uint64(r.data[r.pos:])
	r.pos += 8
	return v, nil
}

// ReadI64 reads a little-endian int64
func (r *BorshReader) ReadI64() (int64, error) {
	v, err := r.ReadU64()
	if err != nil {
		return 0, err
	}
	return int64(v), nil
}

// ReadPubkey reads a 32-byte Solana public key and returns base58 encoded
func (r *BorshReader) ReadPubkey() (string, error) {
	if r.pos+32 > len(r.data) {
		return "", fmt.Errorf("buffer underflow: need 32 bytes for pubkey, have %d", r.Remaining())
	}
	pubkey := r.data[r.pos : r.pos+32]
	r.pos += 32
	return base58Encode(pubkey), nil
}

// ReadPubkeyBytes reads a 32-byte Solana public key as raw bytes
func (r *BorshReader) ReadPubkeyBytes() ([]byte, error) {
	if r.pos+32 > len(r.data) {
		return nil, fmt.Errorf("buffer underflow: need 32 bytes for pubkey, have %d", r.Remaining())
	}
	pubkey := make([]byte, 32)
	copy(pubkey, r.data[r.pos:r.pos+32])
	r.pos += 32
	return pubkey, nil
}

// ReadString reads a Borsh string (4-byte length prefix + UTF-8 data)
func (r *BorshReader) ReadString() (string, error) {
	length, err := r.ReadU32()
	if err != nil {
		return "", fmt.Errorf("failed to read string length: %w", err)
	}
	if r.pos+int(length) > len(r.data) {
		return "", fmt.Errorf("buffer underflow: need %d bytes for string, have %d", length, r.Remaining())
	}
	s := string(r.data[r.pos : r.pos+int(length)])
	r.pos += int(length)
	return s, nil
}

// ReadBytes reads a fixed number of bytes
func (r *BorshReader) ReadBytes(n int) ([]byte, error) {
	if r.pos+n > len(r.data) {
		return nil, fmt.Errorf("buffer underflow: need %d bytes, have %d", n, r.Remaining())
	}
	b := make([]byte, n)
	copy(b, r.data[r.pos:r.pos+n])
	r.pos += n
	return b, nil
}

// ReadBool reads a boolean (1 byte, 0 = false, 1 = true)
func (r *BorshReader) ReadBool() (bool, error) {
	v, err := r.ReadU8()
	if err != nil {
		return false, err
	}
	return v != 0, nil
}

// ReadOption reads an Option<T> (1 byte presence flag + optional value)
// Returns (hasValue, error). If hasValue is true, caller should read the value.
func (r *BorshReader) ReadOption() (bool, error) {
	return r.ReadBool()
}

// ReadVecLen reads the length prefix of a Vec (4-byte length)
func (r *BorshReader) ReadVecLen() (uint32, error) {
	return r.ReadU32()
}

// Skip skips n bytes
func (r *BorshReader) Skip(n int) error {
	if r.pos+n > len(r.data) {
		return fmt.Errorf("buffer underflow: cannot skip %d bytes, have %d", n, r.Remaining())
	}
	r.pos += n
	return nil
}

// Position returns the current read position
func (r *BorshReader) Position() int {
	return r.pos
}

// =============================================================================
// Solana Indexer Implementation
// =============================================================================

// NewSolanaIndexer creates a new Solana indexer
func NewSolanaIndexer(config ChainConfig, db Database) (*SolanaIndexer, error) {
	ctx, cancel := context.WithCancel(context.Background())

	idx := &SolanaIndexer{
		config:            config,
		db:                db,
		client:            &http.Client{Timeout: 30 * time.Second},
		protocols:         make(map[ProtocolType]SolanaProtocolIndexer),
		programToProtocol: make(map[string]SolanaProtocolIndexer),
		ctx:               ctx,
		cancel:            cancel,
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

// initProtocol initializes a Solana protocol indexer
func (s *SolanaIndexer) initProtocol(config ProtocolConfig) error {
	var indexer SolanaProtocolIndexer

	switch config.Type {
	case ProtocolRaydium:
		indexer = NewRaydiumIndexer(config)
	case ProtocolOrca:
		indexer = NewOrcaIndexer(config)
	case ProtocolMarinade:
		indexer = NewMarinadeIndexer(config)
	case ProtocolJito:
		indexer = NewJitoIndexer(config)
	case ProtocolJupiter:
		indexer = NewJupiterIndexer(config)
	case ProtocolDrift:
		indexer = NewDriftIndexer(config)
	case ProtocolMango:
		indexer = NewMangoIndexer(config)
	case ProtocolPhoenix:
		indexer = NewPhoenixIndexer(config)
	case ProtocolMetaplex:
		indexer = NewMetaplexIndexer(config, s.db)
	case ProtocolMagicEden:
		indexer = NewMagicEdenSolanaIndexer(config, s.db)
	case ProtocolTensor:
		indexer = NewTensorIndexer(config, s.db)
	default:
		indexer = NewGenericSolanaIndexer(config)
	}

	s.protocols[config.Type] = indexer

	// Build program ID to protocol mapping
	for _, programID := range indexer.ProgramIDs() {
		s.programToProtocol[programID] = indexer
	}

	return nil
}

// ChainID returns the chain identifier
func (s *SolanaIndexer) ChainID() string {
	return s.config.ID
}

// ChainType returns the chain type
func (s *SolanaIndexer) ChainType() ChainType {
	return ChainTypeSolana
}

// Start starts the indexer
func (s *SolanaIndexer) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return fmt.Errorf("indexer already running")
	}
	s.stats.IsRunning = true
	return nil
}

// Stop stops the indexer
func (s *SolanaIndexer) Stop() error {
	atomic.StoreInt32(&s.running, 0)
	s.cancel()
	s.stats.IsRunning = false
	return nil
}

// IsRunning returns whether the indexer is running
func (s *SolanaIndexer) IsRunning() bool {
	return atomic.LoadInt32(&s.running) == 1
}

// GetLatestBlock returns the latest slot
func (s *SolanaIndexer) GetLatestBlock(ctx context.Context) (uint64, error) {
	result, err := s.rpcCall(ctx, "getSlot", []interface{}{})
	if err != nil {
		return 0, err
	}

	slot, ok := result.(float64)
	if !ok {
		return 0, fmt.Errorf("invalid slot response")
	}

	s.mu.Lock()
	s.latestSlot = uint64(slot)
	s.mu.Unlock()

	return uint64(slot), nil
}

// GetIndexedBlock returns the last indexed slot
func (s *SolanaIndexer) GetIndexedBlock() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.indexedSlot
}

// IndexBlock indexes a single slot
func (s *SolanaIndexer) IndexBlock(ctx context.Context, slot uint64) error {
	block, err := s.fetchBlock(ctx, slot)
	if err != nil {
		return fmt.Errorf("failed to fetch slot %d: %w", slot, err)
	}

	// Process transactions
	for i := range block.Transactions {
		tx := &block.Transactions[i]
		tx.Slot = slot
		tx.BlockTime = block.BlockTime
		s.processTransaction(ctx, tx)
	}

	s.mu.Lock()
	s.indexedSlot = slot
	s.stats.BlocksProcessed++
	s.stats.TxsProcessed += uint64(len(block.Transactions))
	s.stats.LastBlockTime = time.Now()
	s.mu.Unlock()

	return nil
}

// IndexBlockRange indexes a range of slots
func (s *SolanaIndexer) IndexBlockRange(ctx context.Context, from, to uint64) error {
	for slot := from; slot <= to; slot++ {
		if err := s.IndexBlock(ctx, slot); err != nil {
			s.stats.ErrorCount++
			s.stats.LastError = err.Error()
			continue
		}
	}
	return nil
}

// fetchBlock fetches a block from RPC
func (s *SolanaIndexer) fetchBlock(ctx context.Context, slot uint64) (*SolanaBlock, error) {
	params := []interface{}{
		slot,
		map[string]interface{}{
			"encoding":                       "json",
			"transactionDetails":             "full",
			"rewards":                        true,
			"maxSupportedTransactionVersion": 0,
		},
	}

	result, err := s.rpcCall(ctx, "getBlock", params)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}

	var block SolanaBlock
	if err := json.Unmarshal(data, &block); err != nil {
		return nil, err
	}

	block.Slot = slot
	return &block, nil
}

// processTransaction routes a transaction to the appropriate protocol indexer
func (s *SolanaIndexer) processTransaction(ctx context.Context, tx *SolanaTransaction) {
	// Skip failed transactions
	if tx.Status == "failed" || tx.Err != nil {
		return
	}

	// Extract program IDs from instructions
	programIDs := make(map[string]bool)
	for _, ix := range tx.Message.Instructions {
		if ix.ProgramIDIndex < len(tx.Message.AccountKeys) {
			programID := tx.Message.AccountKeys[ix.ProgramIDIndex]
			programIDs[programID] = true
		}
	}

	// Also check inner instructions
	for _, inner := range tx.InnerInstructions {
		for _, ix := range inner.Instructions {
			if ix.ProgramIDIndex < len(tx.Message.AccountKeys) {
				programID := tx.Message.AccountKeys[ix.ProgramIDIndex]
				programIDs[programID] = true
			}
		}
	}

	// Route to appropriate indexers based on program IDs
	processedProtocols := make(map[string]bool)
	for programID := range programIDs {
		if indexer, ok := s.programToProtocol[programID]; ok {
			// Avoid processing same protocol multiple times per transaction
			name := indexer.Name()
			if !processedProtocols[name] {
				processedProtocols[name] = true
				if err := indexer.IndexTransaction(ctx, tx); err != nil {
					s.stats.ErrorCount++
				} else {
					s.stats.EventsProcessed++
				}
			}
		}
	}
}

// rpcCall makes a JSON-RPC call to Solana
func (s *SolanaIndexer) rpcCall(ctx context.Context, method string, params []interface{}) (interface{}, error) {
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.config.RPC, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(bytes.NewReader(data))

	resp, err := s.client.Do(req)
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
func (s *SolanaIndexer) Stats() *IndexerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &IndexerStats{
		ChainID:         s.stats.ChainID,
		ChainName:       s.stats.ChainName,
		IsRunning:       s.stats.IsRunning,
		LatestBlock:     s.latestSlot,
		IndexedBlock:    s.indexedSlot,
		BlocksBehind:    s.latestSlot - s.indexedSlot,
		BlocksProcessed: s.stats.BlocksProcessed,
		TxsProcessed:    s.stats.TxsProcessed,
		EventsProcessed: s.stats.EventsProcessed,
		ErrorCount:      s.stats.ErrorCount,
		LastError:       s.stats.LastError,
		LastBlockTime:   s.stats.LastBlockTime,
		StartTime:       s.stats.StartTime,
		Uptime:          time.Since(s.stats.StartTime),
	}
}

// =============================================================================
// Solana Protocol Indexer Stubs
// =============================================================================

type GenericSolanaIndexer struct{ config ProtocolConfig }

func NewGenericSolanaIndexer(config ProtocolConfig) *GenericSolanaIndexer {
	return &GenericSolanaIndexer{config: config}
}
func (g *GenericSolanaIndexer) Name() string                                              { return "generic_solana" }
func (g *GenericSolanaIndexer) ProgramIDs() []string                                      { return nil }
func (g *GenericSolanaIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Raydium AMM (largest Solana DEX)
type RaydiumIndexer struct{ config ProtocolConfig }

func NewRaydiumIndexer(config ProtocolConfig) *RaydiumIndexer { return &RaydiumIndexer{config: config} }
func (r *RaydiumIndexer) Name() string                        { return "raydium" }
func (r *RaydiumIndexer) ProgramIDs() []string {
	return []string{
		"675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8", // AMM V4
		"CAMMCzo5YL8w4VFF8KVHrK22GGUsp5VTaW7grrKgrWqK", // CLMM
		"routeUGWgWzqBWFcrCfv8tritsqukccJPu3q5GPP3xS",  // Router
	}
}
func (r *RaydiumIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Orca AMM
type OrcaIndexer struct{ config ProtocolConfig }

func NewOrcaIndexer(config ProtocolConfig) *OrcaIndexer { return &OrcaIndexer{config: config} }
func (o *OrcaIndexer) Name() string                     { return "orca" }
func (o *OrcaIndexer) ProgramIDs() []string {
	return []string{
		"whirLbMiicVdio4qvUfM5KAg6Ct8VwpYzGff3uctyCc",  // Whirlpool
		"9W959DqEETiGZocYWCQPaJ6sBmUzgfxXfqGeTEdp3aQP", // Legacy
	}
}
func (o *OrcaIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Marinade Liquid Staking
type MarinadeIndexer struct{ config ProtocolConfig }

func NewMarinadeIndexer(config ProtocolConfig) *MarinadeIndexer {
	return &MarinadeIndexer{config: config}
}
func (m *MarinadeIndexer) Name() string { return "marinade" }
func (m *MarinadeIndexer) ProgramIDs() []string {
	return []string{
		"MarBmsSgKXdrN1egZf5sqe1TMai9K1rChYNDJgjq7aD", // Marinade Finance
	}
}
func (m *MarinadeIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Jito MEV/Staking
type JitoIndexer struct{ config ProtocolConfig }

func NewJitoIndexer(config ProtocolConfig) *JitoIndexer { return &JitoIndexer{config: config} }
func (j *JitoIndexer) Name() string                     { return "jito" }
func (j *JitoIndexer) ProgramIDs() []string {
	return []string{
		"J1toso1uCk3RLmjorhTtrVwY9HJ7X8V9yYac6Y7kGCPn", // JitoSOL
		"Jito4APyf642JPZPx3hGc6WWJ8zPKtRbRs4P815Awbb",  // Tip Payment
	}
}
func (j *JitoIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Jupiter Aggregator
type JupiterIndexer struct{ config ProtocolConfig }

func NewJupiterIndexer(config ProtocolConfig) *JupiterIndexer { return &JupiterIndexer{config: config} }
func (j *JupiterIndexer) Name() string                        { return "jupiter" }
func (j *JupiterIndexer) ProgramIDs() []string {
	return []string{
		"JUP6LkbZbjS1jKKwapdHNy74zcZ3tLUZoi5QNyVTaV4", // Jupiter V6
		"JUP4Fb2cqiRUcaTHdrPC8h2gNsA2ETXiPDD33WcGuJB", // Jupiter V4
	}
}
func (j *JupiterIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Drift Perpetuals
type DriftIndexer struct{ config ProtocolConfig }

func NewDriftIndexer(config ProtocolConfig) *DriftIndexer { return &DriftIndexer{config: config} }
func (d *DriftIndexer) Name() string                      { return "drift" }
func (d *DriftIndexer) ProgramIDs() []string {
	return []string{
		"dRiftyHA39MWEi3m9aunc5MzRF1JYuBsbn6VPcn33UH", // Drift V2
	}
}
func (d *DriftIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Mango Markets
type MangoIndexer struct{ config ProtocolConfig }

func NewMangoIndexer(config ProtocolConfig) *MangoIndexer { return &MangoIndexer{config: config} }
func (m *MangoIndexer) Name() string                      { return "mango" }
func (m *MangoIndexer) ProgramIDs() []string {
	return []string{
		"4MangoMjqJ2firMokCjjGgoK8d4MXcrgL7XJaL3w6fVg", // Mango V4
	}
}
func (m *MangoIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// Phoenix DEX (CLOB)
type PhoenixIndexer struct{ config ProtocolConfig }

func NewPhoenixIndexer(config ProtocolConfig) *PhoenixIndexer { return &PhoenixIndexer{config: config} }
func (p *PhoenixIndexer) Name() string                        { return "phoenix" }
func (p *PhoenixIndexer) ProgramIDs() []string {
	return []string{
		"PhoeNiXZ8ByJGLkxNfZRnkUfjvmuYqLR89jjFHGqdXY", // Phoenix V1
	}
}
func (p *PhoenixIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error { return nil }

// =============================================================================
// Metaplex NFT Indexer
// =============================================================================

// Metaplex instruction discriminators (first 8 bytes of instruction data)
const (
	// Token Metadata Program instructions
	MetaplexCreateMetadataV3      uint8 = 33
	MetaplexUpdateMetadataV2      uint8 = 15
	MetaplexCreateMasterEditionV3 uint8 = 17
	MetaplexVerifyCreator         uint8 = 8
	MetaplexSetAndVerifyCollection uint8 = 25

	// Auction House instructions
	MetaplexAHExecuteSale       uint8 = 0  // ExecuteSale
	MetaplexAHExecuteSaleV2     uint8 = 1  // ExecuteSaleV2
	MetaplexAHBuy               uint8 = 2  // Buy
	MetaplexAHSell              uint8 = 3  // Sell
	MetaplexAHCancel            uint8 = 4  // Cancel
	MetaplexAHPublicBuy         uint8 = 6  // PublicBuy
)

// MetaplexIndexer indexes Metaplex NFT activity
type MetaplexIndexer struct {
	config ProtocolConfig
	db     Database
}

// NewMetaplexIndexer creates a new Metaplex indexer
func NewMetaplexIndexer(config ProtocolConfig, db Database) *MetaplexIndexer {
	return &MetaplexIndexer{config: config, db: db}
}

func (m *MetaplexIndexer) Name() string { return "metaplex" }

func (m *MetaplexIndexer) ProgramIDs() []string {
	return []string{
		"metaqbxxUerdq28cj1RbAWkYQm3ybzjb6a8bt518x1s", // Token Metadata
		"p1exdMJcjVao65QdewkaZRUnU6VPSXhus9n2GzWfh98", // Auction House
		"CoREENxT6tW1HoK8ypY1SxRMZTcVPm7R94rH4PZNhX7d", // Core
		"BGUMAp9Gq7iTEuizy4pqaxsTyUCBK68MDfK752saRPUY", // Bubblegum (cNFTs)
	}
}

func (m *MetaplexIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error {
	for i, ix := range tx.Message.Instructions {
		if ix.ProgramIDIndex >= len(tx.Message.AccountKeys) {
			continue
		}

		programID := tx.Message.AccountKeys[ix.ProgramIDIndex]
		switch programID {
		case "metaqbxxUerdq28cj1RbAWkYQm3ybzjb6a8bt518x1s":
			if err := m.parseTokenMetadataInstruction(ctx, tx, &ix, i); err != nil {
				return err
			}
		case "p1exdMJcjVao65QdewkaZRUnU6VPSXhus9n2GzWfh98":
			if err := m.parseAuctionHouseInstruction(ctx, tx, &ix, i); err != nil {
				return err
			}
		}
	}
	return nil
}

// parseTokenMetadataInstruction parses Token Metadata program instructions
func (m *MetaplexIndexer) parseTokenMetadataInstruction(ctx context.Context, tx *SolanaTransaction, ix *SolanaInstruction, ixIndex int) error {
	reader, err := NewBorshReader(ix.Data)
	if err != nil {
		return nil // Skip unparseable instructions
	}

	discriminator, err := reader.ReadU8()
	if err != nil {
		return nil
	}

	switch discriminator {
	case MetaplexCreateMetadataV3:
		return m.parseCreateMetadataV3(ctx, tx, ix, reader)
	case MetaplexUpdateMetadataV2:
		return m.parseUpdateMetadataV2(ctx, tx, ix, reader)
	}

	return nil
}

// parseCreateMetadataV3 parses CreateMetadataAccountV3 instruction
func (m *MetaplexIndexer) parseCreateMetadataV3(ctx context.Context, tx *SolanaTransaction, ix *SolanaInstruction, reader *BorshReader) error {
	// CreateMetadataAccountV3 accounts:
	// 0: metadata (PDA)
	// 1: mint
	// 2: mint authority
	// 3: payer
	// 4: update authority

	if len(ix.Accounts) < 5 {
		return nil
	}

	mintIndex := ix.Accounts[1]
	updateAuthIndex := ix.Accounts[4]

	var mint, updateAuthority string
	if mintIndex < len(tx.Message.AccountKeys) {
		mint = tx.Message.AccountKeys[mintIndex]
	}
	if updateAuthIndex < len(tx.Message.AccountKeys) {
		updateAuthority = tx.Message.AccountKeys[updateAuthIndex]
	}

	// Parse DataV2 struct
	metadata, err := m.parseDataV2(reader)
	if err != nil {
		return nil
	}

	metadata.Mint = mint
	metadata.UpdateAuthority = updateAuthority

	// Store metadata
	if m.db != nil {
		return m.storeMetadata(ctx, tx, metadata)
	}
	return nil
}

// parseUpdateMetadataV2 parses UpdateMetadataAccountV2 instruction
func (m *MetaplexIndexer) parseUpdateMetadataV2(ctx context.Context, tx *SolanaTransaction, ix *SolanaInstruction, reader *BorshReader) error {
	// UpdateMetadataAccountV2 accounts:
	// 0: metadata (PDA)
	// 1: update authority

	if len(ix.Accounts) < 2 {
		return nil
	}

	// Check if data is provided (Option<DataV2>)
	hasData, err := reader.ReadOption()
	if err != nil {
		return nil
	}

	if !hasData {
		return nil // No metadata update
	}

	metadata, err := m.parseDataV2(reader)
	if err != nil {
		return nil
	}

	// Get update authority from accounts
	updateAuthIndex := ix.Accounts[1]
	if updateAuthIndex < len(tx.Message.AccountKeys) {
		metadata.UpdateAuthority = tx.Message.AccountKeys[updateAuthIndex]
	}

	// Store updated metadata
	if m.db != nil {
		return m.storeMetadata(ctx, tx, metadata)
	}
	return nil
}

// parseDataV2 parses Metaplex DataV2 struct from Borsh
func (m *MetaplexIndexer) parseDataV2(reader *BorshReader) (*MetaplexMetadata, error) {
	metadata := &MetaplexMetadata{}

	// name: String
	name, err := reader.ReadString()
	if err != nil {
		return nil, err
	}
	// Trim null bytes from fixed-length Metaplex strings
	metadata.Name = trimNullBytes(name)

	// symbol: String
	symbol, err := reader.ReadString()
	if err != nil {
		return nil, err
	}
	metadata.Symbol = trimNullBytes(symbol)

	// uri: String
	uri, err := reader.ReadString()
	if err != nil {
		return nil, err
	}
	metadata.URI = trimNullBytes(uri)

	// seller_fee_basis_points: u16
	sellerFee, err := reader.ReadU16()
	if err != nil {
		return nil, err
	}
	metadata.SellerFeeBasisPoints = sellerFee

	// creators: Option<Vec<Creator>>
	hasCreators, err := reader.ReadOption()
	if err != nil {
		return nil, err
	}

	if hasCreators {
		creatorsLen, err := reader.ReadVecLen()
		if err != nil {
			return nil, err
		}

		metadata.Creators = make([]MetaplexCreator, 0, creatorsLen)
		for i := uint32(0); i < creatorsLen; i++ {
			creator, err := m.parseCreator(reader)
			if err != nil {
				return nil, err
			}
			metadata.Creators = append(metadata.Creators, *creator)
		}
	}

	return metadata, nil
}

// parseCreator parses a Metaplex Creator struct
func (m *MetaplexIndexer) parseCreator(reader *BorshReader) (*MetaplexCreator, error) {
	// address: Pubkey
	address, err := reader.ReadPubkey()
	if err != nil {
		return nil, err
	}

	// verified: bool
	verified, err := reader.ReadBool()
	if err != nil {
		return nil, err
	}

	// share: u8
	share, err := reader.ReadU8()
	if err != nil {
		return nil, err
	}

	return &MetaplexCreator{
		Address:  address,
		Verified: verified,
		Share:    share,
	}, nil
}

// parseAuctionHouseInstruction parses Auction House program instructions
func (m *MetaplexIndexer) parseAuctionHouseInstruction(ctx context.Context, tx *SolanaTransaction, ix *SolanaInstruction, ixIndex int) error {
	reader, err := NewBorshReader(ix.Data)
	if err != nil {
		return nil
	}

	// Auction House uses 8-byte discriminators (anchor)
	if reader.Remaining() < 8 {
		return nil
	}

	discBytes, err := reader.ReadBytes(8)
	if err != nil {
		return nil
	}

	// Check for ExecuteSale discriminator
	// Anchor discriminator for "execute_sale" = sha256("global:execute_sale")[0:8]
	executeSaleDisc := []byte{37, 74, 217, 157, 79, 49, 35, 6}
	if bytes.Equal(discBytes, executeSaleDisc) {
		return m.parseExecuteSale(ctx, tx, ix, reader)
	}

	return nil
}

// parseExecuteSale parses Auction House ExecuteSale instruction
func (m *MetaplexIndexer) parseExecuteSale(ctx context.Context, tx *SolanaTransaction, ix *SolanaInstruction, reader *BorshReader) error {
	// ExecuteSale accounts (from Auction House IDL):
	// 0: buyer
	// 1: seller
	// 2: token_account
	// 3: token_mint
	// 4: metadata
	// 5: treasury_mint
	// 6: escrow_payment_account
	// 7: seller_payment_receipt_account
	// 8: buyer_receipt_token_account
	// 9: authority
	// 10: auction_house
	// 11: auction_house_fee_account
	// 12: auction_house_treasury
	// 13: buyer_trade_state
	// 14: seller_trade_state
	// 15: free_trade_state
	// 16: token_program
	// 17: system_program
	// 18: ata_program
	// 19: program_as_signer
	// 20: rent

	if len(ix.Accounts) < 4 {
		return nil
	}

	// Read instruction args
	// escrow_payment_bump: u8
	_, err := reader.ReadU8()
	if err != nil {
		return nil
	}

	// free_trade_state_bump: u8
	_, err = reader.ReadU8()
	if err != nil {
		return nil
	}

	// program_as_signer_bump: u8
	_, err = reader.ReadU8()
	if err != nil {
		return nil
	}

	// buyer_price: u64
	price, err := reader.ReadU64()
	if err != nil {
		return nil
	}

	// token_size: u64 (usually 1 for NFTs)
	_, err = reader.ReadU64()
	if err != nil {
		return nil
	}

	// Extract accounts
	var buyer, seller, mint string
	if ix.Accounts[0] < len(tx.Message.AccountKeys) {
		buyer = tx.Message.AccountKeys[ix.Accounts[0]]
	}
	if ix.Accounts[1] < len(tx.Message.AccountKeys) {
		seller = tx.Message.AccountKeys[ix.Accounts[1]]
	}
	if ix.Accounts[3] < len(tx.Message.AccountKeys) {
		mint = tx.Message.AccountKeys[ix.Accounts[3]]
	}

	sale := &SolanaNFTSale{
		Signature:   tx.Signature,
		Slot:        tx.Slot,
		BlockTime:   tx.BlockTime,
		Marketplace: "metaplex",
		Mint:        mint,
		Buyer:       buyer,
		Seller:      seller,
		Price:       price,
	}

	// Calculate fees from balance changes if available
	sale.MarketplaceFee = m.calculateMarketplaceFee(tx, ix)

	if m.db != nil {
		return m.storeSale(ctx, sale)
	}
	return nil
}

// calculateMarketplaceFee attempts to calculate marketplace fee from balance changes
func (m *MetaplexIndexer) calculateMarketplaceFee(tx *SolanaTransaction, ix *SolanaInstruction) uint64 {
	// Auction house fee account is typically at index 11
	if len(ix.Accounts) < 12 {
		return 0
	}

	feeAccountIdx := ix.Accounts[11]
	if feeAccountIdx >= len(tx.Meta.PreBalances) || feeAccountIdx >= len(tx.Meta.PostBalances) {
		return 0
	}

	pre := tx.Meta.PreBalances[feeAccountIdx]
	post := tx.Meta.PostBalances[feeAccountIdx]
	if post > pre {
		return post - pre
	}
	return 0
}

func (m *MetaplexIndexer) storeMetadata(ctx context.Context, tx *SolanaTransaction, metadata *MetaplexMetadata) error {
	// Database storage implementation
	// This would insert into a metaplex_metadata table
	return nil
}

func (m *MetaplexIndexer) storeSale(ctx context.Context, sale *SolanaNFTSale) error {
	// Database storage implementation
	// This would insert into a solana_nft_sales table
	return nil
}

// =============================================================================
// Magic Eden Solana Indexer
// =============================================================================

// Magic Eden V2 instruction discriminators
const (
	MagicEdenExecuteSaleV2 uint8 = 0
	MagicEdenBuy           uint8 = 1
	MagicEdenSell          uint8 = 2
	MagicEdenCancelBuy     uint8 = 3
	MagicEdenCancelSell    uint8 = 4
)

// MagicEdenSolanaIndexer indexes Magic Eden NFT marketplace on Solana
type MagicEdenSolanaIndexer struct {
	config ProtocolConfig
	db     Database
}

// NewMagicEdenSolanaIndexer creates a new Magic Eden Solana indexer
func NewMagicEdenSolanaIndexer(config ProtocolConfig, db Database) *MagicEdenSolanaIndexer {
	return &MagicEdenSolanaIndexer{config: config, db: db}
}

func (m *MagicEdenSolanaIndexer) Name() string { return "magic_eden_solana" }

func (m *MagicEdenSolanaIndexer) ProgramIDs() []string {
	return []string{
		"M2mx93ekt1fmXSVkTrUL9xVFHkmME8HTUi5Cyc5aF7K", // Magic Eden V2
		"MEisE1HzehtrDpAAT8PnLHjpSSkRYakotTuJRPjTpo8", // Magic Eden V1
	}
}

func (m *MagicEdenSolanaIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error {
	for i, ix := range tx.Message.Instructions {
		if ix.ProgramIDIndex >= len(tx.Message.AccountKeys) {
			continue
		}

		programID := tx.Message.AccountKeys[ix.ProgramIDIndex]
		if programID == "M2mx93ekt1fmXSVkTrUL9xVFHkmME8HTUi5Cyc5aF7K" {
			if err := m.parseMagicEdenV2Instruction(ctx, tx, &ix, i); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *MagicEdenSolanaIndexer) parseMagicEdenV2Instruction(ctx context.Context, tx *SolanaTransaction, ix *SolanaInstruction, ixIndex int) error {
	reader, err := NewBorshReader(ix.Data)
	if err != nil {
		return nil
	}

	// Magic Eden V2 uses Anchor-style 8-byte discriminators
	if reader.Remaining() < 8 {
		return nil
	}

	discBytes, err := reader.ReadBytes(8)
	if err != nil {
		return nil
	}

	// ExecuteSaleV2 discriminator: sha256("global:execute_sale_v2")[0:8]
	executeSaleV2Disc := []byte{227, 151, 21, 125, 251, 27, 83, 71}
	// Buy discriminator
	buyDisc := []byte{102, 6, 61, 18, 1, 218, 235, 234}
	// Sell discriminator
	sellDisc := []byte{51, 230, 133, 164, 1, 127, 131, 173}

	switch {
	case bytes.Equal(discBytes, executeSaleV2Disc):
		return m.parseExecuteSaleV2(ctx, tx, ix, reader)
	case bytes.Equal(discBytes, buyDisc):
		return m.parseBuy(ctx, tx, ix, reader)
	case bytes.Equal(discBytes, sellDisc):
		return m.parseSell(ctx, tx, ix, reader)
	}

	return nil
}

// parseExecuteSaleV2 parses Magic Eden ExecuteSaleV2 instruction
func (m *MagicEdenSolanaIndexer) parseExecuteSaleV2(ctx context.Context, tx *SolanaTransaction, ix *SolanaInstruction, reader *BorshReader) error {
	// Magic Eden V2 ExecuteSaleV2 accounts:
	// 0: buyer
	// 1: seller
	// 2: notary (optional authority)
	// 3: token_account (seller's)
	// 4: token_mint
	// 5: metadata
	// 6: escrow_payment_account
	// 7: buyer_receipt_token_account
	// 8: authority
	// 9: auction_house
	// 10: auction_house_fee_account
	// 11: auction_house_treasury
	// 12: buyer_trade_state
	// 13: seller_trade_state
	// ... more accounts

	if len(ix.Accounts) < 5 {
		return nil
	}

	// Read instruction args
	// buyer_price: u64
	price, err := reader.ReadU64()
	if err != nil {
		return nil
	}

	// token_size: u64
	_, err = reader.ReadU64()
	if err != nil {
		return nil
	}

	// buyer_state_expiry: i64
	_, err = reader.ReadI64()
	if err != nil {
		return nil
	}

	// seller_state_expiry: i64
	_, err = reader.ReadI64()
	if err != nil {
		return nil
	}

	// Extract accounts
	var buyer, seller, mint string
	if ix.Accounts[0] < len(tx.Message.AccountKeys) {
		buyer = tx.Message.AccountKeys[ix.Accounts[0]]
	}
	if ix.Accounts[1] < len(tx.Message.AccountKeys) {
		seller = tx.Message.AccountKeys[ix.Accounts[1]]
	}
	if ix.Accounts[4] < len(tx.Message.AccountKeys) {
		mint = tx.Message.AccountKeys[ix.Accounts[4]]
	}

	sale := &SolanaNFTSale{
		Signature:   tx.Signature,
		Slot:        tx.Slot,
		BlockTime:   tx.BlockTime,
		Marketplace: "magic_eden",
		Mint:        mint,
		Buyer:       buyer,
		Seller:      seller,
		Price:       price,
	}

	// Calculate fees
	sale.MarketplaceFee = m.calculateFee(tx, ix, 10) // fee account at index 10

	if m.db != nil {
		return m.storeSale(ctx, sale)
	}
	return nil
}

// parseBuy parses Magic Eden Buy instruction (creates buy order)
func (m *MagicEdenSolanaIndexer) parseBuy(ctx context.Context, tx *SolanaTransaction, ix *SolanaInstruction, reader *BorshReader) error {
	// Buy instruction creates a buy order, not an actual sale
	// We might want to track these for order book analysis
	// For now, we only index actual sales (ExecuteSaleV2)
	return nil
}

// parseSell parses Magic Eden Sell instruction (creates sell listing)
func (m *MagicEdenSolanaIndexer) parseSell(ctx context.Context, tx *SolanaTransaction, ix *SolanaInstruction, reader *BorshReader) error {
	// Sell instruction creates a listing, not an actual sale
	// For now, we only index actual sales
	return nil
}

func (m *MagicEdenSolanaIndexer) calculateFee(tx *SolanaTransaction, ix *SolanaInstruction, feeAccountPosition int) uint64 {
	if len(ix.Accounts) <= feeAccountPosition {
		return 0
	}

	feeAccountIdx := ix.Accounts[feeAccountPosition]
	if feeAccountIdx >= len(tx.Meta.PreBalances) || feeAccountIdx >= len(tx.Meta.PostBalances) {
		return 0
	}

	pre := tx.Meta.PreBalances[feeAccountIdx]
	post := tx.Meta.PostBalances[feeAccountIdx]
	if post > pre {
		return post - pre
	}
	return 0
}

func (m *MagicEdenSolanaIndexer) storeSale(ctx context.Context, sale *SolanaNFTSale) error {
	// Database storage implementation
	return nil
}

// =============================================================================
// Tensor NFT Marketplace Indexer
// =============================================================================

// Tensor instruction types
const (
	TensorBuyNft     uint8 = 0
	TensorSellNft    uint8 = 1
	TensorBuySingleListing uint8 = 2
	TensorList       uint8 = 3
	TensorDelist     uint8 = 4
	TensorEdit       uint8 = 5
)

// TensorIndexer indexes Tensor NFT marketplace on Solana
type TensorIndexer struct {
	config ProtocolConfig
	db     Database
}

// NewTensorIndexer creates a new Tensor indexer
func NewTensorIndexer(config ProtocolConfig, db Database) *TensorIndexer {
	return &TensorIndexer{config: config, db: db}
}

func (t *TensorIndexer) Name() string { return "tensor" }

func (t *TensorIndexer) ProgramIDs() []string {
	return []string{
		"TSWAPaqyCSx2KABk68Shruf4rp7CxcNi8hAsbdwmHbN", // Tensor Swap
		"TCMPhJdwDryooaGtiocG1u3xcYbRpiJzb283XfCZsDp", // Tensor cNFT
		"TBIDxNsM9DuLs4YCbmA7VuACbMZ5WyYv5JGQxpqLMVJ", // Tensor Bid
	}
}

func (t *TensorIndexer) IndexTransaction(ctx context.Context, tx *SolanaTransaction) error {
	for i, ix := range tx.Message.Instructions {
		if ix.ProgramIDIndex >= len(tx.Message.AccountKeys) {
			continue
		}

		programID := tx.Message.AccountKeys[ix.ProgramIDIndex]
		switch programID {
		case "TSWAPaqyCSx2KABk68Shruf4rp7CxcNi8hAsbdwmHbN":
			if err := t.parseTSwapInstruction(ctx, tx, &ix, i); err != nil {
				return err
			}
		case "TBIDxNsM9DuLs4YCbmA7VuACbMZ5WyYv5JGQxpqLMVJ":
			if err := t.parseTBidInstruction(ctx, tx, &ix, i); err != nil {
				return err
			}
		}
	}
	return nil
}

// parseTSwapInstruction parses Tensor Swap instructions
func (t *TensorIndexer) parseTSwapInstruction(ctx context.Context, tx *SolanaTransaction, ix *SolanaInstruction, ixIndex int) error {
	reader, err := NewBorshReader(ix.Data)
	if err != nil {
		return nil
	}

	// Tensor uses Anchor-style 8-byte discriminators
	if reader.Remaining() < 8 {
		return nil
	}

	discBytes, err := reader.ReadBytes(8)
	if err != nil {
		return nil
	}

	// TSwap buy_nft discriminator
	buyNftDisc := []byte{96, 0, 28, 190, 49, 107, 83, 222}
	// TSwap sell_nft discriminator
	sellNftDisc := []byte{172, 103, 193, 95, 91, 165, 117, 17}
	// TSwap buy_single_listing discriminator
	buySingleListingDisc := []byte{149, 234, 31, 103, 26, 153, 102, 241}

	switch {
	case bytes.Equal(discBytes, buyNftDisc):
		return t.parseBuyNft(ctx, tx, ix, reader)
	case bytes.Equal(discBytes, sellNftDisc):
		return t.parseSellNft(ctx, tx, ix, reader)
	case bytes.Equal(discBytes, buySingleListingDisc):
		return t.parseBuySingleListing(ctx, tx, ix, reader)
	}

	return nil
}

// parseBuyNft parses Tensor buy_nft instruction (buy from pool)
func (t *TensorIndexer) parseBuyNft(ctx context.Context, tx *SolanaTransaction, ix *SolanaInstruction, reader *BorshReader) error {
	// TSwap buy_nft accounts:
	// 0: tswap
	// 1: fee_vault
	// 2: pool
	// 3: whitelist
	// 4: nft_buyer (buyer)
	// 5: nft_buyer_token_account
	// 6: pool_nft_token_account
	// 7: nft_mint
	// 8: nft_metadata
	// 9: nft_edition (optional)
	// 10: owner (seller/pool owner)
	// 11: sol_escrow
	// ... more accounts

	if len(ix.Accounts) < 11 {
		return nil
	}

	// Read instruction args
	// max_price: u64 (max price buyer is willing to pay)
	maxPrice, err := reader.ReadU64()
	if err != nil {
		return nil
	}

	// Extract accounts
	var buyer, seller, mint string
	if ix.Accounts[4] < len(tx.Message.AccountKeys) {
		buyer = tx.Message.AccountKeys[ix.Accounts[4]]
	}
	if ix.Accounts[10] < len(tx.Message.AccountKeys) {
		seller = tx.Message.AccountKeys[ix.Accounts[10]]
	}
	if ix.Accounts[7] < len(tx.Message.AccountKeys) {
		mint = tx.Message.AccountKeys[ix.Accounts[7]]
	}

	// Calculate actual price from balance changes
	actualPrice := t.calculateActualPrice(tx, ix.Accounts[4]) // buyer's balance change
	if actualPrice == 0 {
		actualPrice = maxPrice
	}

	sale := &SolanaNFTSale{
		Signature:   tx.Signature,
		Slot:        tx.Slot,
		BlockTime:   tx.BlockTime,
		Marketplace: "tensor",
		Mint:        mint,
		Buyer:       buyer,
		Seller:      seller,
		Price:       actualPrice,
	}

	// Calculate marketplace fee (fee_vault at index 1)
	sale.MarketplaceFee = t.calculateFeeFromVault(tx, ix, 1)

	if t.db != nil {
		return t.storeSale(ctx, sale)
	}
	return nil
}

// parseSellNft parses Tensor sell_nft instruction (sell to pool)
func (t *TensorIndexer) parseSellNft(ctx context.Context, tx *SolanaTransaction, ix *SolanaInstruction, reader *BorshReader) error {
	// TSwap sell_nft accounts:
	// 0: tswap
	// 1: fee_vault
	// 2: pool
	// 3: whitelist
	// 4: nft_seller (seller)
	// 5: nft_seller_token_account
	// 6: pool_nft_token_account
	// 7: nft_mint
	// 8: nft_metadata
	// 9: owner (pool owner/buyer)
	// 10: sol_escrow
	// ... more accounts

	if len(ix.Accounts) < 10 {
		return nil
	}

	// Read instruction args
	// min_price: u64 (min price seller will accept)
	minPrice, err := reader.ReadU64()
	if err != nil {
		return nil
	}

	// Extract accounts
	var buyer, seller, mint string
	if ix.Accounts[9] < len(tx.Message.AccountKeys) {
		buyer = tx.Message.AccountKeys[ix.Accounts[9]] // pool owner is the buyer
	}
	if ix.Accounts[4] < len(tx.Message.AccountKeys) {
		seller = tx.Message.AccountKeys[ix.Accounts[4]]
	}
	if ix.Accounts[7] < len(tx.Message.AccountKeys) {
		mint = tx.Message.AccountKeys[ix.Accounts[7]]
	}

	// Calculate actual price from balance changes
	actualPrice := t.calculateActualPrice(tx, ix.Accounts[4]) // seller's balance change
	if actualPrice == 0 {
		actualPrice = minPrice
	}

	sale := &SolanaNFTSale{
		Signature:   tx.Signature,
		Slot:        tx.Slot,
		BlockTime:   tx.BlockTime,
		Marketplace: "tensor",
		Mint:        mint,
		Buyer:       buyer,
		Seller:      seller,
		Price:       actualPrice,
	}

	sale.MarketplaceFee = t.calculateFeeFromVault(tx, ix, 1)

	if t.db != nil {
		return t.storeSale(ctx, sale)
	}
	return nil
}

// parseBuySingleListing parses Tensor buy_single_listing instruction
func (t *TensorIndexer) parseBuySingleListing(ctx context.Context, tx *SolanaTransaction, ix *SolanaInstruction, reader *BorshReader) error {
	// buy_single_listing accounts:
	// 0: tswap
	// 1: fee_vault
	// 2: single_listing
	// 3: nft_buyer
	// 4: nft_buyer_token_account
	// 5: nft_mint
	// 6: nft_token_account (seller's)
	// 7: nft_metadata
	// 8: owner (seller)
	// ... more accounts

	if len(ix.Accounts) < 9 {
		return nil
	}

	// Read instruction args
	// max_price: u64
	maxPrice, err := reader.ReadU64()
	if err != nil {
		return nil
	}

	// Extract accounts
	var buyer, seller, mint string
	if ix.Accounts[3] < len(tx.Message.AccountKeys) {
		buyer = tx.Message.AccountKeys[ix.Accounts[3]]
	}
	if ix.Accounts[8] < len(tx.Message.AccountKeys) {
		seller = tx.Message.AccountKeys[ix.Accounts[8]]
	}
	if ix.Accounts[5] < len(tx.Message.AccountKeys) {
		mint = tx.Message.AccountKeys[ix.Accounts[5]]
	}

	actualPrice := t.calculateActualPrice(tx, ix.Accounts[3])
	if actualPrice == 0 {
		actualPrice = maxPrice
	}

	sale := &SolanaNFTSale{
		Signature:   tx.Signature,
		Slot:        tx.Slot,
		BlockTime:   tx.BlockTime,
		Marketplace: "tensor",
		Mint:        mint,
		Buyer:       buyer,
		Seller:      seller,
		Price:       actualPrice,
	}

	sale.MarketplaceFee = t.calculateFeeFromVault(tx, ix, 1)

	if t.db != nil {
		return t.storeSale(ctx, sale)
	}
	return nil
}

// parseTBidInstruction parses Tensor Bid instructions
func (t *TensorIndexer) parseTBidInstruction(ctx context.Context, tx *SolanaTransaction, ix *SolanaInstruction, ixIndex int) error {
	reader, err := NewBorshReader(ix.Data)
	if err != nil {
		return nil
	}

	if reader.Remaining() < 8 {
		return nil
	}

	discBytes, err := reader.ReadBytes(8)
	if err != nil {
		return nil
	}

	// TBid take_bid discriminator
	takeBidDisc := []byte{153, 28, 126, 99, 235, 250, 59, 117}

	if bytes.Equal(discBytes, takeBidDisc) {
		return t.parseTakeBid(ctx, tx, ix, reader)
	}

	return nil
}

// parseTakeBid parses Tensor take_bid instruction
func (t *TensorIndexer) parseTakeBid(ctx context.Context, tx *SolanaTransaction, ix *SolanaInstruction, reader *BorshReader) error {
	// take_bid accounts:
	// 0: tswap
	// 1: fee_vault
	// 2: bid_state
	// 3: nft_seller
	// 4: nft_seller_token_account
	// 5: nft_mint
	// 6: nft_metadata
	// 7: bidder (buyer)
	// 8: bidder_token_account
	// 9: bid_ta (bid token account)
	// ... more accounts

	if len(ix.Accounts) < 8 {
		return nil
	}

	// Read instruction args
	// min_price: u64
	minPrice, err := reader.ReadU64()
	if err != nil {
		return nil
	}

	var buyer, seller, mint string
	if ix.Accounts[7] < len(tx.Message.AccountKeys) {
		buyer = tx.Message.AccountKeys[ix.Accounts[7]]
	}
	if ix.Accounts[3] < len(tx.Message.AccountKeys) {
		seller = tx.Message.AccountKeys[ix.Accounts[3]]
	}
	if ix.Accounts[5] < len(tx.Message.AccountKeys) {
		mint = tx.Message.AccountKeys[ix.Accounts[5]]
	}

	actualPrice := t.calculateActualPrice(tx, ix.Accounts[3])
	if actualPrice == 0 {
		actualPrice = minPrice
	}

	sale := &SolanaNFTSale{
		Signature:   tx.Signature,
		Slot:        tx.Slot,
		BlockTime:   tx.BlockTime,
		Marketplace: "tensor",
		Mint:        mint,
		Buyer:       buyer,
		Seller:      seller,
		Price:       actualPrice,
	}

	sale.MarketplaceFee = t.calculateFeeFromVault(tx, ix, 1)

	if t.db != nil {
		return t.storeSale(ctx, sale)
	}
	return nil
}

// calculateActualPrice calculates the actual sale price from balance changes
func (t *TensorIndexer) calculateActualPrice(tx *SolanaTransaction, accountIdx int) uint64 {
	if accountIdx >= len(tx.Meta.PreBalances) || accountIdx >= len(tx.Meta.PostBalances) {
		return 0
	}

	pre := tx.Meta.PreBalances[accountIdx]
	post := tx.Meta.PostBalances[accountIdx]

	// For buyer: pre > post (spent SOL)
	if pre > post {
		return pre - post
	}
	// For seller: post > pre (received SOL)
	if post > pre {
		return post - pre
	}
	return 0
}

// calculateFeeFromVault calculates marketplace fee from fee vault balance change
func (t *TensorIndexer) calculateFeeFromVault(tx *SolanaTransaction, ix *SolanaInstruction, vaultPosition int) uint64 {
	if len(ix.Accounts) <= vaultPosition {
		return 0
	}

	vaultIdx := ix.Accounts[vaultPosition]
	if vaultIdx >= len(tx.Meta.PreBalances) || vaultIdx >= len(tx.Meta.PostBalances) {
		return 0
	}

	pre := tx.Meta.PreBalances[vaultIdx]
	post := tx.Meta.PostBalances[vaultIdx]
	if post > pre {
		return post - pre
	}
	return 0
}

func (t *TensorIndexer) storeSale(ctx context.Context, sale *SolanaNFTSale) error {
	// Database storage implementation
	return nil
}

// =============================================================================
// Helper Functions
// =============================================================================

// trimNullBytes removes trailing null bytes from Metaplex fixed-length strings
func trimNullBytes(s string) string {
	return string(bytes.TrimRight([]byte(s), "\x00"))
}
