import { flushPromises, shallowMount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import AdminTicketsView from '../AdminTicketsView.vue'

const state = vi.hoisted(() => ({
  assign: vi.fn(),
}))
const permissions = reactive({ manage: true, reply: true, root: false })
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: '8' }, query: {} }),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { id: 1, username: 'admin' },
    hasPermission: (_resource: string, action: 'manage' | 'reply') =>
      permissions.root || permissions[action],
  }),
}))
vi.mock('@/stores/ticketQueue', () => ({
  useTicketQueueStore: () => ({
    summary: { open: 1, unassigned: 1 },
    refresh: vi.fn(),
  }),
}))
const ticket = {
  id: 8,
  title: 'Help',
  user: { id: 3, username: 'alice', email: '' },
  category: 'other',
  priority: 'normal',
  status: 'open',
  assignee_id: null,
  created: 100,
  updated: 100,
  messages: [],
  reply_count: 0,
}
vi.mock('@/api/tickets', () => ({
  isAttachmentStorageFailure: () => false,
  ticketsApi: {
    adminList: vi.fn(async () => ({ items: [], total: 0 })),
    adminDetail: vi.fn(async () => ({ ticket: { ...ticket }, messages: [] })),
    agents: vi.fn(async () => [{ id: 2, username: 'operator' }]),
    assign: state.assign,
  },
}))
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(() => {
  Object.assign(permissions, { manage: true, reply: true, root: false })
  state.assign
    .mockReset()
    .mockResolvedValue({ ticket: { ...ticket, assignee_id: 1 } })
})
async function render() {
  const wrapper = shallowMount(AdminTicketsView, {
    global: { plugins: [i18n] },
  })
  await flushPromises()
  return wrapper
}
describe('ticket self assignment permissions', () => {
  it.each([
    [true, false, false, false],
    [false, true, false, false],
    [true, true, false, true],
    [false, false, true, true],
  ])(
    'requires manage and reply or root (%s, %s, %s)',
    async (manage, reply, root, allowed) => {
      Object.assign(permissions, { manage, reply, root })
      const wrapper = await render()
      const take = wrapper
        .findAll('button')
        .find((button) =>
          button.text().includes(i18n.global.t('tickets.admin.take'))
        )
      expect(Boolean(take)).toBe(allowed)
      if (take) {
        await take.trigger('click')
        await flushPromises()
        expect(state.assign).toHaveBeenCalledWith(8, 1)
      } else expect(state.assign).not.toHaveBeenCalled()
      expect(wrapper.findComponent({ name: 'FilterSelect' }).exists()).toBe(
        true
      )
      wrapper.unmount()
    }
  )
  it('guards a previously rendered self-assignment action after reply permission is revoked', async () => {
    const wrapper = await render()
    const take = wrapper
      .findAll('button')
      .find((button) =>
        button.text().includes(i18n.global.t('tickets.admin.take'))
      )!
    permissions.reply = false
    await take.trigger('click')
    await flushPromises()
    expect(state.assign).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('allows a manager without reply permission to assign another eligible operator', async () => {
    permissions.reply = false
    const wrapper = await render()
    const assignee = wrapper
      .findAllComponents({ name: 'FilterSelect' })
      .filter(
        (select) =>
          select.props('label') === i18n.global.t('tickets.admin.assignee')
      )
      .at(-1)!
    expect(assignee.props('disabled')).toBe(false)
    assignee.vm.$emit('update:modelValue', '2')
    const save = wrapper
      .findAllComponents({ name: 'ConsoleButton' })
      .find((button) => button.props('block') && button.classes('mt-2'))!
    await save.trigger('click')
    await flushPromises()
    expect(state.assign).toHaveBeenCalledWith(8, 2)
    wrapper.unmount()
  })
})
