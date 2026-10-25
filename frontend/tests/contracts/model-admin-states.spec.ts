import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { adminManagementMessages } from '../../src/i18n/adminManagement'
import { modelMetadataMessages } from '../../src/i18n/modelMetadata'
import { modelMetadataSyncMessages } from '../../src/i18n/modelMetadataSync'
import { modelPricingMessages } from '../../src/i18n/modelPricing'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import { mockPriceFetch, nativePriceSource } from './native-core-fixture'
import { captureUI } from './ui-coverage'

const scenarios = [
  {
    key: 'desktop-light-zh-CN',
    locale: 'zh-CN',
    theme: 'light',
    width: 1440,
    height: 900,
  },
  {
    key: 'mobile-dark-en',
    locale: 'en',
    theme: 'dark',
    width: 390,
    height: 844,
  },
] as const

async function checkpoint(page: Page, info: TestInfo, label: string) {
  await assertNoHorizontalOverflow(page)
  await captureUI(page, info, label)
}

async function metadataFixture(page: Page, locale: string) {
  let releaseMissing!: () => void
  const missingReady = new Promise<void>((resolve) => {
    releaseMissing = resolve
  })
  // Match metadataApi's native search and versioned sync-preview contracts.
  // These responses contain metadata only, never prices or access grants.
  await page.route(/\/api\/models\/search(?:\?.*)?$/, (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          page: 1,
          page_size: 20,
          total: 1,
          vendor_counts: {},
          items: [
            {
              id: 5,
              model_name: 'gpt-native',
              description: 'Existing metadata',
              icon: '',
              tags: 'reasoning,tools',
              endpoints: '',
              vendor_id: null,
              status: 1,
              sync_official: 1,
              name_rule: 0,
              matched_models: ['gpt-native'],
              matched_count: 1,
              bound_channels: [],
              quota_types: [0],
              created_time: 1785124800,
              updated_time: 1785124800,
            },
          ],
        },
      },
    })
  )
  await page.route(/\/api\/models\/sync_upstream\/preview(?:\?.*)?$/, (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          source: { version: 'ui-source-v1', locale },
          candidates: [
            {
              model_name: 'gpt-native',
              kind: 'update',
              scope: 'site',
              record_version: 'record-v1',
              vendor_to_create: '',
              fields: [
                {
                  field: 'description',
                  local: 'Existing metadata',
                  upstream: 'Reviewed metadata',
                },
              ],
            },
          ],
        },
      },
    })
  )
  await page.route('**/api/models/missing', async (route) => {
    await missingReady
    await route.fulfill({
      json: { success: true, data: ['ui-missing', 'another-missing'] },
    })
  })
  return releaseMissing
}

