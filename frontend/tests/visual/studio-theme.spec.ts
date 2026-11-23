import { expect, test, type Locator } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  test(`mobile navigation drawer preserves routes, focus and scroll (${theme})`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await configureStablePage(page, { theme })
    await page.goto('/dashboard')
    await waitForStablePage(page)
    const trigger = page.locator('[aria-controls="console-mobile-navigation"]')
    await expect(page.locator('[data-top-nav="mobile"]')).toHaveCount(0)
    await expect(
      page.locator('nav[data-handdrawn="navigation-strip"]')
    ).toHaveCount(0)
    await expect(page.locator('.topbar-nav')).toHaveCSS('height', '52px')
    await trigger.click()
    const dialog = page.getByRole('dialog', { name: '导航', exact: true })
    await expect(dialog).toBeVisible()
    for (const href of [
      '/dashboard',
      '/keys',
      '/models',
      '/wallet',
      '/ops',
      '/channels',
      '/users',
    ])
      await expect(dialog.locator(`a[href="${href}"]`).first()).toBeVisible()
    await expect(page.locator('[data-console-scroll-zone]')).toHaveCSS(
      'overflow',
      'hidden'
    )
    await expect(page.locator('#app')).toHaveJSProperty('inert', true)
    const close = dialog.getByRole('button', { name: '关闭导航菜单' })
    await close.focus()
    await page.keyboard.press('Shift+Tab')
    expect(
      await dialog.evaluate((el) => el.contains(document.activeElement))
    ).toBe(true)
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await expect(trigger).toBeFocused()
    await expect(page.locator('#app')).toHaveJSProperty('inert', false)
    await expect(page.locator('[data-console-scroll-zone]')).toHaveCSS(
      'overflow-y',
      'auto'
    )
    await trigger.click()
    await dialog.locator('[data-console-nav-item="keys"]').click()
    await expect(page).toHaveURL(/\/keys$/)
    await expect(dialog).toHaveCount(0)
    await trigger.click()
    await page.setViewportSize({ width: 1024, height: 900 })
    await expect(dialog).toHaveCount(0)
    await expect(trigger).toBeHidden()
    await expect(page.locator('#app')).toHaveJSProperty('inert', false)
    await assertNoHorizontalOverflow(page)
  })
}

