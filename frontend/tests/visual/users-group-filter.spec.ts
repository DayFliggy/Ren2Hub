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
    quota: 2500000,
    used_quota: 0,
    request_count: 0,
    aff_count: 0,
    aff_quota: 0,
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
      await page.route(/\/api\/user\/(?:search)?(?:\?.*)?$/, (route) => {
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
      const filter = page.getByLabel('用户分组', { exact: true })
      await expect(filter).toBeVisible()
      await expect(
        page.getByText('default.member').filter({ visible: true }).first()
      ).toBeVisible()
      await filter.fill('premium')
      await expect(filter).toHaveValue('premium')
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

      await filter.fill('')
      await expect(
        page.getByText('default.member').filter({ visible: true }).first()
      ).toBeVisible()
      expect(requestedGroups.at(-1)).toBe('')
      expect(errors).toEqual([])
    })
  }
}
