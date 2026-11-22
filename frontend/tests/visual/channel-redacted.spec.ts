import { expect, test } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  captureEvidence,
} from './fixtures'

const hidden = JSON.stringify({ __ren2hub_redacted__: true })
const header = JSON.stringify({ 'X-Upstream-Token': 'real-secret' })
const param = JSON.stringify({ metadata: { api_key: 'real-secret' } })
const setting = JSON.stringify({
  proxy: 'http://user:pass@proxy.example:8080',
  pass_through_body_enabled: false,
})
const settings = JSON.stringify({
  advanced_custom: {
    advanced_routes: [
      {
        incoming_path: '/v1/responses',
        upstream_path: '/v1/responses',
        converter: 'none',
        auth: { type: 'header', name: 'Authorization', value: 'Bearer real' },
      },
    ],
  },
})

// The backend decides redaction: root with channel:write reads the stored
// values, any other administrator receives placeholders and redacted_fields.
for (const [theme, width] of [
  ['light', 1440],
  ['dark', 390],
] as const)
  for (const role of [100, 10] as const)
    test(`channel form hidden fields ${theme} ${width} role=${role}`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, {
        theme,
        authenticated: true,
        user:
          role === 100
            ? { role }
            : {
                role,
                permissions: {
                  admin_permissions: {
                    channel: {
                      read: true,
                      write: true,
                      operate: true,
                      sensitive_write: true,
                    },
                  },
                },
              },
      })
      const channel = {
        id: 101,
        name: 'Hidden Fixture',
        type: 58,
        lab: { id: 'custom', name: 'Custom', source: 'fallback' },
        status: 1,
        priority: 0,
        weight: 10,
        in_flight: 0,
        max_concurrency: 0,
        channel_ratio: '1',
        used_quota: 0,
        balance: 0,
        response_time: 0,
        test_time: 0,
        base_url: 'https://upstream.example',
        models: 'fixture-model',
        model_mapping: '',
        ...(role === 100
          ? {
              header_override: header,
              param_override: param,
              setting,
              settings,
            }
          : {
              header_override: hidden,
              param_override: hidden,
              setting: hidden,
              settings: hidden,
              redacted_fields: [
                'header_override',
                'param_override',
                'setting',
                'settings',
              ],
            }),
      }
      let saved: Record<string, unknown> | undefined
      await page.route(/\/api\/channel\/(?:\?.*)?$/, (route) => {
        if (route.request().method() === 'PUT') {
          saved = route.request().postDataJSON()
          return route.fulfill({ json: { success: true, data: null } })
        }
        return route.fulfill({
          json: {
            success: true,
            data: {
              items: [channel],
              total: 1,
              page: 1,
              page_size: 20,
              type_counts: { '58': 1 },
            },
          },
        })
      })
      await page.goto('/channels')
      await waitForStablePage(page)
      await page.locator('[aria-label="编辑渠道"]:visible').first().click()
      const dialog = page.getByRole('dialog', { name: '编辑渠道', exact: true })
      await dialog
        .getByRole('button', { name: /高级设置/ })
        .first()
        .click()
      const headerInput = dialog.getByLabel('请求头覆盖 JSON', { exact: true })
      const paramInput = dialog.getByLabel('参数覆盖 JSON', { exact: true })
      await headerInput.scrollIntoViewIfNeeded()

      if (role === 100) {
        await expect(headerInput).toHaveValue(header)
        await expect(paramInput).toHaveValue(param)
        await expect(dialog.getByText('已隐藏，未修改则保留原值')).toHaveCount(
          0
        )
        await expect(
          dialog.getByRole('switch', { name: '透传请求体' })
        ).toBeVisible()
      } else {
        await expect(headerInput).toHaveValue('')
        await expect(paramInput).toHaveValue('')
        await expect(
          dialog.getByText('已隐藏，未修改则保留原值；输入新内容将整体替换。', {
            exact: true,
          })
        ).toHaveCount(2)
        await expect(
          dialog.getByText(/代理、插件与透传等设置含已隐藏的凭据/)
        ).toBeVisible()
        await expect(
          dialog.getByText(/高级路由设置含已隐藏的凭据/)
        ).toBeVisible()
        await expect(
          dialog.getByRole('switch', { name: '透传请求体' })
        ).toHaveCount(0)
        await expect(dialog.getByText(hidden)).toHaveCount(0)
      }
      await assertNoHorizontalOverflow(page)
      await captureEvidence(dialog, {
        path: info.outputPath(`channel-hidden-${role}.png`),
      })

      if (role === 10) {
        // Clearing a hidden override needs an explicit, confirmed action.
        await dialog
          .getByRole('button', { name: '清空请求头覆盖', exact: true })
          .click()
        const confirm = page.getByRole('dialog', { name: '清空请求头覆盖' })
        await expect(
          confirm.getByText('保存渠道后，已隐藏的请求头覆盖将被删除。')
        ).toBeVisible()
        await assertNoHorizontalOverflow(page)
        await captureEvidence(page, {
          path: info.outputPath(`channel-hidden-clear-${role}.png`),
        })
        await confirm.getByRole('button', { name: '清空', exact: true }).click()
        await expect(confirm).toHaveCount(0)
        await expect(
          dialog.getByText('保存后将清空请求头覆盖。', { exact: true })
        ).toBeVisible()

        await paramInput.scrollIntoViewIfNeeded()
        await captureEvidence(dialog, {
          path: info.outputPath(`channel-param-hidden-${role}.png`),
        })
        await dialog
          .getByRole('button', { name: '清空参数覆盖', exact: true })
          .click()
        const paramConfirm = page.getByRole('dialog', { name: '清空参数覆盖' })
        await expect(
          paramConfirm.getByText('保存渠道后，已隐藏的参数覆盖将被删除。')
        ).toBeVisible()
        await assertNoHorizontalOverflow(page)
        await captureEvidence(page, {
          path: info.outputPath(`channel-param-clear-${role}.png`),
        })
        await paramConfirm
          .getByRole('button', { name: '清空', exact: true })
          .click()
        await expect(paramConfirm).toHaveCount(0)
        await expect(
          dialog.getByText('保存后将清空参数覆盖。', { exact: true })
        ).toBeVisible()
      }
      await dialog
        .getByRole('button', { name: '保存渠道', exact: true })
        .click()
      await expect.poll(() => saved).toBeDefined()
      if (role === 100) {
        expect(saved).not.toHaveProperty('header_override')
        expect(saved).not.toHaveProperty('param_override')
      } else {
        // An explicit empty string clears the hidden value.
        expect(saved).toHaveProperty('header_override', '')
        expect(saved).toHaveProperty('param_override', '')
        expect(saved).not.toHaveProperty('setting')
        expect(saved).not.toHaveProperty('settings')
      }
    })
