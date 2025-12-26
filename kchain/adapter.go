// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package kchain provides the K-Chain (KMS) adapter for the DAG indexer.
// Handles distributed key management, threshold signatures, and post-quantum algorithms.
package kchain

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/luxfi/indexer/dag"
	"github.com/luxfi/indexer/storage"
)

const (
	// DefaultPort for K-Chain indexer API
	DefaultPort = 4900
	// DefaultDatabase name
	DefaultDatabase = "explorer_kchain"
)

// KeyStatus represents key lifecycle state
type KeyStatus string

const (
	KeyStatusActive   KeyStatus = "active"
	KeyStatusInactive KeyStatus = "inactive"
	KeyStatusRotating KeyStatus = "rotating"
	KeyStatusRevoked  KeyStatus = "revoked"
	KeyStatusPending  KeyStatus = "pending"
)

// AlgorithmType represents cryptographic algorithm categories
type AlgorithmType string

const (
	AlgorithmTypeKeyExchange AlgorithmType = "key-exchange"
	AlgorithmTypeSigning     AlgorithmType = "signing"
	AlgorithmTypeEncryption  AlgorithmType = "encryption"
)

// Key represents a distributed cryptographic key
type Key struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Algorithm   string    `json:"algorithm"`
	KeyType     string    `json:"keyType"`
	PublicKey   string    `json:"publicKey"`
	Threshold   int       `json:"threshold"`
	TotalShares int       `json:"totalShares"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Status      KeyStatus `json:"status"`
	Tags        []string  `json:"tags"`
}

// KeyOperation represents a key lifecycle operation
type KeyOperation struct {
	ID           string    `json:"id"`
	KeyID        string    `json:"keyId"`
	Operation    string    `json:"operation"` // create, rotate, revoke, delete
	Initiator    string    `json:"initiator"`
	Participants []string  `json:"participants"`
	Success      bool      `json:"success"`
	Error        string    `json:"error,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}

// EncryptionRequest represents an encryption operation
type EncryptionRequest struct {
	ID        string    `json:"id"`
	KeyID     string    `json:"keyId"`
	Requester string    `json:"requester"`
	DataSize  int64     `json:"dataSize"`
	Success   bool      `json:"success"`
	Timestamp time.Time `json:"timestamp"`
}

// SignatureRequest represents a signing operation
type SignatureRequest struct {
	ID          string    `json:"id"`
	KeyID       string    `json:"keyId"`
	Signers     []string  `json:"signers"`
	MessageHash string    `json:"messageHash"`
	Algorithm   string    `json:"algorithm"`
	Success     bool      `json:"success"`
	Timestamp   time.Time `json:"timestamp"`
}

// Algorithm represents a supported cryptographic algorithm
type Algorithm struct {
	Name             string   `json:"name"`
	Type             string   `json:"type"`
	SecurityLevel    int      `json:"securityLevel"`
	KeySize          int      `json:"keySize"`
	SignatureSize    int      `json:"signatureSize"`
	PostQuantum      bool     `json:"postQuantum"`
	ThresholdSupport bool     `json:"thresholdSupport"`
	Description      string   `json:"description"`
	Standards        []string `json:"standards"`
}

// vertexData is the parsed vertex content
type vertexData struct {
	Keys               []Key               `json:"keys,omitempty"`
	KeyOperations      []KeyOperation      `json:"keyOperations,omitempty"`
	EncryptionRequests []EncryptionRequest `json:"encryptionRequests,omitempty"`
	SignatureRequests  []SignatureRequest  `json:"signatureRequests,omitempty"`
}

// Adapter implements the DAG adapter interface for K-Chain
type Adapter struct {
	rpcEndpoint string
	httpClient  *http.Client
}

