import type { Config, ConnState } from '@shared/types'

interface Props {
  conn: ConnState
  config?: Config | null
}

export function StatusBlock({ conn, config }: Props) {
  const { title, sub } = describe(conn, config)

  return (
    <div className="status" data-kind={conn.kind}>
      <div className="status__title">
        <span className="status__dot" />
        {title}
      </div>
      <div className="status__sub mono num">{sub}</div>
    </div>
  )
}

function describe(conn: ConnState, config?: Config | null): { title: string; sub: string } {
  switch (conn.kind) {
    case 'connected':
      return {
        title: 'Connected',
        sub: `${String(conn.ping)} ms  ·  ${conn.egress}  ·  ${config?.country ?? '—'}`,
      }
    case 'connecting':
      return {
        title: 'Connecting…',
        sub: `Establishing tunnel to ${config?.host ?? '—'}`,
      }
    case 'disconnecting':
      return {
        title: 'Disconnecting…',
        sub: 'Tearing down tunnel',
      }
    case 'error':
      return {
        title: 'Connection failed',
        sub: `${conn.reason}  ·  retry in 3s`,
      }
    case 'disconnected':
      return {
        title: 'Not connected',
        sub: 'Tap the button to connect',
      }
    default: {
      const _exhaustive: never = conn
      void _exhaustive
      return { title: '', sub: '' }
    }
  }
}
