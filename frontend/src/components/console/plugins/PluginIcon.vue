<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api } from '@/api/client'
import { readMarketIcon } from '@/api/taskPlugins'
import MetadataIcon from '@/components/admin/MetadataIcon.vue'

const props = defineProps<{
  pluginKey: string
  name: string
  icon?: string
  hasIcon?: boolean
  sourceUrl?: string
}>()
const imageURL = ref('')
const failed = ref(false)
let objectURL = ''
let controller: AbortController | undefined
const text = computed(() => {
  if (props.icon === 'text' || props.icon?.startsWith('text:'))
    return [...(props.icon.slice(5).trim() || props.name || props.pluginKey)]
      .slice(0, 4)
      .join('')
  return ''
})
function clear() {
  controller?.abort()
  if (objectURL) URL.revokeObjectURL(objectURL)
  objectURL = ''
}
watch(
  () => [props.pluginKey, props.hasIcon, props.sourceUrl],
  async () => {
    clear()
    failed.value = false
    imageURL.value = ''
    if (!props.hasIcon && !props.sourceUrl) return
    const request = new AbortController()
    controller = request
    try {
      const blob = props.sourceUrl
        ? await readMarketIcon(props.sourceUrl, request.signal)
        : await api.getBlob(
            `/api/plugin/task/${encodeURIComponent(props.pluginKey)}/icon`,
            { signal: request.signal }
          )
      if (request.signal.aborted) return
      if (
        !['image/png', 'image/svg+xml'].includes(blob.type) ||
        blob.size > 512 * 1024
      )
        throw new Error('Invalid plugin icon')
      objectURL = URL.createObjectURL(blob)
      imageURL.value = objectURL
    } catch {
      if (!request.signal.aborted) failed.value = true
    }
  },
  { immediate: true }
)
onBeforeUnmount(clear)
</script>

<template>
  <span
    class="inline-flex h-9 w-9 shrink-0 items-center justify-center overflow-hidden rounded-lg border border-[var(--border-subtle)] bg-[var(--accent-soft)] text-xs font-semibold text-[var(--accent-text)]"
    aria-hidden="true"
  >
    <img
      v-if="imageURL && !failed"
      :src="imageURL"
      alt=""
      referrerpolicy="no-referrer"
      class="h-full w-full object-contain"
      @error="failed = true"
    />
    <span v-else-if="text">{{ text }}</span>
    <MetadataIcon v-else :icon="icon" :name="name || pluginKey" :size="36" />
  </span>
</template>
