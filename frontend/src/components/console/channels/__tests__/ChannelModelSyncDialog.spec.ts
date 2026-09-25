import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import type { AdminChannel } from '@/types/console'
import ChannelModelSyncDialog from '../ChannelModelSyncDialog.vue'

function setup(canWrite = true) {
  return mount(ChannelModelSyncDialog, {
    props: { channel: { id: 101 } as AdminChannel, canWrite },
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
        ConsoleModal: { template: '<div><slot /><slot name="footer" /></div>' },
      },
    },
  })
}

afterEach(() => vi.restoreAllMocks())

describe('upstream model update preview', () => {
  it('only applies removals after explicit selection and apply', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValueOnce({ add_models: ['new'], remove_models: ['old'] })
      .mockResolvedValue({})
    const wrapper = setup()
    await flushPromises()
    expect(post).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith(
      '/api/channel/upstream_updates/detect',
      { id: 101 },
      { signal: expect.any(AbortSignal) }
    )
    const checks = wrapper.findAll('input[type="checkbox"]')
    expect((checks[0]!.element as HTMLInputElement).checked).toBe(true)
    expect((checks[1]!.element as HTMLInputElement).checked).toBe(false)
    await checks[0]!.setValue(false)
    await checks[1]!.setValue(true)
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'channels.syncModelsApply')!
      .trigger('click')
    await flushPromises()
    expect(post).toHaveBeenLastCalledWith(
      '/api/channel/upstream_updates/apply',
      {
        id: 101,
        add_models: [],
        remove_models: ['old'],
        ignore_models: [],
      },
      { signal: expect.any(AbortSignal) }
    )
    expect(wrapper.emitted('applied')).toHaveLength(1)
    wrapper.unmount()
  })

  it('aborts closed previews and ignores a late response', async () => {
    let resolve!: (value: {
      add_models: string[]
      remove_models: string[]
    }) => void
    const post = vi.spyOn(api, 'post').mockReturnValue(
      new Promise((done) => {
        resolve = done
      }) as never
    )
    const wrapper = setup()
    const signal = post.mock.calls[0]![2]!.signal!
    await wrapper.setProps({ channel: null })
    expect(signal.aborted).toBe(true)
    resolve({ add_models: ['late-model'], remove_models: [] })
    await flushPromises()
    expect(wrapper.text()).not.toContain('late-model')
    expect(wrapper.emitted('applied')).toBeUndefined()
    wrapper.unmount()
  })

  it('keeps preview available without permission to apply', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ add_models: ['new'], remove_models: [] })
    const wrapper = setup(false)
    await flushPromises()
    const apply = wrapper
      .findAll('button')
      .find((button) => button.text() === 'channels.syncModelsApply')!
    expect(apply.attributes('disabled')).toBeDefined()
    await apply.trigger('click')
    expect(post).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})
