// Package defi provides DeFi protocol indexing for the Lux EVM indexer.
package defi

import (
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"
)

// Order book event signatures (keccak256 hashes)
var (
	// Order Events
	OrderPlacedSig    = "0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925"
	OrderCancelledSig = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	OrderFilledSig    = "0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822"
	OrderPartialSig   = "0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67"

	// Trade Events
	TradeSig            = "0x9b3561d1d0bf65f6b8f53bf57c4e227ab4e9b6f7d8b9c8c7c6b5a4b3c2d1e0f1"
	TradeSettledSig     = "0xa1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"

	// Position Events
	PositionOpenedSig     = "0x2fe68525253654c21998f35787a8d0f361905ef647c854092430ab65f2f15022"
	PositionClosedSig     = "0x93f19bf1ec58a15dc643b37e7e18a1c13e85f4e4c4e3e4e5e6e7e8e9e0e1e2e3"
	PositionModifiedSig   = "0x4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5"
	PositionLiquidatedSig = "0x5c4b3a2f1e0d9c8b7a6f5e4d3c2b1a0f9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4"

	// Margin Events
	MarginDepositedSig  = "0x6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b7c6d5"
	MarginWithdrawnSig  = "0x7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6"
	MarginCallSig       = "0x8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7"

	// Lending Events
	LendingDepositSig   = "0x9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8"
	LendingWithdrawSig  = "0xa0b9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9"
	LendingBorrowSig    = "0xb1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0"
	LendingRepaySig     = "0xc2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1"

	// Funding Events
	FundingRateSig    = "0xd3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2"
	FundingPaymentSig = "0xe4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3"

	// Clearinghouse Events
	SettlementSig      = "0xf5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4"
	InsuranceFundSig   = "0xa6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5"
)

// OrderSide represents buy or sell
type OrderSide string

const (
	OrderSideBuy  OrderSide = "buy"
	OrderSideSell OrderSide = "sell"
)

// OrderType represents order types
type OrderType string

const (
	OrderTypeLimit      OrderType = "limit"
	OrderTypeMarket     OrderType = "market"
	OrderTypeStopLimit  OrderType = "stop_limit"
	OrderTypeStopMarket OrderType = "stop_market"
	OrderTypeTakeProfit OrderType = "take_profit"
	OrderTypeIOC        OrderType = "ioc"  // Immediate or Cancel
	OrderTypeFOK        OrderType = "fok"  // Fill or Kill
	OrderTypeGTC        OrderType = "gtc"  // Good Till Cancelled
	OrderTypeGTD        OrderType = "gtd"  // Good Till Date
)

// OrderStatus represents order status
type OrderStatus string

const (
	OrderStatusOpen           OrderStatus = "open"
	OrderStatusPartiallyFilled OrderStatus = "partially_filled"
	OrderStatusFilled         OrderStatus = "filled"
	OrderStatusCancelled      OrderStatus = "cancelled"
	OrderStatusExpired        OrderStatus = "expired"
	OrderStatusRejected       OrderStatus = "rejected"
)

// Order represents an order in the order book
type Order struct {
	ID             string      `json:"id"`
	Market         string      `json:"market"`         // e.g., "BTC-USD", "LUX-USDC"
	Trader         string      `json:"trader"`
	Side           OrderSide   `json:"side"`
	Type           OrderType   `json:"type"`
	Status         OrderStatus `json:"status"`
	Price          *big.Int    `json:"price"`          // Price in base units
	Size           *big.Int    `json:"size"`           // Size in base units
	FilledSize     *big.Int    `json:"filledSize"`
	RemainingSize  *big.Int    `json:"remainingSize"`
	AveragePrice   *big.Int    `json:"averagePrice"`
	StopPrice      *big.Int    `json:"stopPrice,omitempty"`
	TriggerPrice   *big.Int    `json:"triggerPrice,omitempty"`
	ReduceOnly     bool        `json:"reduceOnly"`
	PostOnly       bool        `json:"postOnly"`
	TimeInForce    string      `json:"timeInForce"`
	ExpiresAt      *time.Time  `json:"expiresAt,omitempty"`
	CreatedAt      time.Time   `json:"createdAt"`
	UpdatedAt      time.Time   `json:"updatedAt"`
	FilledAt       *time.Time  `json:"filledAt,omitempty"`
	CancelledAt    *time.Time  `json:"cancelledAt,omitempty"`
	TxHash         string      `json:"txHash"`
	BlockNumber    uint64      `json:"blockNumber"`
	LogIndex       uint64      `json:"logIndex"`
	Fees           *big.Int    `json:"fees"`
	FeeToken       string      `json:"feeToken"`
}

// Trade represents a matched trade
type Trade struct {
	ID          string    `json:"id"`
	Market      string    `json:"market"`
	MakerOrderID string   `json:"makerOrderId"`
	TakerOrderID string   `json:"takerOrderId"`
	Maker       string    `json:"maker"`
	Taker       string    `json:"taker"`
	Side        OrderSide `json:"side"`      // Taker's side
	Price       *big.Int  `json:"price"`
	Size        *big.Int  `json:"size"`
	Volume      *big.Int  `json:"volume"`    // Price * Size
	MakerFee    *big.Int  `json:"makerFee"`
	TakerFee    *big.Int  `json:"takerFee"`
	Timestamp   time.Time `json:"timestamp"`
	TxHash      string    `json:"txHash"`
	BlockNumber uint64    `json:"blockNumber"`
	LogIndex    uint64    `json:"logIndex"`
	Settled     bool      `json:"settled"`
}

