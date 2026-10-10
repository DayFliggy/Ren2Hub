import { expect, test } from '@playwright/test'

import { configureStablePage, waitForStablePage } from './fixtures'

test('a device without a language choice adopts the account language', async ({
  page,
}) => {
  await configureStablePage(page, {
    theme: 'light',
    user: { setting: JSON.stringify({ language: 'en' }) },
  })
  // Runs after the fixture script, which stores a zh-CN choice.
  await page.addInitScript(() => localStorage.removeItem('ren2hub_locale'))

  await page.goto('/settings', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  await expect(page.locator('html')).toHaveAttribute('lang', 'en')
  await expect(
    page.getByRole('heading', { name: 'Notifications & preferences' })
  ).toBeVisible()
  expect(
    await page.evaluate(() => localStorage.getItem('ren2hub_locale'))
  ).toBe('en')
})

test('a language chosen while signed in is saved to the account', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  await page.goto('/settings', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  const saved = page.waitForRequest(
    (request) =>
      request.method() === 'PUT' &&
      new URL(request.url()).pathname === '/api/user/self'
  )
  await page.getByRole('button', { name: '语言', exact: true }).click()
  await page.getByRole('menuitemradio', { name: 'English' }).click()

  expect((await saved).postDataJSON()).toEqual({ language: 'en' })
  await expect(page.locator('html')).toHaveAttribute('lang', 'en')
  await expect(page.getByRole('alert')).toHaveCount(0)
})
