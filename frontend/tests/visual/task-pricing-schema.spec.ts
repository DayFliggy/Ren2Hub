import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'
for (const theme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    test(`task schema ${theme} ${width}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme })
      const fields = {
        seconds: {
          type: 'number',
          unit: 'second',
          source: 'completion_fact',
          required: true,
          maximum: '60',
        },
        resolution: {
          type: 'string',
          source: 'request_fixed',
          required: true,
          enum: ['720p', '1080p'],
        },
        audio: { type: 'boolean', source: 'request_fixed', required: false },
      }
      const model = {
        id: 1,
        model_name: 'video-fixture',
        description: '',
        icon: '',
        tags: 'video',
        vendor_id: 0,
        endpoints: '{}',
        status: 1,
        sync_official: 0,
        name_rule: 1,
        bound_channels: [],
        enable_groups: [],
        quota_types: [],
        matched_models: [],
        matched_count: 0,
        created_time: 1,
        updated_time: 1,
      }
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
      await page.route('**/api/models/1', (route) =>
        route.fulfill({ json: { success: true, data: model } })
      )
      await page.route('**/api/vendors/search?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: { page: 1, page_size: 100, total: 0, items: [] },
          },
        })
      )
      await page.route('**/api/prefill_group/**', (route) =>
        route.fulfill({ json: { success: true, data: [] } })
      )
      await page.route('**/api/billing/prices', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: [
              {
                id: 3,
                model: 'video-fixture',
                contract_version: 2,
                calculation_version: 'billing-v2',
                result_unit: 'usd',
                mode: 'expression',
                expression:
                  'v2:u("resolution") == "720p" ? u("seconds") * 0.2 : u("seconds") * 0.4',
                prices: [],
                usage_schema: { version: 1, hash: 'old', fields },
                created_at: 1,
                hash: 'price',
              },
            ],
          },
        })
      )
      await page.route('**/api/billing/prices/task-schema?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              model: 'video-fixture',
              plugin_key: 'video',
              plugin_version: '2',
              source_hash: 'source',
              usage_schema: {
                version: 1,
                hash: 'new',
                fields: {
                  ...fields,
                  resolution: { ...fields.resolution, enum: ['1080p'] },
                },
              },
              manifest_schema: {},
              review_required: false,
              plugins: [{ key: 'video', name: 'Video', version: '2' }],
            },
          },
        })
      )
      let preview: Record<string, unknown> | undefined
      await page.route('**/api/billing/prices/preview', (route) => {
        preview = route.request().postDataJSON()
        return route.fulfill({
          json: {
            success: false,
            message: 'Fixture: invalid old enum requires review',
          },
        })
      })
      await page.goto('/models/metadata')
      await page
        .getByRole('button', { name: '编辑模型', exact: true })
        .filter({ visible: true })
        .first()
        .click()
      const dialog = page.getByRole('dialog')
      await expect(dialog.locator('textarea[spellcheck=false]')).toHaveValue(
        /720p/
      )
      await dialog.locator('[data-adopt-task-schema]').click()
      await expect(dialog.locator('textarea[spellcheck=false]')).toHaveValue(
        /720p/
      )
      await dialog.getByText('价格预览', { exact: true }).click()
      await dialog.getByLabel('seconds', { exact: true }).fill('0')
      await dialog
        .getByLabel('resolution', { exact: true })
        .selectOption('1080p')
      await dialog.getByLabel('audio', { exact: true }).selectOption('false')
      await dialog.locator('[data-price-preview]').click()
      await expect(
        dialog.getByRole('alert').filter({ hasText: 'invalid old enum' })
      ).toBeVisible()
      expect(preview?.request_facts).toEqual({
        resolution: '1080p',
        audio: false,
      })
      expect((preview?.usage as Record<string, unknown>).actual_facts).toEqual({
        seconds: '0',
      })
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('schema-preview.png'),
        fullPage: true,
      })
    })
  }
}
