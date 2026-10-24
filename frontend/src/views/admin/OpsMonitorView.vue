<script setup lang="ts">
import OpsMonitorContent from '@/components/admin/ops/OpsMonitorContent.vue'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
</script>

<template>
  <OpsMonitorContent v-if="auth.isRoot" :key="auth.user?.id" />
</template>
