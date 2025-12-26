// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package evm provides advanced EVM indexing types for Phase 5.
// Includes ERC-4337, MEV, EIP-4844, and multi-chain support.
package evm

import (
	"math/big"
	"time"
)

// Chain IDs for supported Lux chains
const (
	ChainIDLuxMainnet   = 96369
	ChainIDLuxTestnet   = 96368
	ChainIDZooMainnet   = 200200
	ChainIDZooTestnet   = 200201
	ChainIDHanzoMainnet = 36963
)

// Block represents a block for indexing purposes
type Block struct {
	Number           uint64    `json:"number"`
	Hash             string    `json:"hash"`
	ParentHash       string    `json:"parentHash"`
	Timestamp        time.Time `json:"timestamp"`
	Miner            string    `json:"miner"`
	Difficulty       *big.Int  `json:"difficulty,omitempty"`
	TotalDifficulty  *big.Int  `json:"totalDifficulty,omitempty"`
	GasLimit         uint64    `json:"gasLimit"`
	GasUsed          uint64    `json:"gasUsed"`
	BaseFeePerGas    *big.Int  `json:"baseFeePerGas,omitempty"`
	TransactionCount int       `json:"transactionCount"`
	UncleCount       int       `json:"uncleCount"`
}

// EntryPoint addresses for ERC-4337
const (
	EntryPointV06 = "0x5ff137d4b0fdcd49dca30c7cf57e578a026d2789"
	EntryPointV07 = "0x0000000071727de22e5e9d8baf0edac6f37da032"
)

// Event signatures for ERC-4337
var (
	// UserOperationEvent(bytes32 indexed userOpHash, address indexed sender, address indexed paymaster, uint256 nonce, bool success, uint256 actualGasCost, uint256 actualGasUsed)
	TopicUserOperationEvent = "0x49628fd1471006c1482da88028e9ce4dbb080b815c9b0344d39e5a8e6ec1419f"
	// AccountDeployed(bytes32 indexed userOpHash, address indexed sender, address factory, address paymaster)
	TopicAccountDeployed = "0xd51a9c61267aa6196961883ecf5ff2da6619c37dac0fa92122513fb32c032d2d"
	// BeforeExecution()
	TopicBeforeExecution = "0xbb47ee3e183a558b1a2ff0874b079f3fc5478b7454eacf2bfc5af2ff5878f972"
)

// UserOperation represents an ERC-4337 user operation
type UserOperation struct {
	Hash                 string    `json:"hash"`
	Sender               string    `json:"sender"`
	Nonce                string    `json:"nonce"`
	InitCode             string    `json:"initCode,omitempty"`
	CallData             string    `json:"callData"`
	CallGasLimit         uint64    `json:"callGasLimit"`
	VerificationGasLimit uint64    `json:"verificationGasLimit"`
	PreVerificationGas   uint64    `json:"preVerificationGas"`
	MaxFeePerGas         string    `json:"maxFeePerGas"`
	MaxPriorityFeePerGas string    `json:"maxPriorityFeePerGas"`
	PaymasterAndData     string    `json:"paymasterAndData,omitempty"`
	Signature            string    `json:"signature"`
	EntryPoint           string    `json:"entryPoint"`
	BlockNumber          uint64    `json:"blockNumber"`
	BlockHash            string    `json:"blockHash"`
	TransactionHash      string    `json:"transactionHash"`
	BundlerAddress       string    `json:"bundlerAddress"`
	PaymasterAddress     string    `json:"paymasterAddress,omitempty"`
	FactoryAddress       string    `json:"factoryAddress,omitempty"`
	Status               uint8     `json:"status"` // 1 = success, 0 = reverted
	RevertReason         string    `json:"revertReason,omitempty"`
	ActualGasCost        string    `json:"actualGasCost"`
	ActualGasUsed        uint64    `json:"actualGasUsed"`
	Timestamp            time.Time `json:"timestamp"`
	EntryPointVersion    string    `json:"entryPointVersion"` // v0.6 or v0.7
}

