import type { Config, ConnState } from '@shared/types'

/**
 * The connection state-machine boundary.
 *
 * `MockEngine` implements this with setTimeout for the current iteration.
 * A future `DaemonEngine` will implement it over IPC to the Go daemon.
 *
 * Implementations emit `'state'` events whenever the connection state
 * changes. `AppStore` subscribes once and mirrors `conn` from these events
 * into `AppState`.
 */
export interface ConnectionEngine {
  connect(cfg: Config): void
  disconnect(): void
  on(event: 'state', listener: (s: ConnState) => void): this
}
