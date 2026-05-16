import { type Config, type ConfigProto } from './types'

export type ParsedConfig =
  | { ok: true; preview: ParsedPreview; build: () => Config }
  | { ok: false; reason: string }

/**
 * Protocol-specific fields extracted from the URL userinfo / query string.
 * All fields are optional — only those relevant to the given protocol are set.
 */
export interface ParsedDetail {
  /** UUID for vless / vmess */
  uuid?: string
  /** Password for trojan / shadowsocks */
  password?: string
  /** Shadowsocks cipher method (e.g. "2022-blake3-aes-256-gcm") */
  method?: string
  /** vless encryption param (always "none" per spec) */
  encryption?: string
  /** vless / xtls flow (e.g. "xtls-rprx-vision") */
  flow?: string
  /** TLS security type: "tls" | "reality" | "none" | "" */
  security?: string
  /** SNI override */
  sni?: string
  /** uTLS fingerprint */
  fp?: string
  /** Transport type: "tcp" | "ws" | "grpc" | "h2" | "quic" */
  type?: string
  /** WebSocket / HTTP2 path */
  path?: string
  /** Port parsed from host:port */
  port?: number
  /** Host without port */
  address?: string
}

export interface ParsedPreview {
  name: string
  proto: string
  host: string
  extras: { k: string; v: string }[]
  /** Protocol-specific parsed fields for config-builder use */
  detail: ParsedDetail
}

const PROTO_RE = /^(vless|vmess|ss|trojan):\/\/(.*)$/i
const COUNTRY_RE = /^([A-Z]{2})[-_ ]/

