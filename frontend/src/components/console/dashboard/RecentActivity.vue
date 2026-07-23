<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { SERIES_TOKENS } from '@/charts/palette'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import type { LogItem } from '@/types/console'
import { formatQuota, relativeTime } from '@/utils/format'

defineProps<{
  items: LogItem[]
}>()

const { t, locale } = useI18n()
const router = useRouter()
// CSS-var strings so avatar colors re-resolve on theme switch.
const colors = SERIES_TOKENS

const typeLabel: Record<LogItem['type'], string> = {
  consume: 'logs.typeConsume',
  topup: 'logs.typeTopup',
  refund: 'logs.typeRefund',
  manage: 'logs.typeManage',
  error: 'logs.typeError',
  system: 'logs.typeSystem',
}
</script>

<template>
  <ConsoleCard :title="t('dashboard.recentActivity')">
    <template #action>
      <button
        type="button"
        class="text-xs text-[var(--text-tertiary)] transition-colors hover:text-[var(--accent-text)]"
        @click="router.push({ name: 'logs' })"
      >
        {{ t('common.viewMore') }} ›
      </button>
    </template>

    <ul v-if="items.length" class="divide-y divide-[var(--border-subtle)]">
      <li
        v-for="(item, i) in items"
        :key="item.id"
        class="flex items-center gap-3 py-3 first:pt-0 last:pb-0"
      >
        <span
          class="flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-xs font-bold text-[var(--on-colored)]"
          :style="{ background: colors[i % colors.length] }"
        >
          {{ item.model === '—' ? '＄' : item.model.slice(0, 1).toUpperCase() }}
        </span>
        <div class="min-w-0 flex-1">
          <p class="truncate text-sm font-medium text-[var(--text-primary)]">
            {{ item.model }}
          </p>
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ t(typeLabel[item.type]) }}
          </p>
        </div>
        <div class="shrink-0 text-right">
          <p
            class="text-sm font-semibold"
            :style="{
              color:
                item.type === 'topup'
                  ? 'var(--status-success)'
                  : 'var(--text-primary)',
            }"
          >
            {{ item.type === 'topup' ? '+' : '-' }}{{ formatQuota(item.quota) }}
          </p>
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ relativeTime(item.created, locale) }}
          </p>
        </div>
      </li>
    </ul>
    <p v-else class="py-8 text-center text-sm text-[var(--text-tertiary)]">
      {{ t('dashboard.emptyActivity') }}
    </p>
  </ConsoleCard>
</template>
