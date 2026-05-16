import { describe, expect, it } from 'vitest'

import { type Config } from '@shared/types'

import { configToXrayJSON } from './config-builder'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeConfig(overrides: Partial<Config> & { url: string }): Config {
  return {
    id: 'test-id',
    name: 'Test',
    country: 'US',
    proto: 'vless',
    variant: 'vless',
    host: 'example.com:443',
    addedAt: 0,
    ...overrides,
  }
}

// ---------------------------------------------------------------------------
// Shared inbound invariants
// ---------------------------------------------------------------------------

describe('configToXrayJSON — inbound', () => {
  it('produces exactly 1 SOCKS5 inbound on 127.0.0.1:10808 for a vless config', () => {
    const cfg = makeConfig({
      proto: 'vless',
      url: 'vless://550e8400-e29b-41d4-a716-446655440000@example.com:443?security=tls&sni=example.com#Test',
    })
    const parsed: unknown = JSON.parse(configToXrayJSON(cfg))
    const result = parsed as {
      inbounds: { listen: string; port: number; protocol: string; settings: { auth: string } }[]
    }
    expect(result.inbounds).toHaveLength(1)
    expect(result.inbounds[0]?.listen).toBe('127.0.0.1')
    expect(result.inbounds[0]?.port).toBe(10808)
    expect(result.inbounds[0]?.protocol).toBe('socks')
    expect(result.inbounds[0]?.settings.auth).toBe('noauth')
  })

  it('respects custom socksPort option', () => {
    const cfg = makeConfig({
      proto: 'vless',
      url: 'vless://550e8400-e29b-41d4-a716-446655440000@example.com:443?security=tls',
    })
    const parsed = JSON.parse(configToXrayJSON(cfg, { socksPort: 1080 })) as {
      inbounds: { port: number }[]
    }
    expect(parsed.inbounds[0]?.port).toBe(1080)
  })

  it('always emits log loglevel: warning', () => {
    const cfg = makeConfig({
      proto: 'vless',
      url: 'vless://550e8400-e29b-41d4-a716-446655440000@example.com:443',
    })
    const parsed = JSON.parse(configToXrayJSON(cfg)) as { log: { loglevel: string } }
    expect(parsed.log.loglevel).toBe('warning')
  })
})

// ---------------------------------------------------------------------------
// VLESS
// ---------------------------------------------------------------------------

describe('configToXrayJSON — vless', () => {
  const UUID = '550e8400-e29b-41d4-a716-446655440000'
  const url = `vless://${UUID}@example.com:443?security=tls&sni=example.com&type=tcp#US-Test`

  it('outbound has protocol=vless, settings.vnext, and correct user', () => {
    const cfg = makeConfig({ proto: 'vless', host: 'example.com:443', url })
    const parsed = JSON.parse(configToXrayJSON(cfg)) as {
      outbounds: {
        protocol: string
        settings: {
          vnext: { address: string; port: number; users: { id: string; encryption: string }[] }[]
        }
      }[]
    }
    const ob = parsed.outbounds[0]
    expect(ob?.protocol).toBe('vless')
    expect(ob?.settings.vnext).toHaveLength(1)
    expect(ob?.settings.vnext[0]?.address).toBe('example.com')
    expect(ob?.settings.vnext[0]?.port).toBe(443)
    expect(ob?.settings.vnext[0]?.users[0]?.id).toBe(UUID)
    expect(ob?.settings.vnext[0]?.users[0]?.encryption).toBe('none')
  })

  it('includes streamSettings with tls + sni', () => {
    const cfg = makeConfig({ proto: 'vless', url })
    const parsed = JSON.parse(configToXrayJSON(cfg)) as {
      outbounds: { streamSettings?: { security: string; tlsSettings?: { serverName: string } } }[]
    }
    const ss = parsed.outbounds[0]?.streamSettings
    expect(ss?.security).toBe('tls')
    expect(ss?.tlsSettings?.serverName).toBe('example.com')
  })

  it('includes flow when present in URL', () => {
    const flowUrl = `vless://${UUID}@example.com:443?security=reality&flow=xtls-rprx-vision`
    const cfg = makeConfig({ proto: 'vless', url: flowUrl })
    const parsed = JSON.parse(configToXrayJSON(cfg)) as {
      outbounds: { settings: { vnext: { users: { flow: string }[] }[] } }[]
    }
    expect(parsed.outbounds[0]?.settings.vnext[0]?.users[0]?.flow).toBe('xtls-rprx-vision')
  })

  it('throws when UUID is missing', () => {
    // URL with no userinfo
    const cfg = makeConfig({ proto: 'vless', url: 'vless://example.com:443' })
    expect(() => configToXrayJSON(cfg)).toThrow(/missing UUID/)
  })
})

// ---------------------------------------------------------------------------
// VMESS
// ---------------------------------------------------------------------------

