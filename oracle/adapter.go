// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package oracle provides the O-Chain adapter for the DAG indexer.
package oracle

import "github.com/luxfi/indexer/rpcadapter"

const (
	DefaultPort     = 5300
	DefaultDatabase = "explorer_oracle"
	MethodPrefix    = "ovm"
)

// New creates a new O-Chain adapter.
func New(rpcEndpoint string) *rpcadapter.Adapter {
	return rpcadapter.New(rpcEndpoint, MethodPrefix)
}
