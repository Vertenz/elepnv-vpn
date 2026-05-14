import path from 'node:path'

import { app, BrowserWindow, session } from 'electron'
import { downloadChromeExtension } from 'electron-devtools-installer/dist/downloadChromeExtension'
import started from 'electron-squirrel-startup'

// Handle creating/removing shortcuts on Windows when installing/uninstalling.
if (started) {
  app.quit()
}

const createWindow = () => {
  // Create the browser window.
  const mainWindow = new BrowserWindow({
    width: 800,
    height: 600,
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      preload: path.join(__dirname, 'preload.js'),
      sandbox: true,
    },
  })

  mainWindow.webContents.setWindowOpenHandler(() => ({ action: 'deny' }))
  mainWindow.webContents.on('will-navigate', (event) => {
    event.preventDefault()
  })

  // and load the index.html of the app.
  if (MAIN_WINDOW_VITE_DEV_SERVER_URL) {
    void mainWindow.loadURL(MAIN_WINDOW_VITE_DEV_SERVER_URL)
  } else {
    void mainWindow.loadFile(
      path.join(__dirname, `../renderer/${MAIN_WINDOW_VITE_NAME}/index.html`),
    )
  }

  if (!app.isPackaged) {
    mainWindow.webContents.openDevTools()
  }
}

const installDevTools = async () => {
  if (app.isPackaged) {
    return
  }

  try {
    const reactDevToolsId = 'fmkadmapgofadopljbjfkapdkoienihi'
    const installedExtension = session.defaultSession.extensions
      .getAllExtensions()
      .find((extension) => extension.id === reactDevToolsId)

    if (installedExtension) {
      return
    }

    const extensionPath = await downloadChromeExtension(reactDevToolsId)
    await session.defaultSession.extensions.loadExtension(extensionPath, {
      allowFileAccess: true,
    })
  } catch (error) {
    console.warn('React DevTools installation failed:', error)
  }
}

// This method will be called when Electron has finished
// initialization and is ready to create browser windows.
// Some APIs can only be used after this event occurs.
app.whenReady().then(async () => {
  await installDevTools()
  createWindow()
})

// Quit when all windows are closed, except on macOS. There, it's common
// for applications and their menu bar to stay active until the user quits
// explicitly with Cmd + Q.
app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    app.quit()
  }
})

app.on('activate', () => {
  // On OS X it's common to re-create a window in the app when the
  // dock icon is clicked and there are no other windows open.
  if (BrowserWindow.getAllWindows().length === 0) {
    createWindow()
  }
})

// In this file you can include the rest of your app's specific main process
// code. You can also put them in separate files and import them here.
