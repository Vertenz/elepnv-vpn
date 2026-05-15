import { useCallback, useEffect, useState } from 'react'

import { useStore } from '../store/use-store'
import type { Config, ConnState } from '@shared/types'

import { ActiveConfigCard } from './ActiveConfigCard'
import { AddSheet } from './AddSheet'
import { BottomSheetContainerProvider } from './BottomSheet'
import { HiHeader } from './Header'
import { IconRefresh } from './icons'
import { PickerPanel } from './PickerPanel'
import { PowerButton } from './PowerButton'
import { StatusBlock } from './StatusBlock'
import { HiWindow } from './Window'

type PowerState = 'disconnected' | 'connecting' | 'connected' | 'error'

function toPowerState(conn: ConnState): PowerState {
  switch (conn.kind) {
    case 'connected':
      return 'connected'
    case 'connecting':
    case 'disconnecting':
      return 'connecting'
    case 'error':
      return 'error'
    case 'disconnected':
      return 'disconnected'
    default: {
      const _exhaustive: never = conn
      void _exhaustive
      return 'disconnected'
    }
  }
}

export function MainScreen() {
  const {
    configs,
    activeId,
    conn,
    toggleConnection,
    selectConfig,
    addConfig,
    updateConfig,
    deleteConfig,
    duplicateConfig,
    toggleTheme,
  } = useStore()

  const [pickerOpen, setPickerOpen] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const [bodyEl, setBodyEl] = useState<HTMLDivElement | null>(null)
  const bodyRef = useCallback((node: HTMLDivElement | null) => {
    setBodyEl(node)
  }, [])

  const activeConfig =
    configs.find((c: Config) => c.id === activeId) ??
    configs.find(
      (c: Config) =>
        (conn.kind === 'connected' && conn.config === c.id) ||
        (conn.kind === 'connecting' && conn.target === c.id),
    ) ??
    null

  const state = toPowerState(conn)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLElement && e.target.matches('input, textarea')) return
      if (e.key.toLowerCase() === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        setPickerOpen((o) => !o)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('keydown', onKey)
    }
  }, [])

  const overlayOpen = pickerOpen || addOpen

  return (
    <HiWindow title="elepn">
      <div ref={bodyRef} className="window-body__inner">
        <BottomSheetContainerProvider container={bodyEl}>
          <div className="main" data-dim={overlayOpen}>
            <HiHeader onRouting={() => void 0} onSettings={toggleTheme} />

            <div className="hero">
              <PowerButton state={state} onClick={toggleConnection} />
              <StatusBlock config={activeConfig} conn={conn} />
              {state === 'error' && (
                <button className="retry-btn" type="button" onClick={toggleConnection}>
                  <IconRefresh size={13} stroke={1.8} /> Retry now
                </button>
              )}
            </div>

            <div className="footer-section">
              <div className="cap" style={{ marginBottom: 8 }}>
                Active config
              </div>
              {activeConfig ? (
                <ActiveConfigCard
                  config={activeConfig}
                  ping={conn.kind === 'connected' ? conn.ping : (activeConfig.ping ?? null)}
                  onClick={() => {
                    setPickerOpen(true)
                  }}
                />
              ) : (
                <button
                  className="empty-card"
                  type="button"
                  onClick={() => {
                    setPickerOpen(true)
                  }}
                >
                  No config selected — tap to pick one
                </button>
              )}
              <div className="footer-meta">
                <span className="footer-meta__cell mono">{configs.length} saved configs</span>
                <span className="footer-meta__cell mono">
                  {conn.kind === 'connected' ? '↑ 1.2 MB/s  ·  ↓ 8.4 MB/s' : '—'}
                </span>
              </div>
            </div>
          </div>

          <PickerPanel
            activeId={activeId}
            configs={configs}
            open={pickerOpen}
            onAddRequested={() => {
              setPickerOpen(false)
              setAddOpen(true)
            }}
            onDelete={deleteConfig}
            onDuplicate={duplicateConfig}
            onEditUrl={() => {
              setPickerOpen(false)
              setAddOpen(true)
            }}
            onOpenChange={setPickerOpen}
            onRename={(id, name) => {
              updateConfig(id, { name })
            }}
            onSelect={selectConfig}
          />

          <AddSheet
            open={addOpen}
            onOpenChange={setAddOpen}
            onSave={(config, activateNow) => {
              addConfig(config)
              if (activateNow) selectConfig(config.id)
            }}
          />
        </BottomSheetContainerProvider>
      </div>
    </HiWindow>
  )
}
