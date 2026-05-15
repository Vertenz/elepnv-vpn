import { useContext } from 'react'

import { StoreContext, type StoreApi } from './context'

export function useStore(): StoreApi {
  const ctx = useContext(StoreContext)
  if (!ctx) throw new Error('useStore must be used inside <StoreProvider>')
  return ctx
}
