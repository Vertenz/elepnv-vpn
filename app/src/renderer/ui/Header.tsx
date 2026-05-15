import  { type ThemePreference } from '@shared/types'

import { IconDisplay, IconMoon, IconSun } from './icons'

interface HeaderProps {
  themePreference: ThemePreference
  onToggleTheme: () => void
  version?: string
}

const NEXT_TITLE: Record<ThemePreference, string> = {
  light: 'Switch to dark theme',
  dark: 'Follow system theme',
  system: 'Switch to light theme',
}

function ThemeIcon({ pref }: { pref: ThemePreference }) {
  if (pref === 'light') return <IconMoon size={16} />
  if (pref === 'dark') return <IconDisplay size={16} />
  return <IconSun size={16} />
}

export function HiHeader({
  themePreference,
  onToggleTheme,
  version = 'xray-core 1.8.4',
}: HeaderProps) {
  const label = NEXT_TITLE[themePreference]
  return (
    <div className="hi-header">
      <div className="hi-header__brand">
        <div className="hi-header__title">elepn</div>
        <div className="hi-header__version mono">{version}</div>
      </div>
      <div className="hi-header__actions">
        <button
          aria-label={label}
          className="icon-btn"
          title={label}
          type="button"
          onClick={onToggleTheme}
        >
          <ThemeIcon pref={themePreference} />
        </button>
      </div>
    </div>
  )
}
