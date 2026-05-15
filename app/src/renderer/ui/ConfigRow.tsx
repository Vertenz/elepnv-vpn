import * as ContextMenu from '@radix-ui/react-context-menu'
import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { useLayoutEffect, useMemo, useRef, useState } from 'react'
import { type ReactNode } from 'react'

import type { Config, ConfigId } from '@shared/types'

import { Flag, IconCopy, IconEdit, IconLink, IconMore, IconPower, IconTrash, IconX } from './icons'

interface Props {
  config: Config
  active: boolean
  onSelect: (id: ConfigId) => void
  onRename: (id: ConfigId, name: string) => void
  onEditUrl: (id: ConfigId) => void
  onDuplicate: (id: ConfigId) => void
  onDelete: (id: ConfigId) => void
}

type MenuItem =
  | { kind: 'divider'; key: string }
  | {
      kind: 'item'
      key: string
      icon: ReactNode
      label: string
      shortcut?: string
      danger?: boolean
      preventClose?: boolean
      onSelect: () => void
    }

export function ConfigRow({
  config,
  active,
  onSelect,
  onRename,
  onEditUrl,
  onDuplicate,
  onDelete,
}: Props) {
  const [editingName, setEditingName] = useState<string | null>(null)
  const [confirmingDelete, setConfirmingDelete] = useState(false)
  const renameInputRef = useRef<HTMLInputElement>(null)

  const isEditing = editingName != null
  useLayoutEffect(() => {
    if (isEditing) renameInputRef.current?.select()
  }, [isEditing])

  const items = useMemo<MenuItem[]>(
    () => [
      {
        kind: 'item',
        key: 'toggle',
        icon: <IconPower size={14} />,
        label: active ? 'Disconnect' : 'Connect',
        shortcut: '↵',
        onSelect: () => {
          onSelect(config.id)
        },
      },
      {
        kind: 'item',
        key: 'rename',
        icon: <IconEdit size={14} />,
        label: 'Rename',
        shortcut: 'R',
        preventClose: true,
        onSelect: () => {
          setEditingName(config.name)
        },
      },
      {
        kind: 'item',
        key: 'edit-url',
        icon: <IconLink size={14} />,
        label: 'Edit URL',
        onSelect: () => {
          onEditUrl(config.id)
        },
      },
      {
        kind: 'item',
        key: 'duplicate',
        icon: <IconCopy size={14} />,
        label: 'Duplicate',
        onSelect: () => {
          onDuplicate(config.id)
        },
      },
      { kind: 'divider', key: 'divider-1' },
      {
        kind: 'item',
        key: 'delete',
        icon: <IconTrash size={14} />,
        label: 'Delete',
        shortcut: '⌫',
        danger: true,
        preventClose: true,
        onSelect: () => {
          setConfirmingDelete(true)
        },
      },
    ],
    [active, config.id, config.name, onDuplicate, onEditUrl, onSelect],
  )

  const handleRowKey = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (editingName == null && (e.key === 'Enter' || e.key === ' ')) {
      e.preventDefault()
      onSelect(config.id)
    }
  }

  const finishRename = () => {
    if (editingName == null) return
    const trimmed = editingName.trim()
    if (trimmed && trimmed !== config.name) onRename(config.id, trimmed)
    setEditingName(null)
  }

  return (
    <ContextMenu.Root
      onOpenChange={(open) => {
        if (!open) setConfirmingDelete(false)
      }}
    >
      <ContextMenu.Trigger asChild>
        <div
          className="config-row"
          data-active={active}
          role="button"
          tabIndex={0}
          onClick={() => {
            if (editingName == null) onSelect(config.id)
          }}
          onKeyDown={handleRowKey}
        >
          <Flag code={config.country} />
          <div className="config-row__body">
            <div className="config-row__name">
              {editingName != null ? (
                <input
                  ref={renameInputRef}
                  className="config-row__rename-input"
                  value={editingName}
                  onBlur={finishRename}
                  onChange={(e) => {
                    setEditingName(e.target.value)
                  }}
                  onClick={(e) => {
                    e.stopPropagation()
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') e.currentTarget.blur()
                    else if (e.key === 'Escape') setEditingName(null)
                  }}
                />
              ) : (
                <span className="config-row__name-text">{config.name}</span>
              )}
              {active && editingName == null && <span className="active-chip mono">ACTIVE</span>}
            </div>
            <div className="config-row__sub mono">
              {config.variant} · {config.host}
            </div>
          </div>
          <div className="config-row__meta">
            <div className="config-row__ping mono num">
              {config.ping ?? '—'}
              <span className="config-row__ping-unit">ms</span>
            </div>
            <div className="config-row__age mono">{formatAge(config.addedAt)}</div>
          </div>

          <DropdownMenu.Root>
            <DropdownMenu.Trigger asChild>
              <button
                className="icon-btn icon-btn--more"
                title="More"
                type="button"
                onClick={(e) => {
                  e.stopPropagation()
                }}
              >
                <IconMore size={16} />
              </button>
            </DropdownMenu.Trigger>
            <DropdownMenu.Portal>
              <DropdownMenu.Content align="end" className="menu" side="top" sideOffset={4}>
                <MenuBody
                  Item={DropdownMenu.Item}
                  Separator={DropdownMenu.Separator}
                  confirmingDelete={confirmingDelete}
                  items={items}
                  name={config.name}
                  onCancelDelete={() => {
                    setConfirmingDelete(false)
                  }}
                  onConfirmDelete={() => {
                    setConfirmingDelete(false)
                    onDelete(config.id)
                  }}
                />
              </DropdownMenu.Content>
            </DropdownMenu.Portal>
          </DropdownMenu.Root>
        </div>
      </ContextMenu.Trigger>
      <ContextMenu.Portal>
        <ContextMenu.Content className="menu">
          <MenuBody
            Item={ContextMenu.Item}
            Separator={ContextMenu.Separator}
            confirmingDelete={confirmingDelete}
            items={items}
            name={config.name}
            onCancelDelete={() => {
              setConfirmingDelete(false)
            }}
            onConfirmDelete={() => {
              setConfirmingDelete(false)
              onDelete(config.id)
            }}
          />
        </ContextMenu.Content>
      </ContextMenu.Portal>
    </ContextMenu.Root>
  )
}

