import { expect, test, type Locator, type Page } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import { captureUI } from './ui-coverage'

// Historical console-shell and input-composition assertions, using current
// native contracts. No retired /api/next fixture is restored.
const widths = [320, 639, 640, 767, 768, 1023, 1024, 1279, 1280, 1920]
const viewports = [
  ...widths.map((width) => ({ width, height: 900 })),
  { width: 640, height: 360 },
]
const scenarios = [
  {
    locale: 'zh-CN',
    theme: 'light',
    create: '新建渠道',
    models: '选择模型或添加自定义模型',
  },
  {
    locale: 'en',
    theme: 'dark',
    create: 'New channel',
    models: 'Select or add a custom model',
  },
] as const

async function resize(page: Page, viewport: { width: number; height: number }) {
  await page.setViewportSize(viewport)
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
      )
  )
}

async function assertNavigation(page: Page) {
  const desktop = page.locator('[data-top-nav="desktop"]')
  if (await desktop.isVisible()) {
    const geometry = await desktop.evaluate((menu) => {
      const controls = menu.parentElement!.nextElementSibling!
      const bar = menu.closest('header')!
      const textLines = [
        ...menu.querySelectorAll(':scope > div > button'),
      ].flatMap((button) =>
        [...button.childNodes]
          .filter(
            (node) =>
              node.nodeType === Node.TEXT_NODE && node.textContent!.trim()
          )
          .map((node) => {
            const range = document.createRange()
            range.selectNodeContents(node)
            return range.getClientRects().length
          })
      )
      return {
        menuRight: menu.getBoundingClientRect().right,
        controlsLeft: controls.getBoundingClientRect().left,
        controlsRight: controls.getBoundingClientRect().right,
        barRight: bar.getBoundingClientRect().right,
        textLines,
      }
    })
    expect(geometry.textLines.length).toBeGreaterThanOrEqual(4)
    expect(geometry.textLines.every((lines) => lines === 1)).toBe(true)
    expect(geometry.menuRight).toBeLessThanOrEqual(geometry.controlsLeft + 1)
    expect(geometry.controlsRight).toBeLessThanOrEqual(geometry.barRight + 1)
  } else {
    const strip = page.locator('nav[data-handdrawn="navigation-strip"]')
    await expect(strip).toBeVisible()
    const active = strip.locator('[aria-current="page"]')
    await expect
      .poll(() =>
        active.evaluate((element) => {
          const item = element.getBoundingClientRect()
          const parent = element.parentElement!.getBoundingClientRect()
          return item.left >= parent.left - 1 && item.right <= parent.right + 1
        })
      )
      .toBe(true)
    const scroller = active.locator('..')
    const dimensions = await scroller.evaluate((element) => ({
      overflow: getComputedStyle(element).overflowX,
      width: element.clientWidth,
      scroll: element.scrollWidth,
      touchTargets: [...element.children].map(
        (child) => child.getBoundingClientRect().height
      ),
    }))
    expect(dimensions.overflow).toBe('auto')
    expect(dimensions.touchTargets.every((height) => height >= 44)).toBe(true)
    if (dimensions.scroll > dimensions.width + 1) {
      expect(
        await strip.locator('[data-nav-scroll-fade]').count()
      ).toBeGreaterThan(0)
    }
  }
}

async function assertTableAlignment(page: Page) {
  const table = page.locator('.key-list-table')
  await expect(table.locator('.data-table-body-viewport tbody tr')).toHaveCount(
    1
  )
  await expect
    .poll(() =>
      table.evaluate((element) => {
        const header = [
          ...element.querySelectorAll('.data-table-header-clip thead td'),
        ]
        const cells = [
          ...element.querySelectorAll(
            '.data-table-body-viewport tbody tr:first-child > td'
          ),
        ]
        if (header.length !== cells.length || header.length < 4)
          return Number.POSITIVE_INFINITY
        return Math.max(
          ...header.map((head, index) => {
            const h = head.getBoundingClientRect(),
              cell = cells[index]!.getBoundingClientRect()
            return Math.max(
              Math.abs(h.left - cell.left),
              Math.abs(h.width - cell.width)
            )
          })
        )
      })
    )
    .toBeLessThanOrEqual(2)
}

