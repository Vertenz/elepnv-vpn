import { parseConfigUrl } from '@shared/parse-url'
import { type ParsedDetail } from '@shared/parse-url'
import { type Config } from '@shared/types'

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

export interface BuilderOptions {
  /** SOCKS5 loopback port; must match XRAYD_EXPECTED_SOCKS_ADDR (default 10808) */
  socksPort?: number
}

const DEFAULT_SOCKS_PORT = 10808

/**
 * Convert a UI `Config` into the minimal xray-core JSON that the daemon's
 * inboundsafe policy will accept:
 *   - exactly 1 inbound: socks/noauth on 127.0.0.1:<socksPort>
 *   - 1 outbound built from cfg.url
 *
 * Throws `Error` if the URL cannot be parsed or the protocol is unsupported.
 */
export function configToXrayJSON(cfg: Config, opts: BuilderOptions = {}): string {
  const socksPort = opts.socksPort ?? DEFAULT_SOCKS_PORT

  const parsed = parseConfigUrl(cfg.url)
  if (!parsed.ok) {
    throw new Error(`config-builder: ${parsed.reason}`)
  }

  const { detail } = parsed.preview
  const outbound = buildOutbound(cfg.proto, detail)

  const xrayCfg = {
    log: { loglevel: 'warning' },
    inbounds: [buildSocksInbound(socksPort)],
    outbounds: [outbound],
  }

  return JSON.stringify(xrayCfg)
}

// ---------------------------------------------------------------------------
// Inbound
// ---------------------------------------------------------------------------

function buildSocksInbound(port: number): unknown {
  return {
    tag: 'socks-in',
    listen: '127.0.0.1',
    port,
    protocol: 'socks',
    settings: { auth: 'noauth', udp: true },
  }
}

// ---------------------------------------------------------------------------
// Outbound dispatch
// ---------------------------------------------------------------------------

function buildOutbound(proto: Config['proto'], detail: ParsedDetail): unknown {
  switch (proto) {
    case 'vless':
      return buildVless(detail)
    case 'vmess':
      return buildVmess(detail)
    case 'ss':
      return buildShadowsocks(detail)
    case 'trojan':
      return buildTrojan(detail)
    default: {
      // Exhaustiveness guard — TypeScript would normally catch this, but the
      // value comes from user-supplied data so we guard at runtime too.
      const _exhaustive: never = proto
      throw new Error(`config-builder: unsupported proto: ${String(_exhaustive)}`)
    }
  }
}

// ---------------------------------------------------------------------------
// Shared stream settings helper
// ---------------------------------------------------------------------------

function buildStreamSettings(detail: ParsedDetail): Record<string, unknown> | undefined {
  const { security, sni, fp, type: network, path } = detail

  // Only emit streamSettings when there's something meaningful to say.
  if (!security && !network && !path && !sni && !fp) return undefined

  const ss: Record<string, unknown> = {}

  if (network) ss.network = network
  if (security === 'tls' || security === 'reality') {
    ss.security = security
    const tlsObj: Record<string, unknown> = {}
    if (sni) tlsObj.serverName = sni
    if (fp) tlsObj.fingerprint = fp
    if (security === 'tls') ss.tlsSettings = tlsObj
    else ss.realitySettings = tlsObj
  } else if (security && security !== 'none') {
    ss.security = security
  }

  if (network === 'ws' && path) {
    ss.wsSettings = { path }
  } else if (network === 'h2' && path) {
    ss.httpSettings = { path }
  } else if (network === 'grpc' && path) {
    ss.grpcSettings = { serviceName: path }
  }

  return ss
}

// ---------------------------------------------------------------------------
// Per-protocol outbound builders
// ---------------------------------------------------------------------------

function buildVless(detail: ParsedDetail): unknown {
  if (!detail.uuid) throw new Error('config-builder: vless URL missing UUID')
  if (!detail.address) throw new Error('config-builder: vless URL missing address')
  if (detail.port === undefined || !Number.isFinite(detail.port)) {
    throw new Error('config-builder: vless URL missing port')
  }

  const user: Record<string, unknown> = {
    id: detail.uuid,
    encryption: detail.encryption ?? 'none',
  }
  if (detail.flow) user.flow = detail.flow

  const outbound: Record<string, unknown> = {
    tag: 'proxy',
    protocol: 'vless',
    settings: {
      vnext: [
        {
          address: detail.address,
          port: detail.port,
          users: [user],
        },
      ],
    },
  }

  const ss = buildStreamSettings(detail)
  if (ss) outbound.streamSettings = ss

  return outbound
}

function buildVmess(detail: ParsedDetail): unknown {
  if (!detail.uuid) throw new Error('config-builder: vmess URL missing UUID')
  if (!detail.address) throw new Error('config-builder: vmess URL missing address')
  if (detail.port === undefined || !Number.isFinite(detail.port)) {
    throw new Error('config-builder: vmess URL missing port')
  }

  const outbound: Record<string, unknown> = {
    tag: 'proxy',
    protocol: 'vmess',
    settings: {
      vnext: [
        {
          address: detail.address,
          port: detail.port,
          users: [
            {
              id: detail.uuid,
              security: 'auto',
            },
          ],
        },
      ],
    },
  }

  const ss = buildStreamSettings(detail)
  if (ss) outbound.streamSettings = ss

  return outbound
}

function buildShadowsocks(detail: ParsedDetail): unknown {
  if (!detail.password) throw new Error('config-builder: ss URL missing password')
  if (!detail.method) throw new Error('config-builder: ss URL missing method')
  if (!detail.address) throw new Error('config-builder: ss URL missing address')
  if (detail.port === undefined || !Number.isFinite(detail.port)) {
    throw new Error('config-builder: ss URL missing port')
  }

  return {
    tag: 'proxy',
    protocol: 'shadowsocks',
    settings: {
      servers: [
        {
          address: detail.address,
          port: detail.port,
          method: detail.method,
          password: detail.password,
        },
      ],
    },
  }
}

function buildTrojan(detail: ParsedDetail): unknown {
  if (!detail.password) throw new Error('config-builder: trojan URL missing password')
  if (!detail.address) throw new Error('config-builder: trojan URL missing address')
  if (detail.port === undefined || !Number.isFinite(detail.port)) {
    throw new Error('config-builder: trojan URL missing port')
  }

  const outbound: Record<string, unknown> = {
    tag: 'proxy',
    protocol: 'trojan',
    settings: {
      servers: [
        {
          address: detail.address,
          port: detail.port,
          password: detail.password,
        },
      ],
    },
  }

  const ss = buildStreamSettings(detail)
  if (ss) outbound.streamSettings = ss

  return outbound
}