// New creates a new K-Chain adapter
func New(rpcEndpoint string) *Adapter {
	return &Adapter{
		rpcEndpoint: rpcEndpoint,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ParseVertex parses K-Chain vertex data
func (a *Adapter) ParseVertex(data json.RawMessage) (*dag.Vertex, error) {
	var raw struct {
		ID        string          `json:"id"`
		ParentIDs []string        `json:"parentIds"`
		Height    uint64          `json:"height"`
		Timestamp int64           `json:"timestamp"`
		Status    string          `json:"status"`
		Data      json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal vertex: %w", err)
	}

	vertex := &dag.Vertex{
		ID:        raw.ID,
		ParentIDs: raw.ParentIDs,
		Height:    raw.Height,
		Timestamp: time.Unix(0, raw.Timestamp),
		Status:    dag.Status(raw.Status),
		Data:      raw.Data,
		Type:      a.inferVertexType(raw.Data),
	}

	return vertex, nil
}

// inferVertexType determines the vertex type from its data
func (a *Adapter) inferVertexType(data json.RawMessage) string {
	var v vertexData
	if err := json.Unmarshal(data, &v); err != nil {
		return "unknown"
	}

	if len(v.Keys) > 0 {
		return "keys"
	}
	if len(v.KeyOperations) > 0 {
		return "key_operations"
	}
	if len(v.EncryptionRequests) > 0 {
		return "encryption"
	}
	if len(v.SignatureRequests) > 0 {
		return "signatures"
	}
	return "generic"
}

// GetRecentVertices fetches recent vertices from K-Chain RPC
func (a *Adapter) GetRecentVertices(ctx context.Context, limit int) ([]json.RawMessage, error) {
	return a.call(ctx, "kvm.getRecentVertices", map[string]interface{}{
		"limit": limit,
	})
}

// GetVertexByID fetches a specific vertex by ID
func (a *Adapter) GetVertexByID(ctx context.Context, id string) (json.RawMessage, error) {
	results, err := a.call(ctx, "kvm.getVertex", map[string]interface{}{
		"id": id,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("vertex not found: %s", id)
	}
	return results[0], nil
}

// ListKeys fetches all keys from K-Chain
func (a *Adapter) ListKeys(ctx context.Context, algorithm, status string, offset, limit int) ([]Key, error) {
	results, err := a.call(ctx, "kvm.listKeys", map[string]interface{}{
		"algorithm": algorithm,
		"status":    status,
		"offset":    offset,
		"limit":     limit,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}

	var resp struct {
		Keys  []Key `json:"keys"`
		Total int   `json:"total"`
	}
	if err := json.Unmarshal(results[0], &resp); err != nil {
		return nil, fmt.Errorf("unmarshal keys: %w", err)
	}

	return resp.Keys, nil
}

// GetKeyByID fetches a specific key by ID
func (a *Adapter) GetKeyByID(ctx context.Context, id string) (*Key, error) {
	results, err := a.call(ctx, "kvm.getKeyByID", map[string]interface{}{
		"id": id,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("key not found: %s", id)
	}

	var key Key
	if err := json.Unmarshal(results[0], &key); err != nil {
		return nil, fmt.Errorf("unmarshal key: %w", err)
	}

	return &key, nil
}

// ListAlgorithms fetches supported algorithms
func (a *Adapter) ListAlgorithms(ctx context.Context) ([]Algorithm, error) {
	results, err := a.call(ctx, "kvm.listAlgorithms", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}

	var resp struct {
		Algorithms []Algorithm `json:"algorithms"`
	}
	if err := json.Unmarshal(results[0], &resp); err != nil {
		return nil, fmt.Errorf("unmarshal algorithms: %w", err)
	}

	return resp.Algorithms, nil
}

// call makes a JSON-RPC call to the K-Chain node
func (a *Adapter) call(ctx context.Context, method string, params interface{}) ([]json.RawMessage, error) {
	reqBody, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.rpcEndpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", result.Error.Code, result.Error.Message)
	}

	// Try to decode as array
	var vertices []json.RawMessage
	if err := json.Unmarshal(result.Result, &vertices); err != nil {
		// Single result
		return []json.RawMessage{result.Result}, nil
	}
	return vertices, nil
}

// InitSchema creates K-Chain specific database tables using unified storage
func (a *Adapter) InitSchema(ctx context.Context, store storage.Store) error {
	schema := storage.Schema{
		Name: "kchain",
		Tables: []storage.Table{
			{
				Name: "kchain_keys",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "name", Type: storage.TypeText, Nullable: false},
					{Name: "algorithm", Type: storage.TypeText, Nullable: false},
					{Name: "key_type", Type: storage.TypeText, Nullable: false},
					{Name: "public_key", Type: storage.TypeText},
					{Name: "threshold", Type: storage.TypeInt, Default: "1"},
					{Name: "total_shares", Type: storage.TypeInt, Default: "1"},
					{Name: "status", Type: storage.TypeText, Default: "'pending'"},
					{Name: "tags", Type: storage.TypeText, Default: "'[]'"},
					{Name: "created_at", Type: storage.TypeTimestamp, Nullable: false},
					{Name: "updated_at", Type: storage.TypeTimestamp, Nullable: false},
				},
			},
			{
				Name: "kchain_key_operations",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "key_id", Type: storage.TypeText, Nullable: false},
					{Name: "operation", Type: storage.TypeText, Nullable: false},
					{Name: "initiator", Type: storage.TypeText},
					{Name: "participants", Type: storage.TypeText, Default: "'[]'"},
					{Name: "success", Type: storage.TypeBool, Default: "true"},
					{Name: "error_msg", Type: storage.TypeText},
					{Name: "timestamp", Type: storage.TypeTimestamp, Nullable: false},
				},
			},
			{
				Name: "kchain_encryption_requests",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "key_id", Type: storage.TypeText, Nullable: false},
					{Name: "requester", Type: storage.TypeText, Nullable: false},
					{Name: "data_size", Type: storage.TypeBigInt, Default: "0"},
					{Name: "success", Type: storage.TypeBool, Default: "true"},
					{Name: "timestamp", Type: storage.TypeTimestamp, Nullable: false},
				},
			},
			{
				Name: "kchain_signature_requests",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "key_id", Type: storage.TypeText, Nullable: false},
					{Name: "signers", Type: storage.TypeText, Default: "'[]'"},
					{Name: "message_hash", Type: storage.TypeText, Nullable: false},
					{Name: "algorithm", Type: storage.TypeText, Nullable: false},
					{Name: "success", Type: storage.TypeBool, Default: "true"},
					{Name: "timestamp", Type: storage.TypeTimestamp, Nullable: false},
				},
			},
			{
				Name: "kchain_algorithms",
				Columns: []storage.Column{
					{Name: "name", Type: storage.TypeText, Primary: true},
					{Name: "type", Type: storage.TypeText, Nullable: false},
					{Name: "security_level", Type: storage.TypeInt, Default: "128"},
					{Name: "key_size", Type: storage.TypeInt},
					{Name: "signature_size", Type: storage.TypeInt},
					{Name: "post_quantum", Type: storage.TypeBool, Default: "false"},
					{Name: "threshold_support", Type: storage.TypeBool, Default: "false"},
					{Name: "description", Type: storage.TypeText},
					{Name: "standards", Type: storage.TypeText, Default: "'[]'"},
				},
			},
			{
				Name: "kchain_extended_stats",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeInt, Primary: true},
					{Name: "total_keys", Type: storage.TypeBigInt, Default: "0"},
					{Name: "active_keys", Type: storage.TypeBigInt, Default: "0"},
					{Name: "total_operations", Type: storage.TypeBigInt, Default: "0"},
					{Name: "total_encryptions", Type: storage.TypeBigInt, Default: "0"},
					{Name: "total_signatures", Type: storage.TypeBigInt, Default: "0"},
					{Name: "pq_keys_count", Type: storage.TypeBigInt, Default: "0"},
					{Name: "threshold_keys_count", Type: storage.TypeBigInt, Default: "0"},
					{Name: "algorithm_distribution", Type: storage.TypeText, Default: "'{}'"},
					{Name: "updated_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
		},
		Indexes: []storage.Index{
			{Name: "idx_kchain_keys_name", Table: "kchain_keys", Columns: []string{"name"}},
			{Name: "idx_kchain_keys_algorithm", Table: "kchain_keys", Columns: []string{"algorithm"}},
			{Name: "idx_kchain_keys_status", Table: "kchain_keys", Columns: []string{"status"}},
			{Name: "idx_kchain_keys_created", Table: "kchain_keys", Columns: []string{"created_at"}},
			{Name: "idx_kchain_ops_key", Table: "kchain_key_operations", Columns: []string{"key_id"}},
			{Name: "idx_kchain_ops_operation", Table: "kchain_key_operations", Columns: []string{"operation"}},
			{Name: "idx_kchain_ops_timestamp", Table: "kchain_key_operations", Columns: []string{"timestamp"}},
			{Name: "idx_kchain_enc_key", Table: "kchain_encryption_requests", Columns: []string{"key_id"}},
			{Name: "idx_kchain_enc_requester", Table: "kchain_encryption_requests", Columns: []string{"requester"}},
			{Name: "idx_kchain_enc_timestamp", Table: "kchain_encryption_requests", Columns: []string{"timestamp"}},
			{Name: "idx_kchain_sig_key", Table: "kchain_signature_requests", Columns: []string{"key_id"}},
			{Name: "idx_kchain_sig_timestamp", Table: "kchain_signature_requests", Columns: []string{"timestamp"}},
		},
	}

	if err := store.InitSchema(ctx, schema); err != nil {
		return err
	}

	// Initialize stats row
	if err := store.Exec(ctx, "INSERT OR IGNORE INTO kchain_extended_stats (id) VALUES (1)"); err != nil {
		return err
	}

	// Seed algorithms reference data - simpler INSERT OR IGNORE
	algorithms := []string{
		"INSERT OR IGNORE INTO kchain_algorithms (name, type, security_level, key_size, signature_size, post_quantum, threshold_support, description, standards) VALUES ('ml-kem-768', 'key-exchange', 192, 2400, 0, 1, 0, 'ML-KEM-768 post-quantum key encapsulation', '[\"NIST FIPS 203\"]')",
		"INSERT OR IGNORE INTO kchain_algorithms (name, type, security_level, key_size, signature_size, post_quantum, threshold_support, description, standards) VALUES ('ml-dsa-65', 'signing', 192, 0, 3309, 1, 0, 'ML-DSA-65 post-quantum digital signature', '[\"NIST FIPS 204\"]')",
		"INSERT OR IGNORE INTO kchain_algorithms (name, type, security_level, key_size, signature_size, post_quantum, threshold_support, description, standards) VALUES ('bls-threshold', 'signing', 128, 0, 96, 0, 1, 'BLS12-381 threshold signatures', '[\"IETF BLS Signature\"]')",
		"INSERT OR IGNORE INTO kchain_algorithms (name, type, security_level, key_size, signature_size, post_quantum, threshold_support, description, standards) VALUES ('secp256k1', 'signing', 128, 0, 64, 0, 0, 'ECDSA on secp256k1 (Ethereum compatible)', '[\"SEC 2\"]')",
		"INSERT OR IGNORE INTO kchain_algorithms (name, type, security_level, key_size, signature_size, post_quantum, threshold_support, description, standards) VALUES ('frost-secp256k1', 'signing', 128, 0, 64, 0, 1, 'FROST threshold Schnorr on secp256k1', '[\"IETF FROST\", \"BIP-340\"]')",
	}
	for _, q := range algorithms {
		_ = store.Exec(ctx, q)
	}

	return nil
}

