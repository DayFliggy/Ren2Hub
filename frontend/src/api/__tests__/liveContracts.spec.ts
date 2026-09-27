import { offerFixture } from './pricingFixture'
import { describe, expect, it } from 'vitest'

import {
  parseDrawingLogPage,
  parseLogPage,
  parseLogStat,
  parseOperationLogPage,
  parsePerfMetricsSummary,
  parsePricingModels,
  parseRedeemedQuota,
  parseTaskLogPage,
  parseTokenPage,
  parseTopupInfo,
  parseGmpayTopupInfo,
  parseUsageRows,
  parseUserModels,
} from '@/api/liveContracts'

function expectInvalidResponse(parse: () => unknown): void {
  try {
    parse()
    throw new Error('expected INVALID_RESPONSE')
  } catch (error) {
    expect(error).toMatchObject({
      status: 502,
      code: 'INVALID_RESPONSE',
    })
  }
}

function pricingResponse(overrides: Record<string, unknown> = {}) {
  return {
    model_name: 'gpt-4o',
    description: 'Fast model',
    owner_by: 'OpenAI',
    supported_endpoint_types: ['chat'],
    offers: [
      offerFixture({
        contract_version: 3,
        calculation_version: 'billing-v3',
        result_unit: 'usd',
        id: 1,
        model: 'gpt-4o',
        mode: 'unit_price' as const,
        prices: [
          { quantity: 'p', unit: 'per_million_tokens', price: '1' },
          { quantity: 'c', unit: 'per_million_tokens', price: '1.2' },
          { quantity: 'cr', unit: 'per_million_tokens', price: '0.25' },
        ],
        created_at: 1,
        hash: 'fixture',
      }),
    ],
    ...overrides,
  }
}

