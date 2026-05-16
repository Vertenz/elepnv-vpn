/**
 * Manual test runner for config-builder.ts.
 * Run with: node -r tsconfig-paths/register -r ts-node/register src/main/engine/config-builder.validate.ts
 * (or via the npm test:validate script)
 *
 * Exits 0 on success, 1 on failure.
 */
import { configToXrayJSON } from './config-builder'
import { type Config } from '@shared/types'

// ---------------------------------------------------------------------------

let passed = 0
let failed = 0

function test(name: string, fn: () => void): void {
  try {
    fn()
    console.log(`  ✓ ${name}`)
    passed++
  } catch (e) {
    console.error(`  ✗ ${name}`)
    console.error(`    ${(e as Error).message}`)
    failed++
  }
}

function expect(actual: unknown) {
  return {
    toBe(expected: unknown) {
      if (actual !== expected) {
        throw new Error(`Expected ${JSON.stringify(expected)} but got ${JSON.stringify(actual)}`)
      }
    },
    toHaveLength(n: number) {
      if (!Array.isArray(actual)) throw new Error(`Expected array but got ${typeof actual}`)
      if (actual.length !== n) throw new Error(`Expected length ${n} but got ${actual.length}`)
    },
    toThrow() {
      if (typeof actual !== 'function') throw new Error('Expected a function')
      try {
        ;(actual as () => void)()
        throw new Error('Expected function to throw but it did not')
      } catch {
        // good
      }
    },
  }
}

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
// Inbound tests
// ---------------------------------------------------------------------------

console.log('\nInbound invariants:')

test('SOCKS5 inbound on 127.0.0.1:10808', () => {
  const cfg = makeConfig({
    proto: 'vless',
    url: 'vless://550e8400-e29b-41d4-a716-446655440000@example.com:443?security=tls#Test',
  })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    inbounds: { listen: string; port: number; protocol: string; settings: { auth: string } }[]
  }
  expect(parsed.inbounds).toHaveLength(1)
  expect(parsed.inbounds[0]?.listen).toBe('127.0.0.1')
  expect(parsed.inbounds[0]?.port).toBe(10808)
  expect(parsed.inbounds[0]?.protocol).toBe('socks')
  expect(parsed.inbounds[0]?.settings.auth).toBe('noauth')
})

test('custom socksPort', () => {
  const cfg = makeConfig({
    proto: 'vless',
    url: 'vless://550e8400-e29b-41d4-a716-446655440000@example.com:443',
  })
  const parsed = JSON.parse(configToXrayJSON(cfg, { socksPort: 1080 })) as {
    inbounds: { port: number }[]
  }
  expect(parsed.inbounds[0]?.port).toBe(1080)
})

test('log.loglevel = warning', () => {
  const cfg = makeConfig({
    proto: 'vless',
    url: 'vless://550e8400-e29b-41d4-a716-446655440000@example.com:443',
  })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as { log: { loglevel: string } }
  expect(parsed.log.loglevel).toBe('warning')
})

// ---------------------------------------------------------------------------
// VLESS
// ---------------------------------------------------------------------------

console.log('\nVLESS:')

const VLESS_UUID = '550e8400-e29b-41d4-a716-446655440000'
const vlessUrl = `vless://${VLESS_UUID}@example.com:443?security=tls&sni=example.com&type=tcp#Test`

