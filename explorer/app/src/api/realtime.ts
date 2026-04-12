// Base SSE realtime client for live block/transaction updates.
// Connects to /v1/base/realtime (Base Functions SSE endpoint).
// No Phoenix WebSocket, no socket.io — just SSE.

const BASE_REALTIME = import.meta.env.VITE_BASE_REALTIME || '/v1/base/realtime'

type RealtimeEvent = {
  event: string
  data: unknown
}

type Handler = (data: unknown) => void

class RealtimeClient {
  private es: EventSource | null = null
  private handlers = new Map<string, Set<Handler>>()
  private reconnectMs = 1000
  private maxReconnectMs = 30000

  connect() {
    if (this.es) return
    try {
      const url = new URL(BASE_REALTIME, window.location.origin).toString()
      this.es = new EventSource(url)
      this.reconnectMs = 1000

      this.es.onmessage = (e) => {
        try {
          const msg: RealtimeEvent = JSON.parse(e.data)
          const handlers = this.handlers.get(msg.event)
          if (handlers) {
            for (const h of handlers) h(msg.data)
          }
        } catch { /* ignore parse errors */ }
      }

      this.es.addEventListener('PB_CONNECT', () => {
        // Base SSE handshake complete
      })

      this.es.onerror = () => {
        this.es?.close()
        this.es = null
        // Exponential backoff reconnect
        setTimeout(() => this.connect(), this.reconnectMs)
        this.reconnectMs = Math.min(this.reconnectMs * 2, this.maxReconnectMs)
      }
    } catch {
      // SSE not available — no-op, app works fine without realtime
    }
  }

  disconnect() {
    this.es?.close()
    this.es = null
  }

  on(event: string, handler: Handler) {
    if (!this.handlers.has(event)) this.handlers.set(event, new Set())
    this.handlers.get(event)!.add(handler)
    return () => { this.handlers.get(event)?.delete(handler) }
  }
}

export const realtime = new RealtimeClient()
