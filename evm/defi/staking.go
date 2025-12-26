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

// StakingIndexer indexes staking protocol events
type StakingIndexer struct {
	pools       map[string]*StakingPool
	stakes      []*StakeEvent
	unstakes    []*UnstakeEvent
	rewards     []*RewardEvent
	positions   map[string]*StakingPosition // account:pool -> position
	onStake     func(*StakeEvent)
	onUnstake   func(*UnstakeEvent)
	onReward    func(*RewardEvent)
}

// StakingPool represents a staking pool
type StakingPool struct {
	Address        string     `json:"address"`
	Name           string     `json:"name"`
	StakeToken     string     `json:"stakeToken"`
	StakeSymbol    string     `json:"stakeSymbol,omitempty"`
	RewardToken    string     `json:"rewardToken"`
	RewardSymbol   string     `json:"rewardSymbol,omitempty"`
	ReceiptToken   string     `json:"receiptToken,omitempty"` // e.g., sLUX
	ReceiptSymbol  string     `json:"receiptSymbol,omitempty"`
	TotalStaked    *big.Int   `json:"totalStaked"`
	TotalRewards   *big.Int   `json:"totalRewards"`
	RewardRate     *big.Int   `json:"rewardRate"`     // Rewards per second
	APY            float64    `json:"apy"`
	MinStake       *big.Int   `json:"minStake,omitempty"`
	LockPeriod     uint64     `json:"lockPeriod,omitempty"` // Lock period in seconds
	CooldownPeriod uint64     `json:"cooldownPeriod,omitempty"`
	StakerCount    int        `json:"stakerCount"`
	TVL            *big.Float `json:"tvl"`
	IsActive       bool       `json:"isActive"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// StakingPosition represents a user's staking position
type StakingPosition struct {
	ID              string    `json:"id"`
	Account         string    `json:"account"`
	PoolAddress     string    `json:"poolAddress"`
	StakedAmount    *big.Int  `json:"stakedAmount"`
	ReceiptAmount   *big.Int  `json:"receiptAmount"` // Receipt tokens received
	RewardsEarned   *big.Int  `json:"rewardsEarned"`
	RewardsClaimed  *big.Int  `json:"rewardsClaimed"`
	PendingRewards  *big.Int  `json:"pendingRewards"`
	StakedAt        time.Time `json:"stakedAt"`
	LastClaimedAt   *time.Time `json:"lastClaimedAt,omitempty"`
	CooldownStarted *time.Time `json:"cooldownStarted,omitempty"`
	CooldownAmount  *big.Int  `json:"cooldownAmount,omitempty"`
	UnlockTime      *time.Time `json:"unlockTime,omitempty"`
	IsActive        bool      `json:"isActive"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// StakeEvent represents a stake event
type StakeEvent struct {
	ID            string    `json:"id"`
	TxHash        string    `json:"txHash"`
	BlockNumber   uint64    `json:"blockNumber"`
	LogIndex      uint64    `json:"logIndex"`
	PoolAddress   string    `json:"poolAddress"`
	Account       string    `json:"account"`
	Amount        *big.Int  `json:"amount"`
	ReceiptMinted *big.Int  `json:"receiptMinted,omitempty"` // Receipt tokens minted
	Timestamp     time.Time `json:"timestamp"`
}

// UnstakeEvent represents an unstake event
type UnstakeEvent struct {
	ID             string    `json:"id"`
	TxHash         string    `json:"txHash"`
	BlockNumber    uint64    `json:"blockNumber"`
	LogIndex       uint64    `json:"logIndex"`
	PoolAddress    string    `json:"poolAddress"`
	Account        string    `json:"account"`
	Amount         *big.Int  `json:"amount"`
	ReceiptBurned  *big.Int  `json:"receiptBurned,omitempty"` // Receipt tokens burned
	IsCooldown     bool      `json:"isCooldown"`              // Is this a cooldown initiation?
	Timestamp      time.Time `json:"timestamp"`
}

// RewardEvent represents a reward distribution or claim
type RewardEvent struct {
	ID          string    `json:"id"`
	TxHash      string    `json:"txHash"`
	BlockNumber uint64    `json:"blockNumber"`
	LogIndex    uint64    `json:"logIndex"`
	PoolAddress string    `json:"poolAddress"`
	Account     string    `json:"account,omitempty"` // Empty for pool-wide distributions
	Amount      *big.Int  `json:"amount"`
	EventType   string    `json:"eventType"` // "distribute", "claim"
	Timestamp   time.Time `json:"timestamp"`
}

// NewStakingIndexer creates a new staking indexer
func NewStakingIndexer() *StakingIndexer {
	return &StakingIndexer{
		pools:     make(map[string]*StakingPool),
		stakes:    make([]*StakeEvent, 0),
		unstakes:  make([]*UnstakeEvent, 0),
		rewards:   make([]*RewardEvent, 0),
		positions: make(map[string]*StakingPosition),
	}
}

// SetCallbacks sets event callbacks
func (s *StakingIndexer) SetCallbacks(
	onStake func(*StakeEvent),
	onUnstake func(*UnstakeEvent),
	onReward func(*RewardEvent),
) {
	s.onStake = onStake
	s.onUnstake = onUnstake
	s.onReward = onReward
}

// IndexLog processes a log entry for staking events
func (s *StakingIndexer) IndexLog(log *LogEntry) error {
	if len(log.Topics) == 0 {
		return nil
	}
	
	topic0 := log.Topics[0]
	
	switch topic0 {
	case StakingStakedSig:
		return s.indexStake(log)
	case StakingUnstakedSig:
		return s.indexUnstake(log)
	case StakingRewardsSig:
		return s.indexRewards(log)
	case StakingCooldownSig:
		return s.indexCooldown(log)
	}
	
	return nil
}

// indexStake processes a stake event
func (s *StakingIndexer) indexStake(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid stake event")
	}
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 64 {
		return fmt.Errorf("invalid stake data")
	}
	
	account := topicToAddress(log.Topics[1])
	amount := new(big.Int).SetBytes(data[0:32])
	var receiptMinted *big.Int
	if len(data) >= 64 {
		receiptMinted = new(big.Int).SetBytes(data[32:64])
	}
	
	stake := &StakeEvent{
		ID:            fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:        log.TxHash,
		BlockNumber:   log.BlockNumber,
		LogIndex:      log.LogIndex,
		PoolAddress:   log.Address,
		Account:       account,
		Amount:        amount,
		ReceiptMinted: receiptMinted,
		Timestamp:     log.Timestamp,
	}
	
	s.stakes = append(s.stakes, stake)
	
	// Update position
	pos := s.getOrCreatePosition(log.Address, account)
	pos.StakedAmount = new(big.Int).Add(pos.StakedAmount, amount)
	if receiptMinted != nil {
		pos.ReceiptAmount = new(big.Int).Add(pos.ReceiptAmount, receiptMinted)
	}
	pos.StakedAt = log.Timestamp
	pos.IsActive = true
	pos.UpdatedAt = log.Timestamp
	
	// Update pool
	pool := s.getOrCreatePool(log.Address)
	pool.TotalStaked = new(big.Int).Add(pool.TotalStaked, amount)
	pool.UpdatedAt = log.Timestamp
	
	if s.onStake != nil {
		s.onStake(stake)
	}
	
	return nil
}

