// Package daemon is the chain-indexer bootstrap: it owns the per-chain
// indexers and the /v1/indexer/* HTTP API. The standalone indexerd binary
// (cmd/indexerd) is the only consumer in this repo. The unified
// luxfi/explorer binary imports it directly to compose indexer + graph +
// frontend into a single Go binary.
package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// ChainConfig defines a single chain to index.
type ChainConfig struct {
	Slug       string `yaml:"slug"`     // URL-safe identifier (e.g., "cchain")
	Name       string `yaml:"name"`     // Display name
	ChainID    int64  `yaml:"chain_id"` // EVM chain ID (0 for non-EVM)
	Type       string `yaml:"type"`     // evm, dag, linear, solana, bitcoin, cosmos
	RPC        string `yaml:"rpc"`      // RPC endpoint
	WS         string `yaml:"ws"`       // WebSocket endpoint (optional)
	CoinSymbol string `yaml:"coin"`     // Native coin symbol
	Enabled    bool   `yaml:"enabled"`  // Enable indexing
	Default    bool   `yaml:"default"`  // Default chain for /v1/indexer/* (no slug prefix)
}

// Config is the top-level chain-indexer configuration. Extra YAML fields
// (frontend brand, graph schemas, etc.) are accepted and ignored — the
// unified luxfi/explorer binary owns those concerns.
type Config struct {
	DataDir  string        `yaml:"data_dir"`
	HTTPAddr string        `yaml:"http_addr"`
	Chains   []ChainConfig `yaml:"chains"`
}

// EnabledChains returns only the chains with Enabled=true.
func (c Config) EnabledChains() []ChainConfig {
	out := make([]ChainConfig, 0, len(c.Chains))
	for _, ch := range c.Chains {
		if ch.Enabled {
			out = append(out, ch)
		}
	}
	return out
}

// LoadConfig reads, parses and validates a YAML config from disk.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	for i := range cfg.Chains {
		cfg.Chains[i].RPC = os.Expand(cfg.Chains[i].RPC, os.Getenv)
		cfg.Chains[i].WS = os.Expand(cfg.Chains[i].WS, os.Getenv)
		if err := validateSlug(cfg.Chains[i].Slug); err != nil {
			return Config{}, fmt.Errorf("chain[%d]: %w", i, err)
		}
	}
	return cfg, nil
}

// FindConfig returns the first existing path among a default search list,
// or "" if none are found. Callers may pass an explicit path to skip the search.
func FindConfig(dataDir string) string {
	for _, p := range []string{
		filepath.Join(dataDir, "chains.yaml"),
		"chains.yaml",
		"/etc/explorer/chains.yaml",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// slugPattern is the set of characters allowed in a chain slug. Slugs feed
// directly into filesystem paths (per-chain SQLite dir) and URL route prefixes
// (/v1/graph/{slug}/…). Restricting them to lowercase-alphanumeric + hyphen
// keeps both surfaces safe from path traversal and URL-encoding surprises.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func validateSlug(slug string) error {
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("invalid slug %q: must match ^[a-z0-9][a-z0-9-]{0,63}$", slug)
	}
	return nil
}

// EnvOr returns the value of an env var, or fallback if unset.
func EnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// HomeDir returns the current user's home directory, or "" if unknown.
func HomeDir() string {
	h, _ := os.UserHomeDir()
	return h
}