// Bundler represents an ERC-4337 bundler
type Bundler struct {
	Address            string    `json:"address"`
	TotalOperations    uint64    `json:"totalOperations"`
	TotalBundles       uint64    `json:"totalBundles"`
	TotalGasSponsored  string    `json:"totalGasSponsored"`
	SuccessRate        float64   `json:"successRate"`
	FirstSeenBlock     uint64    `json:"firstSeenBlock"`
	LastSeenBlock      uint64    `json:"lastSeenBlock"`
	FirstSeenTimestamp time.Time `json:"firstSeenTimestamp"`
	LastSeenTimestamp  time.Time `json:"lastSeenTimestamp"`
}

// Paymaster represents an ERC-4337 paymaster
type Paymaster struct {
	Address            string    `json:"address"`
	TotalOperations    uint64    `json:"totalOperations"`
	TotalGasSponsored  string    `json:"totalGasSponsored"`
	TotalEthSponsored  string    `json:"totalEthSponsored"`
	UniqueAccounts     uint64    `json:"uniqueAccounts"`
	FirstSeenBlock     uint64    `json:"firstSeenBlock"`
	LastSeenBlock      uint64    `json:"lastSeenBlock"`
	FirstSeenTimestamp time.Time `json:"firstSeenTimestamp"`
	LastSeenTimestamp  time.Time `json:"lastSeenTimestamp"`
}

// AccountFactory represents an ERC-4337 account factory
type AccountFactory struct {
	Address            string    `json:"address"`
	TotalAccounts      uint64    `json:"totalAccounts"`
	ImplementationType string    `json:"implementationType,omitempty"` // Safe, Kernel, SimpleAccount, etc.
	FirstSeenBlock     uint64    `json:"firstSeenBlock"`
	LastSeenBlock      uint64    `json:"lastSeenBlock"`
	FirstSeenTimestamp time.Time `json:"firstSeenTimestamp"`
	LastSeenTimestamp  time.Time `json:"lastSeenTimestamp"`
}

