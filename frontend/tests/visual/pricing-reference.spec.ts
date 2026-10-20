import { expect, test } from '@playwright/test'
import { configureStablePage, assertNoHorizontalOverflow } from './fixtures'
import { createModelMarketFixture } from '../fixtures/modelMarket'

for (const theme of ['light', 'dark'] as const)
  for (const width of [1440, 390]) {
    test(`reference sorting and labels ${theme} ${width}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme })
      await page.addInitScript(() =>
        localStorage.setItem('ren2hub_locale', 'en')
      )
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      await page.route('**/api/pricing', (route) =>
        route.fulfill({
          json: {
            success: true,
            vendors: [],
            supported_endpoint: {},
            data: [
              {
                model_name: 'native',
                quota_type: 0,
                model_ratio: 0.4,
                completion_ratio: 2,
                billing_mode: 'ratio',
                pricing_values: { ModelRatio: '0.4', CompletionRatio: '2' },
              },
              {
                model_name: 'tier',
                quota_type: 0,
                billing_mode: 'tiered_expr',
                billing_expr:
                  'len < 1000 ? tier("short", p * 0.1 + c * 4) : tier("long", p * 9 + c * 12)',
                pricing_values: {},
              },
              {
                model_name: 'task',
                quota_type: 0,
                billing_mode: 'tiered_expr',
                billing_expr: 'u("seconds") * 0.1',
                billing_usage_schema: {
                  seconds: { type: 'number', unit: 'second' },
                },
                usage_reference: {
                  status: 'available',
                  amount_usd: '0.5',
                  label: 'first 5s',
                  facts: { seconds: '5' },
                  plugin_name: 'Task Provider',
                },
                billing_plugin_variants: [
                  {
                    plugin_key: 'alpha',
                    plugin_name: 'Task Provider',
                    billing_mode: 'tiered_expr',
                    billing_expr: 'u("seconds") * 0.1',
                    usage_reference: {
                      status: 'available',
                      amount_usd: '0.5',
                      label: 'first 5s',
                      facts: { seconds: '5' },
                    },
                  },
                  {
                    plugin_key: 'beta',
                    plugin_name: 'Other Provider',
                    billing_mode: 'tiered_expr',
                    billing_expr: 'u("seconds") * 0.1',
                    usage_reference: {
                      status: 'available',
                      amount_usd: '0.2',
                      label: 'second 2s',
                      facts: { seconds: '2' },
                    },
                  },
                ],
              },
              {
                model_name: 'context',
                quota_type: 0,
                billing_mode: 'tiered_expr',
                billing_expr: 'p * param("price")',
              },
            ],
          },
        })
      )
      await page.goto('/pricing')
      const sort = page.getByRole('combobox', {
        name: 'Reference price order',
        exact: true,
      })
      await expect(page.locator('.public-pricing-card h2')).toHaveText([
        'native',
        'tier',
        'task',
        'context',
      ])
      const choices = [
        'Input reference · low to high',
        'Input reference · high to low',
        'Output reference · low to high',
        'Output reference · high to low',
        'Request reference · low to high',
        'Request reference · high to low',
      ]
      for (const choice of choices) {
        await sort.click()
        await page.getByRole('option', { name: choice, exact: true }).click()
        if (choice.startsWith('Input'))
          await expect(page.locator('.public-pricing-card h2')).toHaveText(
            choice.endsWith('low to high')
              ? ['tier', 'native', 'task', 'context']
              : ['native', 'tier', 'task', 'context']
          )
        if (choice.startsWith('Request'))
          await expect(
            page.locator('.public-pricing-card h2').first()
          ).toHaveText('task')
      }
      await expect(
        page.getByText('First-tier reference · short').first()
      ).toBeVisible()
      await expect(
        page.getByText('Conditions: len < 1000').first()
      ).toBeVisible()
      await expect(
        page
          .getByText('Example reference · Task Provider · first 5s')
          .filter({ visible: true })
          .first()
      ).toBeVisible()
      await expect(
        page
          .getByText('/ this example', { exact: true })
          .filter({ visible: true })
          .first()
      ).toBeVisible()
      await expect(
        page.getByText('Reference price unavailable', { exact: true })
      ).toBeVisible()
      await sort.focus()
      await page.keyboard.press('Enter')
      await page.keyboard.press('Home')
      await page.keyboard.press('Enter')
      await expect(page.locator('.public-pricing-card h2').first()).toHaveText(
        'native'
      )
      const taskCard = page.locator('.public-pricing-card').filter({
        has: page.getByRole('heading', { name: 'task', exact: true }),
      })
      await taskCard.getByText('Other Provider', { exact: true }).click()
      await expect(
        taskCard.getByText('Example reference · Other Provider · second 2s')
      ).toBeVisible()
      await expect(taskCard.locator('details').nth(1)).toContainText('$0.200')
      await expect(
        taskCard.locator('.model-price-items > [data-reference-prices]')
      ).toContainText('$0.500')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('public-reference.png'),
        fullPage: true,
      })

      const fixture = createModelMarketFixture()
      const baseline = fixture.models[0]!.baselines[0]!
      baseline.values = {
        'billing_setting.billing_mode': 'tiered_expr',
        'billing_setting.billing_expr': 'tier("first", p * 0.1 + c * 0.2)',
      }
      fixture.models.find(
        (model) => model.id === 'video-preview'
      )!.baselines[0]!.usage_reference = {
        status: 'available',
        amount_usd: '0.5',
        label: 'first 5s',
        facts: { seconds: '5' },
        plugin_name: 'Task Provider',
      }
      await page.route('**/api/user/model-market', (route) =>
        route.fulfill({ json: { success: true, data: fixture } })
      )
      await page.goto('/models')
      await expect(
        page.locator(
          '[data-model-card][data-model-name="gpt-4.1"] [data-channel-quote]'
        )
      ).toContainText('$0.020')
      const marketSort = page.getByRole('combobox', {
        name: 'Model ordering',
        exact: true,
      })
      await marketSort.click()
      await page
        .getByRole('option', {
          name: 'Input reference · low to high',
          exact: true,
        })
        .click()
      await expect(page.locator('[data-model-card]').first()).toHaveAttribute(
        'data-model-name',
        'gpt-4.1'
      )
      for (const view of ['Models', 'Channel shelves', 'Price comparison']) {
        await page.getByRole('radio', { name: view, exact: true }).click()
        await expect(
          page
            .getByText('Example reference · Task Provider · first 5s')
            .filter({ visible: true })
            .first()
        ).toBeVisible()
        await assertNoHorizontalOverflow(page)
      }
      await page.screenshot({
        path: testInfo.outputPath('market-reference.png'),
        fullPage: true,
      })
      expect(errors).toEqual([])
    })
  }
