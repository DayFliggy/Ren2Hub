import { expect, test } from '@playwright/test'

import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage as configureNativePage,
  freezeAndInspectHomeCanvas,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

async function configureStablePage(
  page: import('@playwright/test').Page,
  options: Parameters<typeof configureNativePage>[1]
) {
  await configureNativePage(page, options)
  // Keep the measured uptime fixture used by the historical geometry checks.
  await page.route('**/api/uptime/status', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [{ monitors: [{ uptime: 0.9995, status: 1 }] }],
      },
    })
  )
}

const heroViewports = [
  { name: '4k', width: 3840, height: 2160, titleSize: 102.2 },
  { name: 'ultrawide-monitor', width: 3440, height: 1440, titleSize: 102.2 },
  { name: 'ultra-wide', width: 2560, height: 1264, titleSize: 102.2 },
  { name: 'short-ultra-wide', width: 2560, height: 768, titleSize: 58.4 },
  { name: 'wide-desktop', width: 1920, height: 1080, titleSize: 77.87 },
  { name: 'desktop', width: 1440, height: 900, titleSize: 58.4 },
  { name: 'short-desktop', width: 1366, height: 768, titleSize: 58.4 },
  { name: 'small-desktop', width: 1024, height: 768, titleSize: 54.4 },
  { name: 'portrait-tablet', width: 768, height: 1024, titleSize: 48 },
  { name: 'large-phone', width: 430, height: 932, titleSize: 40 },
  { name: 'phone', width: 390, height: 844, titleSize: 40 },
  { name: 'short-phone', width: 360, height: 640, titleSize: 29.6 },
  { name: 'narrow-phone', width: 320, height: 720, titleSize: 34.4 },
] as const

interface ElementBounds {
  top: number
  right: number
  bottom: number
  left: number
  width: number
  height: number
}

function overlapArea(a: ElementBounds, b: ElementBounds): number {
  const width = Math.max(
    0,
    Math.min(a.right, b.right) - Math.max(a.left, b.left)
  )
  const height = Math.max(
    0,
    Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top)
  )
  return width * height
}

async function inspectHeroGeometry(page: import('@playwright/test').Page) {
  return page.evaluate(() => {
    const bounds = (selector: string) => {
      const element = document.querySelector<HTMLElement>(selector)
      if (!element) throw new Error(`Missing Hero element: ${selector}`)
      const rect = element.getBoundingClientRect()
      return {
        top: rect.top,
        right: rect.right,
        bottom: rect.bottom,
        left: rect.left,
        width: rect.width,
        height: rect.height,
      }
    }

    const hero = bounds('#hero-immersive-stage')
    const focus = Array.from(
      document.querySelectorAll<HTMLElement>('[data-hero-map-focus]')
    )
      .filter((element) => {
        const rect = element.getBoundingClientRect()
        return (
          rect.width > 0 &&
          rect.height > 0 &&
          getComputedStyle(element).display !== 'none'
        )
      })
      .map((element) => {
        const rect = element.getBoundingClientRect()
        return {
          top: rect.top,
          right: rect.right,
          bottom: rect.bottom,
          left: rect.left,
          width: rect.width,
          height: rect.height,
        }
      })[0]
    if (!focus) throw new Error('Visible Hero map focus region missing')

    const canvas =
      document.querySelector<HTMLCanvasElement>('canvas[role="img"]')
    const context = canvas?.getContext('2d')
    if (!canvas || !context) throw new Error('Hero canvas unavailable')
    const canvasRect = canvas.getBoundingClientRect()
    const scaleX = canvas.width / canvasRect.width
    const scaleY = canvas.height / canvasRect.height
    const sampleX = Math.max(
      0,
      Math.floor((focus.left - canvasRect.left) * scaleX)
    )
    const sampleY = Math.max(
      0,
      Math.floor((focus.top - canvasRect.top) * scaleY)
    )
    const sampleWidth = Math.min(
      canvas.width - sampleX,
      Math.max(1, Math.floor(focus.width * scaleX))
    )
    const sampleHeight = Math.min(
      canvas.height - sampleY,
      Math.max(1, Math.floor(focus.height * scaleY))
    )
    const pixels = context.getImageData(
      sampleX,
      sampleY,
      sampleWidth,
      sampleHeight
    ).data
    const colors = new Set<string>()
    let minLuma = 255
    let maxLuma = 0
    const sampleStride = Math.max(4, Math.floor(pixels.length / 16_000 / 4) * 4)
    for (let offset = 0; offset < pixels.length; offset += sampleStride) {
      const red = pixels[offset]!
      const green = pixels[offset + 1]!
      const blue = pixels[offset + 2]!
      colors.add(`${red >> 3},${green >> 3},${blue >> 3}`)
      const luma = 0.2126 * red + 0.7152 * green + 0.0722 * blue
      minLuma = Math.min(minLuma, luma)
      maxLuma = Math.max(maxLuma, luma)
    }

    const nav = document.querySelector<HTMLElement>('.app-navbar nav')
    if (!nav) throw new Error('Home navigation missing')
    const navStyle = getComputedStyle(nav)
    const navRect = nav.getBoundingClientRect()
    const rail = document.querySelector<HTMLElement>('.signal-console__rail')
    const railVisible =
      rail !== null && getComputedStyle(rail).display !== 'none'
    const progressDots = document.querySelector<HTMLElement>(
      '[data-home-progress-dots]'
    )
    const progressDotsVisible =
      progressDots !== null && getComputedStyle(progressDots).display !== 'none'
    const ticker = document.querySelector<HTMLElement>(
      '.signal-console__ticker'
    )
    const tickerVisible =
      ticker !== null && getComputedStyle(ticker).display !== 'none'

    return {
      viewportHeight: innerHeight,
      hero,
      focus,
      copy: bounds('.hero-copy-glow'),
      title: bounds('.hero-title'),
      actions: bounds('.hero-actions'),
      primaryCta: bounds('.hero-cta-primary'),
      integration: bounds('.hero-integration-boundary'),
      ticker: tickerVisible ? bounds('.signal-console__ticker') : null,
      titleSize: Number.parseFloat(
        getComputedStyle(document.querySelector('.hero-title')!).fontSize
      ),
      navWidth: navRect.width,
      navMaxWidth: Number.parseFloat(navStyle.maxWidth),
      navContent: {
        left: navRect.left + Number.parseFloat(navStyle.paddingLeft),
        right: navRect.right - Number.parseFloat(navStyle.paddingRight),
      },
      brand: bounds('.home-brand'),
      signalConsole: railVisible ? bounds('.signal-console') : null,
      truncatedTelemetry: railVisible
        ? Array.from(
            document.querySelectorAll<HTMLElement>(
              '.signal-console__telemetry-value, .signal-console__origin-value'
            )
          )
            .filter((element) => element.scrollWidth > element.clientWidth + 1)
            .map((element) => element.textContent?.trim() ?? '')
        : [],
      progressDots: progressDotsVisible
        ? bounds('[data-home-progress-dots]')
        : null,
      progressDotTargets: progressDotsVisible
        ? Array.from(
            document.querySelectorAll<HTMLElement>('[data-home-progress-dot]')
          ).map((element) => {
            const rect = element.getBoundingClientRect()
            return { width: rect.width, height: rect.height }
          })
        : [],
      focusPixels: {
        distinct: colors.size,
        contrast: maxLuma - minLuma,
      },
    }
  })
}

