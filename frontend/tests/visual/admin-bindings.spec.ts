import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'
for (const theme of ['light', 'dark'] as const)
  for (const width of [1440, 390]) {
    test(`admin bindings ${theme} ${width}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme })
      await page.route('**/api/gateway/users/42', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              user_id: 42,
              group: 'default',
              config_version: 1,
              override: { concurrency: null, wait_ms: null, queue_size: null },
              effective: {
                concurrency: 10,
                wait_ms: 1000,
                queue_size: 2,
                channel_queue_size: 20,
              },
              sources: {
                concurrency: 'system',
                wait_ms: 'system',
                queue_size: 'system',
                channel_queue_size: 'system',
              },
            },
          },
        })
      )
      await page.route('**/api/gateway/runtime/users/42', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              capacity: 10,
              config_version: 1,
              revision: 1,
              started_at: 1785114000,
              draining: false,
              in_flight: 0,
              queued: 0,
            },
          },
        })
      )
      let removed = false
      let emailRemoved = false
      const deletes: string[] = []
      await page.route('**/api/user/42', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              github_id: removed ? '' : 'private-identity',
              email: emailRemoved ? '' : 'visual.member@example.test',
            },
          },
        })
      )
      await page.route('**/api/user/42/oauth/bindings', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: [
              {
                provider_id: 9,
                provider_name: 'Company SSO',
                provider_user_id: 'private-custom',
              },
            ],
          },
        })
      )
      await page.route('**/api/user/42/bindings/github', (route) => {
        deletes.push(route.request().method())
        removed = true
        return route.fulfill({ json: { success: true } })
      })
      await page.route('**/api/user/42/bindings/email', (route) => {
        emailRemoved = true
        return route.fulfill({ json: { success: true } })
      })
      await page.route('**/api/verify/methods?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              scope: 'admin.user.change',
              methods: [{ method: '2fa', available: true }],
              oauth_providers: [],
              password_encryption_enabled: false,
            },
          },
        })
      )
      await page.route('**/api/verify', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              proof_token: 'visual-proof',
              expires_at: Math.floor(Date.now() / 1000) + 300,
              method: '2fa',
              scope: 'admin.user.change',
            },
          },
        })
      )
      await page.goto('/users')
      await page.locator('[aria-label="编辑用户"]:visible').first().click()
      await page.locator('[data-admin-bindings]').click()
      const bindings = page.getByRole('dialog', {
        name: '管理账户绑定',
        exact: true,
      })
      await expect(
        bindings.getByText('Company SSO', { exact: true })
      ).toBeVisible()
      await expect(bindings).not.toContainText('private-')
      await bindings
        .getByRole('button', { name: '解绑 GitHub', exact: true })
        .click()
      const confirmation = page.getByRole('dialog', {
        name: '解绑账户身份',
        exact: true,
      })
      expect(deletes).toEqual([])
      await expect(confirmation).toContainText('visual.member')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('unbind-confirm.png'),
        fullPage: true,
      })
      await confirmation
        .getByRole('button', { name: '解绑', exact: true })
        .click()
      const verification = page.getByRole('dialog', { name: '验证身份' })
      await expect(verification).toBeVisible()
      await verification.getByRole('textbox').fill('123456')
      await verification
        .getByRole('button', { name: '确认', exact: true })
        .click()
      await expect(confirmation).toHaveCount(0)
      await expect(
        bindings.getByRole('button', { name: '解绑 GitHub', exact: true })
      ).toHaveCount(0)
      expect(deletes).toEqual(['DELETE'])
      await bindings
        .getByRole('button', { name: '解绑 Email', exact: true })
        .click()
      await confirmation
        .getByRole('button', { name: '解绑', exact: true })
        .click()
      await expect(verification).toBeVisible()
      await verification.getByRole('textbox').fill('123456')
      await verification
        .getByRole('button', { name: '确认', exact: true })
        .click()
      await expect(confirmation).toHaveCount(0)
      await page.keyboard.press('Escape')
      await expect(bindings).toHaveCount(0)
      await expect(
        page.getByRole('dialog', { name: '编辑用户', exact: true })
      ).toBeVisible()
      await expect(page.locator('[name="admin-user-email"]')).toHaveValue('')
    })
  }
