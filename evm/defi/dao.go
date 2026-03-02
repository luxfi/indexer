package defi

import "math/big"

// DAO event topic0 signatures (keccak256 hashes).
// Source: contracts/governance/GaugeController.sol, vLUX.sol, Karma.sol, DLUX.sol
const (
	// GaugeController events
	// GaugeAdded(uint256 indexed gaugeId, address recipient, string name)
	TopicGaugeAdded = "0x0690c0953072daf431d5cdec64e67412376badca0df468b950c61659830da131"

	// GaugeUpdated(uint256 indexed gaugeId, bool active)
	TopicGaugeUpdated = "0x1e2a3ff4241a28030cd83b954fb7bb7843a2555c29dc5b18f269308414d3bfb8"

	// VoteCast(address indexed user, uint256 indexed gaugeId, uint256 weight) — gauge votes
	TopicGaugeVoteCast = "0xb4cfecf70861b7b150d8337780d34fb4cbc2114b5fb1fe51a5c5fca1849f7274"

	// WeightsUpdated(uint256 timestamp)
	TopicWeightsUpdated = "0x90dd5985ed06249c1f4c7a5a37394070d774f2bc35c0bea714388d778c8f44c2"

	// vLUX events
	// Deposit(address indexed user, uint256 amount, uint256 lockTime, uint256 indexed lockEnd, uint256 ts)
	TopicVLUXDeposit = "0x7162984403f6c73c8639375d45a9187dfd04602231bd8e587c415718b5f7e5f9"

	// Withdraw(address indexed user, uint256 amount, uint256 ts)
	TopicVLUXWithdraw = "0xf279e6a1f5e320cca91135676d9cb6e44ca8a08c0b88342bcdb1144f6511b568"

	// Supply(uint256 prevSupply, uint256 newSupply)
	TopicVLUXSupply = "0x5e2aa66efd74cce82b21852e317e5490d9ecc9e6bb953ae24d90851258cc2f5c"

	// Karma events
	// KarmaMinted(address indexed to, uint256 amount, bytes32 reason)
	TopicKarmaMinted = "0x75a0780e98a4894b81145ffa61783249374538e763a84a56f131f73828eef94c"

	// KarmaSlashed(address indexed from, uint256 amount, bytes32 reason)
	TopicKarmaSlashed = "0x3578cfe093e136b6c466311ff49a5625dff510e16844bed0f2731bc79bd0776d"

	// KarmaDecayed(address indexed account, uint256 amount, bool wasActive)
	TopicKarmaDecayed = "0x5a32bf6cb8d08258f53813b731b066fd8804a627f0f0e0d32b898895841cbc6a"

	// DIDLinked(address indexed account, bytes32 indexed did)
	TopicDIDLinked = "0xabdbb33c848055c04a7674a4d96d0173f89f9f7718cd5426e7bcd81b4596182e"

	// Verified(address indexed account, bool status)
	TopicVerified = "0x04881682880396c5d7f330e192fcd4f8b3d4645842908712b7a005170b3120cc"

	// DLUX events
	// Staked(address indexed user, uint256 amount, uint8 tier, uint256 lockEnd)
	TopicDLUXStaked = "0xbde7f0ba1630d25515c7ab99ba47d5640b7ffb4c673b2a5464ae679195589298"

	// Unstaked(address indexed user, uint256 amount, uint256 luxReturned)
	TopicDLUXUnstaked = "0x7fc4727e062e336010f2c282598ef5f14facb3de68cf8195c2f23e1454b2b74e"

	// Rebased(uint256 epoch, uint256 totalRebased, uint256 rate)
	TopicRebased = "0xd1a8a452d776b1b6802824ca2e8489c6448e2cb0963f552a9a19ab4ae064ca58"

	// DemurrageApplied(address indexed account, uint256 burned)
	TopicDemurrageApplied = "0x92dbe9af03191016deca349de5f6439fc2d722818b96f7b02948d780a913e8e5"

	// TierUpgraded(address indexed user, uint8 from, uint8 to)
	TopicTierUpgraded = "0xcbbf00b5f55f1857f9362f521999f79036413e420d1b236d6bc143af30db731a"

	// Minted(address indexed to, uint256 amount, bytes32 indexed reason)
	TopicDLUXMinted = "0xc263b302aec62d29105026245f19e16f8e0137066ccd4a8bd941f716bd4096bb"
)

