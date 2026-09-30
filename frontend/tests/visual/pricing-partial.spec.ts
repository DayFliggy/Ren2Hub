import { expect, test } from '@playwright/test'
import { configureStablePage, assertNoHorizontalOverflow } from './fixtures'
for (const theme of ['light', 'dark'] as const)
  for (const width of [1440, 390]) {
    test(`upstream public prices ${theme} ${width}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme })
      await page.addInitScript(() =>
        localStorage.setItem('ren2hub_locale', 'en')
      )
      await page.route('**/api/user/models', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: ['priced-model', 'expression-model', 'invalid-model'],
          },
        })
      )
      await page.route('**/api/pricing', (route) =>
        route.fulfill({
          json: {
            success: true,
            supported_endpoint: {
              openai: { path: '/v1/chat/completions', method: 'POST' },
            },
            vendors: [],
            data: [
              {
                model_name: 'priced-model',
                quota_type: 0,
                model_ratio: 1,
                completion_ratio: 2,
                cache_ratio: 0,
                create_cache_ratio: 0.5,
                owner_by: 'OpenAI',
                supported_endpoint_types: ['openai'],
              },
              {
                model_name: 'expression-model',
                quota_type: 0,
                billing_mode: 'tiered_expr',
                billing_expr: 'tier("base", p * 0.000123456789)',
                owner_by: 'OpenAI',
              },
              { model_name: 'invalid-model' },
            ],
          },
        })
      )
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      for (const path of ['/pricing', '/models']) {
        await page.goto(path)
        await expect(
          page.getByText('priced-model', { exact: true }).first()
        ).toBeVisible()
        await expect(
          page.getByText('$2.000 / 1M tokens', { exact: true }).first()
        ).toBeVisible()
        await expect(
          page
            .locator('pre')
            .filter({ hasText: 'tier("base", p * 0.000123456789)' })
            .first()
        ).toBeVisible()
        await assertNoHorizontalOverflow(page)
      }
      await page.screenshot({
        path: testInfo.outputPath('public-prices.png'),
        fullPage: true,
      })
      expect(errors).toEqual([])
    })
  }
