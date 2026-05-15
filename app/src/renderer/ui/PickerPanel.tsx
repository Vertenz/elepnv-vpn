import * as Dialog from '@radix-ui/react-dialog'
import { useMemo, useState } from 'react'

import  { type Config, type ConfigId } from '@shared/types'

import { BottomSheet } from './BottomSheet'
import { ConfigRow } from './ConfigRow'
import { IconLink, IconPlus, IconSearch, IconX } from './icons'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  configs: Config[]
  activeId: ConfigId | null
  onSelect: (id: ConfigId) => void
  onAddRequested: () => void
  onRename: (id: ConfigId, name: string) => void
  onEditUrl: (id: ConfigId) => void
  onDuplicate: (id: ConfigId) => void
  onDelete: (id: ConfigId) => void
}

export function PickerPanel({
  open,
  onOpenChange,
  configs,
  activeId,
  onSelect,
  onAddRequested,
  onRename,
  onEditUrl,
  onDuplicate,
  onDelete,
}: Props) {
  const [query, setQuery] = useState('')

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return configs
    return configs.filter((c) =>
      `${c.name} ${c.country} ${c.variant} ${c.host}`.toLowerCase().includes(q),
    )
  }, [configs, query])

  return (
    <BottomSheet labelledBy="picker-title" open={open} onOpenChange={onOpenChange}>
      <div className="sheet__head">
        <div className="sheet__handle" />
        <div className="sheet__header-row">
          <Dialog.Title className="sheet__title" id="picker-title">
            Choose a config
          </Dialog.Title>
          <Dialog.Close asChild>
            <button aria-label="Close" className="icon-btn icon-btn--ghost" type="button">
              <IconX size={16} />
            </button>
          </Dialog.Close>
        </div>

        <label className="search">
          <IconSearch size={14} />
          <input
            className="search__input"
            placeholder="Filter by name, country, protocol…"
            type="text"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
            }}
          />
        </label>
      </div>

      <div className="add-row-wrap">
        <button className="add-row" type="button" onClick={onAddRequested}>
          <span className="add-row__icon">
            <IconPlus size={16} stroke={2.2} />
          </span>
          <span className="add-row__body">
            <span className="add-row__title">Add new config</span>
            <span className="add-row__hint mono">paste vless:// vmess:// ss:// or trojan://</span>
          </span>
          <IconLink size={14} />
        </button>
      </div>

      <div className="config-list">
        {filtered.length === 0 && (
          <div className="config-list__empty mono">No configs match “{query}”</div>
        )}
        {filtered.map((c) => (
          <ConfigRow
            key={c.id}
            active={c.id === activeId}
            config={c}
            onDelete={onDelete}
            onDuplicate={onDuplicate}
            onEditUrl={onEditUrl}
            onRename={onRename}
            onSelect={(id) => {
              onSelect(id)
              onOpenChange(false)
            }}
          />
        ))}
      </div>
    </BottomSheet>
  )
}
