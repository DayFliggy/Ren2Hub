import { expect, test } from '@playwright/test'
import { nativeCoreFixture } from './native-core-fixture'
import { assertNoHorizontalOverflow } from './fixtures'

for (const theme of ['light', 'dark'])
  for (const width of [1440, 768, 390]) {
    test(`channel routing and concurrency ${theme} ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 })
      const state = await nativeCoreFixture(page, theme)
      const requested: string[] = []
      page.on('request', (request) =>
        requested.push(new URL(request.url()).pathname)
      )
      await page.goto('/keys/7/routing')
      await expect(
        page.getByLabel('Manual routing', { exact: true })
      ).toBeChecked()
      await expect(
        page.getByText('Native channel', { exact: false }).first()
      ).toBeVisible()
      await page
        .getByRole('textbox', { name: 'Model', exact: true })
        .fill('gpt-native')
      await page
        .getByRole('button', { name: 'Preview saved routing', exact: true })
        .click()
      await expect(
        page.locator('li').filter({ hasText: 'Native channel · Eligible' })
      ).toBeVisible()
      await page.getByLabel('Automatic', { exact: true }).check()
      await page.getByRole('button', { name: 'Save', exact: true }).click()
      await expect
        .poll(
          () =>
            state.writes.find((w) => w.path === '/api/token/7/routing')?.body
        )
        .toEqual({
          mode: 'auto',
          version: 1,
          max_channel_ratio: null,
          entries: [],
        })
      await page.goto('/ops')
      await expect(
        page.getByText('Native channel', { exact: false }).first()
      ).toBeVisible()
      await expect(
        page.getByText('Go runtime memory', { exact: true })
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      expect(
        requested.some(
          (path) =>
            path.startsWith('/api/gateway/') || path.startsWith('/api/next/')
        )
      ).toBe(false)
      expect(state.errors).toEqual([])
    })
  }
