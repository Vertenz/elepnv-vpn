// Channel name constants shared by main, preload, and renderer.
// Single source of truth — no string literals elsewhere.

export const STATE_SUBSCRIBE = 'elepn:state:subscribe' as const
export const STATE_CHANGED = 'elepn:state:changed' as const
export const COMMAND = 'elepn:command' as const

export type IpcChannel =
  | typeof STATE_SUBSCRIBE
  | typeof STATE_CHANGED
  | typeof COMMAND
