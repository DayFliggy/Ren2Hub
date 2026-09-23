import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

for (const failed of ['epay', 'gmpay'] as const) {
  test(`wallet keeps the other provider usable after ${failed} configuration fails`, async ({
    page,
  }) => {
    await configureStablePage(page, { theme: 'light' })
    let allFail = false
    await page.route('**/api/next/wallet/config', (route) =>
      route.fulfill({
        json:
          failed === 'epay' || allFail
            ? { success: false, message: 'Epay test failure' }
            : {
                success: true,
                data: {
                  enable_online_topup: true,
                  enable_redemption: true,
                  pay_methods: [
                    { name: 'Alipay', type: 'alipay', min_topup: 5 },
                  ],
                  min_topup: 5,
                  amount_options: [5, 10, 20],
                },
              },
      })
    )
    await page.route('**/api/next/wallet/gmpay/config', (route) =>
      route.fulfill({
        json:
          failed === 'gmpay' || allFail
            ? { success: false, message: 'Crypto test failure' }
            : {
                success: true,
                data: {
                  enable_gmpay_topup: true,
                  enable_gmpay_settlement: true,
                  pay_methods: [
                    {
                      name: 'USDT · Tron',
                      type: 'gmpay:usdt.tron',
                      token: 'usdt',
                      network: 'tron',
                      min_topup: '1',
                      icon: '',
                      color: '',
                    },
                  ],
                  min_topup: 1,
                  amount_options: [10, 20],
                  price: '1',
                  discount: {},
                  crypto_callback_proves_network: false,
                },
              },
      })
    )
    await page.route('**/api/next/wallet/gmpay/quote', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            amount: 10,
            amount_unit: 'USD',
            payment_method: 'usdt.tron',
            currency: 'USD',
            amount_minor: '1000',
            pay_amount: '10.00',
            expected_quota: '5000000',
            min_topup: 1,
            quote_fingerprint: 'crypto-test',
            token: 'usdt',
            network: 'tron',
          },
        },
      })
    )
    await page.goto('/wallet')
    await expect(page.getByRole('alert')).toContainText(
      failed === 'epay' ? '易支付方式加载失败。' : '虚拟币支付方式加载失败。'
    )
    await expect(
      page.getByRole('button', {
        name: failed === 'epay' ? '前往收银台支付' : '立即充值',
        exact: true,
      })
    ).toBeEnabled()
    await assertNoHorizontalOverflow(page)
    allFail = true
    await page.reload()
    await expect(page.getByRole('alert')).toHaveCount(2)
    await expect(
      page.getByRole('button', { name: '立即充值', exact: true })
    ).toBeDisabled()
  })
}

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`GMPay quotes and configuration ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      let epayEnabled = false
      await page.route('**/api/next/wallet/config', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              enable_online_topup: epayEnabled,
              enable_redemption: true,
              pay_methods: epayEnabled
                ? [{ name: 'Alipay', type: 'alipay', min_topup: 50 }]
                : [],
              min_topup: 50,
              amount_options: [10, 20, 50, 100],
            },
          },
        })
      )
      await page.route('**/api/next/wallet/gmpay/config', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              enable_gmpay_topup: true,
              enable_gmpay_settlement: true,
              pay_methods: [
                {
                  name: 'USDT · Tron',
                  type: 'gmpay:usdt.tron',
                  token: 'usdt',
                  network: 'tron',
                  min_topup: '1',
                  icon: '',
                  color: '',
                },
              ],
              min_topup: 1,
              price: '1',
              discount: {},
              crypto_callback_proves_network: false,
              amount_options: [10, 20, 50, 100],
            },
          },
        })
      )
      let failQuote = false
      let quoteVersion = 1
      let orderWrites = 0
      await page.route('**/api/next/wallet/gmpay/quote', (route) => {
        const input = route.request().postDataJSON()
        expect(input.payment_method).toBe('usdt.tron')
        expect(input.amount_unit).toBe('USD')
        if (failQuote)
          return route.fulfill({
            json: { success: false, message: '模拟虚拟币报价失败' },
          })
        const minor = input.amount * 100 + quoteVersion - 1
        return route.fulfill({
          json: {
            success: true,
            data: {
              amount: input.amount,
              amount_unit: 'USD',
              payment_method: 'usdt.tron',
              currency: 'USD',
              amount_minor: String(minor),
              pay_amount: (minor / 100).toFixed(2),
              expected_quota: String(input.amount * 500000),
              min_topup: 1,
              quote_fingerprint: `crypto-${quoteVersion}`,
              token: 'usdt',
              network: 'tron',
            },
          },
        })
      })
      await page.route('**/api/next/wallet/gmpay/topup', (route) => {
        orderWrites++
        const input = route.request().postDataJSON()
        expect(input.payment_method).toBe('usdt.tron')
        expect(input.quote_fingerprint).toBe(`crypto-${quoteVersion}`)
        if (orderWrites === 1) {
          quoteVersion++
          return route.fulfill({
            json: {
              success: false,
              code: 'QUOTE_CHANGED',
              message: '报价变化',
            },
          })
        }
        return route.fulfill({
          json: {
            success: true,
            data: {
              trade_no: 'GPvisual',
              url: 'https://cashier.example/cashier/test',
            },
          },
        })
      })
      await page.route('https://cashier.example/cashier/test', (route) =>
        route.fulfill({
          contentType: 'text/html',
          body: '<main>Hosted test cashier</main>',
        })
      )
      await page.goto('/wallet')
      const submit = page.getByRole('button', {
        name: '前往收银台支付',
        exact: true,
      })
      await expect(
        page.getByText('实际支付 USD', { exact: true })
      ).toBeVisible()
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
      await page
        .getByRole('button', { name: 'USDT · Tron', exact: true })
        .click()
      await page.getByRole('button', { name: '$10', exact: true }).click()
      await expect(submit).toBeEnabled()
      await submit.focus()
      await expect(submit).toBeFocused()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
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
        page.getByText('报价已变化，请确认更新后的金额，再次点击充值。')
      ).toBeVisible()
      await expect(page.getByText('$20.01', { exact: true })).toBeVisible()
      expect(orderWrites).toBe(1)
      await submit.click()
      await expect(page).toHaveURL('https://cashier.example/cashier/test')
      expect(orderWrites).toBe(2)

      const options: Record<string, string> = {
        GmpayPayAddress: 'https://gateway.example',
        GmpayId: '1000',
        GmpayPrice: '1',
        GmpayMinTopUp: '1',
        GmpayGroupRatio: '{"default":1,"vip":0.8}',
        GmpayAssets: '[{"name":"USDT Tron","token":"usdt","network":"tron"}]',
      }
      await page.route('**/api/option/', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: Object.entries(options).map(([key, value]) => ({
              key,
              value,
            })),
          },
        })
      )
      await page.route('**/api/option/secret-status', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: { configured: ['GmpayKey'] },
          },
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
          assets: '[]',
          group_ratio: '{"default":1,"vip":0.75}',
        })
        options.GmpayAssets = input.assets
        options.GmpayGroupRatio = input.group_ratio
        return route.fulfill({ json: { success: true, data: {} } })
      })
      await page.goto('/system-settings/billing/gmpay')
      await expect(
        page.getByRole('heading', { name: 'GMPay 虚拟币支付', exact: true })
      ).toBeVisible()
      await page
        .getByRole('button', { name: '探测可用资产', exact: true })
        .click()
      await expect(
        page.getByRole('button', { name: 'USDT · tron', exact: true })
      ).toHaveAttribute('aria-pressed', 'true')
      await page
        .getByLabel('用户组价格倍率（JSON）', { exact: false })
        .fill('{"default":1,"vip":0.75}')
      await page
        .getByRole('button', { name: '移除 USDT · tron', exact: true })
        .click()
      await expect(
        page.getByText(
          '不选择资产并保存会停止创建新的虚拟币订单；已启用虚拟币结算时，已有订单仍可入账。'
        )
      ).toBeVisible()
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
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('gmpay-settings.png'),
        fullPage: true,
      })
      expect(errors).toEqual([])
    })
  }
}
