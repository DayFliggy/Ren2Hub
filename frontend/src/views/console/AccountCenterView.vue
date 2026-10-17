<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import PageHero from '@/components/console/PageHero.vue'
import StatTileGrid from '@/components/common/StatTileGrid.vue'
import AccountSettingsView from './AccountSettingsView.vue'
import { useAuthStore } from '@/stores/auth'
import { formatQuota, formatNumber } from '@/utils/format'
const { t } = useI18n(),
  auth = useAuthStore()
const stats = computed(() => [
  { label: t('profile.balance'), value: formatQuota(auth.user?.quota ?? 0) },
  {
    label: t('profile.totalUsage'),
    value: formatQuota(auth.user?.used_quota ?? 0),
  },
  {
    label: t('profile.apiRequests'),
    value: formatNumber(auth.user?.request_count ?? 0),
  },
])
</script>
<template>
  <div>
    <PageHero
      :title="auth.user?.display_name || auth.user?.username || ''"
      :crumbs="[t('nav.profile')]"
    /><StatTileGrid
      :tiles="stats"
      :columns="3"
      class="mb-6"
    /><AccountSettingsView embedded />
  </div>
</template>