test.describe('coarse pointer control targets', () => {
  test.use({ hasTouch: true })

  for (const theme of ['light', 'dark'] as const) {
    test(`public navigation keeps usable touch theme and language controls (${theme})`, async ({
      page,
    }, testInfo) => {
      await configureStablePage(page, { theme })
      const measurements: unknown[] = []
      for (const width of [768, 1440]) {
        await page.setViewportSize({ width, height: 1000 })
        await page.goto('/')
        await waitForStablePage(page)
        await page.evaluate(() => window.scrollTo(0, 400))
        const navbar = page.locator('.app-navbar')
        await expect(navbar).toHaveAttribute('data-scrolled', 'true')
        expect(
          await page.evaluate(() => matchMedia('(pointer: coarse)').matches)
        ).toBe(true)
        for (const selector of [
          '[data-theme-switcher]',
          '[aria-controls="language-selector-menu"]',
        ]) {
          const trigger = navbar.locator(selector)
          await expect(trigger).toBeVisible()
          const box = (await trigger.boundingBox())!
          measurements.push({ theme, viewportWidth: width, selector, ...box })
          expect.soft(box.width, `${selector} width`).toBeGreaterThanOrEqual(44)
          expect
            .soft(box.height, `${selector} height`)
            .toBeGreaterThanOrEqual(44)
          expect.soft(box.x).toBeGreaterThanOrEqual(0)
          expect.soft(box.x + box.width).toBeLessThanOrEqual(width)
          await trigger.tap()
          await expect(trigger).toHaveAttribute('aria-expanded', 'true')
          await expect(navbar.getByRole('menu')).toBeVisible()
          await page.keyboard.press('Escape')
          await expect(trigger).toHaveAttribute('aria-expanded', 'false')
          await expect(trigger).toBeFocused()
        }
        await assertNoHorizontalOverflow(page)
      }
      await testInfo.attach('public-coarse-pointer-measurements', {
        body: JSON.stringify(measurements, null, 2),
        contentType: 'application/json',
      })
    })

    test(`dashboard and keys keep usable touch controls (${theme})`, async ({
      page,
    }, testInfo) => {
      await configureStablePage(page, { theme })
      const measurements: unknown[] = []
      const checkTargets = async (targets: Locator, label: string) => {
        const visible = targets.locator('visible=true')
        expect(await visible.count(), label).toBeGreaterThan(0)
        for (const target of await visible.all()) {
          const box = (await target.boundingBox())!
          const name =
            (await target.getAttribute('aria-label')) ||
            (await target.textContent())?.trim()
          const context = `${theme} ${page.viewportSize()!.width}px ${label} ${name}`
          measurements.push({ context, ...box })
          expect.soft(box.width, `${context} width`).toBeGreaterThanOrEqual(44)
          expect
            .soft(box.height, `${context} height`)
            .toBeGreaterThanOrEqual(44)
        }
      }

      for (const width of [768, 1440]) {
        await page.setViewportSize({ width, height: 1000 })
        // Resizing can reveal labels and start subset-font requests on the old
        // route. Let those finish before replacing the document in Firefox.
        await page.waitForLoadState('networkidle')
        await page.evaluate(() => document.fonts.ready)
        await page.goto('/dashboard')
        await waitForStablePage(page)
        expect(
          await page.evaluate(() => matchMedia('(pointer: coarse)').matches)
        ).toBe(true)
        await checkTargets(
          page.locator('.console-tabs [role="tab"]'),
          'dashboard tabs'
        )
        await checkTargets(
          page.locator('[data-handdrawn="segments"] [role="radio"]'),
          'segmented choices'
        )
        await checkTargets(
          page.locator('[data-balance-actions] button'),
          'balance actions'
        )
        await checkTargets(page.locator('[data-bonus-configure]'), 'bonus icon')
        await checkTargets(
          page.locator(
            '.topbar-nav [aria-controls="console-mobile-navigation"], .topbar-nav .search-trigger, .topbar-nav [data-theme-switcher], .topbar-nav [data-user-menu-trigger], .topbar-brand [data-version-badge], .topbar-language > button'
          ),
          'topbar actions'
        )
        await assertNoHorizontalOverflow(page)
        expect(
          await page
            .locator('[data-console-scroll-zone]')
            .evaluate((el) => el.scrollWidth - el.clientWidth)
        ).toBeLessThanOrEqual(1)

        await page.goto('/keys')
        await waitForStablePage(page)
        const selectionCheckboxes = page
          .getByRole('checkbox')
          .filter({ visible: true })
        const rowCheckbox = selectionCheckboxes.nth(1)
        await expect(rowCheckbox).toBeVisible()
        await checkTargets(
          selectionCheckboxes.locator('..'),
          'list and table selection labels'
        )
        await rowCheckbox.scrollIntoViewIfNeeded()
        const selectionLabel = rowCheckbox.locator('..')
        const selectionBox = (await selectionLabel.boundingBox())!
        const checkedBefore = await rowCheckbox.isChecked()
        // Tap outside the glyph, inside the real label's corner.
        await page.touchscreen.tap(selectionBox.x + 2, selectionBox.y + 2)
        await expect(rowCheckbox).toBeChecked({ checked: !checkedBefore })
        await page.touchscreen.tap(selectionBox.x + 2, selectionBox.y + 2)
        await expect(rowCheckbox).toBeChecked({ checked: checkedBefore })
        await checkTargets(
          page.getByRole('button', { name: '创建令牌', exact: true }),
          'create action'
        )
        await checkTargets(
          page.getByRole('button', { name: '编辑令牌', exact: true }),
          'edit icons'
        )
        await checkTargets(
          page.getByRole('link', { name: '路由配置', exact: true }),
          'routing icons'
        )
        const pageSize = page
          .getByRole('combobox', { name: /^每页条数 / })
          .locator('visible=true')
        await checkTargets(pageSize, 'page size filter')
        await pageSize.scrollIntoViewIfNeeded()
        await pageSize.tap()
        await expect(page.getByRole('listbox')).toBeVisible()
        await page.keyboard.press('Escape')
        await assertNoHorizontalOverflow(page)
        expect(
          await page
            .locator('[data-console-scroll-zone]')
            .evaluate((el) => el.scrollWidth - el.clientWidth)
        ).toBeLessThanOrEqual(1)

        await page.getByRole('button', { name: '创建令牌', exact: true }).tap()
        const dialog = page.getByRole('dialog', {
          name: '创建令牌',
          exact: true,
        })
        const toggle = dialog.getByRole('switch', {
          name: '过期时间',
          exact: true,
        })
        await toggle.scrollIntoViewIfNeeded()
        const hotspot = await toggle.evaluate((el) => {
          const rect = el.getBoundingClientRect()
          const style = getComputedStyle(el, '::after')
          const width = parseFloat(style.width)
          const height = parseFloat(style.height)
          const x = rect.x + rect.width / 2
          const y = rect.y + rect.height / 2 - height / 2 + 1
          const hit = document.elementFromPoint(x, y)
          return {
            width,
            height,
            x,
            y,
            hitsToggle: hit === el || el.contains(hit),
          }
        })
        measurements.push({
          context: `${theme} ${width}px toggle hotspot`,
          ...hotspot,
        })
        expect.soft(hotspot.width).toBeGreaterThanOrEqual(44)
        expect.soft(hotspot.height).toBeGreaterThanOrEqual(44)
        expect(
          hotspot.hitsToggle,
          'the expanded touch area must receive real pointer hits'
        ).toBe(true)
        const before = await toggle.getAttribute('aria-checked')
        await page.touchscreen.tap(hotspot.x, hotspot.y)
        await expect(toggle).toHaveAttribute(
          'aria-checked',
          before === 'true' ? 'false' : 'true'
        )
        await page.keyboard.press('Escape')
        await expect(dialog).toHaveCount(0)
        await page.evaluate(() => document.fonts.ready)
      }
      await testInfo.attach('coarse-pointer-measurements', {
        body: JSON.stringify(measurements, null, 2),
        contentType: 'application/json',
      })
    })
  }
})

