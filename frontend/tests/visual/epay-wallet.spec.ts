import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow } from './fixtures'
import { nativeCoreFixture } from './native-core-fixture'

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`EPay native amount, checkout and records ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      const fixture = await nativeCoreFixture(page, theme)
      let failAmount = true
      let completed = false
      const payments: unknown[] = []
      await page.route('**/api/user/amount', (route) => {
        expect(route.request().postDataJSON()).toEqual({
          amount: 10,
          payment_method: 'alipay',
        })
        return route.fulfill({
          json: failAmount
            ? { message: 'Amount unavailable' }
            : { message: 'success', data: '73.00' },
        })
      })
      await page.route('**/api/user/topup/self?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              page: 1,
              page_size: 20,
              total: 1,
              items: [
                {
                  id: 1,
                  user_id: 1,
                  amount: 10,
                  money: 73,
                  currency: 'CNY',
                  pay_amount: '73.00',
                  expected_quota: 5000000,
                  credited_quota: completed ? 5000000 : 0,
                  affiliate_base_quota: 5000000,
                  trade_no: 'epay-native',
                  payment_provider: 'epay',
                  payment_method: 'alipay',
                  status: completed ? 'success' : 'pending',
                  create_time: 1785114000,
                  complete_time: completed ? 1785114100 : 0,
                },
              ],
            },
          },
        })
      )
      await page.route('**/api/user/pay', (route) => {
        const body = route.request().postDataJSON()
        payments.push(body)
        expect(body).toEqual({ amount: 10, payment_method: 'alipay' })
        return route.fulfill({
          json: {
            message: 'success',
            url: `${new URL(route.request().url()).origin}/payment-fixture`,
            data: {
              pid: 'local-merchant',
              out_trade_no: 'epay-native',
              money: '73.00',
              type: 'alipay',
              sign: 'local-signature',
            },
          },
        })
      })
      await page.route('**/payment-fixture', (route) => {
        expect(route.request().method()).toBe('POST')
        expect(
          Object.fromEntries(new URLSearchParams(route.request().postData()!))
        ).toEqual({
          pid: 'local-merchant',
          out_trade_no: 'epay-native',
          money: '73.00',
          type: 'alipay',
          sign: 'local-signature',
        })
        return route.fulfill({
          contentType: 'text/html',
          body: '<h1>Local EPay checkout</h1>',
        })
      })
      await page.goto('/wallet')
      await page
        .getByRole('combobox', { name: 'Payment method', exact: true })
        .selectOption('alipay')
      await expect(
        page.getByRole('spinbutton', { name: 'Top-up quantity', exact: true })
      ).toHaveValue('10')
      await page
        .getByRole('button', { name: 'Calculate amount due', exact: true })
        .click()
      await expect(page.getByRole('alert')).toContainText('Amount unavailable')
      expect(payments).toEqual([])
      failAmount = false
      await page
        .getByRole('button', { name: 'Calculate amount due', exact: true })
        .click()
      await expect(
        page.getByText('Amount due: 73.00', { exact: true })
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('epay-native-wallet.png'),
        fullPage: true,
      })
      await page
        .getByRole('button', { name: 'Continue to checkout', exact: true })
        .click()
      await expect(
        page.getByRole('heading', { name: 'Local EPay checkout' })
      ).toBeVisible()
      expect(payments).toHaveLength(1)
      // The record refresh consumes the native server status; no browser callback
      // or client-side balance mutation is treated as payment confirmation.
      completed = true
      await page.goto('/wallet')
      const row = page.getByRole('listitem').filter({ hasText: 'epay-native' })
      await expect(row).toContainText('Completed')
      await expect(row).toContainText('73')
      await expect(
        page.getByRole('button', { name: 'Complete manually' })
      ).toHaveCount(0)
      expect(fixture.unexpected).toEqual([])
      expect(fixture.errors).toEqual([])
    })
  }
}
