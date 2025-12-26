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
	"net/http"
	"strings"
	"time"
)

// ProxyDetector detects proxy contracts and resolves implementations.
type ProxyDetector struct {
	rpcClient   *http.Client
	rpcEndpoint string
}

// NewProxyDetector creates a new proxy detector.
func NewProxyDetector(rpcEndpoint string) *ProxyDetector {
	return &ProxyDetector{
		rpcClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		rpcEndpoint: rpcEndpoint,
	}
}

// DetectProxy detects if an address is a proxy and returns implementation info.
func (d *ProxyDetector) DetectProxy(ctx context.Context, address string) (*ProxyInfo, error) {
	if address == "" {
		return nil, errors.New("address is required")
	}

	// Get contract bytecode
	bytecode, err := d.getCode(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("get code: %w", err)
	}

	if bytecode == "" || bytecode == "0x" {
		return &ProxyInfo{IsProxy: false}, nil
	}

	// Try bytecode-matching proxies first (EIP-1167, Safe)
	if info := d.detectEIP1167(bytecode); info != nil {
		return info, nil
	}

	if info, err := d.detectMasterCopy(ctx, address, bytecode); err == nil && info != nil {
		return info, nil
	}

	// Try storage-slot based proxies
	if info, err := d.detectEIP1967(ctx, address); err == nil && info != nil && info.IsProxy {
		return info, nil
	}

	if info, err := d.detectEIP1967Beacon(ctx, address); err == nil && info != nil && info.IsProxy {
		return info, nil
	}

	if info, err := d.detectEIP1967OpenZeppelin(ctx, address); err == nil && info != nil && info.IsProxy {
		return info, nil
	}

	if info, err := d.detectEIP1822(ctx, address); err == nil && info != nil && info.IsProxy {
		return info, nil
	}

	// Try basic implementation getter
	if info, err := d.detectBasicImplementation(ctx, address); err == nil && info != nil && info.IsProxy {
		return info, nil
	}

	return &ProxyInfo{IsProxy: false}, nil
}

// detectEIP1167 detects EIP-1167 minimal proxy by bytecode pattern.
func (d *ProxyDetector) detectEIP1167(bytecode string) *ProxyInfo {
	bc := strings.TrimPrefix(strings.ToLower(bytecode), "0x")
	bcBytes, err := hex.DecodeString(bc)
	if err != nil {
		return nil
	}

	// Standard EIP-1167: 363d3d373d3d3d363d73<20-byte-addr>5af43d82803e903d91602b57fd5bf3
	if len(bcBytes) == 45 {
		if bytes.HasPrefix(bcBytes, EIP1167Prefix) && bytes.HasSuffix(bcBytes, EIP1167Suffix) {
			addrBytes := bcBytes[10:30]
			implAddr := "0x" + hex.EncodeToString(addrBytes)
			return &ProxyInfo{
				IsProxy:               true,
				ProxyType:             ProxyEIP1167,
				ImplementationAddress: implAddr,
			}
		}
	}

	// Variant: 3d3d3d3d363d3d37363d73<20-byte-addr>5af43d3d93803e602a57fd5bf3
	if len(bcBytes) == 44 {
		if bytes.HasPrefix(bcBytes, EIP1167VariantPrefix) && bytes.HasSuffix(bcBytes, EIP1167VariantSuffix) {
			addrBytes := bcBytes[11:31]
			implAddr := "0x" + hex.EncodeToString(addrBytes)
			return &ProxyInfo{
				IsProxy:               true,
				ProxyType:             ProxyEIP1167,
				ImplementationAddress: implAddr,
			}
		}
	}

	return nil
}

// detectMasterCopy detects Gnosis Safe proxy pattern.
func (d *ProxyDetector) detectMasterCopy(ctx context.Context, address, bytecode string) (*ProxyInfo, error) {
	bc := strings.TrimPrefix(strings.ToLower(bytecode), "0x")
	bcBytes, err := hex.DecodeString(bc)
	if err != nil {
		return nil, nil
	}

	// Check if bytecode matches known Safe proxy patterns
	if !d.isSafeProxy(bcBytes) {
		return nil, nil
	}

	// Read implementation from slot 0
	implAddr, err := d.getStorageAt(ctx, address, StorageSlotMasterCopy)
	if err != nil {
		return nil, err
	}

	implAddr = extractAddress(implAddr)
	if implAddr == "" || isZeroAddress(implAddr) {
		return nil, nil
	}

	return &ProxyInfo{
		IsProxy:               true,
		ProxyType:             ProxyMasterCopy,
		ImplementationAddress: implAddr,
	}, nil
}

