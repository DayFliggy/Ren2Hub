import { expect, test, type Locator, type Page } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

async function expectReadableControls(page: Page, controls: Locator) {
  expect(await controls.count()).toBeGreaterThan(0)
  const minimum = await page.evaluate(() =>
    Math.max(
      16,
      parseFloat(getComputedStyle(document.documentElement).fontSize)
    )
  )
  for (const control of await controls.all()) {
    const size = await control.evaluate((element) =>
      parseFloat(getComputedStyle(element).fontSize)
    )
    expect(size, await control.getAttribute('name')).toBeGreaterThanOrEqual(
      minimum
    )
  }
}

for (const theme of ['light', 'dark'] as const) {
  test(`contact options receive clicks across the mobile breakpoint (${theme})`, async ({
    page,
    context,
  }, testInfo) => {
    await configureStablePage(page, { theme })
    // Prove that the real link receives the click without contacting Telegram.
    await context.route('https://t.me/ren2hub', (route) =>
      route.fulfill({
        contentType: 'text/html',
        body: '<p>Contact fixture</p>',
      })
    )
    await page.goto('/dashboard')
    await waitForStablePage(page)
    const trigger = page.locator('.contact-float-trigger')
    const link = page.getByRole('link', { name: 'Telegram', exact: true })

    for (const width of [320, 390, 480, 481, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      await trigger.click()
      await expect(trigger).toHaveAttribute('aria-expanded', 'true')
      await link.scrollIntoViewIfNeeded()
      if (width === 390 || width === 1440) {
        await page.screenshot({
          path: testInfo.outputPath(`contact-${theme}-${width}.png`),
        })
      }
      const popupPromise = page.waitForEvent('popup')
      await link.click({ timeout: 5000 })
      const popup = await popupPromise
      await expect(popup).toHaveURL('https://t.me/ren2hub')
      await popup.close()
      await expect(trigger).toHaveAttribute('aria-expanded', 'false')

      await trigger.click()
      await page.mouse.click(8, 880)
      await expect(trigger).toHaveAttribute('aria-expanded', 'false')
      await trigger.click()
      await trigger.click()
      await expect(trigger).toHaveAttribute('aria-expanded', 'false')
      await assertNoHorizontalOverflow(page)
    }
  })

  test(`channel form controls retain readable mobile text (${theme})`, async ({
    page,
  }, testInfo) => {
    await configureStablePage(page, { theme })
    await page.setViewportSize({ width: 390, height: 900 })
    await page.goto('/channels')
    await waitForStablePage(page)
    await page.getByRole('button', { name: '新建渠道', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '新建渠道', exact: true })
    await dialog.getByRole('radio', { name: '多密钥渠道', exact: true }).click()
    const controls = dialog.locator(
      'input:not([type="checkbox"]):not([type="radio"]):not([type="hidden"]):visible, textarea:visible, select:visible'
    )
    await expect(
      dialog.locator('[name="admin-channel-key-multi"]')
    ).toBeVisible()
    await expect(dialog.locator('select').first()).toBeVisible()
    await dialog.locator('[name="admin-channel-name"]').fill('移动端渠道')
    await dialog.locator('[name="admin-channel-key-multi"]').fill('fixture-key')
    for (const width of [320, 390, 639, 640, 767]) {
      await page.setViewportSize({ width, height: 900 })
      await expectReadableControls(page, controls)
      await assertNoHorizontalOverflow(page)
    }
    await page.setViewportSize({ width: 390, height: 900 })
    await page.screenshot({ path: testInfo.outputPath(`inputs-${theme}.png`) })
    await page.evaluate(() => {
      document.documentElement.style.fontSize = '20px'
    })
    await expectReadableControls(page, controls)
    await page.evaluate(() => {
      document.documentElement.style.removeProperty('font-size')
    })
    for (const width of [768, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      await expect(dialog.locator('[name="admin-channel-name"]')).toHaveCSS(
        'font-size',
        '15px'
      )
    }
  })

  test(`public login inputs remain readable through 767px (${theme})`, async ({
    page,
  }) => {
    await configureStablePage(page, { theme, authenticated: false })
    await page.goto('/sign-in')
    await waitForStablePage(page)
    const inputs = page.locator('.auth-card input:visible')
    for (const width of [320, 390, 640, 767]) {
      await page.setViewportSize({ width, height: 900 })
      await expectReadableControls(page, inputs)
      await assertNoHorizontalOverflow(page)
    }
  })

  test(`discount inputs preserve their larger text on phones (${theme})`, async ({
    page,
  }, testInfo) => {
    await configureStablePage(page, { theme })
    await page.setViewportSize({ width: 390, height: 900 })
    await page.goto('/system-settings/billing/usage-discount')
    await waitForStablePage(page)
    const fixedRate = page.getByLabel('固定折扣', { exact: true })
    for (const width of [320, 390, 767, 768, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      await expect(fixedRate, `${theme}, ${width}px`).toHaveCSS(
        'font-size',
        '20px'
      )
    }
    await page.setViewportSize({ width: 390, height: 900 })
    await fixedRate.scrollIntoViewIfNeeded()
    await page.screenshot({
      path: testInfo.outputPath(`discount-${theme}.png`),
    })
    await page.evaluate(() => {
      document.documentElement.style.fontSize = '20px'
    })
    await expect(fixedRate).toHaveCSS('font-size', '25px')
  })
}
