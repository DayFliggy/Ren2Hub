import type { Page } from '@playwright/test'
import type { ModelMetadata, ModelVendor } from '../../src/api/adminManagement'
import type { DrawingLogItem, RelayTaskLogItem } from '../../src/types/console'
import {
  channelStatesFixture,
  redemptionStatesFixture,
} from './channel-states-fixture'

const now = 1785124800
const vendor: ModelVendor = {
  id: 5,
  name: 'UI Matrix Vendor',
  description: 'Synthetic native vendor / 本地合成供应商',
  icon: '',
  status: 1,
  created_time: now - 86400,
  updated_time: now,
}
const model: ModelMetadata = {
  id: 5,
  model_name: 'ui-matrix-chat',
  description: 'Synthetic native model / 本地合成模型',
  icon: '',
  tags: 'chat,reasoning',
  vendor_id: vendor.id,
  endpoints: '',
  status: 1,
  sync_official: 1,
  name_rule: 0,
  bound_channels: [{ name: 'UI vLLM channel', type: 62 }],
  matched_models: ['ui-matrix-chat'],
  matched_count: 1,
  quota_types: [0],
  created_time: now - 86400,
  updated_time: now,
}
const task: RelayTaskLogItem = {
  id: 5,
  task_id: 'ui-matrix-task-001',
  platform: 'ui-fixture',
  action: 'generate',
  status: 'SUCCESS',
  progress: '100%',
  fail_reason: '',
  quota: 500000,
  submit_time: now - 60,
  finish_time: now,
  legacy_video_available: false,
  result_discarded: false,
}
const drawing: DrawingLogItem = {
  id: 5,
  mj_id: 'ui-matrix-drawing-001',
  action: 'IMAGINE',
  prompt: 'A quiet mountain / 安静的山峦',
  prompt_en: 'A quiet mountain',
  status: 'SUCCESS',
  progress: '100%',
  fail_reason: '',
  image_url: '',
  video_url: '',
  quota: 500000,
  submit_time: (now - 60) * 1000,
  finish_time: now * 1000,
}

export const populatedLists = [
  {
    path: '/channels',
    endpoint: '/api/channel/',
    rowText: 'UI vLLM channel',
  },
  {
    path: '/models/metadata',
    endpoint: '/api/models/search',
    rowText: model.model_name,
  },
  {
    path: '/models/vendors',
    endpoint: '/api/vendors/search',
    rowText: vendor.name,
  },
  {
    path: '/redemption-codes',
    endpoint: '/api/redemption/',
    // The optional name column is hidden by default; the code is visible.
    rowText: 'UIHISTOR********0001',
  },
  {
    path: '/usage-logs/task',
    endpoint: '/api/task/self',
    rowText: task.task_id,
  },
  {
    path: '/usage-logs/drawing',
    endpoint: '/api/mj/self',
    rowText: drawing.mj_id,
  },
] as const

// Current native page and timestamp contracts; no legacy /api/next fixtures.
// Results have no media URLs and never require a provider or production call.
export async function populatedListFixture(page: Page, path: string) {
  if (path === '/channels') {
    await channelStatesFixture(page)
    return
  }
  if (path === '/redemption-codes') {
    await redemptionStatesFixture(page)
    return
  }
  const rowsByEndpoint: Record<string, unknown[]> = path.startsWith('/models/')
    ? { '/api/models/search': [model], '/api/vendors/search': [vendor] }
    : path === '/usage-logs/task'
      ? { '/api/task/self': [task] }
      : { '/api/mj/self': [drawing] }
  await page.route('**/api/**', (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const rows = rowsByEndpoint[url.pathname]
    if (request.method() !== 'GET' || !rows) return route.fallback()
    return route.fulfill({
      json: {
        success: true,
        data: {
          page: Number(url.searchParams.get('p') || '1'),
          page_size: Number(url.searchParams.get('page_size') || '20'),
          total: rows.length,
          items: rows,
          ...(url.pathname === '/api/models/search'
            ? { vendor_counts: { [vendor.id]: 1 } }
            : {}),
        },
      },
    })
  })
}
