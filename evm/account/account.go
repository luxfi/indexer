// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package account provides user account management for the EVM indexer.
// Implements user registration, API key management, watchlists, and notifications.
package account

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Common errors
var (
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidAPIKey      = errors.New("invalid API key")
	ErrAPIKeyLimitReached = errors.New("maximum API keys limit reached")
	ErrWatchlistNotFound  = errors.New("watchlist not found")
	ErrAddressExists      = errors.New("address already in watchlist")
	ErrRateLimited        = errors.New("rate limit exceeded")
)

// User represents an authenticated user account
type User struct {
	ID            int64     `json:"id"`
	UID           string    `json:"uid"` // OAuth provider UID
	Email         string    `json:"email"`
	Name          string    `json:"name,omitempty"`
	Nickname      string    `json:"nickname,omitempty"`
	Avatar        string    `json:"avatar,omitempty"`
	AddressHash   string    `json:"address_hash,omitempty"` // Associated wallet address
	EmailVerified bool      `json:"email_verified"`
	PlanID        *int64    `json:"plan_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// APIKey represents a user's API key for rate-limited access
type APIKey struct {
	Value        string     `json:"value"` // UUID value
	Name         string     `json:"name"`
	UserID       int64      `json:"user_id"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	RequestCount int64      `json:"request_count"`
}

