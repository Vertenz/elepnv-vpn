import { randomUUID } from 'node:crypto'

import type { Config } from '@shared/types'

const DAY = 86_400_000

/**
 * Five sample configs used to populate prefs.json on first launch in
 * non-packaged builds. The bootstrap in main/index.ts only calls this
 * when both `!app.isPackaged` and `process.env.ELEPN_SEED !== '0'`.
 */
export function buildSampleConfigs(now: number = Date.now()): Config[] {
  return [
    {
      id: randomUUID(),
      name: 'Frankfurt',
      country: 'DE',
      proto: 'vless',
      variant: 'vless · reality',
      host: '185.220.101.42:443',
      url: 'vless://9f7b2a1c-3d4f-4a8e-9b22-118a774f55a1@185.220.101.42:443?security=reality&sni=cloudflare.com&fp=chrome&pbk=ZK5xKQzM&type=tcp#DE-Frankfurt',
      addedAt: now - 3 * DAY,
      ping: 24,
    },
    {
      id: randomUUID(),
      name: 'Amsterdam',
      country: 'NL',
      proto: 'vmess',
      variant: 'vmess · ws+tls',
      host: 'nl.elepn.io:443',
      url: 'vmess://example',
      addedAt: now - 7 * DAY,
      ping: 41,
    },
    {
      id: randomUUID(),
      name: 'Helsinki',
      country: 'FI',
      proto: 'ss',
      variant: 'shadowsocks-2022',
      host: 'fi.elepn.io:8388',
      url: 'ss://example',
      addedAt: now - 14 * DAY,
      ping: 58,
    },
    {
      id: randomUUID(),
      name: 'Singapore',
      country: 'SG',
      proto: 'trojan',
      variant: 'trojan · grpc',
      host: 'sg.elepn.io:443',
      url: 'trojan://example',
      addedAt: now - 30 * DAY,
      ping: 142,
    },
    {
      id: randomUUID(),
      name: 'Berlin (alt)',
      country: 'DE',
      proto: 'vless',
      variant: 'vless · ws+tls',
      host: 'de2.elepn.io:443',
      url: 'vless://example2',
      addedAt: now - 30 * DAY,
      ping: 31,
    },
  ]
}
