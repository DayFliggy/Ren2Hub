import { expect, test, type Locator } from '@playwright/test'

import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

async function composingKey(input: Locator, key: string) {
  const prevented = await input.evaluate((element, value) => {
    element.dispatchEvent(
      new CompositionEvent('compositionstart', { bubbles: true })
    )
    const event = new KeyboardEvent('keydown', {
      key: value,
      isComposing: true,
      bubbles: true,
      cancelable: true,
    })
    element.dispatchEvent(event)
    element.dispatchEvent(
      new CompositionEvent('compositionend', { bubbles: true })
    )
    return event.defaultPrevented
  }, key)
  expect(prevented).toBe(false)
}

// These scenarios include several routed views or a full edit/create flow.
test.setTimeout(90_000)

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`composition and provider groups ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 900 }
      )
      await configureStablePage(page, {
        theme,
        expectedApiErrors: { '/api/tickets/1/messages': [503] },
      })
      const failures: string[] = []
      page.on('pageerror', (error) => failures.push(error.message))
      await page.route('**/api/channel/fetch_models*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: [
              'meta/muse-spark-1.3-contributor',
              'muse-spark-1.3',
              'sparkdesk-v3.5',
            ],
          },
        })
      )
      await page.goto('/channels')
      await page.getByRole('button', { name: '新建渠道', exact: true }).click()
      const channel = page.getByRole('dialog', {
        name: '新建渠道',
        exact: true,
      })
      const model = channel.getByPlaceholder('选择模型或添加自定义模型', {
        exact: true,
      })
      await model.fill('muse-spark-1.3')
      for (const key of ['Enter', ',', 'Escape']) {
        await composingKey(model, key)
        await expect(model).toHaveValue('muse-spark-1.3')
        await expect(channel).toBeVisible()
      }
      await model.press('Enter')
      await expect(channel.locator('input[placeholder=""]')).toHaveValue('')
      await expect(
        channel.getByText('muse-spark-1.3', { exact: true }).first()
      ).toBeVisible()
      await channel.locator('[name="admin-channel-key"]').fill('fixture-key')
      await channel
        .getByRole('button', { name: '从上游获取', exact: true })
        .click()
      const picker = page.getByRole('dialog', {
        name: '选择上游模型',
        exact: true,
      })
      await expect(
        picker.getByRole('checkbox', { name: '选择 Meta 模型', exact: true })
      ).toBeVisible()
      await expect(
        picker.getByRole('checkbox', { name: '选择 iFlytek 模型', exact: true })
      ).toBeVisible()
      await picker
        .getByRole('checkbox', { name: '选择 Meta 模型', exact: true })
        .uncheck()
      await expect(
        picker.getByRole('checkbox', { name: 'muse-spark-1.3', exact: true })
      ).not.toBeChecked()
      await expect(
        picker.getByRole('checkbox', {
          name: 'meta/muse-spark-1.3-contributor',
          exact: true,
        })
      ).not.toBeChecked()
      await expect(
        picker.getByRole('checkbox', { name: 'sparkdesk-v3.5', exact: true })
      ).toBeChecked()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('provider-groups.png'),
        fullPage: true,
      })
      await page.keyboard.press('Escape')
      await expect(picker).toHaveCount(0)
      await expect(channel).toBeVisible()
      await page.keyboard.press('Escape')
      await expect(channel).toHaveCount(0)

      await page.keyboard.press('Control+k')
      const command = page.locator('input[name="command-search"]')
      await expect(command).toBeFocused()
      const active = await command.getAttribute('aria-activedescendant')
      for (const key of ['ArrowDown', 'ArrowUp', 'Enter', 'Escape']) {
        await composingKey(command, key)
        await expect(command).toBeFocused()
        await expect(command).toHaveAttribute('aria-activedescendant', active!)
        await expect(page).toHaveURL(/\/channels$/)
      }
      await command.press('Escape')
      await expect(command).toHaveCount(0)

      await page.goto('/models/vendors')
      await page
        .getByRole('button', { name: '添加供应商', exact: true })
        .click()
      const drawer = page.getByRole('dialog', {
        name: '创建供应商',
        exact: true,
      })
      const vendorName = drawer.locator('[name="vendor-name"]')
      await vendorName.focus()
      await composingKey(vendorName, 'Escape')
      await expect(vendorName).toBeFocused()
      await expect(drawer).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('vendor-drawer.png'),
        fullPage: true,
      })
      await vendorName.press('Escape')
      await expect(drawer).toHaveCount(0)

      let replies = 0
      await page.route('**/api/tickets/1/messages', (route) => {
        replies += 1
        return route.fulfill({
          status: 503,
          json: { success: false, message: 'Fixture reply unavailable' },
        })
      })
      await page.goto('/tickets/1')
      const reply = page.locator('textarea[name="ticket-reply"]')
      await reply.fill('中文输入保留')
      await composingKey(reply, 'Enter')
      await expect(reply).toHaveValue('中文输入保留')
      expect(replies).toBe(0)
      await reply.press('Shift+Enter')
      await expect(reply).toHaveValue('中文输入保留\n')
      expect(replies).toBe(0)
      await reply.press('Enter')
      await expect(
        page.getByText('Fixture reply unavailable', { exact: true })
      ).toBeVisible()
      expect(replies).toBe(1)
      await expect(reply).toHaveValue('中文输入保留\n')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('ticket-reply-error.png'),
        fullPage: true,
      })

      expect(failures).toEqual([])
    })
  }
}
