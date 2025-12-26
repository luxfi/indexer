// Package defi provides DeFi protocol indexing for the Lux EVM indexer.
package defi

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"
)

// Bridge event signatures (keccak256 hashes)
var (
	// Warp Messenger (Lux native cross-chain)
	WarpMessageSentSig     = "0x56600c567728a800c0aa927500f831cb451df66a7af570eb4df4dfbf4674887d"
	WarpMessageReceivedSig = "0x1f5f0f8c6e4e2e6a5c3f4d5e6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b"

	// Token Bridge (wrapped assets)
	TokenBridgeDepositSig  = "0x5548c837ab068cf56a2c2479df0882a4922fd203edb7517321831d95078c5f62"
	TokenBridgeWithdrawSig = "0x9b1bfa7fa9ee420a16e124f794c35ac9f90472acc99140eb2f6447c714cad8eb"
	TokenBridgeMintSig     = "0x0f6798a560793a54c3bcfe86a93cde1e73087d944c0ea20544137d4121396885"
	TokenBridgeBurnSig     = "0xcc16f5dbb4873280815c1ee09dbd06736cffcc184412cf7a71a0fdb75d397ca5"

	// Native Bridge (LUX/ZOO cross-chain)
	NativeBridgeLockSig    = "0x9f1ec8c880f76798e7b793325d625e9b60e4082a553c98f42b6cda368dd60008"
	NativeBridgeUnlockSig  = "0x6381d9813cabeb57471b5a7e05078e64845ccdb563146a6911d536f24ce960f1"
	NativeBridgeReleaseSig = "0x2317b3c7d6ae2b8cc9d3e8b2c8d4f6e8a0b2c4d6e8f0a2b4c6d8e0f2a4b6c8d0"

	// Cross-Chain Router
	CrossChainSwapInitSig     = "0x76bb911c362d5b1feb3058bc7dc9354703e4b6eb9c61cc845f73da880cf62fe0"
	CrossChainSwapCompleteSig = "0x823eaf01002d7353fbcadb2ea3305cc46fa35d799cb0914846f64b19b424c2e9"
	CrossChainSwapRefundSig   = "0x1c85ff1efe0a905f8feca811e617102cb7ec896aded693eb96366c8ef22bb09f"

	// Teleporter (fast finality bridge)
	TeleporterSendSig    = "0x93f19bf1ec58a15dc643b37e7e18a1c13e85f4e4c4e3e4e5e6e7e8e9e0e1e2e3"
	TeleporterReceiveSig = "0x1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b"
)

// Chain identifiers for Lux ecosystem
type ChainID string

const (
	ChainCMainnet ChainID = "C-96369"  // Lux C-Chain Mainnet
	ChainCTestnet ChainID = "C-96368"  // Lux C-Chain Testnet
	ChainZoo      ChainID = "Z-200200" // Zoo Network Mainnet
	ChainZooTest  ChainID = "Z-200201" // Zoo Network Testnet
	ChainHanzo    ChainID = "H-36963"  // Hanzo AI Chain
	ChainPChain   ChainID = "P"        // Platform Chain (validators)
	ChainXChain   ChainID = "X"        // Exchange Chain (assets)
	ChainBChain   ChainID = "B"        // Bridge Chain
	ChainTChain   ChainID = "T"        // Teleporter Chain
)

// BridgeType represents different bridge mechanisms
type BridgeType string

const (
	BridgeTypeWarp       BridgeType = "warp"       // Native Lux Warp
	BridgeTypeToken      BridgeType = "token"      // ERC20/wrapped tokens
	BridgeTypeNative     BridgeType = "native"     // Native LUX/ZOO
	BridgeTypeTeleporter BridgeType = "teleporter" // Fast finality
	BridgeTypeCrossChain BridgeType = "crosschain" // Cross-chain swaps
)

// BridgeTransferStatus represents the status of a bridge transfer
type BridgeTransferStatus string

const (
	BridgeStatusPending   BridgeTransferStatus = "pending"
	BridgeStatusConfirmed BridgeTransferStatus = "confirmed"
	BridgeStatusCompleted BridgeTransferStatus = "completed"
	BridgeStatusFailed    BridgeTransferStatus = "failed"
	BridgeStatusRefunded  BridgeTransferStatus = "refunded"
)

