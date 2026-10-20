import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

const active = {
  id: 101,
  name: 'OpenAI Codex Model',
  type: 1,
  supplier: 'OpenAI',
  status: 1,
  priority: 0,
  weight: 10,
  capacity_used: 0,
  capacity_total: 20,
  channel_ratio: '1',
  used_quota: '0',
  balance: 0,
  response_time: 0,
  test_time: 0,
  base_url: 'https://mock.example.test',
  models: 'gpt-5-codex',
  setting: '{}',
  settings: '{}',
}
const retired = {
  ...active,
  id: 157,
  name: 'Historical Subscription',
  type: 57,
}

for (const theme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    test(`retired subscription is read-only and deletable ${theme} ${width}`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme, authenticated: true })
      let removed = false
      const unexpected: string[] = []
      page.on('request', (request) => {
        if (/\/api\/channel\/157\/(?:codex|provider-usage)/.test(request.url()))
          unexpected.push(request.url())
      })
      await page.route(/\/api\/next\/admin\/channels(?:\?.*)?$/, (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              items: removed ? [active] : [retired, active],
              total: removed ? 1 : 2,
              page: 1,
              page_size: 20,
              type_counts: { '1': 1, '57': removed ? 0 : 1 },
            },
          },
        })
      )
      await page.route('**/api/channel/157', (route) => {
        expect(route.request().method()).toBe('DELETE')
        removed = true
        return route.fulfill({ json: { success: true, data: { id: 157 } } })
      })
      await page.route('**/api/gateway/channels/101/credentials', (route) =>
        route.fulfill({ json: { success: true, data: [] } })
      )
      await page.route('**/api/gateway/runtime/channels/101', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              channel_id: 101,
              in_flight: 0,
              queued: 0,
              capacity: 20,
              rate_status: 'unavailable',
              rate: null,
              config_version: 1,
              revision: 1,
              started_at: 0,
              draining: false,
            },
          },
        })
      )
      await page.goto('/channels')
      const row =
        width > 640
          ? page.getByRole('row').filter({ hasText: retired.name })
          : page
              .locator('[data-channel-mobile-row]')
              .filter({ hasText: retired.name })
      await expect(row).toContainText('已退役')
      await expect(row).toContainText('订阅凭据不能直接复用')
      await expect(row.getByRole('checkbox')).toBeDisabled()
      for (const name of [
        '编辑渠道',
        '测试渠道',
        '同步模型',
        '启用渠道',
        '禁用渠道',
        '渠道运行状态',
      ]) {
        await expect(
          row.getByRole('button', { name, exact: true })
        ).toHaveCount(0)
      }
      await expect(
        row.getByRole('button', { name: '删除渠道', exact: true })
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({ path: info.outputPath('retired-channel.png') })

      const activeRow =
        width > 640
          ? page.getByRole('row').filter({ hasText: active.name })
          : page
              .locator('[data-channel-mobile-row]')
              .filter({ hasText: active.name })
      await activeRow
        .getByRole('button', { name: '编辑渠道', exact: true })
        .click()
      const editor = page.getByRole('dialog')
      await expect(
        editor.getByText('gpt-5-codex', { exact: true })
      ).toBeVisible()
      await editor.getByRole('button', { name: 'OpenAI', exact: true }).click()
      await expect(
        editor.getByRole('option', { name: 'OpenAI', exact: true })
      ).toBeVisible()
      await expect(
        editor.getByRole('option', { name: /Codex|Subscription/ })
      ).toHaveCount(0)
      await editor.getByRole('option', { name: 'OpenAI', exact: true }).click()
      await editor.getByRole('button', { name: '取消', exact: true }).click()
      await row.getByRole('button', { name: '删除渠道', exact: true }).click()
      const confirm = page.getByRole('dialog')
      await expect(confirm).toContainText(retired.name)
      await confirm.getByRole('button', { name: '删除', exact: true }).click()
      await expect(row).toHaveCount(0)
      expect(removed).toBe(true)
      expect(unexpected).toEqual([])
      await assertNoHorizontalOverflow(page)
    })
  }
}
