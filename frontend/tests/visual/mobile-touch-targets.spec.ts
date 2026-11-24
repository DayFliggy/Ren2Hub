import { readFileSync, writeFileSync } from 'node:fs'

import { expect, test, type Locator, type Page } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

const MOBILE = { width: 390, height: 844 }
const MIN_TARGET = 44
/** Optional output for the audit: REN2HUB_TOUCH_METRICS=/path/metrics.json */
const metricsPath = process.env.REN2HUB_TOUCH_METRICS
/** Optional screenshot directory for the cross-audit. */
const shotsDir = process.env.REN2HUB_TOUCH_SHOTS
const measured: Record<string, { width: number; height: number }> = {}

async function hitBox(control: Locator, name: string) {
  // The hit area includes an expanded ::before area when the element uses the
  // shared .touch-target utility, so measure what a tap can actually reach.
  const box = await control.evaluate((element) => {
    const rect = element.getBoundingClientRect()
    const before = getComputedStyle(element, '::before')
    if (before.content === 'none' || before.position !== 'absolute')
      return { width: rect.width, height: rect.height }
    const inset = (side: string) =>
      parseFloat(before.getPropertyValue(side)) || 0
    return {
      width: rect.width - inset('left') - inset('right'),
      height: rect.height - inset('top') - inset('bottom'),
    }
  })
  measured[`${test.info().title.split(' ')[0]} ${name}`] = {
    width: Math.round(box.width * 10) / 10,
    height: Math.round(box.height * 10) / 10,
  }
  return box
}

async function expectTouchTarget(control: Locator, name: string) {
  await expect(control, name).toBeVisible()
  const box = await hitBox(control, name)
  expect(box.width, `${name} width`).toBeGreaterThanOrEqual(MIN_TARGET)
  expect(box.height, `${name} height`).toBeGreaterThanOrEqual(MIN_TARGET)
}

async function shot(page: Page, name: string) {
  if (shotsDir) await page.screenshot({ path: `${shotsDir}/${name}.png` })
}

async function expectInsideViewport(page: Page, panel: Locator) {
  const box = await panel.evaluate((element) =>
    element.getBoundingClientRect().toJSON()
  )
  const viewport = page.viewportSize()!
  expect(box.left).toBeGreaterThanOrEqual(0)
  expect(box.top).toBeGreaterThanOrEqual(0)
  expect(box.right).toBeLessThanOrEqual(viewport.width)
  expect(box.bottom).toBeLessThanOrEqual(viewport.height)
}

/** The element at the panel's centre and corners belongs to the panel, so no
    ancestor clips it and no surface paints over it. */
async function expectUnobstructed(panel: Locator) {
  const covered = await panel.evaluate((element) => {
    const rect = element.getBoundingClientRect()
    const points = [
      [rect.left + rect.width / 2, rect.top + rect.height / 2],
      // Inset past the rounded corners, which are not hit-testable.
      [rect.left + 20, rect.top + 20],
      [rect.right - 20, rect.bottom - 20],
    ]
    return points
      .filter(([x, y]) => {
        const hit = document.elementFromPoint(x, y)
        return !hit || !element.contains(hit)
      })
      .map(([x, y]) => {
        const hit = document.elementFromPoint(x, y)
        return `${Math.round(x)},${Math.round(y)} ${hit?.tagName}.${hit?.className}`
      })
  })
  expect(covered).toEqual([])
}

const now = 1785124800
const ticket = {
  id: 1,
  title: 'Production request investigation',
  status: 'replied',
  category: 'api',
  priority: 'normal',
  reply_count: 1,
  message_count: 2,
  last_reply_role: 'support',
  model_id: 'gpt-native',
  request_id: 'native-request',
  created: now,
  updated: now,
  assignee_id: null,
  assignee_name: '',
  assigned_at: 0,
  user: {
    id: 2,
    username: 'requester',
    display_name: 'Visual Requester',
    email: 'requester@example.test',
  },
  user_deleted: false,
}