// isSafeProxy checks if bytecode matches known Safe proxy patterns.
func (d *ProxyDetector) isSafeProxy(bytecode []byte) bool {
	// Safe Proxy v1.0.0: 110 bytes
	// Safe Proxy v1.1.1: 170 bytes
	// Safe Proxy v1.3.0: 171 bytes
	// Safe Proxy v1.4.1: 171 bytes
	// Safe Proxy v1.5.0: 123 bytes

	safeSignatures := [][]byte{
		// v1.0.0 prefix
		{0x60, 0x80, 0x60, 0x40, 0x52, 0x73},
		// Common Safe pattern: starts with PUSH1 0x80 PUSH1 0x40 MSTORE
		{0x60, 0x80, 0x60, 0x40, 0x52},
	}

	// Check for a619486e selector (masterCopy())
	masterCopySelector := []byte{0xa6, 0x19, 0x48, 0x6e}

	for _, sig := range safeSignatures {
		if bytes.HasPrefix(bytecode, sig) && bytes.Contains(bytecode, masterCopySelector) {
			return true
		}
	}

	return false
}

// detectEIP1967 detects EIP-1967 transparent proxy.
func (d *ProxyDetector) detectEIP1967(ctx context.Context, address string) (*ProxyInfo, error) {
	implAddr, err := d.getStorageAt(ctx, address, StorageSlotEIP1967Implementation)
	if err != nil {
		return nil, err
	}

	implAddr = extractAddress(implAddr)
	if implAddr == "" || isZeroAddress(implAddr) {
		return &ProxyInfo{IsProxy: false}, nil
	}

	return &ProxyInfo{
		IsProxy:               true,
		ProxyType:             ProxyEIP1967,
		ImplementationAddress: implAddr,
	}, nil
}

// detectEIP1967Beacon detects EIP-1967 beacon proxy.
func (d *ProxyDetector) detectEIP1967Beacon(ctx context.Context, address string) (*ProxyInfo, error) {
	beaconAddr, err := d.getStorageAt(ctx, address, StorageSlotEIP1967Beacon)
	if err != nil {
		return nil, err
	}

	beaconAddr = extractAddress(beaconAddr)
	if beaconAddr == "" || isZeroAddress(beaconAddr) {
		return &ProxyInfo{IsProxy: false}, nil
	}

	// Call implementation() on the beacon
	implAddr, err := d.callContract(ctx, beaconAddr, SelectorImplementation)
	if err != nil {
		// Beacon exists but implementation() failed
		return &ProxyInfo{
			IsProxy:       true,
			ProxyType:     ProxyEIP1967Beacon,
			BeaconAddress: beaconAddr,
		}, nil
	}

	implAddr = extractAddress(implAddr)
	if implAddr == "" || isZeroAddress(implAddr) {
		return &ProxyInfo{
			IsProxy:       true,
			ProxyType:     ProxyEIP1967Beacon,
			BeaconAddress: beaconAddr,
		}, nil
	}

	return &ProxyInfo{
		IsProxy:               true,
		ProxyType:             ProxyEIP1967Beacon,
		ImplementationAddress: implAddr,
		BeaconAddress:         beaconAddr,
	}, nil
}

// detectEIP1967OpenZeppelin detects OpenZeppelin legacy proxy.
func (d *ProxyDetector) detectEIP1967OpenZeppelin(ctx context.Context, address string) (*ProxyInfo, error) {
	implAddr, err := d.getStorageAt(ctx, address, StorageSlotOpenZeppelinLegacy)
	if err != nil {
		return nil, err
	}

	implAddr = extractAddress(implAddr)
	if implAddr == "" || isZeroAddress(implAddr) {
		return &ProxyInfo{IsProxy: false}, nil
	}

	return &ProxyInfo{
		IsProxy:               true,
		ProxyType:             ProxyEIP1967OpenZeppelin,
		ImplementationAddress: implAddr,
	}, nil
}

// detectEIP1822 detects UUPS proxy.
func (d *ProxyDetector) detectEIP1822(ctx context.Context, address string) (*ProxyInfo, error) {
	implAddr, err := d.getStorageAt(ctx, address, StorageSlotEIP1822)
	if err != nil {
		return nil, err
	}

	implAddr = extractAddress(implAddr)
	if implAddr == "" || isZeroAddress(implAddr) {
		return &ProxyInfo{IsProxy: false}, nil
	}

	return &ProxyInfo{
		IsProxy:               true,
		ProxyType:             ProxyEIP1822,
		ImplementationAddress: implAddr,
	}, nil
}

// detectBasicImplementation tries calling implementation() function.
func (d *ProxyDetector) detectBasicImplementation(ctx context.Context, address string) (*ProxyInfo, error) {
	implAddr, err := d.callContract(ctx, address, SelectorImplementation)
	if err != nil {
		return &ProxyInfo{IsProxy: false}, nil
	}

	implAddr = extractAddress(implAddr)
	if implAddr == "" || isZeroAddress(implAddr) {
		return &ProxyInfo{IsProxy: false}, nil
	}

	return &ProxyInfo{
		IsProxy:               true,
		ProxyType:             ProxyBasicImplementation,
		ImplementationAddress: implAddr,
	}, nil
}

