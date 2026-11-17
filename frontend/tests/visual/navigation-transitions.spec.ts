import { expect, test, type Page } from '@playwright/test'

import { configureStablePage, waitForStablePage } from './fixtures'

interface NavigationFrame {
  /** When the frame was sampled, on the page's performance clock. */
  at: number
  /** Scroll offset of the console scroll zone. */
  top: number
  /** The page shown before the navigation is still displayed. */
  previousShown: boolean
  contentWidth: number
  /** Combined opacity of the visible tab strip in the page header. */
  stripOpacity: number | null
  /** Top-level pages displayed inside #app. */
  shells: number
  shellFading: boolean
}

interface NavigationRecording {
  frames: NavigationFrame[]
  running: boolean
  /** When the console dashboard code was requested, on the same clock. */
  consoleRequestedAt: number | null
}

/** Samples the console once per animation frame until `stopRecording`. */
async function startRecording(page: Page) {
  await page.evaluate(() => {
    const content = document.querySelector('.console-content')
    const state = {
      frames: [] as unknown[],
      running: true,
      previous: content?.firstElementChild ?? null,
      consoleRequestedAt: null as number | null,
    }
    Object.assign(window, { navigationRecording: state })
    new PerformanceObserver((list) => {
      const code = list
        .getEntries()
        .find((entry) => entry.name.includes('/DashboardView.vue'))
      if (code) state.consoleRequestedAt ??= code.startTime
    }).observe({ type: 'resource' })
    const displayed = (element: Element | null) =>
      !!element &&
      element.isConnected &&
      getComputedStyle(element).display !== 'none'
    const sample = () => {
      const box = document.querySelector<HTMLElement>('.console-content')
      const zone = document.querySelector('[data-console-scroll-zone]')
      const current = [...(box?.children ?? [])].find(displayed) ?? null
      const strip =
        current?.querySelector('a[href^="/usage-logs"]')?.closest('nav') ?? null
      let stripOpacity: number | null = null
      for (
        let node: Element | null = strip;
        node && node !== box;
        node = node.parentElement
      )
        stripOpacity =
          (stripOpacity ?? 1) * Number(getComputedStyle(node).opacity)
      const shells = [...document.querySelectorAll('#app > *')].filter(
        (element) =>
          !element.hasAttribute('data-navigation-progress') &&
          displayed(element)
      )
      state.frames.push({
        at: performance.now(),
        top: zone?.scrollTop ?? 0,
        previousShown: displayed(state.previous),
        contentWidth: box?.offsetWidth ?? 0,
        stripOpacity,
        shells: shells.length,
        shellFading: shells.some((element) =>
          element.classList.contains('shell-enter-active')
        ),
      })
      if (state.running) requestAnimationFrame(sample)
    }
    requestAnimationFrame(sample)
  })
}

async function stopRecording(page: Page): Promise<NavigationFrame[]> {
  // Long enough for a 160ms fade, or the previous 370ms leave-then-enter.
  await page.waitForTimeout(600)
  return page.evaluate(() => {
    const state = (
      window as unknown as { navigationRecording: NavigationRecording }
    ).navigationRecording
    state.running = false
    return state.frames
  })
}

function consoleRequestedAt(page: Page): Promise<number | null> {
  return page.evaluate(
    () =>
      (window as unknown as { navigationRecording: NavigationRecording })
        .navigationRecording.consoleRequestedAt
  )
}

async function openWithMotion(page: Page, path: string) {
  await page.goto(path)
  await waitForStablePage(page)
  await page.emulateMedia({ reducedMotion: 'no-preference' })
}

async function finish(page: Page) {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await waitForStablePage(page)
}

for (const scenario of [
  { name: 'desktop light root', theme: 'light', role: 100, mobile: false },
  { name: 'phone dark member', theme: 'dark', role: 1, mobile: true },
] as const) {
  test(`a scrolled page is gone before the next one starts at the top (${scenario.name})`, async ({
    page,
  }) => {
    await page.setViewportSize(
      scenario.mobile
        ? { width: 390, height: 844 }
        : { width: 1440, height: 900 }
    )
    await configureStablePage(page, {
      theme: scenario.theme,
      user: { role: scenario.role },
    })
    await openWithMotion(page, '/dashboard')
    const zone = page.locator('[data-console-scroll-zone]')
    await zone.evaluate((element) => {
      element.scrollTop = 300
    })
    await expect.poll(() => zone.evaluate((el) => el.scrollTop)).toBe(300)

    if (scenario.mobile)
      await page.locator('button.console-navigation-trigger').click()
    const entry = scenario.mobile
      ? page.locator(
          '[data-console-mobile-navigation] [data-console-nav-item="models"]'
        )
      : page.locator('aside [data-console-nav-item="models"]')
    await expect(entry).toBeVisible()
    await startRecording(page)
    await entry.click()
    await expect(page).toHaveURL(/\/models$/)
    const frames = await stopRecording(page)

    expect(frames.length).toBeGreaterThan(10)
    expect(
      frames.filter((frame) => frame.previousShown && frame.top === 0),
      'the previous page must not be shown after the scroll reset'
    ).toEqual([])
    expect(frames.at(-1)).toMatchObject({ top: 0, previousShown: false })
    await finish(page)
  })
}

