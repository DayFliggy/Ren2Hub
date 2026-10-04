<script setup lang="ts">
// Upstream components/plugin-icon.tsx. An <img> cannot carry the console
// session, so a gateway-held logo is fetched through the API client and shown
// as an object URL; SVG stays in the browser's inert image mode.
import { computed, onBeforeUnmount, ref, watch } from 'vue'

import { api } from '@/api/client'
import MetadataIcon from '@/components/admin/MetadataIcon.vue'

import {
  pluginIconUrl,
  resolvePluginIcon,
  textAvatarTone,
  type PluginIconInput,
} from '../lib/plugin-icon'

const props = withDefaults(
  defineProps<{ plugin: PluginIconInput; size?: number }>(),
  { size: 20 }
)
const gatewaySrc = ref('')
const failedSrc = ref('')
let objectUrl = ''
let controller: AbortController | undefined

function release() {
  controller?.abort()
  if (objectUrl) URL.revokeObjectURL(objectUrl)
  objectUrl = ''
}

watch(
  () => [props.plugin.key, props.plugin.hasIcon, props.plugin.iconSrc],
  async () => {
    release()
    gatewaySrc.value = ''
    if (!props.plugin.hasIcon || props.plugin.iconSrc) return
    const request = new AbortController()
    controller = request
    try {
      const blob = await api.getBlob(pluginIconUrl(props.plugin.key), {
        signal: request.signal,
      })
      if (request.signal.aborted || !blob.type.startsWith('image/')) return
      objectUrl = URL.createObjectURL(blob)
      gatewaySrc.value = objectUrl
    } catch {
      // A missing logo falls back to the manifest icon or a text avatar.
    }
  },
  { immediate: true }
)
onBeforeUnmount(release)

const descriptor = computed(() => {
  const resolved = resolvePluginIcon({
    ...props.plugin,
    iconSrc: props.plugin.iconSrc || gatewaySrc.value || undefined,
  })
  if (resolved.kind === 'image' && resolved.src === failedSrc.value)
    return resolvePluginIcon({ ...props.plugin, iconSrc: undefined })
  return resolved
})

const toneClass = {
  accent: 'bg-[var(--accent-soft)] text-[var(--accent-text)]',
  success: 'bg-[var(--status-success-soft)] text-[var(--status-success-text)]',
  info: 'bg-[var(--status-info-soft)] text-[var(--status-info-text)]',
  warning: 'bg-[var(--status-warning-soft)] text-[var(--status-warning-text)]',
  danger: 'bg-[var(--status-danger-soft)] text-[var(--status-danger-text)]',
}
</script>

<template>
  <MetadataIcon
    v-if="descriptor.kind === 'lobe'"
    :icon="descriptor.name"
    :name="plugin.name || plugin.key"
    :size="size"
  />
  <span
    v-else-if="descriptor.kind === 'image'"
    aria-hidden="true"
    class="inline-flex shrink-0 items-center justify-center overflow-hidden rounded-md bg-[var(--surface-muted)]"
    :style="{ width: `${size}px`, height: `${size}px` }"
  >
    <img
      :src="descriptor.src"
      alt=""
      :width="size"
      :height="size"
      draggable="false"
      referrerpolicy="no-referrer"
      class="h-full w-full object-contain"
      @error="failedSrc = descriptor.src"
    />
  </span>
  <span
    v-else
    aria-hidden="true"
    class="inline-flex shrink-0 select-none items-center justify-center rounded-md font-semibold"
    :class="toneClass[textAvatarTone(descriptor.colorSeed)]"
    :style="{
      width: `${size}px`,
      height: `${size}px`,
      fontSize: `${Math.max(8, Math.floor(size * 0.42))}px`,
    }"
    >{{ descriptor.label }}</span
  >
</template>
