import { expect, test } from '@playwright/test'
import { nativeCoreFixture } from './native-core-fixture'

for (const theme of ['light', 'dark']) {
  for (const width of [1440, 390]) {
    test(`${theme} ${width} native user creation and EPay settings`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 })
      const state = await nativeCoreFixture(page, theme)
      const creations: unknown[] = []
      const settingsWrites: unknown[] = []
      await page.route('**/api/user/', async (route) => {
        if (route.request().method() !== 'POST') return route.fallback()
        const body = route.request().postDataJSON()
        creations.push(body)
        // The native create endpoint accepts only these form fields. It does
        // not apply email, group, or status from a create request.
        expect(Object.keys(body).sort()).toEqual([
          'display_name',
          'max_concurrency',
          'password',
          'role',
          'username',
        ])
        await route.fulfill({ json: { success: true, message: '' } })
      })
      const options: Record<string, string> = {
        PayAddress: 'https://epay.example.test',
        EpayId: 'merchant-before',
        Price: '7.3',
        MinTopUp: '1',
        PayMethods: '[{"type":"alipay","name":"Alipay"}]',
        // Old persisted options must not reintroduce a control the native
        // payment settings no longer implement.
        'payment_setting.epay_settlement_enabled': 'false',
      }
      await page.route('**/api/option/', async (route) => {
        if (route.request().method() === 'PUT') {
          const body = route.request().postDataJSON()
          settingsWrites.push(body)
          expect(body).toEqual({ key: 'EpayId', value: 'merchant-after' })
          options[body.key] = body.value
          await route.fulfill({ json: { success: true, message: '' } })
          return
        }
        await route.fulfill({
          json: {
            success: true,
            data: Object.entries(options).map(([key, value]) => ({
              key,
              value,
            })),
          },
        })
      })

      await page.goto('/users')
      await page.getByRole('button', { name: 'New user', exact: true }).click()
      const dialog = page.getByRole('dialog', { name: 'New user', exact: true })
      await expect(dialog).toBeVisible()
      await expect(dialog.getByLabel('Email', { exact: true })).toHaveCount(0)
      await expect(
        dialog.getByLabel('User Group', { exact: true })
      ).toHaveCount(0)
      await expect(
        dialog.getByRole('radiogroup', { name: 'Status' })
      ).toHaveCount(0)
      await dialog.locator('[name="admin-user-username"]').fill('audit.member')
      await dialog
        .locator('[name="admin-user-display-name"]')
        .fill('Audit Member')
      await dialog
        .locator('[name="admin-user-password"]')
        .fill('local-test-password')
      await page.screenshot({ path: testInfo.outputPath('create-user.png') })
      await dialog
        .getByRole('button', { name: 'Save user', exact: true })
        .click()
      await expect(dialog).toHaveCount(0)
      expect(creations).toEqual([
        {
          username: 'audit.member',
          max_concurrency: 0,
          display_name: 'Audit Member',
          password: 'local-test-password',
          role: 1,
        },
      ])

      await page.goto('/system-settings/billing/epay')
      await expect(
        page.getByRole('heading', { name: 'EPay', exact: true })
      ).toBeVisible()
      await expect(page.getByLabel('Enable Epay settlement')).toHaveCount(0)
      await page
        .getByLabel('Merchant ID', { exact: true })
        .fill('merchant-after')
      await page
        .getByRole('button', { name: 'Save changes', exact: true })
        .click()
      await expect.poll(() => settingsWrites.length).toBe(1)
      await page.reload()
      await expect(page.getByLabel('Merchant ID', { exact: true })).toHaveValue(
        'merchant-after'
      )
      await expect(page.getByLabel('Enable Epay settlement')).toHaveCount(0)
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1
        )
      ).toBe(true)
      await page.screenshot({
        path: testInfo.outputPath('epay-settings.png'),
        fullPage: true,
      })
      expect(state.unexpected).toEqual([])
      expect(state.errors).toEqual([])
      expect([...new Set(state.missingMessages)]).toEqual([])
    })
  }
}