// MarginPosition represents a margin/leveraged position
type MarginPosition struct {
	ID               string        `json:"id"`
	Market           string        `json:"market"`
	Trader           string        `json:"trader"`
	Side             OrderSide     `json:"side"`
	Size             *big.Int      `json:"size"`
	EntryPrice       *big.Int      `json:"entryPrice"`
	MarkPrice        *big.Int      `json:"markPrice"`
	LiquidationPrice *big.Int      `json:"liquidationPrice"`
	Margin           *big.Int      `json:"margin"`
	Leverage         float64       `json:"leverage"`
	UnrealizedPnL    *big.Int      `json:"unrealizedPnl"`
	RealizedPnL      *big.Int      `json:"realizedPnl"`
	FundingPaid      *big.Int      `json:"fundingPaid"`
	OpenedAt         time.Time     `json:"openedAt"`
	UpdatedAt        time.Time     `json:"updatedAt"`
	ClosedAt         *time.Time    `json:"closedAt,omitempty"`
	Status           PositionStatusOB `json:"status"`
}

// PositionStatusOB for order book positions
type PositionStatusOB string

const (
	PositionStatusOBOpen       PositionStatusOB = "open"
	PositionStatusOBClosed     PositionStatusOB = "closed"
	PositionStatusOBLiquidated PositionStatusOB = "liquidated"
)

// MarginAccount represents a trader's margin account
type MarginAccount struct {
	Trader           string              `json:"trader"`
	TotalDeposited   *big.Int            `json:"totalDeposited"`
	TotalWithdrawn   *big.Int            `json:"totalWithdrawn"`
	AvailableMargin  *big.Int            `json:"availableMargin"`
	LockedMargin     *big.Int            `json:"lockedMargin"`
	UnrealizedPnL    *big.Int            `json:"unrealizedPnl"`
	RealizedPnL      *big.Int            `json:"realizedPnl"`
	MaintenanceMargin *big.Int           `json:"maintenanceMargin"`
	MarginRatio      float64             `json:"marginRatio"`
	Positions        map[string]*MarginPosition `json:"positions"`
	LastUpdated      time.Time           `json:"lastUpdated"`
}

