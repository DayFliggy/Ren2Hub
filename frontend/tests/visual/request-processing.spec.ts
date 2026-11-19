import { expect, test, type Locator, type Page } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  captureEvidence,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

const plugins = [
  {
    key: 'pii-redact',
    name: 'PII Redact',
    description: { en: 'Masks email addresses.', zh: '遮盖邮箱地址。' },
    icon: 'text:PI',
    models: null,
    transform: true,
    transform_hooks: ['request'],
    configSchema: {
      type: 'object',
      properties: { mask: { type: 'string', title: 'Mask text' } },
    },
  },
  {
    key: 'reply-guard',
    name: 'Reply Guard',
    description: { en: 'Stops blocked replies.', zh: '回复包含屏蔽词时停止。' },
    icon: 'text:RG',
    models: null,
    transform: true,
    transform_hooks: ['reply'],
    optional: true,
  },
]
const instances = [
  {
    id: 'a1',
    name: 'Site redaction with a deliberately long instance name',
    plugin: 'pii-redact',
    enabled: true,
    global: true,
    models: [],
    config: { mask: '[redacted]' },
  },
  {
    id: 'b2',
    name: 'Reply guard',
    plugin: 'reply-guard',
    enabled: false,
    global: false,
    models: ['gpt-4.1-mini', 'claude-sonnet-4-5'],
    config: {},
  },
]
const scopedPlugin = {
  key: 'scoped-guard',
  name: 'Scoped Guard',
  description: { en: 'Checks Claude requests.', zh: '检查 Claude 请求。' },
  icon: 'text:SG',
  models: null,
  transform: true,
  transform_hooks: ['outbound'],
  scope: {
    models: ['claude-sonnet-4-5', 'gpt-4.1-mini'],
    channelTypes: [1, 14],
  },
}
// Confirmed before the plugin widened its scope, so it waits for review.
const scopedInstance = {
  id: 'c3',
  name: 'Scoped guard',
  plugin: 'scoped-guard',
  enabled: true,
  global: false,
  models: [],
  config: {},
  plugin_scope: { models: ['claude-sonnet-4-5'], channel_types: [1] },
}
const longLine = `My email is alice@example.com. ${'Summarize the notes. '.repeat(12)}`
const dryRun = {
  rejected: false,
  failed: false,
  steps: [
    {
      instance: 'a1',
      plugin: 'pii-redact',
      stage: 'request',
      action: 'continue',
      effects: 1,
      duration_ms: 3,
    },
  ],
  body: {
    model: 'gpt-4.1-mini',
    messages: [
      {
        role: 'user',
        content: longLine.replace('alice@example.com', '[redacted]'),
      },
    ],
  },
}
const ok = (data: unknown) => ({ json: { success: true, message: '', data } })

async function openPage(
  page: Page,
  theme: 'light' | 'dark',
  width: number,
  list: unknown[],
  options: unknown[] = plugins
) {
  await page.setViewportSize({ width, height: 900 })
  await configureStablePage(page, {
    theme,
    authenticated: true,
    locale: 'zh-CN',
  })
  await page.route('**/api/task_plugin_options', (route) =>
    route.fulfill(ok(options))
  )
  await page.route('**/api/request_plugin/instances', (route) =>
    route.fulfill(ok({ instances: list }))
  )
  await page.route('**/api/request_plugin/dry_run', (route) =>
    route.fulfill(ok(dryRun))
  )
  await page.route('**/api/channel/models_enabled', (route) =>
    route.fulfill(ok(['gpt-4.1-mini', 'claude-sonnet-4-5']))
  )
  await page.goto('/request-processing')
  await waitForStablePage(page)
}

// The console scroll zone is the only scroll container; nothing may widen it.
async function assertFits(page: Page) {
  await assertNoHorizontalOverflow(page)
  const zone = page.locator('[data-console-scroll-zone]')
  const spill = await zone.evaluate((el) => el.scrollWidth - el.clientWidth)
  expect(spill).toBeLessThanOrEqual(1)
}

async function insideViewport(page: Page, target: Locator) {
  const box = await target.boundingBox()
  const width = page.viewportSize()!.width
  expect(box).not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1)
}