// BridgeTransfer represents a cross-chain asset transfer
type BridgeTransfer struct {
	ID            string               `json:"id"`
	BridgeType    BridgeType           `json:"bridgeType"`
	SourceChain   ChainID              `json:"sourceChain"`
	DestChain     ChainID              `json:"destChain"`
	SourceTxHash  string               `json:"sourceTxHash"`
	DestTxHash    string               `json:"destTxHash,omitempty"`
	Sender        string               `json:"sender"`
	Recipient     string               `json:"recipient"`
	Token         string               `json:"token"` // Token address (0x0 for native)
	Amount        *big.Int             `json:"amount"`
	Fee           *big.Int             `json:"fee"`
	Status        BridgeTransferStatus `json:"status"`
	InitiatedAt   time.Time            `json:"initiatedAt"`
	CompletedAt   *time.Time           `json:"completedAt,omitempty"`
	WarpMessageID string               `json:"warpMessageId,omitempty"`
	Confirmations uint64               `json:"confirmations"`
	BlockNumber   uint64               `json:"blockNumber"`
	LogIndex      uint64               `json:"logIndex"`
}

// WarpMessage represents a Lux Warp Messenger message
type WarpMessage struct {
	ID              string    `json:"id"`
	SourceChainID   string    `json:"sourceChainId"`
	DestChainID     string    `json:"destChainId"`
	Sender          string    `json:"sender"`
	Payload         []byte    `json:"payload"`
	Nonce           uint64    `json:"nonce"`
	Timestamp       time.Time `json:"timestamp"`
	Verified        bool      `json:"verified"`
	SignatureWeight uint64    `json:"signatureWeight"`
	TotalWeight     uint64    `json:"totalWeight"`
}

// CrossChainSwap represents a cross-chain swap operation
type CrossChainSwap struct {
	ID           string               `json:"id"`
	SourceChain  ChainID              `json:"sourceChain"`
	DestChain    ChainID              `json:"destChain"`
	Initiator    string               `json:"initiator"`
	Recipient    string               `json:"recipient"`
	TokenIn      string               `json:"tokenIn"`
	TokenOut     string               `json:"tokenOut"`
	AmountIn     *big.Int             `json:"amountIn"`
	AmountOut    *big.Int             `json:"amountOut"`
	MinAmountOut *big.Int             `json:"minAmountOut"`
	Deadline     time.Time            `json:"deadline"`
	Status       BridgeTransferStatus `json:"status"`
	Route        []string             `json:"route"`
	Fees         map[string]*big.Int  `json:"fees"`
	InitiatedAt  time.Time            `json:"initiatedAt"`
	CompletedAt  *time.Time           `json:"completedAt,omitempty"`
	SourceTxHash string               `json:"sourceTxHash"`
	DestTxHash   string               `json:"destTxHash,omitempty"`
}

// TeleporterTransfer represents a fast-finality bridge transfer
type TeleporterTransfer struct {
	ID             string    `json:"id"`
	MessageID      [32]byte  `json:"messageId"`
	SourceChain    ChainID   `json:"sourceChain"`
	DestChain      ChainID   `json:"destChain"`
	Sender         string    `json:"sender"`
	Recipient      string    `json:"recipient"`
	Token          string    `json:"token"`
	Amount         *big.Int  `json:"amount"`
	FeeToken       string    `json:"feeToken"`
	FeeAmount      *big.Int  `json:"feeAmount"`
	RequiredGas    uint64    `json:"requiredGas"`
	AllowedRelayer string    `json:"allowedRelayer,omitempty"`
	Message        []byte    `json:"message"`
	Received       bool      `json:"received"`
	Timestamp      time.Time `json:"timestamp"`
}

// AssetMovement represents any tracked asset movement
type AssetMovement struct {
	ID              string       `json:"id"`
	Type            MovementType `json:"type"`
	Chain           ChainID      `json:"chain"`
	TxHash          string       `json:"txHash"`
	BlockNumber     uint64       `json:"blockNumber"`
	From            string       `json:"from"`
	To              string       `json:"to"`
	Token           string       `json:"token"`
	Amount          *big.Int     `json:"amount"`
	IsNative        bool         `json:"isNative"`
	Timestamp       time.Time    `json:"timestamp"`
	RelatedBridgeID string       `json:"relatedBridgeId,omitempty"`
}

// MovementType categorizes asset movements
type MovementType string

const (
	MovementTypeTransfer     MovementType = "transfer"
	MovementTypeBridgeLock   MovementType = "bridge_lock"
	MovementTypeBridgeMint   MovementType = "bridge_mint"
	MovementTypeBridgeBurn   MovementType = "bridge_burn"
	MovementTypeBridgeUnlock MovementType = "bridge_unlock"
	MovementTypeStake        MovementType = "stake"
	MovementTypeUnstake      MovementType = "unstake"
	MovementTypeReward       MovementType = "reward"
)

