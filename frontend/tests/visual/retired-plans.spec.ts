import { expect, test } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const viewport of ['desktop', 'mobile'] as const) {
    test(`${theme} ${viewport} retired plans use 404 and wallet remains available`, async ({
      page,
    }) => {
      await page.setViewportSize(
        viewport === 'desktop'
          ? { width: 1440, height: 900 }
          : { width: 390, height: 844 }
      )
      await configureStablePage(page, { theme, authenticated: true })
      const retiredRequests: string[] = []
      page.on('request', (request) => {
        const path = new URL(request.url()).pathname
        if (
          path.startsWith('/api/subscription') ||
          path.startsWith('/api/plan/')
        )
          retiredRequests.push(path)
      })

      for (const path of [
        '/subscription',
        '/plan-management',
        '/plans',
        '/console/subscription',
        '/console/plan-management',
        '/console/plans',
      ]) {
        await page.goto(path, { waitUntil: 'domcontentloaded' })
        const notFound = page.locator('[data-handdrawn-scope="not-found"]')
        await expect(notFound).toBeVisible()
        await expect(notFound).toContainText('404')
        await assertNoHorizontalOverflow(page)
        const home = notFound.getByRole('link')
        await home.focus()
        await expect(home).toBeFocused()
      }

      await page.goto('/wallet', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)
      await expect(
        page.locator('[data-handdrawn="page-hero"] h1')
      ).toBeVisible()
      const menuTrigger = page.locator(
        '[aria-controls="console-mobile-navigation"]'
      )
      if (viewport === 'mobile') await menuTrigger.click()
      await expect(
        page.locator(
          viewport === 'mobile'
            ? '[data-console-mobile-navigation]'
            : 'aside[data-handdrawn="navigation"]'
        )
      ).toBeVisible()
      await expect(
        page.locator('[data-console-nav-item="subscription"]')
      ).toHaveCount(0)
      await expect(
        page.locator('[data-console-nav-item="plan-management"]')
      ).toHaveCount(0)
      if (viewport === 'mobile') {
        await page.keyboard.press('Escape')
        await expect(
          page.locator('[data-console-mobile-navigation]')
        ).toHaveCount(0)
        await expect(menuTrigger).toBeFocused()
      }
      await assertNoHorizontalOverflow(page)
      expect(retiredRequests).toEqual([])
    })
  }
}
