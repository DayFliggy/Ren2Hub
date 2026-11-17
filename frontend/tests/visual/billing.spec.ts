import { expect, test } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  captureEvidence,
} from './fixtures'

for (const [theme, mobile] of [
  ['light', false],
  ['dark', true],
] as const) {
  test(`wallet native balance and records ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
    )
    await configureStablePage(page, { theme })
    const failures: string[] = []
    page.on('pageerror', (error) => failures.push(error.message))
    await page.goto('/wallet')
    const card = (name: string) =>
      page
        .getByRole('heading', { name, exact: true })
        .locator('xpath=ancestor::section[1]')
    const topup = card('快捷充值')
    const methods = card('支付方式')
    const redeem = card('兑换码充值')
    const records = card('充值记录')
    await expect(methods.getByRole('button', { name: 'Alipay' })).toBeVisible()
    await expect(redeem).toBeVisible()
    await expect(records).toBeVisible()
    // 统计卡和消耗图已从钱包页移除
    await expect(page.getByText('本月消耗')).toHaveCount(0)
    await expect(card('每日消耗')).toHaveCount(0)
    await expect(card('资金流水')).toHaveCount(0)
    await expect(page.getByRole('tab')).toHaveCount(0)
    await expect(page.getByText('在途预留')).toHaveCount(0)
    const [topupBox, methodsBox, redeemBox, recordsBox] = await Promise.all(
      [topup, methods, redeem, records].map(
        async (item) => (await item.boundingBox())!
      )
    )
    if (mobile) {
      // 单列时先选支付方式，再填金额
      expect(methodsBox.y + methodsBox.height).toBeLessThanOrEqual(topupBox.y)
    } else {
      // 桌面左侧充值、右侧支付方式，两卡顶部和高度对齐
      expect(methodsBox.x).toBeGreaterThanOrEqual(topupBox.x + topupBox.width)
      expect(Math.abs(methodsBox.y - topupBox.y)).toBeLessThan(1)
      expect(Math.abs(methodsBox.height - topupBox.height)).toBeLessThan(1)
    }
    // 兑换码在支付板块下方、充值记录上方
    const paymentBottom = Math.max(
      topupBox.y + topupBox.height,
      methodsBox.y + methodsBox.height
    )
    expect(redeemBox.y).toBeGreaterThanOrEqual(paymentBottom)
    expect(recordsBox.y).toBeGreaterThanOrEqual(redeemBox.y + redeemBox.height)
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: testInfo.outputPath('wallet-fund-flow.png'),
      fullPage: true,
    })
    expect(failures).toEqual([])
  })
}
