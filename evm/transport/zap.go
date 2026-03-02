// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package transport

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luxfi/zap"
)

// ZAP message types for RPC over the wire protocol.
const (
	msgTypeRPCRequest  uint16 = 0x10
	msgTypeRPCResponse uint16 = 0x11

	// ZAP RPC object layout:
	//   [0:4]   uint32 - request ID
	//   [4:12]  text   - method name (offset+length)
	//   [12:20] bytes  - params JSON (offset+length)
	zapRPCObjectSize = 20

	// ZAP RPC response layout:
	//   [0:4]   uint32 - request ID
	//   [4:5]   uint8  - error flag (0=ok, 1=error)
	//   [8:16]  bytes  - result/error JSON (offset+length)
	zapRPCResponseSize = 16

	maxPendingRequests = 4096
	connectTimeout     = 5 * time.Second
	writeTimeout       = 10 * time.Second
	readTimeout        = 30 * time.Second
)

var (
	ErrZAPClosed     = errors.New("zap transport: closed")
	ErrZAPTimeout    = errors.New("zap transport: request timeout")
	ErrZAPConnFailed = errors.New("zap transport: connection failed")
)

// ZAP implements Transport over the luxfi/zap binary wire protocol.
// Maintains a persistent TCP connection with request/response correlation.
type ZAP struct {
	addr     string // ZAP TCP address (host:port)
	httpAddr string // Original HTTP endpoint for fallback context

	conn   net.Conn
	connMu sync.Mutex

	reqID   atomic.Uint32
	pending sync.Map // map[uint32]chan zapResponse

	closed atomic.Bool
	done   chan struct{}
	wg     sync.WaitGroup

	// Subscriptions
	subsMu sync.Mutex
	subs   map[uint32]chan json.RawMessage
}

type zapResponse struct {
	result json.RawMessage
	err    error
}

// NewZAP creates a ZAP transport by connecting to the given address.
// Returns an error if the initial connection fails.
func NewZAP(addr string, httpAddr string) (*ZAP, error) {
	conn, err := net.DialTimeout("tcp", addr, connectTimeout)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrZAPConnFailed, addr, err)
	}

	z := &ZAP{
		addr:     addr,
		httpAddr: httpAddr,
		conn:     conn,
		done:     make(chan struct{}),
		subs:     make(map[uint32]chan json.RawMessage),
	}

	// Send handshake: identify as indexer client
	if err := z.handshake(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("zap handshake failed: %w", err)
	}

	// Start receive loop
	z.wg.Add(1)
	go z.recvLoop()

	return z, nil
}

// Call sends a single RPC request over ZAP and waits for the response.
func (z *ZAP) Call(ctx context.Context, method string, params ...any) (json.RawMessage, error) {
	if z.closed.Load() {
		return nil, ErrZAPClosed
	}

	// Encode params as JSON
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}

	reqID := z.reqID.Add(1)

	// Build ZAP message
	b := zap.NewBuilder(256)
	obj := b.StartObject(zapRPCObjectSize)
	obj.SetUint32(0, reqID)
	obj.SetText(4, method)
	obj.SetBytes(12, paramsJSON)
	obj.FinishAsRoot()
	data := b.FinishWithFlags(uint16(msgTypeRPCRequest) << 8)

	// Register pending response
	respCh := make(chan zapResponse, 1)
	z.pending.Store(reqID, respCh)
	defer z.pending.Delete(reqID)

	// Send
	if err := z.writeMsg(data); err != nil {
		return nil, err
	}

	// Wait for response
	select {
	case resp := <-respCh:
		return resp.result, resp.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-z.done:
		return nil, ErrZAPClosed
	}
}

