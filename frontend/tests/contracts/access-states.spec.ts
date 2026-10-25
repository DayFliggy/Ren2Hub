import { expect, test, type Page } from '@playwright/test'
import en from '../../src/i18n/locales/en/auth'
import zh from '../../src/i18n/locales/zh-CN/auth'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import { captureUI } from './ui-coverage'

for (const mobile of [false, true]) {
  const variant = mobile ? 'mobile-dark-en' : 'desktop-light-zh-CN'
  const theme = mobile ? 'dark' : 'light'
  const locale = mobile ? 'en' : 'zh-CN'
  const viewport = mobile
    ? { width: 390, height: 844 }
    : { width: 1440, height: 900 }

  for (const role of [1, 10, 100]) {
    test(`root monitor keeps role boundary ${role} ${variant}`, async ({
      page,
    }, info) => {
      await page.setViewportSize(viewport)
      await configureStablePage(page, {
        theme,
        locale,
        user: {
          role,
          permissions: { admin_permissions: { channel: { read: true } } },
        },
      })
      const reads: string[] = []
      page.on('request', (request) => {
        if (new URL(request.url()).pathname.startsWith('/api/performance/'))
          reads.push(request.url())
      })
      await page.goto('/ops')
      await expect(page).toHaveURL(role === 100 ? /\/ops$/ : /\/dashboard$/, {
        timeout: 30_000,
      })
      await waitForStablePage(page)
      if (role !== 100) {
        expect(reads).toEqual([])
        await expect(page.locator('[data-resource-caption]')).toHaveCount(0)
      } else {
        await expect.poll(() => reads.length).toBeGreaterThan(0)
      }
      await captureUI(page, info, 'role-boundary')
    })
  }

  test(`guest direct link retains safe return path ${variant}`, async ({
    page,
  }, info) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, { theme, locale, authenticated: false })
    await page.goto('/keys/7/routing')
    await expect(page).toHaveURL(/\/sign-in\?redirect=/)
    expect(new URL(page.url()).searchParams.get('redirect')).toBe(
      '/keys/7/routing'
    )
    await waitForStablePage(page)
    await captureUI(page, info, 'guest-sign-in')
  })

  test(`restricted admin reads channels without create action ${variant}`, async ({
    page,
  }, info) => {
    await page.setViewportSize(viewport)
    const fixture = await configureStablePage(page, {
      theme,
      locale,
      user: {
        role: 10,
        permissions: { admin_permissions: { channel: { read: true } } },
      },
    })
    await page.goto('/channels')
    await expect(page).toHaveURL(/\/channels$/)
    await waitForStablePage(page)
    await expect(
      page.getByRole('button', {
        name: mobile ? 'New channel' : '新建渠道',
        exact: true,
      })
    ).toHaveCount(0)
    expect(
      fixture.writes.filter((entry) => entry.path.startsWith('/api/channel'))
    ).toEqual([])
    await captureUI(page, info, 'restricted-channels')
  })

  test(`rate-limited navigation remains actionable ${variant}`, async ({
    page,
  }, info) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, { theme, locale, authenticated: false })
    await page.route('**/api/user/auth/refresh', (route) =>
      route.fulfill({
        status: 429,
        json: { success: false, message: 'Too many requests' },
      })
    )
    await page.goto('/sign-in')
    await expect(page.locator('[data-navigation-retry]')).toBeVisible()
    await expect(page.locator('[data-navigation-home]')).toHaveAttribute(
      'href',
      '/'
    )
    await page.locator('[data-navigation-retry]').focus()
    await expect(page.locator('[data-navigation-retry]')).toBeFocused()
    await captureUI(page, info, 'navigation-rate-limit')
    await page.unroute('**/api/user/auth/refresh')
    await page.locator('[data-navigation-retry]').click()
    await waitForStablePage(page)
    await expect(page.locator('[data-navigation-retry]')).toHaveCount(0)
    await captureUI(page, info, 'navigation-recovered')
  })
}

const scenarios = [
  {
    key: 'desktop-light-zh-CN',
    width: 1440,
    height: 900,
    theme: 'light',
    locale: 'zh-CN',
  },
  {
    key: 'mobile-dark-en',
    width: 390,
    height: 844,
    theme: 'dark',
    locale: 'en',
  },
] as const

