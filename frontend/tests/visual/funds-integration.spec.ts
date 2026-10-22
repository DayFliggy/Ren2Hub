import { expect, test, type Page } from '@playwright/test'
import { nativeUsageSummary } from './channel-core-fixtures'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'
import {
  configureBusinessPage,
  configureNativeTickets,
} from './businessFixtures'

const NOW = Math.floor(new Date('2026-07-27T12:00:00+08:00').getTime() / 1000)
const viewports = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'mobile', width: 390, height: 844 },
] as const

/** Serves every usage summary and records its query for assertions. */
async function recordSummaries(page: Page): Promise<URLSearchParams[]> {
  const queries: URLSearchParams[] = []
  await page.route(/\/api\/data\/self\/summary(?:\?.*)?$/, (route) => {
    const query = new URL(route.request().url()).searchParams
    queries.push(query)
    const comparison = query.get('include_comparison') === 'true'
    return route.fulfill({
      json: { success: true, data: nativeUsageSummary(NOW, comparison) },
    })
  })
  return queries
}

function affiliate(codeEnabled: boolean, programEnabled: boolean) {
  return {
    code: 'VISUAL',
    code_enabled: codeEnabled,
    invited: 0,
    effective_rate_bps: 2000,
    effective_rate_percent: 20,
    transferable: 0,
    frozen_reward: 0,
    reward_total: 0,
    policy: { enabled: programEnabled, freeze_hours: 24 },
    monthly_series: [],
  }
}

