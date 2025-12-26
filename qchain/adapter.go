// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package qchain provides the Q-Chain (Quantum) adapter for the DAG indexer.
// Q-Chain handles quantum-resistant finality proofs using lattice-based cryptography.
package qchain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/luxfi/indexer/dag"
	"github.com/luxfi/indexer/storage"
)

const (
	// DefaultRPCEndpoint is the default Q-Chain RPC endpoint
	DefaultRPCEndpoint = "http://localhost:9650/ext/bc/Q/rpc"
	// DefaultHTTPPort is the default API port for Q-Chain indexer
	DefaultHTTPPort = 4300
)

// FinalityProof represents a quantum-resistant finality proof
type FinalityProof struct {
	ID            string    `json:"id"`
	VertexID      string    `json:"vertexId"`
	ProofType     ProofType `json:"proofType"`
	LatticeParams Lattice   `json:"latticeParams"`
	Signature     []byte    `json:"signature"`
	PublicKey     []byte    `json:"publicKey"`
	Timestamp     time.Time `json:"timestamp"`
	Verified      bool      `json:"verified"`
}

// ProofType identifies the quantum-resistant algorithm used
type ProofType string

const (
	ProofDilithium ProofType = "dilithium" // NIST PQC standard
	ProofKyber     ProofType = "kyber"     // Key encapsulation
	ProofFalcon    ProofType = "falcon"    // Fast lattice-based
	ProofSphincsh  ProofType = "sphincs+"  // Hash-based
)

// Lattice contains lattice-based cryptography parameters
type Lattice struct {
	Dimension   int    `json:"dimension"`   // Lattice dimension (n)
	Modulus     int64  `json:"modulus"`     // Modulus (q)
	ErrorBound  int    `json:"errorBound"`  // Error distribution bound
	SecurityLvl int    `json:"securityLvl"` // NIST security level (1-5)
	Algorithm   string `json:"algorithm"`   // Specific algorithm variant
}

// QuantumVertex extends DAG vertex with quantum-specific data
type QuantumVertex struct {
	dag.Vertex
	FinalityProof *FinalityProof `json:"finalityProof,omitempty"`
	ProofCount    int            `json:"proofCount"`
	Finalized     bool           `json:"finalized"`
}

// QuantumStamp represents a quantum-certified timestamp for finality
type QuantumStamp struct {
	ID          string    `json:"id"`
	VertexID    string    `json:"vertexId"`
	ChainID     string    `json:"chainId"`     // Which chain this stamp is for
	BlockHeight uint64    `json:"blockHeight"` // Block height being stamped
	BlockHash   []byte    `json:"blockHash"`   // Block hash being certified
	Entropy     []byte    `json:"entropy"`     // Quantum random entropy
	KeyID       string    `json:"keyId"`       // Ringtail key used for signing
	Signature   []byte    `json:"signature"`   // Quantum-resistant signature
	Timestamp   time.Time `json:"timestamp"`
	Certified   bool      `json:"certified"` // Whether stamp has been verified
}

// RingtailKey represents a quantum-resistant signing key (Ringtail is LUX's post-quantum signature scheme)
type RingtailKey struct {
	ID          string    `json:"id"`
	PublicKey   []byte    `json:"publicKey"`
	KeyType     ProofType `json:"keyType"`       // dilithium, falcon, sphincs+
	Algorithm   string    `json:"algorithm"`     // Specific algorithm variant
	SecurityLvl int       `json:"securityLevel"` // NIST security level 1-5
	Owner       string    `json:"owner"`         // Key owner address
	ValidFrom   time.Time `json:"validFrom"`
	ValidUntil  time.Time `json:"validUntil"`
	Revoked     bool      `json:"revoked"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Adapter implements dag.Adapter for Q-Chain
type Adapter struct {
	rpcEndpoint string
	httpClient  *http.Client
}

// New creates a new Q-Chain adapter
func New(rpcEndpoint string) *Adapter {
	if rpcEndpoint == "" {
		rpcEndpoint = DefaultRPCEndpoint
	}
	return &Adapter{
		rpcEndpoint: rpcEndpoint,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// rpcRequest represents a JSON-RPC request
type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params,omitempty"`
}

