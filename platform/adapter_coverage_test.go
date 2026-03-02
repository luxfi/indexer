// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/luxfi/indexer/chain"
)

// TestParseBlockTxIDs verifies transaction IDs are extracted from block
func TestParseBlockTxIDs(t *testing.T) {
	adapter := New("")
	data := json.RawMessage(`{
		"id": "block-txids",
		"parentID": "block-parent",
		"height": 50,
		"timestamp": 1700000000,
		"txs": [
			{"id": "tx-a", "type": "AddValidatorTx"},
			{"id": "tx-b", "type": "AddDelegatorTx"},
			{"id": "tx-c", "type": "CreateNetTx"}
		]
	}`)

	block, err := adapter.ParseBlock(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(block.TxIDs) != 3 {
		t.Errorf("expected 3 txIDs, got %d", len(block.TxIDs))
	}
	if block.TxIDs[0] != "tx-a" {
		t.Errorf("expected first txID tx-a, got %s", block.TxIDs[0])
	}
	if block.TxIDs[2] != "tx-c" {
		t.Errorf("expected third txID tx-c, got %s", block.TxIDs[2])
	}
}

// TestParseBlockStatus verifies block status is always accepted
func TestParseBlockStatus(t *testing.T) {
	adapter := New("")
	data := json.RawMessage(`{
		"id": "block-status",
		"parentID": "block-parent",
		"height": 1,
		"timestamp": 1700000000,
		"txs": []
	}`)

	block, err := adapter.ParseBlock(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.Status != chain.StatusAccepted {
		t.Errorf("expected status accepted, got %s", block.Status)
	}
}

// TestParseBlockTimestamp verifies timestamp parsing
func TestParseBlockTimestamp(t *testing.T) {
	adapter := New("")
	data := json.RawMessage(`{
		"id": "block-ts",
		"parentID": "block-parent",
		"height": 1,
		"timestamp": 1700000000,
		"txs": []
	}`)

	block, err := adapter.ParseBlock(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Unix(1700000000, 0)
	if !block.Timestamp.Equal(expected) {
		t.Errorf("expected timestamp %v, got %v", expected, block.Timestamp)
	}
}

// TestExtractTxTypesEmpty tests tx type extraction with no transactions
func TestExtractTxTypesEmpty(t *testing.T) {
	types := extractTxTypes([]PChainTx{})
	if len(types) != 0 {
		t.Errorf("expected empty map, got %v", types)
	}
}

// TestExtractTxTypesCounting tests tx type counting
func TestExtractTxTypesCounting(t *testing.T) {
	txs := []PChainTx{
		{ID: "tx1", Type: "AddValidatorTx"},
		{ID: "tx2", Type: "AddValidatorTx"},
		{ID: "tx3", Type: "AddValidatorTx"},
		{ID: "tx4", Type: "AddDelegatorTx"},
		{ID: "tx5", Type: "CreateNetTx"},
		{ID: "tx6", Type: "CreateChainTx"},
		{ID: "tx7", Type: "CreateChainTx"},
	}

	types := extractTxTypes(txs)
	if types["AddValidatorTx"] != 3 {
		t.Errorf("AddValidatorTx: got %d, want 3", types["AddValidatorTx"])
	}
	if types["AddDelegatorTx"] != 1 {
		t.Errorf("AddDelegatorTx: got %d, want 1", types["AddDelegatorTx"])
	}
	if types["CreateNetTx"] != 1 {
		t.Errorf("CreateNetTx: got %d, want 1", types["CreateNetTx"])
	}
	if types["CreateChainTx"] != 2 {
		t.Errorf("CreateChainTx: got %d, want 2", types["CreateChainTx"])
	}
}

// TestGetCurrentValidatorsWithNetID tests fetching validators for a specific subnet
func TestGetCurrentValidatorsWithNetID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		params := req["params"].(map[string]interface{})
		if params["netID"] != "subnet123" {
			t.Errorf("expected netID subnet123, got %v", params["netID"])
		}

		w.Write([]byte(`{
			"jsonrpc":"2.0","id":1,
			"result":{"validators":[{"nodeID":"NodeID-subnet","stakeAmount":"1000000"}]}
		}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	validators, err := adapter.GetCurrentValidators(context.Background(), "subnet123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(validators) != 1 {
		t.Errorf("expected 1 validator, got %d", len(validators))
	}
}

// TestGetCurrentValidatorsPrimary tests fetching validators for primary network
func TestGetCurrentValidatorsPrimary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		params := req["params"].(map[string]interface{})
		if _, ok := params["netID"]; ok {
			t.Error("primary network should not include netID param")
		}

		w.Write([]byte(`{
			"jsonrpc":"2.0","id":1,
			"result":{"validators":[]}
		}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetCurrentValidators(context.Background(), "primary")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestGetPendingValidatorsRPCError tests RPC error from GetPendingValidators
func TestGetPendingValidatorsRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"not available"}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetPendingValidators(context.Background(), "")
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestGetBlockByIDRPCError tests RPC error from GetBlockByID
func TestGetBlockByIDRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"block not found"}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetBlockByID(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestGetBlockByHeightRPCError tests RPC error from GetBlockByHeight
func TestGetBlockByHeightRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"height not found"}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetBlockByHeight(context.Background(), 999999)
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestGetNetsRPCError tests RPC error from GetNets
func TestGetNetsRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"unavailable"}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetNets(context.Background())
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestGetBlockchainsRPCError tests RPC error from GetBlockchains
func TestGetBlockchainsRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"unavailable"}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetBlockchains(context.Background())
	if err == nil {
		t.Error("expected error for RPC error response")
	}
}

