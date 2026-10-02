import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

const diagnostic = {
  schema_version: 1,
  request_id: 'diagnostic-p2-request',
  token_id: 7,
  trace_id: 'trace-local-only',
  thread_id: 'thread-local-only',
  model_name: 'local-model',
  created_at: 1785123000,
  wait_ms: 13,
  user_queue_ms: 4,
  attempts: [
    {
      sequence: 1,
      attempt_id: 'attempt-first-failed',
      success: false,
      error_class: 'rate_limit',
      error_http_status: 429,
      origin: 'upstream',
      latency_ms: 100,
      queue_ms: 9,
      send_started_at: 1785123000000,
      finished_at: 1785123000100,
      acceptance: 'rejected',
      retry_permission: 'allowed',
      response_committed: false,
    },
    {
      sequence: 2,
      attempt_id: 'attempt-second-success',
      success: true,
      origin: 'upstream',
      latency_ms: 120,
      upstream_latency_ms: 100,
      queue_ms: 0,
      send_started_at: 1785123000120,
      first_output_at: 1785123000200,
      ttft_ms: 80,
      finished_at: 1785123000240,
      acceptance: 'accepted',
      retry_permission: 'denied',
      response_committed: true,
    },
  ],
}

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`request diagnostics ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme, authenticated: true })
      const errors: string[] = []
      let currentDiagnostic = diagnostic
      page.on('pageerror', (error) => errors.push(error.message))
      await page.route('**/api/log/self?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              items: [
                {
                  id: 711,
                  request_id: diagnostic.request_id,
                  type: 2,
                  token_name: 'local token',
                  model_name: 'local-model',
                  channel_name: '',
                  prompt_tokens: 10,
                  completion_tokens: 5,
                  quota: '0',
                  use_time: 253,
                  is_stream: true,
                  content: 'Local simulation',
                  created_at: 1785123000,
                  other: {},
                },
              ],
              total: 1,
              page: 1,
              page_size: 10,
            },
          },
        })
      )
      await page.route('**/api/log/self/requests/*/diagnostics', (route) =>
        route.fulfill({ json: { success: true, data: currentDiagnostic } })
      )
      await page.route('**/api/log/self/request-diagnostics?*', (route) => {
        const query = new URL(route.request().url()).searchParams
        expect(query.get('token_id')).toBe('7')
        expect(query.get('trace_id')).toBe('trace-local-only')
        return route.fulfill({
          json: {
            success: true,
            data: { items: [diagnostic], total: 1, page: 1, page_size: 10 },
          },
        })
      })
      await page.route('**/api/log/request-diagnostics?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              items: [
                {
                  ...diagnostic,
                  attempts: diagnostic.attempts.map((attempt) => ({
                    ...attempt,
                    channel_id: 12,
                    credential_id: 'credential-stable-admin-only',
                  })),
                },
              ],
              total: 1,
              page: 1,
              page_size: 10,
            },
          },
        })
      )
      await page.goto('/logs')
      await page
        .getByRole('button', { name: '查看日志详情', exact: true })
        .click()
      const detail = page.getByRole('dialog')
      await expect(detail.locator('[data-diagnostic-attempt]')).toHaveCount(2)
      await expect(
        detail.locator('[data-diagnostic-attempt]').first()
      ).toContainText('失败')
      await expect(
        detail.locator('[data-diagnostic-attempt]').last()
      ).toContainText('成功')
      await expect(
        detail.locator('[data-upstream-duration]').first()
      ).toHaveText('未知')
      await expect(
        detail.locator('[data-upstream-duration]').last()
      ).toHaveText('100 ms')
      await expect(detail).toContainText('总尝试耗时')
      await expect(detail).toContainText('明细默认保留 7 天')
      await expect(detail).not.toContainText('credential-stable-admin-only')
      await assertNoHorizontalOverflow(page)
      await detail.getByRole('button', { name: '同 Trace 请求' }).click()
      const browser = page.getByRole('dialog')
      await expect(browser.getByLabel('Token ID')).toHaveValue('7')
      await expect(browser.getByLabel('Trace ID')).toHaveValue(
        'trace-local-only'
      )
      await browser.getByRole('button', { name: '查看时间线' }).click()
      await expect(browser.locator('[data-diagnostic-attempt]')).toHaveCount(2)
      await expect(browser).not.toContainText('credential-stable-admin-only')
      await browser.getByLabel('查询范围').selectOption('all')
      await browser.getByRole('button', { name: '查看时间线' }).click()
      await expect(browser).toContainText('credential-stable-admin-only')
      await assertNoHorizontalOverflow(page)
      await page.keyboard.press('Escape')
      await expect(page.getByRole('dialog')).toHaveCount(0)
      currentDiagnostic = { ...diagnostic, token_id: 0 }
      await page
        .getByRole('button', { name: '查看日志详情', exact: true })
        .click()
      const transientDetail = page.getByRole('dialog')
      await expect(transientDetail).toContainText('无持久 Token')
      await expect(transientDetail).toContainText('仅支持按请求 ID 查看诊断')
      await expect(transientDetail).toContainText(diagnostic.request_id)
      await expect(transientDetail).toContainText('等待预算消耗')
      await expect(
        transientDetail.getByRole('button', { name: '同 Trace 请求' })
      ).toHaveCount(0)
      await expect(
        transientDetail.getByRole('button', { name: '同 Thread 请求' })
      ).toHaveCount(0)
      await assertNoHorizontalOverflow(page)
      await page.keyboard.press('Escape')
      await expect(page.getByRole('dialog')).toHaveCount(0)
      expect(errors).toEqual([])
    })
  }
}
