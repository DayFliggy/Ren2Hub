import { expect, test, type Page } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  captureEvidence,
  configureStablePage,
} from './fixtures'

const ok = (data: unknown) => ({ json: { success: true, data } })
const RELEASE_URL = 'https://github.com/DayFliggy/Ren2Hub/releases/tag/v1.0.2'
const COMMAND =
  'sudo -n python3 -B scripts/server/ren2hub-app.py rollback --version v1.0.0'

const badge = (page: Page) => page.locator('.topbar-nav [data-version-badge]')
const panel = (page: Page) => page.locator('[data-version-panel]')

async function mockRelease(
  page: Page,
  overrides: Record<string, unknown> = {}
) {
  await page.route('**/api/admin/system/check-updates?*', (route) =>
    route.fulfill(
      ok({
        current_version: 'v1.0.1',
        latest_version: 'v1.0.2',
        has_update: true,
        build_type: 'release',
        cached: false,
        release_info: {
          name: 'Ren2Hub v1.0.2',
          body: '## 修复\n\n- 路由在高并发下的**排队**问题\n- 日志导出改为流式下载，详见 [文档](https://example.com/docs)\n\n```\nren2hub-app.py upgrade --version v1.0.2\n```',
          published_at: '2026-11-20T08:00:00Z',
          html_url: RELEASE_URL,
        },
        ...overrides,
      })
    )
  )
  await page.route('**/api/admin/system/rollback-versions', (route) =>
    route.fulfill(
      ok({
        versions: [
          {
            version: '1.0.0',
            published_at: '2026-11-01T00:00:00Z',
            html_url: RELEASE_URL.replace('1.0.2', '1.0.0'),
          },
          {
            version: '0.9.9',
            published_at: '2026-10-12T00:00:00Z',
            html_url: RELEASE_URL.replace('1.0.2', '0.9.9'),
          },
        ],
      })
    )
  )
  await page.route('**/api/admin/system/update', (route) =>
    route.fulfill(ok({ need_restart: true }))
  )
}

function recordApi(page: Page) {
  const paths = new Set<string>()
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (path.startsWith('/api/')) paths.add(path)
  })
  return paths
}

async function gotoDashboard(page: Page) {
  await page.goto('/dashboard')
  await expect(page.locator('.topbar-nav')).toBeVisible({ timeout: 30_000 })
}

/** The panel must stay inside the viewport, scroll nothing sideways, and on
 *  a phone offer only touch-sized controls. Prose links in the release notes
 *  keep the text line height. */
async function checkPanel(page: Page, mobile: boolean) {
  const box = (await panel(page).boundingBox())!
  const width = page.viewportSize()!.width
  expect(box.x).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(width)
  expect(
    await panel(page).evaluate((node) => node.scrollWidth - node.clientWidth)
  ).toBeLessThanOrEqual(1)
  await assertNoHorizontalOverflow(page)
  if (!mobile) return
  const small = await panel(page)
    .locator('button:visible, a:visible:not([data-version-notes] a)')
    .evaluateAll((nodes) =>
      nodes
        .filter((node) => node.getBoundingClientRect().height < 44)
        .map((node) => node.textContent?.trim())
    )
  expect(small).toEqual([])
  const trigger = (await badge(page).boundingBox())!
  expect(trigger.height).toBeGreaterThanOrEqual(44)
}