// Watchlist represents a collection of watched addresses
type Watchlist struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WatchlistAddress represents an address in a watchlist
type WatchlistAddress struct {
	ID             int64     `json:"id"`
	WatchlistID    int64     `json:"watchlist_id"`
	AddressHash    string    `json:"address_hash"`
	Name           string    `json:"name,omitempty"`
	NotifyIncoming bool      `json:"notify_incoming"`
	NotifyOutgoing bool      `json:"notify_outgoing"`
	NotifyERC20    bool      `json:"notify_erc20"`
	NotifyNFT      bool      `json:"notify_nft"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TagAddress represents a custom label for an address
type TagAddress struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	AddressHash string    `json:"address_hash"`
	Tag         string    `json:"tag"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CustomABI represents a user-defined ABI for a contract
type CustomABI struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	ContractHash string    `json:"contract_hash"`
	Name         string    `json:"name"`
	ABI          string    `json:"abi"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Plan represents an API access plan with rate limits
type Plan struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	MaxKeysPerDay int64  `json:"max_keys_per_day"`
	MaxReqPerSec  int64  `json:"max_req_per_sec"`
	MaxReqPerDay  int64  `json:"max_req_per_day"`
}

// Service provides account management functionality
type Service struct {
	db         *sql.DB
	maxAPIKeys int
}

// Config holds account service configuration
type Config struct {
	MaxAPIKeysPerUser int
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		MaxAPIKeysPerUser: 3,
	}
}

// NewService creates a new account service
func NewService(db *sql.DB, config Config) *Service {
	return &Service{
		db:         db,
		maxAPIKeys: config.MaxAPIKeysPerUser,
	}
}

// InitSchema creates account-related database tables
func (s *Service) InitSchema(ctx context.Context) error {
	schema := `
		-- Users table
		CREATE TABLE IF NOT EXISTS account_users (
			id SERIAL PRIMARY KEY,
			uid_hash TEXT UNIQUE NOT NULL,
			uid TEXT NOT NULL,
			email TEXT NOT NULL,
			name TEXT,
			nickname TEXT,
			avatar TEXT,
			address_hash TEXT,
			email_verified BOOLEAN DEFAULT FALSE,
			plan_id INT,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_account_users_email ON account_users(email);
		CREATE INDEX IF NOT EXISTS idx_account_users_address ON account_users(address_hash) WHERE address_hash IS NOT NULL;

		-- API keys table
		CREATE TABLE IF NOT EXISTS account_api_keys (
			value UUID PRIMARY KEY,
			name TEXT NOT NULL,
			user_id INT NOT NULL REFERENCES account_users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			last_used_at TIMESTAMPTZ,
			request_count BIGINT DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_account_api_keys_user ON account_api_keys(user_id);

		-- Watchlists table
		CREATE TABLE IF NOT EXISTS account_watchlists (
			id SERIAL PRIMARY KEY,
			user_id INT NOT NULL REFERENCES account_users(id) ON DELETE CASCADE,
			name TEXT NOT NULL DEFAULT 'default',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_account_watchlists_user ON account_watchlists(user_id);

		-- Watchlist addresses table
		CREATE TABLE IF NOT EXISTS account_watchlist_addresses (
			id SERIAL PRIMARY KEY,
			watchlist_id INT NOT NULL REFERENCES account_watchlists(id) ON DELETE CASCADE,
			address_hash TEXT NOT NULL,
			name TEXT,
			notify_incoming BOOLEAN DEFAULT TRUE,
			notify_outgoing BOOLEAN DEFAULT TRUE,
			notify_erc20 BOOLEAN DEFAULT TRUE,
			notify_nft BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(watchlist_id, address_hash)
		);
		CREATE INDEX IF NOT EXISTS idx_account_watchlist_addr_watchlist ON account_watchlist_addresses(watchlist_id);
		CREATE INDEX IF NOT EXISTS idx_account_watchlist_addr_address ON account_watchlist_addresses(address_hash);

		-- Address tags table
		CREATE TABLE IF NOT EXISTS account_address_tags (
			id SERIAL PRIMARY KEY,
			user_id INT NOT NULL REFERENCES account_users(id) ON DELETE CASCADE,
			address_hash TEXT NOT NULL,
			tag TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(user_id, address_hash)
		);
		CREATE INDEX IF NOT EXISTS idx_account_address_tags_user ON account_address_tags(user_id);
		CREATE INDEX IF NOT EXISTS idx_account_address_tags_address ON account_address_tags(address_hash);

		-- Custom ABIs table
		CREATE TABLE IF NOT EXISTS account_custom_abis (
			id SERIAL PRIMARY KEY,
			user_id INT NOT NULL REFERENCES account_users(id) ON DELETE CASCADE,
			contract_hash TEXT NOT NULL,
			name TEXT NOT NULL,
			abi TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(user_id, contract_hash)
		);
		CREATE INDEX IF NOT EXISTS idx_account_custom_abis_user ON account_custom_abis(user_id);
		CREATE INDEX IF NOT EXISTS idx_account_custom_abis_contract ON account_custom_abis(contract_hash);

		-- Plans table
		CREATE TABLE IF NOT EXISTS account_plans (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			max_keys_per_day INT DEFAULT 3,
			max_req_per_sec INT DEFAULT 10,
			max_req_per_day INT DEFAULT 100000
		);

		-- Insert default plans
		INSERT INTO account_plans (name, max_keys_per_day, max_req_per_sec, max_req_per_day)
		VALUES
			('free', 3, 5, 10000),
			('basic', 5, 10, 100000),
			('pro', 10, 50, 1000000),
			('enterprise', 100, 500, 10000000)
		ON CONFLICT DO NOTHING;
	`

	_, err := s.db.ExecContext(ctx, schema)
	return err
}

// CreateUser creates a new user account
func (s *Service) CreateUser(ctx context.Context, user *User) error {
	// Generate UID hash for indexing
	uidHash := hashUID(user.UID)

	err := s.db.QueryRowContext(ctx, `
		INSERT INTO account_users (uid_hash, uid, email, name, nickname, avatar, address_hash, email_verified)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at
	`, uidHash, user.UID, user.Email, user.Name, user.Nickname, user.Avatar, user.AddressHash, user.EmailVerified).
		Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	// Create default watchlist
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO account_watchlists (user_id, name) VALUES ($1, 'default')
	`, user.ID)
	if err != nil {
		return fmt.Errorf("create default watchlist: %w", err)
	}

	return nil
}

// GetUserByUID retrieves a user by OAuth UID
func (s *Service) GetUserByUID(ctx context.Context, uid string) (*User, error) {
	uidHash := hashUID(uid)

	user := &User{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, uid, email, name, nickname, avatar, address_hash, email_verified, plan_id, created_at, updated_at
		FROM account_users
		WHERE uid_hash = $1
	`, uidHash).Scan(&user.ID, &user.UID, &user.Email, &user.Name, &user.Nickname,
		&user.Avatar, &user.AddressHash, &user.EmailVerified, &user.PlanID, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	return user, nil
}

// GetUserByID retrieves a user by ID
func (s *Service) GetUserByID(ctx context.Context, id int64) (*User, error) {
	user := &User{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, uid, email, name, nickname, avatar, address_hash, email_verified, plan_id, created_at, updated_at
		FROM account_users
		WHERE id = $1
	`, id).Scan(&user.ID, &user.UID, &user.Email, &user.Name, &user.Nickname,
		&user.Avatar, &user.AddressHash, &user.EmailVerified, &user.PlanID, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	return user, nil
}

// UpdateUser updates user information
func (s *Service) UpdateUser(ctx context.Context, user *User) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE account_users
		SET email = $2, name = $3, nickname = $4, avatar = $5, address_hash = $6, email_verified = $7, updated_at = NOW()
		WHERE id = $1
	`, user.ID, user.Email, user.Name, user.Nickname, user.Avatar, user.AddressHash, user.EmailVerified)

	return err
}

// DeleteUser deletes a user account and all associated data
func (s *Service) DeleteUser(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM account_users WHERE id = $1`, userID)
	return err
}

// API Key Methods

// CreateAPIKey creates a new API key for a user
func (s *Service) CreateAPIKey(ctx context.Context, userID int64, name string) (*APIKey, error) {
	// Check key limit
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM account_api_keys WHERE user_id = $1
	`, userID).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("count keys: %w", err)
	}
	if count >= s.maxAPIKeys {
		return nil, ErrAPIKeyLimitReached
	}

	// Generate new key
	keyValue := uuid.New().String()

	apiKey := &APIKey{
		Value:  keyValue,
		Name:   name,
		UserID: userID,
	}

	err = s.db.QueryRowContext(ctx, `
		INSERT INTO account_api_keys (value, name, user_id)
		VALUES ($1, $2, $3)
		RETURNING created_at, updated_at
	`, keyValue, name, userID).Scan(&apiKey.CreatedAt, &apiKey.UpdatedAt)

	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}

	return apiKey, nil
}

// GetAPIKey retrieves an API key by value
func (s *Service) GetAPIKey(ctx context.Context, keyValue string) (*APIKey, error) {
	apiKey := &APIKey{}
	err := s.db.QueryRowContext(ctx, `
		SELECT value, name, user_id, created_at, updated_at, last_used_at, request_count
		FROM account_api_keys
		WHERE value = $1
	`, keyValue).Scan(&apiKey.Value, &apiKey.Name, &apiKey.UserID,
		&apiKey.CreatedAt, &apiKey.UpdatedAt, &apiKey.LastUsedAt, &apiKey.RequestCount)

	if err == sql.ErrNoRows {
		return nil, ErrInvalidAPIKey
	}
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}

	return apiKey, nil
}

// GetUserAPIKeys retrieves all API keys for a user
func (s *Service) GetUserAPIKeys(ctx context.Context, userID int64) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT value, name, user_id, created_at, updated_at, last_used_at, request_count
		FROM account_api_keys
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get user api keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var key APIKey
		if err := rows.Scan(&key.Value, &key.Name, &key.UserID,
			&key.CreatedAt, &key.UpdatedAt, &key.LastUsedAt, &key.RequestCount); err != nil {
			continue
		}
		keys = append(keys, key)
	}

	return keys, nil
}

// UpdateAPIKeyUsage records API key usage
func (s *Service) UpdateAPIKeyUsage(ctx context.Context, keyValue string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE account_api_keys
		SET last_used_at = NOW(), request_count = request_count + 1, updated_at = NOW()
		WHERE value = $1
	`, keyValue)
	return err
}

