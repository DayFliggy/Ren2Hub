import { expect, test } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  captureEvidence,
} from './fixtures'

// These scenarios include several routed views or a full edit/create flow.
test.setTimeout(90_000)

for (const [theme, width] of [
  ['light', 1440],
  ['dark', 390],
] as const) {
  test(`${theme} ${width} advanced channel routes and Responses WebSocket`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 })
    await configureStablePage(page, { theme, authenticated: true })
    const advanced = {
      advanced_routes: [
        {
          incoming_path: '/v1/responses',
          upstream_path: '/v1/responses',
          converter: 'none',
          auth: {
            type: 'header',
            name: 'Authorization',
            value: 'Bearer {api_key}',
          },
        },
      ],
    }
    const channel = {
      id: 101,
      name: 'Advanced Fixture',
      type: 58,
      lab: { id: 'custom', name: 'Custom', source: 'fallback' },
      status: 1,
      priority: 0,
      weight: 10,
      in_flight: 0,
      max_concurrency: 20,
      channel_ratio: '1',

      used_quota: 0,
      balance: 0,
      response_time: 0,
      test_time: 0,
      base_url: 'https://upstream.example',
      models: 'fixture-model',
      model_mapping: '',
      setting: JSON.stringify({
        proxy: '',
        responses_websocket_enabled: false,
        // Bound before its plugin declared a scope; the run plan ignores it.
        request_plugins: ['rp_scoped'],
      }),
      settings: JSON.stringify({
        allow_speed: true,
        advanced_custom: advanced,
      }),
    }
    let saved: Record<string, unknown> | undefined
    let created: Record<string, unknown> | undefined
    let applied: Record<string, unknown> | undefined
    const rawBalance = JSON.stringify(
      { account: { credits: '12.50' }, html: '<script>never-run</script>' },
      null,
      2
    )
    await page.route('**/api/channel/update_balance/101', (route) =>
      route.fulfill({
        json: { success: true, raw_response: rawBalance },
      })
    )
    await page.route('**/api/channel/upstream_updates/detect', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            add_models: ['new-model'],
            remove_models: ['fixture-model'],
            auto_added_models: 0,
          },
        },
      })
    )
    await page.route('**/api/channel/upstream_updates/apply', (route) => {
      applied = route.request().postDataJSON()
      return route.fulfill({
        json: { success: true, data: { models: 'fixture-model,new-model' } },
      })
    })
    await page.route(/\/api\/channel\/(?:\?.*)?$/, (route) => {
      if (route.request().method() === 'POST') {
        created = route.request().postDataJSON()
        return route.fulfill({ json: { success: true, data: { id: 102 } } })
      }
      if (route.request().method() === 'PUT') {
        saved = route.request().postDataJSON()
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
            type_counts: { '58': 1 },
          },
        },
      })
    })
    await page.route('**/api/channel/labs', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: { total: 1, items: [{ ...channel.lab, count: 1 }] },
        },
      })
    )
    await page.goto('/channels')
    await waitForStablePage(page)
    await page.getByRole('button', { name: '字段设置', exact: true }).click()
    await page
      .getByRole('button', { name: '单渠道上游同步操作', exact: true })
      .click()
    await page.keyboard.press('Escape')
    await page.locator('[aria-label="同步上游额度"]:visible').first().click()
    const balanceDialog = page.getByRole('dialog', {
      name: '余额查询原始响应：Advanced Fixture',
      exact: true,
    })
    await expect(balanceDialog.locator('pre')).toHaveText(rawBalance)
    await expect(balanceDialog.locator('script')).toHaveCount(0)
    await assertNoHorizontalOverflow(page)
    await balanceDialog
      .getByRole('button', { name: '关闭', exact: true })
      .click()
    await expect(balanceDialog).toHaveCount(0)
    await page.locator('[aria-label="预览模型更新"]:visible').first().click()
    const syncDialog = page.getByRole('dialog', {
      name: '预览模型更新',
      exact: true,
    })
    await expect(
      syncDialog.getByRole('checkbox', { name: 'new-model', exact: true })
    ).toBeChecked()
    await expect(
      syncDialog.getByRole('checkbox', { name: 'fixture-model', exact: true })
    ).not.toBeChecked()
    expect(applied).toBeUndefined()
    await syncDialog
      .getByRole('button', { name: '应用所选更新', exact: true })
      .click()
    await expect(syncDialog).toHaveCount(0)
    expect(applied).toEqual({
      id: 101,
      add_models: ['new-model'],
      remove_models: [],
      ignore_models: [],
    })
    await page.route('**/api/request_plugin/instances', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            instances: [
              {
                id: 'rp_scoped',
                name: 'Scoped guard',
                plugin: 'scoped-guard',
                enabled: true,
                global: false,
                models: [],
                config: {},
                plugin_scope: { models: [], channel_types: [58] },
              },
              {
                id: 'rp_plain',
                name: 'Plain guard',
                plugin: 'plain-guard',
                enabled: true,
                global: false,
                models: [],
                config: {},
              },
            ],
          },
        },
      })
    )
    await page.locator('[aria-label="编辑渠道"]:visible').first().click()
    const dialog = page.getByRole('dialog', { name: '编辑渠道', exact: true })
    await page.route('**/api/channel/fetch_models**', (route) =>
      route.fulfill({
        json: { success: true, data: ['gpt-4.1', 'gpt-4o', 'wan2.6-t2v'] },
      })
    )
    await dialog
      .getByRole('button', { name: '从上游获取', exact: true })
      .click()
    const modelPicker = page.getByRole('dialog', {
      name: '选择上游模型',
      exact: true,
    })
    await modelPicker
      .getByRole('checkbox', { name: '选择 OpenAI 模型', exact: true })
      .uncheck()
    expect(saved).toBeUndefined()
    await modelPicker.locator('[data-apply-fetched-models]').click()
    await dialog.getByRole('button', { name: '高级设置', exact: false }).click()
    // A saved binding to an instance that follows its plugin's scope stays
    // removable and says where it runs; it is not offered again.
    const bindings = dialog.getByRole('group', {
      name: '渠道请求处理实例（按执行顺序）',
    })
    await expect(bindings.locator('[data-binding-follows-plugin]')).toHaveText(
      '按插件建议的范围运行，不受这里的绑定影响'
    )
    await expect(bindings.locator('option[value="rp_plain"]')).toHaveCount(1)
    await expect(bindings.locator('option[value="rp_scoped"]')).toHaveCount(0)
    await bindings
      .locator('[data-binding-follows-plugin]')
      .scrollIntoViewIfNeeded()
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: test.info().outputPath('scoped-binding.png'),
    })
    const ratioInput = dialog.getByRole('textbox', {
      name: '渠道倍率',
      exact: true,
    })
    await ratioInput.fill('1000.000000000000000001')
    await expect(
      dialog.getByRole('button', { name: '保存渠道' })
    ).toBeDisabled()
    await ratioInput.fill('0.123456789012345678')
    await expect(
      dialog.getByRole('switch', { name: 'Responses WebSocket' })
    ).toHaveAttribute('aria-checked', 'false')
    await dialog.getByRole('switch', { name: 'Responses WebSocket' }).click()
    await dialog
      .getByRole('switch', { name: '透传请求体', exact: true })
      .click()
    await dialog
      .getByRole('textbox', { name: '参数覆盖 JSON', exact: true })
      .fill('[]')
    await expect(
      dialog.getByRole('button', { name: '保存渠道' })
    ).toBeDisabled()
    await dialog
      .getByRole('textbox', { name: '参数覆盖 JSON', exact: true })
      .fill('{"temperature": 0}')
    await dialog
      .getByRole('textbox', { name: '请求头覆盖 JSON', exact: true })
      .fill('{"X-Fixture": "header"}')
    await dialog
      .getByRole('button', { name: 'JSON', exact: true })
      .last()
      .click()
    const json = dialog.getByRole('textbox', { name: '高级路由 JSON' })
    await json.fill('{')
    await expect(
      dialog.getByRole('button', { name: '保存渠道' })
    ).toBeDisabled()
    advanced.advanced_routes[0]!.upstream_path = '/provider/responses'
    await json.fill(JSON.stringify(advanced, null, 2))
    await expect(dialog.getByRole('button', { name: '保存渠道' })).toBeEnabled()
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: test.info().outputPath('advanced-channel.png'),
      fullPage: true,
    })
    await dialog.getByRole('button', { name: '保存渠道' }).click()
    await expect(dialog).toHaveCount(0)
    expect(saved).toBeDefined()
    expect(saved!.channel_ratio).toBe('0.123456789012345678')
    expect(saved).not.toHaveProperty('upstream_ratio')
    expect(saved!.models).toBe('fixture-model,wan2.6-t2v')
    expect(JSON.parse(saved!.setting as string)).toEqual({
      proxy: '',
      responses_websocket_enabled: true,
      pass_through_body_enabled: true,
      request_plugins: ['rp_scoped'],
    })
    expect(JSON.parse(saved!.settings as string)).toEqual({
      allow_speed: true,
      advanced_custom: advanced,
    })
    expect(JSON.parse(saved!.param_override as string)).toEqual({
      temperature: 0,
    })
    expect(JSON.parse(saved!.header_override as string)).toEqual({
      'X-Fixture': 'header',
    })
    await page.getByRole('button', { name: '新建渠道', exact: true }).click()
    const createDialog = page.getByRole('dialog', {
      name: '新建渠道',
      exact: true,
    })
    await createDialog
      .locator('[name="admin-channel-name"]')
      .fill('Single-Key Fixture')
    // A channel holds one key: the dialog offers one key or a batch of
    // channels, never a key list on one channel.
    await expect(
      createDialog
        .getByRole('radiogroup', { name: '添加模式' })
        .getByRole('radio')
    ).toHaveText(['单密钥', '批量密钥'])
    await createDialog
      .locator('[name="admin-channel-key"]')
      .fill('fixture-key-a')
    await createDialog
      .getByPlaceholder('选择模型或添加自定义模型', { exact: true })
      .fill('fixture-model')
    await createDialog
      .getByPlaceholder('选择模型或添加自定义模型', { exact: true })
      .press('Enter')
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: test.info().outputPath('new-channel.png'),
      fullPage: true,
    })
    await createDialog
      .getByRole('button', { name: '保存渠道', exact: true })
      .click()
    await expect(createDialog).toHaveCount(0)
    expect(created).toMatchObject({
      mode: 'single',
      channel: {
        name: 'Single-Key Fixture',
        key: 'fixture-key-a',
        models: 'fixture-model',
      },
    })
    expect(created).not.toHaveProperty('multi_key_mode')
    await assertNoHorizontalOverflow(page)
  })
}
