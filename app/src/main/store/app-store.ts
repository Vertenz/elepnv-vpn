import { EventEmitter } from 'node:events'
import { randomUUID } from 'node:crypto'

import { nativeTheme } from 'electron'

import type {
  AppState,
  Command,
  Config,
  ConfigId,
  ConfigPatch,
  EditableConfigFields,
  Theme,
  ThemePreference,
} from '@shared/types'

import type { ConnectionEngine } from '../engine/connection-engine'
import type { PrefsStore } from '../persistence/prefs-store'

const EDITABLE_FIELDS: ReadonlySet<EditableConfigFields> = new Set<EditableConfigFields>([
  'url', 'host', 'proto', 'variant', 'country', 'name', 'ping', 'lastUsedAt',
])

const ALLOWED_PROTOS = new Set(['vless', 'vmess', 'ss', 'trojan'])
const HOST_RE = /^[^:]+:\d+$/

/**
 * Single source of truth for renderer-visible state.
 *
 * - Mutations happen only via `dispatch(cmd)`. The IPC handler validates
 *   the wire payload, then calls this. Renderer never mutates anything.
 * - Every successful command emits `'change'` with the new snapshot.
 * - `engine` events flow into `conn`; `nativeTheme` events flow into `theme`
 *   when `themePreference === 'system'`.
 */
export class AppStore extends EventEmitter {
  private state: AppState

  constructor(
    private readonly engine: ConnectionEngine,
    private readonly prefs: PrefsStore,
    initial: AppState,
  ) {
    super()
    this.state = initial
    this.engine.on('state', conn => {
      this.state = { ...this.state, conn }
      this.broadcast()
    })
  }

  snapshot(): AppState {
    return this.state
  }

  dispatch(cmd: Command): void {
    switch (cmd.type) {
      case 'selectConfig':
        return this.selectConfig(cmd.id)
      case 'toggleConnection':
        return this.toggleConnection()
      case 'addConfig':
        return this.addConfig(cmd.config, cmd.activate ?? false)
      case 'updateConfig':
        return this.updateConfig(cmd.id, cmd.patch)
      case 'deleteConfig':
        return this.deleteConfig(cmd.id)
      case 'duplicateConfig':
        return this.duplicateConfig(cmd.id)
      case 'setThemePreference':
        return this.setThemePreference(cmd.preference)
      default: {
        const _exhaustive: never = cmd
        void _exhaustive
        throw new Error('AppStore.dispatch: unknown command')
      }
    }
  }

  onSystemThemeChanged(): void {
    if (this.state.themePreference !== 'system') return
    const theme: Theme = nativeTheme.shouldUseDarkColors ? 'dark' : 'light'
    if (theme === this.state.theme) return
    this.state = { ...this.state, theme }
    this.broadcast()
  }

  // --- Command handlers ---------------------------------------------------

  private selectConfig(id: ConfigId): void {
    const cfg = this.findConfig(id)
    if (!cfg) throw new Error(`selectConfig: unknown id ${id}`)
    const now = Date.now()
    const configs = this.state.configs.map(c => (c.id === id ? { ...c, lastUsedAt: now } : c))
    this.state = { ...this.state, configs, activeId: id }
    if (this.state.conn.kind === 'connecting' || this.state.conn.kind === 'connected') {
      const refreshed = configs.find(c => c.id === id)!
      this.engine.connect(refreshed)
    }
    this.prefs.update({ configs })
    this.broadcast()
  }

  private toggleConnection(): void {
    const c = this.state.conn
    if (c.kind === 'connected' || c.kind === 'connecting' || c.kind === 'disconnecting') {
      this.engine.disconnect()
      return
    }
    if (c.kind === 'error') {
      const cfg = this.findConfig(c.lastConfig)
      if (cfg) this.engine.connect(cfg)
      return
    }
    if (this.state.activeId) {
      const cfg = this.findConfig(this.state.activeId)
      if (cfg) this.engine.connect(cfg)
    }
  }

