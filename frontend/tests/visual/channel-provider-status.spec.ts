import { expect, test } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  captureEvidence,
} from './fixtures'
for (const [theme, width] of [
  ['light', 1440],
  ['dark', 390],
] as const) {
  test(`provider status ${theme} ${width}`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 900 })
    await configureStablePage(page, { theme })
    const type = width === 390 ? 63 : 62
    const provider = type === 62 ? 'vllm' : 'sglang'
    await page.route(/\/api\/channel\/(?:\?.*)?$/, (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            items: [
              {
                id: 101,
                name: 'Provider Fixture',
                lab: { id: 'custom', name: 'Custom', source: 'vendor' },
                type,
                status: 1,
                priority: 0,
                weight: 1,
                in_flight: 0,
                max_concurrency: 20,
                channel_ratio: '1',

                used_quota: 0,
                balance: 0,
                response_time: 0,
                test_time: 0,
                models: 'fixture-model',
              },
            ],
            total: 1,
            page: 1,
            page_size: 20,
            type_counts: { [type]: 1 },
          },
        },
      })
    )
    await page.route(`**/api/channel/101/${provider}/status`, (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            sampled_at: 1780000000000,
            version: 'fixture-1.0',
            endpoints: {
              '/health': { status: 200 },
              '/metrics': { status: 503, error: 'http_error' },
            },
            models: [
              {
                id: 'fixture-model',
                root: 'fixture-model',
                max_model_len: 32768,
              },
            ],
            metrics: [],
            raw_metrics: '',
          },
        },
      })
    )
    await page.goto('/channels')
    await page.locator('[aria-label="渠道运行状态"]:visible').first().click()
    const dialog = page.getByRole('dialog', {
      name: 'Provider Fixture',
      exact: true,
    })
    await expect(dialog.getByText(/fixture-1.0/)).toBeVisible()
    await expect(dialog.getByText('/health', { exact: true })).toBeVisible()
    await expect(dialog.getByText(/503/)).toBeVisible()
    await expect(dialog.getByText('32768', { exact: true })).toBeVisible()
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: testInfo.outputPath('provider-status.png'),
      fullPage: true,
    })
  })
}
