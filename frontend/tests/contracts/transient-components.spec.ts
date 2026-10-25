import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { readFileSync } from 'node:fs'
import en from '../../src/i18n/locales/en/console'
import zh from '../../src/i18n/locales/zh-CN/console'
import commonEn from '../../src/i18n/locales/en/common'
import commonZh from '../../src/i18n/locales/zh-CN/common'
import { personalAccessTokenMessages } from '../../src/i18n/personalAccessTokens'
import {
  configureStablePage,
  waitForStablePage,
  assertNoHorizontalOverflow,
} from './fixtures'
import { captureUI } from './ui-coverage'

const now = new Date('2026-07-27T12:00:00+08:00').getTime() / 1000
const image = readFileSync(new URL('../../public/logo.png', import.meta.url))
const resultPage = (items: unknown[]) => ({
  items,
  total: items.length,
  page: 1,
  page_size: 10,
})

async function checkpoint(
  page: Page,
  info: TestInfo,
  label: string,
  components: string[]
) {
  await assertNoHorizontalOverflow(page)
  await captureUI(page, info, label)
  // Require the scenario's stateful components, not just the page shell.
  const evidence = JSON.parse(
    readFileSync(info.outputPath(`${label}.coverage.json`), 'utf8')
  ) as { visible: string[] }
  for (const name of components)
    expect(
      evidence.visible.some((file) => file.endsWith(`/${name}.vue`)),
      name
    ).toBe(true)
}

