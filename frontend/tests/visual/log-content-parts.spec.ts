import { expect, test } from '@playwright/test'

import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

// The rows use keys of the real zh-CN server message catalog (infra.json).
const ROWS = [
  {
    id: 601,
    type: 1,
    token_id: 3,
    token_name: 'wallet',
    model_name: '',
    channel_name: '',
    prompt_tokens: 0,
    completion_tokens: 0,
    quota: 5000000,
    use_time: 0,
    is_stream: false,
    content:
      'Online top-up succeeded, amount added: $10.000000, payment amount: 72.00',
    created_at: 1785123000,
    other: JSON.stringify({
      content_parts: [
        {
          key: 'Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}',
          params: { quota: '$10.000000', amount: '72.00' },
        },
      ],
    }),
  },
  {
    id: 602,
    type: 2,
    token_id: 0,
    token_name: 'Model test',
    model_name: 'gpt-4.1',
    channel_name: 'OpenAI Route 1',
    prompt_tokens: 12,
    completion_tokens: 5,
    quota: 100,
    use_time: 1,
    is_stream: false,
    content: 'Legacy channel test row',
    created_at: 1785122000,
    other: '{}',
  },
]

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`log content_parts ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      await page.route('**/api/log/self?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: { items: ROWS, total: ROWS.length, page: 1, page_size: 20 },
          },
        })
      )
      await Promise.all([
        page.waitForResponse('**/api/log/self?*'),
        page.goto('/logs'),
      ])

      const rows = page.locator(mobile ? 'article:visible' : 'tbody tr:visible')
      await expect(rows).toHaveCount(2)
      const topup = rows.nth(0).locator('[data-log-detail-trigger]')
      await expect(topup).toHaveText(
        '使用在线充值成功，充值金额: $10.000000，支付金额：72.00'
      )
      await expect(rows.nth(1)).toContainText('模型测试')
      // Rows written before content_parts keep their stored text.
      await expect(rows.nth(1).locator('[data-log-detail-trigger]')).toHaveText(
        'Legacy channel test row'
      )
      await expect(page.getByText('Online top-up succeeded')).toHaveCount(0)
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('log-content-parts.png'),
        fullPage: true,
      })
    })
  }
}
