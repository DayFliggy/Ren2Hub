import { expect, test, type Page, type Route } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

const credential = '6a302bce-9ba5-49af-aef0-afcf5971fc16'
const token = {
  id: 7,
  name: 'Gateway token',
  key: 'sk-test********view',
  type: 'auto',
  status: 1,
  conversion_policy: 'safe',
  used_quota: 0,
  remain_quota: 0,
  unlimited_quota: true,
  model_limits_enabled: false,
  model_limits: '',
  allow_ips: '',
  expired_time: -1,
  created_time: 1,
}
const policy = {
  user_id: 42,
  group: 'team',
  config_version: 1,
  override: { concurrency: null, wait_ms: null, queue_size: null },
  effective: {
    concurrency: 10,
    wait_ms: 5000,
    queue_size: 20,
    channel_queue_size: 20,
  },
  sources: {
    concurrency: 'system',
    wait_ms: 'system',
    queue_size: 'system',
    channel_queue_size: 'system',
  },
}
const rate = {
  rpm_used: 6,
  tokens_used: 12000,
  tokens_reserved: 8000,
  cooldown_ms: 0,
}

async function fixture(page: Page) {
  const group = {
    id: 4,
    profile_id: 1,
    name: 'Primary',
    kind: 'manual',
    enabled: true,
    position: 0,
    entries: [
      {
        id: 9,
        group_id: 4,
        channel_id: 12,
        source: 'platform',
        enabled: true,
        position: 0,
        weight: 100,
      },
    ],
    policy: {
      group_id: 4,
      load_balance: false,
      max_ratio: 1,
      retry_mode: 'next_channel',
      max_same_resource_attempts: 0,
      max_failover_attempts: 1,
      sticky: false,
    },
  }
  const profile = {
    profile: {
      id: 1,
      user_id: 42,
      token_id: 7,
      mode: 'manual',
      active_group_id: 4,
      version: 1,
      status: 1,
      created_at: 1,
      updated_at: 1,
    },
    groups: [group],
  }
  let config = {
    total_queue_size: 1024,
    retry_budget_enabled: true,
    retry_budget_percent: 10,
    retry_budget_burst: 20,
    version: 1,
    user_concurrency: 10,
    wait_ms: 5000,
    user_queue_size: 20,
    channel_queue_size: 20,
    groups: { team: { concurrency: 3, wait_ms: null, queue_size: null } },
    channel_rates: { '12': { rpm: 60, tpm: 100000 } },
    credential_rates: { [credential]: { rpm: 30, tpm: 50000 } },
  }
  const result = (route: Route, data: unknown) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ success: true, message: '', data }),
    })
  await page.route('**/api/gateway/**', (route) => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/config')) {
      if (route.request().method() === 'PUT')
        config = {
          ...route.request().postDataJSON(),
          version: config.version + 1,
        }
      return result(route, config)
    }
    if (path.endsWith('/credentials'))
      return result(route, [{ id: credential }])
    if (path.includes('/runtime/channels/'))
      return result(route, {
        channel_id: 12,
        capacity: 20,
        config_version: 1,
        revision: 1,
        started_at: 1,
        draining: false,
        rate_status: 'available',
        in_flight: 2,
        queued: 1,
        rate,
      })
    if (path.includes('/runtime/credentials/')) return result(route, rate)
    if (path.includes('/runtime/') || path.endsWith('/self/runtime'))
      return result(route, {
        in_flight: 2,
        queued: 1,
        capacity: 10,
        config_version: 1,
        revision: 1,
        started_at: 1,
        draining: false,
      })
    if (route.request().method() === 'PUT')
      return result(route, route.request().postDataJSON())
    return result(route, policy)
  })
  await page.route('**/api/token/7', (route) => result(route, token))
  await page.route('**/api/routing/**', (route) => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/profiles')) return result(route, [profile])
    if (path.endsWith('/preview')) {
      const input = route.request().postDataJSON()
      if (input.request?.fail_preview)
        return route.fulfill({
          status: 503,
          contentType: 'application/json',
          body: JSON.stringify({
            success: false,
            message: 'Preview unavailable',
          }),
        })
      const blocked = input.request?.parallel_tool_calls === false
      const diagnostics = blocked
        ? [
            {
              code: 'serial_tools_unsupported',
              path: 'parallel_tool_calls',
              message: 'Gemini conversion cannot enforce serial tool execution',
              severity: 'error',
              from: 'openai',
              to: 'gemini',
            },
          ]
        : []
      return result(route, {
        profile_id: 1,
        profile_version: 1,
        request_model: input.model,
        normalized_model: input.model,
        path: input.path,
        endpoint_type: 'openai',
        active_group: group,
        policy: group.policy,
        entries: [
          {
            entry_id: 9,
            channel_id: 12,
            position: 0,
            weight: 100,
            request_model: input.model,
            actual_model: 'gemini-2.5-flash',
            lab_slug: 'google',
            snapshot_version: 1,
            catalog_version: 'catalog-1',
            capability_state: 'eligible',
            diagnostics,
            filter_reason: blocked ? 'conversion_unavailable' : undefined,
            conversion_plan: {
              from: 'openai',
              to: 'gemini',
              native: false,
              policy: 'safe',
              stream_supported: true,
              steps: [
                { converter: 'openai_to_gemini', from: 'openai', to: 'gemini' },
              ],
              diagnostics,
            },
            health: {
              state: 'closed',
              failure_count: 0,
              cooldown_until: 0,
              health_epoch: 1,
              last_latency_ms: 120,
              first_token_latency_ms: 45,
              updated_at: 1,
            },
          },
        ],
        candidate_channel_ids: blocked ? [] : [12],
        preferred_channel_id: blocked ? undefined : 12,
        selection_mode: 'ordered',
        filter_reason_counts: blocked ? { conversion_unavailable: 1 } : {},
        has_mixed: false,
        runtime_recheck_required: true,
        runtime_recheck_reasons: ['price_qualification'],
        live_selection: false,
        conversion_policy: 'safe',
        request_requirements: {
          format: 'openai',
          model: input.model,
          stream: input.request?.stream === true,
          operation: 'text',
          features: blocked ? ['serial_tools'] : [],
          native_required: false,
          native_state: [],
          tools: [],
          diagnostics: [],
        },
      })
    }
    return result(
      route,
      path.endsWith('/catalog')
        ? { catalog_version: '', catalog_versions: [], items: [] }
        : []
    )
  })
  await page.route('**/api/next/admin/channels?*', (route) =>
    result(route, {
      items: [
        {
          id: 12,
          name: 'Gateway channel',
          type: 1,
          supplier: 'OpenAI',
          status: 1,
          priority: 1,
          weight: 10,
          capacity_used: 2,
          capacity_total: 20,
          channel_ratio: '1',

          used_quota: 0,
          balance: 0,
          response_time: 20,
          test_time: 0,
          base_url: '',
          models: 'gpt-4.1',
          model_mapping: '',
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      type_counts: { '1': 1 },
    })
  )
}

for (const theme of ['light', 'dark'] as const) {
  for (const viewport of [
    { name: 'desktop', width: 1440, height: 1000 },
    { name: 'mobile', width: 390, height: 844 },
  ]) {
    test(`${theme} ${viewport.name} gateway controls`, async ({ page }) => {
      await page.setViewportSize(viewport)
      await configureStablePage(page, { theme, authenticated: true })
      await fixture(page)
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      await page.goto('/system-settings/security/rate-limit')
      await waitForStablePage(page)
      const config = page.getByTestId('gateway-config-panel')
      await expect(
        config.getByRole('heading', { name: '网关准入', exact: true })
      ).toBeVisible()
      await config
        .getByLabel('并发数（0 不限）', { exact: true })
        .first()
        .fill('11')
      await config.getByRole('button', { name: '保存限制' }).click()
      await expect(config.getByText('版本 2')).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await config
        .getByRole('heading', { name: '网关准入', exact: true })
        .scrollIntoViewIfNeeded()
      await page.screenshot({
        path: `output/gateway-${theme}-${viewport.name}-config.png`,
      })

      await page.goto('/users')
      await waitForStablePage(page)
      await page
        .getByRole('button', { name: '编辑用户', exact: true })
        .filter({ visible: true })
        .first()
        .click()
      const user = page.getByTestId('gateway-user-panel')
      await expect(
        user.getByText('系统默认', { exact: true }).first()
      ).toBeVisible()
      await user.getByRole('checkbox').first().uncheck()
      await user.getByRole('button', { name: '保存限制' }).click()
      await assertNoHorizontalOverflow(page)
      await user.screenshot({
        path: `output/gateway-${theme}-${viewport.name}-user.png`,
      })
      await page.keyboard.press('Escape')

      await page.goto('/channels')
      await waitForStablePage(page)
      await page
        .getByRole('button', { name: '渠道运行状态', exact: true })
        .filter({ visible: true })
        .first()
        .click()
      const channel = page.getByTestId('gateway-channel-panel')
      await expect(channel.getByText('2 / 20', { exact: true })).toBeVisible()
      await channel
        .getByLabel('凭据 ID', { exact: true })
        .first()
        .selectOption(credential)
      await expect(
        channel.getByText('12000', { exact: true }).first()
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await channel.screenshot({
        path: `output/gateway-${theme}-${viewport.name}-channel.png`,
      })
      await page.route('**/api/gateway/runtime/channels/12', (route) =>
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              channel_id: 12,
              capacity: 20,
              in_flight: 2,
              queued: 1,
              config_version: 2,
              revision: 9,
              started_at: 1700000000,
              draining: true,
              rate: null,
              rate_status: 'unavailable',
            },
          }),
        })
      )
      await channel
        .getByRole('button', { name: '重新加载', exact: true })
        .first()
        .click()
      await expect(channel.getByText('2 / 20', { exact: true })).toBeVisible()
      await expect(
        channel.getByText('供应商限流状态暂不可用；并发数据仍为本地实际值。', {
          exact: true,
        })
      ).toBeVisible()
      await expect(
        channel.getByText('实例正在排空，暂不接收新生成请求。', { exact: true })
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await channel.screenshot({
        path: `output/gateway-${theme}-${viewport.name}-unavailable.png`,
      })
      await page.keyboard.press('Escape')

      await page.goto('/keys/7/routing')
      await waitForStablePage(page)
      await expect(
        page.getByText('安全：保留请求语义', { exact: true })
      ).toBeVisible()
      await page.getByRole('button', { name: '编辑', exact: true }).click()
      await page
        .getByRole('dialog')
        .getByLabel('协议转换策略')
        .selectOption('strict')
      await expect(page.locator('[name="conversion-policy"]')).toHaveValue(
        'strict'
      )
      await assertNoHorizontalOverflow(page)
      await page.getByRole('dialog').screenshot({
        path: `output/gateway-${theme}-${viewport.name}-token.png`,
      })
      await page.keyboard.press('Escape')
      await expect(page.getByRole('dialog')).toHaveCount(0)

      await page.getByPlaceholder('gpt-5', { exact: true }).fill('gpt-4.1')
      const sample = page.getByTestId('route-preview-request')
      await sample.fill('{"stream":true,"parallel_tool_calls":false,"top_p":0}')
      const request = page.waitForRequest((request) =>
        request.url().endsWith('/tokens/7/preview')
      )
      await page
        .getByRole('button', { name: '运行 Preview', exact: true })
        .click()
      expect((await request).postDataJSON().request).toEqual({
        stream: true,
        parallel_tool_calls: false,
        top_p: 0,
      })
      await expect(
        page.getByTestId('route-preview-requirements')
      ).toContainText('serial_tools')
      await expect(page.getByTestId('route-preview-conversion')).toContainText(
        'gemini'
      )
      await expect(page.getByTestId('route-preview-diagnostics')).toContainText(
        'parallel_tool_calls'
      )
      await assertNoHorizontalOverflow(page)
      await page
        .getByTestId('route-preview-conversion')
        .scrollIntoViewIfNeeded()
      await page.screenshot({
        path: `output/gateway-${theme}-${viewport.name}-preview.png`,
      })
      await sample.fill('{}')
      await expect(page.getByTestId('route-preview-conversion')).toHaveCount(0)
      await page
        .getByRole('button', { name: '运行 Preview', exact: true })
        .click()
      await expect(page.getByTestId('route-preview-conversion')).toBeVisible()
      await expect(page.getByTestId('route-preview-diagnostics')).toHaveCount(0)
      await sample.fill('{"fail_preview":true}')
      await page
        .getByRole('button', { name: '运行 Preview', exact: true })
        .click()
      await expect(page.getByTestId('route-preview-conversion')).toHaveCount(0)
      await expect(
        page.getByText('Preview unavailable', { exact: true })
      ).toBeVisible()
      expect(errors).toEqual([])
    })
  }
}
