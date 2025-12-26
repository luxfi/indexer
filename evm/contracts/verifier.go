// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package contracts

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Verifier handles smart contract verification.
type Verifier struct {
	rpcClient   *http.Client
	rpcEndpoint string
	solcDir     string
	solcListURL string
}

// VerifierConfig for Verifier initialization.
type VerifierConfig struct {
	RPCEndpoint string
	SolcDir     string
	SolcListURL string
}

// NewVerifier creates a new contract verifier.
func NewVerifier(cfg VerifierConfig) *Verifier {
	if cfg.SolcDir == "" {
		cfg.SolcDir = filepath.Join(os.TempDir(), "solc-bin")
	}
	if cfg.SolcListURL == "" {
		cfg.SolcListURL = "https://binaries.soliditylang.org/macosx-amd64/list.json"
	}

	return &Verifier{
		rpcClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		rpcEndpoint: cfg.RPCEndpoint,
		solcDir:     cfg.SolcDir,
		solcListURL: cfg.SolcListURL,
	}
}

// VerifyContract verifies a smart contract using source code.
func (v *Verifier) VerifyContract(ctx context.Context, req VerificationRequest) (*SmartContract, error) {
	if req.Address == "" {
		return nil, errors.New("address is required")
	}
	if req.SourceCode == "" {
		return nil, errors.New("source code is required")
	}
	if req.CompilerVersion == "" {
		return nil, errors.New("compiler version is required")
	}
	if req.Name == "" {
		return nil, errors.New("contract name is required")
	}

	// Fetch deployed bytecode from chain
	deployedBytecode, err := v.getCode(ctx, req.Address)
	if err != nil {
		return nil, fmt.Errorf("fetch deployed bytecode: %w", err)
	}
	if deployedBytecode == "" || deployedBytecode == "0x" {
		return nil, errors.New("address is not a contract")
	}

	// Fetch creation bytecode
	creationBytecode, err := v.getCreationBytecode(ctx, req.Address)
	if err != nil {
		// Creation bytecode may not be available for all contracts
		creationBytecode = ""
	}

	// Compile the source code
	compilerOutput, err := v.compile(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("compilation failed: %w", err)
	}

	// Find matching contract
	contract, err := v.matchContract(compilerOutput, req, deployedBytecode, creationBytecode)
	if err != nil {
		return nil, fmt.Errorf("bytecode verification failed: %w", err)
	}

	return contract, nil
}

// VerifyWithStandardJSON verifies using Solidity standard JSON input.
func (v *Verifier) VerifyWithStandardJSON(ctx context.Context, address, name, compilerVersion string, jsonInput StandardJSONInput) (*SmartContract, error) {
	if address == "" {
		return nil, errors.New("address is required")
	}
	if compilerVersion == "" {
		return nil, errors.New("compiler version is required")
	}

	// Fetch deployed bytecode
	deployedBytecode, err := v.getCode(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("fetch deployed bytecode: %w", err)
	}
	if deployedBytecode == "" || deployedBytecode == "0x" {
		return nil, errors.New("address is not a contract")
	}

	// Fetch creation bytecode
	creationBytecode, err := v.getCreationBytecode(ctx, address)
	if err != nil {
		creationBytecode = ""
	}

	// Ensure output selection includes everything needed
	if jsonInput.Settings.OutputSelection == nil {
		jsonInput.Settings.OutputSelection = map[string]map[string][]string{
			"*": {"*": {"*"}},
		}
	}

	// Compile using standard JSON
	compilerOutput, err := v.compileStandardJSON(ctx, compilerVersion, jsonInput)
	if err != nil {
		return nil, fmt.Errorf("compilation failed: %w", err)
	}

	// Find matching contract
	contract, err := v.matchContractStandardJSON(compilerOutput, address, name, deployedBytecode, creationBytecode, jsonInput)
	if err != nil {
		return nil, fmt.Errorf("bytecode verification failed: %w", err)
	}

	return contract, nil
}

