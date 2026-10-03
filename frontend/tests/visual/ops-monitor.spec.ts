import { expect, test, type Page } from '@playwright/test'

import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'
import {
  ACTIVE_TASKS,
  opsBillingMetrics,
  opsChannels,
  opsOverview,
  opsRequests,
} from './opsFixtures'

const SECTIONS = [
  'status',
  'quality',
  'upstream',
  'infrastructure',
  'resources',
  'storage',
  'tasks',
]

function ok(data: unknown) {
  return { json: { success: true, message: '', data } }
}

async function mockOps(page: Page) {
  await page.route(/\/api\/ops\/overview\?/, (route) => {
    const window = new URL(route.request().url()).searchParams.get('window')
    return route.fulfill(ok(opsOverview(window as '1h' | '6h' | '24h')))
  })
  await page.route(/\/api\/system-task\/list\?/, (route) => {
    const scope = new URL(route.request().url()).searchParams.get('scope')
    const items = scope === 'active' ? ACTIVE_TASKS : []
    return route.fulfill({
      json: { success: true, data: items, total: items.length },
    })
  })
}

function recordApi(page: Page) {
  const paths = new Set<string>()
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (url.pathname.startsWith('/api/')) paths.add(url.pathname)
  })
  return paths
}

function collectErrors(page: Page) {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('console', (message) => {
    if (message.type() === 'error' || message.type() === 'warning')
      errors.push(`${message.type()}: ${message.text()}`)
  })
  return errors
}

