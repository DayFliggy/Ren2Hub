import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

const model = {
  id: 71,
  model_name: 'p2-local',
  description: '',
  icon: '',
  tags: '',
  vendor_id: 0,
  endpoints: '',
  status: 1,
  sync_official: 0,
  name_rule: 0,
  bound_channels: [],
  enable_groups: [],
  matched_models: [],
  quota_types: [],
}

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`native catalog metadata save and retired controls ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme, authenticated: true })
      const failures: string[] = []
      page.on('pageerror', (error) => failures.push(error.message))
      await page.goto('/keys')
      await expect(
        page.getByRole('button', { name: '客户端配置', exact: true })
      ).toHaveCount(0)
      await expect(page.getByRole('dialog')).toHaveCount(0)

      await page.route('**/api/models/search?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              page: 1,
              page_size: 20,
              total: 1,
              items: [model],
              vendor_counts: {},
            },
          },
        })
      )
      await page.route('**/api/vendors/search?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: { page: 1, page_size: 20, total: 0, items: [] },
          },
        })
      )
      await page.route('**/api/models/71', (route) =>
        route.fulfill({ json: { success: true, data: model } })
      )
      const saves: Record<string, unknown>[] = []
      let failSave = true
      await page.route('**/api/models/', (route) => {
        expect(route.request().method()).toBe('PUT')
        const body = route.request().postDataJSON()
        saves.push(body)
        return route.fulfill({
          json: failSave
            ? { success: false, message: 'Metadata save unavailable' }
            : { success: true, data: { ...model, ...body } },
        })
      })
      await page.goto('/models/metadata')
      await page
        .getByRole('button', { name: '编辑', exact: true })
        .filter({ visible: true })
        .first()
        .click()
      const dialog = page.getByRole('dialog')
      await expect(dialog.locator('[data-model-capabilities]')).toHaveCount(0)
      const description = dialog.getByLabel('说明信息', { exact: true })
      await description.fill('Native metadata <literal>')
      await dialog
        .getByRole('button', { name: '保存资料', exact: true })
        .click()
      await expect(dialog.getByRole('alert')).toContainText(
        'Metadata save unavailable'
      )
      await expect(description).toHaveValue('Native metadata <literal>')
      failSave = false
      await dialog
        .getByRole('button', { name: '保存资料', exact: true })
        .click()
      await expect(dialog).toHaveCount(0)
      expect(saves).toHaveLength(2)
      expect(saves[1]).toEqual({
        id: 71,
        model_name: 'p2-local',
        description: 'Native metadata <literal>',
        icon: '',
        tags: '',
        vendor_id: 0,
        endpoints: '',
        status: 1,
        sync_official: 0,
        name_rule: 0,
      })
      await assertNoHorizontalOverflow(page)
      expect(failures).toEqual([])
    })
  }
}
