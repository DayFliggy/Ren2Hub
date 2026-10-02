import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

const card = {
  reasoning_efforts: [],
  input_modalities: null,
  output_modalities: ['text'],
  context_limit: 32000,
  output_limit: null,
  tool_call: false,
  source: 'local-test',
  updated_at: 1,
}
const model = {
  id: 71,
  model_name: 'p2-local',
  description: '',
  icon: '',
  tags: '',
  vendor_id: 0,
  endpoints: '',
  status: 1,
  sync_official: 0,
  name_rule: 0,
  capabilities: card,
  bound_channels: [],
  enable_groups: [],
  matched_models: [],
  quota_types: [],
}

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`catalog P2 ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme, authenticated: true })
      const failures: string[] = []
      page.on('pageerror', (error) => failures.push(error.message))
      await page.route('**/api/token/7/client-config?*', (route) => {
        const format = new URL(route.request().url()).searchParams.get('format')
        return route.fulfill({
          json: {
            success: true,
            data: {
              format,
              filename:
                format === 'pi' ? 'ren2hub.models.json' : 'ren2hub.config.toml',
              media_type: 'text/plain',
              content:
                format === 'pi'
                  ? '{"apiKey":"${REN2HUB_API_KEY}"}'
                  : 'model = "gpt-4.1"\nenv_key = "REN2HUB_API_KEY"',
              models: ['gpt-4.1'],
              diagnostics: [{ code: 'prices_omitted' }],
            },
          },
        })
      })
      await page.goto('/keys')
      await page
        .getByRole('button', { name: '客户端配置', exact: true })
        .filter({ visible: true })
        .click()
      const dialog = page.getByRole('dialog')
      await expect(dialog.getByLabel('配置预览')).toHaveValue(/REN2HUB_API_KEY/)
      await dialog.getByLabel('客户端', { exact: true }).selectOption('pi')
      await expect(dialog.getByLabel('配置预览')).toHaveValue(
        '{"apiKey":"${REN2HUB_API_KEY}"}'
      )
      await expect(dialog).toContainText('配置未填写客户端价格')
      const download = page.waitForEvent('download')
      await dialog.getByRole('button', { name: '下载', exact: true }).click()
      expect((await download).suggestedFilename()).toBe('ren2hub.models.json')
      await assertNoHorizontalOverflow(page)
      await page.keyboard.press('Escape')
      await expect(dialog).toHaveCount(0)

      await page.route('**/api/models/search?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              page: 1,
              page_size: 20,
              total: 1,
              items: [model],
              vendor_counts: {},
            },
          },
        })
      )
      await page.route('**/api/vendors/search?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: { page: 1, page_size: 20, total: 0, items: [] },
          },
        })
      )
      await page.route('**/api/models/71', (route) =>
        route.fulfill({ json: { success: true, data: model } })
      )
      let saved: unknown
      await page.route('**/api/models/', (route) => {
        saved = route.request().postDataJSON()
        return route.fulfill({ json: { success: true, data: model } })
      })
      await page.goto('/models/metadata')
      await page
        .getByRole('button', { name: '编辑', exact: true })
        .filter({ visible: true })
        .first()
        .click()
      const editor = page
        .getByRole('dialog')
        .locator('[data-model-capabilities]')
      await expect(editor.getByLabel('工具调用')).toHaveValue('false')
      await expect(
        editor.locator('#capability-reasoning_efforts-state')
      ).toHaveValue('known')
      await expect(
        editor.locator('#capability-input_modalities-state')
      ).toHaveValue('unknown')
      await editor.getByLabel('工具调用').selectOption('true')
      await assertNoHorizontalOverflow(page)
      await page.getByRole('button', { name: '保存资料', exact: true }).click()
      await expect(page.getByRole('dialog')).toHaveCount(0)
      expect(saved).toMatchObject({
        capabilities: {
          reasoning_efforts: [],
          input_modalities: null,
          tool_call: true,
          context_limit: 32000,
        },
      })
      expect(failures).toEqual([])
    })
  }
}
