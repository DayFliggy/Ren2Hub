import type { Page } from '@playwright/test'
import { PRICING_KEYS } from '../../src/api/pricingKeys'
export async function nativeCoreFixture(
  page: Page,
  theme: string,
  display = 'USD'
) {
  const unexpected: string[] = [],
    errors: string[] = [],
    missingMessages: string[] = [],
    writes: Array<{ path: string; body: unknown }> = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('console', (message) => {
    if (message.text().includes('[intlify] Not found'))
      missingMessages.push(message.text())
  })
  await page.addInitScript((theme) => {
    localStorage.setItem('ren2hub_theme_mode', theme)
    localStorage.setItem('ren2hub_locale', 'en')
  }, theme)
  const now = Math.floor(Date.now() / 1000)
  const user = {
    id: 1,
    username: 'native.root',
    display_name: 'Native Root',
    email: 'root@example.test',
    role: 100,
    status: 1,
    quota: 5000000,
    used_quota: 1000000,
    request_count: 4,
    aff_count: 2,
    aff_quota: 1000000,
    aff_history_quota: 1500000,
    group: 'default',
    setting: '{"language":"en"}',
    has_password: true,
  }
  const session = {
    sid: 'fixture-session',
    current: true,
    login_method: 'password',
    ip: '127.0.0.1',
    user_agent: 'browser',
    created_at: now,
    last_active_at: now,
    expires_at: now + 86400,
  }
  const token = {
    id: 7,
    name: 'Native Key',
    key: 'sk-****1234',
    type: 'manual',
    status: 1,
    remain_quota: 500000,
    used_quota: 250000,
    unlimited_quota: false,
    group: 'default',
    auto_groups: null,
    cross_group_retry: false,
    model_limits_enabled: false,
    model_limits: '',
    allow_ips: null,
    allow_user_quota_query: true,
    created_time: now,
    expired_time: -1,
  }
  let routing = {
    token_id: 7,
    mode: 'manual',
    version: 1,
    entries: [{ channel_id: 2, enabled: true, position: 0 }],
  }
  let checked = false
  const features = Object.fromEntries(
    [
      'registration',
      'dashboard_basic',
      'user_models',
      'api_tokens',
      'token_private_routing',
      'logs',
      'wallet',
      'profile',
      'tickets',
      'admin',
      'orders',
      'taskPlugins',
      'admin_models',
      'pricing',
      'ops_monitor',
      'audit',
      'activity',
      'invites',
    ].map((k) => [k, 'live'])
  )
  const topup = {
    enable_online_topup: true,
    enable_stripe_topup: true,
    enable_creem_topup: true,
    enable_waffo_topup: true,
    enable_waffo_pancake_topup: true,
    enable_gmpay_topup: true,
    enable_redemption: true,
    pay_methods: [
      { type: 'alipay', name: 'Alipay' },
      { type: 'stripe', name: 'Stripe' },
      { type: 'waffo', name: 'Waffo' },
      { type: 'waffo_pancake', name: 'Waffo Pancake' },
      { type: 'gmpay:usdt.tron', name: 'USDT · Tron' },
    ],
    waffo_pay_methods: [
      { name: 'Card', icon: '', payMethodType: 'CARD', payMethodName: '' },
    ],
    creem_products:
      '[{"productId":"prod1","name":"Credits","price":10,"currency":"USD","quota":5000000}]',
    min_topup: 1,
    stripe_min_topup: 1,
    waffo_min_topup: 1,
    waffo_pancake_min_topup: 1,
    amount_options: [10, 20],
    discount: {},
    topup_link: '',
  }
  const emptyPage = { page: 1, page_size: 20, total: 0, items: [] }
  await page.route('**/payment-fixture', (route) =>
    route.fulfill({ contentType: 'text/html', body: '<h1>Local checkout</h1>' })
  )
  await page.route(/^http:\/\/127\.0\.0\.1:\d+\/api\//, async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      path = url.pathname,
      method = req.method()
    const ok = (data: unknown) =>
      route.fulfill({ json: { success: true, message: '', data } })
    if (method !== 'GET')
      writes.push({
        path,
        body: req.postData() ? JSON.parse(req.postData()!) : undefined,
      })
    if (path === '/api/user/auth/refresh')
      return ok({
        access_token: 'fixture-access',
        token_type: 'Bearer',
        access_expires_at: now + 3600,
        session,
        user,
      })
    if (path === '/api/setup')
      return ok({ status: true, root_init: true, database_type: 'sqlite' })
    if (path === '/api/status')
      return ok({
        version: 'native-test',
        system_name: 'Ren2Hub',
        register_enabled: true,
        quota_display_type: display,
        quota_per_unit: 500000,
        usd_exchange_rate: 7.3,
        custom_currency_symbol: '¤',
        custom_currency_exchange_rate: 1,
        frontend_capabilities: features,
        start_time: now - 86400,
        HeaderNavModules: { pricing: true },
        password_login_enabled: true,
        password_login_encryption_enabled: false,
      })
    if (path === '/api/user/self') return ok(user)
    if (path === '/api/notice') return ok('')
    if (path === '/api/pricing')
      return route.fulfill({
        json: {
          success: true,
          data: [
            {
              model_name: 'gpt-native',
              description: 'Native model',
              quota_type: 0,
              model_ratio: 1,
              completion_ratio: 2,
              supported_endpoint_types: ['chat'],
            },
          ],
          vendors: [],
          supported_endpoint: {
            chat: { path: '/v1/chat/completions', method: 'POST' },
          },
          group_ratio: { default: 1 },
          usable_group: { default: 'Default' },
        },
      })
    if (path === '/api/uptime/status') return ok([])
    if (path === '/api/option/model_pricing')
      return ok({
        entries: [
          {
            model_name: 'model-a',
            version: 'v1',
            configured: { ModelPrice: 1 },
            effective: { ModelPrice: 1 },
          },
        ],
        empty_version: 'empty',
        options: Object.fromEntries(PRICING_KEYS.map((key) => [key, '{}'])),
      })
    if (path === '/api/option/model_pricing/preview')
      return ok({ effective: req.postDataJSON().pricing })
    if (path === '/api/vendors/search') return ok(emptyPage)
    if (path === '/api/channel/models_enabled') return ok(['model-a'])
    if (path === '/api/user/models') return ok(['gpt-native'])
    if (path === '/api/user/self/groups')
      return ok({
        default: { ratio: 1, desc: 'Default' },
        discount: { ratio: 0.5, desc: 'Discount' },
        auto: { ratio: 'auto', desc: 'Automatic' },
      })
    if (path === '/api/token/auto-groups')
      return ok({ groups: ['default', 'discount'], max_count: 2 })
    if (path === '/api/data/self')
      return ok([
        {
          model_name: 'gpt-native',
          created_at: now - 3600,
          count: 4,
          quota: 1000000,
          token_used: 1200,
        },
      ])
    if (path === '/api/token/' || path === '/api/token/search')
      return method === 'GET'
        ? ok({ ...emptyPage, total: 1, items: [token] })
        : ok(null)
    if (path === '/api/token/7/routing') {
      if (method === 'PUT') {
        const body = req.postDataJSON()
        routing = {
          ...routing,
          ...body,
          version: routing.version + 1,
          entries: body.entries.map((entry: unknown, position: number) => ({
            ...(entry as object),
            position,
          })),
        }
      }
      return ok(routing)
    }
    if (path === '/api/token/7/routing/channels')
      return ok([
        {
          id: 2,
          name: 'Native channel',
          type: 1,
          status: 1,
          models: ['gpt-native'],
        },
      ])
    if (path === '/api/token/7/routing/preview')
      return ok({
        mode: routing.mode,
        version: routing.version,
        model: url.searchParams.get('model'),
        path: url.searchParams.get('path'),
        groups: ['default'],
        entries: [
          {
            channel_id: 2,
            name: 'Native channel',
            eligible: true,
            reason: 'eligible',
            group: 'default',
          },
        ],
      })
    if (path === '/api/user/topup/info') return ok(topup)
    if (path === '/api/user/amount' || path.endsWith('/amount'))
      return route.fulfill({ json: { message: 'success', data: '73.00' } })
    if (path === '/api/user/gmpay/pay')
      return route.fulfill({
        json: {
          message: 'success',
          url: `${url.origin}/payment-fixture`,
          data: {
            trade_no: 'native-order',
            currency: 'CNY',
            pay_amount: '73.00',
          },
        },
      })
    if (path === '/api/user/topup/self' || path === '/api/user/topup')
      return ok({
        ...emptyPage,
        total: 1,
        items: [
          {
            id: 1,
            user_id: 1,
            amount: 10,
            money: 73,
            trade_no: 'native-order',
            payment_method: 'gmpay:usdt.tron',
            payment_provider: 'gmpay',
            create_time: now,
            complete_time: 0,
            status: 'pending',
          },
        ],
      })
    if (path === '/api/user/checkin') {
      if (method === 'POST') {
        checked = true
        return ok({ quota_awarded: 500 })
      }
      return ok({
        enabled: true,
        min_quota: 500,
        max_quota: 500,
        stats: {
          total_quota: 500,
          total_checkins: 1,
          checkin_count: 1,
          checked_in_today: checked,
          records: [{ checkin_date: '2026-10-17', quota_awarded: 500 }],
        },
      })
    }
    if (path === '/api/user/aff') return ok('NATIVE-AFF')
    if (path === '/api/user/aff_transfer') return ok(null)
    if (path === '/api/admin/tickets/summary')
      return ok({ pending: 0, unassigned: 0, mine: 0 })
    if (path === '/api/user/' || path === '/api/user/search')
      return ok({
        ...emptyPage,
        total: 1,
        items: [
          {
            ...user,
            id: 2,
            role: 1,
            username: 'member',
            display_name: 'Native Member',
            created_at: now,
            last_login_at: now,
            inviter_id: 0,
          },
        ],
      })
    if (path === '/api/authz/catalog')
      return ok({
        resources: [{ resource: 'channel', actions: ['read', 'write'] }],
        roles: {},
      })
    if (path === '/api/audit' || path === '/api/audit/self')
      return ok({
        ...emptyPage,
        total: 1,
        items: [
          {
            id: 1,
            event_id: 'audit-native',
            user_id: 1,
            username: 'native.root',
            actor_role: 100,
            created_at: now,
            category: 'operation',
            action: 'user.update',
            token_ref: '',
            auth_method: 'session',
            ip: '127.0.0.1',
            user_agent: 'browser',
            method: 'PUT',
            route: '/api/user/',
            status: 200,
            success: true,
            request_id: 'native-request',
            content: 'Native audit event',
            other: {
              op: { action: 'user.update', params: { username: 'member' } },
            },
          },
        ],
      })
    if (path === '/api/performance/stats')
      return ok({
        memory_stats: { heap_alloc: 1024 },
        cache_stats: {},
        disk_cache_info: {},
        disk_space_info: {},
        config: {},
      })
    if (path === '/api/system-task/summary') return ok({ types: [] })
    if (path === '/api/system-task/list')
      return route.fulfill({ json: { success: true, data: [], total: 0 } })
    if (path === '/api/option/')
      return ok([
        { key: 'SystemName', value: 'Ren2Hub' },
        { key: 'general_setting.quota_display_type', value: 'USD' },
      ])
    if (path === '/api/option/request_policy')
      return ok({
        options:
          method === 'PATCH'
            ? req.postDataJSON().options
            : { RetryTimes: '2', CheckSensitiveEnabled: 'false' },
      })
    if (path === '/api/option/gmpay/config')
      return ok({
        pay_address: 'https://pay.example',
        merchant_id: 'merchant',
        has_secret: true,
        assets: [{ name: 'USDT', token: 'usdt', network: 'tron' }],
        enabled: true,
      })
    if (path === '/api/user/passkey') return ok({ enabled: false })
    if (path === '/api/user/2fa/status')
      return ok({ enabled: false, backup_codes_remaining: 0 })
    if (path === '/api/user/oauth/bindings' || path === '/api/user/sessions')
      return ok([])
    if (path === '/api/user/access_tokens')
      return ok({ items: [], legacy: null })
    if (path === '/api/user/access_tokens/catalog')
      return ok({
        groups: [
          {
            resources: [{ resource: 'profile', actions: [{ action: 'read' }] }],
          },
        ],
        max_tokens: 10,
        default_expiry_days: 30,
      })
    if (
      path === '/api/log/self' ||
      path === '/api/task/self' ||
      path === '/api/mj/self' ||
      path === '/api/redemption/'
    )
      return ok(emptyPage)
    unexpected.push(`${method} ${path}`)
    return route.fulfill({
      status: 404,
      json: { success: false, message: `Unmapped native fixture ${path}` },
    })
  })
  return { unexpected, errors, missingMessages, writes }
}
