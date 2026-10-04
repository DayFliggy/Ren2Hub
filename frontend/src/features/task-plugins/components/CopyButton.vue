<script setup lang="ts">
// Upstream @/components/copy-button, built on Ren2Hub's IconButton and toast.
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Copy } from 'lucide-vue-next'

import IconButton from '@/components/common/IconButton.vue'
import { useToast } from '@/composables/useToast'

const props = defineProps<{ value: string; label: string }>()
const { t } = useI18n()
const toast = useToast()
const copied = ref(false)

async function copy() {
  try {
    await navigator.clipboard.writeText(props.value)
    copied.value = true
    window.setTimeout(() => (copied.value = false), 1500)
  } catch {
    toast.error(t('common.copyFailed'))
  }
}
</script>

<template>
  <IconButton :label="label" class="shrink-0" @click="copy">
    <Check v-if="copied" :size="14" aria-hidden="true" />
    <Copy v-else :size="14" aria-hidden="true" />
  </IconButton>
</template>
