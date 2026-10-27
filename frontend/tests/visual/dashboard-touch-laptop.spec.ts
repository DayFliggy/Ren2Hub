import { test, expect } from '@playwright/test'
import { configureStablePage, waitForStablePage } from './fixtures'

// A touchscreen laptop driven by a mouse reports any-pointer: coarse but a
// fine primary pointer. launchOptions must be file-level, hence this spec.
test.use({
  launchOptions: {
    args: [
      '--blink-settings=primaryPointerType=4,availablePointerTypes=6,primaryHoverType=2,availableHoverTypes=2',
    ],
  },
})

test('touchscreen laptop keeps the compact desktop overview row', async ({
  page,
}) => {
  await page.setViewportSize({ width: 2560, height: 1440 })
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  const sizing = await page.evaluate(() => {
    const stage = document.querySelector('[data-usage-stage]')
    const usage = document.querySelector('[data-usage-distribution]')
    const balance = document.querySelector('[data-balance-card]')
    if (!stage || !usage || !balance) throw new Error('overview row missing')
    return {
      anyCoarse: matchMedia('(any-pointer: coarse)').matches,
      coarse: matchMedia('(pointer: coarse)').matches,
      minHeight: parseFloat(getComputedStyle(stage).minHeight),
      rem: parseFloat(getComputedStyle(document.documentElement).fontSize),
      usage: usage.getBoundingClientRect().height,
      balance: balance.getBoundingClientRect().height,
    }
  })
  expect(sizing.anyCoarse).toBe(true)
  expect(sizing.coarse).toBe(false)
  // The touch-only 18rem reserve would push the row to about 524px here.
  expect(sizing.minHeight).toBeCloseTo(10.625 * sizing.rem, 0)
  expect(Math.abs(sizing.usage - sizing.balance)).toBeLessThan(1)
  expect(sizing.usage, JSON.stringify(sizing)).toBeLessThan(440)
})
