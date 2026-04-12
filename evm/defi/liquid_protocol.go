// Package defi provides EVM log parsers for DeFi protocol events.
package defi

import (
	"math/big"
	"strings"
)

// Liquid Protocol event topic0 signatures (keccak256 hashes).
//
// Source contracts:
//   - contracts/liquid/LiquidLUX.sol
//   - contracts/liquid/LiquidToken.sol
//   - contracts/liquid/teleport/Teleporter.sol
//   - contracts/liquid/teleport/LiquidVault.sol
//   - contracts/liquid/teleport/LiquidETH.sol
//   - contracts/liquid/teleport/LiquidYield.sol
const (
	// FeesReceived(address indexed from, uint256 amount, bytes32 indexed feeType, uint256 perfFee, uint256 toReserve)
	TopicLiquidFeesReceived = "0x8a4c1aaf4802c24ee698876096626546fc0657965c75455c9a40cdee145add76"

	// ValidatorRewardsReceived(address indexed from, uint256 amount)
	TopicLiquidValidatorRewards = "0x9628da9eedd404dd6aa64f8f0d884912d95253b1463bcd9b6c80a9726264fe2c"

	// SlashingApplied(uint256 amount, uint256 fromReserve, uint256 socialized)
	TopicLiquidSlashing = "0x7623770ff9ea6e6490827b5484bd12de34cfc6caa6a17665a92079f4420cf379"

	// EmergencyWithdrawal(address indexed to, uint256 amount)
	TopicLiquidEmergencyWithdrawal = "0x24775ff1a205291a1bafeb2eb6078c5f7792cbeff59bf0d2907a4f9f9384301a"

	// Paused(address indexed minter, bool state)
	TopicLiquidTokenPaused = "0x7475042b4eedd80f6a2cf1a3de28a23666d83decd2c12609cb85f2488b1aba16"

	// Whitelisted(address indexed minter, bool state)
	TopicLiquidTokenWhitelisted = "0xf0f623fe8b827ffe91ab47855396e5dbe9540e4dd9d96fd4eddafc83faa11a9b"

	// SetFlashMintFee(uint256 fee)
	TopicLiquidSetFlashMintFee = "0xcd33f3992018d16df5cc95c6b6cb36f796cad02acdd79bc12d657ae2dcc4bf88"

	// DepositMinted(uint256 indexed srcChainId, uint256 indexed depositNonce, address indexed recipient, uint256 amount)
	TopicDepositMinted = "0x7640b9ec41591de9075057df5784ffe664325b2870f648afc4199e6614aba3b2"

	// YieldMinted(uint256 indexed srcChainId, uint256 indexed yieldNonce, uint256 amount)
	TopicYieldMinted = "0x40206e6b5c5789d92bb88d51174796aa27dab87eac146bd6ab2c570732683786"

	// BurnedForWithdraw(address indexed user, uint256 amount, uint256 indexed withdrawNonce)
	TopicBurnedForWithdraw = "0xc6e2ce5d105ce7f7e84a11f43fb3f0e62090a93ae6b372e5c42cf1dc43c9b70c"

	// BackingUpdated(uint256 indexed srcChainId, uint256 totalBacking, uint256 timestamp)
	TopicBackingUpdated = "0x83021b1c38ca096b7e00004f578cfabba5e69420bd19388405d76d2b9b889f89"

	// MPCOracleSet(address indexed oracle, bool active)
	TopicMPCOracleSet = "0xd6b2a68eab9c4a75d60b78ac964e20c494f540a91e68e6017bb2597c0e2b9373"

	// StrategyAllocated(uint256 indexed strategyIndex, uint256 amount)
	TopicStrategyAllocated = "0xcde06e78876607b5c7b03476ffce8a74e096eb12eb92093c610f9ae9854a51c1"

	// StrategyDeallocated(uint256 indexed strategyIndex, uint256 amount)
	TopicStrategyDeallocated = "0x2c446e0678ec7d7bf8fc776f6f86c3318770c49010806f28de3ea82159e01f3d"

	// YieldHarvested(uint256 indexed yieldNonce, uint256 totalYield, uint256 timestamp)
	TopicYieldHarvested = "0x0ba95b2ee52f0f864ed1b63ea0539a8de36c5285f49b3d314804a63a3fe48d80"

	// StrategyAdded(uint256 indexed index, address adapter)
	TopicStrategyAdded = "0x0a1cb9d7bfe72a48c0c9f94cb6744219bad2d83009022e650686349312d19507"

	// StrategyRemoved(uint256 indexed index)
	TopicStrategyRemoved = "0x9b15a1049f2049c598d3869a961942b6762950045c68926e487c8a27e7a918cd"
)

