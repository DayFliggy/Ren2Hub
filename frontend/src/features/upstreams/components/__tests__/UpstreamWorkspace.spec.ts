import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeAll, beforeEach, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { api } from '@/api/client'
import type { UpstreamAccount } from '@/api/upstreams'
import ActionMenu, {
  type ActionMenuItem,
} from '@/components/common/ActionMenu.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import UpstreamWorkspace from '../UpstreamWorkspace.vue'
import fixture from '../../../../../tests/fixtures/native/upstreams.get.json'

type Tab = 'keys' | 'bindings' | 'groups'
interface Call {
  url: string
  signal?: AbortSignal
  resolve: (value: unknown) => void
}

const base = fixture.list.data[0] as unknown as UpstreamAccount
const keyRows = fixture.newapi_keys.data
const bindingRows = fixture.bindings.data
let wrapper: VueWrapper | null = null
let calls: Call[] = []
/** URLs answered at once; others wait for respond(). */
let auto = true

const AccountModalStub = {
  name: 'UpstreamAccountModal',
  props: { mode: { type: String, default: '' } },
  emits: ['close', 'done'],
  template: '<div data-account-modal>{{ mode }}</div>',
}
const KeyFormStub = {
  name: 'UpstreamKeyFormModal',
  template: '<div data-key-form />',
}

function account(patch: Partial<UpstreamAccount> = {}): UpstreamAccount {
  return { ...structuredClone(base), ...patch }
}

function answer(url: string) {
  if (url.endsWith('/keys')) return structuredClone(keyRows)
  if (url.endsWith('/bindings'))
    return structuredClone(bindingRows).map((row) => ({
      ...row,
      account_id: Number(url.split('/')[4]),
    }))
  throw new Error(`Unexpected API request: ${url}`)
}

function count(suffix: string) {
  return calls.filter((call) => call.url.endsWith(suffix)).length
}

function respond(url: string) {
  const call = calls.find((item) => item.url === url)
  if (!call) throw new Error(`No pending request: ${url}`)
  call.resolve(answer(url))
}

function signIn(permissions: Record<string, Record<string, boolean>> | null) {
  useAuthStore().persist({
    id: 1,
    username: 'operator',
    display_name: '',
    email: '',
    role: permissions ? 10 : 100,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    permissions: { admin_permissions: permissions ?? {} },
  })
}

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})

beforeEach(() => {
  setActivePinia(createPinia())
  calls = []
  auto = true
  vi.spyOn(api, 'get').mockImplementation(
    (url: string, _params?: unknown, config?: { signal?: AbortSignal }) =>
      new Promise((resolve) => {
        calls.push({ url, signal: config?.signal, resolve })
        if (auto) resolve(answer(url))
      }) as never
  )
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.restoreAllMocks()
})

async function mountWorkspace(props: { account: UpstreamAccount; tab: Tab }) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      {
        path: '/channels',
        name: 'channels',
        component: { template: '<div />' },
      },
    ],
  })
  wrapper = mount(UpstreamWorkspace, {
    props,
    global: {
      plugins: [i18n, router],
      stubs: {
        UpstreamAccountModal: AccountModalStub,
        UpstreamKeyFormModal: KeyFormStub,
        UpstreamCreateChannelModal: true,
        UpstreamBindChannelModal: true,
      },
    },
  })
  await flushPromises()
  return wrapper
}

function accountMenu(view: VueWrapper) {
  return view
    .findAllComponents(ActionMenu)
    .find((menu) => menu.props('label') === `${base.name} 的操作`)!
}

function menuState(view: VueWrapper) {
  const items = accountMenu(view).props('items') as ActionMenuItem[]
  return Object.fromEntries(items.map((item) => [item.key, !!item.disabled]))
}

const keysUrl = (id: number) => `/api/admin/upstreams/${id}/keys`

