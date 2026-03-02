// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package mpc provides the M-Chain adapter for the DAG indexer.
package mpc

import "github.com/luxfi/indexer/rpcadapter"

const (
	DefaultPort     = 5200
	DefaultDatabase = "explorer_mpc"
	MethodPrefix    = "mvm"
)

// New creates a new M-Chain adapter.
func New(rpcEndpoint string) *rpcadapter.Adapter {
	return rpcadapter.New(rpcEndpoint, MethodPrefix)
}
