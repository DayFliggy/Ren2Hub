import { mkdirSync } from 'node:fs'

import { expect, test } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

/** Optional screenshot directory for the cross-audit. */
const shotsDir = process.env.REN2HUB_AFFINITY_SHOTS
// Complete rules as the editor saves them, so the form shows no validation noise.
const rule = (name: string, includeRuleName: boolean) => ({
  name,
  model_regex: [],
  path_regex: ['/v1/responses'],
  user_agent_include: [],
  key_sources: [{ type: 'gjson', path: 'prompt_cache_key' }],
  value_regex: '',
  ttl_seconds: 3600,
  skip_retry_on_failure: false,
  include_model_name: true,
  include_rule_name: includeRuleName,
  param_override_template: {},
})
const rules = JSON.stringify([
  rule('codex-cli-session', true),
  rule('claude-code-session-with-a-long-rule-name', true),
  rule('shared-rule', false),
])

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`affinity rule caches ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme })
      const byRule: Record<string, number> = {
        'claude-code-session-with-a-long-rule-name': 1280,
        'codex-cli-session': 42,
      }
      const deletes: string[] = []
      await page.route('**/api/option/', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: [
              { key: 'channel_affinity_setting.enabled', value: 'true' },
              {
                key: 'channel_affinity_setting.switch_on_success',
                value: 'true',
              },
              {
                key: 'channel_affinity_setting.default_ttl_seconds',
                value: '3600',
              },
              {
                key: 'channel_affinity_setting.keep_on_channel_disabled',
                value: 'false',
              },
              { key: 'channel_affinity_setting.max_entries', value: '100000' },
              { key: 'channel_affinity_setting.rules', value: rules },
            ],
          },
        })
      )
      await page.route('**/api/option/channel_affinity_cache**', (route) => {
        const url = new URL(route.request().url())
        if (route.request().method() === 'DELETE') {
          deletes.push(url.search)
          const name = url.searchParams.get('rule_name')
          if (name) byRule[name] = 0
          return route.fulfill({ json: { success: true, data: {} } })
        }
        return route.fulfill({
          json: {
            success: true,
            data: {
              enabled: true,
              total: 1330,
              unknown: 8,
              by_rule_name: byRule,
              cache_capacity: 100000,
              cache_algo: 'lru',
            },
          },
        })
      })
      await page.goto('/system-settings/models/affinity')
      await waitForStablePage(page)

      const list = page.locator('[data-affinity-rules]')
      await expect(list.locator('li')).toHaveCount(2)
      await expect(list).toContainText('无法单独清空')
      await expect(list).not.toContainText('shared-rule')
      await list.scrollIntoViewIfNeeded()
      for (const row of await list.locator('li').all()) {
        const rowBox = (await row.boundingBox())!
        const button = (await row.getByRole('button').boundingBox())!
        expect(button.x + button.width).toBeLessThanOrEqual(
          rowBox.x + rowBox.width + 0.5
        )
      }
      await assertNoHorizontalOverflow(page)
      if (shotsDir) {
        mkdirSync(shotsDir, { recursive: true })
        await page.screenshot({
          path: `${shotsDir}/affinity-${theme}-${mobile ? 'mobile' : 'desktop'}.png`,
        })
      }

      await list
        .getByRole('button', { name: '清空“codex-cli-session”的缓存' })
        .click()
      const dialog = page.getByRole('dialog')
      await expect(dialog).toContainText(
        '确定清空“codex-cli-session”缓存的会话绑定？后续请求将重新选择渠道。'
      )
      await dialog.getByRole('button', { name: '确认执行' }).click()
      await expect(dialog).toBeHidden()
      expect(deletes).toEqual(['?rule_name=codex-cli-session'])
      await expect(
        list.locator('li').filter({ hasText: 'codex-cli-session' })
      ).toContainText('0 条')
    })
  }
}
