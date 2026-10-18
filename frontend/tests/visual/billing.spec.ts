import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`wallet fund flow and failure ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      const failures: string[] = []
      page.on('pageerror', (error) => failures.push(error.message))
      await page.goto('/wallet')
      // 资金流水列表与重复 quota 卡片已从钱包页面移除，验证其不再渲染
      const history = page
        .locator('section')
        .filter({ has: page.getByRole('heading', { name: '资金流水' }) })
      await expect(history).toHaveCount(0)
      await expect(page.getByRole('tab')).toHaveCount(0)
      await expect(page.getByText('在途预留')).toHaveCount(0)
      // 验证充值记录等核心区块正常显示
      await expect(
        page.getByRole('heading', { name: '充值记录' })
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('wallet-fund-flow.png'),
        fullPage: true,
      })
      expect(failures).toEqual([])
    })
  }
}
