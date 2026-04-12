package defi

import "math/big"

// Liquid staking event topic0 signatures (keccak256 hashes).
// Source: contracts/liquid/LiquidLUX.sol
const (
	// FeesReceived(address indexed from, uint256 amount, bytes32 indexed feeType, uint256 perfFee, uint256 toReserve)
	TopicFeesReceived = "0xffe15e5c651accdc94c3387fa6d493291053ff174cd74f8c8b7f8a820e5dd909"

	// ValidatorRewardsReceived(address indexed from, uint256 amount)
	TopicValidatorRewardsReceived = "0x9d77a73cf83a1a8caf4495066223f6e1c6758b9b03f54ea006254cca0108e27c"

	// SlashingApplied(uint256 amount, uint256 fromReserve, uint256 socialized)
	TopicSlashingApplied = "0xe0ce9feb262640cffce27987d6007683502f8828357b680a1d2a0902ff5f4c1c"

	// EmergencyWithdrawal(address indexed to, uint256 amount)
	TopicEmergencyWithdrawal = "0x23d6711a1d031134a36921253c75aa59e967d38e369ac625992824315e204f20"
)

// LiquidTopics returns all liquid staking event topic0 strings.
func LiquidTopics() []string {
	return []string{
		TopicFeesReceived,
		TopicValidatorRewardsReceived,
		TopicSlashingApplied,
		TopicEmergencyWithdrawal,
	}
}

// LiquidEvent represents a LiquidLUX vault event.
type LiquidEvent struct {
	From       string
	Amount     *big.Int
	FeeType    string // bytes32 hex (fees_received only)
	PerfFee    *big.Int
	ToReserve  *big.Int
	FromReserve *big.Int
	Socialized *big.Int
	Event      string // "fees_received", "validator_rewards", "slashing", "emergency_withdrawal"
	Contract   string
	Block      uint64
	TxHash     string
}

// ParseLiquidEvents extracts LiquidLUX vault events from EVM logs.
func ParseLiquidEvents(logs []Log) []LiquidEvent {
	var events []LiquidEvent

	for i := range logs {
		l := &logs[i]
		if len(l.Topics) == 0 {
			continue
		}
		switch l.Topics[0] {
		case TopicFeesReceived:
			if e := parseFeesReceived(l); e != nil {
				events = append(events, *e)
			}
		case TopicValidatorRewardsReceived:
			if e := parseValidatorRewards(l); e != nil {
				events = append(events, *e)
			}
		case TopicSlashingApplied:
			events = append(events, LiquidEvent{
				Amount:      decodeWord(l.Data, 0),
				FromReserve: decodeWord(l.Data, 1),
				Socialized:  decodeWord(l.Data, 2),
				Event:       "slashing",
				Contract:    l.Address,
				Block:       l.BlockNumber,
				TxHash:      l.TxHash,
			})
		case TopicEmergencyWithdrawal:
			if e := parseEmergencyWithdrawal(l); e != nil {
				events = append(events, *e)
			}
		}
	}
	return events
}

func parseFeesReceived(l *Log) *LiquidEvent {
	// FeesReceived(address indexed from, uint256 amount, bytes32 indexed feeType, uint256 perfFee, uint256 toReserve)
	if len(l.Topics) < 3 {
		return nil
	}
	return &LiquidEvent{
		From:      topicAddress(l.Topics[1]),
		Amount:    decodeWord(l.Data, 0),
		FeeType:   l.Topics[2],
		PerfFee:   decodeWord(l.Data, 1),
		ToReserve: decodeWord(l.Data, 2),
		Event:     "fees_received",
		Contract:  l.Address,
		Block:     l.BlockNumber,
		TxHash:    l.TxHash,
	}
}

func parseValidatorRewards(l *Log) *LiquidEvent {
	// ValidatorRewardsReceived(address indexed from, uint256 amount)
	if len(l.Topics) < 2 {
		return nil
	}
	return &LiquidEvent{
		From:     topicAddress(l.Topics[1]),
		Amount:   decodeWord(l.Data, 0),
		Event:    "validator_rewards",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseEmergencyWithdrawal(l *Log) *LiquidEvent {
	// EmergencyWithdrawal(address indexed to, uint256 amount)
	if len(l.Topics) < 2 {
		return nil
	}
	return &LiquidEvent{
		From:     topicAddress(l.Topics[1]),
		Amount:   decodeWord(l.Data, 0),
		Event:    "emergency_withdrawal",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}
