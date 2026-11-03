<script setup lang="ts">
import { computed, onBeforeMount, onBeforeUnmount, ref, watch } from 'vue'
import { RouterView, useRoute } from 'vue-router'

import ConsoleSidebar from '@/components/console/ConsoleSidebar.vue'
import ConsoleTopbar from '@/components/console/ConsoleTopbar.vue'
import { useAppStore } from '@/stores'
import '@/styles/console.css'

const route = useRoute()
const app = useAppStore()
const noPageScroll = computed(() => Boolean(route.meta.noPageScroll))
const featureStatus = computed(() => app.featureStatus(route.meta.feature))
const isDisabled = computed(() => featureStatus.value === 'disabled')

const isScrolling = ref(false)
const scrollZone = ref<HTMLElement | null>(null)
let scrollTimer: ReturnType<typeof setTimeout> | null = null

function onScroll() {
  isScrolling.value = true
  if (scrollTimer) clearTimeout(scrollTimer)
  scrollTimer = setTimeout(() => {
    isScrolling.value = false
  }, 2300)
}

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
})

onBeforeUnmount(() => {
  document.documentElement.removeAttribute('data-console-layout')
  if (scrollTimer) clearTimeout(scrollTimer)
})
</script>

<template>
  <div
    class="flex h-screen h-dvh overflow-hidden"
    data-handdrawn-scope="console"
    :data-feature-status="featureStatus"
  >
    <ConsoleSidebar />
    <div class="flex min-w-0 flex-1 flex-col overflow-hidden">
      <ConsoleTopbar data-console-scroll-boundary />
      <div
        ref="scrollZone"
        data-console-scroll-zone
        class="night-page-texture scroll-zone texture-paper draft-grid min-w-0 flex-1 overflow-y-auto"
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
        <main class="min-w-0 px-4 py-8 sm:px-6 lg:px-8">
          <div
            class="console-content mx-auto transition-[max-width] duration-300"
            :class="{ 'console-content--wide': route.meta.wide }"
          >
            <RouterView v-slot="{ Component }">
              <Transition name="page" mode="out-in">
                <component :is="Component" />
              </Transition>
            </RouterView>
          </div>
        </main>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page-enter-active {
  transition: all 0.22s ease;
}
.page-leave-active {
  transition: all 0.15s ease;
}
.page-enter-from {
  opacity: 0;
  transform: translateY(8px);
}
.page-leave-to {
  opacity: 0;
}
</style>
