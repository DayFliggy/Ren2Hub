import { expect, test } from '@playwright/test'
import { configureStablePage } from './fixtures'

test('the quota dialog keeps the currency sign clear of the amount', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'light' })
  await page.goto('/users')
  await page.locator('[aria-label="调整额度"]:visible').first().click()
  const dialog = page.getByRole('dialog', { name: '调整额度', exact: true })
  const input = dialog.locator('input[name="admin-user-quota-amount"]')
  // The sign sits inside the input; typed digits must start after it.
  const prefix = input.locator('xpath=preceding-sibling::span[1]')
  await expect(prefix).toHaveText('$')
  const [inputBox, prefixBox, paddingLeft] = await Promise.all([
    input.boundingBox(),
    prefix.boundingBox(),
    input.evaluate((node) =>
      Number.parseFloat(getComputedStyle(node).paddingLeft)
    ),
  ])
  expect(inputBox!.x + paddingLeft).toBeGreaterThanOrEqual(
    prefixBox!.x + prefixBox!.width
  )
})
