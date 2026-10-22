import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'

import { parseChannelPage } from '@/api/channelStatus'
import ChannelKeyStatus from '@/components/console/channels/ChannelKeyStatus.vue'
import messages from '@/i18n/locales/en/console'
import multiKeyList from '../../../../../tests/fixtures/native/channel-multi-key-list.get.json'

const channels = parseChannelPage(multiKeyList.data).items

function mountStatus(name: string, detailed = false) {
  const channel = channels.find((item) => item.name === name)!
  return mount(ChannelKeyStatus, {
    props: { channel, detailed },
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: messages } }),
      ],
    },
  })
}

describe('channel key status', () => {
  it('shows the available key count for multi-key channels only', () => {
    expect(mountStatus('contract-multi-new').text()).toBe('2/2 keys available')
    expect(mountStatus('contract-multi-exhausted').text()).toBe(
      '0/2 keys available'
    )
    expect(
      mountStatus('contract-single').find('[data-channel-key-status]').exists()
    ).toBe(false)
  })

  it('names each disabled key with its manual or automatic state', () => {
    const wrapper = mountStatus('contract-multi-partial', true)
    const keys = wrapper
      .findAll('[data-channel-disabled-keys] li')
      .map((item) => item.findAll('span').map((part) => part.text()))
    expect(keys).toEqual([
      ['Key #2', messages.channels.statusDisabled],
      ['Key #3', messages.channels.statusAutoDisabled],
    ])
  })

  it('keeps the compact chip free of the per-key list', () => {
    const wrapper = mountStatus('contract-multi-partial')
    expect(wrapper.text()).toBe('1/3 keys available')
    expect(wrapper.find('[data-channel-disabled-keys]').exists()).toBe(false)
  })
})
