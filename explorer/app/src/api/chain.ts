// Chain context — determines which chain the indexer API queries
// Switching chains changes the API base path and invalidates all queries

let currentSlug = 'evm'
let listeners: Array<() => void> = []

export function getChainSlug() { return currentSlug }

export function setChainSlug(slug: string) {
  if (slug === currentSlug) return
  currentSlug = slug
  listeners.forEach(fn => fn())
}

export function onChainChange(fn: () => void) {
  listeners.push(fn)
  return () => { listeners = listeners.filter(f => f !== fn) }
}

// Build the API base path with chain slug
export function getApiBase() {
  const base = import.meta.env.VITE_API_BASE || '/v1/indexer'
  if (!currentSlug || currentSlug === 'evm') return base
  // e.g., /v1/indexer/dex or /v1/indexer/fhe
  return `${base}/${currentSlug}`
}