// BridgeStats tracks bridge statistics
type BridgeStats struct {
	TotalTransfers     uint64               `json:"totalTransfers"`
	TotalVolume        map[string]*big.Int  `json:"totalVolume"` // token -> volume
	PendingTransfers   uint64               `json:"pendingTransfers"`
	CompletedTransfers uint64               `json:"completedTransfers"`
	FailedTransfers    uint64               `json:"failedTransfers"`
	AverageTime        time.Duration        `json:"averageTime"`
	ChainVolumes       map[ChainID]*big.Int `json:"chainVolumes"`
	Last24HVolume      *big.Int             `json:"last24hVolume"`
	Last24HTransfers   uint64               `json:"last24hTransfers"`
}

// BridgeIndexer indexes bridge and cross-chain transfers
type BridgeIndexer struct {
	mu              sync.RWMutex
	transfers       map[string]*BridgeTransfer
	warpMessages    map[string]*WarpMessage
	crossChainSwaps map[string]*CrossChainSwap
	teleporter      map[string]*TeleporterTransfer
	movements       []*AssetMovement
	stats           *BridgeStats
	pendingByChain  map[ChainID][]*BridgeTransfer
	chainID         ChainID
}

// NewBridgeIndexer creates a new bridge indexer
func NewBridgeIndexer(chainID ChainID) *BridgeIndexer {
	return &BridgeIndexer{
		transfers:       make(map[string]*BridgeTransfer),
		warpMessages:    make(map[string]*WarpMessage),
		crossChainSwaps: make(map[string]*CrossChainSwap),
		teleporter:      make(map[string]*TeleporterTransfer),
		movements:       make([]*AssetMovement, 0),
		stats: &BridgeStats{
			TotalVolume:  make(map[string]*big.Int),
			ChainVolumes: make(map[ChainID]*big.Int),
		},
		pendingByChain: make(map[ChainID][]*BridgeTransfer),
		chainID:        chainID,
	}
}

// IndexLog processes a log entry for bridge events
func (b *BridgeIndexer) IndexLog(log *LogEntry) error {
	if len(log.Topics) == 0 {
		return nil
	}

	topic0 := log.Topics[0]

	switch topic0 {
	// Warp Messenger
	case WarpMessageSentSig:
		return b.indexWarpMessageSent(log)
	case WarpMessageReceivedSig:
		return b.indexWarpMessageReceived(log)

	// Token Bridge
	case TokenBridgeDepositSig:
		return b.indexTokenBridgeDeposit(log)
	case TokenBridgeWithdrawSig:
		return b.indexTokenBridgeWithdraw(log)
	case TokenBridgeMintSig:
		return b.indexTokenBridgeMint(log)
	case TokenBridgeBurnSig:
		return b.indexTokenBridgeBurn(log)

	// Native Bridge
	case NativeBridgeLockSig:
		return b.indexNativeBridgeLock(log)
	case NativeBridgeUnlockSig:
		return b.indexNativeBridgeUnlock(log)

	// Cross-Chain Swaps
	case CrossChainSwapInitSig:
		return b.indexCrossChainSwapInit(log)
	case CrossChainSwapCompleteSig:
		return b.indexCrossChainSwapComplete(log)
	case CrossChainSwapRefundSig:
		return b.indexCrossChainSwapRefund(log)

	// Teleporter
	case TeleporterSendSig:
		return b.indexTeleporterSend(log)
	case TeleporterReceiveSig:
		return b.indexTeleporterReceive(log)
	}

	return nil
}

