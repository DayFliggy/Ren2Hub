import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

const recovery = {
  request_id: 'request-uncertain-001',
  user_id: 1,
  token_id: 2,
  state: 'active',
  funding_amount: '250000',
  token_amount: '0',
  version: 3,
  created_at: 1785114000,
  updated_at: 1785114020,
  last_error: 'upstream_outcome_unknown',
  manual_review_required: true,
}
const event = {
  event_id: 'reserve:request-uncertain-001',
  request_id: recovery.request_id,
  operation: 'reserve',
  actor_id: 1,
  available_delta: '-250000',
  reserved_delta: '250000',
  used_delta: '0',
  token_available_delta: '-250000',
  token_reserved_delta: '250000',
  token_used_delta: '0',
  available_after: '9007199254740993',
  reserved_after: '250000',
  used_after: '100000',
  reason: 'Initial reservation',
  created_at: 1785114000,
}
const detail = {
  recovery,
  audit: {
    status: 'unconfirmed',
    attempts: [],
    replayed_charge: null,
    quoted_total_usd: null,
    customer_charge_usd: '0',
    customer_charge_quota: '0',
    customer_charge_confirmed: false,
    charge_difference_usd: null,
    charge_difference_quota: null,
    upstream_cost_usd: null,
    profit_usd: null,
    cost_status: 'unknown',
  },
  upstream_cost: {
    attempt_count: 1,
    confirmed_attempt_count: 0,
    complete: false,
    upstream_cost_usd: null,
    records: [],
  },
  attempts: [
    {
      id: 'attempt-channel-2',
      channel_id: 2,
      quote_hash: 'attempt-quote-hash',
      created_at: 1785114010,
      quote: {
        channel_ratio: '1.123456789',
        price_version: {
          id: 3,
          prices: [{ quantity: 'p', price: '0.123456789' }],
        },
      },
    },
  ],
  quote: {
    price_version: { id: 3, mode: 'unit_price' },
    channel_ratio: '1',
    frozen_at: '2026-07-27T04:00:00Z',
  },
  usage: {
    input_tokens: 123,
    output_tokens: 20,
    complete: false,
    estimated: true,
  },
  charge: {
    base_usd: '0.0013992',
    adjustments: [],
    total_usd: '0.0013992',
    calculation_version: 'billing-v3',
    rounding_version: 'half-up-v1',
    final_quota: '700',
    unrounded_quota: '699.6',
    rounding_delta: '0.4',
    lines: [
      {
        quantity: 'p',
        count: '123',
        unit: 'per_million_tokens',
        unit_price: '1',
        subtotal_usd: '0.000123',
      },
    ],
  },
  events: [event],
}

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`billing reconciliation ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      await page.route('**/api/billing/metrics', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              price_versions: 1,
              quote_attempts: 1,
              prices_by_origin: [],
              pending: 1,
              reconciliations: 1,
              reserved_quota: '250000',
              oldest_wait_seconds: 20,
              idempotency_conflicts: 0,
              recovery_failures: 1,
              debt_events: 0,
            },
          },
        })
      )
      await page.route('**/api/billing/reconciliations?*', (route) =>
        route.fulfill({
          json: { success: true, data: { items: [recovery], total: 1 } },
        })
      )
      await page.route(
        `**/api/billing/reconciliations/${recovery.request_id}`,
        (route) => route.fulfill({ json: { success: true, data: detail } })
      )
      const operations: unknown[] = []
      await page.route(
        `**/api/billing/reconciliations/${recovery.request_id}/resolve`,
        (route) => {
          operations.push(route.request().postDataJSON())
          return route.fulfill({
            status: 409,
            json: { success: false, message: 'version conflict' },
          })
        }
      )
      const failures: string[] = []
      page.on('pageerror', (error) => failures.push(error.message))
      await page.goto('/billing/reconciliations')
      await expect(
        page.getByText(recovery.request_id, { exact: true })
      ).toBeVisible()
      await expect(
        page.getByText('$0.500', { exact: true }).first()
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.getByRole('button', { name: '账务详情', exact: true }).click()
      const modal = page.getByRole('dialog')
      await expect(modal.getByText('费用分项与舍入')).toBeVisible()
      await modal.getByText('各次尝试报价', { exact: true }).click()
      const attemptEvidence = modal
        .locator('details')
        .filter({ hasText: '各次尝试报价' })
        .locator('pre')
      await expect(attemptEvidence).toContainText('attempt-channel-2')
      await expect(attemptEvidence).toContainText('attempt-quote-hash')
      await expect(attemptEvidence).toContainText('"channel_ratio": "1.123"')
      await expect(attemptEvidence).toContainText('"price": "0.123"')
      await expect(attemptEvidence).not.toContainText('1.123456789')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('attempt-evidence.png'),
        fullPage: true,
      })
      await modal.getByLabel('实际费用（quota 整数）').fill('700')
      await modal.getByLabel('处理理由').fill('已核对上游用量')
      await modal.getByRole('button', { name: '提交人工决定' }).click()
      await expect(modal.getByRole('alert')).toContainText('冲突')
      await modal.getByRole('button', { name: '提交人工决定' }).click()
      await expect.poll(() => operations.length).toBe(2)
      expect(operations[1]).toEqual(operations[0])
      expect(operations[0]).toMatchObject({
        expected_version: 3,
        action: 'settle',
        quota: '700',
        reason: '已核对上游用量',
      })
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('billing-detail.png'),
        fullPage: true,
      })
      expect(failures).toEqual([])
    })

    test(`wallet reservations and failure ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      await page.route('**/api/billing/self/reservations?*', (route) =>
        route.fulfill({
          json: { success: true, data: { items: [recovery], total: 1 } },
        })
      )
      await page.route('**/api/billing/self/events?*', (route) =>
        route.fulfill({
          status: 503,
          json: { success: false, message: 'unavailable' },
        })
      )
      const failures: string[] = []
      page.on('pageerror', (error) => failures.push(error.message))
      await page.goto('/wallet')
      await expect(
        page.getByText(recovery.request_id, { exact: true })
      ).toBeVisible()
      await expect(page.getByText('$0.500', { exact: true })).toBeVisible()
      await page.getByRole('tab', { name: '资金流水' }).click()
      await expect(page.getByRole('alert')).toContainText('账务数据暂不可用')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('wallet-error.png'),
        fullPage: true,
      })
      expect(failures).toEqual([])
    })
  }
}
