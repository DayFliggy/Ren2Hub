import { expect, test } from '@playwright/test'
import { configureStablePage, assertNoHorizontalOverflow } from './fixtures'

// Attempt timelines belonged to the retired admission product. Native request
// metadata remains available from the consume log itself without a second API.
for (const theme of ['light', 'dark'] as const)
  for (const width of [1440, 390]) {
    test(`native request log details ${theme} ${width}`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 })
      const state = await configureStablePage(page, { theme })
      const requests: string[] = []
      page.on('request', (request) =>
        requests.push(new URL(request.url()).pathname)
      )
      await page.goto('/logs')
      await expect(
        page.getByText('Native Key', { exact: true }).filter({ visible: true })
      ).toBeVisible()
      await page
        .getByRole('button', { name: '查看日志详情', exact: true })
        .filter({ visible: true })
        .click()
      const detail = page.getByRole('dialog')
      await expect(detail).toContainText('gpt-native')
      await expect(detail.locator('pre')).toHaveText('{}')
      await expect(detail.locator('[data-diagnostic-attempt]')).toHaveCount(0)
      await assertNoHorizontalOverflow(page)
      await detail.locator('[data-handdrawn="modal"]').focus()
      await page.keyboard.press('Escape')
      await expect(detail).toHaveCount(0)
      expect(requests.some((path) => path.includes('diagnostics'))).toBe(false)
      expect(state.errors).toEqual([])
    })
  }