for (const theme of ['light', 'dark'] as VisualTheme[]) {
  for (const viewport of heroViewports) {
    test(`${theme} ${viewport.name} Hero geometry`, async ({ page }) => {
      await page.setViewportSize(viewport)
      await configureStablePage(page, { theme, authenticated: true })
      await page.goto('/', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)
      await freezeAndInspectHomeCanvas(page)

      const geometry = await inspectHeroGeometry(page)
      expect(
        Math.abs(geometry.hero.height - geometry.viewportHeight)
      ).toBeLessThanOrEqual(1)
      expect(geometry.titleSize).toBeCloseTo(viewport.titleSize, 1)
      expect(geometry.primaryCta.height).toBeGreaterThanOrEqual(44)
      expect(geometry.focusPixels.distinct).toBeGreaterThan(8)
      expect(geometry.focusPixels.contrast).toBeGreaterThan(18)

      for (const bounds of [
        geometry.focus,
        geometry.copy,
        geometry.title,
        geometry.actions,
        geometry.integration,
      ]) {
        expect(bounds.left).toBeGreaterThanOrEqual(geometry.hero.left - 1)
        expect(bounds.right).toBeLessThanOrEqual(geometry.hero.right + 1)
        expect(bounds.top).toBeGreaterThanOrEqual(geometry.hero.top - 1)
        expect(bounds.bottom).toBeLessThanOrEqual(geometry.hero.bottom + 1)
      }

      expect(overlapArea(geometry.focus, geometry.copy)).toBe(0)
      expect(overlapArea(geometry.actions, geometry.integration)).toBe(0)
      if (geometry.ticker) {
        expect(overlapArea(geometry.ticker, geometry.integration)).toBe(0)
        expect(geometry.ticker.bottom).toBeLessThanOrEqual(
          geometry.hero.bottom + 1
        )
      }
      if (viewport.width >= 1280 && viewport.width <= 1440) {
        expect(geometry.navMaxWidth).toBeCloseTo(1280, 0)
        expect(geometry.navWidth).toBeCloseTo(1280, 0)
      } else if (viewport.width > 1440) {
        // Large displays retain the standard desktop proportions; the copy
        // grows with its container, while exceptionally wide screens are capped.
        expect(geometry.navWidth).toBeGreaterThanOrEqual(
          Math.min(viewport.width * 0.85, 2560)
        )
        expect(geometry.navWidth).toBeLessThanOrEqual(2560)
        expect(geometry.copy.width).toBeGreaterThan(800)
      }
      if (viewport.width >= 1024) {
        // One container: the brand, the signal rail and the Hero copy share
        // the navbar's content edges, and HUD rows never end in an ellipsis.
        const { navContent } = geometry
        expect(
          Math.abs(geometry.brand.left - navContent.left)
        ).toBeLessThanOrEqual(1)
        expect(geometry.signalConsole).not.toBeNull()
        expect(
          Math.abs(geometry.signalConsole!.left - navContent.left)
        ).toBeLessThanOrEqual(1)
        expect(
          Math.abs(geometry.copy.right - navContent.right)
        ).toBeLessThanOrEqual(1)
        expect(geometry.truncatedTelemetry).toEqual([])
      }
      if (geometry.progressDots) {
        expect(overlapArea(geometry.progressDots, geometry.copy)).toBe(0)
        for (const target of geometry.progressDotTargets) {
          expect(target.width).toBeGreaterThanOrEqual(24)
          expect(target.height).toBeGreaterThanOrEqual(24)
        }
      }

      await assertInteractiveCentersVisible(page)
      await assertNoHorizontalOverflow(page)

      const runtime = page.locator('#home-runtime')
      await runtime.scrollIntoViewIfNeeded()
      await expect(page.locator('[data-home-request-total]')).toHaveText('752')
      await expect(page.locator('.runtime-trend polyline')).toHaveAttribute(
        'points',
        /,/
      )
      const runtimeBounds = await runtime.evaluate((section) => {
        const bounds = (selector: string) => {
          const element = section.querySelector<HTMLElement>(selector)
          if (!element) throw new Error(`Missing runtime element: ${selector}`)
          const rect = element.getBoundingClientRect()
          return {
            top: rect.top,
            right: rect.right,
            bottom: rect.bottom,
            left: rect.left,
            width: rect.width,
            height: rect.height,
          }
        }
        const overflow = (selector: string) => {
          const element = section.querySelector<HTMLElement>(selector)
          if (!element) throw new Error(`Missing runtime element: ${selector}`)
          return element.scrollWidth - element.clientWidth
        }
        const rect = section.getBoundingClientRect()
        return {
          section: {
            top: rect.top,
            right: rect.right,
            bottom: rect.bottom,
            left: rect.left,
          },
          uptime: bounds('.runtime-ledger-panel--uptime'),
          requests: bounds('.runtime-ledger-panel--requests'),
          uptimeHeading: bounds(
            '.runtime-ledger-panel--uptime .runtime-ledger-heading'
          ),
          clock: bounds('.runtime-clock'),
          availability: bounds('.runtime-availability'),
          total: bounds('[data-home-request-total]'),
          caption: bounds('.runtime-request-caption'),
          trend: bounds('.runtime-trend'),
          totalOverflow: overflow('[data-home-request-total]'),
          trendOverflow: overflow('.runtime-trend'),
          uptimeHeadingOverflow: overflow(
            '.runtime-ledger-panel--uptime .runtime-ledger-heading'
          ),
        }
      })
      for (const bounds of [
        runtimeBounds.uptime,
        runtimeBounds.requests,
        runtimeBounds.uptimeHeading,
        runtimeBounds.clock,
        runtimeBounds.availability,
        runtimeBounds.total,
        runtimeBounds.caption,
        runtimeBounds.trend,
      ]) {
        expect(bounds.left).toBeGreaterThanOrEqual(
          runtimeBounds.section.left - 1
        )
        expect(bounds.right).toBeLessThanOrEqual(
          runtimeBounds.section.right + 1
        )
        expect(bounds.top).toBeGreaterThanOrEqual(runtimeBounds.section.top - 1)
        expect(bounds.bottom).toBeLessThanOrEqual(
          runtimeBounds.section.bottom + 1
        )
      }
      expect(runtimeBounds.totalOverflow).toBeLessThanOrEqual(1)
      expect(runtimeBounds.trendOverflow).toBeLessThanOrEqual(1)
      expect(runtimeBounds.uptimeHeadingOverflow).toBeLessThanOrEqual(1)
      expect(overlapArea(runtimeBounds.total, runtimeBounds.trend)).toBe(0)
      expect(overlapArea(runtimeBounds.caption, runtimeBounds.trend)).toBe(0)
      if (viewport.width > 900) {
        expect(runtimeBounds.uptime.right).toBeLessThanOrEqual(
          runtimeBounds.requests.left + 1
        )
        expect(
          Math.abs(runtimeBounds.uptime.top - runtimeBounds.requests.top)
        ).toBeLessThanOrEqual(1)
      } else {
        expect(runtimeBounds.uptime.bottom).toBeLessThanOrEqual(
          runtimeBounds.requests.top + 1
        )
      }
      await assertNoHorizontalOverflow(page)
    })
  }
}

