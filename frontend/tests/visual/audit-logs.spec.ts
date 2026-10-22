import { expect, test, type Page } from '@playwright/test'

import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

function auditPaths(page: Page): string[] {
  const paths: string[] = []
  page.on('request', (request) => {
    const { pathname } = new URL(request.url())
    if (pathname.startsWith('/api/audit')) paths.push(pathname)
  })
  return paths
}

const LONG_ACCOUNT =
  'administrator.with.an.extremely.long.account.name@example.test'

function auditRow(row: Record<string, unknown>) {
  return {
    user_id: 1,
    username: 'native.root',
    actor_role: 100,
    created_at: 1_781_928_000,
    category: 'operation',
    token_ref: '',
    auth_method: 'session',
    ip: '198.51.100.9',
    user_agent: 'Playwright',
    method: 'PUT',
    status: 200,
    success: true,
    ...row,
  }
}

// The shared native fixture has one short successful row; the layout checks
// need long values, a failed request, and a historical `system` row written
// before the NewAPI-core rebuild, all in the native /api/audit shape.
const AUDIT_PAGE = {
  page: 1,
  page_size: 10,
  total: 3,
  items: [
    auditRow({
      event_id: 'visual-audit-101',
      request_id: 'visual-request-101',
      username: LONG_ACCOUNT,
      action: 'user.update',
      ip: '203.0.113.25',
      user_agent:
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Visual Audit Browser',
      route: '/api/user/',
      content: 'Updated user account',
      other: {
        op: {
          action: 'user.update',
          params: {
            username: LONG_ACCOUNT,
            changed_fields: ['status', 'group', 'quota'],
          },
        },
      },
    }),
    auditRow({
      event_id: 'visual-audit-100',
      request_id: 'visual-request-100',
      action: 'channel.update',
      route: '/api/channel/',
      status: 500,
      success: false,
      content: 'Updated channel OpenAI Route 1',
      other: {},
    }),
    auditRow({
      event_id: 'visual-audit-99',
      request_id: 'visual-request-99',
      category: 'system',
      action: 'legacy.system_grant',
      route: '',
      method: '',
      content: 'Historical system grant',
      other: {},
    }),
  ],
}

async function inspectAuditLogs(
  page: Page,
  theme: VisualTheme,
  viewport: 'desktop' | 'mobile'
): Promise<void> {
  await page.setViewportSize(
    viewport === 'desktop'
      ? { width: 1440, height: 900 }
      : { width: 390, height: 844 }
  )
  await configureStablePage(page, { theme, authenticated: true })
  await page.route(
    (url) => url.pathname === '/api/audit',
    (route) =>
      route.fulfill({ json: { success: true, message: '', data: AUDIT_PAGE } })
  )
  const paths = auditPaths(page)
  await page.goto('/usage-logs/audit?scope=all', {
    waitUntil: 'domcontentloaded',
  })
  await waitForStablePage(page)

  await expect(page.getByRole('link', { name: '审计日志' })).toHaveAttribute(
    'aria-current',
    'page'
  )
  expect(paths).toContain('/api/audit')
  await expect(
    page.getByRole('searchbox', { name: '按令牌指纹筛选…' })
  ).toBeVisible()
  await expect(
    page
      .getByText('更新用户', { exact: false })
      .filter({ visible: true })
      .first()
  ).toBeVisible()
  await expect(
    page.getByText('失败', { exact: true }).filter({ visible: true }).first()
  ).toBeVisible()
  await expect(
    page.getByText('system', { exact: true }).filter({ visible: true })
  ).toHaveCount(1)
  await assertNoHorizontalOverflow(page)
  await assertInteractiveCentersVisible(page)

  const detailButton = page
    .getByRole('button', { name: '查看详情', exact: true })
    .first()
  await detailButton.click()
  const dialog = page.locator('[role="dialog"][aria-modal="true"]')
  await expect(dialog).toBeVisible()
  await expect(dialog.locator('[data-audit-log-detail]')).toBeVisible()
  await expect(dialog.getByText('203.0.113.25', { exact: true })).toBeVisible()
  await expect(dialog.getByText('令牌指纹', { exact: true })).toHaveCount(0)
  await expect(dialog).not.toContainText('password')

  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(detailButton).toBeFocused()
}

