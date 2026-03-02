// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package rpcadapter provides the shared JSON-RPC plumbing used by every
// chain-specific DAG adapter (ai, bridge, key, mpc, oracle, quantum,
// threshold, zk, ...). One implementation of the generic Vertex shape,
// one implementation of the request/response dance — each chain adapter
// embeds Adapter and adds only its chain-specific extras.
package rpcadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/luxfi/indexer/dag"
)

// Adapter is the shared JSON-RPC adapter. Every DAG chain's adapter
// package embeds this type and provides its method prefix (e.g. "aivm",
// "bvm", "xvm", "mvm"). The two generic RPCs — getRecentVertices and
// getVertex — dispatch via the prefix.
type Adapter struct {
	rpcEndpoint  string
	client       *http.Client
	methodPrefix string
}

// New constructs an Adapter for the given RPC endpoint and JSON-RPC
// method prefix (without the dot). For example, methodPrefix="aivm"
// dispatches to "aivm.getRecentVertices" and "aivm.getVertex".
func New(rpcEndpoint, methodPrefix string) *Adapter {
	return &Adapter{
		rpcEndpoint:  rpcEndpoint,
		client:       &http.Client{Timeout: 30 * time.Second},
		methodPrefix: methodPrefix,
	}
}

// Endpoint returns the configured RPC endpoint (for logging / tests).
func (a *Adapter) Endpoint() string { return a.rpcEndpoint }

// MethodPrefix returns the JSON-RPC method prefix (e.g. "aivm").
func (a *Adapter) MethodPrefix() string { return a.methodPrefix }

// Client exposes the underlying http.Client so chain-specific adapters
// can reuse it for custom RPCs without opening a second pool.
func (a *Adapter) Client() *http.Client { return a.client }

// ParseVertex parses a generic DAG vertex from the RPC response. Chain-
// specific adapters that need extra metadata should override this by
// wrapping the result and copying fields.
func (a *Adapter) ParseVertex(data json.RawMessage) (*dag.Vertex, error) {
	return ParseVertex(data)
}

// GetRecentVertices fetches the N most recent vertices.
func (a *Adapter) GetRecentVertices(ctx context.Context, limit int) ([]json.RawMessage, error) {
	var result struct {
		Vertices []json.RawMessage `json:"vertices"`
	}
	if err := a.Call(ctx, a.methodPrefix+".getRecentVertices",
		map[string]interface{}{"limit": limit}, &result); err != nil {
		return nil, err
	}
	return result.Vertices, nil
}

// GetVertexByID fetches a specific vertex by ID.
func (a *Adapter) GetVertexByID(ctx context.Context, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := a.Call(ctx, a.methodPrefix+".getVertex",
		map[string]interface{}{"vertexID": id}, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// Call is the generic JSON-RPC 2.0 request helper. Chain-specific
// adapters invoke this for their custom methods.
func (a *Adapter) Call(ctx context.Context, method string, params any, out any) error {
	body, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.rpcEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("rpc call: %w", err)
	}
	defer resp.Body.Close()

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *Error          `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("rpc error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(envelope.Result, out); err != nil {
		return fmt.Errorf("unmarshal result: %w", err)
	}
	return nil
}

// Error is the JSON-RPC 2.0 error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ParseVertex parses a generic DAG vertex. Exposed as a free function
// so chain adapters can reuse it from their own ParseVertex overrides.
func ParseVertex(data json.RawMessage) (*dag.Vertex, error) {
	var raw struct {
		ID        string          `json:"id"`
		Type      string          `json:"type"`
		ParentIDs []string        `json:"parentIds"`
		Height    uint64          `json:"height"`
		Epoch     uint32          `json:"epoch"`
		TxIDs     []string        `json:"txIds"`
		Timestamp int64           `json:"timestamp"`
		Status    string          `json:"status"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal vertex: %w", err)
	}
	v := &dag.Vertex{
		ID:        raw.ID,
		Type:      raw.Type,
		ParentIDs: raw.ParentIDs,
		Height:    raw.Height,
		Epoch:     raw.Epoch,
		TxIDs:     raw.TxIDs,
		Status:    dag.Status(raw.Status),
		Data:      raw.Data,
		Metadata:  make(map[string]interface{}),
	}
	if raw.Timestamp > 0 {
		v.Timestamp = time.Unix(raw.Timestamp, 0)
	} else {
		v.Timestamp = time.Now()
	}
	return v, nil
}