for (const theme of ['light', 'dark'] as VisualTheme[]) {
  for (const viewport of ['desktop', 'mobile'] as const) {
    test(`${theme} ${viewport} home showcase`, async ({ page }) => {
      await page.setViewportSize(
        viewport === 'desktop'
          ? { width: 1440, height: 900 }
          : { width: 390, height: 844 }
      )
      await configureStablePage(page, { theme, authenticated: true })
      await page.goto('/', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)
      await freezeAndInspectHomeCanvas(page)

      const heroMetrics = await page
        .locator('#hero-immersive-stage')
        .evaluate((element) => {
          const bounds = element.getBoundingClientRect()
          return {
            height: bounds.height,
            minHeight: Number.parseFloat(getComputedStyle(element).minHeight),
            viewportHeight: window.innerHeight,
          }
        })
      expect(heroMetrics.minHeight).toBeCloseTo(heroMetrics.viewportHeight, 0)
      expect(heroMetrics.height).toBeGreaterThanOrEqual(
        heroMetrics.viewportHeight
      )

      await assertInteractiveCentersVisible(page)
      await expect(page.locator('html')).toHaveClass(/hero-scrollbar-hidden/)
      expect(
        await page.evaluate(
          () => getComputedStyle(document.documentElement).scrollbarWidth
        )
      ).toBe('none')

      const showcase = page.locator('.home-showcase')
      await showcase.scrollIntoViewIfNeeded()
      await expect(showcase).toBeVisible()
      await expect(page.locator('[data-home-request-total]')).toHaveText('752')
      await expect(page.locator('.runtime-trend polyline')).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await expect(page.locator('.scroll-activity-indicator')).toHaveCount(0)
      await expect(page.locator('[data-home-progress-dots]')).toHaveCount(1)
      await expect(page.locator('[data-home-progress-dot]')).toHaveCount(2)
      if (viewport === 'desktop') {
        await expect(page.locator('[data-home-progress-dots]')).toBeVisible()
        expect(
          await page
            .locator('[data-home-progress-dot][aria-current="location"] span')
            .evaluate((element) => getComputedStyle(element).animationName)
        ).toBe('none')
      } else {
        await expect(page.locator('[data-home-progress-dots]')).toBeHidden()
      }

      const pageLevelRadii = await page
        .locator(
          '.landing-shell, #hero-immersive-stage, .home-showcase, .home-band'
        )
        .evaluateAll((elements) =>
          elements.map(
            (element) => getComputedStyle(element).borderTopLeftRadius
          )
        )
      expect(pageLevelRadii.every((radius) => radius === '0px')).toBe(true)

      const displayFont = await page
        .locator('.hero-title')
        .first()
        .evaluate((element) => getComputedStyle(element).fontFamily)
      const expectedFonts =
        theme === 'light'
          ? {
              display: 'Ren2HomeTime',
              stamp: 'Ren2HomeStamp',
              hand: 'Ren2HomeSketch',
              loaded: ['Ren2HomeTime', 'Ren2HomeSketch', 'Ren2HomeStamp'],
            }
          : {
              display: 'Ren2HomeSerif',
              stamp: 'Ren2JetBrainsMono',
              hand: 'Ren2JetBrainsMono',
              loaded: ['Ren2HomeSerif', 'Ren2JetBrainsMono', 'Ren2Inter'],
            }
      expect(displayFont).toContain(expectedFonts.display)
      expect(
        await page.evaluate(
          (font) => document.fonts.check(`16px ${font}`),
          expectedFonts.display
        )
      ).toBe(true)
      expect(
        await page.evaluate(
          (fonts) =>
            fonts.every((name) => document.fonts.check(`16px ${name}`)),
          expectedFonts.loaded
        )
      ).toBe(true)

      expect(
        await page
          .locator('.runtime-flap')
          .first()
          .evaluate((element) => getComputedStyle(element).fontFamily)
      ).toContain(expectedFonts.stamp)
      expect(
        await page
          .locator('.runtime-clock-group small')
          .first()
          .evaluate((element) => getComputedStyle(element).fontFamily)
      ).toContain(expectedFonts.hand)
      if (theme === 'dark') {
        await expect(page.locator('.runtime-ledger-panel')).toHaveCount(2)
        await expect(page.locator('.runtime-ledger-panel').first()).toHaveCSS(
          'border-radius',
          '16px'
        )
        await expect(
          page.locator('.runtime-ledger-panel').first()
        ).not.toHaveCSS('box-shadow', 'none')
        await expect(page.locator('.runtime-band')).toHaveCSS(
          'background-image',
          'none'
        )
      }

      expect(
        await page
          .locator('.runtime-flap')
          .first()
          .evaluate((element) => getComputedStyle(element).animationName)
      ).toBe('none')
    })
  }
}