test('protocol=vless', () => {
  const cfg = makeConfig({ proto: 'vless', url: vlessUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { protocol: string }[]
  }
  expect(parsed.outbounds[0]?.protocol).toBe('vless')
})

test('settings.vnext address + port', () => {
  const cfg = makeConfig({ proto: 'vless', url: vlessUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { settings: { vnext: { address: string; port: number }[] } }[]
  }
  expect(parsed.outbounds[0]?.settings.vnext[0]?.address).toBe('example.com')
  expect(parsed.outbounds[0]?.settings.vnext[0]?.port).toBe(443)
})

test('UUID in user.id', () => {
  const cfg = makeConfig({ proto: 'vless', url: vlessUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { settings: { vnext: { users: { id: string }[] }[] } }[]
  }
  expect(parsed.outbounds[0]?.settings.vnext[0]?.users[0]?.id).toBe(VLESS_UUID)
})

test('encryption=none', () => {
  const cfg = makeConfig({ proto: 'vless', url: vlessUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { settings: { vnext: { users: { encryption: string }[] }[] } }[]
  }
  expect(parsed.outbounds[0]?.settings.vnext[0]?.users[0]?.encryption).toBe('none')
})

test('streamSettings.security=tls', () => {
  const cfg = makeConfig({ proto: 'vless', url: vlessUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { streamSettings: { security: string } }[]
  }
  expect(parsed.outbounds[0]?.streamSettings?.security).toBe('tls')
})

test('streamSettings.tlsSettings.serverName=example.com', () => {
  const cfg = makeConfig({ proto: 'vless', url: vlessUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { streamSettings: { tlsSettings: { serverName: string } } }[]
  }
  expect(parsed.outbounds[0]?.streamSettings?.tlsSettings?.serverName).toBe('example.com')
})

test('flow included when present', () => {
  const flowUrl = `vless://${VLESS_UUID}@example.com:443?security=reality&flow=xtls-rprx-vision`
  const cfg = makeConfig({ proto: 'vless', url: flowUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { settings: { vnext: { users: { flow?: string }[] }[] } }[]
  }
  expect(parsed.outbounds[0]?.settings.vnext[0]?.users[0]?.flow).toBe('xtls-rprx-vision')
})

test('throws on missing UUID', () => {
  const cfg = makeConfig({ proto: 'vless', url: 'vless://example.com:443' })
  expect(() => configToXrayJSON(cfg)).toThrow()
})

// ---------------------------------------------------------------------------
// VMESS
// ---------------------------------------------------------------------------

console.log('\nVMESS:')

const VMESS_UUID = '550e8400-e29b-41d4-a716-446655440000'
const vmessUrl = `vmess://${VMESS_UUID}@example.com:10086?security=tls&type=tcp`

test('protocol=vmess', () => {
  const cfg = makeConfig({ proto: 'vmess', url: vmessUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as { outbounds: { protocol: string }[] }
  expect(parsed.outbounds[0]?.protocol).toBe('vmess')
})

test('settings.vnext address + port', () => {
  const cfg = makeConfig({ proto: 'vmess', url: vmessUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { settings: { vnext: { address: string; port: number }[] } }[]
  }
  expect(parsed.outbounds[0]?.settings.vnext[0]?.address).toBe('example.com')
  expect(parsed.outbounds[0]?.settings.vnext[0]?.port).toBe(10086)
})

test('users[0].id = UUID', () => {
  const cfg = makeConfig({ proto: 'vmess', url: vmessUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { settings: { vnext: { users: { id: string }[] }[] } }[]
  }
  expect(parsed.outbounds[0]?.settings.vnext[0]?.users[0]?.id).toBe(VMESS_UUID)
})

test('users[0].security = auto', () => {
  const cfg = makeConfig({ proto: 'vmess', url: vmessUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { settings: { vnext: { users: { security: string }[] }[] } }[]
  }
  expect(parsed.outbounds[0]?.settings.vnext[0]?.users[0]?.security).toBe('auto')
})

test('throws on missing UUID', () => {
  const cfg = makeConfig({ proto: 'vmess', url: 'vmess://example.com:443' })
  expect(() => configToXrayJSON(cfg)).toThrow()
})

// ---------------------------------------------------------------------------
// Shadowsocks
// ---------------------------------------------------------------------------

console.log('\nShadowsocks:')

const ssUrl = 'ss://2022-blake3-aes-256-gcm:supersecret@ss.example.com:8388#Test'

test('protocol=shadowsocks', () => {
  const cfg = makeConfig({ proto: 'ss', url: ssUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as { outbounds: { protocol: string }[] }
  expect(parsed.outbounds[0]?.protocol).toBe('shadowsocks')
})

test('settings.servers address + port', () => {
  const cfg = makeConfig({ proto: 'ss', url: ssUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { settings: { servers: { address: string; port: number }[] } }[]
  }
  expect(parsed.outbounds[0]?.settings.servers[0]?.address).toBe('ss.example.com')
  expect(parsed.outbounds[0]?.settings.servers[0]?.port).toBe(8388)
})

test('settings.servers method', () => {
  const cfg = makeConfig({ proto: 'ss', url: ssUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { settings: { servers: { method: string }[] } }[]
  }
  expect(parsed.outbounds[0]?.settings.servers[0]?.method).toBe('2022-blake3-aes-256-gcm')
})

test('settings.servers password', () => {
  const cfg = makeConfig({ proto: 'ss', url: ssUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { settings: { servers: { password: string }[] } }[]
  }
  expect(parsed.outbounds[0]?.settings.servers[0]?.password).toBe('supersecret')
})

// ---------------------------------------------------------------------------
// Trojan
// ---------------------------------------------------------------------------

console.log('\nTrojan:')

const trojanUrl =
  'trojan://mypassword@trojan.example.com:443?security=tls&sni=trojan.example.com#Test'

test('protocol=trojan', () => {
  const cfg = makeConfig({ proto: 'trojan', url: trojanUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as { outbounds: { protocol: string }[] }
  expect(parsed.outbounds[0]?.protocol).toBe('trojan')
})

test('settings.servers address + port', () => {
  const cfg = makeConfig({ proto: 'trojan', url: trojanUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { settings: { servers: { address: string; port: number }[] } }[]
  }
  expect(parsed.outbounds[0]?.settings.servers[0]?.address).toBe('trojan.example.com')
  expect(parsed.outbounds[0]?.settings.servers[0]?.port).toBe(443)
})

test('settings.servers password', () => {
  const cfg = makeConfig({ proto: 'trojan', url: trojanUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { settings: { servers: { password: string }[] } }[]
  }
  expect(parsed.outbounds[0]?.settings.servers[0]?.password).toBe('mypassword')
})

test('streamSettings.security=tls', () => {
  const cfg = makeConfig({ proto: 'trojan', url: trojanUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { streamSettings: { security: string } }[]
  }
  expect(parsed.outbounds[0]?.streamSettings?.security).toBe('tls')
})

test('streamSettings.tlsSettings.serverName', () => {
  const cfg = makeConfig({ proto: 'trojan', url: trojanUrl })
  const parsed = JSON.parse(configToXrayJSON(cfg)) as {
    outbounds: { streamSettings: { tlsSettings: { serverName: string } } }[]
  }
  expect(parsed.outbounds[0]?.streamSettings?.tlsSettings?.serverName).toBe('trojan.example.com')
})

test('throws on missing password', () => {
  const cfg = makeConfig({ proto: 'trojan', url: 'trojan://trojan.example.com:443' })
  expect(() => configToXrayJSON(cfg)).toThrow()
})

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

console.log('\nError cases:')

test('throws on empty url', () => {
  const cfg = makeConfig({ proto: 'vless', url: '' })
  expect(() => configToXrayJSON(cfg)).toThrow()
})

test('throws on malformed url (no host:port)', () => {
  const cfg = makeConfig({ proto: 'vless', url: 'vless://550e8400-e29b-41d4-a716-446655440000@nodotport' })
  expect(() => configToXrayJSON(cfg)).toThrow()
})

test('throws on unknown protocol URL', () => {
  const cfg = makeConfig({ proto: 'vless', url: 'unknown://whatever@host:1234' })
  expect(() => configToXrayJSON(cfg)).toThrow()
})

// ---------------------------------------------------------------------------
// Summary
// ---------------------------------------------------------------------------

console.log(`\n${passed} passed, ${failed} failed`)
if (failed > 0) {
  process.exit(1)
}
