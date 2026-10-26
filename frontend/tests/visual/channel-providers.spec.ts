import { expect, test } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

for (const theme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    test(`${theme} ${width} AWS and Vertex channel configuration`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme, authenticated: true })
      let channel = {
        id: 101,
        name: 'Provider Fixture',
        type: 33,
        lab: { id: 'aws', name: 'AWS', source: 'vendor' },
        status: 1,
        priority: 0,
        weight: 10,
        in_flight: 0,
        max_concurrency: 20,
        channel_ratio: '1',
        used_quota: 0,
        balance: 0,
        response_time: 0,
        test_time: 0,
        base_url: '',
        models: 'claude-opus-4-7',
        model_mapping: '',
        other: 'global',
        setting: '{}',
        settings: '{}',
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
              type_counts: { [channel.type]: 1 },
            },
          },
        })
      })
      await page.goto('/channels')
      await waitForStablePage(page)
      await page.locator('[aria-label="编辑渠道"]:visible').first().click()
      let dialog = page.getByRole('dialog', { name: '编辑渠道', exact: true })
      const awsMode = dialog.locator('#channel-aws-api-type')
      await expect(awsMode).toHaveValue('invoke_model')
      await awsMode.focus()
      await expect(awsMode).toBeFocused()
      await awsMode.selectOption('mantle')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: test.info().outputPath('aws-provider.png'),
        fullPage: true,
      })
      await dialog
        .getByRole('button', { name: '保存渠道', exact: true })
        .click()
      await expect(dialog).toHaveCount(0)
      expect(JSON.parse(String(saved!.settings)).aws_api_type).toBe('mantle')

      channel = {
        ...channel,
        type: 41,
        lab: { id: 'google', name: 'Google', source: 'vendor' },
        models: 'gemini-4-argon',
        setting: '{}',
        settings:
          '{"vertex_project_id":"existing-project","vertex_key_type":"api_key"}',
      }
      saved = undefined
      await page.reload()
      await waitForStablePage(page)
      await page.locator('[aria-label="编辑渠道"]:visible').first().click()
      dialog = page.getByRole('dialog', { name: '编辑渠道', exact: true })
      const project = dialog.locator('input[name="channel-vertex-project-id"]')
      await expect(project).toHaveValue('existing-project')
      await project.fill('updated-project')
      await dialog.locator('[name="channel-vertex-region"]').fill('eu')
      await page.keyboard.press('Tab')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: test.info().outputPath('vertex-provider.png'),
        fullPage: true,
      })
      await dialog
        .getByRole('button', { name: '保存渠道', exact: true })
        .click()
      await expect(dialog).toHaveCount(0)
      expect(JSON.parse(String(saved!.settings)).vertex_project_id).toBe(
        'updated-project'
      )
      expect(saved!.other).toBe('eu')
    })
  }
}