it('drops a slow key list from the previous site and shows loading at once', async () => {
  signIn(null)
  auto = false
  const view = await mountWorkspace({ account: account(), tab: 'keys' })
  const first = calls.find((call) => call.url === keysUrl(4))!

  await view.setProps({ account: account({ id: 5, name: 'B 站' }) })
  expect(view.find('[data-upstream-keys-loading]').exists()).toBe(true)
  expect(view.text()).not.toContain(i18n.global.t('upstreams.keys.empty'))
  expect(first.signal?.aborted).toBe(true)

  respond(keysUrl(4))
  await flushPromises()
  expect(view.find('[data-upstream-keys]').exists()).toBe(false)
  respond(keysUrl(5))
  await flushPromises()
  expect(view.get('[data-upstream-keys]').text()).toContain(keyRows[0]!.name)
})

it('closes an open key dialog when the site changes', async () => {
  signIn(null)
  const view = await mountWorkspace({ account: account(), tab: 'keys' })
  view
    .findAllComponents(ActionMenu)
    .find((menu) => menu.props('label') !== `${base.name} 的操作`)!
    .vm.$emit('select', 'edit')
  await flushPromises()
  expect(view.find('[data-key-form]').exists()).toBe(true)

  await view.setProps({ account: account({ id: 5 }) })
  await flushPromises()
  expect(view.find('[data-key-form]').exists()).toBe(false)
})

it('does not reload the new site when a sync started on the old one finishes', async () => {
  signIn(null)
  let finishSync: (value: unknown) => void = () => {}
  vi.spyOn(api, 'post').mockImplementation(
    () => new Promise((resolve) => (finishSync = resolve)) as never
  )
  const view = await mountWorkspace({ account: account(), tab: 'keys' })
  await view.get('[data-upstream-sync]').trigger('click')
  await view.setProps({ account: account({ id: 5 }) })
  await flushPromises()
  expect(count('/5/keys')).toBe(1)

  finishSync(null)
  await flushPromises()
  expect(count('/5/keys')).toBe(1)
  expect(count('/5/bindings')).toBe(1)
  expect(view.emitted('changed')).toHaveLength(1)
})

it('skips remote keys for inactive sites and explains why', async () => {
  signIn(null)
  for (const status of ['reauth_required', 'disabled']) {
    calls = []
    const view = await mountWorkspace({
      account: account({ status }),
      tab: 'keys',
    })
    expect(count('/keys')).toBe(0)
    expect(count('/bindings')).toBe(1)
    expect(view.find('[data-upstream-keys-inactive]').exists()).toBe(true)
    expect(view.find('[role="alert"]').exists()).toBe(false)
    view.unmount()
    wrapper = null
  }
})

it('loads keys when the same site recovers and drops them when it stops', async () => {
  signIn(null)
  const view = await mountWorkspace({
    account: account({ status: 'reauth_required' }),
    tab: 'keys',
  })
  expect(count('/keys')).toBe(0)

  await view.setProps({ account: account() })
  await flushPromises()
  expect(count('/keys')).toBe(1)
  expect(view.get('[data-upstream-keys]').text()).toContain(keyRows[0]!.name)

  await view.setProps({ account: account({ status: 'reauth_required' }) })
  await flushPromises()
  expect(view.find('[data-upstream-keys]').exists()).toBe(false)
  expect(view.find('[data-upstream-keys-inactive]').exists()).toBe(true)
})

it('aborts an in-flight key list when the site becomes inactive', async () => {
  signIn(null)
  auto = false
  const view = await mountWorkspace({ account: account(), tab: 'keys' })
  const pending = calls.find((call) => call.url === keysUrl(4))!
  await view.setProps({ account: account({ status: 'reauth_required' }) })
  expect(pending.signal?.aborted).toBe(true)
  respond(keysUrl(4))
  await flushPromises()
  expect(view.find('[data-upstream-keys]').exists()).toBe(false)
})

