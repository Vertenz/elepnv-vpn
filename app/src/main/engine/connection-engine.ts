import  { type Config, type ConnState } from '@shared/types'

/**
 * The connection state-machine boundary.
 *
 * Implementations emit `'state'` events whenever the connection state
 * changes — `AppStore` subscribes once and mirrors `conn` from these into
 * `AppState`. The `'error'` event is reserved for implementations that
 * surface failures separately from a `{ kind: 'error' }` state; for now
 * MockEngine doesn't emit it, but the contract is fixed so DaemonEngine
 * can implement it without changing this interface.
 */
export interface ConnectionEngine {
  connect(cfg: Config): void
  disconnect(): void
  on(event: 'state', listener: (s: ConnState) => void): this
  on(event: 'error', listener: (err: Error) => void): this
}
