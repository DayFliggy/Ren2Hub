import { createApp, defineComponent, h, ref } from 'vue'
import ConsoleButton from '../src/components/common/ConsoleButton.vue'
import DataTable from '../src/components/common/DataTable.vue'
import EmptyState from '../src/components/common/EmptyState.vue'
import ErrorBanner from '../src/components/common/ErrorBanner.vue'
import HealthMeter from '../src/components/common/HealthMeter.vue'
import SkeletonBlock from '../src/components/common/SkeletonBlock.vue'
import StatusChip from '../src/components/common/StatusChip.vue'
import i18n, { loadMessageDomain } from '../src/i18n'
import '../src/styles/index.css'

// This test-only entry mounts real SFCs. It never modifies their implementation
// or fabricates Vue instance metadata for the coverage collector.
const scenario = new URLSearchParams(location.search).get('scenario')
if (!['visibility', 'nested'].includes(scenario ?? ''))
  throw new Error('Unknown coverage regression scenario')
document.documentElement.classList.add('light')
await loadMessageDomain('console')

const Harness = defineComponent({
  setup() {
    const inserted = ref(false)
    const rows = Array.from({ length: 30 }, (_, index) => ({
      id: index + 1,
      name: `Fixture row ${index + 1}`,
      last: index === 29,
    }))
    return () =>
      scenario === 'visibility'
        ? h('main', { style: 'padding:24px;width:500px;' }, [
            h(ConsoleButton, {}, () => 'Visible control'),
            h(HealthMeter, { value: 95, style: 'opacity:0;' }),
            h('section', { style: 'opacity:0;' }, [
              h(SkeletonBlock, { style: 'height:32px;width:120px;' }),
            ]),
            h(StatusChip, { class: 'sr-only' }, () => 'Screen-reader text'),
            h(
              'section',
              { style: 'width:100px;overflow:hidden;height:220px;' },
              [
                h(EmptyState, {
                  title: 'Horizontally clipped',
                  style: 'width:160px;margin-left:200px;',
                }),
              ]
            ),
            h('section', { style: 'clip-path:inset(50%);' }, [
              h(ErrorBanner, { message: 'Clipped to zero area' }),
            ]),
          ])
        : h('main', [
            inserted.value
              ? h('div', {
                  'data-inserted-before-scroll': '',
                  style: 'position:absolute;width:0;height:0;',
                })
              : null,
            h('header', { id: 'coverage-boundary', style: 'height:40px;' }, [
              h(ConsoleButton, {}, () => 'Visible control'),
            ]),
            h(
              'div',
              {
                id: 'coverage-outer',
                style: 'height:480px;width:600px;overflow:auto;',
                onScroll: () => {
                  inserted.value = true
                },
              },
              [
                h('div', { style: 'height:500px;' }, 'Outer scroll content'),
                h(
                  DataTable,
                  {
                    rows,
                    rowKey: 'id',
                    columns: [
                      { key: 'name', label: 'Name', width: '600px' },
                      { key: 'detail', label: 'Detail', width: '600px' },
                    ],
                    minTableWidth: '1200px',
                    adaptiveScroll: true,
                    pageSize: 30,
                    viewportBottomGap: 0,
                    scrollBoundarySelector: '#coverage-boundary',
                    scrollZoneSelector: '#coverage-outer',
                  },
                  {
                    'cell-name': ({ row }: { row: (typeof rows)[number] }) =>
                      row.last
                        ? h(HealthMeter, {
                            value: 95,
                            'data-tail-row': 'name',
                          })
                        : row.name,
                    'cell-detail': ({ row }: { row: (typeof rows)[number] }) =>
                      row.last
                        ? h(SkeletonBlock, {
                            'data-tail-row': 'detail',
                            style: 'height:32px;width:120px;',
                          })
                        : `Detail ${row.id}`,
                  }
                ),
              ]
            ),
          ])
  },
})
createApp(Harness).use(i18n).mount('#app')
