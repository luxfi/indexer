import { useQuery } from '@tanstack/react-query'
import { fetcher } from './client'
import type {
  Block, Tx, Address, Token, Stats, SearchResult, Paginated,
  InternalTx, TokenTransfer, Contract, Validator, ChartItem,
} from './types'

export function useStats() {
  return useQuery({
    queryKey: ['stats'],
    queryFn: () => fetcher<Stats>('/stats'),
    refetchInterval: 15_000,
  })
}

export function useMainPageBlocks() {
  return useQuery({
    queryKey: ['main-page-blocks'],
    queryFn: () => fetcher<Block[]>('/main-page/blocks'),
    refetchInterval: 15_000,
  })
}

export function useMainPageTransactions() {
  return useQuery({
    queryKey: ['main-page-txs'],
    queryFn: () => fetcher<Tx[]>('/main-page/transactions'),
    refetchInterval: 15_000,
  })
}

export function useBlocks(params?: Record<string, string>) {
  return useQuery({
    queryKey: ['blocks', params],
    queryFn: () => fetcher<Paginated<Block>>('/blocks', { limit: '25', ...params }),
  })
}

export function useBlock(id: string) {
  return useQuery({
    queryKey: ['block', id],
    queryFn: () => fetcher<Block>(`/blocks/${id}`),
    enabled: !!id,
  })
}

export function useBlockTransactions(id: string) {
  return useQuery({
    queryKey: ['block-txs', id],
    queryFn: () => fetcher<Paginated<Tx>>(`/blocks/${id}/transactions`),
    enabled: !!id,
  })
}

export function useTransactions(params?: Record<string, string>) {
  return useQuery({
    queryKey: ['transactions', params],
    queryFn: () => fetcher<Paginated<Tx>>('/transactions', { limit: '25', ...params }),
  })
}

export function useTransaction(hash: string) {
  return useQuery({
    queryKey: ['tx', hash],
    queryFn: () => fetcher<Tx>(`/transactions/${hash}`),
    enabled: !!hash,
  })
}

export function useAddress(hash: string) {
  return useQuery({
    queryKey: ['address', hash],
    queryFn: () => fetcher<Address>(`/addresses/${hash}`),
    enabled: !!hash,
  })
}

export function useAddressTransactions(hash: string, params?: Record<string, string>) {
  return useQuery({
    queryKey: ['address-txs', hash, params],
    queryFn: () => fetcher<Paginated<Tx>>(`/addresses/${hash}/transactions`, params),
    enabled: !!hash,
  })
}

export function useTokens() {
  return useQuery({
    queryKey: ['tokens'],
    queryFn: () => fetcher<Paginated<Token>>('/tokens'),
  })
}

export function useTokenDetail(address: string) {
  return useQuery({
    queryKey: ['token', address],
    queryFn: () => fetcher<Token>(`/tokens/${address}`),
    enabled: !!address,
  })
}

export function useSearch(q: string) {
  return useQuery({
    queryKey: ['search', q],
    queryFn: () => fetcher<{ items: SearchResult[] }>('/search', { q }),
    enabled: q.length > 0,
  })
}

export function useInternalTxs(limit = 25) {
  return useQuery({
    queryKey: ['internal-txs', limit],
    queryFn: () => fetcher<Paginated<InternalTx>>('/internal-transactions', { limit: String(limit) }),
  })
}

export function useTokenTransfers(limit = 25) {
  return useQuery({
    queryKey: ['token-transfers', limit],
    queryFn: () => fetcher<Paginated<TokenTransfer>>('/token-transfers', { limit: String(limit) }),
  })
}

export function useContract(address: string) {
  return useQuery({
    queryKey: ['contract', address],
    queryFn: () => fetcher<Contract>(`/smart-contracts/${address}`),
    enabled: !!address,
  })
}

export function useGasPrice() {
  return useQuery({
    queryKey: ['gas-price'],
    queryFn: () => fetcher<Stats>('/stats'),
    refetchInterval: 15_000,
    select: (data) => data.gas_prices,
  })
}

export function useValidators() {
  return useQuery({
    queryKey: ['validators'],
    queryFn: () => fetcher<Paginated<Validator>>('/validators'),
  })
}

export function useTxChart() {
  return useQuery({
    queryKey: ['chart-txs'],
    queryFn: () => fetcher<{ chart_data: ChartItem[] }>('/stats/charts/transactions'),
  })
}

export function useMarketChart() {
  return useQuery({
    queryKey: ['chart-market'],
    queryFn: () => fetcher<{ chart_data: ChartItem[] }>('/stats/charts/market'),
  })
}
