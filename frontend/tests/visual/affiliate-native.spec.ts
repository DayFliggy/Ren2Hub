import { expect, test } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'
import { configureBusinessPage } from './businessFixtures'

for (const theme of ['light', 'dark'] as const satisfies VisualTheme[]) {
  for (const viewport of [
    { name: 'desktop', width: 1440, height: 900 },
    { name: 'mobile', width: 390, height: 844 },
  ]) {
    test(`${theme} ${viewport.name} native referral pagination and full transfer`, async ({
      page,
    }) => {
      await page.setViewportSize(viewport)
      const user = await configureBusinessPage(page, theme)
      let available = 250000
      let transferBody: unknown
      const orders: string[] = []
      await page.route('**/api/user/affiliate?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              code: 'VISUAL',
              code_enabled: true,
              invited: 21,
              effective_rate_bps: 2000,
              effective_rate_percent: 20,
              transferable: available,
              frozen_reward: 50000,
              reward_total: 300000,
              policy: { enabled: true, freeze_hours: 24 },
              monthly_series: Array.from({ length: 12 }, (_, index) => ({
                month: new Date(Date.UTC(2025, 7 + index, 1))
                  .toISOString()
                  .slice(0, 7),
                new_count: index === 11 ? 21 : 0,
                cumulative: index === 11 ? 21 : 0,
                reward: index === 11 ? 300000 : 0,
              })),
            },
          },
        })
      )
      await page.route('**/api/user/affiliate/invitees?*', (route) => {
        const query = new URL(route.request().url()).searchParams
        const currentPage = Number(query.get('p'))
        orders.push(query.get('order') ?? '')
        const items =
          currentPage === 2
            ? [
                {
                  masked_name: 'z***z',
                  invited_at: 1785038400,
                  reward_quota: 0,
                },
              ]
            : Array.from({ length: 20 }, (_, index) => ({
                masked_name: `a***${index}`,
                invited_at: 1785038400 - index * 86400,
                reward_quota: 12500,
              }))
        return route.fulfill({
          json: {
            success: true,
            data: { items, page: currentPage, page_size: 20, total: 21 },
          },
        })
      })
      await page.route('**/api/user/affiliate/transfer', (route) => {
        transferBody = route.request().postDataJSON()
        const transferred = available
        user.quota += transferred
        available = 0
        return route.fulfill({
          json: {
            success: true,
            data: {
              replayed: false,
              transferred_quota: transferred,
              wallet_quota: user.quota,
            },
          },
        })
      })
      await page.goto('/invite', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)
      await expect(page.getByText('a***0', { exact: true })).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.getByRole('button', { name: '下一页', exact: true }).click()
      await expect(page.getByText('z***z', { exact: true })).toBeVisible()
      expect(orders).toContain('desc')
      await page
        .getByRole('button', { name: '转入余额', exact: true })
        .first()
        .click()
      const dialog = page.getByRole('dialog')
      await expect(dialog.locator('input')).toHaveCount(0)
      await dialog.getByRole('button', { name: '确认', exact: true }).click()
      await expect(dialog).not.toBeVisible()
      expect(transferBody).toEqual({ operation_id: expect.any(String) })
      expect(user.quota).toBe(750000)
    })
  }
}
