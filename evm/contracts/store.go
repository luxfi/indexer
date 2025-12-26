// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package contracts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Store handles database operations for smart contracts.
type Store struct {
	db *sql.DB
}

// NewStore creates a new contract store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// InitSchema creates the database tables for smart contracts.
func (s *Store) InitSchema() error {
	schema := `
		-- Verified smart contracts
		CREATE TABLE IF NOT EXISTS smart_contracts (
			address TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			compiler_version TEXT NOT NULL,
			evm_version TEXT,
			optimization BOOLEAN DEFAULT FALSE,
			optimization_runs INTEGER DEFAULT 200,
			source_code TEXT NOT NULL,
			abi JSONB,
			bytecode TEXT,
			deployed_bytecode TEXT,
			constructor_args TEXT,
			libraries JSONB,
			verification_status TEXT DEFAULT 'unverified',
			proxy_type TEXT,
			implementation_address TEXT,
			file_path TEXT,
			compiler_settings JSONB,
			verified_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_smart_contracts_name ON smart_contracts(name);
		CREATE INDEX IF NOT EXISTS idx_smart_contracts_status ON smart_contracts(verification_status);
		CREATE INDEX IF NOT EXISTS idx_smart_contracts_proxy ON smart_contracts(proxy_type) WHERE proxy_type IS NOT NULL;
		CREATE INDEX IF NOT EXISTS idx_smart_contracts_impl ON smart_contracts(implementation_address) WHERE implementation_address IS NOT NULL;

		-- Secondary sources for multi-file contracts
		CREATE TABLE IF NOT EXISTS smart_contract_sources (
			id SERIAL PRIMARY KEY,
			address TEXT NOT NULL REFERENCES smart_contracts(address) ON DELETE CASCADE,
			file_name TEXT NOT NULL,
			source_code TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(address, file_name)
		);
		CREATE INDEX IF NOT EXISTS idx_contract_sources_address ON smart_contract_sources(address);

		-- Function method signatures (4byte directory)
		CREATE TABLE IF NOT EXISTS contract_methods (
			selector TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			signature TEXT NOT NULL,
			input_types JSONB,
			output_types JSONB,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_contract_methods_name ON contract_methods(name);

		-- Event signatures
		CREATE TABLE IF NOT EXISTS contract_events (
			topic0 TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			signature TEXT NOT NULL,
			input_types JSONB,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_contract_events_name ON contract_events(name);

		-- Proxy implementation mappings
		CREATE TABLE IF NOT EXISTS proxy_implementations (
			proxy_address TEXT PRIMARY KEY,
			implementation_address TEXT NOT NULL,
			proxy_type TEXT NOT NULL,
			beacon_address TEXT,
			detected_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_proxy_impl_address ON proxy_implementations(implementation_address);

		-- Verification attempts (for debugging)
		CREATE TABLE IF NOT EXISTS verification_attempts (
			id SERIAL PRIMARY KEY,
			address TEXT NOT NULL,
			compiler_version TEXT NOT NULL,
			success BOOLEAN DEFAULT FALSE,
			error_message TEXT,
			attempted_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_verification_attempts_address ON verification_attempts(address);
	`

	_, err := s.db.Exec(schema)
	return err
}

