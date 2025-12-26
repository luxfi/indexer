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

// SynthsIndexer indexes synthetic asset events (Alchemix-style)
type SynthsIndexer struct {
	vaults         map[string]*SynthVault
	deposits       []*SynthDeposit
	withdrawals    []*SynthWithdrawal
	mints          []*SynthMint
	burns          []*SynthBurn
	transmutes     []*SynthTransmute
	positions      map[string]*SynthPosition // account:vault -> position
	synthTokens    map[string]*SynthToken
	onDeposit      func(*SynthDeposit)
	onWithdrawal   func(*SynthWithdrawal)
	onMint         func(*SynthMint)
	onBurn         func(*SynthBurn)
	onTransmute    func(*SynthTransmute)
}

// SynthVault represents an Alchemist vault
type SynthVault struct {
	Address          string     `json:"address"`
	Name             string     `json:"name"`
	UnderlyingToken  string     `json:"underlyingToken"`
	UnderlyingSymbol string     `json:"underlyingSymbol,omitempty"`
	YieldToken       string     `json:"yieldToken"`
	SynthToken       string     `json:"synthToken"` // e.g., sUSD, sETH, sLUX
	SynthSymbol      string     `json:"synthSymbol"`
	TotalDeposited   *big.Int   `json:"totalDeposited"`
	TotalBorrowed    *big.Int   `json:"totalBorrowed"`
	CollateralRatio  float64    `json:"collateralRatio"` // e.g., 2.0 = 200%
	MaxLTV           float64    `json:"maxLtv"`          // Max loan-to-value
	HarvestFee       uint64     `json:"harvestFee"`      // Fee in basis points
	LiquidationFee   uint64     `json:"liquidationFee"`
	ActivePositions  int        `json:"activePositions"`
	TVL              *big.Float `json:"tvl"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

// SynthToken represents a synthetic token (sUSD, sETH, sBTC, sLUX, etc.)
type SynthToken struct {
	Address      string   `json:"address"`
	Symbol       string   `json:"symbol"`
	Name         string   `json:"name"`
	Decimals     uint8    `json:"decimals"`
	Underlying   string   `json:"underlying"`
	TotalSupply  *big.Int `json:"totalSupply"`
	Price        *big.Int `json:"price,omitempty"` // Price in underlying terms
	IsCrossChain bool     `json:"isCrossChain"`
}

// SynthPosition represents a user's position in a vault
type SynthPosition struct {
	ID           string    `json:"id"`
	Account      string    `json:"account"`
	VaultAddress string    `json:"vaultAddress"`
	Deposited    *big.Int  `json:"deposited"`   // Collateral deposited
	Borrowed     *big.Int  `json:"borrowed"`    // Synths borrowed
	YieldEarned  *big.Int  `json:"yieldEarned"` // Accumulated yield
	DebtRepaid   *big.Int  `json:"debtRepaid"`  // Debt auto-repaid by yield
	LTV          float64   `json:"ltv"`         // Current loan-to-value
	HealthFactor float64   `json:"healthFactor"`
	IsActive     bool      `json:"isActive"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// SynthDeposit represents a collateral deposit
type SynthDeposit struct {
	ID           string    `json:"id"`
	TxHash       string    `json:"txHash"`
	BlockNumber  uint64    `json:"blockNumber"`
	LogIndex     uint64    `json:"logIndex"`
	VaultAddress string    `json:"vaultAddress"`
	Account      string    `json:"account"`
	Token        string    `json:"token"`
	Amount       *big.Int  `json:"amount"`
	Shares       *big.Int  `json:"shares"` // Vault shares received
	Timestamp    time.Time `json:"timestamp"`
}

// SynthWithdrawal represents a collateral withdrawal
type SynthWithdrawal struct {
	ID           string    `json:"id"`
	TxHash       string    `json:"txHash"`
	BlockNumber  uint64    `json:"blockNumber"`
	LogIndex     uint64    `json:"logIndex"`
	VaultAddress string    `json:"vaultAddress"`
	Account      string    `json:"account"`
	Token        string    `json:"token"`
	Amount       *big.Int  `json:"amount"`
	Shares       *big.Int  `json:"shares"` // Vault shares burned
	Timestamp    time.Time `json:"timestamp"`
}

// SynthMint represents minting synthetic tokens (borrowing)
type SynthMint struct {
	ID           string    `json:"id"`
	TxHash       string    `json:"txHash"`
	BlockNumber  uint64    `json:"blockNumber"`
	LogIndex     uint64    `json:"logIndex"`
	VaultAddress string    `json:"vaultAddress"`
	Account      string    `json:"account"`
	SynthToken   string    `json:"synthToken"`
	Amount       *big.Int  `json:"amount"`
	Recipient    string    `json:"recipient"`
	Timestamp    time.Time `json:"timestamp"`
}

// SynthBurn represents burning synthetic tokens (repaying)
type SynthBurn struct {
	ID           string    `json:"id"`
	TxHash       string    `json:"txHash"`
	BlockNumber  uint64    `json:"blockNumber"`
	LogIndex     uint64    `json:"logIndex"`
	VaultAddress string    `json:"vaultAddress"`
	Account      string    `json:"account"`
	SynthToken   string    `json:"synthToken"`
	Amount       *big.Int  `json:"amount"`
	Timestamp    time.Time `json:"timestamp"`
}

// SynthTransmute represents transmuting synths to underlying
type SynthTransmute struct {
	ID              string    `json:"id"`
	TxHash          string    `json:"txHash"`
	BlockNumber     uint64    `json:"blockNumber"`
	LogIndex        uint64    `json:"logIndex"`
	TransmuterAddr  string    `json:"transmuterAddress"`
	Account         string    `json:"account"`
	SynthToken      string    `json:"synthToken"`
	SynthAmount     *big.Int  `json:"synthAmount"`
	UnderlyingToken string    `json:"underlyingToken"`
	UnderlyingAmount *big.Int `json:"underlyingAmount"`
	Timestamp       time.Time `json:"timestamp"`
}

// NewSynthsIndexer creates a new synthetics indexer
func NewSynthsIndexer() *SynthsIndexer {
	return &SynthsIndexer{
		vaults:      make(map[string]*SynthVault),
		deposits:    make([]*SynthDeposit, 0),
		withdrawals: make([]*SynthWithdrawal, 0),
		mints:       make([]*SynthMint, 0),
		burns:       make([]*SynthBurn, 0),
		transmutes:  make([]*SynthTransmute, 0),
		positions:   make(map[string]*SynthPosition),
		synthTokens: make(map[string]*SynthToken),
	}
}

// RegisterSynthToken registers a synthetic token
func (s *SynthsIndexer) RegisterSynthToken(token *SynthToken) {
	s.synthTokens[strings.ToLower(token.Address)] = token
}

// SetCallbacks sets event callbacks
func (s *SynthsIndexer) SetCallbacks(
	onDeposit func(*SynthDeposit),
	onWithdrawal func(*SynthWithdrawal),
	onMint func(*SynthMint),
	onBurn func(*SynthBurn),
	onTransmute func(*SynthTransmute),
) {
	s.onDeposit = onDeposit
	s.onWithdrawal = onWithdrawal
	s.onMint = onMint
	s.onBurn = onBurn
	s.onTransmute = onTransmute
}

// IndexLog processes a log entry for synths events
func (s *SynthsIndexer) IndexLog(log *LogEntry) error {
	if len(log.Topics) == 0 {
		return nil
	}
	
	topic0 := log.Topics[0]
	
	switch topic0 {
	case SynthsDepositSig:
		return s.indexDeposit(log)
	case SynthsWithdrawSig:
		return s.indexWithdrawal(log)
	case SynthsMintSig:
		return s.indexMint(log)
	case SynthsBurnSig:
		return s.indexBurn(log)
	case SynthsTransmuteSig:
		return s.indexTransmute(log)
	}
	
	return nil
}

// indexDeposit processes a deposit event
func (s *SynthsIndexer) indexDeposit(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid deposit event")
	}
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 96 {
		return fmt.Errorf("invalid deposit data")
	}
	
	account := topicToAddress(log.Topics[1])
	token := "0x" + hex.EncodeToString(data[12:32])
	amount := new(big.Int).SetBytes(data[32:64])
	shares := new(big.Int).SetBytes(data[64:96])
	
	deposit := &SynthDeposit{
		ID:           fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:       log.TxHash,
		BlockNumber:  log.BlockNumber,
		LogIndex:     log.LogIndex,
		VaultAddress: log.Address,
		Account:      account,
		Token:        token,
		Amount:       amount,
		Shares:       shares,
		Timestamp:    log.Timestamp,
	}
	
	s.deposits = append(s.deposits, deposit)
	
	// Update position
	s.updatePositionDeposit(log.Address, account, amount)
	
	// Update vault
	vault := s.getOrCreateVault(log.Address)
	vault.TotalDeposited = new(big.Int).Add(vault.TotalDeposited, amount)
	vault.UpdatedAt = log.Timestamp
	
	if s.onDeposit != nil {
		s.onDeposit(deposit)
	}
	
	return nil
}

