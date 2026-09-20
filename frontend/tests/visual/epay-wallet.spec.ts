import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`Epay quote and recovery ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      let failQuote = false
      let quoteVersion = 1
      let orderWrites = 0
      let verificationWrites = 0
      let completed = false
      await page.route('**/api/user/self', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              id: 1,
              username: 'visual.root',
              display_name: 'Visual Root',
              email: 'visual-root@ren2hub.dev',
              role: 100,
              status: 1,
              available_quota: completed ? '10201314' : '5201314',
              reserved_quota: '150000',
              used_quota: '2985211',
              quota_version: completed ? 2 : 1,
              request_count: 32132,
              group: 'vip',
            },
          },
        })
      )
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
      await page.route('**/api/next/wallet/topups?*', (route) =>
        route.fulfill({
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
      )
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
      await page.route(
        '**/api/next/wallet/topups/USR1NOpayment-test/verify',
        (route) => {
          verificationWrites++
          expect(route.request().method()).toBe('POST')
          if (verificationWrites === 1)
            return route.fulfill({
              json: { success: false, message: '模拟网关查询超时' },
            })
          completed = true
          return route.fulfill({
            json: {
              success: true,
              data: { ...order, status: 'completed', quota: '5000000' },
            },
          })
        }
      )
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
      expect(verificationWrites).toBe(0)
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
      const verify = page.getByRole('button', { name: '检查支付结果' })
      await verify.click()
      await expect(
        page.getByRole('alert').filter({ hasText: '模拟网关查询超时' })
      ).toBeVisible()
      await verify.click()
      await expect(page.getByText('支付已确认并到账。')).toBeVisible()
      await expect(verify).toHaveCount(0)
      expect(verificationWrites).toBe(2)
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('epay-wallet.png'),
        fullPage: true,
      })
      expect(errors).toEqual([])
    })
  }
}
