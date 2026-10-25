import { createApp, defineComponent, h, ref } from 'vue'
import HealthMeter from '../src/components/common/HealthMeter.vue'
import MultiFilterSelect from '../src/components/common/MultiFilterSelect.vue'
import ActivityEntryCard from '../src/components/console/ActivityEntryCard.vue'
import i18n, { loadMessageDomain } from '../src/i18n'
import '../src/styles/index.css'

// This HTML entry is served by the test Vite server; it is not a product route
// or a production build input. These components currently have no business entry.
const component = new URLSearchParams(location.search).get('component')
if (!['health', 'multi-filter', 'activity-entry'].includes(component ?? ''))
  throw new Error('Unknown component regression scenario')
const theme =
  localStorage.getItem('ren2hub_theme_mode') === 'dark' ? 'dark' : 'light'
document.documentElement.classList.add(theme)
await loadMessageDomain('console')

const Harness = defineComponent({
  setup() {
    const health = ref(98)
    const selected = ref<string[]>([])
    const entered = ref(0)
    const english = i18n.global.locale.value === 'en'
    const title = english ? 'Component interaction fixture' : '组件交互夹具'
    return () =>
      h(
        'main',
        {
          'data-component-harness': component,
          style:
            'max-width:600px;margin:32px auto;padding:20px;color:var(--text-primary);',
        },
        [
          h('h1', { style: 'font-size:20px;margin-bottom:24px;' }, title),
          component === 'health'
            ? h('section', { style: 'display:grid;gap:20px;' }, [
                h('label', [
                  english ? 'Fixture health value' : '夹具健康值',
                  h('input', {
                    type: 'number',
                    min: 0,
                    max: 100,
                    value: health.value,
                    class: 'console-input',
                    'aria-label': 'Fixture health value',
                    onInput: (event: Event) => {
                      health.value = Number(
                        (event.target as HTMLInputElement).value
                      )
                    },
                  }),
                ]),
                h(HealthMeter, { value: health.value }),
                h(HealthMeter, { value: health.value, compact: true, bars: 8 }),
              ])
            : null,
          component === 'multi-filter'
            ? h(
                'section',
                {
                  style:
                    'min-height:360px;display:grid;gap:24px;align-content:start;',
                },
                [
                  h(MultiFilterSelect, {
                    modelValue: selected.value,
                    'onUpdate:modelValue': (value: string[]) => {
                      selected.value = value
                    },
                    options: [
                      { value: 'ready', label: english ? 'Ready' : '可用' },
                      { value: 'pending', label: english ? 'Pending' : '等待' },
                      {
                        value: 'disabled',
                        label: english ? 'Disabled' : '停用',
                      },
                    ],
                    label: 'Fixture status filter',
                    placeholder: english ? 'All states' : '全部状态',
                    prefixLabel: english ? 'States' : '状态',
                    clearLabel: english ? 'Clear' : '清除',
                  }),
                  h(
                    'output',
                    { 'aria-label': 'Selected values' },
                    selected.value.join(',')
                  ),
                  h(
                    'button',
                    { type: 'button', style: 'margin-top:220px;' },
                    'Outside control'
                  ),
                ]
              )
            : null,
          component === 'activity-entry'
            ? h('section', { style: 'display:grid;gap:24px;' }, [
                ...[false, true].map((disabled) =>
                  h(ActivityEntryCard, {
                    title: english ? 'Synthetic activity' : '合成活动',
                    subtitle: english
                      ? 'Existing component rendered without restoring a retired feature.'
                      : '只渲染既有组件，不恢复退役业务。',
                    tag: english ? 'Fixture' : '夹具',
                    emoji: '✦',
                    image: 'data:image/png;base64,AA==',
                    gradient: 'linear-gradient(120deg, #d4a373, #f4e5c1)',
                    cta: disabled
                      ? english
                        ? 'Unavailable'
                        : '不可用'
                      : english
                        ? 'Enter'
                        : '进入',
                    disabled,
                    onEnter: () => {
                      entered.value++
                    },
                  })
                ),
                h(
                  'output',
                  { 'aria-label': 'Enter event count' },
                  String(entered.value)
                ),
              ])
            : null,
        ]
      )
  },
})
createApp(Harness).use(i18n).mount('#app')