test('home runtime workbench remains self-contained', async ({ page }) => {
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  await expect(page.locator('.runtime-clock-group')).toHaveCount(4)
  await expect(page.locator('.runtime-flap')).toHaveCount(9)
  await expect(page.locator('.runtime-availability-bar')).toHaveCount(7)
  await expect(page.locator('.runtime-availability strong')).toContainText(
    '99.95%'
  )
  await page.locator('#home-runtime').scrollIntoViewIfNeeded()
  await expect(page.locator('[data-home-request-total]')).toHaveText('752')
  await expect(page.locator('.runtime-trend polyline')).toHaveCount(1)
  await expect(page.locator('[data-home-channel-exchange]')).toHaveCount(0)
  await expect(page.locator('[data-home-token-routing]')).toHaveCount(0)
  await expect(page.getByText('渠道供应，也能自由买卖')).toHaveCount(0)
  await expect(page.getByText('每一枚令牌，都有自己的路由表')).toHaveCount(0)
})

// The metric row split does not depend on the theme.
for (const [theme, viewport] of [
  ['light', { width: 1440, height: 900 }],
  ['dark', { width: 390, height: 844 }],
] as const) {
  test(`${theme} ${viewport.width} availability is omitted without uptime data`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport)
    await configureStablePage(page, { theme, authenticated: true })
    await page.route('**/api/uptime/status', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, message: '', data: [] }),
      })
    )
    await page.goto('/', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const metrics = page.locator('.hero-metrics > div')
    await expect(metrics).toHaveCount(2)
    await expect(page.locator('.hero-metrics')).not.toContainText('可用率')
    const widths = await metrics.evaluateAll((items) =>
      items.map((item) => item.getBoundingClientRect().width)
    )
    if (viewport.width < 1024) {
      // The remaining items split the mobile row evenly.
      expect(Math.abs(widths[0]! - widths[1]!)).toBeLessThanOrEqual(1)
    }

    await page.locator('#home-runtime').scrollIntoViewIfNeeded()
    await expect(page.locator('.runtime-clock-group')).toHaveCount(4)
    await expect(page.locator('.runtime-availability')).toHaveCount(0)
  })
}

test('zero request trend uses a floating wave placeholder', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.route('**/api/home/metrics*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        success: true,
        message: '',
        data: {
          available: true,
          requests_24h: 0,
          hourly_requests: Array(24).fill(0),
          generated_at: 1_785_103_200,
        },
      }),
    })
  )
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
  await page.locator('#home-runtime').scrollIntoViewIfNeeded()

  await expect(page.locator('[data-home-request-total]')).toHaveText('0')
  await expect(page.locator('.runtime-trend polyline')).toHaveCount(0)
  await expect(page.locator('[data-zero-trend-wave]')).toHaveCount(1)
  await expect(page.locator('[data-zero-trend-wave] path')).toHaveAttribute(
    'd',
    /C/
  )
  await expect(page.locator('.runtime-trend-placeholder')).toHaveCount(0)

  const styles = await page.evaluate(() => {
    const wave = document.querySelector<SVGSVGElement>('[data-zero-trend-wave]')
    const path = wave?.querySelector('path')
    if (!wave || !path) {
      throw new Error('Zero request trend wave missing')
    }
    return {
      animationName: getComputedStyle(wave).animationName,
      stroke: getComputedStyle(path).stroke,
      opacity: getComputedStyle(wave).opacity,
    }
  })

  expect(styles.animationName).toBe('none')
  expect(styles.stroke).not.toBe('none')
  expect(Number(styles.opacity)).toBeGreaterThan(0)
  await assertNoHorizontalOverflow(page)
})

async function showZeroRequests(page: import('@playwright/test').Page) {
  await page.route('**/api/home/metrics*', (route) =>
    route.fulfill({
      json: {
        success: true,
        message: '',
        data: {
          available: true,
          requests_24h: 0,
          hourly_requests: Array(24).fill(0),
          generated_at: 1_785_103_200,
        },
      },
    })
  )
}

// Chromium can run an endless transform or opacity animation of an HTML box on
// the compositor. Any other property, or any animation of a shape inside an
// SVG (which Chromium does not composite), makes the main thread restyle and
// repaint on every frame for as long as the page is open. This reads the
// keyframes, not the compositor, so it guards the properties only; the named
// animations must be found, so an empty page cannot pass.
const homeEndlessAnimations = [
  'chip-breathe',
  'cta-sheen-move',
  'home-progress-breathe',
  'home-progress-glow',
  'runtime-trend-wave-float',
]

