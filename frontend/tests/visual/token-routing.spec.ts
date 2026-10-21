import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

for (const width of [390, 1440]) {
  test(`native routing preserves channel order, ratio and version at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 })
    await configureStablePage(page, {
      theme: width === 390 ? 'dark' : 'light',
      authenticated: true,
    })
    await page.route('**/api/token/7/routing/channels', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: [
            {
              id: 2,
              name: 'Native channel',
              type: 1,
              status: 1,
              models: ['gpt-native'],
              channel_ratio: '1',
              max_concurrency: 8,
              in_flight: 2,
              eligible: true,
              filter_reason: '',
            },
            {
              id: 3,
              name: 'Fallback channel',
              type: 1,
              status: 1,
              models: ['gpt-native'],
              channel_ratio: '0.5',
              max_concurrency: 4,
              in_flight: 0,
              eligible: true,
              filter_reason: '',
            },
            {
              id: 4,
              name: 'Full channel',
              type: 1,
              status: 1,
              models: ['gpt-native'],
              channel_ratio: '1',
              max_concurrency: 1,
              in_flight: 1,
              eligible: false,
              filter_reason: 'channel_concurrency_full',
            },
          ],
        },
      })
    )
    const writes: Record<string, unknown>[] = []
    page.on('request', (request) => {
      if (
        request.method() === 'PUT' &&
        new URL(request.url()).pathname === '/api/token/7/routing'
      )
        writes.push(request.postDataJSON())
    })
    await page.goto('/keys/7/routing')
    await expect(
      page.getByRole('radio', { name: '手动路由', exact: true })
    ).toBeChecked()
    const channels = page.getByRole('combobox', { name: '选择平台渠道' })
    await expect(channels.locator('option[value="4"]')).toHaveJSProperty(
      'disabled',
      true
    )
    await channels.selectOption('3')
    await page.getByRole('button', { name: '添加渠道', exact: true }).click()
    await page
      .locator('[data-channel-id="3"]')
      .getByRole('button', { name: '上移', exact: true })
      .click()
    await expect(
      page.getByRole('list', { name: '渠道顺序' }).locator('li').first()
    ).toHaveAttribute('data-channel-id', '3')
    await page
      .getByRole('textbox', { name: '渠道倍率上限' })
      .fill('0.750000000000000001')
    const save = page.getByRole('button', { name: '保存', exact: true })
    await save.click()
    await expect.poll(() => writes.length).toBe(1)
    expect(writes[0]).toEqual({
      mode: 'manual',
      version: 1,
      max_channel_ratio: '0.750000000000000001',
      entries: [
        { channel_id: 3, enabled: true },
        { channel_id: 2, enabled: true },
      ],
    })
    await expect(save).toBeEnabled()
    await page.getByRole('radio', { name: '自动路由', exact: true }).check()
    await page.getByRole('textbox', { name: '渠道倍率上限' }).fill('')
    await save.click()
    await expect.poll(() => writes.length).toBe(2)
    expect(writes[1]).toEqual({
      mode: 'auto',
      version: 2,
      max_channel_ratio: null,
      entries: [],
    })
    await expect(save).toBeEnabled()
    await page
      .getByRole('textbox', { name: '模型', exact: true })
      .fill('gpt-native')
    await page
      .getByRole('button', { name: '预览已保存路由', exact: true })
      .click()
    await expect(page.getByText('已保存版本 3', { exact: true })).toBeVisible()
    await assertNoHorizontalOverflow(page)
  })
}

test('routing version conflict blocks edits until an explicit reload', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.route('**/api/token/7/routing', (route) => {
    if (route.request().method() !== 'PUT') return route.fallback()
    return route.fulfill({
      status: 409,
      json: { success: false, message: 'version conflict' },
    })
  })
  await page.goto('/keys/7/routing')
  const ratio = page.getByRole('textbox', { name: '渠道倍率上限' })
  await ratio.fill('0.5')
  const save = page.getByRole('button', { name: '保存', exact: true })
  await save.click()
  const alert = page
    .getByRole('alert')
    .filter({ hasText: '路由已被其他操作修改' })
  await expect(alert).toBeVisible()
  await expect(save).toBeDisabled()
  await expect(ratio).toBeDisabled()
  await alert.getByRole('button', { name: '重新加载', exact: true }).click()
  await expect(alert).toBeHidden()
  await expect(save).toBeEnabled()
  await expect(ratio).toHaveValue('')
})
