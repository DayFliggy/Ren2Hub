import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/__tests__/setup.ts'],
    include: ['src/**/*.{spec,test}.ts'],
    // Stylesheets import as empty text in tests, except those whose raw rules
    // a spec inspects directly.
    css: { include: /(?:home-font-subsets|tailwind)\.css/ },
    restoreMocks: true,
  },
})
