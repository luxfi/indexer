// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package chain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBlockSerialization(t *testing.T) {
	b := Block{
		ID:        "blk-123",
		ParentID:  "blk-122",
		Height:    42,
		Timestamp: time.Now().UTC().Truncate(time.Second),
		Status:    StatusAccepted,
		TxCount:   3,
		TxIDs:     []string{"tx-1", "tx-2", "tx-3"},
		Data:      json.RawMessage(`{"validator":"0xabc"}`),
		Metadata:  map[string]interface{}{"proposer": "node1"},
	}

	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded Block
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.ID != b.ID {
		t.Errorf("ID mismatch: got %s, want %s", decoded.ID, b.ID)
	}
	if decoded.ParentID != b.ParentID {
		t.Errorf("ParentID mismatch: got %s, want %s", decoded.ParentID, b.ParentID)
	}
	if decoded.Height != b.Height {
		t.Errorf("Height mismatch: got %d, want %d", decoded.Height, b.Height)
	}
	if decoded.Status != StatusAccepted {
		t.Errorf("Status mismatch: got %s, want %s", decoded.Status, StatusAccepted)
	}
	if decoded.TxCount != b.TxCount {
		t.Errorf("TxCount mismatch: got %d, want %d", decoded.TxCount, b.TxCount)
	}
	if len(decoded.TxIDs) != 3 {
		t.Errorf("TxIDs count mismatch: got %d, want 3", len(decoded.TxIDs))
	}
}

func TestBlockSerializationMinimal(t *testing.T) {
	// Test block with only required fields
	b := Block{
		ID:        "blk-001",
		ParentID:  "",
		Height:    0,
		Timestamp: time.Now().UTC().Truncate(time.Second),
		Status:    StatusPending,
	}

	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded Block
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.ID != b.ID {
		t.Errorf("ID mismatch: got %s, want %s", decoded.ID, b.ID)
	}
	if decoded.Height != 0 {
		t.Errorf("Height mismatch: got %d, want 0", decoded.Height)
	}
	if decoded.TxCount != 0 {
		t.Errorf("TxCount should be 0 for minimal block: got %d", decoded.TxCount)
	}
}

