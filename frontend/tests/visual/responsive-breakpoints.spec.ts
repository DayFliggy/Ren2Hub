import { expect, test, type Page } from '@playwright/test'

import {
  configureStablePage,
  type VisualTheme,
  waitForStablePage,
} from './fixtures'

async function configureBrowserFont(
  page: Page,
  theme: VisualTheme,
  fontSize: number
) {
  await configureStablePage(page, { theme })
  // Change Chromium's default font setting, rather than the page's CSS or zoom.
  const session = await page.context().newCDPSession(page)
  await session.send('Page.setFontSizes', {
    fontSizes: { standard: fontSize },
  })
}

for (const theme of ['light', 'dark'] as VisualTheme[]) {
  for (const fontSize of [16, 20]) {
    const setting = `${theme}, browser default font ${fontSize}px`

    test(`home navigation gains space at the small-screen boundary (${setting})`, async ({
      page,
    }) => {
      await configureBrowserFont(page, theme, fontSize)
      await page.setViewportSize({ width: 639, height: 900 })
      await page.goto('/')
      await waitForStablePage(page)
      await expect(page.locator('html')).toHaveCSS('font-size', `${fontSize}px`)
      const nav = page.locator('.app-navbar nav')
      const brand = nav.locator('.home-brand')
      await expect(brand).toHaveCSS('justify-content', 'center')
      const narrowGap = await nav.evaluate((element) =>
        Number.parseFloat(getComputedStyle(element).columnGap)
      )

      await page.setViewportSize({ width: 640, height: 900 })
      await expect(brand).toHaveCSS('justify-content', 'flex-start')
      await expect
        .poll(() =>
          nav.evaluate((element) =>
            Number.parseFloat(getComputedStyle(element).columnGap)
          )
        )
        .toBeGreaterThan(narrowGap)
      const wideGap = await nav.evaluate((element) =>
        Number.parseFloat(getComputedStyle(element).columnGap)
      )

      await page.setViewportSize({ width: 700, height: 900 })
      await expect(brand).toHaveCSS('justify-content', 'flex-start')
      await expect
        .poll(() =>
          nav.evaluate((element) =>
            Number.parseFloat(getComputedStyle(element).columnGap)
          )
        )
        .toBe(wideGap)
    })

    test(`home menu and desktop navigation share the same resize boundary (${setting})`, async ({
      page,
    }) => {
      await configureBrowserFont(page, theme, fontSize)
      await page.setViewportSize({ width: 766, height: 900 })
      await page.goto('/')
      await waitForStablePage(page)
      await expect(page.locator('html')).toHaveCSS('font-size', `${fontSize}px`)
      const menu = page.locator(
        '.app-navbar button[aria-controls="mobile-navigation-drawer"]'
      )
      const drawer = page.locator('#mobile-navigation-drawer')
      await expect(menu).toBeVisible()
      await menu.click()
      await expect(menu).toHaveAttribute('aria-expanded', 'true')
      await expect(drawer.getByRole('dialog')).toBeVisible()

      await page.setViewportSize({ width: 767, height: 900 })
      await expect(menu).toBeVisible()
      await expect(menu).toHaveAttribute('aria-expanded', 'true')
      await expect(drawer.getByRole('dialog')).toBeVisible()

      await page.setViewportSize({ width: 768, height: 900 })
      await expect(menu).toHaveAttribute('aria-expanded', 'false')
      await expect(menu).toBeHidden()
      await expect(drawer).toHaveCount(0)

      for (const width of [900, 901]) {
        await page.setViewportSize({ width, height: 900 })
        await expect(menu).toBeHidden()
        await expect(drawer).toHaveCount(0)
      }

      await page.setViewportSize({ width: 767, height: 900 })
      await expect(menu).toBeVisible()
      await menu.click()
      await expect(drawer.getByRole('dialog')).toBeVisible()
    })

    test(`console switches between sidebar and navigation drawer at the desktop boundary (${setting})`, async ({
      page,
    }) => {
      await configureBrowserFont(page, theme, fontSize)
      await page.setViewportSize({ width: 1023, height: 900 })
      await page.goto('/dashboard')
      await waitForStablePage(page)
      await expect(page.locator('html')).toHaveCSS('font-size', `${fontSize}px`)
      const sidebar = page.locator('aside[data-handdrawn="navigation"]')
      const menu = page.locator('[aria-controls="console-mobile-navigation"]')
      const drawer = page.locator('[data-console-mobile-navigation]')
      await expect(sidebar).toBeHidden()
      await expect(menu).toBeVisible()
      await menu.click()
      await expect(
        drawer.locator('a[data-console-nav-item="keys"]')
      ).toHaveAttribute('href', '/keys')
      await expect(
        drawer.locator('[data-console-nav-item="keys"]')
      ).toBeVisible()

      for (const width of [1024, 1100]) {
        await page.setViewportSize({ width, height: 900 })
        await expect(sidebar).toBeVisible()
        await expect(menu).toHaveAttribute('aria-expanded', 'false')
        await expect(drawer).toHaveCount(0)
        const needsDrawer =
          fontSize === 20 && (width === 1024 || theme === 'dark')
        if (needsDrawer) {
          // Enlarged text uses the drawer when the desktop topbar is too narrow.
          // The full-width light bar has enough room at 1100px; Studio has margins.
          await expect(menu).toBeVisible()
          await menu.click()
          await expect(
            drawer.locator('a[data-console-nav-item="keys"]')
          ).toBeVisible()
          await page.keyboard.press('Escape')
          await expect(drawer).toHaveCount(0)
          await expect(menu).toBeFocused()
        } else {
          await expect(menu).toBeHidden()
        }
      }

      await page.setViewportSize({ width: 1440, height: 900 })
      await expect(sidebar).toBeVisible()
      await expect(menu).toBeHidden()
      await expect(drawer).toHaveCount(0)

      await page.setViewportSize({ width: 1023, height: 900 })
      await expect(sidebar).toBeHidden()
      await expect(menu).toBeVisible()
      await menu.click()
      await expect(
        drawer.locator('a[data-console-nav-item="keys"]')
      ).toBeVisible()
    })
  }
}
