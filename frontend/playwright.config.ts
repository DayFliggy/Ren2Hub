import { defineConfig } from '@playwright/test'
import { localSession } from './tests/local/session'

const { output, session } = localSession()
const url = new URL(session.baseURL)
if (url.protocol !== 'http:' || url.hostname !== '127.0.0.1' || !url.port)
  throw new Error('Only an isolated loopback server is permitted')

export default defineConfig({
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
    contextOptions: { reducedMotion: 'reduce' },
    serviceWorkers: 'block',
    trace: 'off',
    screenshot: 'off',
    video: 'off',
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
})
