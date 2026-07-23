<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import { useBalanceVisibility } from '@/composables/useDashboard'
import { formatQuota } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    quota: number
    delta?: number
    compact?: boolean
  }>(),
  { delta: undefined, compact: false }
)

const { t } = useI18n()
const router = useRouter()
const { hidden, toggle } = useBalanceVisibility()

const display = computed(() =>
  hidden.value ? '********' : formatQuota(props.quota)
)
</script>

<template>
  <ConsoleCard>
    <div class="flex items-center justify-between">
      <p class="text-sm text-[var(--text-tertiary)]">
        {{ t('dashboard.totalBalance') }}
      </p>
      <span
        class="rounded-lg bg-[var(--surface-muted)] px-2 py-1 font-mono text-xs text-[var(--text-secondary)]"
      >
        sk-•••• 0918
      </span>
    </div>

    <div class="mt-3 flex items-center gap-2.5">
      <p class="text-3xl font-bold tracking-tight text-[var(--text-primary)]">
        {{ display }}
      </p>
      <button
        type="button"
        class="text-[var(--text-tertiary)] transition-colors hover:text-[var(--text-primary)] focus-ring"
        :aria-label="hidden ? t('common.showBalance') : t('common.hideBalance')"
        @click="toggle"
      >
        <svg
          v-if="hidden"
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path
            d="M3 3l18 18M10.5 10.7a3 3 0 0 0 4.2 4.2M7.4 7.6C4.8 9.3 3 12 3 12s3.5 6 9 6c1.6 0 3-.4 4.3-1M12 6c5.5 0 9 6 9 6s-.6 1.1-1.8 2.3"
          />
        </svg>
        <svg
          v-else
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path d="M3 12s3.5-6 9-6 9 6 9 6-3.5 6-9 6-9-6-9-6Z" />
          <circle cx="12" cy="12" r="3" />
        </svg>
      </button>
      <span
        v-if="delta !== undefined && !compact"
        class="rounded-md px-1.5 py-0.5 text-xs font-semibold"
        :style="
          delta >= 0
            ? 'background:var(--status-danger-soft);color:var(--status-danger-text)'
            : 'background:var(--status-success-soft);color:var(--status-success-text)'
        "
      >
        {{ delta >= 0 ? '↗' : '↘' }} {{ delta > 0 ? '+' : '' }}{{ delta }}%
      </span>
    </div>

    <div class="mt-4 grid grid-cols-2 gap-3">
      <ConsoleButton @click="router.push({ name: 'wallet' })">
        <svg
          width="15"
          height="15"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
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
        >
          <path
            d="M4 9a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v2a2 2 0 0 0 0 4v2a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-2a2 2 0 0 0 0-4V9Z"
          />
          <path d="M13 7v12" stroke-dasharray="3 3" />
        </svg>
        {{ t('dashboard.redeem') }}
      </ConsoleButton>
    </div>
  </ConsoleCard>
</template>
