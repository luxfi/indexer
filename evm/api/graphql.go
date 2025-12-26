// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// GraphQL schema matching Blockscout's GraphQL API
const GraphQLSchema = `
type Query {
  # Blocks
  block(number: Int, hash: String): Block
  blocks(first: Int, after: String): BlockConnection!

  # Transactions
  transaction(hash: String!): Transaction
  transactions(first: Int, after: String): TransactionConnection!

  # Addresses
  address(hash: String!): Address
  addresses(first: Int, after: String): AddressConnection!

  # Tokens
  token(contractAddress: String!): Token
  tokens(first: Int, after: String, type: TokenType): TokenConnection!

  # Search
  search(query: String!): [SearchResult!]!

  # Stats
  stats: ChainStats!
}

type Subscription {
  # Real-time block updates
  newBlock: Block!

  # Real-time transaction updates
  newTransaction: Transaction!

  # Address-specific updates
  addressUpdates(addressHash: String!): AddressUpdate!
}

type Block {
  hash: String!
  number: Int!
  parentHash: String!
  timestamp: DateTime!
  miner: Address
  gasUsed: BigInt!
  gasLimit: BigInt!
  baseFeePerGas: BigInt
  size: Int!
  transactionCount: Int!
  transactions(first: Int, after: String): TransactionConnection!
  confirmations: Int!
}

type Transaction {
  hash: String!
  blockNumber: Int
  blockHash: String
  timestamp: DateTime
  from: Address!
  to: Address
  createdContract: Address
  value: BigInt!
  gasLimit: BigInt!
  gasPrice: BigInt!
  gasUsed: BigInt
  maxFeePerGas: BigInt
  maxPriorityFeePerGas: BigInt
  nonce: Int!
  position: Int
  input: String!
  status: TransactionStatus
  method: String
  tokenTransfers(first: Int, after: String): TokenTransferConnection!
  internalTransactions(first: Int, after: String): InternalTransactionConnection!
  logs(first: Int, after: String): LogConnection!
}

type Address {
  hash: String!
  balance: BigInt!
  transactionCount: Int!
  isContract: Boolean!
  isVerified: Boolean!
  creator: Address
  creationTxHash: String
  token: Token
  transactions(first: Int, after: String): TransactionConnection!
  tokenTransfers(first: Int, after: String): TokenTransferConnection!
  tokens(first: Int, after: String): TokenBalanceConnection!
}

type Token {
  address: String!
  name: String
  symbol: String
  decimals: Int
  totalSupply: BigInt
  type: TokenType!
  holderCount: Int!
  transferCount: Int!
  transfers(first: Int, after: String): TokenTransferConnection!
  holders(first: Int, after: String): TokenHolderConnection!
}

type TokenTransfer {
  txHash: String!
  blockNumber: Int!
  logIndex: Int!
  timestamp: DateTime
  from: Address!
  to: Address!
  token: Token!
  value: BigInt
  tokenId: String
}

type InternalTransaction {
  txHash: String!
  blockNumber: Int!
  index: Int!
  type: String!
  callType: String
  from: Address!
  to: Address
  value: BigInt!
  gas: BigInt!
  gasUsed: BigInt
  input: String
  output: String
  error: String
  success: Boolean!
}

type Log {
  txHash: String!
  blockNumber: Int!
  index: Int!
  address: Address!
  data: String!
  topics: [String!]!
}

type SmartContract {
  address: String!
  name: String
  compilerVersion: String
  isVerified: Boolean!
  isProxy: Boolean!
  sourceCode: String
  abi: String
  constructorArguments: String
}

type ChainStats {
  totalBlocks: Int!
  totalTransactions: Int!
  totalAddresses: Int!
  averageBlockTime: Float!
  gasPrice: BigInt
}

type SearchResult {
  type: SearchResultType!
  address: String
  hash: String
  blockNumber: Int
  name: String
  symbol: String
}

type AddressUpdate {
  type: AddressUpdateType!
  address: String!
  data: String
}

# Connection types for pagination
type BlockConnection {
  edges: [BlockEdge!]!
  pageInfo: PageInfo!
}

type BlockEdge {
  node: Block!
  cursor: String!
}

type TransactionConnection {
  edges: [TransactionEdge!]!
  pageInfo: PageInfo!
}

type TransactionEdge {
  node: Transaction!
  cursor: String!
}

type AddressConnection {
  edges: [AddressEdge!]!
  pageInfo: PageInfo!
}

type AddressEdge {
  node: Address!
  cursor: String!
}

type TokenConnection {
  edges: [TokenEdge!]!
  pageInfo: PageInfo!
}

type TokenEdge {
  node: Token!
  cursor: String!
}

type TokenTransferConnection {
  edges: [TokenTransferEdge!]!
  pageInfo: PageInfo!
}

type TokenTransferEdge {
  node: TokenTransfer!
  cursor: String!
}

type TokenBalanceConnection {
  edges: [TokenBalanceEdge!]!
  pageInfo: PageInfo!
}

type TokenBalanceEdge {
  node: TokenBalance!
  cursor: String!
}

type TokenBalance {
  token: Token!
  value: BigInt!
  tokenId: String
}

type TokenHolderConnection {
  edges: [TokenHolderEdge!]!
  pageInfo: PageInfo!
}

type TokenHolderEdge {
  node: TokenHolder!
  cursor: String!
}

type TokenHolder {
  address: Address!
  value: BigInt!
}

type InternalTransactionConnection {
  edges: [InternalTransactionEdge!]!
  pageInfo: PageInfo!
}

type InternalTransactionEdge {
  node: InternalTransaction!
  cursor: String!
}

type LogConnection {
  edges: [LogEdge!]!
  pageInfo: PageInfo!
}

type LogEdge {
  node: Log!
  cursor: String!
}

type PageInfo {
  hasNextPage: Boolean!
  hasPreviousPage: Boolean!
  startCursor: String
  endCursor: String
}

# Enums
enum TransactionStatus {
  OK
  ERROR
  PENDING
}

enum TokenType {
  ERC20
  ERC721
  ERC1155
}

enum SearchResultType {
  ADDRESS
  CONTRACT
  TOKEN
  TRANSACTION
  BLOCK
}

enum AddressUpdateType {
  TRANSACTION
  TOKEN_TRANSFER
  BALANCE
}

# Scalars
scalar DateTime
scalar BigInt
`