for (const theme of ['light', 'dark'] as VisualTheme[]) {
  test(`${theme} home's endless animations change only transform and opacity`, async ({
    page,
  }) => {
    await configureStablePage(page, { theme, authenticated: true })
    await showZeroRequests(page)
    await page.emulateMedia({ reducedMotion: 'no-preference' })
    await page.goto('/', { waitUntil: 'domcontentloaded' })
    await page.locator('#app > *').first().waitFor({ state: 'visible' })
    await expect(page.locator('.chip-active').first()).toBeVisible()
    // The wave itself keeps moving, so scroll its band into view.
    await page.locator('#home-runtime').scrollIntoViewIfNeeded()
    await expect(page.locator('[data-zero-trend-wave]')).toBeVisible()

    const endless = await page.evaluate(() =>
      document.getAnimations().flatMap((animation) => {
        const effect = animation.effect as KeyframeEffect | null
        if (!effect || effect.getTiming().iterations !== Infinity) return []
        const properties = new Set(
          effect.getKeyframes().flatMap((frame) => Object.keys(frame))
        )
        for (const key of ['offset', 'computedOffset', 'easing', 'composite'])
          properties.delete(key)
        const paintsOnMain = [...properties].filter(
          (property) =>
            !['transform', 'translate', 'scale', 'rotate', 'opacity'].includes(
              property
            )
        )
        const target = effect.target
        const svgShape =
          target instanceof SVGElement && target.ownerSVGElement !== null
        // Scoped component styles suffix their keyframes with the scope id.
        const name = (
          (animation as CSSAnimation).animationName ?? 'script animation'
        ).replace(/-[0-9a-f]{8}$/, '')
        const problem = svgShape ? 'SVG shape' : paintsOnMain.join(',')
        return [{ name, problem }]
      })
    )

    expect(endless.map(({ name }) => name)).toEqual(
      expect.arrayContaining(homeEndlessAnimations)
    )
    expect(
      endless
        .filter(({ problem }) => problem)
        .map(({ name, problem }) => `${name}: ${problem}`)
    ).toEqual([])
  })
}

test('runtime band motion pauses while the band is off screen', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await showZeroRequests(page)
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await page.locator('#app > *').first().waitFor({ state: 'visible' })
  const band = page.locator('#home-runtime')
  const motion = () =>
    band.evaluate((element) =>
      element
        .getAnimations({ subtree: true })
        .filter(
          (animation) => animation.effect?.getTiming().iterations === Infinity
        )
        .map(
          (animation) =>
            `${(animation as CSSAnimation).animationName} ${animation.playState}`
        )
        .sort()
    )
  const every = (state: string) =>
    [
      'runtime-status-breathe',
      'runtime-status-ripple',
      'runtime-trend-wave-float',
    ].map((name) => `${name} ${state}`)

  // The request metrics, and with them the trend wave, load once the band shows.
  await band.scrollIntoViewIfNeeded()
  await expect(band.locator('[data-zero-trend-wave]')).toHaveCount(1)
  await expect.poll(motion).toEqual(every('running'))
  await page.evaluate(() => window.scrollTo(0, 0))
  await expect.poll(motion).toEqual(every('paused'))
  await band.scrollIntoViewIfNeeded()
  await expect.poll(motion).toEqual(every('running'))
})

// Below the full-height hero the band only touches the viewport, so its clock
// does not tick and its request metrics wait until the visitor scrolls to it.
test('runtime band clock and metrics wait until the band shows', async ({
  page,
}) => {
  // Each clock reading advances the page's time by one second.
  await configureStablePage(page, {
    theme: 'light',
    authenticated: true,
    clockStepMs: 1_000,
  })
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  let metricsRequests = 0
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/home/metrics')
      metricsRequests += 1
  })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  const band = page.locator('#home-runtime')
  const clock = () =>
    band
      .locator('.runtime-flap')
      .evaluateAll((flaps) => flaps.map((flap) => flap.dataset.value).join(''))
  await expect(band.locator('.runtime-flap').first()).toBeAttached()
  // The launch time arrives with the site status; wait for it to settle.
  await page.waitForLoadState('networkidle')
  const atTop = await clock()
  await page.waitForTimeout(2_200)

  expect(await clock()).toBe(atTop)
  expect(metricsRequests).toBe(0)

  await band.scrollIntoViewIfNeeded()
  await expect.poll(() => metricsRequests).toBeGreaterThan(0)
  await expect.poll(clock).not.toBe(atTop)
})

/** The integration rail's moving track, its motion and the highlighted chip. */
function railState(page: import('@playwright/test').Page) {
  return page
    .locator('.hero-integration-marquee-wrapper > div')
    .evaluate((track: HTMLElement) => {
      const [motion] = track.getAnimations()
      const group = track.firstElementChild as HTMLElement
      return {
        inlineTransform: track.style.transform,
        rate: motion?.playbackRate,
        x: new DOMMatrix(getComputedStyle(track).transform).m41,
        groupWidth: group.offsetWidth,
        active: [...group.children].findIndex(
          (chip) => chip.querySelector('.chip-active') !== null
        ),
      }
    })
}

async function openMovingHome(page: import('@playwright/test').Page) {
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await expect(page.locator('.chip-active').first()).toBeVisible()
}

// The browser runs the rail's ambient motion as a Web Animation; script only
// eases its rate and moves the highlight, it never writes a transform per frame.
test('integration rail moves on its own and settles while hovered', async ({
  page,
}) => {
  await openMovingHome(page)
  const start = await railState(page)
  expect(start.inlineTransform).toBe('')
  expect(start.rate).toBe(1)

  await expect.poll(async () => (await railState(page)).x).not.toBe(start.x)
  await expect
    .poll(async () => (await railState(page)).active, { timeout: 8_000 })
    .not.toBe(start.active)

  await page.locator('.hero-integration-marquee-wrapper').hover()
  await expect.poll(async () => (await railState(page)).rate).toBe(0)
  const settled = (await railState(page)).x
  await page.waitForTimeout(400)
  expect((await railState(page)).x).toBe(settled)

  await page.mouse.move(5, 5)
  await expect.poll(async () => (await railState(page)).rate).toBe(1)
  expect((await railState(page)).inlineTransform).toBe('')
})

