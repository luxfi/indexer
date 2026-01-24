// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package multichain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Errors for NFT metadata operations
var (
	ErrInvalidURI        = errors.New("invalid token URI")
	ErrUnsupportedScheme = errors.New("unsupported URI scheme")
	ErrFetchFailed       = errors.New("metadata fetch failed")
	ErrParseFailed       = errors.New("metadata parse failed")
	ErrRateLimited       = errors.New("rate limit exceeded")
	ErrCacheMiss         = errors.New("cache miss")
	ErrTimeout           = errors.New("request timeout")
)

// IPFS gateway URLs in preference order
var defaultIPFSGateways = []string{
	"https://cloudflare-ipfs.com/ipfs/",
	"https://ipfs.io/ipfs/",
	"https://gateway.pinata.cloud/ipfs/",
	"https://dweb.link/ipfs/",
	"https://w3s.link/ipfs/",
}

// Arweave gateway URL
const defaultArweaveGateway = "https://arweave.net/"

// Trait represents a single NFT attribute/trait
type Trait struct {
	TraitType   string      `json:"trait_type"`
	Value       interface{} `json:"value"`
	DisplayType string      `json:"display_type,omitempty"`
	MaxValue    interface{} `json:"max_value,omitempty"`
}

// NFTMetadata represents parsed NFT metadata (OpenSea/ERC721 format)
type NFTMetadata struct {
	Name            string                 `json:"name"`
	Description     string                 `json:"description"`
	Image           string                 `json:"image"`
	ImageData       string                 `json:"image_data,omitempty"`
	ExternalURL     string                 `json:"external_url,omitempty"`
	AnimationURL    string                 `json:"animation_url,omitempty"`
	BackgroundColor string                 `json:"background_color,omitempty"`
	Attributes      []Trait                `json:"attributes,omitempty"`
	Properties      map[string]interface{} `json:"properties,omitempty"`

	// Additional fields for extended metadata
	TokenID    string `json:"token_id,omitempty"`
	Collection string `json:"collection,omitempty"`

	// Raw JSON for custom fields
	Raw json.RawMessage `json:"-"`
}

// cacheEntry holds cached metadata with expiration
type cacheEntry struct {
	metadata  *NFTMetadata
	expiresAt time.Time
}

// rateLimiter implements token bucket rate limiting
type rateLimiter struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

// NFTMetadataServiceConfig holds service configuration
type NFTMetadataServiceConfig struct {
	// HTTP client settings
	Timeout            time.Duration
	MaxRetries         int
	RetryDelay         time.Duration
	MaxResponseSize    int64
	UserAgent          string

	// IPFS settings
	IPFSGateways       []string
	ArweaveGateway     string

	// Cache settings
	CacheTTL           time.Duration
	MaxCacheSize       int

	// Rate limiting
	RequestsPerSecond  float64
	BurstSize          int
}

// DefaultNFTMetadataServiceConfig returns default configuration
func DefaultNFTMetadataServiceConfig() *NFTMetadataServiceConfig {
	return &NFTMetadataServiceConfig{
		Timeout:           30 * time.Second,
		MaxRetries:        3,
		RetryDelay:        time.Second,
		MaxResponseSize:   10 * 1024 * 1024, // 10MB
		UserAgent:         "LuxIndexer/1.0",
		IPFSGateways:      defaultIPFSGateways,
		ArweaveGateway:    defaultArweaveGateway,
		CacheTTL:          time.Hour,
		MaxCacheSize:      10000,
		RequestsPerSecond: 10.0,
		BurstSize:         20,
	}
}

// NFTMetadataService fetches and parses NFT metadata from various sources
type NFTMetadataService struct {
	mu     sync.RWMutex
	config *NFTMetadataServiceConfig
	client *http.Client
	cache  map[string]*cacheEntry
	limiter *rateLimiter

	// Stats
	cacheHits   uint64
	cacheMisses uint64
	fetchCount  uint64
	errorCount  uint64
}

