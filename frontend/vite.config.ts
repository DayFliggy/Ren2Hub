import { fileURLToPath, URL } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { appendFileSync } from 'node:fs'
import { defineConfig, loadEnv, type Plugin } from 'vite'

// Local ui-contracts runs record which /src modules each spec loads, so
// scripts/testing/affected.py can map source changes to browser specs.
function sourceRequestLog(path: string | undefined): Plugin[] {
  if (!path) return []
  return [
    {
      name: 'ren2hub-source-request-log',
      apply: 'serve',
      configureServer(server) {
        server.middlewares.use((request, _response, next) => {
          if (request.url?.startsWith('/src/'))
            appendFileSync(path, `${Date.now()} ${request.url}\n`)
          next()
        })
      },
    },
  ]
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const apiTarget = env.VITE_API_TARGET || 'http://localhost:3000'

  return {
    plugins: [
      vue(),
      tailwindcss(),
      ...sourceRequestLog(process.env.REN2HUB_VISUAL_REQUEST_LOG),
    ],
    base: '/',
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    server: {
      host: '0.0.0.0',
      port: Number(env.VITE_DEV_PORT || 5175),
      proxy: Object.fromEntries(
        ['/api', '/pg', '/v1', '/mj'].map((path) => [
          path,
          { target: apiTarget, changeOrigin: true },
        ])
      ),
    },
    build: {
      manifest: true,
      // A font is fetched only for text that needs it. Inlined into the
      // stylesheet, a small font file would be downloaded by every page.
      assetsInlineLimit: (file) =>
        file.endsWith('.woff2') ? false : undefined,
      rollupOptions: {
        output: {
          manualChunks(id) {
            // One lazy request for every stream's zh-CN server message file.
            if (id.includes('/src/i18n/locales/zh-CN/server-messages/'))
              return 'i18n-server-zh-CN'
            if (!id.includes('node_modules')) return undefined
            if (id.includes('echarts')) return 'vendor-chart'
            // Only the 2FA setup dialog needs it; keep it off the eager vendor chunk.
            if (id.includes('/qrcode.vue/')) return 'vendor-qrcode'
            // Only lazy pages parse plugin documents and Markdown; these stay
            // with the pages that import them.
            if (/\/node_modules\/(yaml|acorn|marked)\//.test(id))
              return undefined
            if (
              id.includes('/vue/') ||
              id.includes('/vue-router/') ||
              id.includes('/pinia/')
            )
              return 'vendor-vue'
            if (id.includes('/vue-i18n/') || id.includes('/@intlify/'))
              return 'vendor-i18n'
            return 'vendor-misc'
          },
        },
      },
    },
  }
})