// indexWarpMessageSent handles Warp message sent events
func (b *BridgeIndexer) indexWarpMessageSent(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 96 {
		return fmt.Errorf("invalid warp message sent log")
	}

	messageID := log.Topics[1]
	destChainID := log.Topics[2]

	// Parse data: sender, payload
	sender := "0x" + log.Data[24:64]
	nonce := new(big.Int).SetBytes(hexToBytes(log.Data[64:128]))

	warpMsg := &WarpMessage{
		ID:            messageID,
		SourceChainID: string(b.chainID),
		DestChainID:   destChainID,
		Sender:        sender,
		Nonce:         nonce.Uint64(),
		Timestamp:     time.Now(),
		Verified:      false,
	}

	// Extract payload if present
	if len(log.Data) > 128 {
		payloadOffset := new(big.Int).SetBytes(hexToBytes(log.Data[128:192])).Uint64()
		if payloadOffset*2+64 < uint64(len(log.Data)) {
			payloadLen := new(big.Int).SetBytes(hexToBytes(log.Data[payloadOffset*2 : payloadOffset*2+64])).Uint64()
			if payloadOffset*2+64+payloadLen*2 <= uint64(len(log.Data)) {
				warpMsg.Payload = hexToBytes(log.Data[payloadOffset*2+64 : payloadOffset*2+64+payloadLen*2])
			}
		}
	}

	b.mu.Lock()
	b.warpMessages[messageID] = warpMsg
	b.mu.Unlock()

	// Create corresponding bridge transfer
	transfer := &BridgeTransfer{
		ID:            fmt.Sprintf("warp-%s", messageID),
		BridgeType:    BridgeTypeWarp,
		SourceChain:   b.chainID,
		DestChain:     ChainID(destChainID),
		SourceTxHash:  log.TxHash,
		Sender:        sender,
		Status:        BridgeStatusPending,
		InitiatedAt:   time.Now(),
		WarpMessageID: messageID,
		BlockNumber:   log.BlockNumber,
		LogIndex:      log.LogIndex,
	}

	return b.addTransfer(transfer)
}

// indexWarpMessageReceived handles Warp message received events
func (b *BridgeIndexer) indexWarpMessageReceived(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid warp message received log")
	}

	messageID := log.Topics[1]

	b.mu.Lock()
	defer b.mu.Unlock()

	// Update warp message
	if msg, ok := b.warpMessages[messageID]; ok {
		msg.Verified = true
	}

	// Update corresponding transfer
	transferID := fmt.Sprintf("warp-%s", messageID)
	if transfer, ok := b.transfers[transferID]; ok {
		transfer.Status = BridgeStatusCompleted
		transfer.DestTxHash = log.TxHash
		now := time.Now()
		transfer.CompletedAt = &now
		b.stats.CompletedTransfers++
		b.stats.PendingTransfers--
	}

	return nil
}

// indexTokenBridgeDeposit handles token bridge deposit (lock) events
func (b *BridgeIndexer) indexTokenBridgeDeposit(log *LogEntry) error {
	if len(log.Topics) < 4 || len(log.Data) < 64 {
		return fmt.Errorf("invalid token bridge deposit log")
	}

	token := "0x" + log.Topics[1][26:]
	sender := "0x" + log.Topics[2][26:]
	destChainRaw := log.Topics[3]

	amount := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))
	recipient := "0x" + log.Data[88:128]

	// Parse destination chain
	destChain := parseChainID(destChainRaw)

	transfer := &BridgeTransfer{
		ID:           fmt.Sprintf("token-deposit-%s-%d", log.TxHash, log.LogIndex),
		BridgeType:   BridgeTypeToken,
		SourceChain:  b.chainID,
		DestChain:    destChain,
		SourceTxHash: log.TxHash,
		Sender:       sender,
		Recipient:    recipient,
		Token:        token,
		Amount:       amount,
		Status:       BridgeStatusPending,
		InitiatedAt:  time.Now(),
		BlockNumber:  log.BlockNumber,
		LogIndex:     log.LogIndex,
	}

	// Track asset movement
	movement := &AssetMovement{
		ID:              fmt.Sprintf("lock-%s-%d", log.TxHash, log.LogIndex),
		Type:            MovementTypeBridgeLock,
		Chain:           b.chainID,
		TxHash:          log.TxHash,
		BlockNumber:     log.BlockNumber,
		From:            sender,
		To:              log.Address,
		Token:           token,
		Amount:          amount,
		Timestamp:       time.Now(),
		RelatedBridgeID: transfer.ID,
	}

	b.mu.Lock()
	b.movements = append(b.movements, movement)
	b.mu.Unlock()

	return b.addTransfer(transfer)
}

