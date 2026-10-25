import {
  expect,
  test,
  type Locator,
  type Page,
  type TestInfo,
} from '@playwright/test'
import en from '../../src/i18n/locales/en/console'
import zh from '../../src/i18n/locales/zh-CN/console'
import {
  configureStablePage,
  waitForStablePage,
  assertNoHorizontalOverflow,
} from './fixtures'
import { captureUI } from './ui-coverage'
import {
  affiliateStatesFixture,
  channelLab,
  channelStatesFixture,
  redemptionStatesFixture,
} from './channel-states-fixture'

const scenarios = [
  {
    key: 'desktop-light-zh-CN',
    locale: 'zh-CN',
    theme: 'light',
    width: 1440,
    height: 900,
  },
  {
    key: 'mobile-dark-en',
    locale: 'en',
    theme: 'dark',
    width: 390,
    height: 844,
  },
] as const

async function capture(
  page: Page,
  info: TestInfo,
  label: string,
  region?: Locator
) {
  if (region) await region.scrollIntoViewIfNeeded()
  await assertNoHorizontalOverflow(page)
  await captureUI(page, info, label)
}
async function close(page: Page, trigger: Locator, remaining = 0) {
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toHaveCount(remaining)
  await expect(trigger).toBeFocused()
}
function visibleButton(page: Page, name: string) {
  return page
    .getByRole('button', { name, exact: true })
    .filter({ visible: true })
    .first()
}

