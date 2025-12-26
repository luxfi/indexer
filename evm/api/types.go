// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package api provides REST, RPC, GraphQL, and WebSocket APIs for the EVM indexer.
// API design matches Blockscout v2 for frontend compatibility.
package api

import (
	"encoding/json"
	"math/big"
	"time"
)

// Pagination parameters
type Pagination struct {
	Page      int `json:"page"`
	PageSize  int `json:"page_size"`
	TotalRows int `json:"total_rows"`
}

// NextPageParams for cursor-based pagination (Blockscout compatible)
type NextPageParams struct {
	BlockNumber      *uint64 `json:"block_number,omitempty"`
	Index            *int    `json:"index,omitempty"`
	ItemsCount       int     `json:"items_count,omitempty"`
	TransactionIndex *int    `json:"transaction_index,omitempty"`
	Hash             string  `json:"hash,omitempty"`
}

// PaginatedResponse wraps items with pagination info
type PaginatedResponse struct {
	Items          interface{}     `json:"items"`
	NextPageParams *NextPageParams `json:"next_page_params,omitempty"`
}

// Block represents a block in Blockscout v2 format
type Block struct {
	Height              uint64          `json:"height"`
	Hash                string          `json:"hash"`
	ParentHash          string          `json:"parent_hash"`
	Timestamp           time.Time       `json:"timestamp"`
	TransactionCount    int             `json:"tx_count"`
	GasUsed             string          `json:"gas_used"`
	GasLimit            string          `json:"gas_limit"`
	BaseFeePerGas       string          `json:"base_fee_per_gas,omitempty"`
	BurntFees           string          `json:"burnt_fees,omitempty"`
	Priority            string          `json:"priority_fee,omitempty"`
	Size                uint64          `json:"size"`
	Miner               *Address        `json:"miner"`
	Nonce               string          `json:"nonce,omitempty"`
	Difficulty          string          `json:"difficulty,omitempty"`
	TotalDifficulty     string          `json:"total_difficulty,omitempty"`
	StateRoot           string          `json:"state_root,omitempty"`
	TransactionsRoot    string          `json:"transactions_root,omitempty"`
	ReceiptsRoot        string          `json:"receipts_root,omitempty"`
	Withdrawals         []Withdrawal    `json:"withdrawals,omitempty"`
	Rewards             []BlockReward   `json:"rewards,omitempty"`
	Type                string          `json:"type"` // block, reorg, uncle
	Confirmations       int             `json:"confirmations,omitempty"`
	InternalTxsIndexed  bool            `json:"has_internal_transactions"`
	TokenTransferCount  int             `json:"token_transfer_count,omitempty"`
}

// Withdrawal represents a beacon chain withdrawal
type Withdrawal struct {
	Index          uint64   `json:"index"`
	ValidatorIndex uint64   `json:"validator_index"`
	Address        *Address `json:"address"`
	Amount         string   `json:"amount"`
}

// BlockReward represents a block reward
type BlockReward struct {
	Type    string   `json:"type"` // block, uncle, emission
	Address *Address `json:"address"`
	Amount  string   `json:"amount"`
}

// Transaction in Blockscout v2 format
type Transaction struct {
	Hash                 string           `json:"hash"`
	BlockHash            string           `json:"block_hash,omitempty"`
	BlockNumber          *uint64          `json:"block_number,omitempty"`
	Timestamp            *time.Time       `json:"timestamp,omitempty"`
	Confirmations        int              `json:"confirmations,omitempty"`
	From                 *Address         `json:"from"`
	To                   *Address         `json:"to,omitempty"`
	CreatedContract      *Address         `json:"created_contract,omitempty"`
	Value                string           `json:"value"`
	Gas                  uint64           `json:"gas_limit"`
	GasPrice             string           `json:"gas_price"`
	GasUsed              *uint64          `json:"gas_used,omitempty"`
	MaxFeePerGas         string           `json:"max_fee_per_gas,omitempty"`
	MaxPriorityFee       string           `json:"max_priority_fee_per_gas,omitempty"`
	BaseFee              string           `json:"base_fee_per_gas,omitempty"`
	TransactionFee       string           `json:"fee,omitempty"`
	BurntFee             string           `json:"tx_burnt_fee,omitempty"`
	Nonce                uint64           `json:"nonce"`
	Position             *int             `json:"position,omitempty"`
	TransactionIndex     *int             `json:"transaction_index,omitempty"`
	Input                string           `json:"raw_input"`
	DecodedInput         *DecodedInput    `json:"decoded_input,omitempty"`
	Type                 int              `json:"type"`
	Status               string           `json:"status,omitempty"` // ok, error, pending
	Result               string           `json:"result,omitempty"` // success, revert, out of gas
	RevertReason         string           `json:"revert_reason,omitempty"`
	Method               string           `json:"method,omitempty"`
	TokenTransfers       []TokenTransfer  `json:"token_transfers,omitempty"`
	TokenTransferCount   int              `json:"token_transfers_count,omitempty"`
	InternalTxsCount     int              `json:"internal_txs_count,omitempty"`
	ExchangeRate         string           `json:"exchange_rate,omitempty"`
	Actions              []TxAction       `json:"actions,omitempty"`
	HasErrorInternalTxs  bool             `json:"has_error_in_internal_txs"`
}

