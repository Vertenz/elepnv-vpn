import type { Config } from '@shared/types'

import { Flag, IconChevron } from './icons'

interface Props {
  config: Config
  ping?: number | null
  onClick?: () => void
  disabled?: boolean
}

export function ActiveConfigCard({ config, ping, onClick, disabled }: Props) {
  return (
    <button className="active-card" disabled={disabled} type="button" onClick={onClick}>
      <Flag code={config.country} />
      <div className="active-card__body">
        <div className="active-card__name">{config.name}</div>
        <div className="active-card__sub mono">
          {config.variant} · {config.host}
        </div>
      </div>
      {ping != null && <div className="active-card__ping mono num">{ping}ms</div>}
      <span className="active-card__chevron">
        <IconChevron size={16} />
      </span>
    </button>
  )
}
