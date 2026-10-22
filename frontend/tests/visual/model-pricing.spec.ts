import { expect, test, type Locator } from '@playwright/test'
import { configureStablePage, assertNoHorizontalOverflow } from './fixtures'
import { PRICING_KEYS } from '../../src/api/pricingKeys'
import type {
  ModelPricingChange,
  ModelPricingEntry,
  PricingValues,
} from '../../src/api/pricingConfig'

async function expectInside(part: Locator, container: Locator) {
  await expect(part).toBeVisible()
  const inner = (await part.boundingBox())!
  const outer = (await container.boundingBox())!
  expect(inner.x).toBeGreaterThanOrEqual(outer.x)
  expect(inner.x + inner.width).toBeLessThanOrEqual(outer.x + outer.width + 1)
}

for (const theme of ['light', 'dark'] as const)
  for (const width of [1440, 390]) {
    test(`plugin-only task price creates and simulates a conditional draft ${theme} ${width}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme })
      await page.addInitScript(() =>
        localStorage.setItem('ren2hub_locale', 'en')
      )
      const pricing: PricingValues = {
        'billing_setting.plugin_billing_expr': { provider: 'u("seconds") * 1' },
      }
      const usage_schema = {
        seconds: {
          type: 'number',
          unit: 'second',
          description: 'Duration unit price',
        },
        resolution: { enum: ['720p', '1080p'], description: 'Resolution' },
      }
      const writes: ModelPricingChange[][] = []
      const simulations: Record<string, unknown>[] = []
      await page.route('**/api/channel/models_enabled', (route) =>
        route.fulfill({ json: { success: true, data: ['task-model'] } })
      )
      await page.route('**/api/option/model_pricing**', (route) => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/simulate')) {
          simulations.push(route.request().postDataJSON())
          return route.fulfill({
            json: { success: true, data: { amount_usd: '2.5' } },
          })
        }
        if (path.endsWith('/preview'))
          return route.fulfill({
            json: { success: true, data: { effective: pricing } },
          })
        if (route.request().method() === 'PATCH') {
          writes.push(route.request().postDataJSON().changes)
          return route.fulfill({ json: { success: true } })
        }
        return route.fulfill({
          json: {
            success: true,
            data: {
              entries: [
                {
                  model_name: 'task-model',
                  version: 'v1',
                  configured: pricing,
                  effective: pricing,
                  usage_schema,
                  plugin_variants: [
                    {
                      plugin_key: 'provider',
                      plugin_name: 'Task provider',
                      usage_schema,
                      configured: 'u("seconds") * 1',
                      effective: 'u("seconds") * 1',
                      compatible: true,
                    },
                  ],
                },
              ],
              options: Object.fromEntries(
                PRICING_KEYS.map((key) => [key, '{}'])
              ),
              empty_version: 'empty',
            },
          },
        })
      })
      await page.goto('/models/pricing')
      await page
        .getByRole('button', { name: 'task-model', exact: true })
        .click()
      const editor = page.locator('.pricing-editor')
      await editor
        .getByRole('combobox', { name: 'Plugin pricing', exact: true })
        .selectOption('provider')
      await editor
        .getByRole('combobox', { name: 'Plugin pricing', exact: true })
        .focus()
      await page.keyboard.press('Tab')
      await expect(
        editor.getByRole('button', { name: 'Expression source', exact: true })
      ).toBeFocused()
      await page.screenshot({
        path: testInfo.outputPath('plugin-selector.png'),
      })
      await editor.locator('summary').filter({ hasText: 'Task usage' }).click()
      await editor.getByPlaceholder('Usage unit price').fill('0.125')
      await editor
        .getByRole('button', { name: 'Generate task pricing draft' })
        .click()
      await editor
        .getByRole('button', { name: 'Add condition', exact: true })
        .click()
      await editor
        .getByRole('combobox', { name: 'Usage field', exact: true })
        .selectOption('resolution')
      await editor
        .getByRole('combobox', { name: 'Value', exact: true })
        .selectOption('1080p')
      await editor.getByLabel('Multiplier', { exact: true }).fill('2')
      await editor.getByRole('button', { name: 'Apply', exact: true }).click()
      await expect(
        editor.getByRole('textbox', { name: 'Expression source', exact: true })
      ).toHaveValue(/u\("resolution"\) == "1080p"/)
      await editor
        .locator('summary')
        .filter({ hasText: 'Request simulation' })
        .click()
      await editor
        .getByLabel('Usage JSON', { exact: true })
        .fill('{"seconds":10,"resolution":"1080p"}')
      await editor
        .getByRole('button', { name: 'Calculate draft charge' })
        .click()
      await expect(
        editor.getByText('Simulated charge: $2.500', { exact: true })
      ).toBeVisible()
      expect(simulations[0]).toMatchObject({
        plugin_key: 'provider',
        usage: { seconds: 10, resolution: '1080p' },
      })
      await assertNoHorizontalOverflow(page)
      await page.screenshot({ path: testInfo.outputPath('plugin-pricing.png') })
      const editorSave = editor.getByRole('button', {
        name: 'Save model prices',
        exact: true,
      })
      // Desktop pins the editor footer, so Save is reachable without
      // scrolling back after editing the lower sections.
      if (width >= 768) await expect(editorSave).toBeInViewport()
      await editorSave.click()
      await expect(
        page.getByText('Model prices saved', { exact: true })
      ).toBeVisible()
      expect(writes[0]?.[0]?.pricing).toEqual({
        'billing_setting.plugin_billing_expr': {
          provider: expect.stringContaining('u("resolution")'),
        },
      })
    })
  }

for (const theme of ['light', 'dark'] as const)
  for (const width of [1440, 390]) {
    test(`manual pricing and source draft ${theme} ${width}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme })
      await page.addInitScript(() =>
        localStorage.setItem('ren2hub_locale', 'en')
      )
      const writes: ModelPricingChange[][] = [],
        errors: string[] = []
      let entries: ModelPricingEntry[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      await page.route('**/api/channel/models_enabled', (route) =>
        route.fulfill({
          json: { success: true, data: ['test-model', 'task-model'] },
        })
      )
      await page.route('**/api/option/model_pricing**', async (route) => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/preview'))
          return route.fulfill({
            json: {
              success: true,
              data: { effective: route.request().postDataJSON().pricing },
            },
          })
        if (path.endsWith('/convert'))
          return route.fulfill({
            json: {
              success: true,
              data: {
                effective: { ModelPrice: 0.2 },
                expression: 'fixed(0.2)',
              },
            },
          })
        if (path.endsWith('/simulate'))
          return route.fulfill({
            json: { success: true, data: { amount_usd: '0.003' } },
          })
        if (route.request().method() === 'PATCH') {
          const changes = route.request().postDataJSON()
            .changes as ModelPricingChange[]
          writes.push(changes)
          entries = changes
            .filter((change) => !change.reset)
            .map((change) => ({
              model_name: change.model_name,
              version: 'saved',
              configured: change.pricing,
              effective: change.pricing,
            }))
          return route.fulfill({ json: { success: true, data: null } })
        }
        return route.fulfill({
          json: {
            success: true,
            data: {
              entries,
              options: Object.fromEntries(
                PRICING_KEYS.map((key) => [key, '{}'])
              ),
              empty_version: 'empty',
            },
          },
        })
      })
      await page.route('**/api/ratio_sync/fetch', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              sources: [
                {
                  id: 'sub2api',
                  name: 'Sub2API',
                  status: 'partial',
                  errors: [],
                  models: [
                    {
                      model_name: 'test-model',
                      provider: 'provider',
                      pricing: {
                        'billing_setting.billing_mode': 'tiered_expr',
                        'billing_setting.billing_expr':
                          'tier("base", p * 2 + c * 4)',
                      },
                    },
                    {
                      model_name: 'unsupported',
                      provider: 'provider',
                      pricing: {},
                      unsupported_reason: 'Unsupported tier',
                    },
                  ],
                },
                {
                  id: 'axonhub',
                  name: 'AxonHub',
                  status: 'error',
                  models: [],
                  errors: ['Unavailable source'],
                },
                {
                  id: 'basellm',
                  name: 'basellm',
                  status: 'success',
                  models: [
                    {
                      model_name: 'second-model',
                      provider: '',
                      pricing: { ModelPrice: 0.3 },
                    },
                  ],
                  errors: [],
                },
                {
                  id: 'models_dev',
                  name: 'models.dev',
                  status: 'success',
                  models: [],
                  errors: [],
                },
                {
                  id: 'openrouter',
                  name: 'OpenRouter',
                  status: 'success',
                  models: [],
                  errors: [],
                },
              ],
            },
          },
        })
      )
      await page.goto('/models/pricing')
      await expect(
        page.getByRole('tab', { name: 'Pricing', exact: true })
      ).toHaveAttribute('aria-selected', 'true')
      await expect(
        page
          .getByText('No prices configured', { exact: true })
          .filter({ visible: true })
      ).toBeVisible()
      const fetch = page.getByRole('button', {
        name: 'Auto fetch',
        exact: true,
      })
      await fetch.click()
      const dialog = page.getByRole('dialog')
      await expect(dialog.getByText('Unavailable source')).toBeVisible()
      await expect(
        dialog.getByRole('button', { name: 'Fill pricing draft' })
      ).toBeDisabled()
      await dialog.locator('input[value=sub2api]').check()
      await expect(
        dialog.getByLabel('Select unsupported provider')
      ).toBeDisabled()
      await dialog.getByLabel('Select test-model provider').check()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('price-source.png'),
        fullPage: true,
      })
      await dialog.getByRole('button', { name: 'Fill pricing draft' }).click()
      await expect(dialog).toHaveCount(0)
      expect(writes).toEqual([])
      await expect(
        page.getByText('1 models filled. Save to apply.')
      ).toBeVisible()
      // Name, mode, price summary and Delete stay inside the list card at
      // desktop and phone widths; the table never needs a hidden h-scroll.
      const list = page.locator('.pricing-models')
      const row = list.locator('tbody tr').filter({ hasText: 'test-model' })
      await expect(row.getByText('Expression', { exact: true })).toBeVisible()
      for (const part of [
        row.locator('.pricing-summary'),
        row.getByRole('button', { name: 'Delete', exact: true }),
      ])
        await expectInside(part, list)
      await page
        .getByRole('button', { name: 'test-model', exact: true })
        .click()
      const editor = page.locator('.pricing-editor')
      await expect(
        editor.getByRole('textbox', { name: 'Expression source', exact: true })
      ).toHaveValue('tier("base", p * 2 + c * 4)')
      await editor
        .getByRole('button', { name: 'Visual tiers', exact: true })
        .click()
      await expect(editor.getByLabel('Tier name')).toHaveValue('base')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('manual-pricing.png'),
        fullPage: true,
      })
      await editor
        .getByRole('button', { name: 'Save model prices', exact: true })
        .click()
      await expect(
        page.getByText('Model prices saved', { exact: true })
      ).toBeVisible()
      expect(writes).toHaveLength(1)
      expect(writes[0]?.[0]).toMatchObject({
        model_name: 'test-model',
        expected_version: 'empty',
        pricing: {
          'billing_setting.billing_expr': 'tier("base", p * 2 + c * 4)',
        },
      })
      if (width < 768) await page.keyboard.press('Escape')
      await fetch.click()
      await expect(page.getByRole('dialog')).toBeVisible()
      await page.keyboard.press('Escape')
      await expect(fetch).toBeFocused()
      expect(writes).toHaveLength(1)
      await page
        .getByRole('radio', { name: 'Tool prices', exact: true })
        .click()
      await page
        .getByRole('button', { name: 'Add price dimension', exact: true })
        .click()
      await page.getByLabel('Tool name', { exact: true }).fill('web_search')
      await page.getByLabel('Unit price', { exact: true }).fill('0')
      await assertNoHorizontalOverflow(page)
      expect(errors).toEqual([])
    })
  }

