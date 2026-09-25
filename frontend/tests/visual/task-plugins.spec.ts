import { test, expect } from '@playwright/test'
import {
  configureStablePage,
  assertNoHorizontalOverflow,
  waitForStablePage,
} from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
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
      }
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
              enabled: true,
              runtime_status: 'registered',
              channel_count: 1,
              in_flight_count: 2,
            },
          ]
        else if (path.endsWith('/icon')) {
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
              key: 'fixture',
              version: '1.0.0',
              active: true,
              factory: false,
              source: 'export const meta = {};',
              source_hash: 'a'.repeat(64),
            },
          ]
        else if (path.endsWith('/preview'))
          data = { meta, source_hash: 'a'.repeat(64) }
        await route.fulfill({ json: { success: true, data } })
      })
      const englishNotes =
        '---\nchangelogVersion: 1\nplugin: fixture\nversion: 1.0.0\nlocale: en\ntranslations:\n  zh-CN: CHANGELOG.zh-CN.md\n---\n# Changelog\n## [1.0.0]\n### Fixed\n- Corrected fixture handling.\n'
      const chineseNotes =
        '---\nchangelogVersion: 1\nplugin: fixture\nversion: 1.0.0\nlocale: zh-CN\n---\n# Changelog\n## [1.0.0]\n### Fixed\n- 修复示例处理。\n'
      await page.route('https://plugins.example/**', (route) => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('index.json'))
          return route.fulfill({
            json: {
              indexVersion: 1,
              plugins: [
                {
                  ...meta,
                  latest: meta.version,
                  versions: [
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
        return route.fulfill({
          contentType: 'text/plain',
          body: path.endsWith('.zh-CN.md') ? chineseNotes : englishNotes,
        })
      })
      await page.goto('/task-plugins')
      await waitForStablePage(page)
      await expect(
        page.getByText('Fixture Driver', { exact: false })
      ).toBeVisible()
      await expect(page.getByText('FD', { exact: true })).toBeVisible()
      await page.getByRole('button', { name: '版本与源码' }).click()
      await expect(page.getByText('fixture · 版本与源码')).toBeVisible()
      await expect(page.getByRole('button', { name: '删除版本' })).toHaveCount(
        0
      )
      await page.getByRole('tab', { name: '源码与预览' }).click()
      await page.locator('textarea').fill('export const meta = {};')
      await expect(
        page.getByRole('button', { name: '保存版本' })
      ).toBeDisabled()
      await page.getByRole('button', { name: '验证并预览' }).click()
      await expect(page.getByRole('button', { name: '保存版本' })).toBeEnabled()
      await page
        .locator('textarea')
        .fill('export const meta = { version: "2" };')
      await expect(
        page.getByRole('button', { name: '保存版本' })
      ).toBeDisabled()
      await page.getByRole('tab', { name: '插件市场', exact: true }).click()
      await page.getByRole('button', { name: '加载市场', exact: true }).click()
      await expect(
        page.getByText('图片与视频任务', { exact: true })
      ).toBeVisible()
      await page.getByRole('button', { name: '更新记录', exact: true }).click()
      const changelog = page.getByRole('dialog', {
        name: '更新记录',
        exact: true,
      })
      await expect(
        changelog.getByText('- 修复示例处理。', { exact: true })
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await changelog.getByRole('button', { name: '关闭', exact: true }).click()
      await assertNoHorizontalOverflow(page)
    })
  }
}
