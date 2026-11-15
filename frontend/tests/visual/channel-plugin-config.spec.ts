import { expect, test, type Locator, type Page } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  captureEvidence,
} from './fixtures'

const longValue = 'LongPluginConfigurationWithoutSpaces'.repeat(160)

async function fitsDialog(page: Page, fieldset: Locator) {
  await assertNoHorizontalOverflow(page)
  const sizes = await fieldset.evaluate((element) => {
    const parent = element.parentElement!
    const box = element.getBoundingClientRect()
    const available = parent.getBoundingClientRect()
    const scroller = element
      .closest('[data-handdrawn="modal"]')!
      .querySelector('.overflow-y-auto')!
    return {
      left: box.left - available.left,
      right: box.right - available.right,
      fieldOverflow: element.scrollWidth - element.clientWidth,
      dialogOverflow: scroller.scrollWidth - scroller.clientWidth,
    }
  })
  expect(sizes.left).toBeGreaterThanOrEqual(-1)
  expect(sizes.right).toBeLessThanOrEqual(1)
  expect(sizes.fieldOverflow).toBeLessThanOrEqual(1)
  expect(sizes.dialogOverflow).toBeLessThanOrEqual(1)
}

// Theme does not change the dialog geometry; each width keeps every schema and
// write permission combination.
for (const [theme, width] of [
  ['light', 1440],
  ['dark', 390],
] as const)
  for (const schema of [true, false])
    for (const sensitiveWrite of [true, false])
      test(`plugin configuration ${theme} ${width} ${schema ? 'schema' : 'raw'} ${sensitiveWrite ? 'editable' : 'read-only'}`, async ({
        page,
      }, info) => {
        await page.setViewportSize({ width, height: 900 })
        await configureStablePage(page, {
          theme,
          authenticated: true,
          user: {
            role: 10,
            permissions: {
              admin_permissions: {
                channel: {
                  read: true,
                  write: true,
                  sensitive_write: sensitiveWrite,
                },
                task_plugin: { bind: true },
              },
            },
          },
        })
        const originalConfig = {
          endpoint: longValue,
          payload: { nested: longValue, values: [longValue] },
        }
        const channel = {
          id: 388,
          name: 'Plugin Configuration',
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
          base_url: 'https://fixture.example',
          models: 'fixture-model',
          model_mapping: '',
          other: '',
          settings: '{}',
          setting: JSON.stringify({
            task_plugin_key: 'layout-plugin',
            plugin_config: originalConfig,
          }),
        }
        const writes: Record<string, unknown>[] = []
        await page.route('**/api/task_plugin_options', (route) =>
          route.fulfill({
            json: {
              success: true,
              data: [
                {
                  key: 'layout-plugin',
                  name: 'Layout Plugin',
                  task: false,
                  provider: true,
                  models: ['fixture-model'],
                  ...(schema
                    ? {
                        channelConfig: {
                          type: 'object',
                          properties: {
                            endpoint: { type: 'string', title: '长值' },
                            payload: { type: 'object', title: '对象配置' },
                          },
                        },
                      }
                    : {}),
                },
              ],
            },
          })
        )
        await page.route(/\/api\/channel\/(?:\?.*)?$/, async (route) => {
          if (route.request().method() === 'PUT') {
            const payload = route.request().postDataJSON()
            writes.push(payload)
            Object.assign(channel, payload)
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
        await page.goto('/channels')
        await waitForStablePage(page)
        await page.locator('[aria-label="编辑渠道"]:visible').first().click()
        const dialog = page.getByRole('dialog', {
          name: '编辑渠道',
          exact: true,
        })
        const config = dialog.getByRole('group', {
          name: '插件渠道配置',
          exact: true,
        })
        await expect(config).toBeVisible()
        const input = config.getByRole('textbox', {
          name: schema ? '对象配置' : '配置（JSON）',
        })
        expect(JSON.parse(await input.inputValue())).toEqual(
          schema ? originalConfig.payload : originalConfig
        )
        await fitsDialog(page, config)
        const editedConfig = {
          endpoint: `${longValue}-edited`,
          payload: {
            nested: `${longValue}-edited`,
            values: [longValue, longValue],
          },
        }
        if (sensitiveWrite) {
          if (schema) {
            await config
              .getByRole('textbox', { name: '长值', exact: true })
              .fill(editedConfig.endpoint)
            await input.fill(JSON.stringify(editedConfig.payload))
          } else await input.fill(JSON.stringify(editedConfig))
          expect(await input.inputValue()).not.toContain('\n')
          await fitsDialog(page, config)
          await input.blur()
          await fitsDialog(page, config)
          await input.fill('{')
          await input.blur()
          await expect(
            dialog.getByRole('button', { name: '保存渠道', exact: true })
          ).toBeDisabled()
          await expect(config.getByRole('alert')).toBeVisible()
          await input.fill(
            JSON.stringify(schema ? editedConfig.payload : editedConfig)
          )
          await input.blur()
        } else {
          await expect(input).toBeDisabled()
          if (schema)
            await expect(
              config.getByRole('textbox', { name: '长值', exact: true })
            ).toBeDisabled()
          await dialog
            .locator('input[name="admin-channel-name"]')
            .fill('Renamed read-only configuration')
        }
        await fitsDialog(page, config)
        await captureEvidence(page, {
          path: info.outputPath('plugin-config.png'),
          fullPage: true,
        })
        await dialog
          .getByRole('button', { name: '保存渠道', exact: true })
          .click()
        await expect(dialog).toHaveCount(0)
        expect(writes).toHaveLength(1)
        if (sensitiveWrite)
          expect(JSON.parse(String(writes[0]!.setting)).plugin_config).toEqual(
            editedConfig
          )
        else {
          expect(writes[0]).not.toHaveProperty('setting')
          expect(writes[0]!.name).toBe('Renamed read-only configuration')
        }
        await page.reload()
        await waitForStablePage(page)
        await page.locator('[aria-label="编辑渠道"]:visible').first().click()
        expect(JSON.parse(await input.inputValue())).toEqual(
          schema
            ? (sensitiveWrite ? editedConfig : originalConfig).payload
            : sensitiveWrite
              ? editedConfig
              : originalConfig
        )
        await fitsDialog(page, config)
      })