// GetStats returns K-Chain specific statistics using unified storage
func (a *Adapter) GetStats(ctx context.Context, store storage.Store) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Get extended stats
	rows, err := store.Query(ctx, `
		SELECT total_keys, active_keys, total_operations, total_encryptions,
		       total_signatures, pq_keys_count, threshold_keys_count, algorithm_distribution, updated_at
		FROM kchain_extended_stats WHERE id = 1
	`)
	if err != nil {
		return nil, err
	}

	if len(rows) > 0 {
		row := rows[0]
		stats["total_keys"] = row["total_keys"]
		stats["active_keys"] = row["active_keys"]
		stats["total_operations"] = row["total_operations"]
		stats["total_encryptions"] = row["total_encryptions"]
		stats["total_signatures"] = row["total_signatures"]
		stats["post_quantum_keys"] = row["pq_keys_count"]
		stats["threshold_keys"] = row["threshold_keys_count"]
		stats["updated_at"] = row["updated_at"]

		// Parse algorithm distribution if present
		if algDist, ok := row["algorithm_distribution"].(string); ok && algDist != "" {
			var dist map[string]int64
			if err := json.Unmarshal([]byte(algDist), &dist); err == nil {
				stats["algorithm_distribution"] = dist
			}
		}
	}

	// Get recent operations
	opRows, err := store.Query(ctx, `
		SELECT key_id, operation, success, timestamp
		FROM kchain_key_operations
		ORDER BY timestamp DESC
		LIMIT 10
	`)
	if err != nil {
		return nil, err
	}

	var recentOps []map[string]interface{}
	for _, row := range opRows {
		recentOps = append(recentOps, map[string]interface{}{
			"key_id":    row["key_id"],
			"operation": row["operation"],
			"success":   row["success"],
			"timestamp": row["timestamp"],
		})
	}
	stats["recent_operations"] = recentOps

	// Get algorithm usage
	algRows, err := store.Query(ctx, `
		SELECT algorithm, COUNT(*) as count
		FROM kchain_keys
		GROUP BY algorithm
		ORDER BY count DESC
	`)
	if err != nil {
		return nil, err
	}

	var algUsage []map[string]interface{}
	for _, row := range algRows {
		algUsage = append(algUsage, map[string]interface{}{
			"algorithm": row["algorithm"],
			"count":     row["count"],
		})
	}
	stats["algorithm_usage"] = algUsage

	return stats, nil
}

