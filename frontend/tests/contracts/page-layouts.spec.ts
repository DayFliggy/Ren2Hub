import { expect, test, type Locator, type Page } from '@playwright/test'
import { nativeTopupInfo } from '../../src/api/__tests__/topupFixtures'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import { captureUI } from './ui-coverage'

const scenarios = [
  {
    name: 'desktop-light-zh-CN',
    viewport: { width: 1440, height: 900 },
    theme: 'light',
    locale: 'zh-CN',
    text: {
      topup: '快捷充值',
      redeem: '兑换码充值',
      loading: '加载中…',
      failed: '易支付方式加载失败。',
      retry: '重试',
      policy: '请求策略',
      save: '保存请求策略',
      sections: ['请求限流', '敏感内容', 'SSRF 防护', 'Token 限制'],
      records: '签到记录',
    },
  },
  {
    name: 'mobile-dark-en',
    viewport: { width: 390, height: 844 },
    theme: 'dark',
    locale: 'en',
    text: {
      topup: 'Quick top-up',
      redeem: 'Redeem code',
      loading: 'Loading…',
      failed: 'Epay payment methods could not be loaded.',
      retry: 'Retry',
      policy: 'Request policy',
      save: 'Save request policy',
      sections: [
        'Request rate limits',
        'Sensitive content',
        'SSRF protection',
        'Token limits',
      ],
      records: 'Check-in records',
    },
  },
] as const

function card(page: Page, title: string) {
  return page
    .locator('section')
    .filter({
      has: page.getByRole('heading', { name: title, exact: true }),
    })
    .last()
}

async function assertColumns(topup: Locator, columns: number) {
  const grid = topup.locator('..')
  const measured = await grid.evaluate((element) => ({
    columns: getComputedStyle(element).gridTemplateColumns.split(' ').length,
    width: element.getBoundingClientRect().width,
  }))
  expect(measured.columns).toBe(columns)
  if (columns === 1) {
    expect(
      Math.abs((await topup.boundingBox())!.width - measured.width)
    ).toBeLessThanOrEqual(1)
  }
}

