import { expect, test } from '@playwright/test'
import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  captureEvidence,
} from './fixtures'

const nativeToken = {
  id: 7,
  name: 'Native Key',
  key: 'sk-****1234',
  type: 'ordered',
  selected_only: true,
  route_channel_count: 0,
  status: 1,
  remain_quota: 500000,
  used_quota: 250000,
  unlimited_quota: false,
  model_limits_enabled: false,
  model_limits: '',
  allow_ips: null,
  allow_user_quota_query: true,
  created_time: 1785124800,
  expired_time: -1,
}

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
      await expect(
        page.getByText('顺序 · 1 条渠道', { exact: true }).filter({
          visible: true,
        })
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      const create = page.getByRole('button', { name: '创建令牌', exact: true })
      await create.click()
      const dialog = page.getByRole('dialog', { name: '创建令牌', exact: true })
      await expect(dialog.getByRole('radio')).toHaveCount(0)
      await dialog
        .locator('input[name="token-name"]')
        .fill('Native routing key')
      await assertNoHorizontalOverflow(page)
      await captureEvidence(page, {
        path: testInfo.outputPath('create-key.png'),
      })
      await dialog.getByRole('button', { name: '确认', exact: true }).click()
      await expect(dialog).toHaveCount(0)
      await expect(create).toBeFocused()
      const created = fixture.writes.find(
        (write) =>
          write.path === '/api/token/' &&
          (write.body as { name?: string }).name === 'Native routing key'
      )
      expect(created?.body).not.toHaveProperty('type')
      const notice = page.locator('[data-key-created-notice]')
      await expect(notice).toContainText('新令牌默认使用自动路由和全平台渠道')
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)
      await captureEvidence(page, {
        path: testInfo.outputPath('keys-created.png'),
        fullPage: true,
      })

      await page.goto('/keys/7/routing')
      await waitForStablePage(page)
      const routePage = page.locator('[data-handdrawn-page="token-routing"]')
      await expect(routePage).toBeVisible()
      const orderedMode = page.getByRole('radio', {
        name: '顺序路由',
        exact: true,
      })
      const automatic = page.getByRole('radio', {
        name: '自动路由',
        exact: true,
      })
      const platform = page.getByRole('radio', {
        name: '全平台渠道',
        exact: true,
      })
      const selectedScope = page.getByRole('radio', {
        name: '指定渠道',
        exact: true,
      })
      const save = page.getByRole('button', { name: '保存', exact: true })
      await expect(orderedMode).toBeChecked()
      await expect(selectedScope).toBeChecked()
      await expect(platform).toBeDisabled()
      await expect(
        routePage.getByRole('button', { name: '上移', exact: true })
      ).toBeVisible()

      await automatic.focus()
      await page.keyboard.press('Space')
      await expect(automatic).toBeChecked()
      await expect(selectedScope).toBeChecked()
      await expect(platform).toBeEnabled()
      await expect(
        routePage.getByRole('button', { name: '上移', exact: true })
      ).toHaveCount(0)
      await save.click()
      await expect(
        page.getByText('路由已保存。', { exact: true })
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)
      await captureEvidence(page, {
        path: testInfo.outputPath('automatic-selected.png'),
        fullPage: true,
      })

      await platform.check()
      await expect(routePage).toContainText('此 Key 使用全平台渠道')
      await expect(routePage).toContainText(
        '以全平台渠道保存会清空已选渠道列表'
      )
      await save.click()
      await expect(save).toBeEnabled()
      await expect(routePage).not.toContainText('以全平台渠道保存会清空')
      await assertNoHorizontalOverflow(page)
      await captureEvidence(page, {
        path: testInfo.outputPath('automatic-platform.png'),
        fullPage: true,
      })

      await orderedMode.check()
      await expect(selectedScope).toBeChecked()
      await expect(platform).toBeDisabled()
      await save.click()
      await expect(
        page.getByRole('alert').filter({ hasText: '请至少添加一个渠道' })
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await captureEvidence(page, {
        path: testInfo.outputPath('ordered-empty-blocked.png'),
        fullPage: true,
      })
      await page
        .getByRole('combobox', { name: '选择平台渠道', exact: true })
        .selectOption('2')
      await page.getByRole('button', { name: '添加渠道', exact: true }).click()
      await page
        .getByRole('textbox', { name: '渠道倍率上限', exact: true })
        .fill('1.25')
      await save.click()
      await expect(save).toBeEnabled()
      await page
        .getByRole('textbox', { name: '模型', exact: true })
        .fill('gpt-native')
      await page
        .getByRole('button', { name: '预览已保存路由', exact: true })
        .click()
      await expect(
        page.getByText('已保存版本 4', { exact: true })
      ).toBeVisible()
      await expect(routePage).toContainText(
        '顺序路由按下方保存的顺序尝试符合条件的渠道。'
      )
      await expect(routePage).toContainText('Native channel')
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)
      await captureEvidence(page, {
        path: testInfo.outputPath('ordered-routing-preview.png'),
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

      const writes = fixture.writes.filter(
        (write) => write.path === '/api/token/7/routing'
      )
      expect(writes.map((write) => write.body)).toEqual([
        expect.objectContaining({
          mode: 'auto',
          selected_only: true,
          entries: [{ channel_id: 2, enabled: true }],
        }),
        expect.objectContaining({
          mode: 'auto',
          selected_only: false,
          entries: [],
        }),
        expect.objectContaining({
          mode: 'ordered',
          selected_only: true,
          max_channel_ratio: '1.25',
          entries: [{ channel_id: 2, enabled: true }],
        }),
      ])
    })

    test(`${theme} ${viewport} selected channels removed by an administrator`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        viewport === 'desktop'
          ? { width: 1440, height: 900 }
          : { width: 390, height: 844 }
      )
      await configureStablePage(page, { theme, authenticated: true })
      await page.route(/\/api\/token\/(?:\?.*)?$/, (route) =>
        route.request().method() === 'GET'
          ? route.fulfill({
              json: {
                success: true,
                data: {
                  page: 1,
                  page_size: 10,
                  total: 1,
                  items: [nativeToken],
                },
              },
            })
          : route.fallback()
      )
      await page.route('**/api/token/7/routing', (route) =>
        route.request().method() === 'GET'
          ? route.fulfill({
              json: {
                success: true,
                data: {
                  token_id: 7,
                  mode: 'ordered',
                  selected_only: true,
                  max_channel_ratio: null,
                  version: 3,
                  entries: [],
                },
              },
            })
          : route.fallback()
      )
      await page.goto('/keys')
      await waitForStablePage(page)
      const chip = page
        .locator('[data-route-warning]')
        .filter({ visible: true })
      await expect(chip).toHaveText(/所选渠道已不可用/)
      await expect(chip).toHaveAttribute('title', /没有可用渠道/)
      if (theme === 'dark') {
        // The warning keeps its status colour; only neutral type labels are muted.
        const colors = await chip.evaluate((element) => {
          const probe = document.createElement('span')
          probe.style.color = 'var(--status-warning-text)'
          document.body.append(probe)
          const expected = getComputedStyle(probe).color
          probe.remove()
          return { actual: getComputedStyle(element).color, expected }
        })
        expect(colors.actual).toBe(colors.expected)
      }
      await assertNoHorizontalOverflow(page)
      await captureEvidence(page, {
        path: testInfo.outputPath('keys-unavailable.png'),
        fullPage: true,
      })
      await page.goto('/keys/7/routing')
      await waitForStablePage(page)
      const alert = page.locator('[data-route-orphaned]')
      await expect(alert).toContainText('所选渠道已不可用')
      await expect(alert).toContainText('没有可用渠道')
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)
      await captureEvidence(page, {
        path: testInfo.outputPath('routing-unavailable.png'),
        fullPage: true,
      })
      await alert
        .getByRole('button', { name: '改回全平台渠道', exact: true })
        .click()
      await expect(
        page.getByRole('radio', { name: '自动路由', exact: true })
      ).toBeChecked()
      await expect(
        page.getByRole('radio', { name: '全平台渠道', exact: true })
      ).toBeChecked()
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
    page.getByRole('radio', { name: '顺序路由', exact: true })
  ).toBeChecked()
  await assertNoHorizontalOverflow(page)
})