test('mobile drawer respects member permissions and disabled modules', async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 844 })
  await configureStablePage(page, { theme: 'dark', user: { role: 1 } })
  await page.goto('/dashboard')
  await waitForStablePage(page)
  await page.locator('[aria-controls="console-mobile-navigation"]').click()
  const nav = page.locator('[data-console-mobile-navigation]')
  await expect(nav.locator('a[href="/ops"]')).toHaveCount(0)
  await expect(nav.locator('a[href="/channels"]')).toHaveCount(0)
  await expect(nav.locator('a[href="/system-settings/site"]')).toHaveCount(0)
  await expect(nav.locator('[data-console-nav-item="keys"]')).toHaveAttribute(
    'href',
    '/keys'
  )
  await expect(nav.locator('[data-console-nav-item="keys"]')).toBeVisible()
  await assertNoHorizontalOverflow(page)
})

test('dashboard keeps hidden captions and enlarged analytics inside its scroll zone', async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 844 })
  await configureStablePage(page, { theme: 'dark', locale: 'en' })
  await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
  await page.goto('/dashboard')
  await waitForStablePage(page)
  const zone = page.locator('[data-console-scroll-zone]')

  // A visually hidden status caption must not create an outer page scrollbar.
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollHeight - innerHeight)
    )
    .toBeLessThanOrEqual(1)
  expect(await zone.evaluate((el) => el.scrollHeight)).toBeGreaterThan(
    await zone.evaluate((el) => el.clientHeight)
  )
  await zone.evaluate((el) => {
    el.scrollTop = el.scrollHeight
  })
  expect(await zone.evaluate((el) => el.scrollTop)).toBeGreaterThan(0)
  await zone.evaluate((el) => {
    el.scrollTop = 0
  })

  await page.setViewportSize({ width: 1024, height: 900 })
  // Root text enlargement exercises the layout in every supported browser.
  await page.evaluate(() => {
    document.documentElement.style.fontSize = '200%'
  })
  await expect(page.locator('html')).toHaveCSS('font-size', '32px')
  await expect
    .poll(() => zone.evaluate((el) => el.scrollWidth - el.clientWidth))
    .toBeLessThanOrEqual(1)
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollHeight - innerHeight)
    )
    .toBeLessThanOrEqual(1)
  await expect(page.locator('[data-kpi-cell]')).toHaveCount(5)
  const distribution = page.locator('[data-usage-distribution]')
  await distribution.scrollIntoViewIfNeeded()
  await expect(distribution.locator('[data-usage-analytics]')).toBeVisible()
  await assertNoHorizontalOverflow(page)
})

