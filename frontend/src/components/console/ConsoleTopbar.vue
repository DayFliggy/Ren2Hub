<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import LanguageSelector from '@/components/common/LanguageSelector.vue'
import ThemeSwitcher from '@/components/common/ThemeSwitcher.vue'
import { getVisibleTopNavItems } from '@/constants/navigation/topNav'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'

import CommandPaletteModal from './CommandPaletteModal.vue'
import ConsoleNavScroller from './ConsoleNavScroller.vue'
import TopNavMenu from './TopNavMenu.vue'
import UserMenu from './UserMenu.vue'

const { t } = useI18n()
const route = useRoute()
const app = useAppStore()
const auth = useAuthStore()

const searchOpen = ref(false)

const isApplePlatform = /Mac|iPhone|iPad|iPod/.test(
  window.navigator.platform || window.navigator.userAgent
)
const searchShortcut = isApplePlatform ? '⌘K' : 'Ctrl+K'

const mobileItems = computed(() =>
  getVisibleTopNavItems({
    checkinEnabled: app.checkinEnabled,
    isAdmin: auth.isAdmin,
    isRoot: auth.isRoot,
    surface: 'mobile',
    featureStatus: (feature) => app.featureStatus(feature),
  })
)

const activeName = computed(() => {
  if (route.meta.topNav) return route.meta.topNav
  return route.name as string
})

function onGlobalKeydown(e: KeyboardEvent) {
  if (e.key.toLowerCase() === 'k' && (e.metaKey || e.ctrlKey)) {
    e.preventDefault()
    searchOpen.value = true
  }
}

onMounted(() => document.addEventListener('keydown', onGlobalKeydown))
onBeforeUnmount(() => document.removeEventListener('keydown', onGlobalKeydown))
</script>

<template>
  <header
    class="topbar-header sticky top-0 z-[60] bg-[var(--surface-raised)] backdrop-blur-xl"
    data-handdrawn="navigation-top"
  >
    <nav
      class="topbar-nav mx-auto flex h-14 max-w-[max(1440px,calc(var(--console-wide-width)+4rem))] items-center justify-between gap-1.5 px-3 sm:gap-3 sm:px-6 lg:h-16 lg:px-8"
    >
      <!-- brand (mobile only) -->
      <div class="flex min-w-0 items-center gap-2.5 lg:hidden">
        <RouterLink
          :to="{ name: 'dashboard' }"
          class="flex min-h-11 items-center gap-2.5 sm:min-h-0"
          aria-current-value="false"
          data-console-brand-link
          :aria-label="`${app.systemName} ${t('nav.dashboard')}`"
        >
          <img
            :src="app.logo"
            alt=""
            aria-hidden="true"
            class="pencil-brand-frame h-8 w-8 shrink-0 object-contain"
            style="filter: var(--brand-icon-shadow)"
          />
          <span
            class="display-title hidden whitespace-nowrap text-lg font-bold tracking-tight text-[var(--text-primary)] sm:inline"
            >{{ app.systemName }}</span
          >
        </RouterLink>
      </div>

      <!-- center nav (desktop) -->
      <div class="hidden flex-1 items-center justify-center lg:flex">
        <TopNavMenu />
      </div>

      <!-- right actions -->
      <div class="flex shrink-0 items-center gap-0.5 sm:gap-1.5">
        <!-- search: underline-style hand-drawn feel -->
        <button
          type="button"
          class="pencil-control search-trigger topbar-search flex h-11 min-w-11 items-center justify-center gap-2 rounded-xl bg-[var(--surface-muted)] px-3 text-sm text-[var(--text-tertiary)] transition-colors hover:bg-[var(--surface-hover)] hover:text-[var(--text-secondary)] focus-ring md:w-auto md:justify-start md:pl-3.5"
          data-handdrawn="control"
          :aria-label="t('nav.search')"
          @click="searchOpen = true"
        >
          <!-- hand-drawn magnifier SVG: rounded linecap, slightly asymmetric -->
          <svg
            width="15"
            height="15"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <circle cx="11" cy="11" r="7" />
            <path d="m20 20-3.5-3.5" />
          </svg>
          <span class="topbar-control-label hidden md:inline">{{
            t('nav.search')
          }}</span>
          <kbd
            class="topbar-control-label hidden rounded-sm border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-1.5 py-px text-[0.625rem] font-medium md:inline"
          >
            {{ searchShortcut }}
          </kbd>
        </button>
        <LanguageSelector variant="console" class="topbar-language" />
        <ThemeSwitcher variant="console" />
        <div
          class="topbar-divider hidden h-6 w-px bg-[var(--border-default)] sm:mx-1 sm:block"
        />
        <UserMenu />
      </div>
    </nav>

    <!-- Mobile nav strip: the active outline carries the hand-drawn tilt. -->
    <div
      class="border-t border-[var(--border-subtle)] lg:hidden"
      data-handdrawn="navigation-strip"
    >
      <ConsoleNavScroller
        :active-key="activeName"
        fade-surface="var(--surface-raised)"
        data-top-nav="mobile"
      >
        <template v-for="item in mobileItems" :key="item.name">
          <button
            v-if="item.disabled"
            type="button"
            class="mobile-pill flex min-h-11 shrink-0 cursor-not-allowed items-center gap-1.5 px-3.5 text-xs font-medium text-[var(--text-tertiary)] opacity-60"
            :title="t('nav.comingSoon')"
            disabled
          >
            {{ t(item.labelKey) }}
            <span class="text-[0.625rem]">{{ t('nav.comingSoon') }}</span>
          </button>
          <RouterLink
            v-else-if="item.route"
            :to="{ name: item.route }"
            class="mobile-pill flex min-h-11 shrink-0 items-center px-3.5 text-xs font-medium transition-all"
            :class="activeName === item.name ? 'active' : ''"
            :style="
              activeName === item.name
                ? 'background:var(--accent);color:var(--accent-contrast)'
                : 'color:var(--text-secondary)'
            "
            :aria-current="activeName === item.name ? 'page' : undefined"
          >
            {{ t(item.labelKey) }}
          </RouterLink>
        </template>
      </ConsoleNavScroller>
    </div>

    <CommandPaletteModal :open="searchOpen" @close="searchOpen = false" />
  </header>
</template>

<style scoped>
/* Gradient bottom border: fades in from center, giving a brush-stroke horizon */
.topbar-header {
  border-bottom: 1px solid transparent;
  background-clip: padding-box;
  box-shadow: 0 1px 0 var(--dec-gold-line);
  /* The bar's width already excludes the sidebar, expanded or collapsed. */
  container: console-topbar / inline-size;
}

/*
 * The desktop menu labels never wrap. At a 16px default font, widths below
 * 1120px reclaim the search and language labels; below 920px the bar and
 * TopNavMenu also tighten their padding. Rem thresholds preserve these tiers
 * as the user's default font grows. Below lg the menu is hidden.
 */
@media (min-width: 1024px) {
  @container console-topbar (max-width: 69.9375rem) {
    .topbar-control-label,
    .topbar-divider,
    .topbar-language :deep([data-language-label]) {
      display: none;
    }
    .topbar-search {
      justify-content: center;
      padding-inline: 0;
    }
  }
  @container console-topbar (max-width: 57.4375rem) {
    .topbar-nav {
      padding-inline: 1rem;
    }
  }
}

/* Active mobile pill keeps its hit geometry fixed. */
.mobile-pill {
  border-radius: 9999px;
}
.mobile-pill.active {
  border-radius: var(--sketch-border-radius-sm);
}
</style>
