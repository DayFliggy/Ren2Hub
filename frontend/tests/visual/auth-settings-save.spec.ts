import { expect, test } from '@playwright/test'
import { configureStablePage, assertNoHorizontalOverflow } from './fixtures'

for (const theme of ['light', 'dark'] as const)
  for (const width of [1440, 390]) {
    test(`authentication prerequisites and partial recovery ${theme} ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme, locale: 'en' })
      const stored: Record<string, string> = {
        GitHubOAuthEnabled: 'false',
        GitHubClientId: '',
        'oidc.enabled': 'false',
      }
      const writes: Array<{ key: string; value: string }> = []
      let rejectEnable = true
      await page.route('**/api/option/', (route) => {
        if (route.request().method() === 'GET')
          return route.fulfill({
            json: {
              success: true,
              data: Object.entries(stored)
                .filter(
                  ([key]) => !key.endsWith('Secret') && !key.endsWith('secret')
                )
                .map(([key, value]) => ({ key, value })),
            },
          })
        const update = route.request().postDataJSON()
        writes.push(update)
        if (update.key === 'GitHubOAuthEnabled' && rejectEnable)
          return route.fulfill({
            json: { success: false, message: 'Fixture enable rejected' },
          })
        if (update.key === 'GitHubOAuthEnabled' && update.value === 'true') {
          expect(stored.GitHubClientId).toBe('browser-client')
          expect(stored.GitHubClientSecret).toBe('browser-secret')
        }
        if (update.key === 'oidc.enabled' && update.value === 'true') {
          expect(stored['oidc.authorization_endpoint']).toBe(
            'https://oidc.example/auth'
          )
          expect(stored['oidc.token_endpoint']).toBe(
            'https://oidc.example/token'
          )
          expect(stored['oidc.user_info_endpoint']).toBe(
            'https://oidc.example/user'
          )
        }
        stored[update.key] = update.value
        return route.fulfill({ json: { success: true } })
      })
      let discoveries = 0
      await page.route('**/api/custom-oauth-provider/discovery', (route) => {
        discoveries++
        expect(route.request().postDataJSON()).toEqual({
          well_known_url: 'https://oidc.example/discovery',
        })
        return route.fulfill({
          json: {
            success: true,
            data: {
              well_known_url: 'https://oidc.example/discovery',
              discovery: {
                authorization_endpoint: 'https://oidc.example/auth',
                token_endpoint: 'https://oidc.example/token',
                userinfo_endpoint: 'https://oidc.example/user',
              },
            },
          },
        })
      })
      await page.goto('/system-settings/auth/oauth')
      await page.locator('[id="setting-GitHubClientId"]').fill('browser-client')
      await page
        .locator('[id="setting-GitHubClientSecret"]')
        .fill('browser-secret')
      await page
        .getByRole('switch', { name: 'Enable GitHub sign-in', exact: true })
        .click()
      const save = page.getByRole('button', {
        name: 'Save changes',
        exact: true,
      })
      await save.click()
      await expect(
        page.getByText(/2 settings were saved before an error/)
      ).toBeVisible()
      await expect(
        page.locator('[id="setting-GitHubClientSecret"]')
      ).toHaveValue('')
      expect(writes.map((x) => x.key)).toEqual([
        'GitHubClientId',
        'GitHubClientSecret',
        'GitHubOAuthEnabled',
      ])
      expect(stored.GitHubOAuthEnabled).toBe('false')
      rejectEnable = false
      writes.length = 0
      await save.click()
      await expect.poll(() => stored.GitHubOAuthEnabled).toBe('true')
      await expect(save).toBeDisabled()
      expect(writes).toEqual([{ key: 'GitHubOAuthEnabled', value: 'true' }])
      await page.locator('[id="setting-oidc.client_id"]').fill('oidc-client')
      await page
        .locator('[id="setting-oidc.client_secret"]')
        .fill('oidc-secret')
      await page
        .locator('[id="setting-oidc.well_known"]')
        .fill('https://oidc.example/discovery')
      await page
        .getByRole('switch', { name: 'Enable OIDC sign-in', exact: true })
        .click()
      writes.length = 0
      await save.click()
      await expect.poll(() => stored['oidc.enabled']).toBe('true')
      await expect(save).toBeDisabled()
      expect(discoveries).toBe(1)
      expect(writes.at(-1)).toEqual({ key: 'oidc.enabled', value: 'true' })
      await assertNoHorizontalOverflow(page)
    })
  }
