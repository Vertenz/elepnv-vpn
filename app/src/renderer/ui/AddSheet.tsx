import * as Dialog from '@radix-ui/react-dialog'
import { useMemo, useState } from 'react'

import { parseConfigUrl } from '@shared/parse-url'
import  { type Config } from '@shared/types'

import { BottomSheet } from './BottomSheet'
import { IconAlert, IconCheck, IconPlus, IconX } from './icons'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing?: Config
  onSave: (data: { config: Config; activateNow: boolean }) => void
}

const URL_PLACEHOLDER = 'vless://… or vmess://… or ss://… or trojan://…'

export function AddSheet({ open, onOpenChange, editing, onSave }: Props) {
  return (
    <BottomSheet
      describedBy="add-subtitle"
      labelledBy="add-title"
      open={open}
      onOpenChange={onOpenChange}
    >
      <AddSheetForm editing={editing} onOpenChange={onOpenChange} onSave={onSave} />
    </BottomSheet>
  )
}

// Lives inside <Dialog.Portal>, which un-mounts on close — so local state
// resets on every fresh open without an effect.
function AddSheetForm({
  editing,
  onOpenChange,
  onSave,
}: {
  editing?: Config
  onOpenChange: (open: boolean) => void
  onSave: (data: { config: Config; activateNow: boolean }) => void
}) {
  const isEdit = editing != null
  const [url, setUrl] = useState(editing?.url ?? '')
  const [activateNow, setActivateNow] = useState(true)

  const parsed = useMemo(() => parseConfigUrl(url), [url])
  const empty = url.trim().length === 0
  const valid = parsed.ok

  return (
    <div className="add-sheet">
      <div className="add-sheet__head">
        <div>
          <Dialog.Title className="sheet__title sheet__title--lg" id="add-title">
            {isEdit ? 'Edit config' : 'Add new config'}
          </Dialog.Title>
          <Dialog.Description className="sheet__subtitle mono" id="add-subtitle">
            Paste a vless://, vmess://, ss:// or trojan:// URL
          </Dialog.Description>
        </div>
        <Dialog.Close asChild>
          <button aria-label="Close" className="icon-btn icon-btn--ghost" type="button">
            <IconX size={16} />
          </button>
        </Dialog.Close>
      </div>

      <div className="add-sheet__url-field">
        <div className="cap" style={{ marginBottom: 6 }}>
          URL
        </div>
        <textarea
          className="add-sheet__url-input"
          placeholder={URL_PLACEHOLDER}
          rows={3}
          spellCheck={false}
          value={url}
          onChange={e => {
            setUrl(e.target.value)
          }}
        />
        <span aria-hidden className="caret" />
      </div>

      <div style={{ marginTop: 14 }}>
        <div className="detected-row">
          <span className="cap">Detected</span>
          <span className="detected-row__rule" />
          {empty ? (
            <span className="detected-row__status" style={{ color: 'var(--muted)' }}>
              Waiting for input…
            </span>
          ) : valid ? (
            <span className="detected-row__status detected-row__status--ok">
              <IconCheck size={14} stroke={2} /> Valid
            </span>
          ) : (
            <span className="detected-row__status detected-row__status--bad">
              <IconAlert size={14} stroke={2} /> Invalid · {parsed.reason}
            </span>
          )}
        </div>

        {valid && (
          <div className="kv-grid">
            {parsed.preview.extras
              .filter(kv => !(isEdit && kv.k === 'name'))
              .map(kv => (
                <KV key={kv.k} k={kv.k} v={kv.v} />
              ))}
          </div>
        )}
      </div>

      <div className="add-sheet__actions">
        {isEdit ? (
          <span />
        ) : (
          <label className="checkbox-label">
            <input
              checked={activateNow}
              type="checkbox"
              onChange={e => {
                setActivateNow(e.target.checked)
              }}
            />
            Activate immediately
          </label>
        )}
        <div style={{ display: 'flex', gap: 8 }}>
          <Dialog.Close asChild>
            <button className="btn btn--ghost" type="button">
              Cancel
            </button>
          </Dialog.Close>
          <button
            className="btn btn--primary"
            disabled={!valid}
            type="button"
            onClick={() => {
              if (!parsed.ok) return
              const built = parsed.build()
              if (editing) {
                // Preserve original id, addedAt, name, ping, lastUsedAt.
                // Only URL-derived fields change.
                onSave({
                  config: {
                    ...editing,
                    url: built.url,
                    host: built.host,
                    proto: built.proto,
                    variant: built.variant,
                    country: built.country,
                  },
                  activateNow: false,
                })
              } else {
                onSave({ config: built, activateNow })
              }
              onOpenChange(false)
            }}
          >
            <IconPlus size={14} stroke={2} />{' '}
            {isEdit ? 'Save changes' : 'Save config'}
          </button>
        </div>
      </div>
    </div>
  )
}

function KV({ k, v }: { k: string; v: string }) {
  return (
    <div className="kv">
      <span className="kv__key mono">{k}</span>
      <span className="kv__value mono">{v}</span>
    </div>
  )
}
