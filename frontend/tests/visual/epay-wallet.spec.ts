import { expect, test } from '@playwright/test'
import { nativeFixture } from '../contracts/native-core-fixture'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  captureEvidence,
} from './fixtures'

// The server quote for an undiscounted CNY top-up of amount USD.
function quoted(amount: number) {
  const payAmount = (amount * 7.3).toFixed(2)
  const { quote } = nativeFixture<{ quote: Record<string, unknown> }>(
    'topup-quote.post.json'
  )
  return {
    message: 'success',
    data: payAmount,
    quote: {
      ...quote,
      expected_quota: amount * 500000,
      list_pay_amount: payAmount,
      pay_amount: payAmount,
      factor: '1',
      applied_rate: '0',
      amount_rate: '0',
      fixed_rate: '0',
    },
  }
}

for (const [theme, mobile] of [
  ['light', false],
  ['dark', true],
] as const) {
  test(`Epay quote, create failure and mocked completed record ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
    )
    const { user } = await configureStablePage(page, { theme })
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    let failQuote = false
    let orderWrites = 0
    const unexpectedTopupWrites: string[] = []
    let completed = false
    const order = {
      id: 1,
      trade_no: 'USR1NOpayment-test',
      user_id: 1,
      amount: 10,
      money: 73,
      currency: 'CNY',
      pay_amount: '73.00',
      expected_quota: 5000000,
      credited_quota: 0,
      affiliate_base_quota: 5000000,
      payment_provider: 'epay',
      payment_method: 'alipay',
      status: 'pending',
      create_time: 1785114000,
      complete_time: 0,
    }
    await page.route('**/api/user/topup/self?*', (route) => {
      expect(route.request().method()).toBe('GET')
      return route.fulfill({
        json: {
          success: true,
          data: {
            items: [
              {
                ...order,
                status: completed ? 'success' : 'pending',
                credited_quota: completed ? 5000000 : 0,
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
        new URL(request.url()).pathname === '/api/user/topup/self' &&
        request.method() !== 'GET'
      )
        unexpectedTopupWrites.push(request.url())
    })
    await page.route('**/api/user/amount', (route) => {
      expect(route.request().method()).toBe('POST')
      const input = route.request().postDataJSON()
      expect(input.payment_method).toBe('alipay')
      return route.fulfill({
        json: failQuote
          ? { message: 'error', data: '模拟报价暂不可用' }
          : quoted(input.amount),
      })
    })
    await page.route('**/api/user/pay', (route) => {
      orderWrites++
      expect(route.request().postDataJSON()).toEqual({
        amount: 20,
        expected_pay_amount: '146.00',
        expected_currency: 'CNY',
        payment_method: 'alipay',
      })
      return route.fulfill({
        json: { message: 'error', data: '模拟支付创建暂不可用' },
      })
    })
    await page.goto('/wallet')
    const pay = page.getByRole('button', { name: '立即充值', exact: true })
    await expect(
      page.getByText('¥73.00', { exact: true }).first()
    ).toBeVisible()
    await expect(page.getByText('到账 USD 额度', { exact: true })).toBeVisible()
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
    await captureEvidence(records, {
      path: testInfo.outputPath('epay-records-pending.png'),
    })
    failQuote = true
    await page.getByPlaceholder('输入金额', { exact: true }).fill('20')
    await expect(
      page.getByRole('alert').filter({ hasText: '模拟报价暂不可用' })
    ).toBeVisible()
    await expect(pay).toBeDisabled()
    await captureEvidence(page, {
      path: testInfo.outputPath('epay-quote-failure.png'),
      fullPage: true,
    })
    failQuote = false
    await page.getByRole('button', { name: '重新获取报价' }).click()
    await expect(page.getByText('¥146.00', { exact: true })).toBeVisible()
    await pay.click()
    await expect(
      page.getByRole('alert').filter({ hasText: '模拟支付创建暂不可用' })
    ).toBeVisible()
    expect(orderWrites).toBe(1)
    completed = true
    user.quota = 10201314
    await page.reload()
    await expect(page.getByText('已完成', { exact: true })).toBeVisible()
    await expect(page.getByText('待支付', { exact: true })).toHaveCount(0)
    await expect(
      page.getByText('$10.00', { exact: true }).first()
    ).toBeVisible()
    expect(unexpectedTopupWrites).toEqual([])
    await assertNoHorizontalOverflow(page)
    await captureEvidence(records, {
      path: testInfo.outputPath('epay-records-completed.png'),
    })
    await page.goto('/system-settings/billing/payment')
    await page.getByRole('button', { name: 'EPay 易支付', exact: true }).click()
    await expect(page).toHaveURL(/\/system-settings\/billing\/epay$/)
    const gatewayAddress = page.getByLabel('EPay 网关基础地址', {
      exact: true,
    })
    await expect(gatewayAddress).toBeVisible()
    await expect(page.getByText('EPay 支付方式', { exact: true })).toBeVisible()
    await gatewayAddress.scrollIntoViewIfNeeded()
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: testInfo.outputPath('epay-settings.png'),
      fullPage: true,
    })
    expect(errors).toEqual([])
  })
}
