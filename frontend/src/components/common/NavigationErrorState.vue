<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { House, RefreshCw } from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import type { NavigationFailure } from '@/router/navigationState'

const props = defineProps<{ failure: NavigationFailure }>()
const { t } = useI18n()
const rateLimited = computed(() => props.failure === 'rate-limited')

function reload() {
  window.location.reload()
}
</script>

<template>
  <main
    role="alert"
    class="flex min-h-screen flex-col items-center justify-center gap-6 bg-[var(--page-background)] p-6 text-center text-[var(--text-primary)]"
  >
    <h1 class="text-2xl font-semibold">
      {{
        rateLimited
          ? t('common.navigationError.rateLimitedTitle')
          : t('common.navigationError.title')
      }}
    </h1>
    <p class="max-w-md text-[var(--text-secondary)]">
      {{
        rateLimited
          ? t('common.navigationError.rateLimitedMessage')
          : t('common.navigationError.message')
      }}
    </p>
    <div class="flex flex-wrap items-center justify-center gap-3">
      <ConsoleButton data-navigation-retry @click="reload"
        ><RefreshCw :size="18" />{{ t('common.retry') }}</ConsoleButton
      >
      <!-- A full load of the home page also resets a router whose initial
           navigation failed, including when the failed navigation started at home. -->
      <a
        href="/"
        class="pencil-control sketch-sm inline-flex min-h-11 items-center justify-center gap-2 border border-[var(--border-default)] bg-[var(--surface-solid)] px-4 text-sm font-semibold text-[var(--text-primary)] hover:bg-[var(--state-hover-layer)] focus-ring sm:h-10 sm:min-h-10"
        data-handdrawn="control"
        data-navigation-home
        ><House :size="18" />{{ t('common.navigationError.home') }}</a
      >
    </div>
  </main>
</template>
