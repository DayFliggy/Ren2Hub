import { expect, test, type Page } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

const channels = [1, 2].map((id) => ({
  id,
  name: id === 1 ? 'Anthropic Primary' : 'OpenAI Main',
  type: id === 1 ? 14 : 1,
  lab:
    id === 1
      ? { id: 'anthropic', name: 'Anthropic', source: 'vendor' }
      : { id: 'openai', name: 'OpenAI', source: 'vendor' },
  status: 1,
  priority: 0,
  weight: 0,
  in_flight: 0,
  max_concurrency: 0,
  channel_ratio: '1',
  used_quota: 0,
  balance: 0,
  response_time: 0,
  test_time: 0,
  base_url: '',
  models: 'claude-sonnet-4-5',
  model_mapping: '',
}))

/** Serves the native channel list and records each list or search request. */
async function configureChannels(page: Page): Promise<URL[]> {
  const requests: URL[] = []
  await page.route(/\/api\/channel\/(?:search)?(?:\?.*)?$/, (route) => {
    requests.push(new URL(route.request().url()))
    return route.fulfill({
      json: {
        success: true,
        data: {
          items: channels,
          total: channels.length,
          type_counts: { '1': 1, '14': 1 },
        },
      },
    })
  })
  await page.route('**/api/channel/labs', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          total: 5,
          items: [
            {
              id: 'anthropic',
              name: 'Anthropic',
              source: 'aggregate',
              count: 1,
            },
            { id: 'openai', name: 'OpenAI', source: 'aggregate', count: 1 },
            { id: 'unknown', name: 'unknown', source: 'aggregate', count: 3 },
          ],
        },
      },
    })
  )
  await page.route('**/api/channel/upstream_ratios*', (route) =>
    route.fulfill({ json: { success: true, data: [] } })
  )
  return requests
}

