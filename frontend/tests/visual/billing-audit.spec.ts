import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`mixed model prices ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      await page.route('**/api/user/models', (route) =>
        route.fulfill({ json: { success: true, data: ['mixed-model'] } })
      )
      await page.route('**/api/pricing', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: [
              {
                model_name: 'mixed-model',
                owner_by: 'OpenAI',
                supported_endpoint_types: ['chat'],
                price_version: {
                  id: 1,
                  model: 'mixed-model',
                  contract_version: 2,
                  calculation_version: 'billing-v2',
                  result_unit: 'usd',
                  mode: 'unit_price',
                  created_at: 1,
                  hash: 'fixture',
                  prices: [
                    { quantity: 'p', unit: 'per_million_tokens', price: '2' },
                    { quantity: 'calls', unit: 'per_call', price: '0.01' },
                    {
                      quantity: 'images',
                      unit: 'per_image',
                      price: '0.1256789',
                    },
                    {
                      quantity: 'cc1h',
                      unit: 'per_million_tokens',
                      price: '3',
                    },
                    {
                      quantity: 'tool:search',
                      unit: 'per_thousand_calls',
                      price: '5',
                    },
                  ],
                },
              },
            ],
          },
        })
      )
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      await page.goto('/models')
      const card = page.locator('[data-model-name="mixed-model"]')
      await expect(card.getByText('$2.000')).toBeVisible()
      await expect(card.getByText('$0.010')).toBeVisible()
      await expect(card.getByText('$0.126')).toBeVisible()
      await expect(card.getByText('缓存写入（1h）')).toBeVisible()
      await expect(card.getByText('每千次调用')).toBeVisible()
      await card.getByRole('button', { name: '详情', exact: true }).click()
      const dialog = page.getByRole('dialog')
      await expect(dialog.getByText('$0.126')).toBeVisible()
      await expect(dialog.getByText('每张图片')).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('mixed-prices.png'),
        fullPage: true,
      })
      expect(errors).toEqual([])
    })

    test(`int64 account projections ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, {
        theme,
        user: {
          available_quota: '9223372036854775807',
          used_quota: '9223372036854775807',
        },
      })
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      await page.goto('/invite')
      await expect(
        page.getByText('$18,446,744,073,709.55', { exact: true }).first()
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('int64-invite.png'),
        fullPage: true,
      })
      await page.goto('/lab/chat')
      await expect(page.locator('main')).toBeVisible()
      expect(errors).toEqual([])
    })
  }
}
