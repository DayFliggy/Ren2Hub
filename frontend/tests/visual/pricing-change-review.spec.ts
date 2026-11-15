import { mkdirSync } from 'node:fs'

import { expect, test } from '@playwright/test'

import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'
import { PRICING_KEYS } from '../../src/api/pricingKeys'
import type {
  ModelPricingChange,
  ModelPricingEntry,
} from '../../src/api/pricingConfig'

/** Optional screenshot directory for the cross-audit. */
const shotsDir = process.env.REN2HUB_PRICING_SHOTS

const saved: ModelPricingEntry[] = [
  {
    model_name: 'gpt-review-modified',
    version: 'v-modified',
    configured: { ModelRatio: 1.5, CompletionRatio: 2 },
    effective: { ModelRatio: 1.5, CompletionRatio: 2 },
  },
  {
    model_name: 'gpt-review-removed-with-a-long-model-name-for-phones',
    version: 'v-removed',
    configured: { ModelPrice: 0.04 },
    effective: { ModelPrice: 0.04 },
  },
  {
    model_name: 'gpt-review-unchanged',
    version: 'v-unchanged',
    configured: { ModelPrice: 0.01 },
    effective: { ModelPrice: 0.01 },
  },
]
// The draft the administrator types in JSON mode: one price changed, one new
// model and one saved price left out, which deletes it.
const draftJSON = {
  ModelRatio: { 'gpt-review-modified': 2.5 },
  CompletionRatio: { 'gpt-review-modified': 2 },
  ModelPrice: { 'gpt-review-unchanged': 0.01, 'gpt-review-added': 0.2 },
}

