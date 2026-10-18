<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useEChart } from '@/charts/useEChart'
import { lineMood } from '@/charts/themePreset'
import type { InviteMonthPoint } from '@/types/console'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import { formatQuota } from '@/utils/format'
import { escapeHtml } from '@/utils/html'

const props = defineProps<{
  series: InviteMonthPoint[]
}>()

const { t } = useI18n()
const el = ref<HTMLElement | null>(null)

type Mode = 'new' | 'cumulative'
const mode = ref<Mode>('new')

const categories = computed(() => props.series.map((p) => p.month))
const values = computed(() =>
  props.series.map((p, index) =>
    mode.value === 'new'
      ? p.reward
      : props.series
          .slice(0, index + 1)
          .reduce((sum, item) => sum + item.reward, 0)
  )
)

const totalNew = computed(() => props.series.reduce((s, p) => s + p.reward, 0))

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
      backgroundColor: p.surfaceSolid,
      borderColor: p.borderSubtle,
      textStyle: { color: p.textPrimary, fontSize: 12 * scale },
      formatter: (params: { name: string; value: number }[]) => {
        const item = params[0]
        if (!item) return ''
        const label =
          mode.value === 'new'
            ? t('invite.chartTabNew')
            : t('invite.chartTabCumulative')
        return `${escapeHtml(item.name)} · ${escapeHtml(label)} ${escapeHtml(formatQuota(item.value))}`
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
      axisLabel: {
        color: p.textTertiary,
        fontSize: 10 * scale,
        formatter: (value: number) => formatQuota(value),
      },
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
            o.value > 0 ? formatQuota(o.value) : '',
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
      <div class="flex gap-1 rounded-lg bg-[var(--surface-muted)] p-0.5">
        <button
          v-for="m in ['new', 'cumulative'] as Mode[]"
          :key="m"
          type="button"
          class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
          :style="
            mode === m
              ? 'background:var(--surface-solid);color:var(--text-primary)'
              : 'color:var(--text-tertiary)'
          "
          @click="mode = m"
        >
          {{
            m === 'new'
              ? t('invite.chartTabNew')
              : t('invite.chartTabCumulative')
          }}
        </button>
      </div>
    </template>

    <p
      class="display-number text-2xl tracking-tight text-[var(--text-primary)]"
    >
      {{ formatQuota(totalNew) }}
    </p>
    <p class="mt-0.5 text-xs text-[var(--text-tertiary)]">
      {{ t('invite.chartSubtitle') }}
    </p>
    <div
      ref="el"
      class="mt-3 h-48 w-full"
      role="img"
      :aria-label="t('invite.chartTitle')"
    />
  </ConsoleCard>
</template>
