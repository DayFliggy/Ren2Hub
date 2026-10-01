import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

const event = {
  event_id: 'settle:request-settled-001',
  request_id: 'request-settled-001',
  operation: 'settle',
  actor_id: 1,
  available_delta: '-250000',
  reserved_delta: '0',
  used_delta: '250000',
  token_available_delta: '-250000',
  token_reserved_delta: '0',
  token_used_delta: '250000',
  available_after: '9007199254740993',
  reserved_after: '0',
  used_after: '350000',
  reason: 'Settled charge',
  created_at: 1785114000,
}

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`wallet fund flow and failure ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      let eventsAvailable = true
      await page.route('**/api/billing/self/events?*', (route) =>
        eventsAvailable
          ? route.fulfill({
              json: { success: true, data: { items: [event], total: 1 } },
            })
          : route.fulfill({
              status: 503,
              json: { success: false, message: 'unavailable' },
            })
      )
      const failures: string[] = []
      page.on('pageerror', (error) => failures.push(error.message))
      await page.goto('/wallet')
      const history = page
        .locator('section')
        .filter({ has: page.getByRole('heading', { name: '资金流水' }) })
      await expect(
        history.getByText(event.event_id, { exact: true })
      ).toBeVisible()
      await expect(history.getByText('$0.500', { exact: true })).toBeVisible()
      await expect(page.getByRole('tab')).toHaveCount(0)
      await expect(page.getByText('在途预留')).toHaveCount(0)
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('wallet-fund-flow.png'),
        fullPage: true,
      })
      eventsAvailable = false
      await history.getByRole('button', { name: '刷新' }).click()
      await expect(history.getByRole('alert')).toContainText('账务数据暂不可用')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('wallet-error.png'),
        fullPage: true,
      })
      expect(failures).toEqual([])
    })
  }
}