// On a landscape phone the rail leaves the screen near the end of the page.
// Its motion stays with the compositor, but the highlight check stops there and
// catches up as soon as the rail shows again.
test('integration rail stops checking its highlight while off screen', async ({
  page,
}) => {
  await page.setViewportSize({ width: 844, height: 390 })
  await openMovingHome(page)
  const rail = page.locator('.hero-integration-marquee-wrapper')
  type Counter = { railHighlightChanges: number }
  await rail.evaluate((element) => {
    const counter = window as unknown as Counter
    counter.railHighlightChanges = 0
    new MutationObserver((records) => {
      counter.railHighlightChanges += records.length
    }).observe(element, { subtree: true, attributeFilter: ['class'] })
  })
  const changes = () =>
    page.evaluate(() => (window as unknown as Counter).railHighlightChanges)

  await page.evaluate(() =>
    window.scrollTo(0, document.documentElement.scrollHeight)
  )
  await expect(rail).not.toBeInViewport()
  // Let the visibility observer report the rail hidden before counting.
  await page.waitForTimeout(300)
  await page.evaluate(() => {
    ;(window as unknown as Counter).railHighlightChanges = 0
  })
  await page.waitForTimeout(3_000)
  expect(await changes()).toBe(0)

  await page.evaluate(() => window.scrollTo(0, 0))
  await expect.poll(changes, { timeout: 8_000 }).toBeGreaterThan(0)
})

test('dragging the integration rail moves it with the pointer', async ({
  page,
}) => {
  await openMovingHome(page)
  const box = (await page
    .locator('.hero-integration-marquee-wrapper')
    .boundingBox())!
  const x = box.x + box.width / 2
  const y = box.y + box.height / 2

  await page.mouse.move(x, y)
  await page.mouse.down()
  await expect.poll(async () => (await railState(page)).rate).toBe(0)
  const before = await railState(page)
  await page.mouse.move(x - 60, y, { steps: 4 })
  await page.mouse.move(x - 120, y, { steps: 4 })
  const after = await railState(page)
  await page.mouse.up()

  const moved =
    (((before.x - after.x) % before.groupWidth) + before.groupWidth) %
    before.groupWidth
  expect(moved).toBeCloseTo(120, 0)
  await expect(page).toHaveURL(/\/$/)
})

test('home showcase remains intact at narrow mobile width', async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 720 })
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  const showcase = page.locator('.home-showcase')
  await showcase.scrollIntoViewIfNeeded()
  await assertNoHorizontalOverflow(page)

  const clockOverflow = await page
    .locator('.runtime-clock')
    .evaluate((element) => element.scrollWidth - element.clientWidth)
  expect(clockOverflow).toBeLessThanOrEqual(1)
  const trendOverflow = await page
    .locator('.runtime-trend')
    .evaluate((element) => element.scrollWidth - element.clientWidth)
  expect(trendOverflow).toBeLessThanOrEqual(1)
})

const requestTotalViewports = [
  { name: 'narrow-phone', width: 320, height: 720 },
  { name: 'small-phone', width: 360, height: 720 },
  { name: 'phone', width: 390, height: 844 },
  { name: 'small-desktop', width: 1024, height: 768 },
] as const

for (const theme of ['light', 'dark'] as VisualTheme[]) {
  for (const viewport of requestTotalViewports) {
    test(`${theme} ${viewport.name} long home request total stays contained`, async ({
      page,
    }) => {
      await page.setViewportSize(viewport)
      await configureStablePage(page, { theme, authenticated: true })
      const requests24h = Number.MAX_SAFE_INTEGER
      await page.route('**/api/home/metrics*', (route) =>
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            message: '',
            data: {
              available: true,
              requests_24h: requests24h,
              hourly_requests: [requests24h, ...Array(23).fill(0)],
              generated_at: 1_785_103_200,
            },
          }),
        })
      )
      await page.goto('/', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)
      await page.locator('#home-runtime').scrollIntoViewIfNeeded()

      const total = page.locator('[data-home-request-total]')
      await expect(total).toHaveText('9,007,199,254,740,991')
      const geometry = await total.evaluate((element) => {
        const totalBounds = element.getBoundingClientRect()
        const panel = element.closest<HTMLElement>(
          '.runtime-ledger-panel--requests'
        )!
        const panelBounds = panel.getBoundingClientRect()
        const trendBounds = panel
          .querySelector<HTMLElement>('.runtime-trend')!
          .getBoundingClientRect()
        const captionBounds = panel
          .querySelector<HTMLElement>('.runtime-request-caption')!
          .getBoundingClientRect()
        // A narrow day panel stacks the trend below the caption; elsewhere it
        // sits beside the total and the caption. Either way none overlap.
        const clear = (bounds: DOMRect) =>
          bounds.right <= trendBounds.left + 1 ||
          bounds.bottom <= trendBounds.top + 1
        return {
          totalLeft: totalBounds.left,
          totalRight: totalBounds.right,
          panelLeft: panelBounds.left,
          panelRight: panelBounds.right,
          overflow: element.scrollWidth - element.clientWidth,
          totalClear: clear(totalBounds),
          captionClear: clear(captionBounds),
        }
      })
      expect(geometry.totalLeft).toBeGreaterThanOrEqual(geometry.panelLeft - 1)
      expect(geometry.totalRight).toBeLessThanOrEqual(geometry.panelRight + 1)
      expect(geometry.overflow).toBeLessThanOrEqual(1)
      expect(geometry.totalClear, '总数与趋势图重叠').toBe(true)
      expect(geometry.captionClear, '说明文字与趋势图重叠').toBe(true)
      await assertNoHorizontalOverflow(page)
    })
  }
}

test('home progress dots navigate and track page position', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  const dots = page.locator('[data-home-progress-dot]')
  await expect(dots).toHaveCount(2)
  await expect(dots.nth(0)).toHaveAttribute('aria-current', 'location')

  await dots.nth(1).click()
  await expect
    .poll(() => page.evaluate(() => window.scrollY), { timeout: 2_000 })
    .toBeGreaterThan(0)
  await expect(dots.nth(1)).toHaveAttribute('aria-current', 'location')
  await expect(page.locator('#home-runtime')).toBeInViewport()

  await dots.nth(0).focus()
  await page.keyboard.press('Enter')
  await expect
    .poll(() => page.evaluate(() => window.scrollY), { timeout: 2_000 })
    .toBeLessThan(2)
  await expect(dots.nth(0)).toHaveAttribute('aria-current', 'location')

  await dots.nth(1).focus()
  await page.keyboard.press('Space')
  await expect
    .poll(() => page.evaluate(() => window.scrollY), { timeout: 2_000 })
    .toBeGreaterThan(0)
  await expect(dots.nth(1)).toHaveAttribute('aria-current', 'location')
})

