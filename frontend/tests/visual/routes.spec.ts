import { expect, test, type Page } from '@playwright/test'

import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage,
  freezeAndInspectHomeCanvas,
  isExpectedGuestRefreshConsoleMessage,
  waitForStablePage,
  captureEvidence,
} from './fixtures'
import { VISUAL_ROUTES } from './routes'

const viewports = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'tablet', width: 1024, height: 820 },
  { name: 'mobile', width: 390, height: 844 },
] as const

const ROUTE_SMOKE_TIMEOUT = 120_000
const routeGroups = Array.from(
  { length: Math.ceil(VISUAL_ROUTES.length / 8) },
  (_, index) => VISUAL_ROUTES.slice(index * 8, (index + 1) * 8)
)

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
    await captureEvidence(page, {
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
    await captureEvidence(page, {
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
  await expect(
    page.getByRole('alert').getByRole('link', { name: '返回首页' })
  ).toHaveAttribute('href', '/')
  const retry = page.getByRole('button', { name: '重试', exact: true })
  await retry.focus()
  await expect(retry).toBeFocused()
})

// A visitor without the server's session hint never refreshes: that request
// is certain to 401 and spends the IP-keyed CriticalRateLimit budget.
test('guest pages skip the session refresh without the session hint', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light', authenticated: false })
  let refreshes = 0
  await page.route('**/api/user/auth/refresh', (route) => {
    refreshes++
    return route.fallback()
  })
  for (const path of ['/', '/sign-in', '/pricing']) {
    await page.goto(path, { waitUntil: 'networkidle' })
    await expect(page.getByRole('banner').first()).toBeVisible()
  }
  expect(refreshes).toBe(0)
})

function limitRefresh(page: Page) {
  return page.route('**/api/user/auth/refresh', (route) =>
    route.fulfill({
      status: 429,
      contentType: 'application/json',
      body: JSON.stringify({ success: false, message: 'Too many requests' }),
    })
  )
}

// docs/authentication.md: a temporary refresh failure is not a signed-out
// verdict. Public pages render the guest view; protected pages stay retryable.
for (const path of ['/sign-in', '/pricing']) {
  test(`rate-limited session check on ${path} renders the guest view`, async ({
    page,
  }) => {
    await configureStablePage(page, { theme: 'light' })
    await limitRefresh(page)
    await page.goto(path, { waitUntil: 'networkidle' })

    expect(new URL(page.url()).pathname).toBe(path)
    await expect(page.getByRole('banner').first()).toBeVisible()
    await expect(page.getByRole('alert')).toHaveCount(0)
    await assertNoHorizontalOverflow(page)
  })
}

test('rate-limited session check on a protected page explains the limit and links home', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  await limitRefresh(page)
  await page.goto('/keys', { waitUntil: 'networkidle' })

  const alert = page.getByRole('alert')
  await expect(alert).toContainText('请求过于频繁')
  await expect(alert).not.toContainText('渲染')
  expect(new URL(page.url()).pathname).toBe('/keys')
  await assertNoHorizontalOverflow(page)
  await alert.getByRole('link', { name: '返回首页' }).click()
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('alert')).toHaveCount(0)
})

for (const viewport of viewports) {
  for (const [groupIndex, routes] of routeGroups.entries()) {
    test(`all routes smoke at ${viewport.name}, group ${groupIndex + 1}`, async ({
      page,
    }) => {
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
          message
            .text()
            .startsWith("Blocked script execution in 'about:srcdoc'")
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

      for (const route of routes) {
        runtimeErrors.length = 0
        await test.step(`${route.name} ${route.path}`, async () => {
          await page.goto(route.path, { waitUntil: 'domcontentloaded' })
          await waitForStablePage(page)
          if (route.path === '/') await freezeAndInspectHomeCanvas(page)
          if (route.path.includes('manage=')) {
            await expect(
              page.locator('[role="dialog"][aria-modal="true"]')
            ).toBeVisible()
            await page
              .getByRole('button', { name: '关闭', exact: true })
              .click()
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
}

test('retired module entry points are absent and routes return 404', async ({
  page,
}) => {
  test.setTimeout(120_000)
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'dark', authenticated: true })

  await page.goto('/activity', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
  await expect(page.locator('[aria-label="签到日历"]')).toBeVisible()
  await expect(
    page.getByRole('button', { name: /RT农家乐|无趣大游戏/ })
  ).toHaveCount(0)

  await page.goto('/profile', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
  const identity = page.locator('.profile-identity')
  await expect(identity).toBeVisible()
  await expect(
    identity.getByRole('button', { name: /即将上线|查看等级权益/ })
  ).toHaveCount(0)

  for (const path of [
    '/market',
    '/invoice',
    '/farm',
    '/bigame',
    '/gateway',
    '/request-diagnostics',
  ]) {
    await page.goto(path, { waitUntil: 'domcontentloaded' })
    await expect(page.getByText('页面不存在', { exact: true })).toBeVisible()
    await expect(page.locator('[data-model-market]')).toHaveCount(0)
  }
})
