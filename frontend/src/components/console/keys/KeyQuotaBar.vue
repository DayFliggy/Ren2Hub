<script setup lang="ts">
import { computed } from 'vue'
import { ChevronDown } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import KeyQuotaDetailList from '@/components/console/keys/KeyQuotaDetailList.vue'
import KeyQuotaRing from '@/components/console/keys/KeyQuotaRing.vue'
import type { TokenSummary } from '@/types/console'
import { formatQuota } from '@/utils/format'
import { tokenMeter, tokenMeterCentre } from '@/utils/tokenMeter'

/**
 * A card's quota strip for touch layouts, where nothing can be hovered: ring
 * plus the balance figures, tinted by state, expanding the rest of the detail
 * inside the card instead of a floating popover that would cover its buttons.
 */
const props = defineProps<{
  token: TokenSummary
  now: number
  expanded: boolean
  detailId: string
}>()
const emit = defineEmits<{ toggle: [] }>()

const { t } = useI18n()
const meter = computed(() => tokenMeter(props.token, props.now))

// Same urgency as the ring colour: blocked keys stay neutral.
const tone = computed(() => {
  if (meter.value.state === 'exhausted') return 'danger'
  if (meter.value.state === 'low') return 'warning'
  if (meter.value.expiresInDays !== null) return 'warning'
  return 'quiet'
})
</script>

<template>
  <div class="key-quota-bar no-handdrawn rounded-xl border" :data-tone="tone">
    <button
      type="button"
      class="flex min-h-14 w-full items-center gap-3 px-3 py-2.5 text-left transition-colors hover:bg-[var(--state-hover-layer)] focus-ring"
      :class="
        expanded
          ? 'rounded-t-[calc(0.75rem-1px)]'
          : 'rounded-[calc(0.75rem-1px)]'
      "
      data-key-quota-ring
      :data-token-id="token.id"
      :aria-expanded="expanded"
      :aria-controls="detailId"
      @click="emit('toggle')"
    >
      <span class="inline-flex shrink-0" aria-hidden="true">
        <KeyQuotaRing
          :meter="meter"
          :unlimited="token.unlimited"
          :size="36"
          :stroke-width="3.5"
        >
          <span
            class="font-mono text-[0.625rem] font-semibold text-[var(--text-primary)]"
          >
            {{ tokenMeterCentre(meter) }}
          </span>
        </KeyQuotaRing>
      </span>
      <span class="min-w-0 flex-1">
        <span class="flex items-baseline">
          <span
            class="font-mono text-sm leading-5 font-semibold"
            :class="{ 'line-through': meter.state === 'expired' }"
            :style="{ color: meter.remainingColor }"
            data-key-remaining
          >
            {{
              token.unlimited
                ? t('common.unlimited')
                : formatQuota(token.remain_quota, 3)
            }}
          </span>
          <span
            v-if="!token.unlimited"
            class="ml-1.5 text-[0.6875rem] text-[var(--text-secondary)]"
          >
            {{ t('keys.meter.available') }}
          </span>
        </span>
        <span
          class="mt-0.5 block font-mono text-xs leading-4 text-[var(--text-secondary)]"
        >
          {{
            t('keys.meter.used', { value: formatQuota(token.used_quota, 3) })
          }}
        </span>
      </span>
      <ChevronDown
        :size="16"
        class="key-quota-bar-chevron shrink-0 text-[var(--text-tertiary)]"
        :class="{ 'is-open': expanded }"
        aria-hidden="true"
      />
    </button>
    <div
      v-if="expanded"
      :id="detailId"
      role="region"
      :aria-label="t('keys.meter.detailLabel', { name: token.name })"
      class="key-quota-bar-detail border-t border-[var(--border-subtle)] px-3 pt-2.5 pb-3"
    >
      <KeyQuotaDetailList :token="token" :meter="meter" :with-balance="false" />
    </div>
  </div>
</template>

<style scoped>
/* The button's corners follow the frame's inner edge (radius minus border);
   expanded, its bottom stays square against the detail's divider. A clipping
   frame would also clip the focus outline, so the radii match instead. */

/* A quiet bar is outline only, so the two tinted states stand out from it. */
.key-quota-bar {
  border-color: var(--border-default);
  background: transparent;
}
.key-quota-bar[data-tone='warning'] {
  border-color: color-mix(in srgb, var(--status-warning-text) 25%, transparent);
  background: var(--status-warning-soft);
}
.key-quota-bar[data-tone='danger'] {
  border-color: color-mix(in srgb, var(--status-danger-text) 25%, transparent);
  background: var(--status-danger-soft);
}
.key-quota-bar-chevron {
  transition: transform 150ms ease-out;
}
.key-quota-bar-chevron.is-open {
  transform: rotate(180deg);
}
.key-quota-bar-detail {
  animation: key-quota-detail-in 150ms ease-out;
}
@keyframes key-quota-detail-in {
  from {
    opacity: 0;
  }
}
@media (prefers-reduced-motion: reduce) {
  .key-quota-bar-chevron {
    transition: none;
  }
  .key-quota-bar-detail {
    animation: none;
  }
}
</style>
