import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'
import {
  nativeTopupInfo,
  nativeTopUp,
  nativeTopUpDetail,
} from '../../src/api/__tests__/topupFixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`EPay native form and confirmed return ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      await page.route('**/api/user/topup/info', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              ...nativeTopupInfo,
              enable_gmpay_topup: false,
              pay_methods: [nativeTopupInfo.pay_methods[0]],
            },
          },
        })
      )
      let creates = 0
      await page.route('**/api/user/pay', (route) => {
        creates++
        expect(route.request().postDataJSON()).toEqual({
          amount: 10,
          payment_method: 'alipay',
        })
        return route.fulfill({
          json: {
            message: 'success',
            url: 'https://cashier.example/epay',
            data: { money: '73.00', type: 'alipay', out_trade_no: 'EPnative' },
          },
        })
      })
      await page.route('https://cashier.example/epay', (route) => {
        expect(route.request().method()).toBe('POST')
        expect(
          new URLSearchParams(route.request().postData() ?? '').get(
            'out_trade_no'
          )
        ).toBe('EPnative')
        return route.fulfill({
          contentType: 'text/html',
          body: '<h1>Local EPay cashier</h1>',
        })
      })
      let complete = false
      const row = () => ({
        ...nativeTopUp,
        trade_no: 'EPnative',
        payment_provider: 'epay',
        payment_method: 'alipay',
        status: complete ? 'success' : 'pending',
        credited_quota: complete ? 5000000 : 0,
        complete_time: complete ? 1785114100 : 0,
      })
      await page.route('**/api/user/topup/self?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: { items: [row()], total: 1, page: 1, page_size: 6 },
          },
        })
      )
      await page.route('**/api/user/topup/detail/EPnative', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: { ...nativeTopUpDetail, ...row(), gmpay: null },
          },
        })
      )
      await page.goto('/wallet')
      const pay = page.getByRole('button', { name: '立即充值', exact: true })
      await expect(pay).toBeEnabled()
      await expect(
        page.getByText('¥73.00', { exact: true }).first()
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('native-epay-wallet.png'),
        fullPage: true,
      })
      await pay.click()
      await expect(page).toHaveURL('https://cashier.example/epay')
      expect(creates).toBe(1)
      complete = true
      await page.goto('/wallet?out_trade_no=EPnative')
      await expect(
        page.getByText('已完成', { exact: true }).first()
      ).toBeVisible()
      expect(creates).toBe(1)
    })
  }
}
