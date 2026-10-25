import { expect, test } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import { captureUI } from './ui-coverage'

test.use({ viewport: { width: 390, height: 844 }, hasTouch: true })

for (const theme of ['light', 'dark'] as const) {
  test(`touch navigation, form focus and modal dismissal ${theme}`, async ({
    page,
  }, info) => {
    const fixture = await configureStablePage(page, { theme, locale: 'en' })
    await page.goto('/keys')
    await waitForStablePage(page)
    expect(await page.evaluate(() => navigator.maxTouchPoints)).toBeGreaterThan(
      0
    )
    await page.evaluate(() => {
      document.documentElement.dataset.touchEvents = '0'
      document.addEventListener('touchstart', () => {
        const root = document.documentElement
        root.dataset.touchEvents = String(Number(root.dataset.touchEvents) + 1)
      })
    })

    const channelLink = page.locator(
      '[data-handdrawn="navigation-strip"] a[href="/channels"]'
    )
    await channelLink.tap()
    await expect(page).toHaveURL(/\/channels$/)
    await waitForStablePage(page)
    const trigger = page.getByRole('button', {
      name: 'New channel',
      exact: true,
    })
    await trigger.tap()
    const dialog = page.getByRole('dialog', {
      name: 'New channel',
      exact: true,
    })
    await expect(dialog).toBeVisible()
    const name = dialog.getByRole('textbox', {
      name: 'Channel name *',
      exact: true,
    })
    await name.tap()
    await expect(name).toBeFocused()
    await assertNoHorizontalOverflow(page)
    await captureUI(page, info, 'touch-channel-modal', { scroll: false })
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).tap()
    await expect(dialog).toHaveCount(0)
    await expect(trigger).toBeFocused()
    expect(
      await page.evaluate(() =>
        Number(document.documentElement.dataset.touchEvents)
      )
    ).toBeGreaterThanOrEqual(4)
    expect(fixture.writes.map((request) => request.path)).toEqual([
      '/api/user/auth/refresh',
    ])
    await waitForStablePage(page)
  })
}