async function assertModalViewport(page: Page, dialog: Locator) {
  const panel = dialog.locator('[data-handdrawn="modal"]')
  const viewport = page.viewportSize()!
  const box = (await panel.boundingBox())!
  expect(box.x).toBeGreaterThanOrEqual(-1)
  expect(box.y).toBeGreaterThanOrEqual(-1)
  expect(box.x + box.width).toBeLessThanOrEqual(viewport.width + 1)
  expect(box.y + box.height).toBeLessThanOrEqual(viewport.height + 1)
  const overflow = await panel.evaluate((el) => ({
    width: el.scrollWidth - el.clientWidth,
    body: el.querySelector('.subtle-scroll')!.clientHeight,
  }))
  expect(overflow.width).toBeLessThanOrEqual(1)
  expect(overflow.body).toBeGreaterThan(40)
  const footer = panel.locator('footer')
  await expect(footer).toBeInViewport()
  await footer.getByRole('button').last().focus()
  await page.keyboard.press('Tab')
  expect(
    await panel.evaluate((el) => el.contains(document.activeElement))
  ).toBe(true)
}

async function composingKey(input: Locator, key: string) {
  const prevented = await input.evaluate((element, value) => {
    element.dispatchEvent(
      new CompositionEvent('compositionstart', { bubbles: true })
    )
    const event = new KeyboardEvent('keydown', {
      key: value,
      isComposing: true,
      bubbles: true,
      cancelable: true,
    })
    element.dispatchEvent(event)
    element.dispatchEvent(
      new CompositionEvent('compositionend', { bubbles: true })
    )
    return event.defaultPrevented
  }, key)
  expect(prevented).toBe(false)
}

