import { randomUUID } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow } from './fixtures'

test.describe('gateway real preview permissions and conversion', () => {
  const ollama = process.env.GATEWAY_PREVIEW_PROVIDER === 'ollama'
  const projection = ollama ? 'ollama_chat' : 'coze_text'
  test.skip(
    process.env.GATEWAY_LIVE_TEST !== '1',
    'requires the isolated local application'
  )
  let credentials: { username: string; password: string }
  let tokenId: number
  let profileId: number
  let channelId: number
  let rootHeaders: { Authorization: string }

  test.beforeAll(async ({ request, baseURL }) => {
    expect(new URL(baseURL!).hostname).toBe('127.0.0.1')
    const root = JSON.parse(
      readFileSync(
        resolve('../.local-tests/gateway/app/credentials.json'),
        'utf8'
      )
    )
    const login = await (
      await request.post('/api/user/login', { data: root })
    ).json()
    expect(login.success).toBe(true)
    rootHeaders = { Authorization: `Bearer ${login.data.access_token}` }
    const metrics = await (
      await request.get('/api/gateway/metrics', { headers: rootHeaders })
    ).json()
    expect(metrics.success).toBe(true)
    expect(metrics.data.counter_scope).toBe('instance')
    expect(metrics.data.recovery_scope).toBe('database')

    credentials = {
      username: `preview_${randomUUID().slice(0, 8)}`,
      password: randomUUID().slice(0, 16),
    }
    const created = await (
      await request.post('/api/user/', {
        headers: rootHeaders,
        data: { ...credentials, display_name: 'Preview test', role: 1 },
      })
    ).json()
    expect(created.success, created.message).toBe(true)
    const channelName = `preview_${randomUUID().slice(0, 8)}`
    const added = await (
      await request.post('/api/channel/', {
        headers: rootHeaders,
        data: {
          mode: 'single',
          channel: {
            name: channelName,
            type: ollama ? 4 : 49,
            key: 'local-preview-test-key',
            other: 'local-preview-bot',
            models: 'gpt-4o-mini',
            model_mapping: '{}',
            base_url: 'http://127.0.0.1:9',
            group: 'default',
            status: 1,
            capacity_total: 10,
            channel_ratio: 1,
          },
        },
      })
    ).json()
    expect(added.success, added.message).toBe(true)
    const userLogin = await (
      await request.post('/api/user/login', { data: credentials })
    ).json()
    expect(userLogin.success).toBe(true)
    const headers = { Authorization: `Bearer ${userLogin.data.access_token}` }
    const forbidden = await request.get('/api/gateway/metrics', { headers })
    expect(forbidden.status()).toBe(403)
    expect((await forbidden.json()).data).toBeUndefined()
    await expect
      .poll(
        async () => {
          const channels = await (
            await request.get('/api/routing/eligible-channels', { headers })
          ).json()
          expect(channels.success).toBe(true)
          const channel = channels.data.find(
            (item: { name: string }) => item.name === channelName
          )
          if (!channel) return 'channel_missing'
          channelId = channel.id
          return channel.filter_reason ?? ''
        },
        { timeout: 10000 }
      )
      .toBe('')
    expect(
      (
        await (
          await request.post('/api/token/', {
            headers,
            data: {
              name: 'Preview token',
              type: 'manual',
              expired_time: -1,
              unlimited_quota: true,
              conversion_policy: 'safe',
            },
          })
        ).json()
      ).success
    ).toBe(true)
    const profiles = await (
      await request.get('/api/routing/profiles', { headers })
    ).json()
    expect(profiles.success).toBe(true)
    expect(profiles.data).toHaveLength(1)
    const profile = profiles.data[0].profile
    tokenId = profile.token_id
    profileId = profile.id
    const saved = await (
      await request.put(`/api/routing/profiles/${profileId}`, {
        headers,
        data: {
          token_id: tokenId,
          mode: 'manual',
          version: profile.version,
          groups: [
            {
              name: 'Preview group',
              enabled: true,
              kind: 'manual',
              position: 0,
              entries: [
                {
                  channel_id: channelId,
                  source: 'platform',
                  enabled: true,
                  position: 0,
                  weight: 1,
                },
              ],
              policy: { retry_mode: 'none' },
            },
          ],
        },
      })
    ).json()
    expect(saved.success, saved.message).toBe(true)
    const foreign = await request.post(
      `/api/routing/profiles/${profileId}/preview`,
      {
        headers: rootHeaders,
        data: { model: 'gpt-4o-mini', path: '/v1/chat/completions' },
      }
    )
    expect(foreign.status()).toBe(404)
  })

  test.afterAll(async ({ request }) => {
    if (channelId) {
      expect(
        (
          await (
            await request.delete(`/api/channel/${channelId}`, {
              headers: rootHeaders,
            })
          ).json()
        ).success
      ).toBe(true)
    }
  })

  for (const theme of ['light', 'dark'] as const) {
    for (const viewport of [
      { name: 'desktop', width: 1440, height: 1000 },
      { name: 'mobile', width: 390, height: 844 },
    ]) {
      test(`${theme} ${viewport.name} normal user previews without sending`, async ({
        page,
      }) => {
        await page.setViewportSize(viewport)
        await page.addInitScript(
          (value) => localStorage.setItem('ren2hub_theme_mode', value),
          theme
        )
        const errors: string[] = []
        page.on('pageerror', (error) => errors.push(error.message))
        await page.goto('/sign-in')
        await page
          .locator('input[autocomplete="username"]')
          .fill(credentials.username)
        await page
          .locator('input[autocomplete="current-password"]')
          .fill(credentials.password)
        await page.locator('button[type="submit"]').click()
        await expect(page).not.toHaveURL(/sign-in/)
        await page.goto(`/keys/${tokenId}/routing`)
        await expect(
          page.getByText('安全：保留请求语义', { exact: true })
        ).toBeVisible()
        await page
          .getByPlaceholder('gpt-5', { exact: true })
          .fill('gpt-4o-mini')
        const sample = page.getByTestId('route-preview-request')
        await sample.fill(
          '{"messages":[{"role":"user","content":"hello"}],"parallel_tool_calls":false,"temperature":0}'
        )
        const rejected = page.waitForResponse((response) =>
          response.url().endsWith(`/tokens/${tokenId}/preview`)
        )
        await page
          .getByRole('button', { name: '运行 Preview', exact: true })
          .click()
        const result = await (await rejected).json()
        expect(result.success).toBe(true)
        expect(result.data.candidate_channel_ids).toEqual([])
        expect(result.data.entries[0].conversion_plan.request_projection).toBe(
          projection
        )
        expect(JSON.stringify(result)).not.toContain('local-preview-test-key')
        expect(JSON.stringify(result)).not.toContain('127.0.0.1:9')
        await expect(
          page.getByTestId('route-preview-conversion')
        ).toContainText(projection)
        await expect(
          page.getByTestId('route-preview-diagnostics')
        ).toContainText(ollama ? 'serial_tools' : 'parallel_tool_calls')
        await assertNoHorizontalOverflow(page)
        await page
          .getByTestId('route-preview-conversion')
          .scrollIntoViewIfNeeded()
        await page.screenshot({
          path: `output/gateway-preview-${projection}-live-${theme}-${viewport.name}.png`,
          fullPage: true,
        })
        await sample.fill('{"messages":[{"role":"user","content":"hello"}]}')
        const accepted = page.waitForResponse((response) =>
          response.url().endsWith(`/tokens/${tokenId}/preview`)
        )
        await page
          .getByRole('button', { name: '运行 Preview', exact: true })
          .click()
        const success = await (await accepted).json()
        expect(success.data.candidate_channel_ids).toEqual([channelId])
        expect(success.data.live_selection).toBe(false)
        await expect(page.getByTestId('route-preview-diagnostics')).toHaveCount(
          0
        )
        expect(errors).toEqual([])
      })
    }
  }
})
