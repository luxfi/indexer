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

// Prediction market event signatures (keccak256)
// Source: ~/work/lux/standard/contracts/prediction/Oracle.sol, Resolver.sol
var (
	// Oracle events
	PredictionAssertionMadeSig     = "0xf94e7cc25aad19548dbdb13a5b9b5b4f2087c3f82f78b5e7daceb2f7a2352cec" // AssertionMade(bytes32,bytes32,bytes,address,address,address,address,uint64,address,uint256,bytes32)
	PredictionAssertionDisputedSig = "0x6139ed560940970b04cfe87f11aad42585432c4f675e36b5fb9a0f4f797cab96" // AssertionDisputed(bytes32,address,address)
	PredictionAssertionSettledSig  = "0xccaaab44ef3b236cdcf1dff4aa808f204f54ce0f7dbbde6d4a9a56dc239998df" // AssertionSettled(bytes32,address,bool,bool,address)

	// Resolver events
	PredictionQuestionInitSig     = "0x376968d710a81c988b6e7ee2f60ebf5da7ca9d88dd383bdb4cfbf51cd723d81f" // QuestionInitialized(bytes32,uint256,address,bytes,address,uint256,uint256)
	PredictionQuestionResolvedSig = "0x5b5621fd8d8f52a474b5df8ff183a39eb101fc06a689c9ef4c5bb86fd7d30b70" // QuestionResolved(bytes32,int256,uint256[])
	PredictionQuestionFlaggedSig  = "0x96fa5c748028896a9cb67890a52cb7fbc932f6a4cf40a0e0f7d73cdd56f0087f" // QuestionFlagged(bytes32)
	PredictionQuestionResetSig    = "0x0f8106ccb8e432043a831b8fa90f45d24c736453b64c0e8d970b642f84535fc7" // QuestionReset(bytes32)
	PredictionQuestionPausedSig   = "0x941ba79289813c3a7cea90f413c65992411fa61cf575653d3937491a1f0c868f" // QuestionPaused(bytes32)
)

// PredictionIndexer indexes prediction market events
type PredictionIndexer struct {
	assertions map[string]*PredictionAssertion // assertionId -> assertion
	questions  map[string]*PredictionQuestion  // questionId -> question
	events     []*PredictionEvent
	onAssertion func(*PredictionEvent)
	onQuestion  func(*PredictionEvent)
}

// PredictionAssertion represents an oracle assertion
type PredictionAssertion struct {
	AssertionID string    `json:"assertionId"`
	DomainID    string    `json:"domainId,omitempty"`
	Asserter    string    `json:"asserter"`
	Disputer    string    `json:"disputer,omitempty"`
	Bond        *big.Int  `json:"bond"`
	IsSettled   bool      `json:"isSettled"`
	Resolution  bool      `json:"resolution"`
	IsDisputed  bool      `json:"isDisputed"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// PredictionQuestion represents a prediction market question
type PredictionQuestion struct {
	QuestionID string    `json:"questionId"`
	Creator    string    `json:"creator"`
	RewardToken string  `json:"rewardToken"`
	Reward     *big.Int  `json:"reward"`
	Bond       *big.Int  `json:"bond"`
	IsResolved bool      `json:"isResolved"`
	IsPaused   bool      `json:"isPaused"`
	IsFlagged  bool      `json:"isFlagged"`
	Outcome    int64     `json:"outcome,omitempty"` // settled price
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// PredictionEvent represents a prediction market event
type PredictionEvent struct {
	ID          string    `json:"id"`
	TxHash      string    `json:"txHash"`
	BlockNumber uint64    `json:"blockNumber"`
	LogIndex    uint64    `json:"logIndex"`
	Contract    string    `json:"contract"`
	EventType   string    `json:"eventType"`
	AssertionID string    `json:"assertionId,omitempty"`
	QuestionID  string    `json:"questionId,omitempty"`
	Asserter    string    `json:"asserter,omitempty"`
	Disputer    string    `json:"disputer,omitempty"`
	Creator     string    `json:"creator,omitempty"`
	Bond        *big.Int  `json:"bond,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// NewPredictionIndexer creates a new prediction market indexer
func NewPredictionIndexer() *PredictionIndexer {
	return &PredictionIndexer{
		assertions: make(map[string]*PredictionAssertion),
		questions:  make(map[string]*PredictionQuestion),
		events:     make([]*PredictionEvent, 0),
	}
}

// SetCallbacks sets event callbacks
func (p *PredictionIndexer) SetCallbacks(onAssertion, onQuestion func(*PredictionEvent)) {
	p.onAssertion = onAssertion
	p.onQuestion = onQuestion
}

// IndexLog processes a log entry for prediction market events
func (p *PredictionIndexer) IndexLog(log *LogEntry) error {
	if len(log.Topics) == 0 {
		return nil
	}

	switch log.Topics[0] {
	case PredictionAssertionMadeSig:
		return p.indexAssertionMade(log)
	case PredictionAssertionDisputedSig:
		return p.indexAssertionDisputed(log)
	case PredictionAssertionSettledSig:
		return p.indexAssertionSettled(log)
	case PredictionQuestionInitSig:
		return p.indexQuestionInitialized(log)
	case PredictionQuestionResolvedSig:
		return p.indexQuestionResolved(log)
	case PredictionQuestionFlaggedSig:
		return p.indexQuestionFlagged(log)
	case PredictionQuestionResetSig:
		return p.indexQuestionReset(log)
	case PredictionQuestionPausedSig:
		return p.indexQuestionPaused(log)
	}

	return nil
}

// indexAssertionMade processes AssertionMade event
// Topics: [sig, assertionId(indexed), domainId(indexed)]
// Data: claim, asserter, callbackRecipient, escalationManager, caller, expirationTime, currency, bond, identifier
func (p *PredictionIndexer) indexAssertionMade(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid AssertionMade event: need 3 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	assertionID := log.Topics[1]
	domainID := log.Topics[2]

	// Bond is at offset 256 (word 8) in data (after claim offset, asserter, callback, escalation, caller, expiry, currency)
	var bond *big.Int
	if len(data) >= 288 { // 9 * 32
		bond = new(big.Int).SetBytes(data[256:288])
	}

	// Asserter is at offset 32 in data (word 1, after claim offset)
	var asserter string
	if len(data) >= 64 {
		asserter = "0x" + hex.EncodeToString(data[44:64])
	}

	evt := &PredictionEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "assertion_made",
		AssertionID: assertionID,
		Asserter:    asserter,
		Bond:        bond,
		Timestamp:   log.Timestamp,
	}
	p.events = append(p.events, evt)

	p.assertions[assertionID] = &PredictionAssertion{
		AssertionID: assertionID,
		DomainID:    domainID,
		Asserter:    asserter,
		Bond:        bond,
		CreatedAt:   log.Timestamp,
		UpdatedAt:   log.Timestamp,
	}

	if p.onAssertion != nil {
		p.onAssertion(evt)
	}
	return nil
}