// DAOTopics returns all DAO-related event topic0 strings.
func DAOTopics() []string {
	return []string{
		TopicGaugeAdded, TopicGaugeUpdated, TopicGaugeVoteCast, TopicWeightsUpdated,
		TopicVLUXDeposit, TopicVLUXWithdraw, TopicVLUXSupply,
		TopicKarmaMinted, TopicKarmaSlashed, TopicKarmaDecayed,
		TopicDIDLinked, TopicVerified,
		TopicDLUXStaked, TopicDLUXUnstaked, TopicRebased,
		TopicDemurrageApplied, TopicTierUpgraded, TopicDLUXMinted,
	}
}

// GaugeEvent represents a GaugeController event.
type GaugeEvent struct {
	GaugeID   *big.Int
	Recipient string
	Name      string
	Active    bool
	Event     string // "added", "updated", "vote", "weights_updated"
	User      string // for vote events
	Weight    *big.Int
	Contract  string
	Block     uint64
	TxHash    string
}

// VLUXEvent represents a vLUX lock/unlock event.
type VLUXEvent struct {
	User     string
	Amount   *big.Int
	LockEnd  *big.Int
	Event    string // "deposit", "withdraw"
	Contract string
	Block    uint64
	TxHash   string
}

// KarmaEvent represents a Karma reputation change.
type KarmaEvent struct {
	Account   string
	Amount    *big.Int
	Reason    string // hex bytes32
	WasActive bool
	Event     string // "minted", "slashed", "decayed", "did_linked", "verified"
	DID       string // hex bytes32 for did_linked
	Contract  string
	Block     uint64
	TxHash    string
}

// DLUXEvent represents a DLUX token event.
type DLUXEvent struct {
	User     string
	Amount   *big.Int
	Tier     uint8
	LockEnd  *big.Int
	Epoch    *big.Int
	Rate     *big.Int
	Reason   string // hex bytes32
	Event    string // "staked", "unstaked", "rebased", "demurrage", "tier_upgraded", "minted"
	Contract string
	Block    uint64
	TxHash   string
}

// ParseDAOEvents extracts gauge, vLUX, Karma, and DLUX events from EVM logs.
func ParseDAOEvents(logs []Log) ([]GaugeEvent, []VLUXEvent, []KarmaEvent, []DLUXEvent) {
	var gauges []GaugeEvent
	var vlux []VLUXEvent
	var karma []KarmaEvent
	var dlux []DLUXEvent

	for i := range logs {
		l := &logs[i]
		if len(l.Topics) == 0 {
			continue
		}
		switch l.Topics[0] {
		// Gauge
		case TopicGaugeAdded:
			if e := parseGaugeAdded(l); e != nil {
				gauges = append(gauges, *e)
			}
		case TopicGaugeUpdated:
			if e := parseGaugeUpdated(l); e != nil {
				gauges = append(gauges, *e)
			}
		case TopicGaugeVoteCast:
			if e := parseGaugeVote(l); e != nil {
				gauges = append(gauges, *e)
			}
		case TopicWeightsUpdated:
			gauges = append(gauges, GaugeEvent{
				Event: "weights_updated", Contract: l.Address,
				Block: l.BlockNumber, TxHash: l.TxHash,
			})

		// vLUX
		case TopicVLUXDeposit:
			if e := parseVLUXDeposit(l); e != nil {
				vlux = append(vlux, *e)
			}
		case TopicVLUXWithdraw:
			if e := parseVLUXWithdraw(l); e != nil {
				vlux = append(vlux, *e)
			}

		// Karma
		case TopicKarmaMinted:
			if e := parseKarmaMinted(l); e != nil {
				karma = append(karma, *e)
			}
		case TopicKarmaSlashed:
			if e := parseKarmaSlashed(l); e != nil {
				karma = append(karma, *e)
			}
		case TopicKarmaDecayed:
			if e := parseKarmaDecayed(l); e != nil {
				karma = append(karma, *e)
			}
		case TopicDIDLinked:
			if e := parseDIDLinked(l); e != nil {
				karma = append(karma, *e)
			}
		case TopicVerified:
			if e := parseVerified(l); e != nil {
				karma = append(karma, *e)
			}

		// DLUX
		case TopicDLUXStaked:
			if e := parseDLUXStaked(l); e != nil {
				dlux = append(dlux, *e)
			}
		case TopicDLUXUnstaked:
			if e := parseDLUXUnstaked(l); e != nil {
				dlux = append(dlux, *e)
			}
		case TopicRebased:
			if e := parseRebased(l); e != nil {
				dlux = append(dlux, *e)
			}
		case TopicDemurrageApplied:
			if e := parseDemurrage(l); e != nil {
				dlux = append(dlux, *e)
			}
		case TopicDLUXMinted:
			if e := parseDLUXMinted(l); e != nil {
				dlux = append(dlux, *e)
			}
		}
	}
	return gauges, vlux, karma, dlux
}