for (const scenario of scenarios) {
  const { locale, theme, create, models } = scenario
  const label = `${locale}-${theme}`

  test(`navigation and split table geometry across all breakpoints ${label}`, async ({
    page,
  }, info) => {
    await configureStablePage(page, { theme, locale })
    await page.goto('/keys')
    await waitForStablePage(page)
    for (const viewport of viewports) {
      await test.step(`${viewport.width}x${viewport.height}`, async () => {
        await resize(page, viewport)
        await assertNavigation(page)
        const body = page.locator('.key-list-table .data-table-body-viewport')
        await body.evaluate((element) => {
          element.scrollLeft = 0
        })
        await assertTableAlignment(page)
        await body.evaluate((element) => {
          element.scrollLeft = element.scrollWidth
        })
        await assertTableAlignment(page)
        await assertNoHorizontalOverflow(page)
      })
    }
    await captureUI(page, info, 'table-and-navigation-short-screen')
  })

  test(`channel modal remains usable across all breakpoints ${label}`, async ({
    page,
  }, info) => {
    await configureStablePage(page, { theme, locale })
    await page.goto('/channels')
    await waitForStablePage(page)
    await page.getByRole('button', { name: create, exact: true }).click()
    const dialog = page.getByRole('dialog', { name: create, exact: true })
    await expect(dialog).toBeVisible()
    for (const viewport of viewports) {
      await test.step(`${viewport.width}x${viewport.height}`, async () => {
        await resize(page, viewport)
        await assertModalViewport(page, dialog)
        await assertNoHorizontalOverflow(page)
      })
    }
    await captureUI(page, info, 'channel-modal-short-screen')
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await expect(
      page.getByRole('button', { name: create, exact: true })
    ).toBeFocused()
  })

  test(`dashboard canvases resize without clipping their cards at breakpoints ${label}`, async ({
    page,
  }, info) => {
    await configureStablePage(page, { theme, locale })
    await page.goto('/dashboard')
    await waitForStablePage(page)
    const charts = page.locator(
      '[data-token-trend-card] [role="img"], [data-model-distribution-chart][role="img"]'
    )
    await expect(charts).toHaveCount(2)
    for (const viewport of viewports) {
      await test.step(`${viewport.width}x${viewport.height}`, async () => {
        await resize(page, viewport)
        await expect
          .poll(() =>
            charts.evaluateAll((elements) =>
              elements.every((element) => {
                const card = element.closest('section')!.getBoundingClientRect()
                const box = element.getBoundingClientRect()
                const canvas = element
                  .querySelector('canvas')
                  ?.getBoundingClientRect()
                return (
                  canvas &&
                  box.width > 80 &&
                  box.height > 80 &&
                  Math.abs(canvas.width - box.width) < 2 &&
                  box.left >= card.left &&
                  box.right <= card.right + 1
                )
              })
            )
          )
          .toBe(true)
        await assertNoHorizontalOverflow(page)
      })
    }
    expect(
      await charts.evaluateAll((elements) =>
        elements.every((element) => {
          const canvas = element.querySelector('canvas')!
          const pixels = canvas
            .getContext('2d')!
            .getImageData(0, 0, canvas.width, canvas.height).data
          let painted = 0
          for (let index = 3; index < pixels.length; index += 4)
            if (pixels[index]! > 0) painted++
          return painted > 100
        })
      )
    ).toBe(true)
    await captureUI(page, info, 'dashboard-chart-short-screen')
  })

  test(`IME keeps composed keys inside model input and command palette ${label}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width: 768, height: 900 })
    await configureStablePage(page, { theme, locale })
    await page.goto('/channels')
    await waitForStablePage(page)
    await page.getByRole('button', { name: create, exact: true }).click()
    const dialog = page.getByRole('dialog', { name: create, exact: true })
    const model = dialog.getByPlaceholder(models, { exact: true })
    await model.fill('中文模型')
    for (const key of ['Enter', ',', 'Escape']) {
      await composingKey(model, key)
      await expect(model).toHaveValue('中文模型')
      await expect(dialog).toBeVisible()
    }
    await model.press('Enter')
    await expect(
      dialog.getByRole('button', { name: 'Remove 中文模型', exact: true })
    ).toBeVisible()
    await captureUI(page, info, 'composed-model-committed')
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await page.keyboard.press('Control+k')
    const command = page.locator('input[name="command-search"]')
    await expect(command).toBeFocused()
    const active = await command.getAttribute('aria-activedescendant')
    for (const key of ['ArrowDown', 'ArrowUp', 'Enter', 'Escape']) {
      await composingKey(command, key)
      await expect(command).toBeFocused()
      await expect(command).toHaveAttribute('aria-activedescendant', active!)
      await expect(page).toHaveURL(/\/channels$/)
    }
    await command.press('Escape')
    await expect(command).toHaveCount(0)
  })
}

for (const rootSize of [16, 20]) {
  test(`delayed fonts preserve manual navigation at ${rootSize}px default text size`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await configureStablePage(page, { theme: 'light', locale: 'en' })
    // Emulate the resulting CSS root size of a user's default-font preference.
    await page.addInitScript((size) => {
      document.addEventListener(
        'DOMContentLoaded',
        () => {
          document.documentElement.style.fontSize = `${size}px`
        },
        { once: true }
      )
    }, rootSize)
    let release!: () => void
    const fonts = new Promise<void>((resolve) => {
      release = resolve
    })
    let requests = 0
    await page.route(/\.woff2(?:\?.*)?$/, async (route) => {
      requests++
      await fonts
      return route.fallback()
    })
    await page.goto('/keys', { waitUntil: 'domcontentloaded' })
    await expect(page.locator('.key-list')).toBeVisible()
    expect(
      await page.evaluate(
        () => getComputedStyle(document.documentElement).fontSize
      )
    ).toBe(`${rootSize}px`)
    await expect.poll(() => requests).toBeGreaterThan(0)
    const strip = page.locator(
      'nav[data-handdrawn="navigation-strip"] .nav-scroller'
    )
    await strip.evaluate((element) => {
      element.scrollLeft = element.scrollWidth
      element.dispatchEvent(new Event('scroll'))
    })
    const before = await strip.evaluate((element) => element.scrollLeft)
    expect(before).toBeGreaterThan(0)
    release()
    await page.evaluate(() => document.fonts.ready)
    await waitForStablePage(page)
    const position = await strip.evaluate((element) => ({
      left: element.scrollLeft,
      maximum: element.scrollWidth - element.clientWidth,
    }))
    expect(
      Math.abs(position.left - Math.min(before, position.maximum))
    ).toBeLessThanOrEqual(1)
    await assertTableAlignment(page)
    await assertNoHorizontalOverflow(page)
    await resize(page, { width: 1280, height: 900 })
    await assertNavigation(page)
    await assertTableAlignment(page)
    await assertNoHorizontalOverflow(page)
    await captureUI(page, info, `loaded-fonts-${rootSize}px`)
  })
}

for (const motion of ['reduce', 'no-preference'] as const) {
  test(`modal keyboard trap and restoration with motion ${motion}`, async ({
    page,
  }, info) => {
    await configureStablePage(page, { theme: 'dark', locale: 'en' })
    await page.emulateMedia({ reducedMotion: motion })
    await page.goto('/channels')
    const trigger = page.getByRole('button', {
      name: 'New channel',
      exact: true,
    })
    await expect(trigger).toBeVisible()
    await page.evaluate(() => document.fonts.ready)
    await trigger.focus()
    await page.keyboard.press('Enter')
    const dialog = page.getByRole('dialog', {
      name: 'New channel',
      exact: true,
    })
    await expect(dialog).toBeVisible()
    const panel = dialog.locator('[data-handdrawn="modal"]')
    const duration = await panel.evaluate((element) =>
      parseFloat(getComputedStyle(element).animationDuration)
    )
    if (motion === 'reduce') expect(duration).toBeLessThanOrEqual(0.001)
    else expect(duration).toBeGreaterThan(0)
    await expect
      .poll(() =>
        panel.evaluate((el) =>
          el
            .getAnimations()
            .every((animation) => animation.playState !== 'running')
        )
      )
      .toBe(true)
    const focusables = dialog.locator(
      'button:visible:not([disabled]), input:visible:not([disabled]), select:visible:not([disabled]), textarea:visible:not([disabled]), a[href]:visible'
    )
    await focusables.first().focus()
    await page.keyboard.press('Shift+Tab')
    await expect(focusables.last()).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(focusables.first()).toBeFocused()
    await captureUI(page, info, `modal-motion-${motion}`)
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await expect(trigger).toBeFocused()
  })
}

for (const { locale, theme, create } of scenarios) {
  test(`browser default font sizes preserve navigation, tables and modals ${locale}-${theme}`, async ({
    page,
  }, info) => {
    test.setTimeout(90_000)
    await configureStablePage(page, { theme, locale })
    const session = await page.context().newCDPSession(page)
    const sibling = await page.context().newPage()
    const measurements = []
    try {
      for (const standard of [16, 20]) {
        // Page.setFontSizes changes Chromium's default, including rem media
        // queries. No stylesheet or inline font-size simulates this setting.
        await session.send('Page.setFontSizes', {
          fontSizes: { standard },
        })
        await page.goto('about:blank')
        await expect(page.locator('html')).toHaveCSS(
          'font-size',
          `${standard}px`
        )
        await expect(sibling.locator('html')).toHaveCSS('font-size', '16px')
        await page.goto('/keys')
        await waitForStablePage(page)
        const root = await page.locator('html').evaluate((element) => ({
          computed: getComputedStyle(element).fontSize,
          inline: element.style.fontSize,
        }))
        expect(root).toEqual({ computed: `${standard}px`, inline: '' })
        measurements.push({ standard, root })
        for (const viewport of [
          { width: 390, height: 844 },
          { width: 1440, height: 900 },
        ]) {
          await resize(page, viewport)
          await assertNavigation(page)
          const body = page.locator('.key-list-table .data-table-body-viewport')
          for (const edge of ['start', 'end']) {
            await body.evaluate((element, position) => {
              element.scrollLeft =
                position === 'start' ? 0 : element.scrollWidth
            }, edge)
            await assertTableAlignment(page)
          }
          await assertNoHorizontalOverflow(page)
          if (viewport.width === 390)
            await captureUI(page, info, `browser-font-${standard}-table`)
        }

        await page.goto('/channels')
        await waitForStablePage(page)
        await expect(page.locator('html')).toHaveCSS(
          'font-size',
          `${standard}px`
        )
        const trigger = page.getByRole('button', {
          name: create,
          exact: true,
        })
        await trigger.click()
        const dialog = page.getByRole('dialog', { name: create, exact: true })
        for (const viewport of [
          { width: 390, height: 844 },
          { width: 1440, height: 900 },
        ]) {
          await resize(page, viewport)
          await assertModalViewport(page, dialog)
          await assertNoHorizontalOverflow(page)
        }
        await captureUI(page, info, `browser-font-${standard}-modal`)
        await page.keyboard.press('Escape')
        await expect(dialog).toHaveCount(0)
        await expect(trigger).toBeFocused()
      }
      await info.attach('browser-default-font-measurements', {
        body: JSON.stringify(measurements),
        contentType: 'application/json',
      })
    } finally {
      await session.send('Page.setFontSizes', { fontSizes: { standard: 16 } })
      await expect(page.locator('html')).toHaveCSS('font-size', '16px')
      await session.detach()
      await sibling.close()
    }
  })
}
