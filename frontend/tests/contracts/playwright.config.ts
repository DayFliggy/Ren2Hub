import { defineConfig } from '@playwright/test'

const output = process.env.REN2HUB_UI_OUTPUT
if (!output) throw new Error('Use scripts/testing/run.py ui-contracts')
const port = Number(process.env.REN2HUB_UI_PORT)
if (!Number.isInteger(port) || port < 1024 || port > 65535)
  throw new Error('Dedicated loopback port is required')
const baseURL = `http://127.0.0.1:${port}`

export default defineConfig({
  testDir: '.',
  testMatch: 'product-regressions.spec.ts',
  outputDir: `${output}/artifacts`,
  workers: 1,
  retries: 0,
  timeout: 45_000,
  expect: { timeout: 8_000 },
  reporter: [['list'], ['json', { outputFile: `${output}/playwright.json` }]],
  use: {
    baseURL,
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    contextOptions: { reducedMotion: 'reduce' },
    trace: 'retain-on-failure',
  },
  webServer: {
    command: `bun run dev -- --host 127.0.0.1 --port ${port} --strictPort --base /`,
    cwd: new URL('../..', import.meta.url).pathname,
    url: baseURL,
    reuseExistingServer: false,
    timeout: 60_000,
    env: { VITE_API_TARGET: 'http://127.0.0.1:1' },
  },
})
