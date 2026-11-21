import { expect, test } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  captureEvidence,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

// A release while the page stays open: the next existing status read (a
// public module navigation) reports another version, and the page offers a
// reload instead of running stale code against the new server.
for (const [theme, mobile] of [
  ['light', false],
  ['dark', true],
] as const) {
  test(`version change notice ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1440, height: 900 }
    )
    // The fixture spreads this object into every /api/status answer.
    const status: Record<string, unknown> = {}
    await configureStablePage(page, { theme, status })
    await page.goto('/pricing')
    await waitForStablePage(page)
    const notice = page.locator('[data-version-update]')
    await expect(notice).toHaveCount(0)

    status.version = 'native-test-2'
    await page.evaluate(() => {
      history.pushState({}, '', '/rankings')
      dispatchEvent(new PopStateEvent('popstate'))
    })
    await expect(notice).toBeVisible()
    await expect(notice).toHaveAttribute('role', 'status')
    await expect(notice).toContainText('站点已更新到新版本')
    const reload = notice.getByRole('button', { name: '刷新页面', exact: true })
    await expect(reload).toBeVisible()
    const box = await notice.boundingBox()
    const viewport = page.viewportSize()!
    expect(box!.x).toBeGreaterThanOrEqual(0)
    expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width)
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: testInfo.outputPath(`version-update-${theme}.png`),
    })

    await reload.click()
    await waitForStablePage(page)
    await expect(notice).toHaveCount(0)
  })
}
