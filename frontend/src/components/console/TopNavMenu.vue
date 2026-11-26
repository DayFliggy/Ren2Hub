<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import {
  getVisibleTopNavItems,
  type TopNavChild as NavChild,
  type TopNavItem as NavGroup,
} from '@/constants/navigation/topNav'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const app = useAppStore()
const auth = useAuthStore()

const navGroups = computed<NavGroup[]>(() =>
  getVisibleTopNavItems({
    docs: app.docsNavigation,
    checkinEnabled: app.checkinEnabled,
    isAdmin: auth.isAdmin,
    isRoot: auth.isRoot,
    surface: 'desktop',
    featureStatus: (feature) => app.featureStatus(feature),
  })
)

const openGroup = ref<string | null>(null)
const root = ref<HTMLElement | null>(null)

const activeGroup = computed(() => {
  if (route.meta.topNav) return route.meta.topNav
  for (const group of navGroups.value) {
    if (group.route && route.name === group.route) return group.name
    if (group.children?.some((c) => c.route && route.name === c.route))
      return group.name
  }
  return null
})

function isActive(group: NavGroup) {
  return activeGroup.value === group.name
}

function onGroupClick(group: NavGroup) {
  if (group.disabled) return
  if (group.route) {
    openGroup.value = null
    router.push({ name: group.route })
    return
  }
  openGroup.value = openGroup.value === group.name ? null : group.name
}

function onChildClick(child: NavChild) {
  if (child.disabled) return
  openGroup.value = null
  if (child.route) router.push({ name: child.route })
}

function onClickOutside(e: MouseEvent) {
  if (root.value && !root.value.contains(e.target as Node))
    openGroup.value = null
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') openGroup.value = null
}

onMounted(() => {
  document.addEventListener('click', onClickOutside)
  document.addEventListener('keydown', onKeydown)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onClickOutside)
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div
    ref="root"
    class="top-nav flex items-center justify-center gap-1"
    data-top-nav="desktop"
  >
    <div v-for="group in navGroups" :key="group.name" class="relative shrink-0">
      <button
        type="button"
        class="top-nav-item flex items-center gap-1.5 whitespace-nowrap rounded-full px-4 py-2 text-sm font-medium transition-colors focus-ring"
        :class="[
          group.disabled
            ? 'cursor-not-allowed opacity-50'
            : isActive(group)
              ? 'top-nav-item--active'
              : 'hover:bg-[var(--surface-muted)] hover:text-[var(--text-primary)]',
        ]"
        :disabled="group.disabled"
        :title="group.disabled ? t('nav.comingSoon') : undefined"
        :aria-expanded="group.children ? openGroup === group.name : undefined"
        :aria-current="isActive(group) && group.route ? 'page' : undefined"
        :data-prefetch-route="group.disabled ? undefined : group.route"
        @click="onGroupClick(group)"
      >
        {{ t(group.labelKey) }}
        <svg
          v-if="group.children"
          class="transition-transform"
          :class="{ 'rotate-180': openGroup === group.name }"
          width="12"
          height="12"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"
        >
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>

      <div
        v-if="group.children && openGroup === group.name"
        class="absolute left-1/2 top-12 z-[var(--z-popover)] w-52 -translate-x-1/2 rounded-2xl border border-[var(--overlay-border)] bg-[var(--surface-overlay)] py-1.5 shadow-[var(--overlay-shadow)] animate-scale-in"
        data-handdrawn="menu"
      >
        <component
          :is="child.href && !child.disabled ? 'a' : 'button'"
          v-for="child in group.children"
          :key="child.name"
          :href="!child.disabled ? child.href : undefined"
          :target="child.href ? '_blank' : undefined"
          :rel="child.href ? 'noopener noreferrer' : undefined"
          :type="!child.href || child.disabled ? 'button' : undefined"
          class="flex w-full items-center gap-3 px-4 py-2 text-left text-sm transition-colors"
          :class="
            child.disabled
              ? 'cursor-not-allowed text-[var(--text-tertiary)] opacity-60'
              : 'text-[var(--text-primary)] hover:bg-[var(--surface-muted)]'
          "
          :disabled="child.disabled"
          @click="onChildClick(child)"
        >
          <svg
            v-if="child.icon"
            class="shrink-0 text-[var(--text-tertiary)]"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            aria-hidden="true"
          >
            <path :d="child.icon" />
          </svg>
          <span class="min-w-0 flex-1 truncate">{{ t(child.labelKey) }}</span>
          <span
            v-if="child.disabled"
            class="rounded-sm bg-[var(--surface-muted)] px-1.5 py-px text-[0.625rem] text-[var(--text-tertiary)]"
          >
            {{ t(child.disabledReason || 'nav.notProvided') }}
          </span>
        </component>
      </div>
    </div>
  </div>
</template>

<style scoped>
.top-nav-item {
  color: var(--text-secondary);
}
.top-nav-item--active {
  background: var(--accent);
  color: var(--accent-contrast);
}
html.dark .top-nav-item {
  min-height: 40px;
  border-radius: var(--shape-control);
}
html.dark .top-nav-item--active {
  position: relative;
  color: var(--accent-text);
  background: linear-gradient(
    180deg,
    rgba(228, 192, 107, 0.13),
    rgba(228, 192, 107, 0.06)
  );
  box-shadow: inset 0 1px 0 rgba(246, 220, 162, 0.1);
}
html.dark .top-nav-item--active::after {
  content: '';
  position: absolute;
  bottom: 2px;
  inset-inline: 30%;
  height: 1.5px;
  border-radius: 2px;
  background: var(--accent);
}
@media (pointer: coarse) {
  .top-nav-item {
    min-height: 44px;
  }
}
/* Labels stay on one line (see the width tiers in ConsoleTopbar). In the
   narrowest bar the widest menu only fits with tighter pills. */
@container console-topbar (max-width: 57.4375rem) {
  .top-nav {
    gap: 2px;
  }
  .top-nav-item {
    padding-inline: 0.625rem;
  }
}
</style>
