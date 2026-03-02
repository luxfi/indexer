// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package transport

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/luxfi/zap"
)

// --- HTTP Transport Tests ---

func TestHTTPCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		method := req["method"].(string)
		if method != "eth_blockNumber" {
			t.Errorf("expected method eth_blockNumber, got %s", method)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result":  "0x64",
		})
	}))
	defer server.Close()

	h := NewHTTP(server.URL)
	defer h.Close()

	result, err := h.Call(context.Background(), "eth_blockNumber")
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var hex string
	if err := json.Unmarshal(result, &hex); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if hex != "0x64" {
		t.Errorf("result = %q, want %q", hex, "0x64")
	}
}

func TestHTTPCallWithParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		params := req["params"].([]any)
		if len(params) != 2 {
			t.Errorf("expected 2 params, got %d", len(params))
		}

		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result":  map[string]any{"hash": "0xabc"},
		})
	}))
	defer server.Close()

	h := NewHTTP(server.URL)
	result, err := h.Call(context.Background(), "eth_getBlockByNumber", "0x64", true)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var block struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(result, &block); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if block.Hash != "0xabc" {
		t.Errorf("hash = %q, want %q", block.Hash, "0xabc")
	}
}

func TestHTTPCallRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]any{
				"code":    -32601,
				"message": "method not found",
			},
		})
	}))
	defer server.Close()

	h := NewHTTP(server.URL)
	_, err := h.Call(context.Background(), "bad_method")
	if err == nil {
		t.Fatal("expected error for RPC error response")
	}
}

func TestHTTPBatchCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch []map[string]any
		json.NewDecoder(r.Body).Decode(&batch)

		if len(batch) != 3 {
			t.Errorf("expected 3 calls in batch, got %d", len(batch))
		}

		results := make([]map[string]any, len(batch))
		for i, req := range batch {
			results[i] = map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  fmt.Sprintf("result_%d", i),
			}
		}
		json.NewEncoder(w).Encode(results)
	}))
	defer server.Close()

	h := NewHTTP(server.URL)
	calls := []RPCCall{
		{Method: "eth_blockNumber"},
		{Method: "eth_chainId"},
		{Method: "net_version"},
	}

	results, err := h.BatchCall(context.Background(), calls)
	if err != nil {
		t.Fatalf("BatchCall failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	for i, r := range results {
		var s string
		json.Unmarshal(r, &s)
		want := fmt.Sprintf("result_%d", i)
		if s != want {
			t.Errorf("result[%d] = %q, want %q", i, s, want)
		}
	}
}

func TestHTTPSubscribeNotSupported(t *testing.T) {
	h := NewHTTP("http://localhost:9650")
	_, err := h.Subscribe(context.Background(), "eth_subscribe", "newHeads")
	if err == nil {
		t.Fatal("expected error for Subscribe over HTTP")
	}
}

func TestHTTPCallContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	h := NewHTTP(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := h.Call(ctx, "eth_blockNumber")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// --- Auto-detect Tests ---

func TestIsLuxdEndpoint(t *testing.T) {
	tests := []struct {
		endpoint string
		want     bool
	}{
		{"http://luxd-0.luxd:9630/ext/bc/C/rpc", true},
		{"http://localhost:9650/ext/bc/C/rpc", true},
		{"https://api.lux.network/mainnet/ext/bc/C/rpc", true},
		{"http://localhost:8545", false},
		{"https://mainnet.infura.io/v3/key", false},
	}

	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			got := isLuxdEndpoint(tt.endpoint)
			if got != tt.want {
				t.Errorf("isLuxdEndpoint(%q) = %v, want %v", tt.endpoint, got, tt.want)
			}
		})
	}
}

