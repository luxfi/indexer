/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_BASE: string
  readonly VITE_REALTIME_URL: string
  readonly VITE_CHAIN_NAME: string
  readonly VITE_COIN: string
  readonly VITE_CHAIN_ID: string
  readonly VITE_NETWORKS: string  // JSON: [{label, domain, chainId}]
  readonly VITE_CHAINS: string    // JSON: [{label, slug}]
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
