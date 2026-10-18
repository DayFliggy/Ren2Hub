import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
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

import { ApiError } from '@/api/types'
import TicketFormModal from '@/components/console/tickets/TicketFormModal.vue'
import TicketReplyBox from '@/components/console/tickets/TicketReplyBox.vue'
import TicketThreadMessage from '@/components/console/tickets/TicketThreadMessage.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import type { AdminTicketItem, TicketMessage } from '@/types/console'
import AdminTicketsView from '@/views/console/AdminTicketsView.vue'
import TicketDetailView from '@/views/console/TicketDetailView.vue'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  patch: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
}))
vi.mock('@/api/console', () => ({ api: mocks }))
vi.mock('@/composables/useToast', () => ({ useToast: () => mocks }))
vi.mock('@/composables/useFeatureAccess', async () => {
  const { ref } = await import('vue')
  return { useFeatureAccess: () => ({ readOnly: ref(false) }) }
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}
const message: TicketMessage = {
  id: 1,
  role: 'user',
  content: 'Initial question',
  images: [],
  created: 10,
}
function ticket(id: number): AdminTicketItem {
  return {
    id,
    title: `Ticket ${id}`,
    category: 'other',
    priority: 'normal',
    status: 'open',
    reply_count: 1,
    message_count: 1,
    last_reply_role: 'user',
    created: 10,
    updated: 10,
    assignee_id: null,
    assigned_at: 0,
    user: { id: 7, username: 'requester', display_name: 'Requester' },
  }
}
const stubs = {
  ConsoleModal: { template: '<div><slot /><slot name="footer" /></div>' },
  TicketImageUploader: {
    template: '<div />',
    methods: { getFiles: () => [], reset: () => {} },
  },
  TicketImageLightbox: true,
  TicketThreadMessage: true,
  TicketReplyBox: {
    template: '<div />',
    props: ['readonly', 'submitting'],
    methods: { reset: () => {} },
  },
}
let wrapper: VueWrapper | undefined
beforeAll(async () => {
  await loadMessageDomain('console')
  setLocale('en')
})
beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
  useAuthStore().user = {
    id: 9,
    username: 'agent',
    display_name: 'Agent',
    email: '',
    role: 100,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
  }
  mocks.get.mockImplementation((url: string) => {
    if (url.endsWith('/summary'))
      return Promise.resolve({ pending: 1, unassigned: 1, mine: 0 })
    if (url.endsWith('/agents')) return Promise.resolve({ items: [] })
    if (url === '/api/admin/tickets')
      return Promise.resolve({ items: [ticket(1)], total: 1 })
    const id = Number(url.split('/').at(-1))
    return Promise.resolve({ ticket: ticket(id), messages: [{ ...message }] })
  })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

async function openDetail(admin = false) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/tickets/:id?',
        name: admin ? 'ticket-management' : 'tickets',
        component: admin ? AdminTicketsView : TicketDetailView,
      },
    ],
  })
  await router.push('/tickets/1')
  await router.isReady()
  wrapper = mount(admin ? AdminTicketsView : TicketDetailView, {
    global: { plugins: [i18n, router], stubs },
  })
  await flushPromises()
  return router
}

