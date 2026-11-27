import { mount } from '@vue/test-utils'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import LogFilterToolbar from '@/components/console/log-ui/LogFilterToolbar.vue'
import LogUnifiedSearch, {
  type LogSearchField,
} from '@/components/console/log-ui/LogUnifiedSearch.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const FIELDS: LogSearchField[] = [
  {
    key: 'request_id',
    label: 'Request ID',
    placeholder: 'Search request ID',
    inputName: 'log-search',
  },
  {
    key: 'model',
    label: 'Model',
    placeholder: 'Filter by model',
    inputName: 'log-model-search',
  },
  {
    key: 'token',
    label: 'Token',
    placeholder: 'Filter by token',
    inputName: 'log-token-search',
  },
]

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

afterAll(async () => {
  await setLocale('zh-CN')
})

describe('LogUnifiedSearch', () => {
  it('hides the field switcher when only one search field is configured', () => {
    const wrapper = mount(LogUnifiedSearch, {
      props: {
        fields: [FIELDS[0]],
        values: { request_id: '' },
      },
      global: { plugins: [i18n] },
    })

    expect(wrapper.find('[data-log-search-field-trigger]').exists()).toBe(false)
    expect(wrapper.findAll('input[type="search"]')).toHaveLength(1)
    expect(wrapper.get('input[type="search"]').attributes('name')).toBe(
      'log-search'
    )
    wrapper.unmount()
  })

  it('switches fields within a single search input and emits per-field values', async () => {
    const wrapper = mount(LogUnifiedSearch, {
      props: {
        fields: FIELDS,
        values: { request_id: 'req-1', model: '', token: '' },
        modelField: 'request_id',
      },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })

    expect(wrapper.findAll('input[type="search"]')).toHaveLength(1)
    expect(
      (wrapper.get('[data-log-search-input]').element as HTMLInputElement).value
    ).toBe('req-1')

    await wrapper.get('[data-log-search-field-trigger]').trigger('click')
    await wrapper.get('[data-search-field-option="model"]').trigger('click')

    expect(wrapper.emitted('update:modelField')?.[0]).toEqual(['model'])
    expect(wrapper.get('[data-log-search-input]').attributes('name')).toBe(
      'log-model-search'
    )
    await wrapper.get('[data-log-search-input]').setValue('gpt-4o')
    expect(wrapper.emitted('update:value')?.at(-1)).toEqual(['model', 'gpt-4o'])
    wrapper.unmount()
  })
})

describe('LogFilterToolbar', () => {
  it('renders active filter chips, single chip removal, global reset, and mobile filter toggle', async () => {
    const wrapper = mount(LogFilterToolbar, {
      props: {
        searchFields: FIELDS,
        searchValues: { request_id: 'req-9', model: 'claude-4', token: '' },
        searchField: 'request_id',
        secondaryActiveCount: 1,
        activeChips: [{ key: 'type', label: 'Type', value: 'Error' }],
        hasActiveFilters: true,
      },
      slots: {
        filters: '<div data-test-secondary>Secondary</div>',
      },
      global: { plugins: [i18n] },
    })

    const chips = wrapper.findAll('[data-log-filter-chip]')
    expect(chips).toHaveLength(3)
    expect(wrapper.text()).toContain('req-9')
    expect(wrapper.text()).toContain('claude-4')
    expect(wrapper.text()).toContain('Error')

    await chips[1].get('button').trigger('click')
    expect(wrapper.emitted('clear-chip')?.[0]).toEqual(['model'])

    await wrapper.get('[data-log-reset-filters]').trigger('click')
    expect(wrapper.emitted('reset')).toHaveLength(1)

    const toggle = wrapper.get('[data-log-mobile-filters-toggle]')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    wrapper.unmount()
  })
})
