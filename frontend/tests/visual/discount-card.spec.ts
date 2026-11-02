import { expect, test, type Locator } from '@playwright/test'
import { nativeFixture } from '../contracts/native-core-fixture'
import {
  configureStablePage,
  minTextContrast,
  waitForStablePage,
} from './fixtures'

const response = nativeFixture<{ data: Record<string, unknown> }>(
  'usage-discount-self.get.json'
)
// Exercise the documented wire contract with all three active windows.
const combined = {
  ...response.data,
  factor: '0.95',
  global_factor: '0.97',
  fixed_rate: '0.01',
  site_rate: '0.02',
  applied_rate: '0.05',
  cap_applied: true,
  winner_window: 'month',
  windows: [
    {
      window: 'day',
      quota: 2500000,
      rate: '0.01',
      tiers: [
        { threshold_quota: '2500000', rate: '0.01' },
        { threshold_quota: '7500000', rate: '0.02' },
      ],
    },
    {
      window: 'week',
      quota: 62500000,
      rate: '0.03',
      tiers: [{ threshold_quota: '62500000', rate: '0.03' }],
    },
    {
      window: 'month',
      quota: 375000000,
      rate: '0.04',
      tiers: [{ threshold_quota: '375000000', rate: '0.04' }],
    },
  ],
}

async function expectAlignedWindows(card: Locator) {
  const windows = await card
    .locator('[data-discount-window]')
    .evaluateAll((items) =>
      items.map((item) => ({
        height: item.getBoundingClientRect().height,
        left: item.getBoundingClientRect().left,
        right: item.getBoundingClientRect().right,
        labelX: item.firstElementChild!.getBoundingClientRect().left,
        rateRight: item.lastElementChild!.getBoundingClientRect().right,
        text: item.textContent,
      }))
    )
  for (const metric of [
    'height',
    'left',
    'right',
    'labelX',
    'rateRight',
  ] as const) {
    const values = windows.map((window) => window[metric])
    expect(
      Math.max(...values) - Math.min(...values),
      metric
    ).toBeLessThanOrEqual(1)
  }
  expect(windows.map((window) => window.text).join(' ')).not.toMatch(
    /当前采用|Selected/
  )
}

async function expectReadableCard(card: Locator) {
  const wideLabels = await card.evaluate((root) => {
    const rem = parseFloat(getComputedStyle(document.documentElement).fontSize)
    if (root.clientWidth < 34 * rem) return []
    return [...root.querySelectorAll('.window-name')].map((label) => ({
      text: label.textContent,
      height: label.getBoundingClientRect().height,
      lineHeight: parseFloat(getComputedStyle(label).lineHeight),
    }))
  })
  for (const label of wideLabels) {
    expect(
      label.height,
      `Window label stays on one line in a wide card: ${label.text}`
    ).toBeLessThanOrEqual(label.lineHeight + 1)
  }
  const measurements = await card.evaluate((root) => {
    const bounds = root.getBoundingClientRect()
    return [...root.querySelectorAll('*')].flatMap((element) => {
      if (!element.getClientRects().length) return []
      const nodes = [...element.childNodes].filter(
        (node) => node.nodeType === Node.TEXT_NODE && node.textContent?.trim()
      )
      if (!nodes.length) return []
      const style = getComputedStyle(element)
      const rects = nodes.flatMap((node) => {
        const range = document.createRange()
        range.selectNodeContents(node)
        return [...range.getClientRects()]
      })
      return [
        {
          text: element.textContent?.trim(),
          fontSize: parseFloat(style.fontSize),
          ellipsis: style.textOverflow === 'ellipsis',
          outside: rects.some(
            (rect) =>
              rect.left < bounds.left - 1 || rect.right > bounds.right + 1
          ),
        },
      ]
    })
  })
  expect(measurements.length).toBeGreaterThan(0)
  for (const item of measurements) {
    expect(item.fontSize, `Readable size: ${item.text}`).toBeGreaterThanOrEqual(
      12
    )
    expect(item.ellipsis, `No hidden copy: ${item.text}`).toBe(false)
    expect(item.outside, `Text stays inside card: ${item.text}`).toBe(false)
  }
  const settings = card.locator('[data-discount-configure]')
  if (await settings.count()) {
    const box = await settings.boundingBox()
    expect(box!.width).toBeGreaterThanOrEqual(44)
    expect(box!.height).toBeGreaterThanOrEqual(44)
  }
}

