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
    created_time: 1700000000,
    updated_time: 1700000300,
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
const page1 = (items: unknown[]) => ({
  success: true,
  data: { page: 1, page_size: 20, total: items.length, items },
})
async function data(page: Page) {
  await page.route('**/api/models/search?*', (route) =>
    route.fulfill({
      json: {
        ...page1([model]),
        data: { ...page1([model]).data, vendor_counts: { 1: 1 } },
      },
    })
  )
  await page.route('**/api/models/1', (route) =>
    route.fulfill({ json: { success: true, data: model } })
  )
  await page.route('**/api/vendors/search?*', (route) =>
    route.fulfill({ json: page1(vendors) })
  )
  await page.route(/\/api\/prefill_group\/(\?.*)?$/, (route) =>
    route.fulfill({ json: { success: true, data: groups } })
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
async function setupPage(
  page: Page,
  options: { language: string; theme: 'light' | 'dark'; mobile: boolean }
) {
  await page.setViewportSize(
    options.mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
  )
  await configureStablePage(page, { theme: options.theme })
  await page.addInitScript(
    (locale) => localStorage.setItem('ren2hub_locale', locale),
    options.language
  )
  await data(page)
  const failures: string[] = []
  page.on('pageerror', (error) => failures.push(error.message))
  page.on('response', (response) => {
    if (response.status() === 404 && response.url().includes('127.0.0.1'))
      failures.push(response.url())
  })
  return failures
}
const visible = (page: Page, text: string) =>
  page.getByText(text, { exact: true }).filter({ visible: true }).first()

for (const language of ['zh-CN', 'en']) {
  for (const theme of ['light', 'dark'] as const) {
    for (const mobile of [false, true]) {
      test(`model management ${language} ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
        page,
      }, testInfo) => {
        const failures = await setupPage(page, { language, theme, mobile })
        const zh = language === 'zh-CN'
        const shot = (name: string) =>
          page.screenshot({ path: testInfo.outputPath(name), fullPage: true })
        await page.goto('/models/metadata')
        await expect(visible(page, 'gpt-local')).toBeVisible()
        // NewAPI naming: sidebar "Models", page "Model management".
        await expect(
          page
            .locator('[data-console-nav-item="models-admin"]')
            .filter({ visible: true })
            .first()
        ).toContainText(zh ? '模型' : 'Models')
        await expect(
          visible(page, zh ? '模型管理' : 'Model management')
        ).toBeVisible()
        await expect(
          page.getByRole('tab', {
            name: zh ? '供应商' : 'Vendors',
            exact: true,
          })
        ).toBeVisible()
        await assertNoHorizontalOverflow(page)
        await shot('models.png')
        await page
          .getByRole('button', { name: zh ? '编辑' : 'Edit', exact: true })
          .filter({ visible: true })
          .first()
          .click()
        const drawer = page.getByRole('dialog')
        await expect(
          drawer.getByLabel(zh ? '模型名称' : 'Model Name', { exact: true })
        ).toHaveValue('gpt-local')
        await expect(
          drawer.getByText(zh ? '基本信息' : 'Basic Information', {
            exact: true,
          })
        ).toBeVisible()
        await drawer.getByRole('radio', { name: 'JSON', exact: true }).click()
        await drawer.getByLabel('JSON', { exact: true }).fill('{')
        await drawer
          .getByRole('button', {
            name: zh ? '保存资料' : 'Save metadata',
            exact: true,
          })
          .click()
        await expect(drawer.getByRole('alert').first()).toBeVisible()
        await assertNoHorizontalOverflow(page)
        await shot('model-drawer.png')
        await drawer
          .getByRole('tab', {
            name: zh ? '渠道与分组' : 'Channels and groups',
            exact: true,
          })
          .click()
        await expect(drawer.getByText('primary', { exact: true })).toBeVisible()
        await expect(
          drawer.getByText('gpt-local-2', { exact: true })
        ).toBeVisible()
        await shot('model-connections.png')
        // The broken endpoint draft is unsaved, so closing asks first.
        await page.keyboard.press('Escape')
        await page
          .getByRole('button', {
            name: zh ? '放弃修改' : 'Discard changes',
            exact: true,
          })
          .click()
        await expect(page.getByRole('dialog')).toHaveCount(0)
        // Vendors is its own section in NewAPI; bookmarks keep query and hash.
        await page.goto('/models/vendors?vFilter=open&source=bookmark#details')
        await expect(page).toHaveURL(
          /\/models\/vendors\?vFilter=open&source=bookmark#details$/
        )
        await expect(
          page.getByRole('tab', {
            name: zh ? '供应商' : 'Vendors',
            exact: true,
          })
        ).toHaveAttribute('aria-selected', 'true')
        await expect(visible(page, 'Model provider')).toBeVisible()
        await assertNoHorizontalOverflow(page)
        await shot('vendors.png')
        await page
          .getByRole('button', { name: 'OpenAI', exact: true })
          .filter({ visible: true })
          .first()
          .click()
        const vendorDrawer = page.getByRole('dialog')
        await expect(
          vendorDrawer.getByRole('tab', {
            name: zh ? '供应商资料' : 'Vendor details',
          })
        ).toBeVisible()
        await assertNoHorizontalOverflow(page)
        await shot('vendor-drawer.png')
        await page.keyboard.press('Escape')
        await expect(page.getByRole('dialog')).toHaveCount(0)
        await page.goto('/models/prefill-groups?source=bookmark#details')
        await expect(page).toHaveURL(
          /\/models\/metadata\?.*manage=prefill-groups.*#details$/
        )
        const groupsDialog = page.getByRole('dialog')
        await expect(
          groupsDialog
            .getByText('Standard models', { exact: true })
            .filter({ visible: true })
            .first()
        ).toBeVisible()
        await assertNoHorizontalOverflow(page)
        await shot('prefill-groups.png')
        await page.keyboard.press('Escape')
        await expect(page.getByRole('dialog')).toHaveCount(0)
        await expect(page).not.toHaveURL(/manage=/)
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
test('sync preview and missing-model creation write nothing until applied', async ({
  page,
}, testInfo) => {
  await setupPage(page, { language: 'en', theme: 'dark', mobile: false })
  const writes: string[] = []
  page.on('request', (request) => {
    if (request.url().includes('/api/models') && request.method() !== 'GET')
      writes.push(request.url())
  })
  await page.goto('/models/metadata')
  await page.getByRole('button', { name: 'Sync metadata', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog
    .getByRole('button', { name: 'Load metadata preview', exact: true })
    .click()
  await expect(dialog.getByText('missing-local', { exact: true })).toBeVisible()
  await dialog.getByRole('checkbox', { name: 'Select gpt-local' }).check()
  await dialog
    .getByRole('button', { name: 'Preview selected changes', exact: true })
    .click()
  await expect(
    dialog.getByText('Upstream description', { exact: true })
  ).toBeVisible()
  await assertNoHorizontalOverflow(page)
  await page.screenshot({
    path: testInfo.outputPath('sync-preview.png'),
    fullPage: true,
  })
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(writes).toEqual([])
  // The page-header menu comes before the per-row menus.
  await page
    .getByRole('button', { name: 'Open menu', exact: true })
    .first()
    .click()
  await page.getByRole('menuitem', { name: 'Missing Models' }).click()
  await page
    .getByRole('dialog')
    .getByRole('button', { name: 'Configure', exact: true })
    .click()
  await expect(
    page.getByRole('dialog').getByLabel('Model Name', { exact: true })
  ).toHaveValue('missing-local')
  await page.keyboard.press('Escape')
  expect(writes).toEqual([])
})
