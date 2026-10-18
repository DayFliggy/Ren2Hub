import { expect, test } from '@playwright/test'
import { nativeCoreFixture } from './native-core-fixture'
for (const theme of ['light', 'dark'])
  for (const width of [1440, 390]) {
    test(`${theme} ${width} native wallet and public attribution`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 })
      const state = await nativeCoreFixture(page, theme)
      await page.goto('/wallet')
      await page
        .getByRole('combobox', { name: 'Payment method', exact: true })
        .selectOption('gmpay:usdt.tron')
      await page.getByRole('button', { name: 'Calculate amount due' }).click()
      await expect(page.getByText('Amount due: 73.00')).toBeVisible()
      await page.screenshot({
        path: testInfo.outputPath(`wallet-${theme}-${width}.png`),
      })
      await page.goto('/pricing')
      await expect(
        page.getByText('gpt-native', { exact: true }).first()
      ).toBeVisible()
      const attribution = page
        .locator('footer')
        .getByText('DayFliggy', { exact: true })
      await attribution.scrollIntoViewIfNeeded()
      await expect(attribution).toBeVisible()
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1
        )
      ).toBe(true)
      await page.screenshot({
        path: testInfo.outputPath(`attribution-${theme}-${width}.png`),
      })
      expect(state.unexpected).toEqual([])
      expect(state.errors).toEqual([])
      expect(state.missingMessages).toEqual([])
    })
  }

test('token-mode recharge uses whole quota units and disables incompatible Stripe checkout', async ({
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 390, height: 900 })
  const state = await nativeCoreFixture(page, 'dark', 'TOKENS')
  await page.goto('/wallet')
  const method = page.getByRole('combobox', {
      name: 'Payment method',
      exact: true,
    }),
    quantity = page.getByRole('spinbutton', {
      name: 'Top-up quantity (tokens)',
      exact: true,
    })
  await expect(quantity).toHaveValue('5000000')
  await expect(quantity).toHaveAttribute('step', '500000')
  await expect(method.locator('option[value="stripe"]')).toHaveJSProperty(
    'disabled',
    true
  )
  await expect(
    page.getByText(/Stripe top-up is disabled in this mode/)
  ).toBeVisible()
  await method.selectOption('gmpay:usdt.tron')
  await quantity.fill('750000')
  await expect(
    page.getByText(
      'Enter a multiple of 500000 tokens so payment and credited quantities match.'
    )
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Continue to checkout' })
  ).toBeDisabled()
  expect(
    state.writes.filter(
      (write) => write.path.endsWith('/amount') || write.path.endsWith('/pay')
    )
  ).toEqual([])
  await quantity.fill('5000000')
  await page.getByRole('button', { name: 'Calculate amount due' }).click()
  await expect(page.getByText('Amount due: 73.00')).toBeVisible()
  expect(
    state.writes.find((write) => write.path === '/api/user/amount')?.body
  ).toEqual({ amount: 5000000, payment_method: 'gmpay:usdt.tron' })
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1
    )
  ).toBe(true)
  await page.screenshot({
    path: testInfo.outputPath('wallet-tokens-dark-390.png'),
  })
  await page.getByRole('button', { name: 'Continue to checkout' }).click()
  await expect(
    page.getByRole('heading', { name: 'Local checkout' })
  ).toBeVisible()
  expect(
    state.writes.find((write) => write.path === '/api/user/gmpay/pay')?.body
  ).toEqual({ amount: 5000000, payment_method: 'gmpay:usdt.tron' })
  expect(state.unexpected).toEqual([])
  expect(state.errors).toEqual([])
  expect(state.missingMessages).toEqual([])
})
