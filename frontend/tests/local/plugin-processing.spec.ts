import { expect, test, type BrowserContext, type Page } from '@playwright/test'
import { localSession } from './session'
import type { RequestPluginInstance } from '../../src/features/task-plugins/types'

const { session } = localSession()

// Authentication and route authorization use the isolated backend. Plugin
// interactions use explicit local contracts, so no provider/site is contacted.
async function login(page: Page, role: 'root' | 'user') {
  await page.goto('/sign-in')
  await page
    .locator('input[autocomplete="username"]')
    .fill(session.accounts[role].username)
  await page
    .locator('input[autocomplete="current-password"]')
    .fill(session.accounts[role].password)
  const deadline = Date.now() + 60_000
  for (let attempt = 1; attempt <= 3; attempt++) {
    const response = page.waitForResponse(
      (item) =>
        new URL(item.url()).pathname === '/api/user/login' &&
        item.request().method() === 'POST'
    )
    await page.locator('form button[type="submit"]').click()
    const result = await response
    if (result.status() !== 429) {
      expect(result.status()).toBe(200)
      const value = await result.json()
      expect(value.success).toBe(true)
      await expect(page).toHaveURL(/\/dashboard$/)
      return value.data.access_token as string
    }
    // Rate-limit responses have no JSON body. Respect the server's window;
    // never retry early just to fit this test's bounded retry budget.
    const retryAfter = (await result.headerValue('retry-after'))?.trim()
    const requestedWait = retryAfter
      ? /^\d+$/.test(retryAfter)
        ? Number(retryAfter) * 1000
        : Date.parse(retryAfter) - Date.now()
      : NaN
    const wait = Number.isFinite(requestedWait)
      ? Math.max(250, requestedWait)
      : 1000 * attempt
    if (attempt === 3 || wait > deadline - Date.now())
      throw new Error(
        `Login rate limit exceeds the 3-attempt / 60-second retry budget (Retry-After: ${retryAfter || 'missing'})`
      )
    await page.waitForTimeout(wait)
  }
  throw new Error('Login retry budget exhausted')
}

async function isolate(
  context: BrowserContext,
  theme: string,
  violations: string[]
) {
  await context.addInitScript((theme) => {
    if (location.protocol !== 'http:') return
    localStorage.setItem('ren2hub_theme_mode', theme)
    localStorage.setItem('ren2hub_locale', 'zh-CN')
  }, theme)
  await context.route('**/*', async (route) => {
    const url = new URL(route.request().url())
    if (
      url.origin !== session.baseURL &&
      !['data:', 'blob:', 'about:'].includes(url.protocol)
    ) {
      violations.push(`external request: ${url.protocol}//${url.host}`)
      await route.abort('blockedbyclient')
      return
    }
    await route.continue()
  })
  await context.routeWebSocket('**/*', (socket) => {
    violations.push('unexpected websocket')
    socket.close()
  })
  context.on('page', (page) =>
    page.on('pageerror', (error) => violations.push(`pageerror: ${error.name}`))
  )
}

async function noOverflow(page: Page) {
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)
  ).toBeLessThanOrEqual(1)
}

async function noCallbackSecrets(page: Page) {
  const persisted = await page.evaluate(() =>
    JSON.stringify({
      href: location.href,
      history: history.state,
      local: { ...localStorage },
      session: { ...sessionStorage },
    })
  )
  for (const value of [
    'guard-secret',
    'guard-flow',
    'guard-fragment',
    'late-flow',
    'late-secret',
  ])
    expect(persisted).not.toContain(value)
}

