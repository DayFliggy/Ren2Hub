import { expect, test } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

// Host CPU, disk and network telemetry has no native public endpoint. Keep the
// restored card's layout and unknown-state checks; do not simulate retired data.
for (const theme of ['light', 'dark'] as const) {
  for (const locale of ['zh-CN', 'en']) {
    for (const width of [320, 390, 1280, 1366, 1440]) {
      test(`system status honest placeholders ${theme} ${locale} ${width}`, async ({
        page,
      }, info) => {
        await page.setViewportSize({ width, height: 900 })
        const state = await configureStablePage(page, {
          theme,
          locale,
          status: { version: '39daa6ccee738f7a26fdba131c829be1adbbdd5b' },
        })
        await page.goto('/dashboard')
        await waitForStablePage(page)
        const card = page.locator('[data-system-status-card]')
        await expect(card).toBeVisible()
        await expect(card.locator('[data-system-status-tile]')).toHaveCount(4)
        await expect(card).toContainText('--')
        await expect(card).not.toContainText(/4\.6%|450 Kbps|420 bps/)
        const heights = await card
          .locator('[data-system-status-tile]')
          .evaluateAll((nodes) =>
            nodes.map((node) => node.getBoundingClientRect().height)
          )
        expect(Math.min(...heights)).toBeGreaterThanOrEqual(88)
        expect(Math.max(...heights) - Math.min(...heights)).toBeLessThanOrEqual(
          1
        )
        const overflow = await card.evaluate(
          (node) => node.scrollWidth - node.clientWidth
        )
        expect(overflow).toBeLessThanOrEqual(1)
        await expect(card.locator('[data-system-version]')).toHaveAttribute(
          'title',
          /39daa6c/
        )
        await assertNoHorizontalOverflow(page)
        await card.screenshot({ path: info.outputPath('system-status.png') })
        expect(state.errors).toEqual([])
      })
    }
  }
}
