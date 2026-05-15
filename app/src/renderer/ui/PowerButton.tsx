import { IconPower } from './icons'

type State = 'disconnected' | 'connecting' | 'connected' | 'error'

interface PowerButtonProps {
  state: State
  onClick?: () => void
}

const LABEL: Record<State, string> = {
  disconnected: 'OFF',
  connecting: 'WAIT',
  connected: 'ON',
  error: 'RETRY',
}

const ARIA_LABEL: Record<State, string> = {
  disconnected: 'Connect',
  connecting: 'Connecting, click to cancel',
  connected: 'Disconnect',
  error: 'Retry connection',
}

export function PowerButton({ state, onClick }: PowerButtonProps) {
  return (
    <button
      aria-label={ARIA_LABEL[state]}
      aria-pressed={state === 'connected'}
      className="power-btn"
      data-state={state}
      type="button"
      onClick={onClick}
    >
      {state === 'connecting' && <span aria-hidden className="power-btn__spinner" />}
      {state === 'connected' && <span aria-hidden className="power-btn__halo" />}
      <IconPower size={42} stroke={1.6} />
      <div className="power-btn__label mono">{LABEL[state]}</div>
    </button>
  )
}
