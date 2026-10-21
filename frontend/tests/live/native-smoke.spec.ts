import { writeFileSync } from 'node:fs'
import { expect, test } from '@playwright/test'
import { accounts, login, navigate } from './helpers'
import { SYSTEM_SETTINGS_DOMAINS } from '../../src/constants/systemSettingsCatalog'

type Event = {
  page: string
  kind: 'api' | 'console' | 'pageerror' | 'navigation'
  path?: string
  method?: string
  status?: number
  success?: boolean
  code?: string
  message?: string
}

const userRoutes = [
  '/dashboard',
  '/activity',
  '/models',
  '/keys',
  '/usage-logs',
  '/usage-logs/drawing',
  '/usage-logs/task',
  '/usage-logs/audit',
  '/orders',
  '/tickets',
  '/wallet',
  '/invite',
  '/settings',
  '/security',
  '/profile',
  '/pricing',
  '/about',
  '/rankings',
]
const adminRoutes = [
  '/ops',
  '/task-plugins',
  '/models/metadata',
  '/models/vendors',
  '/models/pricing',
  '/channels',
  '/ticket-management',
  '/users',
  '/redemption-codes',
  ...SYSTEM_SETTINGS_DOMAINS.flatMap((domain) =>
    domain.sections.map(
      (section) => `/system-settings/${domain.id}/${section.id}`
    )
  ),
]

function sanitized(message: string) {
  return message
    .replace(/Bearer\s+[^\s]+/gi, 'Bearer [redacted]')
    .replace(
      /eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/g,
      '[redacted token]'
    )
    .slice(0, 600)
}

for (const role of ['user', 'root'] as const) {
  for (const theme of ['light', 'dark'] as const) {
    for (const viewport of [
      { name: 'desktop', width: 1440, height: 900 },
      { name: 'mobile', width: 390, height: 844 },
    ] as const) {
      test(`${role} ${theme} ${viewport.name} real backend routes`, async ({
        page,
      }, testInfo) => {
        const events: Event[] = []
        const visited: Array<{
          requested: string
          actual: string
          overflow: number
        }> = []
        const pending = new Set<Promise<void>>()
        let route = 'login'
        page.on('response', (response) => {
          const url = new URL(response.url())
          if (!url.pathname.startsWith('/api/')) return
          const event: Event = {
            page: route,
            kind: 'api',
            path: url.pathname,
            method: response.request().method(),
            status: response.status(),
          }
          const task = (async () => {
            if (
              response.headers()['content-type']?.includes('application/json')
            ) {
              const value = await response.json().catch(() => null)
              if (value && typeof value.success === 'boolean')
                event.success = value.success
              if (value && typeof value.code === 'string')
                event.code = value.code
            }
            events.push(event)
          })()
          pending.add(task)
          void task.finally(() => pending.delete(task))
        })
        page.on('pageerror', (error) =>
          events.push({
            page: route,
            kind: 'pageerror',
            message: sanitized(error.message),
          })
        )
        page.on('console', (message) => {
          if (message.type() === 'error')
            events.push({
              page: route,
              kind: 'console',
              message: sanitized(message.text()),
            })
        })
        await page.setViewportSize(viewport)
        await page.emulateMedia({ colorScheme: theme })
        await page.addInitScript((selectedTheme) => {
          localStorage.setItem('ren2hub_theme_mode', selectedTheme)
          localStorage.setItem('ren2hub_locale', 'zh-CN')
        }, theme)
        try {
          await login(page, role)
          for (const path of [
            ...userRoutes,
            ...(role === 'root' ? adminRoutes : []),
          ]) {
            route = path
            console.log(`VISIT ${role} ${theme} ${viewport.name} ${path}`)
            await navigate(page, path)
            const actual = new URL(page.url()).pathname
            const overflow = await page.evaluate(() =>
              Math.max(
                0,
                document.documentElement.scrollWidth - window.innerWidth
              )
            )
            visited.push({ requested: path, actual, overflow })
            const expectedPath =
              role === 'user' && path === '/orders' ? '/dashboard' : path
            expect(actual, `Unexpected route after navigating to ${path}`).toBe(
              expectedPath
            )
            expect(
              overflow,
              `Horizontal overflow at ${path}`
            ).toBeLessThanOrEqual(1)
            if (
              [
                '/tickets',
                '/invite',
                '/ticket-management',
                '/wallet',
                '/users',
              ].includes(path)
            ) {
              await page.screenshot({
                path: testInfo.outputPath(path.slice(1) + '.png'),
                fullPage: true,
              })
            }
          }
          await Promise.all(pending)
          const failures = events.filter(
            (event) =>
              event.page !== 'login' &&
              (event.kind === 'pageerror' ||
                event.kind === 'console' ||
                (event.kind === 'api' &&
                  ((event.status ?? 200) >= 400 || event.success === false)))
          )
          expect(
            failures,
            'Actual API, business, and browser failures; see sanitized per-page report'
          ).toEqual([])
        } finally {
          await Promise.all(pending)
          const report = testInfo.outputPath('real-backend-report.json')
          writeFileSync(
            report,
            JSON.stringify(
              { role, theme, viewport: viewport.name, visited, events },
              null,
              2
            ),
            { mode: 0o600 }
          )
          await testInfo.attach('real-backend-report.json', {
            path: report,
            contentType: 'application/json',
          })
        }
      })
    }
  }
}

