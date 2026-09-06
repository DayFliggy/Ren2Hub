<script setup lang="ts">
import { computed } from 'vue'
import VendorLogo from '@/components/console/models/VendorLogo.vue'
import { vendorLogoMeta } from '@/constants/console'
const props = withDefaults(
  defineProps<{ icon?: string; name: string; size?: number }>(),
  { icon: '', size: 32 }
)
const aliases: Record<string, string> = {
  claude: 'Anthropic',
  gemini: 'Google',
  qwen: 'Alibaba',
  grok: 'xAI',
  kimi: 'Moonshot',
  zhipu: 'Zhipu AI',
  glm: 'Zhipu AI',
  hunyuan: 'Tencent',
  doubao: 'Bytedance Seed',
}
const vendor = computed(() => {
  const key = props.icon.split('.')[0]?.toLowerCase() ?? ''
  return (
    Object.keys(vendorLogoMeta).find((name) => name.toLowerCase() === key) ??
    aliases[key] ??
    props.name
  )
})
</script>
<template><VendorLogo :vendor="vendor" :size="size" /></template>
