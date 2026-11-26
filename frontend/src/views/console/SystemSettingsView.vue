<script setup lang="ts">
/**
 * System settings shell — top tab bar (7 categories) + RouterView content area.
 * Layout: pill domain switcher as the first row / content panel below.
 * Root-only page; access guard is handled by the router.
 */
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, RouterView, RouterLink } from 'vue-router'
import { useSystemSettings } from '@/composables/useSystemSettings'
import { SYSTEM_SETTINGS_DOMAINS } from '@/constants/systemSettingsCatalog'

const { t } = useI18n()
const route = useRoute()
const { load } = useSystemSettings()

const tabs = computed(() =>
  SYSTEM_SETTINGS_DOMAINS.map((domain) => ({
    key: `system-settings-${domain.id}`,
    label: t(domain.titleKey),
    section: domain.defaultSection,
  }))
)

const activeTab = computed(() => String(route.name ?? ''))

// Pre-fetch all options once on mount so sub-views share the same data
onMounted(() => load())
</script>

<template>
  <div data-handdrawn-page="system-settings" class="sibling-space-y-6">
    <!-- The domain switcher is the first row of the page. -->
    <div class="subtle-scroll max-w-full overflow-x-auto">
      <nav
        class="settings-domain-track flex w-max items-center gap-1 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-1"
        role="tablist"
        :aria-label="t('systemSettings.title')"
      >
        <RouterLink
          v-for="tab in tabs"
          :key="tab.key"
          :to="{ name: tab.key, params: { section: tab.section } }"
          role="tab"
          :aria-selected="activeTab === tab.key"
          class="settings-domain-tab flex min-h-11 shrink-0 items-center rounded-lg px-3 py-1.5 text-xs font-medium transition-colors focus-ring lg:min-h-10 sm:text-sm"
          :class="
            activeTab === tab.key
              ? 'is-active bg-[var(--accent-soft)] font-semibold text-[var(--accent-text)] shadow-none'
              : 'text-[var(--text-secondary)] hover:bg-[var(--surface-hover)] hover:text-[var(--text-primary)]'
          "
        >
          <span>{{ tab.label }}</span>
        </RouterLink>
      </nav>
    </div>

    <!-- Content -->
    <main class="min-w-0">
      <RouterView />
    </main>
  </div>
</template>

<style scoped>
html.dark .settings-domain-track {
  background: var(--studio-control-track);
  border-color: var(--outline-variant);
  box-shadow: inset 0 1px 2px rgba(0, 0, 0, 0.22);
}

html.dark .settings-domain-tab.is-active {
  background: var(--studio-control-thumb);
  color: var(--text-primary);
  box-shadow: var(--studio-control-thumb-shadow);
}

@media (pointer: coarse) {
  .settings-domain-tab {
    min-height: 2.75rem;
  }
}
</style>
