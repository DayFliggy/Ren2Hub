import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'
import {
  nativeTopupInfo,
  nativeTopUp,
  nativeGmpayEvidence,
} from '../../src/api/__tests__/topupFixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`GMPay native CNY checkout and return ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      await page.route('**/api/user/topup/info', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              ...nativeTopupInfo,
              enable_online_topup: false,
              pay_methods: [nativeTopupInfo.pay_methods[1]],
            },
          },
        })
      )
      let failQuote = false
      await page.route('**/api/user/amount', (route) => {
        const input = route.request().postDataJSON()
        expect(input.payment_method).toBe('gmpay:usdt.tron')
        expect(input).not.toHaveProperty('quote_fingerprint')
        return route.fulfill({
          json: failQuote
            ? { message: 'error', data: '模拟报价暂不可用' }
            : { message: 'success', data: (input.amount * 7.3).toFixed(2) },
        })
      })
      let creates = 0
      await page.route('**/api/user/gmpay/pay', (route) => {
        creates++
        expect(route.request().postDataJSON()).toEqual({
          amount: 20,
          payment_method: 'gmpay:usdt.tron',
        })
        return route.fulfill({
          json: {
            message: 'success',
            url: 'https://cashier.example/native',
            data: {
              trade_no: 'GPtest',
              currency: 'CNY',
              pay_amount: '146.00',
              expected_quota: 10000000,
              token: 'usdt',
              network: 'tron',
            },
          },
        })
      })
      await page.route('https://cashier.example/native', (route) =>
        route.fulfill({
          contentType: 'text/html',
          body: '<h1>Local crypto cashier</h1>',
        })
      )
      let settled = false
      await page.route('**/api/user/topup/detail/GPtest', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              ...nativeTopUp,
              status: settled ? 'success' : 'pending',
              credited_quota: settled ? 10000000 : 0,
              complete_time: settled ? 1785114100 : 0,
              gmpay: {
                ...nativeGmpayEvidence,
                status: settled ? 'settled' : 'ready',
              },
            },
          },
        })
      )
      await page.goto('/wallet')
      const submit = page.getByRole('button', {
        name: '前往收银台支付',
        exact: true,
      })
      await expect(
        page.getByText('¥73.00', { exact: true }).first()
      ).toBeVisible()
      await expect(submit).toBeEnabled()
      failQuote = true
      await page.getByPlaceholder('输入金额', { exact: true }).fill('20')
      await expect(
        page.getByRole('alert').filter({ hasText: '模拟报价暂不可用' })
      ).toBeVisible()
      await expect(submit).toBeDisabled()
      failQuote = false
      await page.getByRole('button', { name: '重新获取报价' }).click()
      await expect(
        page.getByText('¥146.00', { exact: true }).first()
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('native-gmpay-wallet.png'),
        fullPage: true,
      })
      await submit.click()
      await expect(page).toHaveURL('https://cashier.example/native')
      expect(creates).toBe(1)
      await page.goto('/wallet?trade_no=GPtest&status=success')
      const status = page.locator('section').filter({
        has: page.getByRole('heading', { name: '支付状态', exact: true }),
      })
      await expect(status.getByText(/待支付/)).toBeVisible()
      settled = true
      await status
        .getByRole('button', { name: '刷新数据', exact: true })
        .click()
      await expect(status.getByText(/已完成/)).toBeVisible()
      expect(creates).toBe(1)
      expect(errors).toEqual([])
    })
  }
}

test('unknown GMPay creation retains its number and cannot create again', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  await page.route('**/api/user/topup/info', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          ...nativeTopupInfo,
          enable_online_topup: false,
          pay_methods: [nativeTopupInfo.pay_methods[1]],
        },
      },
    })
  )
  let creates = 0
  await page.route('**/api/user/gmpay/pay', (route) => {
    creates++
    return route.fulfill({
      json: {
        message: 'error',
        data: 'Creation uncertain',
        trade_no: 'GPtest',
        creation_status: 'unknown',
      },
    })
  })
  await page.route('**/api/user/topup/detail/GPtest', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { ...nativeTopUp, gmpay: nativeGmpayEvidence },
      },
    })
  )
  await page.goto('/wallet')
  const submit = page.getByRole('button', {
    name: '前往收银台支付',
    exact: true,
  })
  await expect(submit).toBeEnabled()
  await submit.click()
  await expect(submit).toBeDisabled()
  await expect(page.getByText('GPtest', { exact: true })).toBeVisible()
  await page.getByPlaceholder('输入金额', { exact: true }).fill('20')
  await expect(submit).toBeDisabled()
  expect(creates).toBe(1)
})

test('GMPay configuration saves native enabled and array assets without retired pricing', async ({
  page,
}, testInfo) => {
  await configureStablePage(page, { theme: 'light' })
  const config = {
    pay_address: 'https://gateway.example',
    merchant_id: 'local',
    has_secret: true,
    enabled: true,
    assets: [
      {
        name: 'USDT Tron',
        token: 'usdt',
        network: 'tron',
        icon: '',
        color: '',
      },
    ],
  }
  await page.route('**/api/option/gmpay/config', (route) =>
    route.fulfill({ json: { success: true, data: config } })
  )
  let saved = false
  await page.route('**/api/option/gmpay/save', (route) => {
    const body = route.request().postDataJSON()
    expect(body.assets).toEqual(config.assets)
    expect(body.enabled).toBe(false)
    expect(body.secret_key).toBe('')
    expect(body).not.toHaveProperty('price')
    expect(body).not.toHaveProperty('group_ratio')
    saved = true
    return route.fulfill({
      json: { success: true, data: { ...config, enabled: false } },
    })
  })
  await page.goto('/system-settings/billing/gmpay')
  await page
    .getByRole('checkbox', { name: '启用 GMPay', exact: true })
    .uncheck()
  await page.getByRole('button', { name: '保存网关配置', exact: true }).click()
  await expect.poll(() => saved).toBe(true)
  await assertNoHorizontalOverflow(page)
  await page.screenshot({
    path: testInfo.outputPath('native-gmpay-settings.png'),
    fullPage: true,
  })
})
