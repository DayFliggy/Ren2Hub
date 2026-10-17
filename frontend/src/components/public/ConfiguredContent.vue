<script setup lang="ts">
import { computed } from 'vue'
import { useTheme } from '@/composables/useTheme'
import { publicDocumentMarkup, publicDocumentUrl } from './documentMarkup'
const props = withDefaults(
  defineProps<{
    content: string
    title: string
    variant?: 'page' | 'footer'
  }>(),
  { variant: 'page' }
)
const { resolvedTheme } = useTheme()
const url = computed(() => publicDocumentUrl(props.content))
const markup = computed(() => {
  const scheme = resolvedTheme.value
  const styles = getComputedStyle(document.documentElement)
  const footer = props.variant === 'footer'
  return publicDocumentMarkup(props.content, {
    scheme,
    background: styles
      .getPropertyValue(footer ? '--surface-footer' : '--page-background')
      .trim(),
    foreground: styles
      .getPropertyValue(footer ? '--footer-text-primary' : '--text-primary')
      .trim(),
    accent: styles
      .getPropertyValue(footer ? '--footer-accent' : '--accent-text')
      .trim(),
  })
})
</script>
<template>
  <iframe
    :src="url || undefined"
    :srcdoc="url ? undefined : markup"
    :title="title"
    sandbox="allow-popups allow-popups-to-escape-sandbox"
    referrerpolicy="no-referrer"
    class="w-full border-0"
  />
</template>
