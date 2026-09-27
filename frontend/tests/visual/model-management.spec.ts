import { expect, test, type Page } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

const model = {
  id: 1,
  model_name: 'gpt-local',
  description: 'Ren2Hub metadata and routing',
  icon: 'OpenAI',
  tags: 'text,vision',
  vendor_id: 1,
  endpoints: '{"openai":{"path":"/v1/chat/completions","method":"POST"}}',
  status: 1,
  sync_official: 1,
  name_rule: 1,
  bound_channels: [{ name: 'primary', type: 1 }],
  enable_groups: ['default', 'vip'],
  quota_types: [0],
  matched_models: ['gpt-local-1', 'gpt-local-2'],
  matched_count: 2,
  created_time: 1700000000,
  updated_time: 1700000300,
}
const vendors = [
  {
    id: 1,
    name: 'OpenAI',
    description: 'Model provider',
    icon: 'OpenAI',
    status: 1,
  },
]
const groups = [
  {
    id: 1,
    name: 'Standard models',
    description: 'Reusable model list',
    type: 'model',
    items: ['gpt-local', 'gpt-local-2'],
  },
  {
    id: 2,
    name: 'Chat endpoints',
    description: '',
    type: 'endpoint',
    items: model.endpoints,
  },
]
async function data(page: Page) {
  await page.route('**/api/billing/prices/task-schema?*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          model: new URL(route.request().url()).searchParams.get('model'),
          plugin_key: '',
          usage_schema: null,
          manifest_schema: null,
          review_required: false,
          plugins: [],
        },
      },
    })
  )
  await page.route('**/api/models/search?*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          page: 1,
          page_size: 20,
          total: 1,
          items: [model],
          vendor_counts: { 1: 1 },
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
        data: { page: 1, page_size: 100, total: 1, items: vendors },
      },
    })
  )
  await page.route('**/api/prefill_group/?*', (route) =>
    route.fulfill({ json: { success: true, data: groups } })
  )
  await page.route('**/api/prefill_group/', (route) =>
    route.fulfill({ json: { success: true, data: groups } })
  )
  await page.route('**/api/billing/prices', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            id: 1,
            model: 'gpt-local',
            provenance: {
              kind: 'retail',
              retail_reason: 'Reviewed visual fixture price',
            },
            contract_version: 3,
            calculation_version: 'billing-v3',
            result_unit: 'usd',
            mode: 'unit_price',
            prices: [
              {
                quantity: 'p',
                unit: 'per_million_tokens',
                price: '1.000000000000000001',
              },
              { quantity: 'c', unit: 'per_million_tokens', price: '2' },
            ],
            created_at: 1700000000,
            hash: 'fixture',
          },
        ],
      },
    })
  )
  await page.route('**/api/models/missing', (route) =>
    route.fulfill({ json: { success: true, data: ['missing-local'] } })
  )
  await page.route('**/api/billing/prices/preview', async (route) => {
    expect(route.request().method()).toBe('POST')
    expect(route.request().postDataJSON().price.contract_version).toBe(2)
    await route.fulfill({
      json: {
        success: true,
        data: {
          quote: {},
          charge: {
            lines: [
              {
                quantity: 'expression',
                count: '1',
                unit: 'per_call',
                unit_price: '0.001234567',
                subtotal_usd: '0.001234567',
              },
            ],
            base_usd: '0.001234567',
            adjustments: [],
            total_usd: '0.001234567',
            final_quota: '617',
            unrounded_quota: '617.2835',
            rounding_delta: '-0.2835',
            calculation_version: 'billing-v3',
            rounding_version: 'half-up-positive-minimum-v1',
            trace: {
              matched_tier: 'night',
              request_rules: [
                {
                  cond: 'hour("Asia/Shanghai") < 6',
                  multiplier: '0.8',
                  matched: true,
                },
              ],
            },
          },
        },
      },
    })
  })
  await page.route('**/api/models/sync_upstream/preview?*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          missing: ['missing-local'],
          conflicts: [
            {
              model_name: 'gpt-local',
              fields: [
                {
                  field: 'description',
                  local: 'Local description',
                  upstream: 'Upstream description',
                },
              ],
            },
          ],
        },
      },
    })
  )
}

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`automatic price controls ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      await page.addInitScript(() =>
        localStorage.setItem('ren2hub_locale', 'en')
      )
      await data(page)
      let restored = false
      let conflicts = true
      let config = { enabled: false, check_interval_minutes: 30 }
      const source = {
        source: 'sub2api',
        snapshot_id: 1,
        parser_version: 'sub2api-v3',
        snapshot_count: 1,
        role: 'primary',
        url: 'https://example.test/sub2api/prices.json',
        hash: 'retained-valid-source-hash',
        checked_at: 1785114000,
        updated_at: 1785114000,
        last_error: 'Upstream unavailable; retained valid snapshot',
        model_count: 1400,
        rejected_count: 2,
        snapshot_state: 'cached',
      }
      await page.route('**/api/billing/prices/automatic', async (route) => {
        if (route.request().method() === 'PUT') {
          config = route.request().postDataJSON()
          expect(config).toEqual({ enabled: false, check_interval_minutes: 60 })
          await route.fulfill({ json: { success: true, data: config } })
          return
        }
        await route.fulfill({
          json: {
            success: true,
            data: {
              config,
              sources: [
                source,
                {
                  source: 'axonhub',
                  snapshot_id: 1,
                  parser_version: 'axonhub-v3',
                  snapshot_count: 1,
                  role: 'fallback',
                  url: 'https://example.test/axonhub/prices.json',
                  hash: 'bundled-fallback-hash',
                  checked_at: 0,
                  updated_at: 1785114000,
                  last_error: '',
                  model_count: 900,
                  rejected_count: 0,
                  snapshot_state: 'bundled',
                },
              ],
              origins: [
                {
                  model: 'gpt-local',
                  version_id: restored ? 2 : 1,
                  origin: restored ? 'sub2api' : 'manual',
                  source_model: restored ? 'gpt-local' : '',
                  source_hash: restored ? source.hash : '',
                },
              ],
            },
          },
        })
      })
      const task = {
        task_id: 'pricing-check',
        type: 'billing_pricing_sync',
        status: 'running',
        created_at: 1785114000,
        updated_at: 1785114000,
      }
      const result = {
        checked: 2,
        changed: 1,
        published: 7,
        deactivated: 2,
        unconfigured_count: 101,
        unconfigured: ['embedding-unavailable'],
        errors: ['Sub2API timeout; retained source snapshot'],
        unconfigured_details: [
          {
            model: 'embedding-unavailable',
            reasons: [
              { source: 'sub2api', reason: 'Unsupported input image cost' },
            ],
          },
        ],
      }
      await page.route('**/api/system-task/list?*', (route) => {
        const query = new URL(route.request().url()).searchParams
        expect(query.get('type')).toBe('billing_pricing_sync')
        expect(query.get('limit')).toBe('1')
        expect(query.has('scope')).toBe(false)
        return route.fulfill({
          json: {
            success: true,
            total: 1,
            data: [{ ...task, task_id: 'scheduled-check' }],
          },
        })
      })
      await page.route('**/api/system-task/scheduled-check', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              ...task,
              task_id: 'scheduled-check',
              status: 'failed',
              error: 'One source unavailable',
              result,
            },
          },
        })
      )
      await page.route('**/api/billing/prices/automatic/sync', (route) =>
        route.fulfill({ json: { success: true, data: task } })
      )
      await page.route('**/api/system-task/pricing-check', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: { ...task, status: 'succeeded', result },
          },
        })
      )
      await page.route(
        '**/api/billing/prices/automatic/restore',
        async (route) => {
          expect(route.request().postDataJSON()).toEqual({
            model: 'gpt-local',
            expected_version: 1,
          })
          if (conflicts) {
            conflicts = false
            await route.fulfill({
              status: 409,
              json: { success: false, message: 'Price version conflict' },
            })
            return
          }
          restored = true
          await route.fulfill({
            json: {
              success: true,
              data: {
                id: 2,
                model: 'gpt-local',
                provenance: {
                  kind: 'retail',
                  retail_reason: 'Reviewed visual fixture price',
                },
                contract_version: 3,
                calculation_version: 'billing-v3',
                result_unit: 'usd',
                mode: 'unit_price',
                prices: [
                  {
                    quantity: 'p',
                    unit: 'per_million_tokens',
                    price: '2.000000000000000001',
                  },
                ],
                created_at: 1785114000,
                hash: 'restored-price',
              },
            },
          })
        }
      )
      await page.goto('/models/metadata')
      const panel = page.locator('[data-automatic-pricing]')
      await expect(panel).toContainText('Price check scheduled-check: Failed')
      await expect(panel).toContainText('One source unavailable')
      await expect(panel.locator('[data-automatic-result]')).toContainText(
        '101'
      )
      await panel.locator('summary').click()
      await expect(panel).toContainText('Unsupported input image cost')
      await expect(panel).toContainText('Sub2API timeout')
      await expect(panel).toContainText(source.last_error)
      await expect(panel.locator('[data-price-source]')).toHaveCount(2)
      await expect(panel.locator('[data-price-source=sub2api]')).toContainText(
        'Refresh failed; using valid cache'
      )
      await expect(panel.locator('[data-price-source=axonhub]')).toContainText(
        'Bundled snapshot; remote source not checked yet'
      )
      await expect(panel.getByRole('checkbox')).not.toBeChecked()
      await panel.getByRole('spinbutton').fill('60')
      await panel.getByRole('button', { name: 'Save schedule' }).click()
      await expect(panel).toContainText('Automatic pricing schedule saved.')
      await panel.getByRole('button', { name: 'Check prices now' }).click()
      await expect(panel).toContainText('Price check pricing-check: Completed')
      await panel.locator('summary').click()
      await panel.locator('[data-automatic-result]').scrollIntoViewIfNeeded()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('automatic-pricing.png'),
        fullPage: true,
      })
      if (mobile) {
        for (const source of ['sub2api', 'axonhub']) {
          const card = panel.locator(`[data-price-source=${source}]`)
          await card.scrollIntoViewIfNeeded()
          await expect(card).toBeInViewport()
          await page.screenshot({
            path: testInfo.outputPath(`automatic-${source}.png`),
          })
        }
      }
      await page
        .getByRole('button', { name: 'Edit model', exact: true })
        .filter({ visible: true })
        .first()
        .click()
      const drawer = page.getByRole('dialog').first()
      await expect(drawer.locator('[data-price-origin]')).toContainText(
        'Manual'
      )
      const amount = drawer.locator('input[inputmode=decimal]').first()
      await amount.fill('7.000000000000000001')
      await drawer.locator('[data-restore-automatic]').click()
      let confirm = page.getByRole('dialog').last()
      await expect(confirm).toContainText('any unsaved price draft')
      await confirm
        .getByRole('button', { name: 'Restore automatic pricing', exact: true })
        .click()
      await expect(drawer).toContainText('changed elsewhere')
      await expect(amount).toHaveValue('7.000000000000000001')
      await drawer.locator('[data-restore-automatic]').click()
      confirm = page.getByRole('dialog').last()
      await confirm
        .getByRole('button', { name: 'Restore automatic pricing', exact: true })
        .click()
      await expect(drawer.locator('[data-price-origin]')).toContainText(
        'Sub2API'
      )
      await expect(amount).toHaveValue('2.000000000000000001')
      await expect(drawer.locator('[data-restore-automatic]')).toHaveCount(0)
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('automatic-restored.png'),
        fullPage: true,
      })
    })
  }
}

for (const language of ['zh-CN', 'en']) {
  for (const theme of ['light', 'dark'] as const) {
    for (const mobile of [false, true]) {
      test(`model management ${language} ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
        page,
      }, testInfo) => {
        await page.setViewportSize(
          mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
        )
        await configureStablePage(page, { theme })
        await page.addInitScript(
          (locale) => localStorage.setItem('ren2hub_locale', locale),
          language
        )
        await data(page)
        const failures: string[] = []
        page.on('pageerror', (error) => failures.push(error.message))
        page.on('response', (response) => {
          if (response.status() === 404 && response.url().includes('127.0.0.1'))
            failures.push(response.url())
        })
        const zh = language === 'zh-CN'
        await page.goto('/models/metadata')
        await expect(page.locator('[data-automatic-pricing]')).toContainText(
          zh ? '模型自动定价' : 'Automatic model pricing'
        )
        await expect(page.locator('[data-price-source]')).toHaveCount(2)
        await expect(page.locator('[data-price-source=axonhub]')).toContainText(
          zh ? '备用来源' : 'Fallback source'
        )
        await expect(page.locator('[data-price-source=sub2api]')).toContainText(
          zh ? '已检查远端，快照有效' : 'Remote source checked; snapshot valid'
        )
        await expect(
          page
            .getByText('gpt-local', { exact: true })
            .filter({ visible: true })
            .first()
        ).toBeVisible()
        await assertNoHorizontalOverflow(page)
        await page.screenshot({
          path: testInfo.outputPath('models.png'),
          fullPage: true,
        })
        const edit = page
          .getByRole('button', {
            name: zh ? '编辑模型' : 'Edit model',
            exact: true,
          })
          .filter({ visible: true })
          .first()
        await edit.click()
        const drawer = page.getByRole('dialog')
        await expect(
          drawer.getByLabel(zh ? '模型名称' : 'Model name', { exact: true })
        ).toHaveValue('gpt-local')
        await expect(
          drawer.getByText(zh ? '模型价格版本' : 'Model price version', {
            exact: true,
          })
        ).toBeVisible()
        await expect(drawer.locator('img').first()).toBeVisible()
        await drawer
          .getByText(zh ? '本币编辑辅助' : 'Local currency editing helper', {
            exact: true,
          })
          .click()
        const usd = drawer.locator('input[inputmode=decimal]').first()
        await expect(usd).toHaveValue('1.000000000000000001')
        await drawer.locator('[data-exchange-rate]').fill('7')
        await expect(usd).toHaveValue('1.000000000000000001')
        await drawer
          .getByRole('button', {
            name: zh
              ? '将单价转换为表达式草稿'
              : 'Convert unit prices to expression draft',
          })
          .click()
        await expect(
          drawer.locator('[data-price-conversion-preview]')
        ).toContainText('v1:p * 1.000000000000000001')
        await drawer.locator('[data-apply-price-conversion]').click()
        await drawer
          .getByText(zh ? '时间价格规则' : 'Time price rule', { exact: true })
          .click()
        await drawer
          .getByRole('button', {
            name: zh ? '添加到表达式' : 'Add rule to expression',
          })
          .click()
        await drawer
          .getByText(zh ? '价格预览' : 'Price preview', { exact: true })
          .click()
        await drawer.locator('[data-price-preview]').click()
        await expect(
          drawer.locator('[data-price-preview-result]')
        ).toContainText('617 quota')
        await expect(
          drawer.locator('[data-price-preview-result]')
        ).toContainText('night')
        const updatedSchema = page.waitForRequest((request) => {
          const url = new URL(request.url())
          return (
            url.pathname === '/api/billing/prices/task-schema' &&
            url.searchParams.get('model') === 'gpt-renamed'
          )
        })
        await drawer
          .getByLabel(zh ? '模型名称' : 'Model name', { exact: true })
          .fill('gpt-renamed')
        await updatedSchema
        await expect(drawer.locator('[data-price-preview-result]')).toHaveCount(
          0
        )
        await expect(drawer.locator('textarea[spellcheck=false]')).toHaveValue(
          /1\.000000000000000001/
        )
        await drawer.locator('[data-price-preview]').click()
        await expect(
          drawer.locator('[data-price-preview-result]')
        ).toContainText('617 quota')
        await drawer
          .locator('[data-price-preview-result]')
          .scrollIntoViewIfNeeded()
        await page.screenshot({
          path: testInfo.outputPath('price-preview.png'),
          fullPage: true,
        })
        await assertNoHorizontalOverflow(page)
        await drawer.getByRole('button', { name: 'JSON', exact: true }).click()
        await drawer.getByLabel('JSON', { exact: true }).fill('{')
        await drawer
          .getByRole('button', { name: zh ? '保存' : 'Save', exact: true })
          .click()
        await expect(drawer.getByRole('alert').first()).toBeVisible()
        await assertNoHorizontalOverflow(page)
        await page.screenshot({
          path: testInfo.outputPath('model-drawer.png'),
          fullPage: true,
        })
        await page.keyboard.press('Escape')
        await expect(drawer).toHaveCount(0)
        await expect(edit).toBeFocused()
        for (const kind of ['vendors', 'prefill-groups']) {
          await page.goto(`/models/${kind}?keyword=gpt&source=bookmark#details`)
          await expect(page).toHaveURL(
            new RegExp(`/models/metadata\\?.*manage=${kind}.*#details$`)
          )
          const manager = page.getByRole('dialog')
          await expect(manager).toBeVisible()
          await expect(
            manager
              .getByText(kind === 'vendors' ? 'OpenAI' : 'Standard models', {
                exact: true,
              })
              .first()
          ).toBeVisible()
          await manager
            .getByRole('button', { name: zh ? '编辑' : 'Edit', exact: true })
            .first()
            .click()
          await expect(page.getByRole('dialog')).toHaveCount(2)
          await assertNoHorizontalOverflow(page)
          await page.screenshot({
            path: testInfo.outputPath(`${kind}.png`),
            fullPage: true,
          })
          await page.keyboard.press('Escape')
          await expect(page.getByRole('dialog')).toHaveCount(1)
          await page.keyboard.press('Escape')
          await expect(page.getByRole('dialog')).toHaveCount(0)
          await expect(page).not.toHaveURL(/manage=/)
        }
        expect(
          await page
            .locator('img')
            .evaluateAll((images) =>
              images
                .filter((image) => !image.complete || image.naturalWidth === 0)
                .map((image) => image.src)
            )
        ).toEqual([])
        expect(failures).toEqual([])
      })
    }
  }
}

