import { fileURLToPath, URL } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig, loadEnv } from 'vite'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const apiTarget = env.VITE_API_TARGET || 'http://localhost:3000'

  return {
    plugins: [vue(), tailwindcss()],
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
