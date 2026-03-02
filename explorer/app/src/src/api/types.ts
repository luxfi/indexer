export interface AddressRef {
  hash: string
  name?: string
}

export interface Block {
  height: number
  hash: string
  parent_hash: string
  miner: AddressRef
  gas_limit: string
  gas_used: string
  base_fee_per_gas: string
  timestamp: string
  tx_count: number
  size: number
  type: string
}

export interface Tx {
  hash: string
  block_number: number
  block_hash: string
  from: AddressRef
  to: AddressRef | null
  created_contract: AddressRef | null
  value: string
  gas_limit: string
  gas_price: string
  gas_used: string
  nonce: number
  status: string
  timestamp: string
  method: string
}

export interface Address {
  hash: string
  balance: string
  tx_count: number
  token_transfers_count: number
  is_contract: boolean
  name?: string
}

export interface Token {
  address: string
  name: string
  symbol: string
  total_supply: string
  decimals: string
  type: string
  holders?: string
}

export interface Stats {
  total_blocks: string
  total_transactions: string
  total_addresses: string
  average_block_time: number
  coin_price: string
  market_cap: string
  gas_prices?: GasPrice
}

export interface SearchResult {
  type: string
  address?: string
  block_number?: number
  tx_hash?: string
  name?: string
}

export interface Paginated<T> {
  items: T[]
  next_page_params: Record<string, string> | null
}

export interface InternalTx {
  block_number: number
  index: number
  transaction_hash: string
  type: string
  call_type: string
  from: AddressRef
  to: AddressRef
  value: string
  gas_limit: string
  gas_used: string
  success: boolean
  error: string | null
}

export interface TokenTransfer {
  from: AddressRef
  to: AddressRef
  token: { address: string; type: string }
  total: { value: string; decimals: string }
  log_index: number
  block_number: number
  transaction_hash: string
  timestamp: string
}

export interface Contract {
  name: string
  compiler_version: string
  source_code: string
  abi: unknown[]
  is_verified: boolean
  optimization_enabled: boolean
  optimization_runs: number
}

export interface GasPrice {
  slow: number
  average: number
  fast: number
}

export interface Validator {
  address: AddressRef
  stake: string
  blocks_validated: number
  state: string
}

export interface ChartItem {
  date: string
  value: string
}
