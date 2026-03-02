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

// Securities event signatures (keccak256)
// Source: ~/work/lux/standard/contracts/securities/
var (
	// Compliance events
	SecWhitelistAddedSig   = "0xb090f36816bb703b76ee25e0a5b09d45ceef93fe7b13a78a2780be5e80b3673b" // WhitelistAdded(address)
	SecWhitelistRemovedSig = "0x7f58f86b6335206730c295dfc88e1132395cedb23c2cc3c7ed24d7ef9b3cb45e" // WhitelistRemoved(address)
	SecLockupSetSig        = "0x31c8a394c333989e7b3c8fcf0d55736b3dcbb204c8cbc9bc5fa5745467ac394f" // LockupSet(address,uint256)
	SecAccreditationSetSig = "0x122a20d847c5829407e01d5cf6ed51b19678b72457e3172d7453f2a574e63011" // AccreditationSet(address,uint8)

	// Corporate action events
	SecForcedTransferSig = "0x89b3521149aa3b41610fe19c951376ff6536b54a3348d50571776665d058d4a1" // ForcedTransfer(address,address,uint256,string)
	SecTokensSeizedSig   = "0xd058ed99a57d50e7fe5220afc792e44890449ef7617b45cca214001867698309" // TokensSeized(address,uint256,string)
	SecTokensFrozenSig   = "0x580c6d09ee167b9db7ef867b9d63359e479f5ada483ff886606c7d8429026f2d" // TokensFrozen(address,uint256)

	// Dividend events
	SecDividendCreatedSig   = "0x2d50f059e2e6345da93812883775b8b08a23287fe0c17a93e282fa5e088abd73" // DividendCreated(uint256,address,uint256,uint256)
	SecDividendClaimedSig   = "0xb8ce5e0c6ccf29264c7897394984be5b64f6fde64fe5d5ad4ed4dc33769defb2" // DividendClaimed(uint256,address,uint256)
	SecDividendReclaimedSig = "0xbccc886fd89a16cbdce5f22334b47f42360c03d8e691064eab3743313f25e391" // DividendReclaimed(uint256,uint256)

	// Partition token events
	SecTransferByPartitionSig = "0x32d1dfd209cb6fd7b2cd2809fe5b7bb0f74bdcbe131a1e596afd49e8aceb8c52" // TransferByPartition(bytes32,address,address,uint256)
	SecIssueByPartitionSig    = "0x956eaf12c9de50b8917a03a4126cf606c76109ec2f518ebb3bb47859c27af1b7" // IssueByPartition(bytes32,address,uint256)
	SecRedeemByPartitionSig   = "0xd94e778295bbca247eabc8f34a29bbe171d0b4eea6ff3b86ea29dcb82f580c7a" // RedeemByPartition(bytes32,address,uint256)

	// Document registry events
	SecDocumentUpdatedSig = "0x5648f58c6233e3959675c4a797ffdcfaec554a9d9e85972e0582d464f03f3440" // DocumentUpdated(bytes32,string,bytes32)
)

// SecuritiesIndexer indexes security token events
type SecuritiesIndexer struct {
	compliance   map[string]*ComplianceStatus // address -> compliance
	dividends    map[uint64]*DividendRound    // roundId -> dividend
	events       []*SecuritiesEvent
	onCompliance func(*SecuritiesEvent)
	onDividend   func(*SecuritiesEvent)
	onCorporate  func(*SecuritiesEvent)
}

