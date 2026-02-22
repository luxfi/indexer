// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/luxfi/indexer/evm/account"
)

// contextKey is used for storing IAM user info in request context.
type contextKey string

const (
	// iamUserContextKey stores the authenticated user in the request context.
	iamUserContextKey contextKey = "iam_user"
)

// IAMConfig holds configuration for the IAM OIDC middleware.
type IAMConfig struct {
	// ServerURL is the IAM OIDC issuer (e.g. https://hanzo.id).
	ServerURL string

	// ClientID is the expected audience in JWT tokens.
	ClientID string

	// Enabled controls whether IAM authentication is active.
	Enabled bool
}

// IAMConfigFromEnv reads IAM configuration from environment variables.
func IAMConfigFromEnv() IAMConfig {
	return IAMConfig{
		ServerURL: getEnvDefault("IAM_SERVER_URL", "https://hanzo.id"),
		ClientID:  getEnvDefault("IAM_CLIENT_ID", "lux-explore-client-id"),
		Enabled:   os.Getenv("IAM_ENABLED") == "true",
	}
}

func getEnvDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// openIDConfiguration represents a subset of the OIDC discovery document.
type openIDConfiguration struct {
	Issuer  string `json:"issuer"`
	JWKSURI string `json:"jwks_uri"`
}

// jwksDocument represents a JSON Web Key Set.
type jwksDocument struct {
	Keys []jwkKey `json:"keys"`
}

// jwkKey represents a single JSON Web Key (RSA only for hanzo.id).
type jwkKey struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// jwtHeader is the decoded JWT header.
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

