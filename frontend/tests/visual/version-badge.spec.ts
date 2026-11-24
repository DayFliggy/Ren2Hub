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

const badge = (page: Page) => page.locator('[data-version-badge]')
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

type Layout = 'expanded' | 'collapsed' | 'mobile'

async function collapseSidebar(page: Page) {
  // After the fixture's own init script, which expands the sidebar.
  await page.addInitScript(() =>
    window.localStorage.setItem('ren2hub_sidebar_collapsed', 'true')
  )
}

/** One badge, a sibling of the brand link: the sidebar from lg up, the top
 *  bar below. Its dot sits on the top-right corner of the site name, or of
 *  the logo when the name is hidden, and its target leaves the brand link
 *  clickable. */
async function checkPlacement(page: Page, layout: Layout) {
  await expect(badge(page)).toHaveCount(1)
  const brand =
    layout === 'mobile'
      ? '.topbar-brand [data-console-brand-link]'
      : 'aside.console-sidebar [data-console-brand-link]'
  const home = layout === 'mobile' ? '.topbar-brand' : 'aside.console-sidebar'
  await expect(page.locator(`${home} [data-version-badge]`)).toBeVisible()
  await expect(page.locator('a [data-version-badge]')).toHaveCount(0)

  // The top bar shows the name from 640px; the collapsed sidebar never does.
  const named = await page.locator(`${brand} span`).isVisible()
  const parts = named ? ['img', 'span'] : ['img']
  const corner = (await page.locator(`${brand} ${parts.at(-1)}`).boundingBox())!
  const dot = (await badge(page).locator('.version-badge-dot').boundingBox())!
  expect(
    Math.abs(dot.x + dot.width / 2 - (corner.x + corner.width))
  ).toBeLessThanOrEqual(8)
  expect(Math.abs(dot.y + dot.height / 2 - corner.y)).toBeLessThanOrEqual(8)
  if (layout !== 'mobile') {
    const aside = (await page.locator('aside.console-sidebar').boundingBox())!
    expect(dot.x + dot.width).toBeLessThanOrEqual(aside.x + aside.width)
    expect(dot.y).toBeGreaterThanOrEqual(aside.y)
  }

  // 44px, except beside the logo of the 64px collapsed sidebar, where a
  // 44px target would cover the logo link; 24px is the minimum there.
  const target = (await badge(page).boundingBox())!
  const size = layout === 'collapsed' ? 24 : 44
  expect(Math.min(target.width, target.height)).toBeGreaterThanOrEqual(size)
  for (const part of parts) {
    const box = (await page.locator(`${brand} ${part}`).boundingBox())!
    const hit = await page.evaluate(
      ({ x, y, selector }) =>
        document
          .querySelector(selector)!
          .contains(document.elementFromPoint(x, y)),
      { x: box.x + box.width / 2, y: box.y + box.height / 2, selector: brand }
    )
    expect(hit).toBe(true)
  }
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
    await checkPlacement(page, mobile ? 'mobile' : 'expanded')
    await expect(badge(page)).toHaveAttribute(
      'aria-label',
      '版本 v1.0.1，有新版本 v1.0.2'
    )
    await expect(badge(page)).toHaveAttribute('data-version-tone', 'warning')

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
    // The second step replaces the button inside the panel; no dialog opens.
    const step = panel(page).getByRole('group', { name: '更新到 v1.0.2？' })
    await expect(step).toContainText('完成后需要重启才会生效')
    await expect(page.locator('[aria-modal="true"]')).toHaveCount(0)
    await expect(
      step.getByRole('button', { name: '取消', exact: true })
    ).toBeFocused()
    await checkPanel(page, mobile)
    await captureEvidence(page, {
      path: testInfo.outputPath(`update-confirm-${theme}-${viewport}.png`),
    })
    await step.getByRole('button', { name: '确认更新', exact: true }).click()
    await expect(panel(page)).toHaveAttribute(
      'data-version-state',
      'restart-needed'
    )
    await expect(
      panel(page).locator('[data-version-restart-needed]')
    ).toContainText('程序已替换，重启后生效。')
    await expect(badge(page)).toHaveAttribute('data-version-tone', 'warning')
    await expect(badge(page)).toHaveAttribute(
      'aria-label',
      /^版本 .+，已替换，重启后生效$/
    )
    await checkPanel(page, mobile)
    await captureEvidence(page, {
      path: testInfo.outputPath(`update-restart-${theme}-${viewport}.png`),
    })

    // The restart offered after the update is confirmed in the panel too.
    await panel(page).locator('[data-version-restart]').click()
    const restartStep = panel(page).getByRole('group', { name: '重启服务器？' })
    await expect(restartStep).toContainText('进行中的请求会被中断')
    await expect(page.locator('[aria-modal="true"]')).toHaveCount(0)
    await expect(
      restartStep.getByRole('button', { name: '取消', exact: true })
    ).toBeFocused()
    await checkPanel(page, mobile)
    await captureEvidence(page, {
      path: testInfo.outputPath(
        `update-restart-confirm-${theme}-${viewport}.png`
      ),
    })
    await restartStep.getByRole('button', { name: '取消', exact: true }).click()
    await expect(panel(page).locator('[data-version-restart]')).toBeFocused()
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
    await expect(badge(page)).toHaveAttribute('data-version-tone', 'neutral')
    await expect(badge(page)).toHaveAttribute(
      'aria-label',
      '版本 v1.0.1，已是最新'
    )
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

/** The dot in each tone and layout, shot as the brand area it sits in. */
const DOT_STATES = {
  current: { tone: 'neutral', label: '版本 v1.0.1，已是最新' },
  update: { tone: 'warning', label: '版本 v1.0.1，有新版本 v1.0.2' },
  error: { tone: 'danger', label: /^版本 .+，无法检查更新$/ },
} as const
for (const theme of ['light', 'dark'] as const) {
  for (const state of Object.keys(DOT_STATES) as (keyof typeof DOT_STATES)[]) {
    for (const layout of ['expanded', 'collapsed', 'mobile'] as const) {
      test(`version dot ${state} ${theme} ${layout}`, async ({
        page,
      }, testInfo) => {
        const mobile = layout === 'mobile'
        await page.setViewportSize(
          mobile ? { width: 390, height: 844 } : { width: 1440, height: 900 }
        )
        await configureStablePage(page, {
          theme,
          expectedApiErrors:
            state === 'error'
              ? { '/api/admin/system/check-updates': [502] }
              : undefined,
        })
        if (layout === 'collapsed') await collapseSidebar(page)
        if (state === 'error') {
          await page.route('**/api/admin/system/check-updates?*', (route) =>
            route.fulfill({
              status: 502,
              json: { success: false, message: 'no release' },
            })
          )
        } else {
          await mockRelease(
            page,
            state === 'current'
              ? { latest_version: 'v1.0.1', has_update: false }
              : {}
          )
        }
        await gotoDashboard(page)
        const expected = DOT_STATES[state]
        await expect(badge(page)).toHaveAttribute(
          'data-version-tone',
          expected.tone
        )
        await expect(badge(page)).toHaveAttribute('aria-label', expected.label)
        await checkPlacement(page, layout)
        await assertNoHorizontalOverflow(page)
        await captureEvidence(page, {
          path: testInfo.outputPath(
            `update-dot-${state}-${theme}-${layout}.png`
          ),
          clip: { x: 0, y: 0, width: mobile ? 390 : 320, height: 96 },
        })
      })
    }
  }
}

test('the dot moves from the logo to the name when the sidebar expands', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'light' })
  await collapseSidebar(page)
  await mockRelease(page)
  await gotoDashboard(page)
  await checkPlacement(page, 'collapsed')
  await page.locator('aside .console-sidebar__collapse').click()
  await checkPlacement(page, 'expanded')
  await badge(page).click()
  await expect(panel(page)).toHaveAttribute('data-version-state', 'update')
  await checkPanel(page, false)
})

