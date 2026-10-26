import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi } from 'vitest'

import { parseChannelPage } from '@/api/channelStatus'
import ChannelMobileList from '@/components/console/channels/ChannelMobileList.vue'
import VendorLogo from '@/components/console/models/VendorLogo.vue'
import type { AdminChannelOptionalField } from '@/constants/adminChannels'
import type { AdminChannel } from '@/types/console'
import channelList from '../../../../../tests/fixtures/native/channel-list.get.json'

const base: AdminChannel = {
  id: 1,
  name: 'openai',
  type: 1,
  channel_ratio: '1',
  max_concurrency: 0,
  in_flight: 0,
  lab: { id: 'openai', name: 'OpenAI', source: 'default' },
  status: 1,
  priority: 0,
  weight: 0,
  used_quota: 0,
  balance: 0,
  response_time: 0,
  test_time: 0,
  base_url: '',
  models: 'gpt-4o',
  model_mapping: '',
}

function channel(patch: Partial<AdminChannel>): AdminChannel {
  return { ...base, ...patch }
}

function mountList(
  channels: AdminChannel[],
  canWrite = true,
  visibleFields: AdminChannelOptionalField[] = ['upstream', 'rowUpstreamAction']
) {
  const refreshBalance = vi.fn().mockResolvedValue(true)
  const runSupplierBalance = vi.fn().mockResolvedValue(undefined)
  const wrapper = mount(ChannelMobileList, {
    props: {
      groups: [{ groupSlug: 'g', supplier: 'Group', channels }],
      visibleFields,
      selectedIds: [],
      allSelected: false,
      selectionDisabled: false,
      toggleAllSelected: vi.fn(),
      toggleSelected: vi.fn(),
      isSupplierCollapsed: () => false,
      toggleSupplier: vi.fn(),
      batchProgress: null,
      groupTestOverrides: {},
      canRunBatch: true,
      canMutate: true,
      canOperate: true,
      canWrite,
      canSensitiveWrite: true,
      runSupplierBalance,
      pickSupplierTest: vi.fn(),
      clearSupplier: vi.fn(),
      isBusy: () => false,
      isRowBusy: () => false,
      updateNumber: vi.fn(),
      toggleStatus: vi.fn(),
      refreshBalance,
      openTest: vi.fn(),
      editChannel: vi.fn(),
      syncModels: vi.fn(),
      showRuntime: vi.fn(),
      deleteChannel: vi.fn(),
    },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          messages: {
            en: {
              channels: { channelRatioValue: 'Channel ratio {ratio}×' },
            },
          },
          missingWarn: false,
          fallbackWarn: false,
        }),
      ],
    },
  })
  return { wrapper, refreshBalance, runSupplierBalance }
}

function balanceButtons(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll('button')
    .filter((button) =>
      button.attributes('aria-label')?.includes('channels.refreshBalance')
    )
}

function groupBalanceButton(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll('button')
    .find((button) =>
      button.attributes('aria-label')?.includes('channels.syncSupplier')
    )
}

describe('channel mobile list balance actions', () => {
  it('offers balance refresh only where the backend has a balance branch', async () => {
    const { wrapper, refreshBalance } = mountList([
      channel({ id: 1, type: 1 }),
      channel({ id: 2, type: 3 }),
      channel({ id: 3, type: 61 }),
      channel({
        id: 4,
        type: 1,
        channel_info: {
          is_multi_key: true,
          multi_key_mode: 'random',
          multi_key_size: 2,
          multi_key_status_list: null,
        },
      }),
      channel({ id: 5, type: 58 }),
    ])

    const buttons = balanceButtons(wrapper)
    expect(buttons).toHaveLength(2)
    for (const button of buttons) await button.trigger('click')
    expect(refreshBalance.mock.calls.map(([item]) => item.id)).toEqual([1, 5])
    expect(groupBalanceButton(wrapper)).toBeDefined()
  })

  it('hides the group balance action when no channel supports it', () => {
    const { wrapper } = mountList([
      channel({ id: 2, type: 14 }),
      channel({ id: 3, type: 61 }),
    ])

    expect(balanceButtons(wrapper)).toHaveLength(0)
    expect(groupBalanceButton(wrapper)).toBeUndefined()
  })
})

describe('channel mobile list edit action', () => {
  const editButtons = (wrapper: ReturnType<typeof mount>) =>
    wrapper
      .findAll('button')
      .filter(
        (button) => button.attributes('aria-label') === 'channels.editChannel'
      )

  it('opens every non-retired type, task plugin channels included', async () => {
    const channels = [
      channel({ id: 1, type: 61 }),
      channel({ id: 2, type: 57 }),
      channel({ id: 3, type: 60 }),
    ]
    const { wrapper } = mountList(channels)
    const buttons = editButtons(wrapper)
    expect(buttons).toHaveLength(2)
    for (const button of buttons) await button.trigger('click')
    const editChannel = wrapper.props('editChannel') as ReturnType<typeof vi.fn>
    expect(editChannel.mock.calls.map(([item]) => item.id)).toEqual([1, 3])
  })

  it('offers no edit entry without channel write', () => {
    const { wrapper } = mountList([channel({ id: 1, type: 61 })], false)
    expect(editButtons(wrapper)).toHaveLength(0)
  })
})

describe('channel mobile list backend labs', () => {
  const { items } = parseChannelPage(channelList.data)

  it('names each row logo from the backend lab classification', () => {
    const { wrapper } = mountList(items, true, ['type'])
    const rowLogos = wrapper
      .findAll('[data-channel-mobile-row]')
      .map((row) => row.findComponent(VendorLogo).props('vendor'))
    expect(rowLogos).toEqual([
      'channels.labUnknown',
      'channels.labUnknown',
      'channels.labMixed',
      'OpenAI',
    ])
  })

  it('labels only the row whose model mapping the backend rejected', () => {
    const { wrapper } = mountList(items)
    const flagged = wrapper
      .findAll('[data-channel-mobile-row]')
      .filter((row) => row.find('[data-channel-invalid-mapping]').exists())
    expect(flagged).toHaveLength(1)
    expect(flagged[0]!.text()).toContain('contract-invalid')
    expect(flagged[0]!.text()).toContain('channels.labInvalidMapping')
  })
})

describe('channel mobile list concurrency and pricing', () => {
  it('distinguishes unlimited concurrency from an explicit cap and shows its ratio', () => {
    const { wrapper } = mountList(
      [
        channel({
          id: 1,
          in_flight: 3,
          max_concurrency: 0,
          channel_ratio: '1.25',
        }),
        channel({
          id: 2,
          in_flight: 2,
          max_concurrency: 8,
          channel_ratio: '0.5',
        }),
      ],
      true,
      ['capacity', 'usage']
    )
    const rows = wrapper.findAll('[data-channel-mobile-row]')
    expect(rows[0]!.text()).toContain('3 / ∞')
    expect(rows[0]!.text()).toContain('Channel ratio 1.250×')
    expect(rows[1]!.text()).toContain('2 / 8')
    expect(rows[1]!.text()).toContain('Channel ratio 0.500×')
  })
})
