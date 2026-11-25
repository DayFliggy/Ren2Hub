import { expect, test, type Page } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

const channels = [
  {
    id: 1,
    name: 'Anthropic Primary',
    type: 14,
    lab: { id: 'anthropic', name: 'Anthropic', source: 'vendor' },
    status: 1,
    priority: 10,
    weight: 100,
    in_flight: 12,
    max_concurrency: 100,
    channel_ratio: '1',
    used_quota: 500_000,
    balance: 120.5,
    response_time: 420,
    test_time: 1_786_742_400,
    base_url: '',
    models: 'claude-sonnet-4-5',
    model_mapping: '',
  },
  {
    id: 2,
    name: 'OpenAI Main',
    type: 1,
    lab: { id: 'openai', name: 'OpenAI', source: 'vendor' },
    status: 2,
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
    models: 'gpt-4o',
    model_mapping: '',
  },
]

/** Serves the native channel list and records each list or search request. */
async function configureChannels(
  page: Page,
  options: { withUpstreams?: boolean; withRatios?: boolean } = {}
): Promise<URL[]> {
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
  await page.route('**/api/channel/upstream_ratios*', (route) => {
    if (!options.withRatios) {
      return route.fulfill({ json: { success: true, data: [] } })
    }
    const url = new URL(route.request().url())
    const ids = (url.searchParams.get('ids') ?? '')
      .split(',')
      .map(Number)
      .filter(Boolean)
    return route.fulfill({
      json: {
        success: true,
        data: ids.map((id) => ({
          channel_id: id,
          kind: id === 1 ? 'newapi' : 'unsupported',
          source: id === 1 ? 'newapi' : '',
          mode: 'auto',
          status: id === 1 ? 'ok' : '',
          ratio: id === 1 ? '1.25' : '',
          effective_ratio: id === 1 ? '1.25' : '',
          group_name: id === 1 ? 'vip' : '',
          mapping: id === 1 ? 'account' : '',
          token_name: id === 1 ? 'vip' : '',
          stale: false,
          received_at: 1_786_742_400,
          next_probe_at: 1_786_744_200,
          last_error: '',
          version: 1,
          groups: [{ name: 'vip', ratio: 1.25, description: 'VIP' }],
        })),
      },
    })
  })
  if (options.withUpstreams) {
    await page.route('**/api/admin/upstreams/bindings*', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: [
            {
              id: 1,
              account_id: 4,
              channel_id: 1,
              channel_created_time: 1_786_742_000,
              remote_key_id: 14,
              remote_status: 'active',
              stale: false,
              ratio_sync_enabled: true,
              ratio_sync_factor: '1.25',
              ratio_sync_error: '',
            },
          ],
        },
      })
    )
  }
  return requests
}

