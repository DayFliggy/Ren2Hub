import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import DataTable from '@/components/common/DataTable.vue'
import i18n from '@/i18n'

describe('DataTable keyboard rows', () => {
  it('activates clickable rows with Enter and Space but ignores child controls', async () => {
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        rows: [{ id: 1, name: 'Alpha' }],
        rowKey: 'id',
        rowClickable: true,
      },
      slots: {
        'cell-name': '<button class="child-control">Child</button>',
      },
      global: { plugins: [i18n] },
    })
    const row = wrapper.get('tbody tr')

    await row.trigger('keydown', { key: 'Enter' })
    await row.trigger('keydown', { key: ' ' })
    expect(wrapper.emitted('row-click')).toHaveLength(2)

    await wrapper.get('.child-control').trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('row-click')).toHaveLength(2)
  })

  it('keeps single clicks inert for double-click-only rows', async () => {
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        rows: [{ id: 1, name: 'Alpha' }],
        rowKey: 'id',
        rowDblclickable: true,
      },
      slots: {
        'cell-name': '<button class="child-control">Child</button>',
      },
      global: { plugins: [i18n] },
    })
    const row = wrapper.get('tbody tr')

    await row.trigger('click')
    expect(wrapper.emitted('row-click')).toBeUndefined()
    expect(wrapper.emitted('row-dblclick')).toBeUndefined()

    await row.trigger('dblclick')
    await row.trigger('keydown', { key: 'Enter' })
    await row.trigger('keydown', { key: ' ' })
    expect(wrapper.emitted('row-dblclick')).toHaveLength(3)

    await wrapper.get('.child-control').trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('row-dblclick')).toHaveLength(3)
  })

  it('keeps mouse click hints separate from keyboard activation', async () => {
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        rows: [{ id: 1, name: 'Alpha' }],
        rowKey: 'id',
        rowClickable: true,
        rowDblclickable: true,
      },
      global: { plugins: [i18n] },
    })
    const row = wrapper.get('tbody tr')

    await row.trigger('click')
    expect(wrapper.emitted('row-click')).toHaveLength(1)

    await row.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('row-dblclick')).toHaveLength(1)
  })
})