for (const width of [320, 390, 768, 1280, 1440, 1920])
  for (const theme of ['light', 'dark'] as const)
    for (const locale of ['zh-CN', 'en'])
      test(`discount card ${width} ${theme} ${locale}`, async ({
        page,
      }, testInfo) => {
        await page.setViewportSize({ width, height: 1000 })
        await configureStablePage(page, { theme, locale, user: { role: 100 } })
        await page.addInitScript(
          (value) => localStorage.setItem('ren2hub_locale', value),
          locale
        )
        await page.route('**/api/user/usage-discount', (route) =>
          route.fulfill({ json: { success: true, data: combined } })
        )
        await page.goto('/dashboard')
        await waitForStablePage(page)
        const card = page.locator('[data-discount-card]')
        await card.scrollIntoViewIfNeeded()
        await expect(card.locator('[data-discount-global]')).toContainText('3%')
        await expect(
          card.locator('[data-discount-personal-rate]')
        ).toContainText('4%')
        await expect(card.locator('[data-discount-effective]')).toContainText(
          '0.95×'
        )
        await expect(card.locator('[data-discount-cap]')).toContainText('5%')
        await expect(card.locator('[data-discount-window]')).toHaveCount(3)
        await expect(card.locator('[data-selected="true"]')).toHaveAttribute(
          'data-discount-window',
          'month'
        )
        await expect(card.locator('[aria-current="true"]')).toHaveAttribute(
          'data-discount-window',
          'month'
        )
        await expectAlignedWindows(card)
        await expectReadableCard(card)
        await expect(card.locator('[data-discount-configure]')).toHaveAttribute(
          'href',
          '/system-settings/billing/usage-discount'
        )
        const effectiveBox = await card
          .locator('[data-discount-effective]')
          .boundingBox()
        const globalBox = await card
          .locator('[data-discount-global]')
          .boundingBox()
        expect(effectiveBox!.y + effectiveBox!.height).toBeLessThanOrEqual(
          globalBox!.y
        )
        if (width === 390) {
          const settingsBox = await card
            .locator('[data-discount-configure]')
            .boundingBox()
          expect(settingsBox!.width).toBeGreaterThanOrEqual(44)
          expect(settingsBox!.height).toBeGreaterThanOrEqual(44)
        }
        await card.screenshot({
          path: testInfo.outputPath('discount-card.png'),
        })
        if (width === 390 && locale === 'zh-CN') {
          // Reviewed responsive design; retain exact pixel comparison.
          await expect(card).toHaveScreenshot(`discount-card-${theme}.png`, {
            maxDiffPixels: 0,
            threshold: 0,
          })
        }
        if (width === 1440 && locale === 'zh-CN') {
          await page.screenshot({
            fullPage: true,
            path: testInfo.outputPath('dashboard-context.png'),
          })
        }
        const { textContrast, selectedBorder } = await card.evaluate((root) => {
          const canvas = document.createElement('canvas')
          canvas.width = canvas.height = 1
          const context = canvas.getContext('2d', {
            willReadFrequently: true,
          })!
          const luminance = (rgb: Uint8ClampedArray) => {
            const linear = [...rgb].slice(0, 3).map((channel) => {
              const value = channel / 255
              return value <= 0.04045
                ? value / 12.92
                : ((value + 0.055) / 1.055) ** 2.4
            })
            return (
              0.2126 * linear[0]! + 0.7152 * linear[1]! + 0.0722 * linear[2]!
            )
          }
          // The selected window's border is its only non-text boundary
          // against the card, so it needs 3:1 against the composited parent.
          const selected = root.querySelector('[data-selected="true"]')!
          const parents: Element[] = []
          for (
            let node = selected.parentElement;
            node;
            node = node.parentElement
          )
            parents.unshift(node)
          context.fillStyle = '#fff'
          context.fillRect(0, 0, 1, 1)
          for (const node of parents) {
            context.fillStyle = getComputedStyle(node).backgroundColor
            context.fillRect(0, 0, 1, 1)
          }
          const parentBackground = luminance(
            context.getImageData(0, 0, 1, 1).data
          )
          const selectedStyle = getComputedStyle(selected)
          context.fillStyle = selectedStyle.borderTopColor
          context.fillRect(0, 0, 1, 1)
          const border = luminance(context.getImageData(0, 0, 1, 1).data)
          const selectedBorder = {
            ratio:
              (Math.max(border, parentBackground) + 0.05) /
              (Math.min(border, parentBackground) + 0.05),
            boxShadow: selectedStyle.boxShadow,
          }
          const textContrast = [
            ...root.querySelectorAll(
              'p, h3, [data-discount-personal-rate], [data-discount-cap], [data-discount-window] > span'
            ),
          ]
            .filter(
              (el) => el.getClientRects().length && el.textContent?.trim()
            )
            .map((el) => {
              const ancestors: Element[] = []
              for (
                let node: Element | null = el;
                node;
                node = node.parentElement
              )
                ancestors.unshift(node)
              context.fillStyle = '#fff'
              context.fillRect(0, 0, 1, 1)
              for (const node of ancestors) {
                context.fillStyle = getComputedStyle(node).backgroundColor
                context.fillRect(0, 0, 1, 1)
              }
              const background = luminance(
                context.getImageData(0, 0, 1, 1).data
              )
              const style = getComputedStyle(el)
              context.fillStyle = style.color
              context.fillRect(0, 0, 1, 1)
              const foreground = luminance(
                context.getImageData(0, 0, 1, 1).data
              )
              return {
                text: el.textContent?.trim(),
                ratio:
                  (Math.max(foreground, background) + 0.05) /
                  (Math.min(foreground, background) + 0.05),
                minimum: parseFloat(style.fontSize) >= 24 ? 3 : 4.5,
              }
            })
          return { textContrast, selectedBorder }
        })
        for (const text of textContrast) {
          expect(
            text.ratio,
            `Text contrast: ${text.text}`
          ).toBeGreaterThanOrEqual(text.minimum)
        }
        // A one-sided inset bar makes the read-only window look like a key.
        expect(selectedBorder.boxShadow).toBe('none')
        expect(
          selectedBorder.ratio,
          'Selected window border contrast'
        ).toBeGreaterThanOrEqual(3)
        await expect(
          card.locator('[data-discount-progress="day"]')
        ).toContainText('10 USD')
        // Adjacent progress sentences mix Chinese and digits; switching
        // between the mono and body fonts row by row reads as broken type.
        const progressFonts = await card
          .locator('[data-discount-progress] p')
          .evaluateAll((items) => [
            ...new Set(items.map((item) => getComputedStyle(item).fontFamily)),
          ])
        expect(progressFonts).toHaveLength(1)
        // ConsoleCard's pencil border extends beyond its box. Check the
        // content sections so decorative strokes do not count as overflow.
        const contentOverflow = await card
          .locator(
            '[data-discount-global], [data-discount-personal], [data-discount-effective]'
          )
          .evaluateAll((sections) =>
            Math.max(...sections.map((el) => el.scrollWidth - el.clientWidth))
          )
        expect(contentOverflow).toBeLessThanOrEqual(1)
        expect(
          await page
            .locator('[data-console-scroll-zone]')
            .evaluate((el) => el.scrollWidth - el.clientWidth)
        ).toBeLessThanOrEqual(1)
        await waitForStablePage(page)
      })

