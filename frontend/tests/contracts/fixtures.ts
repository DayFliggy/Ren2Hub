import { expect, type Page } from '@playwright/test'
import {
  nativeCoreFixture,
  type NativeFixtureOptions,
} from './native-core-fixture'

export type VisualTheme = 'light' | 'dark'
const FIXED_NOW = new Date('2026-07-27T12:00:00+08:00').getTime()
const fixtureByPage = new WeakMap<
  Page,
  Awaited<ReturnType<typeof nativeCoreFixture>>
>()

interface StablePageOptions extends NativeFixtureOptions {
  theme: VisualTheme
  clockStepMs?: number
}

function assertHealthy(page: Page) {
  const fixture = fixtureByPage.get(page)
  if (!fixture) throw new Error('Page fixture was not configured')
  expect(fixture.errors, '页面、网络及服务端错误').toEqual([])
  expect(fixture.unexpected, '未映射的业务请求').toEqual([])
  expect(fixture.missingMessages, '缺失翻译').toEqual([])
}

export async function configureStablePage(
  page: Page,
  { theme, clockStepMs = 1, ...options }: StablePageOptions
) {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.addInitScript(
    ({ fixedNow, selectedTheme, timeStepMs }) => {
      if (window !== window.top || location.protocol !== 'http:') return
      const NativeDate = Date
      let clockTick = 0
      const nextNow = () => fixedNow + clockTick++ * timeStepMs
      const FixedDate = new Proxy(NativeDate, {
        construct(target, args) {
          return Reflect.construct(target, args.length ? args : [nextNow()])
        },
        apply(target, thisArg, args) {
          return Reflect.apply(
            target,
            thisArg,
            args.length ? args : [nextNow()]
          )
        },
      })
      Object.defineProperty(FixedDate, 'now', { value: nextNow })
      Object.defineProperty(window, 'Date', { value: FixedDate })
      let seed = 0x2f6e2b1
      Math.random = () => {
        seed = (seed * 1_664_525 + 1_013_904_223) >>> 0
        return seed / 0x1_0000_0000
      }
      localStorage.setItem('ren2hub_theme_mode', selectedTheme)
      localStorage.setItem('ren2hub_locale', 'zh-CN')
      localStorage.setItem('ren2hub_sidebar_collapsed', 'false')
      sessionStorage.clear()
      history.scrollRestoration = 'manual'
      window.addEventListener('DOMContentLoaded', () => window.scrollTo(0, 0), {
        once: true,
      })
    },
    { fixedNow: FIXED_NOW, selectedTheme: theme, timeStepMs: clockStepMs }
  )

  // nativeCoreFixture owns the pageerror listener; all added checks share its errors.
  const fixture = await nativeCoreFixture(page, theme, 'USD', {
    locale: 'zh-CN',
    now: Math.floor(FIXED_NOW / 1000),
    ...options,
  })
  fixtureByPage.set(page, fixture)
  const origin = `http://127.0.0.1:${process.env.REN2HUB_UI_PORT}`
  await page.route('**/*', (route) => {
    const url = new URL(route.request().url())
    if (url.origin !== origin) {
      fixture.errors.push('外部请求: ' + url.origin)
      return route.abort('blockedbyclient')
    }
    return route.fallback()
  })
  await page.routeWebSocket('**/*', (socket) => {
    if (new URL(socket.url()).origin !== origin.replace('http:', 'ws:')) {
      fixture.errors.push('外部 WebSocket 请求')
      socket.close()
      return
    }
    socket.connectToServer()
  })
  page.on('response', (response) => {
    if (response.status() >= 500)
      fixture.errors.push(
        `HTTP ${response.status()}: ${new URL(response.url()).pathname}`
      )
  })
  page.on('requestfailed', (request) => {
    const failure = request.failure()?.errorText
    // Router navigation can cancel obsolete reads; failed bootstrap/network loads must surface.
    if (failure && failure !== 'net::ERR_ABORTED')
      fixture.errors.push(`${failure}: ${new URL(request.url()).pathname}`)
  })
  return fixture
}

export async function waitForStablePage(page: Page): Promise<void> {
  assertHealthy(page)
  await expect(page.locator('#app > :visible').first()).toBeVisible()
  expect(
    await page.evaluate(
      () => matchMedia('(prefers-reduced-motion: reduce)').matches
    )
  ).toBe(true)
  await page.evaluate(() => document.fonts.ready)
  await expect(
    page.locator('.page-enter-active, .page-leave-active')
  ).toHaveCount(0)
  await page.waitForFunction(() =>
    document
      .getAnimations()
      .every(
        (animation) =>
          animation.effect?.getTiming().iterations === Infinity ||
          animation.playState !== 'running'
      )
  )
  await expect(page.locator('[data-error-retry]')).toHaveCount(0)
  assertHealthy(page)
}

export async function assertNoHorizontalOverflow(page: Page): Promise<void> {
  const overflow = await page.evaluate(() => ({
    html:
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
    body: document.body.scrollWidth - document.body.clientWidth,
  }))
  expect(overflow.html).toBeLessThanOrEqual(1)
  expect(overflow.body).toBeLessThanOrEqual(1)
  assertHealthy(page)
}
