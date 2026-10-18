import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow } from './fixtures'
import { nativeCoreFixture } from './native-core-fixture'

for (const theme of ['light', 'dark'])
  for (const width of [1440, 390]) {
    test(`GMPay order currency and manual completion ${theme} ${width}`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: 900 })
      const fixture = await nativeCoreFixture(page, theme)
      let completed = false,
        fail = true
      const writes: unknown[] = []
      const order = () => ({
        id: 1,
        user_id: 1,
        amount: 10,
        money: 73,
        currency: 'CNY',
        pay_amount: '73.00',
        expected_quota: 5000000,
        credited_quota: completed ? 5000000 : 0,
        affiliate_base_quota: 5000000,
        trade_no: 'GP-native',
        payment_method: 'gmpay:usdt.tron',
        payment_provider: 'gmpay',
        status: completed ? 'success' : 'pending',
        create_time: 1785114000,
        complete_time: completed ? 1785114100 : 0,
        username: 'native.root',
        email: 'root@example.test',
        gmpay: null,
      })
      await page.route('**/api/user/topup?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: { page: 1, page_size: 20, total: 1, items: [order()] },
          },
        })
      )
      await page.route('**/api/user/topup/admin/detail/GP-native', (route) =>
        route.fulfill({ json: { success: true, data: order() } })
      )
      await page.route('**/api/user/topup/complete', (route) => {
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
      await page.getByRole('tab', { name: 'Orders', exact: true }).click()
      await expect(
        page.getByText('GP-native', { exact: true }).filter({ visible: true })
      ).toBeVisible()
      await page
        .getByRole('button', { name: 'View order', exact: true })
        .filter({ visible: true })
        .first()
        .click()
      const detail = page.getByRole('dialog', {
        name: 'Order detail',
        exact: true,
      })
      await expect(detail).toContainText('CNY')
      await expect(detail).toContainText('73.00')
      await expect(detail).toContainText('gmpay:usdt.tron')
      await detail
        .getByRole('button', { name: 'Complete manually', exact: true })
        .click()
      const confirmation = page
        .getByRole('dialog')
        .filter({ hasText: 'Confirm payment was received' })
      expect(writes).toEqual([])
      await confirmation
        .getByRole('button', { name: 'Confirm', exact: true })
        .click()
      await expect(confirmation.getByRole('alert')).toContainText(
        'Completion unavailable'
      )
      await expect(confirmation).toBeVisible()
      fail = false
      await confirmation
        .getByRole('button', { name: 'Confirm', exact: true })
        .click()
      await expect(confirmation).toHaveCount(0)
      await expect(detail).toContainText('Completed')
      await expect(
        detail.getByRole('button', { name: 'Complete manually' })
      ).toHaveCount(0)
      expect(writes).toEqual([
        { trade_no: 'GP-native' },
        { trade_no: 'GP-native' },
      ])
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: info.outputPath('gmpay-order.png'),
        fullPage: true,
      })
      expect(fixture.errors).toEqual([])
    })
  }
