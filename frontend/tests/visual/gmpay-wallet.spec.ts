import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow } from './fixtures'
import { nativeCoreFixture } from './native-core-fixture'

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`GMPay native checkout blocks duplicate requests ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      const fixture = await nativeCoreFixture(page, theme)
      const writes: unknown[] = []
      let release!: () => void
      const pending = new Promise<void>((resolve) => {
        release = resolve
      })
      await page.route('**/api/user/amount', (route) => {
        expect(route.request().postDataJSON()).toEqual({
          amount: 10,
          payment_method: 'gmpay:usdt.tron',
        })
        return route.fulfill({ json: { message: 'success', data: '73.00' } })
      })
      await page.route('**/api/user/gmpay/pay', async (route) => {
        const body = route.request().postDataJSON()
        writes.push(body)
        expect(body).toEqual({ amount: 10, payment_method: 'gmpay:usdt.tron' })
        await pending
        await route.fulfill({
          json: {
            message: 'success',
            url: `${new URL(route.request().url()).origin}/payment-fixture`,
            data: {
              trade_no: 'GP-native',
              payment_method: 'gmpay:usdt.tron',
              currency: 'CNY',
              pay_amount: '73.00',
              expected_quota: 5000000,
              token: 'usdt',
              network: 'tron',
            },
          },
        })
      })
      await page.goto('/wallet')
      await page
        .getByRole('combobox', { name: 'Payment method', exact: true })
        .selectOption('gmpay:usdt.tron')
      await page.getByRole('button', { name: 'Calculate amount due' }).click()
      await expect(
        page.getByText('Amount due: 73.00', { exact: true })
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('gmpay-native-wallet.png'),
        fullPage: true,
      })
      const pay = page.getByRole('button', { name: 'Continue to checkout' })
      await pay.click()
      await expect.poll(() => writes.length).toBe(1)
      await expect(pay).toBeDisabled()
      await expect(
        page.getByRole('combobox', { name: 'Payment method', exact: true })
      ).toBeDisabled()
      await expect(
        page.getByRole('spinbutton', { name: 'Top-up quantity', exact: true })
      ).toBeDisabled()
      release()
      await expect(
        page.getByRole('heading', { name: 'Local checkout' })
      ).toBeVisible()
      expect(writes).toHaveLength(1)
      expect(fixture.unexpected).toEqual([])
      expect(fixture.errors).toEqual([])
    })
  }
}

test('wallet reports unified top-up info failure and recovers after reload', async ({
  page,
}) => {
  const fixture = await nativeCoreFixture(page, 'light')
  let fail = true
  await page.route('**/api/user/topup/info', (route) =>
    fail
      ? route.fulfill({
          json: {
            success: false,
            message: 'Payment configuration unavailable',
          },
        })
      : route.fallback()
  )
  await page.goto('/wallet')
  await expect(page.getByRole('alert')).toContainText(
    'Payment configuration unavailable'
  )
  await expect(
    page.getByRole('button', { name: 'Continue to checkout' })
  ).toBeDisabled()
  fail = false
  await page.reload()
  await page
    .getByRole('combobox', { name: 'Payment method', exact: true })
    .selectOption('gmpay:usdt.tron')
  await expect(
    page.getByRole('button', { name: 'Continue to checkout' })
  ).toBeEnabled()
  expect(fixture.unexpected).toEqual([])
  expect(fixture.errors).toEqual([])
})
