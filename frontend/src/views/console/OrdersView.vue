<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import TopupRecords from '@/components/console/wallet/TopupRecords.vue'
const { t } = useI18n()
</script>
<template>
  <div>
    <PageBreadcrumb :crumbs="[t('orders.title')]" /><TopupRecords admin />
  </div>
</template>
