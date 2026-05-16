import { EventEmitter } from 'node:events'
import * as net from 'node:net'

type JsonValue =
  | null
  | boolean
  | number
  | string
  | JsonValue[]
  | { [k: string]: JsonValue }

// ---------------------------------------------------------------------------
// Wire types (match protocol.json)
// ---------------------------------------------------------------------------

export type TunnelState =
  | 'Disconnected'
  | 'Validating'
  | 'Connecting'
  | 'Connected'
  | 'Disconnecting'
  | 'Error'

export type Health = 'Online' | 'Degraded' | 'Offline' | 'Unknown'

export interface ConnStatus {
  state: TunnelState
  configID?: string
  xrayPid?: number
  since: string
  message?: string
  errorSymbol?: string
}

export interface HealthStatus {
  health: Health
  latencyMs?: number
  lastChecked?: string
}

export interface Status {
  conn: ConnStatus
  health: HealthStatus | null
}

export interface ConfigInfo {
  id: string
  sha256: string
  addedAt: string
}

// ---------------------------------------------------------------------------
// Typed error
// ---------------------------------------------------------------------------

export class DaemonError extends Error {
  constructor(
    public readonly symbol: string,
    public readonly code: number,
    message: string,
    public readonly detail?: JsonValue,
  ) {
    super(message)
    this.name = 'DaemonError'
  }
}

// ---------------------------------------------------------------------------
// Internal types
// ---------------------------------------------------------------------------

interface PendingCall {
  resolve: (value: JsonValue) => void
  reject: (err: Error) => void
  timer: NodeJS.Timeout
}

interface WireFrame {
  jsonrpc?: string
  id?: string | number | null
  method?: string
  params?: JsonValue
  result?: JsonValue
  error?: {
    code: number
    message: string
    data?: { symbol?: string; detail?: JsonValue }
  }
}

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

export interface DaemonClientOptions {
  /** Default: /run/xrayd/control.sock */
  socketPath?: string
  /** Default: 10_000 */
  callTimeoutMs?: number
  /** Default: 30_000. Set to 0 to disable. */
  heartbeatIntervalMs?: number
  /** Default: 5_000 */
  heartbeatTimeoutMs?: number
}

// ---------------------------------------------------------------------------
// DaemonClient
// ---------------------------------------------------------------------------

export class DaemonClient extends EventEmitter {
  private socket: net.Socket | null = null
  private buf = Buffer.alloc(0)
  private nextId = 1
  private readonly pending = new Map<string, PendingCall>()
  private heartbeatTimer: NodeJS.Timeout | null = null
  private closed = false

  private readonly socketPath: string
  private readonly callTimeoutMs: number
  private readonly heartbeatIntervalMs: number
  private readonly heartbeatTimeoutMs: number

  constructor(opts: DaemonClientOptions = {}) {
    super()
    this.socketPath = opts.socketPath ?? '/run/xrayd/control.sock'
    this.callTimeoutMs = opts.callTimeoutMs ?? 10_000
    this.heartbeatIntervalMs = opts.heartbeatIntervalMs ?? 30_000
    this.heartbeatTimeoutMs = opts.heartbeatTimeoutMs ?? 5_000
  }

  // -------------------------------------------------------------------------
  // Lifecycle
  // -------------------------------------------------------------------------

  connect(): Promise<void> {
    if (this.closed) return Promise.reject(new Error('client closed'))
    return new Promise((resolve, reject) => {
      const sock = net.createConnection({ path: this.socketPath })
      sock.once('connect', () => {
        this.socket = sock
        this.setupReader(sock)
        this.startHeartbeat()
        this.emit('open')
        resolve()
      })
      sock.once('error', (err: Error) => {
        sock.removeAllListeners()
        reject(err)
      })
    })
  }

  close(): void {
    this.closed = true
    this.stopHeartbeat()
    if (this.socket) {
      this.socket.destroy()
      this.socket = null
    }
    for (const [, p] of this.pending) {
      clearTimeout(p.timer)
      p.reject(new Error('client closed'))
    }
    this.pending.clear()
  }

  // -------------------------------------------------------------------------
  // Typed method wrappers
  // -------------------------------------------------------------------------

  ping(): Promise<{ ok: boolean }> {
    return this.call('Daemon.Ping', null) as Promise<{ ok: boolean }>
  }

