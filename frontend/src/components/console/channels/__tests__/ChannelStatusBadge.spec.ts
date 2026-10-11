import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import ChannelStatusBadge from '../ChannelStatusBadge.vue'
import type { AdminChannel } from '@/types/console'
import { mergeServerMessages } from '@/i18n/serverMessages'

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

it('shows retirement independently of the historical enabled state', async () => {
  const wrapper = badge()
  await wrapper.setProps({
    channel: { id: 57, type: 57, status: 1 } as AdminChannel,
  })
  expect(wrapper.text()).toBe('channels.statusRetired')
  expect(wrapper.find('button').exists()).toBe(false)
  expect(wrapper.attributes('title')).toBe('channels.retiredHint')
  wrapper.unmount()
})

describe('channel status reason language', () => {
  it('translates a gateway-written reason and keeps provider text', async () => {
    mergeServerMessages('zh-CN', { 'Insufficient balance': '余额不足' })
    for (const [reason, expected] of [
      ['Insufficient balance', '余额不足'],
      ['provider unavailable', 'provider unavailable'],
    ]) {
      const wrapper = mount(ChannelStatusBadge, {
        props: {
          channel: { id: 1, status: 3, status_reason: reason } as AdminChannel,
        },
        global: {
          plugins: [
            createI18n({
              legacy: false,
              locale: 'zh-CN',
              messages: { 'zh-CN': { channels: { disabledReason: '原因' } } },
            }),
          ],
          stubs: { Teleport: true },
        },
      })
      await wrapper.get('button').trigger('focus')
      expect(wrapper.get('[role="tooltip"]').text()).toContain(expected)
      wrapper.unmount()
    }
  })
})
