import { readFileSync } from 'node:fs'
import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { configureStablePage, waitForStablePage } from './fixtures'
import { captureUI } from './ui-coverage'

function evidence(info: TestInfo, label: string) {
  return JSON.parse(
    readFileSync(info.outputPath(`${label}.coverage.json`), 'utf8')
  ) as {
    mounted: string[]
    visible: string[]
    screenshots: string[]
    frames?: Array<{ name: string; visible: string[] }>
  }
}

async function scrollPositions(page: Page) {
  return page
    .locator('#coverage-outer, .data-table-body-viewport')
    .evaluateAll((elements) =>
      elements.map((element) => ({
        top: element.scrollTop,
        left: element.scrollLeft,
      }))
    )
}

test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 640, height: 540 })
  await configureStablePage(page, { theme: 'light', locale: 'en' })
})

test('coverage excludes transparent screen-reader and horizontally clipped components', async ({
  page,
}, info) => {
  await page.goto('/tests/coverage-harness.html?scenario=visibility')
  await waitForStablePage(page)
  await captureUI(page, info, 'visible-intersection', {
    scroll: false,
    compare: true,
  })
  const result = evidence(info, 'visible-intersection')
  for (const name of [
    'HealthMeter',
    'SkeletonBlock',
    'StatusChip',
    'EmptyState',
    'ErrorBanner',
  ]) {
    expect(result.mounted).toContain(`src/components/common/${name}.vue`)
    expect(result.visible, `${name} has no visible pixels`).not.toContain(
      `src/components/common/${name}.vue`
    )
  }
  expect(result.visible).toContain('src/components/common/ConsoleButton.vue')
})

test('coverage discovers activated nested scrolling after DOM insertion and restores positions', async ({
  page,
}, info) => {
  await page.goto('/tests/coverage-harness.html?scenario=nested')
  await waitForStablePage(page)
  const inner = page.locator('.data-table-body-viewport')
  await expect(inner).toHaveCSS('overflow-y', 'hidden')
  const before = await scrollPositions(page)
  await captureUI(page, info, 'nested-scroll', { compare: true })
  await expect(page.locator('[data-inserted-before-scroll]')).toHaveCount(1)
  expect(await scrollPositions(page)).toEqual(before)
  const result = evidence(info, 'nested-scroll')
  expect(result.visible).toContain('src/components/common/HealthMeter.vue')
  expect(result.visible).toContain('src/components/common/SkeletonBlock.vue')
  expect(
    result.frames?.some((frame) =>
      frame.visible.includes('src/components/common/HealthMeter.vue')
    )
  ).toBe(true)
  expect(
    result.frames?.some((frame) =>
      frame.visible.includes('src/components/common/SkeletonBlock.vue')
    )
  ).toBe(true)
})

test('coverage preserves nonzero nested horizontal and vertical positions', async ({
  page,
}, info) => {
  await page.goto('/tests/coverage-harness.html?scenario=nested')
  await waitForStablePage(page)
  await page.locator('#coverage-outer').evaluate((element) => {
    element.scrollTop = element.scrollHeight
  })
  const inner = page.locator('.data-table-body-viewport')
  await expect(inner).toHaveCSS('overflow-y', 'auto')
  await inner.evaluate((element) => {
    element.scrollTop = 80
    element.scrollLeft = 60
  })
  const before = await scrollPositions(page)
  expect(before[1]).toEqual({ top: 80, left: 60 })
  await captureUI(page, info, 'nested-existing-position', { compare: true })
  expect(await scrollPositions(page)).toEqual(before)
  const result = evidence(info, 'nested-existing-position')
  expect(result.visible).toContain('src/components/common/SkeletonBlock.vue')
})
