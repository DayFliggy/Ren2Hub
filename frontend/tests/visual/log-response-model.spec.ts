import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`response model evidence ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      const groups: string[] = []
      await page.route('**/api/log/self?*', (route) => {
        groups.push(
          new URL(route.request().url()).searchParams.get('group') || ''
        )
        return route.fulfill({
          json: {
            success: true,
            data: {
              items: [
                {
                  id: 501,
                  type: 2,
                  token_id: 7,
                  token_name: 'fixture',
                  model_name: 'customer-alias',
                  channel_name: 'fixture-channel',
                  prompt_tokens: 12,
                  completion_tokens: 5,
                  quota: 100,
                  use_time: 1,
                  is_stream: false,
                  content: 'Response evidence fixture',
                  created_at: 1785123000,
                  other: JSON.stringify({
                    upstream_model_name: 'expected-provider-model',
                    response_model_name: 'unexpected-actual-model',
                  }),
                },
              ],
              total: 1,
              page: 1,
              page_size: 20,
            },
          },
        })
      })
      await Promise.all([
        page.waitForResponse('**/api/log/self?*'),
        page.goto('/logs'),
      ])
      await expect.poll(() => groups.length).toBeGreaterThan(0)
      expect(groups.every((group) => group === '')).toBe(true)
      await expect(
        page.getByRole('combobox', { name: '日志分组' })
      ).toHaveCount(0)
      const mismatch = page.locator('[data-log-model-mismatch]:visible')
      await expect(mismatch).toHaveText('响应模型不一致')
      await page
        .getByRole('button', { name: '查看日志详情', exact: true })
        .click()
      const dialog = page.getByRole('dialog')
      await expect(
        dialog.getByText('customer-alias', { exact: true })
      ).toBeVisible()
      const evidence = dialog.locator('pre')
      await expect(evidence).toBeVisible()
      expect(JSON.parse(await evidence.innerText())).toEqual({
        upstream_model_name: 'expected-provider-model',
        response_model_name: 'unexpected-actual-model',
      })
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('response-model.png'),
        fullPage: true,
      })
    })
  }
}
