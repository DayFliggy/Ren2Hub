<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import LanguageSelector from '@/components/common/LanguageSelector.vue'
import ThemeSwitcher from '@/components/common/ThemeSwitcher.vue'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'

import CommandPaletteModal from './CommandPaletteModal.vue'
import ConsoleMobileNavDrawer from './ConsoleMobileNavDrawer.vue'
import TopNavMenu from './TopNavMenu.vue'
import UserMenu from './UserMenu.vue'
import VersionBadge from './version/VersionBadge.vue'

const { t } = useI18n()
const route = useRoute()
const app = useAppStore()
const auth = useAuthStore()

const searchOpen = ref(false)
const navigationOpen = ref(false)
const isDesktop = useMediaQuery('(min-width: 1024px)')

watch(
  () => route.fullPath,
  () => {
    navigationOpen.value = false
  }
)
watch(isDesktop, async (desktop) => {
  if (!desktop || !navigationOpen.value) return
  navigationOpen.value = false
  await nextTick()
  document
    .querySelector<HTMLElement>(
      'aside[data-handdrawn="navigation"] [aria-current="page"]'
    )
    ?.focus()
})

const isApplePlatform = /Mac|iPhone|iPad|iPod/.test(
  window.navigator.platform || window.navigator.userAgent
)
const searchShortcut = isApplePlatform ? '⌘K' : 'Ctrl+K'

async function onGlobalKeydown(e: KeyboardEvent) {
  if (e.key.toLowerCase() === 'k' && (e.metaKey || e.ctrlKey)) {
    e.preventDefault()
    navigationOpen.value = false
    await nextTick()
    searchOpen.value = true
  }
}

onMounted(() => document.addEventListener('keydown', onGlobalKeydown))
onBeforeUnmount(() => document.removeEventListener('keydown', onGlobalKeydown))
</script>

<template>
  <header
    class="topbar-header sticky top-0 z-[var(--z-topbar)] bg-[var(--surface-raised)] backdrop-blur-xl"
    data-handdrawn="navigation-top"
  >
    <nav
      class="topbar-nav mx-auto flex h-14 max-w-[max(1440px,calc(var(--console-wide-width)+4rem))] items-center justify-between gap-1.5 px-3 sm:gap-3 sm:px-6 lg:h-16 lg:px-8"
    >
      <!-- brand (mobile only) -->
      <div class="topbar-brand flex min-w-0 items-center gap-2.5 lg:hidden">
        <button
          type="button"
          class="console-navigation-trigger"
          :aria-label="t('nav.menuTitle')"
          aria-controls="console-mobile-navigation"
          :aria-expanded="navigationOpen"
          aria-haspopup="dialog"
          @click="navigationOpen = true"
        >
          <svg
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            stroke-linecap="round"
            aria-hidden="true"
          >
            <path d="M4 6h16M4 12h16M4 18h16" />
          </svg>
        </button>
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
      <div
        class="topbar-desktop-navigation hidden min-w-0 flex-1 items-center justify-center lg:flex"
      >
        <TopNavMenu />
      </div>

      <!-- right actions -->
      <div class="topbar-actions flex shrink-0 items-center gap-0.5 sm:gap-1.5">
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
        <LanguageSelector
          variant="console"
          class="topbar-language hidden lg:block"
        />
        <ThemeSwitcher variant="console" />
        <!-- Keyed by account: a lost Root role or another account unmounts the
             badge, which ends the old account's restart watch. -->
        <VersionBadge v-if="auth.isRoot" :key="auth.user?.id" />
        <div
          class="topbar-divider hidden h-6 w-px bg-[var(--border-default)] sm:mx-1 sm:block"
        />
        <UserMenu />
      </div>
    </nav>

    <ConsoleMobileNavDrawer
      :open="navigationOpen"
      @close="navigationOpen = false"
    />
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
  /* Text enlargement can leave less space than a phone inside the desktop
     shell. Use the same complete drawer instead of clipping navigation. */
  @container console-topbar (max-width: 40rem) {
    .topbar-desktop-navigation,
    .topbar-language,
    .topbar-brand span,
    .topbar-divider {
      display: none;
    }
    .topbar-brand {
      display: flex;
      gap: 8px;
    }
    .topbar-brand img {
      width: 32px;
      height: 32px;
    }
    .topbar-brand [data-console-brand-link] {
      min-height: 44px;
    }
    .topbar-nav {
      height: auto;
      min-height: 52px;
      padding: 4px 12px;
      gap: 6px;
    }
    .topbar-actions {
      gap: 2px;
    }
    .topbar-search,
    .topbar-actions :deep([data-theme-switcher]),
    .topbar-actions :deep([data-user-menu-trigger]) {
      width: 44px;
      min-width: 44px;
      height: 44px;
      padding: 0;
      flex-shrink: 0;
    }
  }
}

.console-navigation-trigger {
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  flex-shrink: 0;
  border-radius: var(--shape-control);
  color: var(--text-secondary);
}
.console-navigation-trigger:hover {
  background: var(--state-hover-layer);
}
.console-navigation-trigger:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: -2px;
}
html.dark .topbar-header {
  background: var(--nav-surface);
  border: 1px solid var(--nav-border);
  box-shadow: var(--nav-shadow);
  backdrop-filter: blur(28px) saturate(125%);
  -webkit-backdrop-filter: blur(28px) saturate(125%);
}
@media (min-width: 1024px) {
  html.dark .topbar-header {
    margin: 12px 2rem 0;
    border-radius: 1rem;
    flex-shrink: 0;
  }
  html.dark .topbar-nav {
    height: 3.5rem;
    padding-inline: 1rem;
  }
}
@media (max-width: 1023px) {
  .topbar-nav {
    height: auto;
    min-height: 52px;
    padding: 4px 12px;
    gap: 6px;
  }
  .topbar-brand {
    gap: 8px;
  }
  .topbar-brand [data-console-brand-link] {
    min-height: 44px;
    gap: 8px;
  }
  .topbar-brand img {
    width: 32px;
    height: 32px;
  }
  .topbar-actions {
    gap: 2px;
  }
  /* Icon-only targets stay usable without consuming the text zoom budget. */
  .topbar-search,
  .topbar-actions :deep([data-theme-switcher]),
  .topbar-actions :deep([data-user-menu-trigger]) {
    width: 44px;
    min-width: 44px;
    height: 44px;
    padding: 0;
    flex-shrink: 0;
  }
  .topbar-control-label,
  .topbar-divider {
    display: none;
  }
  .topbar-header {
    padding-top: env(safe-area-inset-top);
  }
  html.dark .topbar-header {
    border-width: 0 0 1px;
  }
}
@supports not (
  (backdrop-filter: blur(1px)) or (-webkit-backdrop-filter: blur(1px))
) {
  html.dark .topbar-header {
    background: var(--nav-solid);
  }
}
@media (prefers-reduced-transparency: reduce) {
  html.dark .topbar-header {
    background: var(--nav-solid);
    backdrop-filter: none;
    -webkit-backdrop-filter: none;
  }
}
</style>
