<script setup lang="ts">
// Upstream components/plugin-changelog-panel.tsx. Entries were validated to
// inline-only Markdown, so they render as Vue nodes instead of HTML.
import {
  h,
  onBeforeUnmount,
  ref,
  watch,
  type FunctionalComponent,
  type VNode,
} from 'vue'
import { useI18n } from 'vue-i18n'
import { ExternalLink, History } from 'lucide-vue-next'

import ErrorBanner from '@/components/common/ErrorBanner.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'

import {
  changelogEntries,
  changelogLocale,
  fetchPluginChangelog,
  type ChangelogInline,
  type PluginChangelog,
} from '../lib/plugin-changelog'
import type { MarketplacePlugin } from '../types'

const props = defineProps<{
  indexUrl: string
  plugin: MarketplacePlugin
  version: string
}>()
const { t, locale } = useI18n()
const changelog = ref<PluginChangelog | null>(null)
const loading = ref(true)
const failed = ref(false)
let controller: AbortController | undefined

async function load() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  failed.value = false
  changelog.value = null
  try {
    const result = await fetchPluginChangelog({
      indexUrl: props.indexUrl,
      plugin: props.plugin,
      version: props.version,
      language: changelogLocale(locale.value),
      signal: request.signal,
    })
    if (!request.signal.aborted) changelog.value = result
  } catch {
    // The panel shows a retryable error; the reason is not actionable here.
    if (!request.signal.aborted) failed.value = true
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}
watch(
  () => [props.indexUrl, props.plugin.key, props.version, locale.value],
  load,
  {
    immediate: true,
  }
)
onBeforeUnmount(() => controller?.abort())

function render(node: ChangelogInline): VNode | string {
  if (node.type === 'text') return node.text
  if (node.type === 'break') return h('br')
  if (node.type === 'code')
    return h(
      'code',
      { class: 'rounded-sm bg-[var(--surface-muted)] px-1 font-mono text-xs' },
      node.text
    )
  if (node.type === 'link')
    return h(
      'a',
      {
        href: node.href,
        target: '_blank',
        rel: 'noopener noreferrer',
        class: 'text-[var(--accent-text)] underline',
      },
      node.children.map(render)
    )
  return h(node.type === 'del' ? 's' : node.type, node.children.map(render))
}
const Inline: FunctionalComponent<{ nodes: ChangelogInline[] }> = (inline) =>
  inline.nodes.map(render)
Inline.props = ['nodes']
</script>

<template>
  <SkeletonBlock v-if="loading" rounded="lg" class="h-48" />
  <ErrorBanner
    v-else-if="failed"
    :message="`${t('taskPlugins.changelogLoadFailed')}: ${t('taskPlugins.changelogLoadFailedDesc')}`"
    @retry="load"
  />
  <div
    v-else-if="!changelog"
    class="flex min-h-48 flex-col items-center justify-center gap-2 text-sm text-[var(--text-tertiary)]"
  >
    <History :size="20" aria-hidden="true" />
    {{ t('taskPlugins.changelogEmpty') }}
  </div>
  <div v-else class="min-w-0 sibling-space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <span class="font-mono text-xs text-[var(--text-tertiary)]"
        >v{{ changelog.version }}</span
      >
      <a
        :href="changelog.sourceUrl"
        target="_blank"
        rel="noopener noreferrer"
        class="inline-flex items-center gap-1.5 text-xs text-[var(--text-tertiary)] hover:text-[var(--text-primary)]"
      >
        <ExternalLink :size="14" aria-hidden="true" />
        {{ t('taskPlugins.changelogSource') }}
      </a>
    </div>
    <p
      v-if="changelogLocale(locale) !== 'en' && changelog.locale === 'en'"
      role="status"
      class="text-xs text-[var(--text-tertiary)]"
    >
      {{ t('taskPlugins.changelogFallback') }}
    </p>
    <section
      v-for="section in changelog.sections"
      :key="section.category"
      class="sibling-space-y-2"
    >
      <h3 class="font-medium">
        {{ t(`taskPlugins.changelogCategories.${section.category}`) }}
      </h3>
      <ul
        :lang="changelog.locale"
        class="list-disc sibling-space-y-1 pl-5 text-sm"
      >
        <li
          v-for="(entry, index) in changelogEntries(section.markdown)"
          :key="index"
        >
          <Inline :nodes="entry" />
        </li>
      </ul>
    </section>
  </div>
</template>