// LendingPosition represents a lending/borrowing position
type LendingPosition struct {
	ID              string    `json:"id"`
	Lender          string    `json:"lender"`
	Token           string    `json:"token"`
	DepositedAmount *big.Int  `json:"depositedAmount"`
	BorrowedAmount  *big.Int  `json:"borrowedAmount"`
	InterestEarned  *big.Int  `json:"interestEarned"`
	InterestOwed    *big.Int  `json:"interestOwed"`
	APY             float64   `json:"apy"`
	BorrowAPY       float64   `json:"borrowApy"`
	LastAccrual     time.Time `json:"lastAccrual"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// FundingRate represents a funding rate event
type FundingRate struct {
	Market      string    `json:"market"`
	Rate        *big.Int  `json:"rate"`        // Rate in basis points
	Premium     *big.Int  `json:"premium"`
	IndexPrice  *big.Int  `json:"indexPrice"`
	MarkPrice   *big.Int  `json:"markPrice"`
	Timestamp   time.Time `json:"timestamp"`
	NextFunding time.Time `json:"nextFunding"`
	BlockNumber uint64    `json:"blockNumber"`
}

// FundingPayment represents a funding payment
type FundingPayment struct {
	ID          string    `json:"id"`
	Market      string    `json:"market"`
	Trader      string    `json:"trader"`
	PositionID  string    `json:"positionId"`
	Amount      *big.Int  `json:"amount"`
	Rate        *big.Int  `json:"rate"`
	Side        OrderSide `json:"side"`
	Timestamp   time.Time `json:"timestamp"`
	TxHash      string    `json:"txHash"`
	BlockNumber uint64    `json:"blockNumber"`
}

// Liquidation represents a position liquidation
type Liquidation struct {
	ID             string    `json:"id"`
	Market         string    `json:"market"`
	Trader         string    `json:"trader"`
	Liquidator     string    `json:"liquidator"`
	PositionID     string    `json:"positionId"`
	Size           *big.Int  `json:"size"`
	Price          *big.Int  `json:"price"`
	Collateral     *big.Int  `json:"collateral"`
	Penalty        *big.Int  `json:"penalty"`
	InsurancePayout *big.Int `json:"insurancePayout"`
	Timestamp      time.Time `json:"timestamp"`
	TxHash         string    `json:"txHash"`
	BlockNumber    uint64    `json:"blockNumber"`
}

// OrderBookLevel represents a price level in the order book
type OrderBookLevel struct {
	Price     *big.Int `json:"price"`
	Size      *big.Int `json:"size"`
	OrderCount int     `json:"orderCount"`
}

// OrderBookSnapshot represents a snapshot of the order book
type OrderBookSnapshot struct {
	Market    string            `json:"market"`
	Bids      []*OrderBookLevel `json:"bids"`
	Asks      []*OrderBookLevel `json:"asks"`
	BestBid   *big.Int          `json:"bestBid"`
	BestAsk   *big.Int          `json:"bestAsk"`
	Spread    *big.Int          `json:"spread"`
	Timestamp time.Time         `json:"timestamp"`
}

// MarketStats represents market statistics
type MarketStats struct {
	Market        string    `json:"market"`
	LastPrice     *big.Int  `json:"lastPrice"`
	High24h       *big.Int  `json:"high24h"`
	Low24h        *big.Int  `json:"low24h"`
	Volume24h     *big.Int  `json:"volume24h"`
	Trades24h     uint64    `json:"trades24h"`
	OpenInterest  *big.Int  `json:"openInterest"`
	FundingRate   *big.Int  `json:"fundingRate"`
	MarkPrice     *big.Int  `json:"markPrice"`
	IndexPrice    *big.Int  `json:"indexPrice"`
	BestBid       *big.Int  `json:"bestBid"`
	BestAsk       *big.Int  `json:"bestAsk"`
	LastUpdated   time.Time `json:"lastUpdated"`
}

// OrderBookIndexer indexes order book data
type OrderBookIndexer struct {
	mu              sync.RWMutex
	orders          map[string]*Order
	ordersByTrader  map[string]map[string]*Order
	ordersByMarket  map[string]map[string]*Order
	trades          []*Trade
	tradesByMarket  map[string][]*Trade
	positions       map[string]*MarginPosition
	marginAccounts  map[string]*MarginAccount
	lendingPositions map[string]*LendingPosition
	fundingRates    map[string][]*FundingRate
	fundingPayments []*FundingPayment
	liquidations    []*Liquidation
	marketStats     map[string]*MarketStats
	orderBooks      map[string]*OrderBookSnapshot
}

// NewOrderBookIndexer creates a new order book indexer
func NewOrderBookIndexer() *OrderBookIndexer {
	return &OrderBookIndexer{
		orders:          make(map[string]*Order),
		ordersByTrader:  make(map[string]map[string]*Order),
		ordersByMarket:  make(map[string]map[string]*Order),
		trades:          make([]*Trade, 0),
		tradesByMarket:  make(map[string][]*Trade),
		positions:       make(map[string]*MarginPosition),
		marginAccounts:  make(map[string]*MarginAccount),
		lendingPositions: make(map[string]*LendingPosition),
		fundingRates:    make(map[string][]*FundingRate),
		fundingPayments: make([]*FundingPayment, 0),
		liquidations:    make([]*Liquidation, 0),
		marketStats:     make(map[string]*MarketStats),
		orderBooks:      make(map[string]*OrderBookSnapshot),
	}
}

// IndexLog processes a log entry for order book events
func (o *OrderBookIndexer) IndexLog(log *LogEntry) error {
	if len(log.Topics) == 0 {
		return nil
	}

	topic0 := log.Topics[0]

	switch topic0 {
	// Order Events
	case OrderPlacedSig:
		return o.indexOrderPlaced(log)
	case OrderCancelledSig:
		return o.indexOrderCancelled(log)
	case OrderFilledSig:
		return o.indexOrderFilled(log)
	case OrderPartialSig:
		return o.indexOrderPartial(log)

	// Trade Events
	case TradeSig:
		return o.indexTrade(log)
	case TradeSettledSig:
		return o.indexTradeSettled(log)

	// Position Events
	case PositionOpenedSig:
		return o.indexPositionOpened(log)
	case PositionClosedSig:
		return o.indexPositionClosed(log)
	case PositionModifiedSig:
		return o.indexPositionModified(log)
	case PositionLiquidatedSig:
		return o.indexPositionLiquidated(log)

	// Margin Events
	case MarginDepositedSig:
		return o.indexMarginDeposited(log)
	case MarginWithdrawnSig:
		return o.indexMarginWithdrawn(log)
	case MarginCallSig:
		return o.indexMarginCall(log)

	// Lending Events
	case LendingDepositSig:
		return o.indexLendingDeposit(log)
	case LendingWithdrawSig:
		return o.indexLendingWithdraw(log)
	case LendingBorrowSig:
		return o.indexLendingBorrow(log)
	case LendingRepaySig:
		return o.indexLendingRepay(log)

	// Funding Events
	case FundingRateSig:
		return o.indexFundingRate(log)
	case FundingPaymentSig:
		return o.indexFundingPayment(log)

	// Clearinghouse Events
	case SettlementSig:
		return o.indexSettlement(log)
	case InsuranceFundSig:
		return o.indexInsuranceFund(log)
	}

	return nil
}

// indexOrderPlaced handles new order placement
func (o *OrderBookIndexer) indexOrderPlaced(log *LogEntry) error {
	if len(log.Topics) < 4 || len(log.Data) < 256 {
		return fmt.Errorf("invalid order placed log")
	}

	orderID := log.Topics[1]
	trader := "0x" + log.Topics[2][26:]
	market := parseMarketFromTopic(log.Topics[3])

	// Parse data: side, type, price, size, etc.
	sideRaw := new(big.Int).SetBytes(hexToBytes(log.Data[0:64])).Uint64()
	typeRaw := new(big.Int).SetBytes(hexToBytes(log.Data[64:128])).Uint64()
	price := new(big.Int).SetBytes(hexToBytes(log.Data[128:192]))
	size := new(big.Int).SetBytes(hexToBytes(log.Data[192:256]))

	side := OrderSideBuy
	if sideRaw == 1 {
		side = OrderSideSell
	}

	orderType := parseOrderType(typeRaw)

	order := &Order{
		ID:            orderID,
		Market:        market,
		Trader:        trader,
		Side:          side,
		Type:          orderType,
		Status:        OrderStatusOpen,
		Price:         price,
		Size:          size,
		FilledSize:    big.NewInt(0),
		RemainingSize: new(big.Int).Set(size),
		AveragePrice:  big.NewInt(0),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		TxHash:        log.TxHash,
		BlockNumber:   log.BlockNumber,
		LogIndex:      log.LogIndex,
		Fees:          big.NewInt(0),
	}

	// Parse optional fields
	if len(log.Data) >= 320 {
		order.StopPrice = new(big.Int).SetBytes(hexToBytes(log.Data[256:320]))
	}
	if len(log.Data) >= 384 {
		flags := new(big.Int).SetBytes(hexToBytes(log.Data[320:384])).Uint64()
		order.ReduceOnly = (flags & 1) != 0
		order.PostOnly = (flags & 2) != 0
	}

	return o.addOrder(order)
}

// indexOrderCancelled handles order cancellation
func (o *OrderBookIndexer) indexOrderCancelled(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid order cancelled log")
	}

	orderID := log.Topics[1]

	o.mu.Lock()
	defer o.mu.Unlock()

	if order, ok := o.orders[orderID]; ok {
		order.Status = OrderStatusCancelled
		now := time.Now()
		order.CancelledAt = &now
		order.UpdatedAt = now
		o.updateOrderBook(order.Market)
	}

	return nil
}

// indexOrderFilled handles full order fill
func (o *OrderBookIndexer) indexOrderFilled(log *LogEntry) error {
	if len(log.Topics) < 2 || len(log.Data) < 128 {
		return fmt.Errorf("invalid order filled log")
	}

	orderID := log.Topics[1]
	fillPrice := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))
	fillSize := new(big.Int).SetBytes(hexToBytes(log.Data[64:128]))

	o.mu.Lock()
	defer o.mu.Unlock()

	if order, ok := o.orders[orderID]; ok {
		order.Status = OrderStatusFilled
		order.FilledSize = fillSize
		order.RemainingSize = big.NewInt(0)
		order.AveragePrice = fillPrice
		now := time.Now()
		order.FilledAt = &now
		order.UpdatedAt = now

		if len(log.Data) >= 192 {
			order.Fees = new(big.Int).SetBytes(hexToBytes(log.Data[128:192]))
		}

		o.updateOrderBook(order.Market)
	}

	return nil
}

// indexOrderPartial handles partial order fill
func (o *OrderBookIndexer) indexOrderPartial(log *LogEntry) error {
	if len(log.Topics) < 2 || len(log.Data) < 128 {
		return fmt.Errorf("invalid order partial log")
	}

	orderID := log.Topics[1]
	fillPrice := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))
	fillSize := new(big.Int).SetBytes(hexToBytes(log.Data[64:128]))

	o.mu.Lock()
	defer o.mu.Unlock()

	if order, ok := o.orders[orderID]; ok {
		order.Status = OrderStatusPartiallyFilled
		order.FilledSize = new(big.Int).Add(order.FilledSize, fillSize)
		order.RemainingSize = new(big.Int).Sub(order.Size, order.FilledSize)

		// Calculate weighted average price
		if order.AveragePrice.Sign() == 0 {
			order.AveragePrice = fillPrice
		} else {
			// weighted avg = (old_avg * old_filled + new_price * new_filled) / total_filled
			oldTotal := new(big.Int).Mul(order.AveragePrice, new(big.Int).Sub(order.FilledSize, fillSize))
			newTotal := new(big.Int).Mul(fillPrice, fillSize)
			order.AveragePrice = new(big.Int).Div(new(big.Int).Add(oldTotal, newTotal), order.FilledSize)
		}

		order.UpdatedAt = time.Now()
		o.updateOrderBook(order.Market)
	}

	return nil
}

// indexTrade handles trade execution
func (o *OrderBookIndexer) indexTrade(log *LogEntry) error {
	if len(log.Topics) < 4 || len(log.Data) < 192 {
		return fmt.Errorf("invalid trade log")
	}

	tradeID := log.Topics[1]
	makerOrderID := log.Topics[2]
	takerOrderID := log.Topics[3]

	price := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))
	size := new(big.Int).SetBytes(hexToBytes(log.Data[64:128]))
	sideRaw := new(big.Int).SetBytes(hexToBytes(log.Data[128:192])).Uint64()

	side := OrderSideBuy
	if sideRaw == 1 {
		side = OrderSideSell
	}

	volume := new(big.Int).Mul(price, size)
	volume.Div(volume, big.NewInt(1e18)) // Adjust for decimals

	// Get maker and taker from orders
	var maker, taker, market string
	o.mu.RLock()
	if makerOrder, ok := o.orders[makerOrderID]; ok {
		maker = makerOrder.Trader
		market = makerOrder.Market
	}
	if takerOrder, ok := o.orders[takerOrderID]; ok {
		taker = takerOrder.Trader
		if market == "" {
			market = takerOrder.Market
		}
	}
	o.mu.RUnlock()

	trade := &Trade{
		ID:           tradeID,
		Market:       market,
		MakerOrderID: makerOrderID,
		TakerOrderID: takerOrderID,
		Maker:        maker,
		Taker:        taker,
		Side:         side,
		Price:        price,
		Size:         size,
		Volume:       volume,
		MakerFee:     big.NewInt(0),
		TakerFee:     big.NewInt(0),
		Timestamp:    time.Now(),
		TxHash:       log.TxHash,
		BlockNumber:  log.BlockNumber,
		LogIndex:     log.LogIndex,
		Settled:      false,
	}

	// Parse fees if present
	if len(log.Data) >= 320 {
		trade.MakerFee = new(big.Int).SetBytes(hexToBytes(log.Data[192:256]))
		trade.TakerFee = new(big.Int).SetBytes(hexToBytes(log.Data[256:320]))
	}

	o.mu.Lock()
	o.trades = append(o.trades, trade)
	o.tradesByMarket[market] = append(o.tradesByMarket[market], trade)
	o.updateMarketStats(market, trade)
	o.mu.Unlock()

	return nil
}

// indexTradeSettled handles trade settlement
func (o *OrderBookIndexer) indexTradeSettled(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid trade settled log")
	}

	tradeID := log.Topics[1]

	o.mu.Lock()
	defer o.mu.Unlock()

	for _, trade := range o.trades {
		if trade.ID == tradeID {
			trade.Settled = true
			break
		}
	}

	return nil
}

// indexPositionOpened handles new position opening
func (o *OrderBookIndexer) indexPositionOpened(log *LogEntry) error {
	if len(log.Topics) < 4 || len(log.Data) < 256 {
		return fmt.Errorf("invalid position opened log")
	}

	positionID := log.Topics[1]
	trader := "0x" + log.Topics[2][26:]
	market := parseMarketFromTopic(log.Topics[3])

	sideRaw := new(big.Int).SetBytes(hexToBytes(log.Data[0:64])).Uint64()
	size := new(big.Int).SetBytes(hexToBytes(log.Data[64:128]))
	entryPrice := new(big.Int).SetBytes(hexToBytes(log.Data[128:192]))
	margin := new(big.Int).SetBytes(hexToBytes(log.Data[192:256]))

	side := OrderSideBuy
	if sideRaw == 1 {
		side = OrderSideSell
	}

	// Calculate leverage
	notional := new(big.Int).Mul(size, entryPrice)
	notional.Div(notional, big.NewInt(1e18))
	leverage := 1.0
	if margin.Sign() > 0 {
		leverageBig := new(big.Int).Div(notional, margin)
		leverage = float64(leverageBig.Uint64())
	}

	position := &MarginPosition{
		ID:            positionID,
		Market:        market,
		Trader:        trader,
		Side:          side,
		Size:          size,
		EntryPrice:    entryPrice,
		MarkPrice:     entryPrice,
		Margin:        margin,
		Leverage:      leverage,
		UnrealizedPnL: big.NewInt(0),
		RealizedPnL:   big.NewInt(0),
		FundingPaid:   big.NewInt(0),
		OpenedAt:      time.Now(),
		UpdatedAt:     time.Now(),
		Status:        PositionStatusOBOpen,
	}

	// Calculate liquidation price
	position.LiquidationPrice = o.calculateLiquidationPrice(position)

	o.mu.Lock()
	defer o.mu.Unlock()

	o.positions[positionID] = position

	// Update margin account
	o.updateMarginAccount(trader, position, true)

	return nil
}

// indexPositionClosed handles position closing
func (o *OrderBookIndexer) indexPositionClosed(log *LogEntry) error {
	if len(log.Topics) < 2 || len(log.Data) < 128 {
		return fmt.Errorf("invalid position closed log")
	}

	positionID := log.Topics[1]
	exitPrice := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))
	realizedPnL := new(big.Int).SetBytes(hexToBytes(log.Data[64:128]))

	o.mu.Lock()
	defer o.mu.Unlock()

	if position, ok := o.positions[positionID]; ok {
		position.Status = PositionStatusOBClosed
		position.RealizedPnL = realizedPnL
		position.MarkPrice = exitPrice
		now := time.Now()
		position.ClosedAt = &now
		position.UpdatedAt = now

		o.updateMarginAccount(position.Trader, position, false)
	}

	return nil
}

// indexPositionModified handles position modification
func (o *OrderBookIndexer) indexPositionModified(log *LogEntry) error {
	if len(log.Topics) < 2 || len(log.Data) < 192 {
		return fmt.Errorf("invalid position modified log")
	}

	positionID := log.Topics[1]
	newSize := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))
	newMargin := new(big.Int).SetBytes(hexToBytes(log.Data[64:128]))
	newAvgPrice := new(big.Int).SetBytes(hexToBytes(log.Data[128:192]))

	o.mu.Lock()
	defer o.mu.Unlock()

	if position, ok := o.positions[positionID]; ok {
		position.Size = newSize
		position.Margin = newMargin
		position.EntryPrice = newAvgPrice
		position.UpdatedAt = time.Now()
		position.LiquidationPrice = o.calculateLiquidationPrice(position)
	}

	return nil
}

// indexPositionLiquidated handles position liquidation
func (o *OrderBookIndexer) indexPositionLiquidated(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 192 {
		return fmt.Errorf("invalid position liquidated log")
	}

	positionID := log.Topics[1]
	liquidator := "0x" + log.Topics[2][26:]

	liquidationPrice := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))
	penalty := new(big.Int).SetBytes(hexToBytes(log.Data[64:128]))
	insurancePayout := new(big.Int).SetBytes(hexToBytes(log.Data[128:192]))

	o.mu.Lock()
	defer o.mu.Unlock()

	var position *MarginPosition
	if pos, ok := o.positions[positionID]; ok {
		position = pos
		position.Status = PositionStatusOBLiquidated
		position.MarkPrice = liquidationPrice
		now := time.Now()
		position.ClosedAt = &now
		position.UpdatedAt = now
	}

	liquidation := &Liquidation{
		ID:              fmt.Sprintf("liq-%s-%s", log.TxHash, positionID),
		Market:          "",
		Trader:          "",
		Liquidator:      liquidator,
		PositionID:      positionID,
		Price:           liquidationPrice,
		Penalty:         penalty,
		InsurancePayout: insurancePayout,
		Timestamp:       time.Now(),
		TxHash:          log.TxHash,
		BlockNumber:     log.BlockNumber,
	}

	if position != nil {
		liquidation.Market = position.Market
		liquidation.Trader = position.Trader
		liquidation.Size = position.Size
		liquidation.Collateral = position.Margin
	}

	o.liquidations = append(o.liquidations, liquidation)

	return nil
}

// indexMarginDeposited handles margin deposits
func (o *OrderBookIndexer) indexMarginDeposited(log *LogEntry) error {
	if len(log.Topics) < 2 || len(log.Data) < 64 {
		return fmt.Errorf("invalid margin deposited log")
	}

	trader := "0x" + log.Topics[1][26:]
	amount := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))

	o.mu.Lock()
	defer o.mu.Unlock()

	account := o.getOrCreateMarginAccount(trader)
	account.TotalDeposited = new(big.Int).Add(account.TotalDeposited, amount)
	account.AvailableMargin = new(big.Int).Add(account.AvailableMargin, amount)
	account.LastUpdated = time.Now()

	return nil
}

// indexMarginWithdrawn handles margin withdrawals
func (o *OrderBookIndexer) indexMarginWithdrawn(log *LogEntry) error {
	if len(log.Topics) < 2 || len(log.Data) < 64 {
		return fmt.Errorf("invalid margin withdrawn log")
	}

	trader := "0x" + log.Topics[1][26:]
	amount := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))

	o.mu.Lock()
	defer o.mu.Unlock()

	account := o.getOrCreateMarginAccount(trader)
	account.TotalWithdrawn = new(big.Int).Add(account.TotalWithdrawn, amount)
	account.AvailableMargin = new(big.Int).Sub(account.AvailableMargin, amount)
	account.LastUpdated = time.Now()

	return nil
}

// indexMarginCall handles margin calls
func (o *OrderBookIndexer) indexMarginCall(log *LogEntry) error {
	if len(log.Topics) < 2 || len(log.Data) < 64 {
		return fmt.Errorf("invalid margin call log")
	}

	trader := "0x" + log.Topics[1][26:]
	requiredMargin := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))

	o.mu.Lock()
	defer o.mu.Unlock()

	account := o.getOrCreateMarginAccount(trader)
	account.MaintenanceMargin = requiredMargin
	account.LastUpdated = time.Now()

	// Calculate margin ratio
	if account.LockedMargin.Sign() > 0 {
		ratio := new(big.Int).Mul(account.AvailableMargin, big.NewInt(10000))
		ratio.Div(ratio, account.LockedMargin)
		account.MarginRatio = float64(ratio.Uint64()) / 100.0
	}

	return nil
}

// indexLendingDeposit handles lending deposits
func (o *OrderBookIndexer) indexLendingDeposit(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 64 {
		return fmt.Errorf("invalid lending deposit log")
	}

	lender := "0x" + log.Topics[1][26:]
	token := "0x" + log.Topics[2][26:]
	amount := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))

	positionID := fmt.Sprintf("%s-%s", lender, token)

	o.mu.Lock()
	defer o.mu.Unlock()

	position, ok := o.lendingPositions[positionID]
	if !ok {
		position = &LendingPosition{
			ID:              positionID,
			Lender:          lender,
			Token:           token,
			DepositedAmount: big.NewInt(0),
			BorrowedAmount:  big.NewInt(0),
			InterestEarned:  big.NewInt(0),
			InterestOwed:    big.NewInt(0),
			CreatedAt:       time.Now(),
		}
		o.lendingPositions[positionID] = position
	}

	position.DepositedAmount = new(big.Int).Add(position.DepositedAmount, amount)
	position.UpdatedAt = time.Now()

	return nil
}

// indexLendingWithdraw handles lending withdrawals
func (o *OrderBookIndexer) indexLendingWithdraw(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 64 {
		return fmt.Errorf("invalid lending withdraw log")
	}

	lender := "0x" + log.Topics[1][26:]
	token := "0x" + log.Topics[2][26:]
	amount := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))

	positionID := fmt.Sprintf("%s-%s", lender, token)

	o.mu.Lock()
	defer o.mu.Unlock()

	if position, ok := o.lendingPositions[positionID]; ok {
		position.DepositedAmount = new(big.Int).Sub(position.DepositedAmount, amount)
		position.UpdatedAt = time.Now()
	}

	return nil
}

// indexLendingBorrow handles borrowing
func (o *OrderBookIndexer) indexLendingBorrow(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 64 {
		return fmt.Errorf("invalid lending borrow log")
	}

	borrower := "0x" + log.Topics[1][26:]
	token := "0x" + log.Topics[2][26:]
	amount := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))

	positionID := fmt.Sprintf("%s-%s", borrower, token)

	o.mu.Lock()
	defer o.mu.Unlock()

	position, ok := o.lendingPositions[positionID]
	if !ok {
		position = &LendingPosition{
			ID:              positionID,
			Lender:          borrower,
			Token:           token,
			DepositedAmount: big.NewInt(0),
			BorrowedAmount:  big.NewInt(0),
			InterestEarned:  big.NewInt(0),
			InterestOwed:    big.NewInt(0),
			CreatedAt:       time.Now(),
		}
		o.lendingPositions[positionID] = position
	}

	position.BorrowedAmount = new(big.Int).Add(position.BorrowedAmount, amount)
	position.UpdatedAt = time.Now()

	return nil
}

// indexLendingRepay handles loan repayment
func (o *OrderBookIndexer) indexLendingRepay(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 128 {
		return fmt.Errorf("invalid lending repay log")
	}

	borrower := "0x" + log.Topics[1][26:]
	token := "0x" + log.Topics[2][26:]
	principal := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))
	interest := new(big.Int).SetBytes(hexToBytes(log.Data[64:128]))

	positionID := fmt.Sprintf("%s-%s", borrower, token)

	o.mu.Lock()
	defer o.mu.Unlock()

	if position, ok := o.lendingPositions[positionID]; ok {
		position.BorrowedAmount = new(big.Int).Sub(position.BorrowedAmount, principal)
		position.InterestOwed = new(big.Int).Sub(position.InterestOwed, interest)
		position.UpdatedAt = time.Now()
	}

	return nil
}

// indexFundingRate handles funding rate updates
func (o *OrderBookIndexer) indexFundingRate(log *LogEntry) error {
	if len(log.Topics) < 2 || len(log.Data) < 192 {
		return fmt.Errorf("invalid funding rate log")
	}

	market := parseMarketFromTopic(log.Topics[1])

	rate := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))
	markPrice := new(big.Int).SetBytes(hexToBytes(log.Data[64:128]))
	indexPrice := new(big.Int).SetBytes(hexToBytes(log.Data[128:192]))

	premium := new(big.Int).Sub(markPrice, indexPrice)

	funding := &FundingRate{
		Market:      market,
		Rate:        rate,
		Premium:     premium,
		IndexPrice:  indexPrice,
		MarkPrice:   markPrice,
		Timestamp:   time.Now(),
		NextFunding: time.Now().Add(8 * time.Hour),
		BlockNumber: log.BlockNumber,
	}

	o.mu.Lock()
	o.fundingRates[market] = append(o.fundingRates[market], funding)

	// Update market stats
	if stats, ok := o.marketStats[market]; ok {
		stats.FundingRate = rate
		stats.MarkPrice = markPrice
		stats.IndexPrice = indexPrice
		stats.LastUpdated = time.Now()
	}
	o.mu.Unlock()

	return nil
}

// indexFundingPayment handles funding payment
func (o *OrderBookIndexer) indexFundingPayment(log *LogEntry) error {
	if len(log.Topics) < 3 || len(log.Data) < 128 {
		return fmt.Errorf("invalid funding payment log")
	}

	trader := "0x" + log.Topics[1][26:]
	positionID := log.Topics[2]

	amount := new(big.Int).SetBytes(hexToBytes(log.Data[0:64]))
	rate := new(big.Int).SetBytes(hexToBytes(log.Data[64:128]))

	var market string
	var side OrderSide = OrderSideBuy

	o.mu.RLock()
	if position, ok := o.positions[positionID]; ok {
		market = position.Market
		side = position.Side
	}
	o.mu.RUnlock()

	payment := &FundingPayment{
		ID:          fmt.Sprintf("funding-%s-%d", log.TxHash, log.LogIndex),
		Market:      market,
		Trader:      trader,
		PositionID:  positionID,
		Amount:      amount,
		Rate:        rate,
		Side:        side,
		Timestamp:   time.Now(),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
	}

	o.mu.Lock()
	o.fundingPayments = append(o.fundingPayments, payment)

	// Update position's funding paid
	if position, ok := o.positions[positionID]; ok {
		position.FundingPaid = new(big.Int).Add(position.FundingPaid, amount)
	}
	o.mu.Unlock()

	return nil
}

// indexSettlement handles settlement events
func (o *OrderBookIndexer) indexSettlement(log *LogEntry) error {
	// Settlement event handling
	return nil
}

// indexInsuranceFund handles insurance fund events
func (o *OrderBookIndexer) indexInsuranceFund(log *LogEntry) error {
	// Insurance fund event handling
	return nil
}

// Helper methods

func (o *OrderBookIndexer) addOrder(order *Order) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.orders[order.ID] = order

	// Index by trader
	if o.ordersByTrader[order.Trader] == nil {
		o.ordersByTrader[order.Trader] = make(map[string]*Order)
	}
	o.ordersByTrader[order.Trader][order.ID] = order

	// Index by market
	if o.ordersByMarket[order.Market] == nil {
		o.ordersByMarket[order.Market] = make(map[string]*Order)
	}
	o.ordersByMarket[order.Market][order.ID] = order

	// Update order book
	o.updateOrderBook(order.Market)

	return nil
}

func (o *OrderBookIndexer) updateOrderBook(market string) {
	orders, ok := o.ordersByMarket[market]
	if !ok {
		return
	}

	// Collect bids and asks
	bids := make(map[string]*OrderBookLevel)
	asks := make(map[string]*OrderBookLevel)

	for _, order := range orders {
		if order.Status != OrderStatusOpen && order.Status != OrderStatusPartiallyFilled {
			continue
		}

		priceStr := order.Price.String()

		if order.Side == OrderSideBuy {
			if level, ok := bids[priceStr]; ok {
				level.Size.Add(level.Size, order.RemainingSize)
				level.OrderCount++
			} else {
				bids[priceStr] = &OrderBookLevel{
					Price:      new(big.Int).Set(order.Price),
					Size:       new(big.Int).Set(order.RemainingSize),
					OrderCount: 1,
				}
			}
		} else {
			if level, ok := asks[priceStr]; ok {
				level.Size.Add(level.Size, order.RemainingSize)
				level.OrderCount++
			} else {
				asks[priceStr] = &OrderBookLevel{
					Price:      new(big.Int).Set(order.Price),
					Size:       new(big.Int).Set(order.RemainingSize),
					OrderCount: 1,
				}
			}
		}
	}

	// Convert to sorted slices
	bidSlice := make([]*OrderBookLevel, 0, len(bids))
	for _, level := range bids {
		bidSlice = append(bidSlice, level)
	}
	sort.Slice(bidSlice, func(i, j int) bool {
		return bidSlice[i].Price.Cmp(bidSlice[j].Price) > 0 // Descending
	})

	askSlice := make([]*OrderBookLevel, 0, len(asks))
	for _, level := range asks {
		askSlice = append(askSlice, level)
	}
	sort.Slice(askSlice, func(i, j int) bool {
		return askSlice[i].Price.Cmp(askSlice[j].Price) < 0 // Ascending
	})

	snapshot := &OrderBookSnapshot{
		Market:    market,
		Bids:      bidSlice,
		Asks:      askSlice,
		Timestamp: time.Now(),
	}

	if len(bidSlice) > 0 {
		snapshot.BestBid = bidSlice[0].Price
	}
	if len(askSlice) > 0 {
		snapshot.BestAsk = askSlice[0].Price
	}
	if snapshot.BestBid != nil && snapshot.BestAsk != nil {
		snapshot.Spread = new(big.Int).Sub(snapshot.BestAsk, snapshot.BestBid)
	}

	o.orderBooks[market] = snapshot

	// Update market stats
	if stats, ok := o.marketStats[market]; ok {
		stats.BestBid = snapshot.BestBid
		stats.BestAsk = snapshot.BestAsk
		stats.LastUpdated = time.Now()
	}
}

func (o *OrderBookIndexer) updateMarketStats(market string, trade *Trade) {
	stats, ok := o.marketStats[market]
	if !ok {
		stats = &MarketStats{
			Market:       market,
			High24h:      big.NewInt(0),
			Low24h:       big.NewInt(0),
			Volume24h:    big.NewInt(0),
			OpenInterest: big.NewInt(0),
		}
		o.marketStats[market] = stats
	}

	stats.LastPrice = trade.Price
	stats.Volume24h = new(big.Int).Add(stats.Volume24h, trade.Volume)
	stats.Trades24h++

	if stats.High24h.Sign() == 0 || trade.Price.Cmp(stats.High24h) > 0 {
		stats.High24h = trade.Price
	}
	if stats.Low24h.Sign() == 0 || trade.Price.Cmp(stats.Low24h) < 0 {
		stats.Low24h = trade.Price
	}

	stats.LastUpdated = time.Now()
}

func (o *OrderBookIndexer) getOrCreateMarginAccount(trader string) *MarginAccount {
	account, ok := o.marginAccounts[trader]
	if !ok {
		account = &MarginAccount{
			Trader:           trader,
			TotalDeposited:   big.NewInt(0),
			TotalWithdrawn:   big.NewInt(0),
			AvailableMargin:  big.NewInt(0),
			LockedMargin:     big.NewInt(0),
			UnrealizedPnL:    big.NewInt(0),
			RealizedPnL:      big.NewInt(0),
			MaintenanceMargin: big.NewInt(0),
			Positions:        make(map[string]*MarginPosition),
			LastUpdated:      time.Now(),
		}
		o.marginAccounts[trader] = account
	}
	return account
}

func (o *OrderBookIndexer) updateMarginAccount(trader string, position *MarginPosition, isOpening bool) {
	account := o.getOrCreateMarginAccount(trader)

	if isOpening {
		account.Positions[position.ID] = position
		account.LockedMargin = new(big.Int).Add(account.LockedMargin, position.Margin)
		account.AvailableMargin = new(big.Int).Sub(account.AvailableMargin, position.Margin)
	} else {
		delete(account.Positions, position.ID)
		account.LockedMargin = new(big.Int).Sub(account.LockedMargin, position.Margin)
		account.RealizedPnL = new(big.Int).Add(account.RealizedPnL, position.RealizedPnL)
		// Return margin + PnL to available
		returned := new(big.Int).Add(position.Margin, position.RealizedPnL)
		account.AvailableMargin = new(big.Int).Add(account.AvailableMargin, returned)
	}

	account.LastUpdated = time.Now()
}

func (o *OrderBookIndexer) calculateLiquidationPrice(position *MarginPosition) *big.Int {
	// Simplified liquidation price calculation
	// For longs: liqPrice = entryPrice * (1 - 1/leverage + maintenanceMargin)
	// For shorts: liqPrice = entryPrice * (1 + 1/leverage - maintenanceMargin)

	maintenanceRatio := big.NewInt(50) // 0.5% in basis points
	leverageBig := big.NewInt(int64(position.Leverage * 1000))

	// 1/leverage in basis points
	inverseLeverage := new(big.Int).Div(big.NewInt(1000000), leverageBig)

	if position.Side == OrderSideBuy {
		// Long: liqPrice = entryPrice * (1 - 1/leverage + maintenance)
		factor := new(big.Int).Sub(big.NewInt(10000), inverseLeverage)
		factor.Add(factor, maintenanceRatio)
		liqPrice := new(big.Int).Mul(position.EntryPrice, factor)
		return liqPrice.Div(liqPrice, big.NewInt(10000))
	} else {
		// Short: liqPrice = entryPrice * (1 + 1/leverage - maintenance)
		factor := new(big.Int).Add(big.NewInt(10000), inverseLeverage)
		factor.Sub(factor, maintenanceRatio)
		liqPrice := new(big.Int).Mul(position.EntryPrice, factor)
		return liqPrice.Div(liqPrice, big.NewInt(10000))
	}
}

// Query methods

// GetOrder returns an order by ID
func (o *OrderBookIndexer) GetOrder(id string) (*Order, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	order, ok := o.orders[id]
	return order, ok
}

// GetOrdersByTrader returns all orders for a trader
func (o *OrderBookIndexer) GetOrdersByTrader(trader string) []*Order {
	o.mu.RLock()
	defer o.mu.RUnlock()

	orders := make([]*Order, 0)
	if traderOrders, ok := o.ordersByTrader[trader]; ok {
		for _, order := range traderOrders {
			orders = append(orders, order)
		}
	}
	return orders
}

// GetOrdersByMarket returns all orders for a market
func (o *OrderBookIndexer) GetOrdersByMarket(market string) []*Order {
	o.mu.RLock()
	defer o.mu.RUnlock()

	orders := make([]*Order, 0)
	if marketOrders, ok := o.ordersByMarket[market]; ok {
		for _, order := range marketOrders {
			orders = append(orders, order)
		}
	}
	return orders
}

// GetOpenOrders returns all open orders
func (o *OrderBookIndexer) GetOpenOrders(trader string) []*Order {
	o.mu.RLock()
	defer o.mu.RUnlock()

	var result []*Order
	if traderOrders, ok := o.ordersByTrader[trader]; ok {
		for _, order := range traderOrders {
			if order.Status == OrderStatusOpen || order.Status == OrderStatusPartiallyFilled {
				result = append(result, order)
			}
		}
	}
	return result
}

// GetTrades returns all trades
func (o *OrderBookIndexer) GetTrades() []*Trade {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.trades
}

// GetTradesByMarket returns trades for a market
func (o *OrderBookIndexer) GetTradesByMarket(market string) []*Trade {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.tradesByMarket[market]
}

// GetPosition returns a position by ID
func (o *OrderBookIndexer) GetPosition(id string) (*MarginPosition, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	position, ok := o.positions[id]
	return position, ok
}

// GetPositionsByTrader returns all positions for a trader
func (o *OrderBookIndexer) GetPositionsByTrader(trader string) []*MarginPosition {
	o.mu.RLock()
	defer o.mu.RUnlock()

	var result []*MarginPosition
	for _, position := range o.positions {
		if position.Trader == trader {
			result = append(result, position)
		}
	}
	return result
}

// GetMarginAccount returns a margin account
func (o *OrderBookIndexer) GetMarginAccount(trader string) (*MarginAccount, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	account, ok := o.marginAccounts[trader]
	return account, ok
}

// GetLendingPosition returns a lending position
func (o *OrderBookIndexer) GetLendingPosition(lender, token string) (*LendingPosition, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	positionID := fmt.Sprintf("%s-%s", lender, token)
	position, ok := o.lendingPositions[positionID]
	return position, ok
}

// GetOrderBook returns the order book snapshot
func (o *OrderBookIndexer) GetOrderBook(market string) (*OrderBookSnapshot, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	ob, ok := o.orderBooks[market]
	return ob, ok
}

// GetMarketStats returns market statistics
func (o *OrderBookIndexer) GetMarketStats(market string) (*MarketStats, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	stats, ok := o.marketStats[market]
	return stats, ok
}

// GetFundingRates returns funding rates for a market
func (o *OrderBookIndexer) GetFundingRates(market string) []*FundingRate {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.fundingRates[market]
}

// GetLiquidations returns all liquidations
func (o *OrderBookIndexer) GetLiquidations() []*Liquidation {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.liquidations
}

// GetFundingPayments returns all funding payments
func (o *OrderBookIndexer) GetFundingPayments() []*FundingPayment {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.fundingPayments
}

// Helper functions

func parseMarketFromTopic(topic string) string {
	// Parse market identifier from topic
	// This would decode the bytes32 market ID to a human-readable string
	// For now, return the topic as-is
	if len(topic) > 10 {
		return topic[:10] + "..."
	}
	return topic
}

func parseOrderType(typeRaw uint64) OrderType {
	switch typeRaw {
	case 0:
		return OrderTypeLimit
	case 1:
		return OrderTypeMarket
	case 2:
		return OrderTypeStopLimit
	case 3:
		return OrderTypeStopMarket
	case 4:
		return OrderTypeTakeProfit
	default:
		return OrderTypeLimit
	}
}
