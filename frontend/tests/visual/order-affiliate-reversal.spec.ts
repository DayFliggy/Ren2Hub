import { expect, test } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  captureEvidence,
  configureStablePage,
} from './fixtures'

for (const [theme, mobile] of [
  ['light', false],
  ['dark', true],
] as const) {
  test(`order referral reward reversal ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
    )
    await configureStablePage(page, { theme })
    const order = {
      id: 1,
      trade_no: 'EPvisual',
      user_id: 1,
      username: 'visual',
      email: '',
      user_deleted: false,
      amount: 10,
      money: 73,
      pay_amount: '73.00',
      expected_quota: 5000000,
      credited_quota: 5000000,
      bonus_quota: 0,
      affiliate_base_quota: 5000000,
      currency: 'CNY',
      payment_provider: 'epay',
      payment_method: 'alipay',
      status: 'success',
      create_time: 1785114000,
      complete_time: 1785114100,
    }
    await page.route(/\/api\/user\/topup(?:\?.*)?$/, (route) =>
      route.fulfill({
        json: {
          success: true,
          data: { items: [order], total: 1, page: 1, page_size: 20 },
        },
      })
    )
    await page.route('**/api/user/topup/admin/detail/EPvisual', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            ...order,
            affiliate: {
              inviter_id: 12,
              reward_quota: 1000000,
              reversed: false,
            },
          },
        },
      })
    )
    const bodies: unknown[] = []
    await page.route('**/api/user/topup/admin/affiliate-reversal', (route) => {
      bodies.push(route.request().postDataJSON())
      return route.fulfill({
        json: {
          success: true,
          data: {
            record: { id: 9, kind: 'reversal' },
            replayed: false,
            reversed_quota: 1000000,
            revoked_quota: 750000,
            debt_added_quota: 250000,
          },
        },
      })
    })
    await page.goto('/orders')
    await page.getByRole('tab', { name: '订单列表', exact: true }).click()
    await page
      .getByRole('button', { name: '查看订单', exact: true })
      .filter({ visible: true })
      .click()
    const dialog = page.getByRole('dialog', { name: '订单详情' })
    const section = dialog.locator('[data-order-affiliate]')
    await expect(section).toContainText('邀请人 #12 获得 $2.00')
    const reverse = section.getByRole('button', {
      name: '回收返利',
      exact: true,
    })
    await section.scrollIntoViewIfNeeded()
    await assertNoHorizontalOverflow(page)
    const [dialogBox, sectionBox, reverseBox] = await Promise.all([
      dialog.boundingBox(),
      section.boundingBox(),
      reverse.boundingBox(),
    ])
    expect(sectionBox!.x).toBeGreaterThanOrEqual(dialogBox!.x)
    expect(sectionBox!.x + sectionBox!.width).toBeLessThanOrEqual(
      dialogBox!.x + dialogBox!.width + 0.5
    )
    expect(reverseBox!.x + reverseBox!.width).toBeLessThanOrEqual(
      sectionBox!.x + sectionBox!.width + 0.5
    )
    await captureEvidence(page, {
      path: testInfo.outputPath('order-affiliate.png'),
    })

    await reverse.click()
    const confirmation = page.getByRole('dialog', {
      name: '回收返利',
      exact: true,
    })
    await expect(confirmation).toContainText('回收后不能撤销')
    expect(bodies).toEqual([])
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: testInfo.outputPath('order-affiliate-confirm.png'),
    })
    await confirmation
      .getByRole('button', { name: '确认', exact: true })
      .click()
    await expect(confirmation).toHaveCount(0)
    await expect(section).toContainText('已回收')
    await expect(section.locator('[data-order-affiliate-result]')).toHaveText(
      '收回未转出返利 $1.50，新增待抵扣 $0.50'
    )
    await expect(reverse).toHaveCount(0)
    expect(bodies).toEqual([{ trade_no: 'EPvisual' }])
    await captureEvidence(page, {
      path: testInfo.outputPath('order-affiliate-reversed.png'),
    })
  })
}