describe('configToXrayJSON — vmess', () => {
  const UUID = '550e8400-e29b-41d4-a716-446655440000'
  const url = `vmess://${UUID}@example.com:10086?security=tls&type=tcp#US-vmess`

  it('outbound has protocol=vmess, settings.vnext, and security=auto', () => {
    const cfg = makeConfig({ proto: 'vmess', host: 'example.com:10086', url })
    const parsed = JSON.parse(configToXrayJSON(cfg)) as {
      outbounds: {
        protocol: string
        settings: {
          vnext: { address: string; port: number; users: { id: string; security: string }[] }[]
        }
      }[]
    }
    const ob = parsed.outbounds[0]
    expect(ob?.protocol).toBe('vmess')
    expect(ob?.settings.vnext[0]?.address).toBe('example.com')
    expect(ob?.settings.vnext[0]?.port).toBe(10086)
    expect(ob?.settings.vnext[0]?.users[0]?.id).toBe(UUID)
    expect(ob?.settings.vnext[0]?.users[0]?.security).toBe('auto')
  })

  it('throws when UUID is missing', () => {
    const cfg = makeConfig({ proto: 'vmess', url: 'vmess://example.com:443' })
    expect(() => configToXrayJSON(cfg)).toThrow(/missing UUID/)
  })
})

// ---------------------------------------------------------------------------
// Shadowsocks
// ---------------------------------------------------------------------------

describe('configToXrayJSON — shadowsocks', () => {
  // SIP002 format: ss://method:password@host:port
  const url = 'ss://2022-blake3-aes-256-gcm:supersecret@ss.example.com:8388#US-ss'

  it('outbound has protocol=shadowsocks, settings.servers with method + password', () => {
    const cfg = makeConfig({ proto: 'ss', host: 'ss.example.com:8388', url })
    const parsed = JSON.parse(configToXrayJSON(cfg)) as {
      outbounds: {
        protocol: string
        settings: {
          servers: { address: string; port: number; method: string; password: string }[]
        }
      }[]
    }
    const ob = parsed.outbounds[0]
    expect(ob?.protocol).toBe('shadowsocks')
    expect(ob?.settings.servers[0]?.address).toBe('ss.example.com')
    expect(ob?.settings.servers[0]?.port).toBe(8388)
    expect(ob?.settings.servers[0]?.method).toBe('2022-blake3-aes-256-gcm')
    expect(ob?.settings.servers[0]?.password).toBe('supersecret')
  })

  it('throws when password is missing', () => {
    const cfg = makeConfig({ proto: 'ss', url: 'ss://ss.example.com:8388' })
    expect(() => configToXrayJSON(cfg)).toThrow(/missing password|missing method/)
  })
})

// ---------------------------------------------------------------------------
// Trojan
// ---------------------------------------------------------------------------

describe('configToXrayJSON — trojan', () => {
  const url = 'trojan://mypassword@trojan.example.com:443?security=tls&sni=trojan.example.com#US-trojan'

  it('outbound has protocol=trojan, settings.servers with password', () => {
    const cfg = makeConfig({ proto: 'trojan', host: 'trojan.example.com:443', url })
    const parsed = JSON.parse(configToXrayJSON(cfg)) as {
      outbounds: {
        protocol: string
        settings: {
          servers: { address: string; port: number; password: string }[]
        }
      }[]
    }
    const ob = parsed.outbounds[0]
    expect(ob?.protocol).toBe('trojan')
    expect(ob?.settings.servers[0]?.address).toBe('trojan.example.com')
    expect(ob?.settings.servers[0]?.port).toBe(443)
    expect(ob?.settings.servers[0]?.password).toBe('mypassword')
  })

  it('includes streamSettings for tls with sni', () => {
    const cfg = makeConfig({ proto: 'trojan', url })
    const parsed = JSON.parse(configToXrayJSON(cfg)) as {
      outbounds: { streamSettings?: { security: string; tlsSettings?: { serverName: string } } }[]
    }
    const ss = parsed.outbounds[0]?.streamSettings
    expect(ss?.security).toBe('tls')
    expect(ss?.tlsSettings?.serverName).toBe('trojan.example.com')
  })

  it('throws when password is missing', () => {
    const cfg = makeConfig({ proto: 'trojan', url: 'trojan://trojan.example.com:443' })
    expect(() => configToXrayJSON(cfg)).toThrow(/missing password/)
  })
})

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

describe('configToXrayJSON — error cases', () => {
  it('throws on unsupported proto', () => {
    const cfg = makeConfig({
      proto: 'vless', // TS type says vless but we override at runtime
      url: 'unknown://whatever@host:1234',
    })
    // The URL parse will fail on unknown:// scheme
    expect(() => configToXrayJSON(cfg)).toThrow()
  })

  it('throws on empty url', () => {
    const cfg = makeConfig({ proto: 'vless', url: '' })
    expect(() => configToXrayJSON(cfg)).toThrow()
  })

  it('throws on malformed url (no host:port)', () => {
    const cfg = makeConfig({ proto: 'vless', url: 'vless://550e8400-e29b-41d4-a716-446655440000@nodotport' })
    expect(() => configToXrayJSON(cfg)).toThrow()
  })
})