async function openAdminTickets(page: Page, theme: VisualTheme, path: string) {
  await page.setViewportSize(MOBILE)
  await configureStablePage(page, { theme, authenticated: true })
  await page.route('**/api/admin/tickets/agents', (route) =>
    route.fulfill({ json: { success: true, data: { items: [] } } })
  )
  await page.route(/\/api\/admin\/tickets(?:\?.*)?$/, (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { items: [ticket], page: 1, page_size: 20, total: 1 },
      },
    })
  )
  await page.route(/\/api\/admin\/tickets\/1$/, (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          ticket,
          messages: [
            {
              id: 1,
              role: 'user',
              content: 'Please investigate this request.',
              images: [],
              created: now,
            },
          ],
        },
      },
    })
  )
  await page.goto(path, { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
}

const channels = [1, 2].map((id) => ({
  id,
  name: id === 1 ? 'OpenAI Primary' : 'OpenAI Backup',
  // Type 1 supports the balance query, so the row shows both icon actions.
  type: 1,
  lab: { id: 'openai', name: 'OpenAI', source: 'vendor' },
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
  models: 'gpt-native,gpt-native-mini',
  model_mapping: '',
}))

async function openChannels(page: Page, theme: VisualTheme) {
  await page.setViewportSize(MOBILE)
  await configureStablePage(page, { theme, authenticated: true })
  await page.addInitScript(() =>
    localStorage.setItem(
      'ren2hub_admin_channel_visible_fields_v2',
      JSON.stringify([
        'type',
        'status',
        'upstream',
        'response',
        'rowUpstreamAction',
        'rowResponseAction',
      ])
    )
  )
  await page.route(/\/api\/channel\/(?:search)?(?:\?.*)?$/, (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { items: channels, total: 2, type_counts: { '1': 2 } },
      },
    })
  )
  await page.route('**/api/channel/labs', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          total: 2,
          items: [
            { id: 'openai', name: 'OpenAI', source: 'aggregate', count: 2 },
          ],
        },
      },
    })
  )
  await page.route('**/api/channel/upstream_ratios*', (route) =>
    route.fulfill({ json: { success: true, data: [] } })
  )
  await page.goto('/channels', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
}

