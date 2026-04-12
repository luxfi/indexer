import { colors } from '../theme'

export function Bridge() {
  return (
    <div>
      <h1 style={{ fontSize: 20, fontWeight: 600, marginBottom: 20 }}>Cross-Chain Bridge</h1>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))',
          gap: 16,
          marginBottom: 24,
        }}
      >
        {[
          { name: 'Liquid EVM', desc: 'Standard EVM chain for smart contracts and tokens', chainId: 'Variable' },
          { name: 'Liquid DEX', desc: 'Native orderbook VM for high-performance trading', chainId: '--' },
          { name: 'Liquid FHE', desc: 'Fully homomorphic encryption compute chain', chainId: '--' },
        ].map((chain) => (
          <div
            key={chain.name}
            style={{
              background: colors.card,
              borderRadius: 8,
              padding: 20,
              border: `1px solid ${colors.border}`,
            }}
          >
            <div style={{ fontSize: 16, fontWeight: 600, marginBottom: 8 }}>{chain.name}</div>
            <div style={{ fontSize: 13, color: colors.textMuted, marginBottom: 8 }}>{chain.desc}</div>
            <div style={{ fontSize: 12, fontFamily: colors.mono, color: colors.textMuted }}>
              Chain ID: {chain.chainId}
            </div>
          </div>
        ))}
      </div>

      <div
        style={{
          background: colors.card,
          borderRadius: 8,
          padding: 24,
          border: `1px solid ${colors.border}`,
          maxWidth: 500,
          margin: '0 auto',
        }}
      >
        <h2 style={{ fontSize: 16, fontWeight: 600, marginBottom: 20, textAlign: 'center' }}>
          Transfer Assets
        </h2>

        <div style={{ marginBottom: 16 }}>
          <label style={{ fontSize: 13, color: colors.textMuted, display: 'block', marginBottom: 6 }}>
            From
          </label>
          <select
            style={{
              width: '100%',
              padding: '10px 14px',
              borderRadius: 6,
              border: `1px solid ${colors.border}`,
              background: colors.bg,
              color: colors.text,
              fontSize: 14,
            }}
          >
            <option>Liquid EVM</option>
            <option>Liquid DEX</option>
            <option>Liquid FHE</option>
          </select>
        </div>

        <div style={{ textAlign: 'center', padding: '8px 0', color: colors.textMuted, fontSize: 18 }}>
          |
        </div>

        <div style={{ marginBottom: 16 }}>
          <label style={{ fontSize: 13, color: colors.textMuted, display: 'block', marginBottom: 6 }}>
            To
          </label>
          <select
            style={{
              width: '100%',
              padding: '10px 14px',
              borderRadius: 6,
              border: `1px solid ${colors.border}`,
              background: colors.bg,
              color: colors.text,
              fontSize: 14,
            }}
          >
            <option>Liquid DEX</option>
            <option>Liquid EVM</option>
            <option>Liquid FHE</option>
          </select>
        </div>

        <div style={{ marginBottom: 20 }}>
          <label style={{ fontSize: 13, color: colors.textMuted, display: 'block', marginBottom: 6 }}>
            Amount
          </label>
          <input
            type="text"
            placeholder="0.0"
            style={{
              width: '100%',
              padding: '10px 14px',
              borderRadius: 6,
              border: `1px solid ${colors.border}`,
              background: colors.bg,
              color: colors.text,
              fontSize: 14,
              fontFamily: colors.mono,
              outline: 'none',
            }}
          />
        </div>

        <button
          disabled
          style={{
            width: '100%',
            padding: '12px 20px',
            background: colors.border,
            color: colors.textMuted,
            border: 'none',
            borderRadius: 6,
            cursor: 'not-allowed',
            fontSize: 14,
            fontWeight: 600,
          }}
        >
          Bridge Coming Soon
        </button>
      </div>

      <div
        style={{
          marginTop: 24,
          background: colors.card,
          borderRadius: 8,
          padding: 20,
          border: `1px solid ${colors.border}`,
        }}
      >
        <h2 style={{ fontSize: 16, fontWeight: 600, marginBottom: 12 }}>Recent Transfers</h2>
        <p style={{ color: colors.textMuted, fontSize: 14 }}>
          No cross-chain transfers yet. Bridge transfers will appear here once the cross-chain messaging layer is active.
        </p>
      </div>
    </div>
  )
}
