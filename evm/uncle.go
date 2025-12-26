// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package evm

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"
)

// UncleBlock represents an uncle (ommer) block
type UncleBlock struct {
	Hash             string    `json:"hash"`
	Number           uint64    `json:"number"`
	ParentHash       string    `json:"parentHash"`
	Nonce            string    `json:"nonce,omitempty"`
	Sha3Uncles       string    `json:"sha3Uncles"`
	LogsBloom        string    `json:"logsBloom"`
	TransactionsRoot string    `json:"transactionsRoot"`
	StateRoot        string    `json:"stateRoot"`
	ReceiptsRoot     string    `json:"receiptsRoot"`
	Miner            string    `json:"miner"`
	Difficulty       string    `json:"difficulty"`
	TotalDifficulty  string    `json:"totalDifficulty,omitempty"`
	ExtraData        string    `json:"extraData"`
	Size             uint64    `json:"size"`
	GasLimit         uint64    `json:"gasLimit"`
	GasUsed          uint64    `json:"gasUsed"`
	Timestamp        time.Time `json:"timestamp"`

	// Nephew block info (the block that included this uncle)
	NephewHash   string `json:"nephewHash"`
	NephewNumber uint64 `json:"nephewNumber"`
	UncleIndex   int    `json:"uncleIndex"`

	// Reward info
	MinerReward string `json:"minerReward"`
	UncleReward string `json:"uncleReward"`

	// Indexing metadata
	IndexedAt time.Time `json:"indexedAt"`
}

// UncleReward represents uncle mining reward calculation
type UncleReward struct {
	UncleBlockNumber  uint64   `json:"uncleBlockNumber"`
	NephewBlockNumber uint64   `json:"nephewBlockNumber"`
	UncleReward       *big.Int `json:"uncleReward"`  // Reward to uncle miner
	NephewReward      *big.Int `json:"nephewReward"` // Additional reward to nephew miner
}

// UncleIndexer indexes uncle blocks
type UncleIndexer struct {
	rpcURL      string
	blockReward *big.Int // Base block reward for the chain
}

// NewUncleIndexer creates a new uncle block indexer
func NewUncleIndexer(rpcURL string, blockReward *big.Int) *UncleIndexer {
	if blockReward == nil {
		// Default to 2 ETH (Ethereum post-Constantinople)
		blockReward = big.NewInt(2e18)
	}
	return &UncleIndexer{
		rpcURL:      rpcURL,
		blockReward: blockReward,
	}
}

// GetUncleCount returns the number of uncles in a block
func (u *UncleIndexer) GetUncleCount(ctx context.Context, blockNumberOrHash string) (int, error) {
	var method string
	var params []interface{}

	if len(blockNumberOrHash) == 66 { // Hash
		method = "eth_getUncleCountByBlockHash"
		params = []interface{}{blockNumberOrHash}
	} else {
		method = "eth_getUncleCountByBlockNumber"
		params = []interface{}{blockNumberOrHash}
	}

	result, err := u.rpcCall(ctx, method, params)
	if err != nil {
		return 0, err
	}

	var countHex string
	if err := json.Unmarshal(result, &countHex); err != nil {
		return 0, err
	}

	count, _ := parseHexUint64(countHex)
	return int(count), nil
}

// GetUncle returns an uncle block by block hash/number and uncle index
func (u *UncleIndexer) GetUncle(ctx context.Context, blockNumberOrHash string, index int) (*UncleBlock, error) {
	var method string
	var params []interface{}

	indexHex := fmt.Sprintf("0x%x", index)

	if len(blockNumberOrHash) == 66 { // Hash
		method = "eth_getUncleByBlockHashAndIndex"
		params = []interface{}{blockNumberOrHash, indexHex}
	} else {
		method = "eth_getUncleByBlockNumberAndIndex"
		params = []interface{}{blockNumberOrHash, indexHex}
	}

	result, err := u.rpcCall(ctx, method, params)
	if err != nil {
		return nil, err
	}

	if len(result) == 0 || string(result) == "null" {
		return nil, nil
	}

	var uncle UncleBlock
	if err := json.Unmarshal(result, &uncle); err != nil {
		return nil, fmt.Errorf("failed to unmarshal uncle: %w", err)
	}

	uncle.UncleIndex = index
	uncle.IndexedAt = time.Now()

	return &uncle, nil
}

// GetAllUncles returns all uncle blocks for a given block
func (u *UncleIndexer) GetAllUncles(ctx context.Context, blockNumber uint64, blockHash string) ([]*UncleBlock, error) {
	blockNumHex := fmt.Sprintf("0x%x", blockNumber)

	count, err := u.GetUncleCount(ctx, blockNumHex)
	if err != nil {
		return nil, err
	}

	if count == 0 {
		return nil, nil
	}

	uncles := make([]*UncleBlock, 0, count)
	for i := 0; i < count; i++ {
		uncle, err := u.GetUncle(ctx, blockNumHex, i)
		if err != nil {
			return nil, err
		}
		if uncle != nil {
			uncle.NephewHash = blockHash
			uncle.NephewNumber = blockNumber

			// Calculate rewards
			reward := u.CalculateUncleReward(uncle.Number, blockNumber)
			uncle.UncleReward = reward.UncleReward.String()

			uncles = append(uncles, uncle)
		}
	}

	return uncles, nil
}