// DecodedInput represents decoded function call
type DecodedInput struct {
	MethodID   string          `json:"method_id"`
	MethodCall string          `json:"method_call"`
	Parameters []DecodedParam  `json:"parameters,omitempty"`
}

// DecodedParam represents a decoded parameter
type DecodedParam struct {
	Name  string      `json:"name"`
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
}

// TxAction represents a transaction action (swap, bridge, etc.)
type TxAction struct {
	Protocol string                 `json:"protocol"`
	Type     string                 `json:"type"`
	Data     map[string]interface{} `json:"data"`
}

// Address in Blockscout v2 format
type Address struct {
	Hash                 string          `json:"hash"`
	Name                 string          `json:"name,omitempty"`
	ENS                  string          `json:"ens_domain_name,omitempty"`
	IsContract           bool            `json:"is_contract"`
	IsVerified           bool            `json:"is_verified,omitempty"`
	IsScam               bool            `json:"is_scam,omitempty"`
	IsProxy              bool            `json:"is_proxy,omitempty"`
	Implementation       *Address        `json:"implementation,omitempty"`
	ImplementationName   string          `json:"implementation_name,omitempty"`
	Balance              string          `json:"coin_balance,omitempty"`
	FetchedCoinBalance   string          `json:"fetched_coin_balance,omitempty"`
	TransactionCount     *uint64         `json:"tx_count,omitempty"`
	TokenBalance         string          `json:"token_balance,omitempty"`
	TokenCount           *int            `json:"token_count,omitempty"`
	GasUsed              *uint64         `json:"gas_used,omitempty"`
	BlockNumberCreated   *uint64         `json:"block_number_balance_updated_at,omitempty"`
	Creator              *Address        `json:"creator_address,omitempty"`
	CreationTxHash       string          `json:"creation_tx_hash,omitempty"`
	Token                *Token          `json:"token,omitempty"`
	HasDecompiledCode    bool            `json:"has_decompiled_code,omitempty"`
	HasValidatedBlocks   bool            `json:"has_validated_blocks,omitempty"`
	HasLogs              bool            `json:"has_logs,omitempty"`
	HasTokenTransfers    bool            `json:"has_token_transfers,omitempty"`
	WatchlistNames       []WatchlistName `json:"watchlist_names,omitempty"`
	PrivateTags          []AddressTag    `json:"private_tags,omitempty"`
	PublicTags           []AddressTag    `json:"public_tags,omitempty"`
}

// WatchlistName for user's address labels
type WatchlistName struct {
	Label       string `json:"label"`
	DisplayName string `json:"display_name"`
}

// AddressTag for public/private tagging
type AddressTag struct {
	Label       string `json:"label"`
	DisplayName string `json:"display_name"`
	Type        string `json:"address_tag,omitempty"`
	URL         string `json:"url,omitempty"`
}

// Token in Blockscout v2 format
type Token struct {
	Address          string  `json:"address"`
	Name             string  `json:"name"`
	Symbol           string  `json:"symbol"`
	Decimals         *uint8  `json:"decimals,omitempty"`
	TotalSupply      string  `json:"total_supply,omitempty"`
	CirculatingCap   string  `json:"circulating_market_cap,omitempty"`
	Type             string  `json:"type"` // ERC-20, ERC-721, ERC-1155
	HolderCount      *uint64 `json:"holders,omitempty"`
	TransferCount    *uint64 `json:"transfers_count,omitempty"`
	ExchangeRate     string  `json:"exchange_rate,omitempty"`
	IconURL          string  `json:"icon_url,omitempty"`
	Volume24h        string  `json:"volume_24h,omitempty"`
	IsVerified       bool    `json:"is_verified_via_admin_panel,omitempty"`
}

