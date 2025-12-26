// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package e2e provides comprehensive end-to-end tests for the Lux indexer
// running against luxd in dev mode with K=1 consensus.
package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Lux Indexer E2E Suite")
}

// NodeConfig holds luxd configuration for E2E tests
type NodeConfig struct {
	NetworkID        string
	HTTPPort         int
	StakingPort      int
	DataDir          string
	Mnemonic         string
	LogLevel         string
	PluginDir        string
	BootstrapTimeout time.Duration
	NodeReadyTimeout time.Duration
}

// DefaultNodeConfig returns configuration for dev mode testing
func DefaultNodeConfig() *NodeConfig {
	dataDir := os.Getenv("LUX_E2E_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(os.TempDir(), "lux-e2e", fmt.Sprintf("run_%d", time.Now().UnixNano()))
	}

	mnemonic := os.Getenv("LUX_MNEMONIC")
	if mnemonic == "" {
		// Default test mnemonic (DO NOT USE IN PRODUCTION)
		mnemonic = "test test test test test test test test test test test junk"
	}

	return &NodeConfig{
		NetworkID:        "1337", // Local dev network ID
		HTTPPort:         19650,  // Avoid conflict with running nodes
		StakingPort:      19651,
		DataDir:          dataDir,
		Mnemonic:         mnemonic,
		LogLevel:         "info",
		PluginDir:        os.Getenv("LUX_PLUGIN_DIR"),
		BootstrapTimeout: 60 * time.Second,
		NodeReadyTimeout: 120 * time.Second,
	}
}

// NodeProcess manages a luxd process for testing
type NodeProcess struct {
	Config  *NodeConfig
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	running bool
}

// Global node process for the test suite
var nodeProcess *NodeProcess
var nodeConfig *NodeConfig

var _ = BeforeSuite(func() {
	// Skip if LUX_E2E_SKIP_NODE is set (use external node)
	if os.Getenv("LUX_E2E_SKIP_NODE") != "" {
		GinkgoWriter.Println("Using external node, skipping luxd start")
		return
	}

	nodeConfig = DefaultNodeConfig()

	// Ensure data directory exists
	err := os.MkdirAll(nodeConfig.DataDir, 0755)
	Expect(err).NotTo(HaveOccurred())

	// Check if luxd is available
	luxdPath := findLuxd()
	if luxdPath == "" {
		Skip("luxd not found in PATH or LUX_NODE_PATH")
	}

	GinkgoWriter.Printf("Starting luxd in dev mode at %s\n", luxdPath)
	GinkgoWriter.Printf("Data directory: %s\n", nodeConfig.DataDir)

	// Start luxd in dev mode
	ctx, cancel := context.WithCancel(context.Background())

	args := []string{
		"--dev", // Single-node K=1 consensus (implies --network-id=1337)
		fmt.Sprintf("--http-port=%d", nodeConfig.HTTPPort),
		fmt.Sprintf("--staking-port=%d", nodeConfig.StakingPort),
		fmt.Sprintf("--db-dir=%s", filepath.Join(nodeConfig.DataDir, "db")),
		fmt.Sprintf("--log-dir=%s", filepath.Join(nodeConfig.DataDir, "logs")),
		"--log-level=info",
		"--index-enabled=true",
	}

	if nodeConfig.PluginDir != "" {
		args = append(args, fmt.Sprintf("--plugin-dir=%s", nodeConfig.PluginDir))
	}

	cmd := exec.CommandContext(ctx, luxdPath, args...)
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("LUX_MNEMONIC=%s", nodeConfig.Mnemonic),
	)

	err = cmd.Start()
	Expect(err).NotTo(HaveOccurred())

	nodeProcess = &NodeProcess{
		Config:  nodeConfig,
		cmd:     cmd,
		cancel:  cancel,
		running: true,
	}

	// Wait for node to be ready
	GinkgoWriter.Println("Waiting for node to be ready...")
	waitForNode(nodeConfig.HTTPPort, nodeConfig.NodeReadyTimeout)
	GinkgoWriter.Println("Node is ready!")
})