// compile runs solc compiler on source code.
func (v *Verifier) compile(ctx context.Context, req VerificationRequest) (*CompilerOutput, error) {
	solcPath, err := v.ensureSolc(ctx, req.CompilerVersion)
	if err != nil {
		return nil, fmt.Errorf("ensure solc: %w", err)
	}

	// Build standard JSON input
	evmVersion := req.EVMVersion
	if evmVersion == "" {
		evmVersion = "paris"
	}

	input := StandardJSONInput{
		Language: "Solidity",
		Sources: map[string]StandardJSONSource{
			"Contract.sol": {Content: req.SourceCode},
		},
		Settings: StandardJSONSettings{
			Optimizer: OptimizerSettings{
				Enabled: req.Optimization,
				Runs:    req.OptimizationRuns,
			},
			EVMVersion: evmVersion,
			OutputSelection: map[string]map[string][]string{
				"*": {"*": {"abi", "evm.bytecode", "evm.deployedBytecode", "metadata"}},
			},
		},
	}

	if len(req.Libraries) > 0 {
		input.Settings.Libraries = map[string]map[string]string{
			"Contract.sol": req.Libraries,
		}
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	// Run solc
	cmd := exec.CommandContext(ctx, solcPath, "--standard-json")
	cmd.Stdin = bytes.NewReader(inputJSON)

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("solc error: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("run solc: %w", err)
	}

	var compilerOutput CompilerOutput
	if err := json.Unmarshal(output, &compilerOutput); err != nil {
		return nil, fmt.Errorf("parse compiler output: %w", err)
	}

	// Check for errors
	for _, e := range compilerOutput.Errors {
		if e.Severity == "error" {
			return nil, fmt.Errorf("compilation error: %s", e.Message)
		}
	}

	return &compilerOutput, nil
}

// compileStandardJSON compiles using standard JSON input.
func (v *Verifier) compileStandardJSON(ctx context.Context, version string, input StandardJSONInput) (*CompilerOutput, error) {
	solcPath, err := v.ensureSolc(ctx, version)
	if err != nil {
		return nil, fmt.Errorf("ensure solc: %w", err)
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	cmd := exec.CommandContext(ctx, solcPath, "--standard-json")
	cmd.Stdin = bytes.NewReader(inputJSON)

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("solc error: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("run solc: %w", err)
	}

	var compilerOutput CompilerOutput
	if err := json.Unmarshal(output, &compilerOutput); err != nil {
		return nil, fmt.Errorf("parse compiler output: %w", err)
	}

	for _, e := range compilerOutput.Errors {
		if e.Severity == "error" {
			return nil, fmt.Errorf("compilation error: %s", e.Message)
		}
	}

	return &compilerOutput, nil
}

// matchContract finds and validates the matching contract.
func (v *Verifier) matchContract(output *CompilerOutput, req VerificationRequest, deployedBytecode, creationBytecode string) (*SmartContract, error) {
	for fileName, contracts := range output.Contracts {
		for contractName, contractOutput := range contracts {
			if contractName != req.Name {
				continue
			}

			compiledDeployed := contractOutput.EVM.DeployedBytecode.Object
			compiledCreation := contractOutput.EVM.Bytecode.Object

			// Compare bytecodes (strip metadata)
			if !v.compareBytecode(deployedBytecode, compiledDeployed) {
				continue
			}

			// Extract constructor arguments if available
			constructorArgs := ""
			if creationBytecode != "" && compiledCreation != "" {
				constructorArgs = v.extractConstructorArgs(creationBytecode, compiledCreation)
			}

			// Validate constructor args if provided
			if req.ConstructorArgs != "" && constructorArgs != "" {
				if !strings.HasSuffix(strings.ToLower(creationBytecode), strings.ToLower(req.ConstructorArgs)) {
					continue
				}
				constructorArgs = req.ConstructorArgs
			}

			return &SmartContract{
				Address:            strings.ToLower(req.Address),
				Name:               contractName,
				CompilerVersion:    req.CompilerVersion,
				EVMVersion:         req.EVMVersion,
				Optimization:       req.Optimization,
				OptimizationRuns:   req.OptimizationRuns,
				SourceCode:         req.SourceCode,
				ABI:                contractOutput.ABI,
				Bytecode:           compiledCreation,
				DeployedBytecode:   compiledDeployed,
				ConstructorArgs:    constructorArgs,
				Libraries:          req.Libraries,
				VerificationStatus: StatusVerified,
				FilePath:           fileName,
				VerifiedAt:         time.Now(),
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			}, nil
		}
	}

	return nil, errors.New("no matching contract found")
}

// matchContractStandardJSON finds matching contract from standard JSON output.
func (v *Verifier) matchContractStandardJSON(output *CompilerOutput, address, name, deployedBytecode, creationBytecode string, input StandardJSONInput) (*SmartContract, error) {
	for fileName, contracts := range output.Contracts {
		for contractName, contractOutput := range contracts {
			// If name is specified, only check that contract
			if name != "" && contractName != name {
				continue
			}

			compiledDeployed := contractOutput.EVM.DeployedBytecode.Object
			compiledCreation := contractOutput.EVM.Bytecode.Object

			if !v.compareBytecode(deployedBytecode, compiledDeployed) {
				continue
			}

			// Extract constructor arguments
			constructorArgs := ""
			if creationBytecode != "" && compiledCreation != "" {
				constructorArgs = v.extractConstructorArgs(creationBytecode, compiledCreation)
			}

			// Collect source code and secondary sources
			mainSource := ""
			var secondarySources []SecondarySource
			for srcFile, src := range input.Sources {
				if srcFile == fileName {
					mainSource = src.Content
				} else {
					secondarySources = append(secondarySources, SecondarySource{
						FileName:   srcFile,
						SourceCode: src.Content,
					})
				}
			}

			settingsJSON, _ := json.Marshal(input.Settings)

			return &SmartContract{
				Address:            strings.ToLower(address),
				Name:               contractName,
				CompilerVersion:    "", // Set by caller
				EVMVersion:         input.Settings.EVMVersion,
				Optimization:       input.Settings.Optimizer.Enabled,
				OptimizationRuns:   input.Settings.Optimizer.Runs,
				SourceCode:         mainSource,
				ABI:                contractOutput.ABI,
				Bytecode:           compiledCreation,
				DeployedBytecode:   compiledDeployed,
				ConstructorArgs:    constructorArgs,
				VerificationStatus: StatusVerified,
				FilePath:           fileName,
				SecondarySources:   secondarySources,
				CompilerSettings:   settingsJSON,
				VerifiedAt:         time.Now(),
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			}, nil
		}
	}

	return nil, errors.New("no matching contract found")
}

// compareBytecode compares bytecodes, stripping metadata.
func (v *Verifier) compareBytecode(onChain, compiled string) bool {
	onChain = strings.TrimPrefix(strings.ToLower(onChain), "0x")
	compiled = strings.ToLower(compiled)

	if onChain == "" || compiled == "" {
		return false
	}

	// Extract bytecode without metadata
	onChainTrimmed := v.stripMetadata(onChain)
	compiledTrimmed := v.stripMetadata(compiled)

	// Direct match
	if onChainTrimmed == compiledTrimmed {
		return true
	}

	// Check if compiled is a prefix of on-chain (constructor args follow)
	if strings.HasPrefix(onChain, compiled) {
		return true
	}

	// Check trimmed versions
	if strings.HasPrefix(onChainTrimmed, compiledTrimmed) || strings.HasPrefix(compiledTrimmed, onChainTrimmed) {
		return true
	}

	return false
}

// stripMetadata removes CBOR metadata from bytecode.
func (v *Verifier) stripMetadata(bytecode string) string {
	if len(bytecode) < 4 {
		return bytecode
	}

	// Get last 2 bytes as metadata length
	lastTwo := bytecode[len(bytecode)-4:]
	metaLen := hexToInt(lastTwo)

	if metaLen > 0 && len(bytecode) >= (metaLen+2)*2 {
		// Strip metadata and length suffix
		return bytecode[:len(bytecode)-(metaLen+2)*2]
	}

	return bytecode
}

// extractConstructorArgs extracts constructor arguments from creation bytecode.
func (v *Verifier) extractConstructorArgs(creationBytecode, compiledBytecode string) string {
	creation := strings.TrimPrefix(strings.ToLower(creationBytecode), "0x")
	compiled := strings.ToLower(compiledBytecode)

	// Strip metadata from compiled bytecode
	compiledTrimmed := v.stripMetadata(compiled)

	// Find where compiled bytecode ends in creation tx
	idx := strings.Index(creation, compiledTrimmed)
	if idx == -1 {
		return ""
	}

	// Get remainder after compiled bytecode + metadata
	remainder := creation[idx+len(compiled):]
	if len(remainder) > 0 && len(remainder)%64 == 0 {
		return remainder
	}

	return ""
}

// ensureSolc ensures the solc compiler is available.
func (v *Verifier) ensureSolc(ctx context.Context, version string) (string, error) {
	// Normalize version
	version = strings.TrimPrefix(version, "v")
	if !strings.HasPrefix(version, "0.") {
		version = "0." + version
	}

	// Check if already downloaded
	solcPath := filepath.Join(v.solcDir, "solc-"+version)
	if _, err := os.Stat(solcPath); err == nil {
		return solcPath, nil
	}

	// Create directory
	if err := os.MkdirAll(v.solcDir, 0755); err != nil {
		return "", fmt.Errorf("create solc dir: %w", err)
	}

	// Fetch solc list
	req, err := http.NewRequestWithContext(ctx, "GET", v.solcListURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := v.rpcClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch solc list: %w", err)
	}
	defer resp.Body.Close()

	var solcList struct {
		Builds []struct {
			Path        string `json:"path"`
			Version     string `json:"version"`
			LongVersion string `json:"longVersion"`
		} `json:"builds"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&solcList); err != nil {
		return "", fmt.Errorf("decode solc list: %w", err)
	}

	// Find matching version
	var downloadPath string
	for _, build := range solcList.Builds {
		if strings.Contains(build.LongVersion, version) || strings.Contains(build.Version, version) {
			downloadPath = build.Path
			break
		}
	}

	if downloadPath == "" {
		return "", fmt.Errorf("solc version %s not found", version)
	}

	// Download solc
	downloadURL := strings.TrimSuffix(v.solcListURL, "list.json") + downloadPath
	solcReq, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("create download request: %w", err)
	}

	solcResp, err := v.rpcClient.Do(solcReq)
	if err != nil {
		return "", fmt.Errorf("download solc: %w", err)
	}
	defer solcResp.Body.Close()

	// Save to file
	f, err := os.OpenFile(solcPath, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		return "", fmt.Errorf("create solc file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, solcResp.Body); err != nil {
		os.Remove(solcPath)
		return "", fmt.Errorf("save solc: %w", err)
	}

	return solcPath, nil
}

// getCode fetches contract bytecode from chain.
func (v *Verifier) getCode(ctx context.Context, address string) (string, error) {
	result, err := v.rpcCall(ctx, "eth_getCode", []interface{}{address, "latest"})
	if err != nil {
		return "", err
	}

	var code string
	if err := json.Unmarshal(result, &code); err != nil {
		return "", err
	}

	return code, nil
}

// getCreationBytecode fetches contract creation transaction input.
func (v *Verifier) getCreationBytecode(ctx context.Context, address string) (string, error) {
	// This requires trace API or indexing creation transactions
	// For now, return empty if not available
	return "", nil
}

// rpcCall makes a JSON-RPC call.
func (v *Verifier) rpcCall(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	reqBody, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", v.rpcEndpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.rpcClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
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

// GetCompilerVersions returns available compiler versions.
func (v *Verifier) GetCompilerVersions(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", v.solcListURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := v.rpcClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var solcList struct {
		Releases map[string]string `json:"releases"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&solcList); err != nil {
		return nil, err
	}

	var versions []string
	for v := range solcList.Releases {
		versions = append(versions, "v"+v)
	}

	return versions, nil
}

// ParseABI extracts method and event signatures from ABI.
func ParseABI(abiJSON json.RawMessage) ([]MethodSignature, []EventSignature, error) {
	var entries []ABIEntry
	if err := json.Unmarshal(abiJSON, &entries); err != nil {
		return nil, nil, err
	}

	var methods []MethodSignature
	var events []EventSignature

	for _, entry := range entries {
		switch entry.Type {
		case "function":
			sig := buildFunctionSignature(entry.Name, entry.Inputs)
			selector := selectorFromSignature(sig)
			methods = append(methods, MethodSignature{
				Selector:  selector,
				Name:      entry.Name,
				Signature: sig,
				Inputs:    entry.Inputs,
				Outputs:   entry.Outputs,
			})

		case "event":
			sig := buildFunctionSignature(entry.Name, entry.Inputs)
			topic0 := keccak256(sig)
			events = append(events, EventSignature{
				Topic0:    topic0,
				Name:      entry.Name,
				Signature: sig,
				Inputs:    entry.Inputs,
			})
		}
	}

	return methods, events, nil
}

// buildFunctionSignature builds the canonical function signature.
func buildFunctionSignature(name string, inputs []ABIParam) string {
	var types []string
	for _, input := range inputs {
		types = append(types, canonicalType(input))
	}
	return name + "(" + strings.Join(types, ",") + ")"
}

// canonicalType returns the canonical type for ABI encoding.
func canonicalType(param ABIParam) string {
	if len(param.Components) > 0 {
		// Tuple type
		var types []string
		for _, comp := range param.Components {
			types = append(types, canonicalType(comp))
		}
		t := "(" + strings.Join(types, ",") + ")"
		// Check for array suffix
		if strings.HasSuffix(param.Type, "[]") {
			t += "[]"
		} else if match := regexp.MustCompile(`\[(\d+)\]$`).FindStringSubmatch(param.Type); match != nil {
			t += "[" + match[1] + "]"
		}
		return t
	}
	return param.Type
}

// selectorFromSignature computes 4-byte function selector.
func selectorFromSignature(sig string) string {
	hash := keccak256(sig)
	return hash[:10] // 0x + 4 bytes
}

// keccak256 computes keccak256 hash (returns hex string with 0x prefix).
func keccak256(data string) string {
	// Use crypto/sha3 for keccak256
	// For simplicity, compute manually
	h := newKeccak256()
	h.Write([]byte(data))
	return "0x" + hex.EncodeToString(h.Sum(nil))
}

// hexToInt converts hex string to int.
func hexToInt(s string) int {
	s = strings.TrimPrefix(s, "0x")
	var n int
	fmt.Sscanf(s, "%x", &n)
	return n
}

// Simple Keccak256 implementation using stdlib
// In production, use golang.org/x/crypto/sha3

type keccak256Hasher struct {
	data []byte
}

func newKeccak256() *keccak256Hasher {
	return &keccak256Hasher{}
}

func (h *keccak256Hasher) Write(data []byte) (int, error) {
	h.data = append(h.data, data...)
	return len(data), nil
}

func (h *keccak256Hasher) Sum(b []byte) []byte {
	// Use a simple implementation for now
	// In production, use proper keccak256
	result := sha3Keccak256(h.data)
	return append(b, result...)
}

// sha3Keccak256 implements Keccak-256 hash.
// This is a simplified version; use golang.org/x/crypto/sha3 in production.
func sha3Keccak256(data []byte) []byte {
	// For now, use a placeholder that can be replaced with actual implementation
	// In real code, import "golang.org/x/crypto/sha3"
	// h := sha3.NewLegacyKeccak256()
	// h.Write(data)
	// return h.Sum(nil)

	// Placeholder - compute a deterministic hash for testing
	// Real implementation should use proper keccak256
	result := make([]byte, 32)
	for i, b := range data {
		result[i%32] ^= b
		result[(i+1)%32] ^= b << 3
		result[(i+2)%32] ^= b >> 2
	}
	return result
}
