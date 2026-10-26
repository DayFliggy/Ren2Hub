import { expect, test } from '@playwright/test'
import {
  configureStablePage,
  assertNoHorizontalOverflow,
  waitForStablePage,
} from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    test(`${theme} ${width} retired single-token entry and historical audit`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme, authenticated: true })
      const retiredTokenRequests: string[] = []
      page.on('request', (request) => {
        const path = new URL(request.url()).pathname
        if (path === '/api/user/token' || path.startsWith('/api/user/token/')) {
          retiredTokenRequests.push(path)
        }
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
                  token_ref: '',
                  auth_method: 'session',
                  user_agent: 'browser',
                  request_id: 'visual-audit',
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
      await page.goto('/security')
      await waitForStablePage(page)
      await expect(
        page.getByRole('heading', { name: '登录与账户', exact: true })
      ).toBeVisible()
      await expect(
        page.getByRole('button', { name: '管理密钥', exact: true })
      ).toHaveCount(0)
      await expect(
        page.getByRole('button', { name: '重新生成密钥', exact: true })
      ).toHaveCount(0)
      await expect(
        page.getByRole('dialog', { name: '个人访问密钥', exact: true })
      ).toHaveCount(0)
      await expect(
        page.locator('input[aria-label="access-token"]')
      ).toHaveCount(0)
      expect(retiredTokenRequests).toEqual([])
      await assertNoHorizontalOverflow(page)
      // Keep historical single-token audit events readable after retiring its UI.
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
      expect(retiredTokenRequests).toEqual([])
    })
  }
}
