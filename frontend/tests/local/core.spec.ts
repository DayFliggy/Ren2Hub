import { expect, test, type Page } from '@playwright/test'
import { localSession } from './session'

const { session } = localSession()

async function login(page: Page, role: 'root' | 'user') {
  await page.goto('/sign-in')
  await page
    .locator('input[autocomplete="username"]')
    .fill(session.accounts[role].username)
  await page
    .locator('input[autocomplete="current-password"]')
    .fill(session.accounts[role].password)
  const response = page.waitForResponse(
    (item) =>
      new URL(item.url()).pathname === '/api/user/login' &&
      item.request().method() === 'POST'
  )
  await page.locator('form button[type="submit"]').click()
  expect((await response).status()).toBe(200)
  const value = await (await response).json()
  expect(value.success).toBe(true)
  await expect(page).toHaveURL(/\/dashboard$/)
  return value.data.access_token as string
}

async function layout(page: Page, name: string) {
  await page.waitForLoadState('networkidle')
  expect(
    await page.evaluate(() =>
      Math.max(0, document.documentElement.scrollWidth - window.innerWidth)
    ),
    name + ' 横向溢出'
  ).toBeLessThanOrEqual(1)
  await expect(page.locator('html')).toHaveClass(
    new RegExp(`\\b${session.project.endsWith('dark') ? 'dark' : 'light'}\\b`)
  )
}

async function navigateConsole(
  page: Page,
  item: 'keys' | 'wallet' | 'tickets'
) {
  const path = `/${item}`
  const menu = page.locator('[aria-controls="console-mobile-navigation"]')
  if (await menu.isVisible()) {
    await menu.click()
    const drawer = page.locator('[data-console-mobile-navigation]')
    await expect(drawer).toBeVisible()
    await drawer
      .locator(`a[data-console-nav-item="${item}"][href="${path}"]`)
      .click()
    await expect(drawer).not.toBeVisible()
  } else {
    await page.locator(`a[href="${path}"]:visible`).first().click()
  }
  await expect(page).toHaveURL((url) => url.pathname === path)
}

