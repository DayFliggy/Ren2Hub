<script setup lang="ts">
import { RouterView } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { RefreshCw } from 'lucide-vue-next'
import { navigationError, navigationPending } from '@/router/navigationState'
import ConsoleButton from '@/components/common/ConsoleButton.vue'

import AppErrorBoundary from '@/components/common/AppErrorBoundary.vue'
import ToastHost from '@/components/common/ToastHost.vue'
import { useTheme } from '@/composables/useTheme'

useTheme()
const { t } = useI18n()
function reload() {
  window.location.reload()
}
</script>

<template>
  <AppErrorBoundary>
    <main
      v-if="navigationError"
      role="alert"
      class="flex min-h-screen flex-col items-center justify-center gap-6 bg-[var(--page-background)] p-6 text-[var(--text-primary)]"
    >
      <h1 class="text-2xl font-semibold">{{ t('common.appError.title') }}</h1>
      <p>{{ t('common.appError.message') }}</p>
      <ConsoleButton @click="reload"
        ><RefreshCw :size="18" />{{ t('common.retry') }}</ConsoleButton
      >
    </main>
    <main
      v-else-if="navigationPending"
      role="status"
      class="flex min-h-screen items-center justify-center bg-[var(--page-background)] p-6 text-[var(--text-primary)]"
    >
      {{ t('common.loading') }}
    </main>
    <RouterView v-else />
  </AppErrorBoundary>
  <ToastHost />
</template>