// GetImplementationABI fetches the ABI from the implementation contract.
func (d *ProxyDetector) GetImplementationABI(ctx context.Context, proxyInfo *ProxyInfo, store ContractStore) (json.RawMessage, error) {
	if proxyInfo == nil || !proxyInfo.IsProxy || proxyInfo.ImplementationAddress == "" {
		return nil, nil
	}

	// Look up the implementation contract
	impl, err := store.GetContract(ctx, proxyInfo.ImplementationAddress)
	if err != nil {
		return nil, err
	}

	if impl == nil {
		return nil, nil
	}

	return impl.ABI, nil
}

// CombineABIs combines proxy and implementation ABIs.
func CombineABIs(proxyABI, implABI json.RawMessage) (json.RawMessage, error) {
	if len(proxyABI) == 0 {
		return implABI, nil
	}
	if len(implABI) == 0 {
		return proxyABI, nil
	}

	var proxy, impl []ABIEntry
	if err := json.Unmarshal(proxyABI, &proxy); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(implABI, &impl); err != nil {
		return nil, err
	}

	// Deduplicate by function/event name
	seen := make(map[string]bool)
	var combined []ABIEntry

	for _, entry := range proxy {
		key := entry.Type + ":" + entry.Name
		if !seen[key] {
			seen[key] = true
			combined = append(combined, entry)
		}
	}

	for _, entry := range impl {
		key := entry.Type + ":" + entry.Name
		if !seen[key] {
			seen[key] = true
			combined = append(combined, entry)
		}
	}

	return json.Marshal(combined)
}

// getCode fetches contract bytecode.
func (d *ProxyDetector) getCode(ctx context.Context, address string) (string, error) {
	result, err := d.rpcCall(ctx, "eth_getCode", []interface{}{address, "latest"})
	if err != nil {
		return "", err
	}

	var code string
	if err := json.Unmarshal(result, &code); err != nil {
		return "", err
	}

	return code, nil
}

// getStorageAt reads storage at a specific slot.
func (d *ProxyDetector) getStorageAt(ctx context.Context, address, slot string) (string, error) {
	result, err := d.rpcCall(ctx, "eth_getStorageAt", []interface{}{address, slot, "latest"})
	if err != nil {
		return "", err
	}

	var value string
	if err := json.Unmarshal(result, &value); err != nil {
		return "", err
	}

	return value, nil
}

// callContract calls a contract function.
func (d *ProxyDetector) callContract(ctx context.Context, address, selector string) (string, error) {
	result, err := d.rpcCall(ctx, "eth_call", []interface{}{
		map[string]string{
			"to":   address,
			"data": selector,
		},
		"latest",
	})
	if err != nil {
		return "", err
	}

	var value string
	if err := json.Unmarshal(result, &value); err != nil {
		return "", err
	}

	return value, nil
}

// rpcCall makes a JSON-RPC call.
func (d *ProxyDetector) rpcCall(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	reqBody, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", d.rpcEndpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.rpcClient.Do(req)
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

// extractAddress extracts 20-byte address from 32-byte storage value.
func extractAddress(value string) string {
	value = strings.TrimPrefix(strings.ToLower(value), "0x")
	if len(value) < 40 {
		return ""
	}
	// Address is in the last 20 bytes (40 hex chars)
	addr := value[len(value)-40:]
	return "0x" + addr
}

// isZeroAddress checks if address is zero.
func isZeroAddress(addr string) bool {
	addr = strings.TrimPrefix(strings.ToLower(addr), "0x")
	for _, c := range addr {
		if c != '0' {
			return false
		}
	}
	return true
}

// ContractStore interface for contract storage.
type ContractStore interface {
	GetContract(ctx context.Context, address string) (*SmartContract, error)
	SaveContract(ctx context.Context, contract *SmartContract) error
}

// DetectAllProxies detects proxies for a list of addresses.
func (d *ProxyDetector) DetectAllProxies(ctx context.Context, addresses []string) (map[string]*ProxyInfo, error) {
	results := make(map[string]*ProxyInfo)

	for _, addr := range addresses {
		info, err := d.DetectProxy(ctx, addr)
		if err != nil {
			// Log error but continue with other addresses
			results[addr] = &ProxyInfo{IsProxy: false}
			continue
		}
		results[addr] = info
	}

	return results, nil
}

// ResolveProxyChain follows proxy chain to find the final implementation.
func (d *ProxyDetector) ResolveProxyChain(ctx context.Context, address string, maxDepth int) ([]string, error) {
	if maxDepth <= 0 {
		maxDepth = 10
	}

	var chain []string
	visited := make(map[string]bool)
	current := strings.ToLower(address)

	for i := 0; i < maxDepth; i++ {
		if visited[current] {
			// Circular reference detected
			break
		}
		visited[current] = true
		chain = append(chain, current)

		info, err := d.DetectProxy(ctx, current)
		if err != nil || !info.IsProxy || info.ImplementationAddress == "" {
			break
		}

		current = strings.ToLower(info.ImplementationAddress)
	}

	return chain, nil
}
