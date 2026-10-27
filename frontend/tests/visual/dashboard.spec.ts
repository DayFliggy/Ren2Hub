import { test as base, expect, type Locator, type Page } from '@playwright/test'
import { nativeUsageSummary } from '../contracts/channel-core-fixtures'
import { createModelMarketFixture } from '../fixtures/modelMarket'

const NOW = Date.parse('2026-07-27T12:00:00+08:00') / 1000

import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage,
  contrastRatio,
  measureLuminance,
  minTextContrast,
  visualDateKey,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

const test = base.extend<{
  dashboardTheme: VisualTheme
  emptyAccount: boolean
}>({
  dashboardTheme: ['dark', { option: true }],
  emptyAccount: [false, { option: true }],
})

const PERIOD_LABELS = { month: '月', quarter: '季', year: '年' } as const
type Period = keyof typeof PERIOD_LABELS
const PERIODS = Object.keys(PERIOD_LABELS) as Period[]

function periodOption(card: Locator, period: Period): Locator {
  return card
    .getByRole('radiogroup', { name: '统计周期' })
    .getByRole('radio', { name: PERIOD_LABELS[period], exact: true })
}

function metricOption(card: Locator, label: string): Locator {
  return card
    .getByRole('radiogroup', { name: '分布指标' })
    .getByRole('radio', { name: label, exact: true })
}

test.beforeEach(async ({ page, dashboardTheme, emptyAccount }) => {
  await configureStablePage(page, {
    theme: dashboardTheme,
    authenticated: true,
    user: emptyAccount ? { used_quota: 0 } : {},
  })
  if (emptyAccount) {
    // Registered after the fixture routes, so it takes precedence.
    await page.route('**/api/data/self/summary?*', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: { ...nativeUsageSummary(NOW), distribution: [] },
        },
      })
    )
  }
  await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
})

test('usage heatmap fits the year on desktop and drags on phones', async ({
  page,
}) => {
  const card = page.locator('[data-usage-distribution]')
  await expect(card).toBeVisible()
  await periodOption(card, 'year').click()
  await metricOption(card, 'Token').click()

  await expect(periodOption(card, 'year')).toHaveAttribute(
    'aria-checked',
    'true'
  )
  await expect(metricOption(card, 'Token')).toHaveAttribute(
    'aria-checked',
    'true'
  )
  await expect(card.locator('[data-usage-date]')).toHaveCount(364)
  const scroll = card.locator('[data-usage-scroll]')
  expect(
    await scroll.evaluate(
      (element) => element.scrollWidth - element.clientWidth
    )
  ).toBeLessThanOrEqual(1)
  await expect(card.locator('[data-usage-draggable]')).toHaveCount(0)
  await expect(card.locator('[data-usage-drag-hint]')).toHaveCount(0)
  const monthLabels = await card.locator('[data-usage-month]').allTextContents()
  expect(monthLabels).toEqual(expect.arrayContaining(['1月', '2月', '3月']))
  const labelBoxes = await card
    .locator('[data-usage-month]')
    .evaluateAll((labels) =>
      labels.map((label) => label.getBoundingClientRect())
    )
  labelBoxes.slice(1).forEach((box, index) => {
    expect(box.left, 'month labels overlap').toBeGreaterThanOrEqual(
      labelBoxes[index]!.right
    )
  })
  await assertInteractiveCentersVisible(page)

  await page.setViewportSize({ width: 390, height: 844 })
  await periodOption(card, 'month').click()
  await periodOption(card, 'year').click()
  const dragTarget = card.locator('[data-usage-draggable="true"]')
  await expect(dragTarget).toBeVisible()
  await expect(card.locator('[data-usage-drag-hint]')).toContainText(
    '拖动查看更早'
  )
  await expect(card.locator('[data-usage-scroll-fade="start"]')).toHaveCount(1)

  const dragBox = await dragTarget.boundingBox()
  if (!dragBox) throw new Error('year drag area missing')
  const beforeDrag = await dragTarget.evaluate((element) => element.scrollLeft)
  expect(beforeDrag).toBeGreaterThan(0)
  await page.mouse.move(
    dragBox.x + dragBox.width / 2,
    dragBox.y + dragBox.height / 2
  )
  await page.mouse.down()
  await page.mouse.move(
    dragBox.x + dragBox.width / 2 + 100,
    dragBox.y + dragBox.height / 2,
    { steps: 4 }
  )
  await page.mouse.up()
  const afterDrag = await dragTarget.evaluate((element) => element.scrollLeft)
  expect(afterDrag).toBeLessThan(beforeDrag)
  await expect(card.locator('[data-usage-scroll-fade="end"]')).toHaveCount(1)
  await assertNoHorizontalOverflow(page)
})

