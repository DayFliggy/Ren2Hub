<template>
  <span class="relative flex h-2 w-2" aria-hidden="true">
    <span
      v-if="online"
      class="absolute inline-flex h-full w-full animate-ping rounded-full opacity-70"
      style="background: var(--status-success)"
    />
    <span
      class="relative inline-flex h-2 w-2 rounded-full"
      :style="{
        background: online ? 'var(--status-success)' : 'var(--text-tertiary)',
      }"
    />
  </span>
</template>

<script setup lang="ts">
import { storeToRefs } from 'pinia'

import { useAppStore } from '@/stores'

const { online } = storeToRefs(useAppStore())
</script>