// NewNFTMetadataService creates a new metadata service
func NewNFTMetadataService(config *NFTMetadataServiceConfig) *NFTMetadataService {
	if config == nil {
		config = DefaultNFTMetadataServiceConfig()
	}

	return &NFTMetadataService{
		config: config,
		client: &http.Client{
			Timeout: config.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		cache: make(map[string]*cacheEntry),
		limiter: &rateLimiter{
			tokens:     float64(config.BurstSize),
			maxTokens:  float64(config.BurstSize),
			refillRate: config.RequestsPerSecond,
			lastRefill: time.Now(),
		},
	}
}

// FetchMetadata fetches NFT metadata from a token URI
// Supports: IPFS (ipfs://), Arweave (ar://), HTTP/HTTPS, data URIs
func (s *NFTMetadataService) FetchMetadata(ctx context.Context, tokenURI string) (*NFTMetadata, error) {
	if tokenURI == "" {
		return nil, ErrInvalidURI
	}

	// Check cache first
	if metadata := s.getFromCache(tokenURI); metadata != nil {
		return metadata, nil
	}

	// Rate limit check
	if !s.acquireToken() {
		return nil, ErrRateLimited
	}

	// Resolve URI to HTTP URL
	httpURL, err := s.resolveURI(tokenURI)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURI, err)
	}

	// Handle data URIs
	if strings.HasPrefix(httpURL, "data:") {
		return s.parseDataURI(httpURL)
	}

	// Fetch with retries
	data, err := s.fetchWithRetry(ctx, httpURL)
	if err != nil {
		s.mu.Lock()
		s.errorCount++
		s.mu.Unlock()
		return nil, err
	}

	// Parse metadata
	metadata, err := s.ParseMetadata(data)
	if err != nil {
		s.mu.Lock()
		s.errorCount++
		s.mu.Unlock()
		return nil, err
	}

	// Cache result
	s.putInCache(tokenURI, metadata)

	s.mu.Lock()
	s.fetchCount++
	s.mu.Unlock()

	return metadata, nil
}

// ParseMetadata parses raw JSON data into NFTMetadata struct
func (s *NFTMetadataService) ParseMetadata(data []byte) (*NFTMetadata, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: empty data", ErrParseFailed)
	}

	// Store raw JSON
	metadata := &NFTMetadata{
		Raw: json.RawMessage(data),
	}

	// Parse JSON
	if err := json.Unmarshal(data, metadata); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrParseFailed, err)
	}

	// Handle alternative attribute formats
	if len(metadata.Attributes) == 0 && metadata.Properties != nil {
		metadata.Attributes = s.extractTraitsFromProperties(metadata.Properties)
	}

	// Resolve image URLs if they are IPFS/Arweave
	if metadata.Image != "" {
		if resolved, err := s.resolveURI(metadata.Image); err == nil {
			metadata.Image = resolved
		}
	}
	if metadata.AnimationURL != "" {
		if resolved, err := s.resolveURI(metadata.AnimationURL); err == nil {
			metadata.AnimationURL = resolved
		}
	}

	return metadata, nil
}

// ExtractTraits extracts trait_type/value pairs from metadata
func (s *NFTMetadataService) ExtractTraits(metadata *NFTMetadata) []Trait {
	if metadata == nil {
		return nil
	}

	// Return existing attributes if present
	if len(metadata.Attributes) > 0 {
		traits := make([]Trait, len(metadata.Attributes))
		copy(traits, metadata.Attributes)
		return traits
	}

	// Try to extract from properties
	if metadata.Properties != nil {
		return s.extractTraitsFromProperties(metadata.Properties)
	}

	return nil
}

// extractTraitsFromProperties converts properties map to traits
func (s *NFTMetadataService) extractTraitsFromProperties(props map[string]interface{}) []Trait {
	traits := make([]Trait, 0, len(props))

	for key, value := range props {
		trait := Trait{
			TraitType: key,
			Value:     value,
		}

		// Handle nested property format
		if m, ok := value.(map[string]interface{}); ok {
			if v, exists := m["value"]; exists {
				trait.Value = v
			}
			if dt, exists := m["display_type"]; exists {
				if dtStr, ok := dt.(string); ok {
					trait.DisplayType = dtStr
				}
			}
			if mv, exists := m["max_value"]; exists {
				trait.MaxValue = mv
			}
		}

		traits = append(traits, trait)
	}

	return traits
}

