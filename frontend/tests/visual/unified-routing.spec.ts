import { expect, test } from '@playwright/test'
import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  captureEvidence,
} from './fixtures'

const orphanedToken = {
  id: 7,
  name: 'Orphaned Key',
  key: 'sk-****1234',
  type: 'ordered',
  selected_only: true,
  route_channel_count: 0,
  enabled_route_channel_count: 0,
  route_scope: 'empty',
  effective_mode: 'auto',
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
    test(`${theme} ${viewport} key creation and standalone routing modal`, async ({
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

      // 1. Create modal has no channel picker; it creates a default auto-platform key in one POST.
      const create = page.getByRole('button', { name: '创建令牌', exact: true })
      await create.click()
      const createDialog = page.getByRole('dialog', {
        name: '创建令牌',
        exact: true,
      })
      await expect(
        createDialog.locator('[data-routing-candidate-picker]')
      ).toHaveCount(0)
      await createDialog
        .locator('input[name="token-name"]')
        .fill('Native routing key')
      await assertNoHorizontalOverflow(page)
      await captureEvidence(page, {
        path: testInfo.outputPath('create-key-clean.png'),
      })

      await createDialog
        .getByRole('button', { name: '确认', exact: true })
        .click()
      await expect(createDialog).toHaveCount(0)
      await waitForStablePage(page)
      await expect(create).toBeFocused()
      expect(
        fixture.writes.some(
          (write) =>
            write.path === '/api/token/' &&
            (write.body as { name?: string }).name === 'Native routing key'
        )
      ).toBe(true)
      expect(
        fixture.writes.some((write) => write.path === '/api/token/99/routing')
      ).toBe(false)
      await expect(page.locator('[data-key-created-notice]')).toHaveCount(0)
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)

      // 2. Edit modal owns routing mode + multiplier cap (no channel picker inside).
      await page
        .getByRole('button', { name: '编辑令牌', exact: true })
        .filter({ visible: true })
        .first()
        .click()
      const editDialog = page.getByRole('dialog', {
        name: '编辑令牌',
        exact: true,
      })
      await expect(editDialog).toBeVisible()
      await expect(editDialog.locator('[data-key-form-routing]')).toBeVisible()
      await expect(
        editDialog.locator('[data-routing-candidate-picker]')
      ).toHaveCount(0)
      await expect(
        editDialog.getByRole('radio', { name: /顺序路由/ })
      ).toHaveAttribute('aria-checked', 'true')
      await editDialog.getByRole('radio', { name: /自动路由/ }).click()
      await editDialog
        .locator('input[name="token-max-channel-ratio"]')
        .fill('2')
      await assertNoHorizontalOverflow(page)
      await captureEvidence(page, {
        path: testInfo.outputPath('edit-key-routing-policy.png'),
      })
      await editDialog
        .getByRole('button', { name: '确认', exact: true })
        .click()
      await expect(editDialog).toHaveCount(0)
      const editRoutingWrite = fixture.writes
        .filter((write) => write.path === '/api/token/7/routing')
        .at(-1)
      expect(editRoutingWrite?.body).toMatchObject({
        mode: 'auto',
        selected_only: true,
        max_channel_ratio: '2',
        entries: [{ channel_id: 2, enabled: true }],
      })

      // 3. Dedicated routing action button opens the standalone KeyRoutingModal directly.
      await page.locator('[data-key-routing-action]:visible').first().click()
      const routingDialog = page.getByRole('dialog', {
        name: '路由 · Native Key',
        exact: true,
      })
      await expect(routingDialog).toBeVisible()
      await expect(routingDialog.getByRole('radio')).toHaveCount(0)
      await expect(
        routingDialog.locator('input[name="token-max-channel-ratio"]')
      ).toHaveCount(0)
      await expect(
        routingDialog.locator('[data-routing-policy-summary]')
      ).toContainText('自动路由')
      await expect(
        routingDialog.locator('li[data-selected-channel-id="2"]')
      ).toBeVisible()
      await expect(
        routingDialog.locator('[data-routing-candidate-picker]')
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await captureEvidence(page, {
        path: testInfo.outputPath('routing-modal-open.png'),
      })

      // Pause the sole selected channel -> shows automatic all-platform fallback notice and saves cleanly.
      await routingDialog
        .locator('li[data-selected-channel-id="2"] [role="switch"]')
        .click()
      await expect(
        routingDialog.locator('[data-routing-all-paused]')
      ).toContainText('自动退回全平台路由')
      await captureEvidence(page, {
        path: testInfo.outputPath('routing-modal-all-paused.png'),
      })
      await routingDialog
        .getByRole('button', { name: '保存', exact: true })
        .click()
      await expect(routingDialog).toHaveCount(0)

      const lastRoutingWrite = fixture.writes
        .filter((write) => write.path === '/api/token/7/routing')
        .at(-1)
      expect(lastRoutingWrite?.body).toMatchObject({
        mode: 'auto',
        selected_only: true,
        max_channel_ratio: '2',
        entries: [{ channel_id: 2, enabled: false }],
      })
    })
  }
}

for (const theme of ['light', 'dark'] as const) {
  test(`${theme} orphaned selection warning in standalone routing modal protects against silent fallback`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await configureStablePage(page, {
      theme,
      authenticated: true,
    })
    let savedRoutingBody: Record<string, unknown> | null = null
    await page.route('**/api/token/?*', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            items: [orphanedToken],
            total: 1,
            page: 1,
            page_size: 10,
          },
        },
      })
    )
    await page.route('**/api/token/7/routing', (route) => {
      if (route.request().method() === 'GET') {
        return route.fulfill({
          json: {
            success: true,
            data: {
              token_id: 7,
              mode: 'ordered',
              selected_only: true,
              version: 3,
              max_channel_ratio: null,
              entries: [],
              route_scope: 'empty',
              effective_mode: 'auto',
              enabled_route_channel_count: 0,
            },
          },
        })
      }
      savedRoutingBody = route.request().postDataJSON()
      return route.fulfill({
        json: {
          success: true,
          data: {
            token_id: 7,
            mode: 'auto',
            selected_only: false,
            version: 4,
            max_channel_ratio: null,
            entries: [],
            route_scope: 'platform',
            effective_mode: 'auto',
            enabled_route_channel_count: 0,
          },
        },
      })
    })

    await page.goto('/keys')
    await waitForStablePage(page)
    await expect(
      page.locator('[data-route-warning]:visible').first()
    ).toContainText('自选渠道已失效')

    await page.locator('[data-key-route-trigger]:visible').first().click()
    const dialog = page.getByRole('dialog', {
      name: '路由 · Orphaned Key',
      exact: true,
    })
    await expect(dialog.locator('[data-route-orphaned]')).toBeVisible()
    await captureEvidence(page, {
      path: testInfo.outputPath(`orphaned-routing-modal-${theme}.png`),
    })

    // Explicitly restore all platform channels and save.
    await dialog
      .getByRole('button', { name: '恢复为全平台路由', exact: true })
      .click()
    await expect(dialog.locator('[data-route-orphaned]')).toHaveCount(0)
    await dialog.getByRole('button', { name: '保存', exact: true }).click()
    await expect(dialog).toHaveCount(0)
    expect(savedRoutingBody).toMatchObject({
      mode: 'auto',
      selected_only: false,
      entries: [],
      version: 3,
    })
  })
}