// DeleteAPIKey deletes an API key
func (s *Service) DeleteAPIKey(ctx context.Context, userID int64, keyValue string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM account_api_keys WHERE value = $1 AND user_id = $2
	`, keyValue, userID)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrInvalidAPIKey
	}

	return nil
}

// Watchlist Methods

// GetWatchlist retrieves a watchlist by ID
func (s *Service) GetWatchlist(ctx context.Context, watchlistID int64) (*Watchlist, error) {
	watchlist := &Watchlist{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, created_at, updated_at
		FROM account_watchlists
		WHERE id = $1
	`, watchlistID).Scan(&watchlist.ID, &watchlist.UserID, &watchlist.Name,
		&watchlist.CreatedAt, &watchlist.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, ErrWatchlistNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get watchlist: %w", err)
	}

	return watchlist, nil
}

// GetUserWatchlists retrieves all watchlists for a user
func (s *Service) GetUserWatchlists(ctx context.Context, userID int64) ([]Watchlist, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, name, created_at, updated_at
		FROM account_watchlists
		WHERE user_id = $1
		ORDER BY created_at
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get user watchlists: %w", err)
	}
	defer rows.Close()

	var watchlists []Watchlist
	for rows.Next() {
		var w Watchlist
		if err := rows.Scan(&w.ID, &w.UserID, &w.Name, &w.CreatedAt, &w.UpdatedAt); err != nil {
			continue
		}
		watchlists = append(watchlists, w)
	}

	return watchlists, nil
}

