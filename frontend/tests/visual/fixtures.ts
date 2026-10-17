import {
  expect,
  type ConsoleMessage,
  type Locator,
  type Page,
} from '@playwright/test'
import {
  nativeCoreFixture,
  type NativeFixtureOptions,
} from './native-core-fixture'

export type VisualTheme = 'light' | 'dark'
const FIXED_NOW = new Date('2026-07-27T12:00:00+08:00').getTime()
const DAY_MS = 86_400_000

export function visualDateKey(daysAgo: number): string {
  return new Date(FIXED_NOW - daysAgo * DAY_MS).toISOString().slice(0, 10)
}

interface StablePageOptions extends NativeFixtureOptions {
  theme: VisualTheme
  clockStepMs?: number
}

export async function configureStablePage(
  page: Page,
  { theme, clockStepMs = 1, ...options }: StablePageOptions
) {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.addInitScript(
    ({ fixedNow, selectedTheme, timeStepMs }) => {
      if (window !== window.top) return
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
    },
    {
      fixedNow: FIXED_NOW,
      selectedTheme: theme,
      timeStepMs: clockStepMs,
    }
  )

  return nativeCoreFixture(page, theme, 'USD', {
    locale: 'zh-CN',
    now: Math.floor(FIXED_NOW / 1000),
    ...options,
  })
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

export async function waitForStablePage(page: Page): Promise<void> {
  await page.locator('#app > :visible').first().waitFor({ state: 'visible' })
  expect(
    await page.evaluate(
      () => matchMedia('(prefers-reduced-motion: reduce)').matches
    )
  ).toBe(true)
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(260)
  await expect(page.locator('[data-error-retry]')).toHaveCount(0)
}

export async function freezeAndInspectHomeCanvas(page: Page): Promise<void> {
  const canvas = page.locator('canvas[role="img"]').first()
  await canvas.waitFor({ state: 'visible' })
  await expect(canvas).toHaveCSS('opacity', '1')

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
  await expect(navbar).not.toHaveCSS('background-color', 'rgba(0, 0, 0, 0)')
  await expect(navbar).not.toHaveCSS('box-shadow', 'none')

  await page.evaluate(() => window.scrollTo(0, 0))
  await expect(navbar).toHaveAttribute('data-scrolled', 'false')
  await expect(navbar).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)')
  await expect(navbar).toHaveCSS('box-shadow', 'none')
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
}

/**
 * Relative luminance of each matched element's background and of its text,
 * with the background composited from the element's own and its ancestors'
 * background colours (background images are ignored).
 */
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