for (const scenario of scenarios) {
  const { name, theme, locale, viewport, text } = scenario

  for (const enableRedemption of [false, true]) {
    test(`wallet capability ${enableRedemption ? 'enabled' : 'disabled'} uses available width ${name}`, async ({
      page,
    }, info) => {
      await page.setViewportSize(viewport)
      await configureStablePage(page, { theme, locale })
      await page.route('**/api/user/topup/info', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: { ...nativeTopupInfo, enable_redemption: enableRedemption },
          },
        })
      )
      await page.goto('/wallet')
      const topup = card(page, text.topup)
      await expect(topup.locator('..')).toHaveAttribute('aria-busy', 'false')
      await expect(
        page.getByRole('heading', { name: text.redeem, exact: true })
      ).toHaveCount(enableRedemption ? 1 : 0)
      await waitForStablePage(page)
      await assertColumns(
        topup,
        enableRedemption && viewport.width >= 1280 ? 2 : 1
      )
      if (enableRedemption && viewport.width >= 1280) {
        const redeem = await card(page, text.redeem).boundingBox()
        const payment = await topup.boundingBox()
        expect(Math.abs(redeem!.y - payment!.y)).toBeLessThanOrEqual(1)
        expect(redeem!.x).toBeGreaterThan(payment!.x + payment!.width)
      }
      await assertNoHorizontalOverflow(page)
      await captureUI(
        page,
        info,
        enableRedemption
          ? 'wallet-redemption-enabled'
          : 'wallet-redemption-disabled'
      )
    })
  }

  test(`wallet delayed and failed capabilities remain one column and recover ${name}`, async ({
    page,
  }, info) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, { theme, locale })
    let release!: () => void
    const pending = new Promise<void>((resolve) => {
      release = resolve
    })
    let attempts = 0
    await page.route('**/api/user/topup/info', async (route) => {
      attempts++
      if (attempts === 1) {
        await pending
        return route.fulfill({
          json: { success: false, message: 'Isolated capability failure' },
        })
      }
      return route.fulfill({
        json: {
          success: true,
          data: { ...nativeTopupInfo, enable_redemption: false },
        },
      })
    })
    await page.goto('/wallet')
    const topup = card(page, text.topup)
    await expect(topup.getByRole('status')).toHaveText(text.loading)
    await expect(topup.getByRole('alert')).toHaveCount(0)
    await expect(topup.locator('..')).toHaveAttribute('aria-busy', 'true')
    await assertColumns(topup, 1)
    await captureUI(page, info, 'wallet-capability-loading')
    release()
    const error = topup.getByRole('alert').filter({ hasText: text.failed })
    await expect(error).toBeVisible()
    await expect(topup.getByRole('alert')).toHaveCount(1)
    await expect(topup.locator('..')).toHaveAttribute('aria-busy', 'false')
    await expect(topup.getByRole('status')).toHaveCount(0)
    await assertColumns(topup, 1)
    await captureUI(page, info, 'wallet-capability-failed')
    await error.getByRole('button', { name: text.retry, exact: true }).click()
    await expect(error).toHaveCount(0)
    await expect.poll(() => attempts).toBe(2)
    await expect(topup.locator('..')).toHaveAttribute('aria-busy', 'false')
    await expect(
      page.getByRole('heading', { name: text.redeem, exact: true })
    ).toHaveCount(0)
    await waitForStablePage(page)
    await assertColumns(topup, 1)
    await assertNoHorizontalOverflow(page)
    await captureUI(page, info, 'wallet-capability-recovered')
  })

  test(`request policy belongs to rate limits and protects navigation drafts ${name}`, async ({
    page,
  }, info) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, { theme, locale })
    let policy = {
      RetryTimes: '2',
      CheckSensitiveEnabled: 'false',
      SensitiveWords: 'fixture',
    }
    const patches: Record<string, string>[] = []
    await page.route('**/api/option/', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: Object.entries(policy).map(([key, value]) => ({ key, value })),
        },
      })
    )
    await page.route('**/api/option/request_policy', async (route) => {
      if (route.request().method() === 'PATCH') {
        const patch = route.request().postDataJSON().options as Record<
          string,
          string
        >
        patches.push(patch)
        policy = { ...policy, ...patch }
      }
      return route.fulfill({
        json: { success: true, data: { options: policy } },
      })
    })
    await page.goto('/system-settings/security/rate-limit')
    const editor = page.getByRole('textbox', { name: text.policy, exact: true })
    await expect(editor).toHaveValue(JSON.stringify(policy, null, 2))
    await editor.focus()
    await expect(editor).toBeFocused()
    expect(
      await editor.evaluate((el) => {
        const style = getComputedStyle(el)
        return style.outlineStyle !== 'none' || style.boxShadow !== 'none'
      })
    ).toBe(true)
    await waitForStablePage(page)
    await captureUI(page, info, 'policy-rate-limit')
    const edited = JSON.stringify({ ...policy, RetryTimes: '4' }, null, 2)
    await editor.fill(edited)
    page.once('dialog', (dialog) => dialog.dismiss())
    await page
      .locator('.settings-section-nav')
      .getByRole('button', { name: text.sections[1], exact: true })
      .click()
    await expect(page).toHaveURL(/\/security\/rate-limit$/)
    await expect(editor).toHaveValue(edited)
    await captureUI(page, info, 'policy-draft-kept')
    page.once('dialog', (dialog) => dialog.accept())
    await page
      .locator('.settings-section-nav')
      .getByRole('button', { name: text.sections[1], exact: true })
      .click()
    for (const [sectionIndex, section] of text.sections.slice(1).entries()) {
      await page
        .locator('.settings-section-nav')
        .getByRole('button', { name: section, exact: true })
        .click()
      await expect(editor).toHaveCount(0)
      await waitForStablePage(page)
      await captureUI(page, info, `policy-absent-${sectionIndex + 1}`)
    }
    await page
      .locator('.settings-section-nav')
      .getByRole('button', { name: text.sections[0], exact: true })
      .click()
    await expect(editor).toHaveValue(JSON.stringify(policy, null, 2))
    await editor.fill(edited)
    await page.getByRole('button', { name: text.save, exact: true }).click()
    await expect.poll(() => patches).toEqual([{ RetryTimes: '4' }])
    await expect(editor).toHaveValue(JSON.stringify(policy, null, 2))
    await assertNoHorizontalOverflow(page)
    await captureUI(page, info, 'policy-saved')
  })

  test(`check-in details preserve ledger layout and accessible monthly records ${name}`, async ({
    page,
  }, info) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, { theme, locale })
    await page.route('**/api/user/checkin?*', (route) => {
      const month = new URL(route.request().url()).searchParams.get('month')!
      return route.fulfill({
        json: {
          success: true,
          data: {
            enabled: true,
            min_quota: 500000,
            max_quota: 2500000,
            stats: {
              total_quota: 6500000,
              total_checkins: 3,
              checkin_count: 3,
              checked_in_today: true,
              records: [1, 15, 26].map((day) => ({
                checkin_date: `${month}-${String(day).padStart(2, '0')}`,
                quota_awarded: day === 1 ? 500000 : 3000000,
              })),
            },
          },
        },
      })
    })
    await page.goto('/activity')
    await expect(page.locator('[data-checkin-record]')).toHaveCount(3)
    const rewardRange = page
      .locator('[data-checkin-card] p')
      .filter({ hasText: '$1.00' })
      .filter({ hasText: '$5.00' })
    for (const amount of ['$1.00', '$5.00']) {
      const lines = await rewardRange.evaluate((element, value) => {
        const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT)
        let node: Node | null
        while ((node = walker.nextNode())) {
          const start = node.textContent!.indexOf(value)
          if (start < 0) continue
          const range = document.createRange()
          range.setStart(node, start)
          range.setEnd(node, start + value.length)
          return range.getClientRects().length
        }
        return 0
      }, amount)
      expect(
        lines,
        `${amount} must not split inside a normal monetary value`
      ).toBe(1)
    }
    const details = page.locator('[data-checkin-card] details')
    await details.locator('summary').click()
    const table = details.getByRole('table', {
      name: `${text.records} · 2026-07`,
    })
    await expect(table).toBeVisible()
    await expect(table.getByRole('columnheader')).toHaveCount(2)
    await expect(table.getByRole('rowheader')).toHaveCount(3)
    await expect(table.getByRole('cell')).toHaveText([
      '$1.00',
      '$6.00',
      '$6.00',
    ])
    const numeric = await table
      .getByRole('cell')
      .first()
      .evaluate((el) => ({
        textAlign: getComputedStyle(el).textAlign,
        padding: parseFloat(getComputedStyle(el).paddingRight),
      }))
    expect(numeric.textAlign).toBe('right')
    expect(numeric.padding).toBeGreaterThanOrEqual(8)
    const tableBox = await table.boundingBox()
    const detailsBox = await details.boundingBox()
    expect(tableBox!.width).toBeLessThanOrEqual(detailsBox!.width + 1)
    await waitForStablePage(page)
    await assertNoHorizontalOverflow(page)
    await captureUI(page, info, 'checkin-records-expanded')
    await details.locator('summary').focus()
    await page.keyboard.press('Enter')
    await expect(table).toBeHidden()
    await page.keyboard.press('Enter')
    await expect(table).toBeVisible()
  })
}
