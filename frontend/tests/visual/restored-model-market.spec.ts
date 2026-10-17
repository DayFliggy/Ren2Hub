import { expect, test, type Page } from '@playwright/test'
import { nativeCoreFixture } from './native-core-fixture'
import { assertNoHorizontalOverflow } from './fixtures'

async function assertMarketControlsFit(page: Page, selectedGroup: string) {
  const selector = page.getByRole('combobox', {
    name: 'Available group',
    exact: true,
  })
  const value = selector.locator('span.truncate')
  await expect(value).toHaveText(selectedGroup)
  expect(
    await value.evaluate((element) => ({
      visible: element.clientWidth > 0,
      fits: element.scrollWidth <= element.clientWidth + 1,
    }))
  ).toEqual({ visible: true, fits: true })

  const switcher = page.getByRole('radiogroup', { name: 'Model market view' })
  const bounds = await switcher.boundingBox()
  expect(bounds).not.toBeNull()
  const card = (await page.locator('.market-filter-card').boundingBox())!
  expect(bounds!.x).toBeGreaterThanOrEqual(card.x)
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(card.x + card.width)
  for (const button of await switcher.getByRole('radio').all()) {
    const box = (await button.boundingBox())!
    expect(box.x).toBeGreaterThanOrEqual(bounds!.x - 1)
    expect(box.x + box.width).toBeLessThanOrEqual(bounds!.x + bounds!.width + 1)
    expect(box.y + box.height).toBeLessThanOrEqual(
      bounds!.y + bounds!.height + 1
    )
    // A button can fit while its label still paints outside its border.
    expect(
      await button.locator('span').evaluate((element) => {
        const label = element.getBoundingClientRect()
        const parent = element.parentElement!.getBoundingClientRect()
        return (
          label.left >= parent.left &&
          label.right <= parent.right + 1 &&
          element.scrollWidth <= element.clientWidth + 1
        )
      })
    ).toBe(true)
  }
}

