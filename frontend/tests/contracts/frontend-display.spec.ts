import { mkdirSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

const evidence = process.env.REN2HUB_UI_OUTPUT!
const now = Date.parse('2026-07-27T04:00:00Z') / 1000
mkdirSync(`${evidence}/screenshots`, { recursive: true })

const scenarios = [
  {
    name: 'desktop-light-zh-CN',
    viewport: { width: 1440, height: 1000 },
    theme: 'light',
    locale: 'zh-CN',
    authenticated: true,
  },
  {
    name: 'mobile-dark-en',
    viewport: { width: 390, height: 900 },
    theme: 'dark',
    locale: 'en',
    authenticated: false,
  },
] as const

const copy = {
  'zh-CN': {
    models: '可用模型',
    latency: '近24小时平均延迟',
    signIn: '登录后可见',
    docsCta: '查看文档',
    notProvided: '未提供',
    dashboardFailure: '仪表盘数据加载失败',
    health: '健康评分：未提供',
    database: '主数据库',
    up: '上行',
    down: '下行',
    gc: '最近 GC 停顿',
    refresh: '刷新',
    retry: '重试',
    stale: '数据已过期',
    measuredOk: '已测检查项正常',
  },
  en: {
    models: 'Models',
    latency: '24h average latency',
    signIn: 'Sign in to view',
    docsCta: 'Read the docs',
    notProvided: 'Not provided',
    dashboardFailure: 'Dashboard data could not be loaded',
    health: 'Health score: not provided',
    database: 'Main database',
    up: 'Up',
    down: 'Down',
    gc: 'Last GC pause',
    refresh: 'Refresh',
    retry: 'Retry',
    stale: 'Stale data',
    measuredOk: 'Measured checks passing',
  },
} as const

function heroMetric(page: Page, label: string) {
  return page
    .locator('.hero-metrics > div')
    .filter({ has: page.getByText(label, { exact: true }) })
    .locator('dt')
}

async function capture(page: Page, name: string) {
  await assertNoHorizontalOverflow(page)
  await page.screenshot({
    path: `${evidence}/screenshots/frontend-display-${name}.png`,
    fullPage: true,
  })
}

function performanceStats(cpu: number) {
  return {
    memory_stats: {
      alloc: 1_048_576,
      total_alloc: 8_388_608,
      sys: 4_194_304,
      num_gc: 8,
      num_goroutine: 32,
    },
    cache_stats: {},
    disk_cache_info: {},
    disk_space_info: { total: 1000, free: 600, used: 400, used_percent: 40 },
    config: {
      monitor_enabled: true,
      monitor_cpu_threshold: 90,
      monitor_memory_threshold: 90,
      monitor_disk_threshold: 95,
      disk_cache_path: '/tmp/cache',
    },
    system_usage: { cpu_percent: cpu, memory_percent: 20, sampled_at: now },
    db_pool: {
      main: {
        max_open_connections: 0,
        open_connections: 3,
        in_use: 1,
        idle: 2,
        wait_count: 4,
        wait_duration_ms: 1500,
        max_idle_closed: 0,
        max_lifetime_closed: 0,
      },
      log: null,
      log_shared: true,
    },
  }
}

for (const scenario of scenarios) {
  const { name, viewport, theme, locale } = scenario
  const text = copy[locale]

  test(`frontend display weighted home metrics ${name}`, async ({ page }) => {
    await page.setViewportSize(viewport)
    const fixture = await configureStablePage(page, {
      theme,
      locale,
      authenticated: scenario.authenticated,
      status: { docs_link: '/documentation' },
    })
    await page.route('**/api/pricing', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: ['fast-model', 'slow-model'].map((model_name) => ({
            model_name,
            quota_type: 0,
            model_ratio: 1,
            completion_ratio: 2,
            supported_endpoint_types: ['chat'],
          })),
          vendors: [],
          supported_endpoint: {
            chat: { path: '/v1/chat/completions', method: 'POST' },
          },
        },
      })
    )
    await page.route('**/api/perf-metrics/summary?*', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            window_start: now - 86400,
            window_end: now,
            // Nine 100ms requests and one 900ms request average 180ms.
            summary: { avg_latency_ms: 180, success_rate: 100, avg_tps: 20 },
            models: [
              {
                model_name: 'fast-model',
                avg_latency_ms: 100,
                success_rate: 100,
                avg_tps: 20,
              },
              {
                model_name: 'slow-model',
                avg_latency_ms: 900,
                success_rate: 100,
                avg_tps: 20,
              },
            ],
          },
        },
      })
    )
    await page.goto('/')
    await waitForStablePage(page)
    await expect(heroMetric(page, text.models)).toHaveText('2')
    await expect(heroMetric(page, text.latency)).toHaveText('180ms')
    const docs = page.getByRole('link', { name: text.docsCta, exact: true })
    await expect(docs).toHaveAttribute(
      'href',
      new URL('/documentation', page.url()).href
    )
    await expect(docs).toHaveAttribute('target', '_blank')
    await expect(docs).toHaveAttribute('rel', 'noopener noreferrer')
    await capture(page, `home-weighted-${name}`)
    expect(fixture.errors).toEqual([])
  })

  test(`frontend display anonymous protected metrics ${name}`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, {
      theme,
      locale,
      authenticated: false,
      status: {
        HeaderNavModules: JSON.stringify({
          pricing: { enabled: true, requireAuth: true },
        }),
      },
    })
    const protectedReads: string[] = []
    page.on('request', (request) => {
      const path = new URL(request.url()).pathname
      if (path === '/api/pricing' || path === '/api/perf-metrics/summary')
        protectedReads.push(path)
    })
    await page.goto('/')
    await waitForStablePage(page)
    await expect(heroMetric(page, text.models)).toHaveText(text.signIn)
    await expect(heroMetric(page, text.latency)).toHaveText(text.signIn)
    expect(protectedReads).toEqual([])
    await capture(page, `home-sign-in-${name}`)
  })

  test(`frontend display dashboard unsupported and failed data ${name}`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, { theme, locale })
    await page.goto('/dashboard')
    await waitForStablePage(page)
    await expect(page.locator('[data-kpi-cell="rpm"]')).toContainText(
      text.notProvided
    )
    await expect(page.locator('[data-kpi-cell="rpm"]')).not.toHaveRole('button')
    await expect(page.locator('[data-discount-not-connected]')).toContainText(
      text.notProvided
    )
    await expect(page.locator('[data-system-status-card] h2')).toContainText(
      text.notProvided
    )
    await expect(page.locator('[data-success-rate-empty]')).toHaveText(
      text.notProvided
    )
    await expect(page.locator('[data-status-reachable]')).toHaveAttribute(
      'data-status-reachable',
      'true'
    )
    await capture(page, `dashboard-not-provided-${name}`)

    await page.route('**/api/data/self/summary?*', (route) =>
      route.fulfill({
        json: { success: false, message: 'Display summary unavailable' },
      })
    )
    await page.reload()
    await expect(
      page.getByText(text.dashboardFailure, { exact: true })
    ).toBeVisible()
    await expect(
      page
        .locator('[data-dashboard-page]')
        .getByText('Display summary unavailable', { exact: true })
    ).toBeVisible()
    await expect(page.locator('[data-kpi-cell="rpm"]')).toContainText(
      text.notProvided
    )
    await capture(page, `dashboard-load-failed-${name}`)
  })

  test(`frontend display ops measured faults and unsupported probes ${name}`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, { theme, locale })
    let cpu = 20
    let failed = false
    await page.route('**/api/performance/stats', (route) =>
      route.fulfill({
        json: failed
          ? { success: false, message: 'Display performance unavailable' }
          : { success: true, data: performanceStats(cpu) },
      })
    )
    await page.route('**/api/performance/quality?*', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            enabled: true,
            window_start: now - 3600,
            window_end: now,
            bucket_seconds: 3600,
            summary: null,
            series: [],
          },
        },
      })
    )
    await page.goto('/ops')
    await waitForStablePage(page)
    const status = page.locator('[data-ops-section="status"]')
    await expect(page.locator('[data-ops-overall-status]')).toHaveAttribute(
      'data-status',
      'ok'
    )
    await expect(page.locator('[data-ops-overall-status]')).toContainText(
      text.measuredOk
    )
    await expect(page.locator('[data-ops-health-unavailable]')).toHaveText(
      text.health
    )
    await expect(page.locator('[data-ops-not-provided]')).toContainText(
      text.database
    )
    await expect(page.locator('[data-ops-not-provided]')).toContainText('Redis')
    await expect(page.locator('[data-ops-storage="database"]')).toContainText(
      text.notProvided
    )
    await expect(page.locator('[data-ops-storage="redis"]')).toContainText(
      text.notProvided
    )
    for (const label of [text.up, text.down, text.gc]) {
      const metric = page
        .locator('[data-ops-section="resources"] .ops-metric')
        .filter({ has: page.getByText(label, { exact: true }) })
      await expect(metric.locator('.ops-metric-value')).toHaveText(
        text.notProvided
      )
    }
    await expect(page.locator('[data-ops-reason]')).toHaveCount(0)
    await capture(page, `ops-not-provided-${name}`)

    cpu = 95
    await status
      .getByRole('button', { name: text.refresh, exact: true })
      .click()
    await expect(page.locator('[data-ops-overall-status]')).toHaveAttribute(
      'data-status',
      'critical'
    )
    await expect(page.locator('[data-ops-reason]')).toHaveCount(1)
    await expect(page.locator('[data-ops-reason]')).toContainText('CPU')
    await expect(page.locator('[data-ops-reason]')).toContainText('95%')
    await expect(page.locator('[data-ops-health-unavailable]')).toHaveText(
      text.health
    )
    await capture(page, `ops-cpu-critical-${name}`)

    failed = true
    await status
      .getByRole('button', { name: text.refresh, exact: true })
      .click()
    await expect(page.locator('[data-ops-error]')).toContainText(
      'Display performance unavailable'
    )
    await expect(page.locator('[data-ops-error]')).toContainText(text.stale)
    await expect(page.locator('[data-ops-overall-status]')).toHaveAttribute(
      'data-status',
      'critical'
    )
    await capture(page, `ops-refresh-failed-${name}`)
    failed = false
    cpu = 20
    await page
      .locator('[data-ops-error]')
      .getByRole('button', { name: text.retry, exact: true })
      .click()
    await expect(page.locator('[data-ops-error]')).toHaveCount(0)
    await expect(page.locator('[data-ops-overall-status]')).toHaveAttribute(
      'data-status',
      'ok'
    )
  })
}