for (const role of [1, 10, 100])
  test(`discount settings shortcut role ${role}`, async ({ page }) => {
    await configureStablePage(page, { theme: 'light', user: { role } })
    await page.goto('/dashboard')
    await waitForStablePage(page)
    const shortcut = page.locator('[data-discount-configure]')
    await expect(shortcut).toHaveCount(role === 100 ? 1 : 0)
    if (role === 100) {
      await shortcut.click()
      await expect(page).toHaveURL(
        /\/system-settings\/billing\/usage-discount$/
      )
      await expect(page.locator('[data-discount-editor]')).toBeVisible()
    }
  })

test('empty personal windows remain neutral and tiny discounts stay visible', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'dark', user: { role: 1 } })
  await page.route('**/api/user/usage-discount', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          ...response.data,
          factor: '0.9999',
          global_factor: '0.9999',
          fixed_rate: '0.0001',
          applied_rate: '0.0001',
          winner_window: undefined,
          windows: [],
        },
      },
    })
  )
  await page.goto('/dashboard')
  await waitForStablePage(page)
  const card = page.locator('[data-discount-card]')
  await expect(card.locator('[data-discount-personal-empty]')).toContainText(
    '当前没有适用'
  )
  await expect(card.locator('[data-discount-effective]')).toContainText(
    '0.9999×'
  )
  await expect(card.locator('[data-discount-effective]')).toContainText('0.01%')
  await expect(card.locator('[data-discount-tier-details]')).toHaveCount(0)
})