test('blank expressions and server conflicts keep a recoverable draft', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
  let saves = 0
  await page.route('**/api/channel/models_enabled', (route) =>
    route.fulfill({ json: { success: true, data: [] } })
  )
  await page.route('**/api/option/model_pricing**', (route) => {
    if (route.request().method() === 'PATCH') {
      saves++
      return route.fulfill({
        status: 409,
        json: {
          success: false,
          message: 'Model changed by another administrator',
        },
      })
    }
    return route.fulfill({
      json: {
        success: true,
        data: new URL(route.request().url()).pathname.endsWith('/preview')
          ? { effective: {} }
          : {
              entries: [],
              options: Object.fromEntries(
                PRICING_KEYS.map((key) => [key, '{}'])
              ),
              empty_version: 'empty',
            },
      },
    })
  })
  await page.goto('/models/pricing')
  await page
    .getByRole('button', { name: 'Add model', exact: true })
    .first()
    .click()
  const editor = page.locator('.pricing-editor')
  await editor.getByLabel('Model ID', { exact: true }).fill('free-model')
  await editor
    .getByRole('button', { name: 'Save model prices', exact: true })
    .click()
  await expect(editor.getByRole('alert')).toContainText('explicitly enter 0')
  expect(saves).toBe(0)
  await editor
    .getByRole('textbox', { name: 'Expression source', exact: true })
    .fill('0')
  await editor
    .getByRole('button', { name: 'Save model prices', exact: true })
    .click()
  await expect(page.getByRole('alert').first()).toContainText(
    'Model changed by another administrator'
  )
  expect(saves).toBe(1)
  await expect(
    editor.getByRole('textbox', { name: 'Expression source', exact: true })
  ).toHaveValue('0')
})

