<script setup lang="ts">
import { computed, nextTick, watch } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import { sanitizeRedirect } from '@/router'
import { navigationError } from '@/router/navigationState'
import { useAuthStore } from '@/stores/auth'

import AppErrorBoundary from '@/components/common/AppErrorBoundary.vue'
import NavigationErrorState from '@/components/common/NavigationErrorState.vue'
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
    <RouterView v-else-if="!protectedSessionExpired" />
  </AppErrorBoundary>
  <ToastHost />
</template>
