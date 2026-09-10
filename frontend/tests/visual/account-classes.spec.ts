import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

for (const language of ['zh-CN', 'en']) {
  for (const theme of ['light', 'dark'] as const) {
    for (const mobile of [false, true]) {
      test(`account class settings and user limits ${language} ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
        page,
      }, testInfo) => {
        await page.setViewportSize(
          mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
        )
        await configureStablePage(page, { theme })
        await page.addInitScript(
          (locale) => localStorage.setItem('ren2hub_locale', locale),
          language
        )
        const failures: string[] = []
        page.on('pageerror', (error) => failures.push(error.message))
        await page.goto('/system-settings/billing/pricing')
        const classes = page.locator('[data-account-classes]')
        await expect(classes).toBeVisible()
        await classes
          .getByRole('button', {
            name: language === 'en' ? 'Edit account class' : '编辑账号分类',
          })
          .first()
          .click()
        await expect(
          page.locator('input[name="account-class-code"]')
        ).toHaveAttribute('readonly', '')
        await page.locator('input[name="account-class-concurrency"]').fill('0')
        await assertNoHorizontalOverflow(page)
        await page.screenshot({
          path: testInfo.outputPath('account-class-editor.png'),
          fullPage: true,
        })
        await page
          .getByRole('button', {
            name: language === 'en' ? 'Cancel' : '取消',
            exact: true,
          })
          .click()

        await page.goto('/users')
        await expect(
          page
            .getByText('visual.member', { exact: true })
            .filter({ visible: true })
        ).toBeVisible()
        await page
          .getByRole('button', {
            name: language === 'en' ? 'Edit user' : '编辑用户',
            exact: true,
          })
          .filter({ visible: true })
          .click()
        const concurrency = page.locator('input[name="admin-user-concurrency"]')
        await expect(concurrency).toHaveValue('0')
        await expect(page.locator('input[name="admin-user-rpm"]')).toHaveValue(
          ''
        )
        await concurrency.fill('')
        const policy = page.getByText(
          language === 'en' ? 'Policy version: 7' : '政策版本：7',
          { exact: true }
        )
        await policy.scrollIntoViewIfNeeded()
        const policyBox = await policy.boundingBox()
        const footerBox = await page
          .getByRole('dialog')
          .locator('footer')
          .boundingBox()
        expect(policyBox!.y + policyBox!.height).toBeLessThanOrEqual(
          footerBox!.y + 1
        )
        await assertNoHorizontalOverflow(page)
        await page.screenshot({
          path: testInfo.outputPath('user-limits-editor.png'),
          fullPage: true,
        })
        expect(failures).toEqual([])
        await page.keyboard.press('Escape')
        await expect(page.getByRole('dialog')).toHaveCount(0)
      })
    }
  }
}