// IAMClaims represents the claims extracted from a validated JWT.
type IAMClaims struct {
	Sub           string `json:"sub"`
	Iss           string `json:"iss"`
	Aud           string `json:"aud"`
	Exp           int64  `json:"exp"`
	Iat           int64  `json:"iat"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
	Name          string `json:"name"`
	DisplayName   string `json:"displayName"`
	Avatar        string `json:"avatar"`
}

// IAMMiddleware provides JWT validation using OIDC discovery and JWKS.
type IAMMiddleware struct {
	config     IAMConfig
	accountSvc *account.Service
	httpClient *http.Client

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	issuer    string
	fetchedAt time.Time
	cacheTTL  time.Duration
}

// NewIAMMiddleware creates a new IAM OIDC middleware.
func NewIAMMiddleware(cfg IAMConfig, accountSvc *account.Service) *IAMMiddleware {
	return &IAMMiddleware{
		config:     cfg,
		accountSvc: accountSvc,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		keys:       make(map[string]*rsa.PublicKey),
		cacheTTL:   1 * time.Hour,
	}
}

// RequireAuth returns a middleware that requires a valid JWT bearer token.
// On success, the authenticated account.User is stored in the request context.
func (m *IAMMiddleware) RequireAuth(next http.Handler) http.Handler {
	if !m.config.Enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := extractBearerToken(r)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
			return
		}

		claims, err := m.validateToken(token)
		if err != nil {
			log.Printf("[IAM] token validation failed: %v", err)
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		user, err := m.resolveUser(r.Context(), claims)
		if err != nil {
			log.Printf("[IAM] user resolution failed: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to resolve user account")
			return
		}

		ctx := context.WithValue(r.Context(), iamUserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalAuth returns a middleware that validates the JWT if present but does
// not reject unauthenticated requests. Useful for endpoints where auth is
// optional but enhances the response.
func (m *IAMMiddleware) OptionalAuth(next http.Handler) http.Handler {
	if !m.config.Enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := extractBearerToken(r)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		claims, err := m.validateToken(token)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		user, err := m.resolveUser(r.Context(), claims)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), iamUserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFromContext extracts the authenticated user from the request context.
// Returns nil if no user is present (unauthenticated request).
func UserFromContext(ctx context.Context) *account.User {
	user, _ := ctx.Value(iamUserContextKey).(*account.User)
	return user
}

// validateToken parses and validates a JWT token string.
func (m *IAMMiddleware) validateToken(tokenStr string) (*IAMClaims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed JWT: expected 3 parts")
	}

	// Decode header
	headerBytes, err := base64URLDecode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	var header jwtHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("parse header: %w", err)
	}

	if header.Alg != "RS256" {
		return nil, fmt.Errorf("unsupported algorithm: %s", header.Alg)
	}

	// Decode claims
	claimsBytes, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode claims: %w", err)
	}
	var claims IAMClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}

	// Validate issuer
	expectedIssuer := strings.TrimSuffix(m.config.ServerURL, "/")
	if claims.Iss != expectedIssuer {
		return nil, fmt.Errorf("issuer mismatch: got %q, want %q", claims.Iss, expectedIssuer)
	}

	// Validate expiry
	now := time.Now().Unix()
	if claims.Exp <= now {
		return nil, errors.New("token has expired")
	}

	// Verify signature
	key, err := m.getPublicKey(header.Kid)
	if err != nil {
		return nil, fmt.Errorf("get public key: %w", err)
	}

	sigBytes, err := base64URLDecode(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	if err := verifyRS256(parts[0]+"."+parts[1], sigBytes, key); err != nil {
		return nil, fmt.Errorf("signature verification: %w", err)
	}

	return &claims, nil
}

// resolveUser maps IAM claims to a local account.User, creating one if needed.
func (m *IAMMiddleware) resolveUser(ctx context.Context, claims *IAMClaims) (*account.User, error) {
	if m.accountSvc == nil {
		return &account.User{
			UID:           claims.Sub,
			Email:         claims.Email,
			Name:          claims.Name,
			EmailVerified: claims.EmailVerified,
			Avatar:        claims.Avatar,
		}, nil
	}

	user, err := m.accountSvc.GetUserByUID(ctx, claims.Sub)
	if err == nil {
		// Update user info if changed
		changed := false
		if claims.Email != "" && user.Email != claims.Email {
			user.Email = claims.Email
			changed = true
		}
		if claims.Name != "" && user.Name != claims.Name {
			user.Name = claims.Name
			changed = true
		}
		displayName := claims.DisplayName
		if displayName == "" {
			displayName = claims.Name
		}
		if displayName != "" && user.Nickname != displayName {
			user.Nickname = displayName
			changed = true
		}
		if claims.Avatar != "" && user.Avatar != claims.Avatar {
			user.Avatar = claims.Avatar
			changed = true
		}
		if claims.EmailVerified && !user.EmailVerified {
			user.EmailVerified = true
			changed = true
		}
		if changed {
			if updateErr := m.accountSvc.UpdateUser(ctx, user); updateErr != nil {
				log.Printf("[IAM] warning: failed to update user %d: %v", user.ID, updateErr)
			}
		}
		return user, nil
	}

	if !errors.Is(err, account.ErrUserNotFound) {
		return nil, fmt.Errorf("lookup user: %w", err)
	}

	// Create new user
	newUser := &account.User{
		UID:           claims.Sub,
		Email:         claims.Email,
		Name:          claims.Name,
		Nickname:      claims.DisplayName,
		Avatar:        claims.Avatar,
		EmailVerified: claims.EmailVerified,
	}
	if err := m.accountSvc.CreateUser(ctx, newUser); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	log.Printf("[IAM] created new user %d for IAM subject %s (%s)", newUser.ID, claims.Sub, claims.Email)
	return newUser, nil
}

// getPublicKey retrieves an RSA public key by key ID, refreshing the cache if needed.
func (m *IAMMiddleware) getPublicKey(kid string) (*rsa.PublicKey, error) {
	m.mu.RLock()
	key, found := m.keys[kid]
	expired := time.Since(m.fetchedAt) > m.cacheTTL
	m.mu.RUnlock()

	if found && !expired {
		return key, nil
	}

	// Refresh JWKS
	if err := m.refreshJWKS(); err != nil {
		// If we have a cached key and the refresh failed, use the cached key
		if found {
			log.Printf("[IAM] JWKS refresh failed (using cached key): %v", err)
			return key, nil
		}
		return nil, err
	}

	m.mu.RLock()
	key, found = m.keys[kid]
	m.mu.RUnlock()

	if !found {
		return nil, fmt.Errorf("key ID %q not found in JWKS", kid)
	}
	return key, nil
}

// refreshJWKS fetches the JWKS from the OIDC discovery endpoint.
func (m *IAMMiddleware) refreshJWKS() error {
	// Fetch OIDC discovery document
	discoveryURL := strings.TrimSuffix(m.config.ServerURL, "/") + "/.well-known/openid-configuration"
	resp, err := m.httpClient.Get(discoveryURL)
	if err != nil {
		return fmt.Errorf("fetch OIDC discovery: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OIDC discovery returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read OIDC discovery: %w", err)
	}

	var oidcConfig openIDConfiguration
	if err := json.Unmarshal(body, &oidcConfig); err != nil {
		return fmt.Errorf("parse OIDC discovery: %w", err)
	}

	if oidcConfig.JWKSURI == "" {
		return errors.New("OIDC discovery: missing jwks_uri")
	}

	// Fetch JWKS
	jwksResp, err := m.httpClient.Get(oidcConfig.JWKSURI)
	if err != nil {
		return fmt.Errorf("fetch JWKS: %w", err)
	}
	defer jwksResp.Body.Close()

	if jwksResp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS returned status %d", jwksResp.StatusCode)
	}

	jwksBody, err := io.ReadAll(jwksResp.Body)
	if err != nil {
		return fmt.Errorf("read JWKS: %w", err)
	}

	var jwks jwksDocument
	if err := json.Unmarshal(jwksBody, &jwks); err != nil {
		return fmt.Errorf("parse JWKS: %w", err)
	}

	// Parse RSA public keys
	newKeys := make(map[string]*rsa.PublicKey)
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pubKey, err := parseRSAPublicKey(k)
		if err != nil {
			log.Printf("[IAM] skipping key %s: %v", k.Kid, err)
			continue
		}
		newKeys[k.Kid] = pubKey
	}

	if len(newKeys) == 0 {
		return errors.New("no valid RSA keys found in JWKS")
	}

	m.mu.Lock()
	m.keys = newKeys
	m.fetchedAt = time.Now()
	m.mu.Unlock()

	log.Printf("[IAM] refreshed JWKS: %d RSA keys loaded", len(newKeys))
	return nil
}

// parseRSAPublicKey converts a JWK RSA key to a Go *rsa.PublicKey.
func parseRSAPublicKey(k jwkKey) (*rsa.PublicKey, error) {
	nBytes, err := base64URLDecode(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode modulus: %w", err)
	}

	eBytes, err := base64URLDecode(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode exponent: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)

	// Exponent is a big-endian unsigned integer
	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}

// verifyRS256 verifies an RS256 (RSASSA-PKCS1-v1_5 with SHA-256) signature.
func verifyRS256(signingInput string, signature []byte, key *rsa.PublicKey) error {
	h := sha256.Sum256([]byte(signingInput))
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, h[:], signature)
}

// extractBearerToken extracts the bearer token from the Authorization header.
func extractBearerToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", errors.New("no Authorization header")
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		return "", errors.New("Authorization header is not Bearer type")
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" {
		return "", errors.New("empty bearer token")
	}

	return token, nil
}

// base64URLDecode decodes a base64url-encoded string (no padding).
func base64URLDecode(s string) ([]byte, error) {
	// Add padding if needed
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{Message: message})
}