// indexAssertionDisputed processes AssertionDisputed(bytes32 indexed assertionId, address indexed caller, address indexed disputer)
func (p *PredictionIndexer) indexAssertionDisputed(log *LogEntry) error {
	if len(log.Topics) < 4 {
		return fmt.Errorf("invalid AssertionDisputed event: need 4 topics, got %d", len(log.Topics))
	}

	assertionID := log.Topics[1]
	disputer := topicToAddress(log.Topics[3])

	evt := &PredictionEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "assertion_disputed",
		AssertionID: assertionID,
		Disputer:    disputer,
		Timestamp:   log.Timestamp,
	}
	p.events = append(p.events, evt)

	if assertion, ok := p.assertions[assertionID]; ok {
		assertion.IsDisputed = true
		assertion.Disputer = disputer
		assertion.UpdatedAt = log.Timestamp
	}

	if p.onAssertion != nil {
		p.onAssertion(evt)
	}
	return nil
}

// indexAssertionSettled processes AssertionSettled(bytes32 indexed assertionId, address indexed bondRecipient, bool disputed, bool resolution, address caller)
func (p *PredictionIndexer) indexAssertionSettled(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid AssertionSettled event: need 3 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	assertionID := log.Topics[1]

	var resolution bool
	if len(data) >= 64 {
		// disputed = data[0:32], resolution = data[32:64]
		resBytes := data[32:64]
		resolution = resBytes[31] == 1
	}

	evt := &PredictionEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "assertion_settled",
		AssertionID: assertionID,
		Timestamp:   log.Timestamp,
	}
	p.events = append(p.events, evt)

	if assertion, ok := p.assertions[assertionID]; ok {
		assertion.IsSettled = true
		assertion.Resolution = resolution
		assertion.UpdatedAt = log.Timestamp
	}

	if p.onAssertion != nil {
		p.onAssertion(evt)
	}
	return nil
}

