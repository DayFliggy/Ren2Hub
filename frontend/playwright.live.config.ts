import { defineConfig } from '@playwright/test'

const baseURL = process.env.NATIVE_LIVE_URL
const revision = process.env.NATIVE_LIVE_REVISION
if (
  baseURL !== 'http://127.0.0.1:3000' ||
  !revision ||
  !/^[a-f0-9]{40}$/.test(revision) ||
  !process.env.NATIVE_LIVE_TARGET ||
  !process.env.NATIVE_LIVE_CREDENTIALS
) {
  throw new Error(
    'Live tests require an isolated target, full revision, private credentials and http://127.0.0.1:3000 inside the candidate network namespace'
  )
}

export default defineConfig({
  testDir: './tests/live',
  globalSetup: './tests/live/preflight.ts',
  outputDir: process.env.PLAYWRIGHT_OUTPUT_DIR || './output/playwright-live',
  fullyParallel: false,
  workers: 1,
  timeout: 180_000,
  expect: { timeout: 8_000 },
  reporter: [['list']],
  use: {
    baseURL,
    channel: process.env.PLAYWRIGHT_CHANNEL || undefined,
    locale: 'en',
    timezoneId: 'Asia/Shanghai',
    reducedMotion: 'reduce',
    viewport: { width: 1440, height: 900 },
    trace: 'off',
    video: 'off',
    screenshot: 'off',
    serviceWorkers: 'block',
  },
})