// indexUnstake processes an unstake event
func (s *StakingIndexer) indexUnstake(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid unstake event")
	}
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 64 {
		return fmt.Errorf("invalid unstake data")
	}
	
	account := topicToAddress(log.Topics[1])
	receiptBurned := new(big.Int).SetBytes(data[0:32])
	amount := new(big.Int).SetBytes(data[32:64])
	
	unstake := &UnstakeEvent{
		ID:            fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:        log.TxHash,
		BlockNumber:   log.BlockNumber,
		LogIndex:      log.LogIndex,
		PoolAddress:   log.Address,
		Account:       account,
		Amount:        amount,
		ReceiptBurned: receiptBurned,
		Timestamp:     log.Timestamp,
	}
	
	s.unstakes = append(s.unstakes, unstake)
	
	// Update position
	pos := s.getOrCreatePosition(log.Address, account)
	pos.StakedAmount = new(big.Int).Sub(pos.StakedAmount, amount)
	pos.ReceiptAmount = new(big.Int).Sub(pos.ReceiptAmount, receiptBurned)
	pos.UpdatedAt = log.Timestamp
	
	if pos.StakedAmount.Sign() <= 0 {
		pos.IsActive = false
	}
	
	// Update pool
	pool := s.getOrCreatePool(log.Address)
	pool.TotalStaked = new(big.Int).Sub(pool.TotalStaked, amount)
	pool.UpdatedAt = log.Timestamp
	
	if s.onUnstake != nil {
		s.onUnstake(unstake)
	}
	
	return nil
}

