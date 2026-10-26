import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

test('EPay settings renders translated configuration in English', async ({
  page,
}, testInfo) => {
  await configureStablePage(page, { theme: 'light', locale: 'en' })
  await page.route('**/api/option/', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          { key: 'Price', value: '7.3' },
          { key: 'MinTopUp', value: '1' },
          { key: 'PayMethods', value: '[]' },
        ],
      },
    })
  )
  await page.goto('/')
  await page.evaluate(() => localStorage.setItem('ren2hub_locale', 'en'))
  // ConfigureStablePage deliberately forces Chinese on each navigation.
  await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
  await page.goto('/system-settings/billing/epay')
  await expect(
    page.getByRole('heading', { name: 'EPay', exact: true })
  ).toBeVisible()
  await expect(
    page.getByLabel('EPay gateway base URL', { exact: true })
  ).toBeVisible()
  await expect(
    page.getByLabel('CNY price per USD credit', { exact: true })
  ).toHaveValue('7.3')
  await expect(
    page.getByLabel('Minimum credit purchase (USD)', { exact: true })
  ).toHaveValue('1')
  await expect(
    page.getByText(
      'An empty list disables EPay orders and payment notifications.',
      { exact: false }
    )
  ).toBeVisible()
  await assertNoHorizontalOverflow(page)
  await page.screenshot({
    path: testInfo.outputPath('epay-settings-en.png'),
    fullPage: true,
  })
})

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`EPay settings ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      const options: Record<string, string> = {
        PayAddress: 'https://epay.example',
        EpayId: '0009007199254740993',
        Price: '7.3',
        MinTopUp: '1',
        PayMethods: '[{"name":"支付宝","type":"alipay","min_topup":"1"}]',
        'payment_setting.epay_settlement_enabled': 'true',
      }
      let unavailable = true
      let failWrite = true
      const writes: Array<{ key: string; value: string }> = []
      await page.route('**/api/option/', (route) => {
        if (route.request().method() === 'PUT') {
          const payload = route.request().postDataJSON()
          writes.push(payload)
          expect(['EpayKey', 'Price']).toContain(payload.key)
          expect(typeof payload.value).toBe('string')
          if (!failWrite) options[payload.key] = payload.value
          return route.fulfill({
            json: failWrite
              ? { success: false, message: '模拟保存失败' }
              : { success: true, data: null },
          })
        }
        return route.fulfill({
          json: unavailable
            ? { success: false, message: '配置读取失败' }
            : {
                success: true,
                data: Object.entries(options).map(([key, value]) => ({
                  key,
                  value,
                })),
              },
        })
      })
      await page.goto('/system-settings/billing/epay')
      await expect(
        page.getByRole('heading', { name: 'EPay 易支付', exact: true })
      ).toBeVisible()
      const save = page.getByRole('button', { name: '保存更改', exact: true })
      await expect(
        page.getByText('系统设置读取失败。重新读取成功前不能编辑或保存。')
      ).toBeVisible()
      await expect(
        page.getByLabel('EPay 网关基础地址', { exact: true })
      ).toBeDisabled()
      await expect(save).toBeDisabled()
      unavailable = false
      await page
        .getByRole('button', { name: '重新读取设置', exact: true })
        .click()
      await expect(page.getByLabel('商户 ID', { exact: true })).toHaveValue(
        '0009007199254740993'
      )
      await expect(
        page.getByLabel('最低购买额度（USD）', { exact: true })
      ).toHaveValue('1')
      await page
        .getByLabel('商户密钥', { exact: true })
        .fill('replacement-test-key')
      await page
        .getByLabel('每 USD 额度的支付单价（CNY）', { exact: true })
        .fill('8.1')
      await save.focus()
      await expect(save).toBeFocused()
      await save.click()
      await expect(
        page.getByText('模拟保存失败', { exact: true })
      ).toBeVisible()
      await expect(page.getByLabel('商户密钥', { exact: true })).toHaveValue(
        'replacement-test-key'
      )
      failWrite = false
      await save.click()
      await expect(page.getByLabel('商户密钥', { exact: true })).toHaveValue('')
      await expect(save).toBeDisabled()
      expect(writes).toEqual([
        { key: 'EpayKey', value: 'replacement-test-key' },
        { key: 'EpayKey', value: 'replacement-test-key' },
        { key: 'Price', value: '8.1' },
      ])
      await assertNoHorizontalOverflow(page)
      await page
        .getByRole('heading', { name: 'EPay 易支付', exact: true })
        .scrollIntoViewIfNeeded()
      await page.screenshot({
        path: testInfo.outputPath('epay-settings.png'),
        fullPage: true,
      })
      expect(errors).toEqual([])
    })
  }
}
