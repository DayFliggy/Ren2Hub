import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'

import ChannelToolbarMenu from '@/components/console/channels/ChannelToolbarMenu.vue'

function mountMenu(overrides: Record<string, unknown> = {}) {
  return mount(ChannelToolbarMenu, {
    props: {
      groupByLab: false,
      sortBy: 'id' as const,
      sortOrder: 'desc' as const,
      canOperate: true,
      balanceAllBusy: false,
      pageBalanceBusy: false,
      pageBalanceProgress: '',
      pageBalanceDisabled: false,
      pageBalanceCount: 3,
      batchTestDisabled: false,
      ...overrides,
    },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          missingWarn: false,
          fallbackWarn: false,
        }),
      ],
      stubs: { Teleport: true },
    },
  })
}

describe('ChannelToolbarMenu', () => {
  it('toggles vendor grouping, sort field, and sort order', async () => {
    const wrapper = mountMenu()
    await wrapper.get('[data-channel-toolbar-menu-trigger]').trigger('click')
    expect(wrapper.find('[data-channel-toolbar-menu]').exists()).toBe(true)

    await wrapper.get('[data-channel-group-toggle]').trigger('click')
    expect(wrapper.emitted('update:groupByLab')).toEqual([[true]])

    await wrapper.get('[data-channel-sort-option="balance"]').trigger('click')
    expect(wrapper.emitted('update:sortBy')).toEqual([['balance']])

    await wrapper.get('[data-channel-sort-order="asc"]').trigger('click')
    expect(wrapper.emitted('update:sortOrder')).toEqual([['asc']])

    wrapper.unmount()
  })

  it('respects balanceAllDisabled separately from busy spinner state', async () => {
    const wrapper = mountMenu({ balanceAllDisabled: true })
    await wrapper.get('[data-channel-toolbar-menu-trigger]').trigger('click')
    const btn = wrapper.get('[data-channel-menu-balance-all]')
    expect(btn.attributes('disabled')).toBeDefined()
    await btn.trigger('click')
    expect(wrapper.emitted('startBalanceAll')).toBeUndefined()
    wrapper.unmount()
  })

  it('triggers batch actions and closes menu; hides batch section without operate permission', async () => {
    const wrapper = mountMenu()
    await wrapper.get('[data-channel-toolbar-menu-trigger]').trigger('click')
    await wrapper.get('[data-channel-menu-balance-all]').trigger('click')
    expect(wrapper.emitted('startBalanceAll')).toHaveLength(1)
    expect(wrapper.find('[data-channel-toolbar-menu]').exists()).toBe(false)

    await wrapper.get('[data-channel-toolbar-menu-trigger]').trigger('click')
    await wrapper.get('[data-channel-menu-balance-page]').trigger('click')
    expect(wrapper.emitted('runPageBalance')).toHaveLength(1)

    await wrapper.get('[data-channel-toolbar-menu-trigger]').trigger('click')
    await wrapper.get('[data-channel-menu-batch-test]').trigger('click')
    expect(wrapper.emitted('openPageTest')).toHaveLength(1)
    wrapper.unmount()

    const readOnly = mountMenu({ canOperate: false })
    await readOnly.get('[data-channel-toolbar-menu-trigger]').trigger('click')
    expect(readOnly.find('[data-channel-menu-balance-all]').exists()).toBe(
      false
    )
    expect(readOnly.find('[data-channel-menu-balance-page]').exists()).toBe(
      false
    )
    expect(readOnly.find('[data-channel-menu-batch-test]').exists()).toBe(false)
    readOnly.unmount()
  })
})
