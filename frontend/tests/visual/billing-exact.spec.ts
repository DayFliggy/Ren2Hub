import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`exact token budget ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      await page.route(/\/api\/gateway\/self(?:\/runtime)?$/, (route) =>
        route.fulfill({
          status: 503,
          json: { success: false, message: 'gateway unavailable' },
        })
      )
      const token = {
        id: 7,
        name: 'Exact budget',
        key: 'sk-masked',
        type: 'auto',
        status: 1,
        used_quota: '3',
        available_quota: '9007199254740993',
        reserved_quota: '1',
        quota_version: 7,
        unlimited_quota: false,
        model_limits_enabled: false,
        model_limits: '',
        allow_ips: '',
        expired_time: -1,
        created_time: 1785114000,
      }
      await page.route('**/api/token/?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: { page: 1, page_size: 20, total: 1, items: [token] },
          },
        })
      )
      const writes: unknown[] = []
      await page.route('**/api/token/', (route) => {
        writes.push(route.request().postDataJSON())
        return route.fulfill({
          status: 409,
          json: { success: false, message: 'version conflict' },
        })
      })
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      await page.goto('/keys')
      await expect(
        page
          .getByText('$18,014,398,509.482', { exact: true })
          .filter({ visible: true })
      ).toBeVisible()
      await page
        .getByRole('button', { name: '编辑令牌', exact: true })
        .filter({ visible: true })
        .click()
      const dialog = page.getByRole('dialog')
      await expect(dialog.locator('input[name=token-quota]')).toHaveValue(
        '18014398509.481986'
      )
      await dialog.locator('input[name=token-quota]').fill('18014398509.481988')
      await dialog.getByRole('button', { name: '确认', exact: true }).click()
      await expect.poll(() => writes.length).toBe(1)
      await dialog.getByRole('button', { name: '确认', exact: true }).click()
      await expect.poll(() => writes.length).toBe(2)
      expect(writes[1]).toEqual(writes[0])
      expect(writes[0]).toMatchObject({
        available_quota: '9007199254740994',
        expected_version: 7,
        unlimited_quota: false,
      })
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('exact-token-budget.png'),
        fullPage: true,
      })
      expect(errors).toEqual([])
    })

    test(`public unit prices ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme, authenticated: false })
      const version = {
        id: 3,
        model: 'media-exact',
        mode: 'unit_price',
        prices: [
          {
            quantity: 'p',
            unit: 'per_million_tokens',
            price: '1.000000000000000001',
          },
          { quantity: 'images', unit: 'per_image', price: '0.125' },
          { quantity: 'cc1h', unit: 'per_million_tokens', price: '2.5' },
        ],
        created_at: 1785114000,
        hash: 'fixture',
      }
      await page.route('**/api/pricing', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: [
              {
                model_name: 'media-exact',
                description: '',
                icon: '',
                tags: '',
                vendor_id: 1,
                owner_by: 'OpenAI',
                supported_endpoint_types: ['openai'],
                billing_mode: 'unit_price',
                price_version: version,
              },
            ],
            vendors: [{ id: 1, name: 'OpenAI' }],
            supported_endpoint: {
              openai: { path: '/v1/chat/completions', method: 'POST' },
            },
          },
        })
      )
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      await page.goto('/pricing?tokenUnit=K')
      await expect(page.getByText('$0.001', { exact: true })).toBeVisible()
      await expect(page.getByText('$0.125', { exact: true })).toBeVisible()
      await expect(page.getByText('每张图片', { exact: true })).toBeVisible()
      await page.getByRole('link', { name: 'media-exact', exact: true }).click()
      await expect(
        page.getByText('缓存写入（1h）', { exact: true })
      ).toBeVisible()
      await expect(
        page.getByText('每千 Token', { exact: true }).first()
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('public-unit-prices.png'),
        fullPage: true,
      })
      expect(errors).toEqual([])
    })
  }
}
