import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { MODEL_ENDPOINT_TEMPLATES } from '@/utils/modelEndpoints'
import PrefillGroupDialog from '../PrefillGroupDialog.vue'

const mocks = vi.hoisted(() => ({
  groups: vi.fn(),
  saveGroup: vi.fn(),
  deleteGroup: vi.fn(),
}))
vi.mock('@/api/adminManagement', () => ({ metadataApi: mocks }))

const ModalStub = {
  props: ['open', 'title', 'ariaLabel'],
  emits: ['close'],
  template:
    '<div v-if="open" :data-modal="title || ariaLabel"><slot /><slot name="footer" /></div>',
}
const endpointItems = JSON.stringify(
  { openai: { path: '/v1/chat/completions', method: 'POST' } },
  null,
  2
)
const fixtures = [
  {
    id: 1,
    name: 'zeta',
    description: '',
    type: 'model',
    items: ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h'],
  },
  {
    id: 3,
    name: 'chat',
    description: 'Chat models',
    type: 'model',
    items: ['gpt'],
  },
  {
    id: 2,
    name: 'routes',
    description: '',
    type: 'endpoint',
    items: endpointItems,
  },
]

function setup() {
  return mount(PrefillGroupDialog, {
    props: { open: true },
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', missingWarn: false }),
        createRouter({
          history: createMemoryHistory(),
          routes: [
            { path: '/:pathMatch(.*)*', component: { template: '<div />' } },
          ],
        }),
      ],
      stubs: { ConsoleModal: ModalStub },
    },
  })
}
function button(wrapper: VueWrapper, text: string) {
  const found = wrapper
    .findAll('button')
    .find((item) => item.text().trim() === text)
  if (!found) throw new Error(`button "${text}" not found`)
  return found
}
const listOpen = (wrapper: VueWrapper) =>
  wrapper.find('[data-modal="Prefill Group Management"]').exists()
const formModal = (wrapper: VueWrapper) =>
  wrapper.find('[data-modal="Create Prefill Group"]')

async function chooseEndpointType(wrapper: VueWrapper) {
  await wrapper
    .get('[role="combobox"][aria-label="Group Type"]')
    .trigger('click')
  const option = wrapper
    .findAll('[role="option"]')
    .find((item) => item.text() === 'Endpoint Group')!
  await option.trigger('click')
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.groups.mockResolvedValue(fixtures)
  mocks.saveGroup.mockResolvedValue({})
})

describe('PrefillGroupDialog list', () => {
  it('keeps an edited group when returning to the list is canceled', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const wrapper = setup()
    await flushPromises()
    await button(wrapper, 'New Group').trigger('click')
    await wrapper
      .get('input[name="prefill-group-name"]')
      .setValue('Unsaved group')
    await button(wrapper, 'Cancel').trigger('click')
    expect(formModal(wrapper).exists()).toBe(true)
    expect(confirm).toHaveBeenCalledTimes(1)
    expect(
      wrapper.get('input[name="prefill-group-name"]').element
    ).toHaveProperty('value', 'Unsaved group')
    const unload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(unload)
    expect(unload.defaultPrevented).toBe(true)
    wrapper.unmount()
    confirm.mockRestore()
  })

  it('sorts by type then name and previews at most six items', async () => {
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.text()).toContain('3 groups')
    const rows = wrapper.findAll('tbody tr')
    expect(rows.map((row) => row.get('td span').text())).toEqual([
      'routes',
      'chat',
      'zeta',
    ])
    expect(rows[1]!.text()).toContain('Chat models')
    expect(rows[2]!.text()).toContain('No description provided')
    expect(rows[0]!.text()).toContain('openai')
    expect(rows[0]!.text()).toContain('Endpoint Group')
    const zetaItems = rows[2]!.findAll('td')[2]!
    expect(
      zetaItems.findAll('[data-handdrawn="chip"]').map((c) => c.text())
    ).toEqual(['a', 'b', 'c', 'd', 'e', 'f', '+2'])
    expect(zetaItems.text()).toContain('8 items')
    wrapper.unmount()
  })

  it('swaps the list for the form drawer and back, then resets on reopen', async () => {
    const wrapper = setup()
    await flushPromises()
    await button(wrapper, 'New Group').trigger('click')
    expect(listOpen(wrapper)).toBe(false)
    expect(formModal(wrapper).exists()).toBe(true)
    await button(wrapper, 'Cancel').trigger('click')
    await flushPromises()
    expect(listOpen(wrapper)).toBe(true)
    expect(mocks.groups).toHaveBeenCalledTimes(2)
    await button(wrapper, 'New Group').trigger('click')
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    expect(listOpen(wrapper)).toBe(true)
    expect(formModal(wrapper).exists()).toBe(false)
    wrapper.unmount()
  })

  it('confirms deletion and keeps the dialog open when it fails', async () => {
    mocks.deleteGroup
      .mockRejectedValueOnce(new Error('group denied'))
      .mockResolvedValueOnce(undefined)
    const wrapper = setup()
    await flushPromises()
    await wrapper
      .findAll('button[aria-label="Delete group"]')[0]!
      .trigger('click')
    const dialog = wrapper.getComponent(ConfirmDialog)
    expect(dialog.props('message')).toContain('"routes"')
    dialog.vm.$emit('confirm')
    await flushPromises()
    expect(mocks.deleteGroup).toHaveBeenCalledWith(2)
    expect(dialog.props('open')).toBe(true)
    expect(dialog.text()).toContain('group denied')
    expect(wrapper.emitted('saved')).toBeUndefined()
    dialog.vm.$emit('confirm')
    await flushPromises()
    expect(dialog.props('open')).toBe(false)
    expect(wrapper.emitted('saved')).toHaveLength(1)
    expect(mocks.groups).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})