// indexTokenBridgeWithdraw handles token bridge withdraw (unlock) events
func (b *BridgeIndexer) indexTokenBridgeWithdraw(log *LogEntry) error {
	if len(log.Topics) < 4 || len(log.Data) < 64 {
		return fmt.Errorf("invalid token bridge withdraw log")
	}

	token := "0x" + log.Topics[1][26:]
	recipient := "0x" + log.Topics[2][26:]
	sourceChainRaw := log.Topics[3]

	amount := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))

	sourceChain := parseChainID(sourceChainRaw)

	// Find and update pending transfer
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, transfer := range b.transfers {
		if transfer.Status == BridgeStatusPending &&
			transfer.SourceChain == sourceChain &&
			transfer.DestChain == b.chainID &&
			transfer.Recipient == recipient &&
			transfer.Token == token &&
			transfer.Amount.Cmp(amount) == 0 {

			transfer.Status = BridgeStatusCompleted
			transfer.DestTxHash = log.TxHash
			now := time.Now()
			transfer.CompletedAt = &now

			b.stats.CompletedTransfers++
			b.stats.PendingTransfers--

			// Track asset movement
			movement := &AssetMovement{
				ID:              fmt.Sprintf("unlock-%s-%d", log.TxHash, log.LogIndex),
				Type:            MovementTypeBridgeUnlock,
				Chain:           b.chainID,
				TxHash:          log.TxHash,
				BlockNumber:     log.BlockNumber,
				From:            log.Address,
				To:              recipient,
				Token:           token,
				Amount:          amount,
				Timestamp:       time.Now(),
				RelatedBridgeID: transfer.ID,
			}
			b.movements = append(b.movements, movement)

			break
		}
	}

	return nil
}

// indexTokenBridgeMint handles wrapped token minting on destination
func (b *BridgeIndexer) indexTokenBridgeMint(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 64 {
		return fmt.Errorf("invalid token bridge mint log")
	}

	wrappedToken := log.Address
	recipient := "0x" + log.Topics[1][26:]
	originalToken := "0x" + log.Topics[2][26:]

	amount := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))

	movement := &AssetMovement{
		ID:          fmt.Sprintf("mint-%s-%d", log.TxHash, log.LogIndex),
		Type:        MovementTypeBridgeMint,
		Chain:       b.chainID,
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		From:        "0x0000000000000000000000000000000000000000",
		To:          recipient,
		Token:       wrappedToken,
		Amount:      amount,
		Timestamp:   time.Now(),
	}

	b.mu.Lock()
	b.movements = append(b.movements, movement)
	b.mu.Unlock()

	// Update transfer status if found
	b.updatePendingTransferByRecipient(recipient, originalToken, amount, log.TxHash)

	return nil
}

// indexTokenBridgeBurn handles wrapped token burning on source
func (b *BridgeIndexer) indexTokenBridgeBurn(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 64 {
		return fmt.Errorf("invalid token bridge burn log")
	}

	wrappedToken := log.Address
	sender := "0x" + log.Topics[1][26:]

	amount := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))

	movement := &AssetMovement{
		ID:          fmt.Sprintf("burn-%s-%d", log.TxHash, log.LogIndex),
		Type:        MovementTypeBridgeBurn,
		Chain:       b.chainID,
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		From:        sender,
		To:          "0x0000000000000000000000000000000000000000",
		Token:       wrappedToken,
		Amount:      amount,
		Timestamp:   time.Now(),
	}

	b.mu.Lock()
	b.movements = append(b.movements, movement)
	b.mu.Unlock()

	return nil
}

// indexNativeBridgeLock handles native asset (LUX/ZOO) locking
func (b *BridgeIndexer) indexNativeBridgeLock(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 64 {
		return fmt.Errorf("invalid native bridge lock log")
	}

	sender := "0x" + log.Topics[1][26:]
	destChainRaw := log.Topics[2]

	amount := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))
	recipient := "0x" + log.Data[88:128]

	destChain := parseChainID(destChainRaw)

	transfer := &BridgeTransfer{
		ID:           fmt.Sprintf("native-lock-%s-%d", log.TxHash, log.LogIndex),
		BridgeType:   BridgeTypeNative,
		SourceChain:  b.chainID,
		DestChain:    destChain,
		SourceTxHash: log.TxHash,
		Sender:       sender,
		Recipient:    recipient,
		Token:        "0x0000000000000000000000000000000000000000", // Native
		Amount:       amount,
		Status:       BridgeStatusPending,
		InitiatedAt:  time.Now(),
		BlockNumber:  log.BlockNumber,
		LogIndex:     log.LogIndex,
	}

	movement := &AssetMovement{
		ID:              fmt.Sprintf("native-lock-%s-%d", log.TxHash, log.LogIndex),
		Type:            MovementTypeBridgeLock,
		Chain:           b.chainID,
		TxHash:          log.TxHash,
		BlockNumber:     log.BlockNumber,
		From:            sender,
		To:              log.Address,
		Token:           "0x0000000000000000000000000000000000000000",
		Amount:          amount,
		IsNative:        true,
		Timestamp:       time.Now(),
		RelatedBridgeID: transfer.ID,
	}

	b.mu.Lock()
	b.movements = append(b.movements, movement)
	b.mu.Unlock()

	return b.addTransfer(transfer)
}

