import { mkdirSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import {
  nativeAffiliateInfo,
  nativeModelCatalog,
  nativeUsageSummary,
} from './channel-core-fixtures'
import { configureNativeTickets } from './businessFixtures'

const evidence = process.env.REN2HUB_UI_OUTPUT!
const now = Date.parse('2026-07-27T04:00:00Z') / 1000
mkdirSync(`${evidence}/screenshots`, { recursive: true })
const screens = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'mobile', width: 390, height: 844 },
] as const

async function capture(page: Page, name: string) {
  await assertNoHorizontalOverflow(page)
  await page.screenshot({
    path: `${evidence}/screenshots/${name}.png`,
    fullPage: true,
  })
}

for (const theme of ['light', 'dark'] as const)
  for (const screen of screens) {
    const key = `${screen.name}-${theme}`
    test(`audit dashboard export disabled ${key}`, async ({ page }) => {
      await page.setViewportSize(screen)
      const fixture = await configureStablePage(page, {
        theme,
        status: { password_register_enabled: true },
      })
      await page.route(/\/api\/data\/self\/summary(?:\?.*)?$/, (route) => {
        const data = nativeUsageSummary(now, false)
        data.meta.available = false
        return route.fulfill({ json: { success: true, data } })
      })
      await page.goto('/dashboard')
      await waitForStablePage(page)
      await expect(page.locator('[data-balance-card]')).toBeVisible()
      await expect(
        page.locator('[data-overview-kpi] [data-kpi-cell="requests"]')
      ).toContainText('总请求次数')
      await expect(
        page.locator('[data-overview-kpi] [data-kpi-cell="requests"]')
      ).toContainText('4')
      await expect(
        page.getByText('仪表盘数据加载失败', { exact: true })
      ).toHaveCount(0)
      await expect(
        page.getByText('当前未启用数据导出，无法统计使用情况。').first()
      ).toBeVisible()
      await capture(page, `dashboard-${key}`)
      await page.locator('[data-usage-unavailable]').scrollIntoViewIfNeeded()
      await capture(page, `dashboard-usage-${key}`)
      expect(fixture.errors).toEqual([])
      expect(fixture.missingMessages).toEqual([])
    })

    test(`audit oauth registration only ${key}`, async ({ page }) => {
      await page.setViewportSize(screen)
      const fixture = await configureStablePage(page, {
        theme,
        authenticated: false,
        status: {
          password_register_enabled: false,
          github_oauth: true,
          github_client_id: 'fixture-client',
        },
      })
      await page.goto('/sign-up?aff=AUDIT123')
      await waitForStablePage(page)
      await expect(page).toHaveURL(/\/sign-up\?aff=AUDIT123$/)
      await expect(page.locator('input[type="password"]')).toHaveCount(0)
      await expect(
        page.locator('[data-affiliate-attribution] input[type="text"]')
      ).toHaveValue('AUDIT123')
      await expect(
        page.locator('[data-affiliate-attribution] input[type="checkbox"]')
      ).not.toBeChecked()
      await expect(
        page.getByText('已关闭密码注册，请使用下方可用的第三方服务继续。', {
          exact: true,
        })
      ).toBeVisible()
      await expect(
        page.getByRole('button', { name: '使用 GitHub 继续', exact: true })
      ).toBeVisible()
      await capture(page, `registration-${key}`)
      expect(fixture.errors).toEqual([])
      expect(fixture.missingMessages).toEqual([])
    })

    test(`audit locked two factor ${key}`, async ({ page }) => {
      await page.setViewportSize(screen)
      const fixture = await configureStablePage(page, {
        theme,
        status: { password_register_enabled: true },
      })
      await page.route('**/api/user/2fa/status', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: { enabled: true, locked: true, backup_codes_remaining: 2 },
          },
        })
      )
      await page.goto('/security')
      await waitForStablePage(page)
      const locked = page.getByText('已锁定', { exact: true })
      await expect(locked).toBeVisible()
      await locked.scrollIntoViewIfNeeded()
      await capture(page, `security-${key}`)
      expect(fixture.errors).toEqual([])
      expect(fixture.missingMessages).toEqual([])
    })

    test(`audit redemption status controls ${key}`, async ({ page }) => {
      await page.setViewportSize(screen)
      const fixture = await configureStablePage(page, {
        theme,
        status: { password_register_enabled: true },
      })
      const row = {
        id: 81,
        name: '审计兑换码',
        key: 'AUDIT-REDEMPTION-2026',
        status: 1,
        quota: 5000000,
        used_user_id: 0,
        created_time: now - 3600,
        redeemed_time: 0,
        expired_time: 0,
      }
      const changes: number[] = []
      await page.route(/\/api\/redemption\/(?:\?.*)?$/, (route) => {
        if (route.request().method() === 'PUT') {
          row.status = route.request().postDataJSON().status
          changes.push(row.status)
          return route.fulfill({ json: { success: true, data: row } })
        }
        return route.fulfill({
          json: {
            success: true,
            data: { items: [row], page: 1, page_size: 20, total: 1 },
          },
        })
      })
      await page.goto('/redemption-codes')
      await waitForStablePage(page)
      const disable = page
        .getByRole('button', { name: '禁用兑换码', exact: true })
        .filter({ visible: true })
      await expect(disable).toBeVisible()
      await disable.click()
      const enable = page
        .getByRole('button', { name: '启用兑换码', exact: true })
        .filter({ visible: true })
      await expect(enable).toBeVisible()
      await enable.click()
      await expect(disable).toBeVisible()
      expect(changes).toEqual([2, 1])
      await capture(page, `redemption-${key}`)
      expect(fixture.errors).toEqual([])
      expect(fixture.missingMessages).toEqual([])
    })

    test(`audit empty console settings save ${key}`, async ({ page }) => {
      await page.setViewportSize(screen)
      const fixture = await configureStablePage(page, {
        theme,
        status: { password_register_enabled: true },
      })
      const options: Record<string, string> = {
        'console_setting.announcements': '',
        'console_setting.api_info': '',
        'console_setting.faq': '',
        'console_setting.uptime_kuma_groups': '',
        'console_setting.announcements_enabled': 'false',
      }
      const writes: unknown[] = []
      await page.route(/\/api\/option\/?(?:\?.*)?$/, (route) => {
        if (route.request().method() === 'PUT') {
          const body = route.request().postDataJSON()
          writes.push(body)
          options[body.key] = body.value
          return route.fulfill({ json: { success: true, message: '' } })
        }
        return route.fulfill({
          json: {
            success: true,
            data: Object.entries(options).map(([key, value]) => ({
              key,
              value,
            })),
          },
        })
      })
      await page.goto('/system-settings/content/console-content')
      await waitForStablePage(page)
      await expect(
        page.getByText('配置结构不正确', { exact: true })
      ).toHaveCount(0)
      await page.getByRole('switch', { name: '显示公告', exact: true }).click()
      await page.getByRole('button', { name: '保存更改', exact: true }).click()
      await expect
        .poll(() => writes)
        .toEqual([
          { key: 'console_setting.announcements_enabled', value: 'true' },
        ])
      await expect(
        page.getByRole('switch', { name: '显示公告', exact: true })
      ).toHaveAttribute('aria-checked', 'true')
      await capture(page, `settings-${key}`)
      expect(fixture.errors).toEqual([])
      expect(fixture.missingMessages).toEqual([])
    })

    test(`audit localized ticket details ${key}`, async ({ page }) => {
      await page.setViewportSize(screen)
      await configureNativeTickets(page, theme)
      await page.goto('/tickets/1')
      await waitForStablePage(page)
      await expect(
        page.getByText('模型: gpt-4.1', { exact: true })
      ).toBeVisible()
      await expect(
        page.getByText('请求: req_visual_2026', { exact: true })
      ).toBeVisible()
      await capture(page, `ticket-${key}`)
    })

    test(`audit affiliate reversals and disabled transfer ${key}`, async ({
      page,
    }) => {
      await page.setViewportSize(screen)
      const fixture = await configureStablePage(page, {
        theme,
        status: { password_register_enabled: true },
      })
      await page.route(/\/api\/user\/affiliate(?:\?.*)?$/, (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              ...nativeAffiliateInfo(),
              outstanding_offset: 500000,
              transfer_enabled: false,
            },
          },
        })
      )
      const common = {
        id: 1,
        user_id: 2,
        invitee_id: 3,
        skip_reason: '',
        source_type: 'admin_quota',
        source_id: 'operation-audit',
        base_quota: 10000000,
        rate_bps: 2000,
        reward_quota: 1000000,
        credited_quota: 500000,
        debt_offset_quota: 500000,
        debt_added_quota: 0,
        frozen_until: 0,
        thawed_at: 0,
        created_at: now,
        available_after: 1000000,
        frozen_after: 0,
        wallet_after: 5000000,
      }
      const rows = [
        { ...common, kind: 'accrue' },
        {
          ...common,
          id: 2,
          kind: 'reversal',
          source_type: 'admin_quota_reversal',
          debt_offset_quota: 0,
          debt_added_quota: 500000,
        },
        {
          ...common,
          id: 3,
          kind: 'accrue',
          reward_quota: 0,
          skip_reason: 'compliance_unconfirmed',
        },
      ]
      await page.route(/\/api\/user\/affiliate\/records(?:\?.*)?$/, (route) =>
        route.fulfill({
          json: {
            success: true,
            data: { items: rows, total: rows.length, page: 1, page_size: 20 },
          },
        })
      )
      await page.goto('/invite')
      await waitForStablePage(page)
      await expect(
        page.getByText('支付合规尚未确认，暂不可转入返利。').first()
      ).toBeVisible()
      const transferButtons = page.getByRole('button', {
        name: '转入余额',
        exact: true,
      })
      await expect(transferButtons).toHaveCount(2)
      for (const button of await transferButtons.all())
        await expect(button).toBeDisabled()
      await capture(page, `invite-${key}`)
      const reversal = page.locator('[data-record-kind="reversal"]')
      await expect(reversal).toContainText('−$2.00')
      await expect(page.getByText('待抵扣返利', { exact: true })).toBeVisible()
      await reversal.scrollIntoViewIfNeeded()
      await capture(page, `invite-ledger-${key}`)
      expect(fixture.errors).toEqual([])
      expect(fixture.missingMessages).toEqual([])
    })

    test(`audit multidimensional model prices ${key}`, async ({ page }) => {
      await page.setViewportSize(screen)
      const fixture = await configureStablePage(page, {
        theme,
        status: { password_register_enabled: true },
      })
      const data = nativeModelCatalog(now)
      const prices = {
        input: '2.5',
        output: '10',
        cache: '0.5',
        cache_create: '3.125',
        cache_create_1h: '5',
        image: '8',
        image_output: '20',
        image_cache: '1',
        audio_input: '30',
        audio_output: '60',
      }
      data.models = [
        {
          ...data.models[0]!,
          description: '多维价格验收模型',
          pricing_values: prices,
        },
      ]
      data.offers = data.offers
        .filter((offer) => offer.model_id === 'gpt-native')
        .map((offer) => ({ ...offer, pricing_values: prices }))
      await page.route('**/api/user/model-catalog', (route) =>
        route.fulfill({ json: { success: true, data } })
      )
      await page.goto('/models')
      await waitForStablePage(page)
      await page
        .locator('[data-model-card]')
        .first()
        .locator('.market-card-hit')
        .click()
      const dialog = page.getByRole('dialog')
      const panel = dialog.locator('.market-detail-price-panel')
      await panel.scrollIntoViewIfNeeded()
      for (const key of [
        'cacheRead',
        'cacheWrite',
        'cacheWrite1h',
        'image',
        'imageOutput',
        'imageCache',
        'audioInput',
        'audioOutput',
      ])
        await expect(
          panel.locator(`[data-price-dimension="${key}"]`)
        ).toBeVisible()
      await capture(page, `models-${key}`)
      expect(fixture.errors).toEqual([])
      expect(fixture.missingMessages).toEqual([])
    })
  }
