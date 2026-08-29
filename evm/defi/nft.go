// Copyright (c) 2025 Lux Industries Inc
// SPDX-License-Identifier: MIT

package defi

import (
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// NFT Marketplace Event Signatures
const (
	// OpenSea Seaport v1.1-1.6
	SeaportOrderFulfilledSig   = "0x9d9af8e38d66c62e2c12f0225249fd9d721c54b83f48d9352c97c6cacdcb6f31" // OrderFulfilled(bytes32,address,address,address,(uint8,address,uint256,uint256)[],(uint8,address,uint256,uint256,address)[])
	SeaportOrderCancelledSig   = "0x6bacc01dbe442496068f7d234edd811f1a5f833243e0aec824f86ab861f3c90d" // OrderCancelled(bytes32,address,address)
	SeaportCounterIncrementSig = "0x721c20121297512b72821b97f5326877ea8ecf4bb9948fea5bfcb6453074d37f" // CounterIncremented(uint256,address)

	// LooksRare v1
	LooksRareTakerBidSig  = "0x95fb6205e23ff6bda16a2d1dba56b9ad7c783f67c96fa149785052f47696f2be" // TakerBid(bytes32,uint256,address,address,address,address,address,uint256,uint256,uint256)
	LooksRareTakerAskSig  = "0x68cd251d4d267c6e2034ff0088b990352b97b2002c0476587d0c4da889c11330" // TakerAsk(bytes32,uint256,address,address,address,address,address,uint256,uint256,uint256)
	LooksRareCancelAllSig = "0x1e7178d84f0b0825c65795cd62e7972809ad3aac6917843aaec596161b2c0a97" // CancelAllOrders(address,uint256)

	// LooksRare v2
	LooksRareV2TakerBidSig = "0x7210a3464c2e4bd240e8c68c530b612facbad1018cafa196c32f5586cc507e6d" // TakerBid((bytes32,uint256,address,address,bool,address,address,uint256,uint256,uint256,uint256[],(bytes32,bytes)[]))
	LooksRareV2TakerAskSig = "0x164ca3531068a3c88bf6636b77b918526f07cccef086d64a421b01ef981f1a12" // TakerAsk((bytes32,uint256,address,address,bool,address,address,uint256,uint256,uint256,uint256[],(bytes32,bytes)[]))

	// Blur
	BlurOrdersMatchedSig    = "0x79d80fb5f83ee5a12bb600e1e37e3f29a1918f131e02a604f1c5b7bac2f90789" // OrdersMatched(address,address,(address,uint8,address,address,uint256,uint256,address,uint256,uint256,uint256,(uint16,address)[],uint256,bytes),(address,uint8,address,address,uint256,uint256,address,uint256,uint256,uint256,(uint16,address)[],uint256,bytes))
	BlurOrderCancelledSig   = "0x5152abf959f6564662358c2e52b702259b78bac5ee7842a0f01937e670efcc7d" // OrderCancelled(bytes32)
	BlurNonceIncrementedSig = "0xa82a649bbd060c9099cd7b7326e2b0dc9e9af0836480e0f849dc9eaa79710b3b" // NonceIncremented(address,uint256)

	// Rarible
	RaribleMatchSig  = "0xbca288b70254aacc77da91555993116eb291e093f60774fa231663630c2198d2" // Match(bytes32,bytes32,address,address,uint256,bytes32)
	RaribleCancelSig = "0xe8d9861dbc9c663ed3accd261bbe2fe01e0d3d9e5f51fa38523b265c7757a93a" // Cancel(bytes32)
	// UNVERIFIED. The comment names LibPart.Part[], a struct — an event topic hashes
	// the CANONICAL signature, where a struct is written out as a tuple, so this
	// value cannot be derived from the text beside it and nothing here has
	// checked it against a chain. Expand LibPart.Part[] from the ABI, then let the
	// test derive it like the others.
	RaribleRoyaltiesSig = "0xff6a5e6f64e0de7cc82a417a0c1dc9d1b3a295c4f4de1c6e2a2c2db9f0e0e0e1" // RoyaltiesSet(address,uint256,LibPart.Part[])

	// X2Y2
	// UNVERIFIED. The comment names Item, a struct — an event topic hashes
	// the CANONICAL signature, where a struct is written out as a tuple, so this
	// value cannot be derived from the text beside it and nothing here has
	// checked it against a chain. Expand Item from the ABI, then let the
	// test derive it like the others.
	X2Y2InventorySig = "0x3cbb63f144840e5b1b0a38a7c19211d2e89de4d7c5faf8b2d3c1776c302d1d33" // EvInventory(bytes32,address,address,uint256,uint256,uint256,uint256,address,bytes,Item,Item)
	X2Y2CancelSig    = "0x5b0b06d07e20243724d90e17a20034972f339eb28bd1c9437a71999bd15a1e7a" // EvCancel(bytes32)

	// Foundation
	FoundationBuySig     = "0x26ea3ebbda62eb1baef13e1c237dddd956c87f80b2801f2616d806d52557b121" // ReserveAuctionBidPlaced(uint256,address,uint256,uint256)
	FoundationSettledSig = "0x2edb0e99c6ac35be6731dab554c1d1fa1b7beb675090dbb09fb14e615aca1c4a" // ReserveAuctionFinalized(uint256,address,address,uint256,uint256,uint256)

	// SuperRare
	SuperRareSoldSig  = "0x16dd16959a056953a63cf14bf427881e762e54f03d86b864efea8238dd3b822f" // Sold(address,address,uint256,uint256)
	SuperRareOfferSig = "0xa6da490df6e8f0e592e795b4d96c5810d8ddfda51c21577adbdd8e1a0fa47a67" // OfferAccepted(address,address,uint256,uint256)

	// Zora
	ZoraAskFilledSig = "0x02c4ab12f311049a3dadebc225cafb3bb99acfeb062424d088b109822d656e94" // AskFilled(address,address,address,uint256,uint256,address,address,uint256)
	// UNVERIFIED. The comment names Ask, a struct — an event topic hashes
	// the CANONICAL signature, where a struct is written out as a tuple, so this
	// value cannot be derived from the text beside it and nothing here has
	// checked it against a chain. Expand Ask from the ABI, then let the
	// test derive it like the others.
	ZoraAskCreatedSig   = "0x5b8e2b9f74e3a1a4e2b9f5a7c3e9f5a7c3e9f5a7c3e9f5a7c3e9f5a7c3e9f5a7" // AskCreated(address,uint256,Ask)
	ZoraAskCancelledSig = "0x092298486c005c84902b27e156d6ef143f2b07583141f0e83607b4ddbbaea3f5" // AskCancelled(address,uint256)
)

// NFTMarketplaceProtocol identifies the marketplace protocol
type NFTMarketplaceProtocol string

const (
	ProtocolSeaport     NFTMarketplaceProtocol = "seaport"
	ProtocolLooksRare   NFTMarketplaceProtocol = "looksrare"
	ProtocolLooksRareV2 NFTMarketplaceProtocol = "looksrare_v2"
	ProtocolBlur        NFTMarketplaceProtocol = "blur"
	ProtocolRarible     NFTMarketplaceProtocol = "rarible"
	ProtocolX2Y2        NFTMarketplaceProtocol = "x2y2"
	ProtocolFoundation  NFTMarketplaceProtocol = "foundation"
	ProtocolSuperRare   NFTMarketplaceProtocol = "superrare"
	ProtocolZora        NFTMarketplaceProtocol = "zora"
	ProtocolCustom      NFTMarketplaceProtocol = "custom"
)

// NFTOrderStatus represents the status of an NFT marketplace order
type NFTOrderStatus string

const (
	NFTOrderStatusActive    NFTOrderStatus = "active"
	NFTOrderStatusFilled    NFTOrderStatus = "filled"
	NFTOrderStatusCancelled NFTOrderStatus = "cancelled"
	NFTOrderStatusExpired   NFTOrderStatus = "expired"
	NFTOrderStatusInvalid   NFTOrderStatus = "invalid"
)

// NFTOrderType represents the type of order
type NFTOrderType string

const (
	OrderTypeListing NFTOrderType = "listing" // Seller listing NFT for sale
	OrderTypeBid     NFTOrderType = "bid"     // Buyer bidding on NFT
	OrderTypeOffer   NFTOrderType = "offer"   // Collection-wide or trait-specific offer
	OrderTypeAuction NFTOrderType = "auction" // Auction listing
)

// NFTOrder represents an NFT marketplace order (listing, bid, or offer)
type NFTOrder struct {
	ID            string                 `json:"id"`                  // Unique order ID
	OrderHash     string                 `json:"orderHash"`           // Order hash from protocol
	Protocol      NFTMarketplaceProtocol `json:"protocol"`            // Marketplace protocol
	OrderType     NFTOrderType           `json:"orderType"`           // listing, bid, offer, auction
	Status        NFTOrderStatus         `json:"status"`              // active, filled, cancelled, expired
	Maker         string                 `json:"maker"`               // Order creator
	Taker         string                 `json:"taker,omitempty"`     // Order filler (for filled orders)
	Collection    string                 `json:"collection"`          // NFT collection address
	TokenID       *big.Int               `json:"tokenId,omitempty"`   // Token ID (nil for collection offers)
	TokenStandard string                 `json:"tokenStandard"`       // ERC721, ERC1155
	Quantity      uint64                 `json:"quantity"`            // Amount (for ERC1155)
	Price         *big.Int               `json:"price"`               // Price in payment token
	PaymentToken  string                 `json:"paymentToken"`        // Payment token address (0x0 for native)
	StartTime     time.Time              `json:"startTime"`           // Order start time
	EndTime       time.Time              `json:"endTime"`             // Order expiration
	Salt          *big.Int               `json:"salt,omitempty"`      // Order salt/nonce
	Zone          string                 `json:"zone,omitempty"`      // Seaport zone
	Conduit       string                 `json:"conduit,omitempty"`   // Seaport conduit
	Fees          []*OrderFee            `json:"fees,omitempty"`      // Protocol fees, royalties
	Signature     string                 `json:"signature,omitempty"` // Order signature
	RawOrder      string                 `json:"rawOrder,omitempty"`  // Original order data
	BlockNumber   uint64                 `json:"blockNumber"`
	TxHash        string                 `json:"txHash"`
	LogIndex      uint64                 `json:"logIndex"`
	Timestamp     time.Time              `json:"timestamp"`
	CreatedAt     time.Time              `json:"createdAt"`
	UpdatedAt     time.Time              `json:"updatedAt"`
}

// OrderFee represents a fee attached to an order
type OrderFee struct {
	Recipient   string   `json:"recipient"`
	Amount      *big.Int `json:"amount"`
	BasisPoints uint16   `json:"basisPoints,omitempty"`
	FeeType     string   `json:"feeType"` // "protocol", "royalty", "platform"
}

// NFTSale represents a completed NFT sale
type NFTSale struct {
	ID             string                 `json:"id"`
	OrderHash      string                 `json:"orderHash,omitempty"`
	Protocol       NFTMarketplaceProtocol `json:"protocol"`
	Collection     string                 `json:"collection"`
	TokenID        *big.Int               `json:"tokenId"`
	TokenStandard  string                 `json:"tokenStandard"`
	Quantity       uint64                 `json:"quantity"`
	Seller         string                 `json:"seller"`
	Buyer          string                 `json:"buyer"`
	Price          *big.Int               `json:"price"`
	PaymentToken   string                 `json:"paymentToken"`
	PriceUSD       *big.Float             `json:"priceUsd,omitempty"` // USD value at time of sale
	Fees           []*OrderFee            `json:"fees,omitempty"`
	FeeTotal       *big.Int               `json:"feeTotal"`
	RoyaltyTotal   *big.Int               `json:"royaltyTotal"`
	SellerReceived *big.Int               `json:"sellerReceived"`
	BlockNumber    uint64                 `json:"blockNumber"`
	TxHash         string                 `json:"txHash"`
	LogIndex       uint64                 `json:"logIndex"`
	Timestamp      time.Time              `json:"timestamp"`
}

// NFTCollection represents collection-level marketplace data
type NFTCollection struct {
	Address          string    `json:"address"`
	Name             string    `json:"name,omitempty"`
	Symbol           string    `json:"symbol,omitempty"`
	TokenStandard    string    `json:"tokenStandard"`
	TotalSupply      uint64    `json:"totalSupply,omitempty"`
	FloorPrice       *big.Int  `json:"floorPrice"` // Current floor price
	FloorToken       string    `json:"floorToken"` // Payment token for floor
	Royalty          uint16    `json:"royalty"`    // Royalty in basis points
	RoyaltyRecipient string    `json:"royaltyRecipient,omitempty"`
	Volume24h        *big.Int  `json:"volume24h"`
	Volume7d         *big.Int  `json:"volume7d"`
	VolumeTotal      *big.Int  `json:"volumeTotal"`
	Sales24h         uint64    `json:"sales24h"`
	Sales7d          uint64    `json:"sales7d"`
	SalesTotal       uint64    `json:"salesTotal"`
	ListingCount     uint64    `json:"listingCount"` // Active listings
	OwnerCount       uint64    `json:"ownerCount"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// NFTMarketplaceStats contains marketplace-wide statistics
type NFTMarketplaceStats struct {
	TotalVolume      *big.Int            `json:"totalVolume"`
	Volume24h        *big.Int            `json:"volume24h"`
	Volume7d         *big.Int            `json:"volume7d"`
	TotalSales       uint64              `json:"totalSales"`
	Sales24h         uint64              `json:"sales24h"`
	Sales7d          uint64              `json:"sales7d"`
	ActiveListings   uint64              `json:"activeListings"`
	ActiveBids       uint64              `json:"activeBids"`
	UniqueTraders    uint64              `json:"uniqueTraders"`
	EventsByType     map[string]uint64   `json:"eventsByType"`
	VolumeByProtocol map[string]*big.Int `json:"volumeByProtocol"`
	LastBlockIndexed uint64              `json:"lastBlockIndexed"`
	LastIndexTime    time.Time           `json:"lastIndexTime"`
}

// NFTMarketplaceIndexer indexes NFT marketplace events across protocols
type NFTMarketplaceIndexer struct {
	mu sync.RWMutex

	// In-memory indexes
	orders      map[string]*NFTOrder // orderHash -> order
	sales       []*NFTSale
	collections map[string]*NFTCollection // address -> collection

	// Callback hooks
	onOrder func(*NFTOrder)
	onSale  func(*NFTSale)

	// Stats
	stats *NFTMarketplaceStats
}

// NewNFTMarketplaceIndexer creates a new NFT marketplace indexer
func NewNFTMarketplaceIndexer() *NFTMarketplaceIndexer {
	return &NFTMarketplaceIndexer{
		orders:      make(map[string]*NFTOrder),
		sales:       make([]*NFTSale, 0),
		collections: make(map[string]*NFTCollection),
		stats: &NFTMarketplaceStats{
			TotalVolume:      big.NewInt(0),
			Volume24h:        big.NewInt(0),
			Volume7d:         big.NewInt(0),
			EventsByType:     make(map[string]uint64),
			VolumeByProtocol: make(map[string]*big.Int),
		},
	}
}

// SetCallbacks sets event callbacks
func (n *NFTMarketplaceIndexer) SetCallbacks(onOrder func(*NFTOrder), onSale func(*NFTSale)) {
	n.onOrder = onOrder
	n.onSale = onSale
}

// IndexLog processes a log entry for NFT marketplace events
func (n *NFTMarketplaceIndexer) IndexLog(log *LogEntry) error {
	if len(log.Topics) == 0 {
		return nil
	}

	topic0 := log.Topics[0]

	n.mu.Lock()
	n.stats.EventsByType[topic0]++
	n.stats.LastBlockIndexed = log.BlockNumber
	n.stats.LastIndexTime = time.Now()
	n.mu.Unlock()

	switch topic0 {
	// OpenSea Seaport
	case SeaportOrderFulfilledSig:
		return n.indexSeaportOrderFulfilled(log)
	case SeaportOrderCancelledSig:
		return n.indexSeaportOrderCancelled(log)

	// LooksRare v1
	case LooksRareTakerBidSig:
		return n.indexLooksRareTakerBid(log)
	case LooksRareTakerAskSig:
		return n.indexLooksRareTakerAsk(log)
	case LooksRareCancelAllSig:
		return n.indexLooksRareCancelAll(log)

	// LooksRare v2
	case LooksRareV2TakerBidSig:
		return n.indexLooksRareV2TakerBid(log)
	case LooksRareV2TakerAskSig:
		return n.indexLooksRareV2TakerAsk(log)

	// Blur
	case BlurOrdersMatchedSig:
		return n.indexBlurOrdersMatched(log)
	case BlurOrderCancelledSig:
		return n.indexBlurOrderCancelled(log)

	// Rarible
	case RaribleMatchSig:
		return n.indexRaribleMatch(log)
	case RaribleCancelSig:
		return n.indexRaribleCancel(log)

	// X2Y2
	case X2Y2InventorySig:
		return n.indexX2Y2Inventory(log)
	case X2Y2CancelSig:
		return n.indexX2Y2Cancel(log)

	// Zora
	case ZoraAskFilledSig:
		return n.indexZoraAskFilled(log)
	case ZoraAskCancelledSig:
		return n.indexZoraAskCancelled(log)
	}

	return nil
}

// =============================================================================
// Seaport Indexing
// =============================================================================

func (n *NFTMarketplaceIndexer) indexSeaportOrderFulfilled(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 128 {
		return fmt.Errorf("invalid seaport OrderFulfilled log")
	}

	// Topics: orderHash, offerer, zone
	orderHash := log.Topics[1]
	offerer := "0x" + log.Topics[2][26:]

	// Parse consideration and offer items from data
	// Seaport OrderFulfilled data is complex ABI-encoded
	// offer[] and consideration[] arrays

	sale := &NFTSale{
		ID:           fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		OrderHash:    orderHash,
		Protocol:     ProtocolSeaport,
		Seller:       offerer,
		BlockNumber:  log.BlockNumber,
		TxHash:       log.TxHash,
		LogIndex:     log.LogIndex,
		Timestamp:    log.Timestamp,
		Quantity:     1,
		FeeTotal:     big.NewInt(0),
		RoyaltyTotal: big.NewInt(0),
	}

	// Decode offer and consideration from data
	// This is a simplified version - full decoding requires ABI parsing
	if len(log.Data) >= 256 {
		// Attempt to extract collection and price from data
		// Data layout varies based on order type
		sale.Price = new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))
	}

	n.addSale(sale)
	return nil
}

func (n *NFTMarketplaceIndexer) indexSeaportOrderCancelled(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid seaport OrderCancelled log")
	}

	orderHash := log.Topics[1]
	offerer := "0x" + log.Topics[2][26:]

	n.mu.Lock()
	if order, ok := n.orders[orderHash]; ok {
		order.Status = NFTOrderStatusCancelled
		order.UpdatedAt = log.Timestamp
	}
	n.mu.Unlock()

	// Create cancelled order record if not exists
	order := &NFTOrder{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		OrderHash:   orderHash,
		Protocol:    ProtocolSeaport,
		Status:      NFTOrderStatusCancelled,
		Maker:       offerer,
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		LogIndex:    log.LogIndex,
		Timestamp:   log.Timestamp,
		UpdatedAt:   log.Timestamp,
	}

	n.addOrder(order)
	return nil
}

// =============================================================================
// LooksRare Indexing
// =============================================================================

func (n *NFTMarketplaceIndexer) indexLooksRareTakerBid(log *LogEntry) error {
	// TakerBid: buyer accepts a listing (seller's ask)
	return n.indexLooksRareTrade(log, ProtocolLooksRare, "taker_bid")
}

func (n *NFTMarketplaceIndexer) indexLooksRareTakerAsk(log *LogEntry) error {
	// TakerAsk: seller accepts a bid (buyer's offer)
	return n.indexLooksRareTrade(log, ProtocolLooksRare, "taker_ask")
}

func (n *NFTMarketplaceIndexer) indexLooksRareTrade(log *LogEntry, protocol NFTMarketplaceProtocol, tradeType string) error {
	if len(log.Topics) < 2 || len(log.Data) < 320 {
		return fmt.Errorf("invalid LooksRare trade log")
	}

	orderHash := log.Topics[1]

	// Data: orderNonce, taker, maker, strategy, currency, collection, tokenId, amount, price
	taker := "0x" + log.Data[24:64]
	maker := "0x" + log.Data[88:128]
	currency := "0x" + log.Data[152:192]
	collection := "0x" + log.Data[216:256]
	tokenID := new(big.Int).SetBytes(hexToBytes(log.Data[256:320]))
	amount := new(big.Int).SetBytes(hexToBytes(log.Data[320:384]))
	price := new(big.Int).SetBytes(hexToBytes(log.Data[384:448]))

	var seller, buyer string
	if tradeType == "taker_bid" {
		seller = maker
		buyer = taker
	} else {
		seller = taker
		buyer = maker
	}

	sale := &NFTSale{
		ID:           fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		OrderHash:    orderHash,
		Protocol:     protocol,
		Collection:   collection,
		TokenID:      tokenID,
		Quantity:     amount.Uint64(),
		Seller:       seller,
		Buyer:        buyer,
		Price:        price,
		PaymentToken: currency,
		FeeTotal:     big.NewInt(0),
		RoyaltyTotal: big.NewInt(0),
		BlockNumber:  log.BlockNumber,
		TxHash:       log.TxHash,
		LogIndex:     log.LogIndex,
		Timestamp:    log.Timestamp,
	}

	n.addSale(sale)
	return nil
}

func (n *NFTMarketplaceIndexer) indexLooksRareCancelAll(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return nil
	}
	// User cancelled all orders - mark in stats
	n.mu.Lock()
	n.stats.EventsByType["cancel_all"]++
	n.mu.Unlock()
	return nil
}

func (n *NFTMarketplaceIndexer) indexLooksRareV2TakerBid(log *LogEntry) error {
	return n.indexLooksRareTrade(log, ProtocolLooksRareV2, "taker_bid")
}

func (n *NFTMarketplaceIndexer) indexLooksRareV2TakerAsk(log *LogEntry) error {
	return n.indexLooksRareTrade(log, ProtocolLooksRareV2, "taker_ask")
}

// =============================================================================
// Blur Indexing
// =============================================================================

func (n *NFTMarketplaceIndexer) indexBlurOrdersMatched(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 256 {
		return fmt.Errorf("invalid Blur OrdersMatched log")
	}

	// Topics: maker, taker
	maker := "0x" + log.Topics[1][26:]
	taker := "0x" + log.Topics[2][26:]

	// Parse sell and buy orders from data
	// Complex struct decoding required for full implementation

	sale := &NFTSale{
		ID:           fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		Protocol:     ProtocolBlur,
		Seller:       maker,
		Buyer:        taker,
		Quantity:     1,
		FeeTotal:     big.NewInt(0),
		RoyaltyTotal: big.NewInt(0),
		BlockNumber:  log.BlockNumber,
		TxHash:       log.TxHash,
		LogIndex:     log.LogIndex,
		Timestamp:    log.Timestamp,
	}

	// Extract price and collection from data if available
	if len(log.Data) >= 128 {
		sale.Price = new(big.Int).SetBytes(hexToBytes(log.Data[64:128]))
	}

	n.addSale(sale)
	return nil
}

func (n *NFTMarketplaceIndexer) indexBlurOrderCancelled(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return nil
	}
	orderHash := log.Topics[1]

	n.mu.Lock()
	if order, ok := n.orders[orderHash]; ok {
		order.Status = NFTOrderStatusCancelled
		order.UpdatedAt = log.Timestamp
	}
	n.mu.Unlock()
	return nil
}

// =============================================================================
// Rarible Indexing
// =============================================================================

func (n *NFTMarketplaceIndexer) indexRaribleMatch(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 128 {
		return fmt.Errorf("invalid Rarible Match log")
	}

	leftHash := log.Topics[1]
	rightHash := log.Topics[2]

	leftMaker := "0x" + log.Data[24:64]
	rightMaker := "0x" + log.Data[88:128]

	// Determine buyer/seller based on order types
	sale := &NFTSale{
		ID:           fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		OrderHash:    leftHash + "-" + rightHash,
		Protocol:     ProtocolRarible,
		Seller:       leftMaker,
		Buyer:        rightMaker,
		Quantity:     1,
		FeeTotal:     big.NewInt(0),
		RoyaltyTotal: big.NewInt(0),
		BlockNumber:  log.BlockNumber,
		TxHash:       log.TxHash,
		LogIndex:     log.LogIndex,
		Timestamp:    log.Timestamp,
	}

	n.addSale(sale)
	return nil
}

func (n *NFTMarketplaceIndexer) indexRaribleCancel(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return nil
	}
	orderHash := log.Topics[1]

	n.mu.Lock()
	if order, ok := n.orders[orderHash]; ok {
		order.Status = NFTOrderStatusCancelled
		order.UpdatedAt = log.Timestamp
	}
	n.mu.Unlock()
	return nil
}

// =============================================================================
// X2Y2 Indexing
// =============================================================================

func (n *NFTMarketplaceIndexer) indexX2Y2Inventory(log *LogEntry) error {
	if len(log.Topics) < 2 || len(log.Data) < 256 {
		return fmt.Errorf("invalid X2Y2 Inventory log")
	}

	itemHash := log.Topics[1]

	// Parse maker, taker, price from data
	maker := "0x" + log.Data[24:64]
	taker := "0x" + log.Data[88:128]
	price := new(big.Int).SetBytes(hexToBytes(log.Data[128:192]))

	sale := &NFTSale{
		ID:           fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		OrderHash:    itemHash,
		Protocol:     ProtocolX2Y2,
		Seller:       maker,
		Buyer:        taker,
		Price:        price,
		Quantity:     1,
		FeeTotal:     big.NewInt(0),
		RoyaltyTotal: big.NewInt(0),
		BlockNumber:  log.BlockNumber,
		TxHash:       log.TxHash,
		LogIndex:     log.LogIndex,
		Timestamp:    log.Timestamp,
	}

	n.addSale(sale)
	return nil
}

func (n *NFTMarketplaceIndexer) indexX2Y2Cancel(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return nil
	}
	orderHash := log.Topics[1]

	n.mu.Lock()
	if order, ok := n.orders[orderHash]; ok {
		order.Status = NFTOrderStatusCancelled
		order.UpdatedAt = log.Timestamp
	}
	n.mu.Unlock()
	return nil
}

// =============================================================================
// Zora Indexing
// =============================================================================

func (n *NFTMarketplaceIndexer) indexZoraAskFilled(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 192 {
		return fmt.Errorf("invalid Zora AskFilled log")
	}

	collection := "0x" + log.Topics[1][26:]
	tokenID := new(big.Int).SetBytes(hexToBytes(log.Topics[2]))

	// Data: buyer, finder, price
	buyer := "0x" + log.Data[24:64]
	price := new(big.Int).SetBytes(hexToBytes(log.Data[128:192]))

	sale := &NFTSale{
		ID:           fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		Protocol:     ProtocolZora,
		Collection:   collection,
		TokenID:      tokenID,
		Buyer:        buyer,
		Price:        price,
		Quantity:     1,
		FeeTotal:     big.NewInt(0),
		RoyaltyTotal: big.NewInt(0),
		BlockNumber:  log.BlockNumber,
		TxHash:       log.TxHash,
		LogIndex:     log.LogIndex,
		Timestamp:    log.Timestamp,
	}

	n.addSale(sale)
	return nil
}

func (n *NFTMarketplaceIndexer) indexZoraAskCancelled(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return nil
	}
	collection := "0x" + log.Topics[1][26:]
	tokenID := new(big.Int).SetBytes(hexToBytes(log.Topics[2]))

	orderKey := fmt.Sprintf("%s-%s", collection, tokenID.String())

	n.mu.Lock()
	if order, ok := n.orders[orderKey]; ok {
		order.Status = NFTOrderStatusCancelled
		order.UpdatedAt = log.Timestamp
	}
	n.mu.Unlock()
	return nil
}

// =============================================================================
// Helper Methods
// =============================================================================

func (n *NFTMarketplaceIndexer) addSale(sale *NFTSale) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.sales = append(n.sales, sale)

	// Update stats
	n.stats.TotalSales++
	if sale.Price != nil {
		n.stats.TotalVolume.Add(n.stats.TotalVolume, sale.Price)

		protocol := string(sale.Protocol)
		if n.stats.VolumeByProtocol[protocol] == nil {
			n.stats.VolumeByProtocol[protocol] = big.NewInt(0)
		}
		n.stats.VolumeByProtocol[protocol].Add(n.stats.VolumeByProtocol[protocol], sale.Price)
	}

	// Update collection stats
	if sale.Collection != "" {
		collection := n.getOrCreateCollection(sale.Collection)
		collection.SalesTotal++
		if sale.Price != nil {
			collection.VolumeTotal.Add(collection.VolumeTotal, sale.Price)
		}
		collection.UpdatedAt = sale.Timestamp
	}

	// Callback
	if n.onSale != nil {
		n.onSale(sale)
	}
}

func (n *NFTMarketplaceIndexer) addOrder(order *NFTOrder) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.orders[order.OrderHash] = order

	// Update stats
	if order.Status == NFTOrderStatusActive {
		if order.OrderType == OrderTypeListing {
			n.stats.ActiveListings++
		} else if order.OrderType == OrderTypeBid {
			n.stats.ActiveBids++
		}
	}

	// Callback
	if n.onOrder != nil {
		n.onOrder(order)
	}
}

func (n *NFTMarketplaceIndexer) getOrCreateCollection(address string) *NFTCollection {
	addr := strings.ToLower(address)
	if collection, ok := n.collections[addr]; ok {
		return collection
	}

	collection := &NFTCollection{
		Address:     addr,
		FloorPrice:  big.NewInt(0),
		Volume24h:   big.NewInt(0),
		Volume7d:    big.NewInt(0),
		VolumeTotal: big.NewInt(0),
		UpdatedAt:   time.Now(),
	}
	n.collections[addr] = collection
	return collection
}

// =============================================================================
// Query Methods
// =============================================================================

// GetSales returns recent sales
func (n *NFTMarketplaceIndexer) GetSales(limit int) []*NFTSale {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if limit <= 0 || limit > len(n.sales) {
		limit = len(n.sales)
	}

	start := len(n.sales) - limit
	if start < 0 {
		start = 0
	}

	result := make([]*NFTSale, limit)
	copy(result, n.sales[start:])
	return result
}

// GetSalesByCollection returns sales for a collection
func (n *NFTMarketplaceIndexer) GetSalesByCollection(collection string, limit int) []*NFTSale {
	n.mu.RLock()
	defer n.mu.RUnlock()

	addr := strings.ToLower(collection)
	result := make([]*NFTSale, 0, limit)

	for i := len(n.sales) - 1; i >= 0 && len(result) < limit; i-- {
		if strings.ToLower(n.sales[i].Collection) == addr {
			result = append(result, n.sales[i])
		}
	}
	return result
}

// GetOrder returns an order by hash
func (n *NFTMarketplaceIndexer) GetOrder(orderHash string) *NFTOrder {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.orders[orderHash]
}

// GetActiveOrders returns active orders for a collection
func (n *NFTMarketplaceIndexer) GetActiveOrders(collection string, orderType NFTOrderType, limit int) []*NFTOrder {
	n.mu.RLock()
	defer n.mu.RUnlock()

	addr := strings.ToLower(collection)
	result := make([]*NFTOrder, 0, limit)

	for _, order := range n.orders {
		if order.Status != NFTOrderStatusActive {
			continue
		}
		if strings.ToLower(order.Collection) != addr {
			continue
		}
		if orderType != "" && order.OrderType != orderType {
			continue
		}
		result = append(result, order)
		if len(result) >= limit {
			break
		}
	}
	return result
}

// GetCollection returns collection stats
func (n *NFTMarketplaceIndexer) GetCollection(address string) *NFTCollection {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.collections[strings.ToLower(address)]
}

// GetTopCollections returns top collections by volume
func (n *NFTMarketplaceIndexer) GetTopCollections(limit int) []*NFTCollection {
	n.mu.RLock()
	defer n.mu.RUnlock()

	collections := make([]*NFTCollection, 0, len(n.collections))
	for _, c := range n.collections {
		collections = append(collections, c)
	}

	// Sort by volume (simple bubble sort for now)
	for i := 0; i < len(collections)-1; i++ {
		for j := i + 1; j < len(collections); j++ {
			if collections[j].VolumeTotal.Cmp(collections[i].VolumeTotal) > 0 {
				collections[i], collections[j] = collections[j], collections[i]
			}
		}
	}

	if limit > len(collections) {
		limit = len(collections)
	}
	return collections[:limit]
}

// GetStats returns marketplace statistics
func (n *NFTMarketplaceIndexer) GetStats() *NFTMarketplaceStats {
	n.mu.RLock()
	defer n.mu.RUnlock()

	// Clone stats
	return &NFTMarketplaceStats{
		TotalVolume:      new(big.Int).Set(n.stats.TotalVolume),
		Volume24h:        new(big.Int).Set(n.stats.Volume24h),
		Volume7d:         new(big.Int).Set(n.stats.Volume7d),
		TotalSales:       n.stats.TotalSales,
		Sales24h:         n.stats.Sales24h,
		Sales7d:          n.stats.Sales7d,
		ActiveListings:   n.stats.ActiveListings,
		ActiveBids:       n.stats.ActiveBids,
		UniqueTraders:    n.stats.UniqueTraders,
		EventsByType:     n.stats.EventsByType,
		VolumeByProtocol: n.stats.VolumeByProtocol,
		LastBlockIndexed: n.stats.LastBlockIndexed,
		LastIndexTime:    n.stats.LastIndexTime,
	}
}

// Note: hexToBytes is defined in amm.go
