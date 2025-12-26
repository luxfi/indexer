// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package contracts provides smart contract verification and proxy detection.
package contracts

import (
	"encoding/json"
	"time"
)

// VerificationStatus represents contract verification state.
type VerificationStatus string

const (
	StatusUnverified VerificationStatus = "unverified"
	StatusVerified   VerificationStatus = "verified"
	StatusPartial    VerificationStatus = "partial"
	StatusFailed     VerificationStatus = "failed"
)

// ProxyType identifies the proxy pattern used by a contract.
type ProxyType string

const (
	ProxyNone                   ProxyType = ""
	ProxyEIP1967                ProxyType = "eip1967"
	ProxyEIP1967Beacon          ProxyType = "eip1967_beacon"
	ProxyEIP1967OpenZeppelin    ProxyType = "eip1967_oz"
	ProxyEIP1822                ProxyType = "eip1822"
	ProxyEIP1167                ProxyType = "eip1167"
	ProxyMasterCopy             ProxyType = "master_copy"
	ProxyBasicImplementation    ProxyType = "basic_implementation"
	ProxyCloneWithImmutableArgs ProxyType = "clone_with_immutable_args"
	ProxyEIP2535                ProxyType = "eip2535"
	ProxyResolvedDelegate       ProxyType = "resolved_delegate"
)

// SmartContract represents a verified smart contract.
type SmartContract struct {
	Address            string             `json:"address"`
	Name               string             `json:"name"`
	CompilerVersion    string             `json:"compilerVersion"`
	EVMVersion         string             `json:"evmVersion"`
	Optimization       bool               `json:"optimization"`
	OptimizationRuns   int                `json:"optimizationRuns"`
	SourceCode         string             `json:"sourceCode"`
	ABI                json.RawMessage    `json:"abi"`
	Bytecode           string             `json:"bytecode"`
	DeployedBytecode   string             `json:"deployedBytecode"`
	ConstructorArgs    string             `json:"constructorArgs,omitempty"`
	Libraries          map[string]string  `json:"libraries,omitempty"`
	VerificationStatus VerificationStatus `json:"verificationStatus"`
	ProxyType          ProxyType          `json:"proxyType,omitempty"`
	ImplementationAddr string             `json:"implementationAddress,omitempty"`
	FilePath           string             `json:"filePath,omitempty"`
	SecondarySources   []SecondarySource  `json:"secondarySources,omitempty"`
	CompilerSettings   json.RawMessage    `json:"compilerSettings,omitempty"`
	VerifiedAt         time.Time          `json:"verifiedAt"`
	CreatedAt          time.Time          `json:"createdAt"`
	UpdatedAt          time.Time          `json:"updatedAt"`
}

// SecondarySource represents additional source files.
type SecondarySource struct {
	FileName   string `json:"fileName"`
	SourceCode string `json:"sourceCode"`
}

// VerificationRequest for contract verification.
type VerificationRequest struct {
	Address          string            `json:"address"`
	Name             string            `json:"name"`
	CompilerVersion  string            `json:"compilerVersion"`
	EVMVersion       string            `json:"evmVersion,omitempty"`
	Optimization     bool              `json:"optimization"`
	OptimizationRuns int               `json:"optimizationRuns"`
	SourceCode       string            `json:"sourceCode"`
	ConstructorArgs  string            `json:"constructorArgs,omitempty"`
	Libraries        map[string]string `json:"libraries,omitempty"`
}

// StandardJSONInput for Solidity standard JSON verification.
type StandardJSONInput struct {
	Language string                        `json:"language"`
	Sources  map[string]StandardJSONSource `json:"sources"`
	Settings StandardJSONSettings          `json:"settings"`
}

// StandardJSONSource represents a source file in standard JSON input.
type StandardJSONSource struct {
	Content string   `json:"content,omitempty"`
	URLs    []string `json:"urls,omitempty"`
}

// StandardJSONSettings for compiler settings.
type StandardJSONSettings struct {
	Optimizer       OptimizerSettings              `json:"optimizer"`
	EVMVersion      string                         `json:"evmVersion,omitempty"`
	Libraries       map[string]map[string]string   `json:"libraries,omitempty"`
	OutputSelection map[string]map[string][]string `json:"outputSelection"`
	Metadata        *MetadataSettings              `json:"metadata,omitempty"`
}

