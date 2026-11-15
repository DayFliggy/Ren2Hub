import {
  expect,
  type Page,
  type ConsoleMessage,
  type Locator,
  type LocatorScreenshotOptions,
  type PageScreenshotOptions,
} from '@playwright/test'
import {
  nativeCoreFixture,
  type NativeFixtureOptions,
} from '../contracts/native-core-fixture'

import { createModelMarketFixture } from '../fixtures/modelMarket'

export type VisualTheme = 'light' | 'dark'
const FIXED_NOW = new Date('2026-07-27T12:00:00+08:00').getTime()
const fixtureByPage = new WeakMap<
  Page,
  Awaited<ReturnType<typeof nativeCoreFixture>>
>()

interface StablePageOptions extends NativeFixtureOptions {
  theme: VisualTheme
  clockStepMs?: number
  routeAwareAuth?: boolean
  expectedApiErrors?: Record<string, number[]>
  expectedFailedRequests?: RegExp[]
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
  {
    theme,
    clockStepMs = 1,
    routeAwareAuth = false,
    expectedApiErrors = {},
    expectedFailedRequests = [],
    ...options
  }: StablePageOptions
) {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.addInitScript(
    ({ fixedNow, selectedTheme, timeStepMs }) => {
      if (window !== window.top || location.protocol !== 'http:') return
      const nativeGetContext = HTMLCanvasElement.prototype.getContext
      Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
        configurable: true,
        value(
          this: HTMLCanvasElement,
          contextId: string,
          options?: CanvasRenderingContext2DSettings
        ) {
          if (contextId === '2d') {
            return Reflect.apply(nativeGetContext, this, [
              contextId,
              { ...options, willReadFrequently: true },
            ])
          }
          return Reflect.apply(nativeGetContext, this, [contextId, options])
        },
      })

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
    user: {
      username: 'visual.root',
      display_name: 'Visual Root',
      email: 'visual-root@ren2hub.dev',
      quota: 5201314,
      used_quota: 2985211,
      ...options.user,
    },
  })
  fixtureByPage.set(page, fixture)
  await page.route('**/api/user/model-catalog', (route) =>
    route.fulfill({ json: { success: true, data: createModelMarketFixture() } })
  )
  const hourlyRequests = [
    8, 12, 9, 7, 6, 11, 18, 27, 34, 41, 38, 45, 52, 49, 43, 57, 61, 54, 47, 39,
    32, 26, 21, 15,
  ]
  await page.route('**/api/home/metrics*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          available: true,
          generated_at: Math.floor(FIXED_NOW / 1000),
          requests_24h: hourlyRequests.reduce((sum, value) => sum + value, 0),
          hourly_requests: hourlyRequests,
        },
      },
    })
  )
  if (routeAwareAuth) {
    await page.route('**/api/user/auth/refresh*', (route) => {
      if (
        ['/sign-in', '/sign-up', '/forgot-password', '/reset'].includes(
          new URL(page.url()).pathname
        )
      )
        return route.fulfill({
          status: 401,
          json: {
            success: false,
            code: 'AUTH_UNAUTHORIZED',
            message: 'Authentication required',
          },
        })
      return route.fallback()
    })
  }
  const origin = `http://127.0.0.1:${process.env.PLAYWRIGHT_PORT ?? 4173}`
  // The backend writes this hint beside the Refresh Cookie at sign-in; a
  // fixture session that already exists must carry it too.
  if (options.authenticated !== false)
    await page
      .context()
      .addCookies([{ name: 'new_api_has_session', value: '1', url: origin }])
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
    if (
      response.status() >= 500 &&
      !expectedApiErrors[new URL(response.url()).pathname]?.includes(
        response.status()
      )
    )
      fixture.errors.push(
        `HTTP ${response.status()}: ${new URL(response.url()).pathname}`
      )
  })
  page.on('requestfailed', (request) => {
    const failure = request.failure()?.errorText
    // Router navigation can cancel obsolete reads; failed bootstrap/network loads must surface.
    if (
      failure &&
      failure !== 'net::ERR_ABORTED' &&
      !expectedFailedRequests.some((pattern) => pattern.test(request.url()))
    )
      fixture.errors.push(`${failure}: ${new URL(request.url()).pathname}`)
  })
  return fixture
}

