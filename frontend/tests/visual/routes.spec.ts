import { expect, test } from '@playwright/test'

import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage,
  freezeAndInspectHomeCanvas,
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
  await expect(
    page.getByRole('alert').getByRole('link', { name: '返回首页' })
  ).toHaveAttribute('href', '/')
  const retry = page.getByRole('button', { name: '重试', exact: true })
  await retry.focus()
  await expect(retry).toBeFocused()
})

// docs/authentication.md: a temporary refresh failure stays retryable; it is
// neither a signed-out state nor a rendering error.
for (const path of ['/sign-in', '/pricing']) {
  test(`rate-limited session check on ${path} explains the limit and links home`, async ({
    page,
  }) => {
    await configureStablePage(page, { theme: 'light', authenticated: false })
    await page.route('**/api/user/auth/refresh', (route) =>
      route.fulfill({
        status: 429,
        contentType: 'application/json',
        body: JSON.stringify({ success: false, message: 'Too many requests' }),
      })
    )
    await page.goto(path, { waitUntil: 'networkidle' })

    const alert = page.getByRole('alert')
    await expect(alert).toContainText('请求过于频繁')
    await expect(alert).not.toContainText('渲染')
    expect(new URL(page.url()).pathname).toBe(path)
    await assertNoHorizontalOverflow(page)
    await alert.getByRole('link', { name: '返回首页' }).click()
    await expect(page).toHaveURL(/\/$/)
    await expect(page.getByRole('alert')).toHaveCount(0)
  })
}

for (const viewport of viewports) {
  test(`all routes smoke at ${viewport.name}`, async ({ browser, baseURL }) => {
    test.setTimeout(ROUTE_SMOKE_TIMEOUT)
    for (const route of VISUAL_ROUTES) {
      await test.step(`${route.name} ${route.path}`, async () => {
        const page = await browser.newPage({
          baseURL,
          viewport,
          locale: 'zh-CN',
          timezoneId: 'Asia/Shanghai',
          reducedMotion: 'reduce',
        })
        try {
          const state = await configureStablePage(page, {
            theme: process.env.PLAYWRIGHT_THEME === 'light' ? 'light' : 'dark',
            authenticated: !route.guestOnly,
          })
          await page.goto(route.path, { waitUntil: 'domcontentloaded' })
          await waitForStablePage(page)
          if (route.guestOnly)
            await expect(page).toHaveURL(new RegExp(`${route.path}$`))
          if (route.path === '/') await freezeAndInspectHomeCanvas(page)
          await assertNoHorizontalOverflow(page)
          await assertInteractiveCentersVisible(page)
          expect(state.unexpected, route.path).toEqual([])
          expect(state.errors, route.path).toEqual([])
          expect(state.missingMessages, route.path).toEqual([])
        } finally {
          await page.close()
        }
      })
    }
  })
}

test('unavailable activity entries are disabled and retired routes fail closed', async ({
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
  await expect(page.locator('.profile-identity')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Native Root' })).toBeVisible()

  for (const path of ['/market', '/farm', '/bigame']) {
    await page.goto(path, { waitUntil: 'domcontentloaded' })
    await expect(page.getByText('页面不存在', { exact: true })).toBeVisible()
    await expect(page.locator('.app-sidebar')).toHaveCount(0)
  }
})
