import { expect, test } from '@playwright/test'
import { nativeCoreFixture } from './native-core-fixture'
for (const theme of ['light', 'dark'])
  for (const width of [1440, 390]) {
    test(`${theme} ${width} native user/admin API and payment contracts`, async ({
      page,
    }, testInfo) => {
      test.setTimeout(180_000)
      await page.setViewportSize({ width, height: 900 })
      const state = await nativeCoreFixture(page, theme)
      const routes = [
        ['/dashboard', 'Native Root'],
        ['/models', 'gpt-native'],
        ['/keys', 'Native Key'],
        ['/wallet', 'Quick top-up'],
        ['/activity', 'Check in'],
        ['/invite', 'NATIVE-AFF'],
        ['/users', 'Native Member'],
        ['/orders', 'native-order'],
        ['/usage-logs', 'gpt-native'],
        ['/redemption-codes', 'Redemption codes'],
        ['/usage-logs/audit', 'Updated user member'],
        ['/ops', 'Go runtime system memory'],
        ['/profile', 'Native Root'],
        ['/security', 'Personal access tokens'],
        ['/system-settings/security', 'Request policy'],
        ['/system-settings/billing/gmpay', 'Gateway address'],
      ] as const
      for (const [path, text] of routes) {
        await page.goto(path, { waitUntil: 'domcontentloaded' })
        if (path === '/orders')
          await page.getByRole('tab', { name: 'Orders', exact: true }).click()
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
      const preview = page.locator('section').filter({
        has: page.getByRole('heading', {
          name: 'Preview saved routing',
          exact: true,
        }),
      })
      await expect(preview).toContainText('#2 Native channel')
      await expect(preview.getByText('Eligible', { exact: true })).toBeVisible()
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
        .toEqual({
          mode: 'auto',
          version: 1,
          max_channel_ratio: null,
          entries: [],
        })
      await page.goto('/wallet')
      await page
        .getByRole('button', { name: 'USDT · Tron', exact: true })
        .click()
      await page
        .getByRole('textbox', { name: 'Top-up quantity', exact: true })
        .fill('10')
      await expect(
        page
          .getByText('Amount due', { exact: true })
          .locator('..')
          .getByText('73.00', { exact: true })
      ).toBeVisible()
      await page.screenshot({
        path: testInfo.outputPath(`native-wallet-${theme}-${width}.png`),
      })
      await page
        .getByRole('button', { name: 'Continue to cashier', exact: true })
        .click()
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
