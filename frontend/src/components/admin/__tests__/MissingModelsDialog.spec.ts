import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import MissingModelsDialog from '../MissingModelsDialog.vue'

const mocks = vi.hoisted(() => ({ missing: vi.fn() }))
vi.mock('@/api/adminManagement', () => ({ metadataApi: mocks }))
async function mountDialog() {
  const wrapper = mount(MissingModelsDialog, {
    props: { open: true },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en', messages: {} })],
      stubs: {
        ConsoleModal: { template: '<div><slot /><slot name="footer" /></div>' },
      },
    },
  })
  await flushPromises()
  return wrapper
}
function button(wrapper: VueWrapper, text: string) {
  const match = wrapper
    .findAll('button')
    .find((item) => item.text().includes(text))
  if (!match) throw new Error(`button "${text}" not found`)
  return match
}
const names = Array.from({ length: 25 }, (_, index) => `model-${index + 1}`)
beforeEach(() => {
  vi.clearAllMocks()
  mocks.missing.mockResolvedValue(names)
})
describe('missing models dialog', () => {
  it('emits create with the model name when Configure is clicked', async () => {
    const wrapper = await mountDialog()
    const row = wrapper
      .findAll('li')
      .find((item) => item.text().includes('model-3'))!
    await row.get('button:last-child').trigger('click')
    expect(wrapper.emitted('create')).toEqual([['model-3']])
    wrapper.unmount()
  })
  it('emits sync from the footer without creating anything', async () => {
    const wrapper = await mountDialog()
    await button(wrapper, 'Sync missing metadata').trigger('click')
    expect(wrapper.emitted('sync')).toHaveLength(1)
    expect(wrapper.emitted('create')).toBeUndefined()
    wrapper.unmount()
  })
  it('pages 20 rows at a time and searches across every page', async () => {
    const wrapper = await mountDialog()
    expect(wrapper.text()).toContain('Showing 1-20 of 25')
    expect(wrapper.findAll('li')).toHaveLength(20)
    await wrapper.get('button[aria-label="Next page"]').trigger('click')
    expect(wrapper.text()).toContain('Showing 21-25 of 25')
    expect(wrapper.text()).toContain('model-25')
    // model-2 and model-20..25 span both pages; the search resets to page 1.
    await wrapper.get('input[type="search"]').setValue('MODEL-2')
    expect(wrapper.text()).toContain('Showing 1-7 of 7')
    expect(wrapper.find('button[aria-label="Next page"]').exists()).toBe(false)
    await wrapper.get('input[type="search"]').setValue('absent')
    expect(wrapper.text()).toContain('No matches found')
    wrapper.unmount()
  })
  it('shows the load error and retries on request', async () => {
    mocks.missing.mockRejectedValueOnce(new Error('database offline'))
    const wrapper = await mountDialog()
    expect(wrapper.get('[role="alert"]').text()).toContain('database offline')
    await button(wrapper, 'Retry').trigger('click')
    await flushPromises()
    expect(mocks.missing).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('model-1')
    wrapper.unmount()
  })
  it('separates an empty list from an unmatched search', async () => {
    mocks.missing.mockResolvedValueOnce([])
    const wrapper = await mountDialog()
    expect(wrapper.text()).toContain('No missing models found.')
    expect(wrapper.find('input[type="search"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