for (const theme of ['light', 'dark'] as const satisfies VisualTheme[]) {
  for (const viewport of viewports) {
    const name = `${theme} ${viewport.name}`

    test(`${name} dashboard keeps reads inside a 45-day log range`, async ({
      page,
    }) => {
      await page.setViewportSize(viewport)
      await configureStablePage(page, {
        theme,
        authenticated: true,
        status: { log_max_range_days: 45 },
      })
      const queries = await recordSummaries(page)
      await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)

      // The heatmap reads only the newest 45 days and marks the rest.
      const card = page.locator('[data-usage-distribution]')
      await card
        .getByRole('radiogroup', { name: '统计周期' })
        .getByRole('radio', { name: '年', exact: true })
        .click()
      await expect(card.locator('[data-usage-date]')).toHaveCount(364)
      await expect(
        card.locator('[data-usage-date][data-usage-uncovered]')
      ).toHaveCount(364 - 45)
      await expect(
        card.locator('[data-usage-date="2026-06-13"]')
      ).not.toHaveAttribute('data-usage-uncovered')
      expect(queries.map((query) => query.get('start'))).toContain('2026-06-13')
      expect(queries.map((query) => query.get('include_comparison'))).toEqual(
        queries.map(() => 'false')
      )
      await assertNoHorizontalOverflow(page)

      // 30 days plus its prior window exceeds 45 days; 7 days fits.
      await page.getByRole('tab', { name: '统计数据', exact: true }).click()
      const unavailable = page.locator('[data-trend-comparison-unavailable]')
      await expect(unavailable.first()).toBeVisible()
      expect(queries.at(-1)?.get('range')).toBe('30d')
      expect(queries.at(-1)?.get('include_comparison')).toBe('false')
      await assertNoHorizontalOverflow(page)
      await page.getByRole('radio', { name: '近 7 天', exact: true }).click()
      await expect(page.locator('[data-trend-spend-delta]')).toBeVisible()
      await expect(unavailable).toHaveCount(0)
      expect(queries.at(-1)?.get('range')).toBe('7d')
      expect(queries.at(-1)?.get('include_comparison')).toBe('true')
    })

    test(`${name} dashboard keeps today and the balance past a 20-day log range`, async ({
      page,
    }) => {
      await page.setViewportSize(viewport)
      await configureStablePage(page, {
        theme,
        authenticated: true,
        status: { log_max_range_days: 20 },
      })
      const queries = await recordSummaries(page)
      await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)

      // The 30-day window is skipped, not shortened; its cards name the limit.
      const notice = '近 30 天超出 20 天的日志查询范围'
      await expect(
        page.locator('[data-token-trend-unavailable]')
      ).toContainText(notice)
      await expect(
        page.locator('[data-model-distribution-unavailable]')
      ).toContainText(notice)
      await expect(
        page.locator('[data-kpi-cell="tokens"] .display-number')
      ).toHaveText('1.2K')
      await expect(page.locator('[data-balance-card]')).toBeVisible()
      await expect(page.getByText('仪表盘数据加载失败')).toHaveCount(0)
      const ranges = queries.map((query) => query.get('range'))
      expect(ranges).toContain('today')
      expect(ranges).not.toContain('30d')
      await assertNoHorizontalOverflow(page)
    })

    test(`${name} wallet shows the display currency and skips an out-of-range month`, async ({
      page,
    }) => {
      await page.setViewportSize(viewport)
      await configureStablePage(page, {
        theme,
        authenticated: true,
        status: { quota_display_type: 'CNY', log_max_range_days: 20 },
      })
      const queries = await recordSummaries(page)
      await page.goto('/wallet', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)

      // 5,000,000 quota is $10, shown as ¥73.00 at the 7.3 rate.
      const balance = page.locator('[aria-busy] > p').first()
      await expect(balance).toHaveText(/^¥\s*73\s*\.00$/)
      await expect(page.getByText('本月超出日志查询范围').first()).toBeVisible()
      expect(queries.map((query) => query.get('range'))).toEqual(['today'])
      await assertNoHorizontalOverflow(page)
    })

    test(`${name} ticket management opens a deleted requester's ticket`, async ({
      page,
    }) => {
      await page.setViewportSize(viewport)
      await configureNativeTickets(page, theme, { userDeleted: true })
      await page.goto('/ticket-management/1', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)

      const workspace = page.getByLabel('工单处理区')
      await expect(
        workspace.getByRole('heading', {
          name: 'Production request investigation',
          exact: true,
        })
      ).toBeVisible()
      await expect(
        workspace.getByText('用户已删除', { exact: true })
      ).toBeVisible()
      if (viewport.name === 'mobile')
        await page.getByRole('button', { name: '返回列表' }).click()
      const queue = page.getByRole('complementary', { name: '工单队列' })
      await expect(queue.getByText('用户已删除', { exact: true })).toBeVisible()
      await assertNoHorizontalOverflow(page)
    })

    for (const [state, codeEnabled, programEnabled, notice] of [
      ['code', false, true, '你的邀请码已被停用，请联系管理员。'],
      ['program', true, false, '邀请返利计划已停用，暂时不能邀请新用户注册。'],
    ] as const) {
      test(`${name} invite blocks sharing while the ${state} is disabled`, async ({
        page,
      }) => {
        await page.setViewportSize(viewport)
        await configureBusinessPage(page, theme)
        await page.route('**/api/user/affiliate?*', (route) =>
          route.fulfill({
            json: {
              success: true,
              data: affiliate(codeEnabled, programEnabled),
            },
          })
        )
        await page.route('**/api/user/affiliate/invitees?*', (route) =>
          route.fulfill({
            json: {
              success: true,
              data: { items: [], page: 1, page_size: 20, total: 0 },
            },
          })
        )
        await page.goto('/invite', { waitUntil: 'domcontentloaded' })
        await waitForStablePage(page)

        await expect(
          page.getByRole('status').filter({ hasText: notice })
        ).toBeVisible()
        for (const label of [
          '复制邀请码',
          '复制链接',
          '分享到 X',
          '分享到 Telegram',
          '通过邮件分享',
        ]) {
          await expect(
            page.getByRole('button', { name: label, exact: true })
          ).toBeDisabled()
        }
        await assertNoHorizontalOverflow(page)
      })
    }

    test(`${name} orders filter other providers and flag a deleted owner`, async ({
      page,
    }) => {
      await page.setViewportSize(viewport)
      await configureStablePage(page, { theme, authenticated: true })
      const order = {
        id: 2,
        trade_no: 'PPvisual',
        user_id: 9,
        username: 'removed.buyer',
        email: '',
        amount: 10,
        money: 73,
        expected_quota: 5000000,
        credited_quota: 5000000,
        currency: 'CNY',
        pay_amount: '73.00',
        payment_provider: 'paypal',
        payment_method: 'paypal',
        status: 'success',
        create_time: 1785114000,
        complete_time: 1785114100,
      }
      const listQueries: URLSearchParams[] = []
      await page.route(/\/api\/user\/topup(?:\?.*)?$/, (route) => {
        listQueries.push(new URL(route.request().url()).searchParams)
        return route.fulfill({
          json: {
            success: true,
            data: { items: [order], total: 1, page: 1, page_size: 20 },
          },
        })
      })
      await page.route('**/api/user/topup/ranking?*', (route) =>
        route.fulfill({ json: { success: true, data: { items: [] } } })
      )
      await page.route('**/api/user/topup/admin/detail/PPvisual', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: { ...order, user_deleted: true, gmpay: null },
          },
        })
      )
      await page.goto('/orders', { waitUntil: 'domcontentloaded' })
      await page.getByRole('tab', { name: '订单列表', exact: true }).click()
      await page.getByRole('combobox', { name: '支付服务商' }).click()
      await page.getByRole('option', { name: '其他', exact: true }).click()
      await expect.poll(() => listQueries.at(-1)?.get('method')).toBe('other')
      await page
        .getByRole('button', { name: '查看订单', exact: true })
        .filter({ visible: true })
        .click()
      const dialog = page.getByRole('dialog')
      await expect(
        dialog.getByText('用户已删除', { exact: true })
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
    })
  }
}
