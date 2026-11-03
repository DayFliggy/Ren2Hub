import { expect, test, type Locator, type Page } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

const drawerSelector = '[data-console-mobile-navigation]'
const menuSelector = '[aria-controls="console-mobile-navigation"]'

async function delayFonts(page: Page) {
  let releaseFonts!: () => void
  const pendingFonts = new Promise<void>((resolve) => {
    releaseFonts = resolve
  })
  await page.route('**/*.woff2', async (route) => {
    await pendingFonts
    await route.continue()
  })
  return releaseFonts
}

async function expectItemInDrawer(item: Locator) {
  await expect
    .poll(() =>
      item.evaluate((element) => {
        const bounds = element.getBoundingClientRect()
        const viewport = element
          .closest('[data-console-mobile-navigation]')!
          .parentElement!.getBoundingClientRect()
        return (
          bounds.left >= viewport.left &&
          bounds.right <= viewport.right &&
          bounds.top >= viewport.top &&
          bounds.bottom <= viewport.bottom
        )
      })
    )
    .toBe(true)
}

async function openUsersWithPendingFonts(page: Page, theme: VisualTheme) {
  await configureStablePage(page, { theme, user: { role: 100 } })
  await page.setViewportSize({ width: 375, height: 667 })
  await page.goto('/users', { waitUntil: 'domcontentloaded' })
  await page.locator(menuSelector).click()
  const user = page.locator(`${drawerSelector} a[href="/users"]`)
  await expect(user).toHaveAttribute('aria-current', 'page')
  await user.scrollIntoViewIfNeeded()
  await expectItemInDrawer(user)
  await expect
    .poll(() => page.evaluate(() => document.fonts.status))
    .toBe('loading')
}

for (const theme of ['light', 'dark'] as VisualTheme[]) {
  for (const selection of ['click', 'Tab then Enter'] as const) {
    test(`pending fonts preserve drawer navigation (${theme}, ${selection})`, async ({
      page,
    }) => {
      const releaseFonts = await delayFonts(page)
      try {
        await openUsersWithPendingFonts(page, theme)
        const drawer = page.locator(drawerSelector)
        const redemption = drawer.locator('a[href="/redemption-codes"]')
        if (selection === 'click') {
          await redemption.click()
        } else {
          await drawer.locator('a[href="/users"]').focus()
          await page.keyboard.press('Tab')
          await expect(redemption).toBeFocused()
          await expectItemInDrawer(redemption)
          await page.keyboard.press('Enter')
        }
        await expect(page).toHaveURL(/\/redemption-codes$/)
        await expect(drawer).toHaveCount(0)
        await expect(page.locator(menuSelector)).toBeFocused()
        await page.locator(menuSelector).click()
        await expect(redemption).toHaveAttribute('aria-current', 'page')
        releaseFonts()
        await waitForStablePage(page)
        await redemption.scrollIntoViewIfNeeded()
        await expectItemInDrawer(redemption)
        await assertNoHorizontalOverflow(page)
      } finally {
        releaseFonts()
      }
    })
  }

  test(`loading fonts preserves manual drawer scroll and focus (${theme})`, async ({
    page,
  }) => {
    const releaseFonts = await delayFonts(page)
    try {
      await openUsersWithPendingFonts(page, theme)
      const drawer = page.locator(drawerSelector)
      const scroller = drawer.locator('..')
      await drawer.locator('a[href="/users"]').focus()
      await scroller.evaluate((element) => {
        element.scrollTop = 0
      })
      releaseFonts()
      await waitForStablePage(page)
      expect(await scroller.evaluate((element) => element.scrollTop)).toBe(0)
      await expect(drawer.locator('a[href="/users"]')).toBeFocused()
      await expect(drawer.locator('a[href="/users"]')).toHaveAttribute(
        'aria-current',
        'page'
      )
      await assertNoHorizontalOverflow(page)
    } finally {
      releaseFonts()
    }
  })

  test(`fonts completing after the drawer closes do not steal focus (${theme})`, async ({
    page,
  }) => {
    const releaseFonts = await delayFonts(page)
    try {
      await openUsersWithPendingFonts(page, theme)
      await page.keyboard.press('Escape')
      await expect(page.locator(drawerSelector)).toHaveCount(0)
      await expect(page.locator(menuSelector)).toBeFocused()
      releaseFonts()
      await waitForStablePage(page)
      await expect(page.locator(menuSelector)).toBeFocused()
      await expect(page.locator('#app')).toHaveJSProperty('inert', false)
    } finally {
      releaseFonts()
    }
  })
}