  private addConfig(incoming: Config, activate: boolean): void {
    validateConfig(incoming)
    // Authoritative id and addedAt — renderer-supplied values are ignored
    // to prevent collisions and tampering.
    const stamped: Config = {
      ...incoming,
      id: randomUUID(),
      addedAt: Date.now(),
    }
    const configs = [stamped, ...this.state.configs]
    const activeId = activate ? stamped.id : this.state.activeId
    this.state = { ...this.state, configs, activeId }
    this.prefs.update({ configs })
    if (activate && (this.state.conn.kind === 'connecting' || this.state.conn.kind === 'connected')) {
      this.engine.connect(stamped)
    }
    this.broadcast()
  }

  private updateConfig(id: ConfigId, patch: ConfigPatch): void {
    const cur = this.findConfig(id)
    if (!cur) throw new Error(`updateConfig: unknown id ${id}`)
    const clean: ConfigPatch = {}
    for (const [k, v] of Object.entries(patch)) {
      if (EDITABLE_FIELDS.has(k as EditableConfigFields)) {
        ;(clean as Record<string, unknown>)[k] = v
      }
    }
    const merged: Config = { ...cur, ...clean }
    // If URL-shape fields changed, re-validate the resulting Config.
    if (clean.url !== undefined || clean.proto !== undefined || clean.host !== undefined) {
      validateConfig(merged)
    }
    const configs = this.state.configs.map(c => (c.id === id ? merged : c))
    this.state = { ...this.state, configs }
    this.prefs.update({ configs })
    this.broadcast()
  }

  private deleteConfig(id: ConfigId): void {
    if (!this.findConfig(id)) throw new Error(`deleteConfig: unknown id ${id}`)
    const configs = this.state.configs.filter(c => c.id !== id)
    const wasActive = this.state.activeId === id
    let activeId = this.state.activeId
    let conn = this.state.conn
    if (wasActive) {
      this.engine.disconnect()
      activeId = null
      conn = { kind: 'disconnected' }
    }
    this.state = { ...this.state, configs, activeId, conn }
    this.prefs.update({ configs })
    this.broadcast()
  }

  private duplicateConfig(id: ConfigId): void {
    const src = this.findConfig(id)
    if (!src) throw new Error(`duplicateConfig: unknown id ${id}`)
    const copy: Config = {
      ...src,
      id: randomUUID(),
      name: `${src.name} (copy)`,
      addedAt: Date.now(),
    }
    delete copy.lastUsedAt
    const configs = [copy, ...this.state.configs]
    this.state = { ...this.state, configs }
    this.prefs.update({ configs })
    this.broadcast()
  }

  private setThemePreference(preference: ThemePreference): void {
    const theme: Theme =
      preference === 'system'
        ? nativeTheme.shouldUseDarkColors
          ? 'dark'
          : 'light'
        : preference
    this.state = { ...this.state, themePreference: preference, theme }
    this.prefs.update({ themePreference: preference })
    this.broadcast()
  }

  private findConfig(id: ConfigId): Config | undefined {
    return this.state.configs.find(c => c.id === id)
  }

  private broadcast(): void {
    this.emit('change', this.state)
  }
}

function validateConfig(c: unknown): asserts c is Config {
  if (!c || typeof c !== 'object') throw new Error('config: not an object')
  const cfg = c as Record<string, unknown>
  if (typeof cfg.id !== 'string' || cfg.id.length === 0) throw new Error('config.id invalid')
  if (typeof cfg.name !== 'string') throw new Error('config.name invalid')
  if (typeof cfg.country !== 'string') throw new Error('config.country invalid')
  if (typeof cfg.proto !== 'string' || !ALLOWED_PROTOS.has(cfg.proto)) {
    throw new Error('config.proto invalid')
  }
  if (typeof cfg.variant !== 'string') throw new Error('config.variant invalid')
  if (typeof cfg.host !== 'string' || !HOST_RE.test(cfg.host)) {
    throw new Error('config.host invalid')
  }
  if (typeof cfg.url !== 'string' || !cfg.url.startsWith(`${cfg.proto as string}://`)) {
    throw new Error('config.url invalid')
  }
  if (typeof cfg.addedAt !== 'number') throw new Error('config.addedAt invalid')
}