for (const mobile of [false, true]) {
  const locale = mobile ? 'en' : 'zh-CN'
  const theme = mobile ? 'dark' : 'light'
  const variant = mobile ? 'mobile-dark-en' : 'desktop-light-zh-CN'
  const viewport = mobile
    ? { width: 390, height: 844 }
    : { width: 1440, height: 900 }
  const copy = mobile ? en : zh
  const common = mobile ? commonEn.common : commonZh.common

  test(`public navigation and password verification ${variant}`, async ({
    page,
  }, info) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, {
      theme,
      locale,
      authenticated: false,
      status: {
        turnstile_check: true,
        turnstile_site_key: 'local-test-site',
      },
    })
    await page.route('**/api/notice', (route) =>
      route.fulfill({
        json: { success: true, data: 'UI regression maintenance notice' },
      })
    )
    // A local widget double exercises the callback contract without contacting
    // Cloudflare or treating a synthetic challenge as a real security check.
    await page.addInitScript(() => {
      window.turnstile = {
        render(container, options) {
          const element =
            typeof container === 'string'
              ? document.querySelector(container)
              : container
          const button = document.createElement('button')
          button.type = 'button'
          button.textContent = 'Local verification fixture'
          button.onclick = () => options.callback?.('synthetic-challenge')
          element?.append(button)
          return 'local-widget'
        },
        reset() {},
        remove() {},
      }
    })
    await page.goto('/pricing')
    await waitForStablePage(page)
    if (mobile) {
      const menu = page.locator(
        'button[aria-controls="mobile-navigation-drawer"]'
      )
      await menu.click()
      await expect(
        page.locator('#mobile-navigation-drawer [role="dialog"]')
      ).toBeVisible()
      await checkpoint(page, info, 'public-mobile-menu', ['MobileNavDrawer'])
      await page.keyboard.press('Escape')
      await expect(menu).toBeFocused()
    } else {
      await expect(
        page.getByText('UI regression maintenance notice', { exact: true })
      ).toBeVisible()
      await checkpoint(page, info, 'public-notice', ['NotificationBadge'])
    }
    await page.goto('/sign-up')
    await waitForStablePage(page)
    await page
      .locator('input[type="password"]')
      .first()
      .fill('Synthetic-Password-2026!')
    const verification = page.getByRole('button', {
      name: 'Local verification fixture',
      exact: true,
    })
    await expect(verification).toBeVisible()
    await verification.click()
    await checkpoint(page, info, 'registration-strength-verification', [
      'PasswordStrengthMeter',
      'TurnstileWidget',
    ])
  })

  test(`command palette key reveal and copy ${variant}`, async ({
    page,
    context,
  }, info) => {
    await page.setViewportSize(viewport)
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    await configureStablePage(page, { theme, locale })
    await page.route('**/api/token/7/key', (route) =>
      route.fulfill({
        json: { success: true, data: { key: 'sk-synthetic-ui-only-1234' } },
      })
    )
    await page.goto('/keys')
    await waitForStablePage(page)
    await page.locator('.search-trigger').click()
    await expect(page.locator('[name="command-search"]')).toBeFocused()
    await page.keyboard.press('ArrowDown')
    await checkpoint(page, info, 'command-palette', ['CommandPaletteModal'])
    await page.keyboard.press('Escape')
    const reveal = page
      .getByRole('button', {
        name: mobile ? copy.keys.viewKey : common.copy,
        exact: true,
      })
      .filter({ visible: true })
      .first()
    await reveal.click()
    await expect(page.getByRole('dialog')).toContainText(
      'sk-synthetic-ui-only-1234'
    )
    await checkpoint(page, info, 'key-reveal', ['KeyRevealModal'])
    await page
      .getByRole('dialog')
      .getByRole('button', { name: common.copy, exact: true })
      .click()
    await expect(page.locator('[data-handdrawn="toast"]')).toBeVisible()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(
      'sk-synthetic-ui-only-1234'
    )
    await checkpoint(page, info, 'key-copy-toast', ['ToastHost'])
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(
      page.getByText('sk-synthetic-ui-only-1234', { exact: true })
    ).toHaveCount(0)
  })

  test(`security proof cancellation keeps access token draft ${variant}`, async ({
    page,
  }, info) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, { theme, locale })
    await page.route('**/api/verify/methods?**', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            scope: 'access_token.generate',
            methods: [{ method: 'password', available: true }],
            oauth_providers: [],
            password_encryption_enabled: false,
          },
        },
      })
    )
    await page.goto('/security')
    await waitForStablePage(page)
    const pat = personalAccessTokenMessages[locale].pat
    await page
      .getByTestId('pat-panel')
      .getByRole('button', { name: pat.create, exact: true })
      .click()
    await page
      .getByRole('dialog')
      .getByRole('textbox', { name: pat.name, exact: true })
      .fill('UI proof draft')
    await page.getByRole('dialog').getByRole('checkbox').check()
    await page
      .getByRole('dialog')
      .getByRole('button', { name: pat.save, exact: true })
      .click()
    await expect(page.getByRole('dialog')).toHaveCount(2)
    await expect(
      page.getByRole('dialog').last().locator('input[type="password"]')
    ).toBeVisible()
    await checkpoint(page, info, 'security-proof', ['SecurityProofDialog'])
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(1)
    await expect(
      page.getByRole('textbox', { name: pat.name, exact: true })
    ).toHaveValue('UI proof draft')
  })

  test(`log mismatch and billing details ${variant}`, async ({
    page,
  }, info) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, { theme, locale })
    await page.route('**/api/log/self?**', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: resultPage([
            {
              id: 1,
              type: 2,
              token_id: 7,
              token_name: 'Native Key',
              model_name: 'gpt-native',
              channel_name: 'Native channel',
              prompt_tokens: 100,
              completion_tokens: 20,
              quota: 140,
              use_time: 1,
              is_stream: true,
              content: 'Synthetic usage evidence',
              other: JSON.stringify({
                upstream_model_name: 'gpt-native',
                response_model_name: 'unexpected-model',
                input_cost: '0.0001',
              }),
              created_at: now,
            },
          ]),
        },
      })
    )
    await page.goto('/usage-logs')
    await waitForStablePage(page)
    if (mobile)
      await expect(page.locator('[data-log-mobile-card]')).toBeVisible()
    await checkpoint(
      page,
      info,
      'log-mismatch',
      mobile ? ['LogMobileCard'] : ['LogModelMismatch']
    )
    await page
      .locator('[data-log-detail-trigger]')
      .filter({ visible: true })
      .first()
      .click()
    await expect(page.getByRole('dialog')).toContainText('unexpected-model')
    await checkpoint(page, info, 'log-billing-detail', ['LogBillingDetail'])
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
  })

  test(`task image and audio artifacts ${variant}`, async ({ page }, info) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, { theme, locale })
    const origin = `http://127.0.0.1:${process.env.REN2HUB_UI_PORT}`
    const content = `${origin}/v1/tasks/ui-image/artifacts/image/content?access=${'a'.repeat(43)}`
    await page.route(
      '**/v1/tasks/ui-image/artifacts/image/content?**',
      (route) => route.fulfill({ contentType: 'image/png', body: image })
    )
    const rows = ['ui-image', 'ui-audio'].map((task_id, i) => ({
      id: i + 1,
      task_id,
      platform: i ? 'suno' : 'fixture',
      action: 'generate',
      status: 'SUCCESS',
      progress: '100%',
      fail_reason: '',
      quota: 10,
      submit_time: now - 60,
      finish_time: now,
    }))
    await page.route('**/api/task/self?**', (route) =>
      route.fulfill({ json: { success: true, data: resultPage(rows) } })
    )
    await page.route('**/api/task/ui-image/artifacts', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            task_id: 'ui-image',
            artifacts: [
              {
                key: 'image',
                type: 'image',
                content_url: content,
                mime_type: 'image/png',
              },
            ],
          },
        },
      })
    )
    await page.route('**/api/task/ui-audio/artifacts', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            task_id: 'ui-audio',
            artifacts: [],
            legacy_audio_clips: [
              {
                title: 'Synthetic audio',
                audio_url: `${origin}/fixtures/audio.wav`,
                duration: 62,
                tags: 'UI fixture',
              },
            ],
          },
        },
      })
    )
    await page.goto('/usage-logs/task')
    await waitForStablePage(page)
    const triggers = page
      .locator('[data-task-artifacts-trigger]')
      .filter({ visible: true })
    await expect(triggers).toHaveCount(2)
    await checkpoint(page, info, 'task-artifact-actions', ['TaskArtifactsCell'])
    await triggers.first().click()
    await expect(page.locator('[data-task-artifact] img')).toBeVisible()
    await checkpoint(page, info, 'task-image', ['TaskArtifactCard'])
    await page.keyboard.press('Escape')
    await triggers.last().click()
    await expect(page.locator('[data-task-audio-clip] audio')).toBeVisible()
    await checkpoint(page, info, 'task-audio', ['TaskAudioClipCard'])
  })

  test(`ticket attachment lightbox focus restoration ${variant}`, async ({
    page,
  }, info) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, { theme, locale })
    await page.route('**/api/tickets/1', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            ticket: {
              id: 1,
              title: 'Synthetic attachment ticket',
              status: 'open',
              category: 'billing',
              priority: 'normal',
              reply_count: 0,
              message_count: 1,
              last_reply_role: 'user',
              model_id: '',
              request_id: 'ui-ticket',
              created: now,
              updated: now,
              assignee_id: null,
              assigned_at: 0,
              assignee_name: '',
            },
            messages: [
              {
                id: 1,
                role: 'user',
                content: 'Synthetic attachment',
                created: now,
                images: ['/api/tickets/attachments/1'],
              },
            ],
          },
        },
      })
    )
    await page.route('**/api/tickets/attachments/1', (route) =>
      route.fulfill({ contentType: 'image/png', body: image })
    )
    await page.goto('/tickets/1')
    await waitForStablePage(page)
    const preview = page.locator('button:has(img[src^="blob:"])')
    await expect(preview).toBeVisible()
    await preview.click()
    const lightbox = page.getByRole('dialog')
    await expect(lightbox.locator('img')).toBeVisible()
    await page.keyboard.press('Tab')
    await expect(lightbox.getByRole('button')).toBeFocused()
    await checkpoint(page, info, 'ticket-image-lightbox', [
      'TicketImageLightbox',
    ])
    await page.keyboard.press('Escape')
    await expect(lightbox).toHaveCount(0)
    await expect(preview).toBeFocused()
  })
}
