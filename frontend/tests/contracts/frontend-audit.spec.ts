import { mkdirSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import { nativeUsageSummary } from './channel-core-fixtures'

const evidence = process.env.REN2HUB_UI_OUTPUT!
const now = Date.parse('2026-07-27T04:00:00Z') / 1000
mkdirSync(`${evidence}/screenshots`, { recursive: true })

const screens = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'mobile', width: 390, height: 844 },
] as const
const copy = {
  'zh-CN': {
    create: '新建渠道',
    edit: '编辑渠道',
    save: '保存渠道',
    clear: '清除全部',
    regionRequired: '请填写 Vertex 区域。',
    modelsRequired: '请至少选择一个模型。停用渠道请使用禁用操作。',
    bound: '已绑定',
    signInOidc: '使用 Audit SSO 登录',
    product: '充值商品',
    credit: '到账 USD 额度',
    balanceAfter: '到账后余额',
    topup: '立即充值',
    take: '领取',
    assignee: '负责人',
  },
  en: {
    create: 'New channel',
    edit: 'Edit channel',
    save: 'Save channel',
    clear: 'Clear all',
    regionRequired: 'Enter a Vertex region.',
    modelsRequired: 'Select at least one model. Use Disable to stop a channel.',
    bound: 'Bound',
    signInOidc: 'Sign in with Audit SSO',
    product: 'Recharge product',
    credit: 'Credit received (USD)',
    balanceAfter: 'Balance after',
    topup: 'Top up now',
    take: 'Take',
    assignee: 'Assignee',
  },
} as const

async function capture(page: Page, name: string) {
  await assertNoHorizontalOverflow(page)
  await page.screenshot({
    path: `${evidence}/screenshots/frontend-audit-${name}.png`,
    fullPage: true,
  })
}

const regionJSON = '{\n  "default": "us-central1"\n}'
const vertex = {
  id: 41,
  name: 'Audit Vertex',
  type: 41,
  channel_ratio: '1',
  max_concurrency: 0,
  in_flight: 0,
  lab: { id: 'google', name: 'Google', source: 'vendor' },
  status: 1,
  priority: 0,
  weight: 0,
  used_quota: 0,
  balance: 0,
  response_time: 0,
  test_time: 0,
  base_url: '',
  models: 'gemini-2.5-pro',
  model_mapping: '',
  other: regionJSON,
  settings: '{"vertex_key_type":"json"}',
}

