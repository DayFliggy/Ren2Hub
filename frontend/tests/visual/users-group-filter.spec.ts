import { expect, test } from '@playwright/test'
import { configureStablePage, assertNoHorizontalOverflow } from './fixtures'
import { nativeUser } from './native-core-fixture'

// Business groups were removed; keep responsive user filtering and replace the
// retired group editor with the actual per-user concurrency field.
for (const theme of ['light', 'dark'] as const)
  for (const width of [1440, 768, 390]) {
    test(`users keyword and concurrency ${theme} ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 })
      const state = await configureStablePage(page, { theme })
      const users = [
        nativeUser({
          id: 42,
          username: 'first.member',
          display_name: 'First member',
          role: 1,
          max_concurrency: 3,
          inviter_id: 0,
          last_login_at: 1781928000,
        }),
        nativeUser({
          id: 43,
          username: 'second.member',
          display_name: 'Second member',
          role: 1,
          max_concurrency: 7,
          inviter_id: 0,
          last_login_at: 1781928000,
        }),
      ]
      const queries: URLSearchParams[] = []
      let saved: Record<string, unknown> | undefined
      await page.route(/\/api\/user\/(?:search)?(?:\?.*)?$/, (route) => {
        if (route.request().method() === 'PUT') {
          saved = route.request().postDataJSON()
          return route.fulfill({ json: { success: true } })
        }
        const query = new URL(route.request().url()).searchParams
        queries.push(query)
        const keyword = query.get('keyword') ?? ''
        const items = users.filter((user) =>
          String(user.username).includes(keyword)
        )
        return route.fulfill({
          json: {
            success: true,
            data: { items, total: items.length, page: 1, page_size: 20 },
          },
        })
      })
      await page.route('**/api/user/43', (route) =>
        route.fulfill({ json: { success: true, data: users[1] } })
      )
      await page.goto('/users')
      const search = page.locator('[name="admin-user-search"]')
      await expect(page.getByLabel('用户分组', { exact: true })).toHaveCount(0)
      await search.fill('second')
      await expect(
        page
          .getByText('first.member', { exact: true })
          .filter({ visible: true })
      ).toHaveCount(0)
      await expect(
        page
          .getByText('second.member', { exact: true })
          .filter({ visible: true })
          .first()
      ).toBeVisible()
      await page
        .getByRole('button', { name: '编辑用户', exact: true })
        .filter({ visible: true })
        .first()
        .click()
      const dialog = page.getByRole('dialog', { name: '编辑用户', exact: true })
      await dialog.getByRole('spinbutton').fill('9')
      await dialog
        .getByRole('button', { name: '保存用户', exact: true })
        .click()
      await expect.poll(() => saved?.max_concurrency).toBe(9)
      expect(saved).not.toHaveProperty('group')
      expect(queries.every((query) => !query.has('group'))).toBe(true)
      await expect(dialog).toHaveCount(0)
      await search.fill('')
      await expect(
        page
          .getByText('first.member', { exact: true })
          .filter({ visible: true })
          .first()
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      expect(state.errors).toEqual([])
    })
  }