// LiquidProtocolTopics returns all Liquid Protocol event topic0 strings.
func LiquidProtocolTopics() []string {
	return []string{
		TopicLiquidFeesReceived,
		TopicLiquidValidatorRewards,
		TopicLiquidSlashing,
		TopicLiquidEmergencyWithdrawal,
		TopicLiquidTokenPaused,
		TopicLiquidTokenWhitelisted,
		TopicLiquidSetFlashMintFee,
		TopicDepositMinted,
		TopicYieldMinted,
		TopicBurnedForWithdraw,
		TopicBackingUpdated,
		TopicMPCOracleSet,
		TopicStrategyAllocated,
		TopicStrategyDeallocated,
		TopicYieldHarvested,
		TopicStrategyAdded,
		TopicStrategyRemoved,
	}
}

// LiquidProtocolEvent represents a parsed Liquid Protocol event.
type LiquidProtocolEvent struct {
	Event    string // event name
	Contract string
	Block    uint64
	TxHash   string

	// Common
	From   string
	Amount *big.Int

	// FeesReceived
	FeeType   string // bytes32 hex
	PerfFee   *big.Int
	ToReserve *big.Int

	// Slashing
	FromReserve *big.Int
	Socialized  *big.Int

	// LiquidToken
	Minter string
	State  bool
	Fee    *big.Int

	// Teleporter
	SrcChainID    *big.Int
	DepositNonce  *big.Int
	Recipient     string
	YieldNonce    *big.Int
	WithdrawNonce *big.Int
	TotalBacking  *big.Int
	Timestamp     *big.Int
	Oracle        string
	Active        bool

	// LiquidVault strategies
	StrategyIndex *big.Int
	Adapter       string
	TotalYield    *big.Int
}

// ParseLiquidProtocolEvents extracts Liquid Protocol events from EVM logs.
func ParseLiquidProtocolEvents(logs []Log) []LiquidProtocolEvent {
	var events []LiquidProtocolEvent
	for i := range logs {
		l := &logs[i]
		if len(l.Topics) == 0 {
			continue
		}
		switch l.Topics[0] {
		case TopicLiquidFeesReceived:
			if e := parseLiquidFeesReceived(l); e != nil {
				events = append(events, *e)
			}
		case TopicLiquidValidatorRewards:
			if e := parseLiquidValidatorRewards(l); e != nil {
				events = append(events, *e)
			}
		case TopicLiquidSlashing:
			if e := parseLiquidSlashing(l); e != nil {
				events = append(events, *e)
			}
		case TopicLiquidEmergencyWithdrawal:
			if e := parseLiquidEmergencyWithdrawal(l); e != nil {
				events = append(events, *e)
			}
		case TopicLiquidTokenPaused:
			if e := parseLiquidTokenPaused(l); e != nil {
				events = append(events, *e)
			}
		case TopicLiquidTokenWhitelisted:
			if e := parseLiquidTokenWhitelisted(l); e != nil {
				events = append(events, *e)
			}
		case TopicLiquidSetFlashMintFee:
			if e := parseLiquidSetFlashMintFee(l); e != nil {
				events = append(events, *e)
			}
		case TopicDepositMinted:
			if e := parseDepositMinted(l); e != nil {
				events = append(events, *e)
			}
		case TopicYieldMinted:
			if e := parseYieldMinted(l); e != nil {
				events = append(events, *e)
			}
		case TopicBurnedForWithdraw:
			if e := parseBurnedForWithdraw(l); e != nil {
				events = append(events, *e)
			}
		case TopicBackingUpdated:
			if e := parseBackingUpdated(l); e != nil {
				events = append(events, *e)
			}
		case TopicMPCOracleSet:
			if e := parseMPCOracleSet(l); e != nil {
				events = append(events, *e)
			}
		case TopicStrategyAllocated:
			if e := parseStrategyAllocated(l); e != nil {
				events = append(events, *e)
			}
		case TopicStrategyDeallocated:
			if e := parseStrategyDeallocated(l); e != nil {
				events = append(events, *e)
			}
		case TopicYieldHarvested:
			if e := parseYieldHarvested(l); e != nil {
				events = append(events, *e)
			}
		case TopicStrategyAdded:
			if e := parseStrategyAdded(l); e != nil {
				events = append(events, *e)
			}
		case TopicStrategyRemoved:
			events = append(events, LiquidProtocolEvent{
				Event:         "strategy_removed",
				Contract:      l.Address,
				Block:         l.BlockNumber,
				TxHash:        l.TxHash,
				StrategyIndex: topicBigInt(l.Topics[1]),
			})
		}
	}
	return events
}