// indexWithdrawal processes a withdrawal event
func (s *SynthsIndexer) indexWithdrawal(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid withdrawal event")
	}
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 96 {
		return fmt.Errorf("invalid withdrawal data")
	}
	
	account := topicToAddress(log.Topics[1])
	token := "0x" + hex.EncodeToString(data[12:32])
	amount := new(big.Int).SetBytes(data[32:64])
	shares := new(big.Int).SetBytes(data[64:96])
	
	withdrawal := &SynthWithdrawal{
		ID:           fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:       log.TxHash,
		BlockNumber:  log.BlockNumber,
		LogIndex:     log.LogIndex,
		VaultAddress: log.Address,
		Account:      account,
		Token:        token,
		Amount:       amount,
		Shares:       shares,
		Timestamp:    log.Timestamp,
	}
	
	s.withdrawals = append(s.withdrawals, withdrawal)
	
	// Update position
	s.updatePositionWithdrawal(log.Address, account, amount)
	
	// Update vault
	vault := s.getOrCreateVault(log.Address)
	vault.TotalDeposited = new(big.Int).Sub(vault.TotalDeposited, amount)
	vault.UpdatedAt = log.Timestamp
	
	if s.onWithdrawal != nil {
		s.onWithdrawal(withdrawal)
	}
	
	return nil
}

