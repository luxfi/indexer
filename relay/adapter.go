// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package relay provides the R-Chain adapter for the DAG indexer.
package relay

import "github.com/luxfi/indexer/rpcadapter"

const (
	DefaultPort     = 5400
	DefaultDatabase = "explorer_relay"
	MethodPrefix    = "rvm"
)

// New creates a new R-Chain adapter.
func New(rpcEndpoint string) *rpcadapter.Adapter {
	return rpcadapter.New(rpcEndpoint, MethodPrefix)
}
