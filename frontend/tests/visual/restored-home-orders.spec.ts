import { expect, test } from '@playwright/test'
import { nativeCoreFixture } from './native-core-fixture'
import {
  assertNoHorizontalOverflow,
  waitForStablePage,
  freezeAndInspectHomeCanvas,
} from './fixtures'

for (const theme of ['light', 'dark']) {
  for (const mobile of [false, true]) {
    test(`restored home content and documentation ${theme} ${mobile}`, async ({
      page,
    }, testInfo) => {
      await page.emulateMedia({ reducedMotion: 'reduce' })
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      const fixture = await nativeCoreFixture(page, theme, 'USD', {
        authenticated: false,
        status: {
          docs_link: 'https://docs.newapi.pro',
          footer_html:
            '<p>Site footer restored</p><script>parent.compromised = true</script>',
        },
      })
      await page.route('**/api/home_page_content', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: '<h1>Configured welcome</h1><img src="invalid:" onerror="parent.compromised = true">',
          },
        })
      )
      await page.goto('/')
      const content = page.locator('iframe').first()
      await expect(content).toHaveAttribute(
        'sandbox',
        'allow-popups allow-popups-to-escape-sandbox'
      )
      await expect(
        content
          .contentFrame()
          .getByRole('heading', { name: 'Configured welcome' })
      ).toBeVisible()
      await expect(page.locator('#hero-immersive-stage')).toHaveCount(0)
      await expect(
        page
          .locator('footer iframe')
          .contentFrame()
          .getByText('Site footer restored')
      ).toBeVisible()
      expect(await page.evaluate(() => 'compromised' in window)).toBe(false)
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('home-configured.png'),
        fullPage: true,
      })
      expect(fixture.errors).toEqual([])
      expect(fixture.missingMessages).toEqual([])
      await page.route('**/api/home_page_content', (route) =>
        route.fulfill({ json: { success: true, data: '' } })
      )
      await page.reload()
      await waitForStablePage(page)
      await freezeAndInspectHomeCanvas(page)
      if (mobile) {
        for (const width of [320, 360, 390, 430]) {
          await page.setViewportSize({ width, height: 844 })
          await waitForStablePage(page)
          const copy = await page.locator('.hero-copy-glow').boundingBox()
          const glyphs = await page
            .locator('.hero-title > span')
            .first()
            .evaluate((element) => {
              const range = document.createRange()
              range.selectNodeContents(element)
              return range.getBoundingClientRect().toJSON()
            })
          expect(glyphs.left).toBeGreaterThanOrEqual(copy!.x - 1)
          expect(glyphs.right).toBeLessThanOrEqual(copy!.x + copy!.width + 1)
        }
        await page.setViewportSize({ width: 390, height: 844 })
      }
      await expect(
        page
          .locator('footer iframe')
          .contentFrame()
          .getByText('Site footer restored')
      ).toBeVisible()
      await expect(page.locator('[data-home-request-total]')).toHaveText('--')
      await page.screenshot({
        path: testInfo.outputPath('home-restored.png'),
        fullPage: true,
      })
      // Cross-origin sandbox frames may not paint outside the browser viewport
      // in a full-page capture. Inspect the real footer in view as well.
      await page.locator('footer').scrollIntoViewIfNeeded()
      const footerBackground = await page
        .locator('footer')
        .evaluate((element) => getComputedStyle(element).backgroundColor)
      await expect(
        page.locator('footer iframe').contentFrame().locator('body')
      ).toHaveCSS('background-color', footerBackground)
      await page.screenshot({
        path: testInfo.outputPath('home-footer.png'),
      })
    })
  }
}

test('homepage content load failure remains retryable and preserves the built-in design', async ({
  page,
}) => {
  const fixture = await nativeCoreFixture(page, 'light', 'USD', {
    authenticated: false,
    status: { docs_link: 'https://docs.newapi.pro' },
  })
  let failed = true
  await page.route('**/api/home_page_content', (route) =>
    route.fulfill({
      json: failed
        ? { success: false, message: 'Home content unavailable' }
        : { success: true, data: '' },
    })
  )
  await page.goto('/')
  await expect(page.getByRole('alert')).toContainText(
    'Home content unavailable'
  )
  await expect(page.locator('#hero-immersive-stage')).toBeVisible()
  await expect(
    page.locator('.app-navbar a[href="https://docs.newapi.pro/"]').first()
  ).toBeVisible()
  failed = false
  await page.getByRole('alert').getByRole('button', { name: 'Retry' }).click()
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(page.locator('[data-home-request-total]')).toHaveText('--')
  expect(fixture.unexpected).toEqual([])
})