test('usage heatmap exposes three compact periods', async ({ page }) => {
  const card = page.locator('[data-usage-distribution]')
  const expected = { month: 30, quarter: 91, year: 364 }

  await expect(periodOption(card, 'month')).toHaveAttribute(
    'aria-checked',
    'true'
  )

  for (const [period, count] of Object.entries(expected)) {
    await periodOption(card, period as Period).click()
    await expect(card.locator('[data-usage-date]')).toHaveCount(count)
    await expect(card.locator(`[data-usage-layout="${period}"]`)).toBeVisible()

    if (period === 'month') {
      // Out-of-range calendar slots keep the six-week grid without counting.
      await expect(card.locator('[data-usage-placeholder]')).toHaveCount(12)
      await expect(card.locator('[data-usage-scroll]')).toHaveCount(0)
    } else {
      const scroll = await card
        .locator('[data-usage-scroll]')
        .evaluate((element) => element.scrollWidth - element.clientWidth)
      expect(scroll, `${period} overflow`).toBeLessThanOrEqual(1)
    }

    const side = period !== 'year'
    const analytics = card.locator(
      `[data-usage-analytics="${side ? 'side' : 'bottom'}"]`
    )
    await expect(analytics).toBeVisible()
    await expect(analytics).toContainText('本段最活跃')
    await expect(analytics).toContainText('星期节律')
    if (!side) {
      await expect(
        analytics.locator('[data-usage-segment-compact="true"]')
      ).toHaveCount(2)
      await expect(
        analytics.locator('[data-usage-segment-details]')
      ).toBeHidden()
    }
  }
})

test('month calendar keeps roomy desktop cells and compact mobile cells', async ({
  page,
}) => {
  const card = page.locator('[data-usage-distribution]')
  await periodOption(card, 'month').click()

  const desktopCell = await card
    .locator('.usage-month-cell')
    .first()
    .evaluate((element) => {
      const rect = element.getBoundingClientRect()
      return { width: rect.width, height: rect.height }
    })
  expect(desktopCell).toEqual({ width: 42, height: 24 })

  await page.setViewportSize({ width: 390, height: 844 })
  const mobileCell = await card
    .locator('.usage-month-cell')
    .first()
    .evaluate((element) => {
      const rect = element.getBoundingClientRect()
      return { width: rect.width, height: rect.height }
    })
  expect(mobileCell).toEqual({ width: 32, height: 24 })
  await assertNoHorizontalOverflow(page)
})

