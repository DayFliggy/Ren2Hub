import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

// The shared fixture lists one soft-deleted user (`DeletedAt` set), as the
// unscoped upstream list query does.
for (const width of [1440, 390]) {
  test(`admin users soft-deleted row ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const state = await configureStablePage(page, { theme: 'light' })
    const statuses: Array<string | null> = []
    page.on('request', (request) => {
      const url = new URL(request.url())
      if (url.pathname === '/api/user/search')
        statuses.push(url.searchParams.get('status'))
    })
    await page.goto('/users')

    const row = page
      .locator(width > 768 ? 'tbody tr' : '[data-user-mobile-row]')
      .filter({ hasText: 'removed.member', visible: true })
    await expect(row).toContainText('已删除')
    await expect(row.locator('input[type="checkbox"]')).toHaveCount(0)
    await expect(row.getByRole('button')).toHaveCount(0)
    const active = page
      .locator(width > 768 ? 'tbody tr' : '[data-user-mobile-row]')
      .filter({ hasText: 'Native Member', visible: true })
    await expect(
      active.getByRole('button', { name: '编辑用户', exact: true })
    ).toBeEnabled()

    await page.getByLabel('用户状态', { exact: true }).click()
    await page.getByRole('option', { name: '已删除', exact: true }).click()
    await expect.poll(() => statuses.at(-1)).toBe('-1')
    await assertNoHorizontalOverflow(page)
    expect(state.errors).toEqual([])
  })
}
