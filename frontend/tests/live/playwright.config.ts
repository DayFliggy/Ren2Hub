import { defineConfig } from '@playwright/test'

const port = Number(process.env.PLAYWRIGHT_LIVE_PORT || 4187)
const apiTarget = process.env.PLAYWRIGHT_LIVE_API_TARGET
if (!apiTarget)
  throw new Error('An explicit isolated candidate API target is required')
const target = new URL(apiTarget)
const octets = target.hostname.split('.').map(Number)
const privateIPv4 =
  octets.length === 4 &&
  octets.every((part) => Number.isInteger(part) && part >= 0 && part <= 255) &&
  (octets[0] === 127 ||
    octets[0] === 10 ||
    (octets[0] === 172 && octets[1]! >= 16 && octets[1]! <= 31) ||
    (octets[0] === 192 && octets[1] === 168))
if (
  target.protocol !== 'http:' ||
  target.username ||
  target.password ||
  target.pathname !== '/' ||
  target.search ||
  target.hash ||
  !(
    privateIPv4 ||
    target.hostname === 'localhost' ||
    target.hostname === '[::1]'
  )
)
  throw new Error(
    'Candidate API must be an HTTP loopback or RFC1918 origin without credentials or a path'
  )
const embedded = process.env.PLAYWRIGHT_LIVE_EMBEDDED === 'true'
const baseURL = embedded ? target.origin : `http://127.0.0.1:${port}`

export default defineConfig({
  testDir: '.',
  testMatch: '*.spec.ts',
  outputDir: process.env.PLAYWRIGHT_LIVE_OUTPUT || '../../output/live',
  fullyParallel: false,
  workers: 1,
  timeout: 240_000,
  expect: { timeout: 12_000 },
  reporter: [['list']],
  use: {
    baseURL,
    actionTimeout: 15_000,
    navigationTimeout: 30_000,
    channel: process.env.PLAYWRIGHT_CHANNEL || 'chromium',
    locale: 'zh-CN',
    timezoneId: 'UTC',
    reducedMotion: 'reduce',
    trace: 'off',
    video: 'off',
    screenshot: 'off',
  },
  webServer: embedded
    ? undefined
    : {
        command: `bun run dev -- --host 127.0.0.1 --port ${port} --strictPort --base /`,
        cwd: new URL('../..', import.meta.url).pathname,
        env: { VITE_API_TARGET: apiTarget },
        url: baseURL,
        reuseExistingServer: false,
        timeout: 60_000,
      },
})
