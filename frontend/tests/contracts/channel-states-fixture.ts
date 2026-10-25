import { expect, type Page } from '@playwright/test'
import { nativeUser } from './native-core-fixture'

const now = 1785124800
export const channelLab = 'UI Regression'

// Native /api/channel/ and provider-status shapes; no legacy dashboard DTOs.
export async function channelStatesFixture(
  page: Page,
  advanced = false,
  holdFetchedModels = false
) {
  let releaseFetchedModels!: () => void
  const fetchedModelsReady = new Promise<void>((resolve) => {
    releaseFetchedModels = resolve
  })
  if (!holdFetchedModels) releaseFetchedModels()
  const channel = {
    id: advanced ? 58 : 62,
    name: advanced ? 'UI advanced channel' : 'UI vLLM channel',
    type: advanced ? 58 : 62,
    status: 1,
    channel_ratio: '0.125',
    max_concurrency: 8,
    in_flight: 3,
    priority: 2,
    weight: 4,
    used_quota: 500000,
    balance: 12.5,
    response_time: 120,
    test_time: now,
    base_url: 'https://provider.example.test',
    models: 'contract-chat',
    model_mapping: '',
    other_info: '',
    setting: '{}',
    settings: JSON.stringify({
      advanced_custom: {
        advanced_routes: [
          {
            incoming_path: '/v1/chat/completions',
            upstream_path: '/v1/chat/completions',
            converter: 'none',
            auth: {
              type: 'header',
              name: 'Authorization',
              value: 'Bearer {api_key}',
            },
          },
        ],
      },
    }),
    lab: { id: 'ui-regression', name: channelLab, source: 'manual' },
    channel_info: {
      is_multi_key: true,
      multi_key_mode: 'polling',
      multi_key_size: 3,
      multi_key_status_list: { '1': 2, '2': 3 },
    },
  }
  const requests: Array<{ path: string; model: string | null }> = []
  const applied: unknown[] = []
  await page.route('**/api/channel/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    const ok = (data: unknown) =>
      route.fulfill({ json: { success: true, data } })
    if (request.method() === 'GET') {
      if (path === '/api/channel/')
        return ok({
          items: [channel],
          total: 1,
          type_counts: { [channel.type]: 1 },
        })
      if (path === '/api/channel/labs')
        return ok({ items: [{ ...channel.lab, count: 1 }] })
      if (path === `/api/channel/${channel.id}/vllm/status`)
        return ok({
          sampled_at: now * 1000,
          version: '0.10.0-ui-fixture',
          endpoints: {
            '/health': { status: 200 },
            '/version': { status: 200 },
          },
          models: [
            {
              id: 'contract-chat',
              root: '/models/contract-chat',
              max_model_len: 32768,
            },
          ],
          metrics: [
            {
              name: 'vllm:num_requests_running',
              labels: { model_name: 'contract-chat' },
              value: 3,
            },
          ],
          raw_metrics:
            'vllm:num_requests_running{model_name="contract-chat"} 3',
        })
      if (path === `/api/channel/test/${channel.id}`) {
        requests.push({ path, model: url.searchParams.get('model') })
        return route.fulfill({ json: { success: true, time: 0.12 } })
      }
    }
    if (request.method() === 'POST') {
      if (path === '/api/channel/upstream_updates/detect') {
        expect(request.postDataJSON()).toEqual({ id: channel.id })
        return ok({
          add_models: ['contract-next'],
          remove_models: ['contract-chat'],
        })
      }
      if (path === '/api/channel/upstream_updates/apply') {
        applied.push(request.postDataJSON())
        return ok({})
      }
      if (path === '/api/channel/fetch_models') {
        const body = request.postDataJSON()
        expect(body.channel_id).toBe(channel.id)
        expect(JSON.parse(body.advanced_custom).advanced_routes).toHaveLength(1)
        await fetchedModelsReady
        return ok(['contract-chat', 'gpt-ui-next', 'claude-ui-next'])
      }
    }
    return route.fallback()
  })
  return { channel, requests, applied, releaseFetchedModels }
}

export async function redemptionStatesFixture(page: Page) {
  const original = {
    id: 1,
    name: 'Existing UI code',
    key: 'UIHISTORY00000000000000000000001',
    status: 1,
    quota: 5000000,
    used_user_id: 0,
    created_time: now,
    redeemed_time: 0,
    expired_time: 0,
  }
  const codes = [
    'UIREGRESSION00000000000000000001',
    'UIREGRESSION00000000000000000002',
  ]
  let rows = [original]
  let submitted: unknown = null
  await page.route(/\/api\/redemption\/(?:\?.*)?$/, async (route) => {
    if (route.request().method() === 'GET')
      return route.fulfill({
        json: {
          success: true,
          data: { items: rows, total: rows.length, page: 1, page_size: 20 },
        },
      })
    if (route.request().method() === 'POST') {
      submitted = route.request().postDataJSON()
      expect(submitted).toEqual({
        name: 'UI Regression',
        quota: 5000000,
        count: 2,
        expired_time: 0,
      })
      rows = [
        ...rows,
        ...codes.map((key, index) => ({
          ...original,
          id: index + 2,
          key,
          name: 'UI Regression',
        })),
      ]
      return route.fulfill({ json: { success: true, data: codes } })
    }
    return route.fallback()
  })
  return { codes, original, submitted: () => submitted }
}

export async function affiliateStatesFixture(page: Page) {
  const target = {
    ...nativeUser(),
    id: 8,
    role: 1,
    username: 'ui.inviter',
    display_name: 'UI inviter',
    email: 'inviter@example.test',
    DeletedAt: null,
    inviter_id: 0,
    last_login_at: now,
    aff_count: 3,
    aff_quota: 500000,
    aff_frozen_quota: 100000,
  }
  await page.route(/\/api\/user\/(?:\?.*)?$/, (route) =>
    route.request().method() === 'GET'
      ? route.fulfill({
          json: {
            success: true,
            data: { items: [target], total: 1, page: 1, page_size: 20 },
          },
        })
      : route.fallback()
  )
  await page.route('**/api/admin/affiliate/users/8', (route) =>
    route.request().method() === 'GET'
      ? route.fulfill({
          json: {
            success: true,
            data: {
              user_id: 8,
              code: 'UI-INVITER',
              code_custom: true,
              enabled: true,
              rate_bps: 1234,
              effective_rate_bps: 1234,
              inviter_id: 0,
              invited_at: now,
            },
          },
        })
      : route.fallback()
  )
}
