<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import KeyQuotaRing from '@/components/console/keys/KeyQuotaRing.vue'
import type { TokenSummary } from '@/types/console'
import { formatQuota } from '@/utils/format'
import { tokenMeter, tokenMeterCentre } from '@/utils/tokenMeter'

/**
 * Shared ring + remaining/used quota readout for both the desktop table cell
 * (compact) and the mobile KeyQuotaBar strip. Purely presentational — no
 * popover trigger or accordion toggle.
 */
const props = withDefaults(
  defineProps<{
    token: TokenSummary
    now: number
    compact?: boolean
  }>(),
  { compact: false }
)

const { t } = useI18n()
const meter = computed(() => tokenMeter(props.token, props.now))
const remainingText = computed(() =>
  props.token.unlimited
    ? t('common.unlimited')
    : formatQuota(props.token.remain_quota, 3)
)
const usedText = computed(() =>
  t('keys.meter.used', { value: formatQuota(props.token.used_quota, 3) })
)
</script>

<template>
  <div
    class="flex min-w-0 items-center"
    :class="compact ? 'gap-2.5' : 'gap-3'"
    data-key-quota-figures
    :data-token-id="token.id"
  >
    <span class="inline-flex shrink-0" data-key-quota-ring aria-hidden="true">
      <KeyQuotaRing
        :meter="meter"
        :unlimited="token.unlimited"
        :size="compact ? 28 : 36"
        :stroke-width="compact ? 3 : 3.5"
      >
        <span
          v-if="!compact"
          class="font-mono text-[0.625rem] font-semibold text-[var(--text-primary)]"
        >
          {{ tokenMeterCentre(meter) }}
        </span>
      </KeyQuotaRing>
    </span>
    <span class="min-w-0 flex-1">
      <span class="flex min-w-0 items-baseline">
        <span
          class="truncate font-mono font-semibold tabular-nums"
          :class="[
            compact ? 'text-xs leading-4' : 'text-sm leading-5',
            { 'line-through': meter.state === 'expired' },
          ]"
          :style="{ color: meter.remainingColor }"
          :title="remainingText"
          data-key-remaining
        >
          {{ remainingText }}
        </span>
        <span
          v-if="!compact && !token.unlimited"
          class="ml-1.5 shrink-0 text-[0.6875rem] text-[var(--text-secondary)]"
        >
          {{ t('keys.meter.available') }}
        </span>
      </span>
      <span
        class="mt-0.5 block truncate font-mono tabular-nums text-[var(--text-secondary)]"
        :class="compact ? 'text-[0.6875rem] leading-4' : 'text-xs leading-4'"
        :title="usedText"
        data-key-used
      >
        {{ usedText }}
      </span>
    </span>
  </div>
</template>
