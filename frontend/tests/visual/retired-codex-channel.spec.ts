import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

const active = {
  id: 101,
  name: 'OpenAI Codex Model',
  type: 1,
  channel_ratio: '1',
  max_concurrency: 8,
  in_flight: 0,
  lab: { id: 'fixture', name: 'Fixture lab', source: 'vendor' },
  status: 1,
  priority: 0,
  weight: 10,
  used_quota: 0,
  balance: 0,
  response_time: 0,
  test_time: 0,
  base_url: 'https://mock.example.test',
  models: 'gpt-5-codex',
  setting: '{}',
  settings: '{}',
}
for (const theme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    test(`ordinary Codex models remain available without subscription channels ${theme} ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme, authenticated: true })
      const retiredRequests: string[] = []
      page.on('request', (request) => {
        if (/\/api\/(?:subscription|channel\/[^/]+\/codex)/.test(request.url()))
          retiredRequests.push(request.url())
      })
      await page.route(/\/api\/channel\/(?:\?.*)?$/, (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              items: [active],
              total: 1,
              page: 1,
              page_size: 20,
              type_counts: { '1': 1 },
            },
          },
        })
      )
      await page.goto('/channels')
      await page
        .getByRole('button', { name: '编辑渠道', exact: true })
        .filter({ visible: true })
        .first()
        .click()
      const editor = page.getByRole('dialog', { name: '编辑渠道', exact: true })
      await expect(
        editor.getByText('gpt-5-codex', { exact: true })
      ).toBeVisible()
      await editor.getByRole('button', { name: 'OpenAI', exact: true }).click()
      await expect(
        editor.getByRole('option', { name: 'OpenAI', exact: true })
      ).toBeVisible()
      await expect(
        editor.getByRole('option', { name: /Codex|Subscription/ })
      ).toHaveCount(0)
      await editor.getByRole('option', { name: 'OpenAI', exact: true }).click()
      await assertNoHorizontalOverflow(page)
      expect(retiredRequests).toEqual([])
    })
  }
}
