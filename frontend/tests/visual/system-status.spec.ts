import { expect, type Locator, type Page, test } from '@playwright/test'
import {
  resourceFixture,
  systemStatusFixture,
} from '../../src/components/console/dashboard/__tests__/systemStatusFixture'

import {
  assertNoHorizontalOverflow,
  contrastRatio,
  minTextContrast,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

interface ResourceReadings {
  cpuPercent: number
  memoryUsedGib: number
  memoryTotalGib: number
  diskUsedGib: number
  diskTotalGib: number
}

const DEFAULT_READINGS: ResourceReadings = {
  cpuPercent: 4.6,
  memoryUsedGib: 6.8,
  memoryTotalGib: 16,
  diskUsedGib: 203,
  diskTotalGib: 512,
}

function systemSnapshot(readings: ResourceReadings = DEFAULT_READINGS) {
  const data = systemStatusFixture()
  data.cpu = resourceFixture({ percent: readings.cpuPercent })
  data.memory = resourceFixture({
    percent: (readings.memoryUsedGib / readings.memoryTotalGib) * 100,
    used_bytes: readings.memoryUsedGib * 1024 ** 3,
    total_bytes: readings.memoryTotalGib * 1024 ** 3,
  })
  data.disk = resourceFixture({
    percent: (readings.diskUsedGib / readings.diskTotalGib) * 100,
    scope: 'filesystem',
    used_bytes: readings.diskUsedGib * 1024 ** 3,
    total_bytes: readings.diskTotalGib * 1024 ** 3,
  })
  data.traffic.request_mbps = 0.45
  data.traffic.response_mbps = 0.00042
  return data
}

async function configureSystemPage(
  page: Page,
  options: Parameters<typeof configureStablePage>[1]
) {
  await configureStablePage(page, options)
  await page.route('**/api/user/system-status', (route) =>
    route.fulfill({ json: { success: true, data: systemSnapshot() } })
  )
}

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

/** The title, success-rate caption and value share the compact card header. */
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
  await expect(card.locator('[data-system-health]')).toHaveAttribute(
    'data-system-health',
    'ok'
  )
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
    await configureSystemPage(page, { theme, authenticated: true })
    await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const card = page.locator('[data-system-status-card]')
    await expect(card).toBeVisible()
    await expect(card).toContainText(/4\.6\s*%/)
    await expect(card).toContainText('应用流量')
    await expect(card).toContainText(/↑\s*450 Kbps/)
    await expect(card).toContainText(/↓\s*420 bps/)
    await expectMetricValuesToFit(card)
    await expectStableTileHeights(card)
    await expectUnifiedHeadlineNumbers(card)
    await expectHeaderFits(card)
    await assertNoHorizontalOverflow(page)

    await page.setViewportSize({ width: 390, height: 844 })
    await card.scrollIntoViewIfNeeded()
    await expect(card).toContainText(/4\.6\s*%/)
    await expect(card).toContainText(/↑\s*450 Kbps/)
    await expect(card).toContainText(/↓\s*420 bps/)
    await expectMetricValuesToFit(card)
    await expectStableTileHeights(card)
    await expectUnifiedHeadlineNumbers(card)
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
    await configureSystemPage(page, {
      theme: 'light',
      authenticated: true,
      locale: 'en',
    })
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const card = page.locator('[data-system-status-card]')
    await expect(card).toContainText('System status')
    await expectHeaderFits(card)
    await assertNoHorizontalOverflow(page)
  })
}

// The fixture's 16 GiB / 512 GiB host hides overflow; these are a real server's
// readings and the widest figures each tile can show.
const SERVER_READINGS = {
  realistic: {
    cpuPercent: 45.6,
    memoryUsedGib: 62.8,
    memoryTotalGib: 125.7,
    diskUsedGib: 1843.2,
    diskTotalGib: 3726,
  },
  widest: {
    cpuPercent: 100,
    memoryUsedGib: 87.4,
    memoryTotalGib: 94.2,
    diskUsedGib: 401.5,
    diskTotalGib: 476.9,
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
      return {
        metric: tile.dataset.metric,
        overflow: Math.max(...parts.map((part) => rect(part).right - right)),
        parts: parts
          .filter((part) => rect(part).right - right > 0.5)
          .map((part) => ({
            text: part.textContent?.trim(),
            tag: part.tagName,
            overflow: rect(part).right - right,
          })),
      }
    })
    const cpu = node.querySelector('[data-cpu-gauge]')!
    const disk = node.querySelector('[data-disk-gauge]')!
    const diskText = [...disk.firstElementChild!.querySelectorAll('span')]
    return {
      overflow: Math.max(...overflow.map((tile) => tile.overflow)),
      viewport: innerWidth,
      cardWidth: rect(node).width,
      tiles: overflow,
      cpuGap:
        rect(cpu.lastElementChild!).left - rect(cpu.firstElementChild!).right,
      diskGap:
        rect(disk.lastElementChild!).left -
        Math.max(...diskText.map((part) => rect(part).right)),
    }
  })
  expect(layout.overflow, JSON.stringify(layout)).toBeLessThanOrEqual(0.5)
  expect(layout.cpuGap).toBeGreaterThanOrEqual(2)
  expect(layout.diskGap).toBeGreaterThanOrEqual(2)
}