for (const theme of ['light', 'dark'] as const satisfies VisualTheme[]) {
  test(`${theme} admin ticket filters open unclipped panels with 44px targets`, async ({
    page,
  }) => {
    await openAdminTickets(page, theme, '/ticket-management')
    const queue = page.getByRole('complementary', { name: '工单队列' })
    for (const tab of await queue
      .locator('header button:not([role="combobox"])')
      .filter({ visible: true })
      .all())
      await expectTouchTarget(tab, `ticket status tab ${await tab.innerText()}`)

    const priority = queue.getByRole('combobox', { name: '优先级' })
    await expectTouchTarget(priority, 'ticket priority filter')
    await priority.click()
    const panel = page.getByRole('listbox')
    await expect(panel).toBeVisible()
    // The panel leaves the scrolling queue for <body>, so neither the queue's
    // overflow nor the card's rounded clip can cut it.
    expect(
      await panel.evaluate((element) => element.parentElement === document.body)
    ).toBe(true)
    await expectInsideViewport(page, panel)
    await expectUnobstructed(panel)
    const last = panel.getByRole('option').last()
    await expect(last).toBeVisible()
    await shot(page, `${theme}-admin-tickets-filter-open`)

    // Tapping inside the teleported panel selects instead of dismissing.
    await last.click()
    await expect(priority).toHaveAttribute('aria-expanded', 'false')
    await assertNoHorizontalOverflow(page)
  })

  test(`${theme} admin ticket detail back button is a 44px target`, async ({
    page,
  }) => {
    await openAdminTickets(page, theme, '/ticket-management/1')
    await expectTouchTarget(
      page.getByRole('button', { name: '返回列表' }),
      'ticket back to queue'
    )
  })

  test(`${theme} channel mobile row actions and checkbox are 44px targets`, async ({
    page,
  }) => {
    await openChannels(page, theme)
    const row = page.locator('[data-channel-mobile-row]').first()
    await expect(row).toBeVisible()
    await expectTouchTarget(
      row.locator('label:has(> .checkbox-round)'),
      'channel row checkbox'
    )
    await expectTouchTarget(
      row.getByRole('button', { name: '同步上游额度', exact: true }),
      'channel row refresh balance'
    )
    await expectTouchTarget(
      row.getByRole('button', { name: '测试响应', exact: true }),
      'channel row test'
    )
    await expectTouchTarget(
      page.getByRole('button', { name: '测试 OpenAI 渠道的响应' }),
      'channel group test action'
    )
    await row.scrollIntoViewIfNeeded()
    await shot(page, `${theme}-channel-mobile-row`)

    await row.getByRole('button', { name: '测试响应', exact: true }).click()
    const dialog = page.getByRole('dialog').filter({ visible: true }).last()
    const modelTest = dialog
      .getByRole('button', { name: /^测试模型/ })
      .filter({ visible: true })
      .first()
    await modelTest.scrollIntoViewIfNeeded()
    await expectTouchTarget(modelTest, 'channel test modal row action')
  })

  test(`${theme} log date range picker opens in view with 44px actions`, async ({
    page,
  }) => {
    await page.setViewportSize(MOBILE)
    await configureStablePage(page, { theme, authenticated: true })
    await page.goto('/logs', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)
    const trigger = page
      .getByRole('button', { name: '选择日期范围' })
      .filter({ visible: true })
      .first()
    await expectTouchTarget(trigger, 'date range trigger')
    await trigger.click()
    const panel = page.getByRole('dialog', { name: '选择日期范围' })
    await expect(panel).toBeVisible()
    await expectInsideViewport(page, panel)
    await expectUnobstructed(panel)
    await panel.locator('[data-date-key][data-in-month="true"]').nth(2).click()
    await panel.locator('[data-date-key][data-in-month="true"]').nth(5).click()
    await expect(panel).toHaveCount(0)
    await expectTouchTarget(
      page
        .getByRole('button', { name: '清空', exact: true })
        .filter({ visible: true })
        .first(),
      'date range trigger clear'
    )
    await trigger.click()
    await expect(panel).toBeVisible()
    await expectTouchTarget(
      panel.getByRole('button', { name: '清空', exact: true }),
      'date range footer clear'
    )
    await expectTouchTarget(
      panel.getByRole('button', { name: '确定', exact: true }),
      'date range apply'
    )
    await shot(page, `${theme}-date-range-open`)
  })

  test(`${theme} token routing modal row actions are 44px targets`, async ({
    page,
  }) => {
    await page.setViewportSize(MOBILE)
    await configureStablePage(page, { theme, authenticated: true })
    await page.goto('/keys', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)
    await page
      .locator('[data-key-mobile-row] [data-key-route-trigger]')
      .first()
      .click()
    const dialog = page.getByRole('dialog', { name: '编辑令牌', exact: true })
    await expect(dialog).toBeVisible()
    const row = dialog.locator('li[data-selected-channel-id="2"]')
    await expect(row).toBeVisible()
    for (const name of ['上移', '下移', '移除'])
      await expectTouchTarget(
        row.getByRole('button', { name }),
        `routing ${name}`
      )
    await row.scrollIntoViewIfNeeded()
    await shot(page, `${theme}-token-routing-row`)
  })
}

/** Every top the panel paints during the next frames, via rAF sampling. */
async function recordPanelTops(page: Page, selector: string, frames = 12) {
  return page.evaluate(
    ([target, count]) =>
      new Promise<{ top: number; visible: boolean }[]>((resolve) => {
        const samples: { top: number; visible: boolean }[] = []
        const sample = () => {
          const element = document.querySelector<HTMLElement>(target)
          if (element)
            samples.push({
              top: Math.round(element.getBoundingClientRect().top),
              visible: getComputedStyle(element).visibility === 'visible',
            })
          if (samples.length >= count) resolve(samples)
          else requestAnimationFrame(sample)
        }
        requestAnimationFrame(sample)
      }),
    [selector, frames] as const
  )
}