func TestLuxdZAPAddr(t *testing.T) {
	tests := []struct {
		endpoint string
		want     string
	}{
		{"http://luxd-0.luxd:9630/ext/bc/C/rpc", "luxd-0.luxd:9640"},
		{"http://localhost:9650/ext/bc/C/rpc", "localhost:9660"},
		{"https://api.lux.network/mainnet/ext/bc/C/rpc", "api.lux.network:9640"},
		{"http://10.0.0.1:19630/ext/bc/C/rpc", "10.0.0.1:19640"},
	}

	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			got := luxdZAPAddr(tt.endpoint)
			if got != tt.want {
				t.Errorf("luxdZAPAddr(%q) = %q, want %q", tt.endpoint, got, tt.want)
			}
		})
	}
}

func TestNewFallsBackToHTTP(t *testing.T) {
	// Non-luxd endpoint should always get HTTP
	tr := New("http://localhost:8545")
	if _, ok := tr.(*HTTP); !ok {
		t.Errorf("expected *HTTP for non-luxd endpoint, got %T", tr)
	}

	// luxd endpoint with unreachable ZAP port should fall back to HTTP
	tr = New("http://localhost:1/ext/bc/C/rpc")
	if _, ok := tr.(*HTTP); !ok {
		t.Errorf("expected *HTTP fallback for unreachable ZAP, got %T", tr)
	}
}

// --- ZAP Transport Tests ---

// mockZAPServer creates a local TCP server that speaks the ZAP wire protocol.
// It reads RPC requests and responds with canned data.
func mockZAPServer(t *testing.T, handler func(reqID uint32, method string, params json.RawMessage) (json.RawMessage, error)) (addr string, cleanup func()) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var wg sync.WaitGroup
	done := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					continue
				}
			}
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				handleMockZAPConn(t, c, handler, done)
			}(conn)
		}
	}()

	return ln.Addr().String(), func() {
		select {
		case <-done:
		default:
			close(done)
		}
		ln.Close()
		wg.Wait()
	}
}

func handleMockZAPConn(t *testing.T, conn net.Conn, handler func(uint32, string, json.RawMessage) (json.RawMessage, error), done chan struct{}) {
	t.Helper()

	// Read handshake
	if _, err := readWireMsg(conn); err != nil {
		return
	}

	for {
		select {
		case <-done:
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		data, err := readWireMsg(conn)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return
		}

		msg, err := zap.Parse(data)
		if err != nil {
			continue
		}

		root := msg.Root()
		reqID := root.Uint32(0)
		method := root.Text(4)
		paramsBytes := root.Bytes(12)

		result, rpcErr := handler(reqID, method, json.RawMessage(paramsBytes))

		// Build response
		b := zap.NewBuilder(256)
		obj := b.StartObject(zapRPCResponseSize)
		obj.SetUint32(0, reqID)
		if rpcErr != nil {
			obj.SetUint8(4, 1)
			obj.SetBytes(8, []byte(rpcErr.Error()))
		} else {
			obj.SetUint8(4, 0)
			obj.SetBytes(8, result)
		}
		obj.FinishAsRoot()
		respData := b.FinishWithFlags(uint16(msgTypeRPCResponse) << 8)

		writeWireMsg(conn, respData)
	}
}