func TestStatsSerialization(t *testing.T) {
	s := Stats{
		TotalBlocks:    1000,
		LatestHeight:   999,
		PendingBlocks:  50,
		AcceptedBlocks: 950,
		ChainType:      ChainP,
		LastUpdated:    time.Now().UTC().Truncate(time.Second),
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded Stats
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.TotalBlocks != s.TotalBlocks {
		t.Errorf("TotalBlocks mismatch: got %d, want %d", decoded.TotalBlocks, s.TotalBlocks)
	}
	if decoded.LatestHeight != s.LatestHeight {
		t.Errorf("LatestHeight mismatch: got %d, want %d", decoded.LatestHeight, s.LatestHeight)
	}
	if decoded.PendingBlocks != s.PendingBlocks {
		t.Errorf("PendingBlocks mismatch: got %d, want %d", decoded.PendingBlocks, s.PendingBlocks)
	}
	if decoded.AcceptedBlocks != s.AcceptedBlocks {
		t.Errorf("AcceptedBlocks mismatch: got %d, want %d", decoded.AcceptedBlocks, s.AcceptedBlocks)
	}
	if decoded.ChainType != ChainP {
		t.Errorf("ChainType mismatch: got %s, want %s", decoded.ChainType, ChainP)
	}
}

func TestChainTypeValues(t *testing.T) {
	if string(ChainP) != "pchain" {
		t.Errorf("ChainP: got %s, want pchain", ChainP)
	}
}

func TestStatusValues(t *testing.T) {
	if StatusPending != "pending" {
		t.Errorf("StatusPending: got %s, want pending", StatusPending)
	}
	if StatusAccepted != "accepted" {
		t.Errorf("StatusAccepted: got %s, want accepted", StatusAccepted)
	}
	if StatusRejected != "rejected" {
		t.Errorf("StatusRejected: got %s, want rejected", StatusRejected)
	}
	if StatusFinalized != "finalized" {
		t.Errorf("StatusFinalized: got %s, want finalized", StatusFinalized)
	}
}

func TestConfigValidation(t *testing.T) {
	cfg := Config{
		ChainType:   ChainP,
		ChainName:   "P-Chain",
		RPCEndpoint: "http://localhost:9630/ext/bc/P",
		RPCMethod:   "pvm",
		DataDir:     "/tmp/test-indexer/pchain",
		HTTPPort:    4100,
	}

	if cfg.PollInterval == 0 {
		// Default should be set by caller, verify struct accepts value
		cfg.PollInterval = 30 * time.Second
	}

	if cfg.HTTPPort != 4100 {
		t.Errorf("HTTPPort: got %d, want 4100", cfg.HTTPPort)
	}
	if cfg.ChainType != ChainP {
		t.Errorf("ChainType: got %s, want %s", cfg.ChainType, ChainP)
	}
	if cfg.RPCMethod != "pvm" {
		t.Errorf("RPCMethod: got %s, want pvm", cfg.RPCMethod)
	}
}

func TestSubscriberClientCount(t *testing.T) {
	sub := NewSubscriber(ChainP)
	if sub.ClientCount() != 0 {
		t.Errorf("initial client count: got %d, want 0", sub.ClientCount())
	}
}

func TestSubscriberChainType(t *testing.T) {
	sub := NewSubscriber(ChainP)
	if sub.chainType != ChainP {
		t.Errorf("subscriber chain type: got %s, want %s", sub.chainType, ChainP)
	}
}

func TestPollerCreation(t *testing.T) {
	sub := NewSubscriber(ChainP)
	p := NewPoller(nil, sub)
	if p.sub != sub {
		t.Error("poller subscriber mismatch")
	}
	if p.lastHeight != 0 {
		t.Errorf("poller initial height: got %d, want 0", p.lastHeight)
	}
}

func TestBlockJSONTags(t *testing.T) {
	b := Block{
		ID:        "test-id",
		ParentID:  "parent-id",
		Height:    100,
		Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Status:    StatusAccepted,
		TxCount:   2,
		TxIDs:     []string{"tx1", "tx2"},
	}

	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Verify JSON field names
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map error: %v", err)
	}

	if _, ok := raw["id"]; !ok {
		t.Error("expected 'id' field in JSON")
	}
	if _, ok := raw["parentId"]; !ok {
		t.Error("expected 'parentId' field in JSON")
	}
	if _, ok := raw["height"]; !ok {
		t.Error("expected 'height' field in JSON")
	}
	if _, ok := raw["status"]; !ok {
		t.Error("expected 'status' field in JSON")
	}
}

func TestStatsJSONTags(t *testing.T) {
	s := Stats{
		TotalBlocks:    100,
		LatestHeight:   99,
		PendingBlocks:  5,
		AcceptedBlocks: 95,
		ChainType:      ChainP,
		LastUpdated:    time.Now().UTC(),
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map error: %v", err)
	}

	expected := []string{"total_blocks", "latest_height", "pending_blocks", "accepted_blocks", "chain_type", "last_updated"}
	for _, field := range expected {
		if _, ok := raw[field]; !ok {
			t.Errorf("expected '%s' field in JSON", field)
		}
	}
}

func TestBlockWithEmptyData(t *testing.T) {
	b := Block{
		ID:        "blk-empty",
		ParentID:  "blk-prev",
		Height:    10,
		Timestamp: time.Now().UTC().Truncate(time.Second),
		Status:    StatusPending,
	}

	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded Block
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(decoded.Data) > 0 {
		t.Errorf("expected empty Data field, got %s", string(decoded.Data))
	}
}

func TestStatusConversion(t *testing.T) {
	statuses := []Status{StatusPending, StatusAccepted, StatusRejected, StatusFinalized}
	expected := []string{"pending", "accepted", "rejected", "finalized"}

	for i, s := range statuses {
		if string(s) != expected[i] {
			t.Errorf("Status conversion: got %s, want %s", s, expected[i])
		}
	}
}