export async function waitForStablePage(page: Page): Promise<void> {
  assertHealthy(page)
  // The first isolated Vite request compiles lazy route dependencies. This is
  // readiness, not a retry; runtime and fixture errors are still asserted.
  await expect(page.locator('#app > :visible').first()).toBeVisible({
    timeout: 30_000,
  })
  expect(
    await page.evaluate(
      () => matchMedia('(prefers-reduced-motion: reduce)').matches
    )
  ).toBe(true)
  await page.evaluate(() => document.fonts.ready)
  await expect(
    page.locator('.page-enter-active, .page-leave-active')
  ).toHaveCount(0)
  await expect
    .poll(() =>
      page.evaluate(() =>
        document
          .getAnimations()
          .every(
            (animation) =>
              animation.effect?.getTiming().iterations === Infinity ||
              animation.playState !== 'running'
          )
      )
    )
    .toBe(true)
  await expect(page.locator('[data-error-retry]')).toHaveCount(0)
  assertHealthy(page)
}

const captureScreenshots = process.env.PLAYWRIGHT_CAPTURE_SCREENSHOTS === 'true'

/** Writes review evidence only when screenshot capture is requested. */
export async function captureEvidence(
  target: Page | Locator,
  options: PageScreenshotOptions & LocatorScreenshotOptions
): Promise<void> {
  if (!captureScreenshots) return
  await target.screenshot(options)
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

export function visualDateKey(daysAgo: number): string {
  return new Date(FIXED_NOW - daysAgo * 86_400_000).toISOString().slice(0, 10)
}

export function isExpectedGuestRefreshConsoleMessage(
  page: Page,
  message: ConsoleMessage
): boolean {
  if (message.type() !== 'error' || !message.text().includes('401')) {
    return false
  }
  const pagePath = new URL(page.url()).pathname
  if (
    !['/sign-in', '/sign-up', '/forgot-password', '/reset'].includes(pagePath)
  )
    return false
  const sourceUrl = message.location().url
  return !sourceUrl || sourceUrl.includes('/api/user/auth/refresh')
}

export async function freezeAndInspectHomeCanvas(page: Page): Promise<void> {
  const canvas = page.locator('canvas[role="img"]').first()
  await canvas.waitFor({ state: 'visible' })
  await expect(canvas).toHaveCSS('opacity', '1')
  // The scene debounces viewport resize. Wait for the backing buffer to match
  // the new geometry before testing that reduced-motion frames stay still.
  await expect
    .poll(() =>
      canvas.evaluate((element) => {
        const target = element as HTMLCanvasElement
        const rect = target.getBoundingClientRect()
        const dpr = Math.min(devicePixelRatio || 1, 2)
        return (
          target.width === Math.floor(rect.width * dpr) &&
          target.height === Math.floor(rect.height * dpr)
        )
      })
    )
    .toBe(true)

  const pixels = await canvas.evaluate((element) => {
    const target = element as HTMLCanvasElement
    const context = target.getContext('2d')
    if (!context || target.width === 0 || target.height === 0) {
      return { distinct: 0, changed: 0, samples: 0, area: 0 }
    }

    const image = context.getImageData(0, 0, target.width, target.height)
    const corner = image.data.slice(0, 3)
    const stepX = Math.max(1, Math.floor(target.width / 96))
    const stepY = Math.max(1, Math.floor(target.height / 64))
    const colors = new Set<string>()
    let changed = 0
    let samples = 0

    for (let y = 0; y < target.height; y += stepY) {
      for (let x = 0; x < target.width; x += stepX) {
        const offset = (y * target.width + x) * 4
        const red = image.data[offset]
        const green = image.data[offset + 1]
        const blue = image.data[offset + 2]
        colors.add(`${red},${green},${blue}`)
        const distance =
          Math.abs(red - corner[0]!) +
          Math.abs(green - corner[1]!) +
          Math.abs(blue - corner[2]!)
        if (distance > 24) changed++
        samples++
      }
    }

    const rect = target.getBoundingClientRect()
    return {
      distinct: colors.size,
      changed,
      samples,
      area: rect.width * rect.height,
    }
  })

  expect(pixels.distinct).toBeGreaterThan(24)
  // Fixed-size map landmarks occupy a smaller fraction of a 4K canvas. Keep
  // the required painted area at the 2560px reference, rather than requiring
  // additional decoration merely because the background has more empty pixels.
  const referenceArea = Math.min(pixels.area, 2560 * 1440)
  expect((pixels.changed / pixels.samples) * pixels.area).toBeGreaterThan(
    referenceArea * 0.01
  )

  const frameChecksum = () =>
    canvas.evaluate((element) => {
      const target = element as HTMLCanvasElement
      const context = target.getContext('2d')
      if (!context || target.width === 0 || target.height === 0) return 0

      const image = context.getImageData(0, 0, target.width, target.height)
      const stepX = Math.max(1, Math.floor(target.width / 96))
      const stepY = Math.max(1, Math.floor(target.height / 64))
      let checksum = 2_166_136_261
      for (let y = 0; y < target.height; y += stepY) {
        for (let x = 0; x < target.width; x += stepX) {
          const offset = (y * target.width + x) * 4
          for (let channel = 0; channel < 4; channel++) {
            checksum = Math.imul(
              checksum ^ image.data[offset + channel]!,
              16_777_619
            )
          }
        }
      }
      return checksum >>> 0
    })

  const initialFrame = await frameChecksum()
  await page.waitForTimeout(100)
  expect(await frameChecksum()).toBe(initialFrame)
  await page.evaluate(() => window.dispatchEvent(new Event('pagehide')))
}

export async function assertHomeNavbarInitialState(page: Page): Promise<void> {
  const navbar = page.locator('.app-navbar')
  await expect(navbar).toHaveAttribute('data-scrolled', 'false')
  await expect(navbar).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)')
  await expect(navbar).toHaveCSS('box-shadow', 'none')
  await expect(page.locator('.home-brand')).toHaveCSS(
    'background-color',
    'rgba(0, 0, 0, 0)'
  )
  await expect(page.locator('.home-brand')).toHaveCSS('box-shadow', 'none')
  expect(
    await page.locator('.home-brand').evaluate((element) => ({
      before: getComputedStyle(element, '::before').display,
      after: getComputedStyle(element, '::after').display,
    }))
  ).toEqual({ before: 'none', after: 'none' })

  await page.evaluate(() => window.scrollTo(0, 80))
  await expect(navbar).toHaveAttribute('data-scrolled', 'true')
  // Glass is a gradient surface; its computed background-color can be transparent.
  const surface = await navbar.evaluate((element) => {
    const dark = document.documentElement.classList.contains('dark')
    const glass =
      (CSS.supports('backdrop-filter', 'blur(1px)') ||
        CSS.supports('-webkit-backdrop-filter', 'blur(1px)')) &&
      !matchMedia('(prefers-reduced-transparency: reduce)').matches
    const token = dark
      ? glass
        ? '--nav-surface'
        : '--nav-solid'
      : '--surface-raised'
    const probe = document.createElement('span')
    probe.style.display = 'none'
    probe.style.background = `var(${token})`
    element.append(probe)
    const style = getComputedStyle(probe)
    const paint = {
      color: style.backgroundColor,
      image: style.backgroundImage,
    }
    probe.remove()
    return paint
  })
  await expect(navbar).toHaveCSS('background-color', surface.color)
  await expect(navbar).toHaveCSS('background-image', surface.image)
  await expect(navbar).not.toHaveCSS('box-shadow', 'none')

  await page.evaluate(() => window.scrollTo(0, 0))
  await expect(navbar).toHaveAttribute('data-scrolled', 'false')
  await expect(navbar).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)')
  await expect(navbar).toHaveCSS('box-shadow', 'none')
}

