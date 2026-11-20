import { expect, test, type Locator, type Page } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

async function openPage(page: Page, path: string) {
  await page.goto(path, { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
}

async function assertColumnsAndCellsAligned(tableLocator: Locator) {
  const { hCols, bCols, hCells, bCells } = await tableLocator.evaluate((s) => {
    const rects = (q: string, key: 'width' | 'left') =>
      Array.from(s.querySelectorAll(q)).map(
        (e) => (e as HTMLElement).getBoundingClientRect()[key]
      )
    return {
      hCols: rects('.data-table-header-clip col', 'width'),
      bCols: rects('.data-table-body-viewport col', 'width'),
      hCells: rects('.data-table-header-clip thead tr:first-child td', 'left'),
      bCells: rects(
        '.data-table-body-viewport tbody tr:first-child td',
        'left'
      ),
    }
  })
  expect(hCols.length).toBeGreaterThan(0)
  expect(hCols.length).toBe(bCols.length)
  hCols.forEach((w, i) => expect(Math.abs(w - bCols[i])).toBeLessThanOrEqual(1))
  if (bCells.length > 0) {
    expect(hCells.length).toBe(bCells.length)
    hCells.forEach((l, i) =>
      expect(Math.abs(l - bCells[i])).toBeLessThanOrEqual(1.5)
    )
  }
}

async function assertHeaderSurfaceIntegrity(tableLocator: Locator) {
  const g = await tableLocator.evaluate((shell) => {
    const surface = shell.querySelector(
      '.data-table-header'
    ) as HTMLElement | null
    const clip = shell.querySelector(
      '.data-table-header-clip'
    ) as HTMLElement | null
    const thead = clip?.querySelector('thead') as HTMLElement | null
    const body = shell.querySelector(
      '.data-table-body-viewport'
    ) as HTMLElement | null
    if (!surface || !clip || !body || !thead)
      throw new Error('Missing table header, clip, or body element')

    const sRect = surface.getBoundingClientRect()
    const bRect = body.getBoundingClientRect()
    const gutter = Math.max(0, body.offsetWidth - body.clientWidth)

    if (gutter > 1) {
      const hitX = Math.round(bRect.right - Math.max(1, gutter / 2))
      const hitY = Math.round(sRect.top + sRect.height / 2)
      const hitEl = document.elementFromPoint(hitX, hitY)
      if (!hitEl)
        throw new Error(`elementFromPoint(${hitX}, ${hitY}) in gutter was null`)
      if (hitEl === shell || !surface.contains(hitEl))
        throw new Error('elementFromPoint did not hit surface')
    }

    const sStyle = window.getComputedStyle(surface)
    const cStyle = window.getComputedStyle(clip)
    const tStyle = window.getComputedStyle(thead)
    const isTrans = (bg: string) =>
      !bg || bg === 'transparent' || bg === 'rgba(0, 0, 0, 0)'

    return {
      leftDiff: Math.abs(sRect.left - bRect.left),
      rightDiff: Math.abs(sRect.right - bRect.right),
      clipDiff: Math.abs(clip.getBoundingClientRect().width - body.clientWidth),
      isClipTrans: isTrans(cStyle.backgroundColor),
      isTheadTrans: isTrans(tStyle.backgroundColor),
      hasSurfaceColor: !isTrans(sStyle.backgroundColor),
      hasBottomRule:
        parseFloat(sStyle.borderBottomWidth) > 0 ||
        sStyle.backgroundImage.includes('gradient'),
    }
  })

  expect(g.leftDiff).toBeLessThanOrEqual(1)
  expect(g.rightDiff).toBeLessThanOrEqual(1)
  expect(g.clipDiff).toBeLessThanOrEqual(1)
  expect(g.isClipTrans).toBe(true)
  expect(g.isTheadTrans).toBe(true)
  expect(g.hasSurfaceColor).toBe(true)
  expect(g.hasBottomRule).toBe(true)
  await assertColumnsAndCellsAligned(tableLocator)
}

const standardScenarios = [
  { path: '/keys', theme: 'light', name: 'light keys' },
  { path: '/keys', theme: 'dark', name: 'dark keys' },
  { path: '/usage-logs', theme: 'dark', name: 'dark usage logs' },
  { path: '/usage-logs', theme: 'light', name: 'light usage logs' },
  { path: '/tickets', theme: 'light', name: 'light tickets' },
  { path: '/tickets', theme: 'dark', name: 'dark tickets' },
] as const

for (const scenario of standardScenarios) {
  test(`${scenario.name}: header surface fills full width and aligns columns`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await configureStablePage(page, {
      theme: scenario.theme,
      authenticated: true,
    })
    await openPage(page, scenario.path)

    const table = page.locator('.data-table-shell').first()
    await expect(table).toBeVisible()
    await assertHeaderSurfaceIntegrity(table)
    if (scenario.theme === 'light') {
      const corners = await table.evaluate((shell) => {
        const surface = shell.querySelector('.data-table-header')!
        const card = shell.closest('[data-handdrawn="surface"]')!
        const style = getComputedStyle(surface)
        const cardStyle = getComputedStyle(card)
        return {
          left: style.borderTopLeftRadius,
          right: style.borderTopRightRadius,
          cardLeft: cardStyle.borderTopLeftRadius,
          cardRight: cardStyle.borderTopRightRadius,
        }
      })
      expect(corners.left).toBe(
        scenario.path === '/usage-logs' ? corners.cardLeft : '0px'
      )
      expect(corners.right).toBe(
        scenario.path === '/usage-logs' ? corners.cardRight : '0px'
      )
    }
    await assertNoHorizontalOverflow(page)
  })
}

test('horizontal scroll: synchronizes transform and preserves cell alignment', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1024, height: 768 })
  await configureStablePage(page, { theme: 'dark', authenticated: true })
  await openPage(page, '/usage-logs')

  const table = page.locator('.data-table-shell').first()
  await expect(table).toBeVisible()
  await assertHeaderSurfaceIntegrity(table)

  const bodyViewport = table.locator('.data-table-body-viewport')
  const { scrollWidth, clientWidth } = await bodyViewport.evaluate((el) => ({
    scrollWidth: el.scrollWidth,
    clientWidth: el.clientWidth,
  }))
  expect(scrollWidth).toBeGreaterThan(clientWidth)

  const targetScroll = Math.min(120, scrollWidth - clientWidth)
  expect(targetScroll).toBeGreaterThan(0)
  await bodyViewport.evaluate((el, s) => {
    el.scrollLeft = s
  }, targetScroll)

  await expect
    .poll(async () =>
      table
        .locator('.data-table-header-clip table')
        .evaluate((el) => el.style.transform)
    )
    .toContain(`translate3d(-${targetScroll}px`)

  await assertColumnsAndCellsAligned(table)
  await assertNoHorizontalOverflow(page)
})

