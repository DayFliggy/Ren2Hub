import { expect, test } from '@playwright/test'
import {
  configureStablePage,
  assertNoHorizontalOverflow,
  waitForStablePage,
} from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    test(`${theme} ${width} one-use PAT verification and audit`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme, authenticated: true })
      await page.route('**/api/status', async (route) => {
        await route.fulfill({
          json: {
            success: true,
            data: {
              register_enabled: true,
              password_login_enabled: true,
              frontend_capabilities: {
                login: 'live',
                profile: 'live',
                audit: 'live',
                passkey: 'live',
                two_factor: 'live',
                notifications: 'live',
              },
            },
          },
        })
      })
      let generated = 0
      await page.route('**/api/user/token/status', (route) =>
        route.fulfill({
          json: { success: true, data: { exists: false, token_ref: '' } },
        })
      )
      await page.route('**/api/user/token', async (route) => {
        expect(route.request().method()).toBe('POST')
        expect(route.request().headers()['x-security-proof']).toBe(
          'one-use-proof'
        )
        generated++
        await route.fulfill({
          json: { success: true, data: 'visual-once-only-pat' },
        })
      })
      await page.route('**/api/verify/methods?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              scope: 'access_token.generate',
              methods: [{ method: '2fa', available: true }],
              oauth_providers: [],
              password_encryption_enabled: false,
            },
          },
        })
      )
      await page.route('**/api/verify', (route) => {
        expect(route.request().postDataJSON()).toEqual({
          scope: 'access_token.generate',
          method: '2fa',
          code: '123456',
        })
        return route.fulfill({
          json: {
            success: true,
            data: {
              scope: 'access_token.generate',
              method: '2fa',
              proof_token: 'one-use-proof',
              expires_at: 2_000_000_000,
            },
          },
        })
      })
      await page.route('**/api/audit/self?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              page: 1,
              page_size: 10,
              items: [
                {
                  id: 1,
                  event_id: 'visual-pat-1',
                  user_id: 1,
                  actor_role: 100,
                  created_at: 1_790_000_000,
                  username: 'visual.root',
                  action: 'access_token.generate',
                  category: 'security',
                  success: true,
                  ip: '192.0.2.1',
                  content: 'Token generated',
                  method: 'POST',
                  route: '/api/user/token',
                  status: 200,
                  other: { op: { action: 'access_token.generate' } },
                },
              ],
              total: 1,
            },
          },
        })
      )
      await page.goto('/settings')
      await waitForStablePage(page)
      await page.getByRole('button', { name: '管理密钥', exact: true }).click()
      await page
        .getByRole('button', { name: '重新生成密钥', exact: true })
        .click()
      await page
        .getByRole('dialog')
        .last()
        .getByRole('button', { name: '重新生成密钥', exact: true })
        .click()
      const proof = page.getByRole('dialog').last()
      await expect(
        proof.getByRole('heading', { name: '验证身份' })
      ).toBeVisible()
      expect(generated).toBe(0)
      await proof.getByRole('textbox').fill('123456')
      await proof.getByRole('button', { name: '确认', exact: true }).click()
      await expect(
        page.locator('input[aria-label="access-token"]')
      ).toHaveValue('visual-once-only-pat')
      expect(generated).toBe(1)
      await expect(
        page.getByRole('dialog', { name: '验证身份', exact: true })
      ).toHaveCount(0)
      await expect(
        page.getByRole('dialog', { name: '重新生成访问密钥？', exact: true })
      ).toHaveCount(0)
      await expect(
        page
          .getByRole('dialog', { name: '个人访问密钥', exact: true })
          .getByRole('button', { name: '关闭', exact: true })
      ).toBeEnabled()
      await page.keyboard.press('Escape')
      await expect(
        page.locator('input[aria-label="access-token"]')
      ).toHaveCount(0)
      await assertNoHorizontalOverflow(page)
      // The retired /audit bookmark lands on the merged log page.
      await page.goto('/audit')
      await waitForStablePage(page)
      await expect(page).toHaveURL(/\/usage-logs\/audit$/)
      await expect(
        page
          .getByText('生成个人访问令牌', { exact: true })
          .filter({ visible: true })
          .first()
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
    })
  }
}