// TestValidatorWithDelegators tests Validator struct with nested delegators
func TestValidatorWithDelegators(t *testing.T) {
	validator := Validator{
		TxID:            "tx-val",
		NodeID:          "NodeID-test",
		StartTime:       "1700000000",
		EndTime:         "1731536000",
		Weight:          "2000000000000000",
		PotentialReward: "100000000",
		DelegationFee:   "2.0",
		Uptime:          "99.9",
		Connected:       true,
		Delegators: []Delegator{
			{TxID: "del1", NodeID: "NodeID-test", StakeAmount: "500000000"},
			{TxID: "del2", NodeID: "NodeID-test", StakeAmount: "300000000"},
			{TxID: "del3", NodeID: "NodeID-test", StakeAmount: "200000000"},
		},
	}

	data, err := json.Marshal(validator)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Validator
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.Delegators) != 3 {
		t.Errorf("expected 3 delegators, got %d", len(decoded.Delegators))
	}
	if decoded.Uptime != "99.9" {
		t.Errorf("expected uptime 99.9, got %s", decoded.Uptime)
	}
}

// TestPChainTxAllTypes tests all P-Chain transaction types
func TestPChainTxAllTypes(t *testing.T) {
	txTypes := []string{
		"AddValidatorTx",
		"AddDelegatorTx",
		"CreateNetTx",
		"CreateChainTx",
		"ImportTx",
		"ExportTx",
		"AddSubnetValidatorTx",
		"RemoveSubnetValidatorTx",
		"TransformSubnetTx",
	}

	for _, txType := range txTypes {
		t.Run(txType, func(t *testing.T) {
			tx := PChainTx{
				ID:   "tx-" + txType,
				Type: txType,
			}

			data, err := json.Marshal(tx)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var decoded PChainTx
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if decoded.Type != txType {
				t.Errorf("expected type %s, got %s", txType, decoded.Type)
			}
		})
	}
}

// TestPChainTxSubnetFields tests subnet-specific fields on PChainTx
func TestPChainTxSubnetFields(t *testing.T) {
	tx := PChainTx{
		ID:        "tx-subnet",
		Type:      "AddSubnetValidatorTx",
		NodeID:    "NodeID-subnet-val",
		StartTime: 1700000000,
		EndTime:   1731536000,
		Weight:    1000000000000000,
		NetID:     "subnet-xyz",
	}

	data, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded PChainTx
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.NetID != "subnet-xyz" {
		t.Errorf("expected NetID subnet-xyz, got %s", decoded.NetID)
	}
	if decoded.NodeID != "NodeID-subnet-val" {
		t.Errorf("expected NodeID NodeID-subnet-val, got %s", decoded.NodeID)
	}
}

// TestNewConfigPollInterval verifies poll interval
func TestNewConfigPollInterval(t *testing.T) {
	config := NewConfig()
	if config.PollInterval != 5*time.Second {
		t.Errorf("expected 5s poll interval, got %v", config.PollInterval)
	}
	if config.RPCMethod != "pvm" {
		t.Errorf("expected RPCMethod pvm, got %s", config.RPCMethod)
	}
}

// TestGetRecentBlocksHeightRPCError tests error when height fetch fails
func TestGetRecentBlocksHeightRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"not synced"}}`))
	}))
	defer server.Close()

	adapter := New(server.URL)
	_, err := adapter.GetRecentBlocks(context.Background(), 5)
	if err == nil {
		t.Error("expected error when height RPC fails")
	}
}

// TestBlockchainFullFields tests Blockchain struct with all fields
func TestBlockchainFullFields(t *testing.T) {
	bc := Blockchain{
		ID:    "chain-full",
		Name:  "Zoo Chain",
		NetID: "subnet-zoo",
		VMID:  "srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy",
	}

	data, err := json.Marshal(bc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Blockchain
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Name != "Zoo Chain" {
		t.Errorf("expected name Zoo Chain, got %s", decoded.Name)
	}
	if decoded.NetID != "subnet-zoo" {
		t.Errorf("expected NetID subnet-zoo, got %s", decoded.NetID)
	}
}
