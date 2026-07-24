<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import LanguageSelector from '@/components/common/LanguageSelector.vue'
import ThemeSwitcher from '@/components/common/ThemeSwitcher.vue'
import {
  consoleEntryRoute,
  consoleRouteNames,
} from '@/constants/navigation/consoleNav'
import { labEntryRoute, labRouteNames } from '@/constants/navigation/labNav'

import BrandMark from './BrandMark.vue'
import CommandPaletteModal from './CommandPaletteModal.vue'
import NotificationPanel from './NotificationPanel.vue'
import TopNavMenu from './TopNavMenu.vue'
import UserMenu from './UserMenu.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const searchOpen = ref(false)

// Mobile strip carries top-level groups only; console sub-nav lives in
// the ConsoleNavStrip inside the layout.
const mobileItems = [
  { name: 'activities', labelKey: 'nav.activities', route: 'activity' },
  { name: 'dashboard', labelKey: 'nav.dashboard', route: 'dashboard' },
  { name: 'console', labelKey: 'nav.console', route: consoleEntryRoute },
  { name: 'alchemy', labelKey: 'nav.alchemy', route: labEntryRoute },
]

const activeName = computed(() => {
  if (consoleRouteNames.has(route.name as string)) return 'console'
  if (labRouteNames.has(route.name as string)) return 'alchemy'
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
    class="sticky top-0 z-[60] border-b border-[var(--border-subtle)] bg-[var(--surface-raised)] backdrop-blur-xl"
  >
    <nav
      class="mx-auto flex h-16 max-w-[1440px] items-center justify-between gap-1.5 px-3 sm:gap-3 sm:px-6 lg:px-8"
    >
      <!-- brand (mobile only; on desktop the sidebar header owns the brand) -->
      <div class="flex min-w-0 items-center gap-2.5 lg:hidden">
        <RouterLink
          :to="{ name: 'dashboard' }"
          class="flex items-center gap-2.5"
          :aria-label="`RenRen AI ${t('nav.dashboard')}`"
        >
          <BrandMark class="h-8 w-8 rounded-lg" />
          <span
            class="hidden whitespace-nowrap text-lg font-bold tracking-tight text-[var(--text-primary)] sm:inline"
            >RenRen AI</span
          >
        </RouterLink>
      </div>

      <!-- center nav (desktop) -->
      <div class="hidden flex-1 items-center justify-center lg:flex">
        <TopNavMenu />
      </div>

      <!-- right actions -->
      <div class="flex shrink-0 items-center gap-0.5 sm:gap-1.5">
        <button
          type="button"
          class="flex items-center gap-2 rounded-full bg-[var(--surface-muted)] py-2 pl-3.5 pr-3 text-sm text-[var(--text-tertiary)] transition-colors hover:bg-[var(--surface-hover)] hover:text-[var(--text-secondary)] focus-ring"
          :aria-label="`${t('nav.search')} ⌘K`"
          @click="searchOpen = true"
        >
          <svg
            width="15"
            height="15"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <circle cx="11" cy="11" r="7" />
            <path d="m20 20-3.5-3.5" />
          </svg>
          <span class="hidden md:inline">{{ t('nav.search') }}</span>
          <kbd
            class="hidden rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-1.5 py-px text-[10px] font-medium md:inline"
          >
            ⌘K
          </kbd>
        </button>
        <LanguageSelector variant="console" />
        <ThemeSwitcher variant="console" />
        <NotificationPanel />
        <div
          class="hidden h-6 w-px bg-[var(--border-default)] sm:mx-1 sm:block"
        />
        <UserMenu />
      </div>
    </nav>

    <!-- mobile nav strip -->
    <div
      class="flex gap-1 overflow-x-auto border-t border-[var(--border-subtle)] px-3 py-2 lg:hidden"
    >
      <button
        v-for="item in mobileItems"
        :key="item.name"
        type="button"
        class="shrink-0 rounded-full px-3.5 py-1.5 text-xs font-medium transition-colors"
        :style="
          activeName === item.name
            ? 'background:var(--accent);color:var(--accent-contrast)'
            : 'color:var(--text-secondary)'
        "
        @click="router.push({ name: item.route })"
      >
        {{ t(item.labelKey) }}
      </button>
    </div>

    <CommandPaletteModal :open="searchOpen" @close="searchOpen = false" />
  </header>
</template>
