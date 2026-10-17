import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { defineComponent, reactive } from 'vue'
import en from '@/i18n/locales/en/console'
import common from '@/i18n/locales/en/common'
const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  fetchSelf: vi.fn(),
  auth: {
    isRoot: true,
    user: {
      id: 2,
      role: 1,
      status: 1,
      username: 'native',
      display_name: 'Native User',
      quota: 1000000,
      used_quota: 50000000,
      request_count: 3,
      aff_count: 4,
      aff_quota: 1000000,
      aff_history_quota: 1500000,
    },
  },
}))
vi.mock('@/api/client', () => ({ api: mocks }))
vi.mock('@/api/console', () => ({ api: mocks }))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    ...mocks.auth,
    get isRoot() {
      return mocks.auth.isRoot
    },
    fetchSelf: mocks.fetchSelf,
  }),
}))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}))
import AccountCenterView from '../AccountCenterView.vue'
import InviteView from '../InviteView.vue'
import OpsMonitorView from '@/views/admin/OpsMonitorView.vue'
import { useDashboard } from '@/composables/useDashboard'
function global() {
  return {
    plugins: [
      createI18n({
        legacy: false,
        locale: 'en',
        messages: { en: { ...en, ...common } },
      }),
    ],
    stubs: {
      AccountSettingsView: true,
      PageHero: {
        props: ['title'],
        template: '<header>{{title}}<slot/><slot name="actions"/></header>',
      },
      RouterLink: { template: '<a><slot/></a>' },
      OpsSystemTasksCard: true,
      ConsoleModal: {
        props: ['open'],
        template: '<div v-if="open"><slot/><slot name="footer"/></div>',
      },
      ConfirmDialog: true,
    },
  }
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.auth = reactive({ ...mocks.auth, isRoot: true })
  mocks.fetchSelf.mockResolvedValue(undefined)
})
afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
it('restores identity layout with real account totals and explicit unavailable history and tiers', () => {
  const wrapper = mount(AccountCenterView, { global: global() })
  expect(wrapper.text()).toContain('Native User')
  expect(wrapper.get('[data-testid=profile-join-date]').text()).toBe(
    'Unavailable'
  )
  expect(wrapper.get('[data-testid=profile-member-duration]').text()).toBe(
    'Unavailable'
  )
  expect(wrapper.get('[data-testid=profile-stat-balance]').text()).toContain(
    '$2.00'
  )
  expect(wrapper.get('[data-testid=profile-stat-usage]').text()).toContain(
    '$100.00'
  )
  expect(wrapper.text()).not.toContain('Diamond')
  expect(mocks.get).not.toHaveBeenCalled()
  wrapper.unmount()
})
it('copies and shares the real referral link only after user interaction', async () => {
  mocks.get.mockResolvedValue('NATIVE-CODE')
  const open = vi.spyOn(window, 'open').mockReturnValue(null)
  const writeText = vi.fn().mockResolvedValue(undefined)
  const wrapper = mount(InviteView, { global: global() })
  await flushPromises()
  vi.stubGlobal('navigator', { clipboard: { writeText } })
  expect(open).not.toHaveBeenCalled()
  expect(writeText).not.toHaveBeenCalled()
  await wrapper
    .findAll('button')
    .find((button) => button.text() === 'Copy code')!
    .trigger('click')
  await flushPromises()
  expect(writeText).toHaveBeenLastCalledWith('NATIVE-CODE')
  await wrapper
    .findAll('button')
    .find((button) => button.text() === 'Copy link')!
    .trigger('click')
  await flushPromises()
  const link = `${window.location.origin}/sign-up?aff=NATIVE-CODE`
  expect(writeText).toHaveBeenLastCalledWith(link)
  await wrapper.get('button[aria-label="Share on Telegram"]').trigger('click')
  expect(open).toHaveBeenCalledWith(
    `https://t.me/share/url?url=${encodeURIComponent(link)}`,
    '_blank',
    'noopener,noreferrer'
  )
  expect(wrapper.text()).not.toContain('PAYMENT REBATES')
  expect(mocks.post).not.toHaveBeenCalled()
  wrapper.unmount()
})
it('keeps referral reads and transfers native while missing records and trend stay unavailable', async () => {
  mocks.get.mockImplementation(async (path: string) => {
    if (path === '/api/user/aff') return 'NATIVE-CODE'
    throw new Error(`unexpected ${path}`)
  })
  mocks.post.mockResolvedValue(null)
  const wrapper = mount(InviteView, { global: global() })
  await flushPromises()
  expect(wrapper.text()).toContain('NATIVE-CODE')
  expect(wrapper.text()).toContain('Monthly referral trend')
  expect(wrapper.text()).toContain('invitee details are not provided')
  await wrapper
    .findAll('button')
    .find((button) => button.text() === 'Transfer to balance')!
    .trigger('click')
  await wrapper.get('input').setValue('0.5')
  let submit = wrapper
    .findAll('button')
    .filter((button) => button.text() === 'Transfer to balance')
    .at(-1)!
  expect(submit.attributes('disabled')).toBeDefined()
  await wrapper.get('input').setValue('1')
  submit = wrapper
    .findAll('button')
    .filter((button) => button.text() === 'Transfer to balance')
    .at(-1)!
  await submit.trigger('click')
  await flushPromises()
  expect(mocks.post).toHaveBeenCalledWith('/api/user/aff_transfer', {
    quota: 500000,
  })
  expect(
    mocks.get.mock.calls.every((call) => call[0] === '/api/user/aff')
  ).toBe(true)
  wrapper.unmount()
})
it('loads only native runtime statistics, preserves stale readings and removes them on permission loss', async () => {
  const data = {
    memory_stats: { alloc: 1024, num_goroutine: 7 },
    cache_stats: {},
    disk_cache_info: {},
    disk_space_info: {},
    config: {},
  }
  mocks.get.mockResolvedValue(data)
  const wrapper = mount(OpsMonitorView, { global: global() })
  await flushPromises()
  expect(mocks.get).toHaveBeenCalledWith('/api/performance/stats', undefined, {
    signal: expect.any(AbortSignal),
  })
  expect(wrapper.text()).toContain('1,024 bytes')
  expect(wrapper.text()).toContain('Health score')
  expect(wrapper.findAll('[data-unavailable-panel]')).toHaveLength(5)
  mocks.get.mockRejectedValue(new Error('Runtime offline'))
  await wrapper
    .findAll('button')
    .find((button) => button.text() === 'Refresh')!
    .trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('Runtime offline')
  expect(wrapper.text()).toContain('last successfully read data')
  expect(wrapper.text()).toContain('1,024 bytes')
  mocks.auth.isRoot = false
  await flushPromises()
  expect(wrapper.text()).toContain('Root only')
  expect(wrapper.text()).not.toContain('1,024 bytes')
  wrapper.unmount()
})
it('does not request operations data for a non-root user', async () => {
  mocks.auth.isRoot = false
  const wrapper = mount(OpsMonitorView, { global: global() })
  await flushPromises()
  expect(mocks.get).not.toHaveBeenCalled()
  expect(wrapper.find('button').exists()).toBe(false)
  wrapper.unmount()
})
it('aggregates total Token trend from data/self without inventing input/cache classes', async () => {
  const now = Math.floor(Date.now() / 1000)
  mocks.get.mockImplementation(async (path: string) => {
    if (path === '/api/data/self')
      return [
        {
          model_name: 'a',
          created_at: now,
          count: 2,
          quota: 500000,
          token_used: 12,
        },
        {
          model_name: 'b',
          created_at: now,
          count: 1,
          quota: 250000,
          token_used: 8,
        },
      ]
    throw new Error(`unexpected ${path}`)
  })
  let dashboard!: ReturnType<typeof useDashboard>
  const Host = defineComponent({
    setup() {
      dashboard = useDashboard()
      return () => null
    },
  })
  const wrapper = mount(Host, { global: global() })
  await dashboard.load()
  expect(dashboard.tokenTrend.value.at(-1)).toEqual({
    date: expect.any(String),
    total: 20,
  })
  expect(dashboard.stats.value).toMatchObject({
    today_tokens: 20,
    today_requests: 3,
    today_quota: '750000',
  })
  expect(
    mocks.get.mock.calls.every((call) => call[0] === '/api/data/self')
  ).toBe(true)
  wrapper.unmount()
})
