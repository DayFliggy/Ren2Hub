import { expect, test, type Page } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import {
  performanceFixture,
  qualityFixture,
  routingFixture,
} from '../../src/api/__tests__/opsFixtures'

const viewports = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'mobile', width: 390, height: 844 },
] as const
const DOMAINS = [
  'site',
  'auth',
  'billing',
  'models',
  'security',
  'content',
  'operations',
] as const

for (const theme of ['light', 'dark'] as const) {
  for (const viewport of viewports) {
    test(`system settings domains render ${theme} ${viewport.name}`, async ({
      page,
    }, testInfo) => {
      // Seven full page loads exceed the default budget on a loaded runner.
      test.setTimeout(180_000)
      await page.setViewportSize(viewport)
      const fixture = await configureStablePage(page, { theme })
      for (const domain of DOMAINS) {
        await page.goto(`/system-settings/${domain}`)
        await waitForStablePage(page)
        await expect(page.locator('nav.settings-section-nav')).toBeVisible()
        await expect(page.getByRole('alert')).toHaveCount(0)
        await assertNoHorizontalOverflow(page)
      }
      await page.screenshot({
        path: testInfo.outputPath('system-settings-operations.png'),
        fullPage: true,
      })
      expect(fixture.errors).toEqual([])
      expect(fixture.missingMessages).toEqual([])
    })
  }
}

for (const [theme, viewport] of [
  ['light', viewports[0]],
  ['dark', viewports[1]],
] as const) {
  test(`request policy saves only the edited key ${theme} ${viewport.name}`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport)
    const fixture = await configureStablePage(page, { theme })
    await page.goto('/system-settings/security')
    await waitForStablePage(page)
    const policy = page.getByRole('textbox', { name: '请求策略', exact: true })
    await expect(policy).toHaveValue(/"RetryTimes": "2"/)
    const save = page.getByRole('button', { name: '保存请求策略', exact: true })
    await expect(save).toBeDisabled()

    await policy.fill(
      (await policy.inputValue()).replace(
        '"RetryTimes": "2"',
        '"RetryTimes": "3"'
      )
    )
    const optionsReload = page.waitForRequest(
      (request) =>
        new URL(request.url()).pathname === '/api/option/' &&
        request.method() === 'GET'
    )
    await save.click()
    await optionsReload

    const patches = fixture.writes.filter(
      (write) => write.path === '/api/option/request_policy'
    )
    expect(patches).toEqual([
      {
        path: '/api/option/request_policy',
        body: { options: { RetryTimes: '3' } },
      },
    ])
    await expect(policy).toHaveValue(/"RetryTimes": "3"/)
    await expect(policy).toHaveValue(/"CheckSensitiveEnabled": "false"/)
    await expect(save).toBeDisabled()
    await assertNoHorizontalOverflow(page)
    expect(fixture.errors).toEqual([])
  })
}

// Go stores monitor_*_threshold as a signed int; -1 and 0 disable the check.
async function mockOpsMonitor(page: Page) {
  const stats = performanceFixture()
  stats.config.monitor_cpu_threshold = -1
  stats.config.monitor_memory_threshold = 150
  stats.config.monitor_disk_threshold = 0
  stats.system_usage = {
    cpu_percent: 99,
    memory_percent: 40,
    sampled_at: Math.floor(Date.parse('2026-07-27T11:59:50+08:00') / 1000),
  }
  const bodies: Record<string, unknown> = {
    '/api/performance/stats': stats,
    '/api/performance/quality': qualityFixture(),
    '/api/performance/routing': routingFixture(),
  }
  await page.route(
    /\/api\/performance\/(stats|quality|routing)(\?|$)/,
    (route) =>
      route.fulfill({
        json: {
          success: true,
          message: '',
          data: bodies[new URL(route.request().url()).pathname],
        },
      })
  )
}

for (const theme of ['light', 'dark'] as const) {
  for (const viewport of viewports) {
    test(`ops header treats non-positive thresholds as disabled ${theme} ${viewport.name}`, async ({
      page,
    }) => {
      await page.setViewportSize(viewport)
      const fixture = await configureStablePage(page, { theme })
      await mockOpsMonitor(page)
      await page.goto('/ops')
      await waitForStablePage(page)
      await expect(page.locator('[data-ops-section="status"]')).toBeVisible()
      await expect(page.locator('[data-ops-error]')).toHaveCount(0)
      await expect(page.locator('[data-ops-gauge="cpu"]')).toContainText(
        '阈值关闭'
      )
      await expect(page.locator('[data-ops-gauge="disk"]')).toContainText(
        '阈值关闭'
      )
      await expect(page.locator('[data-ops-gauge="memory"]')).toContainText(
        '阈值 150%'
      )
      // CPU reads 99% but its check is off, so it is unmeasured, not critical.
      await expect(page.locator('[data-ops-reason]')).toHaveCount(0)
      await expect(page.locator('[data-ops-unmeasured]')).toContainText('CPU')
      await expect(page.locator('[data-ops-unmeasured]')).toContainText('磁盘')
      await assertNoHorizontalOverflow(page)
      expect(fixture.errors).toEqual([])
      expect(fixture.missingMessages).toEqual([])
    })
  }
}

for (const [theme, viewport] of [
  ['light', viewports[0]],
  ['dark', viewports[1]],
] as const) {
  test(`hidden modules leave navigation but keep admin routes ${theme} ${viewport.name}`, async ({
    page,
  }) => {
    test.setTimeout(120_000)
    await page.setViewportSize(viewport)
    // controller/misc.go publishes both options as the stored JSON text.
    const status = {
      HeaderNavModules: '{"pricing":true}',
      SidebarModulesAdmin: '{"admin":{"enabled":true,"channel":false}}',
    }
    const fixture = await configureStablePage(page, { theme, status })
    const pricingLinks = page.locator('a[href="/pricing"]')
    const openPublicNav = async () => {
      await page.goto('/')
      await waitForStablePage(page)
      if (viewport.name === 'desktop') return
      await page.getByRole('button', { name: '打开导航菜单' }).click()
      await expect(page.getByRole('dialog')).toBeVisible()
    }

    // On mobile the navbar link is display:none and the drawer has its own.
    await openPublicNav()
    await expect(pricingLinks.filter({ visible: true }).first()).toBeVisible()
    status.HeaderNavModules = '{"pricing":false}'
    await openPublicNav()
    await expect(page.getByRole('navigation').first()).toBeVisible()
    await expect(pricingLinks).toHaveCount(0)
    await page.goto('/pricing')
    await expect(page).toHaveURL(/\/404$/)

    await page.goto('/system-settings/site')
    await waitForStablePage(page)
    const nav = page.locator(
      viewport.name === 'desktop'
        ? 'aside[data-handdrawn="navigation"]'
        : '[data-handdrawn="navigation-strip"]'
    )
    await expect(
      nav.locator('[data-console-nav-item="user-management"]')
    ).toBeVisible()
    await expect(
      nav.locator('[data-console-nav-item="channel-management"]')
    ).toHaveCount(0)
    // Like NewAPI, SidebarModulesAdmin only hides the entry.
    await page.goto('/channels')
    await waitForStablePage(page)
    await expect(page).toHaveURL(/\/channels$/)
    await expect(
      page.getByRole('button', { name: '新建渠道', exact: true })
    ).toBeVisible()
    await assertNoHorizontalOverflow(page)
    expect(fixture.errors).toEqual([])
    expect(fixture.missingMessages).toEqual([])
  })
}