for (const scenario of scenarios) {
  const copy = scenario.locale === 'en' ? en : zh
  test.describe(scenario.key, () => {
    test.beforeEach(async ({ page }) => {
      await page.setViewportSize(scenario)
      await configureStablePage(page, scenario)
    })

    test('channel states runtime multi-key capacity and inline fields', async ({
      page,
    }, info) => {
      const { channel } = await channelStatesFixture(page)
      await page.goto('/channels')
      await waitForStablePage(page)
      await expect(
        page
          .getByText(channel.name, { exact: true })
          .filter({ visible: true })
          .first()
      ).toBeVisible()
      await capture(page, info, 'channel-list-capacity-and-key-state')
      const fields = visibleButton(page, copy.channels.fieldSettings)
      await fields.click()
      const fieldMenu = page.getByRole('dialog')
      await fieldMenu
        .getByRole('button', { name: copy.channels.priority, exact: true })
        .click()
      await fieldMenu
        .getByRole('button', { name: copy.channels.weight, exact: true })
        .click()
      await close(page, fields)
      const priority = page
        .getByRole('spinbutton', {
          name: copy.channels.priorityFor.replace('{name}', channel.name),
          exact: true,
        })
        .filter({ visible: true })
      await expect(priority).toHaveValue('2')
      await priority.fill('9')
      await priority.press('Escape')
      await expect(priority).toHaveValue('2')
      await capture(page, info, 'channel-inline-number-fields', priority)
      const runtime = visibleButton(page, copy.gateway.channelRuntime)
      await runtime.click()
      const dialog = page.getByRole('dialog')
      await expect(dialog).toContainText('0.10.0-ui-fixture')
      await dialog
        .locator('summary')
        .filter({ hasText: `${copy.channels.providerMetrics} (1)` })
        .click()
      await expect(dialog).toContainText('vllm:num_requests_running')
      await capture(
        page,
        info,
        'channel-runtime-provider-status',
        dialog.locator('[data-channel-runtime-keys]')
      )
      await close(page, runtime)
    })

    test('channel states model and lab test dialogs', async ({
      page,
    }, info) => {
      const { requests } = await channelStatesFixture(page)
      await page.goto('/channels')
      await waitForStablePage(page)
      const testChannel = visibleButton(page, copy.channels.testChannel)
      await testChannel.click()
      const dialog = page.getByRole('dialog')
      await dialog
        .getByRole('button', {
          name: copy.channels.testModelAction.replace(
            '{model}',
            'contract-chat'
          ),
          exact: true,
        })
        .click()
      await expect.poll(() => requests.length).toBe(1)
      await expect(dialog).toContainText('120')
      await capture(page, info, 'channel-model-test-result')
      await close(page, testChannel)
      const labTest = visibleButton(
        page,
        copy.channels.testSupplier.replace('{supplier}', channelLab)
      )
      await labTest.click()
      await expect(
        page.getByRole('dialog').getByRole('combobox', {
          name: copy.channels.pickModelLabel,
          exact: true,
        })
      ).toContainText('contract-chat')
      await page.locator('[data-channel-test-start]').click()
      await expect.poll(() => requests.length).toBe(2)
      await expect(page.locator('[data-channel-test-start]')).toBeEnabled()
      await capture(page, info, 'channel-lab-test-picker')
      await close(page, labTest)
      expect(
        requests.every((request) => request.model === 'contract-chat')
      ).toBe(true)
    })

    test('channel states upstream model synchronization', async ({
      page,
    }, info) => {
      const { channel, applied } = await channelStatesFixture(page)
      await page.goto('/channels')
      await waitForStablePage(page)
      const sync = visibleButton(page, copy.channels.syncModelsTitle)
      await sync.click()
      const dialog = page.getByRole('dialog')
      await expect(
        dialog.getByRole('checkbox', { name: 'contract-next', exact: true })
      ).toBeChecked()
      await dialog
        .getByRole('checkbox', { name: 'contract-chat', exact: true })
        .check()
      await capture(page, info, 'channel-upstream-sync-preview')
      await dialog
        .getByRole('button', {
          name: copy.channels.syncModelsApply,
          exact: true,
        })
        .click()
      await expect(page.getByRole('dialog')).toHaveCount(0)
      expect(applied).toEqual([
        {
          id: channel.id,
          add_models: ['contract-next'],
          remove_models: ['contract-chat'],
          ignore_models: [],
        },
      ])
    })

    test('channel states type 58 routes and fetched model picker', async ({
      page,
    }, info) => {
      test.setTimeout(90_000)
      const fixture = await channelStatesFixture(page, true, true)
      await page.goto('/channels')
      await waitForStablePage(page)
      const edit = visibleButton(page, copy.channels.editChannel)
      await edit.click()
      const dialog = page.getByRole('dialog').first()
      await dialog.locator('.channel-form-section--advanced > button').click()
      const incoming = dialog.getByLabel(copy.channels.incomingPath, {
        exact: true,
      })
      await expect(incoming).toHaveValue('/v1/chat/completions')
      await capture(page, info, 'channel-type58-visual-routes', incoming)
      await dialog
        .getByRole('button', {
          name: copy.channels.modelMappingJson,
          exact: true,
        })
        .last()
        .click()
      const jsonEditor = dialog.getByRole('textbox', {
        name: copy.channels.advancedRouteJson,
        exact: true,
      })
      await expect(jsonEditor).toHaveValue(/advanced_routes/)
      await capture(page, info, 'channel-type58-json-routes', jsonEditor)
      const fetch = dialog.getByRole('button', {
        name: copy.channels.quickFetchUpstream,
        exact: true,
      })
      const fetchElement = await fetch.elementHandle()
      await fetch.focus()
      await expect(fetch).toBeFocused()
      await fetch.click()
      expect(fetchElement).not.toBeNull()
      await expect
        .poll(() =>
          fetchElement!.evaluate(
            (button) => button instanceof HTMLButtonElement && button.disabled
          )
        )
        .toBe(true)
      await info.attach('fetched-models-loading-focus', {
        body: JSON.stringify(
          await page.evaluate(() => ({
            tag: document.activeElement?.tagName,
            name: document.activeElement?.getAttribute('aria-label'),
            text: document.activeElement?.textContent?.slice(0, 120),
          }))
        ),
        contentType: 'application/json',
      })
      fixture.releaseFetchedModels()
      const picker = page.getByRole('dialog').last()
      await expect(
        picker.getByRole('checkbox', { name: 'gpt-ui-next', exact: true })
      ).toBeChecked()
      await picker
        .getByRole('checkbox', { name: 'claude-ui-next', exact: true })
        .uncheck()
      await capture(page, info, 'channel-fetched-model-picker')
      await page.keyboard.press('Escape')
      await expect(page.getByRole('dialog')).toHaveCount(1)
      await info.attach('fetched-models-focus-return', {
        body: JSON.stringify(
          await page.evaluate(() => ({
            tag: document.activeElement?.tagName,
            name: document.activeElement?.getAttribute('aria-label'),
            text: document.activeElement?.textContent?.slice(0, 120),
          }))
        ),
        contentType: 'application/json',
      })
      await expect(fetch).toBeFocused()
      await close(page, edit)
    })

    test('channel states redemption generation success and code cells', async ({
      page,
    }, info) => {
      const vueWarnings: string[] = []
      page.on('console', (message) => {
        if (
          message.type() === 'warning' &&
          message.text().includes('[Vue warn]')
        )
          vueWarnings.push(message.text())
      })
      const fixture = await redemptionStatesFixture(page)
      await page.goto('/redemption-codes')
      await waitForStablePage(page)
      await expect(
        page
          .locator(`[title="${fixture.original.key}"]`)
          .filter({ visible: true })
          .first()
      ).toBeVisible()
      await capture(page, info, 'redemption-code-list')
      const generate = visibleButton(page, copy.redemption.generateCodes)
      await generate.click()
      const dialog = page.getByRole('dialog')
      await dialog
        .getByLabel(copy.redemption.name, { exact: true })
        .fill('UI Regression')
      await dialog
        .getByRole('spinbutton', {
          name: new RegExp(`^${copy.redemption.fieldCount}`),
        })
        .fill('2')
      await capture(page, info, 'redemption-generate')
      await dialog
        .getByRole('button', { name: copy.redemption.generate, exact: true })
        .click()
      const success = page.getByRole('dialog', {
        name: copy.redemption.successTitle,
        exact: true,
      })
      await expect(success).toContainText(fixture.codes[0]!)
      await expect(success).toContainText(fixture.codes[1]!)
      expect(fixture.submitted()).not.toBeNull()
      await capture(page, info, 'redemption-generation-success')
      await page.keyboard.press('Escape')
      await expect(page.getByRole('dialog')).toHaveCount(0)
      expect(
        vueWarnings,
        'numeric v-model remains a string without Vue warnings'
      ).toEqual([])
    })

    test('channel states user affiliate policy dialog', async ({
      page,
    }, info) => {
      await affiliateStatesFixture(page)
      await page.goto('/users')
      await waitForStablePage(page)
      const affiliate = visibleButton(page, copy.users.configureAffiliate)
      await affiliate.click()
      const dialog = page.getByRole('dialog')
      await expect(
        dialog.locator('[name="admin-user-affiliate-code"]')
      ).toHaveValue('UI-INVITER')
      await expect(
        dialog.getByRole('spinbutton', {
          name: copy.users.affiliateRatePercent,
          exact: true,
        })
      ).toHaveValue('12.34')
      await capture(page, info, 'user-affiliate-policy')
      await close(page, affiliate)
    })
  })
}