for (const [theme, width] of [
  ['light', 1440],
  ['dark', 390],
] as const) {
  test(`price changes are reviewed before saving ${theme} ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 })
    await configureStablePage(page, { theme, locale: 'en' })
    await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
    const writes: ModelPricingChange[][] = []
    await page.route('**/api/channel/models_enabled', (route) =>
      route.fulfill({
        json: { success: true, data: saved.map((e) => e.model_name) },
      })
    )
    await page.route('**/api/option/model_pricing/issues', (route) =>
      route.fulfill({ json: { success: true, data: [] } })
    )
    await page.route(/\/api\/option\/model_pricing(?!\/issues)/, (route) => {
      if (route.request().method() === 'PATCH') {
        writes.push(route.request().postDataJSON().changes)
        return route.fulfill({ json: { success: true, data: null } })
      }
      return route.fulfill({
        json: {
          success: true,
          data: {
            entries: saved,
            options: Object.fromEntries(PRICING_KEYS.map((key) => [key, '{}'])),
            empty_version: 'empty',
          },
        },
      })
    })
    await page.goto('/models/pricing')
    await page.getByRole('button', { name: 'JSON', exact: true }).click()
    for (const [key, value] of Object.entries(draftJSON))
      await page.getByLabel(key, { exact: true }).fill(JSON.stringify(value))
    await page.getByRole('button', { name: 'Visual', exact: true }).click()

    const list = page.locator('.pricing-models')
    const row = (name: string) =>
      list.locator('tbody tr').filter({ hasText: name })
    const removed = row('gpt-review-removed')
    await expect(removed).toContainText('Pending deletion')
    await expect(removed).toContainText(
      'Unpriced after saving; requests will be rejected'
    )
    await expect(
      removed.locator('.pricing-model-name.pricing-struck')
    ).toHaveCSS('text-decoration-line', 'line-through')
    // Ratios are shown as prices: ratio 1.5 → 2.5 is $3 → $5 per 1M tokens.
    await expect(row('gpt-review-modified')).toContainText(
      'Input price $3.000 → $5.000 / 1M tokens'
    )
    // Every change can be undone; only saved prices not pending deletion
    // can be deleted. Each action names its model for screen readers.
    const actions = async (name: string) =>
      (
        await row(name)
          .locator('td')
          .last()
          .getByRole('button')
          .allTextContents()
      ).map((text) => text.trim())
    expect(await actions('gpt-review-added')).toEqual(['Undo'])
    expect(await actions('gpt-review-modified')).toEqual(['Undo', 'Delete'])
    expect(await actions('gpt-review-removed')).toEqual(['Undo'])
    expect(await actions('gpt-review-unchanged')).toEqual(['Delete'])
    await expect(
      row('gpt-review-modified').getByRole('button', {
        name: 'Undo the price change of gpt-review-modified',
        exact: true,
      })
    ).toBeVisible()
    // The mode chip stays readable; only the name and price are struck.
    await expect(removed.locator('.pricing-struck')).toHaveCount(2)
    // A changed line wraps rather than hiding the new value.
    const change = row('gpt-review-modified').locator('.pricing-change').first()
    await expect(change).toHaveCSS('white-space', 'normal')
    await expect(page.locator('[data-pricing-change-count]')).toHaveText(
      '3 models changed'
    )
    for (const name of ['gpt-review-removed', 'gpt-review-modified']) {
      const nameBox = (await row(name).locator('td').first().boundingBox())!
      const listBox = (await list.boundingBox())!
      expect(nameBox.x + nameBox.width).toBeLessThanOrEqual(
        listBox.x + listBox.width + 1
      )
    }
    await assertNoHorizontalOverflow(page)
    if (shotsDir) {
      mkdirSync(shotsDir, { recursive: true })
      await page.screenshot({
        path: `${shotsDir}/rows-${theme}-${width}.png`,
        fullPage: true,
      })
    }

    await page
      .getByRole('button', { name: 'Save model prices', exact: true })
      .first()
      .click()
    const review = page.getByRole('dialog', { name: 'Confirm price changes' })
    const items = review.locator('[data-pricing-review] > li')
    await expect(items).toHaveCount(3)
    await expect(items.filter({ hasText: 'gpt-review-removed' })).toContainText(
      'Unpriced after saving; requests will be rejected'
    )
    await expect(items.filter({ hasText: 'gpt-review-added' })).toContainText(
      'Unpriced'
    )
    await expect(review).not.toContainText('gpt-review-unchanged')
    const panel = review.locator('[data-handdrawn="modal"]')
    const dialogBox = (await panel.boundingBox())!
    expect(dialogBox.x).toBeGreaterThanOrEqual(0)
    expect(dialogBox.x + dialogBox.width).toBeLessThanOrEqual(width)
    // The review uses the wide dialog on desktop so prices sit side by side.
    if (width >= 768) expect(dialogBox.width).toBeGreaterThan(600)
    // The summary is read when the dialog opens.
    const describedBy = await review.getAttribute('aria-describedby')
    await expect(page.locator(`[id="${describedBy}"]`)).toHaveText(
      'New prices apply to requests as soon as you save. 3 models change.'
    )
    if (shotsDir)
      await page.screenshot({
        path: `${shotsDir}/review-${theme}-${width}.png`,
      })

    await review.getByRole('button', { name: 'Cancel', exact: true }).click()
    await expect(review).toHaveCount(0)
    expect(writes).toEqual([])
    await page
      .getByRole('button', { name: 'Save model prices', exact: true })
      .first()
      .click()
    await review
      .getByRole('button', { name: 'Confirm and save', exact: true })
      .click()
    await expect(
      page.getByText('Model prices saved', { exact: true })
    ).toBeVisible()
    expect(writes).toHaveLength(1)
    expect(writes[0].map((change) => change.model_name).sort()).toEqual([
      'gpt-review-added',
      'gpt-review-modified',
      'gpt-review-removed-with-a-long-model-name-for-phones',
    ])
    expect(
      writes[0].find((change) =>
        change.model_name.startsWith('gpt-review-removed')
      )
    ).toEqual({
      model_name: 'gpt-review-removed-with-a-long-model-name-for-phones',
      expected_version: 'v-removed',
      pricing: {},
    })
  })
}
