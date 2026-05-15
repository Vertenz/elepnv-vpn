import { createContext } from 'react'

import { type AppState, type Config, type ConfigId, type ConfigPatch } from '@shared/types'

export interface StoreApi extends AppState {
  selectConfig: (id: ConfigId) => void
  toggleConnection: () => void
  addConfig: (config: Config, activate?: boolean) => void
  updateConfig: (id: ConfigId, patch: ConfigPatch) => void
  deleteConfig: (id: ConfigId) => void
  duplicateConfig: (id: ConfigId) => void
  toggleTheme: () => void
}

export const StoreContext = createContext<StoreApi | undefined>(undefined)