for (const theme of ['light', 'dark'] as const)
  for (const status of ['inactive', 'unavailable', 'zero-windows'] as const)
    test(`discount state ${status} ${theme}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width: 390, height: 1000 })
      await configureStablePage(page, { theme, user: { role: 1 } })
      const unavailable = status === 'unavailable'
      await page.route('**/api/user/usage-discount', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              ...combined,
              status: status === 'zero-windows' ? 'ok' : status,
              factor: unavailable ? '0.99' : '1',
              global_factor: unavailable ? '0.99' : '1',
              fixed_rate: unavailable ? '0.01' : '0',
              site_rate: '0',
              applied_rate: unavailable ? '0.01' : '0',
              cap_applied: false,
              winner_window: undefined,
              windows:
                status === 'zero-windows'
                  ? combined.windows.map((window) => ({
                      ...window,
                      quota: 0,
                      rate: '0',
                    }))
                  : [],
            },
          },
        })
      )
      await page.goto('/dashboard')
      await waitForStablePage(page)
      const card = page.locator('[data-discount-card]')
      await card.scrollIntoViewIfNeeded()
      await expect(card.locator('[data-discount-effective]')).toContainText(
        unavailable ? '0.99×' : '1×'
      )
      await expect(card.locator('[data-selected="true"]')).toHaveCount(0)
      await expect(card.locator('[data-discount-cap]')).toHaveCount(0)
      await expect(card.locator('[data-discount-unavailable]')).toHaveCount(
        unavailable ? 1 : 0
      )
      await expect(card.locator('[data-discount-window]')).toHaveCount(
        status === 'zero-windows' ? 3 : 0
      )
      await expectReadableCard(card)
      if (status === 'inactive') {
        await expect(card.locator('[data-discount-empty]')).toContainText(
          '标准计费'
        )
        await expect(card.locator('[data-discount-ruler]')).toHaveCount(0)
        await expect(card.locator('[data-discount-global]')).toHaveCount(0)
        expect(
          await minTextContrast(
            card.locator(
              '[data-discount-empty] h3, [data-discount-empty] .standard-copy p, [data-discount-effective] span'
            )
          )
        ).toBeGreaterThanOrEqual(4.5)
        expect(
          await minTextContrast(card.locator('[data-discount-factor]'))
        ).toBeGreaterThanOrEqual(3)
        await expect(card).toHaveScreenshot(`discount-standard-${theme}.png`, {
          maxDiffPixels: 0,
          threshold: 0,
        })
      } else {
        await expect(card.locator('[data-discount-empty]')).toHaveCount(0)
        if (status === 'zero-windows') {
          await expect(card.locator('[data-discount-ruler]')).toHaveCount(0)
          await expect(
            card.locator('[data-discount-progress="day"]')
          ).toContainText('5 USD')
        }
      }
      await card.screenshot({ path: testInfo.outputPath('discount-state.png') })
      await waitForStablePage(page)
    })

for (const theme of ['light', 'dark'] as const)
  test(`discount narrow English ${theme}`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 320, height: 900 })
    await configureStablePage(page, {
      theme,
      locale: 'en',
      user: { role: 100 },
    })
    await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
    await page.route('**/api/user/usage-discount', (route) =>
      route.fulfill({ json: { success: true, data: combined } })
    )
    await page.goto('/dashboard')
    await waitForStablePage(page)
    const card = page.locator('[data-discount-card]')
    await card.scrollIntoViewIfNeeded()
    await expectAlignedWindows(card)
    await expectReadableCard(card)
    expect(
      await card
        .locator('[data-discount-personal]')
        .evaluate((el) => el.scrollWidth - el.clientWidth)
    ).toBeLessThanOrEqual(1)
    expect(
      await page
        .locator('[data-console-scroll-zone]')
        .evaluate((el) => el.scrollWidth - el.clientWidth)
    ).toBeLessThanOrEqual(1)
    await card.screenshot({ path: testInfo.outputPath('discount-narrow.png') })
    await waitForStablePage(page)
  })

for (const theme of ['light', 'dark'] as const)
  for (const locale of ['zh-CN', 'en'])
    test(`standard pricing adapts to card widths ${theme} ${locale}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width: 1440, height: 1000 })
      await configureStablePage(page, { theme, locale, user: { role: 1 } })
      await page.addInitScript(
        (value) => localStorage.setItem('ren2hub_locale', value),
        locale
      )
      await page.route('**/api/user/usage-discount', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              ...combined,
              status: 'ok',
              factor: '1',
              global_factor: '1',
              fixed_rate: '0',
              site_rate: '0',
              applied_rate: '0',
              cap_applied: false,
              winner_window: undefined,
              windows: [],
            },
          },
        })
      )
      await page.goto('/dashboard')
      await waitForStablePage(page)
      const card = page.locator('[data-discount-card]')
      await expect(card.locator('[data-discount-empty]')).toBeVisible()
      await expect(card.locator('[data-discount-configure]')).toHaveCount(0)
      for (const width of [260, 320, 420, 560]) {
        // Exercise the component's container query independently of viewport breakpoints.
        await card.evaluate((element, value) => {
          const html = element as HTMLElement
          html.style.width = `${value}px`
          html.style.height = '400px'
          html.style.gridColumn = '1 / -1'
        }, width)
        await card.scrollIntoViewIfNeeded()
        await expectReadableCard(card)
        const panel = await card.locator('[data-discount-empty]').boundingBox()
        const effective = await card
          .locator('[data-discount-effective]')
          .boundingBox()
        expect(effective!.y - panel!.y).toBeLessThan(240)
        await card.screenshot({
          path: testInfo.outputPath(`standard-${width}.png`),
        })
      }
      await waitForStablePage(page)
    })