// indexMint processes a mint (borrow) event
func (s *SynthsIndexer) indexMint(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid mint event")
	}
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 64 {
		return fmt.Errorf("invalid mint data")
	}
	
	account := topicToAddress(log.Topics[1])
	amount := new(big.Int).SetBytes(data[0:32])
	recipient := "0x" + hex.EncodeToString(data[44:64])
	
	vault := s.getOrCreateVault(log.Address)
	
	mint := &SynthMint{
		ID:           fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:       log.TxHash,
		BlockNumber:  log.BlockNumber,
		LogIndex:     log.LogIndex,
		VaultAddress: log.Address,
		Account:      account,
		SynthToken:   vault.SynthToken,
		Amount:       amount,
		Recipient:    recipient,
		Timestamp:    log.Timestamp,
	}
	
	s.mints = append(s.mints, mint)
	
	// Update position
	s.updatePositionMint(log.Address, account, amount)
	
	// Update vault
	vault.TotalBorrowed = new(big.Int).Add(vault.TotalBorrowed, amount)
	vault.UpdatedAt = log.Timestamp
	
	if s.onMint != nil {
		s.onMint(mint)
	}
	
	return nil
}

// indexBurn processes a burn (repay) event
func (s *SynthsIndexer) indexBurn(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid burn event")
	}
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 32 {
		return fmt.Errorf("invalid burn data")
	}
	
	account := topicToAddress(log.Topics[1])
	amount := new(big.Int).SetBytes(data[0:32])
	
	vault := s.getOrCreateVault(log.Address)
	
	burn := &SynthBurn{
		ID:           fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:       log.TxHash,
		BlockNumber:  log.BlockNumber,
		LogIndex:     log.LogIndex,
		VaultAddress: log.Address,
		Account:      account,
		SynthToken:   vault.SynthToken,
		Amount:       amount,
		Timestamp:    log.Timestamp,
	}
	
	s.burns = append(s.burns, burn)
	
	// Update position
	s.updatePositionBurn(log.Address, account, amount)
	
	// Update vault
	vault.TotalBorrowed = new(big.Int).Sub(vault.TotalBorrowed, amount)
	vault.UpdatedAt = log.Timestamp
	
	if s.onBurn != nil {
		s.onBurn(burn)
	}
	
	return nil
}

