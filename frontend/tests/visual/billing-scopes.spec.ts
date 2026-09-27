import { expect, test, type Page } from '@playwright/test'
import type { PriceVersion } from '../../src/api/modelPricing'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

const metadata = {
  id: 1,
  model_name: 'scoped-model',
  description: 'Scoped billing fixture',
  icon: 'OpenAI',
  tags: 'text',
  vendor_id: 0,
  endpoints: '{}',
  status: 1,
  sync_official: 0,
  name_rule: 0,
  bound_channels: [],
  enable_groups: [],
  quota_types: [],
  matched_models: [],
  matched_count: 0,
  created_time: 1,
  updated_time: 1,
}
const globalPrice: PriceVersion = {
  id: 101,
  model: metadata.model_name,
  scope_kind: 'global_model',
  contract_version: 2,
  calculation_version: 'billing-v2',
  result_unit: 'usd',
  mode: 'unit_price',
  created_at: 1,
  hash: 'global-hash',
  prices: [{ quantity: 'p', unit: 'per_million_tokens', price: '1' }],
}
const channelPrice: PriceVersion = {
  ...globalPrice,
  id: 109,
  scope_kind: 'channel_model',
  channel_id: 9,
  hash: 'channel-nine-hash',
  flat_fee: '0.125',
  effective_from: '2026-01-01T00:00:00Z',
  effective_to: '2027-01-01T00:00:00Z',
  prices: [
    {
      quantity: 'reasoning',
      unit: 'per_million_tokens',
      price: '',
      tier_mode: 'graduated',
      tiers: [
        { up_to: '1000000', unit_price: '2.000000000000000001' },
        { unit_price: '1' },
      ],
    },
  ],
}
const flatPrice: PriceVersion = {
  ...channelPrice,
  id: 110,
  channel_id: 10,
  hash: 'channel-ten-hash',
  flat_fee: '0.25',
  prices: [],
}
const charge = {
  lines: [
    {
      quantity: 'flat_fee',
      count: '1',
      unit: 'per_call',
      unit_price: '0.125',
      subtotal_usd: '0.125',
    },
    {
      quantity: 'reasoning',
      count: '1000000',
      unit: 'per_million_tokens',
      unit_price: '2.000000000000000001',
      subtotal_usd: '2.000000000000000001',
    },
  ],
  total_usd: '2.125000000000000001',
  final_quota: '1062500',
  unrounded_quota: '1062500.0000000000005',
  rounding_delta: '-0.0000000000005',
  calculation_version: 'billing-v2',
  rounding_version: 'half-up-positive-minimum-v1',
}

async function metadataRoutes(page: Page) {
  await page.route('**/api/models/search?*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          page: 1,
          page_size: 20,
          total: 1,
          items: [metadata],
          vendor_counts: {},
        },
      },
    })
  )
  await page.route('**/api/models/1', (route) =>
    route.fulfill({ json: { success: true, data: metadata } })
  )
  await page.route('**/api/models/', (route) =>
    route.fulfill({ json: { success: true, data: metadata } })
  )
  await page.route('**/api/billing/prices/task-schema?*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          model: metadata.model_name,
          plugin_key: '',
          usage_schema: null,
          manifest_schema: null,
          review_required: false,
          plugins: [],
        },
      },
    })
  )
}

