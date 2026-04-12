package defi

import "math/big"

// Treasury event topic0 signatures (keccak256 hashes).
// Source: contracts/treasury/Vault.sol, Router.sol, FeeGov.sol
const (
	// Vault events
	// Receive(bytes32 indexed chain, uint256 amount, bytes32 warpId)
	TopicVaultReceive = "0x746cac8d52ab53d373654cb1bce8a77880a4fb35eba8502e644d45fb9a3544ad"

	// Flush(bytes32 indexed chain, uint256 amount)
	TopicVaultFlush = "0xe033d0823ab017f8b7b1b1ef1d3581d03692445a7c735ce6a0ff5a08b4da24a1"

	// FeeGov events
	// Rate(uint16 rate, uint32 version)
	TopicFeeGovRate = "0x3b973ff951e1f8921bccbc08ec81caee7ec5ce63a5dd396974dc6722bc3a7a2d"

	// Chain(bytes32 indexed id, bool active)
	TopicFeeGovChain = "0x27f091fa3f90d453118174c469e669f3553b1d54c67aaf94ede31b8f0d04f48f"

	// Broadcast(uint32 version, uint256 count)
	TopicFeeGovBroadcast = "0x4f63312ce57db5d707657e7ad8f4f74d1e3269b1ba60ee968c0c810713123928"

	// Router events
	// Weight(address indexed recipient, uint256 weight)
	TopicRouterWeight = "0xf74e8b7d737d24757358039f81965e4a5b41873d22817a0a93a098bc4bdbb02a"

	// Distribute(uint256 amount)
	TopicRouterDistribute = "0x4def474aca53bf221d07d9ab0f675b3f6d8d2494b8427271bcf43c018ef1eead"

	// Claim(address indexed recipient, uint256 amount)
	TopicRouterClaim = "0x47cee97cb7acd717b3c0aa1435d004cd5b3c8c57d70dbceb4e4458bbd60e39d4"
)

// TreasuryTopics returns all treasury event topic0 strings.
func TreasuryTopics() []string {
	return []string{
		TopicVaultReceive, TopicVaultFlush,
		TopicFeeGovRate, TopicFeeGovChain, TopicFeeGovBroadcast,
		TopicRouterWeight, TopicRouterDistribute, TopicRouterClaim,
	}
}

// VaultEvent represents a fee vault receive/flush event.
type VaultEvent struct {
	Chain    string // bytes32 hex
	Amount   *big.Int
	WarpID   string // bytes32 hex (receive only)
	Event    string // "receive", "flush"
	Contract string
	Block    uint64
	TxHash   string
}

// FeeGovEvent represents a FeeGov parameter change.
type FeeGovEvent struct {
	Rate     uint16
	Version  uint32
	ChainID  string // bytes32 hex
	Active   bool
	Count    *big.Int
	Event    string // "rate", "chain", "broadcast"
	Contract string
	Block    uint64
	TxHash   string
}

// RouterEvent represents a fee distribution event.
type RouterEvent struct {
	Recipient string
	Amount    *big.Int
	Weight    *big.Int
	Event     string // "weight", "distribute", "claim"
	Contract  string
	Block     uint64
	TxHash    string
}

