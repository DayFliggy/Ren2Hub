import { mkdirSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import {
  performanceFixture,
  qualityFixture,
} from '../../src/api/__tests__/opsFixtures'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

const output = process.env.REN2HUB_UI_OUTPUT!
mkdirSync(`${output}/screenshots`, { recursive: true })
const screens = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'mobile', width: 390, height: 844 },
] as const

async function capture(page: Page, name: string) {
  await assertNoHorizontalOverflow(page)
  await page.screenshot({
    path: `${output}/screenshots/${name}.png`,
    fullPage: true,
  })
}

for (const theme of ['light', 'dark'] as const) {
  for (const screen of screens) {
    const key = `${screen.name}-${theme}`
    for (const kind of ['drawing', 'task'] as const) {
      test(`query failure clears old ${kind} results and retry recovers ${key}`, async ({
        page,
      }) => {
        await page.setViewportSize(screen)
        await configureStablePage(page, { theme })
        let failed = false
        let recovered = false
        const endpoint = kind === 'drawing' ? '/api/mj/self' : '/api/task/self'
        await page.route(`**${endpoint}?*`, (route) => {
          if (failed && !recovered)
            return route.fulfill({
              json: { success: false, message: '隔离测试查询失败' },
            })
          const marker = recovered ? 'recovered-result' : 'original-result'
          return route.fulfill({
            json: {
              success: true,
              data: {
                page: 1,
                page_size: 10,
                total: 1,
                items: [
                  {
                    id: 1,
                    [kind === 'drawing' ? 'mj_id' : 'task_id']: marker,
                    submit_time:
                      kind === 'drawing' ? 1785124800000 : 1785124800,
                    quota: 1000,
                    status: 'SUCCESS',
                    platform: 'test-plugin',
                  },
                ],
              },
            },
          })
        })
        await page.goto(`/usage-logs/${kind}`)
        await expect(
          page.getByText('original-result').filter({ visible: true })
        ).toBeVisible()
        failed = true
        await page
          .getByRole('searchbox', { name: '按任务 ID 搜索…', exact: true })
          .fill('new-filter')
        const error = page
          .getByRole('alert')
          .filter({ visible: true })
          .filter({ hasText: '隔离测试查询失败' })
          .first()
        await expect(error).toBeVisible()
        await expect(page.getByText('original-result')).toHaveCount(0)
        await capture(page, `logs-${kind}-failure-${key}`)
        recovered = true
        await error.getByRole('button', { name: '重试', exact: true }).click()
        await expect(
          page.getByText('recovered-result').filter({ visible: true })
        ).toBeVisible()
        await expect(
          page.getByRole('alert').filter({ hasText: '隔离测试查询失败' })
        ).toHaveCount(0)
        await waitForStablePage(page)
      })
    }

    for (const role of [1, 100]) {
      test(`dashboard truthful limits and resource permission role ${role} ${key}`, async ({
        page,
      }) => {
        await page.setViewportSize(screen)
        await configureStablePage(page, { theme, user: { role } })
        let performanceRequests = 0
        page.on('request', (request) => {
          if (new URL(request.url()).pathname === '/api/performance/stats')
            performanceRequests++
        })
        await page.goto('/dashboard')
        const limits = page.locator('[data-kpi-cell="requestLimits"]')
        await expect(limits).toContainText('5 分钟窗口')
        await expect(limits).toContainText('总请求：100')
        await expect(limits).toContainText('成功请求：80')
        await waitForStablePage(page)
        await expect(page.locator('[data-system-status-card]')).toHaveCount(
          role === 100 ? 1 : 0
        )
        if (role === 100) {
          await expect(page.locator('[data-resource-sampled-at]')).toBeVisible()
          expect(performanceRequests).toBeGreaterThan(0)
        } else expect(performanceRequests).toBe(0)
        await expect(page.getByText('炼金室', { exact: true })).toHaveCount(0)
        await expect(page.getByText('统一折扣', { exact: true })).toHaveCount(0)
        await capture(page, `dashboard-role-${role}-${key}`)
      })
    }

    test(`ops uses backend coverage and window with explicit missing values ${key}`, async ({
      page,
    }) => {
      await page.setViewportSize(screen)
      await configureStablePage(page, { theme })
      const windows: string[] = []
      await page.route('**/api/performance/quality?*', (route) =>
        route.fulfill({ json: { success: true, data: qualityFixture() } })
      )
      await page.route('**/api/performance/stats?*', (route) => {
        const window = new URL(route.request().url()).searchParams.get(
          'window'
        )!
        windows.push(window)
        const data = performanceFixture()
        data.observability.series.window = window as '1h' | '6h' | '24h'
        data.observability.series.bucket_seconds = window === '6h' ? 300 : 60
        return route.fulfill({ json: { success: true, data } })
      })
      await page.goto('/ops')
      await expect(page.locator('[data-ops-coverage]')).toHaveText(
        '已测 1 / 4 个启用检查项'
      )
      await expect(
        page.locator('[data-ops-gauge="cpu"] [data-ops-unavailable]')
      ).toBeVisible()
      await expect(page.locator('[data-ops-storage="redis"]')).toContainText(
        '未启用'
      )
      await expect(
        page.getByText('应用请求／响应吞吐', { exact: true })
      ).toBeVisible()
      await page.getByRole('radio', { name: '6 小时', exact: true }).click()
      await expect.poll(() => windows.at(-1)).toBe('6h')
      await waitForStablePage(page)
      await capture(page, `ops-${key}`)
      await page.locator('[data-ops-section="resources"]').screenshot({
        path: `${output}/screenshots/ops-resources-${key}.png`,
      })
      await page.locator('[data-ops-storage="redis"]').screenshot({
        path: `${output}/screenshots/ops-redis-${key}.png`,
      })
    })
  }
}