// SaveContract saves a verified contract.
func (s *Store) SaveContract(ctx context.Context, contract *SmartContract) error {
	if contract == nil {
		return errors.New("contract is nil")
	}

	librariesJSON, _ := json.Marshal(contract.Libraries)
	settingsJSON := contract.CompilerSettings
	if len(settingsJSON) == 0 {
		settingsJSON = []byte("{}")
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO smart_contracts (
			address, name, compiler_version, evm_version, optimization, optimization_runs,
			source_code, abi, bytecode, deployed_bytecode, constructor_args, libraries,
			verification_status, proxy_type, implementation_address, file_path,
			compiler_settings, verified_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		ON CONFLICT (address) DO UPDATE SET
			name = EXCLUDED.name,
			compiler_version = EXCLUDED.compiler_version,
			evm_version = EXCLUDED.evm_version,
			optimization = EXCLUDED.optimization,
			optimization_runs = EXCLUDED.optimization_runs,
			source_code = EXCLUDED.source_code,
			abi = EXCLUDED.abi,
			bytecode = EXCLUDED.bytecode,
			deployed_bytecode = EXCLUDED.deployed_bytecode,
			constructor_args = EXCLUDED.constructor_args,
			libraries = EXCLUDED.libraries,
			verification_status = EXCLUDED.verification_status,
			proxy_type = EXCLUDED.proxy_type,
			implementation_address = EXCLUDED.implementation_address,
			file_path = EXCLUDED.file_path,
			compiler_settings = EXCLUDED.compiler_settings,
			verified_at = EXCLUDED.verified_at,
			updated_at = NOW()
	`,
		strings.ToLower(contract.Address),
		contract.Name,
		contract.CompilerVersion,
		contract.EVMVersion,
		contract.Optimization,
		contract.OptimizationRuns,
		contract.SourceCode,
		contract.ABI,
		contract.Bytecode,
		contract.DeployedBytecode,
		contract.ConstructorArgs,
		librariesJSON,
		string(contract.VerificationStatus),
		string(contract.ProxyType),
		contract.ImplementationAddr,
		contract.FilePath,
		settingsJSON,
		contract.VerifiedAt,
		contract.CreatedAt,
		time.Now(),
	)

	if err != nil {
		return fmt.Errorf("save contract: %w", err)
	}

	// Save secondary sources
	for _, source := range contract.SecondarySources {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO smart_contract_sources (address, file_name, source_code)
			VALUES ($1, $2, $3)
			ON CONFLICT (address, file_name) DO UPDATE SET
				source_code = EXCLUDED.source_code
		`, strings.ToLower(contract.Address), source.FileName, source.SourceCode)
		if err != nil {
			return fmt.Errorf("save secondary source: %w", err)
		}
	}

	// Extract and save method signatures
	if len(contract.ABI) > 0 {
		if err := s.saveMethodSignatures(ctx, contract.ABI); err != nil {
			// Log but don't fail
			_ = err
		}
	}

	return nil
}

// GetContract retrieves a contract by address.
func (s *Store) GetContract(ctx context.Context, address string) (*SmartContract, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT address, name, compiler_version, evm_version, optimization, optimization_runs,
		       source_code, abi, bytecode, deployed_bytecode, constructor_args, libraries,
		       verification_status, proxy_type, implementation_address, file_path,
		       compiler_settings, verified_at, created_at, updated_at
		FROM smart_contracts
		WHERE address = $1
	`, strings.ToLower(address))

	var c SmartContract
	var librariesJSON, settingsJSON []byte
	var proxyType, implAddr, filePath sql.NullString
	var verifiedAt sql.NullTime

	err := row.Scan(
		&c.Address, &c.Name, &c.CompilerVersion, &c.EVMVersion,
		&c.Optimization, &c.OptimizationRuns, &c.SourceCode,
		&c.ABI, &c.Bytecode, &c.DeployedBytecode, &c.ConstructorArgs,
		&librariesJSON, &c.VerificationStatus, &proxyType, &implAddr,
		&filePath, &settingsJSON, &verifiedAt, &c.CreatedAt, &c.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query contract: %w", err)
	}

	if len(librariesJSON) > 0 {
		json.Unmarshal(librariesJSON, &c.Libraries)
	}
	if len(settingsJSON) > 0 {
		c.CompilerSettings = settingsJSON
	}
	if proxyType.Valid {
		c.ProxyType = ProxyType(proxyType.String)
	}
	if implAddr.Valid {
		c.ImplementationAddr = implAddr.String
	}
	if filePath.Valid {
		c.FilePath = filePath.String
	}
	if verifiedAt.Valid {
		c.VerifiedAt = verifiedAt.Time
	}

	// Load secondary sources
	rows, err := s.db.QueryContext(ctx, `
		SELECT file_name, source_code FROM smart_contract_sources WHERE address = $1
	`, strings.ToLower(address))
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var source SecondarySource
			if err := rows.Scan(&source.FileName, &source.SourceCode); err == nil {
				c.SecondarySources = append(c.SecondarySources, source)
			}
		}
	}

	return &c, nil
}

// GetContractABI retrieves just the ABI for a contract.
func (s *Store) GetContractABI(ctx context.Context, address string) (json.RawMessage, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT abi FROM smart_contracts WHERE address = $1
	`, strings.ToLower(address))

	var abi json.RawMessage
	err := row.Scan(&abi)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return abi, nil
}