// ParseTreasuryEvents extracts vault, fee governance, and router events from EVM logs.
func ParseTreasuryEvents(logs []Log) ([]VaultEvent, []FeeGovEvent, []RouterEvent) {
	var vaults []VaultEvent
	var feegovs []FeeGovEvent
	var routers []RouterEvent

	for i := range logs {
		l := &logs[i]
		if len(l.Topics) == 0 {
			continue
		}
		switch l.Topics[0] {
		case TopicVaultReceive:
			if e := parseVaultReceive(l); e != nil {
				vaults = append(vaults, *e)
			}
		case TopicVaultFlush:
			if e := parseVaultFlush(l); e != nil {
				vaults = append(vaults, *e)
			}
		case TopicFeeGovRate:
			if e := parseFeeGovRate(l); e != nil {
				feegovs = append(feegovs, *e)
			}
		case TopicFeeGovChain:
			if e := parseFeeGovChain(l); e != nil {
				feegovs = append(feegovs, *e)
			}
		case TopicFeeGovBroadcast:
			if e := parseFeeGovBroadcast(l); e != nil {
				feegovs = append(feegovs, *e)
			}
		case TopicRouterWeight:
			if e := parseRouterWeight(l); e != nil {
				routers = append(routers, *e)
			}
		case TopicRouterDistribute:
			routers = append(routers, RouterEvent{
				Amount:   decodeWord(l.Data, 0),
				Event:    "distribute",
				Contract: l.Address,
				Block:    l.BlockNumber,
				TxHash:   l.TxHash,
			})
		case TopicRouterClaim:
			if e := parseRouterClaim(l); e != nil {
				routers = append(routers, *e)
			}
		}
	}
	return vaults, feegovs, routers
}

func parseVaultReceive(l *Log) *VaultEvent {
	// Receive(bytes32 indexed chain, uint256 amount, bytes32 warpId)
	if len(l.Topics) < 2 {
		return nil
	}
	warpID := decodeWord(l.Data, 1)
	return &VaultEvent{
		Chain:    l.Topics[1],
		Amount:   decodeWord(l.Data, 0),
		WarpID:   wordHex(warpID),
		Event:    "receive",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseVaultFlush(l *Log) *VaultEvent {
	// Flush(bytes32 indexed chain, uint256 amount)
	if len(l.Topics) < 2 {
		return nil
	}
	return &VaultEvent{
		Chain:    l.Topics[1],
		Amount:   decodeWord(l.Data, 0),
		Event:    "flush",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseFeeGovRate(l *Log) *FeeGovEvent {
	// Rate(uint16 rate, uint32 version)
	rate := decodeWord(l.Data, 0)
	ver := decodeWord(l.Data, 1)
	var r uint16
	var v uint32
	if rate != nil && rate.IsUint64() {
		r = uint16(rate.Uint64())
	}
	if ver != nil && ver.IsUint64() {
		v = uint32(ver.Uint64())
	}
	return &FeeGovEvent{
		Rate:     r,
		Version:  v,
		Event:    "rate",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseFeeGovChain(l *Log) *FeeGovEvent {
	// Chain(bytes32 indexed id, bool active)
	if len(l.Topics) < 2 {
		return nil
	}
	active := decodeWord(l.Data, 0)
	return &FeeGovEvent{
		ChainID:  l.Topics[1],
		Active:   active != nil && active.Sign() > 0,
		Event:    "chain",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseFeeGovBroadcast(l *Log) *FeeGovEvent {
	// Broadcast(uint32 version, uint256 count)
	ver := decodeWord(l.Data, 0)
	count := decodeWord(l.Data, 1)
	var v uint32
	if ver != nil && ver.IsUint64() {
		v = uint32(ver.Uint64())
	}
	return &FeeGovEvent{
		Version:  v,
		Count:    count,
		Event:    "broadcast",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseRouterWeight(l *Log) *RouterEvent {
	// Weight(address indexed recipient, uint256 weight)
	if len(l.Topics) < 2 {
		return nil
	}
	return &RouterEvent{
		Recipient: topicAddress(l.Topics[1]),
		Weight:    decodeWord(l.Data, 0),
		Event:     "weight",
		Contract:  l.Address,
		Block:     l.BlockNumber,
		TxHash:    l.TxHash,
	}
}

func parseRouterClaim(l *Log) *RouterEvent {
	// Claim(address indexed recipient, uint256 amount)
	if len(l.Topics) < 2 {
		return nil
	}
	return &RouterEvent{
		Recipient: topicAddress(l.Topics[1]),
		Amount:    decodeWord(l.Data, 0),
		Event:     "claim",
		Contract:  l.Address,
		Block:     l.BlockNumber,
		TxHash:    l.TxHash,
	}
}