for (const theme of ['light', 'dark'])
  for (const width of [1440, 390]) {
    test(`order overview, details and page export ${theme} ${width}`, async ({
      page,
    }, testInfo) => {
      await page.emulateMedia({ reducedMotion: 'reduce' })
      await page.setViewportSize({ width, height: 1000 })
      const fixture = await nativeCoreFixture(page, theme)
      await page.route('**/api/user/topup?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              total: 32,
              items: [
                {
                  id: 8,
                  user_id: 23,
                  trade_no: 'ORDER-native',
                  amount: 10,
                  money: 73,
                  payment_provider: 'gmpay',
                  payment_method: 'gmpay:usdt.tron',
                  status: 'success',
                  create_time: 1785114000,
                  complete_time: 1785114100,
                },
              ],
            },
          },
        })
      )
      await page.goto('/orders')
      const row = page
        .locator('[data-order-row]')
        .filter({ hasText: 'ORDER-native', visible: true })
      await expect(row).toContainText('23')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('orders-list.png'),
        fullPage: true,
      })
      await row.getByRole('button', { name: 'Order details' }).click()
      await expect(page.getByRole('dialog')).toContainText('Completed at')
      await expect(page.getByRole('dialog')).toContainText('ORDER-native')
      await expect(page.getByRole('dialog')).toHaveCSS('opacity', '1')
      await page.screenshot({
        path: testInfo.outputPath('orders-detail.png'),
        fullPage: true,
      })
      await page
        .getByRole('dialog')
        .getByRole('button', { name: 'Close', exact: true })
        .click()
      await page
        .getByRole('button', { name: 'Export current page', exact: true })
        .click()
      await expect(page.getByRole('dialog')).toContainText(
        'Other pages are not included'
      )
      await page.screenshot({
        path: testInfo.outputPath('orders-export.png'),
      })
      const downloaded = page.waitForEvent('download')
      await page
        .getByRole('dialog')
        .getByRole('button', { name: 'Export current page', exact: true })
        .click()
      expect((await downloaded).suggestedFilename()).toBe(
        'ren2hub-orders-page-1.csv'
      )
      await page.getByRole('tab', { name: 'Overview', exact: true }).click()
      await expect(page.getByRole('tabpanel')).toContainText(
        'not platform-wide statistics'
      )
      await expect(page.getByRole('tabpanel')).toContainText('32')
      await expect(
        page.getByRole('tabpanel').getByText('Not available', { exact: true })
      ).toHaveCount(2)
      expect(fixture.unexpected).toEqual([])
      expect(fixture.missingMessages).toEqual([])
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('orders-overview.png'),
        fullPage: true,
      })
    })
  }

test('order page filters remain visible and clearable after paging and refresh', async ({
  page,
}, testInfo) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const fixture = await nativeCoreFixture(page, 'light')
  let refreshed = false
  const loadedPages: number[] = []
  await page.route('**/api/user/topup?*', (route) => {
    const currentPage = Number(
      new URL(route.request().url()).searchParams.get('p')
    )
    loadedPages.push(currentPage)
    const firstPage = currentPage === 1
    return route.fulfill({
      json: {
        success: true,
        data: {
          total: 20,
          items: [
            {
              id: firstPage ? 1 : refreshed ? 3 : 2,
              user_id: 23,
              trade_no: firstPage
                ? 'ORDER-first'
                : refreshed
                  ? 'ORDER-refreshed'
                  : 'ORDER-second',
              amount: 10,
              money: 73,
              payment_provider: firstPage
                ? 'gmpay'
                : refreshed
                  ? 'stripe'
                  : 'epay',
              payment_method: firstPage
                ? 'gmpay:usdt.tron'
                : refreshed
                  ? 'stripe'
                  : 'alipay',
              status: firstPage ? 'success' : refreshed ? 'failed' : 'pending',
              create_time: 1785114000,
              complete_time: firstPage ? 1785114100 : 0,
            },
          ],
        },
      },
    })
  })
  await page.goto('/orders')
  const status = page.getByRole('combobox', {
    name: 'Status · current page',
    exact: true,
  })
  const method = page.getByRole('combobox', {
    name: 'Method · current page',
    exact: true,
  })
  const visibleRows = page.locator('[data-order-row]').filter({ visible: true })
  async function selectFilter(control: typeof status, option: string) {
    await control.click()
    await page.getByRole('option', { name: option, exact: true }).click()
  }
  await expect(visibleRows).toContainText('ORDER-first')
  await selectFilter(status, 'Completed')
  await selectFilter(method, 'gmpay:usdt.tron')
  await page
    .getByRole('button', { name: 'Next', exact: true })
    .filter({ visible: true })
    .click()
  await expect.poll(() => loadedPages).toEqual([1, 2])
  await expect(
    page.getByRole('button', { name: 'Refresh', exact: true })
  ).toBeEnabled()
  await expect(status.locator('span.truncate')).toHaveText('Completed')
  await expect(method.locator('span.truncate')).toHaveText('gmpay:usdt.tron')
  await expect(visibleRows).toHaveCount(0)
  await page.screenshot({
    path: testInfo.outputPath('orders-retained-filters-empty.png'),
    fullPage: true,
  })
  await selectFilter(status, 'All')
  await expect(visibleRows).toHaveCount(0)
  await selectFilter(method, 'All')
  await expect(visibleRows).toContainText('ORDER-second')
  await expect(page.getByRole('listbox')).toHaveCount(0)
  await page.screenshot({
    path: testInfo.outputPath('orders-cleared-filters-restored.png'),
    fullPage: true,
  })

  await selectFilter(status, 'Pending')
  await selectFilter(method, 'alipay')
  refreshed = true
  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  await expect.poll(() => loadedPages).toEqual([1, 2, 2])
  await expect(
    page.getByRole('button', { name: 'Refresh', exact: true })
  ).toBeEnabled()
  await expect(status.locator('span.truncate')).toHaveText('Pending')
  await expect(method.locator('span.truncate')).toHaveText('alipay')
  await expect(visibleRows).toHaveCount(0)
  await selectFilter(status, 'All')
  await expect(visibleRows).toHaveCount(0)
  await selectFilter(method, 'All')
  await expect(visibleRows).toContainText('ORDER-refreshed')
  expect(fixture.writes.filter(({ path }) => path.includes('/topup'))).toEqual(
    []
  )
  expect(fixture.errors).toEqual([])
  expect(fixture.missingMessages).toEqual([])
})
