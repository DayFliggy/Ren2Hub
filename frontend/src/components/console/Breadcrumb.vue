<script setup lang="ts">
import { useI18n } from 'vue-i18n'

/**
 * Single source of truth for the console breadcrumb row. Consumed by both
 * PageHero (with a title) and PageBreadcrumb (standalone). The `spacing` prop
 * lets each host keep its own bottom margin.
 */
const { t } = useI18n()

withDefaults(
  defineProps<{
    crumbs?: string[]
    spacing?: string
  }>(),
  { crumbs: () => [], spacing: 'mb-2' }
)
</script>

<template>
  <nav
    v-if="crumbs.length"
    class="flex items-center gap-1.5 text-xs text-[var(--text-tertiary)]"
    :class="spacing"
    :aria-label="t('common.breadcrumb')"
  >
    <template v-for="(crumb, i) in crumbs" :key="i">
      <span
        v-if="i > 0"
        class="h-1 w-1 shrink-0 rounded-full bg-[var(--accent)] opacity-60"
        aria-hidden="true"
      />
      <span
        :class="i === crumbs.length - 1 ? 'text-[var(--text-secondary)]' : ''"
      >
        {{ crumb }}
      </span>
    </template>
  </nav>
</template>
