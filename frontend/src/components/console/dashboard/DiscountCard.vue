<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import ConsoleCard from '@/components/common/ConsoleCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatTileGrid, {
  type StatTile,
} from '@/components/common/StatTileGrid.vue'
import type { UserDiscounts } from '@/composables/useDashboard'

const props = withDefaults(
  defineProps<{
    discounts: UserDiscounts | null
    loading?: boolean
  }>(),
  { loading: false }
)

const { t } = useI18n()

/**
 * Discount wording for one multiplier. `pct` feeds "12% off" copy and `zhe`
 * feeds 折 copy (0.88 → 8.8折), so each locale reads its own convention.
 * A ratio that rounds to no discount gets dedicated wording, never "0% off"
 * or "10折".
 */
function discountLabel(ratio: number, key: 'global' | 'effective'): string {
  const pct = Math.round((1 - ratio) * 1000) / 10
  if (pct <= 0) return t(`dashboard.discount.${key}None`)
  const zhe = Math.round(ratio * 100) / 10
  return t(`dashboard.discount.${key}Desc`, { pct, zhe })
}

/** Bar fill for the effective rate: 20% off already fills the track. */
const effectiveFill = computed(() => {
  if (!props.discounts) return 0
  const depth = (1 - props.discounts.effective_ratio) * 100
  return Math.min(100, Math.max(0, depth * 5))
})

const tiles = computed<StatTile[]>(() => {
  if (!props.discounts) return []
  return [
    {
      label: t('dashboard.discount.global'),
      value: `${props.discounts.global_ratio.toFixed(2)}×`,
      sub: discountLabel(props.discounts.global_ratio, 'global'),
    },
    {
      label: t('dashboard.discount.effectiveDiscount'),
      value: discountLabel(props.discounts.effective_ratio, 'effective'),
    },
  ]
})
</script>

<template>
  <ConsoleCard
    :title="t('dashboard.discount.title')"
    stretch
    data-discount-card
  >
    <div v-if="loading" class="flex grow flex-col justify-between gap-4">
      <div class="flex grow flex-col justify-center space-y-2">
        <SkeletonBlock rounded="md" class="h-4 w-20" />
        <SkeletonBlock rounded="lg" class="h-10 w-32" />
        <SkeletonBlock rounded="md" class="h-1.5 w-full" />
      </div>
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <SkeletonBlock v-for="i in 2" :key="i" bordered class="h-[4.5rem]" />
      </div>
    </div>

    <div v-else-if="discounts" class="flex grow flex-col justify-between gap-4">
      <!-- Headline: the effective rate, and the bar visualises that same rate. -->
      <div class="flex grow flex-col justify-center">
        <p class="text-xs text-[var(--text-tertiary)]">
          {{ t('dashboard.discount.effective') }}
        </p>
        <p
          class="display-number mt-1 text-3xl tabular-nums tracking-tight text-[var(--accent)] sm:text-4xl"
        >
          {{ discounts.effective_ratio.toFixed(3) }}×
        </p>
        <div
          class="pencil-progress mt-3 h-1.5 overflow-hidden rounded-full bg-[var(--surface-muted)]"
          role="img"
          :aria-label="discountLabel(discounts.effective_ratio, 'effective')"
          data-discount-effective-bar
        >
          <div
            class="h-full rounded-full bg-[var(--accent)] transition-[width]"
            :style="{ width: `${effectiveFill}%` }"
          />
        </div>
      </div>

      <!-- Breakdown: the platform-wide ratio next to the combined depth. -->
      <StatTileGrid :tiles="tiles" :columns="2" />
    </div>

    <!-- Contained in the card body: the card keeps its slot in the grid. -->
    <EmptyState
      v-else
      class="grow"
      :title="t('dashboard.discount.notConnected')"
      :hint="t('dashboard.discount.notConnectedHint')"
      data-discount-not-connected
    />
  </ConsoleCard>
</template>
