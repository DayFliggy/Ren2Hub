import { expect, test, type Page } from '@playwright/test'
import {
  assertInteractiveCentersVisible,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'
import { nativeFixture } from '../contracts/native-core-fixture'

// Bodies generated from the real handlers by controller/frontend_contract_test.go.
// The shared native fixture serves the GET and estimate reads from them.
const configResponse = nativeFixture<{
  data: { version: number; created_at: number; config: Record<string, unknown> }
}>('usage-discount.get.json')
const patchRequest = nativeFixture<{
  expected_version: number
  config: Record<string, unknown>
}>('usage-discount.patch.request.json')

test.setTimeout(90_000)

type Locale = 'zh-CN' | 'en'

async function openPage(
  page: Page,
  {
    theme,
    locale,
    role = 100,
    width = 1440,
  }: { theme: VisualTheme; locale: Locale; role?: number; width?: number }
) {
  await page.setViewportSize({ width, height: 900 })
  const fixture = await configureStablePage(page, {
    theme,
    locale,
    user: { role },
  })
  await page.addInitScript(
    (value) => localStorage.setItem('ren2hub_locale', value),
    locale
  )
  return fixture
}

// The console scrolls inside its zone, so the document never overflows.
async function expectNoZoneOverflow(page: Page) {
  const overflow = await page
    .locator('[data-console-scroll-zone]')
    .evaluate((zone) => zone.scrollWidth - zone.clientWidth)
  expect(overflow).toBeLessThanOrEqual(1)
}

const copy = {
  'zh-CN': {
    title: '用量折扣',
    cutoff: '2026-10-30 00:00 (Asia/Shanghai)',
    site: '全站阶梯',
    month: '个人月阶梯',
    note: '结果是近似值，不是账单。',
    run: '估算当前草稿',
    threshold: '第 1 档阈值（USD）',
    health: '小时写入失败',
  },
  en: {
    title: 'Usage discount',
    cutoff: '2026-10-30 00:00 (Asia/Shanghai)',
    site: 'Site tiers',
    month: 'Personal month tiers',
    note: 'an approximation, not a bill',
    run: 'Estimate this draft',
    threshold: 'Tier 1 threshold (USD)',
    health: 'Hourly write failures',
  },
} as const

for (const [screen, width] of [
  ['desktop', 1440],
  ['mobile', 390],
] as const)
  for (const theme of ['light', 'dark'] as const)
    for (const locale of ['zh-CN', 'en'] as const)
      test(`usage discount page renders ${screen} ${theme} ${locale}`, async ({
        page,
      }, testInfo) => {
        await openPage(page, { theme, locale, width })
        const text = copy[locale]
        await page.goto('/usage-discount')
        await waitForStablePage(page)

        await expect(page.locator('html')).toHaveClass(
          new RegExp(`\\b${theme}\\b`)
        )
        const status = page.locator('[data-discount-status]')
        await expect(status).toContainText('2026-10-30')
        await expect(status).toContainText(text.cutoff)
        // usage-discount-status.get.json reports two hourly write failures.
        await expect(
          status.locator('[data-health="hourly_write_failures"]')
        ).toHaveAttribute('data-failing', 'true')
        await expect(status).toContainText(text.health)

        const editor = page.locator('[data-discount-editor]')
        await expect(
          editor.getByRole('heading', { name: text.site, exact: true })
        ).toBeVisible()
        const month = editor.locator('[data-discount-table="month"]')
        await expect(
          month.getByRole('textbox', { name: text.threshold, exact: true })
        ).toHaveValue('25')
        await expect(month.locator('[data-discount-tier]')).toHaveCount(5)
        await expect(page.locator('[data-discount-estimate]')).toContainText(
          text.note
        )

        await expectNoZoneOverflow(page)
        // Every tier row stays inside the editor card at phone width.
        const card = (await editor.boundingBox())!
        for (const row of await editor.locator('[data-discount-tier]').all()) {
          const box = (await row.boundingBox())!
          expect(box.x).toBeGreaterThanOrEqual(card.x)
          expect(box.x + box.width).toBeLessThanOrEqual(card.x + card.width + 1)
        }
        await assertInteractiveCentersVisible(page)
        await page.screenshot({
          path: testInfo.outputPath(
            `usage-discount-${screen}-${theme}-${locale}.png`
          ),
        })
        await waitForStablePage(page)
      })

test('edits, estimates and saves the generated PATCH body', async ({
  page,
}, testInfo) => {
  await openPage(page, { theme: 'light', locale: 'zh-CN' })
  const patches: unknown[] = [],
    estimates: unknown[] = []
  await page.route('**/api/option/usage_discount/estimate', (route) => {
    estimates.push(route.request().postDataJSON())
    return route.fallback()
  })
  await page.route('**/api/option/usage_discount', (route) => {
    if (route.request().method() !== 'PATCH') return route.fallback()
    const body = route.request().postDataJSON()
    patches.push(body)
    return route.fulfill({
      json: {
        success: true,
        message: '',
        data: {
          version: body.expected_version + 1,
          created_at: 1793300000,
          created_by: 1,
          config: body.config,
        },
      },
    })
  })
  await page.goto('/usage-discount')
  await waitForStablePage(page)
  const editor = page.locator('[data-discount-editor]')
  const save = page.getByRole('button', { name: '保存配置', exact: true })

  // The edit the backend contract test makes: 1% fixed, month tiers on.
  await editor.getByLabel('固定折扣', { exact: true }).fill('1')
  const month = editor.locator('[data-discount-table="month"]')
  await month
    .getByRole('radiogroup', { name: '个人月阶梯模式' })
    .getByRole('radio', { name: '启用', exact: true })
    .click()
  await expect(page.getByText('有未保存的修改', { exact: true })).toBeVisible()

  // A threshold below the tier above is caught inline.
  const second = month.getByRole('textbox', {
    name: '第 2 档阈值（USD）',
    exact: true,
  })
  await second.fill('10')
  await expect(month).toContainText('阈值必须高于上一档。')
  await expect(second).toHaveAttribute('aria-invalid', 'true')
  await expect(save).toBeDisabled()
  await second.fill('75')
  await expect(save).toBeEnabled()

  const estimate = page.locator('[data-discount-estimate]')
  await estimate.getByRole('button', { name: '估算当前草稿' }).click()
  await expect(estimate).toContainText('2026-09-30 至 2026-10-29（30 天）')
  await expect(estimate).toContainText('不足约 60 天')
  await expect(page.locator('[data-estimate-stale]')).toHaveCount(0)
  await editor.getByLabel('折扣上限', { exact: true }).fill('6')
  await expect(page.locator('[data-estimate-stale]')).toBeVisible()
  await editor.getByLabel('折扣上限', { exact: true }).fill('5')
  await expect(page.locator('[data-estimate-stale]')).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath('edited.png') })

  await save.click()
  await expect(page.getByText('已保存为版本 1', { exact: true })).toBeVisible()
  await expect(page.getByText('有未保存的修改', { exact: true })).toHaveCount(0)
  expect(patches).toEqual([patchRequest])
  expect(estimates).toEqual([{ config: patchRequest.config }])
  await waitForStablePage(page)
})

