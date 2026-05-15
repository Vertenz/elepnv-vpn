import { contextBridge, ipcRenderer, type IpcRendererEvent } from 'electron'

import { COMMAND, STATE_CHANGED, STATE_SUBSCRIBE } from '@shared/ipc-channels'
import  { type AppState, type Command } from '@shared/types'

export interface ElepnApi {
  /**
   * Subscribe to AppState snapshots. Calling subscribe also asks main for
   * the current snapshot, which arrives via the same STATE_CHANGED channel
   * — so the first invocation of `cb` IS the initial state. No separate
   * `getState()` exists, which eliminates the race where a concurrent
   * broadcast could overwrite a slower getState() reply.
   *
   * Returns an unsubscribe function.
   */
  subscribe(cb: (s: AppState) => void): () => void

  /**
   * Dispatch a typed command to main. Resolves when main has applied it
   * (the corresponding state broadcast is already in flight, but may not
   * have been delivered yet — callers MUST NOT read state via useStore()
   * synchronously after `await command(...)`).
   *
   * Rejects on origin or shape validation failure.
   */
  command(cmd: Command): Promise<void>
}

const api: ElepnApi = {
  subscribe(cb) {
    const listener = (_event: IpcRendererEvent, snapshot: AppState) => {
      cb(snapshot)
    }
    ipcRenderer.on(STATE_CHANGED, listener)
    ipcRenderer.send(STATE_SUBSCRIBE)
    return () => {
      ipcRenderer.off(STATE_CHANGED, listener)
    }
  },

  async command(cmd) {
    await ipcRenderer.invoke(COMMAND, cmd)
  },
}

contextBridge.exposeInMainWorld('elepn', api)
