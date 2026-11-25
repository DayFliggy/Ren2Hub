<script setup lang="ts">
/**
 * Secondary source switcher inside the unified Async Tasks log category:
 * General tasks (/usage-logs/task) and Midjourney (/usage-logs/drawing).
 * Preserves `?scope=all` for administrators switching between sources.
 */
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  active: 'general' | 'midjourney'
  scopeAll?: boolean
}>()

const { t } = useI18n()

const scopeQuery = computed(() =>
  props.scopeAll ? { scope: 'all' } : undefined
)

const items = [
  {
    key: 'general',
    routeName: 'logs-tasks',
    labelKey: 'relayLogs.subTabGeneral',
  },
  {
    key: 'midjourney',
    routeName: 'logs-drawing',
    labelKey: 'relayLogs.subTabMidjourney',
  },
] as const
</script>

<template>
  <div class="mb-3 flex items-center">
    <div
      role="tablist"
      class="inline-flex max-w-full items-center gap-1 overflow-x-auto rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-1"
      :aria-label="t('relayLogs.subTabsLabel')"
      data-async-task-subtabs
    >
      <RouterLink
        v-for="item in items"
        :key="item.key"
        role="tab"
        :to="{ name: item.routeName, query: scopeQuery }"
        class="flex min-h-11 shrink-0 items-center rounded-lg px-3 py-1.5 text-xs font-medium transition-colors focus-ring sm:min-h-8 sm:text-sm"
        :class="
          active === item.key
            ? 'bg-[var(--surface-solid)] text-[var(--text-primary)] font-semibold shadow-xs border border-[var(--border-subtle)]'
            : 'text-[var(--text-secondary)] hover:text-[var(--text-primary)]'
        "
        :aria-selected="active === item.key"
      >
        {{ t(item.labelKey) }}
      </RouterLink>
    </div>
  </div>
</template>