test('frontend display document navigation opens safe links and rejects scripts', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 1000 })
  const status = { docs_link: '/documentation' }
  await configureStablePage(page, { theme: 'light', locale: 'zh-CN', status })
  await page.context().route('**/documentation', (route) =>
    route.fulfill({
      contentType: 'text/html',
      body: '<!doctype html><html><head><title>Contract documentation</title></head><body>Documentation opened</body></html>',
    })
  )
  await page.goto('/dashboard')
  await waitForStablePage(page)
  const nav = page.locator('[data-top-nav="desktop"]')
  await expect(
    nav.getByRole('button', { name: '炼金室 未提供', exact: true })
  ).toBeDisabled()
  await nav.getByRole('button', { name: '创意社区', exact: true }).click()
  const docs = nav.getByRole('link', { name: '文档', exact: true })
  await expect(docs).toHaveAttribute('rel', 'noopener noreferrer')
  await expect(docs).toHaveAttribute('target', '_blank')
  const sidebarDocs = page.locator('[data-console-tool="docs"]')
  await expect(sidebarDocs).toHaveAttribute(
    'href',
    new URL('/documentation', page.url()).href
  )
  const popupPromise = page.waitForEvent('popup')
  await docs.click()
  const popup = await popupPromise
  await expect(popup).toHaveURL(new URL('/documentation', page.url()).href)
  await expect(
    popup.getByText('Documentation opened', { exact: true })
  ).toBeVisible()
  await popup.close()

  await sidebarDocs.focus()
  await expect(sidebarDocs).toBeFocused()
  const keyboardPopupPromise = page.waitForEvent('popup')
  await page.keyboard.press('Enter')
  const keyboardPopup = await keyboardPopupPromise
  await expect(keyboardPopup).toHaveURL(
    new URL('/documentation', page.url()).href
  )
  await expect(
    keyboardPopup.getByText('Documentation opened', { exact: true })
  ).toBeVisible()
  await keyboardPopup.close()

  status.docs_link = 'javascript:alert(1)'
  await page.reload()
  await waitForStablePage(page)
  await nav.getByRole('button', { name: '创意社区', exact: true }).click()
  await expect(
    nav.getByRole('link', { name: '文档', exact: true })
  ).toHaveCount(0)
  await expect(
    nav.getByRole('button', { name: '文档 链接不可用', exact: true })
  ).toBeDisabled()
  await expect(sidebarDocs).toBeDisabled()
  await expect(sidebarDocs).not.toHaveAttribute('href')
  await capture(page, 'docs-invalid-link-desktop-light-zh-CN')
})
