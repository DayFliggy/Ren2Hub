import { expect, test, type Page } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import { captureUI } from './ui-coverage'
import { pages, viewports } from './page-matrix'

test.describe.configure({ mode: 'parallel' })

async function publicAndSettingsFixtures(page: Page) {
  // Explicit contracts for enabled public documents and read-only integrations.
  const data: Record<string, unknown> = {
    '/api/about':
      '<h1>Ren2Hub</h1><p>Local regression document / 本地回归文档。</p>',
    '/api/privacy-policy':
      '<h1>Privacy / 隐私</h1><p>Synthetic test document.</p>',
    '/api/user-agreement':
      '<h1>Agreement / 协议</h1><p>Synthetic test document.</p>',
    '/api/custom-oauth-provider/': [],
    '/api/plugin/task': [],
    '/api/option/channel_affinity_cache': {
      enabled: false,
      total: 0,
      unknown: 0,
      cache_capacity: 1000,
      cache_algo: 'lru',
    },
    '/api/performance/logs': { enabled: false, file_count: 0, total_size: 0 },
    '/api/admin/tickets/agents': { items: [] },
  }
  await page.route('**/api/**', (route) => {
    const path = new URL(route.request().url()).pathname
    return route.request().method() === 'GET' && Object.hasOwn(data, path)
      ? route.fulfill({ json: { success: true, data: data[path] } })
      : route.fallback()
  })
}

for (const [size, viewport] of Object.entries(viewports)) {
  for (const theme of ['light', 'dark'] as const) {
    for (const locale of ['zh-CN', 'en'] as const) {
      for (const target of pages) {
        test(`page ${target.path} ${size}-${theme}-${locale}`, async ({
          page,
        }, info) => {
          await page.setViewportSize(viewport)
          await configureStablePage(page, {
            theme,
            locale,
            clockStepMs: 0,
            authenticated: !target.guest,
            setupInitialized: !target.setup,
            status: {
              // Display a fixed synthetic endpoint, not the isolated Vite
              // server's randomly allocated transport port.
              server_address: 'https://gateway.example.test',
              HeaderNavModules: JSON.stringify({
                pricing: true,
                rankings: true,
                about: true,
              }),
              user_agreement_enabled: true,
              privacy_policy_enabled: true,
            },
          })
          // Native select text can retain fallback-font metrics after swap.
          // This pixel matrix captures loaded-font layout; delayed-font and
          // startup behavior suites continue to use the real cold-load path.
          await page.route(
            (url) => url.pathname === '/src/main.ts' && url.search === '',
            (route) =>
              route.fulfill({
                contentType: 'application/javascript',
                body: `import '/src/styles/index.css';
const faces = await document.fonts.load('400 14px Ren2Inter', 'Preferred Hour');
if (!faces.length || faces.some((face) => face.status !== 'loaded'))
  throw new Error('Visual bootstrap: Ren2Inter failed to load');
await import('/src/main.ts?visual-fonts-ready');`,
              })
          )
          await publicAndSettingsFixtures(page)
          const pausedClock = target.path === '/ops' || target.path === '/'
          if (pausedClock) {
            const now = new Date('2026-07-27T12:00:00+08:00').getTime()
            await page.clock.install({ time: now - 1000 })
            await page.clock.pauseAt(now)
          }
          await page.goto(target.path)
          await expect
            .poll(
              () =>
                page.evaluate((view) => {
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
                      if (instance.type?.__file?.endsWith(`/${view}.vue`))
                        return true
                      instance = instance.parent as typeof instance
                    }
                  }
                  return false
                }, target.view),
              { timeout: 30_000 }
            )
            .toBe(true)
          await page.waitForLoadState('networkidle')
          if (target.path === '/') {
            // A fresh mobile visit shows this cue at 1400 ms and hides it
            // 4500 ms later. Capture all home variants at the same 6000 ms.
            const hint = page.locator('.hero-map-stage .hero-stage-hint')
            await expect(page.locator('canvas[role="img"]')).toHaveClass(
              /opacity-100/
            )
            await expect(hint).toHaveClass(/opacity-0/)
            await page.clock.runFor(1399)
            await expect(hint).toHaveClass(/opacity-0/)
            await page.clock.runFor(1)
            if (viewport.width < 1024) {
              await expect(hint).toHaveClass(/opacity-100/)
              await expect(hint).toHaveCSS('opacity', '1')
            } else await expect(hint).toHaveClass(/opacity-0/)
            await page.clock.runFor(4499)
            if (viewport.width < 1024)
              await expect(hint).toHaveClass(/opacity-100/)
            await page.clock.runFor(1)
            await expect(hint).toHaveClass(/opacity-0/)
            await page.clock.runFor(100)
          } else if (pausedClock) await page.clock.runFor(64)
          await waitForStablePage(page)
          await expect(page.locator('body')).not.toHaveText('')
          await assertNoHorizontalOverflow(page)
          await captureUI(page, info, 'page', { pausedClock, compare: true })
        })
      }
    }
  }
}
