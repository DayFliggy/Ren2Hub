import { expect, test, type Page } from '@playwright/test'

import {
  configureStablePage,
  waitForStablePage,
  assertNoHorizontalOverflow,
  type VisualTheme,
  captureEvidence,
} from './fixtures'

const THEMES: VisualTheme[] = ['light', 'dark']

for (const theme of THEMES) {
  test(`dashboard greeting renders the time bucket and underlined name (${theme})`, async ({
    page,
  }, testInfo) => {
    await configureStablePage(page, { theme })
    await page.goto('/dashboard')
    await waitForStablePage(page)

    const heading = page.locator('[data-handdrawn="page-hero"] h1')
    await expect(heading).toBeVisible()

    // Fixtures pin the clock to 12:00 Asia/Shanghai, so the noon bucket wins.
    await expect(heading).toHaveText('该去吃饭了，Visual Root。')

    const accent = heading.locator('span.brush-highlight')
    await expect(accent).toHaveText('Visual Root')
    await expect(accent).toHaveClass(/brush-highlight--underline/)

    // Punctuation must sit outside the painted mark.
    await expect(accent).not.toContainText('。')

    await captureEvidence(page, {
      path: testInfo.outputPath(`greeting-hero-${theme}-desktop.png`),
      clip: { x: 0, y: 0, width: 1440, height: 420 },
    })

    await page.setViewportSize({ width: 390, height: 844 })
    await waitForStablePage(page)
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: testInfo.outputPath(`greeting-hero-${theme}-mobile.png`),
      clip: { x: 0, y: 0, width: 390, height: 400 },
    })
  })
}

test('a long display name keeps the underline on each wrapped line', async ({
  page,
}, testInfo) => {
  await configureStablePage(page, { theme: 'light' })
  await page.goto('/dashboard')
  await waitForStablePage(page)

  const accent = page.locator(
    '[data-handdrawn="page-hero"] h1 span.brush-highlight'
  )
  await accent.evaluate((node) => {
    node.textContent = '白日飞猪超长用户名用于验证换行行为是否正确'
  })

  await page.setViewportSize({ width: 390, height: 844 })
  await waitForStablePage(page)
  await assertNoHorizontalOverflow(page)
  await captureEvidence(page, {
    path: testInfo.outputPath('greeting-hero-longname-mobile.png'),
    clip: { x: 0, y: 0, width: 390, height: 460 },
  })
})

/** Where the closing "。" sits relative to the underlined name. */
function greetingLayout(page: Page) {
  return page.locator('[data-handdrawn="page-hero"] h1').evaluate((heading) => {
    const accent = heading.querySelector('.brush-highlight')!
    const walker = document.createTreeWalker(heading, NodeFilter.SHOW_TEXT)
    let suffix: Node | null = null
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      if (node.textContent?.includes('。')) suffix = node
    }
    const offset = suffix!.textContent!.lastIndexOf('。')
    const range = document.createRange()
    range.setStart(suffix!, offset)
    range.setEnd(suffix!, offset + 1)
    const mark = range.getBoundingClientRect()
    const name = accent.getBoundingClientRect()
    const middle = (mark.top + mark.bottom) / 2
    // The name is an inline-block, so text after it starts at its right edge.
    return {
      suffixOnAccentLine:
        mark.left >= name.right - 1 &&
        middle > name.top &&
        middle < name.bottom,
      overflow: heading.scrollWidth - heading.clientWidth,
    }
  })
}

for (const name of [
  'Alex Chen',
  'Alexander Christopher Montgomery-Wellington',
]) {
  test(`the closing punctuation never starts a line (${name})`, async ({
    page,
  }) => {
    await configureStablePage(page, {
      theme: 'light',
      user: { display_name: name },
    })
    await page.setViewportSize({ width: 320, height: 800 })
    await page.goto('/dashboard')
    await waitForStablePage(page)

    // Phone widths 8px apart move the name's end past the right edge in steps
    // smaller than the 36px "。", so some width used to push it onto its own
    // line.
    for (let width = 320; width <= 480; width += 8) {
      await page.setViewportSize({ width, height: 800 })
      const layout = await greetingLayout(page)
      expect(layout.suffixOnAccentLine, `${width}px`).toBe(true)
      expect(layout.overflow, `${width}px`).toBeLessThanOrEqual(0)
    }
    await assertNoHorizontalOverflow(page)
  })
}