for (const theme of ['light', 'dark'] as const)
  test(`long amounts and 200 percent text ${theme}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width: 320, height: 1000 })
    await configureStablePage(page, {
      theme,
      locale: 'en',
      user: { role: 100 },
    })
    await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
    await page.route('**/api/user/usage-discount', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            ...combined,
            factor: '0.9999',
            global_factor: '1',
            fixed_rate: '0',
            site_rate: '0',
            applied_rate: '0.0001',
            cap_applied: false,
            windows: [
              {
                window: 'month',
                quota: 999999999999999,
                rate: '0.0001',
                tiers: [{ threshold_quota: '1000000000000000', rate: '0.02' }],
              },
            ],
          },
        },
      })
    )
    await page.goto('/dashboard')
    await waitForStablePage(page)
    const card = page.locator('[data-discount-card]')
    await expect(
      card.locator('[data-discount-progress="month"]')
    ).toContainText('1999999999.999998 USD')
    await expect(
      card.locator('[data-discount-progress="month"]')
    ).toContainText('0.000002 USD to 2%')
    await expectReadableCard(card)
    await card.screenshot({ path: testInfo.outputPath('long-amounts.png') })
    await page.setViewportSize({ width: 1280, height: 1000 })
    await page.evaluate(() => {
      document.documentElement.style.fontSize = '200%'
    })
    expect((await card.boundingBox())!.width).toBeGreaterThan(600)
    // Keep the full card inside the scroll viewport for visual review.
    await page.setViewportSize({ width: 1280, height: 2400 })
    await card.scrollIntoViewIfNeeded()
    await expectReadableCard(card)
    await expect(card.locator('[data-discount-factor]')).toHaveText('0.9999×')
    await card.screenshot({ path: testInfo.outputPath('text-200-percent.png') })
    await waitForStablePage(page)
  })
