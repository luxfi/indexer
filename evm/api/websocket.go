// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketHub manages WebSocket connections and message broadcasting
type WebSocketHub struct {
	// Block subscriptions
	blockClients    map[*websocket.Conn]bool
	blockBroadcast  chan interface{}
	blockRegister   chan *websocket.Conn
	blockUnregister chan *websocket.Conn

	// Transaction subscriptions
	txClients    map[*websocket.Conn]bool
	txBroadcast  chan interface{}
	txRegister   chan *websocket.Conn
	txUnregister chan *websocket.Conn

	// Address-specific subscriptions
	addressClients    map[string]map[*websocket.Conn]bool
	addressBroadcast  chan AddressEvent
	addressRegister   chan AddressSubscription
	addressUnregister chan AddressSubscription

	mu sync.RWMutex
}

// AddressEvent for address-specific notifications
type AddressEvent struct {
	Address string      `json:"address"`
	Type    string      `json:"type"` // transaction, token_transfer, balance
	Data    interface{} `json:"data"`
}

// AddressSubscription for managing address-specific subscriptions
type AddressSubscription struct {
	Conn    *websocket.Conn
	Address string
}

// WebSocketMessage is the base message format
type WebSocketMessage struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

// NewWebSocketHub creates a new WebSocket hub
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		blockClients:      make(map[*websocket.Conn]bool),
		blockBroadcast:    make(chan interface{}, 256),
		blockRegister:     make(chan *websocket.Conn, 16),
		blockUnregister:   make(chan *websocket.Conn, 16),
		txClients:         make(map[*websocket.Conn]bool),
		txBroadcast:       make(chan interface{}, 256),
		txRegister:        make(chan *websocket.Conn, 16),
		txUnregister:      make(chan *websocket.Conn, 16),
		addressClients:    make(map[string]map[*websocket.Conn]bool),
		addressBroadcast:  make(chan AddressEvent, 256),
		addressRegister:   make(chan AddressSubscription, 16),
		addressUnregister: make(chan AddressSubscription, 16),
	}
}

// Run starts the hub's message processing
func (h *WebSocketHub) Run(ctx context.Context) {
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			h.closeAll()
			return

		// Block subscriptions
		case conn := <-h.blockRegister:
			h.mu.Lock()
			h.blockClients[conn] = true
			h.mu.Unlock()
			h.sendConnected(conn, "blocks")

		case conn := <-h.blockUnregister:
			h.mu.Lock()
			delete(h.blockClients, conn)
			conn.Close()
			h.mu.Unlock()

		case msg := <-h.blockBroadcast:
			h.broadcastToClients(h.blockClients, WebSocketMessage{
				Type:      "new_block",
				Data:      msg,
				Timestamp: time.Now().UnixMilli(),
			})

		// Transaction subscriptions
		case conn := <-h.txRegister:
			h.mu.Lock()
			h.txClients[conn] = true
			h.mu.Unlock()
			h.sendConnected(conn, "transactions")

		case conn := <-h.txUnregister:
			h.mu.Lock()
			delete(h.txClients, conn)
			conn.Close()
			h.mu.Unlock()

		case msg := <-h.txBroadcast:
			h.broadcastToClients(h.txClients, WebSocketMessage{
				Type:      "new_transaction",
				Data:      msg,
				Timestamp: time.Now().UnixMilli(),
			})

		// Address subscriptions
		case sub := <-h.addressRegister:
			h.mu.Lock()
			if h.addressClients[sub.Address] == nil {
				h.addressClients[sub.Address] = make(map[*websocket.Conn]bool)
			}
			h.addressClients[sub.Address][sub.Conn] = true
			h.mu.Unlock()
			h.sendConnected(sub.Conn, "address:"+sub.Address)

		case sub := <-h.addressUnregister:
			h.mu.Lock()
			if clients, ok := h.addressClients[sub.Address]; ok {
				delete(clients, sub.Conn)
				if len(clients) == 0 {
					delete(h.addressClients, sub.Address)
				}
			}
			sub.Conn.Close()
			h.mu.Unlock()

		case event := <-h.addressBroadcast:
			h.mu.RLock()
			clients := h.addressClients[event.Address]
			h.mu.RUnlock()

			if clients != nil {
				h.broadcastToClients(clients, WebSocketMessage{
					Type:      event.Type,
					Data:      event.Data,
					Timestamp: time.Now().UnixMilli(),
				})
			}

		// Heartbeat to all clients
		case <-heartbeat.C:
			h.sendHeartbeat()
		}
	}
}