export async function measureLuminance(
  locator: Locator
): Promise<Array<{ background: number; text: number }>> {
  const measured = await locator.evaluateAll((elements) => {
    const context = document.createElement('canvas').getContext('2d')!
    const paint = (colors: string[]) => {
      context.clearRect(0, 0, 1, 1)
      for (const color of colors) {
        context.fillStyle = color
        context.fillRect(0, 0, 1, 1)
      }
      return [...context.getImageData(0, 0, 1, 1).data.slice(0, 3)]
    }
    const luminance = (rgb: number[]) => {
      const [r, g, b] = rgb.map((channel) => {
        const value = channel / 255
        return value <= 0.04045
          ? value / 12.92
          : ((value + 0.055) / 1.055) ** 2.4
      })
      return 0.2126 * r! + 0.7152 * g! + 0.0722 * b!
    }
    const page = getComputedStyle(document.documentElement)
      .getPropertyValue('--page-background')
      .trim()
    return elements.map((element) => {
      const layers: string[] = []
      for (
        let node: Element | null = element;
        node;
        node = node.parentElement
      ) {
        layers.unshift(getComputedStyle(node).backgroundColor)
      }
      const background = paint([page, ...layers])
      const text = paint([
        `rgb(${background.join(',')})`,
        getComputedStyle(element).color,
      ])
      return { background: luminance(background), text: luminance(text) }
    })
  })
  expect(measured.length, 'no elements to measure').toBeGreaterThan(0)
  return measured
}