test('log tabs keep their tab strip on screen while the content changes', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light', user: { role: 100 } })
  await openWithMotion(page, '/usage-logs')
  await startRecording(page)
  await page.locator('.console-content nav a[href="/usage-logs/task"]').click()
  await expect(page).toHaveURL(/\/usage-logs\/task$/)
  const frames = await stopRecording(page)

  expect(frames.map((frame) => frame.stripOpacity)).not.toContain(null)
  expect(Math.min(...frames.map((frame) => frame.stripOpacity!))).toBe(1)
  await expect(page.locator('.console-content')).toHaveAttribute(
    'data-page-swap',
    'tab'
  )
  await expect(
    page.locator('.console-content nav a[aria-current="page"]')
  ).toHaveAttribute('href', '/usage-logs/task')

  // Leaving the tabs is an ordinary page change again.
  await page.locator('aside [data-console-nav-item="models"]').click()
  await expect(page).toHaveURL(/\/models$/)
  await expect(page.locator('.console-content')).toHaveAttribute(
    'data-page-swap',
    'page'
  )
  await finish(page)
})

test('a wide screen takes the next page width in one step', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await configureStablePage(page, { theme: 'light', user: { role: 100 } })
  await openWithMotion(page, '/tickets')
  const narrow = await page
    .locator('.console-content')
    .evaluate((element: HTMLElement) => element.offsetWidth)
  await startRecording(page)
  await page.locator('aside [data-console-nav-item="keys"]').click()
  await expect(page).toHaveURL(/\/keys$/)
  const frames = await stopRecording(page)

  const wide = frames.at(-1)!.contentWidth
  expect(wide).toBeGreaterThan(narrow)
  expect(
    [...new Set(frames.map((frame) => frame.contentWidth))].sort()
  ).toEqual([narrow, wide])
  await finish(page)
})

test('hovering a menu entry fetches its page and a slow page shows progress', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light', user: { role: 100 } })
  await openWithMotion(page, '/dashboard')
  const pageCode = /src\/views\/console\/ChannelsView\.vue(\?|$)/
  await page.route(pageCode, async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 1_500))
    await route.fallback()
  })
  const progress = page.locator('[data-navigation-progress]')
  const entry = page.locator(
    'aside [data-console-nav-item="channel-management"]'
  )

  const requested = page.waitForRequest(pageCode)
  await entry.hover()
  await requested
  await expect(page).toHaveURL(/\/dashboard$/)
  await expect(progress).toHaveCount(0)

  await entry.click()
  await expect(progress).toBeVisible()
  await expect(page).toHaveURL(/\/dashboard$/)
  await expect(page).toHaveURL(/\/channels$/)
  await expect(progress).toHaveCount(0)
  await finish(page)
})

test('the sign-in page fetches the console and only a change of shell fades', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light', authenticated: false })
  await openWithMotion(page, '/')

  const consoleCode = page.waitForRequest(
    /src\/views\/console\/DashboardView\.vue(\?|$)/
  )
  await startRecording(page)
  await page.locator('a.home-nav-cta').click()
  await expect(page).toHaveURL(/\/sign-in$/)
  const toSignIn = await stopRecording(page)
  await consoleCode
  await expect(page).toHaveURL(/\/sign-in$/)

  expect(Math.max(...toSignIn.map((frame) => frame.shells))).toBe(1)
  expect(toSignIn.some((frame) => frame.shellFading)).toBe(true)
  // The console code waits until the sign-in page has finished arriving.
  await expect.poll(() => consoleRequestedAt(page)).not.toBeNull()
  const arrived = Math.max(
    ...toSignIn.filter((frame) => frame.shellFading).map((frame) => frame.at)
  )
  expect(await consoleRequestedAt(page)).toBeGreaterThan(arrived)

  await startRecording(page)
  await page.locator('a[href^="/forgot-password"]').first().click()
  await expect(page).toHaveURL(/\/forgot-password/)
  const withinAuth = await stopRecording(page)

  expect(Math.max(...withinAuth.map((frame) => frame.shells))).toBe(1)
  expect(withinAuth.some((frame) => frame.shellFading)).toBe(false)
  await finish(page)
})
