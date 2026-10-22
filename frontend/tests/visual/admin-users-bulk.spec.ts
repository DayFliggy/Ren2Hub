import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'
import { nativeUser } from './native-core-fixture'

// Native proofs bind one user each, so a bulk delete verifies every user and
// stops at the first failure without touching the rest.
for (const width of [1440, 390]) {
  test(`admin users bulk delete ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const state = await configureStablePage(page, { theme: 'light' })
    let users = [42, 43, 44].map((id) =>
      nativeUser({
        id,
        username: `member.${id}`,
        display_name: `Member ${id}`,
        role: 1,
        inviter_id: 0,
        last_login_at: 1781928000,
        DeletedAt: null,
      })
    )
    const proofContexts: unknown[] = []
    const deletes: number[] = []
    await page.route(/\/api\/user\/(?:search)?(?:\?.*)?$/, (route) =>
      route.fulfill({
        json: {
          success: true,
          data: { items: users, total: users.length, page: 1, page_size: 20 },
        },
      })
    )
    await page.route(/\/api\/user\/4[234]$/, (route) => {
      const id = Number(
        new URL(route.request().url()).pathname.split('/').pop()
      )
      deletes.push(id)
      if (id === 43)
        return route.fulfill({
          json: { success: false, message: 'Target is busy' },
        })
      users = users.filter((user) => user.id !== id)
      return route.fulfill({ json: { success: true } })
    })
    await page.route('**/api/verify/methods?*', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            scope: 'admin.user.delete',
            methods: [{ method: '2fa', available: true }],
            oauth_providers: [],
            password_encryption_enabled: false,
          },
        },
      })
    )
    await page.route('**/api/verify', (route) => {
      const body = route.request().postDataJSON()
      proofContexts.push(body.context)
      return route.fulfill({
        json: {
          success: true,
          data: {
            proof_token: `proof-${body.context.user_id}`,
            expires_at: Math.floor(Date.now() / 1000) + 300,
            method: '2fa',
            scope: body.scope,
          },
        },
      })
    })
    await page.goto('/users')

    const rows = page.locator(
      width > 768 ? 'tbody tr' : '[data-user-mobile-row]'
    )
    await expect(rows.filter({ visible: true })).toHaveCount(3)
    for (const checkbox of await rows
      .filter({ visible: true })
      .locator('input[type="checkbox"]')
      .all())
      await checkbox.check()
    const toolbar = page.locator('[data-user-bulk-actions]')
    await expect(toolbar).toContainText(
      '批量操作会为每位用户单独发起一次安全验证'
    )
    await toolbar
      .getByRole('button', { name: '批量删除选中用户', exact: true })
      .click()
    const confirmation = page.getByRole('dialog', { name: '批量删除用户' })
    await expect(confirmation).toContainText(
      '每位用户需要单独完成一次安全验证，共 3 次。'
    )
    await confirmation
      .getByRole('button', { name: '删除', exact: true })
      .click()

    const verification = page.getByRole('dialog', { name: '验证身份' })
    for (let step = 0; step < 2; step++) {
      // The next user's verification reuses the dialog, and opening it resets
      // the code. Wait until the previous delete ran and the old dialog left.
      await expect.poll(() => deletes.length).toBe(step)
      await expect(page.locator('.modal-leave-active')).toHaveCount(0)
      await expect(verification).toBeVisible()
      await verification.getByRole('textbox').fill('123456')
      await verification
        .getByRole('button', { name: '确认', exact: true })
        .click()
    }
    await expect(verification).toHaveCount(0)

    await expect(
      page.getByText('已处理 1/3 位用户，member.43 处理失败：Target is busy')
    ).toBeVisible()
    await expect(confirmation).toHaveCount(0)
    await expect(toolbar).toHaveCount(0)
    expect(proofContexts).toEqual([{ user_id: 42 }, { user_id: 43 }])
    expect(deletes).toEqual([42, 43])
    await expect(rows.filter({ visible: true })).toHaveCount(2)
    await assertNoHorizontalOverflow(page)
    expect(state.errors).toEqual([])
  })
}