describe('native ticket contracts and request lifetime', () => {
  it('sends multipart replies to the personal session route', async () => {
    await openDetail()
    const attachment = new File(['image'], 'proof.png', { type: 'image/png' })
    mocks.post.mockResolvedValue({
      message: { ...message, id: 2, content: 'Follow up' },
    })
    wrapper!
      .findComponent(TicketReplyBox)
      .vm.$emit('submit', { content: 'Follow up', attachments: [attachment] })
    await flushPromises()
    const [url, body] = mocks.post.mock.calls[0]!
    expect(url).toBe('/api/tickets/1/messages')
    expect(body.get('content')).toBe('Follow up')
    expect(body.getAll('attachments')).toEqual([attachment])
    expect(wrapper!.findAllComponents(TicketThreadMessage)).toHaveLength(2)
  })

  it('invalid routes cancel pending detail instead of restoring the old thread', async () => {
    const pending = deferred<{
      ticket: AdminTicketItem
      messages: TicketMessage[]
    }>()
    mocks.get.mockReturnValueOnce(pending.promise)
    const router = await openDetail()
    const signal = mocks.get.mock.calls[0]![2].signal as AbortSignal
    await router.push('/tickets/invalid')
    expect(signal.aborted).toBe(true)
    pending.resolve({ ticket: ticket(1), messages: [message] })
    await flushPromises()
    expect(wrapper!.text()).not.toContain('Ticket 1')
    expect(wrapper!.findComponent(TicketReplyBox).exists()).toBe(false)
  })

  it('shows retryable failures separately from a missing ticket', async () => {
    mocks.get.mockRejectedValueOnce(
      new ApiError('Database unavailable', { business: true })
    )
    await openDetail()
    expect(mocks.error).toHaveBeenCalledWith('Database unavailable')
    expect(wrapper!.text()).toContain('Retry')
  })

  it('rejects a stale admin reply after navigating A to B to A', async () => {
    const router = await openDetail(true)
    const pending = deferred<unknown>()
    mocks.post.mockReturnValueOnce(pending.promise)
    wrapper!
      .findComponent(TicketReplyBox)
      .vm.$emit('submit', { content: 'Stale reply', attachments: [] })
    await router.push('/tickets/2')
    await flushPromises()
    await router.push('/tickets/1')
    await flushPromises()
    pending.resolve({
      message: { ...message, id: 2, content: 'Stale reply' },
      ticket: {
        status: 'replied',
        assignee_id: 9,
        assigned_at: 20,
        updated: 20,
      },
    })
    await flushPromises()
    expect(wrapper!.findAllComponents(TicketThreadMessage)).toHaveLength(1)
    expect(mocks.success).not.toHaveBeenCalled()
  })

  it('cancels the agent catalog and clears its choices on manage permission removal', async () => {
    const pending = deferred<{
      items: { id: number; username: string; display_name: string }[]
    }>()
    const original = mocks.get.getMockImplementation()!
    mocks.get.mockImplementation((url: string, ...rest: unknown[]) =>
      url.endsWith('/agents') ? pending.promise : original(url, ...rest)
    )
    await openDetail(true)
    const call = mocks.get.mock.calls.find(([url]) => url.endsWith('/agents'))!
    useAuthStore().user = {
      ...useAuthStore().user!,
      role: 10,
      permissions: { admin_permissions: { ticket: { read: true } } },
    }
    await flushPromises()
    expect((call[2].signal as AbortSignal).aborted).toBe(true)
    pending.resolve({
      items: [
        { id: 88, username: 'private-agent', display_name: 'Private Agent' },
      ],
    })
    await flushPromises()
    expect(wrapper!.text()).not.toContain('Private Agent')
    expect(wrapper!.findComponent(TicketReplyBox).props('readonly')).toBe(true)
    wrapper!
      .findComponent(TicketReplyBox)
      .vm.$emit('submit', { content: 'Denied', attachments: [] })
    await flushPromises()
    expect(mocks.post).not.toHaveBeenCalled()
  })

  it('does not close a reopened creation form when its previous submission finishes', async () => {
    wrapper = mount(TicketFormModal, {
      props: { open: true },
      global: { plugins: [i18n], stubs },
    })
    await wrapper.get('input[name="ticket-title"]').setValue('Question')
    await wrapper.get('textarea[name="ticket-content"]').setValue('Please help')
    const pending = deferred<unknown>()
    mocks.post.mockReturnValueOnce(pending.promise)
    const button = wrapper
      .findAll('button')
      .find((item) => item.text() === 'Confirm')!
    await button.trigger('click')
    expect(mocks.post.mock.calls[0]![0]).toBe('/api/tickets')
    expect(mocks.post.mock.calls[0]![1].get('content')).toBe('Please help')
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    pending.resolve({ ticket: ticket(1) })
    await flushPromises()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(mocks.success).not.toHaveBeenCalled()
  })
})
