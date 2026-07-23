<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { SERIES_TOKENS } from '@/charts/palette'
import { useEChart } from '@/charts/useEChart'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import type { ModelShare } from '@/composables/useDashboard'
import { formatQuota } from '@/utils/format'

const props = defineProps<{
  items: ModelShare[]
}>()

const { t } = useI18n()
const el = ref<HTMLElement | null>(null)
// CSS-var strings so legend swatches re-resolve on theme switch.
const colors = SERIES_TOKENS

useEChart(
  el,
  (p) => ({
    color: p.series,
    tooltip: {
      trigger: 'item',
      backgroundColor: p.surfaceSolid,
      borderColor: p.borderSubtle,
      textStyle: { color: p.textPrimary, fontSize: 12 },
      formatter: (params: { name: string; percent: number }) =>
        `${params.name} · ${params.percent}%`,
    },
    series: [
      {
        type: 'pie',
        radius: ['62%', '82%'],
        center: ['50%', '50%'],
        avoidLabelOverlap: true,
        itemStyle: {
          borderColor: p.surfaceSolid,
          borderWidth: 3,
          borderRadius: 6,
        },
        label: { show: false },
        emphasis: { scaleSize: 6 },
        data: props.items.map((m) => ({ name: m.model, value: m.ratio })),
      },
    ],
    graphic: [
      {
        type: 'text',
        left: 'center',
        top: '44%',
        style: {
          text: '100%',
          fontSize: 22,
          fontWeight: 700,
          fill: p.textPrimary,
          textAlign: 'center',
        },
      },
      {
        type: 'text',
        left: 'center',
        top: '55%',
        style: {
          text: t('dashboard.recorded'),
          fontSize: 11,
          fill: p.textTertiary,
          textAlign: 'center',
        },
      },
    ],
  }),
  () => props.items
)
</script>

<template>
  <ConsoleCard :title="t('dashboard.modelShare')">
    <div class="grid items-center gap-4 sm:grid-cols-[200px_1fr]">
      <div
        ref="el"
        class="h-44 w-full"
        role="img"
        :aria-label="t('dashboard.modelShare')"
      />
      <ul class="space-y-3">
        <li
          v-for="(m, i) in items"
          :key="m.model"
          class="flex items-center justify-between gap-3 text-sm"
        >
          <span class="flex min-w-0 items-center gap-2.5">
            <span
              class="h-2.5 w-2.5 shrink-0 rounded-sm"
              :style="{ background: colors[i % colors.length] }"
            />
            <span class="truncate text-[var(--text-secondary)]">
              {{ m.model }}（{{ m.ratio }}%）
            </span>
          </span>
          <span class="shrink-0 font-semibold text-[var(--text-primary)]">{{
            formatQuota(m.quota)
          }}</span>
        </li>
      </ul>
    </div>
  </ConsoleCard>
</template>
