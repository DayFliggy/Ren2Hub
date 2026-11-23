<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute } from 'vue-router'

import {
  consoleNavTools,
  getAccessibleConsoleNavGroups,
} from '@/constants/navigation/consoleNav'
import { useSidebarCollapsed } from '@/composables/useSidebarCollapsed'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useTicketQueueStore } from '@/stores/ticketQueue'

import './version/version.css'

const { t } = useI18n()
const route = useRoute()
const app = useAppStore()
const auth = useAuthStore()
const ticketQueue = useTicketQueueStore()

onMounted(ticketQueue.start)
onBeforeUnmount(ticketQueue.stop)

const visibleNavGroups = computed(() =>
  getAccessibleConsoleNavGroups({
    isAdmin: auth.isAdmin,
    isRoot: auth.isRoot,
    hasPermission: auth.hasPermission,
    featureStatus: (feature) => app.featureStatus(feature),
    isSidebarModuleEnabled: app.isSidebarModuleEnabled,
  })
)

const toolItems = computed(() =>
  consoleNavTools.map((item) =>
    item.name === 'docs'
      ? { ...item, ...app.docsNavigation }
      : { ...item, disabledReason: undefined }
  )
)

const collapsed = useSidebarCollapsed()

const AUTO_COLLAPSE_KEY = 'ren2hub_activity_auto_collapsed'
watch(
  () => route.name,
  (name) => {
    if (name !== 'activity') return
    try {
      if (window.sessionStorage.getItem(AUTO_COLLAPSE_KEY) === '1') return
      window.sessionStorage.setItem(AUTO_COLLAPSE_KEY, '1')
    } catch {
      // sessionStorage unavailable — collapse on each entry as before.
    }
    collapsed.value = true
  },
  { immediate: true }
)

const activeName = computed(() => {
  const matches = (routeName: string) =>
    routeName === route.name || routeName === route.meta.nav
  for (const group of visibleNavGroups.value) {
    for (const item of group.items) {
      if (item.route && matches(item.route)) return item.name
    }
  }
  for (const item of consoleNavTools) {
    if (item.route && matches(item.route)) return item.name
  }
  return null
})

function navComponent(item: {
  route?: string
  href?: string
  disabled?: boolean
}) {
  if (item.disabled) return 'button'
  if (item.href) return 'a'
  return item.route ? RouterLink : 'button'
}

defineExpose({ collapsed })
</script>

