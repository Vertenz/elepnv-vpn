import { createContext } from 'react'

import { type AppState, type Config, type ConfigId, type ConfigPatch } from '@shared/types'

// Transient renderer-side error surface. Populated when an IPC command is
// rejected by main (origin or shape validation, or a thrown domain rule).
// Lives in the renderer, not in AppState, because it's per-window UI state.
export interface CommandError {
  message: string
  at: number
}

export interface StoreApi extends AppState {
  lastError: CommandError | null
  selectConfig: (id: ConfigId) => void
  toggleConnection: () => void
  addConfig: (config: Config, activate?: boolean) => void
  updateConfig: (id: ConfigId, patch: ConfigPatch) => void
  deleteConfig: (id: ConfigId) => void
  duplicateConfig: (id: ConfigId) => void
  toggleTheme: () => void
  dismissError: () => void
}

export const StoreContext = createContext<StoreApi | undefined>(undefined)