type MenuItemPrimitive = React.ComponentType<{
  key?: React.Key
  onSelect?: (event: Event) => void
  'data-danger'?: 'true' | undefined
  className?: string
  children?: ReactNode
}>

type SeparatorPrimitive = React.ComponentType<{ className?: string }>

interface MenuBodyProps {
  items: MenuItem[]
  confirmingDelete: boolean
  name: string
  Item: MenuItemPrimitive
  Separator: SeparatorPrimitive
  onCancelDelete: () => void
  onConfirmDelete: () => void
}

function MenuBody({
  items,
  confirmingDelete,
  name,
  Item,
  Separator,
  onCancelDelete,
  onConfirmDelete,
}: MenuBodyProps) {
  if (confirmingDelete) {
    return <DeleteConfirm name={name} onCancel={onCancelDelete} onConfirm={onConfirmDelete} />
  }
  return (
    <>
      {items.map((it) =>
        it.kind === 'divider' ? (
          <Separator key={it.key} className="menu__separator" />
        ) : (
          <Item
            key={it.key}
            className="menu__item"
            data-danger={it.danger ? 'true' : undefined}
            onSelect={(e) => {
              if (it.preventClose) e.preventDefault()
              it.onSelect()
            }}
          >
            <MenuItemBody item={it} />
          </Item>
        ),
      )}
    </>
  )
}

function MenuItemBody({ item }: { item: Extract<MenuItem, { kind: 'item' }> }) {
  return (
    <>
      <span className="menu__item-icon">{item.icon}</span>
      <span className="menu__item-label">{item.label}</span>
      {item.shortcut && <span className="menu__item-shortcut mono">{item.shortcut}</span>}
    </>
  )
}

function DeleteConfirm({
  name,
  onCancel,
  onConfirm,
}: {
  name: string
  onCancel: () => void
  onConfirm: () => void
}) {
  return (
    <div className="delete-confirm">
      <div className="delete-confirm__prompt">
        Delete <span className="delete-confirm__name">“{name}”</span>?
      </div>
      <div className="delete-confirm__actions">
        <button
          className="btn btn--xs-ghost"
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            onCancel()
          }}
        >
          Cancel
        </button>
        <button
          className="btn btn--xs-danger"
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            onConfirm()
          }}
        >
          <IconX size={11} stroke={2.4} /> Delete
        </button>
      </div>
    </div>
  )
}

function formatAge(ts: number): string {
  const ms = Date.now() - ts
  const day = 86_400_000
  const days = Math.floor(ms / day)
  if (days < 1) return 'today'
  if (days < 7) return `${String(days)}d ago`
  if (days < 30) return `${String(Math.floor(days / 7))}w ago`
  return `${String(Math.floor(days / 30))}mo ago`
}
