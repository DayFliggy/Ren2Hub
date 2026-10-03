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
        page.getByRole('heading', { name: '会员中心', exact: true })
      ).toBeVisible()
      await expect(
        page
          .getByRole('navigation', { name: '面包屑导航' })
          .getByText('会员中心', { exact: true })
      ).toBeVisible()
      await expect(page.locator('[data-title-side="right"]')).toHaveCount(1)
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
      ).toBeVisible()
      await expect(page.getByTestId('profile-join-date')).toHaveText(
        '2026-06-20'
      )
      await expect(page.getByTestId('profile-member-duration')).toHaveText(
        '37 天'
      )
      await expect(page.getByTestId('profile-total-calls')).toHaveText('32.1K')
      await expect(page.getByTestId('profile-account-status')).toContainText(
        '账户状态 · 正常'
      )
      await expect(page.getByTestId('profile-stat-requests')).toContainText(
        '32.1K'
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