// indexNativeBridgeUnlock handles native asset unlocking
func (b *BridgeIndexer) indexNativeBridgeUnlock(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 64 {
		return fmt.Errorf("invalid native bridge unlock log")
	}

	recipient := "0x" + log.Topics[1][26:]
	sourceChainRaw := log.Topics[2]

	amount := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))

	sourceChain := parseChainID(sourceChainRaw)

	// Find and update pending transfer
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, transfer := range b.transfers {
		if transfer.Status == BridgeStatusPending &&
			transfer.BridgeType == BridgeTypeNative &&
			transfer.SourceChain == sourceChain &&
			transfer.DestChain == b.chainID &&
			transfer.Recipient == recipient &&
			transfer.Amount.Cmp(amount) == 0 {

			transfer.Status = BridgeStatusCompleted
			transfer.DestTxHash = log.TxHash
			now := time.Now()
			transfer.CompletedAt = &now

			b.stats.CompletedTransfers++
			b.stats.PendingTransfers--

			movement := &AssetMovement{
				ID:              fmt.Sprintf("native-unlock-%s-%d", log.TxHash, log.LogIndex),
				Type:            MovementTypeBridgeUnlock,
				Chain:           b.chainID,
				TxHash:          log.TxHash,
				BlockNumber:     log.BlockNumber,
				From:            log.Address,
				To:              recipient,
				Token:           "0x0000000000000000000000000000000000000000",
				Amount:          amount,
				IsNative:        true,
				Timestamp:       time.Now(),
				RelatedBridgeID: transfer.ID,
			}
			b.movements = append(b.movements, movement)

			break
		}
	}

	return nil
}

// indexCrossChainSwapInit handles cross-chain swap initiation
func (b *BridgeIndexer) indexCrossChainSwapInit(log *LogEntry) error {
	if len(log.Topics) < 4 || len(log.Data) < 192 {
		return fmt.Errorf("invalid cross-chain swap init log")
	}

	swapID := log.Topics[1]
	initiator := "0x" + log.Topics[2][26:]
	destChainRaw := log.Topics[3]

	tokenIn := "0x" + log.Data[24:64]
	tokenOut := "0x" + log.Data[88:128]
	amountIn := new(big.Int).SetBytes(hexToBytes(log.Data[128:192]))
	minAmountOut := new(big.Int).SetBytes(hexToBytes(log.Data[192:256]))
	recipient := "0x" + log.Data[280:320]

	destChain := parseChainID(destChainRaw)

	swap := &CrossChainSwap{
		ID:           swapID,
		SourceChain:  b.chainID,
		DestChain:    destChain,
		Initiator:    initiator,
		Recipient:    recipient,
		TokenIn:      tokenIn,
		TokenOut:     tokenOut,
		AmountIn:     amountIn,
		MinAmountOut: minAmountOut,
		Status:       BridgeStatusPending,
		InitiatedAt:  time.Now(),
		SourceTxHash: log.TxHash,
		Fees:         make(map[string]*big.Int),
	}

	b.mu.Lock()
	b.crossChainSwaps[swapID] = swap
	b.mu.Unlock()

	// Also create a bridge transfer for tracking
	transfer := &BridgeTransfer{
		ID:           fmt.Sprintf("ccswap-%s", swapID),
		BridgeType:   BridgeTypeCrossChain,
		SourceChain:  b.chainID,
		DestChain:    destChain,
		SourceTxHash: log.TxHash,
		Sender:       initiator,
		Recipient:    recipient,
		Token:        tokenIn,
		Amount:       amountIn,
		Status:       BridgeStatusPending,
		InitiatedAt:  time.Now(),
		BlockNumber:  log.BlockNumber,
		LogIndex:     log.LogIndex,
	}

	return b.addTransfer(transfer)
}

// indexCrossChainSwapComplete handles cross-chain swap completion
func (b *BridgeIndexer) indexCrossChainSwapComplete(log *LogEntry) error {
	if len(log.Topics) < 2 || len(log.Data) < 64 {
		return fmt.Errorf("invalid cross-chain swap complete log")
	}

	swapID := log.Topics[1]
	amountOut := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))

	b.mu.Lock()
	defer b.mu.Unlock()

	if swap, ok := b.crossChainSwaps[swapID]; ok {
		swap.Status = BridgeStatusCompleted
		swap.AmountOut = amountOut
		swap.DestTxHash = log.TxHash
		now := time.Now()
		swap.CompletedAt = &now
	}

	// Update corresponding transfer
	transferID := fmt.Sprintf("ccswap-%s", swapID)
	if transfer, ok := b.transfers[transferID]; ok {
		transfer.Status = BridgeStatusCompleted
		transfer.DestTxHash = log.TxHash
		now := time.Now()
		transfer.CompletedAt = &now
		b.stats.CompletedTransfers++
		b.stats.PendingTransfers--
	}

	return nil
}

