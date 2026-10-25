import type { Page } from '@playwright/test'

const now = Date.parse('2026-07-27T04:00:00Z') / 1000

/** Current native responses only; every amount and identity is synthetic. */
export async function dataPanelsFixture(page: Page) {
  const reads: string[] = []
  const unexpected: string[] = []
  const amount = (paid: string, count: number) => ({
    currency: 'USD',
    count,
    paid_count: count,
    paid_amount: paid,
    credited_quota: Number(paid) * 500_000,
    unknown_amount_count: 0,
  })
  const history = Array.from({ length: 7 }, (_, index) => ({
    ts: `2026-07-${String(21 + index).padStart(2, '0')}T00:00:00Z`,
    label: `07-${21 + index}`,
    tokens: 1000 + index * 250,
  }))
  const data: Record<string, unknown> = {
    '/api/user/topup/stats': {
      total: 6,
      currencies: [amount('120.00', 6)],
      methods: [
        { ...amount('80.00', 4), method: 'stripe' },
        { ...amount('40.00', 2), method: 'creem' },
      ],
      daily: [
        { ...amount('40.00', 2), date: '2026-07-26' },
        { ...amount('80.00', 4), date: '2026-07-27' },
      ],
      start_time: now - 30 * 86_400,
      end_time: now,
    },
    '/api/user/topup/ranking': {
      items: [
        {
          user_id: 101,
          username: 'fixture.alice',
          paid_count: 4,
          credited_quota: 40_000_000,
        },
        {
          user_id: 102,
          username: 'fixture.bob',
          paid_count: 2,
          credited_quota: 20_000_000,
        },
      ],
      start_time: now - 30 * 86_400,
      end_time: now,
    },
    '/api/rankings': {
      models: [
        {
          rank: 1,
          previous_rank: 2,
          model_name: 'gpt-native',
          vendor: 'OpenAI',
          category: 'chat',
          total_tokens: 12_250,
          share: 1,
          growth_pct: 12.5,
        },
      ],
      vendors: [
        {
          rank: 1,
          vendor: 'OpenAI',
          total_tokens: 12_250,
          share: 1,
          growth_pct: 12.5,
          models_count: 1,
          top_model: 'gpt-native',
        },
      ],
      top_movers: [
        {
          model_name: 'gpt-native',
          vendor: 'OpenAI',
          rank_delta: 1,
          current_rank: 1,
          growth_pct: 12.5,
        },
      ],
      top_droppers: [],
      models_history: {
        points: history.map((point) => ({ ...point, model: 'gpt-native' })),
        models: ['gpt-native'],
        buckets: 7,
      },
      vendor_share_history: {
        points: history.map((point) => ({
          ...point,
          vendor: 'OpenAI',
          share: 1,
        })),
        vendors: ['OpenAI'],
        buckets: 7,
      },
    },
  }
  const pricing = {
    success: true,
    data: [
      {
        model_name: 'gpt-dynamic-fixture',
        description: 'Synthetic tiered token pricing / 合成阶梯价格',
        quota_type: 0,
        billing_mode: 'tiered_expr',
        billing_expr: 'tier("native", p * 2 + c * 8)',
        owner_by: 'OpenAI',
        vendor_id: 1,
        supported_endpoint_types: ['openai'],
      },
    ],
    vendors: [{ id: 1, name: 'OpenAI', description: '' }],
    supported_endpoint: {
      openai: { path: '/v1/chat/completions', method: 'POST' },
    },
  }
  await page.route('**/api/**', (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (!Object.hasOwn(data, path) && path !== '/api/pricing')
      return route.fallback()
    if (request.method() !== 'GET') {
      unexpected.push(`${request.method()} ${path}`)
      return route.fulfill({
        status: 405,
        json: { success: false, message: 'Read-only UI fixture' },
      })
    }
    reads.push(path)
    return route.fulfill({
      json:
        path === '/api/pricing'
          ? pricing
          : { success: true, message: '', data: data[path] },
    })
  })
  return { reads, unexpected }
}