// StoreKey stores a key
func (a *Adapter) StoreKey(ctx context.Context, db *sql.DB, k Key) error {
	tagsJSON, _ := json.Marshal(k.Tags)
	_, err := db.ExecContext(ctx, `
		INSERT INTO kchain_keys
		(id, name, algorithm, key_type, public_key, threshold, total_shares, status, tags, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET
			public_key = EXCLUDED.public_key,
			status = EXCLUDED.status,
			tags = EXCLUDED.tags,
			updated_at = EXCLUDED.updated_at
	`, k.ID, k.Name, k.Algorithm, k.KeyType, k.PublicKey, k.Threshold, k.TotalShares,
		k.Status, tagsJSON, k.CreatedAt, k.UpdatedAt)
	return err
}

// StoreKeyOperation stores a key operation
func (a *Adapter) StoreKeyOperation(ctx context.Context, db *sql.DB, op KeyOperation) error {
	participantsJSON, _ := json.Marshal(op.Participants)
	_, err := db.ExecContext(ctx, `
		INSERT INTO kchain_key_operations
		(id, key_id, operation, initiator, participants, success, error_msg, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO NOTHING
	`, op.ID, op.KeyID, op.Operation, op.Initiator, participantsJSON, op.Success, op.Error, op.Timestamp)
	return err
}

