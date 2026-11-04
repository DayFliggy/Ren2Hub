import { expect, test } from '@playwright/test'
import { configureStablePage, assertNoHorizontalOverflow } from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    test(`wallet query permission ${theme} ${width}`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, {
        theme,
        authenticated: true,
        locale: 'en',
      })
      await page.addInitScript(() =>
        localStorage.setItem('ren2hub_locale', 'en')
      )
      const writes: Record<string, unknown>[] = []
      await page.route('**/api/token/**', async (route) => {
        if (
          route.request().method() === 'POST' ||
          route.request().method() === 'PUT'
        ) {
          writes.push(route.request().postDataJSON())
          await route.fulfill({
            json: { success: true, message: '', data: {} },
          })
          return
        }
        await route.fallback()
      })
      await page.goto('/keys')
      await page
        .getByRole('button', { name: 'Create key', exact: true })
        .click()
      await page.getByRole('button', { name: /Advanced settings/i }).click()
      const toggle = page.getByRole('switch', {
        name: 'Allow querying account balance',
      })
      await expect(toggle).toHaveAttribute('aria-checked', 'true')
      await expect(
        page.getByText(
          'Anyone holding this key can query your account’s available and used quota.'
        )
      ).toBeVisible()
      await page.locator('[name="token-name"]').fill('restricted wallet query')
      await toggle.focus()
      await page.keyboard.press('Space')
      await expect(toggle).toHaveAttribute('aria-checked', 'false')
      await assertNoHorizontalOverflow(page)
      await page.getByRole('button', { name: 'Confirm', exact: true }).click()
      await expect(toggle).toBeHidden()
      expect(writes[0]?.allow_user_quota_query).toBe(false)
      await page
        .getByRole('button', { name: 'Edit key', exact: true })
        .filter({ visible: true })
        .first()
        .click()
      const types = page.getByRole('dialog').getByRole('radio')
      await expect(types).toHaveCount(2)
      await expect(types.nth(0)).toBeDisabled()
      await expect(types.nth(1)).toBeDisabled()
      await page.getByRole('button', { name: /Advanced settings/i }).click()
      await expect(toggle).toHaveAttribute('aria-checked', 'true')
      await toggle.click()
      await expect(toggle).toHaveAttribute('aria-checked', 'false')
      await toggle.click()
      await expect(toggle).toHaveAttribute('aria-checked', 'true')
      await page.getByRole('button', { name: 'Confirm', exact: true }).click()
      await expect(toggle).toBeHidden()
      expect(writes[1]?.allow_user_quota_query).toBe(true)
    })
  }
}
