<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  fetchPluginChangelog,
  type PluginChangelog,
} from '@/api/pluginChangelog'
import type { MarketPlugin } from '@/api/taskPlugins'
const props = defineProps<{ plugin: MarketPlugin; version: string }>()
const { t, locale } = useI18n()
const entry = ref<PluginChangelog | null>(null)
const loading = ref(false)
const error = ref('')
let controller: AbortController | undefined
async function load() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  error.value = ''
  entry.value = null
  try {
    const result = await fetchPluginChangelog({
      indexUrl: props.plugin.indexURL,
      plugin: props.plugin,
      version: props.version,
      language: locale.value,
      signal: request.signal,
    })
    if (!request.signal.aborted) entry.value = result
  } catch (cause) {
    if (!request.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}
watch(() => [props.plugin, props.version, locale.value], load, {
  immediate: true,
})
onBeforeUnmount(() => controller?.abort())
</script>

<template>
  <div class="space-y-3 text-sm">
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <div
      v-else-if="error"
      role="alert"
      class="break-words text-[var(--status-danger)]"
    >
      <p>{{ t('taskPlugins.changelogError') }}: {{ error }}</p>
      <button type="button" class="underline" @click="load">
        {{ t('taskPlugins.refresh') }}
      </button>
    </div>
    <p v-else-if="!entry">{{ t('taskPlugins.changelogEmpty') }}</p>
    <template v-else>
      <p
        v-if="locale !== 'en' && entry.locale === 'en'"
        role="status"
        class="text-[var(--text-secondary)]"
      >
        {{ t('taskPlugins.changelogFallback') }}
      </p>
      <section v-for="section in entry.sections" :key="section.category">
        <h3 class="font-semibold">
          {{ t(`taskPlugins.changelogCategories.${section.category}`) }}
        </h3>
        <p class="mt-1 whitespace-pre-wrap break-words" :lang="entry.locale">
          {{ section.markdown }}
        </p>
      </section>
      <a
        :href="entry.sourceUrl"
        target="_blank"
        rel="noopener noreferrer"
        class="inline-block underline"
        >{{ t('taskPlugins.changelogSource') }}</a
      >
    </template>
  </div>
</template>
