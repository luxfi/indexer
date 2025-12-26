// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package account

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.MaxAPIKeysPerUser != 3 {
		t.Errorf("MaxAPIKeysPerUser = %d, want 3", config.MaxAPIKeysPerUser)
	}
}

func TestNormalizeAddress(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase with prefix",
			input:    "0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
			expected: "0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
		},
		{
			name:     "uppercase",
			input:    "0x742D35CC6634C0532925A3B844BC9E7595F2BD30",
			expected: "0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
		},
		{
			name:     "no prefix",
			input:    "742d35cc6634c0532925a3b844bc9e7595f2bd30",
			expected: "0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
		},
		{
			name:     "mixed case no prefix",
			input:    "742D35cc6634C0532925a3b844bc9E7595f2bD30",
			expected: "0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeAddress(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeAddress(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestHashUID(t *testing.T) {
	// Same input should produce same hash
	uid := "test-user-123"
	hash1 := hashUID(uid)
	hash2 := hashUID(uid)

	if hash1 != hash2 {
		t.Error("Same UID should produce same hash")
	}

	// Different input should produce different hash
	hash3 := hashUID("different-user")
	if hash1 == hash3 {
		t.Error("Different UIDs should produce different hashes")
	}
}

func TestGenerateRandomToken(t *testing.T) {
	token1 := generateRandomToken(16)
	token2 := generateRandomToken(16)

	if len(token1) != 32 { // hex encoding doubles length
		t.Errorf("Token length = %d, want 32", len(token1))
	}

	if token1 == token2 {
		t.Error("Random tokens should be different")
	}
}

func TestDefaultLimit(t *testing.T) {
	limit := DefaultLimit()

	if limit.RequestsPerSecond != 10 {
		t.Errorf("RequestsPerSecond = %d, want 10", limit.RequestsPerSecond)
	}
	if limit.RequestsPerDay != 100000 {
		t.Errorf("RequestsPerDay = %d, want 100000", limit.RequestsPerDay)
	}
	if limit.BurstSize != 20 {
		t.Errorf("BurstSize = %d, want 20", limit.BurstSize)
	}
}

func TestRateLimiter_Allow(t *testing.T) {
	limit := &Limit{
		RequestsPerSecond: 5,
		RequestsPerDay:    1000,
		BurstSize:         10,
	}
	rl := NewRateLimiter(limit)

	apiKey := "test-key-1"

	// Should allow burst requests
	for i := 0; i < 10; i++ {
		if !rl.Allow(apiKey) {
			t.Errorf("Request %d should be allowed (within burst)", i)
		}
	}

	// Next request should be rate limited
	if rl.Allow(apiKey) {
		t.Error("Request 11 should be rate limited")
	}
}

func TestRateLimiter_SetKeyLimit(t *testing.T) {
	rl := NewRateLimiter(DefaultLimit())

	apiKey := "custom-limit-key"
	customLimit := &Limit{
		RequestsPerSecond: 100,
		RequestsPerDay:    1000000,
		BurstSize:         200,
	}

	rl.SetKeyLimit(apiKey, customLimit)

	// Should allow up to burst size
	for i := 0; i < 200; i++ {
		if !rl.Allow(apiKey) {
			t.Errorf("Request %d should be allowed with custom limit", i)
		}
	}

	// Next should be limited
	if rl.Allow(apiKey) {
		t.Error("Request 201 should be rate limited")
	}
}

func TestRateLimiter_GetUsage(t *testing.T) {
	limit := &Limit{
		RequestsPerSecond: 10,
		RequestsPerDay:    1000,
		BurstSize:         20,
	}
	rl := NewRateLimiter(limit)

	apiKey := "usage-test-key"

	// Initial usage
	sec, day := rl.GetUsage(apiKey)
	if sec != 10 {
		t.Errorf("Initial secondsRemaining = %d, want 10", sec)
	}
	if day != 1000 {
		t.Errorf("Initial dayRemaining = %d, want 1000", day)
	}

	// Make some requests
	rl.Allow(apiKey)
	rl.Allow(apiKey)
	rl.Allow(apiKey)

	sec, day = rl.GetUsage(apiKey)
	// Day tokens should decrease
	if day != 997 {
		t.Errorf("After 3 requests dayRemaining = %d, want 997", day)
	}
}

