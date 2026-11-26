<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import IconButton from '@/components/common/IconButton.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { useBalanceVisibility } from '@/composables/useDashboard'
import { quotaRatio } from '@/utils/quota'
import { formatQuota, formatQuotaParts } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    quota: string | null
    usedQuota?: string | null
    todayQuota?: string
    monthQuota?: string
    /** rolling daily burn used for the runway estimate */
    dailyBurn?: string
    compact?: boolean
  }>(),
  {
    usedQuota: '0',
    todayQuota: undefined,
    monthQuota: undefined,
    dailyBurn: undefined,
    compact: false,
  }
)

const { t } = useI18n()
const router = useRouter()
const { hidden, toggle } = useBalanceVisibility()

const balanceParts = computed(() =>
  props.quota === null || hidden.value ? null : formatQuotaParts(props.quota, 2)
)
const fallbackDisplay = computed(() =>
  props.quota === null ? '--' : '••••••••'
)

// This display denominator may exceed one account field's int64 range.
const usedQuota = computed(() => props.usedQuota ?? '0')
const totalBalance = computed(
  () => BigInt(props.quota ?? '0') + BigInt(usedQuota.value)
)
const usedPercent = computed(() =>
  BigInt(totalBalance.value) > 0n
    ? Math.min(
        100,
        Math.round(quotaRatio(usedQuota.value, totalBalance.value) * 100)
      )
    : 0
)
const meterColor = computed(() => {
  if (usedPercent.value >= 90) return 'var(--status-danger)'
  if (usedPercent.value >= 70) return 'var(--status-warning)'
  return 'var(--studio-balance-meter-normal)'
})

/** Remaining share of the balance drives the health indicator. */
const remainPercent = computed(() => 100 - usedPercent.value)
const health = computed(() => {
  if (props.quota === null) {
    return { tone: 'neutral' as const, label: t('dashboard.unknown') }
  }
  if (remainPercent.value < 10) {
    return {
      tone: 'danger' as const,
      label: t('dashboard.balanceHint.critical'),
    }
  }
  if (remainPercent.value < 30) {
    return { tone: 'warning' as const, label: t('dashboard.balanceHint.low') }
  }
  return { tone: 'success' as const, label: t('dashboard.balanceHint.healthy') }
})

/** Runway in whole days; null when there is nothing to extrapolate from. */
const runwayDays = computed(() => {
  const burn = BigInt(props.dailyBurn ?? props.todayQuota ?? '0')
  if (burn <= 0n || props.quota === null || BigInt(props.quota) <= 0n)
    return null
  return (BigInt(props.quota) / burn).toString()
})

const monthSpendDisplay = computed(() => {
  return props.monthQuota === undefined ? '--' : formatQuota(props.monthQuota)
})
</script>