test('header checkbox: keyboard focus and selection toggle maintain surface layout', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'dark', authenticated: true })
  await openPage(page, '/keys')

  const table = page.locator('.data-table-shell').first()
  await expect(table).toBeVisible()

  const checkbox = table
    .locator('.data-table-header-clip thead input[type="checkbox"]')
    .first()
  await expect(checkbox).toBeVisible()
  await checkbox.focus()
  await expect(checkbox).toBeFocused()
  await page.keyboard.press('Space')
  await expect(checkbox).toBeChecked()
  await page.keyboard.press('Space')
  await expect(checkbox).not.toBeChecked()

  await assertHeaderSurfaceIntegrity(table)
  await assertNoHorizontalOverflow(page)
})

test('empty list state: preserves header surface full width and layout integrity', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'dark', authenticated: true })
  await page.route(/\/api\/token\/(?:search)?(?:\?.*)?$/, (route) =>
    route.request().method() === 'GET'
      ? route.fulfill({
          json: {
            success: true,
            data: { items: [], total: 0, page: 1, page_size: 20 },
          },
        })
      : route.fallback()
  )
  await openPage(page, '/keys')

  const table = page.locator('.data-table-shell').first()
  await expect(table).toBeVisible()
  await expect(table.locator('[data-handdrawn="empty-state"]')).toBeVisible()

  await assertHeaderSurfaceIntegrity(table)
  await assertNoHorizontalOverflow(page)
})

test('adaptive height budget and loading: header surface height bounds 50-row body', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 750 })
  let finishLoading!: () => void
  const pending = new Promise<void>((resolve) => {
    finishLoading = resolve
  })

  await configureStablePage(page, { theme: 'dark', authenticated: true })
  await page.route(/\/api\/token\/(?:search)?(?:\?.*)?$/, async (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    await pending
    const items = Array.from({ length: 50 }, (_, i) => ({
      id: i + 1,
      name: `Token ${i + 1}`,
      key: `sk-key-${i + 1}`,
      type: 'ordered',
      selected_only: true,
      route_channel_count: 2,
      status: 1,
      remain_quota: 500000,
      unlimited_quota: false,
      used_quota: 1000 + i * 10,
      created_time: 1785124800,
      accessed_time: 1785124800,
      expired_time: -1,
    }))
    return route.fulfill({
      json: {
        success: true,
        data: { items, total: 50, page: 1, page_size: 50 },
      },
    })
  })

  const nav = page.goto('/keys', { waitUntil: 'domcontentloaded' })
  const table = page.locator('.data-table-shell').first()
  await expect(table).toBeVisible()
  await expect(table.locator('.data-table-body-viewport')).toHaveAttribute(
    'aria-busy',
    'true'
  )
  expect(await table.locator('.data-table-header').boundingBox()).not.toBeNull()

  finishLoading()
  await nav
  await waitForStablePage(page)
  await expect(table.locator('.data-table-body-viewport tbody tr')).toHaveCount(
    50
  )
  await expect(page.getByRole('alert')).toHaveCount(0)
  await assertHeaderSurfaceIntegrity(table)

  const metrics = await table.evaluate((shell) => {
    const s = shell.querySelector('.data-table-header') as HTMLElement
    const b = shell.querySelector('.data-table-body-viewport') as HTMLElement
    const boundary = document.querySelector('[data-console-scroll-boundary]')!
    const footer = shell.querySelector('.data-table-footer')!
    const maxHeight = parseFloat(b.style.maxHeight)
    return {
      surfaceHeight: s.getBoundingClientRect().height,
      maxHeight,
      remaining:
        innerHeight -
        boundary.getBoundingClientRect().bottom -
        s.getBoundingClientRect().height -
        footer.getBoundingClientRect().height -
        maxHeight -
        32,
      scrolls: b.scrollHeight > b.clientHeight,
    }
  })
  expect(metrics.surfaceHeight).toBeGreaterThanOrEqual(30)
  expect(metrics.maxHeight).toBeGreaterThan(0)
  expect(metrics.scrolls).toBe(true)
  expect(metrics.remaining).toBeGreaterThanOrEqual(0)
  expect(metrics.remaining).toBeLessThan(1)
  const lastCheckbox = table
    .locator('.data-table-body-viewport tbody input[type="checkbox"]')
    .last()
  await lastCheckbox.focus()
  await expect(lastCheckbox).toBeFocused()
  await expect
    .poll(() =>
      table.locator('.data-table-body-viewport').evaluate((el) => el.scrollTop)
    )
    .toBeGreaterThan(0)
  await assertNoHorizontalOverflow(page)
})