async function openChannels(
  page: Page,
  theme: VisualTheme,
  size: { width: number; height: number }
): Promise<URL[]> {
  await page.setViewportSize(size)
  await configureStablePage(page, { theme, authenticated: true })
  const requests = await configureChannels(page)
  await page.goto('/channels', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
  await expect(
    page.getByText('Anthropic Primary').filter({ visible: true }).first()
  ).toBeVisible()
  return requests
}

/** Visible toolbar controls with their rounded boxes, in DOM order. */
function controlBoxes(page: Page, selector: string) {
  return page.locator(selector).evaluate((root) =>
    [...root.querySelectorAll('input, button')]
      .filter((element) => (element as HTMLElement).offsetParent !== null)
      .map((element) => {
        const box = element.getBoundingClientRect()
        // aria-labelledby names a combobox by its label and selected value.
        const labelledBy = element
          .getAttribute('aria-labelledby')
          ?.split(' ')
          .map((id) => document.getElementById(id)?.textContent?.trim())
          .join(' ')
        return {
          name:
            labelledBy ??
            element.getAttribute('aria-label') ??
            (element as HTMLInputElement).placeholder ??
            element.textContent?.trim(),
          top: Math.round(box.top),
          height: Math.round(box.height),
          left: box.left,
          right: box.right,
        }
      })
  )
}

for (const theme of [
  'light',
  'dark',
] as const satisfies readonly VisualTheme[]) {
  test(`${theme} desktop channel toolbar keeps every filter on one row`, async ({
    page,
  }) => {
    await openChannels(page, theme, { width: 1440, height: 900 })

    const toolbar = await controlBoxes(page, '[data-channel-toolbar]')
    expect(toolbar.map((control) => control.name)).toEqual([
      '搜索范围 渠道',
      '搜索名称、ID、密钥或上游地址',
      '渠道状态',
      '渠道类型',
      '渠道厂商',
      '更新全部渠道余额',
      '刷新列表',
      '字段设置',
      '排序字段 ID',
      '降序排列',
      '新建渠道',
    ])
    // The page has no header row: sort and create close the toolbar row.
    expect(new Set(toolbar.map((control) => control.top)).size).toBe(1)
    expect(new Set(toolbar.map((control) => control.height))).toEqual(
      new Set([32])
    )

    await expect(page.locator('[data-page-header]')).toHaveCount(0)
    await expect(
      page.getByRole('navigation', { name: '面包屑导航' })
    ).toHaveCount(0)

    // The ledger keeps dashed facets; Studio uses a solid semantic outline.
    const status = page.getByRole('combobox', { name: '渠道状态' })
    await expect(status).toHaveCSS(
      'border-top-style',
      theme === 'dark' ? 'solid' : 'dashed'
    )
    if (theme === 'dark') {
      const outline = await status.evaluate((element) =>
        getComputedStyle(element).getPropertyValue('--outline').trim()
      )
      expect(outline).not.toBe('')
      await expect(status).toHaveCSS('border-top-color', outline)
    }
    await assertNoHorizontalOverflow(page)
  })

  test(`${theme} mobile channel toolbar stays compact and in view`, async ({
    page,
  }) => {
    await openChannels(page, theme, { width: 390, height: 844 })

    const toolbar = await controlBoxes(page, '[data-channel-toolbar]')
    // The merged search, the chip row, the batch sync and icon actions, then
    // the result count with sort and create.
    expect(new Set(toolbar.map((control) => control.top)).size).toBe(4)
    for (const control of toolbar)
      expect(control.height, control.name).toBeGreaterThanOrEqual(44)

    await page.getByRole('combobox', { name: '渠道厂商' }).click()
    const panel = await page
      .getByRole('listbox')
      .evaluate((element) => element.getBoundingClientRect().toJSON())
    expect(panel.left).toBeGreaterThanOrEqual(0)
    expect(panel.right).toBeLessThanOrEqual(390)
    await expect(page.getByRole('option', { name: /未识别厂商/ })).toBeVisible()
    await page.keyboard.press('Escape')
    await assertNoHorizontalOverflow(page)
  })
}

for (const theme of [
  'light',
  'dark',
] as const satisfies readonly VisualTheme[]) {
  test(`${theme} mobile bulk bar stays pinned while the list scrolls`, async ({
    page,
  }) => {
    await openChannels(page, theme, { width: 390, height: 500 })
    await page.getByRole('checkbox', { name: '选择当前页渠道' }).check()
    const bar = page.locator('[data-channel-bulk-actions]')
    await expect(bar).toBeVisible()
    const zone = page.locator('[data-console-scroll-zone]')
    await zone.evaluate((element) => element.scrollTo({ top: 10_000 }))
    await expect
      .poll(() => zone.evaluate((element) => element.scrollTop))
      .toBeGreaterThan(200)
    const [barBox, zoneBox] = await Promise.all([
      bar.boundingBox(),
      zone.boundingBox(),
    ])
    expect(barBox).not.toBeNull()
    expect(zoneBox).not.toBeNull()
    // Pinned to the scroll zone's top edge, on an opaque surface.
    expect(Math.abs(barBox!.y - zoneBox!.y)).toBeLessThanOrEqual(1)
    const background = await bar.evaluate(
      (element) => getComputedStyle(element).backgroundImage
    )
    expect(background).toContain('linear-gradient')
    await assertNoHorizontalOverflow(page)
  })
}

test('channel filter chips query the native list and reset together', async ({
  page,
}) => {
  const requests = await openChannels(page, 'light', {
    width: 1440,
    height: 900,
  })
  const status = page.getByRole('combobox', { name: '渠道状态' })

  await status.click()
  await page.getByRole('option', { name: '已禁用' }).click()
  await expect(status.locator('[data-facet-value]')).toHaveText('已禁用')
  await expect
    .poll(() => requests.at(-1)?.searchParams.get('status'))
    .toBe('disabled')

  // One box serves both native search parameters; the scope picks which.
  const search = page.getByRole('searchbox', {
    name: '搜索名称、ID、密钥或上游地址',
  })
  await search.fill('claude')
  await expect
    .poll(() => requests.at(-1)?.searchParams.get('keyword'))
    .toBe('claude')
  expect(requests.at(-1)?.pathname).toBe('/api/channel/search')
  expect(requests.at(-1)?.searchParams.has('model')).toBe(false)
  await page.getByRole('combobox', { name: /^搜索范围/ }).click()
  await page.getByRole('option', { name: '模型', exact: true }).click()
  await expect
    .poll(() => requests.at(-1)?.searchParams.get('model'))
    .toBe('claude')
  expect(requests.at(-1)?.searchParams.has('keyword')).toBe(false)
  await expect(
    page.getByRole('searchbox', { name: '搜索模型名称' })
  ).toHaveValue('claude')

  await page.getByRole('button', { name: '重置', exact: true }).click()
  await expect(status.locator('[data-facet-value]')).toHaveCount(0)
  await expect.poll(() => requests.at(-1)?.pathname).toBe('/api/channel/')
  expect(requests.at(-1)?.searchParams.has('status')).toBe(false)
  expect(requests.at(-1)?.searchParams.has('model')).toBe(false)
  await expect(
    page.getByRole('button', { name: '重置', exact: true })
  ).toHaveCount(0)
})
