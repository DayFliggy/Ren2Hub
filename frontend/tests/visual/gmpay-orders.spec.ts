import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow } from './fixtures'
import { nativeCoreFixture } from './native-core-fixture'

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`GMPay native order manual completion ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      const fixture = await nativeCoreFixture(page, theme)
      let completed = false
      let fail = true
      const writes: unknown[] = []
      await page.route('**/api/user/topup?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              total: 1,
              items: [
                {
                  id: 1,
                  user_id: 1,
                  amount: 10,
                  money: 73,
                  trade_no: 'GP-native',
                  payment_method: 'gmpay:usdt.tron',
                  payment_provider: 'gmpay',
                  status: completed ? 'success' : 'pending',
                  create_time: 1785114000,
                  complete_time: completed ? 1785114100 : 0,
                },
              ],
            },
          },
        })
      )
      await page.route('**/api/user/topup/complete', (route) => {
        expect(route.request().method()).toBe('POST')
        const body = route.request().postDataJSON()
        writes.push(body)
        expect(body).toEqual({ trade_no: 'GP-native' })
        if (!fail) completed = true
        return route.fulfill({
          json: fail
            ? { success: false, message: 'Completion unavailable' }
            : { success: true, data: null },
        })
      })
      await page.goto('/orders')
      const row = page.getByRole('row').filter({ hasText: 'GP-native' })
      await expect(row).toContainText('Pending')
      await expect(row).toContainText('gmpay / gmpay:usdt.tron')
      await row.getByRole('button', { name: 'Complete manually' }).click()
      const dialog = page.getByRole('dialog')
      await expect(dialog).toContainText('Confirm payment was received')
      expect(writes).toEqual([])
      await dialog.getByRole('button', { name: 'Confirm', exact: true }).click()
      await expect(page.getByRole('alert')).toContainText(
        'Completion unavailable'
      )
      await expect(dialog).toBeVisible()
      await expect(row).toContainText('Pending')
      fail = false
      await dialog.getByRole('button', { name: 'Confirm', exact: true }).click()
      await expect(dialog).toHaveCount(0)
      await expect(row).toContainText('Completed')
      await expect(
        row.getByRole('button', { name: 'Complete manually' })
      ).toHaveCount(0)
      expect(writes).toEqual([
        { trade_no: 'GP-native' },
        { trade_no: 'GP-native' },
      ])
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('gmpay-native-order.png'),
        fullPage: true,
      })
      expect(fixture.unexpected).toEqual([])
      expect(fixture.errors).toEqual([])
    })
  }
}
