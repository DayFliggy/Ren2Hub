import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '@/api/types'
import TicketFormModal from '@/components/console/tickets/TicketFormModal.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() },
}))
vi.mock('@/api/tickets', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/tickets')>()),
  ticketsApi: { create: mocks.create },
}))
vi.mock('@/composables/useToast', () => ({ useToast: () => mocks.toast }))

beforeAll(async () => {
  await loadMessageDomain('console')
  setLocale('en')
})
beforeEach(() => vi.clearAllMocks())

async function submit(error: ApiError) {
  mocks.create.mockRejectedValue(error)
  const wrapper = mount(TicketFormModal, {
    props: { open: true },
    global: {
      plugins: [i18n],
      stubs: {
        ConsoleModal: {
          props: ['open'],
          template: '<div v-if="open"><slot /><slot name="footer" /></div>',
        },
        TicketImageUploader: {
          template: '<div />',
          methods: { getFiles: () => [], reset: () => undefined },
        },
      },
    },
  })
  await wrapper.get('input[name="ticket-title"]').setValue('Invoice')
  await wrapper.get('textarea').setValue('Please check the attachment')
  const buttons = wrapper.findAll('button')
  await buttons.at(-1)!.trigger('click')
  await flushPromises()
  return wrapper
}

describe('ticket creation attachment failures', () => {
  it('treats a saved ticket with unstored attachments as created, not retryable', async () => {
    const wrapper = await submit(
      new ApiError('Ticket was saved but its attachments could not be stored', {
        status: 200,
        business: true,
        code: 'ATTACHMENT_STORAGE_FAILED',
      })
    )
    expect(mocks.create).toHaveBeenCalledOnce()
    expect(wrapper.emitted('saved')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(mocks.toast.error).not.toHaveBeenCalled()
    expect(mocks.toast.warning).toHaveBeenCalledWith(
      expect.stringContaining('do not submit it again')
    )
  })
  it('keeps the form open for a rejected attachment so it can be fixed', async () => {
    const wrapper = await submit(
      new ApiError('Attachment a.png exceeds 10 MB', {
        status: 200,
        business: true,
        code: 'ATTACHMENT_TOO_LARGE',
      })
    )
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(mocks.toast.error).toHaveBeenCalledWith(
      'Attachment a.png exceeds 10 MB'
    )
  })
})
