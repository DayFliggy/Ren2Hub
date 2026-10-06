import { expect, type Locator, test } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

async function expectMetricValuesToFit(card: Locator): Promise<void> {
  for (const metric of ['cpu', 'bandwidth']) {
    const fits = await card
      .locator(`[data-metric="${metric}"]`)
      .evaluate((tile) => {
        const row = tile.querySelector(
          '[data-cpu-gauge] p, [data-bandwidth-direction]'
        )
        if (!row) return false
        const rowRect = row.getBoundingClientRect()
        const parts = row.matches('[data-bandwidth-direction]')
          ? [row]
          : [...row.children]
        return parts.every((part) => {
          const partRect = part.getBoundingClientRect()
          return (
            partRect.left >= rowRect.left - 0.5 &&
            partRect.right <= rowRect.right + 0.5
          )
        })
      })
    expect(fits).toBe(true)
  }
}

/** Hovering a load cell stretches it to show its figure; leaving restores it. */
async function expectLoadCellsStretchOnHover(card: Locator): Promise<void> {
  const tile = card.locator('[data-metric="cpu"]')
  const cells = tile.locator('[data-cpu-load] [data-minutes]')
  await expect(cells).toHaveCount(3)
  const first = cells.first()
  const width = () => first.evaluate((el) => el.getBoundingClientRect().width)
  const restWidth = await width()

  await first.hover()
  await expect.poll(width).toBeGreaterThan(restWidth + 16)
  const value = first.locator('.cpu-load__value')
  await expect(value).toHaveText('12.3')
  await expect
    .poll(() => value.evaluate((el) => getComputedStyle(el).opacity))
    .toBe('1')
  // The stretched cell keeps clear of the label and inside the tile padding.
  const fits = await tile.evaluate((node) => {
    const tileRect = node.getBoundingClientRect()
    const padding = parseFloat(getComputedStyle(node).paddingRight)
    const label = node.querySelector('p')!.getBoundingClientRect()
    const group = node.querySelector('[data-cpu-load]')!.getBoundingClientRect()
    const cell = node.querySelector('[data-minutes]') as HTMLElement
    const figure = cell.querySelector('.cpu-load__value') as HTMLElement
    return (
      group.left >= label.right + 4 &&
      group.right <= tileRect.right - padding + 0.5 &&
      figure.scrollWidth <= cell.clientWidth
    )
  })
  expect(fits).toBe(true)

  await card.page().mouse.move(0, 0)
  await expect.poll(width).toBe(restWidth)
}

/** CPU, memory and disk headlines share one face, size and theme-driven weight. */
async function expectUnifiedHeadlineNumbers(card: Locator): Promise<void> {
  const result = await card
    .locator('[data-metric-value]')
    .evaluateAll((nodes) => ({
      styles: nodes.map((node) => {
        const style = getComputedStyle(node)
        return [style.fontFamily, style.fontWeight, style.fontSize].join('|')
      }),
      weights: nodes.map((node) => getComputedStyle(node).fontWeight),
      themeWeight: getComputedStyle(document.documentElement)
        .getPropertyValue('--display-number-weight')
        .trim(),
    }))
  expect(result.styles).toHaveLength(3)
  expect(new Set(result.styles).size).toBe(1)
  expect(result.styles[0]).toContain('JetBrainsMono')
  expect(result.weights[0]).toBe(result.themeWeight)
}

async function expectStableTileHeights(card: Locator): Promise<void> {
  const heights = await card
    .locator('[data-system-status-tile]')
    .evaluateAll((tiles) =>
      tiles.map((tile) => tile.getBoundingClientRect().height)
    )
  expect(heights).toHaveLength(4)
  expect(Math.max(...heights) - Math.min(...heights)).toBeLessThanOrEqual(1)
  // Compressed tile scale (64a2164f): mobile 88px / desktop sm:92px. The
  // floor guards against future edits collapsing the metric rows.
  expect(Math.min(...heights)).toBeGreaterThanOrEqual(88)
}

for (const theme of ['light', 'dark'] satisfies VisualTheme[]) {
  test(`system status keeps CPU and app traffic readable in ${theme} theme`, async ({
    page,
  }) => {
    await configureStablePage(page, { theme, authenticated: true })
    await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const card = page.locator('[data-system-status-card]')
    await expect(card).toBeVisible()
    await expect(card).toContainText('4.6%')
    await expect(card).toContainText('应用流量')
    await expect(card).toContainText('↑450 Kbps')
    await expect(card).toContainText('↓420 bps')
    await expectMetricValuesToFit(card)
    await expectStableTileHeights(card)
    await expectUnifiedHeadlineNumbers(card)
    await expectLoadCellsStretchOnHover(card)
    await assertNoHorizontalOverflow(page)

    await page.setViewportSize({ width: 390, height: 844 })
    await card.scrollIntoViewIfNeeded()
    await expect(card).toContainText('4.6%')
    await expect(card).toContainText('↑450 Kbps')
    await expect(card).toContainText('↓420 bps')
    await expectMetricValuesToFit(card)
    await expectStableTileHeights(card)
    await expectUnifiedHeadlineNumbers(card)
    await expectLoadCellsStretchOnHover(card)
    await assertNoHorizontalOverflow(page)
  })
}
