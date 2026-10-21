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
// The user catalog carries the configured limit only; live occupancy is admin data.
const capacityLabel = computed(() =>
  t('dashboard.autoRoute.channelLimit', {
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
            <Users :size="12" aria-hidden="true" />{{
              entry.max_concurrency || '∞'
            }}
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
        <p class="text-[0.625rem] font-medium text-[var(--text-tertiary)]">
          {{
            entry.eligible
              ? t('dashboard.autoRoute.catalogEligible')
              : exclusionReason
          }}
        </p>
      </template>
    </div>
    <Gauge
      :size="compact ? 28 : 32"
      class="shrink-0 text-[var(--text-tertiary)]"
      :aria-label="capacityLabel"
    />
  </div>
</template>
