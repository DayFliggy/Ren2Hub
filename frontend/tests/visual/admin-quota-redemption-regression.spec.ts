import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'
import { nativeUser } from './native-core-fixture'

for (const [theme, width] of [
  ['light', 1440],
  ['dark', 390],
] as const) {
  test(`quota response loss and independent operations ${theme} ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 })
    const state = await configureStablePage(page, { theme })
    const users = [42, 43].map((id) =>
      nativeUser({ id, username: `audit.member.${id}`, role: 1, quota: 0 })
    )
    const operations = new Map<string, { id: number; value: number }>()
    const writes: Array<{ id: number; value: number; operation_id: string }> =
      []
    await page.route(/\/api\/user\/(?:search)?(?:\?.*)?$/, (route) =>
      route.fulfill({
        json: {
          success: true,
          data: { items: users, total: 2, page: 1, page_size: 20 },
        },
      })
    )
    await page.route('**/api/user/manage', async (route) => {
      const body = route.request().postDataJSON()
      expect(body).toMatchObject({
        action: 'add_quota',
        mode: 'add',
        value: 500000,
      })
      writes.push(body)
      const original = operations.get(body.operation_id)
      if (original) expect(original).toEqual({ id: body.id, value: body.value })
      else {
        operations.set(body.operation_id, { id: body.id, value: body.value })
        users.find((user) => user.id === body.id)!.quota += body.value
      }
      // The server committed the grant, but the browser cannot confirm it.
      if (writes.length === 1) return route.abort('connectionreset')
      return route.fulfill({ json: { success: true, data: null } })
    })
    await page.goto('/users')
    const adjust = page
      .getByRole('button', { name: '调整额度', exact: true })
      .filter({ visible: true })
    await adjust.first().click()
    const dialog = page.getByRole('dialog', { name: '调整额度', exact: true })
    const submit = dialog.getByRole('button', { name: '确认变更', exact: true })
    await submit.click()
    await expect.poll(() => writes.length).toBe(1)
    await expect(submit).toBeEnabled()
    await submit.click()
    await expect(dialog).toHaveCount(0)
    expect(writes).toHaveLength(2)
    expect(writes[1]!.operation_id).toBe(writes[0]!.operation_id)
    expect(users[0]!.quota).toBe(500000)

    await adjust.first().click()
    await submit.click()
    await expect(dialog).toHaveCount(0)
    expect(writes[2]!.operation_id).not.toBe(writes[0]!.operation_id)
    expect(users[0]!.quota).toBe(1000000)

    await adjust.nth(1).click()
    await submit.click()
    await expect(dialog).toHaveCount(0)
    expect(writes[3]!.id).toBe(43)
    expect(new Set(writes.map((write) => write.operation_id)).size).toBe(3)
    expect(users[1]!.quota).toBe(500000)
    await assertNoHorizontalOverflow(page)
    expect(state.errors).toEqual([])
  })

  test(`redemption native status sorting and pagination ${theme} ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 })
    const state = await configureStablePage(page, { theme })
    const rows = Array.from({ length: 24 }, (_, i) => ({
      id: i + 1,
      name: `Audit code ${i + 1}`,
      key: `AUDIT-${String(i + 1).padStart(3, '0')}`,
      quota: 500000,
      status: i % 4 === 0 ? 3 : i % 4 === 1 ? 2 : 1,
      used_user_id: i % 4 === 0 ? 42 : 0,
      created_time: 1785124800 + (i % 6),
      redeemed_time: i % 4 === 0 ? 1785124900 + i : 0,
      expired_time: i % 4 === 3 ? 1780000000 : 0,
    }))
    const queries: URLSearchParams[] = []
    await page.route(/\/api\/redemption\/(?:search)?(?:\?.*)?$/, (route) => {
      const query = new URL(route.request().url()).searchParams
      queries.push(query)
      let items = rows.filter(
        (row) =>
          !query.get('status') || String(row.status) === query.get('status')
      )
      const field =
        query.get('sort_by') === 'used_time'
          ? 'redeemed_time'
          : query.get('sort_by') === 'created_time'
            ? 'created_time'
            : 'id'
      const sign = query.get('sort_order') === 'asc' ? 1 : -1
      items = items.toSorted(
        (a, b) => sign * (a[field] - b[field] || a.id - b.id)
      )
      const total = items.length
      const size = Number(query.get('page_size'))
      const page = Number(query.get('p'))
      items = items.slice((page - 1) * size, page * size)
      return route.fulfill({
        json: { success: true, data: { items, total, page, page_size: size } },
      })
    })
    await page.goto('/redemption-codes')
    await expect.poll(() => queries.at(-1)?.get('sort_by')).toBe('id')
    await page
      .getByRole('button', { name: '下一页', exact: true })
      .filter({ visible: true })
      .click()
    await expect.poll(() => queries.at(-1)?.get('p')).toBe('2')

    await page.getByRole('combobox', { name: '排序字段', exact: true }).click()
    await page.getByRole('option', { name: '创建时间', exact: true }).click()
    await expect.poll(() => queries.at(-1)?.get('sort_by')).toBe('created_time')
    expect(queries.at(-1)?.get('p')).toBe('1')
    await page.getByRole('button', { name: '降序排列', exact: true }).click()
    await expect.poll(() => queries.at(-1)?.get('sort_order')).toBe('asc')

    await page.getByRole('combobox', { name: '全部状态', exact: true }).click()
    await expect(page.getByRole('option')).toHaveText([
      '全部状态',
      '未使用',
      '已使用',
      '已过期',
      '已禁用',
    ])
    await page.getByRole('option', { name: '已使用', exact: true }).click()
    await expect.poll(() => queries.at(-1)?.get('status')).toBe('3')
    expect(queries.at(-1)?.get('p')).toBe('1')
    await page.getByRole('combobox', { name: '排序字段', exact: true }).click()
    await page.getByRole('option', { name: '使用时间', exact: true }).click()
    await expect.poll(() => queries.at(-1)?.get('sort_by')).toBe('used_time')
    await expect(
      page.getByText('AUDIT-001', { exact: true }).filter({ visible: true })
    ).toBeVisible()
    await assertNoHorizontalOverflow(page)
    expect(state.errors).toEqual([])
  })
}