// OptimizerSettings for compiler optimization.
type OptimizerSettings struct {
	Enabled bool `json:"enabled"`
	Runs    int  `json:"runs"`
}

// MetadataSettings for bytecode metadata.
type MetadataSettings struct {
	BytecodeHash      string `json:"bytecodeHash,omitempty"`
	UseLiteralContent bool   `json:"useLiteralContent,omitempty"`
}

// CompilerOutput from solc compilation.
type CompilerOutput struct {
	Contracts map[string]map[string]ContractOutput `json:"contracts"`
	Sources   map[string]SourceOutput              `json:"sources"`
	Errors    []CompilerError                      `json:"errors,omitempty"`
}

// ContractOutput from compilation.
type ContractOutput struct {
	ABI           json.RawMessage `json:"abi"`
	Metadata      string          `json:"metadata,omitempty"`
	UserDoc       json.RawMessage `json:"userdoc,omitempty"`
	DevDoc        json.RawMessage `json:"devdoc,omitempty"`
	IR            string          `json:"ir,omitempty"`
	StorageLayout json.RawMessage `json:"storageLayout,omitempty"`
	EVM           EVMOutput       `json:"evm"`
}

// EVMOutput contains bytecode and related data.
type EVMOutput struct {
	Assembly          string            `json:"assembly,omitempty"`
	LegacyAssembly    json.RawMessage   `json:"legacyAssembly,omitempty"`
	Bytecode          BytecodeOutput    `json:"bytecode"`
	DeployedBytecode  BytecodeOutput    `json:"deployedBytecode"`
	MethodIdentifiers map[string]string `json:"methodIdentifiers,omitempty"`
	GasEstimates      json.RawMessage   `json:"gasEstimates,omitempty"`
}

// BytecodeOutput represents compiled bytecode.
type BytecodeOutput struct {
	FunctionDebugData json.RawMessage `json:"functionDebugData,omitempty"`
	Object            string          `json:"object"`
	Opcodes           string          `json:"opcodes,omitempty"`
	SourceMap         string          `json:"sourceMap,omitempty"`
	LinkReferences    json.RawMessage `json:"linkReferences,omitempty"`
}

// SourceOutput from compilation.
type SourceOutput struct {
	ID  int             `json:"id"`
	AST json.RawMessage `json:"ast,omitempty"`
}

// CompilerError from solc.
type CompilerError struct {
	Component        string          `json:"component"`
	ErrorCode        string          `json:"errorCode"`
	FormattedMessage string          `json:"formattedMessage"`
	Message          string          `json:"message"`
	Severity         string          `json:"severity"`
	SourceLocation   *SourceLocation `json:"sourceLocation,omitempty"`
	Type             string          `json:"type"`
}

// SourceLocation in source code.
type SourceLocation struct {
	End   int    `json:"end"`
	File  string `json:"file"`
	Start int    `json:"start"`
}

// ABIEntry represents a single ABI element.
type ABIEntry struct {
	Type            string     `json:"type"`
	Name            string     `json:"name,omitempty"`
	Inputs          []ABIParam `json:"inputs,omitempty"`
	Outputs         []ABIParam `json:"outputs,omitempty"`
	StateMutability string     `json:"stateMutability,omitempty"`
	Anonymous       bool       `json:"anonymous,omitempty"`
	Constant        bool       `json:"constant,omitempty"`
	Payable         bool       `json:"payable,omitempty"`
}

// ABIParam represents an ABI parameter.
type ABIParam struct {
	Name         string     `json:"name"`
	Type         string     `json:"type"`
	Indexed      bool       `json:"indexed,omitempty"`
	Components   []ABIParam `json:"components,omitempty"`
	InternalType string     `json:"internalType,omitempty"`
}

// MethodSignature represents a function signature.
type MethodSignature struct {
	Selector  string     `json:"selector"` // 4-byte selector
	Name      string     `json:"name"`
	Signature string     `json:"signature"` // full signature
	Inputs    []ABIParam `json:"inputs"`
	Outputs   []ABIParam `json:"outputs,omitempty"`
}

// EventSignature represents an event signature.
type EventSignature struct {
	Topic0    string     `json:"topic0"` // 32-byte topic
	Name      string     `json:"name"`
	Signature string     `json:"signature"`
	Inputs    []ABIParam `json:"inputs"`
}