func (h *WebSocketHub) sendConnected(conn *websocket.Conn, channel string) {
	msg := WebSocketMessage{
		Type:      "connected",
		Data:      map[string]string{"channel": channel},
		Timestamp: time.Now().UnixMilli(),
	}
	conn.WriteJSON(msg)

	// Start reader goroutine to handle close
	go h.readPump(conn, channel)
}

func (h *WebSocketHub) readPump(conn *websocket.Conn, channel string) {
	defer func() {
		// Unregister based on channel type
		if channel == "blocks" {
			h.blockUnregister <- conn
		} else if channel == "transactions" {
			h.txUnregister <- conn
		} else if len(channel) > 8 && channel[:8] == "address:" {
			h.addressUnregister <- AddressSubscription{
				Conn:    conn,
				Address: channel[8:],
			}
		}
	}()

	conn.SetReadLimit(512)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (h *WebSocketHub) broadcastToClients(clients map[*websocket.Conn]bool, msg WebSocketMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for conn := range clients {
		err := conn.WriteMessage(websocket.TextMessage, data)
		if err != nil {
			// Will be cleaned up by readPump
		}
	}
}

func (h *WebSocketHub) sendHeartbeat() {
	msg := WebSocketMessage{
		Type:      "heartbeat",
		Timestamp: time.Now().UnixMilli(),
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	// Send to block clients
	for conn := range h.blockClients {
		conn.WriteJSON(msg)
	}

	// Send to transaction clients
	for conn := range h.txClients {
		conn.WriteJSON(msg)
	}

	// Send to address clients
	for _, clients := range h.addressClients {
		for conn := range clients {
			conn.WriteJSON(msg)
		}
	}
}

func (h *WebSocketHub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for conn := range h.blockClients {
		conn.Close()
	}
	for conn := range h.txClients {
		conn.Close()
	}
	for _, clients := range h.addressClients {
		for conn := range clients {
			conn.Close()
		}
	}
}

// RegisterBlockClient registers a client for block updates
func (h *WebSocketHub) RegisterBlockClient(conn *websocket.Conn) {
	h.blockRegister <- conn
}

// RegisterTransactionClient registers a client for transaction updates
func (h *WebSocketHub) RegisterTransactionClient(conn *websocket.Conn) {
	h.txRegister <- conn
}

// RegisterAddressClient registers a client for address-specific updates
func (h *WebSocketHub) RegisterAddressClient(conn *websocket.Conn, address string) {
	h.addressRegister <- AddressSubscription{Conn: conn, Address: address}
}

// BroadcastBlock sends a new block to all block subscribers
func (h *WebSocketHub) BroadcastBlock(block *Block) {
	select {
	case h.blockBroadcast <- block:
	default:
		// Channel full, skip
	}
}

// BroadcastTransaction sends a new transaction to all transaction subscribers
func (h *WebSocketHub) BroadcastTransaction(tx *Transaction) {
	select {
	case h.txBroadcast <- tx:
	default:
		// Channel full, skip
	}

	// Also broadcast to address-specific subscribers
	if tx.From != nil {
		h.BroadcastAddressEvent(tx.From.Hash, "transaction", tx)
	}
	if tx.To != nil {
		h.BroadcastAddressEvent(tx.To.Hash, "transaction", tx)
	}
}

// BroadcastAddressEvent sends an event to address-specific subscribers
func (h *WebSocketHub) BroadcastAddressEvent(address, eventType string, data interface{}) {
	select {
	case h.addressBroadcast <- AddressEvent{
		Address: address,
		Type:    eventType,
		Data:    data,
	}:
	default:
		// Channel full, skip
	}
}

// BroadcastTokenTransfer notifies address subscribers of token transfers
func (h *WebSocketHub) BroadcastTokenTransfer(transfer *TokenTransfer) {
	if transfer.From != nil {
		h.BroadcastAddressEvent(transfer.From.Hash, "token_transfer", transfer)
	}
	if transfer.To != nil {
		h.BroadcastAddressEvent(transfer.To.Hash, "token_transfer", transfer)
	}
}

// Stats returns current connection statistics
func (h *WebSocketHub) Stats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	addressCount := 0
	for _, clients := range h.addressClients {
		addressCount += len(clients)
	}

	return map[string]interface{}{
		"block_subscribers":       len(h.blockClients),
		"transaction_subscribers": len(h.txClients),
		"address_subscribers":     addressCount,
		"total_connections":       len(h.blockClients) + len(h.txClients) + addressCount,
	}
}
