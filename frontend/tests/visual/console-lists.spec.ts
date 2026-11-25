import { expect, test, type Locator } from '@playwright/test'

import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

// The key table needs a 60rem card. The sidebar appears at 1024px, so a
// 1024px viewport has a narrower card (cards) than a 1023px one (table), and
// ~1248px is the narrowest table viewport with the sidebar expanded.
const layouts = [
  { width: 768, height: 1024, table: false },
  { width: 1023, height: 768, table: true },
  { width: 1024, height: 768, table: false },
  { width: 1248, height: 900, table: true },
  { width: 1440, height: 900, table: true },
] as const

for (const layout of layouts) {
  test(`keys at ${layout.width} use the ${layout.table ? 'table' : 'cards'}`, async ({
    page,
  }) => {
    await page.setViewportSize(layout)
    await configureStablePage(page, { theme: 'light', authenticated: true })
    await page.goto('/keys', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const table = page.locator('[data-key-page] .data-table-shell')
    const cards = page.locator('[data-key-mobile-row]')
    await expect(layout.table ? table : cards).toBeVisible()
    await expect(layout.table ? cards : table).toBeHidden()
    if (layout.table) {
      const tableMetrics = await page.evaluate(() => {
        const nameCell = document.querySelector<HTMLElement>(
          'td[headers$="-name"]'
        )!
        const cs = getComputedStyle(nameCell)
        const contentWidth =
          nameCell.getBoundingClientRect().width -
          parseFloat(cs.paddingLeft) -
          parseFloat(cs.paddingRight)
        const scroller = document.querySelector<HTMLElement>(
          '[data-key-page] .data-table-shell .overflow-x-auto'
        )
        return {
          contentWidth,
          tableOverflow: scroller
            ? scroller.scrollWidth - scroller.clientWidth
            : 0,
        }
      })
      expect(tableMetrics.contentWidth).toBeGreaterThanOrEqual(130)
      expect(tableMetrics.tableOverflow).toBeLessThanOrEqual(0)
    }
    await assertNoHorizontalOverflow(page)
    await assertInteractiveCentersVisible(page)
  })
}

async function expectTarget(target: Locator, minWidth = 24) {
  const box = await target.boundingBox()
  expect(box?.width).toBeGreaterThanOrEqual(minWidth)
  expect(box?.height).toBeGreaterThanOrEqual(24)
}

for (const theme of ['light', 'dark'] as const satisfies VisualTheme[]) {
  test(`${theme} mobile key and log targets are at least 24px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await configureStablePage(page, { theme, authenticated: true })
    await page.goto('/keys', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const checkbox = page.getByRole('checkbox', {
      name: '选择令牌「Native Key」',
    })
    const checkboxTarget = page.locator('label').filter({ has: checkbox })
    await expectTarget(checkboxTarget)
    // The padding around the glyph must forward clicks to the checkbox.
    const box = await checkboxTarget.boundingBox()
    await page.mouse.click(box!.x + 1, box!.y + 1)
    await expect(checkbox).toBeChecked()

    await expectTarget(
      page
        .locator('[data-key-mobile-row]')
        .getByRole('button', { name: '编辑令牌', exact: true })
    )

    await page.goto('/usage-logs', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)
    await expectTarget(
      page
        .getByRole('button', { name: '查看日志详情', exact: true })
        .filter({ visible: true })
    )
  })
}

for (const theme of ['light', 'dark'] as const satisfies VisualTheme[]) {
  test(`${theme} desktop key row renders inline quota figures with zero hover popovers`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await configureStablePage(page, { theme, authenticated: true })
    await page.goto('/keys', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const row = page.locator('[data-key-page] tbody tr').first()
    const quota = row.locator('td[headers$="-quota"]')
    await expect(quota.locator('[data-key-remaining]')).toHaveText('$1.000')
    await expect(quota.locator('[data-key-used]')).toContainText('已用')
    await expect(quota.locator('button')).toHaveCount(0)

    await row.locator('td[headers$="-name"]').hover()
    await page.waitForTimeout(200)
    await expect(page.locator('[data-key-quota-popover]')).toHaveCount(0)
    await assertNoHorizontalOverflow(page)
    await assertInteractiveCentersVisible(page)
  })

  test(`${theme} mobile key quota bar is a static readout without an accordion`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await configureStablePage(page, { theme, authenticated: true })
    await page.goto('/keys', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const card = page.locator('[data-key-mobile-row]').first()
    const bar = card.locator('.key-quota-bar')
    await expect(bar.locator('[data-key-remaining]')).toHaveText('$1.000')
    await expect(bar.locator('[data-key-used]')).toContainText('已用')
    await expect(card.locator('button[data-key-quota-ring]')).toHaveCount(0)
    await expect(page.locator('[data-key-quota-popover]')).toHaveCount(0)
    await assertNoHorizontalOverflow(page)
    await assertInteractiveCentersVisible(page)
  })
}

test('light mobile cards preserve semantic quota borders and straight row dividers', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.route('**/api/token/?*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          page: 1,
          page_size: 10,
          total: 1,
          items: [
            {
              id: 7,
              name: 'Low Key',
              key: 'sk-****1234',
              type: 'ordered',
              selected_only: true,
              route_channel_count: 2,
              status: 1,
              remain_quota: 40_000,
              used_quota: 960_000,
              unlimited_quota: false,
              model_limits_enabled: false,
              model_limits: '',
              allow_ips: null,
              allow_user_quota_query: true,
              created_time: 1785124800,
              expired_time: -1,
            },
          ],
        },
      },
    })
  )
  await page.goto('/keys', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  const styles = await page.evaluate(() => {
    const row = document.querySelector<HTMLElement>('[data-key-mobile-row]')!
    const bar = document.querySelector<HTMLElement>('.key-quota-bar')!
    return {
      rowRadius: getComputedStyle(row).borderRadius,
      barBorderColor: getComputedStyle(bar).borderColor,
      barRadius: getComputedStyle(bar).borderRadius,
    }
  })
  expect(styles.rowRadius).toBe('0px')
  expect(styles.barRadius).toBe('12px')
  expect(styles.barBorderColor).not.toBe('rgba(56, 55, 43, 0.27)')
})

test('dark route chip matches in table and mobile cards, and en route chip stays single-line', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, {
    theme: 'dark',
    locale: 'en',
    authenticated: true,
  })
  await page.addInitScript(() => {
    if (window !== window.top || location.protocol !== 'http:') return
    localStorage.setItem('ren2hub_locale', 'en')
  })
  await page.route('**/api/token/?*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          page: 1,
          page_size: 10,
          total: 1,
          items: [
            {
              id: 7,
              name: 'Ordered Key',
              key: 'sk-****1234',
              type: 'ordered',
              selected_only: true,
              route_channel_count: 2,
              status: 1,
              remain_quota: 500_000,
              used_quota: 250_000,
              unlimited_quota: false,
              model_limits_enabled: false,
              model_limits: '',
              allow_ips: null,
              allow_user_quota_query: true,
              created_time: 1785124800,
              expired_time: -1,
            },
          ],
        },
      },
    })
  )
  await page.goto('/keys', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  const tableMetrics = await page.evaluate(() => {
    const tableChip = document.querySelector<HTMLElement>(
      'td[headers$="-type"] [data-handdrawn="chip"]'
    )!
    const tcs = getComputedStyle(tableChip)
    return {
      tableHeight: Math.round(tableChip.getBoundingClientRect().height),
      tableBg: tcs.backgroundColor,
      tableColor: tcs.color,
    }
  })
  expect(tableMetrics.tableHeight).toBeLessThanOrEqual(26)

  await page.setViewportSize({ width: 390, height: 844 })
  await waitForStablePage(page)
  const cardMetrics = await page.evaluate(() => {
    const cardChip = document.querySelector<HTMLElement>(
      '[data-key-mobile-row] .key-route-chip'
    )!
    const ccs = getComputedStyle(cardChip)
    return {
      cardBg: ccs.backgroundColor,
      cardColor: ccs.color,
    }
  })
  expect(cardMetrics.cardBg).toBe(tableMetrics.tableBg)
  expect(cardMetrics.cardColor).toBe(tableMetrics.tableColor)
})
