import { expect, test } from '@playwright/test'
import { nativeCoreFixture } from './native-core-fixture'
for (const theme of ['light', 'dark'])
  for (const width of [1440, 390]) {
    test(`${theme} ${width} native embedded model pricing`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 })
      const state = await nativeCoreFixture(page, theme)
      await page.goto('/models/pricing')
      await expect(page.getByText('model-a', { exact: true })).toBeVisible()
      await page.getByText('model-a', { exact: true }).click()
      const editor = page.locator('section[aria-label="Edit model pricing"]')
      await expect(editor).toBeVisible()
      await expect(
        editor.getByRole('tab', {
          name: 'Per-request (deprecated)',
          exact: true,
        })
      ).toHaveAttribute('aria-selected', 'true')
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1
        )
      ).toBe(true)
      await expect(page.locator('[role=alert]')).toHaveCount(0)
      await page.screenshot({
        path: testInfo.outputPath(`native-pricing-${theme}-${width}.png`),
      })
      expect(state.unexpected).toEqual([])
      expect(state.errors).toEqual([])
      expect(state.missingMessages).toEqual([])
    })
  }