// indexRewards processes a reward event
func (s *StakingIndexer) indexRewards(log *LogEntry) error {
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 32 {
		return fmt.Errorf("invalid rewards data")
	}
	
	amount := new(big.Int).SetBytes(data[0:32])
	
	var account string
	eventType := "distribute"
	if len(log.Topics) >= 2 {
		account = topicToAddress(log.Topics[1])
		eventType = "claim"
	}
	
	reward := &RewardEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		PoolAddress: log.Address,
		Account:     account,
		Amount:      amount,
		EventType:   eventType,
		Timestamp:   log.Timestamp,
	}
	
	s.rewards = append(s.rewards, reward)
	
	// Update pool
	pool := s.getOrCreatePool(log.Address)
	pool.TotalRewards = new(big.Int).Add(pool.TotalRewards, amount)
	pool.UpdatedAt = log.Timestamp
	
	// Update position if individual claim
	if account != "" {
		pos := s.getOrCreatePosition(log.Address, account)
		pos.RewardsEarned = new(big.Int).Add(pos.RewardsEarned, amount)
		pos.RewardsClaimed = new(big.Int).Add(pos.RewardsClaimed, amount)
		now := log.Timestamp
		pos.LastClaimedAt = &now
		pos.UpdatedAt = log.Timestamp
	}
	
	if s.onReward != nil {
		s.onReward(reward)
	}
	
	return nil
}

// indexCooldown processes a cooldown initiation event
func (s *StakingIndexer) indexCooldown(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid cooldown event")
	}
	
	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	
	if len(data) < 32 {
		return fmt.Errorf("invalid cooldown data")
	}
	
	account := topicToAddress(log.Topics[1])
	amount := new(big.Int).SetBytes(data[0:32])
	
	// Update position
	pos := s.getOrCreatePosition(log.Address, account)
	now := log.Timestamp
	pos.CooldownStarted = &now
	pos.CooldownAmount = amount
	pos.UpdatedAt = log.Timestamp
	
	// Calculate unlock time based on pool cooldown period
	pool := s.getOrCreatePool(log.Address)
	if pool.CooldownPeriod > 0 {
		unlockTime := now.Add(time.Duration(pool.CooldownPeriod) * time.Second)
		pos.UnlockTime = &unlockTime
	}
	
	// Record as unstake with cooldown flag
	unstake := &UnstakeEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		PoolAddress: log.Address,
		Account:     account,
		Amount:      amount,
		IsCooldown:  true,
		Timestamp:   log.Timestamp,
	}
	
	s.unstakes = append(s.unstakes, unstake)
	
	if s.onUnstake != nil {
		s.onUnstake(unstake)
	}
	
	return nil
}

// getPositionID generates a unique position ID
func (s *StakingIndexer) getPositionID(pool, account string) string {
	return fmt.Sprintf("%s:%s", strings.ToLower(account), strings.ToLower(pool))
}

