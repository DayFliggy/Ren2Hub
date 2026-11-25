import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { api } from '@/api/console'
import FilterSelect from '@/components/common/FilterSelect.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { mergeServerMessages } from '@/i18n/serverMessages'
import { useAuthStore } from '@/stores/auth'
import QuotaLogsView from '@/views/console/QuotaLogsView.vue'

let wrapper: VueWrapper | null = null

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})

beforeEach(() => {
  setActivePinia(createPinia())
  mergeServerMessages('zh-CN', {
    'Daily check-in, received {{quota}}': '每日签到，获得 {{quota}}',
  })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.restoreAllMocks()
})

function setRole(role: number) {
  useAuthStore().persist({
    id: role === 1 ? 7 : 1,
    username: role === 1 ? 'member' : 'admin',
    display_name: '',
    email: '',
    role,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    permissions: { admin_permissions: {} },
  })
}

async function mountQuotaLogs(
  role = 1,
  initialPath = '/usage-logs/quota',
  items: Record<string, unknown>[] = []
) {
  vi.restoreAllMocks()
  setRole(role)
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    page: 1,
    page_size: 10,
    total: items.length,
    items,
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/usage-logs/quota',
        name: 'logs-quota',
        component: QuotaLogsView,
      },
      {
        path: '/usage-logs/task',
        name: 'logs-tasks',
        component: { template: '<div />' },
      },
      {
        path: '/usage-logs/drawing',
        name: 'logs-drawing',
        component: { template: '<div />' },
      },
    ],
  })
  await router.push(initialPath)
  await router.isReady()
  wrapper = mount(QuotaLogsView, {
    attachTo: document.body,
    global: {
      plugins: [i18n, router],
      stubs: { LogsNavTabs: true },
    },
  })
  await flushPromises()
  return { wrapper, get }
}

describe('QuotaLogsView', () => {
  it('requests types=1,4,6 by default and renders semantic amounts and task links', async () => {
    const { wrapper, get } = await mountQuotaLogs(1, '/usage-logs/quota', [
      {
        id: 101,
        type: 1,
        token_name: '',
        model_name: '',
        channel_name: '',
        prompt_tokens: 0,
        completion_tokens: 0,
        quota: 0,
        use_time: 0,
        is_stream: false,
        content: 'Decreased user quota by 1000000',
        other: JSON.stringify({
          op: { action: 'user.quota_subtract', params: { quota: 1000000 } },
        }),
        created_at: 1_750_000_200,
      },
      {
        id: 102,
        type: 4,
        token_name: '',
        model_name: '',
        channel_name: '',
        prompt_tokens: 0,
        completion_tokens: 0,
        quota: 0,
        use_time: 0,
        is_stream: false,
        content: 'Daily check-in, received $0.50',
        other: JSON.stringify({
          content_parts: [
            {
              key: 'Daily check-in, received {{quota}}',
              params: { quota: '$0.50' },
            },
          ],
        }),
        created_at: 1_750_000_100,
      },
      {
        id: 103,
        type: 6,
        token_name: 'k',
        model_name: 'sora-2',
        channel_name: '',
        prompt_tokens: 0,
        completion_tokens: 0,
        quota: 500000,
        use_time: 0,
        is_stream: false,
        content: 'Task failed refund',
        other: JSON.stringify({ task_id: 'task_refund_9' }),
        created_at: 1_750_000_000,
      },
    ])

    expect(get.mock.calls[0]?.[0]).toBe('/api/log/self')
    expect(get.mock.calls[0]?.[1]).toMatchObject({
      type: 0,
      types: '1,4,6',
    })

    const text = wrapper.text()
    expect(text).toContain('管理员扣减')
    expect(text).toContain('管理员扣减额度 $2.00')
    expect(text).toContain('-$2.00')
    expect(text).toContain('每日签到')
    expect(text).toContain('+$0.50')
    expect(text).toContain('每日签到，获得 $0.50')
    expect(text).toContain('任务退款')
    expect(text).toContain('+$1.00')
    expect(text).toContain('task_refund_9')
    expect(
      wrapper.find('a[href="/usage-logs/task?task_id=task_refund_9"]').exists()
    ).toBe(true)

    // Narrow to system rewards -> sends type=4 without types
    const typeSelect = wrapper
      .findAllComponents(FilterSelect)
      .find((s) => s.props('label') === '变动类别')!
    typeSelect.vm.$emit('update:modelValue', 'system')
    await flushPromises()
    expect(get.mock.calls.at(-1)?.[1]).toMatchObject({ type: 4 })
    expect(get.mock.calls.at(-1)?.[1]).not.toHaveProperty('types')
  })

  it('restricts all-accounts scope to admins and queries /api/log/ with username when selected', async () => {
    const member = await mountQuotaLogs(1, '/usage-logs/quota?scope=all')
    expect(member.get.mock.calls[0]?.[0]).toBe('/api/log/self')
    expect(
      member.wrapper.find('input[name="quota-log-username"]').exists()
    ).toBe(false)
    member.wrapper.unmount()

    const admin = await mountQuotaLogs(10, '/usage-logs/quota?scope=all')
    expect(admin.get.mock.calls[0]?.[0]).toBe('/api/log/')
    expect(
      admin.wrapper.find('input[name="quota-log-username"]').exists()
    ).toBe(true)
  })
})