for (const theme of ['light', 'dark']) {
  for (const width of [1440, 390]) {
    test(`restored model market native scopes ${theme} ${width}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 })
      await page.emulateMedia({ reducedMotion: 'reduce' })
      const state = await nativeCoreFixture(page, theme, 'USD', {
        user: {
          role: 1,
          username: 'native.member',
          display_name: 'Native Member',
        },
      })
      const forbidden: string[] = []
      page.on('request', (request) => {
        const path = new URL(request.url()).pathname
        if (path.startsWith('/api/channel') || path.includes('model-market'))
          forbidden.push(path)
      })
      let revoked = false
      await page.route('**/api/user/self/groups', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              standard: { ratio: 0.2, desc: 'Standard' },
              premium: { ratio: 2, desc: 'Premium' },
              auto: { ratio: 'auto', desc: 'Automatic' },
            },
          },
        })
      )
      await page.route(/\/api\/user\/models(?:\?.*)?$/, (route) => {
        if (revoked)
          return route.fulfill({
            status: 403,
            json: { success: false, message: 'permission revoked' },
          })
        const group = new URL(route.request().url()).searchParams.get('group')
        return route.fulfill({
          json: {
            success: true,
            data:
              group === 'premium' ? ['chat-two'] : ['chat-one', 'video-model'],
          },
        })
      })
      await page.route('**/api/pricing', (route) =>
        route.fulfill({
          json: {
            success: true,
            supported_endpoint: {
              openai: { path: '/v1/chat/completions', method: 'POST' },
              'openai-video': { path: '/v1/videos', method: 'POST' },
            },
            vendors: [{ id: 1, name: 'Provider One', description: '' }],
            data: [
              {
                model_name: 'chat-one',
                vendor_id: 1,
                description: 'A model for testing native group prices',
                tags: 'tools',
                supported_endpoint_types: ['openai'],
                quota_type: 0,
                model_ratio: 0.5,
                completion_ratio: 3,
              },
              {
                model_name: 'chat-two',
                vendor_id: 1,
                supported_endpoint_types: ['openai'],
                quota_type: 0,
                model_ratio: 2,
                completion_ratio: 1,
              },
              {
                model_name: 'video-model',
                supported_endpoint_types: ['openai-video'],
                quota_type: 0,
                billing_mode: 'tiered_expr',
                billing_expr: 'u("seconds") * 0.2',
                billing_usage_schema: {
                  seconds: { type: 'number', unit: 'second' },
                },
              },
              { model_name: 'private-only', quota_type: 0, model_ratio: 99 },
            ],
          },
        })
      )
      await page.goto('/models')
      await expect(page.locator('[data-model-card]')).toHaveCount(3)
      await page.evaluate(() => document.fonts.ready)
      await expect(page.getByText('private-only', { exact: true })).toHaveCount(
        0
      )
      await assertNoHorizontalOverflow(page)
      await assertMarketControlsFit(page, 'All groups')
      await page.screenshot({
        path: testInfo.outputPath('models.png'),
        fullPage: true,
      })
      await page.getByRole('searchbox').fill('chat')
      await expect(page.locator('[data-model-card]')).toHaveCount(2)
      await page
        .getByRole('combobox', { name: 'Available group', exact: true })
        .click()
      await page
        .getByRole('option', { name: 'standard · Standard', exact: true })
        .click()
      await expect(page.locator('[data-model-card]')).toHaveCount(1)
      await expect(page.locator('[data-model-card]')).toContainText('$0.200')
      await expect(page.getByRole('listbox')).toHaveCount(0)
      await assertMarketControlsFit(page, 'standard · Standard')
      await page.locator('.market-filter-card').scrollIntoViewIfNeeded()
      await page.screenshot({
        path: testInfo.outputPath('filters.png'),
        fullPage: true,
      })
      await page
        .getByRole('button', { name: 'Clear filters', exact: true })
        .click()
      await page.locator('[data-model-card]').first().scrollIntoViewIfNeeded()
      await page.screenshot({
        path: testInfo.outputPath('models-content.png'),
        fullPage: true,
      })
      await page
        .getByRole('radio', { name: 'Price comparison', exact: true })
        .click()
      await assertMarketControlsFit(page, 'All groups')
      const row = page.locator('tbody tr').filter({ hasText: 'chat-one' })
      await expect(row.locator('[data-group-price="standard"]')).toContainText(
        '$0.200'
      )
      await expect(row.locator('[data-group-price="premium"]')).toContainText(
        'Unavailable in this group'
      )
      await expect(row.locator('[data-group-price="auto"]')).toContainText(
        'no single quote'
      )
      await assertNoHorizontalOverflow(page)
      await page.locator('[data-price-matrix]').scrollIntoViewIfNeeded()
      await page.screenshot({
        path: testInfo.outputPath('comparison.png'),
        fullPage: true,
      })
      await row
        .getByRole('button', {
          name: 'View details and group prices for chat-one',
        })
        .click()
      const dialog = page.getByRole('dialog', {
        name: 'Model details and group prices',
      })
      await expect(dialog).toBeVisible()
      await dialog
        .getByRole('combobox', { name: 'Available groups' })
        .selectOption('standard')
      await expect(dialog).toContainText('$0.200')
      await expect(dialog).toContainText('POST /v1/chat/completions')
      await page.screenshot({
        path: testInfo.outputPath('details.png'),
        fullPage: true,
      })
      await page.keyboard.press('Escape')
      await expect(dialog).toHaveCount(0)
      await page
        .getByRole('radio', { name: 'Channel shelves', exact: true })
        .click()
      await assertMarketControlsFit(page, 'All groups')
      await expect(
        page.locator('[data-channel-shelves-unavailable]')
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page
        .locator('[data-channel-shelves-unavailable]')
        .scrollIntoViewIfNeeded()
      await page.screenshot({
        path: testInfo.outputPath('channels.png'),
        fullPage: true,
      })
      revoked = true
      await page.getByRole('button', { name: 'Refresh', exact: true }).click()
      await expect(page.getByRole('alert')).toContainText('permission revoked')
      await expect(page.locator('[data-model-card]')).toHaveCount(0)
      expect(forbidden).toEqual([])
      expect(state.errors).toEqual([])
      expect(state.missingMessages).toEqual([])
    })
  }
}
