import { readFileSync } from 'node:fs'
import { expect, type Page } from '@playwright/test'

export async function loginCandidate(page: Page) {
  const file = process.env.NATIVE_LIVE_CREDENTIALS
  if (!file) throw new Error('Private candidate credentials are required')
  const value = JSON.parse(readFileSync(file, 'utf8'))
  const credentials = value.root ?? value
  if (
    typeof credentials.username !== 'string' ||
    typeof credentials.password !== 'string'
  )
    throw new Error('Invalid private candidate account file')
  await page.goto('/sign-in')
  await expect(page.locator('input[autocomplete="username"]')).toBeVisible()
  await expect(
    page.locator('input[autocomplete="current-password"]')
  ).toBeVisible()
  // Locator.fill logs its value on failure. Keep credentials out of the
  // function source and action log while retaining the browser's real form.
  await page.evaluate(
    ({ username, password }) => {
      const user = document.querySelector('input[autocomplete="username"]')
      const secret = document.querySelector(
        'input[autocomplete="current-password"]'
      )
      const setter = Object.getOwnPropertyDescriptor(
        HTMLInputElement.prototype,
        'value'
      )?.set
      if (
        !(user instanceof HTMLInputElement) ||
        !(secret instanceof HTMLInputElement) ||
        !setter
      )
        throw new Error('Candidate login fields are unavailable')
      setter.call(user, username)
      user.dispatchEvent(new Event('input', { bubbles: true }))
      setter.call(secret, password)
      secret.dispatchEvent(new Event('input', { bubbles: true }))
    },
    { username: credentials.username, password: credentials.password }
  )
  await page.locator('button[type="submit"]').click()
  await expect(page).not.toHaveURL(/\/sign-in/)
}
