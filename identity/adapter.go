// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package identity provides the I-Chain adapter for the DAG indexer.
package identity

import "github.com/luxfi/indexer/rpcadapter"

const (
	DefaultPort     = 5100
	DefaultDatabase = "explorer_identity"
	MethodPrefix    = "ivm"
)

// New creates a new I-Chain adapter.
func New(rpcEndpoint string) *rpcadapter.Adapter {
	return rpcadapter.New(rpcEndpoint, MethodPrefix)
}