test('sync cancellation and missing-model creation preserve the draft boundary', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'dark' })
  await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
  await data(page)
  const writes: string[] = []
  page.on('request', (request) => {
    if (request.url().includes('/api/models') && request.method() !== 'GET')
      writes.push(request.url())
  })
  await page.goto('/models/metadata')
  await page.getByRole('button', { name: 'Sync metadata', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.locator('option[value=config]')).toHaveAttribute(
    'disabled',
    ''
  )
  await dialog.getByRole('button', { name: 'Get sync preview' }).click()
  await expect(
    dialog.getByText('Upstream description', { exact: true })
  ).toBeVisible()
  await dialog.getByRole('checkbox').last().check()
  await page.keyboard.press('Escape')
  expect(writes).toEqual([])
  await page
    .getByRole('button', { name: 'Missing models', exact: true })
    .click()
  await dialog
    .getByRole('button', { name: 'Create metadata', exact: true })
    .click()
  await expect(
    page.getByRole('dialog').getByLabel('Model name', { exact: true })
  ).toHaveValue('missing-local')
  await page.keyboard.press('Escape')
  expect(writes).toEqual([])
})

for (const mobile of [false, true]) {
  for (const theme of ['light', 'dark'] as const) {
    test(`embedding price restriction ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      await page.addInitScript(() =>
        localStorage.setItem('ren2hub_locale', 'en')
      )
      await data(page)
      const embedding = {
        id: 1,
        model: 'gpt-local',
        provenance: {
          kind: 'retail',
          retail_reason: 'Reviewed visual fixture price',
        },
        contract_version: 3,
        calculation_version: 'billing-v3',
        result_unit: 'usd',
        usage_scope: 'embedding',
        mode: 'unit_price',
        prices: [{ quantity: 'p', unit: 'per_million_tokens', price: '0.025' }],
        created_at: 1700000000,
        hash: 'embedding-price-fixture',
      }
      let published = false
      await page.route('**/api/billing/prices', async (route) => {
        if (route.request().method() === 'POST') {
          const payload = route.request().postDataJSON()
          expect(payload.expected_version).toBe(1)
          expect(payload.price.usage_scope).toBe('embedding')
          expect(payload.price.prices[0].price).toBe('0.035000000000000001')
          published = true
          await route.fulfill({
            json: { success: true, data: { ...payload.price, id: 2 } },
          })
          return
        }
        await route.fulfill({ json: { success: true, data: [embedding] } })
      })
      await page.route('**/api/models/', (route) =>
        route.fulfill({ json: { success: true, data: model } })
      )
      await page.route('**/api/billing/prices/preview', async (route) => {
        const payload = route.request().postDataJSON()
        expect(payload.price.usage_scope).toBe('embedding')
        expect(payload.usage.output_tokens).toBe(0)
        await route.fulfill({
          status: 400,
          json: { success: false, message: 'Preview fixture unavailable' },
        })
      })
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      await page.goto('/models/metadata')
      await page
        .getByRole('button', { name: 'Edit model', exact: true })
        .filter({ visible: true })
        .first()
        .click()
      const drawer = page.getByRole('dialog')
      await expect(drawer.locator('[data-price-usage-scope]')).toContainText(
        'Embedding requests only'
      )
      await drawer.getByText('Price preview', { exact: true }).click()
      const preview = JSON.parse(
        await drawer.locator('[data-price-preview-usage]').inputValue()
      )
      expect(preview.output_tokens).toBe(0)
      await drawer.locator('[data-price-preview]').click()
      await expect(drawer).toContainText('Preview fixture unavailable')
      await drawer
        .locator('input[inputmode=decimal]')
        .first()
        .fill('0.035000000000000001')
      await drawer.locator('[data-price-usage-scope]').scrollIntoViewIfNeeded()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('embedding-price.png'),
        fullPage: true,
      })
      await drawer.getByRole('button', { name: 'Save', exact: true }).click()
      await expect(drawer).toHaveCount(0)
      expect(published).toBe(true)
      expect(errors).toEqual([])
    })
  }
}

for (const mobile of [false, true]) {
  for (const theme of ['light', 'dark'] as const) {
    test(`image count price draft ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      await page.addInitScript(() =>
        localStorage.setItem('ren2hub_locale', 'en')
      )
      await data(page)
      await page.route('**/api/billing/prices', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: [
              {
                id: 1,
                model: 'gpt-local',
                provenance: {
                  kind: 'retail',
                  retail_reason: 'Reviewed visual fixture price',
                },
                contract_version: 3,
                calculation_version: 'billing-v3',
                result_unit: 'usd',
                mode: 'unit_price',
                prices: [
                  { quantity: 'images', unit: 'per_image', price: '0.001' },
                ],
                created_at: 1700000000,
                hash: 'image-price-fixture',
              },
            ],
          },
        })
      )
      await page.route('**/api/billing/prices/preview', async (route) => {
        const payload = route.request().postDataJSON()
        expect(payload.price.expression).toBe('v1:fixed(0.001) * image_count')
        expect(payload.price.prices).toEqual([])
        expect(payload.usage.quantities.images).toBe('2')
        await route.fulfill({
          json: {
            success: true,
            data: {
              quote: {},
              charge: {
                lines: [
                  {
                    quantity: 'expression',
                    count: '1',
                    unit: 'per_call',
                    unit_price: '0.002',
                    subtotal_usd: '0.002',
                  },
                ],
                final_quota: '1000',
                base_usd: '0.002',
                adjustments: [],
                total_usd: '0.002',
                unrounded_quota: '1000',
                rounding_delta: '0',
                calculation_version: 'billing-v3',
                rounding_version: 'half-up-positive-minimum-v1',
                trace: { matched_tier: '', image_count: 2 },
              },
            },
          },
        })
      })
      const errors: string[] = []
      const writes: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      page.on('request', (request) => {
        if (
          request.method() !== 'GET' &&
          request.url().includes('/api/billing/prices') &&
          !request.url().endsWith('/preview')
        )
          writes.push(request.url())
      })
      await page.goto('/models/metadata')
      await page
        .getByRole('button', { name: 'Edit model', exact: true })
        .filter({ visible: true })
        .first()
        .click()
      const drawer = page.getByRole('dialog')
      await drawer
        .getByRole('button', {
          name: 'Convert unit prices to expression draft',
        })
        .click()
      await expect(
        drawer.locator('[data-price-conversion-preview]')
      ).toContainText('v1:fixed(0.001) * image_count')
      await drawer.locator('[data-apply-price-conversion]').click()
      await drawer.getByText('Price preview', { exact: true }).click()
      await drawer.locator('[data-price-preview-usage]').fill(
        JSON.stringify({
          quantities: { images: '2' },
          source: 'provider_image_count',
          complete: true,
        })
      )
      await drawer.locator('[data-price-preview]').click()
      const result = drawer.locator('[data-price-preview-result]')
      await expect(result).toContainText('1000 quota')
      await expect(result).toContainText('Image count: 2')
      await result.scrollIntoViewIfNeeded()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('image-count-preview.png'),
        fullPage: true,
      })
      expect(writes).toEqual([])
      expect(errors).toEqual([])
    })
  }
}
