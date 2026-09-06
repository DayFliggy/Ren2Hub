import { expect, test, type Page } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

const model = {
  id: 1,
  model_name: 'gpt-local',
  description: 'Ren2Hub metadata and routing',
  icon: 'OpenAI',
  tags: 'text,vision',
  vendor_id: 1,
  endpoints: '{"openai":{"path":"/v1/chat/completions","method":"POST"}}',
  status: 1,
  sync_official: 1,
  name_rule: 1,
  bound_channels: [{ name: 'primary', type: 1 }],
  enable_groups: ['default', 'vip'],
  quota_types: [0],
  matched_models: ['gpt-local-1', 'gpt-local-2'],
  matched_count: 2,
  created_time: 1700000000,
  updated_time: 1700000300,
}
const vendors = [
  {
    id: 1,
    name: 'OpenAI',
    description: 'Model provider',
    icon: 'OpenAI',
    status: 1,
  },
]
const groups = [
  {
    id: 1,
    name: 'Standard models',
    description: 'Reusable model list',
    type: 'model',
    items: ['gpt-local', 'gpt-local-2'],
  },
  {
    id: 2,
    name: 'Chat endpoints',
    description: '',
    type: 'endpoint',
    items: model.endpoints,
  },
]
const priceKeys = [
  'ModelPrice',
  'ModelRatio',
  'CompletionRatio',
  'CacheRatio',
  'ImageRatio',
  'AudioRatio',
  'AudioCompletionRatio',
]

async function data(page: Page) {
  await page.route('**/api/models/search?*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          page: 1,
          page_size: 20,
          total: 1,
          items: [model],
          vendor_counts: { 1: 1 },
        },
      },
    })
  )
  await page.route('**/api/models/1', (route) =>
    route.fulfill({ json: { success: true, data: model } })
  )
  await page.route('**/api/vendors/search?*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { page: 1, page_size: 100, total: 1, items: vendors },
      },
    })
  )
  await page.route('**/api/prefill_group/?*', (route) =>
    route.fulfill({ json: { success: true, data: groups } })
  )
  await page.route('**/api/prefill_group/', (route) =>
    route.fulfill({ json: { success: true, data: groups } })
  )
  await page.route('**/api/option/', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: priceKeys.map((key) => ({
          key,
          value: JSON.stringify(key === 'ModelRatio' ? { 'gpt-local': 2 } : {}),
        })),
      },
    })
  )
  await page.route('**/api/models/missing', (route) =>
    route.fulfill({ json: { success: true, data: ['missing-local'] } })
  )
  await page.route('**/api/models/sync_upstream/preview?*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          missing: ['missing-local'],
          conflicts: [
            {
              model_name: 'gpt-local',
              fields: [
                {
                  field: 'description',
                  local: 'Local description',
                  upstream: 'Upstream description',
                },
              ],
            },
          ],
        },
      },
    })
  )
}

for (const language of ['zh-CN', 'en']) {
  for (const theme of ['light', 'dark'] as const) {
    for (const mobile of [false, true]) {
      test(`model management ${language} ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
        page,
      }, testInfo) => {
        await page.setViewportSize(
          mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
        )
        await configureStablePage(page, { theme })
        await page.addInitScript(
          (locale) => localStorage.setItem('ren2hub_locale', locale),
          language
        )
        await data(page)
        const failures: string[] = []
        page.on('pageerror', (error) => failures.push(error.message))
        page.on('response', (response) => {
          if (response.status() === 404 && response.url().includes('127.0.0.1'))
            failures.push(response.url())
        })
        const zh = language === 'zh-CN'
        await page.goto('/models/metadata')
        await expect(
          page
            .getByText('gpt-local', { exact: true })
            .filter({ visible: true })
            .first()
        ).toBeVisible()
        await assertNoHorizontalOverflow(page)
        await page.screenshot({
          path: testInfo.outputPath('models.png'),
          fullPage: true,
        })
        const edit = page
          .getByRole('button', {
            name: zh ? '编辑模型' : 'Edit model',
            exact: true,
          })
          .filter({ visible: true })
          .first()
        await edit.click()
        const drawer = page.getByRole('dialog')
        await expect(
          drawer.getByLabel(zh ? '模型名称' : 'Model name', { exact: true })
        ).toHaveValue('gpt-local')
        await expect(
          drawer.getByText(zh ? '计费' : 'Billing', { exact: true })
        ).toBeVisible()
        await expect(drawer.locator('img').first()).toBeVisible()
        await drawer.getByRole('button', { name: 'JSON', exact: true }).click()
        await drawer.getByLabel('JSON', { exact: true }).fill('{')
        await drawer
          .getByRole('button', { name: zh ? '保存' : 'Save', exact: true })
          .click()
        await expect(drawer.getByRole('alert').first()).toBeVisible()
        await assertNoHorizontalOverflow(page)
        await page.screenshot({
          path: testInfo.outputPath('model-drawer.png'),
          fullPage: true,
        })
        await page.keyboard.press('Escape')
        await expect(drawer).toHaveCount(0)
        await expect(edit).toBeFocused()
        for (const kind of ['vendors', 'prefill-groups']) {
          await page.goto(`/models/${kind}?keyword=gpt&source=bookmark#details`)
          await expect(page).toHaveURL(
            new RegExp(`/models/metadata\\?.*manage=${kind}.*#details$`)
          )
          const manager = page.getByRole('dialog')
          await expect(manager).toBeVisible()
          await expect(
            manager
              .getByText(kind === 'vendors' ? 'OpenAI' : 'Standard models', {
                exact: true,
              })
              .first()
          ).toBeVisible()
          await manager
            .getByRole('button', { name: zh ? '编辑' : 'Edit', exact: true })
            .first()
            .click()
          await expect(page.getByRole('dialog')).toHaveCount(2)
          await assertNoHorizontalOverflow(page)
          await page.screenshot({
            path: testInfo.outputPath(`${kind}.png`),
            fullPage: true,
          })
          await page.keyboard.press('Escape')
          await expect(page.getByRole('dialog')).toHaveCount(1)
          await page.keyboard.press('Escape')
          await expect(page.getByRole('dialog')).toHaveCount(0)
          await expect(page).not.toHaveURL(/manage=/)
        }
        expect(
          await page
            .locator('img')
            .evaluateAll((images) =>
              images
                .filter((image) => !image.complete || image.naturalWidth === 0)
                .map((image) => image.src)
            )
        ).toEqual([])
        expect(failures).toEqual([])
      })
    }
  }
}

test('sync cancellation and missing-model creation preserve the draft boundary', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'dark' })
  await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
  await data(page)
  const writes: string[] = []
  page.on('request', (request) => {
    if (request.url().includes('/api/models') && request.method() !== 'GET')
      writes.push(request.url())
  })
  await page.goto('/models/metadata')
  await page.getByRole('button', { name: 'Sync metadata', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.locator('option[value=config]')).toHaveAttribute(
    'disabled',
    ''
  )
  await dialog.getByRole('button', { name: 'Get sync preview' }).click()
  await expect(
    dialog.getByText('Upstream description', { exact: true })
  ).toBeVisible()
  await dialog.getByRole('checkbox').last().check()
  await page.keyboard.press('Escape')
  expect(writes).toEqual([])
  await page
    .getByRole('button', { name: 'Missing models', exact: true })
    .click()
  await dialog
    .getByRole('button', { name: 'Create metadata', exact: true })
    .click()
  await expect(
    page.getByRole('dialog').getByLabel('Model name', { exact: true })
  ).toHaveValue('missing-local')
  await page.keyboard.press('Escape')
  expect(writes).toEqual([])
})