<template>
  <aside
    class="console-sidebar sticky top-0 hidden h-full shrink-0 select-none flex-col overflow-hidden border-r border-[var(--border-subtle)] bg-[var(--surface-solid)] texture-paper transition-[width] duration-[250ms] lg:flex"
    :style="{ width: collapsed ? '4rem' : '13.75rem' }"
    data-handdrawn="navigation"
  >
    <!-- brand header: serif typeface for hand-crafted feel. The Root version
         badge is teleported into the anchor beside the link: the top-right
         corner of the name, or of the logo when the sidebar is collapsed. -->
    <div
      class="console-sidebar__brand flex h-16 shrink-0 items-center border-b border-[var(--border-subtle)] transition-all"
      :class="collapsed ? 'justify-center px-0' : 'pl-6 pr-11'"
    >
      <div
        class="relative flex h-full min-w-0 items-center"
        :class="collapsed ? 'w-full' : ''"
        :style="
          collapsed
            ? '--version-anchor-rise: 0.875rem; --version-anchor-inset: 1.125rem'
            : '--version-anchor-rise: 0.5rem'
        "
      >
        <RouterLink
          :to="{ name: 'dashboard' }"
          class="flex h-full min-w-0 items-center gap-2.5"
          :class="collapsed ? 'w-full justify-center' : ''"
          aria-current-value="false"
          data-console-brand-link
          :aria-label="`${app.systemName} ${t('nav.dashboard')}`"
        >
          <img
            :src="app.logo"
            alt=""
            aria-hidden="true"
            class="pencil-brand-frame h-7 w-7 shrink-0 object-contain"
            style="filter: var(--brand-icon-shadow)"
          />
          <span
            v-if="!collapsed"
            class="display-title truncate text-lg font-bold tracking-tight text-[var(--text-primary)]"
          >
            {{ app.systemName }}
          </span>
        </RouterLink>
        <div
          class="version-anchor"
          :class="{ 'version-anchor-compact': collapsed }"
          data-sidebar-version-slot
        />
      </div>
    </div>

    <div
      class="subtle-scroll flex min-h-0 flex-1 flex-col overflow-y-auto py-4"
    >
      <!-- nav groups -->
      <div
        v-for="group in visibleNavGroups"
        :key="group.key"
        :class="['px-3', { 'mb-5': !collapsed }]"
      >
        <!-- group label: hand-drawn rust-red tick by day, gilded & slate tick by night -->
        <div v-if="!collapsed" class="mb-1 flex items-center gap-2 px-3 py-0.5">
          <!-- bamboo-node style: a cluster of short horizontal rules -->
          <div
            class="console-sidebar__group-ticks flex shrink-0 flex-col gap-[3px]"
            aria-hidden="true"
          >
            <span
              class="console-sidebar__tick-primary block h-px w-3 rounded-full"
            />
            <span
              class="console-sidebar__tick-secondary block h-px w-2 rounded-full"
            />
          </div>
          <span
            class="text-[0.625rem] font-semibold uppercase tracking-[0.18em] text-[var(--text-tertiary)]"
          >
            {{ t(group.labelKey) }}
          </span>
        </div>

        <ul class="sibling-space-y-0.5">
          <li v-for="item in group.items" :key="item.name">
            <component
              :is="navComponent(item)"
              :to="
                !item.disabled && item.route && !item.href
                  ? { name: item.route }
                  : undefined
              "
              v-bind="!item.disabled && item.href ? { href: item.href } : {}"
              :type="
                item.disabled || (!item.route && !item.href)
                  ? 'button'
                  : undefined
              "
              class="console-sidebar__item group relative flex h-10 w-full items-center gap-3 rounded-xl px-3 text-sm font-medium transition-colors focus-ring"
              :class="[
                item.disabled
                  ? 'cursor-not-allowed opacity-40'
                  : activeName === item.name
                    ? 'console-sidebar__item--active'
                    : 'hover:bg-[var(--surface-warm-tile)]',
                collapsed ? 'justify-center' : '',
              ]"
              :style="
                activeName === item.name && !item.disabled
                  ? 'color:var(--text-primary);background:var(--surface-muted)'
                  : 'color:var(--text-secondary)'
              "
              :disabled="item.disabled || undefined"
              :title="
                collapsed
                  ? t(item.labelKey)
                  : item.disabled
                    ? t('nav.comingSoon')
                    : undefined
              "
              :aria-current="activeName === item.name ? 'page' : undefined"
              :data-console-nav-item="item.name"
            >
              <!-- active indicator: wider brush-stroke bar with rounded caps -->
              <span
                v-if="activeName === item.name && !collapsed"
                class="console-sidebar__active-bar absolute left-0 top-1/2 h-5 w-[3px] -translate-y-1/2 rounded-r-full"
                style="background: var(--accent)"
                aria-hidden="true"
              />

              <svg
                width="17"
                height="17"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.8"
                stroke-linecap="round"
                stroke-linejoin="round"
                :class="
                  activeName === item.name ? 'text-[var(--accent-text)]' : ''
                "
              >
                <path :d="item.icon" />
              </svg>

              <span v-if="!collapsed" class="min-w-0 flex-1 truncate text-left">
                {{ t(item.labelKey) }}
              </span>

              <span
                v-if="
                  item.name === 'ticket-management' &&
                  ticketQueue.summary.pending > 0
                "
                class="flex min-w-5 shrink-0 items-center justify-center rounded-full bg-[var(--status-danger)] px-1.5 text-[0.625rem] font-semibold leading-5 text-[var(--text-inverse)]"
                :class="collapsed ? 'absolute right-1 top-1' : ''"
                :aria-label="
                  t('tickets.admin.pendingCount', {
                    count: ticketQueue.summary.pending,
                  })
                "
              >
                {{
                  ticketQueue.summary.pending > 99
                    ? '99+'
                    : ticketQueue.summary.pending
                }}
              </span>

              <span
                v-if="!collapsed && item.disabled"
                class="shrink-0 rounded-sm bg-[var(--surface-muted)] px-1.5 py-px text-[0.625rem] text-[var(--text-tertiary)]"
              >
                {{ t('nav.comingSoon') }}
              </span>
            </component>
          </li>
        </ul>
      </div>
    </div>

    <!-- bottom tools: ink-brush divider by day, gold hairline by night -->
    <div class="shrink-0 px-3 py-3">
      <div class="ink-divider mb-3 -mx-3" aria-hidden="true" />
      <ul class="sibling-space-y-0.5">
        <li v-for="item in toolItems" :key="item.name">
          <component
            :is="navComponent(item)"
            :to="
              !item.disabled && item.route && !item.href
                ? { name: item.route }
                : undefined
            "
            :href="!item.disabled ? item.href : undefined"
            :target="item.href ? '_blank' : undefined"
            :rel="item.href ? 'noopener noreferrer' : undefined"
            :data-console-tool="item.name"
            :type="
              item.disabled || (!item.route && !item.href)
                ? 'button'
                : undefined
            "
            class="flex h-9 w-full items-center gap-3 rounded-xl px-3 text-xs font-medium transition-colors focus-ring"
            :class="[
              item.disabled
                ? 'cursor-not-allowed opacity-40'
                : 'hover:bg-[var(--surface-warm-tile)]',
              collapsed ? 'justify-center' : '',
            ]"
            style="color: var(--text-tertiary)"
            :disabled="item.disabled || undefined"
            :title="
              item.disabled
                ? `${t(item.labelKey)} · ${t(item.disabledReason || 'nav.notProvided')}`
                : collapsed
                  ? t(item.labelKey)
                  : undefined
            "
          >
            <svg
              width="15"
              height="15"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.8"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <path :d="item.icon" />
            </svg>
            <span v-if="!collapsed" class="min-w-0 flex-1 truncate text-left">{{
              t(item.labelKey)
            }}</span>
            <span v-if="!collapsed && item.disabled" class="text-[0.625rem]">{{
              t(item.disabledReason || 'nav.notProvided')
            }}</span>
          </component>
        </li>
      </ul>

      <!-- collapse toggle -->
      <button
        type="button"
        class="console-sidebar__collapse mt-2 flex h-9 w-full items-center gap-3 rounded-xl px-3 text-xs font-medium text-[var(--text-tertiary)] transition-colors hover:bg-[var(--surface-warm-tile)] focus-ring"
        :class="collapsed ? 'justify-center' : ''"
        :aria-label="collapsed ? t('nav.expand') : t('nav.collapse')"
        @click="collapsed = !collapsed"
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
          class="console-sidebar__collapse-icon shrink-0 text-[var(--status-danger-text)] transition-transform duration-[250ms]"
          :class="collapsed ? 'rotate-180' : ''"
        >
          <path d="m15 6-6 6 6 6" />
        </svg>
        <span v-if="!collapsed">{{ t('nav.collapse') }}</span>
      </button>
    </div>
  </aside>
