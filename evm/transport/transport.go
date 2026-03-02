// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package transport provides pluggable RPC transports for the EVM indexer.
// Two implementations: HTTP JSON-RPC (standard) and ZAP (luxfi/zap binary wire protocol).
// ZAP is auto-selected when the endpoint matches a luxd node pattern.
package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Transport is the interface for making RPC calls to an EVM node.
type Transport interface {
	Call(ctx context.Context, method string, params ...any) (json.RawMessage, error)
	BatchCall(ctx context.Context, calls []RPCCall) ([]json.RawMessage, error)
	Subscribe(ctx context.Context, method string, params ...any) (<-chan json.RawMessage, error)
	Close() error
}

// RPCCall represents a single JSON-RPC call in a batch.
type RPCCall struct {
	Method string
	Params []any
}

// New creates a Transport for the given endpoint, auto-detecting ZAP vs HTTP.
// ZAP is used when the endpoint contains "/ext/" (luxd node path pattern)
// and a ZAP port is reachable. Falls back to HTTP otherwise.
func New(endpoint string) Transport {
	if isLuxdEndpoint(endpoint) {
		zapAddr := luxdZAPAddr(endpoint)
		t, err := NewZAP(zapAddr, endpoint)
		if err == nil {
			return t
		}
		// Fall back to HTTP if ZAP connection fails
	}
	return NewHTTP(endpoint)
}

// isLuxdEndpoint returns true if the endpoint looks like a luxd node.
func isLuxdEndpoint(endpoint string) bool {
	return strings.Contains(endpoint, "/ext/")
}

// luxdZAPAddr derives the ZAP TCP address from a luxd HTTP endpoint.
// luxd listens for ZAP on HTTP port + 10 by convention.
// e.g. http://luxd-0.luxd:9630/ext/bc/C/rpc -> luxd-0.luxd:9640
func luxdZAPAddr(endpoint string) string {
	// Strip scheme
	addr := endpoint
	for _, prefix := range []string{"https://", "http://"} {
		addr = strings.TrimPrefix(addr, prefix)
	}
	// Strip path
	if i := strings.Index(addr, "/"); i >= 0 {
		addr = addr[:i]
	}
	// Parse host:port
	host := addr
	port := 9640 // default ZAP port
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		host = addr[:i]
		var p int
		if _, err := fmt.Sscanf(addr[i+1:], "%d", &p); err == nil {
			port = p + 10
		}
	}
	return fmt.Sprintf("%s:%d", host, port)
}