// ProxyInfo contains proxy detection results.
type ProxyInfo struct {
	IsProxy               bool      `json:"isProxy"`
	ProxyType             ProxyType `json:"proxyType,omitempty"`
	ImplementationAddress string    `json:"implementationAddress,omitempty"`
	BeaconAddress         string    `json:"beaconAddress,omitempty"`
}

// DecodedCall represents a decoded function call.
type DecodedCall struct {
	MethodID   string                 `json:"methodId"`
	Name       string                 `json:"name"`
	Signature  string                 `json:"signature"`
	Parameters map[string]interface{} `json:"parameters"`
}

// DecodedLog represents a decoded event log.
type DecodedLog struct {
	Topic0     string                 `json:"topic0"`
	Name       string                 `json:"name"`
	Signature  string                 `json:"signature"`
	Parameters map[string]interface{} `json:"parameters"`
	Indexed    map[string]interface{} `json:"indexed"`
}

// ContractMethod represents a stored method signature.
type ContractMethod struct {
	Selector    string          `json:"selector"`
	Name        string          `json:"name"`
	Signature   string          `json:"signature"`
	InputTypes  json.RawMessage `json:"inputTypes"`
	OutputTypes json.RawMessage `json:"outputTypes,omitempty"`
}

// Well-known storage slots for proxy detection.
const (
	// EIP-1967 implementation slot: bytes32(uint256(keccak256('eip1967.proxy.implementation')) - 1)
	StorageSlotEIP1967Implementation = "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc"

	// EIP-1967 beacon slot: bytes32(uint256(keccak256('eip1967.proxy.beacon')) - 1)
	StorageSlotEIP1967Beacon = "0xa3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6cb3582b35133d50"

	// EIP-1967 admin slot: bytes32(uint256(keccak256('eip1967.proxy.admin')) - 1)
	StorageSlotEIP1967Admin = "0xb53127684a568b3173ae13b9f8a6016e243e63b6e8ee1178d6a717850b5d6103"

	// OpenZeppelin legacy implementation slot: keccak256("org.zeppelinos.proxy.implementation")
	StorageSlotOpenZeppelinLegacy = "0x7050c9e0f4ca769c69bd3a8ef740bc37934f8e2c036e5a723fd8ee048ed3f8c3"

	// EIP-1822 PROXIABLE slot: keccak256("PROXIABLE")
	StorageSlotEIP1822 = "0xc5f16f0fcc639fa48a6947836d9850f504798523bf8c9a3a87d5876cf622bcf7"

	// Master copy slot (Gnosis Safe): slot 0
	StorageSlotMasterCopy = "0x0"
)

// Well-known function selectors.
const (
	// implementation() selector
	SelectorImplementation = "0x5c60da1b"

	// admin() selector
	SelectorAdmin = "0xf851a440"

	// upgradeTo(address) selector
	SelectorUpgradeTo = "0x3659cfe6"

	// upgradeToAndCall(address,bytes) selector
	SelectorUpgradeToAndCall = "0x4f1ef286"
)

// EIP-1167 minimal proxy bytecode patterns.
var (
	// Standard EIP-1167: 0x363d3d373d3d3d363d73<address>5af43d82803e903d91602b57fd5bf3
	EIP1167Prefix = []byte{0x36, 0x3d, 0x3d, 0x37, 0x3d, 0x3d, 0x3d, 0x36, 0x3d, 0x73}
	EIP1167Suffix = []byte{0x5a, 0xf4, 0x3d, 0x82, 0x80, 0x3e, 0x90, 0x3d, 0x91, 0x60, 0x2b, 0x57, 0xfd, 0x5b, 0xf3}

	// Variant: 0x3d3d3d3d363d3d37363d73<address>5af43d3d93803e602a57fd5bf3
	EIP1167VariantPrefix = []byte{0x3d, 0x3d, 0x3d, 0x3d, 0x36, 0x3d, 0x3d, 0x37, 0x36, 0x3d, 0x73}
	EIP1167VariantSuffix = []byte{0x5a, 0xf4, 0x3d, 0x3d, 0x93, 0x80, 0x3e, 0x60, 0x2a, 0x57, 0xfd, 0x5b, 0xf3}
)