for (const theme of ['light', 'dark'] as const) {
  for (const viewport of ['desktop', 'mobile'] as const) {
    test(`${theme} ${viewport} audit logs`, async ({ page }) => {
      await inspectAuditLogs(page, theme, viewport)
    })
  }
}

test('audit filters use the native request_id and categories', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.goto('/usage-logs/audit?scope=all', {
    waitUntil: 'domcontentloaded',
  })
  await waitForStablePage(page)

  await page.getByRole('combobox', { name: '事件类别' }).click()
  await expect(page.getByRole('option')).toHaveText([
    '全部类别',
    '管理操作',
    '账号安全',
    '登录',
    '访问令牌请求',
  ])
  await page.keyboard.press('Escape')

  const search = page.getByRole('searchbox', { name: '按完整请求 ID 筛选' })
  await expect(search).toHaveAttribute('maxlength', '64')
  const filtered = page.waitForRequest((request) => {
    const url = new URL(request.url())
    return (
      url.pathname === '/api/audit' &&
      url.searchParams.get('request_id') === 'req-45'
    )
  })
  await search.fill('req-45')
  const params = new URL((await filtered).url()).searchParams
  expect(params.has('keyword')).toBe(false)
})

test('members read only their own audit events', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, {
    theme: 'light',
    authenticated: true,
    user: { role: 1 },
  })
  const paths = auditPaths(page)
  await page.goto('/usage-logs/audit?scope=all', {
    waitUntil: 'domcontentloaded',
  })
  await waitForStablePage(page)

  await expect(page.getByRole('link', { name: '审计日志' })).toBeVisible()
  expect(paths.length).toBeGreaterThan(0)
  expect(paths.every((path) => path === '/api/audit/self')).toBe(true)
  await expect(page.getByText('查看范围', { exact: false })).toHaveCount(0)
  await expect(
    page.getByRole('searchbox', { name: '按令牌指纹筛选…' })
  ).toHaveCount(0)
})

test('consume logs keep account events on the audit tab', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.goto('/usage-logs', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  await page.getByRole('combobox', { name: '类型' }).click()
  await expect(page.getByRole('option')).toHaveText([
    '全部',
    '消费',
    '充值',
    '退款',
    '错误',
  ])
  await page.keyboard.press('Escape')

  await page.getByRole('link', { name: '审计日志' }).click()
  await expect(page).toHaveURL(/\/usage-logs\/audit$/)
})

test('retired bookmarks open the merged audit page', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'light', authenticated: true })
  for (const [source, target] of [
    ['/usage-logs/operations', /\/usage-logs\/audit\?scope=all$/],
    ['/audit', /\/usage-logs\/audit$/],
  ] as const) {
    await page.goto(source, { waitUntil: 'domcontentloaded' })
    await expect(page).toHaveURL(target)
  }
})

test('audit logs expose loading and empty states', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.route('**/api/audit/self?*', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 500))
    await route.fulfill({
      json: {
        success: true,
        message: '',
        data: { page: 1, page_size: 10, total: 0, items: [] },
      },
    })
  })

  await page.goto('/usage-logs/audit', { waitUntil: 'domcontentloaded' })
  await expect(page.locator('[aria-busy="true"]:visible').first()).toBeVisible()
  await expect(
    page
      .getByText('暂无审计日志', { exact: true })
      .filter({ visible: true })
      .first()
  ).toBeVisible()
  await assertNoHorizontalOverflow(page)
})

test('audit logs expose a recoverable failure state', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'dark', authenticated: true })
  let calls = 0
  await page.route('**/api/audit/self?*', (route) => {
    calls++
    return route.fulfill({
      status: 500,
      json: { success: false, message: 'audit unavailable' },
    })
  })

  await page.goto('/usage-logs/audit', { waitUntil: 'domcontentloaded' })
  await expect(
    page.getByText('审计日志加载失败', { exact: true })
  ).toBeVisible()
  const before = calls
  await page.getByRole('button', { name: '重试' }).click()
  await expect.poll(() => calls).toBeGreaterThan(before)
  await assertNoHorizontalOverflow(page)
})
