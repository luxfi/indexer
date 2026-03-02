// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// HTTP implements Transport over standard JSON-RPC.
type HTTP struct {
	endpoint string
	client   *http.Client
	reqID    atomic.Uint64
}

// NewHTTP creates an HTTP JSON-RPC transport.
func NewHTTP(endpoint string) *HTTP {
	return &HTTP{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Call makes a single JSON-RPC call.
func (h *HTTP) Call(ctx context.Context, method string, params ...any) (json.RawMessage, error) {
	if len(params) == 0 {
		params = []any{}
	}
	id := h.reqID.Add(1)

	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      id,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	return h.doRequest(ctx, reqBody)
}

// BatchCall makes multiple JSON-RPC calls in a single HTTP request.
func (h *HTTP) BatchCall(ctx context.Context, calls []RPCCall) ([]json.RawMessage, error) {
	if len(calls) == 0 {
		return nil, nil
	}

	batch := make([]map[string]any, len(calls))
	for i, c := range calls {
		params := c.Params
		if len(params) == 0 {
			params = []any{}
		}
		batch[i] = map[string]any{
			"jsonrpc": "2.0",
			"method":  c.Method,
			"params":  params,
			"id":      h.reqID.Add(1),
		}
	}

	reqBody, err := json.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("marshal batch request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", h.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d from %s", resp.StatusCode, h.endpoint)
	}

	var results []struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
		ID     uint64          `json:"id"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("decode batch response: %w", err)
	}

	out := make([]json.RawMessage, len(results))
	for i, r := range results {
		if r.Error != nil {
			return nil, fmt.Errorf("rpc error %d in batch[%d]: %s", r.Error.Code, i, r.Error.Message)
		}
		out[i] = r.Result
	}

	return out, nil
}

// Subscribe is not supported over HTTP. Returns an error.
func (h *HTTP) Subscribe(_ context.Context, _ string, _ ...any) (<-chan json.RawMessage, error) {
	return nil, fmt.Errorf("subscribe not supported over HTTP transport")
}

// Close is a no-op for HTTP (connections are pooled by http.Client).
func (h *HTTP) Close() error {
	return nil
}

func (h *HTTP) doRequest(ctx context.Context, body []byte) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", h.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d from %s", resp.StatusCode, h.endpoint)
	}

	var result struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", result.Error.Code, result.Error.Message)
	}

	return result.Result, nil
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