// ComplianceStatus tracks an address's compliance state
type ComplianceStatus struct {
	Address       string    `json:"address"`
	IsWhitelisted bool      `json:"isWhitelisted"`
	Accreditation uint8     `json:"accreditation"`
	LockupExpiry  uint64    `json:"lockupExpiry,omitempty"`
	IsFrozen      bool      `json:"isFrozen"`
	FrozenAmount  *big.Int  `json:"frozenAmount,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// DividendRound represents a dividend distribution round
type DividendRound struct {
	RoundID       uint64    `json:"roundId"`
	PaymentToken  string    `json:"paymentToken"`
	TotalAmount   *big.Int  `json:"totalAmount"`
	ClaimedAmount *big.Int  `json:"claimedAmount"`
	SnapshotBlock uint64    `json:"snapshotBlock"`
	Claimants     int       `json:"claimants"`
	IsReclaimed   bool      `json:"isReclaimed"`
	CreatedAt     time.Time `json:"createdAt"`
}

// SecuritiesEvent represents a security token event
type SecuritiesEvent struct {
	ID          string    `json:"id"`
	TxHash      string    `json:"txHash"`
	BlockNumber uint64    `json:"blockNumber"`
	LogIndex    uint64    `json:"logIndex"`
	Contract    string    `json:"contract"`
	EventType   string    `json:"eventType"`
	Account     string    `json:"account,omitempty"`
	From        string    `json:"from,omitempty"`
	To          string    `json:"to,omitempty"`
	Amount      *big.Int  `json:"amount,omitempty"`
	RoundID     uint64    `json:"roundId,omitempty"`
	Partition   string    `json:"partition,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// NewSecuritiesIndexer creates a new securities indexer
func NewSecuritiesIndexer() *SecuritiesIndexer {
	return &SecuritiesIndexer{
		compliance: make(map[string]*ComplianceStatus),
		dividends:  make(map[uint64]*DividendRound),
		events:     make([]*SecuritiesEvent, 0),
	}
}

// SetCallbacks sets event callbacks
func (s *SecuritiesIndexer) SetCallbacks(onCompliance, onDividend, onCorporate func(*SecuritiesEvent)) {
	s.onCompliance = onCompliance
	s.onDividend = onDividend
	s.onCorporate = onCorporate
}

// IndexLog processes a log entry for securities events
func (s *SecuritiesIndexer) IndexLog(log *LogEntry) error {
	if len(log.Topics) == 0 {
		return nil
	}

	switch log.Topics[0] {
	// Compliance
	case SecWhitelistAddedSig:
		return s.indexWhitelistChange(log, true)
	case SecWhitelistRemovedSig:
		return s.indexWhitelistChange(log, false)
	case SecLockupSetSig:
		return s.indexLockupSet(log)
	case SecAccreditationSetSig:
		return s.indexAccreditationSet(log)

	// Corporate actions
	case SecForcedTransferSig:
		return s.indexForcedTransfer(log)
	case SecTokensSeizedSig:
		return s.indexTokensSeized(log)
	case SecTokensFrozenSig:
		return s.indexTokensFrozen(log)

	// Dividends
	case SecDividendCreatedSig:
		return s.indexDividendCreated(log)
	case SecDividendClaimedSig:
		return s.indexDividendClaimed(log)
	case SecDividendReclaimedSig:
		return s.indexDividendReclaimed(log)

	// Partitions
	case SecTransferByPartitionSig:
		return s.indexTransferByPartition(log)
	case SecIssueByPartitionSig:
		return s.indexIssueByPartition(log)
	case SecRedeemByPartitionSig:
		return s.indexRedeemByPartition(log)
	}

	return nil
}

// indexWhitelistChange processes WhitelistAdded/WhitelistRemoved(address indexed account)
func (s *SecuritiesIndexer) indexWhitelistChange(log *LogEntry, added bool) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid whitelist event: need 2 topics, got %d", len(log.Topics))
	}

	account := topicToAddress(log.Topics[1])
	eventType := "whitelist_removed"
	if added {
		eventType = "whitelist_added"
	}

	evt := &SecuritiesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   eventType,
		Account:     account,
		Timestamp:   log.Timestamp,
	}
	s.events = append(s.events, evt)

	cs := s.getOrCreateCompliance(account)
	cs.IsWhitelisted = added
	cs.UpdatedAt = log.Timestamp

	if s.onCompliance != nil {
		s.onCompliance(evt)
	}
	return nil
}

// indexLockupSet processes LockupSet(address indexed account, uint256 expiry)
func (s *SecuritiesIndexer) indexLockupSet(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid LockupSet event: need 2 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 32 {
		return fmt.Errorf("invalid LockupSet data: need 32 bytes, got %d", len(data))
	}

	account := topicToAddress(log.Topics[1])
	expiry := new(big.Int).SetBytes(data[0:32]).Uint64()

	evt := &SecuritiesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "lockup_set",
		Account:     account,
		Timestamp:   log.Timestamp,
	}
	s.events = append(s.events, evt)

	cs := s.getOrCreateCompliance(account)
	cs.LockupExpiry = expiry
	cs.UpdatedAt = log.Timestamp

	if s.onCompliance != nil {
		s.onCompliance(evt)
	}
	return nil
}

// indexAccreditationSet processes AccreditationSet(address indexed account, uint8 status)
func (s *SecuritiesIndexer) indexAccreditationSet(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid AccreditationSet event: need 2 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 32 {
		return fmt.Errorf("invalid AccreditationSet data: need 32 bytes, got %d", len(data))
	}

	account := topicToAddress(log.Topics[1])
	status := uint8(new(big.Int).SetBytes(data[0:32]).Uint64())

	evt := &SecuritiesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "accreditation_set",
		Account:     account,
		Timestamp:   log.Timestamp,
	}
	s.events = append(s.events, evt)

	cs := s.getOrCreateCompliance(account)
	cs.Accreditation = status
	cs.UpdatedAt = log.Timestamp

	if s.onCompliance != nil {
		s.onCompliance(evt)
	}
	return nil
}

