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
      class="async-subtabs inline-flex max-w-full items-center gap-1 overflow-x-auto rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-1"
      :aria-label="t('relayLogs.subTabsLabel')"
      data-async-task-subtabs
    >
      <RouterLink
        v-for="item in items"
        :key="item.key"
        role="tab"
        :to="{ name: item.routeName, query: scopeQuery }"
        class="async-subtab flex min-h-11 shrink-0 items-center rounded-lg px-3 py-1.5 text-xs font-medium transition-colors focus-ring lg:min-h-8 sm:text-sm"
        :class="
          active === item.key
            ? 'is-active bg-[var(--surface-solid)] text-[var(--text-primary)] font-semibold shadow-xs border border-[var(--border-subtle)]'
            : 'text-[var(--text-secondary)] hover:text-[var(--text-primary)]'
        "
        :aria-selected="active === item.key"
      >
        {{ t(item.labelKey) }}
      </RouterLink>
    </div>
  </div>
</template>

<style scoped>
html.dark .async-subtabs {
  background: var(--studio-control-track);
  border-color: var(--outline-variant);
  box-shadow: inset 0 1px 2px rgba(0, 0, 0, 0.22);
}

html.dark .async-subtab.is-active {
  background: var(--studio-control-thumb);
  border-color: var(--outline-variant);
  box-shadow: var(--studio-control-thumb-shadow);
}

@media (pointer: coarse) {
  .async-subtab {
    min-height: 2.75rem;
  }
}
</style>