  getVersion(): Promise<{ daemon: string; xray: string | null }> {
    return this.call('Daemon.GetVersion', null) as Promise<{
      daemon: string
      xray: string | null
    }>
  }

  configsAdd(xrayJson: string): Promise<{ id: string }> {
    return this.call('Configs.Add', { json: xrayJson }) as Promise<{ id: string }>
  }

  configsList(): Promise<{ configs: ConfigInfo[] }> {
    return this.call('Configs.List', null) as unknown as Promise<{ configs: ConfigInfo[] }>
  }

  configsGet(id: string): Promise<{ json: string }> {
    return this.call('Configs.Get', { id }) as Promise<{ json: string }>
  }

  configsRemove(id: string): Promise<{ ok: boolean }> {
    return this.call('Configs.Remove', { id }) as Promise<{ ok: boolean }>
  }

  configsValidate(id: string): Promise<{ ok: boolean; error?: string; stderr?: string }> {
    return this.call('Configs.Validate', { id }) as Promise<{
      ok: boolean
      error?: string
      stderr?: string
    }>
  }

  tunnelConnect(id: string): Promise<{ state: TunnelState }> {
    return this.call('Tunnel.Connect', { id }) as Promise<{ state: TunnelState }>
  }

  tunnelDisconnect(): Promise<{ state: TunnelState }> {
    return this.call('Tunnel.Disconnect', null) as Promise<{ state: TunnelState }>
  }

  tunnelSwitch(id: string): Promise<{ state: TunnelState }> {
    return this.call('Tunnel.Switch', { id }) as Promise<{ state: TunnelState }>
  }

  tunnelGetStatus(): Promise<Status> {
    return this.call('Tunnel.GetStatus', null) as unknown as Promise<Status>
  }

  healthSetEnabled(enabled: boolean): Promise<{ ok: boolean }> {
    return this.call('Health.SetEnabled', { enabled }) as Promise<{ ok: boolean }>
  }

  healthProbe(): Promise<HealthStatus> {
    return this.call('Health.Probe', null) as unknown as Promise<HealthStatus>
  }

  healthGetConfig(): Promise<{ enabled: boolean; endpoint: string; intervalSeconds: number }> {
    return this.call('Health.GetConfig', null) as Promise<{
      enabled: boolean
      endpoint: string
      intervalSeconds: number
    }>
  }

  // -------------------------------------------------------------------------
  // Event subscription (typed overloads)
  //
  // Emits:
  //   'state.changed'    ConnStatus       — tunnel state transition
  //   'configs.changed'  { added?, removed? } — config store mutation
  //   'health.changed'   HealthStatus     — health state transition
  //   'unauthorized'     DaemonError      — id:null error on new connection
  //   'open'             —                — socket connected
  //   'close'            —                — socket lost
  //   'error'            Error            — parse / transport failure
  // -------------------------------------------------------------------------

  override on(event: 'state.changed', listener: (s: ConnStatus) => void): this
  override on(
    event: 'configs.changed',
    listener: (p: { added?: string[]; removed?: string[] }) => void,
  ): this
  override on(event: 'health.changed', listener: (s: HealthStatus) => void): this
  override on(event: 'unauthorized', listener: (err: DaemonError) => void): this
  override on(event: 'open' | 'close', listener: () => void): this
  override on(event: 'error', listener: (err: Error) => void): this
  override on(event: string, listener: (...args: never[]) => void): this {
    return super.on(event, listener as (...args: unknown[]) => void)
  }

  // -------------------------------------------------------------------------
  // Internal — call
  // -------------------------------------------------------------------------

