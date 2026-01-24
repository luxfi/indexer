// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package multichain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNFTMetadataService_ParseMetadata(t *testing.T) {
	service := NewNFTMetadataService(nil)

	tests := []struct {
		name    string
		input   string
		want    *NFTMetadata
		wantErr bool
	}{
		{
			name: "basic OpenSea format",
			input: `{
				"name": "Cool NFT #1",
				"description": "A very cool NFT",
				"image": "https://example.com/image.png",
				"external_url": "https://example.com/nft/1",
				"attributes": [
					{"trait_type": "Color", "value": "Blue"},
					{"trait_type": "Size", "value": 42, "display_type": "number"}
				]
			}`,
			want: &NFTMetadata{
				Name:        "Cool NFT #1",
				Description: "A very cool NFT",
				Image:       "https://example.com/image.png",
				ExternalURL: "https://example.com/nft/1",
				Attributes: []Trait{
					{TraitType: "Color", Value: "Blue"},
					{TraitType: "Size", Value: float64(42), DisplayType: "number"},
				},
			},
			wantErr: false,
		},
		{
			name: "IPFS image URL",
			input: `{
				"name": "IPFS NFT",
				"image": "ipfs://QmTest1234567890abcdefghijklmnopqrstuvwxyz"
			}`,
			want: &NFTMetadata{
				Name:  "IPFS NFT",
				Image: "https://cloudflare-ipfs.com/ipfs/QmTest1234567890abcdefghijklmnopqrstuvwxyz",
			},
			wantErr: false,
		},
		{
			name: "with animation_url",
			input: `{
				"name": "Animated NFT",
				"image": "https://example.com/poster.png",
				"animation_url": "https://example.com/video.mp4"
			}`,
			want: &NFTMetadata{
				Name:         "Animated NFT",
				Image:        "https://example.com/poster.png",
				AnimationURL: "https://example.com/video.mp4",
			},
			wantErr: false,
		},
		{
			name:    "empty data",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   "{not valid json",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.ParseMetadata([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseMetadata() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if got.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.Description != tt.want.Description {
				t.Errorf("Description = %q, want %q", got.Description, tt.want.Description)
			}
			if got.Image != tt.want.Image {
				t.Errorf("Image = %q, want %q", got.Image, tt.want.Image)
			}
			if got.ExternalURL != tt.want.ExternalURL {
				t.Errorf("ExternalURL = %q, want %q", got.ExternalURL, tt.want.ExternalURL)
			}
			if got.AnimationURL != tt.want.AnimationURL {
				t.Errorf("AnimationURL = %q, want %q", got.AnimationURL, tt.want.AnimationURL)
			}
		})
	}
}

func TestNFTMetadataService_ExtractTraits(t *testing.T) {
	service := NewNFTMetadataService(nil)

	tests := []struct {
		name     string
		metadata *NFTMetadata
		wantLen  int
	}{
		{
			name: "with attributes",
			metadata: &NFTMetadata{
				Attributes: []Trait{
					{TraitType: "Color", Value: "Red"},
					{TraitType: "Size", Value: 10},
				},
			},
			wantLen: 2,
		},
		{
			name: "with properties",
			metadata: &NFTMetadata{
				Properties: map[string]interface{}{
					"rarity": "legendary",
					"power":  100,
				},
			},
			wantLen: 2,
		},
		{
			name:     "nil metadata",
			metadata: nil,
			wantLen:  0,
		},
		{
			name:     "empty metadata",
			metadata: &NFTMetadata{},
			wantLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			traits := service.ExtractTraits(tt.metadata)
			if len(traits) != tt.wantLen {
				t.Errorf("ExtractTraits() returned %d traits, want %d", len(traits), tt.wantLen)
			}
		})
	}
}