// resolveURI converts various URI schemes to HTTP URLs
func (s *NFTMetadataService) resolveURI(uri string) (string, error) {
	uri = strings.TrimSpace(uri)

	// Handle data URIs
	if strings.HasPrefix(uri, "data:") {
		return uri, nil
	}

	// Handle IPFS URIs
	if strings.HasPrefix(uri, "ipfs://") {
		cid := strings.TrimPrefix(uri, "ipfs://")
		// Remove any leading slashes
		cid = strings.TrimPrefix(cid, "/")
		// Use first gateway
		if len(s.config.IPFSGateways) > 0 {
			return s.config.IPFSGateways[0] + cid, nil
		}
		return defaultIPFSGateways[0] + cid, nil
	}

	// Handle Arweave URIs
	if strings.HasPrefix(uri, "ar://") {
		txID := strings.TrimPrefix(uri, "ar://")
		gateway := s.config.ArweaveGateway
		if gateway == "" {
			gateway = defaultArweaveGateway
		}
		return gateway + txID, nil
	}

	// Handle HTTP/HTTPS
	if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
		// Convert IPFS gateway URLs to use preferred gateway
		for _, gw := range append(defaultIPFSGateways, s.config.IPFSGateways...) {
			if strings.HasPrefix(uri, gw) {
				cid := strings.TrimPrefix(uri, gw)
				if len(s.config.IPFSGateways) > 0 {
					return s.config.IPFSGateways[0] + cid, nil
				}
				return uri, nil
			}
		}
		return uri, nil
	}

	// Handle bare CIDs (assume IPFS)
	if isIPFSCID(uri) {
		if len(s.config.IPFSGateways) > 0 {
			return s.config.IPFSGateways[0] + uri, nil
		}
		return defaultIPFSGateways[0] + uri, nil
	}

	return "", fmt.Errorf("unsupported URI scheme: %s", uri)
}

// parseDataURI parses a data: URI and extracts metadata
func (s *NFTMetadataService) parseDataURI(dataURI string) (*NFTMetadata, error) {
	// Format: data:[<mediatype>][;base64],<data>
	if !strings.HasPrefix(dataURI, "data:") {
		return nil, ErrInvalidURI
	}

	// Find comma separator
	commaIdx := strings.Index(dataURI, ",")
	if commaIdx < 0 {
		return nil, fmt.Errorf("%w: no data in data URI", ErrInvalidURI)
	}

	header := dataURI[5:commaIdx]
	data := dataURI[commaIdx+1:]

	// Check if base64 encoded
	isBase64 := strings.Contains(header, ";base64")

	var jsonData []byte
	if isBase64 {
		// Simple base64 decode (standard library)
		decoded, err := decodeBase64(data)
		if err != nil {
			return nil, fmt.Errorf("%w: base64 decode failed: %v", ErrParseFailed, err)
		}
		jsonData = decoded
	} else {
		// URL decode
		decoded, err := url.QueryUnescape(data)
		if err != nil {
			return nil, fmt.Errorf("%w: URL decode failed: %v", ErrParseFailed, err)
		}
		jsonData = []byte(decoded)
	}

	return s.ParseMetadata(jsonData)
}

// fetchWithRetry fetches URL with retry logic
func (s *NFTMetadataService) fetchWithRetry(ctx context.Context, httpURL string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= s.config.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(s.config.RetryDelay * time.Duration(attempt)):
			}
		}

		data, err := s.fetch(ctx, httpURL)
		if err == nil {
			return data, nil
		}

		lastErr = err

		// Don't retry on certain errors
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			break
		}
	}

	// Try alternative gateways for IPFS
	if strings.Contains(httpURL, "/ipfs/") {
		data, err := s.fetchFromAlternativeGateways(ctx, httpURL)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("%w: %v after %d attempts", ErrFetchFailed, lastErr, s.config.MaxRetries+1)
}

// fetchFromAlternativeGateways tries alternative IPFS gateways
func (s *NFTMetadataService) fetchFromAlternativeGateways(ctx context.Context, httpURL string) ([]byte, error) {
	// Extract CID from URL
	var cid string
	for _, gw := range append(defaultIPFSGateways, s.config.IPFSGateways...) {
		if strings.Contains(httpURL, gw) {
			cid = strings.TrimPrefix(httpURL, gw)
			break
		}
	}

	if cid == "" {
		// Try to extract from generic /ipfs/ path
		idx := strings.Index(httpURL, "/ipfs/")
		if idx >= 0 {
			cid = httpURL[idx+6:]
		}
	}

	if cid == "" {
		return nil, fmt.Errorf("could not extract CID from URL")
	}

	// Try each gateway
	gateways := s.config.IPFSGateways
	if len(gateways) == 0 {
		gateways = defaultIPFSGateways
	}

	var lastErr error
	for i, gw := range gateways {
		if i == 0 {
			continue // Skip first gateway (already tried)
		}

		altURL := gw + cid
		data, err := s.fetch(ctx, altURL)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}

	return nil, lastErr
}