test('one badge keeps its state across the lg breakpoint', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'dark' })
  await mockRelease(page)
  let checks = 0
  page.on('request', (request) => {
    if (request.url().includes('/api/admin/system/check-updates')) checks++
  })
  await gotoDashboard(page)
  await badge(page).click()
  await panel(page).locator('[data-version-update-now]').click()
  await panel(page)
    .getByRole('group', { name: '更新到 v1.0.2？' })
    .getByRole('button', { name: '确认更新', exact: true })
    .click()
  await expect(panel(page)).toHaveAttribute(
    'data-version-state',
    'restart-needed'
  )
  await page.keyboard.press('Escape')
  await expect(panel(page)).toHaveCount(0)
  const before = checks
  await page.setViewportSize({ width: 390, height: 844 })
  await checkPlacement(page, 'mobile')
  await badge(page).click()
  // The same instance moved: the pending restart is still there and the
  // move started no second check.
  await expect(panel(page)).toHaveAttribute(
    'data-version-state',
    'restart-needed'
  )
  expect(checks).toBe(before)
  await checkPanel(page, true)
})

/** The open panel hangs 4px under the badge, lined up with its left edge
 *  unless the viewport margin pushes it in. */
async function checkAnchored(page: Page) {
  await expect
    .poll(async () => {
      const trigger = (await badge(page).boundingBox())!
      const box = (await panel(page).boundingBox())!
      const width = page.viewportSize()!.width
      const left = Math.max(8, Math.min(trigger.x, width - box.width - 8))
      return [
        Math.round(box.y - (trigger.y + trigger.height)),
        Math.round(box.x - left),
      ]
    })
    .toEqual([4, 0])
}

test('an open panel follows the badge across the lg breakpoint', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'light' })
  // Up to date, so the panel keeps its size across the breakpoint and no
  // resize of its own places it again; only the badge moves.
  await mockRelease(page, { latest_version: 'v1.0.1', has_update: false })
  let checks = 0
  page.on('request', (request) => {
    if (request.url().includes('/api/admin/system/check-updates')) checks++
  })
  await gotoDashboard(page)
  await badge(page).click()
  await expect(panel(page)).toHaveAttribute('data-version-state', 'current')
  await checkAnchored(page)
  const before = checks
  for (const viewport of [
    { width: 1000, height: 900 },
    { width: 1100, height: 900 },
    { width: 390, height: 844 },
    { width: 1440, height: 900 },
  ]) {
    await page.setViewportSize(viewport)
    await checkAnchored(page)
    await expect(panel(page)).toHaveAttribute('data-version-state', 'current')
    await checkPlacement(page, viewport.width < 1024 ? 'mobile' : 'expanded')
  }
  expect(checks).toBe(before)
})

test('version badge source build', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await configureStablePage(page, { theme: 'dark' })
  await mockRelease(page, { build_type: 'source', current_version: 'dev' })
  const api = recordApi(page)
  await gotoDashboard(page)
  await expect(badge(page)).toHaveAttribute(
    'aria-label',
    '版本 dev，有新版本 v1.0.2'
  )
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