func TestNFTMetadataService_ResolveURI(t *testing.T) {
	service := NewNFTMetadataService(nil)

	tests := []struct {
		name    string
		uri     string
		wantErr bool
		want    string
	}{
		{
			name:    "IPFS URI",
			uri:     "ipfs://QmTest1234567890abcdefghijklmnopqrstuvwxyz",
			wantErr: false,
			want:    "https://cloudflare-ipfs.com/ipfs/QmTest1234567890abcdefghijklmnopqrstuvwxyz",
		},
		{
			name:    "IPFS URI with leading slash",
			uri:     "ipfs:///QmTest1234567890abcdefghijklmnopqrstuvwxyz",
			wantErr: false,
			want:    "https://cloudflare-ipfs.com/ipfs/QmTest1234567890abcdefghijklmnopqrstuvwxyz",
		},
		{
			name:    "Arweave URI",
			uri:     "ar://abc123def456",
			wantErr: false,
			want:    "https://arweave.net/abc123def456",
		},
		{
			name:    "HTTP URL",
			uri:     "https://example.com/metadata.json",
			wantErr: false,
			want:    "https://example.com/metadata.json",
		},
		{
			name:    "data URI",
			uri:     "data:application/json,{\"name\":\"test\"}",
			wantErr: false,
			want:    "data:application/json,{\"name\":\"test\"}",
		},
		{
			name:    "bare CIDv0",
			uri:     "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG",
			wantErr: false,
			want:    "https://cloudflare-ipfs.com/ipfs/QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG",
		},
		{
			name:    "bare CIDv1",
			uri:     "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi",
			wantErr: false,
			want:    "https://cloudflare-ipfs.com/ipfs/bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.resolveURI(tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveURI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("resolveURI() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNFTMetadataService_FetchMetadata(t *testing.T) {
	// Create test server
	metadata := NFTMetadata{
		Name:        "Test NFT",
		Description: "Test description",
		Image:       "https://example.com/image.png",
		Attributes: []Trait{
			{TraitType: "Rarity", Value: "Common"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metadata)
	}))
	defer server.Close()

	config := DefaultNFTMetadataServiceConfig()
	config.Timeout = 5 * time.Second
	service := NewNFTMetadataService(config)

	ctx := context.Background()
	got, err := service.FetchMetadata(ctx, server.URL)
	if err != nil {
		t.Fatalf("FetchMetadata() error = %v", err)
	}

	if got.Name != metadata.Name {
		t.Errorf("Name = %q, want %q", got.Name, metadata.Name)
	}
	if got.Description != metadata.Description {
		t.Errorf("Description = %q, want %q", got.Description, metadata.Description)
	}
	if len(got.Attributes) != len(metadata.Attributes) {
		t.Errorf("Attributes count = %d, want %d", len(got.Attributes), len(metadata.Attributes))
	}
}

func TestNFTMetadataService_Cache(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name": "Cached NFT"}`))
	}))
	defer server.Close()

	config := DefaultNFTMetadataServiceConfig()
	config.CacheTTL = time.Hour
	service := NewNFTMetadataService(config)

	ctx := context.Background()

	// First fetch
	_, err := service.FetchMetadata(ctx, server.URL)
	if err != nil {
		t.Fatalf("First FetchMetadata() error = %v", err)
	}
	if callCount != 1 {
		t.Errorf("Expected 1 server call, got %d", callCount)
	}

	// Second fetch (should use cache)
	_, err = service.FetchMetadata(ctx, server.URL)
	if err != nil {
		t.Fatalf("Second FetchMetadata() error = %v", err)
	}
	if callCount != 1 {
		t.Errorf("Expected 1 server call (cached), got %d", callCount)
	}

	// Check stats
	stats := service.Stats()
	if stats["cache_hits"] != 1 {
		t.Errorf("Expected 1 cache hit, got %d", stats["cache_hits"])
	}
	if stats["cache_misses"] != 1 {
		t.Errorf("Expected 1 cache miss, got %d", stats["cache_misses"])
	}
}

