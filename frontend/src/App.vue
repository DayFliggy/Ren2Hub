<script setup lang="ts">
import { RouterView } from 'vue-router'
import { navigationError } from '@/router/navigationState'

import AppErrorBoundary from '@/components/common/AppErrorBoundary.vue'
import NavigationErrorState from '@/components/common/NavigationErrorState.vue'
import ToastHost from '@/components/common/ToastHost.vue'
import { useAccountLanguageAdoption } from '@/composables/useAccountLanguage'
import { useTheme } from '@/composables/useTheme'

useTheme()
useAccountLanguageAdoption()
</script>

<template>
  <AppErrorBoundary>
    <NavigationErrorState v-if="navigationError" :failure="navigationError" />
    <RouterView v-else />
  </AppErrorBoundary>
  <ToastHost />
</template>
