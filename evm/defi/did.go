// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package defi

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// DID Registry event signatures (keccak256)
// Source: ~/work/lux/standard/contracts/did/Registry.sol
var (
	DIDIdentityClaimedSig    = "0x36b76e9e1e0bbe49825fb2750f83df22ceca20a579044f34abecc6ef69f6506b" // IdentityClaimed(string,uint256,address)
	DIDIdentityUnclaimedSig  = "0x0a9f93ae44cd5c1f19ae3be343611037fa88d62e1b8e126ae4b9f106c2e816a8" // IdentityUnclaimed(string,uint256)
	DIDKeysUpdatedSig        = "0x5c36d886e638c9dc8b92545612b4ece4ee58ebab03cc9a8c0d4479d8d1b31619" // KeysUpdated(string,string,string)
	DIDNodesUpdatedSig       = "0x0f70a42664b87902e8034c885228f234fa8a9dfbf2cb56441f3bd9c670cb41b7" // NodesUpdated(string,bool,string[])
	DIDStakeUpdatedSig       = "0x77541b7d81884eeeb11898d7c90e813c9b5ad6099325eab22ffb63626b6410b3" // StakeUpdated(string,uint256)
	DIDDelegationsUpdatedSig = "0x73dbe096a4ec6394ebd232acca366a293a2bbc5b578e2884544559fd7a746638" // DelegationsUpdated(string,(string,uint256)[])
	DIDRecordsUpdatedSig     = "0xf04a7deeedf3e3466b1772adca6709e2f7607ec7242661d060c999ce158bd1cb" // RecordsUpdated(string,string[],string[])
)

// DIDIndexer indexes DID Registry events
type DIDIndexer struct {
	identities map[string]*DIDIdentity // did -> identity
	events     []*DIDEvent
	onClaim    func(*DIDEvent)
	onUnclaim  func(*DIDEvent)
	onUpdate   func(*DIDEvent)
}

// DIDIdentity represents a registered decentralized identity
type DIDIdentity struct {
	DID           string    `json:"did"`
	Owner         string    `json:"owner"`
	NFTID         *big.Int  `json:"nftId"`
	StakedTokens  *big.Int  `json:"stakedTokens"`
	EncryptionKey string    `json:"encryptionKey,omitempty"`
	SignatureKey  string    `json:"signatureKey,omitempty"`
	IsActive      bool      `json:"isActive"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// DIDEvent represents a DID registry event
type DIDEvent struct {
	ID          string    `json:"id"`
	TxHash      string    `json:"txHash"`
	BlockNumber uint64    `json:"blockNumber"`
	LogIndex    uint64    `json:"logIndex"`
	Contract    string    `json:"contract"`
	EventType   string    `json:"eventType"`
	DID         string    `json:"did,omitempty"`
	Owner       string    `json:"owner,omitempty"`
	NFTID       *big.Int  `json:"nftId,omitempty"`
	Stake       *big.Int  `json:"stake,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// NewDIDIndexer creates a new DID indexer
func NewDIDIndexer() *DIDIndexer {
	return &DIDIndexer{
		identities: make(map[string]*DIDIdentity),
		events:     make([]*DIDEvent, 0),
	}
}

// SetCallbacks sets event callbacks
func (d *DIDIndexer) SetCallbacks(onClaim, onUnclaim, onUpdate func(*DIDEvent)) {
	d.onClaim = onClaim
	d.onUnclaim = onUnclaim
	d.onUpdate = onUpdate
}

// IndexLog processes a log entry for DID events
func (d *DIDIndexer) IndexLog(log *LogEntry) error {
	if len(log.Topics) == 0 {
		return nil
	}

	switch log.Topics[0] {
	case DIDIdentityClaimedSig:
		return d.indexIdentityClaimed(log)
	case DIDIdentityUnclaimedSig:
		return d.indexIdentityUnclaimed(log)
	case DIDKeysUpdatedSig:
		return d.indexKeysUpdated(log)
	case DIDStakeUpdatedSig:
		return d.indexStakeUpdated(log)
	case DIDNodesUpdatedSig, DIDDelegationsUpdatedSig, DIDRecordsUpdatedSig:
		return d.indexGenericUpdate(log)
	}

	return nil
}

// indexIdentityClaimed processes IdentityClaimed(string indexed did, uint256 indexed nftId, address indexed owner)
func (d *DIDIndexer) indexIdentityClaimed(log *LogEntry) error {
	if len(log.Topics) < 4 {
		return fmt.Errorf("invalid IdentityClaimed event: need 4 topics, got %d", len(log.Topics))
	}

	// topic1 = keccak256(did), topic2 = nftId, topic3 = owner
	nftID := new(big.Int).SetBytes(hexToBytes(log.Topics[2]))
	owner := topicToAddress(log.Topics[3])

	// DID string is in the indexed topic hash; we store the hash as identifier
	didHash := log.Topics[1]

	evt := &DIDEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "identity_claimed",
		DID:         didHash,
		Owner:       owner,
		NFTID:       nftID,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	identity := &DIDIdentity{
		DID:          didHash,
		Owner:        owner,
		NFTID:        nftID,
		StakedTokens: big.NewInt(0),
		IsActive:     true,
		CreatedAt:    log.Timestamp,
		UpdatedAt:    log.Timestamp,
	}
	d.identities[didHash] = identity

	if d.onClaim != nil {
		d.onClaim(evt)
	}
	return nil
}

// indexIdentityUnclaimed processes IdentityUnclaimed(string indexed did, uint256 indexed nftId)
func (d *DIDIndexer) indexIdentityUnclaimed(log *LogEntry) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("invalid IdentityUnclaimed event: need 3 topics, got %d", len(log.Topics))
	}

	didHash := log.Topics[1]
	nftID := new(big.Int).SetBytes(hexToBytes(log.Topics[2]))

	evt := &DIDEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "identity_unclaimed",
		DID:         didHash,
		NFTID:       nftID,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	if identity, ok := d.identities[didHash]; ok {
		identity.IsActive = false
		identity.UpdatedAt = log.Timestamp
	}

	if d.onUnclaim != nil {
		d.onUnclaim(evt)
	}
	return nil
}

