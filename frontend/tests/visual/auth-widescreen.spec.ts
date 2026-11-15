import { expect, test } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  captureEvidence,
} from './fixtures'

const sizes = [
  { width: 1440, height: 900 },
  { width: 1920, height: 1080 },
  { width: 2560, height: 1080 },
  { width: 2560, height: 1264 },
  { width: 2560, height: 1440 },
  { width: 3440, height: 1440 },
  { width: 3840, height: 2160 },
]

for (const theme of ['light', 'dark'] as const) {
  for (const locale of ['zh-CN', 'en']) {
    test(`${theme} ${locale} authentication grows with wide displays`, async ({
      page,
    }, testInfo) => {
      await configureStablePage(page, { theme, authenticated: false })
      await page.addInitScript(
        (value) => localStorage.setItem('ren2hub_locale', value),
        locale
      )
      await page.goto('/sign-in')
      await waitForStablePage(page)

      for (const size of sizes) {
        await page.setViewportSize(size)
        await page.evaluate(() => document.fonts.ready)
        const layout = await page.evaluate(() => {
          const rect = (selector: string) =>
            document.querySelector(selector)!.getBoundingClientRect().toJSON()
          return {
            header: rect('.auth-header'),
            body: rect('.auth-body'),
            card: rect('.auth-card'),
            copy: rect('.panel-measure'),
            titleSize: parseFloat(
              getComputedStyle(document.querySelector('.auth-panel h2 span')!)
                .fontSize
            ),
            input: rect('.auth-card input[autocomplete="username"]'),
            pageHeight: document.documentElement.scrollHeight,
          }
        })

        await assertNoHorizontalOverflow(page)
        expect(layout.header.width).toBeCloseTo(layout.body.width, 0)
        expect(layout.copy.right).toBeLessThan(layout.card.left)
        expect(layout.input.left).toBeGreaterThan(layout.card.left)
        expect(layout.input.right).toBeLessThan(layout.card.right)
        expect(layout.card.bottom).toBeLessThanOrEqual(size.height)
        expect(layout.pageHeight).toBeLessThanOrEqual(size.height + 1)
        if (size.width === 1440) {
          expect(layout.card.width).toBe(448)
          expect(layout.titleSize).toBe(60)
        } else {
          expect(layout.body.width).toBeGreaterThan(1440)
          expect(layout.body.width).toBeLessThanOrEqual(2560)
          expect(layout.card.width).toBeGreaterThan(500)
          expect(layout.titleSize).toBeGreaterThan(60)
          if (size.width <= 2560) {
            expect(layout.body.width / size.width).toBeGreaterThan(0.85)
          }
          if (size.width === 2560) {
            expect(layout.card.width).toBeGreaterThan(600)
            expect(layout.input.height).toBeGreaterThan(48)
          }
          if (locale === 'en') {
            const endingSharesLine = await page.evaluate(() => {
              const title = document.querySelector('.auth-panel h2 span')!
              const text = title.firstChild!
              const content = text.textContent!.trimEnd()
              const dashIndex = content.lastIndexOf('—')
              const precedingWordIndex = content
                .slice(0, dashIndex)
                .trimEnd()
                .lastIndexOf(' ')
              const range = document.createRange()
              range.setStart(text, precedingWordIndex + 1)
              range.setEnd(text, dashIndex + 1)
              return range.getClientRects().length === 1
            })
            expect(endingSharesLine).toBe(true)
          }
        }
        if (
          (size.width === 2560 && size.height === 1264) ||
          size.width === 3840
        ) {
          await captureEvidence(page, {
            path: testInfo.outputPath(
              `auth-${size.width}-${locale}-${theme}.png`
            ),
          })
        }
      }

      // Enlarged fields retain their ordinary keyboard interaction.
      const username = page.locator('input[autocomplete="username"]')
      await username.focus()
      await page.keyboard.type('wide-layout-check')
      await page.keyboard.press('Tab')
      await expect(
        page.locator('input[autocomplete="current-password"]')
      ).toBeFocused()
      await expect(username).toHaveValue('wide-layout-check')
    })
  }

  test(`${theme} shared authentication forms remain reachable on short wide and mobile screens`, async ({
    page,
  }) => {
    await configureStablePage(page, { theme, authenticated: false })
    await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
    for (const path of ['/sign-up', '/forgot-password']) {
      for (const size of [
        { width: 390, height: 844 },
        { width: 1440, height: 900 },
        { width: 2560, height: 1080 },
      ]) {
        await page.setViewportSize(size)
        await page.goto(path)
        await waitForStablePage(page)
        await assertNoHorizontalOverflow(page)
        const submit = page.locator('.auth-card button[type="submit"]')
        await submit.scrollIntoViewIfNeeded()
        await expect(submit).toBeInViewport()
        if (size.width === 390)
          await expect(page.locator('.auth-panel')).toBeHidden()
      }
    }
  })
}