// Wide cards split the month stage between a growing calendar and its
// analytics column instead of opening a dead gap before the divider.
for (const width of [1920, 2560, 3840]) {
  test(`month calendar shares the wide stage at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1440 })
    const card = page.locator('[data-usage-distribution]')
    await periodOption(card, 'month').click()
    const layout = await card.evaluate((element) => {
      const rect = (selector: string) =>
        element.querySelector(selector)!.getBoundingClientRect()
      return {
        gap:
          rect('[data-usage-analytics]').left -
          rect('.usage-month-calendar').right,
        stage: rect('[data-usage-stage]').width,
      }
    })
    expect(layout.gap, JSON.stringify(layout)).toBeLessThan(layout.stage * 0.08)
    await expectStableOverviewRow(page)
  })
}

function quarterGeometry(card: Locator) {
  return card.evaluate((element) => {
    const cells = [...element.querySelectorAll('.usage-dense-cell')].map(
      (cell) => cell.getBoundingClientRect()
    )
    const grid = element.querySelector('.usage-dense-content')
    const analytics = element.querySelector('[data-usage-analytics]')
    const content = element.querySelector('[data-usage-content]')
    if (!cells[0] || !grid || !analytics || !content) {
      throw new Error('quarter layout missing')
    }
    const sorted = (values: number[]) =>
      [...new Set(values.map((value) => Math.round(value * 10) / 10))].sort(
        (left, right) => left - right
      )
    const lefts = sorted(cells.map((cell) => cell.left))
    const tops = sorted(cells.map((cell) => cell.top))
    const first = cells[0]
    const gridRect = grid.getBoundingClientRect()
    const analyticsRect = analytics.getBoundingClientRect()
    return {
      cellWidth: first.width,
      cellHeight: first.height,
      columnGap: lefts[1]! - lefts[0]! - first.width,
      rowGap: tops[1]! - tops[0]! - first.height,
      gridToAnalytics: analyticsRect.left - gridRect.right,
      analyticsWidth: analyticsRect.width,
      analyticsRightInset:
        content.getBoundingClientRect().right - analyticsRect.right,
    }
  })
}

test('quarter heatmap fills the width beside right-aligned analytics', async ({
  page,
}) => {
  const card = page.locator('[data-usage-distribution]')
  await periodOption(card, 'quarter').click()

  const desktop = await quarterGeometry(card)
  expect(desktop.cellHeight).toBe(16)
  expect(desktop.cellWidth).toBeGreaterThan(desktop.cellHeight)
  expect(desktop.columnGap).toBeCloseTo(5, 0)
  expect(desktop.rowGap).toBeCloseTo(4, 0)
  // Below the cell cap the weeks reach the stage gap before the divider.
  expect(desktop.gridToAnalytics).toBeLessThanOrEqual(13)
  expect(Math.abs(desktop.analyticsRightInset)).toBeLessThan(1)

  // Past the cap the cells stop at twice their height and the analytics
  // column still ends at the card edge instead of leaving a gap after it.
  await page.setViewportSize({ width: 1279, height: 900 })
  const capped = await quarterGeometry(card)
  expect(capped.cellWidth).toBe(32)
  expect(capped.analyticsWidth).toBeLessThanOrEqual(352)
  expect(Math.abs(capped.analyticsRightInset)).toBeLessThan(1)

  // Month shares the right-aligned analytics column, so switching periods
  // keeps its right edge on the card edge.
  for (const width of [1440, 1920]) {
    await page.setViewportSize({ width, height: 900 })
    await periodOption(card, 'month').click()
    const monthRightInset = await card.evaluate((element) => {
      const analytics = element.querySelector('[data-usage-analytics]')!
      const content = element.querySelector('[data-usage-content]')!
      return (
        content.getBoundingClientRect().right -
        analytics.getBoundingClientRect().right
      )
    })
    expect(Math.abs(monthRightInset), `month at ${width}`).toBeLessThan(1)
    await periodOption(card, 'quarter').click()
  }

  await page.setViewportSize({ width: 390, height: 844 })
  const mobileCell = await card
    .locator('.usage-dense-cell')
    .first()
    .evaluate((element) => {
      const rect = element.getBoundingClientRect()
      return { width: rect.width, height: rect.height }
    })
  expect(mobileCell.height).toBe(24)
  expect(mobileCell.width).toBeGreaterThanOrEqual(mobileCell.height)
  expect(
    await card
      .locator('[data-usage-scroll]')
      .evaluate((element) => element.scrollWidth - element.clientWidth)
  ).toBeGreaterThan(0)
  await assertNoHorizontalOverflow(page)
})

async function expectStableOverviewRow(page: Page) {
  const distribution = page.locator('[data-usage-distribution]')
  const heights: number[] = []
  const measurements: Array<Record<string, number | string>> = []

  for (const period of PERIODS) {
    await periodOption(distribution, period).click()
    await expect(
      distribution.locator(`[data-usage-layout="${period}"]`)
    ).toBeVisible()

    const row = await page.evaluate(() => {
      const distributionCard = document.querySelector(
        '[data-usage-distribution]'
      )
      const balanceCard = document.querySelector('[data-balance-card]')
      if (!distributionCard || !balanceCard) {
        throw new Error('overview row missing')
      }
      return {
        distribution: distributionCard.getBoundingClientRect().height,
        balance: balanceCard.getBoundingClientRect().height,
        content:
          distributionCard
            .querySelector('[data-usage-content]')
            ?.getBoundingClientRect().height ?? 0,
        stage:
          distributionCard
            .querySelector('[data-usage-stage]')
            ?.getBoundingClientRect().height ?? 0,
        heatmap:
          distributionCard
            .querySelector('[data-usage-layout]')
            ?.getBoundingClientRect().height ?? 0,
        analytics:
          distributionCard
            .querySelector('[data-usage-analytics]')
            ?.getBoundingClientRect().height ?? 0,
        footer:
          distributionCard
            .querySelector('[data-usage-footer]')
            ?.getBoundingClientRect().height ?? 0,
      }
    })

    expect(
      Math.abs(row.distribution - row.balance),
      `${period} row alignment`
    ).toBeLessThan(1)
    heights.push(row.distribution)
    measurements.push({ period, ...row })
  }

  expect(
    Math.max(...heights) - Math.min(...heights),
    `period measurements: ${JSON.stringify(measurements)}`
  ).toBeLessThan(1)
}

for (const width of [1280, 1440, 1680]) {
  test(`usage period switching keeps the overview row stable at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 })
    await expectStableOverviewRow(page)
  })
}