export function parseConfigUrl(raw: string): ParsedConfig {
  const trimmed = raw.trim()
  if (!trimmed) return { ok: false, reason: 'empty input' }

  const match = PROTO_RE.exec(trimmed)
  if (!match?.[1] || match[2] === undefined) {
    return {
      ok: false,
      reason: 'unsupported protocol — use vless / vmess / ss / trojan',
    }
  }

  const proto = match[1].toLowerCase() as ConfigProto
  const rest = match[2]

  const hashIdx = rest.indexOf('#')
  const fragment = hashIdx >= 0 ? safeDecode(rest.slice(hashIdx + 1)) : ''
  const beforeHash = hashIdx >= 0 ? rest.slice(0, hashIdx) : rest

  const queryIdx = beforeHash.indexOf('?')
  const beforeQuery = queryIdx >= 0 ? beforeHash.slice(0, queryIdx) : beforeHash
  const queryStr = queryIdx >= 0 ? beforeHash.slice(queryIdx + 1) : ''
  const atIdx = beforeQuery.lastIndexOf('@')
  const hostPart = atIdx >= 0 ? beforeQuery.slice(atIdx + 1) : beforeQuery

  if (!hostPart.includes(':')) {
    return { ok: false, reason: 'missing host:port' }
  }

  const params = new URLSearchParams(queryStr)
  const extras: { k: string; v: string }[] = []
  const pickKey = (k: string) => {
    const v = params.get(k)
    if (v != null) extras.push({ k, v })
  }
  pickKey('security')
  pickKey('sni')
  pickKey('fp')
  pickKey('type')
  pickKey('flow')
  pickKey('path')

  // Extract userinfo (uuid / password) from the part before '@'
  const userinfo = atIdx >= 0 ? safeDecode(beforeQuery.slice(0, atIdx)) : ''

  // Parse host:port
  const colonIdx = hostPart.lastIndexOf(':')
  const address = colonIdx >= 0 ? hostPart.slice(0, colonIdx) : hostPart
  const portNum = colonIdx >= 0 ? Number(hostPart.slice(colonIdx + 1)) : NaN

  // Build protocol-specific detail
  const detail: ParsedDetail = {
    address,
    ...(Number.isFinite(portNum) ? { port: portNum } : {}),
  }
  if (proto === 'vless') {
    // vless://uuid@host:port?encryption=none&flow=...
    if (userinfo) detail.uuid = userinfo
    detail.encryption = params.get('encryption') ?? 'none'
    const flowVal = params.get('flow')
    if (flowVal) detail.flow = flowVal
  } else if (proto === 'vmess') {
    // vmess URLs come in two flavours:
    //   1. vmess://base64(JSON)  — legacy V2RayN format
    //   2. vmess://uuid@host:port?security=...  — newer clash format
    // Detect by checking whether userinfo is a valid UUID shape.
    if (userinfo && UUID_RE.test(userinfo)) {
      detail.uuid = userinfo
    } else if (!userinfo && beforeQuery && !beforeQuery.includes('@')) {
      // Attempt base64-decode of the whole authority (legacy vmess:// format)
      const decoded = tryBase64Json(beforeQuery)
      if (decoded) {
        if (typeof decoded.id === 'string') detail.uuid = decoded.id
        if (typeof decoded.add === 'string') detail.address = decoded.add
        if (typeof decoded.port === 'number' || typeof decoded.port === 'string') {
          detail.port = Number(decoded.port)
        }
        if (typeof decoded.net === 'string') detail.type = decoded.net
        if (typeof decoded.sni === 'string') detail.sni = decoded.sni
        if (typeof decoded.tls === 'string' && decoded.tls) detail.security = decoded.tls
        if (typeof decoded.path === 'string') detail.path = decoded.path
      }
    }
  } else if (proto === 'trojan') {
    // trojan://password@host:port
    if (userinfo) detail.password = userinfo
  } else {
    // proto === 'ss'
    // Shadowsocks: ss://base64(method:password)@host:port or ss://method:password@host:port
    if (userinfo.includes(':')) {
      const colonAt = userinfo.indexOf(':')
      detail.method = userinfo.slice(0, colonAt)
      detail.password = userinfo.slice(colonAt + 1)
    } else if (userinfo) {
      // Try base64 decode (older SIP002 / SIP008 format)
      const decoded = tryBase64String(userinfo)
      if (decoded?.includes(':')) {
        const colonAt = decoded.indexOf(':')
        detail.method = decoded.slice(0, colonAt)
        detail.password = decoded.slice(colonAt + 1)
      }
    }
  }
  // Shared TLS params from query string (skip if vmess base64 already set them)
  if (!detail.security) {
    const secVal = params.get('security')
    if (secVal) detail.security = secVal
  }
  if (!detail.sni) {
    const sniVal = params.get('sni')
    if (sniVal) detail.sni = sniVal
  }
  if (!detail.fp) {
    const fpVal = params.get('fp')
    if (fpVal) detail.fp = fpVal
  }
  if (!detail.type) {
    const typeVal = params.get('type')
    if (typeVal) detail.type = typeVal
  }
  if (!detail.path) {
    const pathVal = params.get('path')
    if (pathVal) detail.path = pathVal
  }

  const variant = describeVariant(proto, params)
  const name = fragment || `${proto.toUpperCase()} server`
  const country = guessCountry(fragment) ?? '??'

  const preview: ParsedPreview = {
    name,
    proto: variant,
    host: hostPart,
    extras: [
      { k: 'name', v: name },
      { k: 'proto', v: variant },
      { k: 'host', v: hostPart },
      ...extras,
    ],
    detail,
  }

  return {
    ok: true,
    preview,
    build: () => ({
      // Renderer-side placeholder id. Main authoritatively reassigns
      // via crypto.randomUUID() in addConfig, so this value never
      // reaches storage. Web Crypto is available in Electron renderer.
      id: crypto.randomUUID(),
      name,
      country,
      proto,
      variant,
      host: hostPart,
      url: trimmed,
      addedAt: Date.now(),
    }),
  }
}

function describeVariant(proto: ConfigProto, params: URLSearchParams): string {
  const sec = params.get('security')
  const type = params.get('type')
  const tags: string[] = []
  if (proto === 'ss') return 'shadowsocks-2022'
  if (sec === 'reality') tags.push('reality')
  else if (sec === 'tls' && type) tags.push(`${type}+tls`)
  else if (type) tags.push(type)
  else if (sec) tags.push(sec)
  return tags.length ? `${proto} · ${tags.join(' ')}` : proto
}

function guessCountry(fragment: string): string | null {
  const m = COUNTRY_RE.exec(fragment)
  return m?.[1] ?? null
}

function safeDecode(s: string): string {
  try {
    return decodeURIComponent(s)
  } catch {
    return s
  }
}

const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

/**
 * Attempt to base64-decode a string and parse the result as JSON.
 * Returns null if decoding or parsing fails, or if the result is not a plain object.
 */
function tryBase64Json(s: string): Record<string, unknown> | null {
  try {
    const decoded = atob(s)
    const parsed: unknown = JSON.parse(decoded)
    if (parsed !== null && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>
    }
    return null
  } catch {
    return null
  }
}

/**
 * Attempt to base64-decode a string and return the plaintext.
 * Returns null if decoding fails.
 */
function tryBase64String(s: string): string | null {
  try {
    return atob(s)
  } catch {
    return null
  }
}