test('插件适配：权限、请求处理、provider 配置和登录回调（隔离接口契约）', async ({
  browser,
}, info) => {
  test.setTimeout(240_000)
  const violations: string[] = []
  let releaseLateCallback: (() => void) | undefined
  const contextOptions = {
    baseURL: session.baseURL,
    viewport: info.project.use.viewport,
    colorScheme: info.project.use.colorScheme,
    locale: 'zh-CN',
    reducedMotion: 'reduce' as const,
    serviceWorkers: 'block' as const,
  }
  const context = await browser.newContext(contextOptions)
  const userContext = await browser.newContext(contextOptions)
  context.setDefaultTimeout(15_000)
  userContext.setDefaultTimeout(15_000)
  try {
    await isolate(context, info.project.use.colorScheme || 'light', violations)
    await isolate(
      userContext,
      info.project.use.colorScheme || 'light',
      violations
    )
    const user = await userContext.newPage()
    let deniedCallbackSubmissions = 0
    await userContext.route('**/api/channel/plugin_login/callback', (route) => {
      deniedCallbackSubmissions++
      return route.abort('blockedbyclient')
    })
    const deniedCallbackPath = '/oauth/plugin/local-provider/callback'
    const deniedCallback = `${deniedCallbackPath}?code=guard-secret&state=guard-flow#guard-fragment`
    await test.step('未登录回调清参后进入登录页，不在 redirect 或浏览器存储保留凭证', async () => {
      await user.goto(deniedCallback)
      await expect(user).toHaveURL(
        (url) =>
          url.pathname === '/sign-in' &&
          url.searchParams.get('redirect') === deniedCallbackPath
      )
      await noCallbackSecrets(user)
      expect(deniedCallbackSubmissions).toBe(0)
      await user.reload()
      await noCallbackSecrets(user)
      expect(deniedCallbackSubmissions).toBe(0)
      await user.screenshot({
        path: info.outputPath('plugin-callback-unauthenticated.png'),
        fullPage: true,
      })
    })
    await test.step('真实用户权限不能进入请求处理或写入实例', async () => {
      const token = await login(user, 'user')
      await user.goto(deniedCallback)
      await expect(user).toHaveURL(/\/dashboard$/)
      await noCallbackSecrets(user)
      expect(deniedCallbackSubmissions).toBe(0)
      await user.goto('/request-processing')
      await expect(user).toHaveURL(/\/dashboard$/)
      await expect(user.locator('a[href="/request-processing"]')).toHaveCount(0)
      const denied = await user.request.put('/api/request_plugin/instances', {
        headers: { Authorization: `Bearer ${token}` },
        data: { instances: [] },
      })
      expect(denied.status()).toBe(403)
      const dryRunDenied = await user.request.post(
        '/api/request_plugin/dry_run',
        {
          headers: { Authorization: `Bearer ${token}` },
          data: { format: 'openai', model: 'fixture', body: {} },
        }
      )
      expect(dryRunDenied.status()).toBe(403)
    })
    await userContext.close()
    const page = await context.newPage()
    await login(page, 'root')
    let instances: RequestPluginInstance[] = []
    let saved: Record<string, unknown> | undefined
    let dryRun: Record<string, unknown> | undefined
    let callbackSubmissions = 0
    let loggedIn = false
    const channel = {
      id: 901,
      name: 'Local Provider',
      type: 61,
      lab: { id: 'plugin', name: 'Plugin', source: 'vendor' },
      status: 1,
      priority: 0,
      weight: 10,
      in_flight: 0,
      max_concurrency: 0,
      channel_ratio: '1',
      used_quota: 0,
      balance: 0,
      response_time: 0,
      test_time: 0,
      base_url: session.baseURL,
      models: 'fixture-model',
      model_mapping: '',
      other: '',
      settings: '{}',
      setting: JSON.stringify({
        task_plugin_key: 'local-provider',
        plugin_config: { region: 'old' },
      }),
    }
    const options = [
      {
        key: 'local-transform',
        name: 'Local Transform',
        task: false,
        provider: false,
        transform: true,
        models: [],
        configSchema: {
          type: 'object',
          properties: { note: { type: 'string', title: '备注', default: '' } },
        },
      },
      {
        key: 'local-provider',
        name: 'Local Provider',
        task: false,
        provider: true,
        transform: false,
        login: true,
        balance: true,
        models: ['fixture-model'],
        relayPaths: ['/v1/responses'],
        responsesWebsocket: true,
        channelConfig: {
          type: 'object',
          properties: {
            region: { type: 'string', title: '区域', default: 'local' },
          },
        },
      },
    ]
    await context.route('**/api/task_plugin_options', (route) =>
      route.fulfill({ json: { success: true, data: options } })
    )
    await context.route('**/api/request_plugin/instances', async (route) => {
      if (route.request().method() === 'PUT')
        instances = route.request().postDataJSON().instances
      await route.fulfill({ json: { success: true, data: { instances } } })
    })
    await context.route('**/api/request_plugin/dry_run', async (route) => {
      dryRun = route.request().postDataJSON()
      await route.fulfill({
        json: {
          success: true,
          data: {
            body: dryRun?.body,
            steps: [
              {
                instance: 'fixture',
                name: 'Local Transform',
                plugin: 'local-transform',
                stage: 'reply',
                action: 'allow',
                effects: 0,
                duration_ms: 1,
              },
            ],
            record: [],
            reply: dryRun?.reply,
          },
        },
      })
    })
    await test.step('请求处理的顺序保存和回复试运行', async () => {
      await page.goto('/request-processing')
      await page.getByRole('button', { name: '添加实例', exact: true }).click()
      await page
        .getByRole('textbox', { name: '名称', exact: true })
        .fill('First')
      await page
        .getByRole('textbox', { name: '备注', exact: true })
        .fill('first-config')
      await page.getByRole('button', { name: '添加实例', exact: true }).click()
      await page
        .getByRole('textbox', { name: '名称', exact: true })
        .nth(1)
        .fill('Second')
      await page
        .getByRole('button', { name: '上移', exact: true })
        .nth(1)
        .click()
      await page
        .getByRole('button', { name: '保存全部实例', exact: true })
        .click()
      await expect
        .poll(() => instances.map((item) => item.name))
        .toEqual(['Second', 'First'])
      expect(instances[1]?.config).toEqual({ note: 'first-config' })
      expect(instances.every((item) => !('groups' in item))).toBe(true)
      await page.reload()
      await expect(
        page.getByRole('textbox', { name: '名称', exact: true }).first()
      ).toHaveValue('Second')
      await page
        .getByRole('textbox', { name: '请求模型', exact: true })
        .fill('fixture-model')
      await page
        .getByRole('textbox', { name: '示例回复（可选）', exact: true })
        .fill('Local answer')
      await page
        .getByRole('button', { name: '运行已保存实例', exact: true })
        .click()
      await expect.poll(() => dryRun?.reply).toBe('Local answer')
      expect(dryRun).not.toHaveProperty('instance')
      await expect(page.getByText('试运行结果', { exact: true })).toBeVisible()
      await page
        .getByRole('button', { name: '仅运行此草稿', exact: true })
        .first()
        .click()
      await expect.poll(() => Boolean(dryRun?.instance)).toBe(true)
      await noOverflow(page)
      await page.screenshot({
        path: info.outputPath('request-processing.png'),
        fullPage: true,
      })
    })
    await context.route(/\/api\/channel\/(?:\?.*)?$/, async (route) => {
      if (route.request().method() === 'PUT') {
        saved = route.request().postDataJSON()
        if (typeof saved?.setting === 'string') channel.setting = saved.setting
        return route.fulfill({ json: { success: true, data: null } })
      }
      return route.fulfill({
        json: {
          success: true,
          data: {
            items: [channel],
            total: 1,
            page: 1,
            page_size: 20,
            type_counts: { '61': 1 },
          },
        },
      })
    })
    await context.route('**/api/channel/labs', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            items: [
              { id: 'plugin', name: 'Plugin', source: 'vendor', count: 1 },
            ],
          },
        },
      })
    )
    await context.route('**/api/channel/901/plugin_credentials', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            items: loggedIn ? [{ index: 0, label: 'Local account' }] : [],
          },
        },
      })
    )
    await context.route('**/api/channel/901/plugin_login', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            flow: 'local-flow',
            step: {
              type: 'redirect',
              url: `${session.baseURL}/oauth/plugin/local-provider/callback?state=local-flow&code=local-once`,
            },
          },
        },
      })
    )
    await context.route(
      '**/api/channel/plugin_login/callback',
      async (route) => {
        callbackSubmissions++
        const payload = route.request().postDataJSON()
        const late = payload.params.state === 'late-flow'
        expect(payload).toEqual({
          plugin: 'local-provider',
          params: late
            ? { state: 'late-flow', code: 'late-secret' }
            : { state: 'local-flow', code: 'local-once' },
        })
        if (late) {
          await new Promise<void>((resolve) => {
            releaseLateCallback = resolve
          })
          if (route.request().failure()) return
        }
        loggedIn = true
        await route.fulfill({
          json: {
            success: true,
            data: {
              channel_id: 901,
              step: {
                type: 'done',
                credential: { index: 0, label: 'Local account' },
              },
            },
          },
        })
      }
    )
    await test.step('provider 配置读取 channelConfig 并保存，不要求静态 Key', async () => {
      await page.goto('/channels')
      await page.locator('[aria-label="编辑渠道"]:visible').first().click()
      const dialog = page.getByRole('dialog', { name: '编辑渠道', exact: true })
      await expect(
        dialog.getByRole('textbox', { name: '区域', exact: true })
      ).toHaveValue('old')
      await dialog
        .getByRole('textbox', { name: '区域', exact: true })
        .fill('local-region')
      await dialog.getByRole('button', { name: /^高级设置/ }).click()
      await dialog
        .getByRole('combobox', { name: '渠道请求处理实例（按执行顺序）' })
        .selectOption(instances[0]!.id)
      await dialog
        .getByRole('button', { name: '添加实例', exact: true })
        .click()
      await expect(
        dialog.locator('input[name="admin-channel-key"]')
      ).toHaveCount(0)
      await noOverflow(page)
      await page.screenshot({
        path: info.outputPath('provider-config.png'),
        fullPage: true,
      })
      await dialog
        .getByRole('button', { name: '保存渠道', exact: true })
        .click()
      await expect.poll(() => saved?.setting).toBeTruthy()
      expect(JSON.parse(String(saved?.setting)).plugin_config).toEqual({
        region: 'local-region',
      })
      expect(JSON.parse(String(saved?.setting)).request_plugins).toEqual([
        instances[0]!.id,
      ])
      await expect(dialog).toHaveCount(0)
    })
    await test.step('登录回调清参且单次提交，只有匹配 flow/channel 的广播能完成', async () => {
      await page.locator('[aria-label="编辑渠道"]:visible').first().click()
      const editor = page.getByRole('dialog', { name: '编辑渠道', exact: true })
      await editor
        .getByRole('button', { name: '登录账号', exact: true })
        .click()
      const loginDialog = page.getByRole('dialog', {
        name: '插件账号登录',
        exact: true,
      })
      await expect(
        loginDialog.getByText('等待授权完成', { exact: true })
      ).toBeVisible()
      await page.evaluate(() => {
        const channel = new BroadcastChannel('ren2hub-plugin-login')
        channel.postMessage({
          state: 'wrong',
          channel_id: 901,
          step: { type: 'done' },
        })
        channel.postMessage({
          state: 'local-flow',
          channel_id: 902,
          step: { type: 'done' },
        })
        channel.close()
      })
      await expect(
        loginDialog.getByText('登录完成', { exact: true })
      ).toHaveCount(0)
      const popupPromise = context.waitForEvent('page')
      await loginDialog
        .getByRole('link', { name: '打开授权页面', exact: true })
        .click()
      const popup = await popupPromise
      await expect(popup).toHaveURL(
        (url) =>
          url.pathname === '/oauth/plugin/local-provider/callback' &&
          !url.search &&
          !url.hash
      )
      await expect(
        loginDialog.getByText('登录完成', { exact: true })
      ).toBeVisible()
      expect(callbackSubmissions).toBe(1)
      await popup.reload()
      await expect(
        popup
          .getByText('登录未完成，请关闭此页并从登录弹窗重新开始。', {
            exact: true,
          })
          .first()
      ).toBeVisible()
      expect(callbackSubmissions).toBe(1)
      await popup.close()
      await noOverflow(page)
      await page.screenshot({
        path: info.outputPath('plugin-login-complete.png'),
        fullPage: true,
      })
      await loginDialog
        .getByRole('button', { name: '关闭', exact: true })
        .last()
        .click()
      await expect(
        editor.getByText('Local account', { exact: true })
      ).toBeVisible()
      await editor.getByRole('button', { name: '取消', exact: true }).click()
    })
    await test.step('回调提交期间真实注销使请求取消，迟到完成不广播也不重放', async () => {
      const broadcasts: unknown[] = []
      await context.exposeBinding('recordLatePluginCallback', (_, value) =>
        broadcasts.push(value)
      )
      const latePage = await context.newPage()
      await latePage.addInitScript(() => {
        const channel = new BroadcastChannel('ren2hub-plugin-login')
        channel.addEventListener('message', (event) => {
          const record = (
            window as unknown as {
              recordLatePluginCallback: (value: unknown) => void
            }
          ).recordLatePluginCallback
          record(event.data)
        })
      })
      await latePage.goto(
        '/oauth/plugin/local-provider/callback?state=late-flow&code=late-secret'
      )
      await expect.poll(() => callbackSubmissions).toBe(2)
      const cancelled = latePage.waitForEvent('requestfailed', {
        predicate: (request) =>
          new URL(request.url()).pathname ===
          '/api/channel/plugin_login/callback',
      })
      await page.getByRole('button', { name: '个人中心', exact: true }).click()
      const logout = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/user/auth/logout'
      )
      await page.getByRole('button', { name: '退出登录', exact: true }).click()
      expect((await logout).status()).toBe(200)
      await expect(latePage).toHaveURL((url) => url.pathname === '/sign-in')
      await cancelled
      releaseLateCallback?.()
      await noCallbackSecrets(latePage)
      expect(broadcasts).toEqual([])
      expect(callbackSubmissions).toBe(2)
      await latePage.screenshot({
        path: info.outputPath('plugin-callback-session-expired.png'),
        fullPage: true,
      })
      await latePage.close()
    })
    expect(violations).toEqual([])
  } finally {
    releaseLateCallback?.()
    await context.close()
    await userContext.close()
  }
})