// AddWatchlistAddress adds an address to a watchlist
func (s *Service) AddWatchlistAddress(ctx context.Context, addr *WatchlistAddress) error {
	addr.AddressHash = normalizeAddress(addr.AddressHash)

	err := s.db.QueryRowContext(ctx, `
		INSERT INTO account_watchlist_addresses
		(watchlist_id, address_hash, name, notify_incoming, notify_outgoing, notify_erc20, notify_nft)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at
	`, addr.WatchlistID, addr.AddressHash, addr.Name,
		addr.NotifyIncoming, addr.NotifyOutgoing, addr.NotifyERC20, addr.NotifyNFT).
		Scan(&addr.ID, &addr.CreatedAt, &addr.UpdatedAt)

	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			return ErrAddressExists
		}
		return fmt.Errorf("add watchlist address: %w", err)
	}

	return nil
}

// GetWatchlistAddresses retrieves all addresses in a watchlist
func (s *Service) GetWatchlistAddresses(ctx context.Context, watchlistID int64) ([]WatchlistAddress, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, watchlist_id, address_hash, name, notify_incoming, notify_outgoing, notify_erc20, notify_nft, created_at, updated_at
		FROM account_watchlist_addresses
		WHERE watchlist_id = $1
		ORDER BY created_at DESC
	`, watchlistID)
	if err != nil {
		return nil, fmt.Errorf("get watchlist addresses: %w", err)
	}
	defer rows.Close()

	var addresses []WatchlistAddress
	for rows.Next() {
		var a WatchlistAddress
		if err := rows.Scan(&a.ID, &a.WatchlistID, &a.AddressHash, &a.Name,
			&a.NotifyIncoming, &a.NotifyOutgoing, &a.NotifyERC20, &a.NotifyNFT,
			&a.CreatedAt, &a.UpdatedAt); err != nil {
			continue
		}
		addresses = append(addresses, a)
	}

	return addresses, nil
}

// RemoveWatchlistAddress removes an address from a watchlist
func (s *Service) RemoveWatchlistAddress(ctx context.Context, watchlistID int64, addressHash string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM account_watchlist_addresses
		WHERE watchlist_id = $1 AND address_hash = $2
	`, watchlistID, normalizeAddress(addressHash))
	return err
}

// Tag Methods

