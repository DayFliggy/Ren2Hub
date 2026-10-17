import { defineConfig } from '@playwright/test'

const port = Number(process.env.PLAYWRIGHT_PORT || 4173)
const live = process.env.NATIVE_LIVE_TEST === '1'
const baseURL = live ? process.env.NATIVE_LIVE_URL : `http://127.0.0.1:${port}`
if (live && !baseURL)
  throw new Error('NATIVE_LIVE_URL is required for isolated native tests')
const captureScreenshots = process.env.PLAYWRIGHT_CAPTURE_SCREENSHOTS === 'true'

export default defineConfig({
  testDir: './tests/visual',
  outputDir: './output/playwright',
  preserveOutput: captureScreenshots ? 'always' : 'failures-only',
  fullyParallel: false,
  workers: 1,
  timeout: 45_000,
  expect: {
    timeout: 8_000,
  },
  reporter: [['list']],
  use: {
    baseURL,
    channel: process.env.PLAYWRIGHT_CHANNEL || undefined,
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    reducedMotion: 'reduce',
    viewport: { width: 1440, height: 900 },
    trace: 'retain-on-failure',
    screenshot: captureScreenshots ? 'on' : 'only-on-failure',
  },
  webServer: live
    ? undefined
    : {
        command: `bun run dev -- --host 127.0.0.1 --port ${port} --strictPort --base /`,
        url: baseURL,
        reuseExistingServer: true,
        timeout: 60_000,
      },
})
