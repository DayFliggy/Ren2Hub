import { expect, test } from '@playwright/test'
import { createModelMarketFixture } from '../fixtures/modelMarket'
import {
  configureStablePage as configureNativePage,
  waitForStablePage,
  assertNoHorizontalOverflow,
} from './fixtures'

async function configureStablePage(
  ...args: Parameters<typeof configureNativePage>
) {
  await configureNativePage(...args)
  await args[0].route('**/api/user/model-catalog*', (route) =>
    route.fulfill({ json: { success: true, data: createModelMarketFixture() } })
  )
}

for (const locale of ['zh-CN', 'en']) {
  test(`narrow toolbar controls stay separate and clickable ${locale}`, async ({
    page,
  }, testInfo) => {
    await configureStablePage(page, { theme: 'light' })
    await page.addInitScript(
      (value) => localStorage.setItem('ren2hub_locale', value),
      locale
    )
    for (const width of [320, 360, 390]) {
      await page.setViewportSize({ width, height: 900 })
      await page.goto('/models')
      await waitForStablePage(page)
      const sizes = width === 390 ? [null, 340, 341] : [null]
      for (const container of sizes) {
        if (container)
          await page.locator('.model-market').evaluate((element, value) => {
            ;(element as HTMLElement).style.width = `${value}px`
          }, container)
        const sort = (await page.locator('.market-sort-filter').boundingBox())!
        const switchBox = (await page
          .locator('.market-view-switch')
          .boundingBox())!
        const intersect =
          Math.min(sort.x + sort.width, switchBox.x + switchBox.width) >
            Math.max(sort.x, switchBox.x) &&
          Math.min(sort.y + sort.height, switchBox.y + switchBox.height) >
            Math.max(sort.y, switchBox.y)
        expect(intersect, `${width}/${container} toolbar overlap`).toBe(false)
        const modes = page.locator('.market-view-switch [role="radio"]')
        for (let index = 0; index < 3; index++) {
          const button = modes.nth(index)
          const box = (await button.boundingBox())!
          expect(box.height).toBeGreaterThanOrEqual(44)
          expect(box.width).toBeGreaterThanOrEqual(44)
          await button.click()
          await expect(button).toHaveAttribute('aria-checked', 'true')
        }
        await assertNoHorizontalOverflow(page)
      }
      await page.screenshot({
        path: testInfo.outputPath(`toolbar-${width}.png`),
      })
    }
  })
}

for (const width of [1024, 1440]) {
  test(`keyboard traverses the visible channel corridor ${width}`, async ({
    page,
  }, testInfo) => {
    await configureStablePage(page, { theme: 'dark' })
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/models')
    await waitForStablePage(page)
    await page.getByRole('radio', { name: '比价矩阵', exact: true }).click()
    const matrix = page.locator('[data-market-matrix]')
    const buttons = matrix.locator('tbody button')
    await buttons.first().focus()
    const inspect = async () => {
      const bounds = await matrix.evaluate((element) => {
        const active = document.activeElement as HTMLElement
        const box = active.getBoundingClientRect()
        const left = element
          .querySelector('.model-column')!
          .getBoundingClientRect().right
        const right = element
          .querySelector('.benchmark-column')!
          .getBoundingClientRect().left
        const hit = document.elementFromPoint(
          box.x + box.width / 2,
          box.y + box.height / 2
        )
        return {
          offer: active.hasAttribute('data-offer-id'),
          left,
          right,
          x: box.left,
          end: box.right,
          hit: active === hit || active.contains(hit),
        }
      })
      if (!bounds.offer) return
      expect(bounds.x).toBeGreaterThanOrEqual(bounds.left - 1)
      expect(bounds.end).toBeLessThanOrEqual(bounds.right + 1)
      expect(bounds.hit).toBe(true)
    }
    const count = await buttons.count()
    for (let index = 0; index < count; index++) {
      await inspect()
      if (index < count - 1) await page.keyboard.press('Tab')
    }
    for (let index = count - 1; index > 0; index--) {
      await page.keyboard.press('Shift+Tab')
      await inspect()
    }
    const before = await matrix.evaluate((element) => element.scrollLeft)
    await buttons.first().focus()
    expect(await matrix.evaluate((element) => element.scrollLeft)).toBe(before)
    await matrix.locator('tbody [data-channel-id="12"] button').first().focus()
    await page.screenshot({
      path: testInfo.outputPath(`matrix-focus-${width}.png`),
    })
    await page.getByRole('radio', { name: '比价矩阵', exact: true }).focus()
    await expect(matrix.locator('.col-hover')).toHaveCount(0)
  })
}