it('makes no remote key request on the groups tab', async () => {
  signIn(null)
  const view = await mountWorkspace({ account: account(), tab: 'groups' })
  await view.setProps({ account: account({ id: 5 }) })
  await view.setProps({ account: account({ id: 6 }) })
  await flushPromises()
  expect(count('/keys')).toBe(0)

  await view.setProps({ tab: 'keys' })
  await flushPromises()
  expect(count('/keys')).toBe(1)
  await view.setProps({ tab: 'bindings' })
  await view.setProps({ tab: 'keys' })
  await flushPromises()
  expect(count('/keys')).toBe(1)
})

it('reloads keys and bindings after re-authorizing an active site', async () => {
  signIn(null)
  const view = await mountWorkspace({ account: account(), tab: 'keys' })
  accountMenu(view).vm.$emit('select', 'reauth')
  await flushPromises()
  expect(view.get('[data-account-modal]').text()).toBe('reauth')

  view.getComponent(AccountModalStub).vm.$emit('done')
  await flushPromises()
  expect(count('/keys')).toBe(2)
  expect(count('/bindings')).toBe(2)
  expect(view.emitted('changed')).toHaveLength(1)
  expect(view.find('[data-account-modal]').exists()).toBe(false)
})

it('discards an unsaved ratio draft on site change without asking', async () => {
  signIn(null)
  const view = await mountWorkspace({ account: account(), tab: 'bindings' })
  const name = `upstream-ratio-factor-${bindingRows[0]!.channel_id}`
  await view.get(`input[name="${name}"]`).setValue('2')

  await view.setProps({ account: account({ id: 5 }) })
  await flushPromises()
  await view.setProps({ account: account() })
  await flushPromises()
  expect(
    (view.get(`input[name="${name}"]`).element as HTMLInputElement).value
  ).toBe(bindingRows[0]!.ratio_sync_factor)
  for (const dialog of view.findAllComponents(ConfirmDialog))
    expect(dialog.props('open')).toBe(false)
})

it.each([
  [
    'read',
    { upstream: { read: true } },
    { sync: true, create: true, settings: true, reauth: true, delete: true },
  ],
  [
    'operate',
    { upstream: { read: true, operate: true } },
    { sync: false, create: true, settings: true, reauth: true, delete: true },
  ],
  [
    'write',
    { upstream: { read: true, write: true } },
    { sync: true, create: true, settings: false, reauth: true, delete: true },
  ],
  [
    'sensitive_write',
    { upstream: { read: true, sensitive_write: true } },
    { sync: true, create: true, settings: true, reauth: false, delete: false },
  ],
  [
    'sensitive_write + channel',
    {
      upstream: { read: true, sensitive_write: true },
      channel: { sensitive_write: true },
    },
    { sync: true, create: false, settings: true, reauth: false, delete: false },
  ],
])('applies %s permissions to account actions', async (_, grant, expected) => {
  signIn(grant)
  const view = await mountWorkspace({ account: account(), tab: 'groups' })
  const disabled = (selector: string) =>
    view.get(selector).attributes('disabled') !== undefined
  expect(disabled('[data-upstream-sync]')).toBe(expected.sync)
  expect(disabled('[data-upstream-create-key-channel]')).toBe(expected.create)
  expect(menuState(view)).toMatchObject({
    settings: expected.settings,
    reauth: expected.reauth,
    endpoint: expected.reauth,
    delete: expected.delete,
  })
})

it('explains a disabled recovery action in the banner', async () => {
  signIn({ upstream: { read: true } })
  const view = await mountWorkspace({
    account: account({ status: 'reauth_required' }),
    tab: 'groups',
  })
  const banner = view.get('[data-upstream-banner]')
  expect(banner.attributes('role')).toBe('status')
  expect(banner.text()).toContain(i18n.global.t('upstreams.alerts.reauth'))
  expect(banner.text()).toContain(
    i18n.global.t('upstreams.alerts.needPermission')
  )
  expect(
    banner.get('[data-upstream-alert-action]').attributes()
  ).toHaveProperty('disabled')
})