// indexForcedTransfer processes ForcedTransfer(address indexed from, address indexed to, uint256 amount, string reason)
func (s *SecuritiesIndexer) indexForcedTransfer(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid ForcedTransfer event: need 3 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 32 {
		return fmt.Errorf("invalid ForcedTransfer data: need 32 bytes, got %d", len(data))
	}

	from := topicToAddress(log.Topics[1])
	to := topicToAddress(log.Topics[2])
	amount := new(big.Int).SetBytes(data[0:32])

	evt := &SecuritiesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "forced_transfer",
		From:        from,
		To:          to,
		Amount:      amount,
		Timestamp:   log.Timestamp,
	}
	s.events = append(s.events, evt)

	if s.onCorporate != nil {
		s.onCorporate(evt)
	}
	return nil
}

// indexTokensSeized processes TokensSeized(address indexed from, uint256 amount, string reason)
func (s *SecuritiesIndexer) indexTokensSeized(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid TokensSeized event: need 2 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 32 {
		return fmt.Errorf("invalid TokensSeized data: need 32 bytes, got %d", len(data))
	}

	account := topicToAddress(log.Topics[1])
	amount := new(big.Int).SetBytes(data[0:32])

	evt := &SecuritiesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "tokens_seized",
		Account:     account,
		Amount:      amount,
		Timestamp:   log.Timestamp,
	}
	s.events = append(s.events, evt)

	if s.onCorporate != nil {
		s.onCorporate(evt)
	}
	return nil
}

// indexTokensFrozen processes TokensFrozen(address indexed account, uint256 amount)
func (s *SecuritiesIndexer) indexTokensFrozen(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid TokensFrozen event: need 2 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 32 {
		return fmt.Errorf("invalid TokensFrozen data: need 32 bytes, got %d", len(data))
	}

	account := topicToAddress(log.Topics[1])
	amount := new(big.Int).SetBytes(data[0:32])

	evt := &SecuritiesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "tokens_frozen",
		Account:     account,
		Amount:      amount,
		Timestamp:   log.Timestamp,
	}
	s.events = append(s.events, evt)

	cs := s.getOrCreateCompliance(account)
	cs.IsFrozen = true
	cs.FrozenAmount = amount
	cs.UpdatedAt = log.Timestamp

	if s.onCorporate != nil {
		s.onCorporate(evt)
	}
	return nil
}

// indexDividendCreated processes DividendCreated(uint256 indexed roundId, address paymentToken, uint256 totalAmount, uint256 snapshotBlock)
func (s *SecuritiesIndexer) indexDividendCreated(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid DividendCreated event: need 2 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 96 {
		return fmt.Errorf("invalid DividendCreated data: need 96 bytes, got %d", len(data))
	}

	roundID := new(big.Int).SetBytes(hexToBytes(log.Topics[1])).Uint64()
	paymentToken := "0x" + hex.EncodeToString(data[12:32])
	totalAmount := new(big.Int).SetBytes(data[32:64])
	snapshotBlock := new(big.Int).SetBytes(data[64:96]).Uint64()

	evt := &SecuritiesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "dividend_created",
		RoundID:     roundID,
		Amount:      totalAmount,
		Timestamp:   log.Timestamp,
	}
	s.events = append(s.events, evt)

	s.dividends[roundID] = &DividendRound{
		RoundID:       roundID,
		PaymentToken:  paymentToken,
		TotalAmount:   totalAmount,
		ClaimedAmount: big.NewInt(0),
		SnapshotBlock: snapshotBlock,
		CreatedAt:     log.Timestamp,
	}

	if s.onDividend != nil {
		s.onDividend(evt)
	}
	return nil
}

// indexDividendClaimed processes DividendClaimed(uint256 indexed roundId, address indexed account, uint256 amount)
func (s *SecuritiesIndexer) indexDividendClaimed(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid DividendClaimed event: need 3 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 32 {
		return fmt.Errorf("invalid DividendClaimed data: need 32 bytes, got %d", len(data))
	}

	roundID := new(big.Int).SetBytes(hexToBytes(log.Topics[1])).Uint64()
	account := topicToAddress(log.Topics[2])
	amount := new(big.Int).SetBytes(data[0:32])

	evt := &SecuritiesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "dividend_claimed",
		Account:     account,
		RoundID:     roundID,
		Amount:      amount,
		Timestamp:   log.Timestamp,
	}
	s.events = append(s.events, evt)

	if div, ok := s.dividends[roundID]; ok {
		div.ClaimedAmount = new(big.Int).Add(div.ClaimedAmount, amount)
		div.Claimants++
	}

	if s.onDividend != nil {
		s.onDividend(evt)
	}
	return nil
}