for (const theme of ['light', 'dark'] satisfies VisualTheme[]) {
  for (const [name, readings] of Object.entries(SERVER_READINGS)) {
    test(`${name} server readings fit their tiles and stay legible in ${theme} theme`, async ({
      page,
    }, testInfo) => {
      await configureSystemPage(page, { theme, authenticated: true })
      await page.route('**/api/user/system-status', (route) =>
        route.fulfill({
          json: { success: true, data: systemSnapshot(readings) },
        })
      )
      await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)
      const card = page.locator('[data-system-status-card]')
      if (name === 'widest') {
        await expect(card.locator('[data-metric="memory"]')).toContainText(
          /87\.4\s*\/\s*94\.2\s*GiB/
        )
      }
      if (name === 'realistic') {
        await expect(card.locator('[data-metric="disk"]')).toContainText('TiB')
      }

      // 1280-1366 is the three-column grid, where the card is at its narrowest.
      for (const width of [320, 390, 1280, 1366, 1440, 1920]) {
        await page.setViewportSize({ width, height: 900 })
        await expectReadingsInsideTiles(card)
        if (name === 'widest') {
          await card.screenshot({
            path: testInfo.outputPath(`system-card-${theme}-${width}.png`),
          })
        }
      }
      const figures = card.locator(
        '[data-metric-value], [data-percent-badge], [data-bandwidth-direction], [data-system-health] > span:nth-child(2)'
      )
      expect(await minTextContrast(figures)).toBeGreaterThanOrEqual(4.5)
    })
  }
}

/** Two readings that between them put CPU, memory and disk in all five tiers. */
const TIER_READINGS = [
  { cpuPercent: 18, memoryUsedGib: 45, diskUsedGib: 700 },
  { cpuPercent: 82, memoryUsedGib: 95, diskUsedGib: 200 },
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
    await configureSystemPage(page, { theme, authenticated: true })
    await page.route('**/api/user/system-status', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: systemSnapshot({
            ...DEFAULT_READINGS,
            memoryTotalGib: 100,
            diskTotalGib: 1000,
            ...reading,
          }),
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

// Production may report the commit hash as its version.
for (const locale of ['zh-CN', 'en']) {
  test(`a commit-hash version fits the native resource card (${locale})`, async ({
    page,
  }) => {
    await configureSystemPage(page, {
      theme: 'dark',
      authenticated: true,
      locale,
    })
    await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const card = page.locator('[data-system-status-card]')
    const version = card.locator('[data-system-version]')
    await version.evaluate((node) => {
      node.textContent = 'c7b249bb216d82d13e7cb33946172c9644896d4a'
    })

    for (const width of [320, 390, 1024, 1280, 1366, 2560]) {
      await page.setViewportSize({ width, height: 900 })
      const footer = await version.evaluate((node) => {
        const label = node.previousElementSibling!.getBoundingClientRect()
        const value = node.getBoundingClientRect()
        const row = node.parentElement!.getBoundingClientRect()
        return {
          labelHeight: label.height,
          valueHeight: value.height,
          valueInside: value.left >= label.right && value.right <= row.right,
        }
      })
      const context = `${width}px: ${JSON.stringify(footer)}`
      // One 11px line each: the label never breaks, the hash truncates.
      expect(footer.labelHeight, context).toBeLessThan(20)
      expect(footer.valueHeight, context).toBeLessThan(20)
      expect(footer.valueInside, context).toBe(true)
      await expectHeaderFits(card)
    }
    await assertNoHorizontalOverflow(page)
  })
}
