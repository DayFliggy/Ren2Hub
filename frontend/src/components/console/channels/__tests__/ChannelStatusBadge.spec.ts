import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import ChannelStatusBadge from '../ChannelStatusBadge.vue'
import type { AdminChannel } from '@/types/console'

function badge(mobile = false) {
  return mount(ChannelStatusBadge, {
    props: {
      channel: {
        id: 1,
        status: 3,
        status_reason: '<img src=x> provider unavailable',
        status_time: 123,
      } as AdminChannel,
      mobile,
    },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          messages: {
            en: {
              channels: {
                statusAutoDisabled: 'Auto-disabled',
                disabledReason: 'Reason',
                disabledAt: 'Disabled at {time}',
              },
            },
          },
        }),
      ],
      stubs: { Teleport: true },
    },
  })
}
describe('automatic channel disable reason', () => {
  it('shows plain text on desktop focus and closes with Escape', async () => {
    const wrapper = badge()
    await wrapper.get('button').trigger('focus')
    expect(wrapper.get('[role="tooltip"]').text()).toContain(
      '<img src=x> provider unavailable'
    )
    expect(wrapper.find('img').exists()).toBe(false)
    await wrapper.get('button').trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[role="tooltip"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('uses an explicit mobile toggle and resets for another channel', async () => {
    const wrapper = badge(true)
    await wrapper.get('button').trigger('mouseenter')
    expect(wrapper.find('[role="tooltip"]').exists()).toBe(false)
    await wrapper.get('button').trigger('click')
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('true')
    await wrapper.setProps({ channel: { id: 2, status: 1 } as AdminChannel })
    expect(wrapper.find('[role="tooltip"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
