import { expect, test } from '@playwright/test'
import { pricingOfferFixture } from './pricingFixtures'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    test(`partial audio price catalog ${theme} ${width}`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme })
      const offer = pricingOfferFixture({
        id: 1,
        model: 'audio-model',
        mode: 'unit_price',
        created_at: 1,
        hash: 'audio-cache',
        prices: [
          { quantity: 'ai_cr', unit: 'per_million_tokens', price: '0.4' },
        ],
      })
      await page.route('**/api/user/models', (route) =>
        route.fulfill({
          json: { success: true, data: ['audio-alias', 'unsupported-model'] },
        })
      )
      await page.route('**/api/pricing', (route) =>
        route.fulfill({
          json: {
            success: true,
            supported_endpoint: {},
            vendors: [],
            data: [
              {
                model_name: 'audio-alias',
                owner_by: 'OpenAI',
                supported_endpoint_types: ['chat'],
                offers: [offer],
              },
              {
                model_name: 'unsupported-model',
                offers: [
                  {
                    ...offer,
                    price_version: {
                      ...offer.price_version,
                      calculation_version: 'future',
                    },
                  },
                ],
              },
            ],
          },
        })
      )
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      for (const path of ['/pricing', '/models']) {
        await page.goto(path)
        const issues = page.locator('[data-pricing-issues]')
        await expect(issues).toBeVisible()
        await issues.locator('summary').click()
        await expect(issues).toContainText('unsupported-model')
        await expect(issues).toContainText('报价合同不受支持或数据不完整')
        await expect(
          page.getByText('audio-alias', { exact: true }).first()
        ).toBeVisible()
        await expect(
          page.getByText('$0.400').filter({ visible: true }).first()
        ).toBeVisible()
        await assertNoHorizontalOverflow(page)
      }
      expect(errors).toEqual([])
    })
  }
}
