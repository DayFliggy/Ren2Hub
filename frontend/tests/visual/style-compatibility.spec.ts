import { expect, test, type Locator } from '@playwright/test'

import {
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

async function finishAnimations(locator: Locator) {
  await locator.evaluate((element) => {
    for (const animation of element.getAnimations()) {
      if (animation.effect?.getTiming().iterations !== Infinity)
        animation.finish()
    }
  })
}

for (const theme of ['light', 'dark'] as VisualTheme[]) {
  test(`public containers and nested console text keep their line boxes (${theme})`, async ({
    page,
  }) => {
    await configureStablePage(page, { theme })
    for (const width of [390, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      await page.goto('/')
      await waitForStablePage(page)
      const container = page.locator('.app-navbar nav')
      const geometry = await container.evaluate((element) => {
        const rect = element.getBoundingClientRect()
        const style = getComputedStyle(element)
        const brand = element.querySelector('.home-brand')!
        return {
          x: rect.x,
          width: rect.width,
          left: Number.parseFloat(style.paddingLeft),
          right: Number.parseFloat(style.paddingRight),
          brandX: brand.getBoundingClientRect().x,
        }
      })
      const gutter = width < 640 ? 16 : 32
      expect(geometry.x).toBeCloseTo((width - geometry.width) / 2, 1)
      expect(geometry.left).toBe(gutter)
      expect(geometry.right).toBe(gutter)
      expect(geometry.brandX).toBeCloseTo(geometry.x + gutter, 1)
      await expect(page.getByText('稳定与平价、人人用得起。')).toHaveCSS(
        'line-height',
        width < 640 ? '24.375px' : '28px'
      )

      await page.goto('/dashboard')
      await waitForStablePage(page)
      const heading = page.locator('[data-handdrawn="page-hero"] h1')
      await expect(heading).toHaveCSS(
        'line-height',
        width < 1024 ? '45px' : '48px'
      )
      const badge = page
        .locator('span:visible')
        .filter({ hasText: /^即将上线$/ })
        .first()
      await expect(badge).toBeVisible()
      await expect(badge).toHaveCSS(
        'line-height',
        width < 640 ? '16px' : '20px'
      )
    }
  })

  for (const motion of ['no-preference', 'reduce'] as const) {
    test(`menu, check-in stamp and pressed controls keep their geometry (${theme}, ${motion})`, async ({
      page,
    }) => {
      await configureStablePage(page, { theme })
      await page.goto('/dashboard')
      await waitForStablePage(page)
      await page.emulateMedia({ reducedMotion: motion })

      const trigger = page
        .locator('[data-top-nav="desktop"] button[aria-expanded="false"]')
        .first()
      const triggerBox = (await trigger.boundingBox())!
      await trigger.click()
      const menu = page.locator(
        '[data-top-nav="desktop"] [data-handdrawn="menu"]'
      )
      await expect(menu).toBeVisible()
      await finishAnimations(menu)
      const menuBox = (await menu.boundingBox())!
      // The original entrance animation replaces the centering transform;
      // reduced motion removes the animation and retains centering.
      const anchor = triggerBox.x + triggerBox.width / 2
      expect(menuBox.x).toBeCloseTo(
        anchor - (motion === 'reduce' ? menuBox.width / 2 : 0),
        1
      )

      await page.goto('/activity')
      await expect(page.locator('.checkin-stamp')).toBeVisible()
      await page.evaluate(() => document.fonts.ready)
      const stamp = await page.locator('.checkin-stamp').evaluate((element) => {
        const rect = element.getBoundingClientRect()
        return {
          width: rect.width,
          height: rect.height,
          layoutWidth: (element as HTMLElement).offsetWidth,
          layoutHeight: (element as HTMLElement).offsetHeight,
        }
      })
      if (theme === 'dark') {
        expect(stamp.width).toBeCloseTo(stamp.layoutWidth, 0)
        expect(stamp.height).toBeCloseTo(stamp.layoutHeight, 0)
      } else {
        expect(stamp.height).toBeGreaterThan(stamp.layoutHeight + 2)
      }

      const button = page.locator('button.touch-snappy:not(:disabled)').first()
      await expect(button).toBeVisible()
      await button.scrollIntoViewIfNeeded()
      const rest = (await button.boundingBox())!
      await page.mouse.move(rest.x + rest.width / 2, rest.y + rest.height / 2)
      await page.mouse.down()
      try {
        await finishAnimations(button)
        await expect
          .poll(async () => (await button.boundingBox())!.width / rest.width)
          .toBeCloseTo(motion === 'reduce' ? 1 : 0.98, 3)
        const pressed = (await button.boundingBox())!
        expect(pressed.height / rest.height).toBeCloseTo(
          motion === 'reduce' ? 1 : 0.98,
          2
        )
      } finally {
        // Release away from the button so this geometry check never claims a reward.
        await page.mouse.move(0, 0)
        await page.mouse.up()
      }
    })
  }
}

for (const manualScroll of [false, true]) {
  test(`mobile navigation handles delayed fonts${manualScroll ? ' without resetting a manual scroll' : ''}`, async ({
    page,
  }) => {
    await configureStablePage(page, { theme: 'dark', user: { role: 100 } })
    await page.setViewportSize({ width: 375, height: 667 })
    let releaseFonts!: () => void
    const pendingFonts = new Promise<void>((resolve) => {
      releaseFonts = resolve
    })
    await page.route('**/*.woff2', async (route) => {
      await pendingFonts
      await route.continue()
    })
    try {
      await page.goto('/system-settings', { waitUntil: 'domcontentloaded' })
      const active = page.locator(
        'nav[data-handdrawn="navigation-strip"] [aria-current="page"]'
      )
      await expect(active).toBeVisible()
      await expect
        .poll(() => page.evaluate(() => document.fonts.status))
        .toBe('loading')
      const scroller = active.locator('..')
      if (manualScroll) {
        await scroller.hover()
        await page.mouse.wheel(-2000, 0)
        await expect
          .poll(() => scroller.evaluate((element) => element.scrollLeft))
          .toBe(0)
      }
      releaseFonts()
      await waitForStablePage(page)
      if (manualScroll) {
        expect(await scroller.evaluate((element) => element.scrollLeft)).toBe(0)
      } else {
        await expect
          .poll(() =>
            active.evaluate((element) => {
              const item = element.getBoundingClientRect()
              const viewport = element.parentElement!.getBoundingClientRect()
              return item.left >= viewport.left && item.right <= viewport.right
            })
          )
          .toBe(true)
      }
    } finally {
      releaseFonts()
    }
  })
}