describe('live API contracts', () => {
  it('retains alias offers and independent decimal adjustments without exposing channel identity', () => {
    const response = pricingResponse({ model_name: 'gpt-4o-alias' })
    const primary = response.offers[0]!
    const discounted = {
      ...primary,
      offer_id: 'discounted',
      adjustments: [
        { kind: 'channel_multiplier', multiplier: '0.123456789012345678' },
      ],
      display_prices: primary.display_prices.map((line) => ({
        ...line,
        price: '0.123456789012345678',
      })),
    }
    const rows = parsePricingModels([
      { ...response, offers: [primary, discounted] },
    ])
    expect(rows[0]?.model_name).toBe('gpt-4o-alias')
    expect(rows[0]?.offers).toHaveLength(2)
    expect(rows[0]?.offers[1]?.adjustments[0]?.multiplier).toBe(
      '0.123456789012345678'
    )
    expectInvalidResponse(() =>
      parsePricingModels([{ ...response, offers: [primary, primary] }])
    )
    expectInvalidResponse(() =>
      parsePricingModels([{ ...response, offers: [] }])
    )
    expectInvalidResponse(() =>
      parsePricingModels([
        { ...response, offers: [{ ...primary, priced_model: 'other' }] },
      ])
    )
    expectInvalidResponse(() =>
      parsePricingModels([
        {
          ...response,
          offers: [
            {
              ...primary,
              price_version: { ...primary.price_version, channel_id: 9 },
            },
          ],
        },
      ])
    )
  })
  it('parses user models in both supported backend shapes', () => {
    expect(parseUserModels(['gpt-4o', 'claude-3-7-sonnet'])).toEqual([
      'gpt-4o',
      'claude-3-7-sonnet',
    ])
    expect(parseUserModels({ models: [] })).toEqual([])
    expectInvalidResponse(() => parseUserModels({ models: [1] }))
  })

  it('parses model pricing and performance summaries with optional metadata', () => {
    expect(parsePricingModels([pricingResponse()])[0]).toMatchObject({
      model_name: 'gpt-4o',
      offers: [
        {
          price_version: {
            mode: 'unit_price',
            prices: expect.arrayContaining([
              { quantity: 'p', unit: 'per_million_tokens', price: '1' },
            ]),
          },
        },
      ],
      owner_by: 'OpenAI',
    })
    expect(
      parsePerfMetricsSummary({
        models: [
          {
            model_name: 'gpt-4o',
            avg_latency_ms: 850,
            success_rate: 99.5,
            avg_tps: 42,
          },
        ],
      })[0]
    ).toEqual({
      model_name: 'gpt-4o',
      avg_latency_ms: 850,
      success_rate: 99.5,
      avg_tps: 42,
    })
    expectInvalidResponse(() => parsePricingModels([{ model_name: 'gpt-4o' }]))
    expectInvalidResponse(() => parsePerfMetricsSummary({ models: [{}] }))
  })

  it('rejects missing and numeric prices instead of inferring a free model', () => {
    const response = pricingResponse()
    expectInvalidResponse(() =>
      parsePricingModels([{ ...response, offers: undefined }])
    )
    expectInvalidResponse(() =>
      parsePricingModels([
        {
          ...response,
          offers: [
            {
              ...response.offers[0],
              display_prices: [
                { quantity: 'p', unit: 'per_million_tokens', price: 0 },
              ],
            },
          ],
        },
      ])
    )
  })

  it('parses the log overview contract and rejects missing fields', () => {
    expect(
      parseLogStat({
        total_requests: 20,
        total_quota: '500',
        today_requests: 2,
        today_quota: '70',
      })
    ).toEqual({
      total_requests: 20,
      total_quota: '500',
      today_requests: 2,
      today_quota: '70',
    })
    expectInvalidResponse(() => parseLogStat({ quota: '500' }))
  })

  it('uses the route type returned by the backend and normalizes token limits', () => {
    const page = parseTokenPage({
      page: 1,
      page_size: 20,
      total: 1,
      items: [
        {
          id: 7,
          name: 'vip-key',
          key: 'sk-12**********7890',
          type: 'manual',
          status: 1,
          used_quota: '120',
          available_quota: '880',
          reserved_quota: '0',
          quota_version: 0,
          unlimited_quota: false,
          model_limits_enabled: true,
          model_limits: 'gpt-4o, claude-3-7-sonnet',
          allow_ips: '127.0.0.1\n10.0.0.1',
          expired_time: -1,
          created_time: 1_700_000_000,
        },
      ],
    })

    expect(page.items[0]).toMatchObject({
      type: 'manual',
      model_limits: ['gpt-4o', 'claude-3-7-sonnet'],
      ip_limits: ['127.0.0.1', '10.0.0.1'],
    })
    expect(
      parseTokenPage({ page: 1, page_size: 20, total: 0, items: [] }).items
    ).toEqual([])
    expectInvalidResponse(() =>
      parseTokenPage({ page: 1, page_size: 20, total: 1, items: [{}] })
    )
  })

  it('parses usage rows and rejects missing required fields', () => {
    expect(
      parseUsageRows([
        {
          model_name: 'gpt-4o',
          created_at: 1_700_000_000,
          count: 2,
          quota: '300',
          token_used: 42,
        },
      ])
    ).toEqual([
      {
        model_name: 'gpt-4o',
        created_at: 1_700_000_000,
        count: 2,
        quota: '300',
        token_used: 42,
      },
    ])
    expect(parseUsageRows([])).toEqual([])
    expectInvalidResponse(() => parseUsageRows([{ model_name: 'gpt-4o' }]))
  })

  it('parses wallet capabilities, top-up records, and redeemed quota', () => {
    const info = parseTopupInfo({
      enable_online_topup: true,
      enable_redemption: true,
      pay_methods: [
        { name: 'Alipay', type: 'alipay', min_topup: 5 },
        { name: 'WeChat Pay', type: 'wxpay', min_topup: 10 },
      ],
      min_topup: 1,
      amount_options: [5, 10],
    })
    expect(info.pay_methods).toEqual([
      {
        name: 'Alipay',
        type: 'alipay',
        min_topup: 5,
        color: undefined,
      },
      {
        name: 'WeChat Pay',
        type: 'wxpay',
        min_topup: 10,
        color: undefined,
      },
    ])
    expect(info.amount_options).toEqual([5, 10])

    expect(parseRedeemedQuota('500000')).toBe('500000')
    expectInvalidResponse(() => parseRedeemedQuota(500000))
    expect(parseRedeemedQuota({ quota: '250000' })).toBe('250000')
    expectInvalidResponse(() => parseTopupInfo({}))
    expectInvalidResponse(() =>
      parseTopupInfo({
        enable_online_topup: false,
        enable_redemption: false,
        pay_methods: [],
        min_topup: 0,
        amount_options: [10],
      })
    )
    expectInvalidResponse(() =>
      parseTopupInfo({
        enable_online_topup: false,
        enable_redemption: false,
        pay_methods: [],
        min_topup: 1,
        amount_options: [-10],
      })
    )
    const cryptoConfig = {
      enable_gmpay_topup: true,
      enable_gmpay_settlement: true,
      pay_methods: [
        {
          name: 'USDT Tron',
          type: 'gmpay:usdt.tron',
          token: 'usdt',
          network: 'tron',
          min_topup: '2',
          icon: '',
          color: '',
        },
      ],
      min_topup: 1,
      amount_options: [10],
    }
    const crypto = parseGmpayTopupInfo(cryptoConfig)
    expect(crypto.pay_methods).toEqual([
      {
        name: 'USDT Tron',
        type: 'gmpay:usdt.tron',
        min_topup: 2,
        color: undefined,
        token: 'usdt',
        network: 'tron',
        icon: undefined,
      },
    ])
    expectInvalidResponse(() =>
      parseTopupInfo({
        ...cryptoConfig,
        enable_online_topup: true,
        enable_redemption: true,
      })
    )
    // The crypto endpoint identifies an asset with both token and network.
    expectInvalidResponse(() =>
      parseGmpayTopupInfo({
        ...cryptoConfig,
        pay_methods: [{ name: 'USDT', type: 'gmpay:usdt.tron', token: 'usdt' }],
      })
    )

    expectInvalidResponse(() => parseRedeemedQuota({ amount: 1 }))
  })

  it('parses safe user log DTOs without requiring internal channel fields', () => {
    const page = parseLogPage({
      page: 1,
      page_size: 20,
      total: 1,
      items: [
        {
          id: 11,
          type: 2,
          token_name: 'app-key',
          model_name: 'gpt-4o',
          channel_name: 'OpenAI',
          prompt_tokens: 10,
          completion_tokens: 20,
          other: '{"frt":250}',
          quota: '300',
          use_time: 1000,
          is_stream: true,
          content: '',
          created_at: 1_700_000_000,
        },
      ],
    })

    expect(page.items[0]).toMatchObject({
      type: 'consume',
      channel: 'OpenAI',
      latency: 1,
      first_token_latency: 0.25,
      request_mode: 'stream',
      tps: 20,
    })
    expect(
      parseLogPage({ page: 1, page_size: 20, total: 0, items: [] }).items
    ).toEqual([])
    expectInvalidResponse(() =>
      parseLogPage({
        page: 1,
        page_size: 20,
        total: 1,
        items: [{ id: 1, type: 2 }],
      })
    )
  })

  it('parses successful login records in the ordinary user log contract', () => {
    const page = parseLogPage({
      page: 1,
      page_size: 20,
      total: 1,
      items: [
        {
          id: 12,
          type: 7,
          token_name: '',
          model_name: '',
          channel_name: '',
          prompt_tokens: 0,
          completion_tokens: 0,
          quota: '0',
          use_time: 0,
          is_stream: false,
          content: 'Logged in',
          created_at: 1_700_000_000,
        },
      ],
    })

    expect(page.items[0]).toMatchObject({
      id: 12,
      type: 'login',
      content: 'Logged in',
      created: 1_700_000_000,
    })
  })

  it('parses the operation log DTO and rejects raw or malformed shapes', () => {
    const page = parseOperationLogPage({
      page: 2,
      page_size: 20,
      total: 21,
      items: [
        {
          id: 45,
          created_at: 1_700_000_000,
          kind: 'manage',
          action: 'user.update',
          params: { username: 'alice', changed_fields: ['status'] },
          content: 'Updated user alice',
          actor: {
            id: 7,
            username: 'admin',
            role: 10,
            auth_method: 'session',
          },
          ip: '203.0.113.2',
          user_agent: 'Test Browser',
          request: {
            method: 'PUT',
            route: '/api/user/:id',
            path: '/api/user/9',
            status: 200,
            success: true,
          },
        },
      ],
    })

    expect(page).toMatchObject({ page: 2, pageSize: 20, total: 21 })
    expect(page.items[0]).toEqual({
      id: 45,
      created_at: 1_700_000_000,
      kind: 'manage',
      action: 'user.update',
      params: { username: 'alice', changed_fields: ['status'] },
      content: 'Updated user alice',
      actor: {
        id: 7,
        username: 'admin',
        role: 10,
        auth_method: 'session',
      },
      ip: '203.0.113.2',
      user_agent: 'Test Browser',
      request: {
        method: 'PUT',
        route: '/api/user/:id',
        path: '/api/user/9',
        status: 200,
        success: true,
      },
    })
    expectInvalidResponse(() =>
      parseOperationLogPage({
        page: 1,
        page_size: 20,
        total: 1,
        items: [{ id: 1, kind: 'consume', actor: {} }],
      })
    )
    const extraFields = parseOperationLogPage({
      page: 1,
      page_size: 20,
      total: 1,
      items: [
        {
          id: 1,
          created_at: 1,
          kind: 'system',
          action: '',
          params: {},
          content: 'legacy',
          actor: { id: 1, username: 'user' },
          other: '{"password":"must-not-cross-the-contract"}',
        },
      ],
    })
    expect(extraFields.items[0]).not.toHaveProperty('other')
  })

  it('parses drawing logs, defaults optional fields, and keeps unknown statuses', () => {
    const page = parseDrawingLogPage({
      page: 1,
      page_size: 20,
      total: 1,
      items: [
        {
          id: 3,
          mj_id: '1719923456789',
          action: 'IMAGINE',
          prompt: 'a red fox',
          prompt_en: 'a red fox',
          status: 'SUCCESS',
          progress: '100%',
          fail_reason: '',
          image_url: 'https://cdn.example.com/fox.png',
          quota: '5000',
          submit_time: 1_700_000_000_000,
          finish_time: 1_700_000_060_000,
        },
      ],
    })

    expect(page.items[0]).toMatchObject({
      mj_id: '1719923456789',
      action: 'IMAGINE',
      status: 'SUCCESS',
      image_url: 'https://cdn.example.com/fox.png',
      video_url: '',
      submit_time: 1_700_000_000_000,
    })

    const futureStatus = parseDrawingLogPage({
      page: 1,
      page_size: 20,
      total: 1,
      items: [{ id: 4, status: 'BRAND_NEW_STATE', quota: '0' }],
    })
    expect(futureStatus.items[0]).toMatchObject({
      status: 'BRAND_NEW_STATE',
      quota: '0',
      finish_time: 0,
    })

    expect(
      parseDrawingLogPage({ page: 1, page_size: 20, total: 0, items: [] }).items
    ).toEqual([])
    expectInvalidResponse(() =>
      parseDrawingLogPage({
        page: 1,
        page_size: 20,
        total: 1,
        items: [{ mj_id: 'no-id' }],
      })
    )
  })

  it('parses relay task logs including optional result URLs', () => {
    const page = parseTaskLogPage({
      page: 1,
      page_size: 20,
      total: 2,
      items: [
        {
          id: 9,
          task_id: 'suno-abc',
          platform: 'suno',
          action: 'MUSIC',
          status: 'SUCCESS',
          progress: '100%',
          fail_reason: '',
          result_url: 'https://cdn.example.com/song.mp3',
          quota: '12000',
          submit_time: 1_700_000_000,
          finish_time: 1_700_000_090,
        },
        {
          id: 10,
          task_id: 'kling-def',
          platform: 'kling',
          action: 'VIDEO',
          status: 'FAILURE',
          progress: '0%',
          fail_reason: 'upstream timeout',
          quota: '0',
          submit_time: 1_700_000_100,
          finish_time: 0,
        },
      ],
    })

    expect(page.items[0]).toMatchObject({
      task_id: 'suno-abc',
      platform: 'suno',
      result_url: 'https://cdn.example.com/song.mp3',
    })
    expect(page.items[1]).toMatchObject({
      status: 'FAILURE',
      fail_reason: 'upstream timeout',
      result_url: '',
    })
    expectInvalidResponse(() =>
      parseTaskLogPage({
        page: 1,
        page_size: 20,
        total: 1,
        items: [{ task_id: 'missing-id' }],
      })
    )
  })
})
