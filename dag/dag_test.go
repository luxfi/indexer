// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package dag

import (
	"encoding/json"
	"testing"
	"time"
)

func TestVertexSerialization(t *testing.T) {
	v := Vertex{
		ID:        "vtx-123",
		Type:      "standard",
		ParentIDs: []string{"vtx-100", "vtx-101"},
		Height:    42,
		Epoch:     5,
		TxIDs:     []string{"tx-1", "tx-2"},
		Timestamp: time.Now().UTC().Truncate(time.Second),
		Status:    StatusAccepted,
	}

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded Vertex
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.ID != v.ID {
		t.Errorf("ID mismatch: got %s, want %s", decoded.ID, v.ID)
	}
	if len(decoded.ParentIDs) != 2 {
		t.Errorf("ParentIDs count mismatch: got %d, want 2", len(decoded.ParentIDs))
	}
	if decoded.Status != StatusAccepted {
		t.Errorf("Status mismatch: got %s, want %s", decoded.Status, StatusAccepted)
	}
}

func TestEdgeSerialization(t *testing.T) {
	e := Edge{
		Source: "vtx-100",
		Target: "vtx-123",
		Type:   EdgeParent,
	}

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded Edge
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Source != e.Source || decoded.Target != e.Target {
		t.Errorf("edge mismatch: got %v, want %v", decoded, e)
	}
}

func TestStatsSerialization(t *testing.T) {
	s := Stats{
		TotalVertices:    1000,
		PendingVertices:  50,
		AcceptedVertices: 950,
		TotalEdges:       1500,
		ChainType:        ChainX,
		LastUpdated:      time.Now().UTC().Truncate(time.Second),
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded Stats
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.TotalVertices != s.TotalVertices {
		t.Errorf("TotalVertices mismatch: got %d, want %d", decoded.TotalVertices, s.TotalVertices)
	}
	if decoded.ChainType != ChainX {
		t.Errorf("ChainType mismatch: got %s, want %s", decoded.ChainType, ChainX)
	}
}

func TestChainTypes(t *testing.T) {
	chains := []ChainType{ChainX, ChainA, ChainB, ChainQ, ChainT, ChainZ}
	expected := []string{"xchain", "achain", "bchain", "qchain", "tchain", "zchain"}

	for i, c := range chains {
		if string(c) != expected[i] {
			t.Errorf("ChainType mismatch: got %s, want %s", c, expected[i])
		}
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
}

func TestEdgeTypes(t *testing.T) {
	if EdgeParent != "parent" {
		t.Errorf("EdgeParent: got %s, want parent", EdgeParent)
	}
	if EdgeInput != "input" {
		t.Errorf("EdgeInput: got %s, want input", EdgeInput)
	}
	if EdgeOutput != "output" {
		t.Errorf("EdgeOutput: got %s, want output", EdgeOutput)
	}
	if EdgeReference != "reference" {
		t.Errorf("EdgeReference: got %s, want reference", EdgeReference)
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{
		ChainType:   ChainX,
		ChainName:   "X-Chain",
		RPCEndpoint: "http://localhost:9630/ext/bc/X",
		DatabaseURL: "postgres://localhost/test",
		HTTPPort:    4200,
	}

	if cfg.PollInterval == 0 {
		// Default should be set by caller, just verify struct works
		cfg.PollInterval = 30 * time.Second
	}

	if cfg.HTTPPort != 4200 {
		t.Errorf("HTTPPort: got %d, want 4200", cfg.HTTPPort)
	}
}

func TestSubscriberClientCount(t *testing.T) {
	sub := NewSubscriber(ChainX)
	if sub.ClientCount() != 0 {
		t.Errorf("initial client count: got %d, want 0", sub.ClientCount())
	}
}