// indexCrossChainSwapRefund handles cross-chain swap refunds
func (b *BridgeIndexer) indexCrossChainSwapRefund(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid cross-chain swap refund log")
	}

	swapID := log.Topics[1]

	b.mu.Lock()
	defer b.mu.Unlock()

	if swap, ok := b.crossChainSwaps[swapID]; ok {
		swap.Status = BridgeStatusRefunded
		now := time.Now()
		swap.CompletedAt = &now
	}

	// Update corresponding transfer
	transferID := fmt.Sprintf("ccswap-%s", swapID)
	if transfer, ok := b.transfers[transferID]; ok {
		transfer.Status = BridgeStatusRefunded
		now := time.Now()
		transfer.CompletedAt = &now
		b.stats.FailedTransfers++
		b.stats.PendingTransfers--
	}

	return nil
}

// indexTeleporterSend handles Teleporter send events
func (b *BridgeIndexer) indexTeleporterSend(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 192 {
		return fmt.Errorf("invalid teleporter send log")
	}

	messageID := log.Topics[1]
	destChainRaw := log.Topics[2]

	sender := "0x" + log.Data[24:64]
	recipient := "0x" + log.Data[88:128]
	feeAmount := new(big.Int).SetBytes(hexToBytes(log.Data[128:192]))

	destChain := parseChainID(destChainRaw)

	var msgID [32]byte
	copy(msgID[:], hexToBytes(messageID[2:]))

	teleport := &TeleporterTransfer{
		ID:          messageID,
		MessageID:   msgID,
		SourceChain: b.chainID,
		DestChain:   destChain,
		Sender:      sender,
		Recipient:   recipient,
		FeeAmount:   feeAmount,
		Received:    false,
		Timestamp:   time.Now(),
	}

	b.mu.Lock()
	b.teleporter[messageID] = teleport
	b.mu.Unlock()

	// Create bridge transfer
	transfer := &BridgeTransfer{
		ID:            fmt.Sprintf("teleport-%s", messageID),
		BridgeType:    BridgeTypeTeleporter,
		SourceChain:   b.chainID,
		DestChain:     destChain,
		SourceTxHash:  log.TxHash,
		Sender:        sender,
		Recipient:     recipient,
		Fee:           feeAmount,
		Status:        BridgeStatusPending,
		InitiatedAt:   time.Now(),
		WarpMessageID: messageID,
		BlockNumber:   log.BlockNumber,
		LogIndex:      log.LogIndex,
	}

	return b.addTransfer(transfer)
}

// indexTeleporterReceive handles Teleporter receive events
func (b *BridgeIndexer) indexTeleporterReceive(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid teleporter receive log")
	}

	messageID := log.Topics[1]

	b.mu.Lock()
	defer b.mu.Unlock()

	if teleport, ok := b.teleporter[messageID]; ok {
		teleport.Received = true
	}

	// Update corresponding transfer
	transferID := fmt.Sprintf("teleport-%s", messageID)
	if transfer, ok := b.transfers[transferID]; ok {
		transfer.Status = BridgeStatusCompleted
		transfer.DestTxHash = log.TxHash
		now := time.Now()
		transfer.CompletedAt = &now
		b.stats.CompletedTransfers++
		b.stats.PendingTransfers--
	}

	return nil
}

// addTransfer adds a new bridge transfer
func (b *BridgeIndexer) addTransfer(transfer *BridgeTransfer) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.transfers[transfer.ID] = transfer
	b.stats.TotalTransfers++
	b.stats.PendingTransfers++

	// Update volume stats
	if transfer.Amount != nil {
		token := transfer.Token
		if b.stats.TotalVolume[token] == nil {
			b.stats.TotalVolume[token] = big.NewInt(0)
		}
		b.stats.TotalVolume[token].Add(b.stats.TotalVolume[token], transfer.Amount)

		if b.stats.ChainVolumes[transfer.SourceChain] == nil {
			b.stats.ChainVolumes[transfer.SourceChain] = big.NewInt(0)
		}
		b.stats.ChainVolumes[transfer.SourceChain].Add(b.stats.ChainVolumes[transfer.SourceChain], transfer.Amount)
	}

	// Track pending by destination chain
	b.pendingByChain[transfer.DestChain] = append(b.pendingByChain[transfer.DestChain], transfer)

	return nil
}