// rpcResponse represents a JSON-RPC response
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// call executes an RPC request to the Q-Chain node
func (a *Adapter) call(ctx context.Context, method string, params ...interface{}) (json.RawMessage, error) {
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.rpcEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	var rpcResp rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// ParseVertex parses raw RPC data into a DAG vertex
func (a *Adapter) ParseVertex(data json.RawMessage) (*dag.Vertex, error) {
	var qv QuantumVertex
	if err := json.Unmarshal(data, &qv); err != nil {
		return nil, fmt.Errorf("unmarshal quantum vertex: %w", err)
	}

	// Build metadata with quantum-specific fields
	meta := make(map[string]interface{})
	if qv.FinalityProof != nil {
		meta["proof_type"] = qv.FinalityProof.ProofType
		meta["proof_verified"] = qv.FinalityProof.Verified
		meta["lattice_dimension"] = qv.FinalityProof.LatticeParams.Dimension
		meta["security_level"] = qv.FinalityProof.LatticeParams.SecurityLvl
	}
	meta["proof_count"] = qv.ProofCount
	meta["finalized"] = qv.Finalized

	v := &dag.Vertex{
		ID:        qv.ID,
		Type:      "quantum",
		ParentIDs: qv.ParentIDs,
		Height:    qv.Height,
		Epoch:     qv.Epoch,
		TxIDs:     qv.TxIDs,
		Timestamp: qv.Timestamp,
		Status:    qv.Status,
		Data:      data,
		Metadata:  meta,
	}

	// Update status based on finalization
	if qv.Finalized {
		v.Status = dag.StatusAccepted
	}

	return v, nil
}

// GetRecentVertices fetches recent vertices from Q-Chain
func (a *Adapter) GetRecentVertices(ctx context.Context, limit int) ([]json.RawMessage, error) {
	result, err := a.call(ctx, "qvm.getRecentVertices", map[string]interface{}{
		"limit": limit,
	})
	if err != nil {
		return nil, err
	}

	var response struct {
		Vertices []json.RawMessage `json:"vertices"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, fmt.Errorf("unmarshal vertices: %w", err)
	}

	return response.Vertices, nil
}

// GetVertexByID fetches a specific vertex by ID
func (a *Adapter) GetVertexByID(ctx context.Context, id string) (json.RawMessage, error) {
	result, err := a.call(ctx, "qvm.getVertex", map[string]interface{}{
		"id": id,
	})
	if err != nil {
		return nil, err
	}

	var response struct {
		Vertex json.RawMessage `json:"vertex"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, fmt.Errorf("unmarshal vertex: %w", err)
	}

	return response.Vertex, nil
}

// InitSchema creates Q-Chain specific database tables
func (a *Adapter) InitSchema(ctx context.Context, store storage.Store) error {
	// Create finality proofs table
	err := store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS qchain_finality_proofs (
			id TEXT PRIMARY KEY,
			vertex_id TEXT NOT NULL,
			proof_type TEXT NOT NULL,
			lattice_dimension INTEGER,
			lattice_modulus INTEGER,
			lattice_error_bound INTEGER,
			security_level INTEGER,
			algorithm TEXT,
			signature BLOB,
			public_key BLOB,
			verified INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create finality proofs table: %w", err)
	}

	// Create indexes
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_qchain_proofs_vertex ON qchain_finality_proofs(vertex_id)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_qchain_proofs_type ON qchain_finality_proofs(proof_type)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_qchain_proofs_verified ON qchain_finality_proofs(verified)`)

	// Create lattice parameters table
	err = store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS qchain_lattice_params (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			dimension INTEGER NOT NULL,
			modulus INTEGER NOT NULL,
			error_bound INTEGER NOT NULL,
			security_level INTEGER NOT NULL,
			algorithm TEXT NOT NULL,
			description TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create lattice params table: %w", err)
	}

	// Insert default lattice configurations
	defaults := []struct {
		name, algorithm, description  string
		dimension, errorBound, secLvl int
		modulus                       int64
	}{
		{"dilithium2", "dilithium", "NIST Level 2 Dilithium", 256, 2, 2, 8380417},
		{"dilithium3", "dilithium", "NIST Level 3 Dilithium", 256, 4, 3, 8380417},
		{"dilithium5", "dilithium", "NIST Level 5 Dilithium", 256, 8, 5, 8380417},
		{"kyber512", "kyber", "Kyber-512", 256, 2, 1, 3329},
		{"kyber768", "kyber", "Kyber-768", 256, 2, 3, 3329},
		{"kyber1024", "kyber", "Kyber-1024", 256, 2, 5, 3329},
		{"falcon512", "falcon", "Falcon-512", 512, 1, 1, 12289},
		{"falcon1024", "falcon", "Falcon-1024", 1024, 1, 5, 12289},
	}
	for _, d := range defaults {
		_ = store.Exec(ctx, `
			INSERT OR IGNORE INTO qchain_lattice_params (name, dimension, modulus, error_bound, security_level, algorithm, description)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, d.name, d.dimension, d.modulus, d.errorBound, d.secLvl, d.algorithm, d.description)
	}

	// Create quantum stamps table
	err = store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS qchain_stamps (
			id TEXT PRIMARY KEY,
			vertex_id TEXT,
			chain_id TEXT NOT NULL,
			block_height INTEGER NOT NULL,
			block_hash BLOB NOT NULL,
			entropy BLOB,
			key_id TEXT,
			signature BLOB,
			timestamp TIMESTAMP NOT NULL,
			certified INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create stamps table: %w", err)
	}

	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_qchain_stamps_chain ON qchain_stamps(chain_id)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_qchain_stamps_height ON qchain_stamps(chain_id, block_height)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_qchain_stamps_key ON qchain_stamps(key_id)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_qchain_stamps_certified ON qchain_stamps(certified)`)

	// Create Ringtail keys table
	err = store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS qchain_ringtail_keys (
			id TEXT PRIMARY KEY,
			public_key BLOB NOT NULL,
			key_type TEXT NOT NULL,
			algorithm TEXT NOT NULL,
			security_level INTEGER NOT NULL,
			owner TEXT NOT NULL,
			valid_from TIMESTAMP NOT NULL,
			valid_until TIMESTAMP NOT NULL,
			revoked INTEGER DEFAULT 0,
			vertex_id TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create ringtail keys table: %w", err)
	}

	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_qchain_keys_owner ON qchain_ringtail_keys(owner)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_qchain_keys_type ON qchain_ringtail_keys(key_type)`)
	_ = store.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_qchain_keys_active ON qchain_ringtail_keys(revoked, valid_until)`)

	// Create extended stats table
	err = store.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS qchain_extended_stats (
			id INTEGER PRIMARY KEY DEFAULT 1,
			total_proofs INTEGER DEFAULT 0,
			verified_proofs INTEGER DEFAULT 0,
			finalized_vertices INTEGER DEFAULT 0,
			total_stamps INTEGER DEFAULT 0,
			certified_stamps INTEGER DEFAULT 0,
			total_keys INTEGER DEFAULT 0,
			active_keys INTEGER DEFAULT 0,
			dilithium_proofs INTEGER DEFAULT 0,
			kyber_proofs INTEGER DEFAULT 0,
			falcon_proofs INTEGER DEFAULT 0,
			sphincs_proofs INTEGER DEFAULT 0,
			avg_security_level REAL DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create extended stats table: %w", err)
	}

	_ = store.Exec(ctx, `INSERT OR IGNORE INTO qchain_extended_stats (id) VALUES (1)`)

	return nil
}

// GetStats returns Q-Chain specific statistics
func (a *Adapter) GetStats(ctx context.Context, store storage.Store) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Query extended stats
	rows, err := store.Query(ctx, `
		SELECT total_proofs, verified_proofs, finalized_vertices,
		       dilithium_proofs, kyber_proofs, falcon_proofs, sphincs_proofs,
		       avg_security_level
		FROM qchain_extended_stats WHERE id = 1
	`)
	if err != nil {
		return nil, fmt.Errorf("query extended stats: %w", err)
	}

	var totalProofs, verifiedProofs, finalizedVertices int64
	var dilithiumProofs, kyberProofs, falconProofs, sphincsProofs int64
	var avgSecurityLevel float64

	if len(rows) > 0 {
		row := rows[0]
		if v, ok := row["total_proofs"].(int64); ok {
			totalProofs = v
		}
		if v, ok := row["verified_proofs"].(int64); ok {
			verifiedProofs = v
		}
		if v, ok := row["finalized_vertices"].(int64); ok {
			finalizedVertices = v
		}
		if v, ok := row["dilithium_proofs"].(int64); ok {
			dilithiumProofs = v
		}
		if v, ok := row["kyber_proofs"].(int64); ok {
			kyberProofs = v
		}
		if v, ok := row["falcon_proofs"].(int64); ok {
			falconProofs = v
		}
		if v, ok := row["sphincs_proofs"].(int64); ok {
			sphincsProofs = v
		}
		if v, ok := row["avg_security_level"].(float64); ok {
			avgSecurityLevel = v
		}
	}

	stats["total_proofs"] = totalProofs
	stats["verified_proofs"] = verifiedProofs
	stats["finalized_vertices"] = finalizedVertices
	stats["proof_types"] = map[string]int64{
		"dilithium": dilithiumProofs,
		"kyber":     kyberProofs,
		"falcon":    falconProofs,
		"sphincs":   sphincsProofs,
	}
	stats["avg_security_level"] = avgSecurityLevel

	// Calculate verification rate
	if totalProofs > 0 {
		stats["verification_rate"] = float64(verifiedProofs) / float64(totalProofs)
	} else {
		stats["verification_rate"] = 0.0
	}

	return stats, nil
}

// StoreProof stores a finality proof in the database
func (a *Adapter) StoreProof(ctx context.Context, store storage.Store, proof *FinalityProof) error {
	verified := 0
	if proof.Verified {
		verified = 1
	}
	return store.Exec(ctx, `
		INSERT OR REPLACE INTO qchain_finality_proofs
			(id, vertex_id, proof_type, lattice_dimension, lattice_modulus,
			 lattice_error_bound, security_level, algorithm, signature, public_key, verified)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		proof.ID, proof.VertexID, proof.ProofType,
		proof.LatticeParams.Dimension, proof.LatticeParams.Modulus,
		proof.LatticeParams.ErrorBound, proof.LatticeParams.SecurityLvl,
		proof.LatticeParams.Algorithm, proof.Signature, proof.PublicKey, verified,
	)
}

