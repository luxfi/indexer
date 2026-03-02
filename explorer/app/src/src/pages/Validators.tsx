import { colors } from '../theme'

export function Validators() {
  return (
    <div>
      <h1 style={{ fontSize: 20, fontWeight: 600, marginBottom: 20 }}>Validators</h1>
      <div
        style={{
          background: colors.card,
          borderRadius: 8,
          padding: 40,
          border: `1px solid ${colors.border}`,
          textAlign: 'center',
        }}
      >
        <div style={{ fontSize: 32, marginBottom: 12, opacity: 0.3 }}>V</div>
        <p style={{ color: colors.textMuted, fontSize: 14 }}>
          Validator information will be available once the network is fully operational.
        </p>
        <p style={{ color: colors.textMuted, fontSize: 13, marginTop: 8 }}>
          Quasar consensus with BLS + Ringtail + ML-DSA.
        </p>
      </div>
    </div>
  )
}