// updatePendingTransferByRecipient updates a pending transfer when matched
func (b *BridgeIndexer) updatePendingTransferByRecipient(recipient, token string, amount *big.Int, txHash string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, transfer := range b.transfers {
		if transfer.Status == BridgeStatusPending &&
			transfer.DestChain == b.chainID &&
			transfer.Recipient == recipient &&
			(transfer.Token == token || (token != "" && transfer.Token != "")) &&
			transfer.Amount.Cmp(amount) == 0 {

			transfer.Status = BridgeStatusCompleted
			transfer.DestTxHash = txHash
			now := time.Now()
			transfer.CompletedAt = &now

			b.stats.CompletedTransfers++
			b.stats.PendingTransfers--

			break
		}
	}
}

// GetTransfer returns a bridge transfer by ID
func (b *BridgeIndexer) GetTransfer(id string) (*BridgeTransfer, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	transfer, ok := b.transfers[id]
	return transfer, ok
}

// GetTransfersByAddress returns all transfers for an address
func (b *BridgeIndexer) GetTransfersByAddress(address string) []*BridgeTransfer {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var result []*BridgeTransfer
	for _, transfer := range b.transfers {
		if transfer.Sender == address || transfer.Recipient == address {
			result = append(result, transfer)
		}
	}
	return result
}

// GetPendingTransfers returns all pending transfers
func (b *BridgeIndexer) GetPendingTransfers() []*BridgeTransfer {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var result []*BridgeTransfer
	for _, transfer := range b.transfers {
		if transfer.Status == BridgeStatusPending {
			result = append(result, transfer)
		}
	}
	return result
}

// GetPendingByDestChain returns pending transfers for a destination chain
func (b *BridgeIndexer) GetPendingByDestChain(chain ChainID) []*BridgeTransfer {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var result []*BridgeTransfer
	for _, transfer := range b.pendingByChain[chain] {
		if transfer.Status == BridgeStatusPending {
			result = append(result, transfer)
		}
	}
	return result
}

// GetAssetMovements returns all asset movements
func (b *BridgeIndexer) GetAssetMovements() []*AssetMovement {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.movements
}

// GetMovementsByAddress returns movements for an address
func (b *BridgeIndexer) GetMovementsByAddress(address string) []*AssetMovement {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var result []*AssetMovement
	for _, movement := range b.movements {
		if movement.From == address || movement.To == address {
			result = append(result, movement)
		}
	}
	return result
}

// GetWarpMessage returns a warp message by ID
func (b *BridgeIndexer) GetWarpMessage(id string) (*WarpMessage, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	msg, ok := b.warpMessages[id]
	return msg, ok
}

// GetCrossChainSwap returns a cross-chain swap by ID
func (b *BridgeIndexer) GetCrossChainSwap(id string) (*CrossChainSwap, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	swap, ok := b.crossChainSwaps[id]
	return swap, ok
}

// GetStats returns bridge statistics
func (b *BridgeIndexer) GetStats() *BridgeStats {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.stats
}

// StartPendingMonitor starts monitoring pending transfers
func (b *BridgeIndexer) StartPendingMonitor(ctx context.Context, checkInterval time.Duration) {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.checkPendingTransfers()
		}
	}
}

// checkPendingTransfers checks for stale pending transfers
func (b *BridgeIndexer) checkPendingTransfers() {
	b.mu.Lock()
	defer b.mu.Unlock()

	staleThreshold := 1 * time.Hour

	for _, transfer := range b.transfers {
		if transfer.Status == BridgeStatusPending {
			if time.Since(transfer.InitiatedAt) > staleThreshold {
				// Mark as potentially failed
				transfer.Confirmations = 0
			}
		}
	}
}

// parseChainID parses a chain ID from a topic
func parseChainID(topic string) ChainID {
	// Parse chain ID from topic (last 4 bytes typically)
	if len(topic) < 10 {
		return ChainID(topic)
	}

	chainNum := new(big.Int).SetBytes(hexToBytes(topic[len(topic)-8:]))

	switch chainNum.Uint64() {
	case 96369:
		return ChainCMainnet
	case 96368:
		return ChainCTestnet
	case 200200:
		return ChainZoo
	case 200201:
		return ChainZooTest
	case 36963:
		return ChainHanzo
	default:
		return ChainID(fmt.Sprintf("unknown-%d", chainNum.Uint64()))
	}
}

// Note: hexToBytes is defined in amm.go as the canonical implementation
