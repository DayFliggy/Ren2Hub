import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

const now = Math.floor(new Date('2026-07-27T12:00:00+08:00').getTime() / 1000)
const nodes = [
  {
    node_name: 'ren2hub-primary',
    status: 'online',
    stale_after_seconds: 90,
    started_at: now - 86400,
    last_seen_at: now - 12,
    info: {
      node: {
        name: 'ren2hub-primary',
        source: 'manual',
        manually_configured: true,
        should_configure_manually: false,
      },
      role: { is_master: true },
      runtime: { version: 'v1.0.0', goos: 'linux', goarch: 'amd64' },
      host: { hostname: 'primary-host' },
      resources: {
        cpu: { usage_percent: 32.5 },
        memory: { usage_percent: 75.1 },
        storage: {
          total_bytes: 107374182400,
          used_bytes: 26843545600,
          free_bytes: 80530636800,
          used_percent: 25,
        },
      },
    },
  },
  {
    node_name: 'worker-with-a-long-hostname-and-missing-resource-information',
    status: 'stale',
    stale_after_seconds: 90,
    started_at: now - 86400,
    last_seen_at: now - 7200,
    info: {
      node: {
        source: 'hostname',
        manually_configured: false,
        should_configure_manually: true,
      },
      role: { is_master: false },
      runtime: { goos: 'linux', goarch: 'arm64' },
    },
  },
]
const tasks = [
  {
    task_id: 'active-task',
    type: 'route_capability_refresh',
    status: 'running',
    created_at: now - 600,
    updated_at: now - 15,
    locked_by: 'ren2hub-primary',
    state: { progress: 52, processed: 52, total: 100 },
  },
  {
    task_id: 'failed-task',
    type: 'billing_recovery',
    status: 'failed',
    created_at: now - 7200,
    updated_at: now - 3600,
    locked_by: 'ren2hub-primary',
    state: { progress: 5 },
    error:
      'Recovery request timed out. Request state remains pending and can be retried safely.',
  },
]

for (const locale of ['zh-CN', 'en']) {
  for (const theme of ['light', 'dark'] as const) {
    for (const mobile of [false, true]) {
      test(`system instances ${locale} ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
        page,
      }, testInfo) => {
        await page.setViewportSize(
          mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
        )
        await configureStablePage(page, { theme })
        await page.addInitScript(
          (language) => localStorage.setItem('ren2hub_locale', language),
          locale
        )
        await page.route('**/api/system-info/instances', (route) =>
          route.fulfill({ json: { success: true, data: nodes } })
        )
        await page.route('**/api/system-task/list?*', (route) =>
          route.fulfill({ json: { success: true, data: tasks } })
        )
        const failures: string[] = []
        page.on('pageerror', (error) => failures.push(error.message))
        page.on('response', (response) => {
          if (response.status() === 404 && response.url().includes('127.0.0.1'))
            failures.push(response.url())
        })
        await page.goto('/system-info')
        await expect(
          page.getByRole('heading', {
            name: locale === 'en' ? 'System Instances' : '系统实例',
            exact: true,
          })
        ).toBeVisible()
        await expect(page.getByText('32.5%', { exact: true })).toBeVisible()
        await expect(
          page.getByText(
            locale === 'en' ? 'Route capability refresh' : '路由能力刷新',
            { exact: true }
          )
        ).toBeVisible()
        await assertNoHorizontalOverflow(page)
        await page.screenshot({
          path: testInfo.outputPath('system-instances.png'),
          fullPage: true,
        })

        await page
          .getByRole('button', {
            name: `${locale === 'en' ? 'Disk usage' : '磁盘用量'}: ren2hub-primary`,
            exact: true,
          })
          .click()
        const dialog = page.getByRole('dialog')
        await expect(dialog).toBeVisible()
        await expect(dialog.getByText('100 GiB', { exact: true })).toBeVisible()
        await page.keyboard.press('Tab')
        expect(
          await dialog.evaluate((element) =>
            element.contains(document.activeElement)
          )
        ).toBe(true)
        await page.keyboard.press('Escape')
        await expect(dialog).toHaveCount(0)
        await expect(
          page.getByRole('button', {
            name: `${locale === 'en' ? 'Disk usage' : '磁盘用量'}: ren2hub-primary`,
            exact: true,
          })
        ).toBeFocused()
        expect(failures).toEqual([])
      })
    }
  }
}

test('instance errors, independent tasks and empty retry result', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  let failed = true
  await page.route('**/api/system-info/instances', (route) =>
    route.fulfill({
      status: failed ? 500 : 200,
      json: failed
        ? { success: false, message: 'Instance feed unavailable' }
        : { success: true, data: [] },
    })
  )
  await page.route('**/api/system-task/list?*', (route) =>
    route.fulfill({ json: { success: true, data: tasks } })
  )
  await page.goto('/system-info')
  await expect(
    page.getByRole('alert').filter({ hasText: 'Instance feed unavailable' })
  ).toBeVisible()
  await expect(page.getByText('路由能力刷新', { exact: true })).toBeVisible()
  failed = false
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect(page.getByText('暂无系统实例', { exact: true })).toBeVisible()
  await expect(
    page.getByRole('alert').filter({ hasText: 'Instance feed unavailable' })
  ).toHaveCount(0)
})
