<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import {
  RouterView,
  useRoute,
  useRouter,
  type RouteLocationNormalized,
} from 'vue-router'
import { sanitizeRedirect } from '@/router'
import { navigationError } from '@/router/navigationState'
import { useAuthStore } from '@/stores/auth'

import AppErrorBoundary from '@/components/common/AppErrorBoundary.vue'
import NavigationErrorState from '@/components/common/NavigationErrorState.vue'
import NavigationProgress from '@/components/common/NavigationProgress.vue'
import ToastHost from '@/components/common/ToastHost.vue'
import { useAccountLanguageAdoption } from '@/composables/useAccountLanguage'
import { useTheme } from '@/composables/useTheme'

useTheme()
useAccountLanguageAdoption()

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const protectedSessionExpired = computed(
  () => route.meta.requiresAuth && auth.checked && !auth.isAuthenticated
)

// Pages of one shell share their chrome (the public navbar, the auth panel,
// the console frame), so only a change of shell fades the whole page in.
function shellOf(location: RouteLocationNormalized): string {
  if (location.meta.requiresAuth) return 'console'
  if (location.meta.messageDomain === 'auth') return 'auth'
  if (location.meta.setupRoute || location.meta.setupError) return 'setup'
  return 'public'
}
const shellSwap = ref<'shell' | 'shell-page'>('shell-page')
router.afterEach((to, from) => {
  shellSwap.value = shellOf(to) === shellOf(from) ? 'shell-page' : 'shell'
})

watch(
  protectedSessionExpired,
  async (expired) => {
    if (!expired) return
    // Remove protected content and its draft guards before navigation. A
    // pending peer-session refresh (checked=false) is not a confirmed logout.
    await nextTick()
    if (!protectedSessionExpired.value) return
    await router.replace({
      name: 'sign-in',
      query: { redirect: sanitizeRedirect(route.fullPath) ?? '/dashboard' },
    })
  },
  { immediate: true, flush: 'post' }
)
</script>

<template>
  <AppErrorBoundary>
    <NavigationErrorState v-if="navigationError" :failure="navigationError" />
    <RouterView v-else-if="!protectedSessionExpired" v-slot="{ Component }">
      <Transition :name="shellSwap">
        <component :is="Component" />
      </Transition>
    </RouterView>
  </AppErrorBoundary>
  <NavigationProgress />
  <ToastHost />
</template>

<style>
/* The leaving page is dropped at once; only an arriving shell fades in. */
.shell-leave-active,
.shell-page-leave-active {
  display: none !important;
}

.shell-enter-active {
  animation: shell-fade-in 0.16s ease-out;
}

@keyframes shell-fade-in {
  from {
    opacity: 0;
  }
}
</style>
