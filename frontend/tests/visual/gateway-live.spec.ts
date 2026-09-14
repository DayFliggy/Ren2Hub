import { randomUUID } from 'node:crypto'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow } from './fixtures'

test.describe('gateway isolated backend', () => {
  test.skip(
    process.env.GATEWAY_LIVE_TEST !== '1',
    'requires the isolated local application'
  )
  let credentials: { username: string; password: string }

  test.beforeAll(async ({ request, baseURL }) => {
    expect(new URL(baseURL!).hostname).toBe('127.0.0.1')
    const file = resolve('../.local-tests/gateway/app/credentials.json')
    const setup = await (await request.get('/api/setup')).json()
    expect(setup.success).toBe(true)
    if (!setup.data.status) {
      credentials = { username: 'gatewaytest', password: randomUUID() }
      const result = await request.post('/api/setup', {
        data: {
          ...credentials,
          confirmPassword: credentials.password,
          SelfUseModeEnabled: true,
          DemoSiteEnabled: false,
        },
      })
      expect((await result.json()).success).toBe(true)
      writeFileSync(file, JSON.stringify(credentials), { mode: 0o600 })
    } else {
      expect(
        existsSync(file),
        'only the test-created application may be reused'
      ).toBe(true)
      credentials = JSON.parse(readFileSync(file, 'utf8'))
    }
    const login = await (
      await request.post('/api/user/login', { data: credentials })
    ).json()
    expect(login.success).toBe(true)
    const created = await request.post('/api/user/', {
      headers: { Authorization: `Bearer ${login.data.access_token}` },
      data: {
        username: `gw_${randomUUID().slice(0, 8)}`,
        password: randomUUID().slice(0, 16),
        display_name: 'Gateway test',
        role: 1,
      },
    })
    const result = await created.json()
    expect(result.success, result.message).toBe(true)
  })

  for (const theme of ['light', 'dark'] as const) {
    for (const viewport of [
      { name: 'desktop', width: 1440, height: 1000 },
      { name: 'mobile', width: 390, height: 844 },
    ]) {
      test(`${theme} ${viewport.name} persists admission settings`, async ({
        page,
      }) => {
        await page.setViewportSize(viewport)
        await page.addInitScript(
          (value) => localStorage.setItem('ren2hub_theme_mode', value),
          theme
        )
        const errors: string[] = []
        page.on('pageerror', (error) => errors.push(error.message))
        await page.goto('/sign-in')
        await page
          .locator('input[autocomplete="username"]')
          .fill(credentials.username)
        await page
          .locator('input[autocomplete="current-password"]')
          .fill(credentials.password)
        await page.locator('button[type="submit"]').click()
        await expect(page).not.toHaveURL(/sign-in/)
        await page.goto('/system-settings/security/rate-limit')
        const panel = page.getByTestId('gateway-config-panel')
        await expect(
          panel.getByRole('heading', { name: '网关准入', exact: true })
        ).toBeVisible()
        const concurrency = panel
          .getByLabel('并发数（0 不限）', { exact: true })
          .first()
        const original = await concurrency.inputValue()
        const changed = original === '11' ? '12' : '11'
        await concurrency.fill(changed)
        const saved = page.waitForResponse(
          (response) =>
            response.url().endsWith('/api/gateway/config') &&
            response.request().method() === 'PUT'
        )
        await panel.getByRole('button', { name: '保存限制' }).click()
        const response = await saved
        expect(response.status()).toBe(200)
        expect((await response.json()).data.user_concurrency).toBe(
          Number(changed)
        )
        await page.reload()
        await expect(concurrency).toHaveValue(changed)
        await concurrency.fill(original)
        const restored = page.waitForResponse(
          (response) =>
            response.url().endsWith('/api/gateway/config') &&
            response.request().method() === 'PUT'
        )
        await panel.getByRole('button', { name: '保存限制' }).click()
        expect((await restored).status()).toBe(200)
        await assertNoHorizontalOverflow(page)
        await page.screenshot({
          path: `output/gateway-live-${theme}-${viewport.name}.png`,
          fullPage: true,
        })
        await page.goto('/users')
        await page
          .getByRole('button', { name: '编辑用户', exact: true })
          .first()
          .click()
        const user = page.getByTestId('gateway-user-panel')
        await expect(user).toBeVisible()
        await expect(user.getByRole('alert')).toHaveCount(0)
        const inherit = user.getByRole('checkbox').first()
        await inherit.uncheck()
        const overridden = page.waitForResponse(
          (response) =>
            /\/api\/gateway\/users\/\d+$/.test(response.url()) &&
            response.request().method() === 'PUT'
        )
        await user.getByRole('button', { name: '保存限制' }).click()
        expect((await (await overridden).json()).data.concurrency).toBe(0)
        await inherit.check()
        const inherited = page.waitForResponse(
          (response) =>
            /\/api\/gateway\/users\/\d+$/.test(response.url()) &&
            response.request().method() === 'PUT'
        )
        await user.getByRole('button', { name: '保存限制' }).click()
        expect((await (await inherited).json()).data.concurrency).toBeNull()
        await page.goto('/system-settings/models/vendor')
        await expect(
          page.getByRole('switch', { name: 'Gemini 思考适配器', exact: true })
        ).toBeVisible()
        await expect(
          page.getByText('Gemini 思考适配器', { exact: true })
        ).toBeVisible()
        await page
          .getByText('Gemini 思考适配器', { exact: true })
          .scrollIntoViewIfNeeded()
        await expect(
          page.getByRole('switch', {
            name: 'Gemini 函数调用思考签名',
            exact: true,
          })
        ).toHaveCount(0)
        await assertNoHorizontalOverflow(page)
        await page.screenshot({
          path: `output/gateway-vendor-${theme}-${viewport.name}.png`,
          fullPage: true,
        })
        expect(errors).toEqual([])
      })
    }
  }
})