test.describe('empty account by day', () => {
  test.use({ dashboardTheme: 'light', emptyAccount: true })

  // Without the balance usage meter the distribution card sets the row height,
  // so any period-dependent content height would move the whole row.
  test('usage period switching keeps the empty overview row stable', async ({
    page,
  }) => {
    const distribution = page.locator('[data-usage-distribution]')
    await expect(distribution.locator('[data-usage-footer]')).toContainText(
      '活跃 0 天'
    )
    await expect(page.locator('[data-balance-usage-meter]')).toHaveCount(0)
    await expectStableOverviewRow(page)
  })
})

test('balance summary distributes extra row height without a dead gap', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1840, height: 900 })
  const distribution = page.locator('[data-usage-distribution]')
  const balance = page.locator('[data-balance-card]')

  for (const period of PERIODS) {
    await periodOption(distribution, period).click()
    await expect(
      distribution.locator(`[data-usage-layout="${period}"]`)
    ).toBeVisible()

    const gaps = await balance.evaluate((element) => {
      const meter = element.querySelector('[data-balance-usage-meter]')
      const tile = element.querySelector('[data-balance-spend-tile]')
      const action = element.querySelector('[data-balance-actions] button')
      if (!meter || !tile || !action) throw new Error('balance layout missing')

      const meterRect = meter.getBoundingClientRect()
      const tileRect = tile.getBoundingClientRect()
      const actionRect = action.getBoundingClientRect()
      return {
        above: tileRect.top - meterRect.bottom,
        below: actionRect.top - tileRect.bottom,
      }
    })

    expect(
      Math.max(gaps.above, gaps.below),
      `${period} largest gap`
    ).toBeLessThan(80)
    expect(
      Math.abs(gaps.above - gaps.below),
      `${period} gap balance`
    ).toBeLessThan(20)

    const footerBottomGap = await distribution.evaluate((element) => {
      const footer = element.querySelector('[data-usage-footer]')
      if (!footer) throw new Error('usage footer missing')
      return (
        element.getBoundingClientRect().bottom -
        footer.getBoundingClientRect().bottom
      )
    })
    expect(footerBottomGap, `${period} footer bottom gap`).toBeLessThan(20)
  }
})

test('usage distribution stays compact and contained on mobile', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const card = page.locator('[data-usage-distribution]')
  const heights: number[] = []

  for (const period of PERIODS) {
    await periodOption(card, period).click()
    await expect(card.locator(`[data-usage-layout="${period}"]`)).toBeVisible()
    const metrics = await card.evaluate((element) => {
      const rect = element.getBoundingClientRect()
      const scroll = element.querySelector('[data-usage-scroll]')
      return {
        height: rect.height,
        overflow:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
        heatmapOverflow: scroll ? scroll.scrollWidth - scroll.clientWidth : 0,
      }
    })
    // The seven touch rows and scroll hint reserve 60px beyond the calendar.
    expect(metrics.height, `${period} card height`).toBeLessThanOrEqual(560)
    expect(metrics.overflow).toBe(0)
    if (period !== 'month') expect(metrics.heatmapOverflow).toBeGreaterThan(0)
    heights.push(metrics.height)
  }

  // Stacked cards below would jump if the period changed this card's height.
  expect(Math.max(...heights) - Math.min(...heights)).toBeLessThan(1)
  const touchTarget = await periodOption(card, 'year').boundingBox()
  expect(touchTarget?.height).toBeGreaterThanOrEqual(44)
})