for (const theme of ['light', 'dark'] as const) {
  test(`long model titles leave space for details and preserve the full ID ${theme}`, async ({
    page,
  }, testInfo) => {
    await configureStablePage(page, { theme })
    const fixture = createModelMarketFixture()
    const longId = `provider/${'long-model-name-without-break'.repeat(6)}`
    fixture.models[0]!.model_name = longId
    fixture.offers.forEach((offer) => {
      if (offer.model_id === 'gpt-4.1') offer.model_id = longId
    })
    await page.route('**/api/user/model-catalog', (route) =>
      route.fulfill({ json: { success: true, data: fixture } })
    )
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'clipboard', {
        value: {
          writeText: async (value: string) => {
            ;(window as unknown as { copied: string }).copied = value
          },
        },
      })
    })
    for (const viewport of [
      { width: 320, height: 568 },
      { width: 390, height: 500 },
    ]) {
      await page.setViewportSize(viewport)
      await page.goto('/models')
      await waitForStablePage(page)
      const trigger = page
        .locator('[data-model-card]')
        .first()
        .locator('.market-card-hit')
      await trigger.click()
      const dialog = page.getByRole('dialog')
      await expect(dialog.locator('.market-full-id')).toBeVisible()
      const header = await dialog
        .locator('.market-detail-header')
        .evaluate(
          (element) => element.parentElement!.getBoundingClientRect().height
        )
      expect(header / viewport.height).toBeLessThan(0.35)
      await dialog.locator('.market-full-id summary').click()
      await expect(dialog.locator('.market-full-id code')).toHaveText(longId)
      await dialog.getByRole('button', { name: /^复制模型 ID ·/ }).click()
      expect(
        await page.evaluate(
          () => (window as unknown as { copied: string }).copied
        )
      ).toBe(longId)
      await expect(
        page.getByText('已复制模型 ID', { exact: true })
      ).toBeVisible()
      await dialog
        .getByRole('button', { name: '关闭模型详情', exact: true })
        .hover()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath(`long-id-${viewport.width}.png`),
      })
      await page.keyboard.press('Escape')
      await expect(trigger).toBeFocused()
    }
  })

  test(`detail selection feedback and stable price rows ${theme}`, async ({
    page,
  }, testInfo) => {
    await configureStablePage(page, { theme })
    await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 900 })
      await page.goto('/models')
      await waitForStablePage(page)
      await page
        .getByRole('button', {
          name: 'View details and channel prices for gpt-4.1',
          exact: true,
        })
        .click()
      const dialog = page.getByRole('dialog')
      await expect(dialog.locator('.market-full-id')).toHaveCount(0)
      const offer = dialog.locator('button[data-offer-id]').first()
      await expect(offer).toHaveAccessibleDescription(
        /0\.2×.*Usage.*\$0\.500.*1M tokens.*\$2\.000.*Statistics unavailable/s
      )
      const rows = dialog.locator('.market-detail-offer')
      for (const row of await rows.all()) {
        const input = (await row
          .locator('[data-price-dimension="input"]')
          .boundingBox())!
        const output = (await row
          .locator('[data-price-dimension="output"]')
          .boundingBox())!
        expect(output.y).toBeGreaterThanOrEqual(input.y + input.height - 1)
        expect(output.x).toBeCloseTo(input.x, 0)
      }
      const baseline = dialog.locator('[data-market-baseline] button')
      await baseline.scrollIntoViewIfNeeded()
      const scrollBefore = await dialog
        .locator('.market-detail')
        .evaluate((element) => element.parentElement!.scrollTop)
      await baseline.click()
      expect(
        await dialog
          .locator('.market-detail')
          .evaluate((element) => element.parentElement!.scrollTop)
      ).toBe(scrollBefore)
      await expect(baseline).toHaveAttribute('aria-pressed', 'true')
      await expect(dialog.locator('[aria-live="polite"]')).toContainText(
        'Platform base price'
      )
      await expect(
        dialog.locator('[data-market-baseline] .market-detail-selected')
      ).toHaveText('Selected')
      await expect(baseline).not.toHaveAccessibleDescription(/Statistics/)
      await page.keyboard.press('Tab')
      await page.keyboard.press('Shift+Tab')
      await expect(baseline).toBeFocused()
      await expect(baseline).toHaveCSS('outline-style', 'solid')
      const contrasts = await baseline.evaluate((element) => {
        const row = element.parentElement!
        const canvas = document.createElement('canvas')
        canvas.width = canvas.height = 1
        const ctx = canvas.getContext('2d')!
        const rgb = (color: string) => {
          ctx.clearRect(0, 0, 1, 1)
          ctx.fillStyle = color
          ctx.fillRect(0, 0, 1, 1)
          return Array.from(ctx.getImageData(0, 0, 1, 1).data)
        }
        const surface = rgb(
          getComputedStyle(element.closest('[data-handdrawn="modal"]')!)
            .backgroundColor
        )
        const layer = rgb(getComputedStyle(row).backgroundColor)
        const alpha = layer[3]! / 255
        const background = layer
          .slice(0, 3)
          .map((channel, i) => channel * alpha + surface[i]! * (1 - alpha))
        const luminance = (color: number[]) =>
          color
            .slice(0, 3)
            .map((value) => {
              const c = value / 255
              return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
            })
            .reduce(
              (sum, value, i) => sum + value * [0.2126, 0.7152, 0.0722][i]!,
              0
            )
        const contrast = (foreground: number[]) => {
          const a = luminance(foreground),
            b = luminance(background)
          return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
        }
        const selected = rgb(
          getComputedStyle(row.querySelector('.market-detail-selected')!).color
        )
        const focus = rgb(getComputedStyle(element).outlineColor)
        return { selected: contrast(selected), focus: contrast(focus) }
      })
      expect(contrasts.selected).toBeGreaterThanOrEqual(3)
      expect(contrasts.focus).toBeGreaterThanOrEqual(3)
      await page.screenshot({
        path: testInfo.outputPath(`details-selected-${width}.png`),
      })
      await dialog
        .getByRole('button', { name: 'View pricing details ↓', exact: true })
        .click()
      await expect(
        dialog.locator('.market-detail-price-panel h3')
      ).toBeFocused()
      await expect(
        dialog.locator('.market-detail-price-panel h3')
      ).toBeInViewport()
      await page.keyboard.press('Escape')
    }
  })
}

test('comparison preserves a missing native offer beside the platform price', async ({
  page,
}, testInfo) => {
  await configureStablePage(page, { theme: 'light' })
  await page.setViewportSize({ width: 1440, height: 900 })
  const fixture = createModelMarketFixture()
  fixture.offers = fixture.offers.filter(
    (offer) => !(offer.model_id === 'video-preview' && offer.channel_id === 11)
  )
  await page.route('**/api/user/model-catalog', (route) =>
    route.fulfill({ json: { success: true, data: fixture } })
  )
  await page.goto('/models')
  await waitForStablePage(page)
  await page.getByRole('radio', { name: '比价矩阵', exact: true }).click()
  const row = page.locator(
    '[data-market-matrix] tr[data-model-name="video-preview"]'
  )
  await expect(row).toHaveCount(1)
  await expect(row.locator('[data-channel-id="11"]')).toHaveText('—')
  await expect(row.locator('[data-channel-id="12"]')).toContainText('$0.080')
  await expect(row.locator('.benchmark-column')).toContainText('$0.080')
  await assertNoHorizontalOverflow(page)
  await page.screenshot({
    path: testInfo.outputPath('native-offer-alignment.png'),
  })
})