describe('PrefillGroupDialog form', () => {
  it('rejects duplicate list items and saves trimmed ones', async () => {
    const wrapper = setup()
    await flushPromises()
    await button(wrapper, 'New Group').trigger('click')
    await wrapper.get('input[name="prefill-group-name"]').setValue('  Chat  ')
    await button(wrapper, 'Add item').trigger('click')
    await button(wrapper, 'Add item').trigger('click')
    await wrapper.get('input[aria-label="Items 1"]').setValue('gpt-4o')
    await wrapper.get('input[aria-label="Items 2"]').setValue(' gpt-4o ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.saveGroup).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe(
      'Items cannot be empty or duplicated'
    )
    await wrapper.get('input[aria-label="Items 2"]').setValue(' o3 ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.saveGroup).toHaveBeenCalledWith({
      id: undefined,
      name: 'Chat',
      description: '',
      type: 'model',
      items: ['gpt-4o', 'o3'],
    })
    expect(wrapper.emitted('saved')).toHaveLength(1)
    expect(listOpen(wrapper)).toBe(true)
    wrapper.unmount()
  })

  it('validates endpoint JSON and fills the endpoint template', async () => {
    const wrapper = setup()
    await flushPromises()
    await button(wrapper, 'New Group').trigger('click')
    await wrapper.get('input[name="prefill-group-name"]').setValue('Routes')
    await chooseEndpointType(wrapper)
    await wrapper
      .get('textarea[name="prefill-group-endpoints"]')
      .setValue('{"openai": {"path": "v1"}}')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.saveGroup).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe(
      'Endpoints must be a string array or a valid endpoint object'
    )
    await button(wrapper, 'Fill Template').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const payload = mocks.saveGroup.mock.calls[0]![0]
    expect(payload).toMatchObject({ name: 'Routes', type: 'endpoint' })
    expect(JSON.parse(payload.items)).toEqual(MODEL_ENDPOINT_TEMPLATES)
    wrapper.unmount()
  })

  it('edits an endpoint group with its id and stored mappings', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper
      .findAll('button[aria-label="Edit group"]')[0]!
      .trigger('click')
    expect(wrapper.find('[data-modal="Edit Prefill Group"]').exists()).toBe(
      true
    )
    expect(
      wrapper.get('textarea[name="prefill-group-endpoints"]').element
    ).toHaveProperty('value', endpointItems)
    expect(button(wrapper, 'Save changes').attributes('type')).toBe('submit')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.saveGroup).toHaveBeenCalledWith({
      id: 2,
      name: 'routes',
      description: '',
      type: 'endpoint',
      items: JSON.stringify({
        openai: { path: '/v1/chat/completions', method: 'POST' },
      }),
    })
    wrapper.unmount()
  })
})
