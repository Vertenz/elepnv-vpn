import type { Config, ConfigProto } from './types'

export type ParsedConfig =
  | { ok: true; preview: ParsedPreview; build: () => Config }
  | { ok: false; reason: string }

export interface ParsedPreview {
  name: string
  proto: string
  host: string
  extras: { k: string; v: string }[]
}

const PROTOS: ConfigProto[] = ['vless', 'vmess', 'ss', 'trojan']
const PROTO_RE = /^(vless|vmess|ss|trojan):\/\/(.*)$/i
const COUNTRY_RE = /^([A-Z]{2})[-_ ]/

export function parseConfigUrl(raw: string): ParsedConfig {
  const trimmed = raw.trim()
  if (!trimmed) return { ok: false, reason: 'empty input' }

  const match = trimmed.match(PROTO_RE)
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
  const m = fragment.match(COUNTRY_RE)
  return m?.[1] ?? null
}

function safeDecode(s: string): string {
  try {
    return decodeURIComponent(s)
  } catch {
    return s
  }
}

export function isSupportedProto(s: string): s is ConfigProto {
  return PROTOS.includes(s as ConfigProto)
}
