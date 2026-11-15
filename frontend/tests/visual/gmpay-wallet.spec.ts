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

test('wallet reports unified payment configuration failure and recovers explicitly', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  let unavailable = true
  await page.route('**/api/user/topup/info', (route) =>
    unavailable
      ? route.fulfill({
          json: {
            success: false,
            message: 'Payment configuration unavailable',
          },
        })
      : route.fallback()
  )
  await page.goto('/wallet')
  const alert = page
    .getByRole('alert')
    .filter({ hasText: '当前暂无可用支付方式。' })
  await expect(alert).toBeVisible()
  await expect(
    page.getByRole('button', { name: '立即充值', exact: true })
  ).toBeDisabled()
  await assertNoHorizontalOverflow(page)
  unavailable = false
  await alert.getByRole('button', { name: '重试', exact: true }).click()
  await expect(alert).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: 'Alipay', exact: true })
  ).toBeVisible()
  await page.getByRole('button', { name: 'USDT · Tron', exact: true }).click()
  await expect(
    page.getByRole('button', { name: '前往收银台支付', exact: true })
  ).toBeEnabled()
  await assertNoHorizontalOverflow(page)
})

for (const [theme, mobile] of [
  ['light', false],
  ['dark', true],
] as const) {
  test(`GMPay quotes and checkout ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
    )
    await configureStablePage(page, { theme })
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    let epayEnabled = false
    await page.route('**/api/user/topup/info', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            enable_online_topup: epayEnabled,
            enable_stripe_topup: false,
            enable_creem_topup: false,
            enable_waffo_topup: false,
            enable_waffo_pancake_topup: false,
            enable_gmpay_topup: true,
            enable_redemption: true,
            pay_methods: [
              ...(epayEnabled
                ? [{ name: 'Alipay', type: 'alipay', min_topup: '50' }]
                : []),
              {
                name: 'USDT · Tron',
                type: 'gmpay:usdt.tron',
                min_topup: '1',
              },
            ],
            min_topup: 1,
            stripe_min_topup: 1,
            waffo_min_topup: 1,
            waffo_pancake_min_topup: 1,
            amount_options: [10, 20, 50, 100],
            waffo_pay_methods: null,
            creem_products: '[]',
            topup_link: '',
          },
        },
      })
    )
    let failQuote = false
    let orderWrites = 0
    let releaseOrder!: () => void
    const pendingOrder = new Promise<void>((resolve) => {
      releaseOrder = resolve
    })
    await page.route('**/api/user/amount', (route) => {
      const input = route.request().postDataJSON()
      expect(['gmpay:usdt.tron', 'alipay']).toContain(input.payment_method)
      return route.fulfill({
        json: failQuote
          ? { message: 'error', data: '模拟虚拟币报价失败' }
          : quoted(input.amount),
      })
    })
    await page.route('**/api/user/gmpay/pay', async (route) => {
      orderWrites++
      expect(route.request().postDataJSON()).toEqual({
        amount: 20,
        expected_pay_amount: '146.00',
        expected_currency: 'CNY',
        payment_method: 'gmpay:usdt.tron',
      })
      await pendingOrder
      return route.fulfill({
        json: {
          message: 'success',
          url: 'https://cashier.example/cashier/test',
          data: { trade_no: 'GPvisual', creation_status: 'ready' },
        },
      })
    })
    await page.route('**/api/user/topup/detail/GPvisual', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            id: 1,
            user_id: 1,
            trade_no: 'GPvisual',
            user_deleted: false,
            currency: 'CNY',
            pay_amount: '146.00',
            amount: 20,
            credited_quota: 0,
            payment_provider: 'gmpay',
            payment_method: 'gmpay:usdt.tron',
            status: 'pending',
            create_time: 1785114000,
            complete_time: 0,
            gmpay: null,
          },
        },
      })
    )
    await page.route('https://cashier.example/cashier/test', (route) =>
      route.fulfill({
        contentType: 'text/html',
        body: '<main>Hosted test cashier</main>',
      })
    )
    await Promise.all([
      page.waitForResponse('**/api/user/topup/info'),
      page.goto('/wallet'),
    ])
    const submit = page.getByRole('button', {
      name: '前往收银台支付',
      exact: true,
    })
    await expect(page.getByText('实际支付 CNY', { exact: true })).toBeVisible()
    await expect(submit).toBeEnabled()
    await expect(
      page.getByRole('button', { name: '$10', exact: true })
    ).toBeVisible()
    await expect(
      page.getByRole('button', { name: 'Alipay', exact: true })
    ).toHaveCount(0)
    epayEnabled = true
    await page.reload()
    await page.getByRole('button', { name: 'Alipay', exact: true }).click()
    await expect(
      page.getByRole('button', { name: '$10', exact: true })
    ).toHaveCount(0)
    await page.getByRole('button', { name: 'USDT · Tron', exact: true }).click()
    await page.getByRole('button', { name: '$10', exact: true }).click()
    await expect(submit).toBeEnabled()
    await submit.focus()
    await expect(submit).toBeFocused()
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: testInfo.outputPath('gmpay-wallet.png'),
      fullPage: true,
    })
    failQuote = true
    await page.getByPlaceholder('输入金额', { exact: true }).fill('20')
    await expect(
      page.getByRole('alert').filter({ hasText: '模拟虚拟币报价失败' })
    ).toBeVisible()
    await expect(submit).toBeDisabled()
    failQuote = false
    await page.getByRole('button', { name: '重新获取报价' }).click()
    await expect(submit).toBeEnabled()
    await submit.click()
    await expect(
      page.getByPlaceholder('输入金额', { exact: true })
    ).toBeDisabled()
    await expect(
      page.getByRole('button', { name: 'Alipay', exact: true })
    ).toBeDisabled()
    await expect(
      page.getByRole('button', { name: '$10', exact: true })
    ).toBeDisabled()
    await expect(submit).toBeDisabled()
    expect(orderWrites).toBe(1)
    releaseOrder()
    await expect(page).toHaveURL('https://cashier.example/cashier/test')
    expect(orderWrites).toBe(1)

    expect(errors).toEqual([])
  })

  test(`GMPay configuration ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
    )
    await configureStablePage(page, { theme })
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    let config = {
      pay_address: 'https://gateway.example',
      merchant_id: '1000',
      has_secret: true,
      assets: [{ name: 'USDT Tron', token: 'usdt', network: 'tron' }],
      enabled: true,
    }
    let settingsUnavailable = true
    await page.route('**/api/option/gmpay/config', (route) =>
      route.fulfill({
        json: settingsUnavailable
          ? { success: false, message: '模拟配置读取失败' }
          : { success: true, data: config },
      })
    )
    await page.route('**/api/option/gmpay/probe', (route) => {
      expect(route.request().postDataJSON()).toEqual({
        pay_address: 'https://gateway.example',
      })
      return route.fulfill({
        json: {
          success: true,
          data: {
            supported_assets: [
              { network: 'tron', display_name: 'Tron', tokens: ['usdt'] },
            ],
          },
        },
      })
    })
    let saves = 0
    await page.route('**/api/option/gmpay/save', (route) => {
      saves++
      const input = route.request().postDataJSON()
      expect(input).toMatchObject({
        secret_key: '',
        assets: [],
        enabled: false,
      })
      config = {
        ...config,
        assets: input.assets,
        enabled: input.enabled,
      }
      settingsUnavailable = true
      return route.fulfill({ json: { success: true, data: config } })
    })
    await page.goto('/system-settings/billing/gmpay')
    await expect(
      page.getByRole('heading', { name: 'GMPay 虚拟币支付', exact: true })
    ).toBeVisible()
    const gatewayPanel = page.getByRole('region', {
      name: 'GMPay 虚拟币支付',
      exact: true,
    })
    await expect(gatewayPanel.getByRole('alert')).toContainText(
      '网关配置读取失败'
    )
    await expect(
      gatewayPanel.getByRole('button', { name: '保存网关配置', exact: true })
    ).toBeDisabled()
    await expect(
      gatewayPanel.getByLabel('网关地址', { exact: true })
    ).toBeDisabled()
    settingsUnavailable = false
    await gatewayPanel
      .getByRole('button', { name: '重新读取网关配置', exact: true })
      .click()
    await expect(
      gatewayPanel.getByLabel('网关地址', { exact: true })
    ).toHaveValue('https://gateway.example')
    await page
      .getByRole('button', { name: '探测可用资产', exact: true })
      .click()
    await expect(
      page.getByRole('button', { name: 'USDT · tron', exact: true })
    ).toHaveAttribute('aria-pressed', 'true')
    await gatewayPanel.getByRole('switch').uncheck()
    await page
      .getByRole('button', { name: '移除 USDT · tron', exact: true })
      .click()
    const save = page.getByRole('button', {
      name: '保存网关配置',
      exact: true,
    })
    await save.focus()
    await expect(save).toBeFocused()
    await save.click()
    await expect(
      page.getByText('GMPay 配置已保存', { exact: true })
    ).toBeVisible()
    expect(saves).toBe(1)
    await page.reload()
    await expect(gatewayPanel.getByRole('alert')).toContainText(
      '网关配置读取失败'
    )
    await expect(save).toBeDisabled()
    await gatewayPanel.getByRole('alert').scrollIntoViewIfNeeded()
    await captureEvidence(page, {
      path: testInfo.outputPath('gmpay-settings-refresh-failure.png'),
    })
    settingsUnavailable = false
    await gatewayPanel
      .getByRole('button', { name: '重新读取网关配置', exact: true })
      .click()
    await expect(save).toBeEnabled()
    await assertNoHorizontalOverflow(page)
    await gatewayPanel
      .getByLabel('网关地址', { exact: true })
      .scrollIntoViewIfNeeded()
    await captureEvidence(page, {
      path: testInfo.outputPath('gmpay-settings.png'),
      fullPage: true,
    })
    expect(errors).toEqual([])
  })
}
