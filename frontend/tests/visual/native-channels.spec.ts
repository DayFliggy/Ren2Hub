import { expect, test } from '@playwright/test'

for (const theme of ['light', 'dark']) {
  for (const width of [1440, 390]) {
    test(`${theme} ${width} native channel group, provider and permission contracts`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 })
      const errors: string[] = []
      const legacyRequests: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      page.on('request', (request) => {
        if (
          request.url().includes('/api/next/') ||
          request.url().includes('/api/gateway/')
        )
          legacyRequests.push(request.url())
      })
      await page.route('**/native-channel-harness', (route) =>
        route.fulfill({
          contentType: 'text/html',
          body: `<!doctype html><html class="${theme === 'dark' ? 'dark' : ''}"><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><div id="app" style="padding:16px;max-width:100%;min-width:0"></div><script type="module" src="/tests/visual/native-channels-harness.ts"></script></body></html>`,
        })
      )
      let channel = {
        id: 101,
        name: 'Native provider fixture',
        type: 33,
        group: 'default',
        status: 1,
        priority: 10,
        weight: 5,
        used_quota: 500000,
        balance: 2.5,
        response_time: 125,
        test_time: 0,
        base_url: '',
        models: 'claude-opus-4-7',
        model_mapping: '',
        other_info: '',
        other: 'global',
        setting: '{}',
        settings: '{}',
      }
      let saved: Record<string, unknown> | undefined
      await page.route(/^http:\/\/127\.0\.0\.1:\d+\/api\//, (route) => {
        const url = new URL(route.request().url())
        if (url.pathname === '/api/channel/') {
          if (route.request().method() === 'PUT') {
            saved = route.request().postDataJSON()
            channel = { ...channel, ...saved }
            return route.fulfill({ json: { success: true, message: '' } })
          }
          return route.fulfill({
            json: {
              success: true,
              data: {
                items: [channel],
                total: 1,
                type_counts: { [channel.type]: 1 },
                page: 1,
                page_size: 20,
              },
            },
          })
        }
        if (
          url.pathname === '/api/prefill_group/' ||
          url.pathname === '/api/task_plugin_options'
        )
          return route.fulfill({ json: { success: true, data: [] } })
        return route.fulfill({
          status: 404,
          json: {
            success: false,
            message: `Unexpected fixture endpoint ${url.pathname}`,
          },
        })
      })
      await page.goto('/native-channel-harness')
      await expect(
        page
          .getByText('Native provider fixture', { exact: false })
          .filter({ visible: true })
          .first()
      ).toBeVisible()
      await page.locator('[aria-label="编辑渠道"]:visible').first().click()
      let dialog = page.getByRole('dialog', { name: '编辑渠道', exact: true })
      await dialog.locator('#channel-aws-api-type').selectOption('mantle')
      await dialog.locator('#channel-aws-key-type').selectOption('api_key')
      await dialog
        .locator('input[name="admin-channel-group"]')
        .fill('default,vip')
      await expect(
        dialog.locator('input[name="admin-channel-ratio"]')
      ).toHaveCount(0)
      await expect(
        dialog.locator('input[name="admin-channel-capacity"]')
      ).toHaveCount(0)
      await expect
        .poll(() =>
          page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth + 1
          )
        )
        .toBe(true)
      await page.screenshot({
        path: test.info().outputPath('native-aws.png'),
        fullPage: true,
      })
      await dialog
        .getByRole('button', { name: '保存渠道', exact: true })
        .click()
      await expect(dialog).toHaveCount(0)
      expect(saved?.group).toBe('default,vip')
      expect(JSON.parse(String(saved?.settings))).toMatchObject({
        aws_api_type: 'mantle',
        aws_key_type: 'api_key',
      })
      expect(saved).not.toHaveProperty('channel_ratio')
      channel = {
        ...channel,
        type: 41,
        models: 'gemini-4-argon',
        settings:
          '{"vertex_key_type":"api_key","vertex_project_id":"old-project"}',
      }
      await page.reload()
      await page.locator('[aria-label="编辑渠道"]:visible').first().click()
      dialog = page.getByRole('dialog', { name: '编辑渠道', exact: true })
      await expect(
        dialog.locator('input[name="channel-vertex-project-id"]')
      ).toHaveValue('old-project')
      await dialog
        .locator('input[name="channel-vertex-project-id"]')
        .fill('new-project')
      await dialog.locator('input[name="channel-vertex-region"]').fill('eu')
      await page.screenshot({
        path: test.info().outputPath('native-vertex.png'),
        fullPage: true,
      })
      await dialog
        .getByRole('button', { name: '保存渠道', exact: true })
        .click()
      await expect(dialog).toHaveCount(0)
      expect(JSON.parse(String(saved?.settings)).vertex_project_id).toBe(
        'new-project'
      )
      expect(saved?.other).toBe('eu')
      await page.evaluate(() => {
        const auth = (
          window as unknown as {
            channelFixtureAuth: { user: Record<string, unknown> }
          }
        ).channelFixtureAuth
        auth.user = {
          ...auth.user,
          role: 10,
          permissions: { admin_permissions: { channel: { read: true } } },
        }
      })
      await expect(page.locator('[aria-label="编辑渠道"]:visible')).toHaveCount(
        0
      )
      await expect(
        page.getByRole('button', { name: '新建渠道', exact: true })
      ).toHaveCount(0)
      await expect
        .poll(() =>
          page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth + 1
          )
        )
        .toBe(true)
      expect(legacyRequests).toEqual([])
      expect(errors).toEqual([])
    })
  }
}
