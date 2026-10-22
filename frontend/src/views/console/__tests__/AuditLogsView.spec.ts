import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeAll, beforeEach, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { api } from '@/api/console'
import FilterSelect from '@/components/common/FilterSelect.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import AuditLogsView from '@/views/console/AuditLogsView.vue'

let wrapper: VueWrapper | null = null

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

beforeEach(() => {
  setActivePinia(createPinia())
  useAuthStore().persist({
    id: 1,
    username: 'root',
    display_name: '',
    email: '',
    role: 100,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    permissions: { admin_permissions: {} },
  })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.restoreAllMocks()
})

async function mountAuditLogs() {
  const get = vi.spyOn(api, 'get').mockImplementation((path: string) => {
    if (path === '/api/audit')
      return Promise.resolve({ page: 1, page_size: 10, total: 0, items: [] })
    return Promise.reject(new Error(`Unexpected API request: ${path}`))
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/usage-logs/audit', component: AuditLogsView }],
  })
  await router.push('/usage-logs/audit?scope=all')
  wrapper = mount(AuditLogsView, {
    global: {
      plugins: [i18n, router],
      stubs: {
        LogsNavTabs: true,
        ConsoleModal: {
          props: ['open'],
          template: '<div v-if="open"><slot/><slot name="footer"/></div>',
        },
      },
    },
  })
  await flushPromises()
  const lastParams = () =>
    get.mock.calls.at(-1)![1] as unknown as Record<string, unknown>
  return { wrapper, get, lastParams }
}

it('filters by the native exact request_id and never sends keyword', async () => {
  const { wrapper, lastParams } = await mountAuditLogs()
  expect(lastParams()).toHaveProperty('request_id', '')
  expect(lastParams()).not.toHaveProperty('keyword')

  const input = wrapper.get<HTMLInputElement>('[name="audit-log-request-id"]')
  expect(input.attributes('maxlength')).toBe('64')
  expect(input.attributes('placeholder')).toBe('Filter by exact request ID')
  await input.setValue('  req-45  ')
  await vi.waitFor(() =>
    expect(lastParams()).toHaveProperty('request_id', 'req-45')
  )
  expect(lastParams()).not.toHaveProperty('keyword')
})

it('exports with the same request_id filter as the list', async () => {
  const { wrapper, get, lastParams } = await mountAuditLogs()
  // jsdom implements neither object URLs nor download navigation.
  URL.createObjectURL = vi.fn(() => 'blob:audit')
  URL.revokeObjectURL = vi.fn()
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  await wrapper.get('[name="audit-log-request-id"]').setValue('req-45')
  await vi.waitFor(() =>
    expect(lastParams()).toHaveProperty('request_id', 'req-45')
  )
  const listCalls = get.mock.calls.length

  const button = (label: string) =>
    wrapper.findAll('button').find((node) => node.text() === label)!
  await button(i18n.global.t('common.export')).trigger('click')
  await button(i18n.global.t('common.confirm')).trigger('click')
  await flushPromises()

  expect(get.mock.calls.length).toBe(listCalls + 1)
  expect(lastParams()).toMatchObject({ request_id: 'req-45', p: 1 })
  expect(lastParams()).not.toHaveProperty('keyword')
  expect(URL.createObjectURL).toHaveBeenCalledOnce()
})

it('offers only the categories model.ValidAuditCategory accepts', async () => {
  const { wrapper } = await mountAuditLogs()
  const category = wrapper
    .findAllComponents(FilterSelect)
    .find((select) => select.props('label') === 'Event category')!
  expect(
    (category.props('options') as { value: string }[]).map(
      (option) => option.value
    )
  ).toEqual(['all', 'operation', 'security', 'login', 'access_token'])
})
