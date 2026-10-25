import { expect, test, type Page, type TestInfo } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import { nativeUser } from './native-core-fixture'
import { captureUI } from './ui-coverage'

const scenarios = [
  {
    key: 'desktop-light-zh-CN',
    locale: 'zh-CN',
    theme: 'light',
    width: 1440,
    height: 900,
  },
  {
    key: 'mobile-dark-en',
    locale: 'en',
    theme: 'dark',
    width: 390,
    height: 844,
  },
] as const

type Scenario = (typeof scenarios)[number]

async function setup(
  page: Page,
  scenario: Scenario,
  authenticated = true,
  user?: Record<string, unknown>
) {
  await page.setViewportSize(scenario)
  await configureStablePage(page, { ...scenario, authenticated, user })
}

async function hasView(page: Page, view: string) {
  return page.evaluate((name) => {
    for (const element of document.querySelectorAll('*')) {
      let instance = (
        element as Element & {
          __vueParentComponent?: {
            type?: { __file?: string }
            parent?: unknown
          }
        }
      ).__vueParentComponent
      while (instance) {
        if (instance.type?.__file?.endsWith(`/${name}.vue`)) return true
        instance = instance.parent as typeof instance
      }
    }
    return false
  }, view)
}

async function expectLocation(page: Page, expected: string) {
  const target = new URL(expected, 'http://fixture.test')
  await expect.poll(() => new URL(page.url()).pathname).toBe(target.pathname)
  const actual = new URL(page.url())
  expect([...actual.searchParams.entries()].sort()).toEqual(
    [...target.searchParams.entries()].sort()
  )
  expect(actual.hash).toBe(target.hash)
}

async function visit(
  page: Page,
  input: string,
  expected: string,
  view: string
) {
  await page.goto(input)
  await expect.poll(() => hasView(page, view), { timeout: 30_000 }).toBe(true)
  await waitForStablePage(page)
  await expectLocation(page, expected)
  await assertNoHorizontalOverflow(page)
}

async function checkpoint(page: Page, info: TestInfo, label: string) {
  await captureUI(page, info, label, { scroll: false })
}

async function installPasswordLogin(page: Page, locale: string) {
  let count = 0
  await page.route('**/api/user/login/encryption-key', (route) =>
    route.fulfill({ json: { success: true, data: { enabled: false } } })
  )
  await page.route('**/api/user/login', async (route) => {
    expect(route.request().method()).toBe('POST')
    expect(route.request().postDataJSON()).toMatchObject({
      username: 'native.root',
      password: 'ui-regression-password',
    })
    count += 1
    const now = Math.floor(
      new Date('2026-07-27T12:00:00+08:00').getTime() / 1000
    )
    await route.fulfill({
      json: {
        success: true,
        data: {
          access_token: 'fixture-login-access',
          token_type: 'Bearer',
          access_expires_at: now + 3600,
          session: {
            sid: 'fixture-login-session',
            current: true,
            login_method: 'password',
            ip: '127.0.0.1',
            user_agent: 'browser',
            created_at: now,
            last_active_at: now,
            expires_at: now + 86400,
          },
          user: nativeUser({}, locale),
        },
      },
    })
  })
  return () => count
}

async function login(page: Page) {
  await page.locator('input[autocomplete="username"]').fill('native.root')
  await page
    .locator('input[autocomplete="current-password"]')
    .fill('ui-regression-password')
  await page.locator('button[type="submit"]').click()
}

