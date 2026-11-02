import { expect, test } from '@playwright/test'
import { nativeUsageSummary } from '../contracts/channel-core-fixtures'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

for (const role of [1, 10, 100]) {
  for (const theme of ['light', 'dark'] as const) {
    for (const width of [390, 1440]) {
      test(`dashboard scope role ${role} ${theme} ${width}`, async ({
        page,
      }, testInfo) => {
        await page.setViewportSize({ width, height: 900 })
        await configureStablePage(page, {
          theme,
          authenticated: true,
          user: { role, display_name: 'Root User' },
        })
        let globalRequests = 0
        await page.route('**/api/data/summary?*', async (route) => {
          globalRequests++
          const url = new URL(route.request().url())
          const data = nativeUsageSummary(
            Date.parse('2026-07-27T12:00:00+08:00') / 1000,
            url.searchParams.get('include_comparison') !== 'false'
          )
          data.kpi.totalRequests = 40
          data.kpi.totalTokens = 12000
          data.models[0]!.model = 'global-model'
          await route.fulfill({ json: { success: true, data } })
        })
        await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
        await waitForStablePage(page)
        const scope = page.getByRole('radiogroup', {
          name: '统计范围',
          exact: true,
        })
        await expect(page.locator('[data-dashboard-page]')).toHaveAttribute(
          'data-dashboard-scope',
          'self'
        )
        if (role === 1) {
          await expect(scope).toHaveCount(0)
          expect(globalRequests).toBe(0)
        } else {
          await expect(
            scope.getByRole('radio', { name: '个人', exact: true })
          ).toHaveAttribute('aria-checked', 'true')
          await scope.getByRole('radio', { name: '全站', exact: true }).click()
          await expect(
            page.locator('[data-kpi-cell="requests"]')
          ).toContainText('40')
          await expect(page.locator('[data-balance-card]')).toHaveCount(0)
          await expect(page.locator('[data-discount-card]')).toHaveCount(0)
          await expect(page.locator('[data-system-status-card]')).toBeVisible()
          await assertNoHorizontalOverflow(page)
          if (role === 100)
            await page.screenshot({
              path: testInfo.outputPath(`scope-${theme}-${width}.png`),
              fullPage: true,
            })
          await page.getByRole('tab', { name: '统计数据', exact: true }).click()
          await expect(page.locator('[role="tabpanel"]')).toContainText(
            'global-model'
          )
          await expect(
            scope.getByRole('radio', { name: '全站', exact: true })
          ).toHaveAttribute('aria-checked', 'true')
          await scope
            .getByRole('radio', { name: '全站', exact: true })
            .press('ArrowLeft')
          await expect(
            scope.getByRole('radio', { name: '个人', exact: true })
          ).toHaveAttribute('aria-checked', 'true')
          await expect(page.locator('[role="tabpanel"]')).not.toContainText(
            'global-model'
          )
          expect(globalRequests).toBeGreaterThanOrEqual(5)
        }
        await page.getByRole('tab', { name: '自动路由', exact: true }).click()
        await expect(page.locator('[data-auto-route-panel]')).toBeVisible()
        await expect(scope).toHaveCount(0)
        await assertNoHorizontalOverflow(page)
        await waitForStablePage(page)
      })
    }
  }
}