for (const locale of ['zh-CN', 'en'] as const)
  for (const theme of ['light', 'dark'] as const)
    for (const screen of screens) {
      const key = `${screen.name}-${theme}-${locale}`
      const text = copy[locale]

      test(`frontend audit channel validation ${key}`, async ({ page }) => {
        await page.setViewportSize(screen)
        const fixture = await configureStablePage(page, { theme, locale })
        const mutations: Array<{ method: string; body: unknown }> = []
        await page.route(/\/api\/channel\/(?:\?.*)?$/, async (route) => {
          const request = route.request()
          if (request.method() !== 'GET') {
            mutations.push({
              method: request.method(),
              body: request.postDataJSON(),
            })
            return route.fulfill({ json: { success: true, data: vertex } })
          }
          return route.fulfill({
            json: {
              success: true,
              data: { total: 1, items: [vertex], type_counts: { 41: 1 } },
            },
          })
        })
        await page.goto('/channels')
        await waitForStablePage(page)
        await page
          .getByRole('button', { name: text.create, exact: true })
          .click()
        const dialog = page.getByRole('dialog')
        await dialog.locator('button[aria-expanded]').first().click()
        await page.getByRole('option').filter({ hasText: 'Vertex AI' }).click()
        await dialog
          .locator('input[name="admin-channel-name"]')
          .fill('New audit Vertex')
        await dialog
          .locator('input[name="admin-channel-key"]')
          .fill('{"project_id":"audit-project"}')
        const modelInput = dialog.locator(
          '.channel-form-section--models input[type="text"]'
        )
        await modelInput.fill('gemini-2.5-pro')
        await modelInput.press('Enter')
        const region = dialog.locator('textarea[name="channel-vertex-region"]')
        const save = dialog.getByRole('button', {
          name: text.save,
          exact: true,
        })
        await region.fill(' \t\n ')
        await expect(
          dialog.getByText(text.regionRequired, { exact: true })
        ).toBeVisible()
        await expect(save).toBeDisabled()
        expect(mutations).toEqual([])
        await region.scrollIntoViewIfNeeded()
        await capture(page, `channel-create-blank-${key}`)
        await region.fill(regionJSON)
        await dialog
          .getByRole('button', { name: 'Remove gemini-2.5-pro', exact: true })
          .click()
        await expect(
          dialog.getByText(text.modelsRequired, { exact: true })
        ).toBeVisible()
        await expect(save).toBeDisabled()
        expect(mutations).toEqual([])
        await modelInput.fill('gemini-2.5-pro')
        await modelInput.press('Enter')
        await expect(save).toBeEnabled()
        await save.click()
        await expect(dialog).toHaveCount(0)
        expect(mutations).toHaveLength(1)
        expect(mutations[0]).toMatchObject({
          method: 'POST',
          body: { channel: { other: regionJSON, models: 'gemini-2.5-pro' } },
        })

        await page
          .getByRole('button', { name: text.edit, exact: true })
          .filter({ visible: true })
          .first()
          .click()
        await region.fill(' ')
        await expect(save).toBeDisabled()
        await expect(
          dialog.getByText(text.regionRequired, { exact: true })
        ).toBeVisible()
        await region.fill(regionJSON)
        await dialog
          .getByRole('button', { name: text.clear, exact: true })
          .click()
        await expect(
          dialog.getByText(text.modelsRequired, { exact: true })
        ).toBeVisible()
        await expect(save).toBeDisabled()
        expect(mutations).toHaveLength(1)
        await dialog
          .getByText(text.modelsRequired, { exact: true })
          .scrollIntoViewIfNeeded()
        await capture(page, `channel-edit-empty-models-${key}`)
        await modelInput.fill('gemini-2.5-pro')
        await modelInput.press('Enter')
        await expect(save).toBeEnabled()
        await save.click()
        await expect(dialog).toHaveCount(0)
        expect(mutations[1]).toMatchObject({
          method: 'PUT',
          body: { id: 41, other: regionJSON, models: 'gemini-2.5-pro' },
        })
        expect(fixture.errors).toEqual([])
      })

      test(`frontend audit bound OIDC display ${key}`, async ({ page }) => {
        await page.setViewportSize(screen)
        await configureStablePage(page, {
          theme,
          locale,
          user: { oidc_id: 'audit-company-user' },
          status: { oidc_enabled: true, oidc_display_name: '  Audit SSO  ' },
        })
        await page.goto('/security')
        await waitForStablePage(page)
        const label = page.getByText('Audit SSO', { exact: true })
        const binding = label.locator('xpath=../../..')
        await expect(label).toBeVisible()
        await expect(binding).toContainText('audit-company-user')
        await expect(
          binding.getByText(text.bound, { exact: true })
        ).toBeVisible()
        await expect(binding.getByRole('button')).toHaveCount(0)
        await binding.scrollIntoViewIfNeeded()
        await capture(page, `bound-oidc-${key}`)
        await page.route('**/api/user/auth/refresh', (route) =>
          route.fulfill({
            status: 401,
            json: { success: false, message: 'Unauthenticated' },
          })
        )
        await page.goto('/sign-in')
        await waitForStablePage(page)
        await expect(
          page.getByRole('button', { name: text.signInOidc, exact: true })
        ).toBeVisible()
        await capture(page, `oidc-sign-in-${key}`)
      })

      test(`frontend audit wallet without statistics ${key}`, async ({
        page,
      }) => {
        await page.setViewportSize(screen)
        const fixture = await configureStablePage(page, {
          theme,
          locale,
          user: { quota: 5000000, used_quota: 1000000 },
        })
        await page.route(/\/api\/data\/self\/summary(?:\?.*)?$/, (route) => {
          const summary = nativeUsageSummary(now, false)
          summary.meta.available = false
          return route.fulfill({ json: { success: true, data: summary } })
        })
        await page.route('**/api/user/topup/info', (route) =>
          route.fulfill({
            json: {
              success: true,
              data: {
                enable_online_topup: false,
                enable_stripe_topup: false,
                enable_creem_topup: true,
                enable_waffo_topup: false,
                enable_waffo_pancake_topup: false,
                enable_gmpay_topup: false,
                enable_redemption: false,
                pay_methods: [],
                waffo_pay_methods: null,
                creem_products: JSON.stringify([
                  {
                    productId: 'audit-small',
                    name: 'Audit small',
                    price: 7,
                    currency: 'USD',
                    quota: 1500000,
                  },
                  {
                    productId: 'audit-large',
                    name: 'Audit large',
                    price: 12,
                    currency: 'USD',
                    quota: 3500000,
                  },
                ]),
                min_topup: 1,
                stripe_min_topup: 1,
                waffo_min_topup: 1,
                waffo_pancake_min_topup: 1,
                amount_options: [10],
                discount: {},
                topup_link: '',
              },
            },
          })
        )
        await page.goto('/wallet')
        await waitForStablePage(page)
        await expect(page.locator('.display-number.text-5xl')).toHaveText('10')
        const credit = page
          .getByText(text.credit, { exact: true })
          .locator('..')
        const projected = page
          .getByText(text.balanceAfter, { exact: true })
          .locator('..')
        await expect(credit).toContainText('$3.000')
        await expect(projected).toContainText('$13.00')
        await expect(
          page.getByRole('button', { name: text.topup, exact: true })
        ).toBeEnabled()
        await capture(page, `wallet-balance-${key}`)
        await page
          .getByRole('combobox', { name: text.product, exact: true })
          .selectOption('audit-large')
        await expect(credit).toContainText('$7.000')
        await expect(projected).toContainText('$17.00')
        await projected.scrollIntoViewIfNeeded()
        await capture(page, `wallet-creem-${key}`)
        expect(
          fixture.writes.filter((write) => /\/pay$/.test(write.path))
        ).toEqual([])
      })

      test(`frontend audit ticket self assignment permissions ${key}`, async ({
        page,
      }) => {
        await page.setViewportSize(screen)
        const ticketPermissions = { read: true, manage: true, reply: false }
        await configureStablePage(page, {
          theme,
          locale,
          user: {
            role: 10,
            permissions: { admin_permissions: { ticket: ticketPermissions } },
          },
        })
        const ticket = {
          id: 1,
          title: 'Audit assignment permission',
          status: 'open',
          category: 'billing',
          priority: 'normal',
          reply_count: 0,
          message_count: 1,
          last_reply_role: 'user',
          model_id: '',
          request_id: '',
          created: now,
          updated: now,
          assignee_id: null as number | null,
          assignee_name: '',
          assigned_at: 0,
          user: {
            id: 8,
            username: 'audit.requester',
            display_name: 'Audit Requester',
            email: 'requester@example.test',
          },
          user_deleted: false,
        }
        const assignmentWrites: unknown[] = []
        await page.route(/\/api\/admin\/tickets(?:\/|\?|$)/, async (route) => {
          const request = route.request()
          const path = new URL(request.url()).pathname
          const ok = (data: unknown) =>
            route.fulfill({ json: { success: true, data } })
          if (path.endsWith('/agents'))
            return ok({
              items: [
                { id: 1, username: 'native.root', display_name: 'Native Root' },
              ],
            })
          if (path.endsWith('/summary'))
            return ok({
              pending: 1,
              unassigned: ticket.assignee_id ? 0 : 1,
              mine: ticket.assignee_id ? 1 : 0,
            })
          if (path.endsWith('/assignee')) {
            expect(request.method()).toBe('PATCH')
            assignmentWrites.push(request.postDataJSON())
            ticket.assignee_id = request.postDataJSON().assignee_id
            ticket.assignee_name = 'Native Root'
            ticket.assigned_at = now
            return ok({ ticket })
          }
          if (path === '/api/admin/tickets/1')
            return ok({
              ticket,
              messages: [
                {
                  id: 1,
                  role: 'user',
                  content: 'Please assign this ticket.',
                  images: [],
                  created: now,
                },
              ],
            })
          if (path === '/api/admin/tickets')
            return ok({ page: 1, page_size: 20, total: 1, items: [ticket] })
          return route.fallback()
        })
        await page.goto('/ticket-management/1')
        await waitForStablePage(page)
        const assigneeControl = page
          .getByRole('heading', { name: text.assignee, exact: true })
          .locator('../..')
          .getByRole('combobox', { name: text.assignee, exact: true })
        await expect(
          page.getByText('Please assign this ticket.', { exact: true })
        ).toBeVisible()
        await expect(
          page.getByRole('button', { name: text.take, exact: true })
        ).toHaveCount(0)
        await expect(assigneeControl).toBeEnabled()
        expect(assignmentWrites).toEqual([])
        await assigneeControl.scrollIntoViewIfNeeded()
        await capture(page, `ticket-without-reply-${key}`)

        ticketPermissions.reply = true
        await page.reload()
        await waitForStablePage(page)
        const take = page.getByRole('button', { name: text.take, exact: true })
        await expect(take).toBeVisible()
        await take.click()
        await expect.poll(() => assignmentWrites).toEqual([{ assignee_id: 1 }])
        await expect(take).toHaveCount(0)
        await expect(assigneeControl).toContainText('Native Root')
        await assigneeControl.scrollIntoViewIfNeeded()
        await capture(page, `ticket-assigned-${key}`)
      })
    }