function currentNavigation(page: Page, mobile: boolean) {
  return page.locator(
    mobile
      ? 'nav[data-handdrawn="navigation-strip"]'
      : 'aside[data-handdrawn="navigation"]'
  )
}

for (const scenario of scenarios) {
  const mobile = scenario.width < 1024
  const copy = (scenario.locale === 'en' ? en : zh).auth

  test(`registration verification row remains named usable and pending ${scenario.key}`, async ({
    page,
  }, info) => {
    await page.setViewportSize(scenario)
    const fixture = await configureStablePage(page, {
      ...scenario,
      authenticated: false,
      status: {
        email_verification: true,
        register_enabled: true,
        password_register_enabled: true,
      },
    })
    let release!: () => void
    const pending = new Promise<void>((resolve) => {
      release = resolve
    })
    const sends: Array<{ method: string; email: string | null }> = []
    await page.route('**/api/verification?**', async (route) => {
      const request = route.request()
      const url = new URL(request.url())
      sends.push({
        method: request.method(),
        email: url.searchParams.get('email'),
      })
      await pending
      await route.fulfill({ json: { success: true, message: '' } })
    })
    await page.goto('/sign-up')
    await waitForStablePage(page)
    const code = page.getByRole('textbox', {
      name: copy.verificationCode,
      exact: true,
    })
    const label = page.getByText(copy.verificationCode, { exact: true })
    const email = page.getByRole('textbox', { name: copy.email, exact: true })
    const send = page.getByRole('button', { name: copy.sendCode, exact: true })
    const submit = page.getByRole('button', {
      name: copy.continue,
      exact: true,
    })
    await expect(code).toHaveAccessibleName(copy.verificationCode)
    await expect(send).toHaveAccessibleName(copy.sendCode)
    await expect(send).toBeDisabled()
    expect(
      await send.evaluate((element) => element.closest('label'))
    ).toBeNull()
    await expect(send).toHaveAttribute('type', 'button')
    await email.fill('ui-verification@example.test')
    await code.fill('123456')
    await expect(send).toBeEnabled()
    for (const width of mobile ? [390, 320] : [1440]) {
      await page.setViewportSize({ width, height: scenario.height })
      await label.click()
      await expect(code).toBeFocused()
      await page.keyboard.press('Tab')
      await expect(send).toBeFocused()
      const inputBox = (await code.boundingBox())!
      const sendBox = (await send.boundingBox())!
      for (const box of [inputBox, sendBox]) {
        expect(box.width).toBeGreaterThan(40)
        expect(box.x).toBeGreaterThanOrEqual(0)
        expect(box.x + box.width).toBeLessThanOrEqual(width + 1)
      }
      if (width < 640) {
        expect(sendBox.y - inputBox.y - inputBox.height).toBeGreaterThanOrEqual(
          7
        )
      } else {
        expect(sendBox.x - inputBox.x - inputBox.width).toBeGreaterThanOrEqual(
          7
        )
        expect(
          Math.abs(
            sendBox.y + sendBox.height / 2 - inputBox.y - inputBox.height / 2
          )
        ).toBeLessThanOrEqual(1)
      }
      await expect(code).toBeInViewport()
      await expect(send).toBeInViewport()
      await assertNoHorizontalOverflow(page)
      await captureUI(page, info, `verification-ready-${width}`)
    }
    try {
      await send.click()
      await expect
        .poll(() => sends)
        .toEqual([{ method: 'GET', email: 'ui-verification@example.test' }])
      await expect(send).toBeDisabled()
      await expect(send.locator('.animate-spin')).toBeVisible()
      await expect(submit).toBeDisabled()
      await expect(code).toHaveValue('123456')
      await assertNoHorizontalOverflow(page)
      await captureUI(page, info, 'verification-sending')
    } finally {
      release()
    }
    await expect(send).toBeEnabled()
    await expect(send.locator('.animate-spin')).toHaveCount(0)
    await expect(submit).toBeEnabled()
    await expect(code).toHaveValue('123456')
    expect(sends).toHaveLength(1)
    expect(
      fixture.writes.filter((write) => write.path === '/api/user/register')
    ).toEqual([])
    await waitForStablePage(page)
    await captureUI(page, info, 'verification-sent')
  })

  test(`token capability blocks access while sidebar hiding only removes entry ${scenario.key}`, async ({
    page,
  }, info) => {
    await page.setViewportSize(scenario)
    // These are current consumers, not ignored legacy configuration switches.
    // Capability disabled renders a disabled button; SidebarModulesAdmin hides it.
    const capabilities: Record<string, 'live' | 'disabled'> = {
      dashboard_basic: 'live',
      api_tokens: 'live',
      user_models: 'live',
      logs: 'live',
      tickets: 'live',
      wallet: 'live',
      invites: 'live',
    }
    const status = {
      frontend_capabilities: capabilities,
      SidebarModulesAdmin: JSON.stringify({
        console: { enabled: true, token: true },
      }),
    }
    await configureStablePage(page, {
      ...scenario,
      user: { role: 1, username: 'ui.member' },
      status,
    })
    const reads: string[] = []
    page.on('request', (request) => {
      if (
        request.method() === 'GET' &&
        /^\/api\/token(?:\/|$)/.test(new URL(request.url()).pathname)
      )
        reads.push(new URL(request.url()).pathname)
    })
    await page.goto('/keys')
    await waitForStablePage(page)
    const navigation = currentNavigation(page, mobile)
    const item = navigation.locator('[data-console-nav-item="keys"]')
    await expect(item).toHaveAttribute('href', '/keys')
    expect(reads.length).toBeGreaterThan(0)
    capabilities.api_tokens = 'disabled'
    reads.length = 0
    await page.goto('/keys')
    await expect(page).toHaveURL(/\/dashboard$/)
    await waitForStablePage(page)
    await expect(page.locator('.key-list')).toHaveCount(0)
    await expect(item).toBeDisabled()
    expect(await item.evaluate((element) => element.tagName)).toBe('BUTTON')
    await expect(item).not.toHaveAttribute('href', /.+/)
    expect(reads).toEqual([])
    await item.scrollIntoViewIfNeeded()
    await captureUI(page, info, 'token-capability-disabled')

    capabilities.api_tokens = 'live'
    status.SidebarModulesAdmin = JSON.stringify({
      console: { enabled: true, token: false },
    })
    await page.goto('/keys')
    await expect(page).toHaveURL(/\/keys$/)
    await waitForStablePage(page)
    await expect(page.locator('.key-list')).toBeVisible()
    await expect(item).toHaveCount(0)
    expect(reads.length).toBeGreaterThan(0)
    await assertNoHorizontalOverflow(page)
    await captureUI(page, info, 'token-sidebar-hidden-route-live')
  })

  test(`root disabled admin capability blocks routes and disables entries ${scenario.key}`, async ({
    page,
  }, info) => {
    await page.setViewportSize(scenario)
    const capabilities: Record<string, 'live' | 'disabled'> = {
      dashboard_basic: 'live',
      admin: 'live',
      api_tokens: 'live',
      user_models: 'live',
      logs: 'live',
      tickets: 'live',
      wallet: 'live',
      invites: 'live',
      orders: 'live',
      taskPlugins: 'live',
    }
    await configureStablePage(page, {
      ...scenario,
      user: { role: 100 },
      status: { frontend_capabilities: capabilities },
    })
    const reads: string[] = []
    page.on('request', (request) => {
      const path = new URL(request.url()).pathname
      if (
        request.method() === 'GET' &&
        (path === '/api/channel/' || path === '/api/models/search')
      )
        reads.push(path)
    })
    await page.goto('/dashboard')
    await waitForStablePage(page)
    const navigation = currentNavigation(page, mobile)
    const channels = navigation.locator(
      '[data-console-nav-item="channel-management"]'
    )
    const models = navigation.locator('[data-console-nav-item="models-admin"]')
    await expect(channels).toHaveAttribute('href', '/channels')
    await expect(models).toHaveAttribute('href', '/models/metadata')
    capabilities.admin = 'disabled'
    reads.length = 0
    for (const path of ['/channels', '/models/metadata']) {
      await page.goto(path)
      await expect(page).toHaveURL(/\/dashboard$/)
      await waitForStablePage(page)
      await expect(channels).toBeDisabled()
      await expect(models).toBeDisabled()
      await expect(channels).not.toHaveAttribute('href', /.+/)
      await expect(models).not.toHaveAttribute('href', /.+/)
      expect(reads).toEqual([])
    }
    await channels.scrollIntoViewIfNeeded()
    await assertNoHorizontalOverflow(page)
    await captureUI(page, info, 'root-admin-capability-disabled')
  })
}