test('user creates attached ticket replies closes and reopens through real UI', async ({
  page,
}, testInfo) => {
  await page.addInitScript(() =>
    localStorage.setItem('ren2hub_locale', 'zh-CN')
  )
  await login(page, 'user')
  await navigate(page, '/tickets')
  await page.getByRole('button', { name: '新建工单', exact: true }).click()
  const dialog = page.getByRole('dialog')
  const title = `Browser ticket ${Date.now()}`
  await dialog.locator('input[name="ticket-title"]').fill(title)
  await dialog
    .locator('textarea[name="ticket-content"]')
    .fill('Created through the actual Vue form and native backend.')
  await dialog.locator('input[type="file"]').setInputFiles({
    name: 'candidate.png',
    mimeType: 'image/png',
    buffer: Buffer.from(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a0p8AAAAASUVORK5CYII=',
      'base64'
    ),
  })
  const createResponse = page.waitForResponse(
    (response) =>
      response.request().method() === 'POST' &&
      new URL(response.url()).pathname === '/api/tickets'
  )
  await dialog.getByRole('button', { name: '确认', exact: true }).click()
  const created = await (await createResponse).json()
  expect(created.success).toBe(true)
  await expect(dialog).toHaveCount(0)
  await page.getByRole('button', { name: title, exact: true }).click()
  await expect(page).toHaveURL(
    new RegExp(`/tickets/${created.data.ticket.id}$`)
  )
  await expect(
    page.getByRole('heading', { name: title, exact: true })
  ).toBeVisible()
  await page.waitForLoadState('networkidle')
  await expect(page.locator('img[src^="blob:"]')).toHaveCount(1)
  const reply = 'Reply persisted through the browser.'
  await page.locator('textarea[name="ticket-reply"]').fill(reply)
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByText(reply, { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '关闭工单', exact: true }).click()
  await page
    .getByRole('dialog')
    .getByRole('button', { name: '关闭工单', exact: true })
    .click()
  await expect(page.getByText('此工单已关闭', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '重新开启', exact: true }).click()
  await expect(page.locator('textarea[name="ticket-reply"]')).toBeEnabled()
  await page.screenshot({
    path: testInfo.outputPath('ticket-real-create-reply-reopen.png'),
    fullPage: true,
  })
  writeFileSync(
    testInfo.outputPath('ticket-ui-result.json'),
    JSON.stringify({
      ticket_id: created.data.ticket.id,
      attached: true,
      replied: true,
      closed: true,
      reopened: true,
    }),
    { mode: 0o600 }
  )
})

test('inviter transfers real GMPay rebate in full and native replay remains idempotent', async ({
  page,
}, testInfo) => {
  test.skip(
    process.env.REN2HUB_LIVE_AFFILIATE_TRANSFER !== 'true',
    'Requires an explicitly prepared real local payment rebate'
  )
  await page.addInitScript(() =>
    localStorage.setItem('ren2hub_locale', 'zh-CN')
  )
  const session = await login(page, 'user')
  const infoResponse = page.waitForResponse(
    (response) => new URL(response.url()).pathname === '/api/user/affiliate'
  )
  await navigate(page, '/invite')
  const before = (await (await infoResponse).json()).data
  expect(before.transferable).toBeGreaterThan(0)
  await page
    .getByRole('button', { name: '转入余额', exact: true })
    .first()
    .click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.locator('input')).toHaveCount(0)
  const transferResponse = page.waitForResponse(
    (response) =>
      response.request().method() === 'POST' &&
      new URL(response.url()).pathname === '/api/user/affiliate/transfer'
  )
  await dialog.getByRole('button', { name: '确认', exact: true }).click()
  const response = await transferResponse
  const body = response.request().postDataJSON() as Record<string, unknown>
  expect(Object.keys(body)).toEqual(['operation_id'])
  const transfer = (await response.json()).data
  expect(transfer.replayed).toBe(false)
  expect(transfer.transferred_quota).toBe(before.transferable)
  expect(transfer.wallet_quota).toBe(session.user.quota + before.transferable)
  await expect(dialog).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: '转入余额', exact: true }).first()
  ).toBeDisabled()
  const replay = await page.request.post('/api/user/affiliate/transfer', {
    headers: { Authorization: `Bearer ${session.access_token}` },
    data: body,
  })
  expect(replay.status()).toBe(200)
  const replayed = await replay.json()
  expect(replayed.success).toBe(true)
  expect(replayed.data.replayed).toBe(true)
  expect(replayed.data.transferred_quota).toBe(transfer.transferred_quota)
  expect(replayed.data.wallet_quota).toBe(transfer.wallet_quota)
  await page.screenshot({
    path: testInfo.outputPath('invite-full-transfer.png'),
    fullPage: true,
  })
  writeFileSync(
    testInfo.outputPath('affiliate-ui-result.json'),
    JSON.stringify({
      transferred_quota: transfer.transferred_quota,
      wallet_delta: before.transferable,
      full_transfer_body: true,
      replayed_once: true,
    }),
    { mode: 0o600 }
  )
})

