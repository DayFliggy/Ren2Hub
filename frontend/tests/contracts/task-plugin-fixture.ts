import { createHash } from 'node:crypto'
import type { Page } from '@playwright/test'
import type {
  TaskPluginDetail,
  TaskPluginListItem,
  TaskPluginMeta,
  TaskPluginRecord,
} from '../../src/features/task-plugins/types'

export const PLUGIN_KEY = 'ui-regression-task'
export const MARKET_ORIGIN = 'https://task-plugin-ui.invalid'
export const meta: TaskPluginMeta = {
  apiVersion: 1,
  key: PLUGIN_KEY,
  name: 'UI task fixture',
  version: '2.0.0',
  description: {
    en: 'Synthetic UI plugin with no executable provider.',
    'zh-CN': '只用于界面回归的合成插件，不执行供应商请求。',
  },
  models: ['ui-video', 'ui-image'],
  channelTypes: [1],
  baseUrl: 'https://upstream.ui.invalid',
  allowedHosts: ['upstream.ui.invalid'],
  website: 'https://docs.ui.invalid/task-plugin',
  fetchMode: 'polling',
  protocols: [
    { name: 'openai_video', models: ['ui-video'], supports: ['background'] },
  ],
  routes: [
    {
      method: 'POST',
      path: '/ui-fixture/tasks',
      type: 'submit',
      models: ['ui-image'],
    },
  ],
  usageSchema: {
    duration: {
      type: 'number',
      unit: 'second',
      description: { en: 'Video duration', 'zh-CN': '视频时长' },
    },
  },
  usageProfiles: [
    {
      models: ['ui-image'],
      schema: { images: { type: 'number', unit: 'count' } },
    },
  ],
}

export function pluginSource(version: string) {
  // The literal parser may inspect metadata. Any attempted execution must fail.
  return `export const meta = ${JSON.stringify({ ...meta, version })};\nthrow new Error("UI fixture must never execute");\n`
}

const hash = (source: string) =>
  createHash('sha256').update(source).digest('hex')
const source = pluginSource('2.0.0')
const listItem: TaskPluginListItem = {
  meta,
  source: 'override',
  enabled: true,
  active: true,
  source_hash: hash(source),
  remark: 'UI regression fixture',
  runtime_status: 'registered',
  channel_count: 1,
  in_flight_count: 0,
}
function versionRecord(version: string, id: number): TaskPluginRecord {
  const text = pluginSource(version)
  return {
    id,
    key: PLUGIN_KEY,
    api_version: 1,
    version,
    source: text,
    source_hash: hash(text),
    enabled: true,
    active: version === '2.0.0',
    factory: false,
    created_at: 1785052800,
    remark: version === '2.0.0' ? 'Current UI fixture' : 'Previous UI fixture',
  }
}
function detail(version = '2.0.0'): TaskPluginDetail {
  return {
    meta: { ...meta, version },
    source: pluginSource(version),
    layer: 'override',
    plugin: versionRecord(version, 1),
  }
}

export async function taskPluginFixture(page: Page) {
  const unexpected: string[] = []
  const writes: Array<{ path: string; body: unknown }> = []
  const fetched: string[] = []
  await page.route('**/api/option/', (route) =>
    route.request().method() === 'GET'
      ? route.fulfill({
          json: {
            success: true,
            data: [{ key: 'TaskPluginEnabled', value: 'true' }],
          },
        })
      : route.fallback()
  )
  await page.route('**/api/plugin/task{,/**}', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    const reply = (data: unknown) =>
      route.fulfill({ json: { success: true, data } })
    if (request.method() === 'GET') {
      if (path === '/api/plugin/task') return reply([listItem])
      if (path === '/api/plugin/task/marketplace/sources')
        return reply([
          {
            name: 'UI fixture registry',
            index_url: `${MARKET_ORIGIN}/index.json`,
          },
        ])
      if (path === `/api/plugin/task/${PLUGIN_KEY}/versions`)
        return reply([versionRecord('2.0.0', 2), versionRecord('1.0.0', 1)])
      if (path === `/api/plugin/task/${PLUGIN_KEY}`)
        return reply(detail(url.searchParams.get('version') ?? '2.0.0'))
    }
    if (
      request.method() === 'POST' &&
      path === `/api/plugin/task/${PLUGIN_KEY}/dryrun`
    ) {
      writes.push({ path, body: request.postDataJSON() })
      return reply({
        simulated: true,
        no_plugin_executed: true,
        provider_requests: 0,
      })
    }
    unexpected.push(`${request.method()} ${path}`)
    return route.fulfill({
      status: 400,
      json: { success: false, message: 'Unexpected fixture operation' },
    })
  })
  // Fulfil the exact synthetic HTTPS origin in memory. No external source is downloaded.
  await page.route(`${MARKET_ORIGIN}/**`, async (route) => {
    const path = new URL(route.request().url()).pathname
    fetched.push(path)
    const headers = { 'access-control-allow-origin': '*' }
    if (path === '/index.json')
      return route.fulfill({
        headers,
        json: {
          indexVersion: 1,
          name: 'UI fixture registry',
          plugins: [
            {
              ...meta,
              latest: '2.1.0',
              versions: ['2.1.0', '2.0.0', '1.0.0'].map((version) => ({
                version,
                path: `${PLUGIN_KEY}/${version}/plugin.js`,
                sha256: hash(pluginSource(version)),
                kind: 'task',
                minApiVersion: 1,
              })),
            },
          ],
        },
      })
    const match = path.match(
      new RegExp(`^/${PLUGIN_KEY}/(2\\.1\\.0|2\\.0\\.0|1\\.0\\.0)/plugin\\.js$`)
    )
    if (match)
      return route.fulfill({
        headers,
        contentType: 'text/javascript',
        body: pluginSource(match[1]!),
      })
    if (path === `/${PLUGIN_KEY}/2.1.0/CHANGELOG.md`)
      return route.fulfill({
        headers,
        contentType: 'text/markdown',
        body: `---\nchangelogVersion: 1\nplugin: ${PLUGIN_KEY}\nversion: 2.1.0\nlocale: en\n---\n# Changelog\n## [2.1.0]\n### Changed\n- Synthetic interface review with **no execution**.\n`,
      })
    unexpected.push(`GET ${path}`)
    return route.fulfill({
      headers,
      status: 404,
      body: 'Unmapped fixture resource',
    })
  })
  return { unexpected, writes, fetched }
}
