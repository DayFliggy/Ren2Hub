<script setup lang="ts">
/**
 * Shared switcher for the log pages. Rendered inside the PageBreadcrumb action
 * slot so the breadcrumb and the tabs share one row. Every tab is self-service:
 * the audit tab shows the viewer's own events unless the server grants more.
 */
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'

defineProps<{ active: 'consume' | 'drawing' | 'tasks' | 'audit' }>()

const { t } = useI18n()
const app = useAppStore()

const tabs = [
  { key: 'consume', routeName: 'logs', labelKey: 'relayLogs.tabConsume' },
  {
    key: 'drawing',
    routeName: 'logs-drawing',
    labelKey: 'relayLogs.tabDrawing',
  },
  { key: 'tasks', routeName: 'logs-tasks', labelKey: 'relayLogs.tabTasks' },
  {
    key: 'audit',
    routeName: 'logs-audit',
    labelKey: 'relayLogs.tabAudit',
    feature: 'audit',
  },
] as const
</script>

<template>
  <div class="subtle-scroll max-w-full overflow-x-auto sm:ml-auto">
    <nav
      class="flex w-max items-center gap-1 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-1 shadow-[var(--card-shadow)]"
      :aria-label="t('relayLogs.tabsLabel')"
    >
      <RouterLink
        v-for="tab in tabs.filter(
          (item) => !('feature' in item) || app.isFeatureEnabled(item.feature)
        )"
        :key="tab.key"
        :to="{ name: tab.routeName }"
        class="flex min-h-11 shrink-0 items-center rounded-lg px-3 py-1.5 text-sm font-medium transition-colors focus-ring sm:min-h-8"
        :class="
          active === tab.key
            ? 'bg-[var(--accent-soft)] text-[var(--accent-text)] font-semibold shadow-xs'
            : 'text-[var(--text-secondary)] hover:bg-[var(--surface-hover)] hover:text-[var(--text-primary)]'
        "
        :aria-current="active === tab.key ? 'page' : undefined"
      >
        {{ t(tab.labelKey) }}
      </RouterLink>
    </nav>
  </div>
</template>
