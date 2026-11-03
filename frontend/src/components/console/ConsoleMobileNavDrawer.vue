<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import LanguageSelector from '@/components/common/LanguageSelector.vue'
import { getVisibleTopNavItems } from '@/constants/navigation/topNav'
import { getAccessibleConsoleNavGroups } from '@/constants/navigation/consoleNav'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useTicketQueueStore } from '@/stores/ticketQueue'

defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const route = useRoute()
const app = useAppStore()
const auth = useAuthStore()
const ticketQueue = useTicketQueueStore()

interface DrawerItem {
  name: string
  labelKey: string
  route?: string
  href?: string
  icon?: string
  disabled?: boolean
  disabledReason?: string
  external?: boolean
}

const sections = computed(() => {
  const top = getVisibleTopNavItems({
    isAdmin: auth.isAdmin,
    isRoot: auth.isRoot,
    checkinEnabled: app.checkinEnabled,
    docs: app.docsNavigation,
    surface: 'drawer',
    featureStatus: app.featureStatus,
  })
  const groups = getAccessibleConsoleNavGroups({
    isAdmin: auth.isAdmin,
    isRoot: auth.isRoot,
    hasPermission: auth.hasPermission,
    featureStatus: app.featureStatus,
    isSidebarModuleEnabled: app.isSidebarModuleEnabled,
  })
  return [
    {
      key: 'main',
      labelKey: 'nav.navigation',
      items: top.filter((item) => item.route) as DrawerItem[],
    },
    ...groups,
    ...top
      .filter((item) => item.children)
      .map((item) => ({
        key: item.name,
        labelKey: item.labelKey,
        items: item.children!.map((child) => ({
          ...child,
          external: Boolean(child.href),
        })),
      })),
  ] as { key: string; labelKey: string; items: DrawerItem[] }[]
})

function isActive(item: DrawerItem, section: string) {
  if (item.disabled || !item.route) return false
  return section === 'main'
    ? item.name === (route.meta.topNav ?? route.name)
    : item.route === route.name || item.route === route.meta.nav
}

function componentFor(item: DrawerItem) {
  if (item.disabled) return 'button'
  return item.href ? 'a' : RouterLink
}

function linkProps(item: DrawerItem) {
  if (item.disabled)
    return {
      type: 'button',
      disabled: true,
      title: t(item.disabledReason || 'nav.disabledMode'),
    }
  if (item.href)
    return {
      href: item.href,
      target: item.external ? '_blank' : undefined,
      rel: item.external ? 'noopener noreferrer' : undefined,
    }
  // An undefined href attribute would overwrite RouterLink's generated URL.
  return { to: { name: item.route } }
}
</script>

<template>
  <ConsoleModal
    :open="open"
    :aria-label="t('nav.menuTitle')"
    presentation="drawer"
    drawer-side="left"
    size="nav"
    @close="emit('close')"
  >
    <template #header>
      <div class="flex min-w-0 items-center justify-between gap-3">
        <span
          class="display-title truncate text-xl font-semibold text-[var(--text-primary)]"
          >{{ app.systemName }}</span
        >
        <button
          type="button"
          class="drawer-close"
          :aria-label="t('nav.closeMenu')"
          @click="emit('close')"
        >
          <svg
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            aria-hidden="true"
          >
            <path d="m6 6 12 12M18 6 6 18" />
          </svg>
        </button>
      </div>
    </template>
    <nav
      id="console-mobile-navigation"
      data-console-mobile-navigation
      :aria-label="t('nav.console')"
      class="drawer-navigation"
    >
      <section
        v-for="section in sections"
        :key="section.key"
        class="drawer-section"
      >
        <h2 class="drawer-section-title">{{ t(section.labelKey) }}</h2>
        <ul>
          <li v-for="item in section.items" :key="item.name">
            <component
              :is="componentFor(item)"
              v-bind="linkProps(item)"
              :aria-current="isActive(item, section.key) ? 'page' : undefined"
              :data-console-nav-item="item.name"
              class="drawer-link"
              @click="!item.disabled && emit('close')"
            >
              <span class="drawer-link-primary">
                <svg
                  v-if="item.icon"
                  width="17"
                  height="17"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.8"
                  aria-hidden="true"
                >
                  <path :d="item.icon" />
                </svg>
                <span class="drawer-link-label">{{ t(item.labelKey) }}</span>
              </span>
              <span
                v-if="item.disabled"
                class="drawer-link-hint text-xs text-[var(--text-tertiary)]"
                >{{ t(item.disabledReason || 'nav.disabledMode') }}</span
              >
              <span
                v-if="
                  item.name === 'ticket-management' &&
                  ticketQueue.summary.pending > 0
                "
                class="drawer-badge"
                :aria-label="
                  t('tickets.admin.pendingCount', {
                    count: ticketQueue.summary.pending,
                  })
                "
                >{{
                  ticketQueue.summary.pending > 99
                    ? '99+'
                    : ticketQueue.summary.pending
                }}</span
              >
            </component>
          </li>
        </ul>
      </section>
    </nav>
    <template #footer>
      <div class="flex items-center justify-between gap-3">
        <span class="text-sm text-[var(--text-secondary)]">{{
          t('nav.language')
        }}</span>
        <LanguageSelector variant="console" placement="top" />
      </div>
    </template>
  </ConsoleModal>
</template>

<style scoped>
.drawer-navigation {
  display: grid;
  gap: 1.25rem;
  user-select: none;
}
.drawer-section-title {
  margin: 0 0 0.375rem;
  padding-inline: 0.75rem;
  color: var(--text-tertiary);
  font-size: 0.75rem;
  font-weight: 600;
  letter-spacing: 0.06em;
}
.drawer-link {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.625rem;
  min-height: 44px;
  width: 100%;
  padding: 0.625rem 0.75rem;
  border-radius: var(--shape-control);
  color: var(--text-secondary);
  font-size: 0.875rem;
  text-align: start;
}
.drawer-link svg {
  flex-shrink: 0;
}
.drawer-link-primary {
  display: flex;
  flex: 1 1 auto;
  align-items: center;
  gap: 0.625rem;
  min-width: 0;
}
.drawer-link-label {
  min-width: 0;
  overflow-wrap: anywhere;
}
.drawer-link-hint {
  max-width: 100%;
  overflow-wrap: anywhere;
}
.drawer-link:hover:not(:disabled) {
  background: var(--state-hover-layer);
  color: var(--text-primary);
}
.drawer-link[aria-current='page'] {
  background: var(--accent-soft);
  color: var(--accent-text);
  box-shadow: inset 2px 0 0 var(--accent);
  font-weight: 600;
}
.drawer-link:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}
.drawer-link:focus-visible,
.drawer-close:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: -2px;
}
.drawer-close {
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  flex-shrink: 0;
  border-radius: var(--shape-control);
  color: var(--text-secondary);
  background: var(--surface-muted);
}
.drawer-badge {
  padding: 0.125rem 0.375rem;
  border-radius: 999px;
  background: var(--status-danger-soft);
  color: var(--status-danger-text);
  font-size: 0.75rem;
}
</style>
