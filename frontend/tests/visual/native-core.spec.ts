import { expect, test } from '@playwright/test'
import { nativeCoreFixture } from './native-core-fixture'
for (const theme of ['light', 'dark'])
  for (const width of [1440, 390]) {
    test(`${theme} ${width} native user/admin API and payment contracts`, async ({
      page,
    }, testInfo) => {
      test.setTimeout(90_000)
      await page.setViewportSize({ width, height: 900 })
      const state = await nativeCoreFixture(page, theme)
      const routes = [
        ['/dashboard', 'Native Root'],
        ['/models', 'gpt-native'],
        ['/keys', 'Native Key'],
        ['/wallet', 'Online top-up'],
        ['/activity', 'Check in'],
        ['/invite', 'NATIVE-AFF'],
        ['/users', 'Native Member'],
        ['/orders', 'native-order'],
        ['/usage-logs', 'gpt-native'],
        ['/redemption-codes', 'Redemption codes'],
        ['/usage-logs/audit', 'Updated user member'],
        ['/ops', 'Go runtime memory'],
        ['/profile', 'Native Root'],
        ['/security', 'Personal access tokens'],
        ['/system-settings/security', 'Request policy'],
        ['/system-settings/billing/gmpay', 'Gateway address'],
      ] as const
      for (const [path, text] of routes) {
        await page.goto(path, { waitUntil: 'domcontentloaded' })
        await expect(
          page
            .getByText(text, { exact: false })
            .filter({ visible: true })
            .first()
        ).toBeVisible()
        expect(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth + 1
          ),
          path
        ).toBe(true)
        expect(state.errors, path).toEqual([])
        await expect(page.locator('[role=alert]')).toHaveCount(0)
      }
      await page.goto('/keys/7/routing')
      await page
        .getByRole('textbox', { name: 'Model', exact: true })
        .fill('gpt-native')
      await page
        .getByRole('button', { name: 'Preview saved routing', exact: true })
        .click()
      await expect(
        page.locator('li').filter({ hasText: 'Native channel · Eligible' })
      ).toBeVisible()
      expect(
        state.writes.some(
          (w) => w.path.includes('/relay') || w.path.includes('/preview')
        )
      ).toBe(false)
      await page.getByLabel('Automatic', { exact: true }).check()
      await page.getByRole('button', { name: 'Save', exact: true }).click()
      await expect
        .poll(
          () =>
            state.writes.find((w) => w.path === '/api/token/7/routing')?.body
        )
        .toEqual({ mode: 'auto', version: 1, entries: [] })
      await page.goto('/wallet')
      await page
        .getByRole('combobox', { name: 'Payment method', exact: true })
        .selectOption('gmpay:usdt.tron')
      await page
        .getByRole('spinbutton', { name: 'Top-up quantity', exact: true })
        .fill('10')
      await page.getByRole('button', { name: 'Calculate amount due' }).click()
      await expect(page.getByText('Amount due: 73.00')).toBeVisible()
      await page.screenshot({
        path: testInfo.outputPath(`native-wallet-${theme}-${width}.png`),
      })
      await page.getByRole('button', { name: 'Continue to checkout' }).click()
      await expect(
        page.getByRole('heading', { name: 'Local checkout' })
      ).toBeVisible()
      expect(
        state.writes.find((w) => w.path === '/api/user/gmpay/pay')?.body
      ).toEqual({ amount: 10, payment_method: 'gmpay:usdt.tron' })
      expect(state.unexpected).toEqual([])
      expect(state.errors).toEqual([])
      expect([...new Set(state.missingMessages)]).toEqual([])
    })
  }
