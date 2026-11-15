import { mkdirSync } from 'node:fs'

import { expect, test, type Page } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

/** Optional screenshot directory for the cross-audit. */
const shotsDir = process.env.REN2HUB_LEGAL_SHOTS
const viewports = {
  desktop: { width: 1440, height: 900 },
  mobile: { width: 390, height: 844 },
}
const legalStatus = {
  user_agreement_enabled: true,
  privacy_policy_enabled: true,
  github_oauth: true,
}

async function openAuthPage(
  page: Page,
  path: '/sign-in' | '/sign-up',
  theme: VisualTheme,
  viewport: { width: number; height: number }
) {
  await page.setViewportSize(viewport)
  await configureStablePage(page, {
    theme,
    authenticated: false,
    status: legalStatus,
  })
  await page.goto(path)
  await waitForStablePage(page)
}

// Theme does not change the consent geometry; each viewport keeps both auth pages.
for (const [screen, theme] of [
  ['desktop', 'light'],
  ['mobile', 'dark'],
] as const) {
  const viewport = viewports[screen]
  for (const path of ['/sign-in', '/sign-up'] as const) {
    test(`${screen} ${theme} ${path} requires consent before every method`, async ({
      page,
    }) => {
      await openAuthPage(page, path, theme, viewport)
      const consent = page.locator('[data-legal-consent]')
      await expect(consent).toHaveCount(1)
      await expect(consent).toBeVisible()
      await expect(
        consent.getByRole('link', { name: '用户协议' })
      ).toHaveAttribute('href', '/user-agreement')
      await expect(
        consent.getByRole('link', { name: '隐私政策' })
      ).toHaveAttribute('target', '_blank')
      const box = (await consent.boundingBox())!
      const card = (await page.locator('.auth-card').boundingBox())!
      expect(box.x).toBeGreaterThanOrEqual(card.x)
      expect(box.x + box.width).toBeLessThanOrEqual(card.x + card.width)
      // The whole label is the hit area for the checkbox.
      expect(box.height).toBeGreaterThanOrEqual(44)

      const github = page.getByRole('button', { name: /GitHub/ })
      await expect(github).toBeDisabled()
      const consentId = await consent.getAttribute('id')
      expect(consentId).toBeTruthy()
      await expect(github).toHaveAttribute('aria-describedby', consentId!)
      // A link inside the label opens the document without toggling consent.
      const popup = page.context().waitForEvent('page')
      await consent.getByRole('link', { name: '隐私政策' }).click()
      await (await popup).close()
      await expect(consent.locator('input[type="checkbox"]')).not.toBeChecked()
      await assertNoHorizontalOverflow(page)
      if (shotsDir) {
        mkdirSync(shotsDir, { recursive: true })
        await consent.scrollIntoViewIfNeeded()
        await page.screenshot({
          path: `${shotsDir}/${screen}-${theme}-${path.slice(1)}-unchecked.png`,
        })
      }

      await consent.locator('input[type="checkbox"]').check()
      await expect(github).toBeEnabled()
      await expect(github).not.toHaveAttribute('aria-describedby', /.+/)
      if (shotsDir && screen === 'mobile')
        await page.screenshot({
          path: `${shotsDir}/${screen}-${theme}-${path.slice(1)}-checked.png`,
        })
    })
  }
}

test('no consent row is shown when no legal document is published', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light', authenticated: false })
  await page.goto('/sign-in')
  await waitForStablePage(page)
  await expect(page.locator('[data-legal-consent]')).toHaveCount(0)
})
