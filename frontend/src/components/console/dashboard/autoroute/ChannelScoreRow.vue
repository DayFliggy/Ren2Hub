<script setup lang="ts">
import { computed } from 'vue'
import {
  CircleCheck,
  CircleDollarSign,
  CircleOff,
  Gauge,
  Users,
} from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import MiniRing from '@/components/console/dashboard/MiniRing.vue'
import type { CatalogChannel } from '@/api/modelCatalog'
import { tokenRoutingMessages } from '@/i18n/tokenRouting'

const props = withDefaults(
  defineProps<{ entry: CatalogChannel; compact?: boolean }>(),
  { compact: false }
)
const { t } = useI18n()
const { t: routingText, te: hasRoutingText } = useI18n({
  useScope: 'local',
  messages: tokenRoutingMessages,
})
const exclusionReason = computed(() => {
  const key = `routing.reasons.${props.entry.filter_reason}`
  return hasRoutingText(key)
    ? routingText(key)
    : t('dashboard.autoRoute.excluded')
})
const occupancy = computed(() =>
  props.entry.max_concurrency > 0
    ? Math.min(100, (props.entry.in_flight / props.entry.max_concurrency) * 100)
    : null
)
const capacityLabel = computed(() =>
  t('dashboard.autoRoute.channelOccupancy', {
    active: props.entry.in_flight,
    limit: props.entry.max_concurrency || '∞',
  })
)
</script>

<template>
  <div
    class="flex items-center gap-2 first:pt-0 sm:gap-3"
    :class="compact ? 'py-2' : 'py-3'"
    data-route-channel
    :data-route-channel-compact="compact ? '' : undefined"
  >
    <span
      class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full"
      :class="
        entry.eligible
          ? 'bg-[var(--status-success-soft)] text-[var(--status-success-text)]'
          : 'bg-[var(--status-danger-soft)] text-[var(--status-danger-text)]'
      "
    >
      <component
        :is="entry.eligible ? CircleCheck : CircleOff"
        :size="13"
        aria-hidden="true"
      />
    </span>
    <div class="min-w-0 flex-1" :class="{ 'sibling-space-y-2': !compact }">
      <div class="flex min-w-0 flex-wrap items-center justify-between gap-2">
        <div class="min-w-0">
          <p
            class="truncate text-sm font-medium text-[var(--text-primary)]"
            :title="entry.name"
          >
            {{ entry.name }}
          </p>
          <p
            class="truncate text-[0.625rem] leading-tight text-[var(--text-tertiary)]"
          >
            {{ entry.lab.name }}
          </p>
        </div>
        <span class="flex shrink-0 items-center gap-2">
          <span
            class="flex items-center gap-1 text-[0.6875rem] tabular-nums text-[var(--text-tertiary)] sm:text-xs"
            :title="t('dashboard.autoRoute.concurrency')"
          >
            <Users :size="12" aria-hidden="true" />{{ entry.in_flight }} /
            {{ entry.max_concurrency || '∞' }}
          </span>
          <span
            class="flex items-center gap-1 font-mono text-[0.6875rem] text-[var(--text-tertiary)] sm:text-xs"
            :title="t('channels.channelRatio')"
            data-route-ratio
          >
            <CircleDollarSign :size="12" aria-hidden="true" />×{{
              entry.channel_ratio
            }}
          </span>
        </span>
      </div>
      <template v-if="!compact">
        <div
          class="pencil-progress h-1.5 w-full overflow-hidden rounded-full bg-[var(--surface-muted)]"
          role="img"
          :aria-label="capacityLabel"
        >
          <div
            v-if="occupancy !== null"
            class="h-full bg-[var(--accent)]"
            :style="{ width: `${occupancy}%` }"
          />
        </div>
        <p class="text-[0.625rem] font-medium text-[var(--text-tertiary)]">
          {{
            entry.eligible
              ? t('dashboard.autoRoute.catalogEligible')
              : exclusionReason
          }}
        </p>
      </template>
    </div>
    <MiniRing
      v-if="occupancy !== null"
      :percent="occupancy"
      color="var(--accent)"
      :size="compact ? 32 : 40"
      :aria-label="capacityLabel"
    >
      <span
        class="font-bold tabular-nums text-[var(--accent-text)]"
        :class="compact ? 'text-[0.6875rem]' : 'text-xs'"
        >{{ Math.round(occupancy) }}%</span
      >
    </MiniRing>
    <Gauge
      v-else
      :size="compact ? 28 : 32"
      class="shrink-0 text-[var(--text-tertiary)]"
      :aria-label="capacityLabel"
    />
  </div>
</template>