test('a version conflict keeps the draft until the server version is loaded', async ({
  page,
}, testInfo) => {
  await openPage(page, { theme: 'dark', locale: 'en', width: 390 })
  // Another administrator saved version 3 with a 2% fixed discount.
  let server = configResponse
  await page.route('**/api/option/usage_discount', (route) => {
    if (route.request().method() === 'PATCH') {
      server = {
        ...configResponse,
        data: {
          ...configResponse.data,
          version: 3,
          created_at: 1793300000,
          config: { ...configResponse.data.config, fixed_rate: '0.02' },
        },
      }
      return route.fulfill({
        status: 409,
        json: {
          success: false,
          message: 'usage_discount_version_conflict',
          code: 'usage_discount_version_conflict',
        },
      })
    }
    return route.fulfill({ json: server })
  })
  await page.goto('/usage-discount')
  await waitForStablePage(page)
  const fixed = page.getByLabel('Fixed discount', { exact: true })
  const save = page.getByRole('button', {
    name: 'Save configuration',
    exact: true,
  })

  await fixed.fill('1')
  await save.click()
  const conflict = page.locator('[data-discount-conflict]')
  await expect(conflict).toContainText('now version 3')
  await expect(conflict).toContainText('Your draft is kept')
  await expect(fixed).toHaveValue('1')
  await expect(save).toBeDisabled()
  await expectNoZoneOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('conflict.png') })

  await conflict.getByRole('button', { name: 'Load server version' }).click()
  await page
    .getByRole('dialog')
    .getByRole('button', { name: 'Reload', exact: true })
    .click()
  await expect(fixed).toHaveValue('2')
  await expect(conflict).toHaveCount(0)
  await expect(page.locator('[data-discount-editor]')).toContainText(
    'Version 3'
  )
  await waitForStablePage(page)
})

test('root users find the page in the admin navigation', async ({ page }) => {
  await openPage(page, { theme: 'light', locale: 'zh-CN' })
  await page.goto('/dashboard')
  await waitForStablePage(page)
  const link = page.locator('aside a[data-console-nav-item="usage-discount"]')
  await expect(link).toHaveText('用量折扣')
  await expect(link).toHaveAttribute('href', '/usage-discount')
  await link.click()
  await expect(page).toHaveURL(/\/usage-discount$/)
  await expect(page.locator('[data-discount-editor]')).toBeVisible()
  await waitForStablePage(page)
})

for (const role of [10, 1])
  test(`role ${role} gets neither the entry nor the page`, async ({ page }) => {
    await openPage(page, { theme: 'light', locale: 'zh-CN', role })
    const reads: string[] = []
    page.on('request', (request) => {
      if (
        new URL(request.url()).pathname.startsWith('/api/option/usage_discount')
      )
        reads.push(request.url())
    })
    await page.goto('/usage-discount')
    await expect(page).toHaveURL(/\/dashboard$/)
    await waitForStablePage(page)
    await expect(
      page.locator('[data-console-nav-item="usage-discount"]')
    ).toHaveCount(0)
    expect(reads).toEqual([])
  })
