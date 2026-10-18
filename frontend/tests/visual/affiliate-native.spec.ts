import { expect, test } from '@playwright/test'
import { nativeCoreFixture } from './native-core-fixture'
import { nativeAffiliateInfo } from './channel-core-fixtures'
import { assertNoHorizontalOverflow } from './fixtures'

for (const theme of ['light', 'dark'])
  for (const width of [1440, 390]) {
    test(`affiliate whole transfer retry ${theme} ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 })
      const state = await nativeCoreFixture(page, theme)
      let transferred = false,
        fail = true
      const writes: Array<Record<string, unknown>> = []
      await page.route('**/api/user/affiliate', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              ...nativeAffiliateInfo(),
              transferable: transferred ? 0 : 123,
              available_reward: transferred ? 0 : 123,
            },
          },
        })
      )
      await page.route('**/api/user/affiliate/transfer', (route) => {
        const body = route.request().postDataJSON()
        writes.push(body)
        expect(Object.keys(body)).toEqual(['operation_id'])
        expect(body.operation_id).toMatch(/^[0-9a-f-]{36}$/)
        if (fail)
          return route.fulfill({
            status: 503,
            json: {
              success: false,
              message: 'Transfer temporarily unavailable',
            },
          })
        transferred = true
        state.user.quota += 123
        return route.fulfill({ json: { success: true, data: { quota: 123 } } })
      })
      await page.goto('/invite')
      const open = page
        .getByRole('button', { name: 'Transfer to balance', exact: true })
        .first()
      await expect(open).toBeEnabled()
      await open.click()
      const dialog = page.getByRole('dialog', {
        name: 'Transfer invite rewards',
        exact: true,
      })
      await expect(dialog.getByRole('spinbutton')).toHaveCount(0)
      await expect(dialog.getByRole('textbox')).toHaveCount(0)
      await dialog.getByRole('button', { name: 'Confirm', exact: true }).click()
      await expect(
        page.getByText('Transfer temporarily unavailable', { exact: true })
      ).toBeVisible()
      await expect(dialog).toBeVisible()
      fail = false
      await dialog.getByRole('button', { name: 'Confirm', exact: true }).click()
      await expect(dialog).toHaveCount(0)
      expect(writes).toHaveLength(2)
      expect(writes[1]).toEqual(writes[0])
      await expect(open).toBeDisabled()
      await assertNoHorizontalOverflow(page)
      expect(state.errors).toEqual([])
    })
  }

for (const width of [1440, 390]) {
  test(`affiliate invitees use masked paginated attribution ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 })
    const state = await nativeCoreFixture(page, 'light')
    const reads: Array<{ page: number; order: string }> = []
    await page.route('**/api/user/affiliate/invitees?*', (route) => {
      const url = new URL(route.request().url())
      const currentPage = Number(url.searchParams.get('p'))
      const order = url.searchParams.get('order') ?? ''
      expect(url.searchParams.get('page_size')).toBe('20')
      reads.push({ page: currentPage, order })
      return route.fulfill({
        json: {
          success: true,
          data: {
            page: currentPage,
            page_size: 20,
            total: 21,
            items:
              currentPage === 2
                ? [
                    {
                      masked_name: 'Z***d',
                      invited_at: 1792000000,
                      reward_quota: 123,
                    },
                  ]
                : Array.from({ length: 20 }, (_, index) => ({
                    masked_name: `${order === 'asc' ? 'E' : 'R'}***${index}`,
                    invited_at: 1792100000 + index,
                    reward_quota: index * 100,
                  })),
          },
        },
      })
    })
    await page.goto('/invite')
    const invitees = page
      .locator('section')
      .filter({ has: page.getByRole('heading', { name: 'Invited users' }) })
    await expect(invitees.getByText('R***0', { exact: true })).toBeVisible()
    await expect(invitees.getByRole('listitem')).toHaveCount(20)
    await invitees.getByRole('button', { name: 'Next', exact: true }).click()
    await expect(invitees.getByText('Z***d', { exact: true })).toBeVisible()
    await expect(invitees.getByRole('listitem')).toHaveCount(1)
    await expect(
      invitees.getByRole('button', { name: 'Next', exact: true })
    ).toBeDisabled()
    await invitees.getByRole('radio', { name: 'Earliest', exact: true }).click()
    await expect(invitees.getByText('E***0', { exact: true })).toBeVisible()
    await expect(
      invitees.getByRole('button', { name: 'Prev', exact: true })
    ).toBeDisabled()
    expect(reads).toEqual([
      { page: 1, order: 'desc' },
      { page: 2, order: 'desc' },
      { page: 1, order: 'asc' },
    ])
    await assertNoHorizontalOverflow(page)
    expect(state.errors).toEqual([])
  })
}