test('home progress dots activate the runtime section at a tall viewport', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 1400 })
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  const dots = page.locator('[data-home-progress-dot]')
  await dots.nth(1).click()
  await expect(dots.nth(1)).toHaveAttribute('aria-current', 'location')
  await expect(page.locator('#home-runtime')).toBeInViewport()
})

const INTER = 'UcC73FwrK3iLTeHuS_nVMrMxCp50SjIa1ZL7'
const JETBRAINS_MONO =
  'tDbv2o-flEEny0FZhsfKu5WU4zr3E_BX0PnT8RD8yKwBNntkaToggR7BYRbKPxDcwg'

// The display fonts load as subsets cut from the home copy, and Chinese body
// text uses the system font, which the test host has. A complete display font
// or a Noto slice among the requests means some text lost its subset or went
// back to a web font.
for (const theme of ['light', 'dark'] as VisualTheme[]) {
  for (const locale of ['zh-CN', 'en'] as const) {
    test(`${theme} ${locale} home fetches only the fonts its text needs`, async ({
      page,
    }) => {
      const fonts = new Set<string>()
      page.on('request', (request) => {
        const font = /\/fonts\/(.+)\.woff2$/.exec(
          new URL(request.url()).pathname
        )
        if (font) fonts.add(font[1])
      })
      await configureStablePage(page, { theme, authenticated: true, locale })
      await page.goto('/')
      await waitForStablePage(page)
      await page.locator('.home-showcase').scrollIntoViewIfNeeded()
      await page.evaluate(() => document.fonts.ready)

      const subsets = [...fonts].filter((font) =>
        font.startsWith('home/subset/')
      )
      expect(
        [...fonts].filter((font) => !subsets.includes(font)).sort()
      ).toEqual([INTER, JETBRAINS_MONO].sort())
      expect(
        [
          ...new Set(subsets.map((font) => /subset\/([a-z]+)/.exec(font)![1])),
        ].sort()
      ).toEqual(theme === 'light' ? ['sketch', 'stamp', 'time'] : ['serif'])
    })
  }
}

test('homepage display fonts stay scoped away from the console', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.goto('/console', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  const consoleFont = await page
    .locator("[data-handdrawn-scope='console']")
    .evaluate((element) => getComputedStyle(element).fontFamily)
  expect(consoleFont).not.toContain('Ren2Home')
})

// The console message domain used to overwrite these eager home labels as soon
// as the router started loading it, so the CTA under the pointer shrank from
// "进入控制台" to "控制台" and kept the console wording after returning.
test('home navbar labels survive a console round trip', async ({ page }) => {
  await configureStablePage(page, { theme: 'light', authenticated: true })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  const labels = () =>
    page.evaluate(() =>
      [
        '.app-navbar .home-nav-cta',
        '.app-navbar a[href="/pricing"]',
        '.hero-cta-primary',
      ].map((selector) => {
        const element = document.querySelector<HTMLElement>(selector)!
        return {
          text: element.innerText.trim(),
          width: Math.round(element.getBoundingClientRect().width),
        }
      })
    )
  const before = await labels()
  expect(before.map((label) => label.text)).toEqual([
    '进入控制台',
    '定价',
    '进入控制台',
  ])

  await page.locator('.app-navbar .home-nav-cta').click()
  await expect(page).toHaveURL(/\/dashboard$/)
  await waitForStablePage(page)
  await page.goBack()
  await expect(page.locator('.hero-title')).toBeVisible()
  await waitForStablePage(page)

  expect(await labels()).toEqual(before)
})

