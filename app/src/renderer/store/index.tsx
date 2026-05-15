import { useCallback, useEffect, useState, type ReactNode } from 'react'

import { type AppState, type Command, type ThemePreference } from '@shared/types'

import { type CommandError, StoreContext, type StoreApi } from './context'

const cyclePreference = (cur: ThemePreference): ThemePreference =>
  cur === 'light' ? 'dark' : cur === 'dark' ? 'system' : 'light'

export function StoreProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AppState | null>(null)
  const [lastError, setLastError] = useState<CommandError | null>(null)

  useEffect(() => {
    return window.elepn.subscribe(setState)
  }, [])

  // Wraps every command dispatch: on IPC rejection (main threw — origin
  // check, shape validation, or domain rule) surface as `lastError` for the
  // UI to display. Without this, rejections vanished into `void`.
  const send = useCallback((cmd: Command) => {
    window.elepn.command(cmd).catch((err: unknown) => {
      const message = err instanceof Error ? err.message : String(err)
      setLastError({ message, at: Date.now() })
    })
  }, [])

  if (!state) return null

  const api: StoreApi = {
    ...state,
    lastError,
    selectConfig: id => {
      send({ type: 'selectConfig', id })
    },
    toggleConnection: () => {
      send({ type: 'toggleConnection' })
    },
    addConfig: (config, activate = false) => {
      send({ type: 'addConfig', config, activate })
    },
    updateConfig: (id, patch) => {
      send({ type: 'updateConfig', id, patch })
    },
    deleteConfig: id => {
      send({ type: 'deleteConfig', id })
    },
    duplicateConfig: id => {
      send({ type: 'duplicateConfig', id })
    },
    toggleTheme: () => {
      send({ type: 'setThemePreference', preference: cyclePreference(state.themePreference) })
    },
    dismissError: () => {
      setLastError(null)
    },
  }

  return <StoreContext.Provider value={api}>{children}</StoreContext.Provider>
}
