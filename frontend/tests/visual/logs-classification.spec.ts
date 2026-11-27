import { expect, test } from '@playwright/test'

import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  captureEvidence,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

const CALL_ROWS = [
  {
    id: 901,
    request_id: 'req-call-1',
    type: 2,
    username: 'alice',
    token_id: 4,
    token_name: 'prod-key',
    model_name: 'sora-2',
    channel: 9,
    channel_name: 'Primary Video',
    prompt_tokens: 120,
    completion_tokens: 45,
    quota: 250000,
    use_time: 2,
    is_stream: false,
    content: 'Action text_to_video',
    created_at: 1785124800,
    other: JSON.stringify({ is_task: true, task_id: 'task_visual_901' }),
  },
  {
    id: 902,
    request_id: 'req-call-2',
    type: 5,
    username: 'bob',
    token_id: 5,
    token_name: 'dev-key',
    model_name: 'gpt-4o',
    channel: 9,
    channel_name: 'Primary OpenAI',
    prompt_tokens: 0,
    completion_tokens: 0,
    quota: 0,
    use_time: 1,
    is_stream: true,
    content: 'upstream timeout',
    created_at: 1785124700,
    other: '{}',
  },
]

const QUOTA_ROWS = [
  {
    id: 911,
    request_id: 'req-quota-1',
    type: 1,
    username: 'alice',
    token_id: 0,
    token_name: '',
    model_name: '',
    channel_name: '',
    prompt_tokens: 0,
    completion_tokens: 0,
    quota: 0,
    use_time: 0,
    is_stream: false,
    content:
      'Online top-up succeeded, amount added: $10.00, payment amount: 72.00',
    created_at: 1785124800,
    other: JSON.stringify({
      content_parts: [
        {
          key: 'Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}',
          params: { quota: '$10.00', amount: '72.00' },
        },
      ],
    }),
  },
  {
    id: 912,
    request_id: 'req-quota-2',
    type: 1,
    username: 'alice',
    token_id: 0,
    token_name: '',
    model_name: '',
    channel_name: '',
    prompt_tokens: 0,
    completion_tokens: 0,
    quota: 0,
    use_time: 0,
    is_stream: false,
    content: 'Decreased user quota by 1000000',
    created_at: 1785124700,
    other: JSON.stringify({
      op: { action: 'user.quota_subtract', params: { quota: 1000000 } },
    }),
  },
  {
    id: 913,
    request_id: 'req-quota-3',
    type: 4,
    username: 'alice',
    token_id: 0,
    token_name: '',
    model_name: '',
    channel_name: '',
    prompt_tokens: 0,
    completion_tokens: 0,
    quota: 0,
    use_time: 0,
    is_stream: false,
    content: 'Daily check-in, received $0.50',
    created_at: 1785124600,
    other: JSON.stringify({
      content_parts: [
        {
          key: 'Daily check-in, received {{quota}}',
          params: { quota: '$0.50' },
        },
      ],
    }),
  },
  {
    id: 914,
    request_id: 'req-quota-4',
    type: 6,
    username: 'alice',
    token_id: 4,
    token_name: 'prod-key',
    model_name: 'sora-2',
    channel_name: '',
    prompt_tokens: 0,
    completion_tokens: 0,
    quota: 500000,
    use_time: 0,
    is_stream: false,
    content: 'Task failed refund',
    created_at: 1785124500,
    other: JSON.stringify({ task_id: 'task_visual_901', reason: 'timeout' }),
  },
]

const TASK_ROWS = [
  {
    id: 921,
    user_id: 7,
    username: 'alice',
    channel_id: 9,
    task_id: 'task_visual_901',
    platform: 'sunoapi',
    action: 'song',
    status: 'SUCCESS',
    progress: '100%',
    fail_reason: '',
    quota: 500000,
    submit_time: 1785124500,
    finish_time: 1785124545,
    properties: { origin_model_name: 'suno-v4' },
    result_discarded: true,
    admin_info: { request_id: 'req-call-1' },
  },
]