var _ = AfterSuite(func() {
	if nodeProcess != nil && nodeProcess.running {
		GinkgoWriter.Println("Stopping luxd...")
		nodeProcess.cancel()

		// Wait for graceful shutdown
		done := make(chan error)
		go func() {
			done <- nodeProcess.cmd.Wait()
		}()

		select {
		case <-done:
			GinkgoWriter.Println("luxd stopped gracefully")
		case <-time.After(30 * time.Second):
			GinkgoWriter.Println("Forcing luxd termination...")
			nodeProcess.cmd.Process.Kill()
		}

		nodeProcess.running = false
	}

	// Cleanup data directory if LUX_E2E_KEEP_DATA is not set
	if os.Getenv("LUX_E2E_KEEP_DATA") == "" && nodeConfig != nil {
		os.RemoveAll(nodeConfig.DataDir)
	}
})

// findLuxd looks for luxd binary
func findLuxd() string {
	// Check environment variable first
	if path := os.Getenv("LUX_NODE_PATH"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Check common locations
	locations := []string{
		"luxd",
		"/usr/local/bin/luxd",
		filepath.Join(os.Getenv("HOME"), "go/bin/luxd"),
		filepath.Join(os.Getenv("HOME"), "work/lux/node/build/luxd"),
		filepath.Join(os.Getenv("GOPATH"), "bin/luxd"),
	}

	for _, loc := range locations {
		if path, err := exec.LookPath(loc); err == nil {
			return path
		}
	}

	return ""
}

// waitForNode waits for the node to be ready
func waitForNode(port int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	// Info API is at /ext/info
	client := NewRPCClient(fmt.Sprintf("http://127.0.0.1:%d/ext/info", port))

	for time.Now().Before(deadline) {
		// Check bootstrap status via info API
		_, err := client.CallInfo("info.isBootstrapped", map[string]string{"chain": "P"})
		if err == nil {
			return
		}

		time.Sleep(2 * time.Second)
	}

	Fail(fmt.Sprintf("Node did not become ready within %v", timeout))
}

// GetRPCEndpoint returns the RPC endpoint for a chain
func GetRPCEndpoint(chain string) string {
	// Check for external node via LUX_RPC_URL
	if baseURL := os.Getenv("LUX_RPC_URL"); baseURL != "" {
		// Extract host:port from base URL
		// e.g., http://127.0.0.1:9650/ext/bc/C/rpc -> http://127.0.0.1:9650
		parts := strings.Split(baseURL, "/ext/")
		host := parts[0]
		switch strings.ToUpper(chain) {
		case "C":
			return host + "/ext/bc/C/rpc"
		case "P":
			return host + "/ext/bc/P"
		case "X":
			return host + "/ext/bc/X"
		default:
			return host + "/ext/bc/" + chain + "/rpc"
		}
	}

	port := 9650 // Default
	if nodeConfig != nil {
		port = nodeConfig.HTTPPort
	}

	switch strings.ToUpper(chain) {
	case "C":
		return fmt.Sprintf("http://127.0.0.1:%d/ext/bc/C/rpc", port)
	case "P":
		return fmt.Sprintf("http://127.0.0.1:%d/ext/bc/P", port)
	case "X":
		return fmt.Sprintf("http://127.0.0.1:%d/ext/bc/X", port)
	default:
		return fmt.Sprintf("http://127.0.0.1:%d/ext/bc/%s/rpc", port, chain)
	}
}

// GetInfoEndpoint returns the info API endpoint
func GetInfoEndpoint() string {
	if baseURL := os.Getenv("LUX_RPC_URL"); baseURL != "" {
		parts := strings.Split(baseURL, "/ext/")
		return parts[0] + "/ext/info"
	}
	port := 9650
	if nodeConfig != nil {
		port = nodeConfig.HTTPPort
	}
	return fmt.Sprintf("http://127.0.0.1:%d/ext/info", port)
}

// GetAdminEndpoint returns the admin API endpoint
func GetAdminEndpoint() string {
	if baseURL := os.Getenv("LUX_RPC_URL"); baseURL != "" {
		parts := strings.Split(baseURL, "/ext/")
		return parts[0] + "/ext/admin"
	}
	port := 9650
	if nodeConfig != nil {
		port = nodeConfig.HTTPPort
	}
	return fmt.Sprintf("http://127.0.0.1:%d/ext/admin", port)
}
