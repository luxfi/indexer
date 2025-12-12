// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package achain provides the A-Chain (AI) adapter for the DAG indexer.
// Handles AI compute attestations, model hashes, and inference results.
package achain

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/luxfi/indexer/dag"
)

const (
	// DefaultPort for A-Chain indexer API
	DefaultPort = 4500
	// DefaultDatabase name
	DefaultDatabase = "explorer_achain"
)

// AttestationType categorizes AI attestations
type AttestationType string

const (
	AttestationCompute   AttestationType = "compute"   // Compute job attestation
	AttestationModel     AttestationType = "model"     // Model registration
	AttestationInference AttestationType = "inference" // Inference result
	AttestationTraining  AttestationType = "training"  // Training job
)

// ComputeAttestation represents an AI compute attestation
type ComputeAttestation struct {
	ID           string          `json:"id"`
	Type         AttestationType `json:"type"`
	ModelHash    string          `json:"modelHash,omitempty"`
	InputHash    string          `json:"inputHash,omitempty"`
	OutputHash   string          `json:"outputHash,omitempty"`
	ComputeUnits uint64          `json:"computeUnits"`
	Provider     string          `json:"provider"`
	Requester    string          `json:"requester"`
	Timestamp    time.Time       `json:"timestamp"`
	Status       string          `json:"status"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
}

// ModelRegistration represents a registered AI model
type ModelRegistration struct {
	ID          string          `json:"id"`
	ModelHash   string          `json:"modelHash"`
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Owner       string          `json:"owner"`
	Framework   string          `json:"framework,omitempty"`
	Parameters  uint64          `json:"parameters,omitempty"`
	Timestamp   time.Time       `json:"timestamp"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

// InferenceResult represents an AI inference result
type InferenceResult struct {
	ID           string          `json:"id"`
	ModelID      string          `json:"modelId"`
	InputHash    string          `json:"inputHash"`
	OutputHash   string          `json:"outputHash"`
	Confidence   float64         `json:"confidence,omitempty"`
	Latency      time.Duration   `json:"latency"`
	Provider     string          `json:"provider"`
	Timestamp    time.Time       `json:"timestamp"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
}

// AIProvider represents an AI compute provider
type AIProvider struct {
	ID          string          `json:"id"`
	Address     string          `json:"address"`
	Name        string          `json:"name"`
	Reputation  float64         `json:"reputation"`
	Capacity    uint64          `json:"capacity"`
	Active      bool            `json:"active"`
	Timestamp   time.Time       `json:"timestamp"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

// AIReceipt represents a payment receipt for AI compute services
type AIReceipt struct {
	ID            string    `json:"id"`
	AttestationID string    `json:"attestationId"`
	Provider      string    `json:"provider"`
	Requester     string    `json:"requester"`
	Amount        uint64    `json:"amount"`
	Currency      string    `json:"currency"`
	Status        string    `json:"status"`
	Timestamp     time.Time `json:"timestamp"`
}

// TrainingJob represents an AI training job
type TrainingJob struct {
	ID           string          `json:"id"`
	ModelID      string          `json:"modelId"`
	DatasetHash  string          `json:"datasetHash"`
	Epochs       int             `json:"epochs"`
	BatchSize    int             `json:"batchSize"`
	LearningRate float64         `json:"learningRate"`
	Provider     string          `json:"provider"`
	Status       string          `json:"status"`
	StartedAt    time.Time       `json:"startedAt,omitempty"`
	CompletedAt  time.Time       `json:"completedAt,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
}

// Adapter implements dag.Adapter for A-Chain
type Adapter struct {
	rpcEndpoint string
	httpClient  *http.Client
}

// New creates a new A-Chain adapter
func New(rpcEndpoint string) *Adapter {
	return &Adapter{
		rpcEndpoint: rpcEndpoint,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// ParseVertex parses A-Chain vertex data from RPC response
func (a *Adapter) ParseVertex(data json.RawMessage) (*dag.Vertex, error) {
	var raw struct {
		ID        string          `json:"id"`
		Type      string          `json:"type"`
		ParentIDs []string        `json:"parentIds"`
		Height    uint64          `json:"height"`
		Epoch     uint32          `json:"epoch"`
		TxIDs     []string        `json:"txIds"`
		Timestamp int64           `json:"timestamp"`
		Status    string          `json:"status"`
		Data      json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal vertex: %w", err)
	}

	v := &dag.Vertex{
		ID:        raw.ID,
		Type:      raw.Type,
		ParentIDs: raw.ParentIDs,
		Height:    raw.Height,
		Epoch:     raw.Epoch,
		TxIDs:     raw.TxIDs,
		Status:    dag.Status(raw.Status),
		Data:      raw.Data,
		Metadata:  make(map[string]interface{}),
	}

	if raw.Timestamp > 0 {
		v.Timestamp = time.Unix(raw.Timestamp, 0)
	} else {
		v.Timestamp = time.Now()
	}

	// Parse AI-specific metadata from vertex data
	if len(raw.Data) > 0 {
		var aiData struct {
			AttestationType string `json:"attestationType"`
			ModelHash       string `json:"modelHash"`
			Provider        string `json:"provider"`
			ComputeUnits    uint64 `json:"computeUnits"`
		}
		if err := json.Unmarshal(raw.Data, &aiData); err == nil {
			if aiData.AttestationType != "" {
				v.Metadata["attestation_type"] = aiData.AttestationType
			}
			if aiData.ModelHash != "" {
				v.Metadata["model_hash"] = aiData.ModelHash
			}
			if aiData.Provider != "" {
				v.Metadata["provider"] = aiData.Provider
			}
			if aiData.ComputeUnits > 0 {
				v.Metadata["compute_units"] = aiData.ComputeUnits
			}
		}
	}

	return v, nil
}

// GetRecentVertices fetches recent vertices from A-Chain via RPC
func (a *Adapter) GetRecentVertices(ctx context.Context, limit int) ([]json.RawMessage, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "avm.getRecentVertices",
		"params": map[string]interface{}{
			"limit": limit,
		},
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
		return nil, fmt.Errorf("rpc call: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Result struct {
			Vertices []json.RawMessage `json:"vertices"`
		} `json:"result"`
		Error *struct {
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

	return result.Result.Vertices, nil
}

// GetVertexByID fetches a specific vertex by ID
func (a *Adapter) GetVertexByID(ctx context.Context, id string) (json.RawMessage, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "avm.getVertex",
		"params": map[string]interface{}{
			"vertexID": id,
		},
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
		return nil, fmt.Errorf("rpc call: %w", err)
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

	return result.Result, nil
}

// InitSchema creates A-Chain specific database tables
func (a *Adapter) InitSchema(db *sql.DB) error {
	schema := `
		-- AI Compute Attestations
		CREATE TABLE IF NOT EXISTS achain_attestations (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			model_hash TEXT,
			input_hash TEXT,
			output_hash TEXT,
			compute_units BIGINT DEFAULT 0,
			provider TEXT,
			requester TEXT,
			status TEXT DEFAULT 'pending',
			vertex_id TEXT,
			timestamp TIMESTAMPTZ NOT NULL,
			metadata JSONB,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_achain_attestations_type ON achain_attestations(type);
		CREATE INDEX IF NOT EXISTS idx_achain_attestations_provider ON achain_attestations(provider);
		CREATE INDEX IF NOT EXISTS idx_achain_attestations_model ON achain_attestations(model_hash);
		CREATE INDEX IF NOT EXISTS idx_achain_attestations_timestamp ON achain_attestations(timestamp DESC);

		-- AI Model Registry
		CREATE TABLE IF NOT EXISTS achain_models (
			id TEXT PRIMARY KEY,
			model_hash TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			version TEXT,
			owner TEXT NOT NULL,
			framework TEXT,
			parameters BIGINT DEFAULT 0,
			vertex_id TEXT,
			timestamp TIMESTAMPTZ NOT NULL,
			metadata JSONB,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_achain_models_owner ON achain_models(owner);
		CREATE INDEX IF NOT EXISTS idx_achain_models_hash ON achain_models(model_hash);
		CREATE INDEX IF NOT EXISTS idx_achain_models_name ON achain_models(name);

		-- AI Inference Results
		CREATE TABLE IF NOT EXISTS achain_inferences (
			id TEXT PRIMARY KEY,
			model_id TEXT NOT NULL,
			input_hash TEXT NOT NULL,
			output_hash TEXT NOT NULL,
			confidence NUMERIC(10,6),
			latency_ms BIGINT,
			provider TEXT,
			vertex_id TEXT,
			timestamp TIMESTAMPTZ NOT NULL,
			metadata JSONB,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_achain_inferences_model ON achain_inferences(model_id);
		CREATE INDEX IF NOT EXISTS idx_achain_inferences_provider ON achain_inferences(provider);
		CREATE INDEX IF NOT EXISTS idx_achain_inferences_timestamp ON achain_inferences(timestamp DESC);

		-- AI Training Jobs
		CREATE TABLE IF NOT EXISTS achain_training (
			id TEXT PRIMARY KEY,
			model_id TEXT,
			dataset_hash TEXT NOT NULL,
			epochs INT,
			batch_size INT,
			learning_rate NUMERIC(10,8),
			provider TEXT,
			status TEXT DEFAULT 'pending',
			vertex_id TEXT,
			started_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ,
			metadata JSONB,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_achain_training_model ON achain_training(model_id);
		CREATE INDEX IF NOT EXISTS idx_achain_training_status ON achain_training(status);
		CREATE INDEX IF NOT EXISTS idx_achain_training_provider ON achain_training(provider);

		-- AI Compute Providers Registry
		CREATE TABLE IF NOT EXISTS achain_providers (
			id TEXT PRIMARY KEY,
			address TEXT UNIQUE NOT NULL,
			name TEXT,
			reputation NUMERIC(5,2) DEFAULT 0,
			capacity BIGINT DEFAULT 0,
			active BOOLEAN DEFAULT true,
			vertex_id TEXT,
			timestamp TIMESTAMPTZ NOT NULL,
			metadata JSONB,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_achain_providers_active ON achain_providers(active);
		CREATE INDEX IF NOT EXISTS idx_achain_providers_address ON achain_providers(address);

		-- AI Payment Receipts
		CREATE TABLE IF NOT EXISTS achain_receipts (
			id TEXT PRIMARY KEY,
			attestation_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			requester TEXT NOT NULL,
			amount BIGINT NOT NULL,
			currency TEXT DEFAULT 'AI',
			status TEXT DEFAULT 'pending',
			vertex_id TEXT,
			timestamp TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_achain_receipts_attestation ON achain_receipts(attestation_id);
		CREATE INDEX IF NOT EXISTS idx_achain_receipts_provider ON achain_receipts(provider);
		CREATE INDEX IF NOT EXISTS idx_achain_receipts_requester ON achain_receipts(requester);

		-- Add missing index for requester queries
		CREATE INDEX IF NOT EXISTS idx_achain_attestations_requester ON achain_attestations(requester);

		-- A-Chain specific stats
		CREATE TABLE IF NOT EXISTS achain_stats (
			id INT PRIMARY KEY DEFAULT 1,
			total_attestations BIGINT DEFAULT 0,
			total_models BIGINT DEFAULT 0,
			total_inferences BIGINT DEFAULT 0,
			total_training_jobs BIGINT DEFAULT 0,
			total_compute_units BIGINT DEFAULT 0,
			unique_providers INT DEFAULT 0,
			unique_requesters INT DEFAULT 0,
			total_receipts BIGINT DEFAULT 0,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		INSERT INTO achain_stats (id) VALUES (1) ON CONFLICT DO NOTHING;
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("init achain schema: %w", err)
	}

	return nil
}

// GetStats returns A-Chain specific statistics
func (a *Adapter) GetStats(ctx context.Context, db *sql.DB) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Update stats first
	_, _ = db.ExecContext(ctx, `
		UPDATE achain_stats SET
			total_attestations = (SELECT COUNT(*) FROM achain_attestations),
			total_models = (SELECT COUNT(*) FROM achain_models),
			total_inferences = (SELECT COUNT(*) FROM achain_inferences),
			total_compute_units = (SELECT COALESCE(SUM(compute_units), 0) FROM achain_attestations),
			unique_providers = (SELECT COUNT(DISTINCT provider) FROM achain_attestations WHERE provider IS NOT NULL),
			unique_requesters = (SELECT COUNT(DISTINCT requester) FROM achain_attestations WHERE requester IS NOT NULL),
			updated_at = NOW()
		WHERE id = 1
	`)

	row := db.QueryRowContext(ctx, `
		SELECT total_attestations, total_models, total_inferences, 
		       total_compute_units, unique_providers, unique_requesters
		FROM achain_stats WHERE id = 1
	`)

	var totalAttestations, totalModels, totalInferences, totalComputeUnits int64
	var uniqueProviders, uniqueRequesters int

	if err := row.Scan(&totalAttestations, &totalModels, &totalInferences,
		&totalComputeUnits, &uniqueProviders, &uniqueRequesters); err != nil {
		return nil, fmt.Errorf("scan stats: %w", err)
	}

	stats["total_attestations"] = totalAttestations
	stats["total_models"] = totalModels
	stats["total_inferences"] = totalInferences
	stats["total_compute_units"] = totalComputeUnits
	stats["unique_providers"] = uniqueProviders
	stats["unique_requesters"] = uniqueRequesters

	// Get attestation breakdown by type
	rows, err := db.QueryContext(ctx, `
		SELECT type, COUNT(*) FROM achain_attestations GROUP BY type
	`)
	if err == nil {
		defer rows.Close()
		breakdown := make(map[string]int64)
		for rows.Next() {
			var t string
			var c int64
			if rows.Scan(&t, &c) == nil {
				breakdown[t] = c
			}
		}
		stats["attestations_by_type"] = breakdown
	}

	return stats, nil
}

// StoreAttestation stores a compute attestation
func (a *Adapter) StoreAttestation(ctx context.Context, db *sql.DB, att *ComputeAttestation, vertexID string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO achain_attestations (id, type, model_hash, input_hash, output_hash, 
			compute_units, provider, requester, status, vertex_id, timestamp, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status
	`, att.ID, att.Type, att.ModelHash, att.InputHash, att.OutputHash,
		att.ComputeUnits, att.Provider, att.Requester, att.Status, vertexID, att.Timestamp, att.Metadata)
	return err
}

// StoreModel stores a model registration
func (a *Adapter) StoreModel(ctx context.Context, db *sql.DB, model *ModelRegistration, vertexID string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO achain_models (id, model_hash, name, version, owner, 
			framework, parameters, vertex_id, timestamp, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO NOTHING
	`, model.ID, model.ModelHash, model.Name, model.Version, model.Owner,
		model.Framework, model.Parameters, vertexID, model.Timestamp, model.Metadata)
	return err
}

// StoreInference stores an inference result
func (a *Adapter) StoreInference(ctx context.Context, db *sql.DB, inf *InferenceResult, vertexID string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO achain_inferences (id, model_id, input_hash, output_hash,
			confidence, latency_ms, provider, vertex_id, timestamp, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO NOTHING
	`, inf.ID, inf.ModelID, inf.InputHash, inf.OutputHash,
		inf.Confidence, inf.Latency.Milliseconds(), inf.Provider, vertexID, inf.Timestamp, inf.Metadata)
	return err
}

// StoreTrainingJob stores a training job
func (a *Adapter) StoreTrainingJob(ctx context.Context, db *sql.DB, job *TrainingJob, vertexID string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO achain_training (id, model_id, dataset_hash, epochs, batch_size,
			learning_rate, provider, status, vertex_id, started_at, completed_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			completed_at = EXCLUDED.completed_at
	`, job.ID, job.ModelID, job.DatasetHash, job.Epochs, job.BatchSize,
		job.LearningRate, job.Provider, job.Status, vertexID, job.StartedAt, job.CompletedAt, job.Metadata)
	return err
}

// StoreProvider stores or updates a provider registration
func (a *Adapter) StoreProvider(ctx context.Context, db *sql.DB, provider *AIProvider, vertexID string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO achain_providers (id, address, name, reputation, capacity,
			active, vertex_id, timestamp, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			reputation = EXCLUDED.reputation,
			capacity = EXCLUDED.capacity,
			active = EXCLUDED.active
	`, provider.ID, provider.Address, provider.Name, provider.Reputation, provider.Capacity,
		provider.Active, vertexID, provider.Timestamp, provider.Metadata)
	return err
}

// StoreReceipt stores a payment receipt
func (a *Adapter) StoreReceipt(ctx context.Context, db *sql.DB, receipt *AIReceipt, vertexID string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO achain_receipts (id, attestation_id, provider, requester,
			amount, currency, status, vertex_id, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status
	`, receipt.ID, receipt.AttestationID, receipt.Provider, receipt.Requester,
		receipt.Amount, receipt.Currency, receipt.Status, vertexID, receipt.Timestamp)
	return err
}

// Verify interface compliance at compile time
var _ dag.Adapter = (*Adapter)(nil)
