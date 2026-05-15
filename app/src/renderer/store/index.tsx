import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'

import type { Config, ConfigId, ConnState } from '@shared/types'

const now = () => Date.now()

const DAY = 86_400_000

const SAMPLE_CONFIGS: Config[] = [
  {
    id: 'de',
    name: 'Frankfurt',
    country: 'DE',
    proto: 'vless',
    variant: 'vless · reality',
    host: '185.220.101.42:443',
    url: 'vless://9f7b2a1c-3d4f-4a8e-9b22-118a774f55a1@185.220.101.42:443?security=reality&sni=cloudflare.com&fp=chrome&pbk=ZK5xKQzM&type=tcp#DE-Frankfurt',
    addedAt: now() - 3 * DAY,
    ping: 24,
  },
  {
    id: 'nl',
    name: 'Amsterdam',
    country: 'NL',
    proto: 'vmess',
    variant: 'vmess · ws+tls',
    host: 'nl.elepn.io:443',
    url: 'vmess://example',
    addedAt: now() - 7 * DAY,
    ping: 41,
  },
  {
    id: 'fi',
    name: 'Helsinki',
    country: 'FI',
    proto: 'ss',
    variant: 'shadowsocks-2022',
    host: 'fi.elepn.io:8388',
    url: 'ss://example',
    addedAt: now() - 14 * DAY,
    ping: 58,
  },
  {
    id: 'sg',
    name: 'Singapore',
    country: 'SG',
    proto: 'trojan',
    variant: 'trojan · grpc',
    host: 'sg.elepn.io:443',
    url: 'trojan://example',
    addedAt: now() - 30 * DAY,
    ping: 142,
  },
  {
    id: 'de2',
    name: 'Berlin (alt)',
    country: 'DE',
    proto: 'vless',
    variant: 'vless · ws+tls',
    host: 'de2.elepn.io:443',
    url: 'vless://example2',
    addedAt: now() - 30 * DAY,
    ping: 31,
  },
]

type Theme = 'light' | 'dark'

interface StoreApi {
  configs: Config[]
  activeId: ConfigId | null
  conn: ConnState
  theme: Theme

  toggleTheme: () => void
  selectConfig: (id: ConfigId) => void
  toggleConnection: () => void
  addConfig: (config: Config) => void
  updateConfig: (id: ConfigId, patch: Partial<Config>) => void
  deleteConfig: (id: ConfigId) => void
  duplicateConfig: (id: ConfigId) => void
}

const StoreContext = createContext<StoreApi | undefined>(undefined)

const TRANSITION_MS = 900
const THEME_STORAGE_KEY = 'elepn:theme'

function readInitialTheme(): Theme {
  if (typeof window === 'undefined') return 'light'
  const stored = window.localStorage.getItem(THEME_STORAGE_KEY)
  if (stored === 'light' || stored === 'dark') return stored
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function StoreProvider({ children }: { children: React.ReactNode }) {
  const [configs, setConfigs] = useState<Config[]>(SAMPLE_CONFIGS)
  const [activeId, setActiveId] = useState<ConfigId | null>('de')
  const [conn, setConn] = useState<ConnState>({
    kind: 'connected',
    config: 'de',
    since: now() - 600_000,
    ping: 24,
    egress: '185.220.101.42',
  })
  const [theme, setTheme] = useState<Theme>(readInitialTheme)

  const pendingTimer = useRef<number | null>(null)

  const clearPending = useCallback(() => {
    if (pendingTimer.current !== null) {
      window.clearTimeout(pendingTimer.current)
      pendingTimer.current = null
    }
  }, [])

  useEffect(
    () => () => {
      clearPending()
    },
    [clearPending],
  )

  useEffect(() => {
    const mql = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = (e: MediaQueryListEvent) => {
      if (window.localStorage.getItem(THEME_STORAGE_KEY)) return
      setTheme(e.matches ? 'dark' : 'light')
    }
    mql.addEventListener('change', onChange)
    return () => {
      mql.removeEventListener('change', onChange)
    }
  }, [])

  const toggleTheme = useCallback(() => {
    setTheme((t) => {
      const next: Theme = t === 'light' ? 'dark' : 'light'
      window.localStorage.setItem(THEME_STORAGE_KEY, next)
      return next
    })
  }, [])

  const transitionTo = useCallback(
    (target: ConfigId) => {
      clearPending()
      setConn({ kind: 'connecting', target, since: now() })
      const cfg = configs.find((c) => c.id === target)
      pendingTimer.current = window.setTimeout(() => {
        pendingTimer.current = null
        setConn({
          kind: 'connected',
          config: target,
          since: now(),
          ping: cfg?.ping ?? 50,
          egress: cfg?.host.split(':')[0] ?? '',
        })
      }, TRANSITION_MS)
    },
    [clearPending, configs],
  )

  const toggleConnection = useCallback(() => {
    setConn((current) => {
      if (
        current.kind === 'connected' ||
        current.kind === 'connecting' ||
        current.kind === 'disconnecting'
      ) {
        clearPending()
        return { kind: 'disconnected' }
      }
      if (!activeId) return current
      transitionTo(activeId)
      return { kind: 'connecting', target: activeId, since: now() }
    })
  }, [activeId, clearPending, transitionTo])

  const selectConfig = useCallback(
    (id: ConfigId) => {
      setActiveId(id)
      setConfigs((list) => list.map((c) => (c.id === id ? { ...c, lastUsedAt: now() } : c)))
      if (conn.kind === 'connected' || conn.kind === 'connecting') {
        transitionTo(id)
      }
    },
    [conn.kind, transitionTo],
  )

  const addConfig = useCallback((config: Config) => {
    setConfigs((list) => [config, ...list])
  }, [])

  const updateConfig = useCallback((id: ConfigId, patch: Partial<Config>) => {
    setConfigs((list) => list.map((c) => (c.id === id ? { ...c, ...patch } : c)))
  }, [])

  const deleteConfig = useCallback(
    (id: ConfigId) => {
      setConfigs((list) => list.filter((c) => c.id !== id))
      if (activeId === id) {
        clearPending()
        setActiveId(null)
        setConn({ kind: 'disconnected' })
      }
    },
    [activeId, clearPending],
  )

  const duplicateConfig = useCallback((id: ConfigId) => {
    setConfigs((list) => {
      const src = list.find((c) => c.id === id)
      if (!src) return list
      const copy: Config = {
        ...src,
        id: `${src.id}-${Math.random().toString(36).slice(2, 7)}`,
        name: `${src.name} (copy)`,
        addedAt: now(),
        lastUsedAt: undefined,
      }
      return [copy, ...list]
    })
  }, [])

  const api = useMemo<StoreApi>(
    () => ({
      configs,
      activeId,
      conn,
      theme,
      toggleTheme,
      selectConfig,
      toggleConnection,
      addConfig,
      updateConfig,
      deleteConfig,
      duplicateConfig,
    }),
    [
      configs,
      activeId,
      conn,
      theme,
      toggleTheme,
      selectConfig,
      toggleConnection,
      addConfig,
      updateConfig,
      deleteConfig,
      duplicateConfig,
    ],
  )

  return <StoreContext.Provider value={api}>{children}</StoreContext.Provider>
}

export function useStore(): StoreApi {
  const ctx = useContext(StoreContext)
  if (!ctx) throw new Error('useStore must be used inside <StoreProvider>')
  return ctx
}
