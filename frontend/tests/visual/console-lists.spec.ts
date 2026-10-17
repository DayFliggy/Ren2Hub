import { expect, test, type Locator } from '@playwright/test'

import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

// The key table needs a 60rem card. The sidebar appears at 1024px, so a
// 1024px viewport has a narrower card (cards) than a 1023px one (table).
const layouts = [
  { width: 768, height: 1024, table: false },
  { width: 1023, height: 768, table: true },
  { width: 1024, height: 768, table: false },
  { width: 1440, height: 900, table: true },
] as const

for (const layout of layouts) {
  test(`keys at ${layout.width} use the ${layout.table ? 'table' : 'cards'}`, async ({
    page,
  }) => {
    await page.setViewportSize(layout)
    await configureStablePage(page, { theme: 'light', authenticated: true })
    await page.goto('/keys', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const table = page.locator('[data-key-page] .data-table-shell')
    const cards = page.locator('[data-key-mobile-row]')
    await expect(layout.table ? table : cards).toBeVisible()
    await expect(layout.table ? cards : table).toBeHidden()
    await assertNoHorizontalOverflow(page)
    await assertInteractiveCentersVisible(page)
  })
}

async function expectTarget(target: Locator, minWidth = 24) {
  const box = await target.boundingBox()
  expect(box?.width).toBeGreaterThanOrEqual(minWidth)
  expect(box?.height).toBeGreaterThanOrEqual(24)
}

for (const theme of ['light', 'dark'] as const satisfies VisualTheme[]) {
  test(`${theme} mobile key and log targets are at least 24px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await configureStablePage(page, { theme, authenticated: true })
    await page.goto('/keys', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const checkbox = page.getByRole('checkbox', {
      name: '选择令牌「Native Key」',
    })
    const checkboxTarget = page.locator('label').filter({ has: checkbox })
    await expectTarget(checkboxTarget)
    // The padding around the glyph must forward clicks to the checkbox.
    const box = await checkboxTarget.boundingBox()
    await page.mouse.click(box!.x + 1, box!.y + 1)
    await expect(checkbox).toBeChecked()

    await expectTarget(
      page
        .locator('[data-key-mobile-row]')
        .getByRole('button', { name: '编辑令牌', exact: true })
    )

    await page.goto('/usage-logs', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)
    await expectTarget(
      page
        .getByRole('button', { name: '查看日志详情', exact: true })
        .filter({ visible: true })
    )
  })
}
