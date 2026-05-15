import { useCallback, useEffect, useState } from 'react'

import  { type Config, type ConnState } from '@shared/types'

import { useStore } from '../store/use-store'

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

type Overlay =
  | { kind: 'none' }
  | { kind: 'picker' }
  | { kind: 'add'; editing?: Config }

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

const ERROR_AUTODISMISS_MS = 4000

export function MainScreen() {
  const {
    configs,
    activeId,
    conn,
    themePreference,
    lastError,
    toggleConnection,
    selectConfig,
    addConfig,
    updateConfig,
    deleteConfig,
    duplicateConfig,
    toggleTheme,
    dismissError,
  } = useStore()

  const [overlay, setOverlay] = useState<Overlay>({ kind: 'none' })
  const [bodyEl, setBodyEl] = useState<HTMLDivElement | null>(null)
  const bodyRef = useCallback((node: HTMLDivElement | null) => {
    setBodyEl(node)
  }, [])

  const activeConfig = configs.find(c => c.id === activeId) ?? null
  const state = toPowerState(conn)
  const overlayOpen = overlay.kind !== 'none'

  // Auto-dismiss the error toast a few seconds after it appears.
  useEffect(() => {
    if (!lastError) return
    const id = window.setTimeout(dismissError, ERROR_AUTODISMISS_MS)
    return () => { window.clearTimeout(id) }
  }, [lastError, dismissError])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLElement && e.target.matches('input, textarea')) return
      if (e.key.toLowerCase() !== 'k' || !(e.metaKey || e.ctrlKey)) return
      e.preventDefault()
      setOverlay(cur => {
        // Cmd+K only toggles picker. AddSheet is invariant under Cmd+K
        // (user closes it via Esc/click-outside/Cancel).
        if (cur.kind === 'add') return cur
        return cur.kind === 'picker' ? { kind: 'none' } : { kind: 'picker' }
      })
    }
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('keydown', onKey)
    }
  }, [])

  return (
    <HiWindow title="elepn">
      <div ref={bodyRef} className="window-body__inner">
        <BottomSheetContainerProvider container={bodyEl}>
          <div className="main" data-dim={overlayOpen}>
            <HiHeader
              themePreference={themePreference}
              onToggleTheme={toggleTheme}
            />

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
                    setOverlay({ kind: 'picker' })
                  }}
                />
              ) : (
                <button
                  className="empty-card"
                  type="button"
                  onClick={() => {
                    setOverlay({ kind: 'picker' })
                  }}
                >
                  No config selected — tap to pick one
                </button>
              )}
              <div className="footer-meta">
                <span className="footer-meta__cell mono">{configs.length} saved configs</span>
                {/* TODO(daemon): real throughput when DaemonEngine reports xray-core stats. */}
                <span className="footer-meta__cell mono">
                  {conn.kind === 'connected' ? '↑ 1.2 MB/s  ·  ↓ 8.4 MB/s' : '—'}
                </span>
              </div>
            </div>
          </div>

          <PickerPanel
            activeId={activeId}
            configs={configs}
            open={overlay.kind === 'picker'}
            onAddRequested={() => {
              setOverlay({ kind: 'add' })
            }}
            onDelete={id => {
              deleteConfig(id)
            }}
            onDuplicate={id => {
              duplicateConfig(id)
            }}
            onEditUrl={id => {
              const cfg = configs.find(c => c.id === id)
              if (cfg) setOverlay({ kind: 'add', editing: cfg })
            }}
            onOpenChange={open => {
              setOverlay(open ? { kind: 'picker' } : { kind: 'none' })
            }}
            onRename={(id, name) => {
              updateConfig(id, { name })
            }}
            onSelect={id => {
              selectConfig(id)
            }}
          />

          {lastError && (
            <div className="error-toast" role="alert">
              <span className="error-toast__msg mono">{lastError.message}</span>
              <button
                aria-label="Dismiss error"
                className="error-toast__close"
                type="button"
                onClick={dismissError}
              >
                ×
              </button>
            </div>
          )}

          <AddSheet
            editing={overlay.kind === 'add' ? overlay.editing : undefined}
            open={overlay.kind === 'add'}
            onOpenChange={open => {
              if (!open) setOverlay({ kind: 'none' })
            }}
            onSave={({ config, activateNow }) => {
              // AddSheet calls onOpenChange(false) right after onSave, which
              // closes the overlay — we don't need to setOverlay here.
              if (overlay.kind === 'add' && overlay.editing) {
                updateConfig(overlay.editing.id, {
                  url: config.url,
                  host: config.host,
                  proto: config.proto,
                  variant: config.variant,
                  country: config.country,
                })
              } else {
                addConfig(config, activateNow)
              }
            }}
          />
        </BottomSheetContainerProvider>
      </div>
    </HiWindow>
  )
}
