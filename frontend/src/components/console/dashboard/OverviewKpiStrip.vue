<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import StatTileGrid from '@/components/common/StatTileGrid.vue'
import type { DashboardStats } from '@/composables/useDashboard'
import { formatQuota, formatNumber } from '@/utils/format'
const props = defineProps<{
    stats: DashboardStats | null
    loading?: boolean
  }>(),
  { t } = useI18n()
const tiles = computed(() => [
  {
    label: t('dashboard.statTodaySpend'),
    value: props.stats ? formatQuota(props.stats.today_quota) : '—',
  },
  {
    label: t('dashboard.statTodayRequests'),
    value: props.stats ? formatNumber(props.stats.today_requests) : '—',
  },
  {
    label: t('dashboard.statTodayTokens'),
    value: props.stats ? formatNumber(props.stats.today_tokens ?? 0) : '—',
  },
])
</script>
<template><StatTileGrid :tiles="tiles" :columns="3" /></template>