// GraphQLHandler handles GraphQL requests
type GraphQLHandler struct {
	repo *Repository
}

// NewGraphQLHandler creates a new GraphQL handler
func NewGraphQLHandler(repo *Repository) *GraphQLHandler {
	return &GraphQLHandler{repo: repo}
}

// GraphQLRequest represents a GraphQL request
type GraphQLRequest struct {
	Query         string                 `json:"query"`
	OperationName string                 `json:"operationName"`
	Variables     map[string]interface{} `json:"variables"`
}

// GraphQLResponse represents a GraphQL response
type GraphQLResponse struct {
	Data   interface{}     `json:"data,omitempty"`
	Errors []GraphQLError  `json:"errors,omitempty"`
}

// GraphQLError represents a GraphQL error
type GraphQLError struct {
	Message   string                 `json:"message"`
	Locations []GraphQLErrorLocation `json:"locations,omitempty"`
	Path      []interface{}          `json:"path,omitempty"`
}

// GraphQLErrorLocation represents error location in query
type GraphQLErrorLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Handle processes GraphQL requests
func (h *GraphQLHandler) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Handle GET request for playground
	if r.Method == "GET" {
		h.servePlayground(w, r)
		return
	}

	var req GraphQLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(GraphQLResponse{
			Errors: []GraphQLError{{Message: "Invalid request body"}},
		})
		return
	}

	result := h.execute(r.Context(), req)
	json.NewEncoder(w).Encode(result)
}

