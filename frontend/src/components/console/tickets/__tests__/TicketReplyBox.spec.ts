import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import TicketReplyBox from '@/components/console/tickets/TicketReplyBox.vue'
import i18n from '@/i18n'

describe('TicketReplyBox', () => {
  it('commits IME text before Enter sends and preserves Shift+Enter', async () => {
    const wrapper = mount(TicketReplyBox, {
      global: { plugins: [i18n], stubs: { TicketImageUploader: true } },
    })
    try {
      const textarea = wrapper.get('textarea')
      await textarea.setValue('Draft ')
      await textarea.trigger('compositionstart')
      const composing = new KeyboardEvent('keydown', {
        key: 'Enter',
        isComposing: true,
        bubbles: true,
        cancelable: true,
      })
      textarea.element.dispatchEvent(composing)
      expect(composing.defaultPrevented).toBe(false)
      expect(wrapper.emitted('submit')).toBeUndefined()

      ;(textarea.element as HTMLTextAreaElement).value = 'Draft 中文'
      await textarea.trigger('compositionend')
      const newline = new KeyboardEvent('keydown', {
        key: 'Enter',
        shiftKey: true,
        bubbles: true,
        cancelable: true,
      })
      textarea.element.dispatchEvent(newline)
      expect(newline.defaultPrevented).toBe(false)
      expect(wrapper.emitted('submit')).toBeUndefined()

      await textarea.trigger('keydown', { key: 'Enter' })
      expect(wrapper.emitted('submit')).toEqual([
        [{ content: 'Draft 中文', attachments: [] }],
      ])
    } finally {
      wrapper.unmount()
    }
  })

  it('keeps the draft after submit until the parent confirms success', async () => {
    const wrapper = mount(TicketReplyBox, {
      global: {
        plugins: [i18n],
        stubs: {
          TicketImageUploader: true,
        },
      },
    })
    const textarea = wrapper.get('textarea')

    await textarea.setValue('  Please keep this draft  ')
    await wrapper
      .get('button[type="submit"], button:last-child')
      .trigger('click')

    expect(wrapper.emitted('submit')).toEqual([
      [{ content: 'Please keep this draft', attachments: [] }],
    ])
    expect((textarea.element as HTMLTextAreaElement).value).toBe(
      '  Please keep this draft  '
    )

    wrapper.vm.reset()
    await wrapper.vm.$nextTick()

    expect((textarea.element as HTMLTextAreaElement).value).toBe('')
  })
})
