import { expect, test } from '@playwright/test'
import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const viewport of ['desktop', 'mobile'] as const) {
    test(`${theme} ${viewport} key creation and native per-Key routing`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        viewport === 'desktop'
          ? { width: 1440, height: 900 }
          : { width: 390, height: 844 }
      )
      const fixture = await configureStablePage(page, {
        theme,
        authenticated: true,
      })
      await page.goto('/keys')
      await waitForStablePage(page)
      await expect(
        page.getByText('Native Key').filter({ visible: true })
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('keys.png'),
        fullPage: true,
      })
      const create = page.getByRole('button', { name: '创建令牌', exact: true })
      await create.click()
      const dialog = page.getByRole('dialog', { name: '创建令牌', exact: true })
      await dialog
        .locator('input[name="token-name"]')
        .fill('Native routing key')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({ path: testInfo.outputPath('create-key.png') })
      await dialog.getByRole('button', { name: '确认', exact: true }).click()
      await expect(dialog).toHaveCount(0)
      await expect(create).toBeFocused()
      expect(
        fixture.writes.some(
          (write) =>
            write.path === '/api/token/' &&
            (write.body as { name?: string }).name === 'Native routing key'
        )
      ).toBe(true)
      await page.goto('/keys/7/routing')
      await waitForStablePage(page)
      const routePage = page.locator('[data-handdrawn-page="token-routing"]')
      await expect(routePage).toBeVisible()
      const automatic = page.getByRole('radio', {
        name: '自动路由',
        exact: true,
      })
      const manual = page.getByRole('radio', { name: '手动路由', exact: true })
      await automatic.focus()
      await page.keyboard.press('Space')
      await expect(automatic).toBeChecked()
      await page.getByRole('button', { name: '保存', exact: true }).click()
      await expect(
        page.getByText('路由已保存。', { exact: true })
      ).toBeVisible()
      await page.screenshot({
        path: testInfo.outputPath('automatic-routing.png'),
        fullPage: true,
      })
      await manual.check()
      await page
        .getByRole('combobox', { name: '选择平台渠道', exact: true })
        .selectOption('2')
      await page.getByRole('button', { name: '添加渠道', exact: true }).click()
      await page
        .getByRole('textbox', { name: '渠道倍率上限', exact: true })
        .fill('1.25')
      await page.getByRole('button', { name: '保存', exact: true }).click()
      await expect(
        page.getByRole('button', { name: '保存', exact: true })
      ).toBeEnabled()
      await page
        .getByRole('textbox', { name: '模型', exact: true })
        .fill('gpt-native')
      await page
        .getByRole('button', { name: '预览已保存路由', exact: true })
        .click()
      await expect(
        page.getByText('已保存版本 3', { exact: true })
      ).toBeVisible()
      await expect(routePage).toContainText('Native channel')
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)
      await page.screenshot({
        path: testInfo.outputPath('manual-routing-preview.png'),
        fullPage: true,
      })
      await page
        .getByRole('combobox', { name: '传输方式', exact: true })
        .selectOption('websocket')
      await expect(
        page.getByRole('combobox', { name: 'API 路径', exact: true })
      ).toHaveValue('/v1/responses')
      const response = page.waitForResponse(
        (response) =>
          response.url().includes('/routing/preview') &&
          response.url().includes('transport=websocket')
      )
      await page
        .getByRole('button', { name: '预览已保存路由', exact: true })
        .click()
      await response
      await expect(routePage).toContainText(
        'gpt-native · /v1/responses · WebSocket'
      )
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)
      await page.screenshot({
        path: testInfo.outputPath('websocket-routing-preview.png'),
        fullPage: true,
      })
      const writes = fixture.writes.filter(
        (write) => write.path === '/api/token/7/routing'
      )
      expect(writes).toHaveLength(2)
      expect(writes[0]!.body).toMatchObject({ mode: 'auto', entries: [] })
      expect(writes[1]!.body).toMatchObject({
        mode: 'manual',
        max_channel_ratio: '1.25',
        entries: [{ channel_id: 2, enabled: true }],
      })
    })
  }
}

test('native routing load failure stays visible and can be retried', async ({
  page,
}) => {
  await configureStablePage(page, {
    theme: 'light',
    expectedApiErrors: { '/api/token/7/routing': [503] },
  })
  let failing = true
  await page.route('**/api/token/7/routing', (route) =>
    failing
      ? route.fulfill({
          status: 503,
          json: { success: false, message: 'Routing temporarily unavailable' },
        })
      : route.fallback()
  )
  await page.goto('/keys/7/routing')
  await expect(
    page.getByText('Routing temporarily unavailable', { exact: true })
  ).toBeVisible()
  failing = false
  await page
    .getByRole('button', { name: '重新加载', exact: true })
    .last()
    .click()
  await expect(
    page.getByRole('radio', { name: '手动路由', exact: true })
  ).toBeChecked()
  await assertNoHorizontalOverflow(page)
})