for (const [theme, mobile] of [
  ['light', false],
  ['dark', false],
  ['light', true],
  ['dark', true],
] as const) {
  const viewport = mobile ? 'mobile' : 'desktop'
  test(`version badge release ${theme} ${viewport}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1440, height: 900 }
    )
    await configureStablePage(page, { theme })
    await mockRelease(page)
    await gotoDashboard(page)
    await expect(badge(page)).toHaveAttribute(
      'aria-label',
      '版本 v1.0.1，有新版本'
    )
    await expect(badge(page).locator('[data-version-badge-dot]')).toBeVisible()

    await badge(page).click()
    await expect(panel(page)).toHaveAttribute('data-version-state', 'update')
    await expect(panel(page).locator('[data-version-current]')).toHaveText(
      'v1.0.1'
    )
    await expect(panel(page).locator('[data-version-latest]')).toHaveText(
      '最新：v1.0.2'
    )
    await expect(
      panel(page).locator('[data-version-release-link]')
    ).toHaveAttribute('href', RELEASE_URL)
    await panel(page).locator('[data-version-notes-toggle]').click()
    await expect(panel(page).locator('[data-version-notes] h3')).toHaveText(
      '修复'
    )
    await checkPanel(page, mobile)
    await captureEvidence(page, {
      path: testInfo.outputPath(`update-available-${theme}-${viewport}.png`),
    })

    await panel(page).locator('[data-version-update-now]').click()
    await page
      .getByRole('dialog', { name: '更新到 v1.0.2？' })
      .getByRole('button', { name: '更新', exact: true })
      .click()
    await expect(panel(page)).toHaveAttribute(
      'data-version-state',
      'restart-needed'
    )
    await expect(
      panel(page).locator('[data-version-restart-needed]')
    ).toContainText('程序已替换，重启后生效。')
    await expect(badge(page).locator('[data-version-badge-dot]')).toHaveCount(0)
    await checkPanel(page, mobile)
    await captureEvidence(page, {
      path: testInfo.outputPath(`update-restart-${theme}-${viewport}.png`),
    })
  })

  test(`version badge rollback ${theme} ${viewport}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1440, height: 900 }
    )
    await configureStablePage(page, { theme })
    await mockRelease(page, {
      latest_version: 'v1.0.1',
      has_update: false,
      release_info: {
        name: 'Ren2Hub v1.0.1',
        body: '',
        published_at: '2026-11-10T08:00:00Z',
        html_url: RELEASE_URL.replace('1.0.2', '1.0.1'),
      },
    })
    const api = recordApi(page)
    await gotoDashboard(page)
    await expect(badge(page).locator('[data-version-badge-dot]')).toHaveCount(0)
    await badge(page).click()
    await expect(panel(page)).toHaveAttribute('data-version-state', 'current')
    await expect(panel(page)).toContainText('已是最新')
    expect(api.has('/api/admin/system/rollback-versions')).toBe(false)
    await panel(page).locator('[data-version-rollback-toggle]').click()
    await panel(page)
      .getByRole('radio', { name: /v1\.0\.0/ })
      .click()
    await expect(
      panel(page).locator('[data-version-rollback-command]')
    ).toHaveText(COMMAND)
    await expect(panel(page)).toContainText('应用内回滚不备份数据库')
    await expect(
      panel(page).locator('[data-version-rollback-confirm]')
    ).toHaveText('回滚到 v1.0.0')
    await checkPanel(page, mobile)
    await captureEvidence(page, {
      path: testInfo.outputPath(`update-rollback-${theme}-${viewport}.png`),
    })
  })
}

test('version badge source build', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await configureStablePage(page, { theme: 'dark' })
  await mockRelease(page, { build_type: 'source', current_version: 'dev' })
  const api = recordApi(page)
  await gotoDashboard(page)
  await expect(badge(page)).toContainText('dev')
  await badge(page).click()
  await expect(panel(page)).toHaveAttribute(
    'data-version-state',
    'source-update'
  )
  await expect(panel(page)).toContainText('源码构建不支持后台更新。')
  await expect(panel(page).locator('[data-version-update-now]')).toHaveCount(0)
  await checkPanel(page, true)
  expect(api.has('/api/admin/system/rollback-versions')).toBe(false)
  await captureEvidence(page, {
    path: testInfo.outputPath('update-source-dark-mobile.png'),
  })
})

test('version badge check failure offers retry', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, {
    theme: 'light',
    expectedApiErrors: { '/api/admin/system/check-updates': [502] },
  })
  await page.route('**/api/admin/system/check-updates?*', (route) =>
    route.fulfill({
      status: 502,
      json: { success: false, message: 'no release' },
    })
  )
  await gotoDashboard(page)
  await badge(page).click()
  await expect(panel(page)).toHaveAttribute('data-version-state', 'check-error')
  await expect(panel(page)).toContainText('无法从 GitHub 获取版本信息。')
  await expect(panel(page).locator('[data-version-retry]')).toHaveText('重试')
  await checkPanel(page, false)
  await captureEvidence(page, {
    path: testInfo.outputPath('update-error-light-desktop.png'),
  })
})

for (const [role, theme, mobile] of [
  [1, 'light', false],
  [10, 'dark', true],
] as const) {
  const viewport = mobile ? 'mobile' : 'desktop'
  test(`non-Root role ${role} sees no version badge (${theme} ${viewport})`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1440, height: 900 }
    )
    await configureStablePage(page, { theme, user: { role } })
    const api = recordApi(page)
    await gotoDashboard(page)
    await expect(page.locator('[data-user-menu-trigger]')).toBeVisible()
    await expect(page.locator('[data-version-badge]')).toHaveCount(0)
    expect(
      [...api].filter((path) => path.startsWith('/api/admin/system/'))
    ).toEqual([])
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: testInfo.outputPath(`update-nonroot-${theme}-${viewport}.png`),
    })
  })
}

test('english badge renders without raw keys', async ({ page }) => {
  await configureStablePage(page, { theme: 'light', locale: 'en' })
  await mockRelease(page)
  await gotoDashboard(page)
  await badge(page).click()
  await expect(panel(page)).toContainText('Version and updates')
  await expect(panel(page)).toContainText('Latest: v1.0.2')
  await expect(panel(page)).toContainText('Update now')
  expect(await panel(page).innerText()).not.toMatch(
    /\b(?:errors|latestLine|updateNow|badge)\b/
  )
})
