// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package evm provides a unified EVM indexer for all Lux EVM chains.
// Indexes blocks, transactions, addresses, tokens, smart contracts, and more.
// Designed to be a complete Go-native replacement for Blockscout.
//
// Supported chains:
//   - Lux C-Chain (mainnet: 96369, testnet: 96368)
//   - Zoo EVM (mainnet: 200200, testnet: 200201)
//   - Hanzo AI Chain (36963)
//   - Any EVM-compatible subnet
package evm

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/luxfi/indexer/chain"
	"github.com/luxfi/indexer/storage"
)

const (
	// DefaultPort for EVM indexer API
	DefaultPort = 4000
	// DefaultDatabase name
	DefaultDatabase = "explorer_evm"
	// ChainID for Lux C-Chain mainnet
	ChainID = 96369
)

// Transaction represents an EVM transaction
type Transaction struct {
	Hash             string    `json:"hash"`
	BlockHash        string    `json:"blockHash"`
	BlockNumber      uint64    `json:"blockNumber"`
	From             string    `json:"from"`
	To               string    `json:"to"`
	Value            string    `json:"value"`
	Gas              uint64    `json:"gas"`
	GasPrice         string    `json:"gasPrice"`
	GasUsed          uint64    `json:"gasUsed"`
	Nonce            uint64    `json:"nonce"`
	Input            string    `json:"input"`
	TransactionIndex uint64    `json:"transactionIndex"`
	Type             uint8     `json:"type"`
	Status           uint8     `json:"status"` // 1 = success, 0 = fail
	ContractAddress  string    `json:"contractAddress,omitempty"`
	Timestamp        time.Time `json:"timestamp"`
}

