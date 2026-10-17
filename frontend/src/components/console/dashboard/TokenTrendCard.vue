<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useEChart } from '@/charts/useEChart'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import type { TotalTokenPoint } from '@/composables/useDashboard'
import { restoredConsoleMessages } from '@/i18n/restoredConsole'
const props = defineProps<{ points: TotalTokenPoint[]; loading?: boolean }>()
const { t: rootT } = useI18n({ useScope: 'global' })
const { t, locale } = useI18n({ messages: restoredConsoleMessages })
const el = ref<HTMLElement | null>(null)
const hasUsage = computed(() => props.points.some((point) => point.total > 0))
useEChart(
  el,
  (palette) => ({
    color: [palette.accent],
    textStyle: { color: palette.textSecondary },
    tooltip: {
      trigger: 'axis',
      renderMode: 'richText',
      backgroundColor: palette.surfaceSolid,
      borderColor: palette.borderSubtle,
      textStyle: { color: palette.textPrimary },
    },
    grid: { top: 24, right: 18, bottom: 32, left: 60 },
    xAxis: {
      type: 'category',
      data: props.points.map((point) => point.date),
      axisLine: { lineStyle: { color: palette.borderSubtle } },
      axisLabel: { color: palette.textTertiary },
    },
    yAxis: {
      type: 'value',
      min: 0,
      axisLabel: { color: palette.textTertiary },
      splitLine: { lineStyle: { color: palette.chartGrid } },
    },
    series: [
      {
        name: t('restored.tokenTotal'),
        type: 'line',
        showSymbol: false,
        data: props.points.map((point) => point.total),
      },
    ],
  }),
  [() => props.points, locale]
)
</script>
<template>
  <ConsoleCard :title="t('restored.tokenTrend')" :aria-busy="loading">
    <p class="text-xs leading-5 text-[var(--text-tertiary)]">
      {{ t('restored.tokenHint') }}
    </p>
    <div
      v-if="hasUsage"
      ref="el"
      class="mt-3 h-56 w-full"
      role="img"
      :aria-label="t('restored.tokenTotal')"
    />
    <p
      v-else
      class="flex min-h-56 items-center justify-center text-sm text-[var(--text-tertiary)]"
    >
      {{ loading ? rootT('common.loading') : t('restored.noUsage') }}
    </p>
    <details v-if="hasUsage" class="mt-2 text-xs">
      <summary class="cursor-pointer">{{ t('restored.tokenTotal') }}</summary>
      <div class="mt-2 max-h-48 overflow-auto">
        <table class="w-full text-left">
          <tbody>
            <tr v-for="point in points" :key="point.date">
              <th class="py-1 font-normal">{{ point.date }}</th>
              <td>{{ point.total.toLocaleString() }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </details>
  </ConsoleCard>
</template>
