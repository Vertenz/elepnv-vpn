import path from 'node:path'

import { defineConfig } from 'vitest/config'

export default defineConfig({
  css: {
    // Disable CSS transforms to avoid loading native lightningcss binaries
    // which are incompatible with this build environment (arm64 Linux).
    transformer: 'postcss',
  },
  resolve: {
    alias: { '@shared': path.resolve(__dirname, 'src/shared') },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
})
