import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`GMPay order evidence ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      const order = {
        id: 1,
        order_no: 'GPvisual',
        user_id: 1,
        username: 'visual',
        email: '',
        amount: 10,
        quota: '0',
        type: 'topup',
        currency: 'USD',
        method: 'gmpay',
        payment_method: 'gmpay:usdt.tron',
        status: 'pending',
        created: 1785114000,
        paid_at: 0,
      }
      await page.route(/\/api\/next\/admin\/orders(?:\?.*)?$/, (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              items: [order],
              total: 1,
              page: 1,
              page_size: 20,
              status_counts: { pending: 1 },
              method_counts: { gmpay: 1 },
              type_counts: { topup: 1 },
              filtered_epay_revenue: 0,
            },
          },
        })
      )
      let fail = true
      let settled = false
      await page.route('**/api/next/admin/orders/GPvisual', (route) => {
        expect(route.request().method()).toBe('GET')
        if (fail)
          return route.fulfill({
            json: { success: false, message: 'test unavailable' },
          })
        return route.fulfill({
          json: {
            success: true,
            data: {
              ...order,
              status: settled ? 'completed' : 'pending',
              quota: settled ? '5000000' : '0',
              paid_at: settled ? 1785114100 : 0,
              gmpay: {
                status: settled ? 'settled' : 'unknown',
                last_failure: settled ? '' : 'TRANSPORT',
                token: 'usdt',
                network: 'tron',
                gateway_trade_no: settled ? 'gateway-visual' : '',
                gateway_crypto_amount: '10.1',
                receive_address: 'original-tron-address',
                settled_crypto_amount: settled ? '9.9' : '',
                settled_receive_address: settled ? `0x${'b'.repeat(40)}` : '',
                settled_transaction_id: settled ? `0x${'a'.repeat(64)}` : '',
              },
            },
          },
        })
      })
      await page.goto('/orders')
      await page.getByRole('tab', { name: '订单列表', exact: true }).click()
      await page
        .getByRole('button', { name: '查看订单', exact: true })
        .filter({ visible: true })
        .click()
      const dialog = page.getByRole('dialog')
      await expect(dialog.getByRole('alert')).toHaveText(
        '无法读取网关订单证据。'
      )
      fail = false
      await dialog
        .getByRole('button', { name: '重新读取证据', exact: true })
        .click()
      await expect(
        dialog.getByText('创建结果未确定', { exact: true })
      ).toBeVisible()
      await expect(dialog.getByText('TRANSPORT', { exact: true })).toBeVisible()
      await dialog.screenshot({
        path: testInfo.outputPath('gmpay-order-unknown.png'),
      })
      await page.keyboard.press('Escape')
      await expect(dialog).toHaveCount(0)
      settled = true
      await page
        .getByRole('button', { name: '查看订单', exact: true })
        .filter({ visible: true })
        .click()
      await expect(dialog.getByText('已结算', { exact: true })).toBeVisible()
      await expect(dialog.getByText('已完成', { exact: true })).toBeVisible()
      await expect(dialog.getByText('待支付', { exact: true })).toHaveCount(0)
      await expect(
        dialog.getByText('请求网络（未验证实际网络）', { exact: true })
      ).toBeVisible()
      const receipt = dialog.getByText(`0x${'a'.repeat(64)}`, { exact: true })
      await receipt.scrollIntoViewIfNeeded()
      await expect(receipt).toBeVisible()
      await expect(dialog.getByText('9.9', { exact: true })).toBeVisible()
      await assertNoHorizontalOverflow(page)
      const fits = await dialog.evaluate(
        (element) => element.scrollWidth <= element.clientWidth + 1
      )
      expect(fits).toBe(true)
      await dialog.screenshot({
        path: testInfo.outputPath('gmpay-order-settled.png'),
      })
      await page.keyboard.press('Escape')
      await expect(dialog).toHaveCount(0)
      expect(errors).toEqual([])
    })
  }
}
