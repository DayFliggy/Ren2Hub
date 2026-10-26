import { expect, test, type Locator, type Page } from '@playwright/test'

import {
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

const stripSelector = 'nav[data-handdrawn="navigation-strip"] .nav-scroller'

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

async function expectItemInStrip(item: Locator) {
  await expect
    .poll(() =>
      item.evaluate((element) => {
        const bounds = element.getBoundingClientRect()
        const viewport = element.parentElement!.getBoundingClientRect()
        return bounds.left >= viewport.left && bounds.right <= viewport.right
      })
    )
    .toBe(true)
}

async function openUsersWithPendingFonts(page: Page, theme: VisualTheme) {
  await configureStablePage(page, { theme, user: { role: 100 } })
  await page.setViewportSize({ width: 375, height: 667 })
  await page.goto('/users', { waitUntil: 'domcontentloaded' })
  await expect(
    page.locator(`${stripSelector} [aria-current="page"]`)
  ).toHaveAttribute('href', '/users')
  await expectItemInStrip(page.locator(`${stripSelector} a[href="/users"]`))
  await expect
    .poll(() => page.evaluate(() => document.fonts.status))
    .toBe('loading')
}

async function expectSelectedRedemption(page: Page) {
  const active = page.locator(`${stripSelector} [aria-current="page"]`)
  await expect(active).toHaveAttribute('href', '/redemption-codes')
  await expectItemInStrip(active)
  await expect
    .poll(() => page.evaluate(() => document.fonts.status))
    .toBe('loading')
}

for (const theme of ['light', 'dark'] as VisualTheme[]) {
  for (const selection of ['click', 'Tab then Enter'] as const) {
    test(`pending fonts keep a newly selected navigation item visible (${theme}, ${selection})`, async ({
      page,
    }) => {
      const releaseFonts = await delayFonts(page)
      try {
        await openUsersWithPendingFonts(page, theme)
        const scroller = page.locator(stripSelector)
        const redemption = scroller.locator('a[href="/redemption-codes"]')
        // The target is already fully visible, so selection needs no horizontal scroll.
        await expectItemInStrip(redemption)
        if (selection === 'click') {
          await redemption.click()
        } else {
          const beforeFocus = await scroller.evaluate(
            (element) => element.scrollLeft
          )
          await scroller.locator('a[href="/users"]').focus()
          await page.keyboard.press('Tab')
          await expect(redemption).toBeFocused()
          expect(await scroller.evaluate((element) => element.scrollLeft)).toBe(
            beforeFocus
          )
          await page.keyboard.press('Enter')
        }
        await expectSelectedRedemption(page)
        releaseFonts()
        await waitForStablePage(page)
        await expectItemInStrip(scroller.locator('[aria-current="page"]'))
      } finally {
        releaseFonts()
      }
    })
  }

  test(`selecting a page renews font correction after an earlier horizontal scroll (${theme})`, async ({
    page,
  }) => {
    const releaseFonts = await delayFonts(page)
    try {
      await openUsersWithPendingFonts(page, theme)
      const scroller = page.locator(stripSelector)
      const position = await scroller.evaluate((element) => ({
        left: element.scrollLeft,
        maximum: element.scrollWidth - element.clientWidth,
      }))
      // Retired navigation entries shorten the strip. Scroll toward available
      // space so this still exercises a real user gesture at either end.
      const delta = position.maximum - position.left >= 80 ? 80 : -80
      await scroller.hover()
      await page.mouse.wheel(delta, 0)
      await expect
        .poll(() => scroller.evaluate((element) => element.scrollLeft))
        .toBe(position.left + delta)
      const redemption = scroller.locator('a[href="/redemption-codes"]')
      await redemption.click()
      await expectSelectedRedemption(page)
      releaseFonts()
      await waitForStablePage(page)
      await expectItemInStrip(scroller.locator('[aria-current="page"]'))
    } finally {
      releaseFonts()
    }
  })

  test(`fonts keep a user's horizontal position after selecting another page (${theme})`, async ({
    page,
  }) => {
    const releaseFonts = await delayFonts(page)
    try {
      await openUsersWithPendingFonts(page, theme)
      const scroller = page.locator(stripSelector)
      await scroller.locator('a[href="/redemption-codes"]').click()
      await expectSelectedRedemption(page)
      await scroller.hover()
      await page.mouse.wheel(-2000, 0)
      await expect
        .poll(() => scroller.evaluate((element) => element.scrollLeft))
        .toBe(0)
      releaseFonts()
      await waitForStablePage(page)
      expect(await scroller.evaluate((element) => element.scrollLeft)).toBe(0)
    } finally {
      releaseFonts()
    }
  })
}
