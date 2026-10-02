import { expect, test, type Page } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

const now = Math.floor(new Date('2026-07-27T12:00:00+08:00').getTime() / 1000)
const channel = {
  id: 157,
  name: 'Local Codex Fixture',
  type: 57,
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
  models: 'gpt-local',
  setting: '{}',
  settings: '{}',
}
const unknown = {
  channel_id: 157,
  provider: 'codex',
  state: 'unknown',
  plan_type: '',
  windows: [],
  fetched_at: 0,
  expires_at: 0,
  last_attempt_at: 0,
  next_refresh_at: 0,
  upstream_status: 0,
  error_code: '',
  auto_refresh: false,
}
const known = {
  ...unknown,
  state: 'known',
  plan_type: 'plus',
  fetched_at: now,
  expires_at: now + 300,
  last_attempt_at: now,
  next_refresh_at: now + 60,
  windows: [
    {
      name: 'rate_limit_primary',
      used_percent: 55.5,
      limit_seconds: 18000,
      reset_at: now + 18000,
    },
    {
      name: 'rate_limit_secondary',
      used_percent: null,
      limit_seconds: null,
      reset_at: null,
    },
  ],
}

async function prepare(
  page: Page,
  theme: 'light' | 'dark',
  mobile: boolean,
  canOperate = true
) {
  await page.setViewportSize(
    mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
  )
  await configureStablePage(page, {
    theme,
    authenticated: true,
    ...(canOperate
      ? {}
      : {
          user: {
            role: 10,
            permissions: {
              admin_permissions: {
                channel: { read: true, write: true, operate: false },
              },
            },
          },
        }),
  })
  await page.route(/\/api\/next\/admin\/channels(?:\?.*)?$/, (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          items: [channel],
          total: 1,
          page: 1,
          page_size: 20,
          type_counts: { '57': 1 },
        },
      },
    })
  )
  await page.route('**/api/gateway/channels/157/credentials', (route) =>
    route.fulfill({ json: { success: true, data: [] } })
  )
  await page.route('**/api/gateway/runtime/channels/157', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          channel_id: 157,
          in_flight: 0,
          queued: 0,
          capacity: 20,
          rate_status: 'unavailable',
          rate: null,
          config_version: 1,
          revision: 1,
          started_at: now,
          draining: false,
        },
      },
    })
  )
}

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`Codex usage ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }) => {
      await prepare(page, theme, mobile)
      let refreshed = 0
      let automatic = false
      await page.route('**/api/channel/157/provider-usage', (route) =>
        route.fulfill({ json: { success: true, data: unknown } })
      )
      await page.route('**/api/channel/157/provider-usage/refresh', (route) => {
        refreshed++
        return route.fulfill({
          json: {
            success: true,
            data:
              refreshed === 1
                ? known
                : {
                    ...known,
                    auto_refresh: automatic,
                    state: 'error',
                    last_attempt_at: now + 301,
                    next_refresh_at: now + 421,
                    error_code: 'rate_limited',
                    upstream_status: 429,
                  },
          },
        })
      })
      await page.route(
        '**/api/channel/157/provider-usage/settings',
        (route) => {
          automatic = route.request().postDataJSON().auto_refresh
          return route.fulfill({
            json: {
              success: true,
              data: { ...known, auto_refresh: automatic },
            },
          })
        }
      )
      await page.goto('/channels')
      await page.locator('[aria-label="编辑渠道"]:visible').first().click()
      const dialog = page.getByRole('dialog', { name: '编辑渠道', exact: true })
      const usage = dialog.locator('section').filter({
        has: page.getByRole('heading', {
          name: 'Codex 供应商用量',
          exact: true,
        }),
      })
      await expect(usage).toContainText('尚未采集用量')
      await expect(
        usage.getByRole('checkbox', { name: '每 5 分钟自动采集' })
      ).not.toBeChecked()
      expect(refreshed).toBe(0)
      const refresh = usage.getByRole('button', { name: '刷新供应商用量' })
      await refresh.click()
      await expect(usage).toContainText('已用 55.5%')
      await expect(usage).toContainText('次要用量窗口')
      await expect(refresh).toBeDisabled()
      expect(refreshed).toBe(1)
      await page.evaluate(
        (time) => {
          Date.now = () => time
        },
        (now + 61) * 1000
      )
      await expect(refresh).toBeEnabled()
      await usage.getByRole('checkbox', { name: '每 5 分钟自动采集' }).click()
      await expect.poll(() => automatic).toBe(true)
      await expect(
        usage.getByRole('checkbox', { name: '每 5 分钟自动采集' })
      ).toBeChecked()
      await page.evaluate(
        (time) => {
          Date.now = () => time
        },
        (now + 301) * 1000
      )
      await expect(usage).toContainText('已过期')
      await refresh.click()
      await expect(usage).toContainText('供应商限制了刷新频率')
      await expect(usage).toContainText('刷新失败')
      await expect(refresh).toBeDisabled()
      await expect(usage).toContainText('已用 55.5%')
      await assertNoHorizontalOverflow(page)
    })
  }
}

test('Codex usage hides operation controls without channel operate permission', async ({
  page,
}) => {
  await prepare(page, 'light', true, false)
  const writes: string[] = []
  await page.route('**/api/channel/157/provider-usage**', (route) => {
    if (route.request().method() !== 'GET') writes.push(route.request().url())
    return route.fulfill({ json: { success: true, data: known } })
  })
  await page.goto('/channels')
  await page.locator('[aria-label="编辑渠道"]:visible').first().click()
  const usage = page
    .getByRole('dialog')
    .locator('section')
    .filter({
      has: page.getByRole('heading', { name: 'Codex 供应商用量', exact: true }),
    })
  await expect(usage).toContainText('已用 55.5%')
  await expect(
    usage.getByRole('button', { name: '刷新供应商用量' })
  ).toHaveCount(0)
  await expect(usage.getByRole('checkbox')).toHaveCount(0)
  expect(writes).toEqual([])
  await assertNoHorizontalOverflow(page)
})
