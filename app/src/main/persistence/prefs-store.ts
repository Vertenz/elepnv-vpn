import fs from 'node:fs'
import path from 'node:path'

import { app } from 'electron'
import writeFileAtomic from 'write-file-atomic'

import  { type Config, type ThemePreference } from '@shared/types'

const FILE_VERSION = 1
const FLUSH_DEBOUNCE_MS = 200

interface PersistedFields {
  themePreference: ThemePreference
  configs: Config[]
}

interface PrefsFile extends PersistedFields {
  version: number
}

export interface LoadResult {
  store: PrefsStore
  wasMissing: boolean
}

export class PrefsStore {
  private current: PrefsFile
  private dirty = false
  private flushTimer: NodeJS.Timeout | null = null

  private constructor(private readonly filePath: string, initial: PrefsFile) {
    this.current = initial
  }

  static loadSync(): LoadResult {
    const filePath = path.join(app.getPath('userData'), 'prefs.json')
    let prefs: PrefsFile
    let wasMissing = false
    try {
      const raw = fs.readFileSync(filePath, 'utf-8')
      const parsed = JSON.parse(raw) as Partial<PrefsFile>
      prefs = {
        version: typeof parsed.version === 'number' ? parsed.version : FILE_VERSION,
        themePreference: validateTheme(parsed.themePreference),
        configs: Array.isArray(parsed.configs) ? parsed.configs : [],
      }
    } catch (err) {
      const code = (err as NodeJS.ErrnoException).code
      if (code !== 'ENOENT') {
        console.warn('[PrefsStore] load failed, starting empty:', err)
      }
      prefs = { version: FILE_VERSION, themePreference: 'system', configs: [] }
      wasMissing = code === 'ENOENT'
    }
    return { store: new PrefsStore(filePath, prefs), wasMissing }
  }

  snapshot(): PersistedFields {
    return { themePreference: this.current.themePreference, configs: this.current.configs }
  }

  update(patch: Partial<PersistedFields>): void {
    this.current = { ...this.current, ...patch }
    this.dirty = true
    this.scheduleFlush()
  }

  flushSync(): void {
    if (this.flushTimer) {
      clearTimeout(this.flushTimer)
      this.flushTimer = null
    }
    if (!this.dirty) return
    try {
      writeFileAtomic.sync(this.filePath, JSON.stringify(this.current, null, 2), 'utf-8')
      this.dirty = false
    } catch (err) {
      console.warn('[PrefsStore] flushSync failed:', err)
    }
  }

  private scheduleFlush(): void {
    if (this.flushTimer) return
    this.flushTimer = setTimeout(() => {
      this.flushTimer = null
      void this.flush()
    }, FLUSH_DEBOUNCE_MS)
  }

  private async flush(): Promise<void> {
    if (!this.dirty) return
    const payload = JSON.stringify(this.current, null, 2)
    this.dirty = false
    try {
      await writeFileAtomic(this.filePath, payload, 'utf-8')
    } catch (err) {
      console.warn('[PrefsStore] flush failed:', err)
      this.dirty = true
    }
  }
}

function validateTheme(t: unknown): ThemePreference {
  return t === 'light' || t === 'dark' || t === 'system' ? t : 'system'
}
