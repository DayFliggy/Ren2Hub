import { expect, test } from '@playwright/test'
import { configureStablePage, waitForStablePage } from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  test(`native month control keeps themed spacing and visible focus ${theme}`, async ({
    page,
  }) => {
    await configureStablePage(page, { theme })
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/activity')
    await waitForStablePage(page)
    const input = page.locator('input[type="month"]')
    await expect(input).toBeVisible()
    const box = await input.boundingBox()
    expect(box!.height).toBeGreaterThanOrEqual(36)
    const padding = await input.evaluate((el) =>
      parseFloat(getComputedStyle(el).paddingLeft)
    )
    expect(padding).toBeGreaterThanOrEqual(8)
    await input.focus()
    await expect(input).toBeFocused()
    expect(
      await input.evaluate((el) => {
        const css = getComputedStyle(el)
        return css.outlineStyle !== 'none' || css.boxShadow !== 'none'
      })
    ).toBe(true)
    await input.fill('2026-06')
    await expect(input).toHaveValue('2026-06')
  })
}
