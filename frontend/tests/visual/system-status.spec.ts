import { expect, type Locator, type Page, test } from '@playwright/test'
import type {
  SystemResourceState,
  SystemStatus,
} from '../../src/api/systemStatus'
import {
  resourceFixture,
  systemStatusFixture,
} from '../../src/components/console/dashboard/__tests__/systemStatusFixture'

import {
  assertNoHorizontalOverflow,
  contrastRatio,
  minTextContrast,
  oklabDistance,
  relativeLuminance,
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
  /** Server threshold verdicts; only these raise the warning and danger tiers. */
  states?: Partial<Record<'cpu' | 'memory' | 'disk', SystemResourceState>>
  /** The overall verdict on the pill; the fixture's "ok" when omitted. */
  status?: SystemStatus['status']
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
  const states = readings.states ?? {}
  if (readings.status) data.status = readings.status
  data.cpu = resourceFixture({
    percent: readings.cpuPercent,
    state: states.cpu ?? 'ok',
  })
  data.memory = resourceFixture({
    state: states.memory ?? 'ok',
    percent: (readings.memoryUsedGib / readings.memoryTotalGib) * 100,
    used_bytes: readings.memoryUsedGib * 1024 ** 3,
    total_bytes: readings.memoryTotalGib * 1024 ** 3,
  })
  data.disk = resourceFixture({
    state: states.disk ?? 'ok',
    percent: (readings.diskUsedGib / readings.diskTotalGib) * 100,
    scope: 'filesystem',
    used_bytes: readings.diskUsedGib * 1024 ** 3,
    total_bytes: readings.diskTotalGib * 1024 ** 3,
  })
  // Server side: the widest figure is outbound (↑), the smallest inbound (↓).
  data.traffic.outbound_mbps = 0.45
  data.traffic.inbound_mbps = 0.00042
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

/** The widest English pill beside the widest empty rate. */
function longestHeaderSnapshot() {
  const data = systemStatusFixture()
  data.status = 'critical'
  data.storage_status = 'critical'
  data.success_rate = { ...data.success_rate, state: 'no_requests', rate: null }
  return data
}

/** Pill label and rate stay on one line each and the header keeps its height;
 * only the title may truncate. */
async function readHeaderLines(card: Locator) {
  return card.evaluate((node) => {
    const lines = (element: Element) =>
      Math.round(
        element.getBoundingClientRect().height /
          parseFloat(getComputedStyle(element).lineHeight)
      )
    return {
      pillLines: lines(
        node.querySelector('[data-system-health] > span:nth-child(2)')!
      ),
      valueLines: lines(node.querySelector('.success-rate-value')!),
      height: node.querySelector('header')!.getBoundingClientRect().height,
    }
  })
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
  test(`system status keeps CPU and traffic readable in ${theme} theme`, async ({
    page,
  }) => {
    await configureSystemPage(page, { theme, authenticated: true })
    await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const card = page.locator('[data-system-status-card]')
    await expect(card).toBeVisible()
    await expect(card).toContainText(/4\.6\s*%/)
    await expect(card.locator('[data-metric="bandwidth"] [title]')).toHaveText(
      '流量'
    )
    await expect(card).toContainText(/↑\s*450 Kbps/)
    await expect(card).toContainText(/↓\s*420 bps/)
    await expect(
      card.locator('[data-metric="bandwidth"] [data-metric-scope]')
    ).toHaveText('服务器网卡，出站 450 Kbps，入站 420 bps')
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

const TRAFFIC_WORDS = {
  'zh-CN': {
    title: '流量',
    host_interface: '服务器网卡，出站 450 Kbps，入站 420 bps',
    application_listener: '应用连接，出站 450 Kbps，入站 420 bps',
  },
  en: {
    title: 'Traffic',
    host_interface: 'Server interface, outbound 450 Kbps, inbound 420 bps',
    application_listener: 'App connections, outbound 450 Kbps, inbound 420 bps',
  },
} as const

/** The traffic scope changes only words in the title and screen-reader text:
 * the arrows, the card and its tiles keep their size in either scope. */
for (const locale of ['zh-CN', 'en'] as const) {
  for (const theme of ['light', 'dark'] satisfies VisualTheme[]) {
    test(`both traffic scopes keep the card size in ${locale} ${theme}`, async ({
      page,
    }) => {
      let scope: SystemStatus['traffic']['scope'] = 'host_interface'
      await configureSystemPage(page, { theme, authenticated: true, locale })
      await page.route('**/api/user/system-status', (route) => {
        const data = systemSnapshot()
        data.traffic.scope = scope
        return route.fulfill({ json: { success: true, data } })
      })
      const words = TRAFFIC_WORDS[locale]
      const card = page.locator('[data-system-status-card]')
      const tile = card.locator('[data-metric="bandwidth"]')
      for (const viewport of [
        { width: 1440, height: 900 },
        { width: 390, height: 844 },
      ]) {
        await page.setViewportSize(viewport)
        const sizes: string[] = []
        for (const next of [
          'host_interface',
          'application_listener',
        ] as const) {
          scope = next
          await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
          await waitForStablePage(page)
          await expect(tile.locator('[data-metric-scope]')).toHaveText(
            words[next]
          )
          await expect(tile.locator(`[title="${words[next]}"]`)).toHaveText(
            words.title
          )
          await expect(
            tile.locator('[data-bandwidth-direction="up"]')
          ).toHaveText(/↑\s*450 Kbps/)
          await expect(
            tile.locator('[data-bandwidth-direction="down"]')
          ).toHaveText(/↓\s*420 bps/)
          await expectMetricValuesToFit(card)
          await expectStableTileHeights(card)
          await assertNoHorizontalOverflow(page)
          sizes.push(
            JSON.stringify(
              await card.evaluate((node) => {
                const box = (element: Element) => {
                  const rect = element.getBoundingClientRect()
                  return [rect.width, rect.height]
                }
                return [
                  box(node),
                  ...[
                    ...node.querySelectorAll('[data-system-status-tile]'),
                  ].map(box),
                ]
              })
            )
          )
        }
        expect(sizes[1], `${viewport.width}px`).toBe(sizes[0])
      }
    })
  }
}

// English needs the widest header. The card width follows the dashboard grid,
// so a 1280 desktop is as tight as a phone. The healthy title never truncates;
// the longest pill and rate may truncate it but never wrap.
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
    const healthy = await readHeaderLines(card)

    await page.route('**/api/user/system-status', (route) =>
      route.fulfill({ json: { success: true, data: longestHeaderSnapshot() } })
    )
    await page.reload({ waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)
    await expect(card).toContainText('Storage down')
    await expect(card).toContainText('No requests')
    const longest = await readHeaderLines(card)
    expect(longest.pillLines).toBe(1)
    expect(longest.valueLines).toBe(1)
    expect(longest.height).toBeCloseTo(healthy.height, 0)
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

/** Resource monitoring off: CPU, memory and disk say so; traffic is unmeasured. */
function monitorOffSnapshot() {
  const data = systemStatusFixture()
  const off = resourceFixture({
    available: false,
    percent: null,
    state: 'disabled',
  })
  data.cpu = off
  data.memory = off
  data.disk = { ...off, scope: 'filesystem' }
  data.traffic.inbound_mbps = null
  return data
}

for (const locale of ['en', 'zh-CN'] as const) {
  test(`placeholders share one face and fit their tiles in ${locale}`, async ({
    page,
  }) => {
    await configureSystemPage(page, {
      theme: 'light',
      authenticated: true,
      locale,
    })
    await page.route('**/api/user/system-status', (route) =>
      route.fulfill({ json: { success: true, data: monitorOffSnapshot() } })
    )
    await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)
    const card = page.locator('[data-system-status-card]')
    const placeholders = card.locator('[data-metric-placeholder]')
    await expect(placeholders).toHaveText(
      locale === 'en'
        ? ['Off', 'Off', '--', 'Off']
        : ['未启用', '未启用', '--', '未启用']
    )
    const faces = await placeholders.evaluateAll((nodes) =>
      nodes.map((node) => {
        const style = getComputedStyle(node)
        return [style.fontFamily, style.fontSize, style.fontWeight].join('|')
      })
    )
    expect(new Set(faces).size, faces.join('\n')).toBe(1)
    for (const width of [320, 390, 1280, 1366, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      await expectReadingsInsideTiles(card)
    }
  })
}

/** Readings that between them put CPU, memory and disk in all five tiers and
 * the unchecked state; the share sets the three calm tiers and the server
 * verdict the two alarms. Each overall verdict names the tier its pill takes. */
const TIER_READINGS: Array<Partial<ResourceReadings> & { pillTier: string }> = [
  {
    cpuPercent: 18,
    memoryUsedGib: 45,
    diskUsedGib: 700,
    pillTier: 'normal',
  },
  {
    cpuPercent: 82,
    memoryUsedGib: 95,
    diskUsedGib: 200,
    states: { cpu: 'warning', memory: 'critical' },
    status: 'critical',
    pillTier: 'danger',
  },
  {
    cpuPercent: 93,
    memoryUsedGib: 20,
    diskUsedGib: 990,
    states: { cpu: 'warning', disk: 'unchecked' },
    status: 'warning',
    pillTier: 'warning',
  },
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

/** The pill dot, composited on the header, like a tile's fill on its tile. */
async function readPillDot(card: Locator): Promise<number[]> {
  return card
    .locator('[data-system-health] > span:first-child > span:last-child')
    .evaluate((dot) => {
      const context = document.createElement('canvas').getContext('2d')!
      context.fillStyle = getComputedStyle(document.documentElement)
        .getPropertyValue('--page-background')
        .trim()
      context.fillRect(0, 0, 1, 1)
      context.fillStyle = getComputedStyle(dot).backgroundColor
      context.fillRect(0, 0, 1, 1)
      return [...context.getImageData(0, 0, 1, 1).data.slice(0, 3)]
    })
}

/**
 * Gauges reach 3:1 and figures 4.5:1 in every load tier, and neighbouring tiers
 * stay clearly apart, so attention and warning cannot read as one colour again.
 * No tier's figures may pass for the accent colour of money figures either.
 * A reading with its check off stays apart from every tier, and the pill takes
 * the colour of the tile tier that matches its verdict.
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
    const pills: Array<{ tier: string; dot: number[] }> = []
    for (const next of TIER_READINGS) {
      reading = next
      await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)
      const card = page.locator('[data-system-status-card]')
      await expect(card.locator('[data-load-tier]')).toHaveCount(4)
      pills.push({ tier: next.pillTier, dot: await readPillDot(card) })
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

    expect([...seen.keys()].sort()).toEqual([...LOAD_TIERS, 'unchecked'].sort())
    for (let index = 1; index < LOAD_TIERS.length; index += 1) {
      const lower = seen.get(LOAD_TIERS[index - 1]!)!
      const higher = seen.get(LOAD_TIERS[index]!)!
      const pair = `${lower.tier} → ${higher.tier}`
      expect(oklabDistance(lower.fill, higher.fill), pair).toBeGreaterThan(6)
      expect(oklabDistance(lower.text, higher.text), pair).toBeGreaterThan(6)
    }
    const unchecked = seen.get('unchecked')!
    for (const tier of LOAD_TIERS) {
      const other = seen.get(tier)!
      const pair = `unchecked → ${tier}`
      expect(oklabDistance(unchecked.fill, other.fill), pair).toBeGreaterThan(6)
      expect(oklabDistance(unchecked.text, other.text), pair).toBeGreaterThan(6)
    }
    // The dot and the fill are opaque tokens, so equal tokens paint equal pixels.
    for (const pill of pills) {
      expect(pill.dot, `${pill.tier} pill`).toEqual(seen.get(pill.tier)!.fill)
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
