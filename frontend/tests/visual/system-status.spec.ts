import { expect, test } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const width of [390, 1440]) {
    test(`native dashboard marks unavailable system measurements ${theme} ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme, authenticated: true })
      const apiRequests: string[] = []
      page.on('request', (request) => {
        const path = new URL(request.url()).pathname
        if (path.startsWith('/api/')) apiRequests.push(path)
      })
      await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)

      const card = page.locator('[data-system-status-card]')
      await expect(card).toBeVisible()
      await expect(
        card.locator('[data-status-reachable="true"]')
      ).toContainText('未知')
      await expect(card.locator('[data-success-rate-empty]')).toBeVisible()
      await expect(card.locator('[data-system-version]')).toHaveText(
        'native-test'
      )
      const tiles = card.locator('[data-system-status-tile]')
      await expect(tiles).toHaveCount(4)
      for (const tile of await tiles.all()) {
        await expect(tile).toHaveAttribute('data-load-tier', 'unknown')
        await expect(tile).toContainText('--')
      }
      await expect(card.locator('[data-cpu-gauge-unknown]')).toBeVisible()
      await expect(
        card.locator('[data-cpu-gauge-active], [data-bandwidth-sparkline]')
      ).toHaveCount(0)
      expect(apiRequests.some((path) => path.startsWith('/api/next/'))).toBe(
        false
      )
      await assertNoHorizontalOverflow(page)
    })
  }
}
