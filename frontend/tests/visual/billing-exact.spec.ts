import { expect, test } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  captureEvidence,
} from './fixtures'

for (const [theme, mobile] of [
  ['light', false],
  ['dark', true],
] as const) {
  test(`exact token budget ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
    )
    await configureStablePage(page, { theme })
    const token = {
      id: 7,
      name: 'Exact budget',
      key: 'sk-masked',
      type: 'auto',
      selected_only: false,
      route_channel_count: 0,
      status: 1,
      used_quota: 3,
      remain_quota: 1234567,
      allow_user_quota_query: true,
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
    await page.route('**/api/token/7', (route) =>
      route.fulfill({ json: { success: true, data: token } })
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
      page.getByText('$2.469', { exact: true }).filter({ visible: true })
    ).toBeVisible()
    await page
      .getByRole('button', { name: '编辑令牌', exact: true })
      .filter({ visible: true })
      .click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.locator('input[name=token-quota]')).toHaveValue(
      '2.469134'
    )
    await dialog.locator('input[name=token-quota]').fill('2.469136')
    await dialog.getByRole('button', { name: '确认', exact: true }).click()
    await expect.poll(() => writes.length).toBe(1)
    await dialog.getByRole('button', { name: '确认', exact: true }).click()
    await expect.poll(() => writes.length).toBe(2)
    expect(writes[1]).toEqual(writes[0])
    expect(writes[0]).toMatchObject({
      remain_quota: 1234568,
      unlimited_quota: false,
    })
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: testInfo.outputPath('exact-token-budget.png'),
      fullPage: true,
    })
    expect(errors).toEqual([])
  })
}
