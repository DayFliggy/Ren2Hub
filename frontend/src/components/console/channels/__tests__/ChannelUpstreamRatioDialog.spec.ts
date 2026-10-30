import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import type { ChannelUpstreamRatio } from '@/api/channelUpstreamRatio'
import { ApiError } from '@/api/types'
import type { AdminChannel } from '@/types/console'
import ChannelUpstreamRatioDialog from '../ChannelUpstreamRatioDialog.vue'

const record: ChannelUpstreamRatio = {
  channel_id: 101,
  kind: 'newapi',
  source: 'newapi',
  mode: 'auto',
  status: 'ok',
  ratio: '1.5',
  effective_ratio: '1.5',
  group_name: 'vip',
  mapping: 'name_match',
  token_name: 'vip',
  stale: false,
  received_at: 1700000000,
  next_probe_at: 1700001800,
  last_error: '',
  version: 4,
  groups: [
    { name: 'default', description: '', ratio: 1 },
    { name: 'vip', description: 'VIP', ratio: 1.5 },
    { name: 'auto', description: '', ratio: null },
  ],
  sub2api: null,
}

// The backend omits a nil group ratio instead of sending null.
const wire = {
  ...record,
  sub2api: undefined,
  groups: record.groups.map(({ ratio, ...group }) =>
    ratio === null ? group : { ...group, ratio }
  ),
}

function setup(canWrite = true) {
  return mount(ChannelUpstreamRatioDialog, {
    props: {
      channel: { id: 101, name: 'relay' } as AdminChannel,
      canOperate: true,
      canWrite,
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
      stubs: {
        ConsoleModal: {
          template: '<div><slot /><slot name="footer" /></div>',
        },
      },
    },
  })
}

function buttonNamed(wrapper: ReturnType<typeof setup>, text: string) {
  return wrapper.findAll('button').find((button) => button.text() === text)!
}

afterEach(() => vi.restoreAllMocks())

describe('channel upstream ratio dialog', () => {
  it('loads the record with groups and saves a group link using its version', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue([wire])
    const put = vi.spyOn(api, 'put').mockResolvedValue({ ...wire, version: 5 })
    const wrapper = setup()
    await flushPromises()
    expect(get).toHaveBeenCalledWith(
      '/api/channel/upstream_ratios',
      { ids: '101', with_groups: 'true' },
      { signal: expect.any(AbortSignal) }
    )

    const select = wrapper.find('select')
    expect(
      select.findAll('option').map((option) => option.text())
    ).toHaveLength(3)
    await select.setValue('default')
    await buttonNamed(wrapper, 'common.save').trigger('click')
    await flushPromises()

    expect(put).toHaveBeenCalledWith(
      '/api/channel/101/upstream_ratio',
      { mode: 'auto', group_name: 'default', version: 4 },
      { signal: expect.any(AbortSignal) }
    )
    expect(wrapper.emitted('saved')?.[0]?.[0]).toMatchObject({ version: 5 })
  })

  it('rejects an out-of-range manual ratio before sending a request', async () => {
    vi.spyOn(api, 'get').mockResolvedValue([wire])
    const put = vi.spyOn(api, 'put')
    const wrapper = setup()
    await flushPromises()

    await wrapper.findAll('input[type="radio"]')[1]!.setValue(true)
    await wrapper.find('input[inputmode="decimal"]').setValue('1001')
    expect(wrapper.text()).toContain('channels.upstreamRatio.manualInvalid')
    expect(
      (buttonNamed(wrapper, 'common.save').element as HTMLButtonElement)
        .disabled
    ).toBe(true)
    expect(put).not.toHaveBeenCalled()
  })

  it('reloads and shows a conflict message when the version is stale', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue([wire])
    vi.spyOn(api, 'put').mockRejectedValue(
      new ApiError('conflict', { status: 409 })
    )
    const wrapper = setup()
    await flushPromises()
    await buttonNamed(wrapper, 'common.save').trigger('click')
    await flushPromises()

    expect(wrapper.find('[role="alert"]').text()).toBe(
      'channels.upstreamRatio.conflict'
    )
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('hides the save control without write permission', async () => {
    vi.spyOn(api, 'get').mockResolvedValue([wire])
    const wrapper = setup(false)
    await flushPromises()
    expect(wrapper.find('input[type="radio"]').exists()).toBe(false)
    expect(
      wrapper.findAll('button').map((button) => button.text())
    ).not.toContain('common.save')
  })
})