</template>

<style scoped>
.console-sidebar__tick-primary {
  background: var(--status-danger);
  opacity: 0.7;
}

.console-sidebar__tick-secondary {
  background: var(--status-danger);
  opacity: 0.45;
}

:global(html.dark .console-sidebar) {
  margin: 12px 0 12px 12px;
  height: calc(100% - 24px);
  border-radius: 16px;
  background: var(--nav-surface) !important;
  -webkit-backdrop-filter: blur(28px) saturate(130%);
  backdrop-filter: blur(28px) saturate(130%);
  border: 1px solid var(--nav-border) !important;
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.11),
    inset 0 0 0 0.5px rgba(228, 192, 107, 0.05),
    0 14px 36px -6px rgba(3, 6, 14, 0.42),
    0 4px 12px rgba(3, 6, 14, 0.24) !important;
}

:global(html.dark .console-sidebar__brand) {
  border-bottom: 1px solid transparent !important;
  background-image: linear-gradient(
    90deg,
    transparent 8%,
    rgba(228, 192, 107, 0.18) 36%,
    rgba(226, 232, 245, 0.08) 72%,
    transparent 94%
  );
  background-size: 100% 1px;
  background-position: bottom center;
  background-repeat: no-repeat;
}

:global(html.dark .console-sidebar__brand .display-title) {
  font-family: var(--font-display);
  font-weight: 600;
  letter-spacing: 0.01em;
}

:global(html.dark .console-sidebar__tick-primary) {
  background: var(--accent);
  opacity: 0.78;
}

:global(html.dark .console-sidebar__tick-secondary) {
  background: var(--signal);
  opacity: 0.48;
}

:global(html.dark .console-sidebar__item) {
  height: auto;
  min-height: 2.375rem;
  border-radius: 10px !important;
  font-size: 0.8125rem;
  font-weight: 500;
  letter-spacing: 0.005em;
}

:global(
  html.dark
    .console-sidebar__item:not([aria-current='page']):not(:disabled):hover
) {
  background: rgba(228, 192, 107, 0.065) !important;
  color: var(--text-primary) !important;
}

:global(html.dark .console-sidebar__item[aria-current='page']) {
  background: linear-gradient(
    90deg,
    rgba(228, 192, 107, 0.14) 0%,
    rgba(228, 192, 107, 0.05) 100%
  ) !important;
  color: var(--text-primary) !important;
  font-weight: 600;
  box-shadow:
    inset 2.5px 0 0 var(--accent),
    inset 0 1px 0 rgba(255, 248, 225, 0.08),
    0 1px 3px rgba(0, 0, 0, 0.16) !important;
}

:global(html.dark .console-sidebar__active-bar) {
  display: none;
}

:global(html.dark .console-sidebar__collapse-icon) {
  color: var(--text-tertiary);
}

:global(
  html.dark .console-sidebar__collapse:hover .console-sidebar__collapse-icon
) {
  color: var(--accent-text);
}

@supports not (
  (backdrop-filter: blur(1px)) or (-webkit-backdrop-filter: blur(1px))
) {
  :global(html.dark .console-sidebar) {
    background: var(--nav-solid) !important;
  }
}

@media (prefers-reduced-transparency: reduce) {
  :global(html.dark .console-sidebar) {
    background: var(--nav-solid) !important;
    -webkit-backdrop-filter: none !important;
    backdrop-filter: none !important;
  }
}

:global(html.dark[data-material='solid'] .console-sidebar) {
  background: var(--nav-solid) !important;
  -webkit-backdrop-filter: none !important;
  backdrop-filter: none !important;
}
</style>