// CalculateUncleReward calculates the reward for an uncle block
// Based on Ethereum's uncle reward formula:
// Uncle reward = (uncle_number + 8 - nephew_number) / 8 * block_reward
// Nephew reward = block_reward / 32 per uncle included
func (u *UncleIndexer) CalculateUncleReward(uncleNumber, nephewNumber uint64) *UncleReward {
	// Uncle reward: (8 + uncleNumber - nephewNumber) / 8 * blockReward
	// Valid uncle numbers: nephewNumber - 7 to nephewNumber - 1

	diff := int64(nephewNumber) - int64(uncleNumber)
	if diff < 1 || diff > 7 {
		// Invalid uncle (too old or not old enough)
		return &UncleReward{
			UncleBlockNumber:  uncleNumber,
			NephewBlockNumber: nephewNumber,
			UncleReward:       big.NewInt(0),
			NephewReward:      big.NewInt(0),
		}
	}

	// Uncle reward = (8 - diff) / 8 * blockReward
	numerator := big.NewInt(8 - diff)
	uncleReward := new(big.Int).Mul(u.blockReward, numerator)
	uncleReward.Div(uncleReward, big.NewInt(8))

	// Nephew reward = blockReward / 32
	nephewReward := new(big.Int).Div(u.blockReward, big.NewInt(32))

	return &UncleReward{
		UncleBlockNumber:  uncleNumber,
		NephewBlockNumber: nephewNumber,
		UncleReward:       uncleReward,
		NephewReward:      nephewReward,
	}
}

// IndexBlockUncles indexes all uncles for a block and returns the results
func (u *UncleIndexer) IndexBlockUncles(ctx context.Context, block *Block) ([]*UncleBlock, error) {
	if block == nil {
		return nil, fmt.Errorf("block is nil")
	}

	return u.GetAllUncles(ctx, block.Number, block.Hash)
}

// UncleStats represents uncle statistics for a range of blocks
type UncleStats struct {
	BlockRange       [2]uint64 `json:"blockRange"`
	TotalBlocks      int       `json:"totalBlocks"`
	BlocksWithUncles int       `json:"blocksWithUncles"`
	TotalUncles      int       `json:"totalUncles"`
	MaxUnclesInBlock int       `json:"maxUnclesInBlock"`
	UniqueMiners     int       `json:"uniqueMiners"`
	TotalUncleReward *big.Int  `json:"totalUncleReward"`
	AvgUncleDepth    float64   `json:"avgUncleDepth"` // Average (nephew - uncle) depth
}

// GetUncleStats calculates uncle statistics for a block range
func (u *UncleIndexer) GetUncleStats(ctx context.Context, fromBlock, toBlock uint64) (*UncleStats, error) {
	stats := &UncleStats{
		BlockRange:       [2]uint64{fromBlock, toBlock},
		TotalBlocks:      int(toBlock - fromBlock + 1),
		TotalUncleReward: big.NewInt(0),
	}

	miners := make(map[string]bool)
	totalDepth := 0

	for blockNum := fromBlock; blockNum <= toBlock; blockNum++ {
		blockNumHex := fmt.Sprintf("0x%x", blockNum)
		count, err := u.GetUncleCount(ctx, blockNumHex)
		if err != nil {
			continue
		}

		if count > 0 {
			stats.BlocksWithUncles++
			stats.TotalUncles += count
			if count > stats.MaxUnclesInBlock {
				stats.MaxUnclesInBlock = count
			}

			// Get uncle details
			for i := 0; i < count; i++ {
				uncle, err := u.GetUncle(ctx, blockNumHex, i)
				if err != nil || uncle == nil {
					continue
				}

				miners[uncle.Miner] = true

				// Calculate depth
				depth := int(blockNum) - int(uncle.Number)
				totalDepth += depth

				// Calculate reward
				reward := u.CalculateUncleReward(uncle.Number, blockNum)
				stats.TotalUncleReward.Add(stats.TotalUncleReward, reward.UncleReward)
			}
		}
	}

	stats.UniqueMiners = len(miners)
	if stats.TotalUncles > 0 {
		stats.AvgUncleDepth = float64(totalDepth) / float64(stats.TotalUncles)
	}

	return stats, nil
}

// rpcCall makes a JSON-RPC call
func (u *UncleIndexer) rpcCall(ctx context.Context, method string, params []interface{}) (json.RawMessage, error) {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := newHTTPRequest(ctx, "POST", u.rpcURL, body)
	if err != nil {
		return nil, err
	}

	resp, err := doHTTPRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, err
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// parseHexUint64 parses a hex string to uint64
func parseHexUint64(s string) (uint64, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid hex string")
	}
	if s[:2] == "0x" || s[:2] == "0X" {
		s = s[2:]
	}

	var result uint64
	for _, c := range s {
		result *= 16
		switch {
		case c >= '0' && c <= '9':
			result += uint64(c - '0')
		case c >= 'a' && c <= 'f':
			result += uint64(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			result += uint64(c - 'A' + 10)
		default:
			return 0, fmt.Errorf("invalid hex character: %c", c)
		}
	}

	return result, nil
}