func TestNFTMetadataService_RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name": "Rate Limited NFT"}`))
	}))
	defer server.Close()

	config := DefaultNFTMetadataServiceConfig()
	config.RequestsPerSecond = 1.0
	config.BurstSize = 2
	config.CacheTTL = 0 // Disable cache
	service := NewNFTMetadataService(config)

	ctx := context.Background()

	// Exhaust burst capacity
	for i := 0; i < 2; i++ {
		// Clear cache between requests
		service.ClearCache()
		_, err := service.FetchMetadata(ctx, server.URL+"?id="+string(rune('0'+i)))
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
	}

	// Next request should be rate limited
	service.ClearCache()
	_, err := service.FetchMetadata(ctx, server.URL+"?id=2")
	if err == nil {
		t.Error("Expected rate limit error, got nil")
	}
}

func TestNFTMetadataService_DataURI(t *testing.T) {
	service := NewNFTMetadataService(nil)

	// Test URL-encoded data URI
	dataURI := "data:application/json,%7B%22name%22%3A%22Data%20NFT%22%7D"
	got, err := service.parseDataURI(dataURI)
	if err != nil {
		t.Fatalf("parseDataURI() error = %v", err)
	}
	if got.Name != "Data NFT" {
		t.Errorf("Name = %q, want %q", got.Name, "Data NFT")
	}

	// Test base64 data URI
	// {"name":"Base64 NFT"} in base64
	base64URI := "data:application/json;base64,eyJuYW1lIjoiQmFzZTY0IE5GVCJ9"
	got, err = service.parseDataURI(base64URI)
	if err != nil {
		t.Fatalf("parseDataURI() base64 error = %v", err)
	}
	if got.Name != "Base64 NFT" {
		t.Errorf("Name = %q, want %q", got.Name, "Base64 NFT")
	}
}

func TestDecodeBase64(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "simple",
			input:   "SGVsbG8gV29ybGQ=",
			want:    "Hello World",
			wantErr: false,
		},
		{
			name:    "no padding",
			input:   "SGVsbG8",
			want:    "Hello",
			wantErr: false,
		},
		{
			name:    "with whitespace",
			input:   "SGVs\nbG8g\nV29y\nbGQ=",
			want:    "Hello World",
			wantErr: false,
		},
		{
			name:    "JSON",
			input:   "eyJuYW1lIjoidGVzdCJ9",
			want:    `{"name":"test"}`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeBase64(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("decodeBase64() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if string(got) != tt.want {
				t.Errorf("decodeBase64() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestTrait_JSON(t *testing.T) {
	// Test trait marshaling
	trait := Trait{
		TraitType:   "Power",
		Value:       100,
		DisplayType: "number",
		MaxValue:    1000,
	}

	data, err := json.Marshal(trait)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got Trait
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got.TraitType != trait.TraitType {
		t.Errorf("TraitType = %q, want %q", got.TraitType, trait.TraitType)
	}
	if got.DisplayType != trait.DisplayType {
		t.Errorf("DisplayType = %q, want %q", got.DisplayType, trait.DisplayType)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultNFTMetadataServiceConfig()

	if config.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want %v", config.Timeout, 30*time.Second)
	}
	if config.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want %d", config.MaxRetries, 3)
	}
	if len(config.IPFSGateways) == 0 {
		t.Error("IPFSGateways should not be empty")
	}
	if config.ArweaveGateway == "" {
		t.Error("ArweaveGateway should not be empty")
	}
	if config.CacheTTL != time.Hour {
		t.Errorf("CacheTTL = %v, want %v", config.CacheTTL, time.Hour)
	}
}

func TestIsIPFSCID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG", true},
		{"bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi", true},
		{"bafkreigaknpexyvxt76zgkitavbwx6ejgfheup5oybpm77f3pxzrvwpfdi", true},
		{"https://example.com/image.png", false},
		{"random string", false},
		{"Qm", false}, // Too short
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isIPFSCID(tt.input)
			if got != tt.want {
				t.Errorf("isIPFSCID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