<template>
  <ConsoleCard variant="sketch" stretch data-balance-card>
    <!-- Top row: label + health indicator -->
    <div class="flex items-center justify-between gap-3">
      <p class="text-sm text-[var(--text-tertiary)]">
        {{ t('dashboard.totalBalance') }}
      </p>
      <StatusChip v-if="!compact" :tone="health.tone">
        {{ health.label }}
      </StatusChip>
    </div>

    <!-- Balance number: mono font for ledger feel; weight is theme-driven -->
    <div class="mt-3 flex items-center gap-2.5">
      <p
        class="display-number font-mono tracking-tight text-[var(--text-primary)]"
      >
        <template v-if="balanceParts"
          ><span
            v-if="balanceParts.prefix"
            class="text-xl text-[var(--text-tertiary)]"
            >{{ balanceParts.prefix }}</span
          ><span class="text-3xl">{{ balanceParts.whole }}</span
          ><span
            v-if="balanceParts.fraction"
            class="text-xl text-[var(--text-secondary)]"
            >.{{ balanceParts.fraction }}</span
          ></template
        >
        <span v-else class="text-3xl">{{ fallbackDisplay }}</span>
      </p>
      <IconButton
        :label="hidden ? t('common.showBalance') : t('common.hideBalance')"
        @click="toggle"
      >
        <!-- eye-off -->
        <svg
          v-if="hidden"
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <path
            d="M3 3l18 18M10.5 10.7a3 3 0 0 0 4.2 4.2M7.4 7.6C4.8 9.3 3 12 3 12s3.5 6 9 6c1.6 0 3-.4 4.3-1M12 6c5.5 0 9 6 9 6s-.6 1.1-1.8 2.3"
          />
        </svg>
        <!-- eye -->
        <svg
          v-else
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <path d="M3 12s3.5-6 9-6 9 6 9 6-3.5 6-9 6-9-6-9-6Z" />
          <circle cx="12" cy="12" r="3" />
        </svg>
      </IconButton>
    </div>

    <!-- Runway estimate -->
    <p v-if="!compact" class="mt-1.5 text-xs text-[var(--text-tertiary)]">
      <template v-if="runwayDays !== null">
        {{ t('dashboard.balanceHint.runway', { days: runwayDays }) }}
      </template>
      <template v-else>
        {{ t('dashboard.balanceHint.runwayUnknown') }}
      </template>
    </p>

    <!-- Usage bar: hand-drawn brush-stroke style -->
    <div
      v-if="BigInt(usedQuota) > 0n && !compact"
      class="mt-4"
      data-balance-usage-meter
    >
      <div
        class="mb-1.5 flex items-baseline justify-between gap-3 text-xs text-[var(--text-tertiary)]"
      >
        <span>
          {{ t('dashboard.usedQuota') }}
          <span
            class="font-mono font-medium tabular-nums text-[var(--text-secondary)]"
            >{{ formatQuota(usedQuota) }}</span
          >
        </span>
        <span class="font-mono tabular-nums">{{ usedPercent }}%</span>
      </div>
      <!-- Thicker bar with irregular radius for brush-stroke feel -->
      <div
        class="h-1.5 overflow-hidden bg-[var(--surface-muted)]"
        style="border-radius: 3px 2px 4px 2px / 2px 3px 2px 4px"
      >
        <div
          class="h-full transition-[width] duration-700"
          style="border-radius: 3px 1px 3px 2px / 2px 2px 3px 1px"
          :style="{ width: `${usedPercent}%`, background: meterColor }"
        />
      </div>
    </div>

    <!-- Month vs. average spend grid -->
    <div
      v-if="
        !compact &&
        (monthQuota !== undefined ||
          todayQuota !== undefined ||
          dailyBurn !== undefined)
      "
      class="grid grow content-center grid-cols-2 gap-3 py-2.5"
      data-balance-spend-summary
    >
      <div
        class="px-3.5 py-2.5"
        data-balance-spend-tile
        style="
          background: var(--wallet-tile-surface);
          border-radius: var(--sketch-border-radius-sm);
        "
      >
        <p class="text-[0.6875rem] text-[var(--text-secondary)]">
          {{ t('dashboard.monthSpend') }}
        </p>
        <p
          class="display-number mt-1 font-mono text-base text-[var(--text-primary)]"
        >
          {{ monthSpendDisplay }}
        </p>
      </div>
      <div
        class="px-3.5 py-2.5"
        data-balance-spend-tile
        style="
          background: var(--wallet-tile-surface);
          border-radius: var(--sketch-border-radius-sm);
        "
      >
        <p class="text-[0.6875rem] text-[var(--text-secondary)]">
          {{ t('dashboard.balanceHint.avgBurn') }}
        </p>
        <p
          class="display-number mt-1 font-mono text-base text-[var(--text-primary)]"
        >
          {{ dailyBurn === undefined ? '--' : formatQuota(dailyBurn) }}
        </p>
      </div>
    </div>

    <!-- Action buttons -->
    <div class="grid grid-cols-2 gap-3 pt-3" data-balance-actions>
      <ConsoleButton @click="router.push({ name: 'wallet' })">
        <svg
          width="15"
          height="15"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <rect x="3" y="6" width="18" height="13" rx="2" />
          <path d="M3 10h18M8 15h4" />
        </svg>
        {{ t('dashboard.recharge') }}
      </ConsoleButton>
      <ConsoleButton
        variant="secondary"
        @click="router.push({ name: 'wallet', query: { panel: 'redeem' } })"
      >
        <svg
          width="15"
          height="15"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path
            d="M4 9a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v2a2 2 0 0 0 0 4v2a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-2a2 2 0 0 0 0-4V9Z"
          />
          <path d="M13 8v8" stroke-opacity="0.65" />
        </svg>
        {{ t('dashboard.redeem') }}
      </ConsoleButton>
    </div>
  </ConsoleCard>
</template>

<style scoped>
html.dark [data-balance-card] {
  position: relative;
  overflow: hidden;
  background: var(--studio-balance-bg) !important;
  border: 1px solid var(--studio-balance-border) !important;
  border-radius: var(--shape-card, 16px) !important;
  box-shadow: var(--elevation-2) !important;
}

html.dark [data-balance-card]::before {
  content: '';
  position: absolute;
  inset: 0 0 auto;
  height: 1px;
  background: var(--studio-balance-top-line);
  pointer-events: none;
}

html.dark [data-balance-card] .display-number {
  font-weight: var(--display-number-weight, 500);
  letter-spacing: -0.025em;
}

html.dark [data-balance-spend-tile] {
  background: var(--wallet-tile-surface) !important;
  border: 1px solid var(--outline-variant) !important;
  border-radius: var(--shape-control, 10px) !important;
  box-shadow: none !important;
}

html.dark [data-balance-usage-meter] > div:last-child {
  border-radius: 9999px !important;
  background: var(--surface-container-low) !important;
  border: 1px solid var(--outline-variant);
  box-shadow: none;
}

html.dark [data-balance-usage-meter] > div:last-child > div {
  border-radius: 9999px !important;
}

html.dark [data-balance-actions] > :deep(button:first-child) {
  background: var(--studio-balance-cta-bg) !important;
  color: var(--studio-balance-cta-text) !important;
  border: 1px solid var(--studio-balance-cta-border) !important;
  font-weight: 500;
  box-shadow: inset 0 1px 0 rgba(255, 237, 195, 0.1) !important;
}

html.dark [data-balance-actions] > :deep(button:first-child:enabled:hover) {
  background: var(--studio-balance-cta-hover) !important;
  border-color: var(--border-strong) !important;
}

html.dark [data-balance-actions] > :deep(button:first-child:enabled:active) {
  background: var(--studio-balance-cta-active) !important;
  box-shadow: inset 0 1px 2px rgba(0, 0, 0, 0.35) !important;
}

html.dark [data-balance-actions] > :deep(button:last-child) {
  font-weight: 500;
}
</style>