func TestRateLimiter_RemoveKey(t *testing.T) {
	rl := NewRateLimiter(DefaultLimit())

	apiKey := "remove-test-key"

	// Add and use key
	rl.Allow(apiKey)

	// Remove key
	rl.RemoveKey(apiKey)

	// Usage should be reset to defaults
	sec, day := rl.GetUsage(apiKey)
	if sec != 10 {
		t.Errorf("After remove secondsRemaining = %d, want 10", sec)
	}
	if day != 100000 {
		t.Errorf("After remove dayRemaining = %d, want 100000", day)
	}
}

func TestUser_Struct(t *testing.T) {
	user := &User{
		ID:            1,
		UID:           "oauth-uid-123",
		Email:         "test@example.com",
		Name:          "Test User",
		Nickname:      "testuser",
		Avatar:        "https://example.com/avatar.png",
		AddressHash:   "0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
		EmailVerified: true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if user.ID <= 0 {
		t.Error("User ID should be positive")
	}
	if user.Email == "" {
		t.Error("User email should be set")
	}
}

func TestAPIKey_Struct(t *testing.T) {
	now := time.Now()
	key := &APIKey{
		Value:        "550e8400-e29b-41d4-a716-446655440000",
		Name:         "My API Key",
		UserID:       1,
		CreatedAt:    now,
		UpdatedAt:    now,
		LastUsedAt:   &now,
		RequestCount: 100,
	}

	if len(key.Value) != 36 { // UUID format
		t.Errorf("API key value length = %d, want 36", len(key.Value))
	}
	if key.Name == "" {
		t.Error("API key name should be set")
	}
}

func TestWatchlist_Struct(t *testing.T) {
	watchlist := &Watchlist{
		ID:        1,
		UserID:    1,
		Name:      "My Watchlist",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if watchlist.ID <= 0 {
		t.Error("Watchlist ID should be positive")
	}
	if watchlist.Name == "" {
		t.Error("Watchlist name should be set")
	}
}

func TestWatchlistAddress_Struct(t *testing.T) {
	addr := &WatchlistAddress{
		ID:             1,
		WatchlistID:    1,
		AddressHash:    "0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
		Name:           "My Wallet",
		NotifyIncoming: true,
		NotifyOutgoing: true,
		NotifyERC20:    true,
		NotifyNFT:      true,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if addr.AddressHash == "" {
		t.Error("Address hash should be set")
	}
	if !addr.NotifyIncoming {
		t.Error("Notify incoming should be true")
	}
}

func TestTagAddress_Struct(t *testing.T) {
	tag := &TagAddress{
		ID:          1,
		UserID:      1,
		AddressHash: "0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
		Tag:         "Binance Hot Wallet",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if tag.Tag == "" {
		t.Error("Tag should be set")
	}
}

func TestCustomABI_Struct(t *testing.T) {
	abi := &CustomABI{
		ID:           1,
		UserID:       1,
		ContractHash: "0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
		Name:         "MyToken",
		ABI:          `[{"type":"function","name":"transfer","inputs":[]}]`,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if abi.ABI == "" {
		t.Error("ABI should be set")
	}
	if abi.Name == "" {
		t.Error("Name should be set")
	}
}

func TestErrors(t *testing.T) {
	// Check error values are unique
	errors := []error{
		ErrUserNotFound,
		ErrInvalidAPIKey,
		ErrAPIKeyLimitReached,
		ErrWatchlistNotFound,
		ErrAddressExists,
		ErrRateLimited,
	}

	for i, e1 := range errors {
		for j, e2 := range errors {
			if i != j && e1.Error() == e2.Error() {
				t.Errorf("Errors should be unique: %v == %v", e1, e2)
			}
		}
	}
}

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("postgres", "postgres://localhost/explorer_evm_test?sslmode=disable")
	if err != nil {
		t.Skip("No test database available:", err)
	}
	if err := db.Ping(); err != nil {
		t.Skip("Cannot connect to test database:", err)
	}
	return db
}

func TestService_Integration(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	config := DefaultConfig()
	service := NewService(db, config)
	ctx := context.Background()

	// Initialize schema
	t.Run("InitSchema", func(t *testing.T) {
		err := service.InitSchema(ctx)
		if err != nil {
			t.Fatalf("InitSchema failed: %v", err)
		}
	})

	var testUserID int64

	// Create user
	t.Run("CreateUser", func(t *testing.T) {
		user := &User{
			UID:           "test-uid-" + generateRandomToken(8),
			Email:         "test@example.com",
			Name:          "Test User",
			Nickname:      "testuser",
			EmailVerified: true,
		}

		err := service.CreateUser(ctx, user)
		if err != nil {
			t.Fatalf("CreateUser failed: %v", err)
		}

		if user.ID <= 0 {
			t.Error("User ID should be set after creation")
		}
		testUserID = user.ID
	})

	// Get user by ID
	t.Run("GetUserByID", func(t *testing.T) {
		if testUserID == 0 {
			t.Skip("No user created")
		}

		user, err := service.GetUserByID(ctx, testUserID)
		if err != nil {
			t.Fatalf("GetUserByID failed: %v", err)
		}

		if user.Email != "test@example.com" {
			t.Errorf("Email = %s, want test@example.com", user.Email)
		}
	})

	var testAPIKey string

	// Create API key
	t.Run("CreateAPIKey", func(t *testing.T) {
		if testUserID == 0 {
			t.Skip("No user created")
		}

		key, err := service.CreateAPIKey(ctx, testUserID, "Test Key")
		if err != nil {
			t.Fatalf("CreateAPIKey failed: %v", err)
		}

		if key.Value == "" {
			t.Error("API key value should be set")
		}
		testAPIKey = key.Value
	})

	// Get API key
	t.Run("GetAPIKey", func(t *testing.T) {
		if testAPIKey == "" {
			t.Skip("No API key created")
		}

		key, err := service.GetAPIKey(ctx, testAPIKey)
		if err != nil {
			t.Fatalf("GetAPIKey failed: %v", err)
		}

		if key.Name != "Test Key" {
			t.Errorf("Name = %s, want Test Key", key.Name)
		}
	})

	// Get user watchlists
	t.Run("GetUserWatchlists", func(t *testing.T) {
		if testUserID == 0 {
			t.Skip("No user created")
		}

		watchlists, err := service.GetUserWatchlists(ctx, testUserID)
		if err != nil {
			t.Fatalf("GetUserWatchlists failed: %v", err)
		}

		if len(watchlists) == 0 {
			t.Error("User should have at least one default watchlist")
		}
	})

	// Set address tag
	t.Run("SetAddressTag", func(t *testing.T) {
		if testUserID == 0 {
			t.Skip("No user created")
		}

		err := service.SetAddressTag(ctx, testUserID, "0x742d35cc6634c0532925a3b844bc9e7595f2bd30", "My Wallet")
		if err != nil {
			t.Fatalf("SetAddressTag failed: %v", err)
		}

		tags, err := service.GetUserAddressTags(ctx, testUserID)
		if err != nil {
			t.Fatalf("GetUserAddressTags failed: %v", err)
		}

		if len(tags) == 0 {
			t.Error("Should have at least one tag")
		}
	})

	// Clean up
	t.Run("DeleteUser", func(t *testing.T) {
		if testUserID == 0 {
			t.Skip("No user created")
		}

		err := service.DeleteUser(ctx, testUserID)
		if err != nil {
			t.Fatalf("DeleteUser failed: %v", err)
		}

		// Verify user is deleted
		_, err = service.GetUserByID(ctx, testUserID)
		if err != ErrUserNotFound {
			t.Error("User should be deleted")
		}
	})
}

func BenchmarkRateLimiter_Allow(b *testing.B) {
	rl := NewRateLimiter(&Limit{
		RequestsPerSecond: 1000,
		RequestsPerDay:    1000000,
		BurstSize:         10000,
	})

	apiKey := "bench-key"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.Allow(apiKey)
	}
}

func BenchmarkNormalizeAddress(b *testing.B) {
	addresses := []string{
		"0x742d35cc6634c0532925a3b844bc9e7595f2bd30",
		"742D35CC6634C0532925A3B844BC9E7595F2BD30",
		"742d35cc6634c0532925a3b844bc9e7595f2bd30",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, addr := range addresses {
			normalizeAddress(addr)
		}
	}
}

func BenchmarkHashUID(b *testing.B) {
	uid := "oauth-user-12345678"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hashUID(uid)
	}
}
