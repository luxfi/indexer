// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// RPCClient provides JSON-RPC communication with Lux nodes
type RPCClient struct {
	url    string
	client *http.Client
}

// NewRPCClient creates a new RPC client
func NewRPCClient(url string) *RPCClient {
	return &RPCClient{
		url:    url,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// RPCError represents a JSON-RPC error
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

// Call makes a JSON-RPC call
func (c *RPCClient) Call(method string, params interface{}) (json.RawMessage, error) {
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.client.Post(c.url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("post request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result struct {
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if result.Error != nil {
		return nil, result.Error
	}

	return result.Result, nil
}

// CallInfo makes a call to the info API
func (c *RPCClient) CallInfo(method string, params interface{}) (json.RawMessage, error) {
	return c.Call(method, params)
}

// EthCall makes an eth_call RPC
func (c *RPCClient) EthCall(to string, data string) (string, error) {
	result, err := c.Call("eth_call", []interface{}{
		map[string]string{"to": to, "data": data},
		"latest",
	})
	if err != nil {
		return "", err
	}
	var res string
	json.Unmarshal(result, &res)
	return res, nil
}

// GetChainID gets the chain ID
func (c *RPCClient) GetChainID() (uint64, error) {
	result, err := c.Call("eth_chainId", []interface{}{})
	if err != nil {
		return 0, err
	}

	var chainIDHex string
	if err := json.Unmarshal(result, &chainIDHex); err != nil {
		return 0, err
	}

	chainID := new(big.Int)
	chainID.SetString(strings.TrimPrefix(chainIDHex, "0x"), 16)
	return chainID.Uint64(), nil
}

// GetBlockNumber gets the latest block number
func (c *RPCClient) GetBlockNumber() (uint64, error) {
	result, err := c.Call("eth_blockNumber", []interface{}{})
	if err != nil {
		return 0, err
	}

	var blockNumHex string
	if err := json.Unmarshal(result, &blockNumHex); err != nil {
		return 0, err
	}

	blockNum := new(big.Int)
	blockNum.SetString(strings.TrimPrefix(blockNumHex, "0x"), 16)
	return blockNum.Uint64(), nil
}

// GetBalance gets the balance of an address
func (c *RPCClient) GetBalance(address string) (*big.Int, error) {
	result, err := c.Call("eth_getBalance", []interface{}{address, "latest"})
	if err != nil {
		return nil, err
	}

	var balanceHex string
	if err := json.Unmarshal(result, &balanceHex); err != nil {
		return nil, err
	}

	balance := new(big.Int)
	balance.SetString(strings.TrimPrefix(balanceHex, "0x"), 16)
	return balance, nil
}

// GetTransactionCount gets the nonce for an address
func (c *RPCClient) GetTransactionCount(address string) (uint64, error) {
	result, err := c.Call("eth_getTransactionCount", []interface{}{address, "latest"})
	if err != nil {
		return 0, err
	}

	var nonceHex string
	if err := json.Unmarshal(result, &nonceHex); err != nil {
		return 0, err
	}

	nonce := new(big.Int)
	nonce.SetString(strings.TrimPrefix(nonceHex, "0x"), 16)
	return nonce.Uint64(), nil
}

// SendRawTransaction sends a signed transaction
func (c *RPCClient) SendRawTransaction(signedTx string) (string, error) {
	result, err := c.Call("eth_sendRawTransaction", []interface{}{signedTx})
	if err != nil {
		return "", err
	}

	var txHash string
	json.Unmarshal(result, &txHash)
	return txHash, nil
}

// GetTransactionReceipt gets a transaction receipt
func (c *RPCClient) GetTransactionReceipt(txHash string) (map[string]interface{}, error) {
	result, err := c.Call("eth_getTransactionReceipt", []interface{}{txHash})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil // Transaction not mined yet
	}

	var receipt map[string]interface{}
	if err := json.Unmarshal(result, &receipt); err != nil {
		return nil, err
	}

	return receipt, nil
}

// WaitForTransaction waits for a transaction to be mined
func (c *RPCClient) WaitForTransaction(txHash string, timeout time.Duration) (map[string]interface{}, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		receipt, err := c.GetTransactionReceipt(txHash)
		if err != nil {
			return nil, err
		}
		if receipt != nil {
			return receipt, nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return nil, fmt.Errorf("transaction %s not mined within %v", txHash, timeout)
}

// GetBlockByNumber gets a block by number
func (c *RPCClient) GetBlockByNumber(number uint64, fullTxs bool) (map[string]interface{}, error) {
	result, err := c.Call("eth_getBlockByNumber", []interface{}{
		fmt.Sprintf("0x%x", number),
		fullTxs,
	})
	if err != nil {
		return nil, err
	}

	var block map[string]interface{}
	if err := json.Unmarshal(result, &block); err != nil {
		return nil, err
	}

	return block, nil
}

// GetLogs gets logs matching a filter
func (c *RPCClient) GetLogs(fromBlock, toBlock uint64, addresses []string, topics [][]string) ([]map[string]interface{}, error) {
	filter := map[string]interface{}{
		"fromBlock": fmt.Sprintf("0x%x", fromBlock),
		"toBlock":   fmt.Sprintf("0x%x", toBlock),
	}

	if len(addresses) > 0 {
		filter["address"] = addresses
	}
	if len(topics) > 0 {
		filter["topics"] = topics
	}

	result, err := c.Call("eth_getLogs", []interface{}{filter})
	if err != nil {
		return nil, err
	}

	var logs []map[string]interface{}
	if err := json.Unmarshal(result, &logs); err != nil {
		return nil, err
	}

	return logs, nil
}

// PChainClient provides P-Chain specific RPC methods
type PChainClient struct {
	*RPCClient
}

// NewPChainClient creates a P-Chain client
func NewPChainClient(url string) *PChainClient {
	return &PChainClient{NewRPCClient(url)}
}

// GetHeight gets the current P-Chain height
func (c *PChainClient) GetHeight() (uint64, error) {
	result, err := c.Call("platform.getHeight", map[string]interface{}{})
	if err != nil {
		return 0, err
	}

	var resp struct {
		Height string `json:"height"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return 0, err
	}

	height := new(big.Int)
	height.SetString(resp.Height, 10)
	return height.Uint64(), nil
}

// GetCurrentValidators gets current validators
func (c *PChainClient) GetCurrentValidators(netID string) ([]map[string]interface{}, error) {
	params := map[string]interface{}{}
	if netID != "" && netID != "primary" {
		params["netID"] = netID
	}

	result, err := c.Call("platform.getCurrentValidators", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Validators []map[string]interface{} `json:"validators"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	return resp.Validators, nil
}

// GetNets gets all chains (kept for backward compatibility)
func (c *PChainClient) GetNets() ([]map[string]interface{}, error) {
	result, err := c.Call("platform.getBlockchains", map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	var resp struct {
		Blockchains []map[string]interface{} `json:"blockchains"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	return resp.Blockchains, nil
}

// GetBlockchains gets all blockchains
func (c *PChainClient) GetBlockchains() ([]map[string]interface{}, error) {
	result, err := c.Call("platform.getBlockchains", map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	var resp struct {
		Blockchains []map[string]interface{} `json:"blockchains"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	return resp.Blockchains, nil
}

// GetStake gets stake for addresses
func (c *PChainClient) GetStake(addresses []string) (map[string]interface{}, error) {
	result, err := c.Call("platform.getStake", map[string]interface{}{
		"addresses": addresses,
	})
	if err != nil {
		return nil, err
	}

	var stake map[string]interface{}
	json.Unmarshal(result, &stake)
	return stake, nil
}

// XChainClient provides X-Chain specific RPC methods
type XChainClient struct {
	*RPCClient
}

// NewXChainClient creates an X-Chain client
func NewXChainClient(url string) *XChainClient {
	return &XChainClient{NewRPCClient(url)}
}

// GetBalance gets the X-Chain balance for an address
func (c *XChainClient) GetBalance(address, assetID string) (uint64, error) {
	params := map[string]interface{}{
		"address": address,
	}
	if assetID != "" {
		params["assetID"] = assetID
	}

	result, err := c.Call("xvm.getBalance", params)
	if err != nil {
		return 0, err
	}

	var resp struct {
		Balance string `json:"balance"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return 0, err
	}

	balance := new(big.Int)
	balance.SetString(resp.Balance, 10)
	return balance.Uint64(), nil
}

// GetUTXOs gets UTXOs for addresses
func (c *XChainClient) GetUTXOs(addresses []string, limit int) ([]map[string]interface{}, error) {
	params := map[string]interface{}{
		"addresses": addresses,
	}
	if limit > 0 {
		params["limit"] = limit
	}

	result, err := c.Call("xvm.getUTXOs", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		UTXOs []map[string]interface{} `json:"utxos"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	return resp.UTXOs, nil
}

// GetAssetDescription gets asset info
func (c *XChainClient) GetAssetDescription(assetID string) (map[string]interface{}, error) {
	result, err := c.Call("xvm.getAssetDescription", map[string]interface{}{
		"assetID": assetID,
	})
	if err != nil {
		return nil, err
	}

	var asset map[string]interface{}
	json.Unmarshal(result, &asset)
	return asset, nil
}

// InfoClient provides info API methods
type InfoClient struct {
	*RPCClient
}

// NewInfoClient creates an info client
func NewInfoClient(url string) *InfoClient {
	return &InfoClient{NewRPCClient(url)}
}

// IsBootstrapped checks if a chain is bootstrapped
func (c *InfoClient) IsBootstrapped(chain string) (bool, error) {
	result, err := c.Call("info.isBootstrapped", map[string]string{"chain": chain})
	if err != nil {
		return false, err
	}

	var resp struct {
		IsBootstrapped bool `json:"isBootstrapped"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return false, err
	}

	return resp.IsBootstrapped, nil
}

// GetNodeID gets the node ID
func (c *InfoClient) GetNodeID() (string, error) {
	result, err := c.Call("info.getNodeID", nil)
	if err != nil {
		return "", err
	}

	var resp struct {
		NodeID string `json:"nodeID"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", err
	}

	return resp.NodeID, nil
}

// GetNetworkID gets the network ID
func (c *InfoClient) GetNetworkID() (uint32, error) {
	result, err := c.Call("info.getNetworkID", nil)
	if err != nil {
		return 0, err
	}

	var resp struct {
		NetworkID string `json:"networkID"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return 0, err
	}

	id := new(big.Int)
	id.SetString(resp.NetworkID, 10)
	return uint32(id.Uint64()), nil
}

// GetNetworkName gets the network name
func (c *InfoClient) GetNetworkName() (string, error) {
	result, err := c.Call("info.getNetworkName", nil)
	if err != nil {
		return "", err
	}

	var resp struct {
		NetworkName string `json:"networkName"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", err
	}

	return resp.NetworkName, nil
}

// GetVMs gets all VM IDs
func (c *InfoClient) GetVMs() (map[string][]string, error) {
	result, err := c.Call("info.getVMs", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		VMs map[string][]string `json:"vms"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	return resp.VMs, nil
}
