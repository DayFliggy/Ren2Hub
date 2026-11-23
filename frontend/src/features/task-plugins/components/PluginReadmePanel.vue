<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import MarkdownDocument from '@/components/common/MarkdownDocument.vue'
import type { MarketplacePlugin } from '../types'
import { fetchPluginReadme, type PluginReadme } from '../lib/plugin-readme'
const props = defineProps<{
  plugin: MarketplacePlugin
  indexUrl: string
  version: string
}>()
const { t, locale } = useI18n()
const readme = ref<PluginReadme | null>(null)
const error = ref('')
let controller: AbortController | undefined
async function load() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  readme.value = null
  error.value = ''
  try {
    const result = await fetchPluginReadme({
      indexUrl: props.indexUrl,
      plugin: props.plugin,
      version: props.version,
      language: locale.value,
      signal: request.signal,
    })
    if (!request.signal.aborted) readme.value = result
  } catch (cause) {
    if (!request.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  }
}
watch(
  () => [props.plugin.key, props.indexUrl, props.version, locale.value],
  load,
  { immediate: true }
)
onBeforeUnmount(() => controller?.abort())
</script>
<template>
  <section v-if="readme || error" class="sibling-space-y-3">
    <h3 class="font-medium">{{ t('pluginExtensions.readme') }}</h3>
    <ErrorBanner v-if="error" :message="error" @retry="load" />
    <MarkdownDocument
      v-else-if="readme"
      :markdown="readme.markdown"
      :base-url="readme.sourceUrl"
    />
  </section>
</template>