test('enlarged mobile day tabs preserve labels and keyboard access within their scroll container', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await configureStablePage(page, { theme: 'light', locale: 'en' })
  await page.goto('/dashboard')
  await waitForStablePage(page)
  await page.evaluate(() => {
    document.documentElement.style.fontSize = '200%'
  })
  await expect(page.locator('html')).toHaveCSS('font-size', '32px')
  const tabs = page.locator('.console-tabs')
  const buttons = tabs.getByRole('tab')
  await expect(buttons).toHaveCount(3)
  await tabs.scrollIntoViewIfNeeded()
  expect(
    await tabs.evaluate((el) => el.scrollWidth - el.clientWidth)
  ).toBeGreaterThan(0)
  for (const button of await buttons.all()) {
    const fits = await button.evaluate((el) => {
      const rect = el.getBoundingClientRect()
      const label = el.querySelector('span')!
      const range = document.createRange()
      range.selectNodeContents(label)
      return [...range.getClientRects()].every(
        (text) => text.left >= rect.left - 1 && text.right <= rect.right + 1
      )
    })
    expect(fits, (await button.textContent()) || 'tab label').toBe(true)
  }
  await buttons.first().focus()
  await page.keyboard.press('End')
  await expect(buttons.last()).toBeFocused()
  await expect(buttons.last()).toHaveAttribute('aria-selected', 'true')
  await expect(buttons.last()).toHaveCSS('outline-width', '2px')
  await expect(buttons.last()).toHaveCSS('outline-offset', '-2px')
  const container = (await tabs.boundingBox())!
  const selected = (await buttons.last().boundingBox())!
  expect(selected.x).toBeGreaterThanOrEqual(container.x - 1)
  expect(selected.x + selected.width).toBeLessThanOrEqual(
    container.x + container.width + 1
  )
  await assertNoHorizontalOverflow(page)
})

test('theme transitions preserve scroll, finish cleanly and honor reduced motion', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  await page.addInitScript(() => {
    const native = document.startViewTransition?.bind(document)
    const counts = { started: 0, finished: 0 }
    Object.assign(window, { studioTransitions: counts })
    if (!native) return
    document.startViewTransition = ((update: ViewTransitionUpdateCallback) => {
      counts.started++
      const transition = native(update)
      void transition.finished.then(() => {
        counts.finished++
      })
      return transition
    }) as typeof document.startViewTransition
  })
  await page.goto('/dashboard')
  await waitForStablePage(page)
  await page.emulateMedia({
    reducedMotion: 'no-preference',
    colorScheme: 'light',
  })
  const zone = page.locator('[data-console-scroll-zone]')
  await zone.evaluate((el) => {
    el.scrollTop = 300
  })
  const originalScroll = await zone.evaluate((el) => el.scrollTop)
  await page.locator('.topbar-header [data-theme-switcher]').click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(page.locator('html')).not.toHaveAttribute(
    'data-theme-transition'
  )
  expect(await zone.evaluate((el) => el.scrollTop)).toBe(originalScroll)
  const counts = await page.evaluate(
    () =>
      (
        window as unknown as {
          studioTransitions: { started: number; finished: number }
        }
      ).studioTransitions
  )
  const supported = await page.evaluate(
    () => typeof document.startViewTransition === 'function'
  )
  expect(counts).toEqual({
    started: supported ? 1 : 0,
    finished: supported ? 1 : 0,
  })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.locator('.topbar-header [data-theme-switcher]').click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  expect(
    await page.evaluate(
      () =>
        (window as unknown as { studioTransitions: { started: number } })
          .studioTransitions.started
    )
  ).toBe(counts.started)
  await expect(page.locator('html')).not.toHaveAttribute(
    'data-theme-transition'
  )
})

