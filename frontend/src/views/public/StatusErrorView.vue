<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { House, LogIn, RefreshCw } from 'lucide-vue-next'
import PublicPage from '@/components/public/PublicPage.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { publicPageMessages } from '@/i18n/publicPages'
withDefaults(defineProps<{ status?: 401 | 403 | 404 | 500 | 503 }>(), {
  status: 404,
})
const { t } = useI18n({ useScope: 'local', messages: publicPageMessages })
function reload() {
  window.location.reload()
}
</script>

<template>
  <PublicPage :title="String(status)">
    <section
      class="flex min-h-80 flex-col items-center justify-center gap-6 text-center"
    >
      <h2 class="text-2xl font-semibold">
        {{ t(`publicPage.status${status}`) }}
      </h2>
      <div class="flex flex-wrap justify-center gap-4">
        <RouterLink
          :to="{ name: 'home' }"
          class="inline-flex min-h-11 items-center gap-2 text-[var(--accent-text)]"
          ><House :size="18" />{{ t('publicPage.home') }}</RouterLink
        >
        <RouterLink
          v-if="status === 401"
          :to="{ name: 'sign-in' }"
          class="inline-flex min-h-11 items-center gap-2 text-[var(--accent-text)]"
          ><LogIn :size="18" />{{ t('publicPage.signIn') }}</RouterLink
        >
        <ConsoleButton v-if="status >= 500" variant="secondary" @click="reload"
          ><RefreshCw :size="16" />{{ t('publicPage.retry') }}</ConsoleButton
        >
      </div>
    </section>
  </PublicPage>
</template>
