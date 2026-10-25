import {
  expect,
  test,
  type Locator,
  type Page,
  type TestInfo,
} from '@playwright/test'
import en from '../../src/i18n/locales/en/console'
import zh from '../../src/i18n/locales/zh-CN/console'
import { clientConfigMessages } from '../../src/i18n/clientConfig'
import { modelMetadataMessages } from '../../src/i18n/modelMetadata'
import { modelPricingMessages } from '../../src/i18n/modelPricing'
import { personalAccessTokenMessages } from '../../src/i18n/personalAccessTokens'
import { nativePaymentMessages } from '../../src/i18n/nativePayment'
import { nativeTopUpDetail } from '../../src/api/__tests__/topupFixtures'
import {
  configureStablePage,
  assertNoHorizontalOverflow,
  waitForStablePage,
} from './fixtures'
import { nativeUser } from './native-core-fixture'
import { captureUI } from './ui-coverage'

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

const focusable =
  'a[href],button:not([disabled]),input:not([disabled]),textarea:not([disabled]),select:not([disabled]),[tabindex]:not([tabindex="-1"])'

async function checkpoint(
  page: Page,
  info: TestInfo,
  label: string,
  dialog?: Locator,
  region?: Locator
) {
  await assertNoHorizontalOverflow(page)
  if (dialog) {
    await expect(dialog).toBeVisible()
    const controls = dialog.locator(focusable).filter({ visible: true })
    await controls.last().focus()
    await page.keyboard.press('Tab')
    await expect(controls.first()).toBeFocused()
    await page.keyboard.press('Shift+Tab')
    await expect(controls.last()).toBeFocused()
  }
  if (region) await region.scrollIntoViewIfNeeded()
  await captureUI(page, info, label)
}

async function escapeTo(page: Page, trigger: Locator, dialogCount = 0) {
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toHaveCount(dialogCount)
  await expect(trigger).toBeFocused()
}

async function namedInput(input: Locator) {
  await expect(input).toBeVisible()
  expect(
    await input.evaluate((element) => {
      const field = element as HTMLInputElement
      return Boolean(
        field.getAttribute('aria-label')?.trim() ||
        [...(field.labels ?? [])].some((label) => label.textContent?.trim())
      )
    })
  ).toBe(true)
}