for (const theme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    test(`scoped prices and frozen evidence ${theme} ${width}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: width === 390 ? 844 : 1000 })
      await configureStablePage(page, { theme })
      await metadataRoutes(page)
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      let versions = [globalPrice, channelPrice, flatPrice]
      const published: Array<{
        price: PriceVersion
        expected_version: number
      }> = []
      await page.route('**/api/billing/prices', async (route) => {
        if (route.request().method() === 'POST') {
          const input = route.request().postDataJSON()
          published.push(input)
          const saved = {
            ...input.price,
            id: 200 + published.length,
            hash: `published-${published.length}`,
            created_at: 1785114000,
          }
          versions = versions.filter(
            (price) => (price.channel_id ?? 0) !== saved.channel_id
          )
          versions.push(saved)
          await route.fulfill({ json: { success: true, data: saved } })
          return
        }
        await route.fulfill({ json: { success: true, data: versions } })
      })
      const previews: Array<{ price: PriceVersion; channel_ratio: string }> = []
      await page.route('**/api/billing/prices/preview', async (route) => {
        previews.push(route.request().postDataJSON())
        await route.fulfill({
          json: {
            success: true,
            data: { quote: { hash: 'preview-hash' }, charge },
          },
        })
      })
      await page.goto('/models/metadata')
      await page
        .getByRole('button', { name: '编辑模型', exact: true })
        .filter({ visible: true })
        .first()
        .click()
      const drawer = page.getByRole('dialog')
      const scope = drawer.locator('[data-price-scope]')
      await expect(scope).toHaveValue('global_model')
      await scope.selectOption('channel_model')
      await scope.focus()
      await page.keyboard.press('Tab')
      const channel = drawer.locator('[data-price-channel]')
      await expect(channel).toBeFocused()
      await channel.fill('9')
      await channel.press('Tab')
      const structure = drawer.locator('[data-price-structure]')
      await expect
        .poll(async () => JSON.parse(await structure.inputValue()))
        .toEqual(channelPrice.prices)
      await expect(drawer.locator('[data-price-effective-from]')).toHaveValue(
        channelPrice.effective_from!
      )
      await expect(drawer.locator('[data-price-effective-to]')).toHaveValue(
        channelPrice.effective_to!
      )
      await drawer.locator('[data-price-flat-fee]').fill('0.125000000000000001')
      await structure.fill('[{"quantity":')
      await drawer.getByRole('button', { name: '保存', exact: true }).click()
      await expect(drawer.getByRole('alert')).toContainText('请填写有效单价')
      expect(published).toHaveLength(0)
      await structure.fill(JSON.stringify(channelPrice.prices, null, 2))
      await drawer.locator('summary').filter({ hasText: '价格预览' }).click()
      await drawer.locator('[data-price-preview]').click()
      await expect.poll(() => previews.length).toBe(1)
      expect(previews[0]).toMatchObject({
        channel_ratio: '1',
        price: {
          channel_id: 9,
          scope_kind: 'channel_model',
          prices: channelPrice.prices,
        },
      })
      await expect(
        drawer.getByText('$2.125', { exact: false }).first()
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('channel-price-editor.png'),
        fullPage: true,
      })
      await drawer.getByRole('button', { name: '保存', exact: true }).click()
      await expect.poll(() => published.length).toBe(1)
      expect(published[0]).toMatchObject({
        expected_version: 109,
        price: {
          scope_kind: 'channel_model',
          channel_id: 9,
          flat_fee: '0.125000000000000001',
          prices: channelPrice.prices,
          effective_from: channelPrice.effective_from,
          effective_to: channelPrice.effective_to,
        },
      })
      await expect(drawer).not.toBeVisible()

      await page
        .getByRole('button', { name: '编辑模型', exact: true })
        .filter({ visible: true })
        .first()
        .click()
      await scope.selectOption('channel_model')
      await channel.fill('10')
      await channel.press('Tab')
      await drawer.locator('[data-price-flat-fee]').fill('0.375')
      await drawer.getByRole('button', { name: '保存', exact: true }).click()
      await expect.poll(() => published.length).toBe(2)
      expect(published[1]).toMatchObject({
        expected_version: 110,
        price: { channel_id: 10, prices: [], flat_fee: '0.375' },
      })

      await page.route('**/api/pricing', (route) => {
        const visible = versions.filter(
          (price) => price.scope_kind === 'channel_model'
        )
        return route.fulfill({
          json: {
            success: true,
            data: [
              {
                model_name: metadata.model_name,
                owner_by: 'OpenAI',
                vendor_id: 0,
                supported_endpoint_types: ['openai'],
                price_version: visible[0],
                price_versions: visible,
              },
            ],
            vendors: [],
            supported_endpoint: {
              openai: { path: '/v1/chat/completions', method: 'POST' },
            },
          },
        })
      })
      await page.goto('/pricing')
      await expect(
        page.getByRole('link', { name: metadata.model_name, exact: true })
      ).toHaveCount(2)
      await expect(
        page.locator('tbody tr').filter({ hasText: 'channel_model · 9' })
      ).toHaveCount(1)
      await expect(
        page.locator('tbody tr').filter({ hasText: 'channel_model · 10' })
      ).toHaveCount(1)
      await expect(page.getByText('$0.375', { exact: true })).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('channel-only-public-list.png'),
        fullPage: true,
      })
      await page
        .getByRole('link', { name: metadata.model_name, exact: true })
        .first()
        .click()
      await expect(page.locator('[data-price-version-evidence]')).toContainText(
        'channel_model'
      )
      await expect(page.locator('[data-price-version-evidence]')).toContainText(
        'published-1'
      )
      const versionPicker = page.getByRole('combobox', {
        name: '价格作用域',
        exact: true,
      })
      await versionPicker.focus()
      await versionPicker.press('Enter')
      await versionPicker.press('End')
      await versionPicker.press('Enter')
      await expect(page.locator('[data-price-version-evidence]')).toContainText(
        'published-2'
      )
      await assertNoHorizontalOverflow(page)

      await page.route('**/api/log/self?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              page: 1,
              page_size: 20,
              total: 1,
              items: [
                {
                  id: 1,
                  type: 2,
                  token_name: 'scope-fixture',
                  model_name: metadata.model_name,
                  channel_name: 'channel-nine',
                  prompt_tokens: 10,
                  completion_tokens: 5,
                  quota: charge.final_quota,
                  use_time: 1000,
                  is_stream: false,
                  content: '',
                  created_at: 1785114000,
                  other: {
                    price_scope: 'channel_model',
                    price_channel_id: 9,
                    price_version_id: 109,
                    price_version_hash: 'frozen-version-before-publication',
                    quote_hash: 'frozen-attempt-quote',
                    charge_result: charge,
                  },
                },
              ],
            },
          },
        })
      )
      await page.route('**/api/log/self/groups?*', (route) =>
        route.fulfill({ json: { success: true, data: [] } })
      )
      await page.goto('/logs')
      await page
        .getByRole('button', { name: '查看日志详情', exact: true })
        .click()
      const evidence = page.locator('[data-log-billing-detail]')
      await expect(evidence).toContainText('channel_model · 9')
      await expect(evidence).toContainText('frozen-attempt-quote')
      await expect(evidence).toContainText('frozen-version-before-publication')
      await expect(evidence).toContainText('1062500 quota')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('frozen-log-evidence.png'),
        fullPage: true,
      })
      await page.keyboard.press('Escape')
      await expect(evidence).not.toBeVisible()
      expect(errors).toEqual([])
    })
  }
}