// TokenBalance for address token holdings
type TokenBalance struct {
	Token        *Token  `json:"token"`
	TokenID      string  `json:"token_id,omitempty"`
	Value        string  `json:"value"`
	TokenInstance *NFT   `json:"token_instance,omitempty"`
}

// TokenTransfer in Blockscout v2 format
type TokenTransfer struct {
	TxHash       string     `json:"tx_hash"`
	BlockHash    string     `json:"block_hash,omitempty"`
	BlockNumber  uint64     `json:"block_number"`
	LogIndex     int        `json:"log_index"`
	Timestamp    *time.Time `json:"timestamp,omitempty"`
	From         *Address   `json:"from"`
	To           *Address   `json:"to"`
	Token        *Token     `json:"token"`
	TokenID      string     `json:"token_id,omitempty"`
	Total        *TokenTotal `json:"total,omitempty"`
	TokenIDs     []string   `json:"token_ids,omitempty"` // ERC-1155 batch
	Amounts      []string   `json:"amounts,omitempty"`   // ERC-1155 batch
	Type         string     `json:"type,omitempty"`      // token_minting, token_burning, token_transfer
	Method       string     `json:"method,omitempty"`
}

// TokenTotal for transfer value/tokenId
type TokenTotal struct {
	Value    string `json:"value,omitempty"`
	Decimals *uint8 `json:"decimals,omitempty"`
	TokenID  string `json:"token_id,omitempty"`
}

// InternalTransaction for traces
type InternalTransaction struct {
	TxHash      string     `json:"transaction_hash"`
	BlockNumber uint64     `json:"block_number"`
	BlockHash   string     `json:"block_hash,omitempty"`
	Index       int        `json:"index"`
	Timestamp   *time.Time `json:"timestamp,omitempty"`
	Type        string     `json:"type"` // call, create, suicide, reward
	CallType    string     `json:"call_type,omitempty"` // call, delegatecall, staticcall
	From        *Address   `json:"from"`
	To          *Address   `json:"to,omitempty"`
	Value       string     `json:"value"`
	Gas         uint64     `json:"gas_limit"`
	GasUsed     *uint64    `json:"gas_used,omitempty"`
	Input       string     `json:"input,omitempty"`
	Output      string     `json:"output,omitempty"`
	Error       string     `json:"error,omitempty"`
	Success     bool       `json:"success"`
	Created     *Address   `json:"created_contract,omitempty"`
}

// Log represents an event log
type Log struct {
	TxHash      string     `json:"tx_hash"`
	BlockNumber uint64     `json:"block_number"`
	BlockHash   string     `json:"block_hash"`
	Index       int        `json:"index"`
	Address     *Address   `json:"address"`
	Data        string     `json:"data"`
	Topics      []string   `json:"topics"`
	Decoded     *DecodedLog `json:"decoded,omitempty"`
}

// DecodedLog represents a decoded event
type DecodedLog struct {
	MethodID   string         `json:"method_id"`
	MethodCall string         `json:"method_call"`
	Parameters []DecodedParam `json:"parameters,omitempty"`
}

// SmartContract in Blockscout v2 format
type SmartContract struct {
	Address                string          `json:"address_hash"`
	Name                   string          `json:"name,omitempty"`
	CompilerVersion        string          `json:"compiler_version,omitempty"`
	OptimizationEnabled    bool            `json:"optimization_enabled"`
	OptimizationRuns       int             `json:"optimization_runs,omitempty"`
	EVMVersion             string          `json:"evm_version,omitempty"`
	SourceCode             string          `json:"source_code,omitempty"`
	ABI                    json.RawMessage `json:"abi,omitempty"`
	ConstructorArgs        string          `json:"constructor_arguments,omitempty"`
	DecodedConstructorArgs []DecodedParam  `json:"decoded_constructor_arguments,omitempty"`
	IsVerified             bool            `json:"is_verified"`
	VerifiedAt             *time.Time      `json:"verified_at,omitempty"`
	IsProxy                bool            `json:"is_proxy"`
	ProxyType              string          `json:"proxy_type,omitempty"`
	Implementations        []Implementation `json:"implementations,omitempty"`
	ContractSourceCode     []SourceFile    `json:"contract_source_code,omitempty"`
	ExternalLibraries      []Library       `json:"external_libraries,omitempty"`
	Language               string          `json:"language,omitempty"` // solidity, vyper
	LicenseType            string          `json:"license_type,omitempty"`
	IsFullMatch            bool            `json:"is_fully_verified"`
	IsPartialMatch         bool            `json:"is_partially_verified,omitempty"`
}