const MJ_ROWS = [
  {
    id: 931,
    user_id: 7,
    channel_id: 9,
    mj_id: 'mj_visual_931',
    action: 'IMAGINE',
    prompt: 'minimalist desert ledger workstation',
    prompt_en: 'minimalist desert ledger workstation',
    status: 'SUCCESS',
    progress: '100%',
    fail_reason: '',
    image_url: 'https://example.invalid/mj.png',
    video_url: '',
    quota: 500000,
    submit_time: 1785124500000,
    finish_time: 1785124520000,
  },
]

for (const scenario of [
  { theme: 'light' as VisualTheme, mobile: false, role: 100 },
  { theme: 'dark' as VisualTheme, mobile: false, role: 1 },
  { theme: 'dark' as VisualTheme, mobile: true, role: 100 },
  { theme: 'light' as VisualTheme, mobile: true, role: 1 },
]) {
  test(`log center 4-way classification ${scenario.theme} ${scenario.mobile ? 'mobile' : 'desktop'} role=${scenario.role}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(
      scenario.mobile
        ? { width: 390, height: 844 }
        : { width: 1440, height: 900 }
    )
    await configureStablePage(page, {
      theme: scenario.theme,
      authenticated: true,
      user: { role: scenario.role },
    })

    const logQueries: string[] = []
    await page.route(/\/api\/log(?:\/self|\/)?(?:\?.*)?$/, (route) => {
      const url = new URL(route.request().url())
      logQueries.push(`${url.pathname}?${url.searchParams.toString()}`)
      const typesParam = url.searchParams.get('types') ?? ''
      const isQuota =
        typesParam === '1,4,6' ||
        ['1', '4', '6'].includes(url.searchParams.get('type') ?? '')
      const items = isQuota ? QUOTA_ROWS : CALL_ROWS
      return route.fulfill({
        json: {
          success: true,
          data: { page: 1, page_size: 10, total: items.length, items },
        },
      })
    })
    await page.route(/\/api\/task(?:\/self)?(?:\?.*)?$/, (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            page: 1,
            page_size: 10,
            total: TASK_ROWS.length,
            items: TASK_ROWS,
          },
        },
      })
    )
    await page.route(/\/api\/mj(?:\/self|\/)?(?:\?.*)?$/, (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            page: 1,
            page_size: 10,
            total: MJ_ROWS.length,
            items: MJ_ROWS,
          },
        },
      })
    )
    await page.route(/\/api\/audit(?:\/self)?(?:\?.*)?$/, (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            page: 1,
            page_size: 10,
            total: 1,
            items: [
              {
                id: 1,
                event_id: 'evt-risk-1',
                user_id: 0,
                username: '',
                actor_role: 0,
                created_at: 1785124800,
                category: 'security',
                action: 'payment.callback_rejected',
                token_ref: '',
                auth_method: 'webhook',
                ip: '203.0.113.10',
                user_agent: 'stripe-webhook',
                method: 'POST',
                route: '/api/stripe/webhook',
                status: 200,
                success: false,
                request_id: 'req-webhook-1',
                content: 'Rejected verified receipt',
                other: {},
              },
            ],
          },
        },
      })
    )

    // 1. Calls tab (/usage-logs)
    await page.goto('/usage-logs', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const navLinks = page.locator('nav[aria-label="日志类别"] a')
    await expect(navLinks).toHaveText([
      '调用日志',
      '异步任务',
      '额度变动',
      '审计日志',
    ])
    expect(logQueries.some((q) => q.includes('types=2%2C5'))).toBe(true)
    await expect(
      page
        .getByText('任务计费', { exact: true })
        .filter({ visible: true })
        .first()
    ).toBeVisible()
    await expect(page.locator('[data-log-search-input]:visible')).toHaveCount(1)
    if (scenario.mobile) {
      await page.locator('[data-log-mobile-filters-toggle]:visible').click()
    }
    await expect(page.getByRole('combobox', { name: '查看范围' })).toHaveCount(
      scenario.role >= 10 ? 1 : 0
    )
    if (scenario.mobile) {
      await page.locator('[data-log-mobile-filters-toggle]:visible').click()
    }
    await assertNoHorizontalOverflow(page)
    await assertInteractiveCentersVisible(page)
    await captureEvidence(page, {
      path: testInfo.outputPath(
        `calls-${scenario.theme}-${scenario.mobile ? '390' : '1440'}-r${scenario.role}.png`
      ),
      fullPage: true,
    })

    // 2. Quota ledger tab (/usage-logs/quota)
    await page.getByRole('link', { name: '额度变动' }).click()
    await expect(page).toHaveURL(/\/usage-logs\/quota$/)
    await waitForStablePage(page)
    expect(logQueries.some((q) => q.includes('types=1%2C4%2C6'))).toBe(true)
    await expect(
      page
        .getByText('管理员扣减', { exact: true })
        .filter({ visible: true })
        .first()
    ).toBeVisible()
    await expect(
      page
        .getByText('-$2.00', { exact: true })
        .filter({ visible: true })
        .first()
    ).toBeVisible()
    await expect(
      page
        .getByText('+$10.00', { exact: true })
        .filter({ visible: true })
        .first()
    ).toBeVisible()
    await expect(
      page
        .getByText('+$0.50', { exact: true })
        .filter({ visible: true })
        .first()
    ).toBeVisible()
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: testInfo.outputPath(
        `quota-${scenario.theme}-${scenario.mobile ? '390' : '1440'}-r${scenario.role}.png`
      ),
      fullPage: true,
    })

    // 3. Click linked task_id -> lands directly on Async tasks filtered to task_visual_901
    await page
      .locator('a[href="/usage-logs/task?task_id=task_visual_901"]:visible')
      .first()
      .click()
    await expect(page).toHaveURL(/\/usage-logs\/task\?task_id=task_visual_901$/)
    await waitForStablePage(page)
    await expect(page.getByRole('link', { name: '异步任务' })).toHaveAttribute(
      'aria-current',
      'page'
    )
    await expect(page.locator('[data-async-task-subtabs] a')).toHaveText([
      '通用生成任务',
      'Midjourney',
    ])
    await expect(
      page
        .getByText('Suno (sunoapi)', { exact: true })
        .filter({ visible: true })
        .first()
    ).toBeVisible()
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: testInfo.outputPath(
        `tasks-${scenario.theme}-${scenario.mobile ? '390' : '1440'}-r${scenario.role}.png`
      ),
      fullPage: true,
    })

    // Switch to Midjourney sub-tab -> top tab stays on 异步任务
    await page
      .locator('[data-async-task-subtabs] a', { hasText: 'Midjourney' })
      .click()
    await expect(page).toHaveURL(/\/usage-logs\/drawing$/)
    await waitForStablePage(page)
    await expect(page.getByRole('link', { name: '异步任务' })).toHaveAttribute(
      'aria-current',
      'page'
    )
    await assertNoHorizontalOverflow(page)

    // 4. Audit tab (/usage-logs/audit) -> retention notice + role-aware categories + payment risk badge
    await page.getByRole('link', { name: '审计日志' }).click()
    await expect(page).toHaveURL(/\/usage-logs\/audit$/)
    await waitForStablePage(page)
    await expect(page.locator('[data-audit-retention-notice]')).toBeVisible()
    await expect(
      page
        .getByText('支付风控', { exact: true })
        .filter({ visible: true })
        .first()
    ).toBeVisible()

    if (scenario.mobile) {
      await page.locator('[data-log-mobile-filters-toggle]:visible').click()
    }
    await page.getByRole('combobox', { name: '事件类别' }).click()
    const expectedAuditOptions =
      scenario.role >= 10
        ? [
            '全部类别',
            '登录与安全',
            '管理操作',
            '账号安全',
            '登录',
            '访问令牌请求',
          ]
        : ['全部类别', '登录与安全', '账号安全', '登录', '访问令牌请求']
    await expect(page.getByRole('option')).toHaveText(expectedAuditOptions)
    await page.keyboard.press('Escape')
    await assertNoHorizontalOverflow(page)
  })
}