// VerifyProof performs quantum-resistant proof verification
// Returns true if the proof is valid under the specified lattice parameters
func (a *Adapter) VerifyProof(ctx context.Context, proof *FinalityProof) (bool, error) {
	// Delegate verification to the Q-Chain node via RPC
	result, err := a.call(ctx, "qvm.verifyProof", map[string]interface{}{
		"proofId":   proof.ID,
		"vertexId":  proof.VertexID,
		"proofType": proof.ProofType,
		"signature": proof.Signature,
		"publicKey": proof.PublicKey,
	})
	if err != nil {
		return false, err
	}

	var response struct {
		Valid   bool   `json:"valid"`
		Message string `json:"message,omitempty"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return false, fmt.Errorf("unmarshal verify response: %w", err)
	}

	return response.Valid, nil
}

// UpdateExtendedStats updates Q-Chain specific statistics
func (a *Adapter) UpdateExtendedStats(ctx context.Context, store storage.Store) error {
	return store.Exec(ctx, `
		UPDATE qchain_extended_stats SET
			total_proofs = (SELECT COUNT(*) FROM qchain_finality_proofs),
			verified_proofs = (SELECT COUNT(*) FROM qchain_finality_proofs WHERE verified = 1),
			finalized_vertices = (SELECT COUNT(*) FROM qchain_vertices WHERE status = 'accepted'),
			dilithium_proofs = (SELECT COUNT(*) FROM qchain_finality_proofs WHERE proof_type = 'dilithium'),
			kyber_proofs = (SELECT COUNT(*) FROM qchain_finality_proofs WHERE proof_type = 'kyber'),
			falcon_proofs = (SELECT COUNT(*) FROM qchain_finality_proofs WHERE proof_type = 'falcon'),
			sphincs_proofs = (SELECT COUNT(*) FROM qchain_finality_proofs WHERE proof_type = 'sphincs+'),
			avg_security_level = COALESCE((SELECT AVG(security_level) FROM qchain_finality_proofs), 0),
			updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`)
}

// NewConfig creates a default Q-Chain indexer configuration
func NewConfig() dag.Config {
	return dag.Config{
		ChainType:    dag.ChainQ,
		ChainName:    "Q-Chain (Quantum)",
		RPCEndpoint:  DefaultRPCEndpoint,
		RPCMethod:    "qvm",
		HTTPPort:     DefaultHTTPPort,
		PollInterval: 5 * time.Second,
	}
}

// StoreStamp stores a quantum stamp for cross-chain finality
func (a *Adapter) StoreStamp(ctx context.Context, store storage.Store, stamp *QuantumStamp) error {
	certified := 0
	if stamp.Certified {
		certified = 1
	}
	return store.Exec(ctx, `
		INSERT OR REPLACE INTO qchain_stamps
			(id, vertex_id, chain_id, block_height, block_hash, entropy, key_id, signature, timestamp, certified)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, stamp.ID, stamp.VertexID, stamp.ChainID, stamp.BlockHeight, stamp.BlockHash,
		stamp.Entropy, stamp.KeyID, stamp.Signature, stamp.Timestamp, certified)
}

