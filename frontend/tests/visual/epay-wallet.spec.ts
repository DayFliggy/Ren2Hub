import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`Epay quote and callback settlement ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      const user = { available_quota: '5201314', quota_version: 1 }
      await configureStablePage(page, { theme, user })
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      let failQuote = false
      let quoteVersion = 1
      let orderWrites = 0
      const unexpectedTopupWrites: string[] = []
      let completed = false
      const order = {
        id: 1,
        order_no: 'USR1NOpayment-test',
        user_id: 1,
        username: '',
        email: '',
        amount: 73,
        quota: '0',
        type: 'topup',
        currency: 'CNY',
        method: 'epay',
        payment_method: 'alipay',
        status: 'pending',
        created: 1785114000,
        paid_at: 0,
      }
      await page.route('**/api/next/wallet/topups?*', (route) => {
        expect(route.request().method()).toBe('GET')
        return route.fulfill({
          json: {
            success: true,
            data: {
              items: [
                {
                  ...order,
                  status: completed ? 'completed' : 'pending',
                  quota: completed ? '5000000' : '0',
                },
              ],
              total: 1,
              page: 1,
              page_size: 6,
            },
          },
        })
      })
      page.on('request', (request) => {
        if (
          request.url().includes('/api/next/wallet/topups') &&
          request.method() !== 'GET'
        ) {
          unexpectedTopupWrites.push(request.url())
        }
      })
      await page.route('**/api/next/wallet/topup/quote', (route) => {
        expect(route.request().method()).toBe('POST')
        const input = route.request().postDataJSON()
        expect(input.amount_unit).toBe('USD')
        if (failQuote)
          return route.fulfill({
            json: { success: false, message: '模拟报价暂不可用' },
          })
        const minor = input.amount * 730 + (quoteVersion - 1) * 100
        return route.fulfill({
          json: {
            success: true,
            data: {
              amount: input.amount,
              amount_unit: 'USD',
              payment_method: input.payment_method,
              amount_minor: String(minor),
              pay_amount: (minor / 100).toFixed(2),
              currency: 'CNY',
              expected_quota: String(input.amount * 500000),
              min_topup: 5,
              quote_fingerprint: `quote-${quoteVersion}`,
            },
          },
        })
      })
      await page.route('**/api/next/wallet/topup', (route) => {
        orderWrites++
        expect(route.request().postDataJSON().quote_fingerprint).toBe(
          `quote-${quoteVersion}`
        )
        quoteVersion++
        return route.fulfill({
          json: {
            success: false,
            code: 'QUOTE_CHANGED',
            message: 'quote changed',
          },
        })
      })
      await page.goto('/wallet')
      const pay = page.getByRole('button', { name: '立即充值', exact: true })
      await expect(
        page.getByText('¥73.00', { exact: true }).first()
      ).toBeVisible()
      await expect(
        page.getByText('到账 USD 额度', { exact: true })
      ).toBeVisible()
      await expect(pay).toBeEnabled()
      await pay.focus()
      await expect(pay).toBeFocused()
      await expect(
        page.getByRole('button', { name: '检查支付结果' })
      ).toHaveCount(0)
      await expect(page.getByText('待支付', { exact: true })).toBeVisible()
      const records = page.locator('section').filter({
        has: page.getByRole('heading', { name: '充值记录', exact: true }),
      })
      await records.screenshot({
        path: testInfo.outputPath('epay-records-pending.png'),
      })
      failQuote = true
      await page.getByPlaceholder('输入金额', { exact: true }).fill('20')
      await expect(
        page.getByRole('alert').filter({ hasText: '模拟报价暂不可用' })
      ).toBeVisible()
      await expect(pay).toBeDisabled()
      await page.screenshot({
        path: testInfo.outputPath('epay-quote-failure.png'),
        fullPage: true,
      })
      failQuote = false
      await page.getByRole('button', { name: '重新获取报价' }).click()
      await expect(page.getByText('¥146.00', { exact: true })).toBeVisible()
      await pay.click()
      await expect(
        page.getByText('报价已变化，请确认更新后的金额，再次点击充值。')
      ).toBeVisible()
      await expect(page.getByText('¥147.00', { exact: true })).toBeVisible()
      expect(orderWrites).toBe(1)
      completed = true
      user.available_quota = '10201314'
      user.quota_version = 2
      await page.reload()
      await expect(page.getByText('已完成', { exact: true })).toBeVisible()
      await expect(page.getByText('待支付', { exact: true })).toHaveCount(0)
      await expect(
        page.getByText('10201314 quota', { exact: true })
      ).toBeVisible()
      expect(unexpectedTopupWrites).toEqual([])
      await assertNoHorizontalOverflow(page)
      await records.screenshot({
        path: testInfo.outputPath('epay-records-completed.png'),
      })
      await page.goto('/system-settings/billing/payment')
      await page
        .getByRole('button', { name: 'EPay 易支付', exact: true })
        .click()
      const settlementDescription = page.getByText(
        '允许支付网关回调确认已有订单并入账。异常订单由网关重试通知，或由管理员复查后补单。关闭后暂停回调结算，与新单支付方式独立控制。'
      )
      await expect(settlementDescription).toBeVisible()
      await settlementDescription.scrollIntoViewIfNeeded()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('epay-settings.png'),
        fullPage: true,
      })
      expect(errors).toEqual([])
    })
  }
}
