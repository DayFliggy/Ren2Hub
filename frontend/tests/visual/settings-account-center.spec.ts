import { expect, test } from '@playwright/test'

import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage,
  type VisualTheme,
  waitForStablePage,
} from './fixtures'

const scenarios = [
  { name: 'wide', width: 1680, height: 1000 },
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'mobile', width: 390, height: 844 },
] as const

for (const theme of ['light', 'dark'] as VisualTheme[]) {
  for (const viewport of scenarios) {
    test(`${theme} ${viewport.name} account settings`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(viewport)
      await configureStablePage(page, { theme, authenticated: true })

      // /settings keeps preferences only; credentials live on /security.
      await page.goto('/settings', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)
      const securityEntry = page.locator('[data-security-entry]')
      await expect(securityEntry).toBeVisible()
      await expect(
        page.getByRole('heading', { name: '通知与偏好', exact: true })
      ).toBeVisible()
      await expect(
        page.getByRole('heading', { name: '登录与账户', exact: true })
      ).toHaveCount(0)
      await expect(page.getByText('演示功能', { exact: true })).toHaveCount(0)
      await expect(
        page.getByText('演示状态仅保存在当前浏览器会话', { exact: true })
      ).toHaveCount(0)
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)

      const [entryBox, preferencesBox] = await Promise.all([
        securityEntry.boundingBox(),
        page
          .locator('section[data-surface-variant]')
          .filter({
            has: page.getByRole('heading', {
              name: '通知与偏好',
              exact: true,
            }),
          })
          .boundingBox(),
      ])
      expect(entryBox).not.toBeNull()
      expect(preferencesBox).not.toBeNull()
      expect(preferencesBox!.y).toBeGreaterThanOrEqual(
        entryBox!.y + entryBox!.height
      )

      await securityEntry
        .getByRole('button', { name: '管理', exact: true })
        .click()
      await expect(page).toHaveURL(/\/security$/)
      await waitForStablePage(page)
      await expect(
        page.getByRole('heading', { name: '安全与访问', exact: true })
      ).toBeVisible()
      await expect(
        page.getByRole('heading', { name: '登录与账户', exact: true })
      ).toBeVisible()
      await expect(
        page.getByRole('heading', { name: '登录验证', exact: true })
      ).toBeVisible()
      await expect(page.getByRole('switch')).toHaveCount(0)
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)

      if (process.env.PLAYWRIGHT_CAPTURE_SCREENSHOTS === 'true') {
        await page.screenshot({
          path: testInfo.outputPath('security-access-top.png'),
          fullPage: true,
        })
      }

      const scrollZone = page.locator('.scroll-zone')
      await scrollZone.evaluate((element) =>
        element.scrollTo({ top: element.scrollHeight, behavior: 'instant' })
      )
      await page.waitForTimeout(100)
      await assertInteractiveCentersVisible(page)

      await page.goto('/profile', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)
      await expect(
        page.getByRole('heading', { name: 'Native Root', exact: true })
      ).toBeVisible()
      await expect(
        page
          .getByRole('navigation', { name: '面包屑导航' })
          .getByText('会员中心', { exact: true })
      ).toBeVisible()
      const profileHero = page.locator('[data-title-side="right"]')
      await expect(
        profileHero.getByRole('heading', { name: '会员中心', exact: true })
      ).toBeVisible()
      if (viewport.width < 768) {
        const titleBox = await profileHero.getByRole('heading').boundingBox()
        const subtitleBox = await profileHero.locator('p').boundingBox()
        expect(titleBox).not.toBeNull()
        expect(subtitleBox).not.toBeNull()
        expect(subtitleBox!.y).toBeGreaterThanOrEqual(
          titleBox!.y + titleBox!.height
        )
      }
      const userMenuTrigger = page.getByRole('button', {
        name: '个人中心',
        exact: true,
      })
      await expect(userMenuTrigger).toHaveAttribute('aria-expanded', 'false')
      await userMenuTrigger.click()
      await expect(userMenuTrigger).toHaveAttribute('aria-expanded', 'true')
      // Balance, member center, security & access, sign out.
      await expect(page.locator('[data-user-menu-item]')).toHaveCount(4)
      await page.keyboard.press('Escape')
      await expect(userMenuTrigger).toHaveAttribute('aria-expanded', 'false')
      await expect(page.locator('[data-user-menu-item]')).toHaveCount(0)
      await expect(
        page.getByRole('button', { name: '个人中心', exact: true })
      ).toBeVisible()
      await expect(
        page.getByRole('heading', { name: '账号设置', exact: true })
      ).toHaveCount(1)
      // Native account data exposes quota/usage/request counts; the retired
      // aggregate join-date/member-duration projection is not synthesized.
      await expect(
        page.getByTestId('profile-stat-balance').locator('strong')
      ).toHaveText('$10.00')
      await expect(
        page.getByTestId('profile-stat-usage').locator('strong')
      ).toHaveText('$2.00')
      await expect(
        page.getByTestId('profile-stat-requests').locator('strong')
      ).toHaveText('4')
      await expect(page.getByTestId('profile-join-date')).toHaveText('暂不可用')
      await expect(page.getByTestId('profile-member-duration')).toHaveText(
        '暂不可用'
      )
      await expect(page.locator('[data-security-entry]')).toBeVisible()
      await expect(
        page.getByRole('heading', { name: '通知与偏好', exact: true })
      ).toBeVisible()
      await expect(page.getByText('快速导航', { exact: true })).toHaveCount(0)
      await expect(page.getByText('个人资料', { exact: true })).toHaveCount(0)
      await expect(page.getByText('账户安全', { exact: true })).toHaveCount(0)
      await expect(page.getByText('演示功能', { exact: true })).toHaveCount(0)
      await expect(
        page.getByText('演示状态仅保存在当前浏览器会话', { exact: true })
      ).toHaveCount(0)
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)

      await page
        .getByRole('heading', { name: '通知与偏好', exact: true })
        .scrollIntoViewIfNeeded()
      await page.waitForTimeout(100)
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)
    })
  }
}