// BatchCall sends multiple RPC requests over ZAP concurrently.
// Each call is sent as a separate ZAP message (pipelined on the same connection).
func (z *ZAP) BatchCall(ctx context.Context, calls []RPCCall) ([]json.RawMessage, error) {
	if len(calls) == 0 {
		return nil, nil
	}

	type indexedResult struct {
		idx    int
		result json.RawMessage
		err    error
	}

	results := make(chan indexedResult, len(calls))
	var wg sync.WaitGroup

	for i, c := range calls {
		wg.Add(1)
		go func(idx int, call RPCCall) {
			defer wg.Done()
			res, err := z.Call(ctx, call.Method, call.Params...)
			results <- indexedResult{idx: idx, result: res, err: err}
		}(i, c)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	out := make([]json.RawMessage, len(calls))
	for r := range results {
		if r.err != nil {
			return nil, fmt.Errorf("batch call[%d] %s: %w", r.idx, calls[r.idx].Method, r.err)
		}
		out[r.idx] = r.result
	}

	return out, nil
}

// Subscribe opens a ZAP subscription stream for the given method.
// The returned channel receives messages until the context is cancelled or Close is called.
func (z *ZAP) Subscribe(ctx context.Context, method string, params ...any) (<-chan json.RawMessage, error) {
	if z.closed.Load() {
		return nil, ErrZAPClosed
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}

	subID := z.reqID.Add(1)

	// Build subscribe message (same layout, different message type)
	b := zap.NewBuilder(256)
	obj := b.StartObject(zapRPCObjectSize)
	obj.SetUint32(0, subID)
	obj.SetText(4, method)
	obj.SetBytes(12, paramsJSON)
	obj.FinishAsRoot()
	// Use high bit to mark subscription
	data := b.FinishWithFlags(uint16(msgTypeRPCRequest)<<8 | 0x01)

	ch := make(chan json.RawMessage, 64)

	z.subsMu.Lock()
	z.subs[subID] = ch
	z.subsMu.Unlock()

	if err := z.writeMsg(data); err != nil {
		z.subsMu.Lock()
		delete(z.subs, subID)
		z.subsMu.Unlock()
		close(ch)
		return nil, err
	}

	// Cleanup when context is done
	go func() {
		select {
		case <-ctx.Done():
		case <-z.done:
		}
		z.subsMu.Lock()
		delete(z.subs, subID)
		z.subsMu.Unlock()
		close(ch)
	}()

	return ch, nil
}

// Close shuts down the ZAP connection.
func (z *ZAP) Close() error {
	if z.closed.Swap(true) {
		return nil // already closed
	}
	close(z.done)

	z.connMu.Lock()
	err := z.conn.Close()
	z.connMu.Unlock()

	z.wg.Wait()

	// Drain pending requests
	z.pending.Range(func(key, value any) bool {
		ch := value.(chan zapResponse)
		select {
		case ch <- zapResponse{err: ErrZAPClosed}:
		default:
		}
		z.pending.Delete(key)
		return true
	})

	return err
}

func (z *ZAP) handshake() error {
	b := zap.NewBuilder(128)
	obj := b.StartObject(64)
	// Identify as "explorer-indexer"
	id := []byte("explorer-indexer")
	for i, c := range id {
		if i >= 60 {
			break
		}
		obj.SetUint8(i, c)
	}
	obj.SetUint32(60, uint32(len(id)))
	obj.FinishAsRoot()

	data := b.Finish()
	return z.writeMsg(data)
}

func (z *ZAP) writeMsg(data []byte) error {
	z.connMu.Lock()
	defer z.connMu.Unlock()

	z.conn.SetWriteDeadline(time.Now().Add(writeTimeout))

	// Wire format: [4 bytes length][message bytes]
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(data)))

	if _, err := z.conn.Write(lenBuf[:]); err != nil {
		return fmt.Errorf("zap write length: %w", err)
	}
	if _, err := z.conn.Write(data); err != nil {
		return fmt.Errorf("zap write data: %w", err)
	}
	return nil
}

func (z *ZAP) recvLoop() {
	defer z.wg.Done()

	for {
		select {
		case <-z.done:
			return
		default:
		}

		z.conn.SetReadDeadline(time.Now().Add(readTimeout))

		data, err := z.readMsg()
		if err != nil {
			if z.closed.Load() {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}

		z.handleResponse(data)
	}
}

func (z *ZAP) readMsg() ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(z.conn, lenBuf[:]); err != nil {
		return nil, err
	}

	length := binary.LittleEndian.Uint32(lenBuf[:])
	if length > 10*1024*1024 { // 10MB max
		return nil, errors.New("zap message too large")
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(z.conn, data); err != nil {
		return nil, err
	}

	return data, nil
}

func (z *ZAP) handleResponse(data []byte) {
	msg, err := zap.Parse(data)
	if err != nil {
		return
	}

	root := msg.Root()
	reqID := root.Uint32(0)
	errFlag := root.Uint8(4)
	payload := root.Bytes(8)

	// Check if this is a subscription notification
	z.subsMu.Lock()
	subCh, isSub := z.subs[reqID]
	z.subsMu.Unlock()

	if isSub {
		select {
		case subCh <- json.RawMessage(payload):
		default:
			// Drop if subscriber is slow
		}
		return
	}

	// Route to pending Call
	val, ok := z.pending.Load(reqID)
	if !ok {
		return
	}
	ch := val.(chan zapResponse)

	if errFlag != 0 {
		select {
		case ch <- zapResponse{err: fmt.Errorf("zap rpc error: %s", string(payload))}:
		default:
		}
	} else {
		select {
		case ch <- zapResponse{result: json.RawMessage(payload)}:
		default:
		}
	}
}