func readWireMsg(r io.Reader) ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	length := binary.LittleEndian.Uint32(lenBuf[:])
	if length > 10*1024*1024 {
		return nil, fmt.Errorf("message too large: %d", length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return data, nil
}

func writeWireMsg(w io.Writer, data []byte) error {
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

func TestZAPCall(t *testing.T) {
	addr, cleanup := mockZAPServer(t, func(reqID uint32, method string, params json.RawMessage) (json.RawMessage, error) {
		if method != "eth_blockNumber" {
			return nil, fmt.Errorf("unexpected method: %s", method)
		}
		return json.RawMessage(`"0x64"`), nil
	})
	defer cleanup()

	z, err := NewZAP(addr, "http://localhost/ext/bc/C/rpc")
	if err != nil {
		t.Fatalf("NewZAP: %v", err)
	}
	defer z.Close()

	result, err := z.Call(context.Background(), "eth_blockNumber")
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var hex string
	if err := json.Unmarshal(result, &hex); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if hex != "0x64" {
		t.Errorf("result = %q, want %q", hex, "0x64")
	}
}

func TestZAPCallWithParams(t *testing.T) {
	addr, cleanup := mockZAPServer(t, func(reqID uint32, method string, params json.RawMessage) (json.RawMessage, error) {
		if method != "eth_getBlockByNumber" {
			return nil, fmt.Errorf("unexpected method: %s", method)
		}
		return json.RawMessage(`{"hash":"0xabc","number":"0x64"}`), nil
	})
	defer cleanup()

	z, err := NewZAP(addr, "http://localhost/ext/bc/C/rpc")
	if err != nil {
		t.Fatalf("NewZAP: %v", err)
	}
	defer z.Close()

	result, err := z.Call(context.Background(), "eth_getBlockByNumber", "0x64", true)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var block struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(result, &block); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if block.Hash != "0xabc" {
		t.Errorf("hash = %q, want %q", block.Hash, "0xabc")
	}
}

func TestZAPBatchCall(t *testing.T) {
	addr, cleanup := mockZAPServer(t, func(reqID uint32, method string, params json.RawMessage) (json.RawMessage, error) {
		switch method {
		case "eth_blockNumber":
			return json.RawMessage(`"0x64"`), nil
		case "eth_chainId":
			return json.RawMessage(`"0x178a1"`), nil
		case "net_version":
			return json.RawMessage(`"96369"`), nil
		default:
			return nil, fmt.Errorf("unexpected method: %s", method)
		}
	})
	defer cleanup()

	z, err := NewZAP(addr, "http://localhost/ext/bc/C/rpc")
	if err != nil {
		t.Fatalf("NewZAP: %v", err)
	}
	defer z.Close()

	calls := []RPCCall{
		{Method: "eth_blockNumber"},
		{Method: "eth_chainId"},
		{Method: "net_version"},
	}

	results, err := z.BatchCall(context.Background(), calls)
	if err != nil {
		t.Fatalf("BatchCall failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
}

func TestZAPCallRPCError(t *testing.T) {
	addr, cleanup := mockZAPServer(t, func(reqID uint32, method string, params json.RawMessage) (json.RawMessage, error) {
		return nil, fmt.Errorf("method not found")
	})
	defer cleanup()

	z, err := NewZAP(addr, "http://localhost/ext/bc/C/rpc")
	if err != nil {
		t.Fatalf("NewZAP: %v", err)
	}
	defer z.Close()

	_, err = z.Call(context.Background(), "bad_method")
	if err == nil {
		t.Fatal("expected error for RPC error response")
	}
}

func TestZAPClose(t *testing.T) {
	addr, cleanup := mockZAPServer(t, func(reqID uint32, method string, params json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})
	defer cleanup()

	z, err := NewZAP(addr, "http://localhost/ext/bc/C/rpc")
	if err != nil {
		t.Fatalf("NewZAP: %v", err)
	}

	if err := z.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Calling after close should return error
	_, err = z.Call(context.Background(), "eth_blockNumber")
	if err == nil {
		t.Fatal("expected error after close")
	}
}

func TestZAPConnectFail(t *testing.T) {
	_, err := NewZAP("127.0.0.1:1", "http://localhost/ext/bc/C/rpc")
	if err == nil {
		t.Fatal("expected error connecting to closed port")
	}
}

func TestZAPCallContextTimeout(t *testing.T) {
	// Server that never responds
	addr, cleanup := mockZAPServer(t, func(reqID uint32, method string, params json.RawMessage) (json.RawMessage, error) {
		time.Sleep(10 * time.Second)
		return json.RawMessage(`"ok"`), nil
	})
	defer cleanup()

	z, err := NewZAP(addr, "http://localhost/ext/bc/C/rpc")
	if err != nil {
		t.Fatalf("NewZAP: %v", err)
	}
	defer z.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = z.Call(ctx, "eth_blockNumber")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// --- Transport interface compliance ---

func TestHTTPImplementsTransport(t *testing.T) {
	var _ Transport = (*HTTP)(nil)
}

func TestZAPImplementsTransport(t *testing.T) {
	var _ Transport = (*ZAP)(nil)
}