// indexKeysUpdated processes KeysUpdated(string indexed did, string encryptionKey, string signatureKey)
func (d *DIDIndexer) indexKeysUpdated(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid KeysUpdated event: need 2 topics, got %d", len(log.Topics))
	}

	didHash := log.Topics[1]

	evt := &DIDEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "keys_updated",
		DID:         didHash,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	if identity, ok := d.identities[didHash]; ok {
		identity.UpdatedAt = log.Timestamp
	}

	if d.onUpdate != nil {
		d.onUpdate(evt)
	}
	return nil
}

// indexStakeUpdated processes StakeUpdated(string indexed did, uint256 newStake)
func (d *DIDIndexer) indexStakeUpdated(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid StakeUpdated event: need 2 topics, got %d", len(log.Topics))
	}

	data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
	if err != nil {
		return err
	}
	if len(data) < 32 {
		return fmt.Errorf("invalid StakeUpdated data: need 32 bytes, got %d", len(data))
	}

	didHash := log.Topics[1]
	stake := new(big.Int).SetBytes(data[0:32])

	evt := &DIDEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   "stake_updated",
		DID:         didHash,
		Stake:       stake,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	if identity, ok := d.identities[didHash]; ok {
		identity.StakedTokens = stake
		identity.UpdatedAt = log.Timestamp
	}

	if d.onUpdate != nil {
		d.onUpdate(evt)
	}
	return nil
}

// indexGenericUpdate processes NodesUpdated, DelegationsUpdated, RecordsUpdated
func (d *DIDIndexer) indexGenericUpdate(log *LogEntry) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("invalid DID update event: need 2 topics, got %d", len(log.Topics))
	}

	var eventType string
	switch log.Topics[0] {
	case DIDNodesUpdatedSig:
		eventType = "nodes_updated"
	case DIDDelegationsUpdatedSig:
		eventType = "delegations_updated"
	case DIDRecordsUpdatedSig:
		eventType = "records_updated"
	}

	didHash := log.Topics[1]

	evt := &DIDEvent{
		ID:          fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.LogIndex,
		Contract:    log.Address,
		EventType:   eventType,
		DID:         didHash,
		Timestamp:   log.Timestamp,
	}
	d.events = append(d.events, evt)

	if identity, ok := d.identities[didHash]; ok {
		identity.UpdatedAt = log.Timestamp
	}

	if d.onUpdate != nil {
		d.onUpdate(evt)
	}
	return nil
}

// GetIdentity returns an identity by DID hash
func (d *DIDIndexer) GetIdentity(didHash string) *DIDIdentity {
	return d.identities[didHash]
}

// GetAllIdentities returns all identities
func (d *DIDIndexer) GetAllIdentities() []*DIDIdentity {
	result := make([]*DIDIdentity, 0, len(d.identities))
	for _, id := range d.identities {
		result = append(result, id)
	}
	return result
}

// GetActiveIdentities returns active identities
func (d *DIDIndexer) GetActiveIdentities() []*DIDIdentity {
	var result []*DIDIdentity
	for _, id := range d.identities {
		if id.IsActive {
			result = append(result, id)
		}
	}
	return result
}

// GetEventsByDID returns events for a specific DID hash
func (d *DIDIndexer) GetEventsByDID(didHash string) []*DIDEvent {
	var result []*DIDEvent
	for _, evt := range d.events {
		if evt.DID == didHash {
			result = append(result, evt)
		}
	}
	return result
}

// DIDStats represents DID indexer statistics
type DIDStats struct {
	TotalIdentities  uint64    `json:"totalIdentities"`
	ActiveIdentities uint64    `json:"activeIdentities"`
	TotalEvents      uint64    `json:"totalEvents"`
	LastUpdated      time.Time `json:"lastUpdated"`
}

// GetStats returns aggregate statistics
func (d *DIDIndexer) GetStats() *DIDStats {
	active := uint64(0)
	for _, id := range d.identities {
		if id.IsActive {
			active++
		}
	}
	return &DIDStats{
		TotalIdentities:  uint64(len(d.identities)),
		ActiveIdentities: active,
		TotalEvents:      uint64(len(d.events)),
		LastUpdated:      time.Now(),
	}
}
