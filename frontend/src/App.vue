<script setup lang="ts">
import { RouterView } from 'vue-router'

import ToastHost from '@/components/common/ToastHost.vue'
import { useTheme } from '@/composables/useTheme'

useTheme()
</script>

<template>
  <RouterView />
  <ToastHost />
</template>
