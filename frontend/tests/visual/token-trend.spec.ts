import { expect, test } from '@playwright/test'
import { nativeUsageSummary } from '../contracts/channel-core-fixtures'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

for (const [theme, viewport] of [
  ['light', 'desktop'],
  ['dark', 'mobile'],
] as const) {
  test(`${theme} ${viewport} token trend shows real chart canvas by default`, async ({
    page,
  }) => {
    await page.setViewportSize(
      viewport === 'desktop'
        ? { width: 1440, height: 900 }
        : { width: 390, height: 844 }
    )
    await configureStablePage(page, { theme, authenticated: true })
    const summary = nativeUsageSummary(
      Date.parse('2026-07-27T12:00:00+08:00') / 1000
    )
    summary.token_trend = summary.token_trend.map((point) => ({
      ...point,
      total: 0,
      input: 0,
      output: 0,
      cache_create: 0,
      cache_read: 0,
    }))
    await page.route('**/api/data/self/summary?*', (route) =>
      route.fulfill({ json: { success: true, data: summary } })
    )
    await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const card = page.locator('[data-token-trend-card]')
    await expect(card).toBeVisible()
    await expect(card.locator('canvas')).toHaveCount(1)
    await assertNoHorizontalOverflow(page)
  })
}

test('token trend mounts a non-empty ECharts canvas', async ({ page }) => {
  await configureStablePage(page, { theme: 'dark', authenticated: true })
  await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  const card = page.locator('[data-token-trend-card]')
  const canvas = card.locator('canvas')
  await expect(canvas).toBeVisible()
  const size = await canvas.evaluate((element) => {
    const target = element as HTMLCanvasElement
    return { width: target.width, height: target.height }
  })
  expect(size.width).toBeGreaterThan(0)
  expect(size.height).toBeGreaterThan(0)
})