test('preferences keep the latest theme choice and system changes do not animate', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  await page.goto('/settings')
  await waitForStablePage(page)
  await page.emulateMedia({
    reducedMotion: 'no-preference',
    colorScheme: 'light',
  })
  const choices = page.getByRole('radiogroup', {
    name: '外观主题',
    exact: true,
  })
  await choices.scrollIntoViewIfNeeded()
  await choices.evaluate((group) => {
    const find = (text: string) =>
      [...group.querySelectorAll<HTMLButtonElement>('button')].find(
        (button) => button.textContent?.trim() === text
      )!
    find('夜间').click()
    find('日间').click()
    find('夜间').click()
  })
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(page.locator('html')).not.toHaveAttribute(
    'data-theme-transition'
  )
  await expect(
    choices.getByRole('radio', { name: '夜间', exact: true })
  ).toHaveAttribute('aria-checked', 'true')
  await choices.getByRole('radio', { name: '跟随系统', exact: true }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await expect(page.locator('html')).not.toHaveAttribute(
    'data-theme-transition'
  )
  await page.evaluate(() => {
    const original = document.startViewTransition?.bind(document)
    Object.assign(window, { systemTransitions: 0 })
    if (original)
      document.startViewTransition = ((
        update: ViewTransitionUpdateCallback
      ) => {
        ;(window as unknown as { systemTransitions: number })
          .systemTransitions++
        return original(update)
      }) as typeof document.startViewTransition
  })
  await page.emulateMedia({ colorScheme: 'dark' })
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  expect(
    await page.evaluate(
      () =>
        (window as unknown as { systemTransitions: number }).systemTransitions
    )
  ).toBe(0)
})

test('a storage theme update keeps an open dialog, its draft and focus', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  await page.goto('/keys')
  await waitForStablePage(page)
  const create = page.getByRole('button', { name: '创建令牌', exact: true })
  await create.click()
  const dialog = page.getByRole('dialog')
  const name = dialog.locator('input[name="token-name"]')
  await name.fill('Studio draft')
  await page.evaluate(() => {
    localStorage.setItem('ren2hub_theme_mode', 'dark')
    window.dispatchEvent(
      new StorageEvent('storage', {
        key: 'ren2hub_theme_mode',
        oldValue: 'light',
        newValue: 'dark',
        storageArea: localStorage,
      })
    )
  })
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(page.locator('html')).not.toHaveAttribute(
    'data-theme-transition'
  )
  await expect(dialog).toBeVisible()
  await expect(name).toHaveValue('Studio draft')
  await expect(name).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(create).toBeFocused()
})

test('cold theme artwork is decoded before the destination snapshot completes', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  let release!: () => void
  let started!: () => void
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  const requested = new Promise<void>((resolve) => {
    started = resolve
  })
  await page.route(/activity-banner\.webp(?:\?.*)?$/, async (route) => {
    if (route.request().resourceType() !== 'image') return route.fallback()
    started()
    await pending
    await route.continue()
  })
  await page.goto('/activity')
  await waitForStablePage(page)
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  const supported = await page.evaluate(
    () => typeof document.startViewTransition === 'function'
  )
  try {
    await page.locator('.topbar-header [data-theme-switcher]').click()
    await requested
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
    if (supported)
      await expect(page.locator('html')).toHaveAttribute(
        'data-theme-transition',
        ''
      )
  } finally {
    release()
  }
  await expect(page.locator('html')).not.toHaveAttribute(
    'data-theme-transition'
  )
  await expect
    .poll(() =>
      page
        .locator('img[data-theme-asset]')
        .evaluate(
          (image: HTMLImageElement) => image.complete && image.naturalWidth > 0
        )
    )
    .toBe(true)
})