for (const [theme, width] of [
  ['light', 1440],
  ['dark', 390],
] as const) {
  const desktop = width > 600

  test(`${theme} ${width} request processing list, empty state and picker`, async ({
    page,
  }) => {
    await openPage(page, theme, width, [])
    await expect(page.getByText('暂无请求处理', { exact: true })).toBeVisible()
    await insideViewport(
      page,
      page.getByRole('button', { name: '添加', exact: true })
    )
    await assertFits(page)

    await openPage(page, theme, width, instances)
    const rows = page.locator('[data-request-plugin-row]')
    await expect(rows).toHaveCount(2)
    await expect(
      page.getByRole('switch', { name: `启用 ${instances[1]!.name}` })
    ).toBeVisible()
    await assertFits(page)
    await captureEvidence(page, { path: test.info().outputPath('list.png') })

    await page.getByRole('button', { name: '添加', exact: true }).click()
    const cards = page.locator('[data-plugin-card]')
    await expect(cards).toHaveCount(2)
    const [first, second] = await Promise.all([
      cards.nth(0).boundingBox(),
      cards.nth(1).boundingBox(),
    ])
    // Two columns on desktop as upstream; stacked cards on phones.
    if (desktop) expect(Math.abs(first!.y - second!.y)).toBeLessThanOrEqual(1)
    else expect(second!.y).toBeGreaterThan(first!.y + first!.height - 1)
    await insideViewport(page, cards.nth(1))
    await assertFits(page)
  })

  test(`${theme} ${width} request processing dry run keeps long JSON inside the dialog`, async ({
    page,
  }) => {
    await openPage(page, theme, width, instances)
    await page.getByRole('button', { name: '试运行', exact: true }).click()
    const dialog = page.locator('[data-dry-run-dialog]')
    await dialog.locator('textarea').fill(
      JSON.stringify(
        {
          model: 'gpt-4.1-mini',
          messages: [{ role: 'user', content: longLine }],
        },
        null,
        2
      )
    )
    await dialog.getByRole('button', { name: '运行', exact: true }).click()
    await expect(dialog.getByText('结果', { exact: true })).toBeVisible()
    await expect(
      dialog.getByRole('cell', { name: '通过', exact: true })
    ).toBeVisible()

    // The diff scrolls sideways inside its own region instead of widening the dialog.
    const diff = dialog.getByRole('region').last()
    await diff.scrollIntoViewIfNeeded()
    await insideViewport(page, diff)
    const scrolls = await diff.evaluate((el) => el.scrollWidth > el.clientWidth)
    expect(scrolls).toBe(true)
    await assertFits(page)
    await captureEvidence(page, { path: test.info().outputPath('dry-run.png') })
  })
}

for (const [theme, width] of [
  ['light', 1440],
  ['dark', 1440],
  ['light', 390],
  ['dark', 390],
] as const) {
  test(`${theme} ${width} request processing follows and reviews a plugin scope`, async ({
    page,
  }) => {
    await openPage(
      page,
      theme,
      width,
      [...instances, scopedInstance],
      [...plugins, scopedPlugin]
    )
    const row = page.locator('[data-request-plugin-row]').nth(2)
    await expect(row.getByText('按插件建议', { exact: true })).toBeVisible()
    await expect(row.locator('[data-plugin-scope]')).toContainText('OpenAI')
    const review = row.locator('[data-scope-review]')
    await expect(review).toContainText('插件调整了作用范围')
    await assertFits(page)
    await captureEvidence(page, {
      path: test.info().outputPath('scope-list.png'),
    })

    await review.click()
    const editor = page.locator('[data-instance-editor]')
    await expect(editor.locator('input[value="plugin"]')).toBeChecked()
    // The model choice gives way to the plugin's scope.
    await expect(editor.locator('input[value="chosen"]')).toHaveCount(0)
    const confirm = editor.locator('[data-plugin-scope-confirm]')
    await expect(confirm).toContainText('保存即确认新的范围')
    await expect(confirm).toContainText('gpt-4.1-mini')
    await confirm.scrollIntoViewIfNeeded()
    await insideViewport(page, confirm)
    await assertFits(page)
    await captureEvidence(page, {
      path: test.info().outputPath('scope-editor.png'),
    })
  })
}

for (const role of [10, 1])
  test(`role ${role} gets neither the request processing page nor its instances`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await configureStablePage(page, {
      theme: 'light',
      locale: 'zh-CN',
      user: { role },
    })
    const reads: string[] = []
    page.on('request', (request) => {
      if (new URL(request.url()).pathname.startsWith('/api/request_plugin'))
        reads.push(request.url())
    })
    await page.goto('/request-processing')
    await waitForStablePage(page)
    await expect(page).not.toHaveURL(/\/request-processing$/)
    await expect(page.locator('[data-request-plugin-row]')).toHaveCount(0)
    expect(reads).toEqual([])
  })
