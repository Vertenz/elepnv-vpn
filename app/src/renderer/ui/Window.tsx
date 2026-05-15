import { type ReactNode } from 'react'

interface WindowProps {
  title?: string
  children: ReactNode
}

const TRAFFIC_LIGHTS = ['#e26c5a', '#dfa845', '#5ab35e'] as const

export function HiWindow({ title = 'elepn', children }: WindowProps) {
  return (
    <div className="window-shell">
      <div className="titlebar">
        <div className="titlebar__dots">
          {TRAFFIC_LIGHTS.map((c) => (
            <span key={c} className="titlebar__dot" style={{ background: c }} />
          ))}
        </div>
        <div className="titlebar__title mono">{title}</div>
        <div className="titlebar__spacer" />
      </div>
      <div className="window-body">{children}</div>
    </div>
  )
}
