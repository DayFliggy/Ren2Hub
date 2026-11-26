import { test, expect } from '@playwright/test'
import {
  configureStablePage,
  assertNoHorizontalOverflow,
  waitForStablePage,
  captureEvidence,
} from './fixtures'

for (const [theme, width] of [
  ['light', 1440],
  ['dark', 390],
] as const) {
  test(`${theme} ${width} plugin versions and safe source preview`, async ({
    page,
  }) => {
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
    const meta = {
      key: 'fixture',
      name: 'Fixture Driver',
      version: '1.0.0',
      apiVersion: 1,
      models: ['fixture-model'],
      icon: 'text:FD',
      description: { en: 'Image and video tasks', zh: '图片与视频任务' },
      routes: [{ method: 'POST', path: '/v1/video/generate', type: 'submit' }],
      scope: { models: ['fixture-model'], channelTypes: [1, 14] },
    }
    let enabled = false
    const statusWrites: unknown[] = []
    await page.route('**/api/plugin/task**', async (route) => {
      const path = new URL(route.request().url()).pathname
      let data: unknown = []
      if (path === '/api/plugin/task')
        data = [
          {
            meta,
            source: 'override',
            source_hash: 'a'.repeat(64),
            active: true,
            has_icon: true,
            enabled,
            runtime_status: enabled ? 'registered' : 'disabled',
            channel_count: 1,
            in_flight_count: 2,
          },
        ]
      else if (path === '/api/plugin/task/fixture/status') {
        statusWrites.push(route.request().postDataJSON())
        enabled = true
        data = {
          plugin_enabled: true,
          disabled_channels: 0,
          unbound_channels: 0,
          pricing_issues: [
            {
              model_name: 'fixture-model',
              plugin_key: 'fixture',
              plugin_name: 'Fixture Driver',
              reason: 'incompatible',
            },
          ],
        }
      } else if (path.endsWith('/icon')) {
        expect(route.request().headers()['authorization']).toMatch(/^Bearer /)
        return route.fulfill({ status: 404 })
      } else if (path.endsWith('/marketplace/sources'))
        data = [
          {
            name: 'Fixture Market',
            index_url: 'https://plugins.example/index.json',
          },
        ]
      else if (path.endsWith('/runtime/status'))
        data = {
          current_generation: 3,
          database_revision: 'revision',
          last_rebuild: { status: 'success' },
          plugin_errors: {},
        }
      else if (path.endsWith('/versions'))
        data = [
          {
            id: 1,
            key: 'fixture',
            api_version: 1,
            version: '1.0.0',
            active: true,
            enabled: true,
            factory: false,
            created_at: 1,
            remark: '',
            source: 'export const meta = {};',
            source_hash: 'a'.repeat(64),
          },
        ]
      else if (path.endsWith('/preview'))
        data = { meta, source_hash: 'a'.repeat(64), unresolved: [] }
      else if (path === '/api/plugin/task/fixture')
        data = {
          meta,
          source: 'export const meta = {};',
          layer: 'override',
          skipped_routes: ['POST /v1/video/generate'],
        }
      await route.fulfill({ json: { success: true, data } })
    })
    const englishNotes =
      '---\nchangelogVersion: 1\nplugin: fixture\nversion: 1.1.0\nlocale: en\ntranslations:\n  zh-CN: CHANGELOG.zh-CN.md\n---\n# Changelog\n## [1.1.0]\n### Fixed\n- Corrected fixture handling.\n'
    const chineseNotes =
      '---\nchangelogVersion: 1\nplugin: fixture\nversion: 1.1.0\nlocale: zh-CN\n---\n# Changelog\n## [1.1.0]\n### Fixed\n- 修复示例处理。\n'
    const upgradedSource =
      'export const meta = { key: "fixture", name: "Fixture Driver", version: "1.1.0", apiVersion: 1 }\n'
    const upgradedHash = 'b'.repeat(64)
    await page.route('https://plugins.example/**', (route) => {
      const path = new URL(route.request().url()).pathname
      if (path.endsWith('index.json'))
        return route.fulfill({
          json: {
            indexVersion: 1,
            plugins: [
              {
                ...meta,
                name_i18n: { 'zh-CN': '示例驱动', en: 'Fixture Driver' },
                latest: '1.1.0',
                versions: [
                  {
                    version: '1.1.0',
                    path: 'fixture/1.1.0/plugin.js',
                    sha256: upgradedHash,
                  },
                  {
                    version: meta.version,
                    path: 'fixture/1.0.0/plugin.js',
                    sha256: 'a'.repeat(64),
                  },
                ],
              },
            ],
          },
        })
      if (path.endsWith('/1.1.0/plugin.js'))
        return route.fulfill({
          contentType: 'text/javascript',
          body: upgradedSource,
        })
      return route.fulfill({
        contentType: 'text/plain',
        body: path.endsWith('.zh-CN.md') ? chineseNotes : englishNotes,
      })
    })
    await page.goto('/task-plugins')
    await waitForStablePage(page)
    await expect(
      page.getByText('Fixture Driver', { exact: false }).first()
    ).toBeVisible()
    await expect(page.getByText('FD', { exact: true }).first()).toBeVisible()
    // Row menu → details sheet with the upstream tabs.
    await page.getByRole('button', { name: '打开菜单' }).first().click()
    await page.getByRole('menuitem', { name: '详情' }).click()
    const sheet = page.getByRole('dialog', { name: 'Fixture Driver' })
    for (const tab of [
      '概览',
      '计费参数',
      '插件源码',
      '版本历史',
      '源码差异',
      '插件沙盒',
    ])
      await expect(sheet.getByRole('tab', { name: tab })).toBeVisible()
    const conflict = sheet.getByText('此路径仅通过 /fixture 地址提供服务。', {
      exact: true,
    })
    await expect(conflict).toBeVisible()
    await expect(
      sheet.getByText('/v1/video/generate', { exact: true })
    ).toBeVisible()
    // The overview tab shows the scope the plugin declares: its channels and
    // models, each spelled out.
    const scopeSummary = sheet
      .getByRole('region', { name: '按插件建议' })
      .locator('[data-plugin-scope]')
    await expect(scopeSummary).toContainText('OpenAI')
    await expect(scopeSummary).toContainText('Anthropic')
    await expect(scopeSummary).toContainText('fixture-model')
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: test.info().outputPath('route-conflict.png'),
    })
    await sheet.getByRole('radio', { name: '/fixture', exact: true }).click()
    await expect(
      sheet.getByText('/fixture/v1/video/generate', { exact: true })
    ).toBeVisible()
    await expect(conflict).toHaveCount(0)
    await assertNoHorizontalOverflow(page)
    await sheet.getByRole('tab', { name: '版本历史' }).click()
    await expect(
      sheet.getByRole('button', { name: '激活 / 回滚' })
    ).toBeDisabled()
    await assertNoHorizontalOverflow(page)
    await page.keyboard.press('Escape')

    const enable = page.getByRole('switch', { name: '启用插件 fixture' })
    await expect(enable).not.toBeChecked()
    await enable.click()
    const confirmation = page.getByRole('dialog', { name: '启用插件？' })
    await confirmation
      .getByRole('button', { name: '启用', exact: true })
      .click()
    await expect(confirmation).toHaveCount(0)
    await expect(enable).toBeChecked()
    expect(statusWrites).toEqual([{ enabled: true }])
    await expect(
      page.getByText(/插件已启用\. 部分任务模型价格需要处理/)
    ).toContainText(
      'fixture-model (Fixture Driver): 价格引用了插件不再提供的用量字段'
    )
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: test.info().outputPath('plugin-pricing-warning.png'),
    })

    // Marketplace: single install integrity check + localized bulk updates dialog.
    await page.getByRole('tab', { name: '插件市场', exact: true }).click()
    await expect(
      page.getByText('图片与视频任务', { exact: true })
    ).toBeVisible()
    await page.getByRole('button', { name: '安装', exact: true }).click()
    const install = page.getByRole('dialog', { name: '安装 示例驱动' })
    await expect(install.getByText('完整性校验失败')).toBeVisible()
    await expect(
      install.getByRole('button', { name: '安装并启用' })
    ).toBeDisabled()
    await install.getByRole('tab', { name: '更新日志' }).click()
    await expect(
      install.getByText('修复示例处理。', { exact: true })
    ).toBeVisible()
    await assertNoHorizontalOverflow(page)
    await install.getByRole('button', { name: '取消', exact: true }).click()

    // Bulk updates dialog with localized names and nested review-only diff modal.
    await page.getByRole('button', { name: '更新全部（1）' }).click()
    const bulk = page.getByRole('dialog', { name: '更新已安装插件' })
    await expect(bulk.getByText('示例驱动')).toBeVisible()
    await expect(bulk.getByText('v1.0.0')).toBeVisible()
    await expect(bulk.getByText('v1.1.0')).toBeVisible()
    await assertNoHorizontalOverflow(page)
    await bulk.getByRole('button', { name: '查看「示例驱动」的变更' }).click()
    const review = page.getByRole('dialog', { name: '示例驱动' })
    await expect(review.getByRole('tab', { name: '更新日志' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    await expect(
      review.getByRole('button', { name: '安装并启用' })
    ).toHaveCount(0)
    await assertNoHorizontalOverflow(page)
    await page.keyboard.press('Escape')
    await expect(review).toHaveCount(0)
    await expect(bulk).toBeVisible()
    await bulk.getByRole('button', { name: '取消', exact: true }).click()
    await expect(bulk).toHaveCount(0)
    await assertNoHorizontalOverflow(page)
  })
}