// FeesReceived(address indexed from, uint256 amount, bytes32 indexed feeType, uint256 perfFee, uint256 toReserve)
func parseLiquidFeesReceived(l *Log) *LiquidProtocolEvent {
	if len(l.Topics) < 3 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	if len(d) < 192 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:     "fees_received",
		Contract:  l.Address,
		Block:     l.BlockNumber,
		TxHash:    l.TxHash,
		From:      topicAddress(l.Topics[1]),
		Amount:    decodeWordStr(d, 0),
		FeeType:   l.Topics[2],
		PerfFee:   decodeWordStr(d, 1),
		ToReserve: decodeWordStr(d, 2),
	}
}

// ValidatorRewardsReceived(address indexed from, uint256 amount)
func parseLiquidValidatorRewards(l *Log) *LiquidProtocolEvent {
	if len(l.Topics) < 2 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	if len(d) < 64 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:    "validator_rewards",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
		From:     topicAddress(l.Topics[1]),
		Amount:   decodeWordStr(d, 0),
	}
}

// SlashingApplied(uint256 amount, uint256 fromReserve, uint256 socialized)
func parseLiquidSlashing(l *Log) *LiquidProtocolEvent {
	d := stripHexPrefix(l.Data)
	if len(d) < 192 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:       "slashing",
		Contract:    l.Address,
		Block:       l.BlockNumber,
		TxHash:      l.TxHash,
		Amount:      decodeWordStr(d, 0),
		FromReserve: decodeWordStr(d, 1),
		Socialized:  decodeWordStr(d, 2),
	}
}

// EmergencyWithdrawal(address indexed to, uint256 amount)
func parseLiquidEmergencyWithdrawal(l *Log) *LiquidProtocolEvent {
	if len(l.Topics) < 2 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	if len(d) < 64 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:    "emergency_withdrawal",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
		From:     topicAddress(l.Topics[1]),
		Amount:   decodeWordStr(d, 0),
	}
}

// Paused(address indexed minter, bool state)
func parseLiquidTokenPaused(l *Log) *LiquidProtocolEvent {
	if len(l.Topics) < 2 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	if len(d) < 64 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:    "paused",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
		Minter:   topicAddress(l.Topics[1]),
		State:    decodeWordStr(d, 0).Sign() > 0,
	}
}

// Whitelisted(address indexed minter, bool state)
func parseLiquidTokenWhitelisted(l *Log) *LiquidProtocolEvent {
	if len(l.Topics) < 2 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	if len(d) < 64 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:    "whitelisted",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
		Minter:   topicAddress(l.Topics[1]),
		State:    decodeWordStr(d, 0).Sign() > 0,
	}
}

// SetFlashMintFee(uint256 fee)
func parseLiquidSetFlashMintFee(l *Log) *LiquidProtocolEvent {
	d := stripHexPrefix(l.Data)
	if len(d) < 64 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:    "set_flash_mint_fee",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
		Fee:      decodeWordStr(d, 0),
	}
}

// DepositMinted(uint256 indexed srcChainId, uint256 indexed depositNonce, address indexed recipient, uint256 amount)
func parseDepositMinted(l *Log) *LiquidProtocolEvent {
	if len(l.Topics) < 4 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	if len(d) < 64 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:        "deposit_minted",
		Contract:     l.Address,
		Block:        l.BlockNumber,
		TxHash:       l.TxHash,
		SrcChainID:   topicBigInt(l.Topics[1]),
		DepositNonce: topicBigInt(l.Topics[2]),
		Recipient:    topicAddress(l.Topics[3]),
		Amount:       decodeWordStr(d, 0),
	}
}

