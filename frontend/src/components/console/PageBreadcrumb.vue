<script setup lang="ts">
/**
 * Standalone breadcrumb row for console pages that don't render a hero title.
 * Thin wrapper over the shared Breadcrumb (its own bottom spacing).
 */
import Breadcrumb from './Breadcrumb.vue'

withDefaults(
  defineProps<{
    crumbs?: string[]
  }>(),
  { crumbs: () => [] }
)
</script>

<template>
  <Breadcrumb :crumbs="crumbs" spacing="mb-6" />
</template>