// StoreRingtailKey stores a quantum-resistant signing key
func (a *Adapter) StoreRingtailKey(ctx context.Context, store storage.Store, key *RingtailKey, vertexID string) error {
	revoked := 0
	if key.Revoked {
		revoked = 1
	}
	return store.Exec(ctx, `
		INSERT OR REPLACE INTO qchain_ringtail_keys
			(id, public_key, key_type, algorithm, security_level, owner, valid_from, valid_until, revoked, vertex_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, key.ID, key.PublicKey, key.KeyType, key.Algorithm, key.SecurityLvl,
		key.Owner, key.ValidFrom, key.ValidUntil, revoked, vertexID)
}

// GetStampsByChain retrieves quantum stamps for a specific chain
func (a *Adapter) GetStampsByChain(ctx context.Context, store storage.Store, chainID string, limit int) ([]QuantumStamp, error) {
	rows, err := store.Query(ctx, `
		SELECT id, vertex_id, chain_id, block_height, block_hash, entropy, key_id, signature, timestamp, certified
		FROM qchain_stamps
		WHERE chain_id = ?
		ORDER BY block_height DESC
		LIMIT ?
	`, chainID, limit)
	if err != nil {
		return nil, err
	}

	var stamps []QuantumStamp
	for _, row := range rows {
		s := QuantumStamp{}
		if v, ok := row["id"].(string); ok {
			s.ID = v
		}
		if v, ok := row["vertex_id"].(string); ok {
			s.VertexID = v
		}
		if v, ok := row["chain_id"].(string); ok {
			s.ChainID = v
		}
		if v, ok := row["block_height"].(int64); ok {
			s.BlockHeight = uint64(v)
		}
		if v, ok := row["block_hash"].([]byte); ok {
			s.BlockHash = v
		}
		if v, ok := row["entropy"].([]byte); ok {
			s.Entropy = v
		}
		if v, ok := row["key_id"].(string); ok {
			s.KeyID = v
		}
		if v, ok := row["signature"].([]byte); ok {
			s.Signature = v
		}
		if v, ok := row["timestamp"].(time.Time); ok {
			s.Timestamp = v
		}
		if v, ok := row["certified"].(int64); ok {
			s.Certified = v == 1
		}
		stamps = append(stamps, s)
	}
	return stamps, nil
}

// GetActiveKeys retrieves active Ringtail keys for an owner
func (a *Adapter) GetActiveKeys(ctx context.Context, store storage.Store, owner string) ([]RingtailKey, error) {
	rows, err := store.Query(ctx, `
		SELECT id, public_key, key_type, algorithm, security_level, owner, valid_from, valid_until, revoked, created_at
		FROM qchain_ringtail_keys
		WHERE owner = ? AND revoked = 0 AND valid_until > CURRENT_TIMESTAMP
		ORDER BY created_at DESC
	`, owner)
	if err != nil {
		return nil, err
	}

	var keys []RingtailKey
	for _, row := range rows {
		k := RingtailKey{}
		if v, ok := row["id"].(string); ok {
			k.ID = v
		}
		if v, ok := row["public_key"].([]byte); ok {
			k.PublicKey = v
		}
		if v, ok := row["key_type"].(string); ok {
			k.KeyType = ProofType(v)
		}
		if v, ok := row["algorithm"].(string); ok {
			k.Algorithm = v
		}
		if v, ok := row["security_level"].(int64); ok {
			k.SecurityLvl = int(v)
		}
		if v, ok := row["owner"].(string); ok {
			k.Owner = v
		}
		if v, ok := row["valid_from"].(time.Time); ok {
			k.ValidFrom = v
		}
		if v, ok := row["valid_until"].(time.Time); ok {
			k.ValidUntil = v
		}
		if v, ok := row["revoked"].(int64); ok {
			k.Revoked = v == 1
		}
		if v, ok := row["created_at"].(time.Time); ok {
			k.CreatedAt = v
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// Verify interface compliance at compile time
var _ dag.Adapter = (*Adapter)(nil)