// ListVerifiedContracts lists all verified contracts.
func (s *Store) ListVerifiedContracts(ctx context.Context, limit, offset int) ([]*SmartContract, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT address, name, compiler_version, verification_status, proxy_type, verified_at
		FROM smart_contracts
		WHERE verification_status = 'verified'
		ORDER BY verified_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contracts []*SmartContract
	for rows.Next() {
		var c SmartContract
		var proxyType sql.NullString
		var verifiedAt sql.NullTime

		if err := rows.Scan(&c.Address, &c.Name, &c.CompilerVersion, &c.VerificationStatus, &proxyType, &verifiedAt); err != nil {
			continue
		}
		if proxyType.Valid {
			c.ProxyType = ProxyType(proxyType.String)
		}
		if verifiedAt.Valid {
			c.VerifiedAt = verifiedAt.Time
		}
		contracts = append(contracts, &c)
	}

	return contracts, nil
}

// SaveProxyInfo saves proxy detection results.
func (s *Store) SaveProxyInfo(ctx context.Context, proxyAddress string, info *ProxyInfo) error {
	if info == nil || !info.IsProxy {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO proxy_implementations (proxy_address, implementation_address, proxy_type, beacon_address, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (proxy_address) DO UPDATE SET
			implementation_address = EXCLUDED.implementation_address,
			proxy_type = EXCLUDED.proxy_type,
			beacon_address = EXCLUDED.beacon_address,
			updated_at = NOW()
	`, strings.ToLower(proxyAddress), info.ImplementationAddress, string(info.ProxyType), info.BeaconAddress)

	return err
}

// GetProxyInfo retrieves proxy information.
func (s *Store) GetProxyInfo(ctx context.Context, proxyAddress string) (*ProxyInfo, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT implementation_address, proxy_type, beacon_address
		FROM proxy_implementations
		WHERE proxy_address = $1
	`, strings.ToLower(proxyAddress))

	var implAddr, proxyType string
	var beaconAddr sql.NullString

	err := row.Scan(&implAddr, &proxyType, &beaconAddr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	info := &ProxyInfo{
		IsProxy:               true,
		ProxyType:             ProxyType(proxyType),
		ImplementationAddress: implAddr,
	}
	if beaconAddr.Valid {
		info.BeaconAddress = beaconAddr.String
	}

	return info, nil
}

// saveMethodSignatures extracts and saves method signatures from ABI.
func (s *Store) saveMethodSignatures(ctx context.Context, abiJSON json.RawMessage) error {
	methods, events, err := ParseABI(abiJSON)
	if err != nil {
		return err
	}

	for _, m := range methods {
		inputsJSON, _ := json.Marshal(m.Inputs)
		outputsJSON, _ := json.Marshal(m.Outputs)

		_, err := s.db.ExecContext(ctx, `
			INSERT INTO contract_methods (selector, name, signature, input_types, output_types)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (selector) DO NOTHING
		`, m.Selector, m.Name, m.Signature, inputsJSON, outputsJSON)
		if err != nil {
			return err
		}
	}

	for _, e := range events {
		inputsJSON, _ := json.Marshal(e.Inputs)

		_, err := s.db.ExecContext(ctx, `
			INSERT INTO contract_events (topic0, name, signature, input_types)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (topic0) DO NOTHING
		`, e.Topic0, e.Name, e.Signature, inputsJSON)
		if err != nil {
			return err
		}
	}

	return nil
}

// LookupMethod finds a method signature by selector.
func (s *Store) LookupMethod(ctx context.Context, selector string) (*MethodSignature, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT selector, name, signature, input_types, output_types
		FROM contract_methods
		WHERE selector = $1
	`, strings.ToLower(selector))

	var m MethodSignature
	var inputsJSON, outputsJSON []byte

	err := row.Scan(&m.Selector, &m.Name, &m.Signature, &inputsJSON, &outputsJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	json.Unmarshal(inputsJSON, &m.Inputs)
	json.Unmarshal(outputsJSON, &m.Outputs)

	return &m, nil
}

// LookupEvent finds an event signature by topic0.
func (s *Store) LookupEvent(ctx context.Context, topic0 string) (*EventSignature, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT topic0, name, signature, input_types
		FROM contract_events
		WHERE topic0 = $1
	`, strings.ToLower(topic0))

	var e EventSignature
	var inputsJSON []byte

	err := row.Scan(&e.Topic0, &e.Name, &e.Signature, &inputsJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	json.Unmarshal(inputsJSON, &e.Inputs)

	return &e, nil
}

// RecordVerificationAttempt records a verification attempt.
func (s *Store) RecordVerificationAttempt(ctx context.Context, address, compilerVersion string, success bool, errorMsg string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO verification_attempts (address, compiler_version, success, error_message)
		VALUES ($1, $2, $3, $4)
	`, strings.ToLower(address), compilerVersion, success, errorMsg)
	return err
}

// GetStats returns contract verification statistics.
func (s *Store) GetStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total verified contracts
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smart_contracts WHERE verification_status = 'verified'`)
	var verified int64
	row.Scan(&verified)
	stats["verified_contracts"] = verified

	// Total proxies
	row = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM proxy_implementations`)
	var proxies int64
	row.Scan(&proxies)
	stats["proxy_contracts"] = proxies

	// Proxy types breakdown
	rows, err := s.db.QueryContext(ctx, `
		SELECT proxy_type, COUNT(*) as count
		FROM proxy_implementations
		GROUP BY proxy_type
	`)
	if err == nil {
		defer rows.Close()
		proxyTypes := make(map[string]int64)
		for rows.Next() {
			var typ string
			var count int64
			if rows.Scan(&typ, &count) == nil {
				proxyTypes[typ] = count
			}
		}
		stats["proxy_types"] = proxyTypes
	}

	// Method signatures count
	row = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_methods`)
	var methods int64
	row.Scan(&methods)
	stats["method_signatures"] = methods

	// Event signatures count
	row = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_events`)
	var events int64
	row.Scan(&events)
	stats["event_signatures"] = events

	return stats, nil
}

// SearchContracts searches contracts by name or address.
func (s *Store) SearchContracts(ctx context.Context, query string, limit int) ([]*SmartContract, error) {
	if limit <= 0 {
		limit = 20
	}

	query = "%" + strings.ToLower(query) + "%"

	rows, err := s.db.QueryContext(ctx, `
		SELECT address, name, compiler_version, verification_status, proxy_type, verified_at
		FROM smart_contracts
		WHERE LOWER(address) LIKE $1 OR LOWER(name) LIKE $1
		ORDER BY verified_at DESC NULLS LAST
		LIMIT $2
	`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contracts []*SmartContract
	for rows.Next() {
		var c SmartContract
		var proxyType sql.NullString
		var verifiedAt sql.NullTime

		if err := rows.Scan(&c.Address, &c.Name, &c.CompilerVersion, &c.VerificationStatus, &proxyType, &verifiedAt); err != nil {
			continue
		}
		if proxyType.Valid {
			c.ProxyType = ProxyType(proxyType.String)
		}
		if verifiedAt.Valid {
			c.VerifiedAt = verifiedAt.Time
		}
		contracts = append(contracts, &c)
	}

	return contracts, nil
}
