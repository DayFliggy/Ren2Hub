import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeAll, beforeEach, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { api } from '@/api/client'
import { ApiError } from '@/api/types'
import ActionMenu, {
  type ActionMenuItem,
} from '@/components/common/ActionMenu.vue'
import { useToast } from '@/composables/useToast'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import UpstreamsView from '@/views/console/UpstreamsView.vue'
import fixture from '../../../../tests/fixtures/native/upstreams.get.json'

let wrapper: VueWrapper | null = null

const DrawerStub = {
  name: 'UpstreamDetailDrawer',
  props: { account: { type: Object, default: null } },
  template: '<div data-drawer-stub>{{ account?.id ?? "" }}</div>',
}
const ConfirmStub = {
  name: 'ConfirmDialog',
  props: { open: Boolean, message: { type: String, default: '' } },
  emits: ['confirm', 'cancel'],
  template:
    '<div v-if="open" data-confirm>{{ message }}<button data-confirm-ok @click="$emit(\'confirm\')" /></div>',
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
  const toast = useToast()
  toast.toasts.splice(0)
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.restoreAllMocks()
})

async function mountView(path = '/upstreams') {
  const get = vi.spyOn(api, 'get').mockImplementation((url: string) => {
    if (url === '/api/admin/upstreams')
      return Promise.resolve(structuredClone(fixture.list.data))
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
        UpstreamDetailDrawer: DrawerStub,
        UpstreamAccountModal: true,
        ConfirmDialog: ConfirmStub,
      },
    },
  })
  await flushPromises()
  return { wrapper, get, router }
}

function menuItems(view: VueWrapper): Record<string, boolean | undefined> {
  const items = view
    .findAllComponents(ActionMenu)[0]!
    .props('items') as ActionMenuItem[]
  return Object.fromEntries(items.map((item) => [item.key, item.disabled]))
}

it('shows the account with its raw NewAPI balance and full actions for root', async () => {
  signIn(100)
  const { wrapper } = await mountView()
  expect(wrapper.get('[data-upstream-open]').text()).toContain('演示账号')
  expect(wrapper.get('[data-upstream-balance]').text()).toBe(
    i18n.global.t('upstreams.balance.newapi', { value: '300' })
  )
  expect(wrapper.get('[data-upstream-add]').attributes('disabled')).toBe(
    undefined
  )
  expect(menuItems(wrapper)).toMatchObject({
    details: undefined,
    sync: false,
    edit: false,
    reauth: false,
    delete: false,
  })
})

it('keeps a read-only admin on details only', async () => {
  signIn(10, { read: true })
  const { wrapper } = await mountView()
  expect(wrapper.get('[data-upstream-add]').attributes()).toHaveProperty(
    'disabled'
  )
  expect(menuItems(wrapper)).toMatchObject({
    sync: true,
    edit: true,
    reauth: true,
    endpoint: true,
    delete: true,
  })
  expect(menuItems(wrapper).details).toBeFalsy()
})

it('opens the linked account from ?account and clears it on close', async () => {
  signIn(100)
  const { wrapper, router } = await mountView('/upstreams?account=4')
  const drawer = wrapper.getComponent(DrawerStub)
  expect(drawer.props('account')).toMatchObject({ id: 4 })
  drawer.vm.$emit('close')
  await flushPromises()
  expect(router.currentRoute.value.query).not.toHaveProperty('account')
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
  wrapper.findAllComponents(ActionMenu)[0]!.vm.$emit('select', 'delete')
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
    routes: [{ path: '/upstreams', component: UpstreamsView }],
  })
  await router.push('/upstreams')
  wrapper = mount(UpstreamsView, {
    global: {
      plugins: [i18n, router],
      stubs: { UpstreamDetailDrawer: DrawerStub, UpstreamAccountModal: true },
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
})