// indexTransmute processes a transmute event
func (s *SynthsIndexer) indexTransmute(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid transmute event")
	}
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 128 {
		return fmt.Errorf("invalid transmute data")
	}
	
	account := topicToAddress(log.Topics[1])
	synthToken := "0x" + hex.EncodeToString(data[12:32])
	synthAmount := new(big.Int).SetBytes(data[32:64])
	underlyingToken := "0x" + hex.EncodeToString(data[76:96])
	underlyingAmount := new(big.Int).SetBytes(data[96:128])
	
	transmute := &SynthTransmute{
		ID:               fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:           log.TxHash,
		BlockNumber:      log.BlockNumber,
		LogIndex:         log.LogIndex,
		TransmuterAddr:   log.Address,
		Account:          account,
		SynthToken:       synthToken,
		SynthAmount:      synthAmount,
		UnderlyingToken:  underlyingToken,
		UnderlyingAmount: underlyingAmount,
		Timestamp:        log.Timestamp,
	}
	
	s.transmutes = append(s.transmutes, transmute)
	
	if s.onTransmute != nil {
		s.onTransmute(transmute)
	}
	
	return nil
}

// Position update helpers
func (s *SynthsIndexer) getPositionID(vault, account string) string {
	return fmt.Sprintf("%s:%s", strings.ToLower(account), strings.ToLower(vault))
}