for (const scenario of scenarios) {
  const copy = scenario.locale === 'en' ? en : zh
  const client = clientConfigMessages[scenario.locale]
  const model = modelMetadataMessages[scenario.locale]
  const pricing = modelPricingMessages[scenario.locale]
  const pat = personalAccessTokenMessages[scenario.locale].pat

  test.describe(scenario.key, () => {
    test.beforeEach(async ({ page }) => {
      await page.setViewportSize(scenario)
      await configureStablePage(page, scenario)
    })

    test('component interactions key editor and client configuration', async ({
      page,
    }, info) => {
      await page.goto('/keys')
      await waitForStablePage(page)
      const create = page.getByRole('button', {
        name: copy.keys.createKey,
        exact: true,
      })
      await create.click()
      let dialog = page.getByRole('dialog')
      await namedInput(dialog.locator('[name="token-name"]'))
      await dialog.locator('[name="token-name"]').fill('UI regression key')
      await checkpoint(page, info, 'key-create', dialog)
      const advanced = dialog.locator('button[aria-expanded]')
      await advanced.click()
      await expect(dialog.locator('[name="token-ip-limits"]')).toBeVisible()
      await checkpoint(
        page,
        info,
        'key-create-advanced',
        dialog,
        dialog.locator('[name="token-ip-limits"]')
      )
      await escapeTo(page, create)

      const config = page
        .getByRole('button', { name: client.title, exact: true })
        .filter({ visible: true })
        .first()
      await config.click()
      dialog = page.getByRole('dialog')
      await expect(
        dialog.getByRole('combobox', { name: client.model, exact: true })
      ).toBeEnabled()
      await namedInput(
        dialog.getByRole('textbox', {
          name: client.server_address,
          exact: true,
        })
      )
      await checkpoint(page, info, 'client-config-codex', dialog)
      await dialog
        .getByRole('combobox', { name: client.client, exact: true })
        .selectOption('pi')
      await namedInput(
        dialog.getByRole('spinbutton', {
          name: client.context_window,
          exact: true,
        })
      )
      await checkpoint(
        page,
        info,
        'client-config-pi',
        dialog,
        dialog.getByRole('spinbutton', {
          name: client.context_window,
          exact: true,
        })
      )
      await escapeTo(page, config)
    })

    test('component interactions personal access token scope editor', async ({
      page,
    }, info) => {
      await page.goto('/security')
      await waitForStablePage(page)
      const create = page
        .getByTestId('pat-panel')
        .getByRole('button', { name: pat.create, exact: true })
      await create.click()
      const dialog = page.getByRole('dialog')
      await namedInput(
        dialog.getByRole('textbox', { name: pat.name, exact: true })
      )
      await expect(dialog.getByRole('checkbox')).toHaveCount(1)
      await dialog.getByRole('checkbox').check()
      await expect(dialog.getByRole('checkbox')).toBeChecked()
      await checkpoint(page, info, 'pat-create-scopes', dialog)
      await escapeTo(page, create)
    })

    test('component interactions channel advanced and provider controls', async ({
      page,
    }, info) => {
      await page.goto('/channels')
      await waitForStablePage(page)
      const create = page.getByRole('button', {
        name: copy.channels.createTitle,
        exact: true,
      })
      await create.click()
      const dialog = page.getByRole('dialog')
      await namedInput(dialog.locator('[name="admin-channel-name"]'))
      await dialog.locator('.channel-form-section--advanced > button').click()
      const concurrency = dialog.locator('[name="channel-max-concurrency"]')
      const ratio = dialog.locator('[name="channel-ratio"]')
      await namedInput(concurrency)
      await namedInput(ratio)
      await concurrency.fill('4')
      await ratio.fill('0.123456789012345678')
      await expect(ratio).toHaveValue('0.123456789012345678')
      await expect(
        ratio.locator('..').locator('[aria-hidden="true"]')
      ).toHaveText('×')
      await checkpoint(page, info, 'channel-advanced', dialog, ratio)
      await dialog
        .locator('.channel-form-section--basic button[aria-expanded]')
        .click()
      await page.getByRole('option').filter({ hasText: 'Vertex AI' }).click()
      await expect(
        dialog.locator('[name="channel-vertex-region"]')
      ).toBeVisible()
      await namedInput(dialog.locator('[name="channel-vertex-region"]'))
      await dialog
        .locator('[name="channel-vertex-region"]')
        .fill('{"default":"global"}')
      await checkpoint(
        page,
        info,
        'channel-vertex',
        dialog,
        dialog.locator('[name="channel-vertex-region"]')
      )
      await dialog
        .locator('.channel-form-section--basic button[aria-expanded]')
        .click()
      await page.getByRole('option').filter({ hasText: 'AWS' }).click()
      await expect(dialog.locator('#channel-aws-api-type')).toBeVisible()
      await dialog.locator('#channel-aws-api-type').selectOption('mantle')
      await checkpoint(
        page,
        info,
        'channel-aws',
        dialog,
        dialog.locator('#channel-aws-api-type')
      )
      await escapeTo(page, create)
    })

    test('component interactions administrator permissions bindings and quota', async ({
      page,
    }, info) => {
      const user = nativeUser(
        {
          id: 2,
          role: 10,
          username: 'ui.admin',
          display_name: 'UI administrator',
          email: 'ui.admin@example.test',
          DeletedAt: null,
          inviter_id: 0,
          last_login_at: 1785124800,
          admin_permissions: { channel: { read: true } },
        },
        scenario.locale
      )
      await page.route(/\/api\/user\/(?:\?.*)?$/, (route) =>
        route.fulfill({
          json: {
            success: true,
            data: { items: [user], total: 1, page: 1, page_size: 20 },
          },
        })
      )
      await page.route('**/api/user/2', (route) =>
        route.fulfill({ json: { success: true, data: user } })
      )
      await page.route('**/api/user/2/oauth/bindings', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: [{ provider_id: 2, provider_name: 'UI SSO' }],
          },
        })
      )
      await page.route('**/api/authz/catalog', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              resources: [
                {
                  resource: 'channel',
                  label_key: 'Channel Management',
                  actions: [
                    {
                      action: 'read',
                      label_key: 'Read channels',
                      description_key:
                        'View channel lists and details without secrets.',
                    },
                    {
                      action: 'secret_view',
                      label_key: 'View channel secrets',
                      description_key:
                        'Reserved for viewing complete channel keys after secure verification.',
                    },
                  ],
                },
              ],
              roles: [],
            },
          },
        })
      )
      await page.goto('/users')
      await waitForStablePage(page)
      const edit = page
        .getByRole('button', { name: copy.users.editUser, exact: true })
        .filter({ visible: true })
        .first()
      await edit.click()
      const dialog = page.getByRole('dialog').first()
      await namedInput(dialog.locator('[name="admin-user-max-concurrency"]'))
      await expect(
        dialog.locator('fieldset input[type="checkbox"]')
      ).toHaveCount(2)
      await checkpoint(
        page,
        info,
        'user-edit-permissions',
        dialog,
        dialog.locator('fieldset').first()
      )
      const bindings = dialog.locator('[data-admin-bindings]')
      await bindings.click()
      const bindingDialog = page.getByRole('dialog').last()
      await expect(bindingDialog).toContainText('UI SSO')
      await checkpoint(page, info, 'user-bindings', bindingDialog)
      await escapeTo(page, bindings, 1)
      await escapeTo(page, edit)

      const quota = page
        .getByRole('button', { name: copy.users.adjustQuota, exact: true })
        .filter({ visible: true })
        .first()
      await quota.click()
      const quotaDialog = page.getByRole('dialog')
      await namedInput(quotaDialog.locator('[name="admin-user-quota-amount"]'))
      await quotaDialog.locator('[name="admin-user-quota-amount"]').fill('2.5')
      await checkpoint(page, info, 'user-quota', quotaDialog)
      await escapeTo(page, quota)
    })

    test('component interactions order detail and manual completion confirmation', async ({
      page,
    }, info) => {
      // parseTopUpRanking: native numeric quota and paid count, scoped to the
      // separate admin statistics endpoint rather than the order-detail body.
      await page.route(/\/api\/user\/topup\/ranking(?:\?.*)?$/, (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              items: [
                {
                  user_id: 7,
                  username: 'payer',
                  paid_count: 3,
                  credited_quota: 5000000,
                },
              ],
              start_time: 1782532800,
              end_time: 1785124800,
            },
          },
        })
      )
      await page.route('**/api/user/topup/admin/detail/native-order', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              ...nativeTopUpDetail,
              trade_no: 'native-order',
              user_id: 1,
              create_time: 1785124800,
            },
          },
        })
      )
      await page.goto('/orders')
      await waitForStablePage(page)
      await page
        .getByRole('tab', { name: copy.orders.tabList, exact: true })
        .click()
      const detail = page
        .getByRole('button', { name: copy.orders.viewOrder, exact: true })
        .filter({ visible: true })
        .first()
      await detail.click()
      const dialog = page.getByRole('dialog').first()
      await expect(dialog).toContainText('native-order')
      await checkpoint(page, info, 'order-details', dialog)
      const complete = dialog.getByRole('button', {
        name: nativePaymentMessages[scenario.locale].nativePayment.complete,
        exact: true,
      })
      await complete.click()
      const confirmation = page.getByRole('dialog').last()
      await checkpoint(page, info, 'order-complete-confirm', confirmation)
      await escapeTo(page, complete, 1)
      await escapeTo(page, detail)
    })

    test('component interactions ticket composer and detail', async ({
      page,
    }, info) => {
      await page.goto('/tickets')
      await waitForStablePage(page)
      const create = page.getByRole('button', {
        name: copy.tickets.newTicket,
        exact: true,
      })
      await create.click()
      const dialog = page.getByRole('dialog')
      await namedInput(dialog.locator('[name="ticket-title"]'))
      await dialog.locator('[name="ticket-title"]').fill('UI support question')
      await checkpoint(page, info, 'ticket-create', dialog)
      await escapeTo(page, create)
      await page
        .getByRole('button', { name: 'Native billing question', exact: true })
        .filter({ visible: true })
        .click()
      await expect(page).toHaveURL(/\/tickets\/1$/)
      await expect(
        page.getByText('Please check this native order.', { exact: true })
      ).toBeVisible()
      await checkpoint(page, info, 'ticket-details')
    })

    test('component interactions model metadata vendor pricing and menus', async ({
      page,
    }, info) => {
      await page.goto('/models/metadata')
      await waitForStablePage(page)
      const createModel = page.getByRole('button', {
        name: model.addModel,
        exact: true,
      })
      await createModel.click()
      let dialog = page.getByRole('dialog')
      await namedInput(dialog.locator('#model-metadata-name'))
      await checkpoint(page, info, 'model-metadata-create', dialog)
      const statusSection = dialog.locator('.drawer-section').filter({
        has: page.getByRole('heading', { name: model.statusSync, exact: true }),
      })
      await checkpoint(
        page,
        info,
        'model-metadata-endpoint-status-spacing',
        dialog,
        statusSection
      )
      const sections = await dialog
        .locator('.drawer-section')
        .evaluateAll((elements, endpointsTitle) => {
          const endpoints = elements.find((element) =>
            [...element.querySelectorAll('h3')].some(
              (heading) => heading.textContent?.trim() === endpointsTitle
            )
          )
          const status = elements.at(-1)
          return {
            emptySections: elements.filter(
              (element) => !element.textContent?.trim()
            ).length,
            gap:
              status && endpoints
                ? status.getBoundingClientRect().top -
                  endpoints.getBoundingClientRect().bottom
                : null,
            expectedGap: status
              ? Number.parseFloat(getComputedStyle(status).marginTop)
              : null,
          }
        }, model.endpoints)
      await info.attach('metadata-section-spacing', {
        body: JSON.stringify(sections, null, 2),
        contentType: 'application/json',
      })
      expect(sections.emptySections).toBe(0)
      expect(sections.gap).not.toBeNull()
      expect(sections.expectedGap).not.toBeNull()
      expect(sections.gap!).toBeLessThanOrEqual(sections.expectedGap! + 1)
      await escapeTo(page, createModel)
      const menu = page
        .getByRole('button', { name: model.openMenu, exact: true })
        .first()
      await menu.click()
      await expect(page.getByRole('menu')).toBeVisible()
      await checkpoint(page, info, 'model-actions-menu')
      await page.keyboard.press('Escape')
      await expect(menu).toBeFocused()
      const columns = page
        .getByRole('button', { name: model.toggleColumns, exact: true })
        .filter({ visible: true })
        .first()
      await columns.click()
      await checkpoint(page, info, 'model-columns-menu')
      await page.keyboard.press('Escape')

      await page.goto('/models/vendors')
      await waitForStablePage(page)
      const createVendor = page.getByRole('button', {
        name: model.addVendor,
        exact: true,
      })
      await createVendor.click()
      dialog = page.getByRole('dialog')
      await namedInput(dialog.locator('[name="vendor-name"]'))
      await checkpoint(page, info, 'vendor-create', dialog)
      await escapeTo(page, createVendor)

      await page.goto('/models/pricing')
      await waitForStablePage(page)
      const addPrice = page
        .getByRole('button', { name: pricing.add, exact: true })
        .filter({ visible: true })
        .first()
      await addPrice.click()
      if (scenario.width < 768) {
        dialog = page.getByRole('dialog')
        await checkpoint(page, info, 'model-pricing-editor', dialog)
        await escapeTo(page, addPrice)
      } else {
        await expect(page.locator('.pricing-mode')).toBeVisible()
        await checkpoint(page, info, 'model-pricing-editor')
      }
    })
  })
}
