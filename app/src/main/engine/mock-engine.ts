import { EventEmitter } from 'node:events'

import type { Config, ConnState } from '@shared/types'

import type { ConnectionEngine } from './connection-engine'

const TRANSITION_MS = 900

export class MockEngine extends EventEmitter implements ConnectionEngine {
  private _state: ConnState = { kind: 'disconnected' }
  private timer: NodeJS.Timeout | null = null

  get state(): ConnState {
    return this._state
  }

  override on(event: 'state', listener: (s: ConnState) => void): this {
    return super.on(event, listener)
  }

  connect(cfg: Config): void {
    this.clearTimer()
    this.set({ kind: 'connecting', target: cfg.id, since: Date.now() })
    this.timer = setTimeout(() => {
      this.timer = null
      this.set({
        kind: 'connected',
        config: cfg.id,
        since: Date.now(),
        ping: cfg.ping ?? 50,
        egress: cfg.host.split(':')[0] ?? '',
      })
    }, TRANSITION_MS)
  }

  disconnect(): void {
    this.clearTimer()
    this.set({ kind: 'disconnected' })
  }

  private set(s: ConnState): void {
    this._state = s
    this.emit('state', s)
  }

  private clearTimer(): void {
    if (this.timer) {
      clearTimeout(this.timer)
      this.timer = null
    }
  }
}
