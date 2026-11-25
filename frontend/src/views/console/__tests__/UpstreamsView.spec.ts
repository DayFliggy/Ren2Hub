import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeAll, beforeEach, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { api } from '@/api/client'
import { ApiError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import UpstreamsView from '@/views/console/UpstreamsView.vue'
import fixture from '../../../../tests/fixtures/native/upstreams.get.json'

type Row = (typeof fixture.list.data)[number]
let wrapper: VueWrapper | null = null
let rows: Row[] = []
let desktop = true

const WorkspaceStub = {
  name: 'UpstreamWorkspace',
  props: {
    account: { type: Object, required: true },
    tab: { type: String, required: true },
  },
  emits: ['update:tab', 'delete', 'changed', 'back'],
  template: '<div data-workspace-stub>{{ account.id }}:{{ tab }}</div>',
}
const ConfirmStub = {
  name: 'ConfirmDialog',
  props: { open: Boolean, message: { type: String, default: '' } },
  emits: ['confirm', 'cancel'],
  template:
    '<div v-if="open" data-confirm>{{ message }}<button data-confirm-ok @click="$emit(\'confirm\')" /></div>',
}

function site(id: number, patch: Partial<Row> = {}): Row {
  return { ...structuredClone(fixture.list.data[0]!), id, ...patch }
}

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

function signIn(role: number, upstream: Record<string, boolean> = {}) {
  useAuthStore().persist({
    id: 1,
    username: 'operator',
    display_name: '',
    email: '',
    role,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    permissions: { admin_permissions: { upstream } },
  })
}

beforeEach(() => {
  setActivePinia(createPinia())
  useToast().toasts.splice(0)
  rows = [structuredClone(fixture.list.data[0]!)]
  desktop = true
  window.matchMedia = ((query: string) => ({
    matches: desktop,
    media: query,
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.restoreAllMocks()
})

async function mountView(path = '/upstreams') {
  const get = vi.spyOn(api, 'get').mockImplementation((url: string) => {
    if (url === '/api/admin/upstreams')
      return Promise.resolve(structuredClone(rows))
    return Promise.reject(new Error(`Unexpected API request: ${url}`))
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/upstreams', name: 'upstreams', component: UpstreamsView },
    ],
  })
  await router.push(path)
  wrapper = mount(UpstreamsView, {
    global: {
      plugins: [i18n, router],
      stubs: {
        UpstreamWorkspace: WorkspaceStub,
        UpstreamAccountModal: true,
        ConfirmDialog: ConfirmStub,
      },
    },
  })
  await flushPromises()
  const listCalls = () =>
    get.mock.calls.filter(([url]) => url === '/api/admin/upstreams').length
  return { wrapper, router, listCalls }
}

const query = (router: ReturnType<typeof createRouter>) =>
  router.currentRoute.value.query

function siteHrefs(view: VueWrapper) {
  return view
    .findAll('[data-upstream-site]')
    .map((link) => link.attributes('href'))
}

it('lists the site with its raw NewAPI balance and lets root add accounts', async () => {
  signIn(100)
  const { wrapper } = await mountView()
  const item = wrapper.get('[data-upstream-site]')
  expect(item.text()).toContain('演示账号')
  expect(item.text()).toContain(
    i18n.global.t('upstreams.balance.newapi', { value: '300' })
  )
  expect(item.text()).toContain('example.invalid')
  expect(wrapper.get('[data-upstream-add]').attributes('disabled')).toBe(
    undefined
  )
})

it('keeps the add button disabled for a read-only admin', async () => {
  signIn(10, { read: true })
  const { wrapper } = await mountView()
  expect(wrapper.get('[data-upstream-add]').attributes()).toHaveProperty(
    'disabled'
  )
})

it('selects the first visible site on desktop only', async () => {
  signIn(100)
  rows = [site(4), site(7)]
  const { wrapper, router } = await mountView()
  expect(query(router).account).toBe('4')
  expect(wrapper.get('[data-workspace-stub]').text()).toBe('4:keys')
  expect(wrapper.get('[aria-current="page"]').attributes('href')).toContain(
    'account=4'
  )
  wrapper.unmount()

  desktop = false
  const narrow = await mountView()
  expect(query(narrow.router)).not.toHaveProperty('account')
  expect(narrow.wrapper.find('[data-workspace-stub]').exists()).toBe(false)
})

it('drops a malformed ?account', async () => {
  signIn(100)
  const { router } = await mountView('/upstreams?account=abc&tab=groups')
  expect(query(router)).toEqual({ account: '4', tab: 'groups' })
})

it('reloads once for an unknown ?account and then reports it missing', async () => {
  signIn(100)
  const { wrapper, router, listCalls } = await mountView(
    '/upstreams?account=99'
  )
  expect(listCalls()).toBe(2)
  expect(query(router).account).toBe('99')
  expect(wrapper.find('[data-workspace-stub]').exists()).toBe(false)
  expect(wrapper.get('[data-upstream-not-found]').text()).toContain(
    i18n.global.t('upstreams.notFound')
  )
  await router.replace('/upstreams?account=99&tab=groups')
  await flushPromises()
  expect(listCalls()).toBe(2)
})

it('keeps the selected site when filters hide it', async () => {
  signIn(100)
  rows = [
    site(4),
    site(7, {
      name: 'Sub 站',
      platform: 'sub2api',
      status: 'reauth_required',
      last_error: 'upstream_unreachable',
    }),
  ]
  const { wrapper, router } = await mountView('/upstreams?account=4')
  const toggle = wrapper.get('[data-upstream-abnormal]')
  expect(toggle.text()).toBe(
    i18n.global.t('upstreams.abnormalOnly', { count: 1 })
  )
  await toggle.trigger('click')
  expect(toggle.attributes('aria-pressed')).toBe('true')
  expect(wrapper.findAll('[data-upstream-site]')).toHaveLength(1)
  expect(wrapper.get('[data-upstream-site]').text()).toContain('Sub 站')
  expect(wrapper.get('[data-upstream-summary]').text()).toBe(
    i18n.global.t('upstreams.siteFiltered', { visible: 1, total: 2 })
  )
  expect(query(router).account).toBe('4')
  expect(wrapper.get('[data-workspace-stub]').text()).toBe('4:keys')

  await wrapper.get('[data-upstream-selected-hidden]').trigger('click')
  expect(wrapper.findAll('[data-upstream-site]')).toHaveLength(2)
})

it('hides the attention filter when every site is healthy', async () => {
  signIn(100)
  const { wrapper } = await mountView()
  expect(wrapper.find('[data-upstream-abnormal]').exists()).toBe(false)
})

it('carries only a valid tab on site links', async () => {
  signIn(100)
  rows = [site(4), site(7)]
  const bogus = await mountView('/upstreams?account=4&tab=bogus')
  expect(siteHrefs(bogus.wrapper)).toEqual([
    '/upstreams?account=4',
    '/upstreams?account=7',
  ])
  bogus.wrapper.unmount()

  const groups = await mountView('/upstreams?account=4&tab=groups')
  expect(siteHrefs(groups.wrapper)).toEqual([
    '/upstreams?account=4&tab=groups',
    '/upstreams?account=7&tab=groups',
  ])
})

it('keeps the tab when switching sites', async () => {
  signIn(100)
  rows = [site(4), site(7)]
  const { wrapper, router } = await mountView('/upstreams?account=4')
  wrapper.getComponent(WorkspaceStub).vm.$emit('update:tab', 'bindings')
  await flushPromises()
  expect(query(router)).toEqual({ account: '4', tab: 'bindings' })

  await wrapper.findAll('[data-upstream-site]')[1]!.trigger('click')
  await flushPromises()
  expect(query(router)).toEqual({ account: '7', tab: 'bindings' })
  expect(wrapper.get('[data-workspace-stub]').text()).toBe('7:bindings')
})

it.each([
  ['next site', 4, '7'],
  ['previous site when it was last', 7, '4'],
])('moves to the %s after deleting the selected one', async (_, id, next) => {
  signIn(100)
  rows = [site(4), site(7)]
  const { wrapper, router } = await mountView(`/upstreams?account=${id}`)
  vi.spyOn(api, 'delete').mockImplementation(() => {
    rows = rows.filter((row) => row.id !== id)
    return Promise.resolve(null)
  })
  wrapper.getComponent(WorkspaceStub).vm.$emit(
    'delete',
    rows.find((row) => row.id === id)
  )
  await flushPromises()
  await wrapper.get('[data-confirm-ok]').trigger('click')
  await flushPromises()
  expect(query(router).account).toBe(next)
})

it('reports a translated business code when deletion is refused', async () => {
  signIn(100)
  const { wrapper } = await mountView()
  const remove = vi.spyOn(api, 'delete').mockRejectedValue(
    new ApiError('has bindings', {
      status: 409,
      code: 'remote_key_already_bound',
    })
  )
  wrapper.getComponent(WorkspaceStub).vm.$emit('delete', rows[0])
  await flushPromises()
  expect(wrapper.get('[data-confirm]').text()).toContain('演示账号')
  await wrapper.get('[data-confirm-ok]').trigger('click')
  await flushPromises()
  expect(remove).toHaveBeenCalledWith(
    '/api/admin/upstreams/4',
    undefined,
    expect.anything()
  )
  expect(useToast().toasts.map((toast) => toast.message)).toContain(
    i18n.global.t('upstreams.errors.remote_key_already_bound')
  )
})

it('shows the rate-limit wait when the list cannot load', async () => {
  signIn(100)
  vi.spyOn(api, 'get').mockRejectedValue(
    new ApiError('slow', {
      status: 429,
      code: 'upstream_rate_limited',
      cause: { response: { headers: { 'retry-after': '12' } } },
    })
  )
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/upstreams', name: 'upstreams', component: UpstreamsView },
    ],
  })
  await router.push('/upstreams')
  wrapper = mount(UpstreamsView, {
    global: {
      plugins: [i18n, router],
      stubs: { UpstreamWorkspace: WorkspaceStub, UpstreamAccountModal: true },
    },
  })
  await flushPromises()
  const alert = wrapper.get('[role="alert"]').text()
  expect(alert).toContain(
    i18n.global.t('upstreams.errors.upstream_rate_limited')
  )
  expect(alert).toContain(
    i18n.global.t('upstreams.retryAfter', { seconds: 12 })
  )
  expect(query(router)).toEqual({})
})