// getOrCreatePosition gets or creates a position
func (s *StakingIndexer) getOrCreatePosition(pool, account string) *StakingPosition {
	id := s.getPositionID(pool, account)
	if pos, exists := s.positions[id]; exists {
		return pos
	}
	
	pos := &StakingPosition{
		ID:             id,
		Account:        account,
		PoolAddress:    pool,
		StakedAmount:   big.NewInt(0),
		ReceiptAmount:  big.NewInt(0),
		RewardsEarned:  big.NewInt(0),
		RewardsClaimed: big.NewInt(0),
		PendingRewards: big.NewInt(0),
		IsActive:       true,
		UpdatedAt:      time.Now(),
	}
	s.positions[id] = pos
	return pos
}

// getOrCreatePool gets or creates a pool
func (s *StakingIndexer) getOrCreatePool(address string) *StakingPool {
	addr := strings.ToLower(address)
	if pool, exists := s.pools[addr]; exists {
		return pool
	}
	
	pool := &StakingPool{
		Address:      addr,
		TotalStaked:  big.NewInt(0),
		TotalRewards: big.NewInt(0),
		RewardRate:   big.NewInt(0),
		TVL:          big.NewFloat(0),
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	s.pools[addr] = pool
	return pool
}

// GetPool returns a pool by address
func (s *StakingIndexer) GetPool(address string) *StakingPool {
	return s.pools[strings.ToLower(address)]
}

// GetAllPools returns all pools
func (s *StakingIndexer) GetAllPools() []*StakingPool {
	pools := make([]*StakingPool, 0, len(s.pools))
	for _, p := range s.pools {
		pools = append(pools, p)
	}
	return pools
}

// GetPositionsByAccount returns all positions for an account
func (s *StakingIndexer) GetPositionsByAccount(account string) []*StakingPosition {
	var result []*StakingPosition
	acc := strings.ToLower(account)
	for _, pos := range s.positions {
		if strings.ToLower(pos.Account) == acc {
			result = append(result, pos)
		}
	}
	return result
}

// GetActivePositions returns all active positions
func (s *StakingIndexer) GetActivePositions() []*StakingPosition {
	var result []*StakingPosition
	for _, pos := range s.positions {
		if pos.IsActive {
			result = append(result, pos)
		}
	}
	return result
}

// StakingStats represents staking indexer statistics
type StakingStats struct {
	PoolCount         uint64    `json:"poolCount"`
	PositionCount     uint64    `json:"positionCount"`
	ActivePositions   uint64    `json:"activePositions"`
	TotalStaked       *big.Int  `json:"totalStaked"`
	TotalRewards      *big.Int  `json:"totalRewards"`
	TotalStakes       uint64    `json:"totalStakes"`
	TotalUnstakes     uint64    `json:"totalUnstakes"`
	TotalRewardEvents uint64    `json:"totalRewardEvents"`
	LastUpdated       time.Time `json:"lastUpdated"`
}

// GetStats returns aggregate statistics
func (s *StakingIndexer) GetStats() *StakingStats {
	stats := &StakingStats{
		PoolCount:         uint64(len(s.pools)),
		PositionCount:     uint64(len(s.positions)),
		ActivePositions:   uint64(len(s.GetActivePositions())),
		TotalStaked:       big.NewInt(0),
		TotalRewards:      big.NewInt(0),
		TotalStakes:       uint64(len(s.stakes)),
		TotalUnstakes:     uint64(len(s.unstakes)),
		TotalRewardEvents: uint64(len(s.rewards)),
		LastUpdated:       time.Now(),
	}
	
	for _, pool := range s.pools {
		stats.TotalStaked.Add(stats.TotalStaked, pool.TotalStaked)
		stats.TotalRewards.Add(stats.TotalRewards, pool.TotalRewards)
	}
	
	return stats
}
