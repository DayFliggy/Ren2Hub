import { expect, test, type Page } from '@playwright/test'

import {
  configureStablePage,
  waitForStablePage,
  captureEvidence,
} from './fixtures'

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

test('mobile drawer exposes every group with usable targets and real links', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'dark' })
  await page.setViewportSize({ width: 375, height: 667 })
  await page.goto('/models')
  await waitForStablePage(page)

  const trigger = page.locator('[aria-controls="console-mobile-navigation"]')
  await trigger.click()
  const drawer = page.locator('[data-console-mobile-navigation]')
  const items = drawer.locator('[data-console-nav-item]')
  expect(await items.count()).toBeGreaterThan(15)
  for (const item of await items.all()) {
    expect((await item.boundingBox())!.height).toBeGreaterThanOrEqual(44)
  }
  for (const href of await drawer
    .locator('a')
    .evaluateAll((links) => links.map((link) => link.getAttribute('href'))))
    expect(href).toMatch(/^\//)
  await expect(
    drawer.locator('[data-console-nav-item="models"]')
  ).toHaveAttribute('aria-current', 'page')
  await expect(drawer.locator('[data-console-nav-item="docs"]')).toBeDisabled()
  const last = drawer.locator('a[data-console-nav-item]').last()
  const lastName = await last.getAttribute('data-console-nav-item')
  const lastHref = await last.getAttribute('href')
  expect(lastHref).toMatch(/^\//)
  await last.click()
  // Settings group routes redirect to their default nested section.
  await expect(page).toHaveURL(new RegExp(`${lastHref}(?:/|$)`))
  await waitForStablePage(page)
  await expect(drawer).toHaveCount(0)
  await trigger.click()
  await expect(
    drawer.locator(`[data-console-nav-item="${lastName}"]`)
  ).toHaveAttribute('aria-current', 'page')
})

for (const theme of ['light', 'dark'] as const) {
  test(`drawer labels and disabled explanations stay separate with enlarged English text (${theme})`, async ({
    page,
  }, testInfo) => {
    await configureStablePage(page, { theme, locale: 'en' })
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/dashboard')
    await waitForStablePage(page)
    await page.locator('[aria-controls="console-mobile-navigation"]').click()
    const drawer = page.locator('[data-console-mobile-navigation]')
    const docs = drawer.locator('[data-console-nav-item="docs"]')
    await expect(docs).toBeDisabled()
    await expect(docs).not.toHaveAttribute('aria-current')
    await expect(
      drawer.locator('[data-console-nav-item="dashboard"]')
    ).toHaveAttribute('aria-current', 'page')

    for (const fontSize of [16, 32]) {
      await page.evaluate((size) => {
        document.documentElement.style.fontSize = `${size}px`
      }, fontSize)
      for (const width of [320, 390]) {
        await page.setViewportSize({ width, height: 844 })
        await docs.scrollIntoViewIfNeeded()
        const geometry = await docs.evaluate((button) => {
          const box = button.getBoundingClientRect()
          const ranges = [...button.querySelectorAll('span')]
            .filter((span) => !span.querySelector('span'))
            .flatMap((span) => {
              const range = document.createRange()
              range.selectNodeContents(span)
              return [...range.getClientRects()].map((rect) => ({
                left: rect.left,
                right: rect.right,
                top: rect.top,
                bottom: rect.bottom,
              }))
            })
          return {
            ranges,
            box: {
              left: box.left,
              right: box.right,
              top: box.top,
              bottom: box.bottom,
            },
            overflow: document.documentElement.scrollWidth - innerWidth,
          }
        })
        const context = `${theme}, ${width}px, font ${fontSize}px`
        expect(geometry.ranges.length, context).toBeGreaterThanOrEqual(2)
        expect(geometry.overflow, context).toBeLessThanOrEqual(1)
        for (const [index, rect] of geometry.ranges.entries()) {
          expect(rect.left, context).toBeGreaterThanOrEqual(geometry.box.left)
          expect(rect.right, context).toBeLessThanOrEqual(geometry.box.right)
          expect(rect.top, context).toBeGreaterThanOrEqual(geometry.box.top)
          expect(rect.bottom, context).toBeLessThanOrEqual(geometry.box.bottom)
          for (const other of geometry.ranges.slice(index + 1)) {
            const intersects =
              Math.min(rect.right, other.right) -
                Math.max(rect.left, other.left) >
                1 &&
              Math.min(rect.bottom, other.bottom) -
                Math.max(rect.top, other.top) >
                1
            expect(intersects, context).toBe(false)
          }
        }
      }
    }
    await captureEvidence(page, {
      path: testInfo.outputPath(`drawer-${theme}-font200.png`),
    })
    await page.keyboard.press('Escape')
    await expect(drawer).toHaveCount(0)
    await expect(
      page.locator('[aria-controls="console-mobile-navigation"]')
    ).toBeFocused()
  })
}

test('mobile drawer preserves manual scrolling until it closes at the desktop boundary', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'dark', user: { role: 100 } })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/users')
  await waitForStablePage(page)

  const trigger = page.locator('[aria-controls="console-mobile-navigation"]')
  const drawer = page.locator('[data-console-mobile-navigation]')
  await expect(trigger).toBeHidden()
  await page.setViewportSize({ width: 768, height: 1024 })
  await trigger.click()
  const active = drawer.locator('[data-console-nav-item="user-management"]')
  await expect(active).toHaveAttribute('aria-current', 'page')
  await active.scrollIntoViewIfNeeded()
  const scroller = drawer.locator('..')
  await scroller.hover()
  await page.mouse.wheel(0, 180)
  await expect
    .poll(() => scroller.evaluate((element) => element.scrollTop))
    .toBeGreaterThan(0)
  const scrollTop = await scroller.evaluate((element) => element.scrollTop)
  await page.setViewportSize({ width: 760, height: 1024 })
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
      )
  )
  expect(await scroller.evaluate((element) => element.scrollTop)).toBe(
    scrollTop
  )
  await page.setViewportSize({ width: 1024, height: 900 })
  await expect(drawer).toHaveCount(0)
  await expect(trigger).toHaveAttribute('aria-expanded', 'false')
  await expect(page.locator('[data-console-scroll-zone]')).toHaveCSS(
    'overflow-y',
    'auto'
  )
  await expect(
    page.locator('aside a[data-console-nav-item="user-management"]')
  ).toBeFocused()
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