// SetAddressTag sets a custom tag for an address
func (s *Service) SetAddressTag(ctx context.Context, userID int64, addressHash, tag string) error {
	addressHash = normalizeAddress(addressHash)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO account_address_tags (user_id, address_hash, tag)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, address_hash) DO UPDATE SET tag = EXCLUDED.tag, updated_at = NOW()
	`, userID, addressHash, tag)

	return err
}

// GetUserAddressTags retrieves all address tags for a user
func (s *Service) GetUserAddressTags(ctx context.Context, userID int64) ([]TagAddress, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, address_hash, tag, created_at, updated_at
		FROM account_address_tags
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get user address tags: %w", err)
	}
	defer rows.Close()

	var tags []TagAddress
	for rows.Next() {
		var t TagAddress
		if err := rows.Scan(&t.ID, &t.UserID, &t.AddressHash, &t.Tag, &t.CreatedAt, &t.UpdatedAt); err != nil {
			continue
		}
		tags = append(tags, t)
	}

	return tags, nil
}

// DeleteAddressTag removes a custom tag
func (s *Service) DeleteAddressTag(ctx context.Context, userID int64, addressHash string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM account_address_tags
		WHERE user_id = $1 AND address_hash = $2
	`, userID, normalizeAddress(addressHash))
	return err
}

// Custom ABI Methods

// SetCustomABI sets a custom ABI for a contract
func (s *Service) SetCustomABI(ctx context.Context, userID int64, contractHash, name, abi string) error {
	contractHash = normalizeAddress(contractHash)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO account_custom_abis (user_id, contract_hash, name, abi)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, contract_hash) DO UPDATE
		SET name = EXCLUDED.name, abi = EXCLUDED.abi, updated_at = NOW()
	`, userID, contractHash, name, abi)

	return err
}

// GetUserCustomABIs retrieves all custom ABIs for a user
func (s *Service) GetUserCustomABIs(ctx context.Context, userID int64) ([]CustomABI, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, contract_hash, name, abi, created_at, updated_at
		FROM account_custom_abis
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get user custom abis: %w", err)
	}
	defer rows.Close()

	var abis []CustomABI
	for rows.Next() {
		var a CustomABI
		if err := rows.Scan(&a.ID, &a.UserID, &a.ContractHash, &a.Name, &a.ABI, &a.CreatedAt, &a.UpdatedAt); err != nil {
			continue
		}
		abis = append(abis, a)
	}

	return abis, nil
}

// GetCustomABI retrieves a specific custom ABI
func (s *Service) GetCustomABI(ctx context.Context, userID int64, contractHash string) (*CustomABI, error) {
	contractHash = normalizeAddress(contractHash)

	abi := &CustomABI{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, contract_hash, name, abi, created_at, updated_at
		FROM account_custom_abis
		WHERE user_id = $1 AND contract_hash = $2
	`, userID, contractHash).Scan(&abi.ID, &abi.UserID, &abi.ContractHash,
		&abi.Name, &abi.ABI, &abi.CreatedAt, &abi.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get custom abi: %w", err)
	}

	return abi, nil
}

// DeleteCustomABI removes a custom ABI
func (s *Service) DeleteCustomABI(ctx context.Context, userID int64, contractHash string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM account_custom_abis
		WHERE user_id = $1 AND contract_hash = $2
	`, userID, normalizeAddress(contractHash))
	return err
}

// Helper Functions

// hashUID creates a hash of the UID for database indexing
func hashUID(uid string) string {
	// Simple hash - in production use SHA256
	h := make([]byte, 32)
	copy(h, []byte(uid))
	return hex.EncodeToString(h)
}

// normalizeAddress converts address to lowercase with 0x prefix
func normalizeAddress(address string) string {
	address = strings.ToLower(address)
	if !strings.HasPrefix(address, "0x") {
		address = "0x" + address
	}
	return address
}

// generateRandomToken generates a random hex token
func generateRandomToken(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return hex.EncodeToString(b)
}
