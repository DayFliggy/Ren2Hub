import { expect, test } from '@playwright/test'

import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage,
  freezeAndInspectHomeCanvas,
  isExpectedGuestRefreshConsoleMessage,
  waitForStablePage,
} from './fixtures'
import { VISUAL_ROUTES } from './routes'

const viewports = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'tablet', width: 1024, height: 820 },
  { name: 'mobile', width: 390, height: 844 },
] as const

const ROUTE_SMOKE_TIMEOUT = 300_000

for (const viewport of [viewports[0], viewports[2]]) {
  test(`retired next prefix opens only the error page at ${viewport.name}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, { theme: 'light', authenticated: true })
    for (const path of ['/next', '/next/', '/next/keys']) {
      await page.goto(path, { waitUntil: 'domcontentloaded' })
      await expect(page).toHaveURL(/\/404$/)
      await expect(page.getByText('页面不存在', { exact: true })).toBeVisible()
      await expect(page.locator('[data-key-page]')).toHaveCount(0)
    }
    await assertNoHorizontalOverflow(page)
    await page.screenshot({
      path: testInfo.outputPath('retired-next.png'),
      fullPage: true,
    })
    await page.goto('/keys', { waitUntil: 'domcontentloaded' })
    await expect(page.locator('[data-key-page]')).toBeVisible()
  })
}

for (const viewport of [viewports[0], viewports[2]]) {
  test(`initial navigation has no loading screen at ${viewport.name}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, {
      theme: process.env.PLAYWRIGHT_THEME === 'dark' ? 'dark' : 'light',
      authenticated: false,
    })
    let release!: () => void
    let started!: () => void
    const setupStarted = new Promise<void>((resolve) => {
      started = resolve
    })
    const pending = new Promise<void>((resolve) => {
      release = resolve
    })
    await page.route('**/api/setup?*', async (route) => {
      started()
      await pending
      await route.fulfill({
        json: {
          success: true,
          data: { status: true, root_init: true, database_type: 'sqlite' },
        },
      })
    })
    await page.goto('/', { waitUntil: 'domcontentloaded' })
    try {
      await setupStarted
      await expect(page.locator('#app')).toBeAttached()
      await expect(page.locator('#app > main[role="status"]')).toHaveCount(0)
      await expect(page.locator('.app-navbar')).toHaveCount(0)
    } finally {
      release()
    }
    await expect(page.locator('.app-navbar')).toBeVisible()
    await waitForStablePage(page)
    await freezeAndInspectHomeCanvas(page)
    await assertNoHorizontalOverflow(page)
    await page.screenshot({
      path: testInfo.outputPath(`home-${viewport.name}.png`),
      fullPage: true,
    })
  })
}

test('failed lazy navigation displays an accessible retry state', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'dark', authenticated: false })
  await page.goto('/', { waitUntil: 'networkidle' })
  await page.route(
    /(?:assets\/SignInView-[^/]+\.js|src\/views\/auth\/SignInView\.vue)/,
    (route) => route.abort()
  )
  await page.evaluate(() => sessionStorage.setItem('ren2hub_chunk_reload', '1'))
  await page.locator('a[href="/sign-in"]').first().click()
  await expect(page.getByRole('alert')).toContainText('此页面暂时无法显示')
  const retry = page.getByRole('button', { name: '重试', exact: true })
  await retry.focus()
  await expect(retry).toBeFocused()
})

for (const viewport of viewports) {
  test(`all routes smoke at ${viewport.name}`, async ({ page }) => {
    test.setTimeout(ROUTE_SMOKE_TIMEOUT)
    await page.setViewportSize(viewport)
    await configureStablePage(page, {
      theme: process.env.PLAYWRIGHT_THEME === 'light' ? 'light' : 'dark',
      routeAwareAuth: true,
    })

    const runtimeErrors: string[] = []
    const routeFailures: string[] = []
    page.on('pageerror', (error) => runtimeErrors.push(error.message))
    page.on('console', (message) => {
      if (isExpectedGuestRefreshConsoleMessage(page, message)) return
      // Public documents intentionally reject Playwright's injected frame script.
      if (
        message.text().startsWith("Blocked script execution in 'about:srcdoc'")
      )
        return
      if (message.type() === 'warning' || message.type() === 'error') {
        runtimeErrors.push(`${message.type()}: ${message.text()}`)
      }
    })
    page.on('requestfailed', (request) => {
      const failure = request.failure()?.errorText || 'request failed'
      if (
        request.url().startsWith('http://127.0.0.1:') &&
        !failure.includes('ERR_ABORTED')
      ) {
        runtimeErrors.push(`${request.method()} ${request.url()}: ${failure}`)
      }
    })

    for (const route of VISUAL_ROUTES) {
      runtimeErrors.length = 0
      await test.step(`${route.name} ${route.path}`, async () => {
        await page.goto(route.path, { waitUntil: 'domcontentloaded' })
        await waitForStablePage(page)
        if (route.path === '/') await freezeAndInspectHomeCanvas(page)
        if (route.path.includes('manage=')) {
          await expect(
            page.locator('[role="dialog"][aria-modal="true"]')
          ).toBeVisible()
          await page.getByRole('button', { name: '关闭', exact: true }).click()
          await expect(
            page.locator('[role="dialog"][aria-modal="true"]')
          ).toHaveCount(0)
        }
        if (runtimeErrors.length > 0) {
          routeFailures.push(`${route.path}: ${runtimeErrors.join(' | ')}`)
          return
        }
        await assertNoHorizontalOverflow(page)
        await assertInteractiveCentersVisible(page)
      })
    }
    expect(routeFailures).toEqual([])
  })
}

test('deferred module entry points are disabled and routes fail closed', async ({
  page,
}) => {
  test.setTimeout(120_000)
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'dark', authenticated: true })

  await page.goto('/activity', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
  await expect(page.getByRole('button', { name: /RT农家乐/ })).toBeDisabled()
  await expect(page.getByRole('button', { name: /无趣大游戏/ })).toBeDisabled()
  await expect(
    page.getByRole('button', { name: '炼金室', exact: true })
  ).toBeDisabled()

  await page.goto('/profile', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
  await expect(
    page.locator('.profile-identity').getByRole('button', {
      name: '即将上线',
      exact: true,
    })
  ).toBeDisabled()

  for (const path of [
    '/market',
    '/subscription',
    '/plan-management',
    '/invoice',
    '/farm',
    '/bigame',
    '/lab/chat',
  ]) {
    await page.goto(path, { waitUntil: 'domcontentloaded' })
    await expect(page).toHaveURL(/\/dashboard$/)
  }
})