// fetch performs a single HTTP GET request
func (s *NFTMetadataService) fetch(ctx context.Context, httpURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", s.config.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Limit response size
	reader := io.LimitReader(resp.Body, s.config.MaxResponseSize)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// Cache operations

func (s *NFTMetadataService) getFromCache(key string) *NFTMetadata {
	s.mu.RLock()
	entry, ok := s.cache[key]
	s.mu.RUnlock()

	if !ok {
		s.mu.Lock()
		s.cacheMisses++
		s.mu.Unlock()
		return nil
	}

	if time.Now().After(entry.expiresAt) {
		s.mu.Lock()
		delete(s.cache, key)
		s.cacheMisses++
		s.mu.Unlock()
		return nil
	}

	s.mu.Lock()
	s.cacheHits++
	s.mu.Unlock()

	return entry.metadata
}

func (s *NFTMetadataService) putInCache(key string, metadata *NFTMetadata) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Evict oldest entries if cache is full
	if len(s.cache) >= s.config.MaxCacheSize {
		s.evictOldest()
	}

	s.cache[key] = &cacheEntry{
		metadata:  metadata,
		expiresAt: time.Now().Add(s.config.CacheTTL),
	}
}

func (s *NFTMetadataService) evictOldest() {
	// Simple eviction: remove expired entries first
	now := time.Now()
	for key, entry := range s.cache {
		if now.After(entry.expiresAt) {
			delete(s.cache, key)
		}
	}

	// If still over limit, remove 10% of entries
	if len(s.cache) >= s.config.MaxCacheSize {
		count := 0
		toRemove := s.config.MaxCacheSize / 10
		for key := range s.cache {
			delete(s.cache, key)
			count++
			if count >= toRemove {
				break
			}
		}
	}
}

// ClearCache clears the metadata cache
func (s *NFTMetadataService) ClearCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = make(map[string]*cacheEntry)
}

// Rate limiting

func (s *NFTMetadataService) acquireToken() bool {
	s.limiter.mu.Lock()
	defer s.limiter.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(s.limiter.lastRefill).Seconds()

	// Refill tokens
	s.limiter.tokens += elapsed * s.limiter.refillRate
	if s.limiter.tokens > s.limiter.maxTokens {
		s.limiter.tokens = s.limiter.maxTokens
	}
	s.limiter.lastRefill = now

	// Try to consume a token
	if s.limiter.tokens >= 1.0 {
		s.limiter.tokens--
		return true
	}

	return false
}

// Stats returns service statistics
func (s *NFTMetadataService) Stats() map[string]uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]uint64{
		"cache_hits":   s.cacheHits,
		"cache_misses": s.cacheMisses,
		"cache_size":   uint64(len(s.cache)),
		"fetch_count":  s.fetchCount,
		"error_count":  s.errorCount,
	}
}

// Helper functions

// isIPFSCID checks if a string looks like an IPFS CID
func isIPFSCID(s string) bool {
	// CIDv0 starts with Qm and is 46 chars
	if strings.HasPrefix(s, "Qm") && len(s) >= 46 {
		return true
	}
	// CIDv1 starts with bafy (base32) or b (other bases)
	if strings.HasPrefix(s, "bafy") || strings.HasPrefix(s, "bafk") {
		return true
	}
	return false
}

// decodeBase64 decodes base64 string (standard library implementation)
func decodeBase64(s string) ([]byte, error) {
	// Base64 alphabet
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

	// Build reverse lookup
	decodeMap := make(map[byte]int)
	for i := 0; i < len(alphabet); i++ {
		decodeMap[alphabet[i]] = i
	}
	decodeMap['='] = 0

	// Remove whitespace
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, s)

	// Pad if necessary
	for len(s)%4 != 0 {
		s += "="
	}

	result := make([]byte, 0, len(s)*3/4)

	for i := 0; i < len(s); i += 4 {
		a, ok1 := decodeMap[s[i]]
		b, ok2 := decodeMap[s[i+1]]
		c, ok3 := decodeMap[s[i+2]]
		d, ok4 := decodeMap[s[i+3]]

		if !ok1 || !ok2 || !ok3 || !ok4 {
			return nil, fmt.Errorf("invalid base64 character")
		}

		triple := (a << 18) | (b << 12) | (c << 6) | d

		result = append(result, byte(triple>>16))
		if s[i+2] != '=' {
			result = append(result, byte(triple>>8))
		}
		if s[i+3] != '=' {
			result = append(result, byte(triple))
		}
	}

	return result, nil
}
