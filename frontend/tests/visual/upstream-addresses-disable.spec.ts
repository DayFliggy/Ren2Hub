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
      await page.getByRole('button', { name: '停用', exact: true }).click()
      await expect(page.getByRole('dialog')).toBeVisible()
      await page.getByRole('button', { name: '取消', exact: true }).click()
      expect(mutations).toHaveLength(0)
      await page.getByRole('button', { name: '停用', exact: true }).click()
      await page.getByRole('button', { name: '确认停用', exact: true }).click()
      await expect(
        page.getByRole('button', { name: '启用', exact: true })
      ).toBeVisible()
      expect(mutations).toEqual([{ query: '', body: { enabled: false } }])

      enabled = true
      mode = 'bound'
      await page.getByRole('button', { name: '刷新', exact: true }).click()
      await page.getByRole('button', { name: '停用', exact: true }).click()
      await page.getByRole('button', { name: '确认停用', exact: true }).click()
      const dialog = page.getByRole('dialog', { name: '插件仍在使用中' })
      await expect(dialog.getByText('Shared gateway (#8)')).toBeVisible()
      await expect(
        dialog.getByText(
          'New API：仅解除当前插件绑定，其他插件和普通流量继续可用。'
        )
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('cascade-confirmation.png'),
        fullPage: true,
      })
      await page.getByRole('button', { name: '取消', exact: true }).click()
      expect(mutations).toHaveLength(2)
      await page.getByRole('button', { name: '停用', exact: true }).click()
      await page.getByRole('button', { name: '确认停用', exact: true }).click()
      await page
        .getByRole('button', { name: '级联停用渠道', exact: true })
        .click()
      await expect(
        page.getByRole('button', { name: '启用', exact: true })
      ).toBeVisible()
      expect(mutations.at(-1)).toEqual({
        query: '?cascade=true',
        body: { enabled: false },
      })

      enabled = true
      mode = 'pending'
      await page.getByRole('button', { name: '刷新', exact: true }).click()
      await page.getByRole('button', { name: '停用', exact: true }).click()
      await page.getByRole('button', { name: '确认停用', exact: true }).click()
      await expect(dialog.getByText('未完成任务：2')).toBeVisible()
      await expect(
        page.getByRole('button', { name: '级联停用渠道', exact: true })
      ).toBeDisabled()
      await page.getByRole('button', { name: '强制停用', exact: true }).click()
      await expect(
        page.getByRole('button', { name: '启用', exact: true })
      ).toBeVisible()
      expect(mutations.at(-1)).toEqual({
        query: '?cascade=true&force=true',
        body: { enabled: false },
      })

      enabled = true
      mode = 'error'
      await page.getByRole('button', { name: '刷新', exact: true }).click()
      await page.getByRole('button', { name: '停用', exact: true }).click()
      await page.getByRole('button', { name: '确认停用', exact: true }).click()
      await expect(page.getByRole('dialog').getByRole('alert')).toHaveText(
        'Fixture database unavailable'
      )
      await assertNoHorizontalOverflow(page)
      await page.keyboard.press('Escape')
      await expect(page.getByRole('dialog')).toHaveCount(0)
      await expect(
        page.getByRole('button', { name: '停用', exact: true })
      ).toBeFocused()
    })
  }
}
