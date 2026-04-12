/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_BASE: string
  readonly VITE_BASE_REALTIME: string
  readonly VITE_CHAIN_NAME: string
  readonly VITE_COIN: string
  readonly VITE_CHAIN_ID: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
