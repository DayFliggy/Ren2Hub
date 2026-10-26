import { expect, test, type Page } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

async function openPage(page: Page, path: string) {
  await page.goto(path, { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
}

async function expectAlignedTable(page: Page) {
  const table = page.locator('.data-table-shell').first()
  await expect(table).toBeVisible()
  const tracks = await table.evaluate((element) => {
    const widths = (selector: string) =>
      Array.from(element.querySelectorAll(selector)).map(
        (column) => column.getBoundingClientRect().width
      )
    return {
      header: widths('.data-table-header-clip col'),
      body: widths('.data-table-body-viewport col'),
    }
  })
  expect(tracks.header.length).toBeGreaterThan(0)
  expect(tracks.body.length).toBe(tracks.header.length)
  tracks.header.forEach((width, index) => {
    expect(Math.abs(width - tracks.body[index])).toBeLessThanOrEqual(1)
  })
}

for (const theme of ['light', 'dark'] as const) {
  test(`${theme} widescreen English navigation fits with either sidebar width`, async ({
    page,
  }) => {
    await configureStablePage(page, {
      theme,
      authenticated: true,
      locale: 'en',
    })
    await page.setViewportSize({ width: 3840, height: 2160 })
    await openPage(page, '/dashboard')
    const sidebar = page.locator('aside[data-handdrawn="navigation"]')
    for (const collapsed of [false, true]) {
      if (collapsed) {
        await sidebar.locator(':scope > div:last-child > button').click()
      }
      await expect
        .poll(async () => (await sidebar.boundingBox())!.width)
        .toBe(collapsed ? 80 : 275)
      const geometry = await page.evaluate(() => {
        const menu = document.querySelector('[data-top-nav="desktop"]')!
        const controls = menu.parentElement!.nextElementSibling!
        return {
          heights: [...menu.querySelectorAll(':scope > div > button')].map(
            (item) => item.getBoundingClientRect().height
          ),
          menuRight: menu.getBoundingClientRect().right,
          controlsLeft: controls.getBoundingClientRect().left,
        }
      })
      expect(geometry.heights.length).toBeGreaterThanOrEqual(5)
      expect(Math.max(...geometry.heights)).toBeLessThanOrEqual(45.5)
      expect(geometry.menuRight).toBeLessThanOrEqual(geometry.controlsLeft)
      await assertNoHorizontalOverflow(page)
    }
  })

  test(`${theme} console grows moderately and stops growing on ultrawide screens`, async ({
    page,
  }) => {
    test.setTimeout(90_000)
    await configureStablePage(page, { theme, authenticated: true })
    const widths: number[] = []
    for (const viewport of [
      { width: 1440, height: 900 },
      { width: 1920, height: 1080 },
      { width: 2560, height: 1440 },
      { width: 3440, height: 1440 },
      { width: 3840, height: 2160 },
    ]) {
      await page.setViewportSize(viewport)
      await openPage(page, '/dashboard')
      const box = await page.locator('.console-content').boundingBox()
      widths.push(box!.width)
      await assertNoHorizontalOverflow(page)
    }
    expect(widths[1]).toBeGreaterThan(widths[0] + 100)
    expect(widths[2]).toBeGreaterThan(widths[1] + 100)
    expect(widths[2]).toBeLessThanOrEqual(1760)
    expect(widths[3]).toBeCloseTo(widths[2], 0)
    expect(widths[4]).toBeCloseTo(widths[2], 0)

    await openPage(page, '/keys')
    const list = await page.locator('.console-content').boundingBox()
    expect(list!.width).toBeGreaterThanOrEqual(2000)
    expect(list!.width).toBeLessThanOrEqual(2080)
    await expectAlignedTable(page)
    await assertNoHorizontalOverflow(page)
  })

  test(`${theme} widescreen dialogs scale, remain usable and release sizing on public navigation`, async ({
    page,
  }) => {
    test.setTimeout(90_000)
    await configureStablePage(page, { theme, authenticated: true })
    const panelWidths: number[] = []
    for (const viewport of [
      { width: 1440, height: 900 },
      { width: 3840, height: 2160 },
      { width: 2560, height: 720 },
    ]) {
      await page.setViewportSize(viewport)
      await openPage(page, '/keys')
      const trigger = page.getByRole('button', {
        name: '创建令牌',
        exact: true,
      })
      await trigger.click()
      const dialog = page.getByRole('dialog')
      const panel = dialog.locator('[data-handdrawn="modal"]')
      await expect(panel).toBeVisible()
      const box = await panel.boundingBox()
      panelWidths.push(box!.width)
      expect(box!.x).toBeGreaterThanOrEqual(0)
      expect(box!.y).toBeGreaterThanOrEqual(0)
      expect(box!.y + box!.height).toBeLessThanOrEqual(viewport.height)
      await page.keyboard.press('Tab')
      expect(
        await dialog.evaluate((element) =>
          element.contains(document.activeElement)
        )
      ).toBe(true)
      await page.keyboard.press('Escape')
      await expect(dialog).toBeHidden()
      await expect(trigger).toBeFocused()
      await assertNoHorizontalOverflow(page)
    }
    expect(panelWidths[1]).toBeGreaterThan(panelWidths[0] * 1.2)
    expect(panelWidths[1]).toBeLessThanOrEqual(panelWidths[0] * 1.25 + 1)
    expect(panelWidths[2]).toBeLessThan(panelWidths[1])

    // Leaving the layout must also reset body-teleported/public components.
    await page.evaluate(async () => {
      const app = document.querySelector('#app') as HTMLElement & {
        __vue_app__: {
          config: {
            globalProperties: { $router: { push: (path: string) => unknown } }
          }
        }
      }
      await app.__vue_app__.config.globalProperties.$router.push('/')
    })
    await expect(page).toHaveURL(/\/$/)
    await expect(page.locator('html')).not.toHaveAttribute(
      'data-console-layout',
      ''
    )
    expect(
      await page
        .locator('html')
        .evaluate((element) =>
          Number.parseFloat(getComputedStyle(element).fontSize)
        )
    ).toBe(16)
    await assertNoHorizontalOverflow(page)
  })

  test(`${theme} phone and tablet lists keep their existing responsive layout`, async ({
    page,
  }) => {
    test.setTimeout(90_000)
    await configureStablePage(page, { theme, authenticated: true })
    for (const viewport of [
      { width: 375, height: 667 },
      { width: 768, height: 1024 },
      { width: 1024, height: 768 },
    ]) {
      await page.setViewportSize(viewport)
      await openPage(page, '/keys')
      await expect(page.locator('[data-key-mobile-row]').first()).toBeVisible()
      await expect(page.locator('.data-table-shell')).toBeHidden()
      await assertNoHorizontalOverflow(page)
    }
  })
}
