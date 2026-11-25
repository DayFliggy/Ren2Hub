import { expect, test, type Page } from '@playwright/test'
import {
  measuredResource,
  performanceFixture,
  qualityFixture,
  routingFixture,
} from '../../src/api/__tests__/opsFixtures'
import type { Observability } from '../../src/api/observability'
import { createModelMarketFixture } from '../fixtures/modelMarket'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  contrastRatio,
  oklabDistance,
  relativeLuminance,
  captureEvidence,
} from './fixtures'

const SECTIONS = [
  'status',
  'quality',
  'upstream',
  'gateway',
  'resources',
  'storage',
  'tasks',
]
const section = (page: Page, name: string) =>
  page.locator(`[data-ops-section="${name}"]`)
const ok = (data: unknown) => ({ json: { success: true, data } })
function overview(window: '1h' | '6h' | '24h' = '1h') {
  const data = performanceFixture()
  data.observability.resources.cpu = measuredResource(36.2)
  data.observability.resources.memory = measuredResource(58.9)
  for (const id of ['cpu', 'memory'] as const) {
    const check = data.observability.checks.find((item) => item.id === id)
    if (check) {
      check.status = 'ok'
      check.reason = ''
    }
  }
  data.observability.coverage = { measured: 3, total: 4 }
  data.observability.status = 'warning'
  data.observability.series.window = window
  return data
}
function routing() {
  const data = routingFixture()
  data.channels[0]!.id = 11
  data.observations[0]!.channel_id = 11
  return {
    ...data,
    quality: [
      {
        channel_id: 11,
        model: 'quality-model',
        operation: 1,
        stream: true,
        band: 1,
        reason: 'speed',
        samples: 50,
        latency_samples: 32,
        speed_samples: 25,
      },
    ],
    cache: {
      free_requests: 200,
      explorations: 10,
      bands: [
        {
          channel_id: 11,
          model: 'quality-model',
          operation: 1,
          stream: true,
          mode: 'explicit',
          known: true,
          band: 1,
          reason: 'comparable',
          coverage: 0.8,
          token_rate: 0.7,
          samples: 56,
          sessions: 15,
        },
      ],
    },
  }
}
async function mockOps(page: Page) {
  await page.route('**/api/performance/stats?*', (route) => {
    const window = new URL(route.request().url()).searchParams.get('window') as
      '1h' | '6h' | '24h'
    return route.fulfill(ok(overview(window)))
  })
  await page.route('**/api/performance/routing', (route) =>
    route.fulfill(ok(routing()))
  )
  await page.route('**/api/performance/quality?*', (route) =>
    route.fulfill(ok(qualityFixture()))
  )
}
async function gotoOps(page: Page) {
  await page.goto('/ops')
  await expect(section(page, 'status')).toBeVisible({ timeout: 30_000 })
}

function recordApi(page: Page) {
  const paths = new Set<string>()
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (path.startsWith('/api/')) paths.add(path)
  })
  return paths
}

