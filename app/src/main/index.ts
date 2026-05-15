import path from 'node:path'

import { app, BrowserWindow, Menu, nativeTheme, session } from 'electron'
import { downloadChromeExtension } from 'electron-devtools-installer/dist/downloadChromeExtension'
import started from 'electron-squirrel-startup'

import type { AppState } from '@shared/types'

import { MockEngine } from './engine/mock-engine'
import { registerIpc } from './ipc/handlers'
import { PrefsStore } from './persistence/prefs-store'
import { AppStore } from './store/app-store'
import { buildSampleConfigs } from './store/seed'

// Handle creating/removing shortcuts on Windows when installing/uninstalling.
if (started) {
  app.quit()
}

let prefsStore: PrefsStore | null = null

const SEED_DISABLED = process.env.ELEPN_SEED === '0'

function bootstrap(): void {
  const { store: prefs, wasMissing } = PrefsStore.loadSync()
  prefsStore = prefs

  if (wasMissing && !app.isPackaged && !SEED_DISABLED) {
    prefs.update({ configs: buildSampleConfigs() })
  }

  const persisted = prefs.snapshot()
  const theme =
    persisted.themePreference === 'system'
      ? nativeTheme.shouldUseDarkColors
        ? 'dark'
        : 'light'
      : persisted.themePreference

  const initial: AppState = {
    configs: persisted.configs,
    activeId: persisted.configs[0]?.id ?? null,
    conn: { kind: 'disconnected' },
    theme,
    themePreference: persisted.themePreference,
  }

  const engine = new MockEngine()
  const store = new AppStore(engine, prefs, initial)

  nativeTheme.on('updated', () => {
    store.onSystemThemeChanged()
  })

  const expectedOrigin = MAIN_WINDOW_VITE_DEV_SERVER_URL ?? 'file://'
  registerIpc(store, { expectedOrigin })
}

const createWindow = () => {
  // Fixed 720x520 frameless window — the renderer paints its own title bar.
  const mainWindow = new BrowserWindow({
    width: 720,
    height: 520,
    minWidth: 720,
    minHeight: 520,
    maxWidth: 720,
    maxHeight: 520,
    resizable: false,
    maximizable: false,
    fullscreenable: false,
    frame: false,
    titleBarStyle: 'hidden',
    title: 'elepn',
    backgroundColor: '#7a7a7a',
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      preload: path.join(__dirname, 'preload.js'),
      sandbox: true,
    },
  })

  mainWindow.webContents.setWindowOpenHandler(() => ({ action: 'deny' }))
  mainWindow.webContents.on('will-navigate', event => {
    event.preventDefault()
  })

  if (MAIN_WINDOW_VITE_DEV_SERVER_URL) {
    void mainWindow.loadURL(MAIN_WINDOW_VITE_DEV_SERVER_URL)
  } else {
    void mainWindow.loadFile(
      path.join(__dirname, `../renderer/${MAIN_WINDOW_VITE_NAME}/index.html`),
    )
  }

  if (!app.isPackaged) {
    mainWindow.webContents.openDevTools({ mode: 'detach' })
  }

  return mainWindow
}

const installDevTools = async () => {
  if (app.isPackaged) return
  try {
    const reactDevToolsId = 'fmkadmapgofadopljbjfkapdkoienihi'
    const installedExtension = session.defaultSession.extensions
      .getAllExtensions()
      .find(extension => extension.id === reactDevToolsId)
    if (installedExtension) return
    const extensionPath = await downloadChromeExtension(reactDevToolsId)
    await session.defaultSession.extensions.loadExtension(extensionPath, {
      allowFileAccess: true,
    })
  } catch (error) {
    console.warn('React DevTools installation failed:', error)
  }
}

void app.whenReady().then(() => {
  Menu.setApplicationMenu(null)
  bootstrap()
  createWindow()
  void installDevTools()
})

app.on('before-quit', () => {
  prefsStore?.flushSync()
})

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    app.quit()
  }
})

app.on('activate', () => {
  if (BrowserWindow.getAllWindows().length === 0) {
    createWindow()
  }
})