test('a scrolled select panel follows its trigger without flashing or losing its list scroll', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 640 })
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.goto('/logs', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
  const trigger = page
    .getByRole('combobox', { name: '类型' })
    .filter({ visible: true })
    .first()
  await trigger.click()
  const panel = page.getByRole('listbox')
  await expect(panel).toBeVisible()
  // Cap the list so it scrolls, then scroll it and the page.
  await panel.evaluate((element) => {
    element.style.maxHeight = '96px'
    element.scrollTop = 40
  })
  const gap = async () => {
    const [anchor, box] = await Promise.all([
      trigger.boundingBox(),
      panel.boundingBox(),
    ])
    return Math.round(box!.y - (anchor!.y + anchor!.height))
  }
  const before = await gap()
  const recording = recordPanelTops(page, '[role="listbox"]')
  await page
    .locator('[data-console-scroll-zone]')
    .evaluate((zone) => zone.scrollBy(0, 30))
  const tops = await recording
  // Each painted frame is a final placement: always visible, and only ever
  // one of the two positions (before and after the scroll).
  expect(tops.every((sample) => sample.visible)).toBe(true)
  expect(new Set(tops.map((sample) => sample.top)).size).toBeLessThanOrEqual(2)
  expect(await gap()).toBe(before)
  expect(await panel.evaluate((element) => element.scrollTop)).toBe(40)
})

test('ChipPicker panel opened upward moves with its content as matches shrink', async ({
  page,
}) => {
  // A short phone keeps the create dialog scrollable, so the field can sit low.
  await page.setViewportSize({ width: MOBILE.width, height: 640 })
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.route('**/api/user/models', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: Array.from({ length: 8 }, (_, i) => `model-${i}`),
      },
    })
  )
  await page.goto('/keys', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
  await page.getByRole('button', { name: '创建令牌', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '创建令牌', exact: true })
  await dialog.getByRole('button', { name: /高级设置/ }).click()
  const input = dialog.getByRole('textbox', { name: '搜索模型…' })
  await input.scrollIntoViewIfNeeded()
  // Put the field low in the viewport so the panel opens upward.
  await input.evaluate((element) => {
    const body = element.closest('.subtle-scroll')!
    body.scrollTop -= innerHeight - element.getBoundingClientRect().bottom - 60
  })
  await input.click()
  const panel = page.locator('[data-floating-panel]')
  await expect(panel).toBeVisible()
  const gapAbove = async () => {
    const [field, box] = await Promise.all([
      input.boundingBox(),
      panel.boundingBox(),
    ])
    return Math.round(field!.y - (box!.y + box!.height))
  }
  const opened = await gapAbove()
  expect(opened).toBeGreaterThanOrEqual(0)
  const tallBefore = (await panel.boundingBox())!.height
  await input.pressSequentially('model-1')
  await expect
    .poll(async () => (await panel.boundingBox())!.height)
    .toBeLessThan(tallBefore)
  // Re-placed against the field rather than left where the taller list
  // ended; the shorter list may now fit on the preferred side below.
  const gapBelow = async () => {
    const [field, box] = await Promise.all([
      input.boundingBox(),
      panel.boundingBox(),
    ])
    return Math.round(box!.y - (field!.y + field!.height))
  }
  await expect
    .poll(async () => [await gapAbove(), await gapBelow()])
    .toContainEqual(opened)

  // Keyboard users reach the options from the input.
  await input.press('Tab')
  await expect(panel.getByRole('button', { name: 'model-1' })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(dialog.getByText('model-1', { exact: true })).toBeVisible()
})

test('public navigation drawer locks the page and keeps its scroll position', async ({
  page,
}) => {
  await page.setViewportSize(MOBILE)
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.goto('/pricing', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
  await page.evaluate(() => {
    document.body.style.minHeight = '3000px'
    window.scrollTo({ top: 400, behavior: 'instant' })
  })
  const menu = page.locator(
    '.app-navbar button[aria-controls="mobile-navigation-drawer"]'
  )
  await menu.click()
  await expect(page.locator('#mobile-navigation-drawer')).toBeVisible()
  await page.mouse.move(40, 600)
  await page.mouse.wheel(0, 600)
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(400)

  await page.keyboard.press('Escape')
  await expect(page.locator('#mobile-navigation-drawer')).toHaveCount(0)
  expect(await page.evaluate(() => window.scrollY)).toBe(400)
  await page.mouse.wheel(0, 300)
  await expect
    .poll(() => page.evaluate(() => window.scrollY))
    .toBeGreaterThan(400)
})

test.afterEach(() => {
  if (!metricsPath) return
  let previous: Record<string, unknown> = {}
  try {
    previous = JSON.parse(readFileSync(metricsPath, 'utf8'))
  } catch {
    // First test of the run creates the file.
  }
  writeFileSync(
    metricsPath,
    JSON.stringify({ ...previous, ...measured }, null, 2) + '\n'
  )
})