func parseGaugeAdded(l *Log) *GaugeEvent {
	if len(l.Topics) < 2 {
		return nil
	}
	return &GaugeEvent{
		GaugeID:   wordFromTopic(l.Topics[1]),
		Recipient: decodeAddress(l.Data, 0),
		Event:     "added",
		Contract:  l.Address,
		Block:     l.BlockNumber,
		TxHash:    l.TxHash,
	}
}

func parseGaugeUpdated(l *Log) *GaugeEvent {
	if len(l.Topics) < 2 {
		return nil
	}
	active := decodeWord(l.Data, 0)
	return &GaugeEvent{
		GaugeID:  wordFromTopic(l.Topics[1]),
		Active:   active != nil && active.Sign() > 0,
		Event:    "updated",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseGaugeVote(l *Log) *GaugeEvent {
	// VoteCast(address indexed user, uint256 indexed gaugeId, uint256 weight)
	if len(l.Topics) < 3 {
		return nil
	}
	return &GaugeEvent{
		User:     topicAddress(l.Topics[1]),
		GaugeID:  wordFromTopic(l.Topics[2]),
		Weight:   decodeWord(l.Data, 0),
		Event:    "vote",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseVLUXDeposit(l *Log) *VLUXEvent {
	// Deposit(address indexed user, uint256 amount, uint256 lockTime, uint256 indexed lockEnd, uint256 ts)
	if len(l.Topics) < 3 {
		return nil
	}
	return &VLUXEvent{
		User:     topicAddress(l.Topics[1]),
		Amount:   decodeWord(l.Data, 0),
		LockEnd:  wordFromTopic(l.Topics[2]),
		Event:    "deposit",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseVLUXWithdraw(l *Log) *VLUXEvent {
	// Withdraw(address indexed user, uint256 amount, uint256 ts)
	if len(l.Topics) < 2 {
		return nil
	}
	return &VLUXEvent{
		User:     topicAddress(l.Topics[1]),
		Amount:   decodeWord(l.Data, 0),
		Event:    "withdraw",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseKarmaMinted(l *Log) *KarmaEvent {
	// KarmaMinted(address indexed to, uint256 amount, bytes32 reason)
	if len(l.Topics) < 2 {
		return nil
	}
	reason := decodeWord(l.Data, 1)
	return &KarmaEvent{
		Account:  topicAddress(l.Topics[1]),
		Amount:   decodeWord(l.Data, 0),
		Reason:   wordHex(reason),
		Event:    "minted",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseKarmaSlashed(l *Log) *KarmaEvent {
	// KarmaSlashed(address indexed from, uint256 amount, bytes32 reason)
	if len(l.Topics) < 2 {
		return nil
	}
	reason := decodeWord(l.Data, 1)
	return &KarmaEvent{
		Account:  topicAddress(l.Topics[1]),
		Amount:   decodeWord(l.Data, 0),
		Reason:   wordHex(reason),
		Event:    "slashed",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseKarmaDecayed(l *Log) *KarmaEvent {
	// KarmaDecayed(address indexed account, uint256 amount, bool wasActive)
	if len(l.Topics) < 2 {
		return nil
	}
	wasActive := decodeWord(l.Data, 1)
	return &KarmaEvent{
		Account:   topicAddress(l.Topics[1]),
		Amount:    decodeWord(l.Data, 0),
		WasActive: wasActive != nil && wasActive.Sign() > 0,
		Event:     "decayed",
		Contract:  l.Address,
		Block:     l.BlockNumber,
		TxHash:    l.TxHash,
	}
}

func parseDIDLinked(l *Log) *KarmaEvent {
	// DIDLinked(address indexed account, bytes32 indexed did)
	if len(l.Topics) < 3 {
		return nil
	}
	return &KarmaEvent{
		Account:  topicAddress(l.Topics[1]),
		DID:      l.Topics[2],
		Event:    "did_linked",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseVerified(l *Log) *KarmaEvent {
	// Verified(address indexed account, bool status)
	if len(l.Topics) < 2 {
		return nil
	}
	return &KarmaEvent{
		Account:  topicAddress(l.Topics[1]),
		Event:    "verified",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseDLUXStaked(l *Log) *DLUXEvent {
	// Staked(address indexed user, uint256 amount, uint8 tier, uint256 lockEnd)
	if len(l.Topics) < 2 {
		return nil
	}
	tier := decodeWord(l.Data, 1)
	var t uint8
	if tier != nil && tier.IsUint64() {
		t = uint8(tier.Uint64())
	}
	return &DLUXEvent{
		User:     topicAddress(l.Topics[1]),
		Amount:   decodeWord(l.Data, 0),
		Tier:     t,
		LockEnd:  decodeWord(l.Data, 2),
		Event:    "staked",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseDLUXUnstaked(l *Log) *DLUXEvent {
	// Unstaked(address indexed user, uint256 amount, uint256 luxReturned)
	if len(l.Topics) < 2 {
		return nil
	}
	return &DLUXEvent{
		User:     topicAddress(l.Topics[1]),
		Amount:   decodeWord(l.Data, 0),
		Event:    "unstaked",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseRebased(l *Log) *DLUXEvent {
	// Rebased(uint256 epoch, uint256 totalRebased, uint256 rate)
	return &DLUXEvent{
		Epoch:    decodeWord(l.Data, 0),
		Amount:   decodeWord(l.Data, 1),
		Rate:     decodeWord(l.Data, 2),
		Event:    "rebased",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseDemurrage(l *Log) *DLUXEvent {
	// DemurrageApplied(address indexed account, uint256 burned)
	if len(l.Topics) < 2 {
		return nil
	}
	return &DLUXEvent{
		User:     topicAddress(l.Topics[1]),
		Amount:   decodeWord(l.Data, 0),
		Event:    "demurrage",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

func parseDLUXMinted(l *Log) *DLUXEvent {
	// Minted(address indexed to, uint256 amount, bytes32 indexed reason)
	if len(l.Topics) < 3 {
		return nil
	}
	return &DLUXEvent{
		User:     topicAddress(l.Topics[1]),
		Amount:   decodeWord(l.Data, 0),
		Reason:   l.Topics[2],
		Event:    "minted",
		Contract: l.Address,
		Block:    l.BlockNumber,
		TxHash:   l.TxHash,
	}
}

// wordFromTopic parses a topic hex string into a big.Int.
func wordFromTopic(topic string) *big.Int {
	n := new(big.Int)
	if len(topic) > 2 {
		n.SetString(topic[2:], 16)
	}
	return n
}

// wordHex formats a big.Int as a 0x-prefixed hex string.
func wordHex(n *big.Int) string {
	if n == nil {
		return "0x0"
	}
	return "0x" + n.Text(16)
}