// Implementation for proxy contracts
type Implementation struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
}

// SourceFile for multi-file contracts
type SourceFile struct {
	FileName    string `json:"file_name"`
	SourceCode  string `json:"source_code"`
}

// Library for external libraries
type Library struct {
	Name    string `json:"name"`
	Address string `json:"address_hash"`
}

// NFT represents an NFT instance
type NFT struct {
	ID         string                 `json:"id"`
	Token      *Token                 `json:"token"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	Owner      *Address               `json:"owner,omitempty"`
	IsUnique   bool                   `json:"is_unique,omitempty"`
	ImageURL   string                 `json:"image_url,omitempty"`
	AnimationURL string               `json:"animation_url,omitempty"`
	ExternalURL string                `json:"external_app_url,omitempty"`
}

// ChainStats for /api/v2/stats
type ChainStats struct {
	TotalBlocks            int64   `json:"total_blocks"`
	TotalTransactions      int64   `json:"total_transactions"`
	TotalAddresses         int64   `json:"total_addresses"`
	TotalContracts         int64   `json:"total_contracts,omitempty"`
	TotalTokens            int64   `json:"total_tokens,omitempty"`
	TotalTokenTransfers    int64   `json:"total_token_transfers,omitempty"`
	AverageBlockTime       float64 `json:"average_block_time"`
	CoinPrice              string  `json:"coin_price,omitempty"`
	CoinPriceChange        float64 `json:"coin_price_change_percentage,omitempty"`
	TotalGasUsed           string  `json:"total_gas_used,omitempty"`
	GasPrice               string  `json:"gas_prices,omitempty"`
	NetworkUtilization     float64 `json:"network_utilization_percentage,omitempty"`
	TxnsToday              int64   `json:"transactions_today,omitempty"`
	MarketCap              string  `json:"market_cap,omitempty"`
	TVL                    string  `json:"tvl,omitempty"`
}

// GasPrice information
type GasPrice struct {
	Slow     GasPriceTier `json:"slow"`
	Average  GasPriceTier `json:"average"`
	Fast     GasPriceTier `json:"fast"`
}

// GasPriceTier for gas estimates
type GasPriceTier struct {
	Price       string  `json:"price"`
	FiatPrice   string  `json:"fiat_price,omitempty"`
	BaseFee     string  `json:"base_fee,omitempty"`
	Priority    string  `json:"priority_fee,omitempty"`
	Time        float64 `json:"time,omitempty"` // estimated seconds
}

// SearchResult for universal search
type SearchResult struct {
	Type            string   `json:"type"` // address, contract, token, transaction, block, ens
	Address         string   `json:"address,omitempty"`
	Hash            string   `json:"hash,omitempty"`
	BlockNumber     *uint64  `json:"block_number,omitempty"`
	Name            string   `json:"name,omitempty"`
	Symbol          string   `json:"symbol,omitempty"`
	TokenType       string   `json:"token_type,omitempty"`
	ExchangeRate    string   `json:"exchange_rate,omitempty"`
	IsSmartContract bool     `json:"is_smart_contract_verified,omitempty"`
	URL             string   `json:"url,omitempty"`
	Priority        int      `json:"priority,omitempty"`
}

// Error response format
type ErrorResponse struct {
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

// Helper for big.Int JSON handling
type BigInt big.Int

func (b *BigInt) String() string {
	if b == nil {
		return "0"
	}
	return (*big.Int)(b).String()
}

func (b *BigInt) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.String())
}

func (b *BigInt) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	bi := new(big.Int)
	bi.SetString(s, 10)
	*b = BigInt(*bi)
	return nil
}
