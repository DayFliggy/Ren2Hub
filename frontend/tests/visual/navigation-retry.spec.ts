import { expect, test } from '@playwright/test'
import type { App } from 'vue'
import type { Router } from 'vue-router'

import { configureStablePage, waitForStablePage } from './fixtures'

test('retry returns to a failed in-app destination with its query and hash', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light', authenticated: false })
  let limited = true
  await page.route('**/api/user/auth/refresh', async (route) => {
    if (!limited) return route.fallback()
    await route.fulfill({
      status: 429,
      json: { success: false, message: 'Too many requests' },
    })
  })
  // Home now restores the session too. Start on an unguarded error page so
  // the rejected refresh belongs to the in-app destination under test.
  await page.goto('/404')
  await expect(page.getByText('页面不存在', { exact: true })).toBeVisible()

  const target = '/pricing?model=gpt%2F4&unit=K#prices'
  // Use the live router to exercise an in-app navigation, whose failure
  // leaves the address bar on the source page.
  await page.evaluate(async (path) => {
    const app = (
      document.querySelector('#app') as HTMLElement & { __vue_app__: App }
    ).__vue_app__
    const router = app.config.globalProperties.$router as Router
    await router.push(path).catch(() => undefined)
  }, target)
  await expect(page.getByRole('alert')).toContainText('请求过于频繁')
  expect(new URL(page.url()).pathname).toBe('/404')

  limited = false
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect(page).toHaveURL(new URL(target, page.url()).href)
  await waitForStablePage(page)
  await expect(page.locator('.public-pricing-filters')).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('manual retry recovers the failed lazy page after the automatic reload was spent', async ({
  page,
}) => {
  const chunk =
    /(?:assets\/SignInView-[^/]+\.js|src\/views\/auth\/SignInView\.vue)/
  await configureStablePage(page, {
    theme: 'dark',
    authenticated: false,
    expectedFailedRequests: [chunk],
  })
  await page.goto('/')
  await expect(page.locator('.app-navbar')).toBeVisible()
  await page.route(chunk, (route) => route.abort())
  await page.evaluate(() => sessionStorage.setItem('ren2hub_chunk_reload', '1'))
  await page.locator('a[href="/sign-in"]').first().click()
  await expect(page.getByRole('alert')).toContainText('此页面暂时无法显示')
  expect(new URL(page.url()).pathname).toBe('/')

  await page.unroute(chunk)
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect(page).toHaveURL(/\/sign-in$/)
  await waitForStablePage(page)
  await expect(page.locator('input[autocomplete="username"]')).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
  expect(
    await page.evaluate(() => sessionStorage.getItem('ren2hub_chunk_reload'))
  ).toBeNull()
})