// indexQuestionInitialized processes QuestionInitialized(bytes32 indexed questionID, uint256 indexed requestTimestamp, address indexed creator, bytes ancillaryData, address rewardToken, uint256 reward, uint256 proposalBond)
func (p *PredictionIndexer) indexQuestionInitialized(log *LogEntry) error {
	if len(log.Topics) < 4 {
		return fmt.Errorf("invalid QuestionInitialized event: need 4 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}

	questionID := log.Topics[1]
	creator := topicToAddress(log.Topics[3])

	// Data layout: ancillaryData offset, rewardToken, reward, proposalBond
	var rewardToken string
	var reward, bond *big.Int
	if len(data) >= 128 {
		rewardToken = "0x" + hex.EncodeToString(data[44:64])
		reward = new(big.Int).SetBytes(data[64:96])
		bond = new(big.Int).SetBytes(data[96:128])
	}

	evt := &PredictionEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "question_initialized",
		QuestionID:  questionID,
		Creator:     creator,
		Bond:        bond,
		Timestamp:   log.Timestamp,
	}
	p.events = append(p.events, evt)

	p.questions[questionID] = &PredictionQuestion{
		QuestionID:  questionID,
		Creator:     creator,
		RewardToken: rewardToken,
		Reward:      reward,
		Bond:        bond,
		CreatedAt:   log.Timestamp,
		UpdatedAt:   log.Timestamp,
	}

	if p.onQuestion != nil {
		p.onQuestion(evt)
	}
	return nil
}

// indexQuestionResolved processes QuestionResolved(bytes32 indexed questionID, int256 indexed settledPrice, uint256[] payouts)
func (p *PredictionIndexer) indexQuestionResolved(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid QuestionResolved event: need 3 topics, got %d", len(log.Topics))
	}

	questionID := log.Topics[1]

	// settledPrice is indexed topic2 as int256
	priceBytes := hexToBytes(log.Topics[2])
	price := new(big.Int).SetBytes(priceBytes)
	if len(priceBytes) > 0 && priceBytes[0]&0x80 != 0 {
		price.Sub(price, new(big.Int).Lsh(big.NewInt(1), 256))
	}

	evt := &PredictionEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "question_resolved",
		QuestionID:  questionID,
		Timestamp:   log.Timestamp,
	}
	p.events = append(p.events, evt)

	if q, ok := p.questions[questionID]; ok {
		q.IsResolved = true
		q.Outcome = price.Int64()
		q.UpdatedAt = log.Timestamp
	}

	if p.onQuestion != nil {
		p.onQuestion(evt)
	}
	return nil
}

// indexQuestionFlagged processes QuestionFlagged(bytes32 indexed questionID)
func (p *PredictionIndexer) indexQuestionFlagged(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid QuestionFlagged event: need 2 topics, got %d", len(log.Topics))
	}

	questionID := log.Topics[1]

	evt := &PredictionEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "question_flagged",
		QuestionID:  questionID,
		Timestamp:   log.Timestamp,
	}
	p.events = append(p.events, evt)

	if q, ok := p.questions[questionID]; ok {
		q.IsFlagged = true
		q.UpdatedAt = log.Timestamp
	}

	if p.onQuestion != nil {
		p.onQuestion(evt)
	}
	return nil
}

// indexQuestionReset processes QuestionReset(bytes32 indexed questionID)
func (p *PredictionIndexer) indexQuestionReset(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid QuestionReset event: need 2 topics, got %d", len(log.Topics))
	}

	questionID := log.Topics[1]

	evt := &PredictionEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "question_reset",
		QuestionID:  questionID,
		Timestamp:   log.Timestamp,
	}
	p.events = append(p.events, evt)

	if q, ok := p.questions[questionID]; ok {
		q.IsResolved = false
		q.IsFlagged = false
		q.UpdatedAt = log.Timestamp
	}

	if p.onQuestion != nil {
		p.onQuestion(evt)
	}
	return nil
}

// indexQuestionPaused processes QuestionPaused(bytes32 indexed questionID)
func (p *PredictionIndexer) indexQuestionPaused(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid QuestionPaused event: need 2 topics, got %d", len(log.Topics))
	}

	questionID := log.Topics[1]

	evt := &PredictionEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "question_paused",
		QuestionID:  questionID,
		Timestamp:   log.Timestamp,
	}
	p.events = append(p.events, evt)

	if q, ok := p.questions[questionID]; ok {
		q.IsPaused = true
		q.UpdatedAt = log.Timestamp
	}

	if p.onQuestion != nil {
		p.onQuestion(evt)
	}
	return nil
}

// GetAssertion returns an assertion by ID
func (p *PredictionIndexer) GetAssertion(id string) *PredictionAssertion {
	return p.assertions[id]
}

// GetQuestion returns a question by ID
func (p *PredictionIndexer) GetQuestion(id string) *PredictionQuestion {
	return p.questions[id]
}

// GetActiveQuestions returns unresolved questions
func (p *PredictionIndexer) GetActiveQuestions() []*PredictionQuestion {
	var result []*PredictionQuestion
	for _, q := range p.questions {
		if !q.IsResolved {
			result = append(result, q)
		}
	}
	return result
}

// PredictionStats represents prediction market statistics
type PredictionStats struct {
	TotalAssertions   uint64    `json:"totalAssertions"`
	TotalQuestions    uint64    `json:"totalQuestions"`
	ResolvedQuestions uint64    `json:"resolvedQuestions"`
	DisputedAssertions uint64  `json:"disputedAssertions"`
	TotalEvents       uint64    `json:"totalEvents"`
	LastUpdated       time.Time `json:"lastUpdated"`
}

// GetStats returns aggregate statistics
func (p *PredictionIndexer) GetStats() *PredictionStats {
	resolved := uint64(0)
	for _, q := range p.questions {
		if q.IsResolved {
			resolved++
		}
	}
	disputed := uint64(0)
	for _, a := range p.assertions {
		if a.IsDisputed {
			disputed++
		}
	}
	return &PredictionStats{
		TotalAssertions:    uint64(len(p.assertions)),
		TotalQuestions:     uint64(len(p.questions)),
		ResolvedQuestions:  resolved,
		DisputedAssertions: disputed,
		TotalEvents:        uint64(len(p.events)),
		LastUpdated:        time.Now(),
	}
}