// SmartAccount represents an ERC-4337 smart contract account
type SmartAccount struct {
	Address          string    `json:"address"`
	FactoryAddress   string    `json:"factoryAddress,omitempty"`
	DeploymentTxHash string    `json:"deploymentTxHash,omitempty"`
	DeploymentBlock  uint64    `json:"deploymentBlock"`
	TotalOperations  uint64    `json:"totalOperations"`
	TotalGasUsed     string    `json:"totalGasUsed"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// Bundle represents a bundler's transaction containing multiple UserOperations
type Bundle struct {
	TransactionHash string    `json:"transactionHash"`
	BlockNumber     uint64    `json:"blockNumber"`
	BlockHash       string    `json:"blockHash"`
	BundlerAddress  string    `json:"bundlerAddress"`
	OperationCount  int       `json:"operationCount"`
	OperationHashes []string  `json:"operationHashes"`
	TotalGasUsed    uint64    `json:"totalGasUsed"`
	GasPrice        string    `json:"gasPrice"`
	Timestamp       time.Time `json:"timestamp"`
}

// MEV Types

// MEVType identifies the type of MEV activity
type MEVType string

const (
	MEVTypeSandwich    MEVType = "sandwich"
	MEVTypeArbitrage   MEVType = "arbitrage"
	MEVTypeLiquidation MEVType = "liquidation"
	MEVTypeJIT         MEVType = "jit_liquidity" // Just-in-time liquidity
	MEVTypeBackrun     MEVType = "backrun"
	MEVTypeFrontrun    MEVType = "frontrun"
	MEVTypePrivate     MEVType = "private" // Private/flashbots tx
)

// MEVTransaction represents a detected MEV transaction
type MEVTransaction struct {
	ID               string    `json:"id"`
	TransactionHash  string    `json:"transactionHash"`
	BlockNumber      uint64    `json:"blockNumber"`
	MEVType          MEVType   `json:"mevType"`
	ExtractorAddress string    `json:"extractorAddress"`
	VictimAddress    string    `json:"victimAddress,omitempty"`
	Protocol         string    `json:"protocol,omitempty"` // e.g., Uniswap, Aave
	ProfitETH        string    `json:"profitEth"`
	ProfitUSD        string    `json:"profitUsd,omitempty"`
	GasCostETH       string    `json:"gasCostEth"`
	RelatedTxHashes  []string  `json:"relatedTxHashes,omitempty"` // For sandwiches
	Timestamp        time.Time `json:"timestamp"`
	Confidence       float64   `json:"confidence"` // Detection confidence 0-1
}

// SandwichAttack represents a detected sandwich attack
type SandwichAttack struct {
	ID                string    `json:"id"`
	BlockNumber       uint64    `json:"blockNumber"`
	FrontrunTxHash    string    `json:"frontrunTxHash"`
	VictimTxHash      string    `json:"victimTxHash"`
	BackrunTxHash     string    `json:"backrunTxHash"`
	AttackerAddress   string    `json:"attackerAddress"`
	VictimAddress     string    `json:"victimAddress"`
	TokenAddress      string    `json:"tokenAddress"`
	PoolAddress       string    `json:"poolAddress"`
	VictimLossETH     string    `json:"victimLossEth"`
	AttackerProfitETH string    `json:"attackerProfitEth"`
	Timestamp         time.Time `json:"timestamp"`
}

// ArbitrageTx represents a detected arbitrage transaction
type ArbitrageTx struct {
	ID                string    `json:"id"`
	TransactionHash   string    `json:"transactionHash"`
	BlockNumber       uint64    `json:"blockNumber"`
	ArbitragerAddress string    `json:"arbitragerAddress"`
	ProfitETH         string    `json:"profitEth"`
	ProfitUSD         string    `json:"profitUsd,omitempty"`
	PathLength        int       `json:"pathLength"` // Number of swaps
	Tokens            []string  `json:"tokens"`
	Pools             []string  `json:"pools"`
	IsAtomic          bool      `json:"isAtomic"` // Flash loan based
	Timestamp         time.Time `json:"timestamp"`
}

// LiquidationTx represents a detected liquidation
type LiquidationTx struct {
	ID                  string    `json:"id"`
	TransactionHash     string    `json:"transactionHash"`
	BlockNumber         uint64    `json:"blockNumber"`
	LiquidatorAddress   string    `json:"liquidatorAddress"`
	BorrowerAddress     string    `json:"borrowerAddress"`
	Protocol            string    `json:"protocol"` // Aave, Compound, etc.
	CollateralToken     string    `json:"collateralToken"`
	DebtToken           string    `json:"debtToken"`
	CollateralSeized    string    `json:"collateralSeized"`
	DebtRepaid          string    `json:"debtRepaid"`
	LiquidatorProfitETH string    `json:"liquidatorProfitEth"`
	Timestamp           time.Time `json:"timestamp"`
}

// EIP-4844 Blob Types

// BlobTransaction represents an EIP-4844 type 3 transaction
type BlobTransaction struct {
	Hash                string    `json:"hash"`
	BlockNumber         uint64    `json:"blockNumber"`
	BlockHash           string    `json:"blockHash"`
	From                string    `json:"from"`
	To                  string    `json:"to"`
	Value               string    `json:"value"`
	MaxFeePerBlobGas    string    `json:"maxFeePerBlobGas"`
	BlobGasUsed         uint64    `json:"blobGasUsed"`
	BlobGasPrice        string    `json:"blobGasPrice"`
	BlobVersionedHashes []string  `json:"blobVersionedHashes"`
	BlobCount           int       `json:"blobCount"`
	TransactionIndex    uint64    `json:"transactionIndex"`
	Timestamp           time.Time `json:"timestamp"`
}

// BlobData represents stored blob data
type BlobData struct {
	VersionedHash   string    `json:"versionedHash"`
	Commitment      string    `json:"commitment"`     // KZG commitment
	Proof           string    `json:"proof"`          // KZG proof
	Data            string    `json:"data,omitempty"` // Actual blob data (optional, may be pruned)
	Size            uint64    `json:"size"`
	TransactionHash string    `json:"transactionHash"`
	BlockNumber     uint64    `json:"blockNumber"`
	BlobIndex       uint64    `json:"blobIndex"`
	StorageURI      string    `json:"storageUri,omitempty"` // Link to external blob storage
	Timestamp       time.Time `json:"timestamp"`
}

// Block enhancement types

// Uncle represents an uncle/ommer block
type Uncle struct {
	Hash        string    `json:"hash"`
	BlockNumber uint64    `json:"blockNumber"` // Block that included this uncle
	UncleIndex  uint64    `json:"uncleIndex"`
	ParentHash  string    `json:"parentHash"`
	Miner       string    `json:"miner"`
	Difficulty  string    `json:"difficulty"`
	GasLimit    uint64    `json:"gasLimit"`
	GasUsed     uint64    `json:"gasUsed"`
	Timestamp   time.Time `json:"timestamp"`
	Reward      string    `json:"reward"`
}

// BlockReward represents validator/miner rewards
type BlockReward struct {
	BlockNumber uint64    `json:"blockNumber"`
	BlockHash   string    `json:"blockHash"`
	Validator   string    `json:"validator"`
	BaseReward  string    `json:"baseReward"`
	TxFeeReward string    `json:"txFeeReward"`
	UncleReward string    `json:"uncleReward"`
	TotalReward string    `json:"totalReward"`
	BurntFees   string    `json:"burntFees"` // EIP-1559
	Timestamp   time.Time `json:"timestamp"`
}

// Withdrawal represents an EIP-4895 withdrawal
type Withdrawal struct {
	Index          uint64    `json:"index"`
	ValidatorIndex uint64    `json:"validatorIndex"`
	Address        string    `json:"address"`
	Amount         string    `json:"amount"` // In Gwei
	BlockNumber    uint64    `json:"blockNumber"`
	BlockHash      string    `json:"blockHash"`
	Timestamp      time.Time `json:"timestamp"`
}

// Enhanced block with all new fields
type EnhancedBlock struct {
	Hash             string    `json:"hash"`
	ParentHash       string    `json:"parentHash"`
	Number           uint64    `json:"number"`
	Timestamp        time.Time `json:"timestamp"`
	Miner            string    `json:"miner"`
	Difficulty       string    `json:"difficulty"`
	TotalDifficulty  string    `json:"totalDifficulty"`
	Size             uint64    `json:"size"`
	GasLimit         uint64    `json:"gasLimit"`
	GasUsed          uint64    `json:"gasUsed"`
	BaseFeePerGas    string    `json:"baseFeePerGas,omitempty"`
	ExtraData        string    `json:"extraData"`
	StateRoot        string    `json:"stateRoot"`
	TransactionsRoot string    `json:"transactionsRoot"`
	ReceiptsRoot     string    `json:"receiptsRoot"`
	LogsBloom        string    `json:"logsBloom"`
	TxCount          int       `json:"txCount"`
	UncleCount       int       `json:"uncleCount"`
	UncleHashes      []string  `json:"uncleHashes,omitempty"`
	// EIP-4844 fields
	BlobGasUsed      uint64 `json:"blobGasUsed,omitempty"`
	ExcessBlobGas    uint64 `json:"excessBlobGas,omitempty"`
	ParentBeaconRoot string `json:"parentBeaconBlockRoot,omitempty"`
	// EIP-4895 withdrawals
	Withdrawals     []Withdrawal `json:"withdrawals,omitempty"`
	WithdrawalsRoot string       `json:"withdrawalsRoot,omitempty"`
	// Rewards
	Reward *BlockReward `json:"reward,omitempty"`
}

// Transaction enhancement types
// Note: PendingTransaction is defined in pending.go

// ReplacedTransaction tracks transaction replacements
type ReplacedTransaction struct {
	OriginalHash    string    `json:"originalHash"`
	ReplacementHash string    `json:"replacementHash"`
	BlockNumber     uint64    `json:"blockNumber"`
	From            string    `json:"from"`
	Nonce           uint64    `json:"nonce"`
	OldGasPrice     string    `json:"oldGasPrice"`
	NewGasPrice     string    `json:"newGasPrice"`
	ReplacementType string    `json:"replacementType"` // speedup, cancel
	Timestamp       time.Time `json:"timestamp"`
}

// TransactionRevertReason contains decoded revert information
type TransactionRevertReason struct {
	TransactionHash string `json:"transactionHash"`
	RevertReason    string `json:"revertReason"`
	DecodedReason   string `json:"decodedReason,omitempty"` // Human readable
	ErrorSelector   string `json:"errorSelector,omitempty"` // 4-byte selector
	ErrorName       string `json:"errorName,omitempty"`     // Error name if known
}

// AccessListEntry represents an EIP-2930 access list entry
type AccessListEntry struct {
	Address     string   `json:"address"`
	StorageKeys []string `json:"storageKeys"`
}

// EnhancedTransaction with all new fields
type EnhancedTransaction struct {
	Hash                 string `json:"hash"`
	BlockHash            string `json:"blockHash"`
	BlockNumber          uint64 `json:"blockNumber"`
	TransactionIndex     uint64 `json:"transactionIndex"`
	From                 string `json:"from"`
	To                   string `json:"to"`
	Value                string `json:"value"`
	Gas                  uint64 `json:"gas"`
	GasPrice             string `json:"gasPrice,omitempty"`
	MaxFeePerGas         string `json:"maxFeePerGas,omitempty"`
	MaxPriorityFeePerGas string `json:"maxPriorityFeePerGas,omitempty"`
	GasUsed              uint64 `json:"gasUsed"`
	CumulativeGasUsed    uint64 `json:"cumulativeGasUsed"`
	EffectiveGasPrice    string `json:"effectiveGasPrice"`
	Nonce                uint64 `json:"nonce"`
	Input                string `json:"input"`
	Type                 uint8  `json:"type"` // 0=legacy, 1=2930, 2=1559, 3=4844
	Status               uint8  `json:"status"`
	ContractAddress      string `json:"contractAddress,omitempty"`
	// EIP-2930 access list
	AccessList []AccessListEntry `json:"accessList,omitempty"`
	// EIP-4844 blob fields
	MaxFeePerBlobGas    string   `json:"maxFeePerBlobGas,omitempty"`
	BlobVersionedHashes []string `json:"blobVersionedHashes,omitempty"`
	BlobGasUsed         uint64   `json:"blobGasUsed,omitempty"`
	BlobGasPrice        string   `json:"blobGasPrice,omitempty"`
	// Revert info
	RevertReason string `json:"revertReason,omitempty"`
	// Timing
	Timestamp        time.Time `json:"timestamp"`
	ConfirmationTime int64     `json:"confirmationTimeMs,omitempty"` // Time from first seen to confirmed
}

// Multi-chain types

// ChainConfig represents configuration for a specific chain
type ChainConfig struct {
	ChainID     uint64 `json:"chainId"`
	Name        string `json:"name"`
	Symbol      string `json:"symbol"`
	RPCEndpoint string `json:"rpcEndpoint"`
	WSEndpoint  string `json:"wsEndpoint,omitempty"`
	ExplorerURL string `json:"explorerUrl,omitempty"`
	IsTestnet   bool   `json:"isTestnet"`
	BlockTime   int    `json:"blockTimeMs"`
	// Feature support
	SupportsEIP1559 bool     `json:"supportsEip1559"`
	SupportsEIP4844 bool     `json:"supportsEip4844"`
	SupportsEIP4337 bool     `json:"supportsEip4337"`
	EntryPoints     []string `json:"entryPoints,omitempty"`
}

// CrossChainTransaction links transactions across chains
type CrossChainTransaction struct {
	ID              string    `json:"id"`
	SourceChainID   uint64    `json:"sourceChainId"`
	DestChainID     uint64    `json:"destChainId"`
	SourceTxHash    string    `json:"sourceTxHash"`
	DestTxHash      string    `json:"destTxHash,omitempty"`
	BridgeProtocol  string    `json:"bridgeProtocol"` // Warp, LayerZero, etc.
	TokenAddress    string    `json:"tokenAddress,omitempty"`
	Amount          string    `json:"amount,omitempty"`
	Sender          string    `json:"sender"`
	Recipient       string    `json:"recipient"`
	Status          string    `json:"status"` // pending, completed, failed
	SourceTimestamp time.Time `json:"sourceTimestamp"`
	DestTimestamp   time.Time `json:"destTimestamp,omitempty"`
}

// Helper functions for big.Int conversions

// ParseBigInt safely parses a hex string to big.Int
func ParseBigInt(s string) *big.Int {
	if s == "" || s == "0x" {
		return big.NewInt(0)
	}
	n := new(big.Int)
	s = trimHexPrefix(s)
	n.SetString(s, 16)
	return n
}

// FormatBigInt formats big.Int as hex string
func FormatBigInt(n *big.Int) string {
	if n == nil {
		return "0x0"
	}
	return "0x" + n.Text(16)
}

// trimHexPrefix removes 0x prefix from hex string
func trimHexPrefix(s string) string {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		return s[2:]
	}
	return s
}