for (const theme of ['light', 'dark'] as VisualTheme[]) {
  test(`home brand shows a keyboard focus ring (${theme})`, async ({
    page,
  }) => {
    await configureStablePage(page, { theme, authenticated: true })
    await page.goto('/', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    // Public pages share the navbar without the landing reset, so the ring
    // has to come out rounded there too.
    for (const path of ['/', '/pricing']) {
      if (path !== '/') {
        await page.goto(path, { waitUntil: 'domcontentloaded' })
        await waitForStablePage(page)
      }
      await page.locator('.app-navbar a[href="/pricing"]').focus()
      await page.keyboard.press('Shift+Tab')
      const brand = page.locator('.app-navbar .home-brand')
      await expect(brand).toBeFocused()
      await expect(brand).toHaveCSS('outline-style', 'solid')
      await expect(brand).toHaveCSS('outline-width', '2px')
      await expect(brand).toHaveCSS('border-top-left-radius', '8px')
    }
  })
}

// The day theme's pencil frame gives .hero-stage-hint position: relative,
// which once overrode the recall button's absolute placement and pushed it
// out of the viewport.
for (const theme of ['light', 'dark'] as VisualTheme[]) {
  test(`immersive recall button stays on screen and takes keyboard focus (${theme})`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await configureStablePage(page, { theme, authenticated: true })
    await page.goto('/', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const hint = page.locator('.hero-map-stage .hero-stage-hint')
    await page.locator('.app-navbar .home-nav-cta').focus()
    for (let step = 0; step < 20; step++) {
      if (await hint.evaluate((element) => element === document.activeElement))
        break
      await page.keyboard.press('Tab')
    }
    await expect(hint).toBeFocused()
    await expect(hint).toHaveCSS('opacity', '1')
    await page.keyboard.press('Enter')

    const recall = page.getByRole('button', { name: '展开介绍' })
    await expect(recall).toBeFocused()
    await expect(recall).toBeInViewport({ ratio: 1 })
    const box = (await recall.boundingBox())!
    expect(Math.abs(box.x + box.width / 2 - 195)).toBeLessThan(2)
    expect(
      await page.evaluate(
        () => document.getElementById('hero-immersive-stage')!.scrollLeft
      )
    ).toBe(0)

    await page.keyboard.press('Enter')
    await expect(hint).toBeFocused()
    await expect(hint).toHaveCSS('opacity', '1')
  })
}

async function swipe(
  page: import('@playwright/test').Page,
  x: number,
  fromY: number,
  toY: number
) {
  const cdp = await page.context().newCDPSession(page)
  const touch = (type: string, y?: number) =>
    cdp.send('Input.dispatchTouchEvent', {
      type,
      touchPoints: y === undefined ? [] : [{ x, y }],
    })
  await touch('touchStart', fromY)
  for (let step = 1; step <= 6; step++)
    await touch('touchMove', fromY + ((toY - fromY) * step) / 6)
  await touch('touchEnd')
  await cdp.detach()
}

test.describe('immersive focus hand-off', () => {
  test.use({ hasTouch: true })

  // The stage strip covers the thumb zone of the first screen; it once
  // declared pan-x, so swiping up there could not scroll the page.
  test('swiping up on the stage strip scrolls the page', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await configureStablePage(page, { theme: 'dark', authenticated: true })
    await page.goto('/', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const stageStrip = page.locator('.hero-map-stage')
    await expect(stageStrip).toHaveCSS('touch-action', 'pan-y')
    const stage = (await stageStrip.boundingBox())!
    // Start below the hint so the gesture lands on the strip itself.
    const fromY = stage.y + stage.height - 12
    await swipe(page, 60, fromY, fromY - 240)

    await expect
      .poll(() => page.evaluate(() => window.scrollY))
      .toBeGreaterThan(0)
    await expect(page.getByRole('button', { name: '展开介绍' })).toHaveCount(0)
  })

  // Gestures and the desktop breakpoint end the mode without a click, and
  // used to drop focus to <body> when the focused toggle disappeared.
  test('focus follows gesture and breakpoint exits; taps show no ring', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await configureStablePage(page, { theme: 'dark', authenticated: true })
    await page.goto('/', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    const hint = page.locator('.hero-map-stage .hero-stage-hint')
    const recall = page.getByRole('button', { name: '展开介绍' })
    const stage = (await page.locator('.hero-map-stage').boundingBox())!

    await swipe(page, 60, stage.y + 40, stage.y + 140)
    await expect(recall).toBeVisible()
    // Chromium swallows a tap that lands within ~50ms of the swipe's touchend
    // (no click is synthesized, on the base build too); a person never taps
    // that fast, so leave a realistic gap between the two gestures.
    await page.waitForTimeout(500)
    await recall.tap()
    await expect(recall).toHaveCount(0)
    expect(await hint.evaluate((el) => el.matches(':focus-visible'))).toBe(
      false
    )

    await hint.focus()
    await page.keyboard.press('Enter')
    await expect(recall).toBeFocused()
    await swipe(page, 60, 520, 400)
    await expect(recall).toHaveCount(0)
    await expect(hint).toBeFocused()

    await page.keyboard.press('Enter')
    await expect(recall).toBeFocused()
    await page.setViewportSize({ width: 1280, height: 844 })
    await expect(recall).toHaveCount(0)
    await expect(
      page
        .locator('[data-hero-map-copy]')
        .locator('a[href], button:not([disabled])')
        .first()
    ).toBeFocused()
  })
})

test('runtime motion starts only when motion is allowed', async ({ page }) => {
  await configureStablePage(page, {
    theme: 'dark',
    authenticated: true,
    clockStepMs: 1_000,
  })
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await page.locator('#app > *').first().waitFor({ state: 'visible' })
  await page.evaluate(() => document.fonts.ready)

  await page.locator('.home-showcase').scrollIntoViewIfNeeded()

  const animationName = await page
    .locator('.runtime-flap')
    .first()
    .evaluate((element) => getComputedStyle(element).animationName)
  expect(animationName).toBe('runtime-flap-tick')

  const statusAnimations = await page
    .locator('.runtime-status-dot')
    .evaluate((element) => ({
      dot: getComputedStyle(element).animationName,
      ripple: getComputedStyle(element, '::after').animationName,
    }))
  expect(statusAnimations).toEqual({
    dot: 'runtime-status-breathe',
    ripple: 'runtime-status-ripple',
  })

  await expect
    .poll(
      async () => {
        const activeLeaf = page.locator('.runtime-flap.is-flipping').first()
        if ((await activeLeaf.count()) === 0) return false
        return (
          (await activeLeaf.locator('.flap-leaf--previous-top').count()) ===
            1 &&
          (await activeLeaf.locator('.flap-leaf--previous-bottom').count()) ===
            1 &&
          (await activeLeaf.locator('.flap-leaf--next-bottom').count()) === 1
        )
      },
      { timeout: 5_000, intervals: [50, 100, 150, 200] }
    )
    .toBe(true)
})

for (const theme of ['light', 'dark'] as VisualTheme[]) {
  test(`${theme} English wide Hero preserves the two-column composition`, async ({
    page,
  }) => {
    await configureStablePage(page, {
      theme,
      authenticated: true,
      locale: 'en',
    })
    await page.goto('/')
    await waitForStablePage(page)
    for (const viewport of [
      { width: 1920, height: 1080 },
      { width: 2560, height: 768 },
      { width: 2560, height: 1264 },
      { width: 3840, height: 2160 },
    ]) {
      await page.setViewportSize(viewport)
      await waitForStablePage(page)
      await freezeAndInspectHomeCanvas(page)
      const geometry = await inspectHeroGeometry(page)
      expect(geometry.hero.height).toBeCloseTo(viewport.height, 0)
      expect(overlapArea(geometry.focus, geometry.copy)).toBe(0)
      const lead = page.locator('.hero-title > span').first()
      const glyphs = await lead.evaluate((element) => {
        const range = document.createRange()
        range.selectNodeContents(element)
        return range.getBoundingClientRect().toJSON()
      })
      expect(glyphs.left).toBeGreaterThanOrEqual(geometry.copy.left - 1)
      expect(glyphs.right).toBeLessThanOrEqual(geometry.copy.right + 1)
      await assertInteractiveCentersVisible(page)
      await assertNoHorizontalOverflow(page)
    }
  })
}
