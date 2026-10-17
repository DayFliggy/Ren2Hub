import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow } from './fixtures'
import { nativeCoreFixture } from './native-core-fixture'

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`EPay native option save and GMPay order control ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      const fixture = await nativeCoreFixture(page, theme)
      const options: Record<string, string> = {
        PayAddress: 'https://epay.example',
        EpayId: '0009007199254740993',
        Price: '7.3',
        MinTopUp: '1',
        PayMethods: '[{"name":"Alipay","type":"alipay","min_topup":"1"}]',
        'payment_setting.epay_settlement_enabled': 'false',
        'payment_setting.gmpay_settlement_enabled': 'false',
      }
      let unavailable = true
      let failWrite = true
      const writes: Array<{ key: string; value: string }> = []
      await page.route('**/api/option/', (route) => {
        if (route.request().method() === 'GET')
          return route.fulfill({
            json: unavailable
              ? { success: false, message: 'Configuration unavailable' }
              : {
                  success: true,
                  data: Object.entries(options).map(([key, value]) => ({
                    key,
                    value,
                  })),
                },
          })
        expect(route.request().method()).toBe('PUT')
        const body = route.request().postDataJSON()
        writes.push(body)
        expect(['EpayKey', 'Price']).toContain(body.key)
        if (!failWrite && body.key !== 'EpayKey')
          options[body.key] = String(body.value)
        return route.fulfill({
          json: failWrite
            ? { success: false, message: 'Save unavailable' }
            : { success: true, data: null },
        })
      })
      await page.goto('/system-settings/billing/epay')
      const save = page.getByRole('button', {
        name: 'Save changes',
        exact: true,
      })
      await expect(
        page.getByLabel('EPay gateway base URL', { exact: true })
      ).toBeDisabled()
      await expect(save).toBeDisabled()
      unavailable = false
      await page.getByRole('button', { name: 'Reload settings' }).click()
      await expect(page.getByLabel('Merchant ID', { exact: true })).toHaveValue(
        '0009007199254740993'
      )
      await expect(
        page.getByRole('switch', { name: 'Enable Epay settlement' })
      ).toHaveCount(0)
      await expect(
        page.getByText(
          'An empty list disables EPay orders and payment notifications.',
          { exact: false }
        )
      ).toBeVisible()
      await page
        .getByLabel('Merchant secret', { exact: true })
        .fill('replacement-test-key')
      await page
        .getByLabel('CNY price per USD credit', { exact: true })
        .fill('8.1')
      await save.click()
      await expect(
        page.getByText('Save unavailable', { exact: false }).first()
      ).toBeVisible()
      await expect(
        page.getByLabel('Merchant secret', { exact: true })
      ).toHaveValue('replacement-test-key')
      expect(writes).toEqual([
        { key: 'EpayKey', value: 'replacement-test-key' },
      ])
      failWrite = false
      await save.click()
      await expect(
        page.getByLabel('Merchant secret', { exact: true })
      ).toHaveValue('')
      await expect(save).toBeDisabled()
      expect(writes.map((entry) => entry.key)).toEqual([
        'EpayKey',
        'EpayKey',
        'Price',
      ])
      expect(String(writes[2]?.value)).toBe('8.1')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('epay-native-settings.png'),
        fullPage: true,
      })
      let enabled = true
      await page.route('**/api/option/gmpay/config', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              pay_address: 'https://pay.example',
              merchant_id: 'merchant',
              has_secret: true,
              assets: [{ name: 'USDT', token: 'usdt', network: 'tron' }],
              enabled,
            },
          },
        })
      )
      await page.route('**/api/option/gmpay/save', (route) => {
        const body = route.request().postDataJSON()
        expect(body).toEqual({
          pay_address: 'https://pay.example',
          merchant_id: 'merchant',
          secret_key: '',
          enabled: false,
          assets: [{ name: 'USDT', token: 'usdt', network: 'tron' }],
        })
        enabled = body.enabled
        return route.fulfill({ json: { success: true, data: null } })
      })
      await page.goto('/system-settings/billing/gmpay')
      await expect(
        page.getByRole('switch', { name: 'Enable crypto settlement' })
      ).toHaveCount(0)
      await expect(
        page.getByText(
          'Valid payment notifications for existing orders still credit the wallet',
          { exact: false }
        )
      ).toBeVisible()
      await page.getByRole('checkbox', { name: 'Accept new orders' }).uncheck()
      await page
        .getByRole('button', { name: 'Save gateway configuration' })
        .click()
      await expect(
        page.getByRole('checkbox', { name: 'Accept new orders' })
      ).not.toBeChecked()
      await expect.poll(() => enabled).toBe(false)
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('gmpay-new-orders-control.png'),
        fullPage: true,
      })
      expect(fixture.unexpected).toEqual([])
      expect(fixture.errors).toEqual([])
    })
  }
}
