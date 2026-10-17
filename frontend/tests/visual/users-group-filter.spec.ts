import { expect, test } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

const viewports = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'mobile', width: 390, height: 844 },
] as const

function listedUser(id: number, username: string, group: string) {
  return {
    id,
    username,
    display_name: '',
    email: `${username}@example.test`,
    role: 1,
    status: 1,
    group,
    available_quota: '2500000',
    reserved_quota: '0',
    quota_version: 1,
    used_quota: '0',
    request_count: 0,
    invited_count: 0,
    affiliate_quota: '0',
    inviter_id: 0,
    created_time: 1_780_000_000,
    last_login_time: 0,
  }
}

for (const theme of ['light', 'dark'] as VisualTheme[]) {
  for (const viewport of viewports) {
    test(`${theme} ${viewport.name} users group filter`, async ({ page }) => {
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      await page.setViewportSize(viewport)
      await configureStablePage(page, { theme, authenticated: true })
      const requestedGroups: string[] = []
      await page.route(/\/api\/next\/admin\/users(?:\?.*)?$/, (route) => {
        const group = new URL(route.request().url()).searchParams.get('group')
        requestedGroups.push(group ?? '')
        const items =
          group === 'premium'
            ? [listedUser(43, 'premium.member', 'premium')]
            : [
                listedUser(42, 'default.member', 'default'),
                listedUser(43, 'premium.member', 'premium'),
              ]
        return route.fulfill({
          json: {
            success: true,
            data: {
              items,
              total: items.length,
              page: 1,
              page_size: 20,
              role_counts: { '1': 2 },
              status_counts: { enabled: 2, disabled: 0 },
              group_counts: { default: 1, premium: 1 },
            },
          },
        })
      })

      await page.goto('/users')
      await waitForStablePage(page)
      const filter = page.getByRole('combobox', {
        name: '用户分组',
        exact: true,
      })
      await expect(filter).toBeVisible()
      await expect(
        page.getByText('default.member').filter({ visible: true }).first()
      ).toBeVisible()
      if (viewport.name === 'tablet') {
        // Dormant referral cells contain screen-reader text. It must remain
        // inside the horizontal table scroller even when its column is hidden.
        const table = page.locator('.data-table-body-viewport')
        await expect(
          table.locator('[data-user-invite="dormant"] .sr-only').first()
        ).toContainText('无邀请记录')
        await assertNoHorizontalOverflow(page)
        await table.evaluate((element) => {
          element.scrollLeft = element.scrollWidth
        })
        await expect
          .poll(() => table.evaluate((element) => element.scrollLeft))
          .toBeGreaterThan(0)
        await assertNoHorizontalOverflow(page)
      }

      await filter.click()
      await page.getByRole('option', { name: 'premium (1)' }).click()
      await expect(filter).toContainText('premium')
      await expect(
        page.getByText('default.member').filter({ visible: true })
      ).toHaveCount(0)
      await expect(
        page.getByText('premium.member').filter({ visible: true }).first()
      ).toBeVisible()
      expect(requestedGroups.at(-1)).toBe('premium')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: test
          .info()
          .outputPath(`users-group-${theme}-${viewport.name}.png`),
      })

      await filter.click()
      await page.getByRole('option', { name: '全部分组' }).click()
      await expect(
        page.getByText('default.member').filter({ visible: true }).first()
      ).toBeVisible()
      expect(requestedGroups.at(-1)).toBe('')
      expect(errors).toEqual([])
    })
  }
}
