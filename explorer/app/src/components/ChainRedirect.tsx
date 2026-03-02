import { useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { setChainSlug, getChainSlug } from '../api/chain'

const validChains = new Set(['evm', 'dex', 'fhe'])

// Syncs URL :chain param → chain context
// Invalid chains redirect to /evm
export function ChainRedirect() {
  const { chain } = useParams()
  const navigate = useNavigate()

  useEffect(() => {
    if (!chain || !validChains.has(chain)) {
      navigate('/evm', { replace: true })
      return
    }
    if (chain !== getChainSlug()) {
      setChainSlug(chain)
    }
  }, [chain, navigate])

  return null
}