test('model pricing is a model management section that guards its draft', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1024, height: 900 })
  await configureStablePage(page, { theme: 'light' })
  await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
  const pricing = { ModelPrice: 0.2 }
  await page.route('**/api/channel/models_enabled', (route) =>
    route.fulfill({ json: { success: true, data: ['test-model'] } })
  )
  await page.route('**/api/option/model_pricing**', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          entries: [
            {
              model_name: 'test-model',
              version: 'v1',
              configured: pricing,
              effective: pricing,
            },
          ],
          options: Object.fromEntries(PRICING_KEYS.map((key) => [key, '{}'])),
          empty_version: 'empty',
        },
      },
    })
  )
  await page.goto('/models/metadata')
  const expectModelsAdminCurrent = async () => {
    await expect(
      page.locator('[data-console-nav-item="models-admin"]').first()
    ).toBeVisible()
    await expect(
      page.locator(
        '[data-console-nav-item="models-admin"]:not([aria-current="page"])'
      )
    ).toHaveCount(0)
  }
  await expectModelsAdminCurrent()
  await expect(
    page.locator('[data-console-nav-item="model-pricing"]')
  ).toHaveCount(0)

  await page.getByRole('tab', { name: 'Pricing', exact: true }).click()
  await expect(page).toHaveURL(/\/models\/pricing$/)
  await expect(
    page.getByRole('tab', { name: 'Pricing', exact: true })
  ).toHaveAttribute('aria-selected', 'true')
  await expectModelsAdminCurrent()
  await assertNoHorizontalOverflow(page)

  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  await expect(page.getByText('Unsaved prices', { exact: true })).toBeVisible()
  const modelsTab = page.getByRole('tab', { name: 'Models', exact: true })
  page.once('dialog', (dialog) => void dialog.dismiss())
  await modelsTab.click()
  await expect(page).toHaveURL(/\/models\/pricing$/)
  await expect(page.getByText('Unsaved prices', { exact: true })).toBeVisible()
  page.once('dialog', (dialog) => void dialog.accept())
  await modelsTab.click()
  await expect(page).toHaveURL(/\/models\/metadata$/)
})
