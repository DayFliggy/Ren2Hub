import { test, expect } from '@playwright/test'
import {
  configureStablePage,
  assertNoHorizontalOverflow,
  waitForStablePage,
} from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    test(`${theme} ${width} configured API address copy and fallbacks`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme, authenticated: true })
      await page.addInitScript(() => {
        Object.defineProperty(navigator, 'clipboard', {
          configurable: true,
          value: {
            writeText: async (value: string) => {
              sessionStorage.setItem('copied-address', value)
            },
          },
        })
      })
      let status: Record<string, unknown> = {
        api_info_enabled: true,
        server_address: 'https://gateway.example.test/proxy/',
        api_info: [
          {
            route: 'Regional gateway',
            description: 'Configured gateway path',
            url: 'https://regional-api.example.test/organization/production/openai-compatible/v1/',
          },
        ],
      }
      await page.route('**/api/status', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              ...status,
              frontend_capabilities: {
                profile: 'live',
                api_tokens: 'live',
                login: 'live',
                admin: 'live',
              },
            },
          },
        })
      )
      await page.goto('/keys')
      await waitForStablePage(page)
      const strip = page.locator('[aria-label="API 端点"]')
      await expect(strip.locator('code')).toHaveText(
        'https://regional-api.example.test/organization/production/openai-compatible/v1/'
      )
      await strip
        .getByRole('button', { name: '复制Regional gateway', exact: true })
        .click()
      await expect
        .poll(() =>
          page.evaluate(() => sessionStorage.getItem('copied-address'))
        )
        .toBe(
          'https://regional-api.example.test/organization/production/openai-compatible/v1/'
        )
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('configured-addresses.png'),
        fullPage: true,
      })
      status = { ...status, api_info_enabled: false }
      await page.reload()
      await expect(strip.locator('code')).toHaveText(
        'https://gateway.example.test/proxy/'
      )
      status = { api_info: [] }
      await page.reload()
      await expect(strip.locator('code')).toHaveText(new URL(page.url()).origin)
      await strip
        .getByRole('button', { name: '复制当前域名', exact: true })
        .focus()
      await page.keyboard.press('Enter')
      await expect
        .poll(() =>
          page.evaluate(() => sessionStorage.getItem('copied-address'))
        )
        .toBe(new URL(page.url()).origin)
      await assertNoHorizontalOverflow(page)
    })

    test(`${theme} ${width} plugin disable explicit cascade force cancel and errors`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme, authenticated: true })
      await page.route('**/api/status', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              frontend_capabilities: {
                taskPlugins: 'live',
                admin: 'live',
                profile: 'live',
                login: 'live',
              },
            },
          },
        })
      )
      let enabled = true
      let mode: 'unbound' | 'bound' | 'pending' | 'error' = 'unbound'
      const mutations: Array<{ query: string; body: unknown }> = []
      await page.route('**/api/plugin/task**', async (route) => {
        const request = route.request(),
          url = new URL(request.url())
        if (url.pathname.endsWith('/status') && request.method() === 'POST') {
          mutations.push({ query: url.search, body: request.postDataJSON() })
          if (mode === 'error')
            return route.fulfill({
              json: { success: false, message: 'Fixture database unavailable' },
            })
          if (
            mode !== 'unbound' &&
            (!url.searchParams.has('cascade') ||
              (mode === 'pending' && !url.searchParams.has('force')))
          )
            return route.fulfill({
              json: {
                success: false,
                message: 'task plugin is still in use',
                data: {
                  channels: [
                    { id: 7, name: 'Dedicated channel', type: 61 },
                    { id: 8, name: 'Shared gateway', type: 60 },
                  ],
                  in_flight_count: mode === 'pending' ? 2 : 0,
                },
              },
            })
          enabled = false
          return route.fulfill({
            json: {
              success: true,
              data: { disabled_channels: 1, unbound_channels: 1 },
            },
          })
        }
        let data: unknown = []
        if (url.pathname === '/api/plugin/task')
          data = [
            {
              meta: {
                key: 'fixture',
                name: 'Fixture Driver',
                version: '1',
                apiVersion: 1,
                models: ['fixture-model'],
                icon: 'text:FD',
              },
              source: 'custom',
              source_hash: 'a'.repeat(64),
              active: true,
              enabled,
              runtime_status: 'registered',
              channel_count: mode === 'unbound' ? 0 : 2,
              in_flight_count: mode === 'pending' ? 2 : 0,
            },
          ]
        else if (url.pathname.endsWith('/runtime/status'))
          data = {
            current_generation: 1,
            last_rebuild: { status: 'success' },
            plugin_errors: {},
          }
        return route.fulfill({ json: { success: true, data } })
      })
      await page.goto('/task-plugins')
      await waitForStablePage(page)
      const toggle = page.getByRole('switch', { name: '启用插件 fixture' })
      const confirmDisable = () =>
        page
          .getByRole('dialog', { name: '禁用插件？' })
          .getByRole('button', { name: '禁用', exact: true })
          .click()

      await toggle.click()
      await expect(
        page.getByRole('dialog', { name: '禁用插件？' })
      ).toBeVisible()
      await page.getByRole('button', { name: '取消', exact: true }).click()
      expect(mutations).toHaveLength(0)
      await toggle.click()
      await confirmDisable()
      await expect(toggle).toHaveAttribute('aria-checked', 'false')
      expect(mutations).toEqual([{ query: '', body: { enabled: false } }])

      enabled = true
      mode = 'bound'
      await page.reload()
      await waitForStablePage(page)
      await toggle.click()
      await confirmDisable()
      const dialog = page.getByRole('dialog', { name: '插件仍在使用中' })
      await expect(dialog.getByText('#8 Shared gateway')).toBeVisible()
      await expect(
        dialog.getByText(
          'New API：仅解除当前插件绑定，其他插件和普通流量继续可用。'
        )
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await dialog.screenshot({
        path: testInfo.outputPath('cascade-confirmation.png'),
      })
      await dialog.getByRole('button', { name: '取消', exact: true }).click()
      expect(mutations).toHaveLength(2)
      await toggle.click()
      await confirmDisable()
      await dialog
        .getByRole('button', { name: '级联禁用渠道', exact: true })
        .click()
      await expect(toggle).toHaveAttribute('aria-checked', 'false')
      expect(mutations.at(-1)).toEqual({
        query: '?cascade=true',
        body: { enabled: false },
      })

      enabled = true
      mode = 'pending'
      await page.reload()
      await waitForStablePage(page)
      await toggle.click()
      await confirmDisable()
      await expect(
        dialog.getByText('2 个在途任务', { exact: false })
      ).toBeVisible()
      await expect(
        dialog.getByRole('button', { name: '级联禁用渠道', exact: true })
      ).toBeDisabled()
      await dialog
        .getByRole('button', { name: '强制操作', exact: true })
        .click()
      await expect(toggle).toHaveAttribute('aria-checked', 'false')
      expect(mutations.at(-1)).toEqual({
        query: '?cascade=true&force=true',
        body: { enabled: false },
      })

      enabled = true
      mode = 'error'
      await page.reload()
      await waitForStablePage(page)
      await toggle.click()
      await confirmDisable()
      // Upstream reports a non-usage failure through the toast and keeps the
      // switch unchanged.
      await expect(page.getByText('Fixture database unavailable')).toBeVisible()
      await expect(toggle).toHaveAttribute('aria-checked', 'true')
      await assertNoHorizontalOverflow(page)
    })
  }
}