for (const scenario of scenarios) {
  test.describe(scenario.key, () => {
    test('navigation console prefix and alias preserve query and hash', async ({
      page,
    }, info) => {
      await setup(page, scenario)
      await visit(
        page,
        '/console/token///?source=bookmark#tokens',
        '/keys?source=bookmark#tokens',
        'KeysView'
      )
      await expect(
        page
          .getByText('Native Key', { exact: true })
          .filter({ visible: true })
          .first()
      ).toBeVisible()
      await checkpoint(page, info, 'canonical-token-bookmark')
      await visit(page, '/console/', '/dashboard', 'DashboardView')
    })

    test('navigation log tabs and operation scope migrate explicitly', async ({
      page,
    }, info) => {
      await setup(page, scenario)
      await visit(
        page,
        '/logs?tab=midjourney&source=bookmark#records',
        '/usage-logs/drawing?tab=midjourney&source=bookmark#records',
        'DrawingLogsView'
      )
      await visit(
        page,
        '/usage-logs?tab=tasks&source=bookmark',
        '/usage-logs/task?tab=tasks&source=bookmark',
        'TaskLogsView'
      )
      await visit(
        page,
        '/console/logs/operations/?source=bookmark',
        '/usage-logs/audit?scope=all&source=bookmark',
        'AuditLogsView'
      )
      await checkpoint(page, info, 'operation-log-default-scope')
      await visit(
        page,
        '/usage-logs/operations?scope=self&source=bookmark',
        '/usage-logs/audit?scope=self&source=bookmark',
        'AuditLogsView'
      )
    })

    test('navigation setting tabs and section aliases preserve intent', async ({
      page,
    }, info) => {
      await setup(page, scenario)
      await visit(
        page,
        '/setting?tab=ratelimit&source=bookmark#policy',
        '/system-settings/security/rate-limit?tab=ratelimit&source=bookmark#policy',
        'SystemSettingsView'
      )
      await checkpoint(page, info, 'settings-rate-limit-bookmark')
      await visit(
        page,
        '/system-settings/billing/currency?source=bookmark',
        '/system-settings/billing/quota?source=bookmark',
        'SystemSettingsView'
      )
      await visit(
        page,
        '/setting?tab=unknown&source=bookmark',
        '/system-settings/site/system-info?tab=unknown&source=bookmark',
        'SystemSettingsView'
      )
    })

    test('navigation retired pricing sections share current pricing page', async ({
      page,
    }, info) => {
      await setup(page, scenario)
      await visit(
        page,
        '/setting?tab=ratio&source=bookmark',
        '/models/pricing?tab=ratio&source=bookmark',
        'ModelManagementView'
      )
      await visit(
        page,
        '/system-settings/models/deployment?source=bookmark#pricing',
        '/models/pricing?source=bookmark#pricing',
        'ModelManagementView'
      )
      await checkpoint(page, info, 'canonical-model-pricing')
    })

    test('navigation model query migration prefers explicit canonical fields', async ({
      page,
    }, info) => {
      await setup(page, scenario)
      await visit(
        page,
        '/admin/models?keyword=old&filter=contract&sync_official=1&p=2&page=1&page_size=20&source=bookmark#models',
        '/models/metadata?filter=contract&sync=1&page=1&pageSize=20&source=bookmark#models',
        'ModelManagementView'
      )
      await checkpoint(page, info, 'canonical-model-filter')
      await visit(
        page,
        '/models/models?manage=vendors&keyword=native&p=1&page_size=20&source=bookmark',
        '/models/vendors?filter=native&page=1&pageSize=20&source=bookmark',
        'ModelManagementView'
      )
    })

    test('navigation admin record redirects preserve query hash and panel', async ({
      page,
    }, info) => {
      await setup(page, scenario)
      await visit(
        page,
        '/admin/prefill-groups?source=bookmark#groups',
        '/models/metadata?source=bookmark&manage=prefill-groups#groups',
        'ModelManagementView'
      )
      await expect(page.getByRole('dialog')).toBeVisible()
      await checkpoint(page, info, 'prefill-redirect-panel')
      await visit(
        page,
        '/system-info?source=bookmark#status',
        '/ops?source=bookmark#status',
        'OpsMonitorView'
      )
    })

    test('navigation retired route families show 404 without retired APIs', async ({
      page,
    }, info) => {
      await setup(page, scenario)
      const retiredRequests: string[] = []
      page.on('request', (request) => {
        const path = new URL(request.url()).pathname
        if (/^\/api\/(next|playground|chat|deployment)(\/|$)/.test(path))
          retiredRequests.push(path)
      })
      for (const path of [
        '/next/dashboard',
        '/playground/demo',
        '/admin/deployments/7',
        '/system-settings/content/chat',
      ]) {
        await visit(
          page,
          `${path}?source=bookmark#retired`,
          '/404?source=bookmark#retired',
          'StatusErrorView'
        )
        await expect(
          page.getByRole('heading', { name: '404', exact: true })
        ).toBeVisible()
      }
      expect(retiredRequests).toEqual([])
      await checkpoint(page, info, 'retired-route-404')
    })

    test('navigation guest canonical return survives password login', async ({
      page,
    }, info) => {
      await setup(page, scenario, false)
      const loginCount = await installPasswordLogin(page, scenario.locale)
      await visit(
        page,
        '/console/token?source=login#tokens',
        '/sign-in?redirect=%2Fkeys%3Fsource%3Dlogin%23tokens',
        'SignInView'
      )
      await checkpoint(page, info, 'guest-canonical-return')
      await login(page)
      await expect.poll(() => hasView(page, 'KeysView')).toBe(true)
      await waitForStablePage(page)
      await expectLocation(page, '/keys?source=login#tokens')
      expect(loginCount()).toBe(1)
    })

    test('navigation password login rejects unsafe returns and resolves retired bookmarks', async ({
      page,
    }, info) => {
      await setup(page, scenario, false)
      const loginCount = await installPasswordLogin(page, scenario.locale)
      // Each full navigation creates a fresh anonymous application/session.
      for (const { redirect, destination, view } of [
        {
          redirect: '//outside.invalid/private',
          destination: '/dashboard',
          view: 'DashboardView',
        },
        {
          redirect: '/\\outside.invalid/private',
          destination: '/dashboard',
          view: 'DashboardView',
        },
        {
          redirect: '/next/dashboard',
          destination: '/404',
          view: 'StatusErrorView',
        },
        {
          redirect: '/sign-in',
          destination: '/dashboard',
          view: 'DashboardView',
        },
      ]) {
        await visit(
          page,
          `/login?redirect=${encodeURIComponent(redirect)}`,
          `/sign-in?redirect=${encodeURIComponent(redirect)}`,
          'SignInView'
        )
        await login(page)
        await expect.poll(() => hasView(page, view)).toBe(true)
        await waitForStablePage(page)
        await expectLocation(page, destination)
      }
      expect(loginCount()).toBe(4)
      await checkpoint(page, info, 'safe-default-login-return')
    })

    test('navigation ordinary user cannot enter canonical admin pages', async ({
      page,
    }, info) => {
      await setup(page, scenario, true, { role: 1, username: 'native.member' })
      await visit(
        page,
        '/admin/models?source=direct',
        '/dashboard',
        'DashboardView'
      )
      expect(await hasView(page, 'ModelManagementView')).toBe(false)
      await visit(
        page,
        '/system-info?source=direct',
        '/dashboard',
        'DashboardView'
      )
      expect(await hasView(page, 'OpsMonitorView')).toBe(false)
      await checkpoint(page, info, 'guarded-admin-return')
    })

    test('navigation expired authenticated session unloads protected view and returns safely', async ({
      page,
    }, info) => {
      await setup(page, scenario)
      await visit(
        page,
        '/keys?source=expired#tokens',
        '/keys?source=expired#tokens',
        'KeysView'
      )
      await expect(
        page
          .getByText('Native Key', { exact: true })
          .filter({ visible: true })
          .first()
      ).toBeVisible()
      const rejected: string[] = []
      await page.route('**/api/token/search?*', async (route) => {
        rejected.push('protected-query')
        expect(route.request().headers().authorization).toBe(
          'Bearer fixture-access'
        )
        await route.fulfill({
          status: 401,
          json: { success: false, message: 'Session expired' },
        })
      })
      await page.route('**/api/user/auth/refresh', async (route) => {
        rejected.push('refresh')
        expect(route.request().method()).toBe('POST')
        await route.fulfill({
          status: 401,
          json: { success: false, message: 'Refresh session expired' },
        })
      })
      await page.locator('[name="token-search"]').fill('expire-session')
      await expect.poll(() => rejected).toEqual(['protected-query', 'refresh'])
      await expect(
        page.getByText('Native Key', { exact: true }).filter({ visible: true })
      ).toHaveCount(0)
      await info.attach('session-invalidation-state', {
        body: JSON.stringify({
          url: page.url(),
          protectedViewMounted: await hasView(page, 'KeysView'),
          rejected,
        }),
        contentType: 'application/json',
      })
      await expect.poll(() => hasView(page, 'SignInView')).toBe(true)
      expect(await hasView(page, 'KeysView')).toBe(false)
      await expectLocation(
        page,
        '/sign-in?redirect=%2Fkeys%3Fsource%3Dexpired%23tokens'
      )
      await waitForStablePage(page)
      await checkpoint(page, info, 'session-expired-state')
    })
  })
}
