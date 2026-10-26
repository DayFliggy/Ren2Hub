import { expect, test, type Page } from '@playwright/test'

import { configureStablePage, waitForStablePage } from './fixtures'

const DESKTOP_WIDTHS = [1024, 1100, 1180, 1280, 1366, 1440]

/** Top-bar geometry: menu item heights and the right controls' extent. */
async function measureTopbar(page: Page) {
  return page.evaluate(() => {
    const menu = document.querySelector('[data-top-nav="desktop"]')!
    const controls = menu.parentElement!.nextElementSibling!
    const bar = menu.closest('header')!
    return {
      heights: [...menu.querySelectorAll(':scope > div > button')].map(
        (item) => item.getBoundingClientRect().height
      ),
      menuRight: menu.getBoundingClientRect().right,
      controlsLeft: controls.getBoundingClientRect().left,
      controlsRight: controls.getBoundingClientRect().right,
      barRight: bar.getBoundingClientRect().right,
    }
  })
}

async function setSidebar(page: Page, collapsed: boolean) {
  const sidebar = page.locator('aside[data-handdrawn="navigation"]')
  const width = collapsed ? 64 : 220
  if ((await sidebar.boundingBox())!.width !== width) {
    await sidebar.locator(':scope > div:last-child > button').click()
  }
  await expect
    .poll(async () => (await sidebar.boundingBox())!.width)
    .toBe(width)
}

// The widest menu is English for an administrator (five entries); Chinese
// labels wrapped per character before they were held on one line.
for (const locale of ['zh-CN', 'en']) {
  for (const role of [100, 1]) {
    test(`top menu labels stay on one line (${locale}, role ${role})`, async ({
      page,
    }) => {
      await configureStablePage(page, {
        theme: 'light',
        locale,
        user: { role },
      })
      await page.addInitScript(
        (value) => localStorage.setItem('ren2hub_locale', value),
        locale
      )
      await page.setViewportSize({ width: 1024, height: 768 })
      await page.goto('/dashboard')
      await waitForStablePage(page)

      for (const collapsed of [false, true]) {
        await setSidebar(page, collapsed)
        for (const width of DESKTOP_WIDTHS) {
          await page.setViewportSize({ width, height: 768 })
          const bar = await measureTopbar(page)
          const context = `${width}px, sidebar ${collapsed ? 'collapsed' : 'expanded'}: ${JSON.stringify(bar)}`
          expect(bar.heights.length, context).toBe(role >= 100 ? 5 : 4)
          expect(Math.max(...bar.heights), context).toBeLessThanOrEqual(36.5)
          expect(bar.menuRight, context).toBeLessThanOrEqual(bar.controlsLeft)
          expect(bar.controlsRight, context).toBeLessThanOrEqual(bar.barRight)
        }
      }
    })
  }
}

test('mobile strips mark hidden items and keep the current page in view', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'dark' })
  await page.setViewportSize({ width: 375, height: 667 })
  // Models is the first console entry, so the strip starts unscrolled.
  await page.goto('/models')
  await waitForStablePage(page)

  const strip = page.locator('nav[data-handdrawn="navigation-strip"]')
  const scroller = strip.locator('[aria-current="page"]').locator('..')
  for (const item of await scroller.locator(':scope > *').all()) {
    expect((await item.boundingBox())!.height).toBeGreaterThanOrEqual(44)
  }
  // More items than the phone shows: the far edge fades, the near edge does not.
  await expect(strip.locator('[data-nav-scroll-fade="end"]')).toBeVisible()
  await expect(strip.locator('[data-nav-scroll-fade="start"]')).toHaveCount(0)

  // Loading the last entry's page centres it as far as the strip scrolls.
  const last = scroller.locator(':scope > a').last()
  const lastName = await last.getAttribute('data-console-nav-item')
  const lastHref = await last.getAttribute('href')
  expect(lastHref).toMatch(/^\//)
  await page.goto(lastHref!)
  await waitForStablePage(page)
  const active = strip.locator('[aria-current="page"]')
  await expect(active).toHaveAttribute('data-console-nav-item', lastName!)
  const activeBox = (await active.boundingBox())!
  const stripBox = (await scroller.boundingBox())!
  expect(activeBox.x).toBeGreaterThanOrEqual(stripBox.x)
  expect(activeBox.x + activeBox.width).toBeLessThanOrEqual(
    stripBox.x + stripBox.width
  )
  await expect(strip.locator('[data-nav-scroll-fade="start"]')).toBeVisible()
})

test('revealing mobile navigation shows the current page and preserves manual scrolling', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'dark', user: { role: 100 } })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/users')
  await waitForStablePage(page)

  const strip = page.locator('nav[data-handdrawn="navigation-strip"]')
  const active = strip.locator('[aria-current="page"]')
  const scroller = active.locator('..')
  await expect(strip).toBeHidden()
  await page.setViewportSize({ width: 768, height: 1024 })
  await expect(strip).toBeVisible()
  await expect
    .poll(() =>
      active.evaluate((element) => {
        const item = element.getBoundingClientRect()
        const viewport = element.parentElement!.getBoundingClientRect()
        return item.left >= viewport.left && item.right <= viewport.right
      })
    )
    .toBe(true)

  await scroller.hover()
  await page.mouse.wheel(-2000, 0)
  await expect
    .poll(() => scroller.evaluate((element) => element.scrollLeft))
    .toBe(0)
  // A small resize while the strip remains visible must respect the user's
  // position instead of jumping back to the active page.
  await page.setViewportSize({ width: 760, height: 1024 })
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
      )
  )
  expect(await scroller.evaluate((element) => element.scrollLeft)).toBe(0)
})

test('dashboard content scrolls clear of the floating support button', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  await page.setViewportSize({ width: 1024, height: 768 })
  await page.goto('/dashboard')
  await waitForStablePage(page)

  await page
    .locator('[data-console-scroll-zone]')
    .evaluate((zone) => zone.scrollTo({ top: zone.scrollHeight }))
  const trigger = (await page
    .getByRole('button', { name: '联系我们' })
    .boundingBox())!
  const content = (await page.getByRole('tabpanel').boundingBox())!
  expect(content.y + content.height).toBeLessThanOrEqual(trigger.y)
})

test('console navigation entries are real links', async ({ page }) => {
  await configureStablePage(page, { theme: 'light' })
  await page.goto('/keys')
  await waitForStablePage(page)

  // A RouterLink given an explicit undefined href rendered without one, which
  // left the entries unreachable by Tab and unopenable in a new tab.
  const links = page.locator('aside a[data-console-nav-item]')
  expect(await links.count()).toBeGreaterThan(3)
  for (const href of await links.evaluateAll((items) =>
    items.map((item) => item.getAttribute('href'))
  )) {
    expect(href).toMatch(/^\//)
  }
  await expect(
    page.locator('aside a[data-console-nav-item="keys"]')
  ).toHaveAttribute('href', '/keys')
})