func (h *GraphQLHandler) servePlayground(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
  <title>Lux EVM GraphQL Playground</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/graphql-playground-react/build/static/css/index.css" />
  <script src="https://cdn.jsdelivr.net/npm/graphql-playground-react/build/static/js/middleware.js"></script>
</head>
<body>
  <div id="root"></div>
  <script>
    window.addEventListener('load', function() {
      GraphQLPlayground.init(document.getElementById('root'), {
        endpoint: window.location.href,
        settings: {
          'editor.theme': 'dark',
          'editor.fontSize': 14,
        }
      })
    })
  </script>
</body>
</html>`))
}

func (h *GraphQLHandler) execute(ctx context.Context, req GraphQLRequest) GraphQLResponse {
	// Simple GraphQL executor - for production, use gqlgen or graphql-go
	query := strings.TrimSpace(req.Query)

	// Parse operation type
	if strings.HasPrefix(query, "query") || !strings.HasPrefix(query, "mutation") && !strings.HasPrefix(query, "subscription") {
		return h.executeQuery(ctx, req)
	}

	if strings.HasPrefix(query, "subscription") {
		return GraphQLResponse{
			Errors: []GraphQLError{{Message: "Subscriptions require WebSocket connection"}},
		}
	}

	return GraphQLResponse{
		Errors: []GraphQLError{{Message: "Mutations not supported"}},
	}
}

func (h *GraphQLHandler) executeQuery(ctx context.Context, req GraphQLRequest) GraphQLResponse {
	data := make(map[string]interface{})

	// Parse and execute query fields
	query := req.Query

	// Simple field detection
	if strings.Contains(query, "block(") || strings.Contains(query, "block {") {
		data["block"] = h.resolveBlock(ctx, req.Variables)
	}

	if strings.Contains(query, "blocks(") || strings.Contains(query, "blocks {") {
		data["blocks"] = h.resolveBlocks(ctx, req.Variables)
	}

	if strings.Contains(query, "transaction(") {
		data["transaction"] = h.resolveTransaction(ctx, req.Variables)
	}

	if strings.Contains(query, "transactions(") || strings.Contains(query, "transactions {") {
		data["transactions"] = h.resolveTransactions(ctx, req.Variables)
	}

	if strings.Contains(query, "address(") {
		data["address"] = h.resolveAddress(ctx, req.Variables)
	}

	if strings.Contains(query, "addresses(") || strings.Contains(query, "addresses {") {
		data["addresses"] = h.resolveAddresses(ctx, req.Variables)
	}

	if strings.Contains(query, "token(") {
		data["token"] = h.resolveToken(ctx, req.Variables)
	}

	if strings.Contains(query, "tokens(") || strings.Contains(query, "tokens {") {
		data["tokens"] = h.resolveTokens(ctx, req.Variables)
	}

	if strings.Contains(query, "search(") {
		data["search"] = h.resolveSearch(ctx, req.Variables)
	}

	if strings.Contains(query, "stats") {
		data["stats"] = h.resolveStats(ctx)
	}

	// Introspection query
	if strings.Contains(query, "__schema") {
		data["__schema"] = h.resolveSchema()
	}

	if strings.Contains(query, "__typename") {
		data["__typename"] = "Query"
	}

	return GraphQLResponse{Data: data}
}

func (h *GraphQLHandler) resolveBlock(ctx context.Context, vars map[string]interface{}) interface{} {
	var block *Block
	var err error

	if number, ok := vars["number"].(float64); ok {
		block, err = h.repo.GetBlockByNumber(ctx, uint64(number))
	} else if hash, ok := vars["hash"].(string); ok {
		block, err = h.repo.GetBlockByHash(ctx, hash)
	}

	if err != nil {
		return nil
	}

	return h.blockToGraphQL(block)
}

func (h *GraphQLHandler) resolveBlocks(ctx context.Context, vars map[string]interface{}) interface{} {
	first := 10
	if f, ok := vars["first"].(float64); ok {
		first = int(f)
	}

	result, err := h.repo.GetBlocks(ctx, 0, first, nil)
	if err != nil {
		return nil
	}

	blocks := result.Items.([]Block)
	var edges []map[string]interface{}
	for i, block := range blocks {
		edges = append(edges, map[string]interface{}{
			"node":   h.blockToGraphQL(&block),
			"cursor": fmt.Sprintf("block_%d", i),
		})
	}

	return map[string]interface{}{
		"edges": edges,
		"pageInfo": map[string]interface{}{
			"hasNextPage":     result.NextPageParams != nil,
			"hasPreviousPage": false,
		},
	}
}

func (h *GraphQLHandler) resolveTransaction(ctx context.Context, vars map[string]interface{}) interface{} {
	hash, ok := vars["hash"].(string)
	if !ok {
		return nil
	}

	tx, err := h.repo.GetTransactionByHash(ctx, hash)
	if err != nil {
		return nil
	}

	return h.transactionToGraphQL(tx)
}

func (h *GraphQLHandler) resolveTransactions(ctx context.Context, vars map[string]interface{}) interface{} {
	first := 10
	if f, ok := vars["first"].(float64); ok {
		first = int(f)
	}

	result, err := h.repo.GetTransactions(ctx, 0, first, nil)
	if err != nil {
		return nil
	}

	txs := result.Items.([]Transaction)
	var edges []map[string]interface{}
	for i, tx := range txs {
		edges = append(edges, map[string]interface{}{
			"node":   h.transactionToGraphQL(&tx),
			"cursor": fmt.Sprintf("tx_%d", i),
		})
	}

	return map[string]interface{}{
		"edges": edges,
		"pageInfo": map[string]interface{}{
			"hasNextPage":     result.NextPageParams != nil,
			"hasPreviousPage": false,
		},
	}
}

func (h *GraphQLHandler) resolveAddress(ctx context.Context, vars map[string]interface{}) interface{} {
	hash, ok := vars["hash"].(string)
	if !ok {
		return nil
	}

	addr, err := h.repo.GetAddress(ctx, hash)
	if err != nil {
		return nil
	}

	return h.addressToGraphQL(addr)
}

func (h *GraphQLHandler) resolveAddresses(ctx context.Context, vars map[string]interface{}) interface{} {
	// Would need address listing with pagination
	return map[string]interface{}{
		"edges":    []interface{}{},
		"pageInfo": map[string]interface{}{"hasNextPage": false, "hasPreviousPage": false},
	}
}

func (h *GraphQLHandler) resolveToken(ctx context.Context, vars map[string]interface{}) interface{} {
	address, ok := vars["contractAddress"].(string)
	if !ok {
		return nil
	}

	token, err := h.repo.GetToken(ctx, address)
	if err != nil {
		return nil
	}

	return h.tokenToGraphQL(token)
}

func (h *GraphQLHandler) resolveTokens(ctx context.Context, vars map[string]interface{}) interface{} {
	first := 10
	if f, ok := vars["first"].(float64); ok {
		first = int(f)
	}

	tokenType := ""
	if t, ok := vars["type"].(string); ok {
		tokenType = t
	}

	result, err := h.repo.GetTokens(ctx, 0, first, tokenType)
	if err != nil {
		return nil
	}

	tokens := result.Items.([]Token)
	var edges []map[string]interface{}
	for i, token := range tokens {
		edges = append(edges, map[string]interface{}{
			"node":   h.tokenToGraphQL(&token),
			"cursor": fmt.Sprintf("token_%d", i),
		})
	}

	return map[string]interface{}{
		"edges": edges,
		"pageInfo": map[string]interface{}{
			"hasNextPage":     result.NextPageParams != nil,
			"hasPreviousPage": false,
		},
	}
}

func (h *GraphQLHandler) resolveSearch(ctx context.Context, vars map[string]interface{}) interface{} {
	query, ok := vars["query"].(string)
	if !ok {
		return []interface{}{}
	}

	results, err := h.repo.Search(ctx, query, 20)
	if err != nil {
		return []interface{}{}
	}

	var gqlResults []map[string]interface{}
	for _, r := range results {
		gqlResults = append(gqlResults, map[string]interface{}{
			"type":        strings.ToUpper(r.Type),
			"address":     r.Address,
			"hash":        r.Hash,
			"blockNumber": r.BlockNumber,
			"name":        r.Name,
			"symbol":      r.Symbol,
		})
	}

	return gqlResults
}

func (h *GraphQLHandler) resolveStats(ctx context.Context) interface{} {
	stats, err := h.repo.GetStats(ctx)
	if err != nil {
		return nil
	}

	return map[string]interface{}{
		"totalBlocks":       stats.TotalBlocks,
		"totalTransactions": stats.TotalTransactions,
		"totalAddresses":    stats.TotalAddresses,
		"averageBlockTime":  stats.AverageBlockTime,
		"gasPrice":          stats.GasPrice,
	}
}

func (h *GraphQLHandler) resolveSchema() interface{} {
	// Return minimal schema introspection
	return map[string]interface{}{
		"types": []map[string]interface{}{
			{"name": "Query", "kind": "OBJECT"},
			{"name": "Subscription", "kind": "OBJECT"},
			{"name": "Block", "kind": "OBJECT"},
			{"name": "Transaction", "kind": "OBJECT"},
			{"name": "Address", "kind": "OBJECT"},
			{"name": "Token", "kind": "OBJECT"},
		},
		"queryType":        map[string]string{"name": "Query"},
		"subscriptionType": map[string]string{"name": "Subscription"},
	}
}

// Type converters

func (h *GraphQLHandler) blockToGraphQL(block *Block) map[string]interface{} {
	if block == nil {
		return nil
	}

	result := map[string]interface{}{
		"hash":             block.Hash,
		"number":           block.Height,
		"parentHash":       block.ParentHash,
		"timestamp":        block.Timestamp.Format("2006-01-02T15:04:05Z"),
		"gasUsed":          block.GasUsed,
		"gasLimit":         block.GasLimit,
		"baseFeePerGas":    block.BaseFeePerGas,
		"size":             block.Size,
		"transactionCount": block.TransactionCount,
		"confirmations":    block.Confirmations,
	}

	if block.Miner != nil {
		result["miner"] = h.addressToGraphQL(block.Miner)
	}

	return result
}

func (h *GraphQLHandler) transactionToGraphQL(tx *Transaction) map[string]interface{} {
	if tx == nil {
		return nil
	}

	result := map[string]interface{}{
		"hash":     tx.Hash,
		"value":    tx.Value,
		"gasLimit": strconv.FormatUint(tx.Gas, 10),
		"gasPrice": tx.GasPrice,
		"nonce":    tx.Nonce,
		"input":    tx.Input,
		"method":   tx.Method,
	}

	if tx.BlockNumber != nil {
		result["blockNumber"] = *tx.BlockNumber
	}
	if tx.BlockHash != "" {
		result["blockHash"] = tx.BlockHash
	}
	if tx.Timestamp != nil {
		result["timestamp"] = tx.Timestamp.Format("2006-01-02T15:04:05Z")
	}
	if tx.GasUsed != nil {
		result["gasUsed"] = strconv.FormatUint(*tx.GasUsed, 10)
	}
	if tx.Position != nil {
		result["position"] = *tx.Position
	}
	if tx.From != nil {
		result["from"] = h.addressToGraphQL(tx.From)
	}
	if tx.To != nil {
		result["to"] = h.addressToGraphQL(tx.To)
	}
	if tx.CreatedContract != nil {
		result["createdContract"] = h.addressToGraphQL(tx.CreatedContract)
	}

	result["status"] = "OK"
	if tx.Status == "error" {
		result["status"] = "ERROR"
	} else if tx.Status == "pending" {
		result["status"] = "PENDING"
	}

	return result
}

func (h *GraphQLHandler) addressToGraphQL(addr *Address) map[string]interface{} {
	if addr == nil {
		return nil
	}

	result := map[string]interface{}{
		"hash":       addr.Hash,
		"balance":    addr.Balance,
		"isContract": addr.IsContract,
		"isVerified": addr.IsVerified,
	}

	if addr.TransactionCount != nil {
		result["transactionCount"] = *addr.TransactionCount
	}
	if addr.Creator != nil {
		result["creator"] = h.addressToGraphQL(addr.Creator)
	}
	if addr.CreationTxHash != "" {
		result["creationTxHash"] = addr.CreationTxHash
	}
	if addr.Token != nil {
		result["token"] = h.tokenToGraphQL(addr.Token)
	}

	return result
}

func (h *GraphQLHandler) tokenToGraphQL(token *Token) map[string]interface{} {
	if token == nil {
		return nil
	}

	result := map[string]interface{}{
		"address":     token.Address,
		"name":        token.Name,
		"symbol":      token.Symbol,
		"totalSupply": token.TotalSupply,
		"type":        token.Type,
	}

	if token.Decimals != nil {
		result["decimals"] = *token.Decimals
	}
	if token.HolderCount != nil {
		result["holderCount"] = *token.HolderCount
	}
	if token.TransferCount != nil {
		result["transferCount"] = *token.TransferCount
	}

	return result
}
