import { expect, test, type Page, type Response } from '@playwright/test'
import { loginCandidate } from './session'

interface UserRow {
  id: number
  username: string
  quota: number
}

interface RedemptionRow {
  id: number
  status: number
  created_time: number
  redeemed_time: number
}

interface Collection<T> {
  items: T[]
  total: number
}

function isApi(response: Response, path: string, method = 'GET'): boolean {
  return (
    new URL(response.url()).pathname === path &&
    response.request().method() === method
  )
}

async function successfulData<T>(response: Response): Promise<T> {
  expect(response.status()).toBe(200)
  const body = await response.json()
  expect(body.success).toBe(true)
  return body.data as T
}

async function collectionAfter<T>(
  page: Page,
  path: string,
  params: Record<string, string>,
  action: () => Promise<unknown>
): Promise<Collection<T>> {
  const pending = page.waitForResponse((response) => {
    if (!isApi(response, path)) return false
    const query = new URL(response.url()).searchParams
    return Object.entries(params).every(
      ([key, value]) => (query.get(key) ?? '') === value
    )
  })
  await action()
  return successfulData<Collection<T>>(await pending)
}

async function select(
  page: Page,
  label: string,
  option: string
): Promise<void> {
  await page.getByRole('combobox', { name: label, exact: true }).click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
  await loginCandidate(page)
})

test('real admin grants commit twice and keep operation identities per user', async ({
  page,
}) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto('/users')
  const prefix = `qa${Date.now().toString(36)}`

  async function createUser(username: string): Promise<void> {
    await page.getByRole('button', { name: 'New user', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'New user', exact: true })
    await dialog.locator('[name="admin-user-username"]').fill(username)
    // This disposable account secret is never logged by a fill action or saved
    // in an artifact. The normal browser form still creates the real account.
    await dialog.locator('[name="admin-user-password"]').evaluate((element) => {
      const setter = Object.getOwnPropertyDescriptor(
        HTMLInputElement.prototype,
        'value'
      )?.set
      if (!setter) throw new Error('User password input setter is unavailable')
      setter.call(element, `Q!${crypto.randomUUID()}`)
      element.dispatchEvent(new Event('input', { bubbles: true }))
    })
    const pending = page.waitForResponse((response) =>
      isApi(response, '/api/user/', 'POST')
    )
    await dialog.getByRole('button', { name: 'Save user', exact: true }).click()
    await successfulData(await pending)
    await expect(dialog).toHaveCount(0)
  }

  async function findUser(username: string): Promise<UserRow> {
    const data = await collectionAfter<UserRow>(
      page,
      '/api/user/search',
      { keyword: username, p: '1' },
      () => page.locator('[name="admin-user-search"]').fill(username)
    )
    const user = data.items.find((item) => item.username === username)
    expect(Boolean(user)).toBe(true)
    return user!
  }

  async function grant(user: UserRow) {
    await page
      .getByRole('button', { name: 'Adjust quota', exact: true })
      .filter({ visible: true })
      .click()
    const dialog = page.getByRole('dialog', {
      name: 'Adjust quota',
      exact: true,
    })
    const pending = page.waitForResponse((response) =>
      isApi(response, '/api/user/manage', 'POST')
    )
    const data = await collectionAfter<UserRow>(
      page,
      '/api/user/search',
      { keyword: user.username },
      async () => {
        await dialog
          .getByRole('button', { name: 'Confirm change', exact: true })
          .click()
        await successfulData(await pending)
      }
    )
    const write = (await pending).request().postDataJSON()
    expect(write.id).toBe(user.id)
    expect(write.action).toBe('add_quota')
    expect(write.mode).toBe('add')
    expect(Number.isSafeInteger(write.value) && write.value > 0).toBe(true)
    expect(write.operation_id).toMatch(/^[a-f0-9-]{36}$/)
    await expect(dialog).toHaveCount(0)
    const current = data.items.find((item) => item.id === user.id)
    expect(current?.quota).toBe(user.quota + write.value)
    return {
      current: current!,
      value: write.value as number,
      id: write.operation_id as string,
    }
  }

  await createUser(`${prefix}a`)
  await createUser(`${prefix}b`)
  const first = await findUser(`${prefix}a`)
  const grant1 = await grant(first)
  const grant2 = await grant(grant1.current)
  expect(grant2.value).toBe(grant1.value)
  expect(grant2.id).not.toBe(grant1.id)
  expect(grant2.current.quota).toBe(first.quota + 2 * grant1.value)
  const second = await findUser(`${prefix}b`)
  const grant3 = await grant(second)
  expect(new Set([grant1.id, grant2.id, grant3.id]).size).toBe(3)
  expect(errors).toEqual([])
})

