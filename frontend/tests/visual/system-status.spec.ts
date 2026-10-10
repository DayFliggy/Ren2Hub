import { expect, type Locator, test } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  contrastRatio,
  minTextContrast,
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

// The fixture's 16 GB / 512 GB host hides overflow; these are a real server's
// readings and the widest figures each tile can show.
const SERVER_READINGS = {
  realistic: {
    cpu_percent: 45.6,
    memory_used_gb: 62.8,
    memory_total_gb: 125.7,
    disk_used_gb: 1843.2,
    disk_total_gb: 3726,
  },
  widest: {
    cpu_percent: 100,
    memory_used_gb: 87.4,
    memory_total_gb: 94.2,
    disk_used_gb: 401.5,
    disk_total_gb: 476.9,
  },
}

/** Every figure stays inside its tile and clear of the CPU gauge and disk ring. */
async function expectReadingsInsideTiles(card: Locator): Promise<void> {
  const layout = await card.evaluate((node) => {
    const rect = (element: Element) => element.getBoundingClientRect()
    const tiles = [
      ...node.querySelectorAll<HTMLElement>('[data-system-status-tile]'),
    ]
    const overflow = tiles.map((tile) => {
      const right =
        rect(tile).right - parseFloat(getComputedStyle(tile).paddingRight)
      const parts = [...tile.querySelectorAll('span, p, svg')].filter(
        (part) => !part.closest('[data-load-readout]') && rect(part).width > 0
      )
      return Math.max(...parts.map((part) => rect(part).right - right))
    })
    const cpu = node.querySelector('[data-cpu-gauge]')!
    const disk = node.querySelector('[data-disk-gauge]')!
    const diskText = [...disk.firstElementChild!.querySelectorAll('span')]
    return {
      overflow: Math.max(...overflow),
      cpuGap:
        rect(cpu.lastElementChild!).left - rect(cpu.firstElementChild!).right,
      diskGap:
        rect(disk.lastElementChild!).left -
        Math.max(...diskText.map((part) => rect(part).right)),
    }
  })
  expect(layout.overflow).toBeLessThanOrEqual(0.5)
  expect(layout.cpuGap).toBeGreaterThanOrEqual(2)
  expect(layout.diskGap).toBeGreaterThanOrEqual(2)
}

for (const theme of ['light', 'dark'] satisfies VisualTheme[]) {
  for (const [name, readings] of Object.entries(SERVER_READINGS)) {
    test(`${name} server readings fit their tiles and stay legible in ${theme} theme`, async ({
      page,
    }) => {
      await configureStablePage(page, { theme, authenticated: true })
      await page.route(
        /\/api\/next\/dashboard\/system-status(?:\?.*)?$/,
        (route) =>
          route.fulfill({
            json: {
              success: true,
              message: '',
              data: {
                cpu_cores: 16,
                load_average: { one: 7.2, five: 6.8, fifteen: 6.1 },
                bandwidth_up_mbps: 48.6,
                bandwidth_down_mbps: 312.4,
                api_success_rate: 99.7,
                bandwidth_series: null,
                ...readings,
              },
            },
          })
      )
      await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)
      const card = page.locator('[data-system-status-card]')
      if (name === 'realistic') {
        await expect(card.locator('[data-metric="disk"]')).toContainText('TB')
      }

      // 1280-1366 is the three-column grid, where the card is at its narrowest.
      for (const width of [320, 390, 1280, 1366, 1440, 1920]) {
        await page.setViewportSize({ width, height: 900 })
        await expectReadingsInsideTiles(card)
      }
      const figures = card.locator(
        '[data-metric-value], [data-percent-badge], [data-bandwidth-direction], .success-rate-value, [data-status-reachable] > span:last-child'
      )
      expect(await minTextContrast(figures)).toBeGreaterThanOrEqual(4.5)
    })
  }
}

/** Two readings that between them put CPU, memory and disk in all five tiers. */
const TIER_READINGS = [
  { cpu_percent: 18, memory_used_gb: 45, disk_used_gb: 700 },
  { cpu_percent: 82, memory_used_gb: 95, disk_used_gb: 200 },
]
const LOAD_TIERS = ['idle', 'normal', 'notice', 'warning', 'danger']

interface TierColours {
  tier: string
  tile: number[]
  fill: number[]
  text: number[]
  /** --accent-text, the gold or brown that money figures use on the same page. */
  accentText: number[]
}

