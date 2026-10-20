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
      await page.route('**/api/log/self/groups?*', (route) =>
        route.fulfill({
          json: { success: true, data: ['legacy-team', 'auto'] },
        })
      )
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
                  token_name: 'fixture',
                  model_name: 'customer-alias',
                  channel_name: 'fixture-channel',
                  prompt_tokens: 12,
                  completion_tokens: 5,
                  quota: '100',
                  use_time: 1000,
                  is_stream: false,
                  content: 'Response evidence fixture',
                  created_at: 1785123000,
                  other: {
                    upstream_model_name: 'expected-provider-model',
                    response_model_name: 'unexpected-actual-model',
                  },
                },
              ],
              total: 1,
              page: 1,
              page_size: 20,
            },
          },
        })
      })
      await page.goto('/logs')
      await page
        .getByRole('combobox', { name: '日志分组' })
        .fill('historical-custom')
      await expect.poll(() => groups.at(-1)).toBe('historical-custom')
      await expect(page.locator('datalist option[value=auto]')).toHaveCount(0)
      const mismatch = page.locator('[data-log-model-mismatch]:visible')
      await expect(mismatch).toHaveText('响应模型不一致')
      await page
        .getByRole('button', { name: '查看日志详情', exact: true })
        .click()
      const dialog = page.getByRole('dialog')
      await expect(
        dialog.getByText('unexpected-actual-model', { exact: true })
      ).toBeVisible()
      await expect(
        dialog.getByText('expected-provider-model', { exact: true })
      ).toBeVisible()
      await expect(dialog.locator('[data-log-model-mismatch]')).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('response-model.png'),
        fullPage: true,
      })
    })
  }
}
