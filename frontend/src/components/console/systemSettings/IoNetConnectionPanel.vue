<script setup lang="ts">
import { ref } from 'vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { api } from '@/api/console'
import { useToast } from '@/composables/useToast'

const loading = ref(false)
const toast = useToast()
async function testConnection() {
  loading.value = true
  try {
    await api.post('/api/deployments/settings/test-connection', {})
    toast.success('iONet 连接成功')
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="ionet-panel">
    <ConsoleButton variant="secondary" size="sm" :loading="loading" @click="testConnection">测试 iONet 连接</ConsoleButton>
  </div>
</template>

<style scoped>
.ionet-panel { margin-top: 1.5rem; border-top: 1px dashed var(--border-default); padding-top: 1rem; }
</style>
