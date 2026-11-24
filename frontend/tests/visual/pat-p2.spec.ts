import { expect, test, type Page } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

const endpoint = '/api/user/access_tokens'
const now = Math.floor(new Date('2026-07-27T12:00:00+08:00').getTime() / 1000)
const secret = `nap_${'p'.repeat(43)}`
const old = {
  id: 7,
  name: 'existing automation',
  scopes: ['profile:read'],
  created_at: now - 120,
  last_used_at: 0,
  expires_at: now + 86400,
  token_ref: 'native-pat-ref',
  token_hint: 'nap_****',
  last_used_ip: '',
}

async function verification(page: Page) {
  const dialog = page.getByRole('dialog', { name: '验证身份', exact: true })
  await expect(dialog).toBeVisible()
  await dialog.getByLabel('验证码', { exact: true }).fill('123456')
  await dialog.getByRole('button', { name: '确认', exact: true }).click()
  await expect(dialog).toHaveCount(0)
}

async function prepare(page: Page, theme: 'light' | 'dark', mobile: boolean) {
  await page.setViewportSize(
    mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
  )
  await configureStablePage(page, { theme, authenticated: true })
  let tokens = [
    { ...old },
    { ...old, id: 8, name: 'expired automation', expires_at: now - 1 },
  ]
  const operations: Array<Record<string, unknown>> = []
  let proofCount = 0
  await page.route(`**${endpoint}/catalog`, (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          groups: [
            {
              group: 'personal',
              resources: ['profile', 'api_key'].map((resource) => ({
                resource,
                label_key: resource === 'profile' ? 'Profile' : 'API Keys',
                actions: [
                  { action: 'read', label_key: 'View', description_key: '' },
                ],
              })),
            },
          ],
          max_tokens: 20,
          default_expiry_days: 30,
        },
      },
    })
  )
  await page.route('**/api/verify/methods?*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          scope: new URL(route.request().url()).searchParams.get('scope'),
          methods: [{ method: '2fa', available: true }],
          oauth_providers: [],
          password_encryption_enabled: false,
        },
      },
    })
  )
  await page.route('**/api/verify', (route) => {
    const operation = route.request().postDataJSON()
    operations.push(operation)
    proofCount++
    return route.fulfill({
      json: {
        success: true,
        data: {
          proof_token: `proof-${proofCount}`,
          expires_at: now + 600,
          method: '2fa',
          scope: operation.scope,
        },
      },
    })
  })
  await page.route(`**${endpoint}`, (route) => {
    if (route.request().method() === 'GET')
      return route.fulfill({
        json: { success: true, data: { items: tokens, legacy: null } },
      })
    const input = route.request().postDataJSON()
    expect(route.request().headers()['x-security-proof']).toBe(
      `proof-${proofCount}`
    )
    const item = {
      ...old,
      id: 10,
      name: input.name,
      scopes: input.scopes,
      expires_at: now + 30 * 86400,
    }
    tokens = [item, ...tokens]
    return route.fulfill({
      json: {
        success: true,
        data: { token: secret, item },
      },
    })
  })
  await page.route(new RegExp(`${endpoint}/\\d+$`), (route) => {
    const id = Number(new URL(route.request().url()).pathname.split('/').at(-1))
    if (route.request().method() === 'DELETE') {
      expect(route.request().headers()['x-security-proof']).toBe(
        `proof-${proofCount}`
      )
      tokens = tokens.filter((token) => token.id !== id)
      return route.fulfill({ json: { success: true, data: null } })
    }
    const input = route.request().postDataJSON()
    if (input.scopes)
      expect(route.request().headers()['x-security-proof']).toBe(
        `proof-${proofCount}`
      )
    else expect(route.request().headers()['x-security-proof']).toBeUndefined()
    tokens = tokens.map((token) =>
      token.id === id ? { ...token, ...input } : token
    )
    return route.fulfill({
      json: {
        success: true,
        data: tokens.find((token) => token.id === id),
      },
    })
  })
  return { operations }
}

