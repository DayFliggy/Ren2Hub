import { readFileSync, writeFileSync } from 'node:fs'
import { expect, test } from '@playwright/test'

test.describe('native isolated backend', () => {
  test.skip(
    process.env.NATIVE_LIVE_TEST !== '1',
    'requires an isolated native candidate and private credentials'
  )
  const paths = [
    '/dashboard',
    '/models',
    '/keys',
    '/wallet',
    '/activity',
    '/invite',
    '/usage-logs',
    '/users',
    '/redemption-codes',
    '/orders',
    '/usage-logs/audit',
    '/ops',
    '/profile',
    '/security',
    '/system-settings/security',
    '/system-settings/billing/gmpay',
    '/models/pricing',
    '/ticket-management',
    '/task-plugins',
  ]
  for (const [theme, width] of [
    ['light', 1440],
    ['dark', 390],
  ] as const) {
    test(`${theme} ${width} real session and retained console routes`, async ({
      page,
      baseURL,
    }, testInfo) => {
      test.setTimeout(180_000)
      if (
        !baseURL ||
        !['127.0.0.1', 'localhost'].includes(new URL(baseURL).hostname)
      )
        throw new Error('Native live tests require a loopback candidate URL')
      const credentialsPath = process.env.NATIVE_LIVE_CREDENTIALS
      if (!credentialsPath)
        throw new Error(
          'NATIVE_LIVE_CREDENTIALS must name the private candidate account file'
        )
      const value = JSON.parse(readFileSync(credentialsPath, 'utf8')) as {
        username?: string
        password?: string
        root?: { username?: string; password?: string }
      }
      const credentials = value.root ?? value
      if (
        typeof credentials.username !== 'string' ||
        typeof credentials.password !== 'string'
      )
        throw new Error('Invalid private candidate account file')
      await page.setViewportSize({ width, height: 900 })
      await page.addInitScript((theme) => {
        localStorage.setItem('ren2hub_locale', 'en')
        localStorage.setItem('ren2hub_theme_mode', theme)
      }, theme)
      const errors: string[] = [],
        failures: string[] = [],
        visited: Array<{ path: string; actual_path: string }> = []
      page.on('pageerror', (error) => errors.push(error.message))
      await page.goto('/sign-in')
      await page
        .locator('input[autocomplete="username"]')
        .fill(credentials.username)
      await page
        .locator('input[autocomplete="current-password"]')
        .fill(credentials.password)
      await page.locator('button[type="submit"]').click()
      await expect(page).not.toHaveURL(/\/sign-in/)
      page.on('response', (response) => {
        const url = new URL(response.url())
        if (url.pathname.startsWith('/api/') && response.status() >= 400)
          failures.push(
            `${response.request().method()} ${url.pathname}: ${response.status()}`
          )
      })
      for (const path of paths) {
        await page.goto(path, { waitUntil: 'networkidle' })
        const expectedPath =
          path === '/system-settings/security'
            ? '/system-settings/security/rate-limit'
            : path
        await expect(page).toHaveURL(
          new RegExp(`${expectedPath.replaceAll('/', '\\/')}(?:[?#].*)?$`)
        )
        await expect(page.locator('main')).toBeVisible()
        await expect(page.locator('[role=alert]')).toHaveCount(0)
        expect(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth + 1
          ),
          path
        ).toBe(true)
        expect(errors, path).toEqual([])
        expect(failures, path).toEqual([])
        visited.push({ path, actual_path: new URL(page.url()).pathname })
      }
      await page.goto('/wallet', { waitUntil: 'networkidle' })
      await page.screenshot({
        path: testInfo.outputPath(`native-live-wallet-${theme}-${width}.png`),
      })
      await page.goto('/pricing', { waitUntil: 'networkidle' })
      const attribution = page.getByRole('link', {
        name: 'Frontend design and development by New API contributors.',
        exact: true,
      })
      await attribution.scrollIntoViewIfNeeded()
      await expect(attribution).toBeVisible()
      expect(errors).toEqual([])
      expect(failures).toEqual([])
      const routesPath = testInfo.outputPath('verified-routes.json')
      writeFileSync(
        routesPath,
        JSON.stringify({ theme, width, routes: visited })
      )
      await testInfo.attach('verified-routes.json', {
        path: routesPath,
        contentType: 'application/json',
      })
    })
  }
})