/** WCAG contrast ratio between two relative luminances. */
export function contrastRatio(first: number, second: number): number {
  return (Math.max(first, second) + 0.05) / (Math.min(first, second) + 0.05)
}

function linearChannels(rgb: number[]): [number, number, number] {
  return rgb.map((channel) => {
    const value = channel / 255
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
  }) as [number, number, number]
}

export function relativeLuminance(rgb: number[]): number {
  const [r, g, b] = linearChannels(rgb)
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

/** OKLab distance ×100; about 2 is the smallest difference people notice. */
export function oklabDistance(first: number[], second: number[]): number {
  const oklab = (rgb: number[]) => {
    const [r, g, b] = linearChannels(rgb)
    const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b)
    const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b)
    const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b)
    return [
      0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
      1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s,
      0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s,
    ]
  }
  const [a, b] = [oklab(first), oklab(second)]
  return Math.hypot(a[0]! - b[0]!, a[1]! - b[1]!, a[2]! - b[2]!) * 100
}

/** Lowest text contrast among the matched elements. */
export async function minTextContrast(locator: Locator): Promise<number> {
  const measured = await measureLuminance(locator)
  return Math.min(
    ...measured.map(({ background, text }) => contrastRatio(background, text))
  )
}

export async function assertInteractiveCentersVisible(
  page: Page
): Promise<void> {
  const covered = await page.evaluate(() => {
    const selector =
      'button:not([disabled]), a[href], input:not([type="hidden"]):not([disabled]), textarea:not([disabled]), select:not([disabled]), [role="button"]:not([aria-disabled="true"])'
    return Array.from(document.querySelectorAll<HTMLElement>(selector))
      .filter((element) => {
        const details = element.closest('details')
        if (details && !details.open) return false
        const rect = element.getBoundingClientRect()
        const style = getComputedStyle(element)
        return (
          rect.width > 4 &&
          rect.height > 4 &&
          rect.bottom > 0 &&
          rect.right > 0 &&
          rect.top < innerHeight &&
          rect.left < innerWidth &&
          style.visibility !== 'hidden' &&
          style.display !== 'none' &&
          Number(style.opacity) > 0.05
        )
      })
      .flatMap((element) => {
        const rect = element.getBoundingClientRect()
        const x = Math.min(
          innerWidth - 1,
          Math.max(0, rect.left + rect.width / 2)
        )
        const y = Math.min(
          innerHeight - 1,
          Math.max(0, rect.top + rect.height / 2)
        )

        let ancestor = element.parentElement
        while (ancestor) {
          const ancestorStyle = getComputedStyle(ancestor)
          const ancestorRect = ancestor.getBoundingClientRect()
          const clipsX = ['auto', 'scroll', 'hidden', 'clip'].includes(
            ancestorStyle.overflowX
          )
          const clipsY = ['auto', 'scroll', 'hidden', 'clip'].includes(
            ancestorStyle.overflowY
          )
          if (
            (clipsX && (x < ancestorRect.left || x > ancestorRect.right)) ||
            (clipsY && (y < ancestorRect.top || y > ancestorRect.bottom))
          ) {
            return []
          }
          ancestor = ancestor.parentElement
        }

        const hit = document.elementFromPoint(x, y)
        if (
          !hit ||
          element === hit ||
          element.contains(hit) ||
          hit.contains(element)
        ) {
          return []
        }
        return [
          {
            control:
              element.getAttribute('aria-label') ||
              element.textContent?.trim().slice(0, 40) ||
              element.tagName,
            covering: `${hit.tagName}.${(hit as HTMLElement).className}`.slice(
              0,
              120
            ),
          },
        ]
      })
      .slice(0, 5)
  })

  expect(covered).toEqual([])
}