  private call(method: string, params: JsonValue | null): Promise<JsonValue> {
    if (!this.socket || this.closed) {
      return Promise.reject(new Error('not connected'))
    }
    const sock = this.socket
    const id = String(this.nextId++)
    const req: Record<string, JsonValue> = { jsonrpc: '2.0', id, method }
    if (params !== null) req.params = params
    const line = JSON.stringify(req) + '\n'

    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id)
        reject(new Error(`call timeout: ${method}`))
      }, this.callTimeoutMs)

      this.pending.set(id, { resolve, reject, timer })

      sock.write(line, (err?: Error | null) => {
        if (err) {
          clearTimeout(timer)
          this.pending.delete(id)
          reject(err)
        }
      })
    })
  }

  // -------------------------------------------------------------------------
  // Internal — NDJSON reader
  // -------------------------------------------------------------------------

  private setupReader(sock: net.Socket): void {
    sock.on('data', (chunk: Buffer) => {
      this.buf = Buffer.concat([this.buf, chunk])
      this.drain()
    })

    sock.on('close', () => {
      this.stopHeartbeat()
      this.socket = null
      for (const [, p] of this.pending) {
        clearTimeout(p.timer)
        p.reject(new Error('connection closed'))
      }
      this.pending.clear()
      this.emit('close')
    })

    sock.on('error', (err: Error) => {
      this.emit('error', err)
    })
  }

  private drain(): void {
    let nl: number
    while ((nl = this.buf.indexOf(0x0a)) >= 0) {
      const line = this.buf.subarray(0, nl).toString('utf8').trim()
      this.buf = this.buf.subarray(nl + 1)
      if (line === '') continue
      this.handleFrame(line)
    }
  }

  private handleFrame(line: string): void {
    let frame: WireFrame
    try {
      frame = JSON.parse(line) as WireFrame
    } catch (err) {
      this.emit('error', new Error(`frame parse: ${(err as Error).message}`))
      return
    }

    // Notification: method present, id absent
    if (frame.method !== undefined && frame.id === undefined) {
      switch (frame.method) {
        case 'State.Changed':
          this.emit('state.changed', frame.params)
          break
        case 'Configs.Changed':
          this.emit(
            'configs.changed',
            frame.params,
          )
          break
        case 'Health.Changed':
          this.emit('health.changed', frame.params)
          break
        default:
          // Unknown notification — silently ignore
          break
      }
      return
    }

    // Unsolicited id:null error — unauthorized or daemon_shutting_down
    if (frame.id === null && frame.error !== undefined) {
      const sym = frame.error.data?.symbol ?? 'internal'
      const dErr = new DaemonError(
        sym,
        frame.error.code,
        frame.error.message,
        frame.error.data?.detail,
      )
      if (sym === 'unauthorized') {
        this.emit('unauthorized', dErr)
      } else {
        this.emit('error', dErr)
      }
      return
    }

    // Response to a pending call
    const id = String(frame.id ?? '')
    const pend = this.pending.get(id)
    if (!pend) {
      // Unsolicited or stale response — drop
      return
    }
    this.pending.delete(id)
    clearTimeout(pend.timer)

    if (frame.error !== undefined) {
      const sym = frame.error.data?.symbol ?? 'internal'
      pend.reject(
        new DaemonError(sym, frame.error.code, frame.error.message, frame.error.data?.detail),
      )
    } else {
      pend.resolve((frame.result ?? null))
    }
  }

  // -------------------------------------------------------------------------
  // Internal — heartbeat
  // -------------------------------------------------------------------------

  private startHeartbeat(): void {
    if (this.heartbeatIntervalMs <= 0) return
    this.heartbeatTimer = setInterval(() => {
      // Use a one-shot call with heartbeatTimeoutMs rather than the standard
      // callTimeoutMs. Build the raw call directly to avoid patching shared state.
      this.pingWithTimeout(this.heartbeatTimeoutMs).catch(() => {
        // Heartbeat failed — assume daemon is gone, force socket close.
        if (this.socket) this.socket.destroy()
      })
    }, this.heartbeatIntervalMs)
  }

  private pingWithTimeout(timeoutMs: number): Promise<{ ok: boolean }> {
    if (!this.socket || this.closed) {
      return Promise.reject(new Error('not connected'))
    }
    const sock = this.socket
    const id = String(this.nextId++)
    const line = JSON.stringify({ jsonrpc: '2.0', id, method: 'Daemon.Ping' }) + '\n'

    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id)
        reject(new Error('heartbeat timeout'))
      }, timeoutMs)

      this.pending.set(id, {
        resolve: (v) => {
          resolve(v as { ok: boolean })
        },
        reject,
        timer,
      })

      sock.write(line, (err?: Error | null) => {
        if (err) {
          clearTimeout(timer)
          this.pending.delete(id)
          reject(err)
        }
      })
    })
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer)
      this.heartbeatTimer = null
    }
  }
}