// A tablet-wide card puts the analytics beside the month calendar instead of
// leaving half the stage empty, and every period keeps one card height.
for (const width of [640, 768, 1024]) {
  test(`usage analytics sit beside the month calendar at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 })
    const card = page.locator('[data-usage-distribution]')
    const heights: number[] = []

    for (const period of PERIODS) {
      await periodOption(card, period).click()
      await expect(
        card.locator(`[data-usage-layout="${period}"]`)
      ).toBeVisible()
      const layout = await card.evaluate((element) => {
        const rect = (selector: string) =>
          element.querySelector(selector)!.getBoundingClientRect()
        const scroll = element.querySelector('[data-usage-scroll]')
        return {
          height: element.getBoundingClientRect().height,
          heatmapRight: rect('[data-usage-layout]').right,
          analyticsLeft: rect('[data-usage-analytics]').left,
          heatmapOverflow: scroll ? scroll.scrollWidth - scroll.clientWidth : 0,
        }
      })
      if (period === 'month') {
        expect(layout.analyticsLeft).toBeGreaterThan(layout.heatmapRight)
      }
      expect(layout.heatmapOverflow, `${period} pans`).toBeLessThanOrEqual(1)
      heights.push(layout.height)
    }

    expect(Math.max(...heights) - Math.min(...heights)).toBeLessThan(1)
    await assertNoHorizontalOverflow(page)
  })
}

test('weekday labels stay under their rhythm slots when weekends are quiet', async ({
  page,
}) => {
  const quietWeekends = Array.from({ length: 366 }, (_, index) => {
    const date = visualDateKey(365 - index)
    const day = new Date(`${date}T00:00:00`).getDay()
    const weekend = day === 0 || day === 6
    return {
      date,
      requests: weekend ? 0 : 900 + ((index * 37) % 400),
      consume: weekend ? 0 : 120_000 + index,
      tokens: weekend ? 0 : 400_000 + index,
    }
  })
  await page.route('**/api/data/self/summary?*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { ...nativeUsageSummary(NOW), distribution: quietWeekends },
      },
    })
  )
  await page.reload({ waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  const card = page.locator('[data-usage-distribution]')
  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    const drift = await card
      .locator('[data-usage-segment-bar="rhythm"]')
      .evaluate((bar) => {
        const center = (element: Element) => {
          const rect = element.getBoundingClientRect()
          return rect.left + rect.width / 2
        }
        const slots = [...bar.querySelectorAll('[data-usage-segment]')]
        const labels = [
          ...bar.querySelectorAll('[data-usage-segment-labels] > span'),
        ]
        if (slots.length !== 7 || labels.length !== 7) {
          throw new Error(`${slots.length} slots, ${labels.length} labels`)
        }
        return Math.max(
          ...slots.map((slot, index) =>
            Math.abs(center(slot) - center(labels[index]!))
          )
        )
      })
    expect(drift, `label drift at ${width}px`).toBeLessThanOrEqual(2)
  }
})

test('ranked day segments keep 24px targets when one day dominates', async ({
  page,
}) => {
  // Three active days: one dominant, two well under 1% of the total.
  const lopsided = Array.from({ length: 30 }, (_, index) => {
    const value = [1_000_000, 4_000, 1_000][index] ?? 0
    return {
      date: visualDateKey(index),
      requests: value,
      consume: value,
      tokens: value,
    }
  })
  await page.route('**/api/data/self/summary?*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { ...nativeUsageSummary(NOW), distribution: lopsided },
      },
    })
  )
  await page.reload({ waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  const card = page.locator('[data-usage-distribution]')
  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    const boxes = await card
      .locator('[data-usage-segment-bar="ranked"] [data-usage-segment]')
      .evaluateAll((segments) =>
        segments.map((segment) => segment.getBoundingClientRect())
      )
    expect(boxes, `segments at ${width}px`).toHaveLength(3)
    const bar = await card
      .locator('[data-usage-segment-bar="ranked"] [role="group"]')
      .evaluate((group) => group.getBoundingClientRect())
    boxes.forEach((box, index) => {
      expect(box.width, `segment ${index} width`).toBeGreaterThanOrEqual(24)
      expect(box.height, `segment ${index} height`).toBeGreaterThanOrEqual(24)
      if (index > 0) {
        expect(box.left, 'segments overlap').toBeGreaterThanOrEqual(
          boxes[index - 1]!.right - 0.5
        )
      }
    })
    expect(boxes.at(-1)!.right).toBeLessThanOrEqual(bar.right + 0.5)
    // The dominant day still reads as the larger share.
    expect(boxes[0]!.width).toBeGreaterThan(boxes[1]!.width * 3)
  }
})

for (const theme of ['light', 'dark'] satisfies VisualTheme[]) {
  test.describe(`heatmap ramp by ${theme === 'light' ? 'day' : 'night'}`, () => {
    test.use({ dashboardTheme: theme })

    // Day numbers stay readable on every level, and neighbouring levels of
    // the ramp remain distinguishable for each metric.
    test('day numbers and ramp levels stay legible for each metric', async ({
      page,
    }) => {
      const card = page.locator('[data-usage-distribution]')
      for (const metric of ['调用', '消费', 'Token']) {
        await metricOption(card, metric).click()
        expect(
          await minTextContrast(card.locator('.usage-month-cell')),
          `${metric} day numbers`
        ).toBeGreaterThanOrEqual(4.5)

        const levels = await measureLuminance(
          card.locator('[data-usage-footer] span.border')
        )
        expect(levels).toHaveLength(6)
        for (let level = 1; level < levels.length; level += 1) {
          expect(
            contrastRatio(
              levels[level]!.background,
              levels[level - 1]!.background
            ),
            `${metric} level ${level - 1} to ${level}`
          ).toBeGreaterThanOrEqual(1.15)
        }
      }
    })
  })
}

test('usage heatmap exposes one roving cell and moves by date', async ({
  page,
}) => {
  const card = page.locator('[data-usage-distribution]')
  const initial = card.locator('[data-usage-date][tabindex="0"]')
  await expect(initial).toHaveCount(1)
  const start = await initial.getAttribute('data-usage-date')
  await initial.focus()
  await initial.press('ArrowLeft')

  const focused = page.locator(':focus')
  await expect(focused).toHaveAttribute('data-usage-date', /\d{4}-\d{2}-\d{2}/)
  expect(await focused.getAttribute('data-usage-date')).not.toBe(start)
})

for (const theme of ['light', 'dark'] satisfies VisualTheme[]) {
  test.describe(`touch heatmap ${theme}`, () => {
    test.use({ dashboardTheme: theme, hasTouch: true })

    for (const width of [320, 390, 768]) {
      test(`dense periods support touch and keyboard at ${width}px`, async ({
        page,
        context,
      }, testInfo) => {
        test.setTimeout(75_000)
        await page.setViewportSize({ width, height: 900 })
        const card = page.locator('[data-usage-distribution]')
        const heights: number[] = []
        for (const period of PERIODS) {
          await periodOption(card, period).tap()
          const layout = await card.evaluate((element) => {
            const heatmap = element.querySelector('[data-usage-layout]')!
            const analytics = element.querySelector('[data-usage-analytics]')!
            return {
              height: element.getBoundingClientRect().height,
              heatmapBottom: heatmap.getBoundingClientRect().bottom,
              analyticsTop: analytics.getBoundingClientRect().top,
              targets: Array.from(
                heatmap.querySelectorAll('[data-usage-date]')
              ).map((cell) => {
                const rect = cell.getBoundingClientRect()
                return { width: rect.width, height: rect.height }
              }),
            }
          })
          heights.push(layout.height)
          for (const target of layout.targets) {
            expect(target.width).toBeGreaterThanOrEqual(24)
            expect(target.height).toBeGreaterThanOrEqual(24)
          }
          if (width < 640 || period === 'year') {
            expect(layout.analyticsTop).toBeGreaterThan(layout.heatmapBottom)
          }
          if (period === 'month') continue

          const scroll = card.locator('[data-usage-scroll]')
          const overflow = await scroll.evaluate(
            (element) => element.scrollWidth - element.clientWidth
          )
          if (width < 640 || period === 'year') {
            expect(overflow).toBeGreaterThan(0)
          }
          if (overflow > 1) {
            await expect(card.locator('[data-usage-drag-hint]')).toBeVisible()
          }
          const latest = card.locator('[data-usage-date]').last()
          await latest.tap()
          await expect(card.getByRole('tooltip')).toHaveText(
            (await latest.getAttribute('aria-label'))!
          )
          await latest.press('ArrowUp')
          await expect(page.locator(':focus')).toHaveAttribute(
            'data-usage-date',
            visualDateKey(1)
          )
          await page.keyboard.press('ArrowLeft')
          await expect(page.locator(':focus')).toHaveAttribute(
            'data-usage-date',
            visualDateKey(8)
          )
          await page.keyboard.press('Home')
          await expect(card.locator('[data-usage-date]').first()).toBeFocused()
          await expect
            .poll(() => scroll.evaluate((element) => element.scrollLeft))
            .toBeLessThanOrEqual(4)
          await page.keyboard.press('End')
          await expect(latest).toBeFocused()

          if (overflow <= 1) continue

          await scroll.scrollIntoViewIfNeeded()
          const box = await scroll.boundingBox()
          if (!box) throw new Error('heatmap touch area missing')
          const before = await scroll.evaluate((element) => element.scrollLeft)
          expect(before).toBeGreaterThan(0)
          const client = await context.newCDPSession(page)
          const x = box.x + box.width / 3
          const y = box.y + box.height / 2
          await client.send('Input.dispatchTouchEvent', {
            type: 'touchStart',
            touchPoints: [{ x, y }],
          })
          for (let step = 1; step <= 6; step++) {
            await client.send('Input.dispatchTouchEvent', {
              type: 'touchMove',
              touchPoints: [{ x: x + step * 12, y }],
            })
          }
          await client.send('Input.dispatchTouchEvent', {
            type: 'touchEnd',
            touchPoints: [],
          })
          await client.detach()
          await expect
            .poll(() => scroll.evaluate((element) => element.scrollLeft))
            .toBeLessThan(before)
          await expect(
            card.locator('[data-usage-scroll-fade="end"]')
          ).toBeVisible()
          await assertNoHorizontalOverflow(page)
          await card.screenshot({
            path: testInfo.outputPath(`${period}.png`),
          })
        }
        expect(
          Math.max(...heights) - Math.min(...heights),
          JSON.stringify(heights)
        ).toBeLessThan(1)
      })
    }
  })
}

test('statistics trend supports the single-series modes', async ({ page }) => {
  await page.getByRole('tab', { name: '统计数据', exact: true }).click()
  await waitForStablePage(page)
  const trend = page.locator('[data-stats-dual-trend]')
  await expect(trend).toBeVisible()
  await trend.locator('[data-trend-mode="consume"]').click()
  await expect(trend.locator('[data-trend-mode="consume"]')).toHaveAttribute(
    'aria-pressed',
    'true'
  )
  await expect(trend.locator('[role="img"]')).toBeVisible()
  expect(
    await page
      .locator('[data-stats-model-scroll]')
      .evaluate((element) => element.scrollWidth - element.clientWidth)
  ).toBe(0)
  await assertNoHorizontalOverflow(page)
  await assertInteractiveCentersVisible(page)
})

test('RPM and discount placeholders stay unconnected instead of invented', async ({
  page,
}) => {
  const rpm = page.locator('[data-overview-kpi] [data-kpi-cell="rpm"]')
  await expect(rpm).toContainText('请求限速')
  await expect(rpm).toContainText('未接入')
  await expect(rpm.locator('.display-number')).toHaveText('--')
  const discount = page.locator('[data-discount-card]')
  await expect(discount).toHaveCount(1)
  await expect(discount).toContainText('未接入')
  await assertNoHorizontalOverflow(page)
})

test('KPI dividers are solid at night and dashed ledger rules by day', async ({
  page,
}) => {
  const cells = page.locator('[data-overview-kpi] > div > *')
  await expect(cells).toHaveCount(5)
  const dividerStyles = () =>
    cells.evaluateAll((all) =>
      all.map((cell) => getComputedStyle(cell).borderLeftStyle)
    )
  // Set the theme class the way useTheme does; the dividers key off it only,
  // so the result does not depend on the theme this run started in.
  const showTheme = (theme: VisualTheme) =>
    page.evaluate((next) => {
      document.documentElement.classList.remove('dark', 'light')
      document.documentElement.classList.add(next)
    }, theme)

  await showTheme('dark')
  expect(new Set(await dividerStyles())).toEqual(new Set(['solid']))
  await showTheme('light')
  expect(new Set(await dividerStyles())).toEqual(new Set(['dashed']))
})

test('all dashboard panels remain contained at compact width', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })

  const overviewStrip = page.locator('[data-overview-kpi]')
  const overviewLastCell = overviewStrip.locator(':scope > div > :last-child')
  await expect(overviewLastCell).toBeVisible()
  const lastCellLayout = await overviewLastCell.evaluate((element) => {
    // Cell layout: wrapper > [label + figure block, mini-visual slot].
    const [figure, visual] = [...(element.firstElementChild?.children ?? [])]
    if (!figure || !visual) throw new Error('KPI cell parts missing')
    const figureRect = figure.getBoundingClientRect()
    const visualRect = visual.getBoundingClientRect()
    return {
      width: element.getBoundingClientRect().width,
      visualBesideFigure: visualRect.left >= figureRect.right - 0.5,
      sharedRow:
        visualRect.top < figureRect.bottom &&
        visualRect.bottom > figureRect.top,
    }
  })
  // The fifth cell takes the full final row and puts its sparkline beside the
  // figure rather than under it.
  expect(lastCellLayout.width).toBeGreaterThan(300)
  expect(lastCellLayout.visualBesideFigure).toBe(true)
  expect(lastCellLayout.sharedRow).toBe(true)

  await page.getByRole('tab', { name: '统计数据', exact: true }).click()
  await waitForStablePage(page)
  const modelScroll = page.locator('[data-stats-model-scroll]')
  await expect(modelScroll).toBeVisible()
  expect(
    await modelScroll.evaluate(
      (element) => element.scrollWidth - element.clientWidth
    )
  ).toBeGreaterThan(0)

  const catalog = createModelMarketFixture()
  for (const id of [14, 15, 16, 17]) {
    catalog.channels.push({
      ...catalog.channels[0]!,
      id,
      name: `Native channel ${id}`,
      eligible: id !== 17,
      status: id === 17 ? 2 : 1,
      filter_reason: id === 17 ? 'channel_unavailable' : '',
    })
  }
  await page.route('**/api/user/model-catalog', (route) =>
    route.fulfill({ json: { success: true, data: catalog } })
  )
  await page.getByRole('tab', { name: '自动路由', exact: true }).click()
  await waitForStablePage(page)
  const routeGroups = page.locator('[data-route-group]')
  await expect(routeGroups).toHaveCount(1)
  const firstGroup = routeGroups.first()
  await expect(firstGroup.locator('[data-route-lab-fallback]')).toBeVisible()
  await expect(firstGroup.locator('button[aria-expanded]')).toHaveAttribute(
    'aria-expanded',
    'false'
  )
  await firstGroup.locator('button[aria-expanded]').first().click()
  // Seven catalog channels retain their order; the collapsed group shows three.
  const visibleChannels = firstGroup.locator('[data-route-channel]:visible')
  await expect(visibleChannels).toHaveCount(3)
  await expect(visibleChannels.first()).toContainText('雾桥中转')
  await expect(firstGroup.locator('[data-route-channel] img')).toHaveCount(0)

  const moreToggle = firstGroup.locator('[data-route-more-toggle]')
  await expect(moreToggle).toHaveText('展开余下 4 条渠道')
  await expect(moreToggle).toHaveAttribute('aria-expanded', 'false')
  const controlsId = await moreToggle.getAttribute('aria-controls')
  expect(controlsId).toBeTruthy()
  // Mobile touch target floor.
  expect(
    await moreToggle.evaluate((el) => el.getBoundingClientRect().height)
  ).toBeGreaterThanOrEqual(44)

  await moreToggle.click()
  await expect(moreToggle).toHaveAttribute('aria-expanded', 'true')
  await expect(visibleChannels).toHaveCount(7)
  await expect(
    page.locator(`[id="${controlsId}"] [data-route-channel]`)
  ).toHaveCount(4)
  // Compact rows retain source order and expose exclusions through their icon.
  await expect(visibleChannels.last()).toContainText('Native channel 17')
  await expect(
    visibleChannels.last().locator('svg.lucide-circle-off')
  ).toBeVisible()

  await moreToggle.click()
  await expect(visibleChannels).toHaveCount(3)

  await assertNoHorizontalOverflow(page)
  await assertInteractiveCentersVisible(page)
})
