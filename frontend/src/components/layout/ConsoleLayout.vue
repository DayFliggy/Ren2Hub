<script setup lang="ts">
import { computed, onBeforeMount, onBeforeUnmount, ref, watch } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'

import ConsoleSidebar from '@/components/console/ConsoleSidebar.vue'
import ConsoleTopbar from '@/components/console/ConsoleTopbar.vue'
import { useAppStore } from '@/stores'
import '@/styles/console.css'

const route = useRoute()
const router = useRouter()
const app = useAppStore()
const noPageScroll = computed(() => Boolean(route.meta.noPageScroll))
const featureStatus = computed(() => app.featureStatus(route.meta.feature))
const isDisabled = computed(() => featureStatus.value === 'disabled')

const isScrolling = ref(false)
const scrollZone = ref<HTMLElement | null>(null)
let scrollTimer: ReturnType<typeof setTimeout> | null = null
const nativeFullscreen = ref(false)

function syncNativeFullscreen() {
  nativeFullscreen.value =
    typeof document !== 'undefined' && Boolean(document.fullscreenElement)
}

const isOpsFullscreen = computed(
  () =>
    Boolean(route.meta.fullscreenable) &&
    (route.query.fullscreen === '1' ||
      route.query.fullscreen === 'true' ||
      nativeFullscreen.value)
)

function onScroll() {
  isScrolling.value = true
  if (scrollTimer) clearTimeout(scrollTimer)
  scrollTimer = setTimeout(() => {
    isScrolling.value = false
  }, 2300)
}

// Routes that are tabs of one page (the log pages) swap only what is below
// the shared breadcrumb row; any other navigation fades the whole page in.
const pageSwap = ref<'page' | 'tab'>('page')
const stopTrackingSwap = router.afterEach((to, from) => {
  pageSwap.value =
    to.meta.tabGroup !== undefined && to.meta.tabGroup === from.meta.tabGroup
      ? 'tab'
      : 'page'
})

// Pages scroll inside this zone, not the window the router's scrollBehavior
// resets. A new path starts at the top; query-only updates such as filters
// and pagination keep their place.
watch(
  () => route.path,
  () => scrollZone.value?.scrollTo({ top: 0 }),
  { flush: 'post' }
)

// Scope rem sizing to the console, including overlays teleported to body.
onBeforeMount(() => {
  document.documentElement.setAttribute('data-console-layout', '')
  syncNativeFullscreen()
  if (typeof document !== 'undefined') {
    document.addEventListener('fullscreenchange', syncNativeFullscreen)
  }
})

onBeforeUnmount(() => {
  stopTrackingSwap()
  document.documentElement.removeAttribute('data-console-layout')
  if (typeof document !== 'undefined') {
    document.removeEventListener('fullscreenchange', syncNativeFullscreen)
  }
  if (scrollTimer) clearTimeout(scrollTimer)
})
</script>

<template>
  <div
    class="flex h-screen h-dvh overflow-hidden"
    data-handdrawn-scope="console"
    :data-feature-status="featureStatus"
    :data-ops-fullscreen="isOpsFullscreen ? 'true' : undefined"
  >
    <ConsoleSidebar v-if="!isOpsFullscreen" />
    <div class="flex min-w-0 flex-1 flex-col overflow-hidden">
      <ConsoleTopbar v-if="!isOpsFullscreen" data-console-scroll-boundary />
      <div
        ref="scrollZone"
        data-console-scroll-zone
        class="night-page-texture scroll-zone texture-paper draft-grid min-w-0 flex-1 overflow-y-auto overscroll-contain"
        :class="{ scrolling: isScrolling, 'no-page-scroll': noPageScroll }"
        @scroll.passive="onScroll"
      >
        <div
          v-if="isDisabled"
          class="border-b border-[var(--status-warning)] bg-[var(--status-warning-soft)] px-4 py-2 text-center text-xs font-semibold text-[var(--status-warning-text)] sm:px-6 lg:px-8"
          role="status"
        >
          {{ $t('nav.disabledMode') }} · {{ $t('nav.disabledModeHint') }}
        </div>
        <main
          class="min-w-0 px-4 sm:px-6 lg:px-8"
          :class="isOpsFullscreen ? 'py-5' : 'py-8'"
        >
          <div
            class="console-content mx-auto"
            :class="{
              'console-content--wide': route.meta.wide,
              'max-w-none': isOpsFullscreen,
            }"
            :data-page-swap="pageSwap"
          >
            <RouterView v-slot="{ Component }">
              <Transition name="page">
                <component :is="Component" />
              </Transition>
            </RouterView>
          </div>
        </main>
      </div>
    </div>
  </div>
</template>