// indexDividendReclaimed processes DividendReclaimed(uint256 indexed roundId, uint256 unclaimedAmount)
func (s *SecuritiesIndexer) indexDividendReclaimed(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid DividendReclaimed event: need 2 topics, got %d", len(log.Topics))
	}

	roundID := new(big.Int).SetBytes(hexToBytes(log.Topics[1])).Uint64()

	evt := &SecuritiesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "dividend_reclaimed",
		RoundID:     roundID,
		Timestamp:   log.Timestamp,
	}
	s.events = append(s.events, evt)

	if div, ok := s.dividends[roundID]; ok {
		div.IsReclaimed = true
	}

	if s.onDividend != nil {
		s.onDividend(evt)
	}
	return nil
}

// indexTransferByPartition processes TransferByPartition(bytes32 indexed partition, address indexed from, address indexed to, uint256 value)
func (s *SecuritiesIndexer) indexTransferByPartition(log *LogEntry) error {
	if len(log.Topics) < 4 {
		return fmt.Errorf("invalid TransferByPartition event: need 4 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 32 {
		return fmt.Errorf("invalid TransferByPartition data: need 32 bytes, got %d", len(data))
	}

	partition := log.Topics[1]
	from := topicToAddress(log.Topics[2])
	to := topicToAddress(log.Topics[3])
	amount := new(big.Int).SetBytes(data[0:32])

	evt := &SecuritiesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "transfer_by_partition",
		From:        from,
		To:          to,
		Amount:      amount,
		Partition:   partition,
		Timestamp:   log.Timestamp,
	}
	s.events = append(s.events, evt)

	if s.onCorporate != nil {
		s.onCorporate(evt)
	}
	return nil
}

// indexIssueByPartition processes IssueByPartition(bytes32 indexed partition, address indexed to, uint256 value)
func (s *SecuritiesIndexer) indexIssueByPartition(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid IssueByPartition event: need 3 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 32 {
		return fmt.Errorf("invalid IssueByPartition data: need 32 bytes, got %d", len(data))
	}

	partition := log.Topics[1]
	to := topicToAddress(log.Topics[2])
	amount := new(big.Int).SetBytes(data[0:32])

	evt := &SecuritiesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "issue_by_partition",
		To:          to,
		Amount:      amount,
		Partition:   partition,
		Timestamp:   log.Timestamp,
	}
	s.events = append(s.events, evt)

	if s.onCorporate != nil {
		s.onCorporate(evt)
	}
	return nil
}

// indexRedeemByPartition processes RedeemByPartition(bytes32 indexed partition, address indexed from, uint256 value)
func (s *SecuritiesIndexer) indexRedeemByPartition(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid RedeemByPartition event: need 3 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 32 {
		return fmt.Errorf("invalid RedeemByPartition data: need 32 bytes, got %d", len(data))
	}

	partition := log.Topics[1]
	from := topicToAddress(log.Topics[2])
	amount := new(big.Int).SetBytes(data[0:32])

	evt := &SecuritiesEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "redeem_by_partition",
		From:        from,
		Amount:      amount,
		Partition:   partition,
		Timestamp:   log.Timestamp,
	}
	s.events = append(s.events, evt)

	if s.onCorporate != nil {
		s.onCorporate(evt)
	}
	return nil
}

func (s *SecuritiesIndexer) getOrCreateCompliance(address string) *ComplianceStatus {
	addr := strings.ToLower(address)
	if cs, ok := s.compliance[addr]; ok {
		return cs
	}
	cs := &ComplianceStatus{
		Address:   addr,
		UpdatedAt: time.Now(),
	}
	s.compliance[addr] = cs
	return cs
}

// GetCompliance returns compliance status for an address
func (s *SecuritiesIndexer) GetCompliance(address string) *ComplianceStatus {
	return s.compliance[strings.ToLower(address)]
}

// GetDividend returns a dividend round
func (s *SecuritiesIndexer) GetDividend(roundID uint64) *DividendRound {
	return s.dividends[roundID]
}

// SecuritiesStats represents securities indexer statistics
type SecuritiesStats struct {
	WhitelistedAccounts uint64    `json:"whitelistedAccounts"`
	FrozenAccounts      uint64    `json:"frozenAccounts"`
	DividendRounds      uint64    `json:"dividendRounds"`
	TotalEvents         uint64    `json:"totalEvents"`
	LastUpdated         time.Time `json:"lastUpdated"`
}

// GetStats returns aggregate statistics
func (s *SecuritiesIndexer) GetStats() *SecuritiesStats {
	whitelisted := uint64(0)
	frozen := uint64(0)
	for _, cs := range s.compliance {
		if cs.IsWhitelisted {
			whitelisted++
		}
		if cs.IsFrozen {
			frozen++
		}
	}
	return &SecuritiesStats{
		WhitelistedAccounts: whitelisted,
		FrozenAccounts:      frozen,
		DividendRounds:      uint64(len(s.dividends)),
		TotalEvents:         uint64(len(s.events)),
		LastUpdated:         time.Now(),
	}
}
