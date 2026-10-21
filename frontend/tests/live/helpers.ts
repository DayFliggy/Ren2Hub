import { readFileSync, statSync } from 'node:fs'
import { expect, type Page } from '@playwright/test'

export type Role = 'user' | 'root'

const credentialsPath = process.env.REN2HUB_LIVE_CREDENTIALS
if (!credentialsPath || !credentialsPath.includes('/candidate-private/'))
  throw new Error('Dedicated candidate credentials are required')
if ((statSync(credentialsPath).mode & 0o777) !== 0o600)
  throw new Error('Candidate credentials must have mode 0600')
export const accounts = JSON.parse(
  readFileSync(credentialsPath, 'utf8')
) as Record<Role, { username: string; password: string }>
let clientSequence = 1

export async function navigate(page: Page, path: string) {
  // Await the application's real router, including guards and lazy page loads.
  // Changing history alone can capture the previous page during a transition.
  await page.evaluate(async (nextPath) => {
    const element = document.querySelector('#app') as Element & {
      __vue_app__: {
        config: {
          globalProperties: {
            $router: { push(path: string): Promise<unknown> }
          }
        }
      }
    }
    await element.__vue_app__.config.globalProperties.$router.push(nextPath)
    await new Promise<void>((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
    )
  }, path)
  await expect(
    page.locator('.page-leave-active, .page-enter-active')
  ).toHaveCount(0)
  await page.waitForLoadState('networkidle')
  await expect(page.locator('main').first()).toBeVisible()
  await expect(page.locator('main').first()).not.toBeEmpty()
}

export async function login(page: Page, role: Role) {
  // The local Vite proxy collapses browsers to one source address. Give each
  // isolated browser a distinct forwarded test address, like distinct clients
  // behind that proxy, without disabling the backend's actual rate limits.
  await page.setExtraHTTPHeaders({
    'X-Forwarded-For': `127.78.${(process.pid % 254) + 1}.${clientSequence++}`,
  })
  await page.goto('/sign-in', { waitUntil: 'networkidle' })
  await page
    .locator('input[autocomplete="username"]')
    .fill(accounts[role].username)
  await page
    .locator('input[autocomplete="current-password"]')
    .fill(accounts[role].password)
  const response = page.waitForResponse(
    (value) =>
      value.request().method() === 'POST' &&
      new URL(value.url()).pathname === '/api/user/login'
  )
  await page.locator('form button[type="submit"]').click()
  await expect(page).toHaveURL(/\/dashboard$/)
  await page.waitForLoadState('networkidle')
  return (await (await response).json()).data as {
    access_token: string
    user: { quota: number }
  }
}
