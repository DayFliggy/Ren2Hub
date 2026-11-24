import { expect, test, type Locator } from '@playwright/test'

import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

// The key table needs a 60rem card. The sidebar appears at 1024px, so a
// 1024px viewport has a narrower card (cards) than a 1023px one (table).
const layouts = [
  { width: 768, height: 1024, table: false },
  { width: 1023, height: 768, table: true },
  { width: 1024, height: 768, table: false },
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

function overlaps(
  a: { x: number; y: number; width: number; height: number },
  b: { x: number; y: number; width: number; height: number }
) {
  return (
    a.x < b.x + b.width &&
    b.x < a.x + a.width &&
    a.y < b.y + b.height &&
    b.y < a.y + a.height
  )
}

for (const theme of ['light', 'dark'] as const satisfies VisualTheme[]) {
  test(`${theme} desktop key detail clears the hovered row and its actions`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await configureStablePage(page, { theme, authenticated: true })
    await page.goto('/keys', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const row = page.locator('[data-key-page] tbody tr').first()
    const ring = row.locator('[data-key-quota-ring]')
    const edit = row.getByRole('button', { name: '编辑令牌', exact: true })
    const popover = page.locator('[data-key-quota-popover]')
    await row.locator('td[headers$="-name"]').hover()
    await expect(popover).toBeVisible()
    await expect(popover).toContainText('Native Key')
    await expect(ring).toHaveAttribute('aria-expanded', 'true')

    const [rowBox, ringBox, editBox, box] = await Promise.all([
      row.boundingBox(),
      ring.boundingBox(),
      edit.boundingBox(),
      popover.boundingBox(),
    ])
    // Right of the ring, off the hovered row, so its buttons stay in view.
    expect(box!.x).toBeGreaterThanOrEqual(ringBox!.x + ringBox!.width)
    expect(overlaps(box!, rowBox!)).toBe(false)
    expect(overlaps(box!, editBox!)).toBe(false)
    expect(box!.x + box!.width).toBeLessThanOrEqual(1440 - 15)
    expect(box!.y + box!.height).toBeLessThanOrEqual(900 - 15)

    // Opened by hover it lets the pointer through to the rows it covers.
    const hitsPopover = () =>
      popover.evaluate((el) => {
        const { x, y, width, height } = el.getBoundingClientRect()
        return el.contains(
          document.elementFromPoint(x + width / 2, y + height / 2)
        )
      })
    expect(await hitsPopover()).toBe(false)

    // Reaching the actions column closes it.
    await edit.hover()
    await expect(popover).toBeHidden()

    // Pinned from the keyboard it takes the pointer, so a click on it cannot
    // land on a button it hides.
    await ring.focus()
    await page.keyboard.press('Enter')
    await expect(popover).toHaveAttribute('data-pinned', 'true')
    expect(await hitsPopover()).toBe(true)
    await page.keyboard.press('Escape')
    await expect(popover).toBeHidden()
  })

  test(`${theme} mobile key detail expands inside its card`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await configureStablePage(page, { theme, authenticated: true })
    await page.goto('/keys', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const card = page.locator('[data-key-mobile-row]').first()
    const bar = card.locator('button[data-key-quota-ring]')
    await expect(bar.locator('[data-key-remaining]')).toHaveText('$1.000')
    const barBox = (await bar.boundingBox())!
    expect(barBox.height).toBeGreaterThanOrEqual(44)

    await bar.click()
    await expect(bar).toHaveAttribute('aria-expanded', 'true')
    const region = card.getByRole('region')
    await expect(region).toBeVisible()
    await expect(region).toContainText('查询账户额度')
    await expect(page.locator('[data-key-quota-popover]')).toHaveCount(0)
    // Nothing floats over the card, so every control stays reachable.
    await assertNoHorizontalOverflow(page)
    await assertInteractiveCentersVisible(page)

    await bar.click()
    await expect(region).toBeHidden()
  })
}
