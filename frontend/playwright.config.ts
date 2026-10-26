import { defineConfig } from '@playwright/test'
import { localSession } from './tests/local/session'

const visualPort = Number(process.env.PLAYWRIGHT_PORT || 4173)
const visualBaseURL = `http://127.0.0.1:${visualPort}`
const captureScreenshots = process.env.PLAYWRIGHT_CAPTURE_SCREENSHOTS === 'true'

function localConfig() {
  const { output, session } = localSession()
  const url = new URL(session.baseURL)
  if (url.protocol !== 'http:' || url.hostname !== '127.0.0.1' || !url.port)
    throw new Error('Only an isolated loopback server is permitted')

  return {
    testDir: './tests/local',
    outputDir: output + '/artifacts',
    fullyParallel: false,
    workers: 1,
    retries: 0,
    forbidOnly: true,
    timeout: 90_000,
    expect: { timeout: 10_000 },
    reporter: [['./tests/local/reporter.ts']],
    use: {
      baseURL: url.origin,
      locale: 'zh-CN',
      timezoneId: 'Asia/Shanghai',
      contextOptions: { reducedMotion: 'reduce' as const },
      serviceWorkers: 'block' as const,
      trace: 'off' as const,
      screenshot: 'off' as const,
      video: 'off' as const,
      launchOptions: {
        args: ['--no-proxy-server', '--disable-background-networking'],
      },
    },
    projects: ['desktop', 'mobile'].flatMap((screen) =>
      (['light', 'dark'] as const).map((colorScheme) => ({
        name: `${screen}-${colorScheme}`,
        use: {
          colorScheme,
          viewport:
            screen === 'desktop'
              ? { width: 1440, height: 900 }
              : { width: 390, height: 844 },
        },
      }))
    ),
  }
}

const visualConfig = {
  testDir: './tests/visual',
  outputDir: './output/playwright',
  preserveOutput: captureScreenshots
    ? ('always' as const)
    : ('failures-only' as const),
  fullyParallel: false,
  workers: 1,
  timeout: 45_000,
  expect: { timeout: 8_000 },
  reporter: [['list']],
  use: {
    baseURL: visualBaseURL,
    channel: process.env.PLAYWRIGHT_CHANNEL || undefined,
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    reducedMotion: 'reduce' as const,
    viewport: { width: 1440, height: 900 },
    trace: 'retain-on-failure' as const,
    screenshot: captureScreenshots
      ? ('on' as const)
      : ('only-on-failure' as const),
  },
  webServer: {
    command: `bun run dev -- --host 127.0.0.1 --port ${visualPort} --strictPort --base /`,
    url: visualBaseURL,
    reuseExistingServer: true,
    timeout: 60_000,
  },
}

export default defineConfig(
  process.env.REN2HUB_LOCAL_SESSION ? localConfig() : visualConfig
)
