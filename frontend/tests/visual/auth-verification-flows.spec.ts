import {
  expect,
  test,
  type Locator,
  type Page,
  type Route,
} from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

const now = Math.floor(new Date('2026-07-27T12:00:00+08:00').getTime() / 1000)
const turnstileStatus = {
  turnstile_check: true,
  turnstile_site_key: 'visual-site-key',
}
const wechatQRCodeURL = 'https://cdn.example.com/wechat.png'
// A 1x1 PNG standing in for the administrator-configured WeChat QR image.
const wechatImage = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
  'base64'
)
// Mirrors service.TwoFASetup.
const twoFASetup = {
  secret: 'JBSWY3DPEHPK3PXP',
  qr_code_data:
    'otpauth://totp/Ren2Hub:native.root?secret=JBSWY3DPEHPK3PXP&issuer=Ren2Hub',
  backup_codes: ['AAAA-1111', 'BBBB-2222', 'CCCC-3333', 'DDDD-4444'],
  flow_token: 'visual-2fa-flow',
  expires_at: now + 600,
}

interface SentRequest {
  path: string
  query: Record<string, string>
  turnstileHeader: boolean
  body: unknown
}

function record(route: Route, sent: SentRequest[]) {
  const request = route.request()
  const url = new URL(request.url())
  sent.push({
    path: url.pathname,
    query: Object.fromEntries(url.searchParams),
    turnstileHeader: Object.keys(request.headers()).some((name) =>
      name.includes('turnstile')
    ),
    body: request.postData() ? request.postDataJSON() : undefined,
  })
}

// Stands in for the Cloudflare script. Each render or reset issues the next
// numbered token asynchronously, as the real widget does after a challenge.
async function stubTurnstile(page: Page) {
  await page.addInitScript(() => {
    let issued = 0
    const callbacks = new Map<string, (token: string) => void>()
    const issue = (id: string) =>
      setTimeout(() => callbacks.get(id)?.(`visual-turnstile-${++issued}`), 0)
    Object.assign(window, {
      turnstile: {
        render(
          container: HTMLElement,
          options: { callback: (token: string) => void }
        ) {
          const id = `widget-${callbacks.size + 1}`
          callbacks.set(id, options.callback)
          container.innerHTML =
            '<div data-visual-turnstile style="width:300px;height:65px;border:1px solid #9ca3af;border-radius:4px"></div>'
          issue(id)
          return id
        },
        reset: issue,
        remove: (id: string) => callbacks.delete(id),
      },
    })
  })
}

function viewport(width: number) {
  return { width, height: width === 390 ? 844 : 900 }
}

async function expectWithinViewport(page: Page, target: Locator) {
  const box = await target.boundingBox()
  expect(box).not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(page.viewportSize()!.width)
}