for (const [theme, mobile] of [
  ['light', false],
  ['dark', true],
] as const) {
  test(`ops monitor root ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1440, height: 900 }
    )
    await configureStablePage(page, { theme })
    await mockOps(page)
    const clockTime = new Date('2026-07-27T04:00:00Z')
    await page.clock.install({ time: clockTime })
    await page.clock.pauseAt(clockTime)
    const api = recordApi(page)
    await gotoOps(page)
    await expect(
      page.getByRole('heading', { level: 1, name: '运维监控' })
    ).toBeVisible({ timeout: 30_000 })
    await expect(section(page, 'tasks')).toContainText('暂无等待或运行中的任务')
    await expect(section(page, 'upstream')).toContainText('雾桥中转')
    await expect(section(page, 'billing')).toHaveCount(0)
    if (mobile)
      await page.locator('[aria-controls="console-mobile-navigation"]').click()
    const nav = mobile
      ? page
          .locator('[data-console-mobile-navigation] .drawer-section')
          .first()
          .locator('[data-console-nav-item]')
      : page.locator('[data-top-nav="desktop"] > div > button')
    await expect(nav.first()).toBeVisible()
    expect(
      (await nav.allInnerTexts())
        .map((text) => text.trim().split('\n')[0])
        .slice(0, 4)
    ).toEqual(['活动', '仪表盘', '运维监控', '控制台'])
    await expect(nav.nth(2)).toHaveAttribute('aria-current', 'page')
    if (mobile) {
      await page.keyboard.press('Escape')
      // Vue removes the leaving dialog after its animation-frame boundary.
      await page.clock.runFor(50)
      await expect(
        page.locator('[data-console-mobile-navigation]')
      ).toHaveCount(0)
    }
    await expect(section(page, 'status')).toContainText('在途并发')
    await expect(section(page, 'status')).toContainText('熔断与冷却')
    await expect(
      section(page, 'status').locator('[data-ops-infra-strip]')
    ).toBeVisible()
    const countdown = section(page, 'status').locator('.ops-countdown-badge')
    await expect(countdown).toHaveAttribute('title', '30s')
    await page.clock.runFor(1000)
    await expect(countdown).toHaveAttribute('title', '29s')
    await section(page, 'status')
      .getByRole('button', { name: '刷新', exact: true })
      .click()
    await expect(countdown).toHaveAttribute('title', '30s')
    await expect(page.locator('[data-ops-overall-status]')).toHaveAttribute(
      'data-status',
      'warning'
    )
    // Verify WCAG AA contrast (>= 4.5:1) on the coverage score inside MiniRing.
    const healthScoreContrast = await page
      .locator('.ops-health-score')
      .evaluate((el) => {
        const ctx = document.createElement('canvas').getContext('2d')!
        const paint = (colors: string[]) => {
          ctx.clearRect(0, 0, 1, 1)
          for (const c of colors) {
            if (!c || c === 'rgba(0, 0, 0, 0)' || c === 'transparent') continue
            ctx.fillStyle = c
            ctx.fillRect(0, 0, 1, 1)
          }
          return [...ctx.getImageData(0, 0, 1, 1).data.slice(0, 3)]
        }
        const pageBg = getComputedStyle(document.documentElement)
          .getPropertyValue('--page-background')
          .trim()
        const layers: string[] = []
        for (
          let n: HTMLElement | null = el as HTMLElement;
          n;
          n = n.parentElement
        )
          layers.unshift(getComputedStyle(n).backgroundColor)
        const bg = paint([pageBg, ...layers])
        const fg = paint([`rgb(${bg.join(',')})`, getComputedStyle(el).color])
        return { fg, bg }
      })
    expect(
      contrastRatio(
        relativeLuminance(healthScoreContrast.fg),
        relativeLuminance(healthScoreContrast.bg)
      )
    ).toBeGreaterThanOrEqual(4.5)

    // Verify maintenance buttons never collide with 0px gap.
    const maintBoxes = await page
      .locator('.ops-maintenance-actions button')
      .evaluateAll((buttons) =>
        buttons.map((b) => {
          const r = b.getBoundingClientRect()
          return { left: r.left, right: r.right, top: r.top, bottom: r.bottom }
        })
      )
    for (let i = 1; i < maintBoxes.length; i++) {
      const prev = maintBoxes[i - 1]!
      const curr = maintBoxes[i]!
      const sameRow = Math.abs(curr.top - prev.top) < 8
      if (sameRow) expect(curr.left - prev.right).toBeGreaterThanOrEqual(4)
      else expect(curr.top - prev.bottom).toBeGreaterThanOrEqual(4)
    }

    // Verify Redis card does not stretch into a tall empty void.
    const redisBox = await page
      .locator('[data-ops-storage="redis"]')
      .boundingBox()
    expect(redisBox!.height).toBeLessThan(180)

    // Verify night-mode 3px glow on measured resource rings (flat by day).
    const diskRingFilter = await page
      .locator('[data-ops-gauge="disk"] circle[stroke-linecap]')
      .evaluate((el) => getComputedStyle(el).filter)
    if (theme === 'dark') expect(diskRingFilter).toContain('drop-shadow')
    else expect(diskRingFilter).toBe('none')

    await assertNoHorizontalOverflow(page)
    const boxes = await Promise.all(
      SECTIONS.map((name) => section(page, name).boundingBox())
    )
    if (mobile) {
      const tops = boxes.map((box) => box!.y)
      expect(tops).toEqual([...tops].sort((a, b) => a - b))
    } else {
      const [status, quality, upstream, gateway, resources, storage, tasks] =
        boxes
      expect(status!.y).toBeLessThan(quality!.y)
      expect(upstream!.x).toBeGreaterThan(quality!.x)
      expect(gateway!.y).toBeGreaterThan(quality!.y)
      expect(resources!.y).toBeGreaterThan(gateway!.y)
      expect(storage!.x).toBeGreaterThan(resources!.x)
      expect(storage!.y).toBe(resources!.y)
      expect(tasks!.width).toBeGreaterThan(quality!.width)
    }
    await captureEvidence(page, { path: testInfo.outputPath('ops-top.png') })
    const history = page.locator('[data-ops-task-history]')
    await history.getByRole('button', { name: '展开' }).click()
    await expect(history.getByText('清理任务历史')).toBeVisible()
    if (mobile) {
      const toggleBox = await history
        .locator('button.task-toggle')
        .boundingBox()
      expect(toggleBox!.height).toBeGreaterThanOrEqual(44)
      expect(toggleBox!.height).toBeLessThanOrEqual(52)
    }
    await page
      .locator('[data-console-scroll-zone]')
      .evaluate((zone) => zone.scrollTo(0, zone.scrollHeight / 2))
    await captureEvidence(page, {
      path: testInfo.outputPath('ops-middle.png'),
    })
    await page
      .locator('[data-console-scroll-zone]')
      .evaluate((zone) => zone.scrollTo(0, zone.scrollHeight))
    await captureEvidence(page, {
      path: testInfo.outputPath('ops-bottom.png'),
    })
    for (const path of [
      '/api/performance/stats',
      '/api/performance/routing',
      '/api/performance/quality',
      '/api/user/model-catalog',
      '/api/system-task/list',
      // The Root version badge in the top bar.
      '/api/admin/system/check-updates',
    ])
      expect(api.has(path), path).toBe(true)
  })
}

/** The colours a gauge can take: the five load tiers and the two neutrals. */
const GAUGE_TOKENS = [
  '--load-idle',
  '--load-normal',
  '--load-notice',
  '--load-warning',
  '--load-danger',
  '--load-unchecked',
  '--text-tertiary',
]

/**
 * Resource history lines take hues outside the gauge colours, so a line cannot
 * read as a verdict: each stays apart from every tier, from the neutrals and
 * from the other lines, and keeps 3:1 against the card.
 */
for (const theme of ['light', 'dark'] as const) {
  test(`resource trend lines stay apart from gauge colours in ${theme} theme`, async ({
    page,
  }) => {
    await configureStablePage(page, { theme })
    await mockOps(page)
    await gotoOps(page)
    const legend = section(page, 'resources')
      .locator('.ops-trend')
      .first()
      .locator('.ops-legend .ops-dot')
    await expect(legend).toHaveCount(3)
    const measured = await legend.evaluateAll((dots, tokens) => {
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
      const layers: string[] = []
      for (let node = dots[0]!.parentElement; node; node = node.parentElement)
        layers.unshift(getComputedStyle(node).backgroundColor)
      const card = paint([page, ...layers])
      const probe = document.createElement('span')
      document.body.append(probe)
      const gauges = tokens.map((token) => {
        probe.style.color = `var(${token})`
        return { token, rgb: paint([getComputedStyle(probe).color]) }
      })
      probe.remove()
      const lines = dots.map((dot) =>
        paint([`rgb(${card.join(',')})`, getComputedStyle(dot).backgroundColor])
      )
      return { card, gauges, lines }
    }, GAUGE_TOKENS)
    const card = relativeLuminance(measured.card)
    measured.lines.forEach((line, index) => {
      expect(
        contrastRatio(card, relativeLuminance(line)),
        `line ${index} on the card`
      ).toBeGreaterThanOrEqual(3)
      for (const gauge of measured.gauges)
        expect(
          oklabDistance(line, gauge.rgb),
          `line ${index} against ${gauge.token}`
        ).toBeGreaterThanOrEqual(10)
      for (const other of measured.lines.slice(index + 1))
        expect(
          oklabDistance(line, other),
          `line ${index}`
        ).toBeGreaterThanOrEqual(10)
    })
  })
}

const TRAFFIC: Record<
  Observability['traffic']['scope'],
  Observability['traffic']
> = {
  host_interface: {
    scope: 'host_interface',
    inbound_mbps: 1.5,
    outbound_mbps: 6.2,
    interface: 'eth0',
    reason: '',
  },
  application_listener: {
    scope: 'application_listener',
    inbound_mbps: 1.5,
    outbound_mbps: 6.2,
    interface: '',
    reason: '',
  },
}
const TRAFFIC_TEXT = {
  'zh-CN': {
    up: '出站',
    down: '入站',
    title: '流量',
    host_interface:
      '宿主机网卡 eth0 的全部收发；中转请求同时产生客户端和上游两段流量。',
    application_listener:
      '主服务端口上的全部连接字节，含 HTTP 头、正文、WebSocket 和健康检查；不含应用主动连接上游的流量和 pprof 端口。',
  },
  en: {
    up: 'Outbound',
    down: 'Inbound',
    title: 'Traffic',
    host_interface:
      'All traffic on host interface eth0; relayed requests count both client and upstream legs.',
    application_listener:
      "All bytes on the main service port, including headers, bodies, WebSocket and health checks; excludes the app's own upstream connections and the pprof port.",
  },
} as const

async function mockTraffic(
  page: Page,
  traffic: () => Observability['traffic']
) {
  await page.route('**/api/performance/stats?*', (route) => {
    const window = new URL(route.request().url()).searchParams.get('window') as
      '1h' | '6h' | '24h'
    const data = overview(window)
    data.observability.traffic = traffic()
    return route.fulfill(ok(data))
  })
}

/** Outbound and inbound read the same in the figures and the trend lines, and
 * the scope changes only the description: the card keeps its size. */
for (const locale of ['zh-CN', 'en'] as const) {
  for (const theme of ['light', 'dark'] as const) {
    test(`traffic scopes read the server side in ${locale} ${theme}`, async ({
      page,
    }) => {
      let scope: Observability['traffic']['scope'] = 'host_interface'
      await configureStablePage(page, { theme, locale })
      await mockOps(page)
      await mockTraffic(page, () => TRAFFIC[scope])
      const text = TRAFFIC_TEXT[locale]
      const resources = section(page, 'resources')
      const traffic = resources.locator('[data-ops-traffic]')
      const figure = (label: string) =>
        traffic
          .locator('.ops-metric')
          .filter({
            has: page.getByText(label, { exact: true }),
          })
          .locator('.ops-metric-value')
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
          await gotoOps(page)
          await expect(traffic).toHaveAttribute('title', text[next])
          await expect(traffic).toHaveAttribute('aria-description', text[next])
          await expect(figure(text.up)).toHaveText(/^6\.2\s*Mbps$/)
          await expect(figure(text.down)).toHaveText(/^1\.5\s*Mbps$/)
          // The resource trend comes first, the traffic trend last.
          const chart = resources.locator('.ops-trend').last()
          await expect(chart.locator('.ops-trend-title')).toHaveText(text.title)
          await expect(chart.locator('.ops-legend li')).toHaveText([
            text.down,
            text.up,
          ])
          await expect(chart.locator('figcaption')).toHaveText(
            `${text.title}: ${text.down}: 1.5Mbps; ${text.up}: 6.2Mbps`
          )
          await assertNoHorizontalOverflow(page)
          const box = await resources.boundingBox()
          sizes.push(JSON.stringify([box!.width, box!.height]))
        }
        expect(sizes[1], `${viewport.width}px`).toBe(sizes[0])
      }
    })
  }
}

test('a host read failure names its reason and keeps the figures unavailable', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  await mockOps(page)
  await mockTraffic(page, () => ({
    ...TRAFFIC.host_interface,
    inbound_mbps: null,
    outbound_mbps: null,
    reason: 'host_counters_unreadable',
  }))
  await gotoOps(page)
  const traffic = section(page, 'resources').locator('[data-ops-traffic]')
  await expect(traffic).toHaveAttribute(
    'title',
    `${TRAFFIC_TEXT['zh-CN'].host_interface}读数不可用：无法读取宿主机网卡计数。`
  )
  await expect(traffic.locator('.ops-metric-value').first()).toHaveText(
    '暂无数据'
  )
  await expect(traffic.locator('.ops-metric-value').nth(1)).toHaveText(
    '暂无数据'
  )
  await assertNoHorizontalOverflow(page)
})

test('overview failure shows an inline retry while other sections load', async ({
  page,
}) => {
  await configureStablePage(page, {
    theme: 'light',
    expectedApiErrors: { '/api/performance/stats': [500] },
  })
  await mockOps(page)
  let failing = true
  await page.route('**/api/performance/stats?*', (route) =>
    failing
      ? route.fulfill({
          status: 500,
          json: { success: false, message: 'overview unavailable' },
        })
      : route.fulfill(ok(overview()))
  )
  await gotoOps(page)
  const status = section(page, 'status')
  await expect(status.getByRole('alert')).toContainText('overview unavailable')
  await expect(status.getByRole('alert')).toContainText('尚未成功加载')
  await expect(section(page, 'quality')).toContainText('样本最多的观察')
  await expect(section(page, 'gateway')).toContainText('quality-model')
  await expect(section(page, 'gateway')).toContainText('探索占比')
  failing = false
  await status.getByRole('button', { name: '重试', exact: true }).click()
  await expect(status.getByRole('alert')).toHaveCount(0)
  await expect(page.locator('[data-ops-gauge="cpu"]')).toContainText('36.2')
  await assertNoHorizontalOverflow(page)
})

test('a failed refresh keeps earlier data only behind a stale marker', async ({
  page,
}) => {
  await configureStablePage(page, {
    theme: 'dark',
    expectedApiErrors: { '/api/performance/stats': [500] },
  })
  await mockOps(page)
  await gotoOps(page)
  await expect(page.locator('[data-ops-gauge="cpu"]')).toContainText('36.2')
  await page.route('**/api/performance/stats?*', (route) =>
    route.fulfill({
      status: 500,
      json: { success: false, message: 'sampler timeout' },
    })
  )
  await section(page, 'status')
    .getByRole('button', { name: '刷新', exact: true })
    .click()
  await expect(section(page, 'status').getByRole('alert')).toContainText(
    '数据已过期'
  )
  await expect(section(page, 'resources')).toHaveAttribute(
    'data-ops-stale',
    'true'
  )
  await expect(page.locator('[data-ops-gauge="cpu"]')).toContainText('36.2')
  await assertNoHorizontalOverflow(page)
})

test('native empty observations remain distinct from healthy measurements', async ({
  page,
}, testInfo) => {
  await configureStablePage(page, { theme: 'light' })
  await mockOps(page)
  await page.route('**/api/performance/routing', (route) =>
    route.fulfill(ok({ users: [], channels: [], observations: [] }))
  )
  await page.route('**/api/performance/quality?*', (route) =>
    route.fulfill(ok({ ...qualityFixture(), summary: null, series: [] }))
  )
  await gotoOps(page)
  await expect(section(page, 'upstream')).toContainText('未采样不表示健康')
  await expect(section(page, 'quality')).toContainText('暂无请求观察')
  await expect(section(page, 'tasks')).toContainText('暂无等待或运行中的任务')
  await assertNoHorizontalOverflow(page)
  await captureEvidence(page, { path: testInfo.outputPath('ops-empty.png') })
})

for (const role of [1, 10]) {
  test(`non-Root role ${role} cannot see or request native ops`, async ({
    page,
  }) => {
    await configureStablePage(page, { theme: 'light', user: { role } })
    const api = recordApi(page)
    await page.goto('/ops')
    await expect(page).toHaveURL(/\/dashboard$/)
    await expect(page.locator('[data-ops-page]')).toHaveCount(0)
    expect(api.has('/api/performance/stats')).toBe(false)
    expect(api.has('/api/performance/routing')).toBe(false)
    for (const path of [
      '/api/admin/system/version',
      '/api/admin/system/check-updates',
      '/api/admin/system/rollback-versions',
    ])
      expect(api.has(path), path).toBe(false)
    await assertNoHorizontalOverflow(page)
  })
}

test('many abnormal observations scroll inside the upstream card', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'dark' })
  await mockOps(page)
  const snapshot = routing()
  snapshot.observations = Array.from({ length: 50 }, (_, index) => ({
    ...snapshot.observations[0]!,
    model: `native-model-${index}`,
  }))
  await page.route('**/api/performance/routing', (route) =>
    route.fulfill(ok(snapshot))
  )
  await gotoOps(page)
  const list = page.locator('[data-ops-cooling]')
  await expect(list.locator('li')).toHaveCount(50)
  const size = await list.evaluate((element) => ({
    client: element.clientHeight,
    scroll: element.scrollHeight,
  }))
  expect(size.client).toBeLessThanOrEqual(260)
  expect(size.scroll).toBeGreaterThan(size.client)
  await assertNoHorizontalOverflow(page)
})

test('an empty published catalog is not reported as healthy infrastructure', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  await mockOps(page)
  const catalog = createModelMarketFixture()
  catalog.channels = []
  catalog.offers = []
  await page.route('**/api/user/model-catalog', (route) =>
    route.fulfill(ok(catalog))
  )
  await gotoOps(page)
  await expect(section(page, 'upstream')).toContainText(
    '模型目录中暂无已发布渠道'
  )
  await expect(section(page, 'upstream')).not.toContainText('所有上游通道正常')
})

for (const width of [1280, 1440]) {
  test(`store readings fit their cards at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await configureStablePage(page, { theme: 'light' })
    await mockOps(page)
    await gotoOps(page)
    const stores = page.locator('[data-ops-storage]')
    await expect(stores).toHaveCount(3)
    const problems = await stores.evaluateAll((cards) =>
      cards.flatMap((card) => [
        ...Array.from(card.querySelectorAll('.ops-metric-value'))
          .filter((value) => value.scrollWidth > value.clientWidth + 1)
          .map((value) => `truncated: ${value.textContent?.trim()}`),
        ...Array.from(card.querySelectorAll('.ops-metric-label'))
          .filter((label) => label.getBoundingClientRect().height > 24)
          .map((label) => `wrapped: ${label.textContent?.trim()}`),
      ])
    )
    expect(problems).toEqual([])
  })
}