func (s *SynthsIndexer) getOrCreatePosition(vault, account string) *SynthPosition {
	id := s.getPositionID(vault, account)
	if pos, exists := s.positions[id]; exists {
		return pos
	}
	
	pos := &SynthPosition{
		ID:           id,
		Account:      account,
		VaultAddress: vault,
		Deposited:    big.NewInt(0),
		Borrowed:     big.NewInt(0),
		YieldEarned:  big.NewInt(0),
		DebtRepaid:   big.NewInt(0),
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	s.positions[id] = pos
	return pos
}

func (s *SynthsIndexer) updatePositionDeposit(vault, account string, amount *big.Int) {
	pos := s.getOrCreatePosition(vault, account)
	pos.Deposited = new(big.Int).Add(pos.Deposited, amount)
	pos.UpdatedAt = time.Now()
	s.updatePositionLTV(pos)
}

func (s *SynthsIndexer) updatePositionWithdrawal(vault, account string, amount *big.Int) {
	pos := s.getOrCreatePosition(vault, account)
	pos.Deposited = new(big.Int).Sub(pos.Deposited, amount)
	pos.UpdatedAt = time.Now()
	s.updatePositionLTV(pos)
}

func (s *SynthsIndexer) updatePositionMint(vault, account string, amount *big.Int) {
	pos := s.getOrCreatePosition(vault, account)
	pos.Borrowed = new(big.Int).Add(pos.Borrowed, amount)
	pos.UpdatedAt = time.Now()
	s.updatePositionLTV(pos)
}

func (s *SynthsIndexer) updatePositionBurn(vault, account string, amount *big.Int) {
	pos := s.getOrCreatePosition(vault, account)
	pos.Borrowed = new(big.Int).Sub(pos.Borrowed, amount)
	pos.UpdatedAt = time.Now()
	s.updatePositionLTV(pos)
	
	if pos.Borrowed.Sign() <= 0 && pos.Deposited.Sign() <= 0 {
		pos.IsActive = false
	}
}

func (s *SynthsIndexer) updatePositionLTV(pos *SynthPosition) {
	if pos.Deposited.Sign() == 0 {
		pos.LTV = 0
		pos.HealthFactor = 0
		return
	}
	
	borrowedFloat := new(big.Float).SetInt(pos.Borrowed)
	depositedFloat := new(big.Float).SetInt(pos.Deposited)
	
	ltv, _ := new(big.Float).Quo(borrowedFloat, depositedFloat).Float64()
	pos.LTV = ltv
	
	// Health factor = 1/LTV (higher is healthier)
	if ltv > 0 {
		pos.HealthFactor = 1.0 / ltv
	} else {
		pos.HealthFactor = 100 // Fully collateralized
	}
}

// getOrCreateVault gets or creates a vault entry
func (s *SynthsIndexer) getOrCreateVault(address string) *SynthVault {
	addr := strings.ToLower(address)
	if vault, exists := s.vaults[addr]; exists {
		return vault
	}
	
	vault := &SynthVault{
		Address:        addr,
		TotalDeposited: big.NewInt(0),
		TotalBorrowed:  big.NewInt(0),
		TVL:            big.NewFloat(0),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	s.vaults[addr] = vault
	return vault
}

// GetVault returns a vault by address
func (s *SynthsIndexer) GetVault(address string) *SynthVault {
	return s.vaults[strings.ToLower(address)]
}

// GetAllVaults returns all indexed vaults
func (s *SynthsIndexer) GetAllVaults() []*SynthVault {
	vaults := make([]*SynthVault, 0, len(s.vaults))
	for _, v := range s.vaults {
		vaults = append(vaults, v)
	}
	return vaults
}

// GetPositionsByAccount returns all positions for an account
func (s *SynthsIndexer) GetPositionsByAccount(account string) []*SynthPosition {
	var result []*SynthPosition
	acc := strings.ToLower(account)
	for _, pos := range s.positions {
		if strings.ToLower(pos.Account) == acc {
			result = append(result, pos)
		}
	}
	return result
}

// GetActivePositions returns all active positions
func (s *SynthsIndexer) GetActivePositions() []*SynthPosition {
	var result []*SynthPosition
	for _, pos := range s.positions {
		if pos.IsActive {
			result = append(result, pos)
		}
	}
	return result
}

// SynthsStats represents synthetics indexer statistics
type SynthsStats struct {
	VaultCount       uint64    `json:"vaultCount"`
	PositionCount    uint64    `json:"positionCount"`
	ActivePositions  uint64    `json:"activePositions"`
	TotalDeposited   *big.Int  `json:"totalDeposited"`
	TotalBorrowed    *big.Int  `json:"totalBorrowed"`
	TotalDeposits    uint64    `json:"totalDeposits"`
	TotalWithdrawals uint64    `json:"totalWithdrawals"`
	TotalMints       uint64    `json:"totalMints"`
	TotalBurns       uint64    `json:"totalBurns"`
	TotalTransmutes  uint64    `json:"totalTransmutes"`
	LastUpdated      time.Time `json:"lastUpdated"`
}

// GetStats returns aggregate statistics
func (s *SynthsIndexer) GetStats() *SynthsStats {
	stats := &SynthsStats{
		VaultCount:       uint64(len(s.vaults)),
		PositionCount:    uint64(len(s.positions)),
		ActivePositions:  uint64(len(s.GetActivePositions())),
		TotalDeposited:   big.NewInt(0),
		TotalBorrowed:    big.NewInt(0),
		TotalDeposits:    uint64(len(s.deposits)),
		TotalWithdrawals: uint64(len(s.withdrawals)),
		TotalMints:       uint64(len(s.mints)),
		TotalBurns:       uint64(len(s.burns)),
		TotalTransmutes:  uint64(len(s.transmutes)),
		LastUpdated:      time.Now(),
	}
	
	for _, vault := range s.vaults {
		stats.TotalDeposited.Add(stats.TotalDeposited, vault.TotalDeposited)
		stats.TotalBorrowed.Add(stats.TotalBorrowed, vault.TotalBorrowed)
	}
	
	return stats
}
