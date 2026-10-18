import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import UserQuotaModal from '@/components/console/users/UserQuotaModal.vue'
import type { AdminUser } from '@/types/console'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

const member = (id: number) =>
  ({ id, username: `member-${id}`, quota: 0 }) as AdminUser

function setup() {
  const save = vi.fn().mockResolvedValue(true)
  const wrapper = mount(UserQuotaModal, {
    props: { open: true, target: member(1), save },
    global: {
      stubs: {
        ConsoleModal: {
          props: ['open'],
          template: '<div v-if="open"><slot /><slot name="footer" /></div>',
        },
      },
    },
  })
  const submit = async () => {
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'users.quotaSave')!
      .trigger('click')
    await flushPromises()
  }
  return { wrapper, save, submit }
}

describe('UserQuotaModal operation identity', () => {
  it('starts a new operation for a second confirmed grant with the same amount', async () => {
    const { wrapper, save, submit } = setup()
    try {
      await submit()
      await wrapper.setProps({ open: false })
      await wrapper.setProps({ open: true })
      await submit()
      expect(save).toHaveBeenCalledTimes(2)
      expect(save.mock.calls[0]![0]).toBe('500000')
      expect(save.mock.calls[1]![0]).toBe('500000')
      expect(save.mock.calls[1]![2]).not.toBe(save.mock.calls[0]![2])
      expect(wrapper.emitted('close')).toHaveLength(2)
    } finally {
      wrapper.unmount()
    }
  })

  it('reuses an unchanged unconfirmed operation when retrying, including after reopening', async () => {
    const { wrapper, save, submit } = setup()
    try {
      save.mockResolvedValueOnce(false)
      await submit()
      expect(wrapper.emitted('close')).toBeUndefined()
      await wrapper.setProps({ open: false })
      await wrapper.setProps({ open: true })
      await submit()
      expect(save.mock.calls[1]![2]).toBe(save.mock.calls[0]![2])
      expect(wrapper.emitted('close')).toHaveLength(1)
    } finally {
      wrapper.unmount()
    }
  })

  it('does not reuse an unconfirmed operation for a different user', async () => {
    const { wrapper, save, submit } = setup()
    try {
      save.mockResolvedValueOnce(false)
      await submit()
      await wrapper.setProps({ open: false, target: null })
      await wrapper.setProps({ open: true, target: member(2) })
      await submit()
      expect(save.mock.calls[1]![2]).not.toBe(save.mock.calls[0]![2])
    } finally {
      wrapper.unmount()
    }
  })

  it('starts a different operation when the amount changes after an unconfirmed result', async () => {
    const { wrapper, save, submit } = setup()
    try {
      save.mockResolvedValueOnce(false)
      await submit()
      await wrapper.get('input[name="admin-user-quota-amount"]').setValue('2')
      await submit()
      expect(save.mock.calls[1]![0]).toBe('1000000')
      expect(save.mock.calls[1]![2]).not.toBe(save.mock.calls[0]![2])
    } finally {
      wrapper.unmount()
    }
  })

  it('does not submit without a target user', async () => {
    const { wrapper, save, submit } = setup()
    try {
      await wrapper.setProps({ target: null })
      await submit()
      expect(save).not.toHaveBeenCalled()
    } finally {
      wrapper.unmount()
    }
  })
})
