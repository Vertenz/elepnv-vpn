import { defineConfig, type Plugin } from 'vite'

function removeDeprecatedInlineDynamicImports(): Plugin {
  return {
    name: 'remove-deprecated-inline-dynamic-imports',
    configResolved(config) {
      const output = config.build.rollupOptions.output

      if (output && !Array.isArray(output) && 'inlineDynamicImports' in output) {
        delete (output as { inlineDynamicImports?: boolean }).inlineDynamicImports
      }
    },
  }
}

// https://vitejs.dev/config
export default defineConfig({
  plugins: [removeDeprecatedInlineDynamicImports()],
  build: {
    rollupOptions: {
      output: {
        codeSplitting: false,
        entryFileNames: 'preload.js',
      },
    },
  },
})