async function openChannels(
  page: Page,
  theme: VisualTheme,
  size: { width: number; height: number },
  options: {
    withUpstreams?: boolean
    withRatios?: boolean
    user?: Record<string, unknown>
  } = {}
): Promise<URL[]> {
  await page.setViewportSize(size)
  await configureStablePage(page, {
    theme,
    authenticated: true,
    user: options.user,
  })
  const requests = await configureChannels(page, options)
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
  test(`${theme} desktop channel toolbar stays single-row with default flat table and zero horizontal table overflow`, async ({
    page,
  }) => {
    await openChannels(
      page,
      theme,
      { width: 1440, height: 900 },
      { withUpstreams: true, withRatios: true }
    )

    const toolbar = await controlBoxes(page, '[data-channel-toolbar]')
    expect(toolbar.map((control) => control.name)).toEqual([
      '搜索范围 渠道',
      '搜索名称、ID、密钥或上游地址',
      '渠道状态',
      '渠道类型',
      '渠道厂商',
      '上游账号',
      '刷新列表',
      '字段设置',
      '更多操作与视图',
      '新建渠道',
    ])
    expect(new Set(toolbar.map((control) => control.top)).size).toBe(1)
    expect(new Set(toolbar.map((control) => control.height))).toEqual(
      new Set([32])
    )

    await expect(page.locator('[data-page-header]')).toHaveCount(0)
    await expect(
      page.getByRole('navigation', { name: '面包屑导航' })
    ).toHaveCount(0)

    // Flat by default: no vendor group rows until opted in.
    await expect(page.locator('[data-table-group-row]')).toHaveCount(0)

    // Default columns fit inside 1440px with zero horizontal scroll and unshrunk action buttons.
    const tableMetrics = await page.evaluate(() => {
      const viewport = document.querySelector(
        '.data-table-body-viewport'
      ) as HTMLElement | null
      const actionBtns = [
        ...document.querySelectorAll('td[headers$="-actions"] button'),
      ]
        .filter((el) => (el as HTMLElement).offsetParent !== null)
        .map((el) => Math.round(el.getBoundingClientRect().width))
      const bindingLink = document.querySelector(
        '[data-channel-upstream-binding] a'
      ) as HTMLElement | null
      const disabledRow = document.querySelectorAll(
        '.data-table-body-viewport tbody tr'
      )[1] as HTMLElement | undefined
      return {
        overflowX: viewport ? viewport.scrollWidth - viewport.clientWidth : -1,
        minActionBtnWidth: actionBtns.length ? Math.min(...actionBtns) : 0,
        linkColor: bindingLink ? getComputedStyle(bindingLink).color : '',
        accentText: getComputedStyle(document.documentElement)
          .getPropertyValue('--accent-text')
          .trim(),
        disabledOpacity: disabledRow
          ? getComputedStyle(disabledRow).opacity
          : '',
      }
    })
    expect(tableMetrics.overflowX).toBeLessThanOrEqual(1)
    expect(tableMetrics.minActionBtnWidth).toBeGreaterThanOrEqual(30)
    expect(tableMetrics.disabledOpacity).toBe('1')

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

  test(`${theme} tablet 768px renders mobile cards with uniform 44px toolbar height`, async ({
    page,
  }) => {
    await openChannels(
      page,
      theme,
      { width: 768, height: 1024 },
      { withRatios: true }
    )

    const toolbar = await controlBoxes(page, '[data-channel-toolbar]')
    for (const control of toolbar) {
      expect(control.height, control.name).toBeGreaterThanOrEqual(44)
    }
    await expect(
      page.locator('[data-channel-mobile-row]').first()
    ).toBeVisible()
    await expect(page.locator('.data-table-shell')).toBeHidden()
    await assertNoHorizontalOverflow(page)
  })

  test(`${theme} mobile channel toolbar is compact and mobile card exposes upstream ratio`, async ({
    page,
  }) => {
    await openChannels(
      page,
      theme,
      { width: 390, height: 844 },
      { withRatios: true }
    )

    const toolbar = await controlBoxes(page, '[data-channel-toolbar]')
    expect(new Set(toolbar.map((control) => control.top)).size).toBe(3)
    for (const control of toolbar)
      expect(control.height, control.name).toBeGreaterThanOrEqual(44)

    await expect(page.locator('[data-channel-mobile-group]')).toHaveCount(0)
    const ratioBtn = page
      .locator('[data-channel-mobile-upstream-ratio]')
      .first()
    await expect(ratioBtn).toBeVisible()
    await ratioBtn.click()
    const ratioDialog = page.getByRole('dialog', {
      name: /上游倍率 · Anthropic Primary/,
    })
    await expect(ratioDialog).toBeVisible()
    await ratioDialog.getByRole('button', { name: '取消', exact: true }).click()
    await expect(ratioDialog).toHaveCount(0)

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

test('unified toolbar menu toggles vendor grouping, sort order, balance task and respects permissions', async ({
  page,
}) => {
  const requests = await openChannels(page, 'dark', {
    width: 1440,
    height: 900,
  })
  await page.route('**/api/channel/update_balance', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { task_id: 'bal-1', status: 'succeeded' },
      },
    })
  )

  // Grouping starts off; toggling inside unified menu activates vendor groups.
  await expect(page.locator('[data-table-group-row]')).toHaveCount(0)
  await page.locator('[data-channel-toolbar-menu-trigger]').click()
  const menu = page.locator('[data-channel-toolbar-menu]')
  await expect(menu).toBeVisible()
  await menu.locator('[data-channel-group-toggle]').click()
  await expect(page.locator('[data-table-group-row]')).toHaveCount(2)

  // Change sort field and direction from the unified menu.
  await menu.locator('[data-channel-sort-option="balance"]').click()
  await expect
    .poll(() => requests.at(-1)?.searchParams.get('sort_by'))
    .toBe('balance')
  await menu.getByRole('button', { name: '升序排列' }).click()
  await expect
    .poll(() => requests.at(-1)?.searchParams.get('sort_order'))
    .toBe('asc')

  // Triggering update-all balance closes menu and shows status bar.
  await menu.locator('[data-channel-menu-balance-all]').click()
  await expect(menu).toHaveCount(0)
  await expect(page.locator('[data-channel-balance-status]')).toContainText(
    '余额更新完成'
  )

  // Read-only admin without operate or sensitive_write sees no create button and no batch tools.
  await openChannels(
    page,
    'light',
    { width: 1440, height: 900 },
    {
      user: {
        role: 10,
        permissions: {
          admin_permissions: {
            channel: {
              read: true,
              write: false,
              operate: false,
              sensitive_write: false,
            },
          },
        },
      },
    }
  )
  await expect(page.getByRole('button', { name: '新建渠道' })).toHaveCount(0)
  await page.locator('[data-channel-toolbar-menu-trigger]').click()
  await expect(page.locator('[data-channel-menu-balance-all]')).toHaveCount(0)
  await expect(page.locator('[data-channel-group-toggle]')).toBeVisible()
})

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
