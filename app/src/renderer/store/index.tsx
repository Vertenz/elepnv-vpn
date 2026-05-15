import { useEffect, useState, type ReactNode } from 'react'

import { type AppState, type ThemePreference } from '@shared/types'

import { StoreContext, type StoreApi } from './context'

const cyclePreference = (cur: ThemePreference): ThemePreference =>
  cur === 'light' ? 'dark' : cur === 'dark' ? 'system' : 'light'

export function StoreProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AppState | null>(null)

  useEffect(() => {
    // subscribe() also triggers main to send the current snapshot, so the
    // very first setState IS the initial state. No getState(), no race.
    return window.elepn.subscribe(setState)
  }, [])

  if (!state) return null

  const api: StoreApi = {
    ...state,
    selectConfig: id => {
      void window.elepn.command({ type: 'selectConfig', id })
    },
    toggleConnection: () => {
      void window.elepn.command({ type: 'toggleConnection' })
    },
    addConfig: (config, activate = false) => {
      void window.elepn.command({ type: 'addConfig', config, activate })
    },
    updateConfig: (id, patch) => {
      void window.elepn.command({ type: 'updateConfig', id, patch })
    },
    deleteConfig: id => {
      void window.elepn.command({ type: 'deleteConfig', id })
    },
    duplicateConfig: id => {
      void window.elepn.command({ type: 'duplicateConfig', id })
    },
    toggleTheme: () => {
      void window.elepn.command({
        type: 'setThemePreference',
        preference: cyclePreference(state.themePreference),
      })
    },
  }

  return <StoreContext.Provider value={api}>{children}</StoreContext.Provider>
}

// Temporary compat re-export so App.tsx (which still imports `useStore` from
// `./store`) keeps compiling. Removed in Task 15 after App.tsx is updated.
// eslint-disable-next-line react-refresh/only-export-components
export { useStore } from './use-store'
