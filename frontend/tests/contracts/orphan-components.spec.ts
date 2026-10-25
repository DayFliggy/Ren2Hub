import { expect, test } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import { captureUI } from './ui-coverage'

const variants = [
  { theme: 'light', locale: 'zh-CN', width: 1440, height: 900 },
  { theme: 'dark', locale: 'en', width: 390, height: 844 },
] as const

test('unrouted health meter renders measured fixture values and accessible states', async ({
  page,
}, info) => {
  for (const variant of variants) {
    await page.setViewportSize({ width: variant.width, height: variant.height })
    await configureStablePage(page, variant)
    await page.goto('/tests/harness.html?component=health')
    await waitForStablePage(page)
    const meters = page.getByRole('img')
    await expect(meters).toHaveCount(2)
    await expect(meters.first()).toHaveAttribute('aria-label', /98/)
    await page
      .getByRole('spinbutton', { name: 'Fixture health value' })
      .fill('70')
    await expect(meters.first()).toHaveAttribute('aria-label', /70/)
    await captureUI(page, info, `health-danger-${variant.theme}`)
    await page
      .getByRole('spinbutton', { name: 'Fixture health value' })
      .fill('0')
    await expect(meters.first()).toHaveAttribute('aria-label', /0/)
    await assertNoHorizontalOverflow(page)
  }
})

test('unrouted multiple filter preserves keyboard selection clear and escape', async ({
  page,
}, info) => {
  for (const variant of variants) {
    await page.setViewportSize({ width: variant.width, height: variant.height })
    await configureStablePage(page, variant)
    await page.goto('/tests/harness.html?component=multi-filter')
    await waitForStablePage(page)
    const trigger = page.getByRole('combobox', {
      name: 'Fixture status filter',
    })
    await trigger.focus()
    await trigger.press('ArrowDown')
    await expect(page.getByRole('listbox')).toBeVisible()
    await trigger.press('Enter')
    await trigger.press('ArrowDown')
    await trigger.press('Space')
    await expect(
      page.getByRole('status', { name: 'Selected values' })
    ).toHaveText('ready,pending')
    await expect(page.getByRole('option', { selected: true })).toHaveCount(2)
    await captureUI(page, info, `multi-selected-${variant.theme}`)
    await trigger.press('Escape')
    await expect(page.getByRole('listbox')).toHaveCount(0)
    await expect(trigger).toBeFocused()
    await trigger.click()
    await page
      .getByRole('button', {
        name: variant.locale === 'en' ? 'Clear' : '清除',
        exact: false,
      })
      .click()
    await expect(
      page.getByRole('status', { name: 'Selected values' })
    ).toBeEmpty()
    await page.getByRole('button', { name: 'Outside control' }).click()
    await expect(page.getByRole('listbox')).toHaveCount(0)
    await assertNoHorizontalOverflow(page)
  }
})

test('unrouted activity card preserves image fallback keyboard and disabled behavior', async ({
  page,
}, info) => {
  for (const variant of variants) {
    await page.setViewportSize({ width: variant.width, height: variant.height })
    await configureStablePage(page, variant)
    await page.goto('/tests/harness.html?component=activity-entry')
    await waitForStablePage(page)
    const cards = page.locator('.activity-entry')
    await expect(cards).toHaveCount(2)
    await expect(cards.first().locator('img')).toHaveCount(0)
    await expect(cards.last()).toBeDisabled()
    await cards.first().focus()
    await cards.first().press('Enter')
    await expect(
      page.getByRole('status', { name: 'Enter event count' })
    ).toHaveText('1')
    await cards.first().press('Tab')
    await expect(cards.last()).not.toBeFocused()
    await captureUI(page, info, `activity-fallback-${variant.theme}`)
    await expect(
      page.getByRole('status', { name: 'Enter event count' })
    ).toHaveText('1')
    await assertNoHorizontalOverflow(page)
  }
})