/** Each tiered tile's gauge fill, figure and accent colours, composited on its tile. */
async function readTierColours(card: Locator): Promise<TierColours[]> {
  return card
    .locator('[data-load-tier]:not([data-load-tier="unknown"])')
    .evaluateAll((tiles) => {
      const context = document.createElement('canvas').getContext('2d')!
      const paint = (colors: string[]) => {
        context.clearRect(0, 0, 1, 1)
        for (const color of colors) {
          context.fillStyle = color
          context.fillRect(0, 0, 1, 1)
        }
        return [...context.getImageData(0, 0, 1, 1).data.slice(0, 3)]
      }
      const root = getComputedStyle(document.documentElement)
      const page = root.getPropertyValue('--page-background').trim()
      const accentText = root.getPropertyValue('--accent-text').trim()
      return tiles.map((tile) => {
        const layers: string[] = []
        for (let node: Element | null = tile; node; node = node.parentElement) {
          layers.unshift(getComputedStyle(node).backgroundColor)
        }
        const tileRgb = paint([page, ...layers])
        const base = `rgb(${tileRgb.join(',')})`
        const gauge = tile.querySelector(
          '[data-cpu-gauge-active], [data-memory-segments] > span, [data-disk-gauge] circle[stroke-linecap]'
        )!
        const fill = gauge.matches('span')
          ? getComputedStyle(gauge).backgroundColor
          : getComputedStyle(gauge).stroke
        const text = getComputedStyle(
          tile.querySelector('[data-metric-value]')!
        )
        return {
          tier: tile.getAttribute('data-load-tier')!,
          tile: tileRgb,
          fill: paint([base, fill]),
          text: paint([base, text.color]),
          accentText: paint([base, accentText]),
        }
      })
    })
}

function linearChannels(rgb: number[]): [number, number, number] {
  return rgb.map((channel) => {
    const value = channel / 255
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
  }) as [number, number, number]
}

function relativeLuminance(rgb: number[]): number {
  const [r, g, b] = linearChannels(rgb)
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

/** OKLab distance ×100; about 2 is the smallest difference people notice. */
function oklabDistance(first: number[], second: number[]): number {
  const oklab = (rgb: number[]) => {
    const [r, g, b] = linearChannels(rgb)
    const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b)
    const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b)
    const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b)
    return [
      0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
      1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s,
      0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s,
    ]
  }
  const [a, b] = [oklab(first), oklab(second)]
  return Math.hypot(a[0]! - b[0]!, a[1]! - b[1]!, a[2]! - b[2]!) * 100
}

/**
 * Gauges reach 3:1 and figures 4.5:1 in every load tier, and neighbouring tiers
 * stay clearly apart, so attention and warning cannot read as one colour again.
 * No tier's figures may pass for the accent colour of money figures either.
 */
for (const theme of ['light', 'dark'] satisfies VisualTheme[]) {
  test(`load tiers stay legible and distinct in ${theme} theme`, async ({
    page,
  }) => {
    let reading = TIER_READINGS[0]!
    await configureStablePage(page, { theme, authenticated: true })
    await page.route(
      /\/api\/next\/dashboard\/system-status(?:\?.*)?$/,
      (route) =>
        route.fulfill({
          json: {
            success: true,
            message: '',
            data: {
              cpu_cores: 4,
              load_average: { one: 3.4, five: 3.8, fifteen: 0.8 },
              memory_total_gb: 100,
              disk_total_gb: 1000,
              bandwidth_up_mbps: 3.2,
              bandwidth_down_mbps: 12.5,
              api_success_rate: 99.7,
              bandwidth_series: null,
              ...reading,
            },
          },
        })
    )

    const seen = new Map<string, TierColours>()
    for (const next of TIER_READINGS) {
      reading = next
      await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)
      const card = page.locator('[data-system-status-card]')
      await expect(card.locator('[data-load-tier]')).toHaveCount(4)
      const figures = card.locator('[data-metric-value], [data-percent-badge]')
      expect(await minTextContrast(figures)).toBeGreaterThanOrEqual(4.5)
      for (const colours of await readTierColours(card)) {
        const tile = relativeLuminance(colours.tile)
        const fill = relativeLuminance(colours.fill)
        expect(
          contrastRatio(tile, fill),
          `${colours.tier} gauge`
        ).toBeGreaterThanOrEqual(3)
        expect(
          oklabDistance(colours.text, colours.accentText),
          `${colours.tier} figure against --accent-text`
        ).toBeGreaterThan(5.5)
        seen.set(colours.tier, colours)
      }
    }

    expect([...seen.keys()].sort()).toEqual([...LOAD_TIERS].sort())
    for (let index = 1; index < LOAD_TIERS.length; index += 1) {
      const lower = seen.get(LOAD_TIERS[index - 1]!)!
      const higher = seen.get(LOAD_TIERS[index]!)!
      const pair = `${lower.tier} → ${higher.tier}`
      expect(oklabDistance(lower.fill, higher.fill), pair).toBeGreaterThan(6)
      expect(oklabDistance(lower.text, higher.text), pair).toBeGreaterThan(6)
    }
  })
}