for (const theme of ['light', 'dark'] as const)
  for (const width of [1440, 390]) {
    test(`${theme} ${width} sign-up sends each Turnstile token once as a query parameter`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(viewport(width))
      const fixture = await configureStablePage(page, {
        theme,
        authenticated: false,
        status: { ...turnstileStatus, email_verification: true },
      })
      await stubTurnstile(page)
      const sent: SentRequest[] = []
      let releaseCode = () => {}
      const codeSent = new Promise<void>((resolve) => {
        releaseCode = resolve
      })
      await page.route('**/api/verification?*', async (route) => {
        record(route, sent)
        await codeSent
        await route.fulfill({ json: { success: true, message: '' } })
      })
      await page.route('**/api/user/register?*', (route) => {
        record(route, sent)
        return route.fulfill({ json: { success: true, message: '' } })
      })
      await page.goto('/sign-up')
      await waitForStablePage(page)

      const card = page.locator('.auth-card')
      await expect(card.locator('[data-visual-turnstile]')).toBeVisible()
      await card.locator('input[autocomplete="username"]').fill('visual.user')
      await card
        .locator('input[autocomplete="email"]')
        .fill('visual@example.test')
      const submit = card.locator('button[type="submit"]')
      await card.getByRole('button', { name: '发送验证码' }).click()
      // Sending the code and registering share one busy state.
      await expect(submit).toBeDisabled()
      releaseCode()
      await expect(submit).toBeEnabled()
      await card.locator('input[autocomplete="one-time-code"]').fill('246810')
      const passwords = card.locator('input[autocomplete="new-password"]')
      await passwords.nth(0).fill('visual-pass-123')
      await passwords.nth(1).fill('visual-pass-123')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath(`sign-up-${theme}-${width}.png`),
        fullPage: true,
      })
      await submit.click()
      await expect(page).toHaveURL(/\/sign-in$/)
      await expect(page.getByText('账户已创建，请登录。')).toBeVisible()

      expect(sent).toEqual([
        {
          path: '/api/verification',
          query: {
            email: 'visual@example.test',
            turnstile: 'visual-turnstile-1',
          },
          turnstileHeader: false,
          body: undefined,
        },
        {
          path: '/api/user/register',
          query: { turnstile: 'visual-turnstile-2' },
          turnstileHeader: false,
          body: {
            username: 'visual.user',
            password: 'visual-pass-123',
            email: 'visual@example.test',
            verification_code: '246810',
          },
        },
      ])
      expect(fixture.errors).toEqual([])
      expect(fixture.missingMessages).toEqual([])
    })

    test(`${theme} ${width} password reset sends the Turnstile token as a query parameter`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(viewport(width))
      const fixture = await configureStablePage(page, {
        theme,
        authenticated: false,
        status: turnstileStatus,
      })
      await stubTurnstile(page)
      const sent: SentRequest[] = []
      await page.route('**/api/reset_password?*', (route) => {
        record(route, sent)
        return route.fulfill({ json: { success: true, message: '' } })
      })
      await page.goto('/forgot-password')
      await waitForStablePage(page)

      const card = page.locator('.auth-card')
      await expect(card.locator('[data-visual-turnstile]')).toBeVisible()
      await card.locator('input[type="email"]').fill('visual@example.test')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath(`reset-password-${theme}-${width}.png`),
        fullPage: true,
      })
      const submit = card.locator('button[type="submit"]')
      await submit.click()
      await expect(submit).toContainText('重置链接已发送，请查收邮箱')
      await expect(submit).toBeDisabled()

      expect(sent).toEqual([
        {
          path: '/api/reset_password',
          query: {
            email: 'visual@example.test',
            turnstile: 'visual-turnstile-1',
          },
          turnstileHeader: false,
          body: undefined,
        },
      ])
      expect(fixture.errors).toEqual([])
      expect(fixture.missingMessages).toEqual([])
    })

    test(`${theme} ${width} security dialogs render the 2FA and WeChat QR codes`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(viewport(width))
      const fixture = await configureStablePage(page, {
        theme,
        authenticated: true,
        status: { wechat_login: true, wechat_qrcode: wechatQRCodeURL },
      })
      await page.route(wechatQRCodeURL, (route) =>
        route.fulfill({ contentType: 'image/png', body: wechatImage })
      )
      await page.route('**/api/verify/methods?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              scope: '2fa.setup',
              methods: [{ method: 'password', available: true }],
              oauth_providers: [],
              password_encryption_enabled: false,
            },
          },
        })
      )
      await page.route('**/api/user/login/encryption-key', (route) =>
        route.fulfill({ json: { success: true, data: { enabled: false } } })
      )
      await page.route('**/api/verify', (route) => {
        expect(route.request().postDataJSON()).toMatchObject({
          scope: '2fa.setup',
          password: 'visual-pass',
        })
        return route.fulfill({
          json: {
            success: true,
            data: {
              proof_token: 'visual-proof',
              expires_at: now + 600,
              method: 'password',
              scope: '2fa.setup',
            },
          },
        })
      })
      await page.route('**/api/user/2fa/setup', (route) => {
        expect(route.request().headers()['x-security-proof']).toBe(
          'visual-proof'
        )
        return route.fulfill({
          json: { success: true, message: '', data: twoFASetup },
        })
      })
      await page.goto('/security')
      await waitForStablePage(page)

      await page
        .getByRole('button', { name: '启用两步验证', exact: true })
        .click()
      const verification = page.getByRole('dialog', { name: '验证身份' })
      await verification.locator('input[type="password"]').fill('visual-pass')
      await verification
        .getByRole('button', { name: '确认', exact: true })
        .click()
      const twoFA = page.getByRole('dialog', { name: '配置 2FA', exact: true })
      await expect(
        twoFA.getByRole('img', { name: '两步验证二维码' }).locator('svg')
      ).toBeVisible()
      await expect(twoFA.locator('[data-twofa-secret]')).toHaveText(
        twoFASetup.secret
      )
      await expectWithinViewport(page, twoFA)
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath(`two-fa-setup-${theme}-${width}.png`),
      })
      await page.keyboard.press('Escape')
      await expect(twoFA).toHaveCount(0)

      await page.getByRole('button', { name: '绑定', exact: true }).click()
      const wechat = page.getByRole('dialog', { name: '绑定 微信' })
      const image = wechat.getByRole('img', { name: '微信公众号二维码' })
      await expect(image).toHaveAttribute('src', wechatQRCodeURL)
      await expect
        .poll(() =>
          image.evaluate((element: HTMLImageElement) => element.naturalWidth)
        )
        .toBe(1)
      await expectWithinViewport(page, wechat)
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath(`wechat-bind-${theme}-${width}.png`),
      })
      expect(fixture.errors).toEqual([])
      expect(fixture.missingMessages).toEqual([])
    })
  }