// Address represents an account or contract
type Address struct {
	Hash            string    `json:"hash"`
	Balance         string    `json:"balance"`
	TxCount         uint64    `json:"txCount"`
	IsContract      bool      `json:"isContract"`
	ContractCode    string    `json:"contractCode,omitempty"`
	ContractCreator string    `json:"contractCreator,omitempty"`
	ContractTxHash  string    `json:"contractTxHash,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// TokenTransfer represents an ERC20/721/1155 transfer
type TokenTransfer struct {
	ID              string    `json:"id"`
	TxHash          string    `json:"txHash"`
	LogIndex        uint64    `json:"logIndex"`
	BlockNumber     uint64    `json:"blockNumber"`
	TokenAddress    string    `json:"tokenAddress"`
	TokenType       string    `json:"tokenType"` // ERC20, ERC721, ERC1155
	From            string    `json:"from"`
	To              string    `json:"to"`
	Value           string    `json:"value"`   // amount for ERC20, tokenId for NFT
	TokenID         string    `json:"tokenId"` // for ERC721/1155
	Timestamp       time.Time `json:"timestamp"`
}

// Token represents an ERC20/721/1155 token contract
type Token struct {
	Address     string    `json:"address"`
	Name        string    `json:"name"`
	Symbol      string    `json:"symbol"`
	Decimals    uint8     `json:"decimals"`
	TotalSupply string    `json:"totalSupply"`
	TokenType   string    `json:"tokenType"` // ERC20, ERC721, ERC1155
	HolderCount uint64    `json:"holderCount"`
	TxCount     uint64    `json:"txCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Log represents an EVM event log
type Log struct {
	TxHash      string   `json:"txHash"`
	LogIndex    uint64   `json:"logIndex"`
	BlockNumber uint64   `json:"blockNumber"`
	Address     string   `json:"address"`
	Topics      []string `json:"topics"`
	Data        string   `json:"data"`
	Removed     bool     `json:"removed"`
}

// InternalTransaction represents an internal call trace
type InternalTransaction struct {
	ID           string    `json:"id"`
	TxHash       string    `json:"txHash"`
	BlockNumber  uint64    `json:"blockNumber"`
	TraceIndex   uint64    `json:"traceIndex"`
	TraceAddress []int     `json:"traceAddress"` // Position in call tree [0], [0,1], [0,1,0], etc.
	CallType     string    `json:"callType"`     // call, delegatecall, staticcall, callcode, create, create2, selfdestruct
	From         string    `json:"from"`
	To           string    `json:"to"`
	Value        string    `json:"value"`
	Gas          uint64    `json:"gas"`
	GasUsed      uint64    `json:"gasUsed"`
	Input        string    `json:"input"`
	Output       string    `json:"output"`
	Error        string    `json:"error,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
	// For create/create2
	CreatedContractAddress string `json:"createdContractAddress,omitempty"`
	CreatedContractCode    string `json:"createdContractCode,omitempty"`
	Init                   string `json:"init,omitempty"` // Contract init code for creates
}

// TokenBalance represents a token balance for an address
type TokenBalance struct {
	TokenAddress  string    `json:"tokenAddress"`
	HolderAddress string    `json:"holderAddress"`
	Balance       string    `json:"balance"`
	TokenID       string    `json:"tokenId,omitempty"` // For ERC721/1155
	BlockNumber   uint64    `json:"blockNumber"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// AddressCoinBalance represents native coin balance history
type AddressCoinBalance struct {
	AddressHash    string    `json:"addressHash"`
	BlockNumber    uint64    `json:"blockNumber"`
	Value          string    `json:"value"`
	ValueFetchedAt time.Time `json:"valueFetchedAt"`
}

// blockData is the parsed block content
type blockData struct {
	Transactions         []Transaction         `json:"transactions,omitempty"`
	Logs                 []Log                 `json:"logs,omitempty"`
	TokenTransfers       []TokenTransfer       `json:"tokenTransfers,omitempty"`
	InternalTransactions []InternalTransaction `json:"internalTransactions,omitempty"`
}

// Well-known event signatures
var (
	// ERC20 Transfer(address,address,uint256)
	TopicTransferERC20 = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	// ERC721 Transfer(address,address,uint256)
	TopicTransferERC721 = TopicTransferERC20 // Same signature, different indexed params
	// ERC1155 TransferSingle(address,address,address,uint256,uint256)
	TopicTransferSingle = "0xc3d58168c5ae7397731d063d5bbf3d657854427343f4c083240f7aacaa2d0f62"
	// ERC1155 TransferBatch(address,address,address,uint256[],uint256[])
	TopicTransferBatch = "0x4a39dc06d4c0dbc64b70af90fd698a233a518aa5d07e595d983b8c0526c8f7fb"
)

// TracerType specifies which tracer to use
type TracerType string

const (
	// TracerCallTracer uses geth's built-in callTracer (recommended)
	TracerCallTracer TracerType = "callTracer"
	// TracerJS uses custom JavaScript tracer for Blockscout compatibility
	TracerJS TracerType = "js"
	// TracerParity uses trace_replayBlockTransactions (Parity/Nethermind)
	TracerParity TracerType = "parity"
)

// Adapter implements the chain.Adapter interface for C-Chain
type Adapter struct {
	rpcEndpoint  string
	httpClient   *http.Client
	tracerType   TracerType
	traceTimeout string // e.g. "120s"
	mu           sync.RWMutex
}

// AdapterOption configures the adapter
type AdapterOption func(*Adapter)

// WithTracerType sets the tracer type
func WithTracerType(t TracerType) AdapterOption {
	return func(a *Adapter) {
		a.tracerType = t
	}
}

// WithTraceTimeout sets the trace timeout
func WithTraceTimeout(timeout string) AdapterOption {
	return func(a *Adapter) {
		a.traceTimeout = timeout
	}
}

// New creates a new C-Chain adapter
func New(rpcEndpoint string, opts ...AdapterOption) *Adapter {
	a := &Adapter{
		rpcEndpoint:  rpcEndpoint,
		tracerType:   TracerCallTracer,
		traceTimeout: "120s",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// ParseBlock parses C-Chain block data
func (a *Adapter) ParseBlock(data json.RawMessage) (*chain.Block, error) {
	var raw struct {
		Hash         string          `json:"hash"`
		ParentHash   string          `json:"parentHash"`
		Number       string          `json:"number"`
		Timestamp    string          `json:"timestamp"`
		Transactions json.RawMessage `json:"transactions"`
		GasUsed      string          `json:"gasUsed"`
		GasLimit     string          `json:"gasLimit"`
		Miner        string          `json:"miner"`
		BaseFee      string          `json:"baseFeePerGas"`
		Size         string          `json:"size"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal block: %w", err)
	}

	height := hexToUint64(raw.Number)
	ts := hexToUint64(raw.Timestamp)

	// Count transactions
	var txCount int
	var txHashes []string

	// Try as array of hashes first
	if err := json.Unmarshal(raw.Transactions, &txHashes); err != nil {
		// Reset txHashes (unmarshal may have partially populated it with empty values)
		txHashes = nil
		// Try as array of objects
		var txs []struct {
			Hash string `json:"hash"`
		}
		if err := json.Unmarshal(raw.Transactions, &txs); err == nil {
			for _, tx := range txs {
				txHashes = append(txHashes, tx.Hash)
			}
		}
	}
	txCount = len(txHashes)

	block := &chain.Block{
		ID:        raw.Hash,
		ParentID:  raw.ParentHash,
		Height:    height,
		Timestamp: time.Unix(int64(ts), 0),
		Status:    chain.StatusAccepted,
		TxCount:   txCount,
		TxIDs:     txHashes,
		Data:      data,
		Metadata: map[string]interface{}{
			"gasUsed":  hexToUint64(raw.GasUsed),
			"gasLimit": hexToUint64(raw.GasLimit),
			"miner":    raw.Miner,
			"baseFee":  raw.BaseFee,
			"size":     hexToUint64(raw.Size),
		},
	}

	return block, nil
}

// GetRecentBlocks fetches recent blocks from C-Chain RPC
func (a *Adapter) GetRecentBlocks(ctx context.Context, limit int) ([]json.RawMessage, error) {
	// Get latest block number
	latestResult, err := a.call(ctx, "eth_blockNumber", []interface{}{})
	if err != nil {
		return nil, err
	}

	var latestHex string
	if err := json.Unmarshal(latestResult, &latestHex); err != nil {
		return nil, fmt.Errorf("parse block number: %w", err)
	}
	latest := hexToUint64(latestHex)

	// Fetch recent blocks
	var blocks []json.RawMessage
	for i := 0; i < limit && latest-uint64(i) > 0; i++ {
		blockNum := fmt.Sprintf("0x%x", latest-uint64(i))
		result, err := a.call(ctx, "eth_getBlockByNumber", []interface{}{blockNum, true})
		if err != nil {
			continue
		}
		blocks = append(blocks, result)
	}

	return blocks, nil
}

// GetBlockByID fetches a specific block by hash
func (a *Adapter) GetBlockByID(ctx context.Context, id string) (json.RawMessage, error) {
	return a.call(ctx, "eth_getBlockByHash", []interface{}{id, true})
}

// GetBlockByHeight fetches a specific block by number
func (a *Adapter) GetBlockByHeight(ctx context.Context, height uint64) (json.RawMessage, error) {
	blockNum := fmt.Sprintf("0x%x", height)
	return a.call(ctx, "eth_getBlockByNumber", []interface{}{blockNum, true})
}

// GetLatestBlock fetches the latest block and returns a parsed EVMBlock
func (a *Adapter) GetLatestBlock(ctx context.Context) (*EVMBlock, error) {
	result, err := a.call(ctx, "eth_getBlockByNumber", []interface{}{"latest", true})
	if err != nil {
		return nil, err
	}
	return a.parseEVMBlock(result)
}

// GetBlockByNumber fetches a specific block by number and returns a parsed EVMBlock
func (a *Adapter) GetBlockByNumber(ctx context.Context, number uint64) (*EVMBlock, error) {
	blockNum := fmt.Sprintf("0x%x", number)
	result, err := a.call(ctx, "eth_getBlockByNumber", []interface{}{blockNum, true})
	if err != nil {
		return nil, err
	}
	return a.parseEVMBlock(result)
}

// parseEVMBlock parses raw JSON into an EVMBlock
func (a *Adapter) parseEVMBlock(data json.RawMessage) (*EVMBlock, error) {
	var raw struct {
		Hash         string          `json:"hash"`
		ParentHash   string          `json:"parentHash"`
		Number       string          `json:"number"`
		Nonce        string          `json:"nonce"`
		Miner        string          `json:"miner"`
		Difficulty   string          `json:"difficulty"`
		GasLimit     string          `json:"gasLimit"`
		GasUsed      string          `json:"gasUsed"`
		Timestamp    string          `json:"timestamp"`
		BaseFee      string          `json:"baseFeePerGas"`
		Size         string          `json:"size"`
		Transactions json.RawMessage `json:"transactions"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal block: %w", err)
	}

	// Parse transactions (can be hashes or full objects)
	var txHashes []string
	if err := json.Unmarshal(raw.Transactions, &txHashes); err != nil {
		// Try as array of objects
		var txs []struct {
			Hash string `json:"hash"`
		}
		if err := json.Unmarshal(raw.Transactions, &txs); err == nil {
			for _, tx := range txs {
				txHashes = append(txHashes, tx.Hash)
			}
		}
	}

	return &EVMBlock{
		Number:       hexToUint64(raw.Number),
		Hash:         raw.Hash,
		ParentHash:   raw.ParentHash,
		Nonce:        raw.Nonce,
		Miner:        raw.Miner,
		Difficulty:   raw.Difficulty,
		GasLimit:     hexToUint64(raw.GasLimit),
		GasUsed:      hexToUint64(raw.GasUsed),
		Timestamp:    time.Unix(int64(hexToUint64(raw.Timestamp)), 0),
		TxCount:      len(txHashes),
		BaseFee:      raw.BaseFee,
		Size:         hexToUint64(raw.Size),
		Transactions: txHashes,
	}, nil
}

// GetTransactionReceipt fetches a transaction receipt
func (a *Adapter) GetTransactionReceipt(ctx context.Context, txHash string) (*Transaction, []Log, error) {
	result, err := a.call(ctx, "eth_getTransactionReceipt", []interface{}{txHash})
	if err != nil {
		return nil, nil, err
	}

	var receipt struct {
		TransactionHash  string `json:"transactionHash"`
		BlockHash        string `json:"blockHash"`
		BlockNumber      string `json:"blockNumber"`
		From             string `json:"from"`
		To               string `json:"to"`
		GasUsed          string `json:"gasUsed"`
		Status           string `json:"status"`
		ContractAddress  string `json:"contractAddress"`
		TransactionIndex string `json:"transactionIndex"`
		Logs             []struct {
			Address  string   `json:"address"`
			Topics   []string `json:"topics"`
			Data     string   `json:"data"`
			LogIndex string   `json:"logIndex"`
			Removed  bool     `json:"removed"`
		} `json:"logs"`
	}

	if err := json.Unmarshal(result, &receipt); err != nil {
		return nil, nil, fmt.Errorf("parse receipt: %w", err)
	}

	tx := &Transaction{
		Hash:             receipt.TransactionHash,
		BlockHash:        receipt.BlockHash,
		BlockNumber:      hexToUint64(receipt.BlockNumber),
		From:             strings.ToLower(receipt.From),
		To:               strings.ToLower(receipt.To),
		GasUsed:          hexToUint64(receipt.GasUsed),
		TransactionIndex: hexToUint64(receipt.TransactionIndex),
		ContractAddress:  strings.ToLower(receipt.ContractAddress),
	}
	if receipt.Status == "0x1" {
		tx.Status = 1
	}

	var logs []Log
	for _, l := range receipt.Logs {
		logs = append(logs, Log{
			TxHash:      receipt.TransactionHash,
			LogIndex:    hexToUint64(l.LogIndex),
			BlockNumber: tx.BlockNumber,
			Address:     strings.ToLower(l.Address),
			Topics:      l.Topics,
			Data:        l.Data,
			Removed:     l.Removed,
		})
	}

	return tx, logs, nil
}

// TraceTransaction traces internal calls for a transaction using debug_traceTransaction
func (a *Adapter) TraceTransaction(ctx context.Context, txHash string, blockNumber uint64, timestamp time.Time) ([]InternalTransaction, error) {
	a.mu.RLock()
	tracerType := a.tracerType
	timeout := a.traceTimeout
	a.mu.RUnlock()

	switch tracerType {
	case TracerParity:
		return a.traceTransactionParity(ctx, txHash, blockNumber, timestamp)
	case TracerCallTracer:
		return a.traceTransactionCallTracer(ctx, txHash, blockNumber, timestamp, timeout)
	case TracerJS:
		return a.traceTransactionJS(ctx, txHash, blockNumber, timestamp, timeout)
	default:
		return a.traceTransactionCallTracer(ctx, txHash, blockNumber, timestamp, timeout)
	}
}

// traceTransactionCallTracer uses geth's built-in callTracer
func (a *Adapter) traceTransactionCallTracer(ctx context.Context, txHash string, blockNumber uint64, timestamp time.Time, timeout string) ([]InternalTransaction, error) {
	params := map[string]interface{}{
		"tracer": "callTracer",
		"tracerConfig": map[string]interface{}{
			"withLog": false,
		},
	}
	if timeout != "" {
		params["timeout"] = timeout
	}

	result, err := a.call(ctx, "debug_traceTransaction", []interface{}{txHash, params})
	if err != nil {
		return nil, fmt.Errorf("debug_traceTransaction: %w", err)
	}

	var callFrame CallFrame
	if err := json.Unmarshal(result, &callFrame); err != nil {
		return nil, fmt.Errorf("parse trace result: %w", err)
	}

	// Convert call tree to flat list with trace addresses
	var traces []InternalTransaction
	traces = flattenCallFrame(&callFrame, txHash, blockNumber, timestamp, []int{}, &traces, 0)

	return traces, nil
}

// CallFrame represents a call in the callTracer output
type CallFrame struct {
	Type    string       `json:"type"`
	From    string       `json:"from"`
	To      string       `json:"to"`
	Value   string       `json:"value,omitempty"`
	Gas     string       `json:"gas"`
	GasUsed string       `json:"gasUsed"`
	Input   string       `json:"input"`
	Output  string       `json:"output,omitempty"`
	Error   string       `json:"error,omitempty"`
	Calls   []*CallFrame `json:"calls,omitempty"`
}

// flattenCallFrame converts a call tree to a flat list of internal transactions
func flattenCallFrame(frame *CallFrame, txHash string, blockNumber uint64, timestamp time.Time, traceAddr []int, traces *[]InternalTransaction, index int) []InternalTransaction {
	callType := normalizeCallType(frame.Type)

	itx := InternalTransaction{
		ID:           fmt.Sprintf("%s-%d", txHash, index),
		TxHash:       txHash,
		BlockNumber:  blockNumber,
		TraceIndex:   uint64(index),
		TraceAddress: make([]int, len(traceAddr)),
		CallType:     callType,
		From:         strings.ToLower(frame.From),
		To:           strings.ToLower(frame.To),
		Value:        normalizeValue(frame.Value),
		Gas:          hexToUint64(frame.Gas),
		GasUsed:      hexToUint64(frame.GasUsed),
		Input:        frame.Input,
		Output:       frame.Output,
		Error:        frame.Error,
		Timestamp:    timestamp,
	}
	copy(itx.TraceAddress, traceAddr)

	// Handle create opcodes
	if callType == "create" || callType == "create2" {
		itx.CreatedContractAddress = itx.To
		itx.CreatedContractCode = frame.Output
		itx.Init = frame.Input
		itx.Output = ""
	}

	*traces = append(*traces, itx)
	index++

	// Recursively process child calls
	for i, child := range frame.Calls {
		childAddr := append(append([]int{}, traceAddr...), i)
		flattenCallFrame(child, txHash, blockNumber, timestamp, childAddr, traces, index)
		index = len(*traces)
	}

	return *traces
}

// normalizeCallType normalizes call type from geth to Blockscout format
func normalizeCallType(t string) string {
	switch strings.ToLower(t) {
	case "call":
		return "call"
	case "delegatecall":
		return "delegatecall"
	case "staticcall":
		return "staticcall"
	case "callcode":
		return "callcode"
	case "create":
		return "create"
	case "create2":
		return "create2"
	case "selfdestruct", "suicide":
		return "selfdestruct"
	default:
		return strings.ToLower(t)
	}
}

// normalizeValue ensures value is a valid hex string
func normalizeValue(v string) string {
	if v == "" {
		return "0x0"
	}
	return v
}

// traceTransactionJS uses custom JavaScript tracer (Blockscout compatible)
func (a *Adapter) traceTransactionJS(ctx context.Context, txHash string, blockNumber uint64, timestamp time.Time, timeout string) ([]InternalTransaction, error) {
	// JavaScript tracer that mimics Blockscout's tracer.js
	tracer := `{
		callStack: [{}],
		descended: false,
		step: function(log, db) {
			var error = log.getError();
			if (error !== undefined) {
				this.fault(log, db);
			} else {
				this.success(log, db);
			}
		},
		fault: function(log, db) {
			if (this.topCall().error === undefined) {
				this.putError(log);
			}
		},
		putError: function(log) {
			if (this.callStack.length > 1) {
				var call = this.callStack.pop();
				call.error = log.getError();
				if (call.gasBigInt !== undefined) {
					call.gasUsedBigInt = call.gasBigInt;
				}
				delete call.outputOffset;
				delete call.outputLength;
				this.pushChildCall(call);
			} else {
				var call = this.bottomCall();
				call.error = log.getError();
			}
		},
		topCall: function() { return this.callStack[this.callStack.length - 1]; },
		bottomCall: function() { return this.callStack[0]; },
		pushChildCall: function(childCall) {
			var topCall = this.topCall();
			if (topCall.calls === undefined) { topCall.calls = []; }
			topCall.calls.push(childCall);
		},
		success: function(log, db) {
			var op = log.op.toString();
			if (this.descended) {
				this.topCall().gasBigInt = log.getGas();
				this.descended = false;
			}
			this.beforeOp(log, db);
			switch (op) {
				case 'CREATE': this.createOp(log); break;
				case 'CREATE2': this.create2Op(log); break;
				case 'SELFDESTRUCT': this.selfDestructOp(log, db); break;
				case 'CALL':
				case 'CALLCODE':
				case 'DELEGATECALL':
				case 'STATICCALL': this.callOp(log, op); break;
				case 'REVERT': this.topCall().error = 'execution reverted'; break;
			}
		},
		beforeOp: function(log, db) {
			var logDepth = log.getDepth();
			var callStackDepth = this.callStack.length;
			if (logDepth < callStackDepth) {
				var call = this.callStack.pop();
				var ret = log.stack.peek(0);
				if (!ret.equals(0)) {
					if (call.type === 'create' || call.type === 'create2') {
						call.createdContractAddressHash = toHex(toAddress(ret.toString(16)));
						call.createdContractCode = toHex(db.getCode(toAddress(ret.toString(16))));
					} else {
						call.output = toHex(log.memory.slice(call.outputOffset, call.outputOffset + call.outputLength));
					}
				} else if (call.error === undefined) {
					call.error = 'internal failure';
				}
				delete call.outputOffset;
				delete call.outputLength;
				this.pushChildCall(call);
			} else {
				var topCall = this.topCall();
				if (topCall.gasBigInt === undefined) { topCall.gasBigInt = log.getGas(); }
				topCall.gasUsedBigInt = topCall.gasBigInt - log.getGas() - log.getCost();
			}
		},
		createOp: function(log) {
			var inputOffset = log.stack.peek(1).valueOf();
			var inputLength = log.stack.peek(2).valueOf();
			var call = {
				type: 'create',
				from: toHex(log.contract.getAddress()),
				init: toHex(log.memory.slice(inputOffset, inputOffset + inputLength)),
				valueBigInt: bigInt(log.stack.peek(0).toString(10))
			};
			this.callStack.push(call);
			this.descended = true;
		},
		create2Op: function(log) {
			var inputOffset = log.stack.peek(1).valueOf();
			var inputLength = log.stack.peek(2).valueOf();
			var call = {
				type: 'create2',
				from: toHex(log.contract.getAddress()),
				init: toHex(log.memory.slice(inputOffset, inputOffset + inputLength)),
				valueBigInt: bigInt(log.stack.peek(0).toString(10))
			};
			this.callStack.push(call);
			this.descended = true;
		},
		selfDestructOp: function(log, db) {
			this.pushChildCall({
				type: 'selfdestruct',
				from: toHex(log.contract.getAddress()),
				to: toHex(toAddress(log.stack.peek(0).toString(16))),
				gasBigInt: log.getGas(),
				gasUsedBigInt: log.getCost(),
				valueBigInt: db.getBalance(log.contract.getAddress())
			});
		},
		callOp: function(log, op) {
			var to = toAddress(log.stack.peek(1).toString(16));
			if (!isPrecompiled(to)) {
				var stackOffset = (op === 'DELEGATECALL' || op === 'STATICCALL' ? 0 : 1);
				var inputOffset = log.stack.peek(2 + stackOffset).valueOf();
				var inputLength = log.stack.peek(3 + stackOffset).valueOf();
				var inputEnd = Math.min(inputOffset + inputLength, log.memory.length());
				var call = {
					type: 'call',
					callType: op.toLowerCase(),
					from: toHex(log.contract.getAddress()),
					to: toHex(to),
					input: (inputLength == 0 ? '0x' : toHex(log.memory.slice(inputOffset, inputEnd))),
					outputOffset: log.stack.peek(4 + stackOffset).valueOf(),
					outputLength: log.stack.peek(5 + stackOffset).valueOf()
				};
				switch (op) {
					case 'CALL':
					case 'CALLCODE': call.valueBigInt = bigInt(log.stack.peek(2)); break;
					case 'STATICCALL': call.valueBigInt = bigInt.zero; break;
				}
				this.callStack.push(call);
				this.descended = true;
			}
		},
		result: function(ctx, db) {
			var result = this.ctxToResult(ctx, db);
			var callSequence = this.sequence(result, [], result.valueBigInt, []).callSequence;
			return this.encodeCallSequence(callSequence);
		},
		ctxToResult: function(ctx, db) {
			var result;
			switch (ctx.type) {
				case 'CALL':
					result = {
						type: 'call', callType: 'call',
						from: toHex(ctx.from), to: toHex(ctx.to),
						valueBigInt: bigInt(ctx.value.toString(10)),
						gasBigInt: bigInt(ctx.gas), gasUsedBigInt: bigInt(ctx.gasUsed),
						input: toHex(ctx.input)
					};
					break;
				case 'CREATE':
				case 'CREATE2':
					result = {
						type: ctx.type.toLowerCase(),
						from: toHex(ctx.from),
						init: toHex(ctx.input),
						valueBigInt: bigInt(ctx.value.toString(10)),
						gasBigInt: bigInt(ctx.gas), gasUsedBigInt: bigInt(ctx.gasUsed)
					};
					if (!ctx.error) {
						result.createdContractAddressHash = toHex(ctx.to);
						result.createdContractCode = toHex(db.getCode(ctx.to));
					}
					break;
			}
			var bottomCall = this.bottomCall();
			if (bottomCall.calls !== undefined) { result.calls = bottomCall.calls; }
			if (bottomCall.error !== undefined) { result.error = bottomCall.error; }
			else if (ctx.error !== undefined) { result.error = ctx.error; }
			else if (result.type === 'call') { result.output = toHex(ctx.output); }
			return result;
		},
		sequence: function(call, callSequence, availableValueBigInt, traceAddress) {
			var subcalls = call.calls;
			delete call.calls;
			call.traceAddress = traceAddress;
			if (call.type === 'call' && call.callType === 'delegatecall') {
				call.valueBigInt = availableValueBigInt;
			}
			var newCallSequence = callSequence.concat([call]);
			if (subcalls !== undefined) {
				for (var i = 0; i < subcalls.length; i++) {
					var nestedSequenced = this.sequence(subcalls[i], newCallSequence, call.valueBigInt, traceAddress.concat([i]));
					newCallSequence = nestedSequenced.callSequence;
				}
			}
			return { callSequence: newCallSequence };
		},
		encodeCallSequence: function(calls) {
			for (var i = 0; i < calls.length; i++) {
				var call = calls[i];
				call.value = '0x' + (call.valueBigInt || bigInt.zero).toString(16);
				delete call.valueBigInt;
				call.gas = '0x' + (call.gasBigInt || bigInt.zero).toString(16);
				delete call.gasBigInt;
				call.gasUsed = '0x' + (call.gasUsedBigInt || bigInt.zero).toString(16);
				delete call.gasUsedBigInt;
			}
			return calls;
		}
	}`

	params := map[string]interface{}{
		"tracer": tracer,
	}
	if timeout != "" {
		params["timeout"] = timeout
	}

	result, err := a.call(ctx, "debug_traceTransaction", []interface{}{txHash, params})
	if err != nil {
		return nil, fmt.Errorf("debug_traceTransaction: %w", err)
	}

	var jsTraces []struct {
		Type                       string `json:"type"`
		CallType                   string `json:"callType,omitempty"`
		From                       string `json:"from"`
		To                         string `json:"to,omitempty"`
		Value                      string `json:"value"`
		Gas                        string `json:"gas"`
		GasUsed                    string `json:"gasUsed"`
		Input                      string `json:"input,omitempty"`
		Init                       string `json:"init,omitempty"`
		Output                     string `json:"output,omitempty"`
		Error                      string `json:"error,omitempty"`
		TraceAddress               []int  `json:"traceAddress"`
		CreatedContractAddressHash string `json:"createdContractAddressHash,omitempty"`
		CreatedContractCode        string `json:"createdContractCode,omitempty"`
	}

	if err := json.Unmarshal(result, &jsTraces); err != nil {
		return nil, fmt.Errorf("parse JS trace result: %w", err)
	}

	var traces []InternalTransaction
	for i, t := range jsTraces {
		callType := t.Type
		if t.CallType != "" {
			callType = t.CallType
		}

		itx := InternalTransaction{
			ID:                     fmt.Sprintf("%s-%d", txHash, i),
			TxHash:                 txHash,
			BlockNumber:            blockNumber,
			TraceIndex:             uint64(i),
			TraceAddress:           t.TraceAddress,
			CallType:               normalizeCallType(callType),
			From:                   strings.ToLower(t.From),
			To:                     strings.ToLower(t.To),
			Value:                  normalizeValue(t.Value),
			Gas:                    hexToUint64(t.Gas),
			GasUsed:                hexToUint64(t.GasUsed),
			Input:                  t.Input,
			Output:                 t.Output,
			Error:                  t.Error,
			Timestamp:              timestamp,
			CreatedContractAddress: strings.ToLower(t.CreatedContractAddressHash),
			CreatedContractCode:    t.CreatedContractCode,
			Init:                   t.Init,
		}

		traces = append(traces, itx)
	}

	return traces, nil
}

// traceTransactionParity uses trace_replayBlockTransactions (Parity/Nethermind)
func (a *Adapter) traceTransactionParity(ctx context.Context, txHash string, blockNumber uint64, timestamp time.Time) ([]InternalTransaction, error) {
	// First, get the block with transactions to find transaction index
	blockNumHex := fmt.Sprintf("0x%x", blockNumber)
	result, err := a.call(ctx, "trace_replayBlockTransactions", []interface{}{blockNumHex, []string{"trace"}})
	if err != nil {
		return nil, fmt.Errorf("trace_replayBlockTransactions: %w", err)
	}

	var blockTraces []struct {
		TransactionHash string `json:"transactionHash"`
		Trace           []struct {
			Action struct {
				CallType         string `json:"callType,omitempty"`
				From             string `json:"from"`
				To               string `json:"to,omitempty"`
				Value            string `json:"value,omitempty"`
				Gas              string `json:"gas"`
				Input            string `json:"input,omitempty"`
				Init             string `json:"init,omitempty"`
				Address          string `json:"address,omitempty"`          // selfdestruct
				RefundAddress    string `json:"refundAddress,omitempty"`    // selfdestruct
				Balance          string `json:"balance,omitempty"`          // selfdestruct
				CreationMethod   string `json:"creationMethod,omitempty"`   // create, create2
			} `json:"action"`
			Result struct {
				GasUsed string `json:"gasUsed"`
				Output  string `json:"output,omitempty"`
				Address string `json:"address,omitempty"` // created contract
				Code    string `json:"code,omitempty"`    // created contract code
			} `json:"result"`
			Error        string `json:"error,omitempty"`
			Type         string `json:"type"` // call, create, suicide
			Subtraces    int    `json:"subtraces"`
			TraceAddress []int  `json:"traceAddress"`
		} `json:"trace"`
	}

	if err := json.Unmarshal(result, &blockTraces); err != nil {
		return nil, fmt.Errorf("parse trace result: %w", err)
	}

	// Find traces for our transaction
	var traces []InternalTransaction
	for _, bt := range blockTraces {
		if strings.EqualFold(bt.TransactionHash, txHash) {
			for i, t := range bt.Trace {
				callType := normalizeCallType(t.Type)
				if t.Action.CallType != "" {
					callType = normalizeCallType(t.Action.CallType)
				}
				if t.Action.CreationMethod != "" {
					callType = normalizeCallType(t.Action.CreationMethod)
				}

				to := strings.ToLower(t.Action.To)
				if t.Type == "suicide" || t.Type == "selfdestruct" {
					to = strings.ToLower(t.Action.RefundAddress)
				}

				value := normalizeValue(t.Action.Value)
				if t.Action.Balance != "" {
					value = t.Action.Balance
				}

				itx := InternalTransaction{
					ID:                     fmt.Sprintf("%s-%d", txHash, i),
					TxHash:                 txHash,
					BlockNumber:            blockNumber,
					TraceIndex:             uint64(i),
					TraceAddress:           t.TraceAddress,
					CallType:               callType,
					From:                   strings.ToLower(t.Action.From),
					To:                     to,
					Value:                  value,
					Gas:                    hexToUint64(t.Action.Gas),
					GasUsed:                hexToUint64(t.Result.GasUsed),
					Input:                  t.Action.Input,
					Output:                 t.Result.Output,
					Error:                  t.Error,
					Timestamp:              timestamp,
					CreatedContractAddress: strings.ToLower(t.Result.Address),
					CreatedContractCode:    t.Result.Code,
					Init:                   t.Action.Init,
				}

				if t.Type == "suicide" || t.Type == "selfdestruct" {
					itx.From = strings.ToLower(t.Action.Address)
				}

				traces = append(traces, itx)
			}
			break
		}
	}

	return traces, nil
}

// TraceBlock traces all transactions in a block
func (a *Adapter) TraceBlock(ctx context.Context, blockNumber uint64, timestamp time.Time) ([]InternalTransaction, error) {
	blockNumHex := fmt.Sprintf("0x%x", blockNumber)

	a.mu.RLock()
	tracerType := a.tracerType
	timeout := a.traceTimeout
	a.mu.RUnlock()

	if tracerType == TracerParity {
		// Use trace_replayBlockTransactions for all transactions at once
		return a.traceBlockParity(ctx, blockNumHex, blockNumber, timestamp)
	}

	// For callTracer, trace each transaction individually
	params := map[string]interface{}{
		"tracer": "callTracer",
		"tracerConfig": map[string]interface{}{
			"withLog": false,
		},
	}
	if timeout != "" {
		params["timeout"] = timeout
	}

	result, err := a.call(ctx, "debug_traceBlockByNumber", []interface{}{blockNumHex, params})
	if err != nil {
		return nil, fmt.Errorf("debug_traceBlockByNumber: %w", err)
	}

	var blockTraces []struct {
		TxHash string    `json:"txHash"`
		Result CallFrame `json:"result"`
	}

	if err := json.Unmarshal(result, &blockTraces); err != nil {
		return nil, fmt.Errorf("parse block trace result: %w", err)
	}

	var allTraces []InternalTransaction
	for _, bt := range blockTraces {
		var traces []InternalTransaction
		flattenCallFrame(&bt.Result, bt.TxHash, blockNumber, timestamp, []int{}, &traces, 0)
		allTraces = append(allTraces, traces...)
	}

	return allTraces, nil
}

// traceBlockParity traces a block using trace_replayBlockTransactions
func (a *Adapter) traceBlockParity(ctx context.Context, blockNumHex string, blockNumber uint64, timestamp time.Time) ([]InternalTransaction, error) {
	result, err := a.call(ctx, "trace_replayBlockTransactions", []interface{}{blockNumHex, []string{"trace"}})
	if err != nil {
		return nil, fmt.Errorf("trace_replayBlockTransactions: %w", err)
	}

	var blockTraces []struct {
		TransactionHash string `json:"transactionHash"`
		Trace           []struct {
			Action struct {
				CallType       string `json:"callType,omitempty"`
				From           string `json:"from"`
				To             string `json:"to,omitempty"`
				Value          string `json:"value,omitempty"`
				Gas            string `json:"gas"`
				Input          string `json:"input,omitempty"`
				Init           string `json:"init,omitempty"`
				Address        string `json:"address,omitempty"`
				RefundAddress  string `json:"refundAddress,omitempty"`
				Balance        string `json:"balance,omitempty"`
				CreationMethod string `json:"creationMethod,omitempty"`
			} `json:"action"`
			Result struct {
				GasUsed string `json:"gasUsed"`
				Output  string `json:"output,omitempty"`
				Address string `json:"address,omitempty"`
				Code    string `json:"code,omitempty"`
			} `json:"result"`
			Error        string `json:"error,omitempty"`
			Type         string `json:"type"`
			Subtraces    int    `json:"subtraces"`
			TraceAddress []int  `json:"traceAddress"`
		} `json:"trace"`
	}

	if err := json.Unmarshal(result, &blockTraces); err != nil {
		return nil, fmt.Errorf("parse trace result: %w", err)
	}

	var allTraces []InternalTransaction
	for _, bt := range blockTraces {
		for i, t := range bt.Trace {
			callType := normalizeCallType(t.Type)
			if t.Action.CallType != "" {
				callType = normalizeCallType(t.Action.CallType)
			}
			if t.Action.CreationMethod != "" {
				callType = normalizeCallType(t.Action.CreationMethod)
			}

			to := strings.ToLower(t.Action.To)
			if t.Type == "suicide" || t.Type == "selfdestruct" {
				to = strings.ToLower(t.Action.RefundAddress)
			}

			value := normalizeValue(t.Action.Value)
			if t.Action.Balance != "" {
				value = t.Action.Balance
			}

			itx := InternalTransaction{
				ID:                     fmt.Sprintf("%s-%d", bt.TransactionHash, i),
				TxHash:                 bt.TransactionHash,
				BlockNumber:            blockNumber,
				TraceIndex:             uint64(i),
				TraceAddress:           t.TraceAddress,
				CallType:               callType,
				From:                   strings.ToLower(t.Action.From),
				To:                     to,
				Value:                  value,
				Gas:                    hexToUint64(t.Action.Gas),
				GasUsed:                hexToUint64(t.Result.GasUsed),
				Input:                  t.Action.Input,
				Output:                 t.Result.Output,
				Error:                  t.Error,
				Timestamp:              timestamp,
				CreatedContractAddress: strings.ToLower(t.Result.Address),
				CreatedContractCode:    t.Result.Code,
				Init:                   t.Action.Init,
			}

			if t.Type == "suicide" || t.Type == "selfdestruct" {
				itx.From = strings.ToLower(t.Action.Address)
			}

			allTraces = append(allTraces, itx)
		}
	}

	return allTraces, nil
}

// GetTokenBalance fetches ERC20 token balance for an address
func (a *Adapter) GetTokenBalance(ctx context.Context, tokenAddress, holderAddress string, blockNumber uint64) (*big.Int, error) {
	// ERC20 balanceOf(address) = 0x70a08231 + padded address
	data := "0x70a08231" + strings.Repeat("0", 24) + strings.TrimPrefix(strings.ToLower(holderAddress), "0x")

	blockTag := "latest"
	if blockNumber > 0 {
		blockTag = fmt.Sprintf("0x%x", blockNumber)
	}

	result, err := a.call(ctx, "eth_call", []interface{}{
		map[string]string{"to": tokenAddress, "data": data},
		blockTag,
	})
	if err != nil {
		return nil, err
	}

	var balanceHex string
	if err := json.Unmarshal(result, &balanceHex); err != nil {
		return nil, err
	}

	return hexToBigInt(balanceHex), nil
}

// GetERC721Owner fetches the owner of an ERC721 token
func (a *Adapter) GetERC721Owner(ctx context.Context, tokenAddress string, tokenID *big.Int) (string, error) {
	// ERC721 ownerOf(uint256) = 0x6352211e + padded tokenId
	tokenIDHex := fmt.Sprintf("%064x", tokenID)
	data := "0x6352211e" + tokenIDHex

	result, err := a.call(ctx, "eth_call", []interface{}{
		map[string]string{"to": tokenAddress, "data": data},
		"latest",
	})
	if err != nil {
		return "", err
	}

	var ownerHex string
	if err := json.Unmarshal(result, &ownerHex); err != nil {
		return "", err
	}

	return topicToAddress(ownerHex), nil
}

// GetERC1155Balance fetches ERC1155 token balance
func (a *Adapter) GetERC1155Balance(ctx context.Context, tokenAddress, holderAddress string, tokenID *big.Int) (*big.Int, error) {
	// ERC1155 balanceOf(address,uint256) = 0x00fdd58e + padded address + padded tokenId
	addressPadded := strings.Repeat("0", 24) + strings.TrimPrefix(strings.ToLower(holderAddress), "0x")
	tokenIDHex := fmt.Sprintf("%064x", tokenID)
	data := "0x00fdd58e" + addressPadded + tokenIDHex

	result, err := a.call(ctx, "eth_call", []interface{}{
		map[string]string{"to": tokenAddress, "data": data},
		"latest",
	})
	if err != nil {
		return nil, err
	}

	var balanceHex string
	if err := json.Unmarshal(result, &balanceHex); err != nil {
		return nil, err
	}

	return hexToBigInt(balanceHex), nil
}

// GetBalanceAtBlock fetches native coin balance at a specific block
func (a *Adapter) GetBalanceAtBlock(ctx context.Context, address string, blockNumber uint64) (*big.Int, error) {
	blockTag := "latest"
	if blockNumber > 0 {
		blockTag = fmt.Sprintf("0x%x", blockNumber)
	}

	result, err := a.call(ctx, "eth_getBalance", []interface{}{address, blockTag})
	if err != nil {
		return nil, err
	}

	var balanceHex string
	if err := json.Unmarshal(result, &balanceHex); err != nil {
		return nil, err
	}

	return hexToBigInt(balanceHex), nil
}

// call makes a JSON-RPC call to the C-Chain node
func (a *Adapter) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	reqBody, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.rpcEndpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
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

// InitSchema creates C-Chain specific database tables using unified storage
func (a *Adapter) InitSchema(ctx context.Context, store storage.Store) error {
	schema := storage.Schema{
		Name: "cchain",
		Tables: []storage.Table{
			{
				Name: "cchain_transactions",
				Columns: []storage.Column{
					{Name: "hash", Type: storage.TypeText, Primary: true},
					{Name: "block_hash", Type: storage.TypeText, Nullable: false},
					{Name: "block_number", Type: storage.TypeBigInt, Nullable: false},
					{Name: "tx_from", Type: storage.TypeText, Nullable: false},
					{Name: "tx_to", Type: storage.TypeText},
					{Name: "value", Type: storage.TypeText, Default: "'0'"},
					{Name: "gas", Type: storage.TypeBigInt, Nullable: false},
					{Name: "gas_price", Type: storage.TypeText},
					{Name: "gas_used", Type: storage.TypeBigInt},
					{Name: "nonce", Type: storage.TypeBigInt, Nullable: false},
					{Name: "input", Type: storage.TypeText},
					{Name: "tx_index", Type: storage.TypeInt, Nullable: false},
					{Name: "tx_type", Type: storage.TypeInt, Default: "0"},
					{Name: "status", Type: storage.TypeInt, Default: "1"},
					{Name: "contract_address", Type: storage.TypeText},
					{Name: "timestamp", Type: storage.TypeTimestamp, Nullable: false},
				},
			},
			{
				Name: "cchain_addresses",
				Columns: []storage.Column{
					{Name: "hash", Type: storage.TypeText, Primary: true},
					{Name: "balance", Type: storage.TypeText, Default: "'0'"},
					{Name: "tx_count", Type: storage.TypeBigInt, Default: "0"},
					{Name: "is_contract", Type: storage.TypeBool, Default: "false"},
					{Name: "contract_code", Type: storage.TypeText},
					{Name: "contract_creator", Type: storage.TypeText},
					{Name: "contract_tx_hash", Type: storage.TypeText},
					{Name: "created_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
					{Name: "updated_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
			{
				Name: "cchain_token_transfers",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "tx_hash", Type: storage.TypeText, Nullable: false},
					{Name: "log_index", Type: storage.TypeBigInt, Nullable: false},
					{Name: "block_number", Type: storage.TypeBigInt, Nullable: false},
					{Name: "token_address", Type: storage.TypeText, Nullable: false},
					{Name: "token_type", Type: storage.TypeText, Nullable: false},
					{Name: "tx_from", Type: storage.TypeText, Nullable: false},
					{Name: "tx_to", Type: storage.TypeText, Nullable: false},
					{Name: "value", Type: storage.TypeText, Nullable: false},
					{Name: "token_id", Type: storage.TypeText},
					{Name: "timestamp", Type: storage.TypeTimestamp, Nullable: false},
				},
			},
			{
				Name: "cchain_tokens",
				Columns: []storage.Column{
					{Name: "address", Type: storage.TypeText, Primary: true},
					{Name: "name", Type: storage.TypeText},
					{Name: "symbol", Type: storage.TypeText},
					{Name: "decimals", Type: storage.TypeInt, Default: "18"},
					{Name: "total_supply", Type: storage.TypeText},
					{Name: "token_type", Type: storage.TypeText, Nullable: false},
					{Name: "holder_count", Type: storage.TypeBigInt, Default: "0"},
					{Name: "tx_count", Type: storage.TypeBigInt, Default: "0"},
					{Name: "created_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
					{Name: "updated_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
			{
				Name: "cchain_logs",
				Columns: []storage.Column{
					{Name: "tx_hash", Type: storage.TypeText, Nullable: false},
					{Name: "log_index", Type: storage.TypeBigInt, Nullable: false},
					{Name: "block_number", Type: storage.TypeBigInt, Nullable: false},
					{Name: "address", Type: storage.TypeText, Nullable: false},
					{Name: "topics", Type: storage.TypeText, Nullable: false},
					{Name: "data", Type: storage.TypeText},
					{Name: "removed", Type: storage.TypeBool, Default: "false"},
				},
			},
			{
				Name: "cchain_internal_transactions",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeText, Primary: true},
					{Name: "tx_hash", Type: storage.TypeText, Nullable: false},
					{Name: "block_number", Type: storage.TypeBigInt, Nullable: false},
					{Name: "trace_index", Type: storage.TypeBigInt, Nullable: false},
					{Name: "trace_address", Type: storage.TypeText, Default: "''"},
					{Name: "call_type", Type: storage.TypeText, Nullable: false},
					{Name: "tx_from", Type: storage.TypeText, Nullable: false},
					{Name: "tx_to", Type: storage.TypeText},
					{Name: "value", Type: storage.TypeText, Default: "'0'"},
					{Name: "gas", Type: storage.TypeBigInt},
					{Name: "gas_used", Type: storage.TypeBigInt},
					{Name: "input", Type: storage.TypeText},
					{Name: "output", Type: storage.TypeText},
					{Name: "error", Type: storage.TypeText},
					{Name: "created_contract_address", Type: storage.TypeText},
					{Name: "created_contract_code", Type: storage.TypeText},
					{Name: "init", Type: storage.TypeText},
					{Name: "timestamp", Type: storage.TypeTimestamp, Nullable: false},
				},
			},
			{
				Name: "cchain_token_balances",
				Columns: []storage.Column{
					{Name: "token_address", Type: storage.TypeText, Nullable: false},
					{Name: "holder_address", Type: storage.TypeText, Nullable: false},
					{Name: "balance", Type: storage.TypeText, Default: "'0'"},
					{Name: "token_id", Type: storage.TypeText},
					{Name: "block_number", Type: storage.TypeBigInt, Default: "0"},
					{Name: "updated_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
			{
				Name: "cchain_extended_stats",
				Columns: []storage.Column{
					{Name: "id", Type: storage.TypeInt, Primary: true},
					{Name: "total_transactions", Type: storage.TypeBigInt, Default: "0"},
					{Name: "total_addresses", Type: storage.TypeBigInt, Default: "0"},
					{Name: "total_contracts", Type: storage.TypeBigInt, Default: "0"},
					{Name: "total_tokens", Type: storage.TypeBigInt, Default: "0"},
					{Name: "total_token_transfers", Type: storage.TypeBigInt, Default: "0"},
					{Name: "total_internal_transactions", Type: storage.TypeBigInt, Default: "0"},
					{Name: "total_gas_used", Type: storage.TypeBigInt, Default: "0"},
					{Name: "avg_gas_price", Type: storage.TypeText, Default: "'0'"},
					{Name: "avg_block_time", Type: storage.TypeFloat, Default: "0"},
					{Name: "tps_24h", Type: storage.TypeFloat, Default: "0"},
					{Name: "updated_at", Type: storage.TypeTimestamp, Default: "CURRENT_TIMESTAMP"},
				},
			},
		},
		Indexes: []storage.Index{
			{Name: "idx_cchain_tx_block", Table: "cchain_transactions", Columns: []string{"block_number"}},
			{Name: "idx_cchain_tx_from", Table: "cchain_transactions", Columns: []string{"tx_from"}},
			{Name: "idx_cchain_tx_to", Table: "cchain_transactions", Columns: []string{"tx_to"}},
			{Name: "idx_cchain_tx_timestamp", Table: "cchain_transactions", Columns: []string{"timestamp"}},
			{Name: "idx_cchain_addr_balance", Table: "cchain_addresses", Columns: []string{"balance"}},
			{Name: "idx_cchain_transfer_token", Table: "cchain_token_transfers", Columns: []string{"token_address"}},
			{Name: "idx_cchain_transfer_from", Table: "cchain_token_transfers", Columns: []string{"tx_from"}},
			{Name: "idx_cchain_transfer_to", Table: "cchain_token_transfers", Columns: []string{"tx_to"}},
			{Name: "idx_cchain_transfer_block", Table: "cchain_token_transfers", Columns: []string{"block_number"}},
			{Name: "idx_cchain_token_type", Table: "cchain_tokens", Columns: []string{"token_type"}},
			{Name: "idx_cchain_logs_address", Table: "cchain_logs", Columns: []string{"address"}},
			{Name: "idx_cchain_logs_block", Table: "cchain_logs", Columns: []string{"block_number"}},
			{Name: "idx_cchain_internal_tx", Table: "cchain_internal_transactions", Columns: []string{"tx_hash"}},
			{Name: "idx_cchain_internal_from", Table: "cchain_internal_transactions", Columns: []string{"tx_from"}},
			{Name: "idx_cchain_internal_to", Table: "cchain_internal_transactions", Columns: []string{"tx_to"}},
			{Name: "idx_cchain_internal_block", Table: "cchain_internal_transactions", Columns: []string{"block_number"}},
			{Name: "idx_cchain_balance_holder", Table: "cchain_token_balances", Columns: []string{"holder_address"}},
			{Name: "idx_cchain_balance_token", Table: "cchain_token_balances", Columns: []string{"token_address"}},
		},
	}

	if err := store.InitSchema(ctx, schema); err != nil {
		return err
	}

	// Initialize stats row
	return store.Exec(ctx, "INSERT OR IGNORE INTO cchain_extended_stats (id) VALUES (1)")
}

// GetStats returns C-Chain specific statistics using unified storage
func (a *Adapter) GetStats(ctx context.Context, store storage.Store) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Get extended stats
	rows, err := store.Query(ctx, `
		SELECT total_transactions, total_addresses, total_contracts, total_tokens,
		       total_token_transfers, total_internal_transactions, total_gas_used,
		       avg_gas_price, avg_block_time, tps_24h, updated_at
		FROM cchain_extended_stats WHERE id = 1
	`)
	if err != nil {
		return nil, err
	}

	if len(rows) > 0 {
		row := rows[0]
		stats["total_transactions"] = row["total_transactions"]
		stats["total_addresses"] = row["total_addresses"]
		stats["total_contracts"] = row["total_contracts"]
		stats["total_tokens"] = row["total_tokens"]
		stats["total_token_transfers"] = row["total_token_transfers"]
		stats["total_internal_transactions"] = row["total_internal_transactions"]
		stats["total_gas_used"] = row["total_gas_used"]
		stats["avg_gas_price"] = row["avg_gas_price"]
		stats["avg_block_time_seconds"] = row["avg_block_time"]
		stats["tps_24h"] = row["tps_24h"]
		stats["updated_at"] = row["updated_at"]
	}
	stats["chain_id"] = ChainID

	// Get top tokens by holder count
	tokenRows, err := store.Query(ctx, `
		SELECT address, name, symbol, token_type, holder_count, tx_count
		FROM cchain_tokens
		ORDER BY holder_count DESC
		LIMIT 10
	`)
	if err != nil {
		return stats, nil // Return stats without tokens on error
	}

	var topTokens []map[string]interface{}
	for _, row := range tokenRows {
		topTokens = append(topTokens, map[string]interface{}{
			"address":      row["address"],
			"name":         row["name"],
			"symbol":       row["symbol"],
			"token_type":   row["token_type"],
			"holder_count": row["holder_count"],
			"tx_count":     row["tx_count"],
		})
	}
	stats["top_tokens"] = topTokens

	return stats, nil
}

// StoreTransaction stores a transaction
func (a *Adapter) StoreTransaction(ctx context.Context, db *sql.DB, tx Transaction) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO cchain_transactions
		(hash, block_hash, block_number, tx_from, tx_to, value, gas, gas_price, gas_used,
		 nonce, input, tx_index, tx_type, status, contract_address, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (hash) DO UPDATE SET
			status = EXCLUDED.status,
			gas_used = EXCLUDED.gas_used
	`, tx.Hash, tx.BlockHash, tx.BlockNumber, tx.From, tx.To, tx.Value, tx.Gas, tx.GasPrice,
		tx.GasUsed, tx.Nonce, tx.Input, tx.TransactionIndex, tx.Type, tx.Status, tx.ContractAddress, tx.Timestamp)
	return err
}

// StoreAddress stores an address
func (a *Adapter) StoreAddress(ctx context.Context, db *sql.DB, addr Address) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO cchain_addresses
		(hash, balance, tx_count, is_contract, contract_code, contract_creator, contract_tx_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (hash) DO UPDATE SET
			balance = EXCLUDED.balance,
			tx_count = cchain_addresses.tx_count + 1,
			updated_at = EXCLUDED.updated_at
	`, addr.Hash, addr.Balance, addr.TxCount, addr.IsContract, addr.ContractCode,
		addr.ContractCreator, addr.ContractTxHash, addr.CreatedAt, addr.UpdatedAt)
	return err
}

// StoreTokenTransfer stores a token transfer
func (a *Adapter) StoreTokenTransfer(ctx context.Context, db *sql.DB, t TokenTransfer) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO cchain_token_transfers
		(id, tx_hash, log_index, block_number, token_address, token_type, tx_from, tx_to, value, token_id, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO NOTHING
	`, t.ID, t.TxHash, t.LogIndex, t.BlockNumber, t.TokenAddress, t.TokenType, t.From, t.To, t.Value, t.TokenID, t.Timestamp)
	return err
}

// StoreToken stores a token
func (a *Adapter) StoreToken(ctx context.Context, db *sql.DB, t Token) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO cchain_tokens
		(address, name, symbol, decimals, total_supply, token_type, holder_count, tx_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (address) DO UPDATE SET
			total_supply = EXCLUDED.total_supply,
			holder_count = EXCLUDED.holder_count,
			tx_count = cchain_tokens.tx_count + 1,
			updated_at = EXCLUDED.updated_at
	`, t.Address, t.Name, t.Symbol, t.Decimals, t.TotalSupply, t.TokenType, t.HolderCount, t.TxCount, t.CreatedAt, t.UpdatedAt)
	return err
}

// StoreLog stores an event log
func (a *Adapter) StoreLog(ctx context.Context, db *sql.DB, l Log) error {
	topicsJSON, _ := json.Marshal(l.Topics)
	_, err := db.ExecContext(ctx, `
		INSERT INTO cchain_logs (tx_hash, log_index, block_number, address, topics, data, removed)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (tx_hash, log_index) DO NOTHING
	`, l.TxHash, l.LogIndex, l.BlockNumber, l.Address, topicsJSON, l.Data, l.Removed)
	return err
}

// StoreInternalTransaction stores an internal transaction
func (a *Adapter) StoreInternalTransaction(ctx context.Context, db *sql.DB, itx InternalTransaction) error {
	// Convert trace address to PostgreSQL array format
	traceAddrStr := intSliceToPostgresArray(itx.TraceAddress)

	_, err := db.ExecContext(ctx, `
		INSERT INTO cchain_internal_transactions
		(id, tx_hash, block_number, trace_index, trace_address, call_type, tx_from, tx_to,
		 value, gas, gas_used, input, output, error, created_contract_address,
		 created_contract_code, init, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (id) DO NOTHING
	`, itx.ID, itx.TxHash, itx.BlockNumber, itx.TraceIndex, traceAddrStr, itx.CallType,
		itx.From, itx.To, itx.Value, itx.Gas, itx.GasUsed, itx.Input, itx.Output, itx.Error,
		itx.CreatedContractAddress, itx.CreatedContractCode, itx.Init, itx.Timestamp)
	return err
}

// intSliceToPostgresArray converts an int slice to PostgreSQL array format
func intSliceToPostgresArray(arr []int) string {
	if len(arr) == 0 {
		return "{}"
	}
	strs := make([]string, len(arr))
	for i, v := range arr {
		strs[i] = fmt.Sprintf("%d", v)
	}
	return "{" + strings.Join(strs, ",") + "}"
}

// StoreTokenBalance stores or updates a token balance
func (a *Adapter) StoreTokenBalance(ctx context.Context, db *sql.DB, tb TokenBalance) error {
	tokenID := tb.TokenID
	if tokenID == "" {
		tokenID = ""
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO cchain_token_balances
		(token_address, holder_address, balance, token_id, block_number, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (token_address, holder_address, COALESCE(token_id, '')) DO UPDATE SET
			balance = EXCLUDED.balance,
			block_number = EXCLUDED.block_number,
			updated_at = EXCLUDED.updated_at
	`, tb.TokenAddress, tb.HolderAddress, tb.Balance, tokenID, tb.BlockNumber, tb.UpdatedAt)
	return err
}

// StoreTokenBalanceHistory stores token balance history
func (a *Adapter) StoreTokenBalanceHistory(ctx context.Context, db *sql.DB, tb TokenBalance) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO cchain_token_balance_history
		(token_address, holder_address, balance, token_id, block_number)
		VALUES ($1, $2, $3, $4, $5)
	`, tb.TokenAddress, tb.HolderAddress, tb.Balance, tb.TokenID, tb.BlockNumber)
	return err
}

// StoreAddressCoinBalance stores native coin balance for an address at a block
func (a *Adapter) StoreAddressCoinBalance(ctx context.Context, db *sql.DB, acb AddressCoinBalance) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO cchain_address_coin_balances
		(address_hash, block_number, value, value_fetched_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (address_hash, block_number) DO UPDATE SET
			value = CASE
				WHEN EXCLUDED.value IS NOT NULL AND (cchain_address_coin_balances.value_fetched_at IS NULL
					OR EXCLUDED.value_fetched_at > cchain_address_coin_balances.value_fetched_at)
				THEN EXCLUDED.value
				ELSE cchain_address_coin_balances.value
			END,
			value_fetched_at = CASE
				WHEN EXCLUDED.value IS NOT NULL AND (cchain_address_coin_balances.value_fetched_at IS NULL
					OR EXCLUDED.value_fetched_at > cchain_address_coin_balances.value_fetched_at)
				THEN EXCLUDED.value_fetched_at
				ELSE cchain_address_coin_balances.value_fetched_at
			END,
			updated_at = NOW()
	`, acb.AddressHash, acb.BlockNumber, acb.Value, acb.ValueFetchedAt)
	return err
}

// UpdateTokenHolderCount updates the holder count for a token
func (a *Adapter) UpdateTokenHolderCount(ctx context.Context, db *sql.DB, tokenAddress string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE cchain_tokens
		SET holder_count = (
			SELECT COUNT(DISTINCT holder_address)
			FROM cchain_token_balances
			WHERE token_address = $1 AND balance != '0'
		),
		updated_at = NOW()
		WHERE address = $1
	`, tokenAddress)
	return err
}

// ProcessBlock processes a block and extracts all data
func (a *Adapter) ProcessBlock(ctx context.Context, db *sql.DB, block *chain.Block) error {
	timestamp := block.Timestamp
	now := time.Now()

	// Collect addresses involved in this block for balance tracking
	addressSet := make(map[string]struct{})

	// Process transactions from block data
	for _, txHash := range block.TxIDs {
		// Get receipt for full transaction details
		tx, logs, err := a.GetTransactionReceipt(ctx, txHash)
		if err != nil {
			continue
		}
		tx.Timestamp = timestamp

		// Store transaction
		if err := a.StoreTransaction(ctx, db, *tx); err != nil {
			continue
		}

		// Track addresses for balance fetching
		if tx.From != "" {
			addressSet[strings.ToLower(tx.From)] = struct{}{}
		}
		if tx.To != "" {
			addressSet[strings.ToLower(tx.To)] = struct{}{}
		}

		// Store logs and process token transfers
		for _, log := range logs {
			if err := a.StoreLog(ctx, db, log); err != nil {
				continue
			}

			// Parse token transfers from logs
			transfers := parseTokenTransfers(log, timestamp)
			for _, transfer := range transfers {
				if err := a.StoreTokenTransfer(ctx, db, transfer); err != nil {
					continue
				}

				// Update token balances
				a.updateTokenBalances(ctx, db, transfer, block.Height)
			}
		}

		// Trace internal transactions
		internalTxs, err := a.TraceTransaction(ctx, txHash, block.Height, timestamp)
		if err == nil {
			for _, itx := range internalTxs {
				if err := a.StoreInternalTransaction(ctx, db, itx); err != nil {
					continue
				}

				// Track addresses from internal transactions
				if itx.From != "" {
					addressSet[strings.ToLower(itx.From)] = struct{}{}
				}
				if itx.To != "" {
					addressSet[strings.ToLower(itx.To)] = struct{}{}
				}
				if itx.CreatedContractAddress != "" {
					addressSet[strings.ToLower(itx.CreatedContractAddress)] = struct{}{}
				}
			}
		}

		// Update addresses
		if tx.From != "" {
			a.StoreAddress(ctx, db, Address{
				Hash:      strings.ToLower(tx.From),
				TxCount:   1,
				UpdatedAt: now,
			})
		}
		if tx.To != "" {
			a.StoreAddress(ctx, db, Address{
				Hash:      strings.ToLower(tx.To),
				TxCount:   1,
				UpdatedAt: now,
			})
		}

		// Handle contract creation
		if tx.ContractAddress != "" {
			a.StoreAddress(ctx, db, Address{
				Hash:            strings.ToLower(tx.ContractAddress),
				IsContract:      true,
				ContractCreator: tx.From,
				ContractTxHash:  tx.Hash,
				CreatedAt:       timestamp,
				UpdatedAt:       now,
			})
		}
	}

	// Fetch and store coin balances for all involved addresses
	a.fetchAndStoreCoinBalances(ctx, db, addressSet, block.Height)

	return nil
}

// updateTokenBalances updates token balances after a transfer
func (a *Adapter) updateTokenBalances(ctx context.Context, db *sql.DB, transfer TokenTransfer, blockNumber uint64) {
	now := time.Now()

	// Fetch and update sender balance (if not zero address)
	if transfer.From != "" && transfer.From != "0x0000000000000000000000000000000000000000" {
		var balance *big.Int
		var err error

		switch transfer.TokenType {
		case "ERC20":
			balance, err = a.GetTokenBalance(ctx, transfer.TokenAddress, transfer.From, blockNumber)
		case "ERC721":
			// For ERC721, balance is 0 or 1 based on ownership
			owner, ownerErr := a.GetERC721Owner(ctx, transfer.TokenAddress, hexToBigInt(transfer.TokenID))
			if ownerErr == nil && strings.EqualFold(owner, transfer.From) {
				balance = big.NewInt(1)
			} else {
				balance = big.NewInt(0)
			}
			err = ownerErr
		case "ERC1155":
			balance, err = a.GetERC1155Balance(ctx, transfer.TokenAddress, transfer.From, hexToBigInt(transfer.TokenID))
		}

		if err == nil && balance != nil {
			tb := TokenBalance{
				TokenAddress:  transfer.TokenAddress,
				HolderAddress: transfer.From,
				Balance:       balance.String(),
				TokenID:       transfer.TokenID,
				BlockNumber:   blockNumber,
				UpdatedAt:     now,
			}
			a.StoreTokenBalance(ctx, db, tb)
			a.StoreTokenBalanceHistory(ctx, db, tb)
		}
	}

	// Fetch and update receiver balance
	if transfer.To != "" && transfer.To != "0x0000000000000000000000000000000000000000" {
		var balance *big.Int
		var err error

		switch transfer.TokenType {
		case "ERC20":
			balance, err = a.GetTokenBalance(ctx, transfer.TokenAddress, transfer.To, blockNumber)
		case "ERC721":
			owner, ownerErr := a.GetERC721Owner(ctx, transfer.TokenAddress, hexToBigInt(transfer.TokenID))
			if ownerErr == nil && strings.EqualFold(owner, transfer.To) {
				balance = big.NewInt(1)
			} else {
				balance = big.NewInt(0)
			}
			err = ownerErr
		case "ERC1155":
			balance, err = a.GetERC1155Balance(ctx, transfer.TokenAddress, transfer.To, hexToBigInt(transfer.TokenID))
		}

		if err == nil && balance != nil {
			tb := TokenBalance{
				TokenAddress:  transfer.TokenAddress,
				HolderAddress: transfer.To,
				Balance:       balance.String(),
				TokenID:       transfer.TokenID,
				BlockNumber:   blockNumber,
				UpdatedAt:     now,
			}
			a.StoreTokenBalance(ctx, db, tb)
			a.StoreTokenBalanceHistory(ctx, db, tb)
		}
	}

	// Update holder count for the token
	a.UpdateTokenHolderCount(ctx, db, transfer.TokenAddress)
}

// fetchAndStoreCoinBalances fetches and stores native coin balances for addresses
func (a *Adapter) fetchAndStoreCoinBalances(ctx context.Context, db *sql.DB, addresses map[string]struct{}, blockNumber uint64) {
	now := time.Now()

	for addr := range addresses {
		balance, err := a.GetBalanceAtBlock(ctx, addr, blockNumber)
		if err != nil {
			continue
		}

		acb := AddressCoinBalance{
			AddressHash:    addr,
			BlockNumber:    blockNumber,
			Value:          balance.String(),
			ValueFetchedAt: now,
		}

		a.StoreAddressCoinBalance(ctx, db, acb)

		// Also update the main address balance
		a.StoreAddress(ctx, db, Address{
			Hash:      addr,
			Balance:   balance.String(),
			UpdatedAt: now,
		})
	}
}

// UpdateExtendedStats updates C-Chain specific statistics
func (a *Adapter) UpdateExtendedStats(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		UPDATE cchain_extended_stats SET
			total_transactions = (SELECT COUNT(*) FROM cchain_transactions),
			total_addresses = (SELECT COUNT(*) FROM cchain_addresses),
			total_contracts = (SELECT COUNT(*) FROM cchain_addresses WHERE is_contract = TRUE),
			total_tokens = (SELECT COUNT(*) FROM cchain_tokens),
			total_token_transfers = (SELECT COUNT(*) FROM cchain_token_transfers),
			total_internal_transactions = (SELECT COUNT(*) FROM cchain_internal_transactions),
			total_gas_used = COALESCE((SELECT SUM(gas_used) FROM cchain_transactions), 0),
			avg_gas_price = COALESCE((SELECT AVG(CAST(gas_price AS NUMERIC)) FROM cchain_transactions WHERE gas_price IS NOT NULL), 0)::TEXT,
			tps_24h = COALESCE((
				SELECT COUNT(*)::FLOAT / 86400
				FROM cchain_transactions
				WHERE timestamp > NOW() - INTERVAL '24 hours'
			), 0),
			updated_at = NOW()
		WHERE id = 1
	`)
	return err
}

// parseTokenTransfers extracts token transfers from a log
func parseTokenTransfers(log Log, timestamp time.Time) []TokenTransfer {
	var transfers []TokenTransfer

	if len(log.Topics) == 0 {
		return transfers
	}

	topic0 := log.Topics[0]

	switch topic0 {
	case TopicTransferERC20:
		// ERC20/ERC721 Transfer
		if len(log.Topics) >= 3 {
			from := topicToAddress(log.Topics[1])
			to := topicToAddress(log.Topics[2])

			var value, tokenID string
			tokenType := "ERC20"

			if len(log.Topics) == 4 {
				// ERC721: indexed tokenId
				tokenType = "ERC721"
				tokenID = log.Topics[3]
				value = "1"
			} else {
				// ERC20: value in data
				value = hexToBigInt(log.Data).String()
			}

			transfers = append(transfers, TokenTransfer{
				ID:           fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
				TxHash:       log.TxHash,
				LogIndex:     log.LogIndex,
				BlockNumber:  log.BlockNumber,
				TokenAddress: log.Address,
				TokenType:    tokenType,
				From:         from,
				To:           to,
				Value:        value,
				TokenID:      tokenID,
				Timestamp:    timestamp,
			})
		}

	case TopicTransferSingle:
		// ERC1155 TransferSingle
		if len(log.Topics) >= 4 && len(log.Data) >= 64 {
			from := topicToAddress(log.Topics[2])
			to := topicToAddress(log.Topics[3])
			tokenID := hexToBigInt(log.Data[:66]).String()
			value := hexToBigInt("0x" + log.Data[66:]).String()

			transfers = append(transfers, TokenTransfer{
				ID:           fmt.Sprintf("%s-%d", log.TxHash, log.LogIndex),
				TxHash:       log.TxHash,
				LogIndex:     log.LogIndex,
				BlockNumber:  log.BlockNumber,
				TokenAddress: log.Address,
				TokenType:    "ERC1155",
				From:         from,
				To:           to,
				Value:        value,
				TokenID:      tokenID,
				Timestamp:    timestamp,
			})
		}
	}

	return transfers
}

// Helper functions

func hexToUint64(s string) uint64 {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return 0
	}
	n := new(big.Int)
	n.SetString(s, 16)
	return n.Uint64()
}

func hexToBigInt(s string) *big.Int {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return big.NewInt(0)
	}
	n := new(big.Int)
	n.SetString(s, 16)
	return n
}

func topicToAddress(topic string) string {
	topic = strings.TrimPrefix(topic, "0x")
	if len(topic) >= 40 {
		return strings.ToLower("0x" + topic[len(topic)-40:])
	}
	return ""
}

func addressToTopic(addr string) string {
	addr = strings.TrimPrefix(strings.ToLower(addr), "0x")
	return "0x" + strings.Repeat("0", 24) + addr
}

// GetBalance fetches the balance of an address
func (a *Adapter) GetBalance(ctx context.Context, address string) (*big.Int, error) {
	result, err := a.call(ctx, "eth_getBalance", []interface{}{address, "latest"})
	if err != nil {
		return nil, err
	}

	var balanceHex string
	if err := json.Unmarshal(result, &balanceHex); err != nil {
		return nil, err
	}

	return hexToBigInt(balanceHex), nil
}

// GetCode fetches the code of a contract
func (a *Adapter) GetCode(ctx context.Context, address string) (string, error) {
	result, err := a.call(ctx, "eth_getCode", []interface{}{address, "latest"})
	if err != nil {
		return "", err
	}

	var code string
	if err := json.Unmarshal(result, &code); err != nil {
		return "", err
	}

	return code, nil
}

// GetTokenInfo fetches ERC20 token metadata
func (a *Adapter) GetTokenInfo(ctx context.Context, tokenAddress string) (*Token, error) {
	token := &Token{
		Address:   strings.ToLower(tokenAddress),
		TokenType: "ERC20",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Get name
	if result, err := a.callContract(ctx, tokenAddress, "0x06fdde03"); err == nil { // name()
		token.Name = decodeString(result)
	}

	// Get symbol
	if result, err := a.callContract(ctx, tokenAddress, "0x95d89b41"); err == nil { // symbol()
		token.Symbol = decodeString(result)
	}

	// Get decimals
	if result, err := a.callContract(ctx, tokenAddress, "0x313ce567"); err == nil { // decimals()
		token.Decimals = uint8(hexToUint64(result))
	}

	// Get totalSupply
	if result, err := a.callContract(ctx, tokenAddress, "0x18160ddd"); err == nil { // totalSupply()
		token.TotalSupply = hexToBigInt(result).String()
	}

	return token, nil
}

// callContract makes an eth_call to a contract
func (a *Adapter) callContract(ctx context.Context, to, data string) (string, error) {
	result, err := a.call(ctx, "eth_call", []interface{}{
		map[string]string{"to": to, "data": data},
		"latest",
	})
	if err != nil {
		return "", err
	}

	var resultHex string
	if err := json.Unmarshal(result, &resultHex); err != nil {
		return "", err
	}

	return resultHex, nil
}

// decodeString decodes an ABI-encoded string
func decodeString(data string) string {
	data = strings.TrimPrefix(data, "0x")
	if len(data) < 128 {
		return ""
	}

	// Skip offset (32 bytes) and get length
	lengthHex := data[64:128]
	length := hexToUint64("0x" + lengthHex)

	if length == 0 || len(data) < 128+int(length*2) {
		return ""
	}

	// Decode string bytes
	strData := data[128 : 128+length*2]
	decoded, err := hex.DecodeString(strData)
	if err != nil {
		return ""
	}

	return string(decoded)
}

// GetInternalTransactions returns internal transactions for a transaction
func (a *Adapter) GetInternalTransactions(ctx context.Context, db *sql.DB, txHash string) ([]InternalTransaction, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, tx_hash, block_number, trace_index, trace_address, call_type,
		       tx_from, tx_to, value, gas, gas_used, input, output, error,
		       created_contract_address, created_contract_code, init, timestamp
		FROM cchain_internal_transactions
		WHERE tx_hash = $1
		ORDER BY trace_index
	`, txHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var itxs []InternalTransaction
	for rows.Next() {
		var itx InternalTransaction
		var traceAddrStr string
		var createdAddr, createdCode, init sql.NullString
		var errStr sql.NullString

		err := rows.Scan(
			&itx.ID, &itx.TxHash, &itx.BlockNumber, &itx.TraceIndex, &traceAddrStr,
			&itx.CallType, &itx.From, &itx.To, &itx.Value, &itx.Gas, &itx.GasUsed,
			&itx.Input, &itx.Output, &errStr, &createdAddr, &createdCode, &init, &itx.Timestamp,
		)
		if err != nil {
			continue
		}

		itx.TraceAddress = postgresArrayToIntSlice(traceAddrStr)
		if errStr.Valid {
			itx.Error = errStr.String
		}
		if createdAddr.Valid {
			itx.CreatedContractAddress = createdAddr.String
		}
		if createdCode.Valid {
			itx.CreatedContractCode = createdCode.String
		}
		if init.Valid {
			itx.Init = init.String
		}

		itxs = append(itxs, itx)
	}

	return itxs, nil
}

// postgresArrayToIntSlice converts a PostgreSQL array string to an int slice
func postgresArrayToIntSlice(s string) []int {
	s = strings.Trim(s, "{}")
	if s == "" {
		return []int{}
	}

	parts := strings.Split(s, ",")
	result := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var n int
		fmt.Sscanf(p, "%d", &n)
		result = append(result, n)
	}
	return result
}

// GetAddressCoinBalanceHistory returns coin balance history for an address
func (a *Adapter) GetAddressCoinBalanceHistory(ctx context.Context, db *sql.DB, address string, limit int) ([]AddressCoinBalance, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT address_hash, block_number, value, value_fetched_at
		FROM cchain_address_coin_balances
		WHERE address_hash = $1 AND value IS NOT NULL
		ORDER BY block_number DESC
		LIMIT $2
	`, strings.ToLower(address), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var balances []AddressCoinBalance
	for rows.Next() {
		var acb AddressCoinBalance
		var value sql.NullString
		var fetchedAt sql.NullTime

		err := rows.Scan(&acb.AddressHash, &acb.BlockNumber, &value, &fetchedAt)
		if err != nil {
			continue
		}

		if value.Valid {
			acb.Value = value.String
		}
		if fetchedAt.Valid {
			acb.ValueFetchedAt = fetchedAt.Time
		}

		balances = append(balances, acb)
	}

	return balances, nil
}

// GetTokenBalanceHistory returns token balance history for an address
func (a *Adapter) GetTokenBalanceHistory(ctx context.Context, db *sql.DB, address string, limit int) ([]TokenBalance, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT token_address, holder_address, balance, token_id, block_number, created_at
		FROM cchain_token_balance_history
		WHERE holder_address = $1
		ORDER BY block_number DESC
		LIMIT $2
	`, strings.ToLower(address), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var balances []TokenBalance
	for rows.Next() {
		var tb TokenBalance
		var tokenID sql.NullString
		var createdAt time.Time

		err := rows.Scan(&tb.TokenAddress, &tb.HolderAddress, &tb.Balance, &tokenID, &tb.BlockNumber, &createdAt)
		if err != nil {
			continue
		}

		if tokenID.Valid {
			tb.TokenID = tokenID.String
		}
		tb.UpdatedAt = createdAt

		balances = append(balances, tb)
	}

	return balances, nil
}

// GetTokenHolders returns all holders of a token with non-zero balances
func (a *Adapter) GetTokenHolders(ctx context.Context, db *sql.DB, tokenAddress string, limit, offset int) ([]TokenBalance, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT token_address, holder_address, balance, token_id, block_number, updated_at
		FROM cchain_token_balances
		WHERE token_address = $1 AND balance != '0'
		ORDER BY CAST(balance AS NUMERIC) DESC
		LIMIT $2 OFFSET $3
	`, strings.ToLower(tokenAddress), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holders []TokenBalance
	for rows.Next() {
		var tb TokenBalance
		var tokenID sql.NullString

		err := rows.Scan(&tb.TokenAddress, &tb.HolderAddress, &tb.Balance, &tokenID, &tb.BlockNumber, &tb.UpdatedAt)
		if err != nil {
			continue
		}

		if tokenID.Valid {
			tb.TokenID = tokenID.String
		}

		holders = append(holders, tb)
	}

	return holders, nil
}

// TraceAddressToString converts a trace address to a string representation
func TraceAddressToString(addr []int) string {
	if len(addr) == 0 {
		return "[]"
	}
	strs := make([]string, len(addr))
	for i, v := range addr {
		strs[i] = fmt.Sprintf("%d", v)
	}
	return "[" + strings.Join(strs, ",") + "]"
}

// SortInternalTransactionsByTraceAddress sorts internal transactions by trace address
func SortInternalTransactionsByTraceAddress(itxs []InternalTransaction) {
	sort.Slice(itxs, func(i, j int) bool {
		a, b := itxs[i].TraceAddress, itxs[j].TraceAddress
		minLen := len(a)
		if len(b) < minLen {
			minLen = len(b)
		}
		for k := 0; k < minLen; k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return len(a) < len(b)
	})
}
