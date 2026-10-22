import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

import type { TicketRow } from '@/api/tickets'
import TicketThreadMessage from '@/components/console/tickets/TicketThreadMessage.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { TicketMessage } from '@/types/console'

const mocks = vi.hoisted(() => ({
  detail: vi.fn(),
  changeStatus: vi.fn(),
  attachment: vi.fn(),
  refresh: vi.fn(async () => {}),
}))
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: '8' } }),
  useRouter: () => ({ push: vi.fn() }),
}))
vi.mock('@/api/tickets', async (original) => ({
  ...(await original<typeof import('@/api/tickets')>()),
  ticketsApi: {
    detail: mocks.detail,
    changeStatus: mocks.changeStatus,
    attachment: mocks.attachment,
  },
}))
vi.mock('@/stores/ticketQueue', () => ({
  useTicketQueueStore: () => ({ refresh: mocks.refresh }),
}))
vi.mock('@/composables/useFeatureAccess', () => ({
  useFeatureAccess: () => ({ readOnly: ref(false) }),
}))
import TicketDetailView from '../TicketDetailView.vue'

// Parsed shapes of the controller/tickets.go detail and status responses.
const ticket: TicketRow = {
  id: 8,
  title: 'Invoice attachment',
  category: 'billing',
  priority: 'normal',
  status: 'closed',
  reply_count: 2,
  message_count: 2,
  last_reply_role: 'support',
  created: 100,
  updated: 120,
}
const messages: TicketMessage[] = [
  { id: 1, role: 'user', content: 'Please check', images: [], created: 100 },
  { id: 2, role: 'support', content: 'Done', images: [], created: 110 },
]

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

describe('ticket status change', () => {
  it('applies the PATCH result and keeps the loaded thread without reloading', async () => {
    mocks.detail.mockResolvedValue({ ticket, messages: [...messages] })
    mocks.changeStatus.mockResolvedValue({
      ticket: { ...ticket, status: 'open', updated: 130 },
    })
    const wrapper = mount(TicketDetailView, {
      global: { plugins: [i18n], stubs: { teleport: true } },
    })
    await flushPromises()
    const reopen = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Reopen')
    expect(reopen).toBeDefined()

    await reopen!.trigger('click')
    await flushPromises()

    expect(mocks.changeStatus).toHaveBeenCalledWith(8, 'open')
    expect(mocks.detail).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain(i18n.global.t('tickets.status.open'))
    expect(
      wrapper
        .findAll('button')
        .some((button) => button.text() === 'Close ticket')
    ).toBe(true)
    const thread = wrapper.findAllComponents(TicketThreadMessage)
    expect(thread.map((item) => item.props('message'))).toEqual(messages)
    wrapper.unmount()
  })
})