test('the window selector reloads native server resource history', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  await mockOps(page)
  const windows: string[] = []
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (url.pathname === '/api/performance/stats')
      windows.push(url.searchParams.get('window') ?? '')
  })
  await gotoOps(page)
  await expect(page.locator('[data-ops-gauge="cpu"]')).toBeVisible()
  await page
    .locator('[data-ops-runtime-window]')
    .getByRole('radio', { name: '6 小时' })
    .click()
  await expect.poll(() => windows.at(-1)).toBe('6h')
  await expect(section(page, 'resources')).toContainText('趋势：6 小时')
})

test('focus fullscreen mode hides console chrome and diagnosis modal lists active breakers', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'dark' })
  await mockOps(page)
  await page.route('**/api/performance/routing', (route) =>
    route.fulfill(
      ok({
        ...routing(),
        breakers: [
          {
            channel_id: 11,
            channel_name: '雾桥中转',
            model: 'claude-sonnet-4-5',
            state: 'open',
            open_until: new Date('2026-07-27T04:02:00Z').getTime(),
            probe_in_flight: false,
            trigger: 'consecutive_errors',
            last_status: 502,
            last_error_code: 'upstream_error',
            opened_at: new Date('2026-07-27T04:00:00Z').getTime(),
            reopens: 2,
          },
        ],
        cooldowns: [
          {
            channel_id: 12,
            channel_name: '原生专线',
            until: new Date('2026-07-27T04:01:00Z').getTime(),
            source: 'retry_after',
          },
        ],
      })
    )
  )
  await page.goto('/ops?fullscreen=1')
  await expect(section(page, 'status')).toBeVisible({ timeout: 30_000 })
  await expect(page.locator('[data-ops-fullscreen="true"]')).toBeVisible()
  await expect(page.locator('[data-top-nav="desktop"]')).toHaveCount(0)
  await expect(page.locator('[data-ops-breakers]')).toContainText('雾桥中转')
  await expect(page.locator('[data-ops-breakers]')).toContainText('原生专线')
  await page.locator('[data-ops-diagnosis-trigger]').click()
  await expect(page.locator('[data-ops-diagnosis-modal]')).toContainText(
    'upstream_error'
  )
  // Verify no future-tense relative timestamp ("84天后") leaks under clock skew.
  expect(await page.locator('[data-ops-page]').innerText()).not.toMatch(
    /\d+\s*(?:天后|小时后|分钟后|秒后)/
  )
  await assertNoHorizontalOverflow(page)
})

test('english labels render without raw keys', async ({ page }) => {
  await configureStablePage(page, { theme: 'light', locale: 'en' })
  await mockOps(page)
  await gotoOps(page)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Ops monitor' })
  ).toBeVisible()
  await expect(section(page, 'quality')).toContainText(
    'Observations with most samples'
  )
  expect(await page.locator('[data-ops-page]').innerText()).not.toMatch(
    /\b(?:ops|gateway|quality|storage|tasks)\.[a-z]/
  )
  await assertNoHorizontalOverflow(page)
})