test('真实后端：会话、权限、Key 路由、钱包、工单与设置', async ({
  browser,
}, info) => {
  // Includes the added routing, wallet, ticket and retired-path flows.
  test.setTimeout(150_000)
  const violations: string[] = []
  const context = await browser.newContext({
    baseURL: session.baseURL,
    viewport: info.project.use.viewport,
    colorScheme: info.project.use.colorScheme,
    locale: 'zh-CN',
    reducedMotion: 'reduce',
    serviceWorkers: 'block',
  })
  context.setDefaultTimeout(15_000)
  await context.addInitScript((theme) => {
    if (location.protocol !== 'http:') return
    localStorage.setItem('ren2hub_theme_mode', theme)
    localStorage.setItem('ren2hub_locale', 'zh-CN')
  }, info.project.use.colorScheme || 'light')
  await context.route('**/*', async (route) => {
    const url = new URL(route.request().url())
    if (
      url.origin !== session.baseURL &&
      !['data:', 'blob:', 'about:'].includes(url.protocol)
    ) {
      violations.push('external request: ' + url.protocol + '//' + url.host)
      await route.abort('blockedbyclient')
      return
    }
    await route.continue()
  })
  await context.routeWebSocket('**/*', (socket) => {
    violations.push('unexpected websocket request')
    socket.close()
  })
  context.on('page', (page) => {
    page.on('pageerror', (error) => violations.push('pageerror: ' + error.name))
    page.on('response', (response) => {
      if (response.status() >= 500 || response.status() === 429)
        violations.push(
          `HTTP ${response.status()}: ${new URL(response.url()).pathname}`
        )
    })
  })
  const page = await context.newPage()
  try {
    await test.step('URL 邀请码预填，默认未确认', async () => {
      await page.goto('/sign-up?aff=LOCAL_INVITE')
      const field = page.locator('[data-affiliate-attribution]')
      await expect(field.locator('input[type="text"]')).toHaveValue(
        'LOCAL_INVITE'
      )
      await expect(field.getByRole('checkbox')).not.toBeChecked()
      await layout(page, '注册页')
      await page.screenshot({
        path: info.outputPath('registration.png'),
        fullPage: true,
      })
    })
    let userToken = ''
    let rootToken = ''
    let tokenId = 0
    await test.step('密码登录、刷新会话与管理员权限拒绝', async () => {
      userToken = await login(page, 'user')
      const refresh = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/user/auth/refresh'
      )
      await page.reload()
      expect((await refresh).status()).toBe(200)
      const rotated = await (await refresh).json()
      expect(rotated.success).toBe(true)
      userToken = rotated.data.access_token
      await expect(page).toHaveURL(/\/dashboard$/)
      const denied = await page.request.get('/api/option/', {
        headers: { Authorization: 'Bearer ' + userToken },
      })
      expect(denied.status()).toBe(403)
      expect((await denied.json()).success).toBe(false)
      const bonus = await page.request.get('/api/user/topup-bonus', {
        headers: { Authorization: 'Bearer ' + userToken },
      })
      expect(bonus.status()).toBe(200)
      const bonusBody = await bonus.json()
      expect(bonusBody.success).toBe(true)
      // The built-in configuration grants no bonus.
      expect(bonusBody.data.applied_rate).toBe('0')
      await expect(page.locator('[data-bonus-rate]')).toHaveText('0%')
      await expect(page.locator('[data-bonus-configure]')).toHaveCount(0)
      const bonusSettings = await page.request.get('/api/option/topup_bonus', {
        headers: { Authorization: 'Bearer ' + userToken },
      })
      expect(bonusSettings.status()).toBe(403)
      await layout(page, '用户首页')
    })
    await test.step('从页面创建 Token，列表仅返回脱敏 Key，阻止跨用户读取和删除', async () => {
      await navigateConsole(page, 'keys')
      await page.getByRole('button', { name: '创建令牌', exact: true }).click()
      const modal = page.getByRole('dialog')
      const name = 'local-browser-' + info.project.name
      await modal.locator('input[name="token-name"]').fill(name)
      const saved = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/token/' &&
          response.request().method() === 'POST'
      )
      await modal.getByRole('button', { name: '确认', exact: true }).click()
      expect((await saved).status()).toBe(200)
      expect((await (await saved).json()).success).toBe(true)
      await expect(modal).toHaveCount(0)
      await expect(
        page.getByText(name, { exact: true }).filter({ visible: true })
      ).toBeVisible()
      const headers = { Authorization: 'Bearer ' + userToken }
      const listed = await page.request.get('/api/token/', { headers })
      expect(listed.status()).toBe(200)
      const value = await listed.json()
      expect(value.success).toBe(true)
      const item = value.data.items.find(
        (row: { name: string }) => row.name === name
      )
      expect(Boolean(item)).toBe(true)
      tokenId = item.id
      expect(typeof item.key === 'string' && item.key.includes('*')).toBe(true)
      const peer = await browser.newContext({ baseURL: session.baseURL })
      try {
        const authenticated = await peer.request.post('/api/user/login', {
          data: session.accounts.peer,
        })
        expect(authenticated.status()).toBe(200)
        const peerSession = await authenticated.json()
        expect(peerSession.success).toBe(true)
        const peerHeaders = {
          Authorization: 'Bearer ' + peerSession.data.access_token,
        }
        for (const method of ['get', 'delete'] as const) {
          const denied = await peer.request[method]('/api/token/' + item.id, {
            headers: peerHeaders,
          })
          expect(denied.status()).toBe(200)
          expect((await denied.json()).success).toBe(false)
        }
        const retained = await page.request.get('/api/token/' + item.id, {
          headers,
        })
        expect((await retained.json()).success).toBe(true)
      } finally {
        await peer.close()
      }
      await layout(page, 'Token 列表')
      await page.screenshot({
        path: info.outputPath('tokens.png'),
        fullPage: true,
      })
    })
    await test.step('每 Key 路由通过编辑弹窗保存并在刷新后保持', async () => {
      await page.locator('[data-key-route-trigger]:visible').first().click()
      const dialog = page.getByRole('dialog', { name: '编辑令牌', exact: true })
      await expect(dialog).toBeVisible()
      // The empty install has no channels, so this flow checks the default
      // automatic all-platform state, the client-side block on an empty
      // ordered list, and saving a ratio cap via the modal.
      const orderedMode = dialog.getByRole('radio', { name: /顺序路由/ })
      const automatic = dialog.getByRole('radio', { name: /自动路由/ })
      const confirm = dialog.getByRole('button', { name: '确认', exact: true })
      await expect(automatic).toHaveAttribute('aria-checked', 'true')
      const writes: string[] = []
      const recordWrite = (request: import('@playwright/test').Request) => {
        if (
          request.method() === 'PUT' &&
          new URL(request.url()).pathname === `/api/token/${tokenId}/routing`
        )
          writes.push(request.url())
      }
      page.on('request', recordWrite)
      try {
        await orderedMode.click()
        await expect(
          dialog.locator('[data-routing-ordered-empty]')
        ).toBeVisible()
        await confirm.click()
        expect(writes).toEqual([])
      } finally {
        page.off('request', recordWrite)
      }
      await automatic.click()
      await dialog.getByRole('button', { name: /渠道倍率上限/ }).click()
      await dialog.locator('input[name="token-max-channel-ratio"]').fill('2')
      const saved = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname ===
            `/api/token/${tokenId}/routing` &&
          response.request().method() === 'PUT'
      )
      await confirm.click()
      const response = await saved
      expect(response.status()).toBe(200)
      const body = await response.json()
      expect(body.success).toBe(true)
      expect(body.data).toMatchObject({
        mode: 'auto',
        selected_only: false,
        max_channel_ratio: '2',
        entries: [],
        version: 1,
      })
      await expect(dialog).toHaveCount(0)
      await page.reload()
      await page.locator('[data-key-route-trigger]:visible').first().click()
      await expect(
        page.getByRole('dialog', { name: '编辑令牌', exact: true })
      ).toBeVisible()
      await expect(
        page.locator('input[name="token-max-channel-ratio"]')
      ).toHaveValue('2')
      await layout(page, 'Key 路由弹窗')
      await page.screenshot({
        path: info.outputPath('token-routing.png'),
        fullPage: true,
      })
      await page
        .getByRole('dialog', { name: '编辑令牌', exact: true })
        .getByRole('button', { name: '取消', exact: true })
        .click()
    })
    await test.step('钱包使用原生信息，空安装不创建支付订单', async () => {
      const infoResponse = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/user/topup/info'
      )
      const paymentWrites: string[] = []
      const recordPayment = (request: import('@playwright/test').Request) => {
        if (
          request.method() === 'POST' &&
          new URL(request.url()).pathname.startsWith('/api/user/topup/')
        )
          paymentWrites.push(new URL(request.url()).pathname)
      }
      page.on('request', recordPayment)
      try {
        await navigateConsole(page, 'wallet')
        const response = await infoResponse
        expect(response.status()).toBe(200)
        expect((await response.json()).success).toBe(true)
        await expect(
          page.getByRole('heading', { name: '充值记录', exact: true })
        ).toBeVisible()
        await layout(page, '钱包')
        expect(paymentWrites).toEqual([])
        await page.screenshot({
          path: info.outputPath('wallet.png'),
          fullPage: true,
        })
      } finally {
        page.off('request', recordPayment)
      }
    })
    await test.step('创建工单并读取真实详情和消息', async () => {
      await navigateConsole(page, 'tickets')
      await page.getByRole('button', { name: '新建工单', exact: true }).click()
      const modal = page.getByRole('dialog', { name: '新建工单', exact: true })
      const title = '本地回归 ' + info.project.name
      const content = '仅用于隔离实例验证的工单内容'
      await modal.locator('[name="ticket-title"]').fill(title)
      await modal.locator('[name="ticket-content"]').fill(content)
      const created = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/tickets' &&
          response.request().method() === 'POST'
      )
      await modal.getByRole('button', { name: '确认', exact: true }).click()
      expect((await created).status()).toBe(200)
      expect((await (await created).json()).success).toBe(true)
      await expect(modal).toHaveCount(0)
      await page.getByRole('button', { name: title, exact: true }).click()
      await expect(page).toHaveURL(/\/tickets\/\d+$/)
      await expect(page.getByText(content, { exact: true })).toBeVisible()
      await layout(page, '工单详情')
      await page.screenshot({
        path: info.outputPath('ticket.png'),
        fullPage: true,
      })
    })
    await test.step('退役入口返回标准未找到页面', async () => {
      // The native server rejects this retired namespace before serving the SPA.
      expect((await page.request.get('/market')).status()).toBe(404)
      for (const path of [
        '/invoice',
        '/farm',
        '/bigame',
        '/gateway',
        '/request-diagnostics',
      ]) {
        // Exercise stale browser-history entries without bootstrapping a new
        // session for every path and consuming the shared critical API limit.
        await page.evaluate((path) => {
          history.pushState({}, '', path)
          dispatchEvent(new PopStateEvent('popstate'))
        }, path)
        await expect(page).toHaveURL(new URL(path, session.baseURL).href)
        await expect(
          page.getByText('页面不存在', { exact: true })
        ).toBeVisible()
      }
      await page.goto('/keys')
    })
    await test.step('注销使刷新凭证失效', async () => {
      await page.getByRole('button', { name: '个人中心', exact: true }).click()
      const logout = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/user/auth/logout'
      )
      await page.getByRole('button', { name: '退出登录', exact: true }).click()
      expect((await logout).status()).toBe(200)
      await expect(page).toHaveURL(/\/sign-in$/)
      const refresh = await page.request.post('/api/user/auth/refresh')
      expect(refresh.status()).toBe(401)
      await page.goto('/dashboard')
      await expect(page).toHaveURL(/\/sign-in(?:\?|$)/)
    })
    await test.step('管理员空设置保存不发写请求，实际修改可保存', async () => {
      rootToken = await login(page, 'root')
      const loaded = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/option/' &&
          response.request().method() === 'GET'
      )
      await page.goto('/system-settings/content/console-content')
      const options = await (await loaded).json()
      expect(options.success).toBe(true)
      await page.waitForLoadState('networkidle')
      const toggle = page.getByRole('switch', { name: '显示公告', exact: true })
      await expect(toggle).toBeVisible()
      const writes: string[] = []
      page.on('request', (request) => {
        if (
          new URL(request.url()).pathname === '/api/option/' &&
          request.method() === 'PUT'
        )
          writes.push(request.postDataJSON().key)
      })
      const save = page.getByRole('button', { name: '保存更改', exact: true })
      await expect(save).toBeDisabled()
      expect(writes).toEqual([])
      const original = options.data.find(
        (option: { key: string }) =>
          option.key === 'console_setting.announcements_enabled'
      ).value
      await expect(toggle).toHaveAttribute('aria-checked', original)
      await toggle.click()
      const changed = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/option/' &&
          response.request().method() === 'PUT'
      )
      await save.click()
      expect((await changed).status()).toBe(200)
      expect((await (await changed).json()).success).toBe(true)
      await page.reload()
      await expect(toggle).toHaveAttribute(
        'aria-checked',
        original === 'true' ? 'false' : 'true'
      )
      expect(writes).toEqual(['console_setting.announcements_enabled'])
      await layout(page, '系统设置')
      await page.screenshot({
        path: info.outputPath('settings.png'),
        fullPage: true,
      })
    })
    await test.step('加赠卡片读取真实配置、封顶结果与个人窗口，刷新后保持', async () => {
      const headers = { Authorization: 'Bearer ' + rootToken }
      const loaded = await page.request.get('/api/option/topup_bonus', {
        headers,
      })
      expect(loaded.status()).toBe(200)
      const settings = await loaded.json()
      expect(settings.success).toBe(true)
      const active = (rate: string) => ({
        mode: 'on',
        tiers: [{ threshold_quota: '0', rate }],
      })
      // Only the runner's isolated instance is writable; no production data.
      const saved = await page.request.patch('/api/option/topup_bonus', {
        headers,
        data: {
          expected_version: settings.data.version,
          config: {
            ...settings.data.config,
            fixed_rate: '0.01',
            cap_rate: '0.05',
            excluded_user_ids: [],
            site: active('0.02'),
            personal: {
              day: active('0.01'),
              week: active('0.03'),
              month: active('0.04'),
            },
          },
        },
      })
      expect(saved.status()).toBe(200)
      expect((await saved.json()).success).toBe(true)
      const self = await page.request.get('/api/user/topup-bonus', {
        headers,
      })
      expect(self.status()).toBe(200)
      const result = await self.json()
      expect(result.success).toBe(true)
      expect(result.data).not.toHaveProperty('factor')
      expect(result.data).toMatchObject({
        fixed_rate: '0.01',
        site_rate: '0.02',
        personal_rate: '0.04',
        raw_rate: '0.07',
        applied_rate: '0.05',
        cap_applied: true,
        winner_window: 'month',
      })
      await page.goto('/dashboard')
      const card = page.locator('[data-bonus-card]')
      for (const refresh of [false, true]) {
        if (refresh) await page.reload()
        await expect(card.locator('[data-bonus-rate]')).toHaveText('+5%')
        await expect(card.locator('[data-bonus-global-rate]')).toHaveText(
          '加赠 3%'
        )
        await expect(card.locator('[data-bonus-personal-rate]')).toHaveText(
          '加赠 4%'
        )
        await expect(card.locator('[data-bonus-cap]')).toContainText('5%')
        await expect(card.locator('[data-bonus-window]')).toHaveCount(3)
        await expect(card.locator('[data-selected="true"]')).toHaveAttribute(
          'data-bonus-window',
          'month'
        )
      }
      await layout(page, '加赠卡片')
      await card.screenshot({ path: info.outputPath('bonus-card-live.png') })
      await card.locator('[data-bonus-configure]').click()
      await expect(page).toHaveURL(/\/system-settings\/billing\/topup-bonus$/)
      const editor = page.locator('[data-bonus-editor]')
      await expect(editor).toBeVisible()
      await expect(page.locator('[data-bonus-status]')).toHaveCount(0)
      await editor.getByLabel('固定加赠', { exact: true }).fill('1.25')
      const updated = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/option/topup_bonus' &&
          response.request().method() === 'PATCH'
      )
      await editor
        .getByRole('button', { name: '保存配置', exact: true })
        .click()
      const update = await updated
      expect(update.status()).toBe(200)
      expect((await update.json()).success).toBe(true)
      const reloaded = await page.request.get('/api/option/topup_bonus', {
        headers,
      })
      expect((await reloaded.json()).data.config.fixed_rate).toBe('0.0125')
      await page.reload()
      await expect(editor.getByLabel('固定加赠', { exact: true })).toHaveValue(
        '1.25'
      )
      await editor.getByRole('tab', { name: /近 30 日/ }).click()
      await layout(page, '充值加赠')
      await editor.screenshot({
        path: info.outputPath('topup-bonus-live.png'),
      })
    })
  } finally {
    await context.close()
    expect
      .soft(violations, '禁止外网访问、页面异常、服务端 5xx 和限频')
      .toEqual([])
  }
})