// StoreEncryptionRequest stores an encryption request
func (a *Adapter) StoreEncryptionRequest(ctx context.Context, db *sql.DB, req EncryptionRequest) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO kchain_encryption_requests
		(id, key_id, requester, data_size, success, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO NOTHING
	`, req.ID, req.KeyID, req.Requester, req.DataSize, req.Success, req.Timestamp)
	return err
}

// StoreSignatureRequest stores a signature request
func (a *Adapter) StoreSignatureRequest(ctx context.Context, db *sql.DB, req SignatureRequest) error {
	signersJSON, _ := json.Marshal(req.Signers)
	_, err := db.ExecContext(ctx, `
		INSERT INTO kchain_signature_requests
		(id, key_id, signers, message_hash, algorithm, success, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO NOTHING
	`, req.ID, req.KeyID, signersJSON, req.MessageHash, req.Algorithm, req.Success, req.Timestamp)
	return err
}

// StoreVertex stores vertex data
func (a *Adapter) StoreVertex(ctx context.Context, db *sql.DB, v *dag.Vertex) error {
	var data vertexData
	if err := json.Unmarshal(v.Data, &data); err != nil {
		return fmt.Errorf("unmarshal vertex data: %w", err)
	}

	// Store keys
	for _, k := range data.Keys {
		if err := a.StoreKey(ctx, db, k); err != nil {
			return fmt.Errorf("store key: %w", err)
		}
	}

	// Store key operations
	for _, op := range data.KeyOperations {
		if err := a.StoreKeyOperation(ctx, db, op); err != nil {
			return fmt.Errorf("store key operation: %w", err)
		}
	}

	// Store encryption requests
	for _, req := range data.EncryptionRequests {
		if err := a.StoreEncryptionRequest(ctx, db, req); err != nil {
			return fmt.Errorf("store encryption request: %w", err)
		}
	}

	// Store signature requests
	for _, req := range data.SignatureRequests {
		if err := a.StoreSignatureRequest(ctx, db, req); err != nil {
			return fmt.Errorf("store signature request: %w", err)
		}
	}

	return nil
}

// UpdateExtendedStats updates K-Chain specific statistics
func (a *Adapter) UpdateExtendedStats(ctx context.Context, db *sql.DB) error {
	// Update algorithm distribution
	rows, err := db.QueryContext(ctx, `
		SELECT algorithm, COUNT(*) FROM kchain_keys GROUP BY algorithm
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	algDist := make(map[string]int64)
	for rows.Next() {
		var alg string
		var count int64
		if err := rows.Scan(&alg, &count); err != nil {
			continue
		}
		algDist[alg] = count
	}
	algDistJSON, _ := json.Marshal(algDist)

	_, err = db.ExecContext(ctx, `
		UPDATE kchain_extended_stats SET
			total_keys = (SELECT COUNT(*) FROM kchain_keys),
			active_keys = (SELECT COUNT(*) FROM kchain_keys WHERE status = 'active'),
			total_operations = (SELECT COUNT(*) FROM kchain_key_operations),
			total_encryptions = (SELECT COUNT(*) FROM kchain_encryption_requests),
			total_signatures = (SELECT COUNT(*) FROM kchain_signature_requests),
			pq_keys_count = (SELECT COUNT(*) FROM kchain_keys WHERE algorithm LIKE 'ml-%' OR algorithm = 'ringtail'),
			threshold_keys_count = (SELECT COUNT(*) FROM kchain_keys WHERE threshold > 1),
			algorithm_distribution = $1,
			updated_at = NOW()
		WHERE id = 1
	`, algDistJSON)
	return err
}

// GetKeysByAlgorithm returns keys filtered by algorithm
func (a *Adapter) GetKeysByAlgorithm(ctx context.Context, db *sql.DB, algorithm string, limit, offset int) ([]Key, error) {
	query := `
		SELECT id, name, algorithm, key_type, public_key, threshold, total_shares,
		       status, tags, created_at, updated_at
		FROM kchain_keys
		WHERE algorithm = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := db.QueryContext(ctx, query, algorithm, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []Key
	for rows.Next() {
		var k Key
		var tagsJSON []byte
		if err := rows.Scan(&k.ID, &k.Name, &k.Algorithm, &k.KeyType, &k.PublicKey,
			&k.Threshold, &k.TotalShares, &k.Status, &tagsJSON, &k.CreatedAt, &k.UpdatedAt); err != nil {
			continue
		}
		if len(tagsJSON) > 0 {
			json.Unmarshal(tagsJSON, &k.Tags)
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// GetPostQuantumKeys returns all post-quantum keys
func (a *Adapter) GetPostQuantumKeys(ctx context.Context, db *sql.DB, limit, offset int) ([]Key, error) {
	query := `
		SELECT id, name, algorithm, key_type, public_key, threshold, total_shares,
		       status, tags, created_at, updated_at
		FROM kchain_keys
		WHERE algorithm LIKE 'ml-%' OR algorithm = 'ringtail'
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`
	rows, err := db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []Key
	for rows.Next() {
		var k Key
		var tagsJSON []byte
		if err := rows.Scan(&k.ID, &k.Name, &k.Algorithm, &k.KeyType, &k.PublicKey,
			&k.Threshold, &k.TotalShares, &k.Status, &tagsJSON, &k.CreatedAt, &k.UpdatedAt); err != nil {
			continue
		}
		if len(tagsJSON) > 0 {
			json.Unmarshal(tagsJSON, &k.Tags)
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// GetThresholdKeys returns all threshold keys (threshold > 1)
func (a *Adapter) GetThresholdKeys(ctx context.Context, db *sql.DB, limit, offset int) ([]Key, error) {
	query := `
		SELECT id, name, algorithm, key_type, public_key, threshold, total_shares,
		       status, tags, created_at, updated_at
		FROM kchain_keys
		WHERE threshold > 1
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`
	rows, err := db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []Key
	for rows.Next() {
		var k Key
		var tagsJSON []byte
		if err := rows.Scan(&k.ID, &k.Name, &k.Algorithm, &k.KeyType, &k.PublicKey,
			&k.Threshold, &k.TotalShares, &k.Status, &tagsJSON, &k.CreatedAt, &k.UpdatedAt); err != nil {
			continue
		}
		if len(tagsJSON) > 0 {
			json.Unmarshal(tagsJSON, &k.Tags)
		}
		keys = append(keys, k)
	}
	return keys, nil
}
