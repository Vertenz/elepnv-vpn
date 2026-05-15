import type { IpcMainEvent, IpcMainInvokeEvent, WebContents } from 'electron'
import { ipcMain } from 'electron'

import type { AppState, Command, Config, ConfigPatch, ThemePreference } from '@shared/types'
import { COMMAND, STATE_CHANGED, STATE_SUBSCRIBE } from '@shared/ipc-channels'

import type { AppStore } from '../store/app-store'

interface RegisterOptions {
  /** URL prefix that the renderer's top frame must start with. In dev:
   *  the Vite dev server URL; in packaged builds: `file://`. */
  expectedOrigin: string
}

/**
 * Wire main-process IPC. Returns a cleanup function for tests / shutdown.
 *
 * Three layers of validation at the boundary (in order):
 *  1. Frame check — only top-frame messages from `expectedOrigin`.
 *  2. Shape validation — discriminant + payload shape per command.
 *  3. Domain rules — handled by AppStore (id existence, allow-list,
 *     value ranges). These throw; the invoke handler converts to reject.
 */
export function registerIpc(store: AppStore, opts: RegisterOptions): () => void {
  const subscribers = new Set<WebContents>()

  const broadcast = (state: AppState) => {
    for (const wc of subscribers) {
      if (!wc.isDestroyed()) {
        wc.send(STATE_CHANGED, state)
      }
    }
  }

  store.on('change', broadcast)

  const validateFrame = (event: IpcMainInvokeEvent | IpcMainEvent): boolean => {
    const frame = event.senderFrame
    if (!frame) return false
    if (frame.parent !== null) return false
    return frame.url.startsWith(opts.expectedOrigin)
  }

  const subscribeHandler = (event: IpcMainEvent) => {
    if (!validateFrame(event)) {
      console.warn('[ipc] subscribe rejected: untrusted frame')
      return
    }
    const wc = event.sender
    if (!subscribers.has(wc)) {
      subscribers.add(wc)
      wc.once('destroyed', () => {
        subscribers.delete(wc)
      })
    }
    // Always send the current snapshot in response to subscribe —
    // this is the renderer's "initial state" path. No separate getState.
    wc.send(STATE_CHANGED, store.snapshot())
  }

  const commandHandler = async (event: IpcMainInvokeEvent, raw: unknown): Promise<void> => {
    if (!validateFrame(event)) {
      throw new Error('elepn:command rejected: untrusted frame')
    }
    const cmd = parseCommand(raw)
    store.dispatch(cmd)
  }

  ipcMain.on(STATE_SUBSCRIBE, subscribeHandler)
  ipcMain.handle(COMMAND, commandHandler)

  return () => {
    ipcMain.removeListener(STATE_SUBSCRIBE, subscribeHandler)
    ipcMain.removeHandler(COMMAND)
    store.off('change', broadcast)
    subscribers.clear()
  }
}

function parseCommand(raw: unknown): Command {
  if (!raw || typeof raw !== 'object') throw new Error('command: not an object')
  const obj = raw as Record<string, unknown>
  const type = obj.type
  switch (type) {
    case 'toggleConnection':
      return { type: 'toggleConnection' }

    case 'selectConfig':
    case 'deleteConfig':
    case 'duplicateConfig': {
      if (typeof obj.id !== 'string' || !obj.id) {
        throw new Error(`${type}: id required`)
      }
      return { type, id: obj.id }
    }

    case 'addConfig': {
      if (!obj.config || typeof obj.config !== 'object') {
        throw new Error('addConfig: config required')
      }
      const activate = obj.activate === true
      // Deep validation happens in AppStore.addConfig (validateConfig).
      return { type: 'addConfig', config: obj.config as Config, activate }
    }

    case 'updateConfig': {
      if (typeof obj.id !== 'string' || !obj.id) throw new Error('updateConfig: id required')
      if (!obj.patch || typeof obj.patch !== 'object') throw new Error('updateConfig: patch required')
      return { type: 'updateConfig', id: obj.id, patch: obj.patch as ConfigPatch }
    }

    case 'setThemePreference': {
      const p = obj.preference
      if (p !== 'light' && p !== 'dark' && p !== 'system') {
        throw new Error('setThemePreference: invalid preference')
      }
      return { type: 'setThemePreference', preference: p as ThemePreference }
    }

    default:
      throw new Error(`unknown command type: ${String(type)}`)
  }
}
