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

/**
 * Hovering the CPU load cells reveals a readout with all three load values. It
 * stays open while the pointer moves onto it, Escape dismisses it in place
 * (WCAG 1.4.13), and it stays within the card.
 */
async function expectLoadReadoutOnInteraction(card: Locator): Promise<void> {
  const page = card.page()
  const tile = card.locator('[data-metric="cpu"]')
  await expect(tile.locator('[data-cpu-load] [data-minutes]')).toHaveCount(3)

  const trigger = tile.locator('.cpu-load__trigger')
  const readout = tile.locator('[data-load-readout]')
  const opacity = () => readout.evaluate((el) => getComputedStyle(el).opacity)
  await expect(trigger).toHaveAttribute('aria-expanded', 'false')

  await trigger.hover()
  await expect.poll(opacity).toBe('1')
  await expect(
    readout.locator('[data-load-window]').first().locator('.cpu-load__figure')
  ).toHaveText('12.3')

  const fits = await tile.evaluate((node) => {
    const cardRect = node
      .closest('[data-system-status-card]')!
      .getBoundingClientRect()
    const readoutRect = node
      .querySelector('[data-load-readout]')!
      .getBoundingClientRect()
    return (
      readoutRect.left >= cardRect.left - 2 &&
      readoutRect.right <= cardRect.right + 2 &&
      readoutRect.bottom <= cardRect.bottom + 2
    )
  })
  expect(fits).toBe(true)

  await readout.hover()
  await expect(trigger).toHaveAttribute('aria-expanded', 'true')
  await page.keyboard.press('Escape')
  await expect(trigger).toHaveAttribute('aria-expanded', 'false')
  await expect.poll(opacity).toBe('0')
  await page.mouse.move(0, 0)
}

/** The tap target for the CPU load cells meets the 44×44px minimum. */
async function expectMinimumTapTarget(card: Locator): Promise<void> {
  const size = await card
    .locator('[data-metric="cpu"] .cpu-load__trigger')
    .evaluate((button) => {
      const target = getComputedStyle(button, '::before')
      return [parseFloat(target.width), parseFloat(target.height)]
    })
  expect(size[0]).toBeGreaterThanOrEqual(44)
  expect(size[1]).toBeGreaterThanOrEqual(44)
}

/** Keyboard focus on the load trigger draws the shared focus outline. */
async function expectKeyboardFocusRing(card: Locator): Promise<void> {
  const trigger = card.locator('[data-metric="cpu"] .cpu-load__trigger')
  await trigger.focus()
  await card.page().keyboard.press('Shift+Tab')
  await card.page().keyboard.press('Tab')
  await expect(trigger).toBeFocused()
  const outline = await trigger.evaluate((button) => {
    const style = getComputedStyle(button)
    return [style.outlineStyle, parseFloat(style.outlineWidth)] as const
  })
  expect(outline[0]).toBe('solid')
  expect(outline[1]).toBeGreaterThanOrEqual(2)
  await trigger.blur()
}

/**
 * The header keeps its title whole and the rate on screen at every card width;
 * a tight header drops only the caption, which stays in the accessibility tree.
 */
async function expectHeaderFits(card: Locator): Promise<void> {
  const header = await card.evaluate((node) => {
    const title = node.querySelector('h2')!
    const label = node.querySelector('.success-rate-label')!
    const value = node.querySelector('.success-rate-value')!
    const labelRect = label.getBoundingClientRect()
    return {
      titleClipped: title.scrollWidth > title.clientWidth,
      labelLines: labelRect.width > 1 ? Math.round(labelRect.height / 11) : 0,
      valueWidth: value.getBoundingClientRect().width,
    }
  })
  expect(header.titleClipped).toBe(false)
  expect(header.labelLines).toBeLessThanOrEqual(1)
  expect(header.valueWidth).toBeGreaterThan(0)
  await expect(card.getByText(/API/)).toHaveCount(1)
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
    await expectLoadReadoutOnInteraction(card)
    await expectMinimumTapTarget(card)
    await expectKeyboardFocusRing(card)
    await expectHeaderFits(card)
    await assertNoHorizontalOverflow(page)

    await page.setViewportSize({ width: 390, height: 844 })
    await card.scrollIntoViewIfNeeded()
    await expect(card).toContainText('4.6%')
    await expect(card).toContainText('↑450 Kbps')
    await expect(card).toContainText('↓420 bps')
    await expectMetricValuesToFit(card)
    await expectStableTileHeights(card)
    await expectUnifiedHeadlineNumbers(card)
    await expectLoadReadoutOnInteraction(card)
    await expectMinimumTapTarget(card)
    await expectHeaderFits(card)
    await assertNoHorizontalOverflow(page)
  })
}

// English needs the widest header. The card width follows the dashboard grid,
// so a 1280 desktop is as tight as a phone.
for (const width of [320, 390, 1280, 1366, 1440]) {
  test(`system status header fits in English at ${width}px`, async ({
    page,
  }) => {
    await configureStablePage(page, { theme: 'light', authenticated: true })
    await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const card = page.locator('[data-system-status-card]')
    await expect(card).toContainText('System status')
    await expectHeaderFits(card)
    await assertNoHorizontalOverflow(page)
  })
}
