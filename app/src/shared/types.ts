export type ConfigId = string
export type ConfigProto = 'vless' | 'vmess' | 'ss' | 'trojan'

export interface Config {
  id: ConfigId
  name: string
  country: string
  proto: ConfigProto
  variant: string
  host: string
  url: string
  addedAt: number
  lastUsedAt?: number
  ping?: number
}

// `configId` is the connection's subject across all states: the config being
// connected to (`connecting`), currently connected to (`connected`), or being
// disconnected from (`disconnecting`). For `error`, `configId` is the last
// config the engine attempted, so a retry knows what to reconnect to.
export type ConnState =
  | { kind: 'disconnected' }
  | { kind: 'connecting'; configId: ConfigId; since: number }
  | {
      kind: 'connected'
      configId: ConfigId
      since: number
      ping: number
      egress: string
    }
  | { kind: 'disconnecting'; configId: ConfigId }
  | { kind: 'error'; reason: string; configId: ConfigId }

export type Theme = 'light' | 'dark'
export type ThemePreference = 'system' | 'light' | 'dark'

export interface AppState {
  configs: Config[]
  activeId: ConfigId | null
  conn: ConnState
  theme: Theme
  themePreference: ThemePreference
}

// Only these fields of a Config may be mutated via updateConfig.
// id and addedAt are immutable post-creation; main strips anything else
// at the IPC boundary, even if TS is bypassed in renderer.
export type EditableConfigFields =
  | 'url'
  | 'host'
  | 'proto'
  | 'variant'
  | 'country'
  | 'name'
  | 'ping'
  | 'lastUsedAt'

export type ConfigPatch = Partial<Pick<Config, EditableConfigFields>>

export type Command =
  | { type: 'selectConfig'; id: ConfigId }
  | { type: 'toggleConnection' }
  | { type: 'addConfig'; config: Config; activate?: boolean }
  | { type: 'updateConfig'; id: ConfigId; patch: ConfigPatch }
  | { type: 'deleteConfig'; id: ConfigId }
  | { type: 'duplicateConfig'; id: ConfigId }
  | { type: 'setThemePreference'; preference: ThemePreference }