const section = (page: Page, name: string) =>
  page.locator(`[data-ops-section="${name}"]`)

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`ops monitor root ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 900 }
      )
      await configureStablePage(page, { theme })
      await mockOps(page)
      const errors = collectErrors(page)
      const api = recordApi(page)
      await page.goto('/ops')
      await expect(
        page.getByRole('heading', { level: 1, name: '运维监控' })
      ).toBeVisible()
      await expect(section(page, 'billing')).toHaveCount(0)
      await expect(section(page, 'tasks')).toContainText('网关状态清理')
      await expect(section(page, 'upstream')).toContainText('upstream-1')

      const nav = page
        .locator(`[data-top-nav="${mobile ? 'mobile' : 'desktop'}"]`)
        .locator(
          mobile ? ':scope > a, :scope > button' : ':scope > div > button'
        )
      await expect(nav.first()).toBeVisible()
      const labels = (await nav.allInnerTexts()).map(
        (text) => text.trim().split('\n')[0]
      )
      expect(labels.slice(0, 4)).toEqual([
        '活动',
        '仪表盘',
        '运维监控',
        '控制台',
      ])
      expect(labels).toHaveLength(mobile ? 5 : 6)
      await expect(nav.nth(2)).toHaveAttribute('aria-current', 'page')

      await expect(section(page, 'status')).toContainText('磁盘')
      await expect(section(page, 'status')).not.toContainText('版本')
      await expect(section(page, 'status')).not.toContainText('运行时长')
      await expect(
        section(page, 'status').locator('.ops-countdown-badge')
      ).toHaveAttribute('title', '30s')
      await expect(page.locator('[data-ops-overall-status]')).toHaveAttribute(
        'data-status',
        'warning'
      )
      await assertNoHorizontalOverflow(page)

      const boxes = await Promise.all(
        SECTIONS.map(async (name) => section(page, name).boundingBox())
      )
      if (mobile) {
        const tops = boxes.map((box) => box!.y)
        expect(tops).toEqual([...tops].sort((a, b) => a - b))
      } else {
        const [status, quality, , infrastructure, resources, , tasks] = boxes
        // Header and request quality fit the first screen.
        expect(status!.y).toBeLessThan(quality!.y)
        expect(resources!.y).toBeGreaterThan(quality!.y)
        expect(infrastructure!.y).toBeGreaterThan(quality!.y)
        expect(tasks!.width).toBeGreaterThan(quality!.width)
        await expect(section(page, 'infrastructure')).toBeVisible()
      }

      await page.screenshot({ path: testInfo.outputPath('ops-top.png') })
      await page
        .locator('[data-console-scroll-zone]')
        .evaluate((zone) => zone.scrollTo(0, zone.scrollHeight / 2))
      await page.screenshot({ path: testInfo.outputPath('ops-middle.png') })
      await page
        .locator('[data-console-scroll-zone]')
        .evaluate((zone) => zone.scrollTo(0, zone.scrollHeight))
      await page.screenshot({ path: testInfo.outputPath('ops-bottom.png') })

      for (const path of [
        '/api/ops/overview',
        '/api/ops/requests',
        '/api/ops/channels',
        '/api/system-task/summary',
      ])
        expect(api.has(path), path).toBe(true)
      expect(api.has('/api/gateway/metrics')).toBe(false)
      expect(errors).toEqual([])
    })
  }
}

test('overview failure shows an inline retry while other sections load', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  await mockOps(page)
  let failing = true
  await page.route(/\/api\/ops\/overview\?/, (route) =>
    failing
      ? route.fulfill({
          status: 500,
          json: { success: false, message: 'overview unavailable' },
        })
      : route.fulfill(ok(opsOverview('1h')))
  )
  await page.goto('/ops')
  const status = section(page, 'status')
  await expect(status.getByRole('alert')).toContainText('overview unavailable')
  await expect(status.getByRole('alert')).toContainText('尚未成功加载')
  await expect(section(page, 'resources')).toContainText('暂无数据')
  await expect(page.locator('[data-ops-gauge]')).toHaveCount(0)
  await expect(section(page, 'quality')).toContainText('流量最高的模型')
  failing = false
  await status.getByRole('button', { name: '重试', exact: true }).click()
  await expect(status.getByRole('alert')).toHaveCount(0)
  await expect(page.locator('[data-ops-gauge="cpu"]')).toContainText('36.2')
})

test('a failed refresh keeps earlier data only behind a stale marker', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'dark' })
  await mockOps(page)
  await page.goto('/ops')
  await expect(page.locator('[data-ops-gauge="cpu"]')).toContainText('36.2')
  await page.route(/\/api\/ops\/overview\?/, (route) =>
    route.fulfill({
      status: 500,
      json: { success: false, message: 'sampler timeout' },
    })
  )
  await section(page, 'status')
    .getByRole('button', { name: '刷新', exact: true })
    .click()
  const alert = section(page, 'status').getByRole('alert')
  await expect(alert).toContainText('数据已过期')
  await expect(alert).toContainText('最近成功')
  await expect(section(page, 'resources')).toHaveAttribute(
    'data-ops-stale',
    'true'
  )
  await expect(section(page, 'resources')).toContainText('数据已过期')
  await expect(page.locator('[data-ops-gauge="cpu"]')).toContainText('36.2')
})

test('empty states read as healthy instead of zero-filled', async ({
  page,
}, testInfo) => {
  await configureStablePage(page, { theme: 'light' })
  await mockOps(page)
  await page.route(/\/api\/ops\/channels/, (route) =>
    route.fulfill(ok(opsChannels(0)))
  )
  await page.route(/\/api\/ops\/requests\?/, (route) =>
    route.fulfill(ok({ ...opsRequests(), enabled: false }))
  )
  await page.route(/\/api\/system-task\/summary/, (route) =>
    route.fulfill(ok({ types: [] }))
  )
  await page.route(/\/api\/system-task\/list\?/, (route) =>
    route.fulfill({ json: { success: true, data: [], total: 0 } })
  )
  await page.route(/\/api\/gateway\/metrics/, (route) =>
    route.fulfill(
      ok({
        ...opsBillingMetrics(),
        billing_recovery: { pending: 0, oldest_updated_at: 0 },
      })
    )
  )
  await page.goto('/ops')
  await expect(section(page, 'upstream')).toContainText('所有上游通道正常')
  await expect(section(page, 'quality')).toContainText('请求日志未启用')
  await expect(section(page, 'tasks')).toContainText('没有已注册的系统任务')
  await expect(section(page, 'tasks')).toContainText('暂无等待或运行中的任务')
  await expect(section(page, 'billing')).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath('ops-empty.png') })
})

for (const theme of ['light', 'dark'] as const) {
  test(`non-Root admin never sees or requests Root sections (${theme})`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await configureStablePage(page, {
      theme,
      user: {
        role: 10,
        permissions: { admin_permissions: { channel: { read: true } } },
      },
    })
    await mockOps(page)
    const api = recordApi(page)
    const errors = collectErrors(page)
    await page.goto('/ops')
    await expect(section(page, 'upstream')).toContainText('upstream-1')
    await expect(section(page, 'quality')).toBeVisible()
    await expect(section(page, 'tasks')).toHaveCount(0)
    await expect(section(page, 'billing')).toHaveCount(0)
    await assertNoHorizontalOverflow(page)
    await page.screenshot({ path: testInfo.outputPath('ops-admin.png') })
    expect(api.has('/api/system-task/summary')).toBe(false)
    expect(api.has('/api/system-task/list')).toBe(false)
    expect(api.has('/api/gateway/metrics')).toBe(false)
    expect(errors).toEqual([])
  })
}

test('admin without channel.read gets no upstream section or request', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light', user: { role: 10 } })
  await mockOps(page)
  const api = recordApi(page)
  await page.goto('/ops')
  await expect(section(page, 'quality')).toBeVisible()
  await expect(section(page, 'upstream')).toHaveCount(0)
  expect(api.has('/api/ops/channels')).toBe(false)
})

test('members get no ops entry and are sent away from /ops', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light', user: { role: 1 } })
  await mockOps(page)
  await page.goto('/dashboard')
  const nav = page.locator('[data-top-nav="desktop"]')
  await expect(
    nav.getByRole('button', { name: '仪表盘', exact: true })
  ).toBeVisible()
  await expect(nav.getByRole('button', { name: '运维监控' })).toHaveCount(0)
  await page.goto('/ops')
  await expect(page).toHaveURL(/\/dashboard$/)
})

test('a mass breaker event scrolls inside the upstream card', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'dark' })
  await mockOps(page)
  await page.route(/\/api\/ops\/channels/, (route) =>
    route.fulfill(ok(opsChannels(50)))
  )
  await page.goto('/ops')
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

test('the window selector only reloads the overview trends', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  await mockOps(page)
  const windows: string[] = []
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (url.pathname === '/api/ops/overview')
      windows.push(url.searchParams.get('window') ?? '')
  })
  await page.goto('/ops')
  await expect(page.locator('[data-ops-gauge="cpu"]')).toBeVisible()
  await page.getByRole('radio', { name: '6 小时' }).click()
  await expect.poll(() => windows.at(-1)).toBe('6h')
  await expect(section(page, 'resources')).toContainText('趋势：6 小时')
})

test('english labels render without raw keys', async ({ page }) => {
  await configureStablePage(page, { theme: 'light' })
  await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
  await mockOps(page)
  await page.goto('/ops')
  await expect(
    page.getByRole('heading', { level: 1, name: 'Ops monitor' })
  ).toBeVisible()
  await expect(section(page, 'status')).not.toContainText('Version')
  await expect(section(page, 'status')).not.toContainText('Uptime')
  await expect(section(page, 'quality')).toContainText('Top models by traffic')
  await expect(section(page, 'tasks')).toContainText('Gateway state cleanup')
  const text = await page.locator('[data-ops-page]').innerText()
  expect(text).not.toMatch(/\b(?:ops|gateway|quality|storage|tasks)\.[a-z]/)
})