// YieldMinted(uint256 indexed srcChainId, uint256 indexed yieldNonce, uint256 amount)
func parseYieldMinted(l *Log) *LiquidProtocolEvent {
	if len(l.Topics) < 3 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	if len(d) < 64 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:      "yield_minted",
		Contract:   l.Address,
		Block:      l.BlockNumber,
		TxHash:     l.TxHash,
		SrcChainID: topicBigInt(l.Topics[1]),
		YieldNonce: topicBigInt(l.Topics[2]),
		Amount:     decodeWordStr(d, 0),
	}
}

// BurnedForWithdraw(address indexed user, uint256 amount, uint256 indexed withdrawNonce)
func parseBurnedForWithdraw(l *Log) *LiquidProtocolEvent {
	if len(l.Topics) < 3 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	if len(d) < 64 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:         "burned_for_withdraw",
		Contract:      l.Address,
		Block:         l.BlockNumber,
		TxHash:        l.TxHash,
		From:          topicAddress(l.Topics[1]),
		Amount:        decodeWordStr(d, 0),
		WithdrawNonce: topicBigInt(l.Topics[2]),
	}
}

// BackingUpdated(uint256 indexed srcChainId, uint256 totalBacking, uint256 timestamp)
func parseBackingUpdated(l *Log) *LiquidProtocolEvent {
	if len(l.Topics) < 2 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	if len(d) < 128 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:        "backing_updated",
		Contract:     l.Address,
		Block:        l.BlockNumber,
		TxHash:       l.TxHash,
		SrcChainID:   topicBigInt(l.Topics[1]),
		TotalBacking: decodeWordStr(d, 0),
		Timestamp:    decodeWordStr(d, 1),
	}
}

// MPCOracleSet(address indexed oracle, bool active)
func parseMPCOracleSet(l *Log) *LiquidProtocolEvent {
	if len(l.Topics) < 2 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	if len(d) < 64 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:    "mpc_oracle_set",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
		Oracle:   topicAddress(l.Topics[1]),
		Active:   decodeWordStr(d, 0).Sign() > 0,
	}
}

// StrategyAllocated(uint256 indexed strategyIndex, uint256 amount)
func parseStrategyAllocated(l *Log) *LiquidProtocolEvent {
	if len(l.Topics) < 2 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	if len(d) < 64 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:         "strategy_allocated",
		Contract:      l.Address,
		Block:         l.BlockNumber,
		TxHash:        l.TxHash,
		StrategyIndex: topicBigInt(l.Topics[1]),
		Amount:        decodeWordStr(d, 0),
	}
}

// StrategyDeallocated(uint256 indexed strategyIndex, uint256 amount)
func parseStrategyDeallocated(l *Log) *LiquidProtocolEvent {
	if len(l.Topics) < 2 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	if len(d) < 64 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:         "strategy_deallocated",
		Contract:      l.Address,
		Block:         l.BlockNumber,
		TxHash:        l.TxHash,
		StrategyIndex: topicBigInt(l.Topics[1]),
		Amount:        decodeWordStr(d, 0),
	}
}

// YieldHarvested(uint256 indexed yieldNonce, uint256 totalYield, uint256 timestamp)
func parseYieldHarvested(l *Log) *LiquidProtocolEvent {
	if len(l.Topics) < 2 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	if len(d) < 128 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:      "yield_harvested",
		Contract:   l.Address,
		Block:      l.BlockNumber,
		TxHash:     l.TxHash,
		YieldNonce: topicBigInt(l.Topics[1]),
		TotalYield: decodeWordStr(d, 0),
		Timestamp:  decodeWordStr(d, 1),
	}
}

// StrategyAdded(uint256 indexed index, address adapter)
func parseStrategyAdded(l *Log) *LiquidProtocolEvent {
	if len(l.Topics) < 2 {
		return nil
	}
	d := stripHexPrefix(l.Data)
	if len(d) < 64 {
		return nil
	}
	return &LiquidProtocolEvent{
		Event:         "strategy_added",
		Contract:      l.Address,
		Block:         l.BlockNumber,
		TxHash:        l.TxHash,
		StrategyIndex: topicBigInt(l.Topics[1]),
		Adapter:       wordAddress(d, 0),
	}
}

// topicBigInt parses a topic hex string to *big.Int.
func topicBigInt(topic string) *big.Int {
	v := new(big.Int)
	hex := strings.TrimPrefix(topic, "0x")
	v.SetString(hex, 16)
	return v
}