test('real redemption creation, use, status filters and stable paginated sorting', async ({
  page,
}) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto('/redemption-codes')
  const campaign = `qa_${Date.now().toString(36)}`
  const keys: string[] = []

  async function generateBatch(): Promise<void> {
    await page
      .getByRole('button', { name: 'Generate codes', exact: true })
      .click()
    const dialog = page.getByRole('dialog', {
      name: 'Generate codes',
      exact: true,
    })
    await dialog.locator('input').nth(0).fill(campaign)
    await dialog.locator('input').nth(1).fill('0.01')
    await dialog.locator('input').nth(2).fill('12')
    const pending = page.waitForResponse((response) =>
      isApi(response, '/api/redemption/', 'POST')
    )
    await dialog.getByRole('button', { name: 'Generate', exact: true }).click()
    const created = await successfulData<string[]>(await pending)
    expect(Array.isArray(created) && created.length === 12).toBe(true)
    keys.push(...created)
    const success = page.getByRole('dialog', { name: 'Generated', exact: true })
    await expect(success).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(success).toHaveCount(0)
  }

  await generateBatch()
  const firstSecond = Math.floor(Date.now() / 1000)
  await expect
    .poll(() => Math.floor(Date.now() / 1000))
    .toBeGreaterThan(firstSecond)
  await generateBatch()

  const search = page.locator('[name="admin-redemption-search"]')
  const initial = await collectionAfter<RedemptionRow>(
    page,
    '/api/redemption/search',
    { keyword: campaign, p: '1' },
    () => search.fill(campaign)
  )
  expect(initial.total).toBe(24)
  expect(initial.items).toHaveLength(20)

  const nextPage = () =>
    page
      .getByRole('button', { name: 'Next', exact: true })
      .filter({ visible: true })
      .click()
  const secondPage = await collectionAfter<RedemptionRow>(
    page,
    '/api/redemption/search',
    { p: '2' },
    nextPage
  )
  expect(secondPage.items).toHaveLength(4)

  function expectSorted(
    rows: RedemptionRow[],
    field: 'created_time' | 'redeemed_time',
    ascending: boolean
  ) {
    const sign = ascending ? 1 : -1
    for (let index = 1; index < rows.length; index++) {
      const before = rows[index - 1]!
      const after = rows[index]!
      expect(
        sign * (after[field] - before[field] || after.id - before.id)
      ).toBeGreaterThan(0)
    }
  }

  let firstPage = await collectionAfter<RedemptionRow>(
    page,
    '/api/redemption/search',
    { sort_by: 'created_time', sort_order: 'desc', p: '1' },
    () => select(page, 'Sort by', 'Created')
  )
  let lastPage = await collectionAfter<RedemptionRow>(
    page,
    '/api/redemption/search',
    { sort_by: 'created_time', p: '2' },
    nextPage
  )
  const descending = [...firstPage.items, ...lastPage.items]
  expect(new Set(descending.map((item) => item.id)).size).toBe(24)
  expect(
    new Set(descending.map((item) => item.created_time)).size
  ).toBeGreaterThan(1)
  expectSorted(descending, 'created_time', false)
  firstPage = await collectionAfter<RedemptionRow>(
    page,
    '/api/redemption/search',
    { sort_order: 'asc', p: '1' },
    () =>
      page.getByRole('button', { name: 'Sort descending', exact: true }).click()
  )
  lastPage = await collectionAfter<RedemptionRow>(
    page,
    '/api/redemption/search',
    { sort_order: 'asc', p: '2' },
    nextPage
  )
  expect(
    [...firstPage.items, ...lastPage.items].map((item) => item.id)
  ).toEqual(descending.map((item) => item.id).reverse())

  await page.goto('/wallet?panel=redeem')
  for (const key of keys.slice(0, 2)) {
    // Redeemable keys are also kept out of Playwright's action log.
    await page.locator('#redeem-input').evaluate((element, value) => {
      const setter = Object.getOwnPropertyDescriptor(
        HTMLInputElement.prototype,
        'value'
      )?.set
      if (!setter) throw new Error('Redemption input setter is unavailable')
      setter.call(element, value)
      element.dispatchEvent(new Event('input', { bubbles: true }))
    }, key)
    const pending = page.waitForResponse((response) =>
      isApi(response, '/api/user/topup', 'POST')
    )
    await page.getByRole('button', { name: '→ Redeem', exact: true }).click()
    await successfulData(await pending)
    await expect(page.locator('#redeem-input')).toHaveValue('')
    const second = Math.floor(Date.now() / 1000)
    await expect
      .poll(() => Math.floor(Date.now() / 1000))
      .toBeGreaterThan(second)
  }

  await page.goto('/redemption-codes')
  await collectionAfter<RedemptionRow>(
    page,
    '/api/redemption/search',
    { keyword: campaign },
    () => search.fill(campaign)
  )
  await page
    .getByRole('combobox', { name: 'All statuses', exact: true })
    .click()
  await expect(page.getByRole('option')).toHaveText([
    'All statuses',
    'Unused',
    'Used',
    'Expired',
    'Disabled',
  ])
  let filtered = await collectionAfter<RedemptionRow>(
    page,
    '/api/redemption/search',
    { status: '3', p: '1' },
    () => page.getByRole('option', { name: 'Used', exact: true }).click()
  )
  expect(filtered.total).toBe(2)
  expect(filtered.items.every((item) => item.status === 3)).toBe(true)
  filtered = await collectionAfter<RedemptionRow>(
    page,
    '/api/redemption/search',
    { status: '3', sort_by: 'used_time', sort_order: 'desc' },
    () => select(page, 'Sort by', 'Used at')
  )
  expect(filtered.items.every((item) => item.redeemed_time > 0)).toBe(true)
  expect(new Set(filtered.items.map((item) => item.redeemed_time)).size).toBe(2)
  expectSorted(filtered.items, 'redeemed_time', false)
  const usedDescending = filtered.items.map((item) => item.id)
  filtered = await collectionAfter<RedemptionRow>(
    page,
    '/api/redemption/search',
    { status: '3', sort_by: 'used_time', sort_order: 'asc' },
    () =>
      page.getByRole('button', { name: 'Sort descending', exact: true }).click()
  )
  expectSorted(filtered.items, 'redeemed_time', true)
  expect(filtered.items.map((item) => item.id)).toEqual(
    usedDescending.reverse()
  )

  for (const [label, status, total] of [
    ['Unused', '1', 22],
    ['Disabled', '2', 0],
    ['Expired', 'expired', 0],
    ['All statuses', '', 24],
  ] as const) {
    filtered = await collectionAfter<RedemptionRow>(
      page,
      '/api/redemption/search',
      { status, p: '1' },
      () => select(page, 'All statuses', label)
    )
    expect(filtered.total).toBe(total)
  }
  expect(errors).toEqual([])
})
