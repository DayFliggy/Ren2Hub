import { mount, type VueWrapper } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'

import i18n, { loadMessageDomain, setLocale } from '@/i18n'

import * as pluginApi from '../api'
import MarketplaceSourcesDialog from '../components/MarketplaceSourcesDialog.vue'

vi.mock('../api', () => ({
  updateMarketplaceSources: vi.fn(),
}))

let wrapper: VueWrapper | undefined

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

beforeEach(() => vi.clearAllMocks())

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

async function mountDialog(sources: { name: string; index_url: string }[]) {
  wrapper = mount(MarketplaceSourcesDialog, {
    props: { sources },
    global: { plugins: [i18n], stubs: { Teleport: true } },
  })
  return wrapper
}

function saveButton(root: VueWrapper) {
  const save = root
    .findAll('button')
    .find((button) => button.text() === i18n.global.t('taskPlugins.save'))
  expect(save, 'save button').toBeTruthy()
  return save!
}

describe('marketplace source list', () => {
  it('keeps a configured source list editable', async () => {
    const root = await mountDialog([
      { name: 'Official', index_url: 'https://example.com/index.json' },
    ])
    expect(saveButton(root).attributes('disabled')).toBeUndefined()
  })

  // The server stores "[]" verbatim, which reads back as a deliberate empty
  // market instead of the builtin defaults. The dialog must not offer to save
  // that state, even though no row is individually invalid.
  it('blocks saving once every source row is removed', async () => {
    const root = await mountDialog([
      { name: 'Official', index_url: 'https://example.com/index.json' },
    ])
    const remove = root.findAll('button[aria-label]')
    expect(remove.length).toBeGreaterThan(0)
    const removeRow = remove.find((button) =>
      button
        .attributes('aria-label')
        ?.startsWith(
          i18n.global.t('taskPlugins.removeSource', { name: 'Official' })
        )
    )
    expect(removeRow, 'remove row button').toBeTruthy()
    await removeRow!.trigger('click')

    expect(saveButton(root).attributes('disabled')).toBeDefined()
    expect(root.text()).toContain(i18n.global.t('taskPlugins.sourcesRequired'))

    await saveButton(root).trigger('click')
    expect(pluginApi.updateMarketplaceSources).not.toHaveBeenCalled()
  })
})