for (const update of ['manual', 'storage'] as const) {
  test(`a newer ${update} preference cancels pending theme artwork cleanly`, async ({
    page,
  }) => {
    const artwork = /\/src\/assets\/activity\/activity-banner\.webp(?:\?.*)?$/
    let superseded = false
    const cancelledRequests: { failure: string; superseded: boolean }[] = []
    await configureStablePage(page, {
      theme: 'light',
      expectedFailedRequests: [artwork],
    })
    page.on('requestfailed', (request) => {
      if (artwork.test(request.url()))
        cancelledRequests.push({
          failure: request.failure()?.errorText ?? 'unknown',
          superseded,
        })
    })
    let release!: () => void
    let started!: () => void
    const pending = new Promise<void>((resolve) => {
      release = resolve
    })
    const requested = new Promise<void>((resolve) => {
      started = resolve
    })
    await page.route(/activity-banner\.webp(?:\?.*)?$/, async (route) => {
      if (route.request().resourceType() !== 'image') return route.fallback()
      started()
      await pending
      await route.continue()
    })
    await page.goto('/activity')
    await waitForStablePage(page)
    await page.emulateMedia({
      reducedMotion: 'no-preference',
      colorScheme: 'light',
    })
    try {
      await page.locator('.topbar-header [data-theme-switcher]').click()
      await requested
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
      superseded = true
      if (update === 'manual') {
        // Trigger the real handler while the browser is waiting on its snapshot.
        await page
          .locator('.topbar-header [data-theme-switcher]')
          .evaluate((button: HTMLButtonElement) => button.click())
      } else {
        await page.evaluate(() => {
          localStorage.setItem('ren2hub_theme_mode', 'light')
          window.dispatchEvent(
            new StorageEvent('storage', {
              key: 'ren2hub_theme_mode',
              oldValue: 'dark',
              newValue: 'light',
              storageArea: localStorage,
            })
          )
        })
      }
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
    } finally {
      release()
    }
    await expect(page.locator('html')).not.toHaveAttribute(
      'data-theme-transition'
    )
    await expect
      .poll(() =>
        page
          .locator('img[data-theme-asset]')
          .evaluate(
            (image: HTMLImageElement) =>
              image.complete && image.naturalWidth > 0
          )
      )
      .toBe(true)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await waitForStablePage(page)
    // Firefox cancels the obsolete image request when its source changes.
    // Accept only that deliberate cancellation, never a failed active asset.
    for (const request of cancelledRequests) {
      expect(request.superseded).toBe(true)
      expect(['NS_BINDING_ABORTED', 'net::ERR_ABORTED']).toContain(
        request.failure
      )
    }
    expect(
      await page.evaluate(() => localStorage.getItem('ren2hub_theme_mode'))
    ).toBe(update === 'manual' ? 'auto' : 'light')
  })
}

for (const theme of ['light', 'dark'] as const) {
  test(`mobile topbar remains reachable with 200 percent text (${theme})`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 320, height: 640 })
    await configureStablePage(page, { theme, locale: 'en' })
    await page.goto('/dashboard')
    await waitForStablePage(page)
    await page.evaluate(() => {
      document.documentElement.style.fontSize = '200%'
    })
    const nav = page.locator('.topbar-nav')
    const box = (await nav.boundingBox())!
    const buttons = await nav
      .locator('button:visible')
      .evaluateAll((elements) =>
        elements.map((element) => {
          const { x, y, width, height } = element.getBoundingClientRect()
          return { x, y, width, height }
        })
      )
    // Navigation and the Root version badge beside the brand, then search,
    // theme and the user menu.
    expect(buttons).toHaveLength(5)
    for (const button of buttons) {
      expect(button.width).toBeGreaterThanOrEqual(44)
      expect(button.height).toBeGreaterThanOrEqual(44)
      expect(button.x).toBeGreaterThanOrEqual(0)
      expect(button.x + button.width).toBeLessThanOrEqual(320)
      expect(button.y).toBeGreaterThanOrEqual(box.y)
      expect(button.y + button.height).toBeLessThanOrEqual(box.y + box.height)
    }
    await page.locator('[data-user-menu-trigger]').click()
    const menu = page.locator('.user-menu-panel')
    const menuBox = (await menu.boundingBox())!
    expect(menuBox.x).toBeGreaterThanOrEqual(0)
    expect(menuBox.x + menuBox.width).toBeLessThanOrEqual(320)
    await menu.locator('[data-user-menu-item]').last().scrollIntoViewIfNeeded()
    await expect(menu.locator('[data-user-menu-item]').last()).toBeInViewport()
    await page.keyboard.press('Escape')
    await page.setViewportSize({ width: 1024, height: 900 })
    const trigger = page.locator('[aria-controls="console-mobile-navigation"]')
    await expect(trigger).toBeVisible()
    for (const button of await nav.locator('button:visible').all()) {
      const bounds = (await button.boundingBox())!
      expect(bounds.x).toBeGreaterThanOrEqual(0)
      expect(bounds.x + bounds.width).toBeLessThanOrEqual(1024)
    }
    await trigger.click()
    const drawer = page.locator('[data-console-mobile-navigation]')
    await expect(drawer).toBeVisible()
    await expect(drawer.locator('[data-console-nav-item="keys"]')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(trigger).toBeFocused()
    await waitForStablePage(page)
  })
}
