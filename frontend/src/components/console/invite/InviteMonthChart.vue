<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useEChart } from '@/charts/useEChart'
import { lineMood, tooltipStyle } from '@/charts/themePreset'
import type { AffiliateMonth } from '@/api/affiliate'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import { escapeHtml } from '@/utils/html'

const props = defineProps<{
  series: Pick<AffiliateMonth, 'month' | 'new_count' | 'cumulative'>[]
}>()

const { t } = useI18n()
const el = ref<HTMLElement | null>(null)

type Mode = 'new' | 'cumulative'
const mode = ref<Mode>('new')
const modeOptions = computed(() => [
  { value: 'new', label: t('invite.chartTabNew') },
  { value: 'cumulative', label: t('invite.chartTabCumulative') },
])

const categories = computed(() => props.series.map((p) => p.month))
const values = computed(() =>
  props.series.map((p) => (mode.value === 'new' ? p.new_count : p.cumulative))
)

const totalNew = computed(() =>
  props.series.reduce((s, p) => s + p.new_count, 0)
)

useEChart(
  el,
  (p, scale = 1) => ({
    grid: {
      left: 32 * scale,
      right: 12 * scale,
      top: 16 * scale,
      bottom: 26 * scale,
    },
    tooltip: {
      trigger: 'axis',
      ...tooltipStyle(p, scale),
      formatter: (params: { name: string; value: number }[]) => {
        const item = params[0]
        if (!item) return ''
        const label =
          mode.value === 'new'
            ? t('invite.chartTabNew')
            : t('invite.chartTabCumulative')
        return `${escapeHtml(item.name)} · ${escapeHtml(label)} ${escapeHtml(t('invite.people', { count: item.value }))}`
      },
    },
    xAxis: {
      type: 'category',
      data: categories.value,
      axisLine: { lineStyle: { color: p.borderSubtle } },
      axisTick: { show: false },
      axisLabel: { color: p.textTertiary, fontSize: 11 * scale },
    },
    yAxis: {
      type: 'value',
      minInterval: 1,
      splitLine: lineMood(p).splitLine,
      axisLabel: { color: p.textTertiary, fontSize: 10 * scale },
    },
    series: [
      {
        type: 'bar',
        data: values.value,
        barMaxWidth: 40 * scale,
        itemStyle: {
          color: p.accent,
          borderRadius: lineMood(p).barRadius.map((radius) => radius * scale),
        },
        label: {
          show: true,
          position: 'top',
          color: p.textSecondary,
          fontSize: 11 * scale,
          formatter: (o: { value: number }) =>
            o.value > 0 ? String(o.value) : '',
        },
      },
    ],
  }),
  [categories, values]
)
</script>

<template>
  <ConsoleCard :title="t('invite.chartTitle')">
    <template #action>
      <SegmentedToggle
        v-model="mode"
        :options="modeOptions"
        :label="t('invite.chartTitle')"
        size="sm"
      />
    </template>

    <p
      class="display-number text-2xl tracking-tight text-[var(--text-primary)]"
    >
      {{ t('invite.people', { count: totalNew }) }}
    </p>
    <p class="mt-0.5 text-xs text-[var(--text-tertiary)]">
      {{ t('invite.chartSubtitle', { count: series.length }) }}
    </p>
    <div
      ref="el"
      class="mt-3 h-48 w-full"
      role="img"
      :aria-label="t('invite.chartTitle')"
    />
  </ConsoleCard>
</template>