for (const scenario of scenarios) {
  const admin = adminManagementMessages[scenario.locale]
  const metadata = modelMetadataMessages[scenario.locale]
  const sync = modelMetadataSyncMessages[scenario.locale]
  const pricing = modelPricingMessages[scenario.locale]

  test.describe(scenario.key, () => {
    test.beforeEach(async ({ page }) => {
      await page.setViewportSize(scenario)
      await configureStablePage(page, scenario)
    })

    test('model admin metadata sync missing models and prefill editor', async ({
      page,
    }, info) => {
      const releaseMissing = await metadataFixture(page, scenario.locale)
      await page.goto('/models/metadata')
      await waitForStablePage(page)
      await expect(
        page.getByRole('button', { name: 'gpt-native', exact: true }).filter({
          visible: true,
        })
      ).toBeVisible()
      await checkpoint(page, info, 'metadata-row')

      const syncTrigger = page.getByRole('button', {
        name: metadata.syncMetadata,
        exact: true,
      })
      await syncTrigger.click()
      let dialog = page.getByRole('dialog')
      await dialog
        .getByRole('button', { name: sync.loadPreview, exact: true })
        .click()
      await dialog
        .getByRole('checkbox', {
          name: sync.selectModel.replace('{name}', 'gpt-native'),
          exact: true,
        })
        .check()
      await dialog
        .getByRole('button', { name: sync.previewSelected, exact: true })
        .click()
      await expect(dialog).toContainText('Existing metadata')
      await expect(dialog).toContainText('Reviewed metadata')
      await checkpoint(page, info, 'metadata-sync-field-preview')
      await page.keyboard.press('Escape')
      await expect(dialog).toHaveCount(0)
      await expect(syncTrigger).toBeFocused()

      const menu = page
        .getByRole('button', { name: metadata.openMenu, exact: true })
        .first()
      await menu.click()
      await page
        .getByRole('menuitem', { name: metadata.missingModels, exact: true })
        .click()
      dialog = page.getByRole('dialog')
      await expect(dialog.getByRole('status')).toContainText(sync.loading)
      await expect(dialog.locator('.animate-pulse')).toHaveCount(5)
      await expect(dialog.locator('.animate-pulse').first()).toBeVisible()
      await checkpoint(page, info, 'metadata-missing-loading')
      releaseMissing()
      await dialog
        .getByRole('searchbox', { name: sync.searchMissing, exact: true })
        .fill('ui-missing')
      await expect(
        dialog.getByText('ui-missing', { exact: true })
      ).toBeVisible()
      await expect(
        dialog.getByText('another-missing', { exact: true })
      ).toHaveCount(0)
      await checkpoint(page, info, 'metadata-missing-filter')
      await page.keyboard.press('Escape')
      await expect(dialog).toHaveCount(0)

      await menu.click()
      await page
        .getByRole('menuitem', { name: metadata.prefillGroups, exact: true })
        .click()
      await expect(page).toHaveURL(/manage=prefill-groups/)
      dialog = page.getByRole('dialog')
      await dialog
        .getByRole('button', { name: admin.newGroup, exact: true })
        .click()
      await dialog.locator('[name="prefill-group-name"]').fill('UI model set')
      await dialog
        .getByRole('button', { name: admin.addItem, exact: true })
        .click()
      const member = dialog.getByRole('textbox', {
        name: `${admin.items} 1`,
        exact: true,
      })
      await member.fill('gpt-native')
      await expect(member).toHaveValue('gpt-native')
      await expect(dialog.locator('.admin-list-row')).toHaveCount(1)
      await checkpoint(page, info, 'metadata-prefill-string-editor')
    })

    test('model admin price source comparison uses native fetched rows', async ({
      page,
    }, info) => {
      await mockPriceFetch(page, [
        nativePriceSource({ id: 'sub2api' }),
        nativePriceSource({ id: 'axonhub' }),
      ])
      await page.goto('/models/pricing')
      await waitForStablePage(page)
      await page
        .getByRole('button', { name: pricing.fetch, exact: true })
        .click()
      const dialog = page.getByRole('dialog')
      const source = dialog.locator(
        'input[name="price-source"][value="sub2api"]'
      )
      await source.check()
      await expect(source).toBeChecked()
      await expect(dialog).toContainText('contract-chat')
      await expect(
        dialog.locator('[data-fingerprint="fingerprintMatched"]')
      ).toBeVisible()
      await expect(dialog.locator('.price-diff')).toHaveCount(2)
      await expect(dialog).toContainText('tier("base", p * 1 + c * 2)')
      await dialog.getByRole('checkbox').check()
      await expect(
        dialog.getByRole('button', { name: pricing.fill, exact: true })
      ).toBeEnabled()
      await checkpoint(page, info, 'pricing-native-source-diff')
      // Candidate prices are in a horizontally scrollable column on phones.
      // Bring that column into view, rather than counting off-screen text.
      const candidate = dialog.locator('.price-diff').last()
      await candidate.scrollIntoViewIfNeeded()
      await expect(candidate).toBeInViewport()
      await expect(candidate).toContainText('tier("base", p * 1 + c * 2)')
      await checkpoint(page, info, 'pricing-native-source-candidate')
      await page.keyboard.press('Escape')
      await expect(dialog).toHaveCount(0)
    })

    test('model admin tool prices round trip visual edits and JSON', async ({
      page,
    }, info) => {
      await page.goto('/models/pricing')
      await waitForStablePage(page)
      await page
        .getByRole('radio', { name: pricing.tools, exact: true })
        .click()
      const tools = page.locator('.tool-pricing')
      await expect(tools).toBeVisible()
      await tools
        .getByRole('button', { name: pricing.addPrice, exact: true })
        .click()
      const row = tools.locator('tbody tr').last()
      await row
        .getByRole('textbox', { name: pricing.toolName, exact: true })
        .fill('ui_web_search')
      await row
        .getByRole('textbox', { name: pricing.price, exact: true })
        .fill('0.01')
      await checkpoint(page, info, 'pricing-tool-visual-edit')
      await tools
        .getByRole('button', { name: pricing.json, exact: true })
        .click()
      const json = tools.getByRole('textbox')
      expect(JSON.parse(await json.inputValue())).toMatchObject({
        ui_web_search: 0.01,
      })
      await json.fill('{"ui_web_search":0.02}')
      await checkpoint(page, info, 'pricing-tool-json-edit')
      await tools
        .getByRole('button', { name: pricing.visual, exact: true })
        .click()
      await expect(
        tools.getByRole('textbox', { name: pricing.price, exact: true })
      ).toHaveValue('0.02')
      await expect(tools.getByRole('alert')).toHaveCount(0)
    })

    test('model admin visual expression preserves conditional tiers', async ({
      page,
    }, info) => {
      await page.goto('/models/pricing')
      await waitForStablePage(page)
      await page
        .getByRole('button', { name: pricing.add, exact: true })
        .filter({ visible: true })
        .first()
        .click()
      const editor = page.locator('.pricing-editor').filter({ visible: true })
      await editor
        .getByRole('textbox', { name: pricing.name, exact: true })
        .fill('ui-conditional')
      await editor
        .getByRole('tab', { name: pricing.expression, exact: true })
        .click()
      const expression = editor.locator('.price-expression')
      await expression
        .getByRole('button', { name: pricing.preset, exact: true })
        .click()
      await expression.locator('.price-tier-line input').nth(0).fill('1')
      await expression.locator('.price-tier-line input').nth(1).fill('2')
      await expression
        .getByRole('button', { name: pricing.branch, exact: true })
        .click()
      const condition = expression.locator('.price-condition')
      await condition
        .getByRole('combobox', { name: pricing.variable, exact: true })
        .selectOption('p')
      await condition
        .getByRole('combobox', { name: pricing.operator, exact: true })
        .selectOption('<')
      await condition
        .getByRole('textbox', { name: pricing.value, exact: true })
        .fill('128000')
      await expression
        .getByRole('textbox', { name: pricing.tierLabel, exact: true })
        .nth(1)
        .fill('large')
      await expression.locator('.price-tier-line input').nth(2).fill('3')
      await expression.locator('.price-tier-line input').nth(3).fill('4')
      await expect(expression.getByRole('alert')).toHaveCount(0)
      await checkpoint(page, info, 'pricing-conditional-visual')
      await expression
        .getByRole('button', { name: pricing.exprSource, exact: true })
        .click()
      const source = expression.getByRole('textbox', {
        name: pricing.exprSource,
        exact: true,
      })
      await expect(source).toHaveValue(/128000.*tier\("base".*tier\("large"/)
      await checkpoint(page, info, 'pricing-conditional-source')
      await expression
        .getByRole('button', { name: pricing.exprVisual, exact: true })
        .click()
      await expect(
        expression.getByRole('textbox', { name: pricing.value, exact: true })
      ).toHaveValue('128000')
      await expect(expression.locator('.price-tier-line input')).toHaveCount(4)
      await expect(expression.getByRole('alert')).toHaveCount(0)
    })
  })
}