for (const [theme, mobile] of [
  ['light', false],
  ['dark', true],
] as const) {
  test(`PAT management ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
    page,
  }) => {
    const { operations } = await prepare(page, theme, mobile)
    await page.goto('/security')
    await waitForStablePage(page)
    const panel = page.getByTestId('pat-panel')
    await expect(
      panel.getByText('existing automation', { exact: true })
    ).toBeVisible()
    await expect(panel.locator('li [data-handdrawn="chip"]')).toHaveCount(2)
    for (const name of ['expired automation']) {
      const row = panel.locator('li').filter({ hasText: name })
      await expect(row.getByRole('button')).toHaveCount(0)
    }
    await panel
      .getByRole('button', { name: '创建访问令牌', exact: true })
      .click()
    const create = page.getByRole('dialog', {
      name: '创建访问令牌',
      exact: true,
    })
    await expect(create.locator('header h2.display-title')).toHaveText(
      '创建访问令牌'
    )
    await expect(create.locator('footer')).toBeVisible()
    await create.getByLabel('名称', { exact: true }).fill('browser automation')
    await create.locator('input[type=checkbox][value="profile:read"]').check()
    await create.getByRole('button', { name: '保存', exact: true }).click()
    await verification(page)
    await expect(page.getByTestId('pat-secret')).toHaveText(secret)
    expect(operations.at(-1)).toMatchObject({
      scope: 'access_token.generate',
    })
    await assertNoHorizontalOverflow(page)
    await page.getByRole('button', { name: '完成', exact: true }).click()
    await expect(page.getByTestId('pat-secret')).toHaveCount(0)
    await panel.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(
      panel.getByText('browser automation', { exact: true })
    ).toBeVisible()
    expect(await page.content()).not.toContain(secret)

    const row = panel.locator('li').filter({ hasText: 'browser automation' })
    await row.getByRole('button', { name: '改名', exact: true }).click()
    const edit = page.getByRole('dialog', {
      name: '编辑访问令牌',
      exact: true,
    })
    await edit.getByLabel('名称', { exact: true }).fill('renamed automation')
    await edit.getByRole('button', { name: '保存', exact: true }).click()
    await expect(edit).toHaveCount(0)
    expect(operations).toHaveLength(1)
    const renamed = panel
      .locator('li')
      .filter({ hasText: 'renamed automation' })
    await renamed.getByRole('button', { name: '权限', exact: true }).click()
    await edit.locator('input[type=checkbox][value="api_key:read"]').check()
    await edit.getByRole('button', { name: '保存', exact: true }).click()
    await verification(page)
    expect(operations.at(-1)).toMatchObject({
      scope: 'access_token.update',
      context: { token_id: 10, scopes: ['api_key:read', 'profile:read'] },
    })
    await expect(edit).toHaveCount(0)
    await renamed.getByRole('button', { name: '撤销', exact: true }).click()
    await page
      .getByRole('dialog', { name: '撤销访问令牌？', exact: true })
      .getByRole('button', { name: '撤销', exact: true })
      .click()
    await verification(page)
    expect(operations.at(-1)).toMatchObject({
      scope: 'access_token.revoke',
      context: { token_id: 10 },
    })
    await expect(renamed).toHaveCount(0)
    await assertNoHorizontalOverflow(page)
    expect(await page.content()).not.toContain(secret)
  })
}

test('PAT late creation cannot reveal a secret after session sign-out', async ({
  page,
}) => {
  await prepare(page, 'dark', true)
  let release!: () => void
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  let received!: () => void
  const started = new Promise<void>((resolve) => {
    received = resolve
  })
  await page.route(`**${endpoint}`, async (route) => {
    if (route.request().method() !== 'POST') return route.fallback()
    received()
    await pending
    await route.fulfill({
      json: {
        success: true,
        data: { token: secret, item: { ...old, id: 10 } },
      },
    })
  })
  await page.goto('/security')
  await page
    .getByTestId('pat-panel')
    .getByRole('button', { name: '创建访问令牌', exact: true })
    .click()
  const editor = page.getByRole('dialog', { name: '创建访问令牌', exact: true })
  await editor.getByLabel('名称', { exact: true }).fill('late result')
  await editor.locator('input[type=checkbox][value="profile:read"]').check()
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  await verification(page)
  await started
  await page.route('**/api/user/auth/refresh*', (route) =>
    route.fulfill({
      status: 401,
      json: {
        success: false,
        code: 'AUTH_UNAUTHORIZED',
        message: 'signed out',
      },
    })
  )
  await page.evaluate(() => {
    const channel = new BroadcastChannel('new-api:auth-session')
    channel.postMessage({
      kind: 'signed_out',
      sid: 'fixture-session',
      source: 'other-local-tab',
      nonce: 'logout-pat-test',
      timestamp: Date.now(),
    })
    channel.close()
  })
  await expect(page.getByTestId('pat-panel')).toHaveCount(0)
  release()
  await expect(page.getByTestId('pat-secret')).toHaveCount(0)
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(await page.content()).not.toContain(secret)
})
