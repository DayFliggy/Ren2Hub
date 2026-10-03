<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ShieldCheck } from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import PageHero from '@/components/console/PageHero.vue'
import PreferencesNotificationsPanel from '@/components/console/PreferencesNotificationsPanel.vue'
import { useAuthStore } from '@/stores/auth'

// Credentials, verification, sessions and tokens live only on /security.
// This view keeps notification and privacy preferences plus the way there.
const props = withDefaults(defineProps<{ embedded?: boolean }>(), {
  embedded: false,
})

const { t } = useI18n()
const router = useRouter()
const auth = useAuthStore()
</script>

<template>
  <div class="space-y-6" data-handdrawn-page="settings">
    <PageHero
      v-if="!props.embedded"
      :title="t('settings.title')"
      :crumbs="[t('nav.profile'), t('settings.title')]"
    >
      <p class="mt-1 max-w-2xl text-sm text-[var(--text-tertiary)]">
        {{ t('settings.pageSubtitle') }}
      </p>
    </PageHero>

    <div
      class="flex flex-col items-start justify-between gap-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-4 sm:flex-row sm:items-center sm:px-6"
      data-security-entry
    >
      <div class="flex items-center gap-3">
        <ShieldCheck
          :size="20"
          class="shrink-0 text-[var(--accent-text)]"
          aria-hidden="true"
        />
        <div>
          <p class="text-sm font-semibold text-[var(--text-primary)]">
            {{ t('nav.securityAccess') }}
          </p>
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ t('security.page.subtitle') }}
          </p>
        </div>
      </div>
      <ConsoleButton
        variant="secondary"
        size="sm"
        @click="router.push({ name: 'security' })"
      >
        {{ t('security.page.manage') }}
      </ConsoleButton>
    </div>

    <PreferencesNotificationsPanel :is-admin="auth.isAdmin" />
  </div>
</template>