for (const viewport of [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'mobile', width: 390, height: 844 },
] as const) {
  test(`administrator ${viewport.name} replies and preserves referral policy during code reset`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(viewport)
    await page.addInitScript(() =>
      localStorage.setItem('ren2hub_locale', 'zh-CN')
    )
    await login(page, 'root')
    await navigate(page, '/ticket-management')
    await page
      .getByRole('button')
      .filter({ hasText: /Browser ticket|Candidate ticket/ })
      .first()
      .click()
    const workspace = page.getByLabel('工单处理区')
    const reply = `Real administrator ${viewport.name} reply ${Date.now()}`
    await workspace.locator('textarea[name="ticket-reply"]').fill(reply)
    await workspace.getByRole('button', { name: '发送', exact: true }).click()
    await expect(workspace.getByText(reply, { exact: true })).toBeVisible()
    await page.screenshot({
      path: testInfo.outputPath('admin-ticket-real-reply.png'),
      fullPage: true,
    })
    await navigate(page, '/users')
    const userRow =
      viewport.name === 'desktop'
        ? page.getByRole('row').filter({ hasText: accounts.user.username })
        : page
            .locator('[data-user-mobile-row]')
            .filter({ hasText: accounts.user.username })
    const policyResponse = page.waitForResponse(
      (response) =>
        response.request().method() === 'GET' &&
        /\/api\/admin\/affiliate\/users\/\d+$/.test(
          new URL(response.url()).pathname
        )
    )
    await userRow
      .getByRole('button', { name: '配置邀请码', exact: true })
      .click()
    const initial = (await (await policyResponse).json()).data
    const dialog = page.getByRole('dialog', { name: '邀请码与返利配置' })
    if (initial.enabled)
      await dialog
        .getByRole('switch', { name: '启用邀请码', exact: true })
        .click()
    if (initial.rate_bps === null)
      await dialog
        .getByRole('switch', { name: '专属返利比例', exact: true })
        .click()
    await dialog
      .getByRole('spinbutton', { name: '返利比例（百分比）' })
      .fill('12.34')
    const saveResponse = page.waitForResponse(
      (response) =>
        response.request().method() === 'PUT' &&
        /\/api\/admin\/affiliate\/users\/\d+$/.test(
          new URL(response.url()).pathname
        )
    )
    await dialog.getByRole('button', { name: '保存邀请配置' }).click()
    const saved = (await (await saveResponse).json()).data
    expect(saved.rate_bps).toBe(1234)
    expect(saved.enabled).toBe(false)
    await expect(dialog).toHaveCount(0)
    await userRow
      .getByRole('button', { name: '配置邀请码', exact: true })
      .click()
    await expect(
      dialog.locator('input[name="admin-user-affiliate-code"]')
    ).toHaveValue(initial.code)
    const resetResponse = page.waitForResponse(
      (response) =>
        response.request().method() === 'PUT' &&
        /\/api\/admin\/affiliate\/users\/\d+$/.test(
          new URL(response.url()).pathname
        )
    )
    await dialog.getByRole('button', { name: '重置', exact: true }).click()
    const reset = (await (await resetResponse).json()).data
    expect(reset.rate_bps).toBe(1234)
    expect(reset.enabled).toBe(false)
    expect(reset.code).not.toBe(initial.code)
    await expect(
      dialog.locator('input[name="admin-user-affiliate-code"]')
    ).toHaveValue(reset.code)
    await page.screenshot({
      path: testInfo.outputPath('admin-referral-reset-retains-policy.png'),
      fullPage: true,
    })
    await dialog
      .locator('input[name="admin-user-affiliate-code"]')
      .fill(initial.code)
    if (initial.enabled)
      await dialog
        .getByRole('switch', { name: '启用邀请码', exact: true })
        .click()
    if (initial.rate_bps === null) {
      await dialog
        .getByRole('switch', { name: '专属返利比例', exact: true })
        .click()
    } else {
      await dialog
        .getByRole('spinbutton', { name: '返利比例（百分比）' })
        .fill(String(initial.rate_bps / 100))
    }
    const restoreResponse = page.waitForResponse(
      (response) =>
        response.request().method() === 'PUT' &&
        /\/api\/admin\/affiliate\/users\/\d+$/.test(
          new URL(response.url()).pathname
        )
    )
    await dialog.getByRole('button', { name: '保存邀请配置' }).click()
    const restored = (await (await restoreResponse).json()).data
    expect(restored.code).toBe(initial.code)
    expect(restored.enabled).toBe(initial.enabled)
    expect(restored.rate_bps).toBe(initial.rate_bps)
    writeFileSync(
      testInfo.outputPath('admin-business-ui-result.json'),
      JSON.stringify({
        real_support_reply: true,
        saved_rate_bps: 1234,
        reset_preserved_disabled: true,
        reset_preserved_rate: true,
        restored_original_code_rate_and_enabled: true,
      }),
      { mode: 0o600 }
    )
  })
}
