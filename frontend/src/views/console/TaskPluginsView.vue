<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  taskPluginsApi,
  readMarketText,
  parseMarketIndex,
  marketSourceURL,
  pluginSourceHash,
  TaskPluginUsageError,
  type TaskPluginUsage,
  type TaskPlugin,
  type PluginVersion,
  type MarketSource,
  type MarketPlugin,
  type PluginMeta,
  type PluginRuntime,
} from '@/api/taskPlugins'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import PageHero from '@/components/console/PageHero.vue'
import PluginIcon from '@/components/console/plugins/PluginIcon.vue'
import PluginChangelog from '@/components/console/plugins/PluginChangelog.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import { localizedText } from '@/utils/localizedText'

const { t, locale } = useI18n()
const tab = ref<'installed' | 'market' | 'editor' | 'sources'>('installed')
const rows = ref<TaskPlugin[]>([]),
  versions = ref<PluginVersion[]>([]),
  sources = ref<MarketSource[]>([]),
  market = ref<MarketPlugin[]>([])
const runtime = ref<PluginRuntime | null>(null)
const disableTarget = ref<TaskPlugin | null>(null)
const disableUsage = ref<TaskPluginUsage | null>(null)
const disableError = ref('')
const disabling = ref(false)
const changelog = ref<{ plugin: MarketPlugin; version: string } | null>(null)
const selected = ref(''),
  source = ref(''),
  sourcesText = ref('[]'),
  enabled = ref(true),
  hook = ref('extractUsage'),
  member = ref(''),
  args = ref('[{}]'),
  debugResult = ref('')
const preview = ref<{
  meta: Partial<PluginMeta>
  source_hash: string
  unresolved: string[]
} | null>(null)
const selectedVersions = ref<Record<string, string>>({})
const busy = ref(false),
  error = ref(''),
  message = ref('')
const abort = new AbortController()
let sequence = 0
let detailSequence = 0
const readablePreview = computed(() =>
  JSON.stringify(preview.value?.meta ?? {}, null, 2)
)
watch(source, () => {
  preview.value = null
})
function description(row: TaskPlugin) {
  return localizedText(row.meta.description, locale.value)
}
async function action(work: () => Promise<void>) {
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    await work()
  } catch (cause) {
    if (!abort.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    busy.value = false
  }
}
function beginDisable(plugin: TaskPlugin) {
  disableTarget.value = plugin
  disableUsage.value = null
  disableError.value = ''
}
function closeDisable() {
  if (disabling.value) return
  disableTarget.value = null
  disableUsage.value = null
  disableError.value = ''
}
async function confirmDisable(options?: {
  cascade?: boolean
  force?: boolean
}) {
  if (!disableTarget.value || disabling.value) return
  const key = disableTarget.value.meta.key
  disabling.value = true
  disableError.value = ''
  try {
    await taskPluginsApi.enabled(key, false, options)
    if (abort.signal.aborted) return
    disableTarget.value = null
    disableUsage.value = null
    await action(async () => {
      await load()
      if (selected.value) await viewVersions(selected.value)
      message.value = t('taskPlugins.saved')
    })
  } catch (cause) {
    if (abort.signal.aborted) return
    if (cause instanceof TaskPluginUsageError) disableUsage.value = cause.usage
    else
      disableError.value =
        cause instanceof Error ? cause.message : String(cause)
  } finally {
    disabling.value = false
  }
}
async function load() {
  const current = ++sequence
  const result = await Promise.all([
    taskPluginsApi.list(abort.signal),
    taskPluginsApi.runtime(abort.signal),
    taskPluginsApi.sources(abort.signal),
  ])
  if (current !== sequence || abort.signal.aborted) return
  ;[rows.value, runtime.value, sources.value] = result
  sourcesText.value = JSON.stringify(sources.value, null, 2)
}
async function viewVersions(key: string) {
  const current = ++detailSequence
  const result = await taskPluginsApi.versions(key, abort.signal)
  if (current !== detailSequence || abort.signal.aborted) return
  selected.value = key
  versions.value = result
}
async function mutate(work: () => Promise<unknown>) {
  await work()
  await load()
  if (selected.value) await viewVersions(selected.value)
  message.value = t('taskPlugins.saved')
}
async function validate() {
  if (new TextEncoder().encode(source.value).length > 8 << 20)
    throw new Error(t('taskPlugins.sourceTooLarge'))
  const original = source.value
  const result = await taskPluginsApi.preview(original)
  if (original !== source.value) throw new Error(t('taskPlugins.draftStale'))
  preview.value = result
}
function editSource(version: PluginVersion) {
  source.value = version.source
  tab.value = 'editor'
}
async function install() {
  if (
    !preview.value ||
    (await pluginSourceHash(source.value)) !== preview.value.source_hash
  )
    throw new Error(t('taskPlugins.draftStale'))
  await mutate(() =>
    taskPluginsApi.upload(
      source.value,
      preview.value!.source_hash,
      enabled.value
    )
  )
  tab.value = 'installed'
}
async function fileSelected(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file) return
  await action(async () => {
    if (file.size > 8 << 20) throw new Error(t('taskPlugins.sourceTooLarge'))
    source.value = await file.text()
    tab.value = 'editor'
    await validate()
  })
}
async function loadMarket() {
  const result: MarketPlugin[] = []
  const failures: string[] = []
  for (const item of sources.value) {
    try {
      result.push(
        ...parseMarketIndex(
          JSON.parse(
            await readMarketText(item.index_url, 2 << 20, abort.signal)
          ),
          item
        )
      )
    } catch (cause) {
      failures.push(
        `${item.name}: ${cause instanceof Error ? cause.message : String(cause)}`
      )
    }
  }
  market.value = result
  error.value = failures.join('\n')
}
async function importMarket(item: MarketPlugin) {
  const version =
    item.versions.find(
      (v) =>
        v.version ===
        (selectedVersions.value[`${item.indexURL}:${item.key}`] || item.latest)
    ) || item.versions[0]!
  if ((version.minApiVersion ?? 1) > 1)
    throw new Error(t('taskPlugins.unsupported'))
  const text = await readMarketText(
    marketSourceURL(item.indexURL, version.path),
    8 << 20,
    abort.signal
  )
  if ((await pluginSourceHash(text)) !== version.sha256)
    throw new Error(t('taskPlugins.hashMismatch'))
  source.value = text
  tab.value = 'editor'
  await validate()
  if (
    preview.value?.meta.key !== item.key ||
    preview.value.meta.version !== version.version
  ) {
    preview.value = null
    throw new Error(t('taskPlugins.identityMismatch'))
  }
}
async function remove(version: PluginVersion) {
  if (
    window.confirm(t('taskPlugins.confirmDelete', { version: version.version }))
  )
    await mutate(() => taskPluginsApi.remove(version.key, version.version))
}
async function saveSources() {
  const parsed: unknown = JSON.parse(sourcesText.value)
  if (!Array.isArray(parsed)) throw new Error(t('taskPlugins.sourcesHelp'))
  for (const row of parsed) {
    if (
      !row ||
      typeof row.name !== 'string' ||
      typeof row.index_url !== 'string'
    )
      throw new Error(t('taskPlugins.sourcesHelp'))
    marketSourceURL(row.index_url, row.index_url)
  }
  await mutate(() => taskPluginsApi.saveSources(parsed))
}
async function runDebug() {
  const parsed: unknown = JSON.parse(args.value)
  if (!Array.isArray(parsed)) throw new Error(t('taskPlugins.args'))
  debugResult.value = JSON.stringify(
    await taskPluginsApi.dryRun(
      selected.value,
      hook.value,
      member.value,
      parsed
    ),
    null,
    2
  )
}
void action(load)
onBeforeUnmount(() => {
  sequence++
  detailSequence++
  abort.abort()
})
</script>

<template>
  <div class="space-y-5">
    <PageHero :title="t('taskPlugins.title')" :crumbs="[t('taskPlugins.title')]"
      ><p class="text-sm text-[var(--text-secondary)]">
        {{ t('taskPlugins.hint') }}
      </p></PageHero
    >
    <div
      class="flex flex-wrap gap-2"
      role="tablist"
      :aria-label="t('taskPlugins.title')"
    >
      <ConsoleButton
        v-for="name in ['installed', 'market', 'editor', 'sources'] as const"
        :key="name"
        role="tab"
        :aria-selected="tab === name"
        :variant="tab === name ? 'primary' : 'secondary'"
        @click="tab = name"
        >{{ t(`taskPlugins.${name}`) }}</ConsoleButton
      >
    </div>
    <p
      v-if="error"
      role="alert"
      class="whitespace-pre-wrap break-words text-sm text-[var(--status-danger)]"
    >
      {{ error }}
    </p>
    <p
      v-if="message"
      role="status"
      class="text-sm text-[var(--text-secondary)]"
    >
      {{ message }}
    </p>
    <ConsoleCard v-if="tab === 'installed'">
      <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
        <p v-if="runtime" class="text-sm">
          {{ t('taskPlugins.runtime') }}: {{ runtime.current_generation }} ·
          {{ runtime.last_rebuild.status }}
        </p>
        <ConsoleButton
          :loading="busy"
          variant="secondary"
          @click="action(load)"
          >{{ t('taskPlugins.refresh') }}</ConsoleButton
        >
      </div>
      <p class="mb-4 text-sm text-[var(--text-secondary)]">
        {{ t('taskPlugins.immutable') }}
      </p>
      <p v-if="!rows.length">{{ t('taskPlugins.empty') }}</p>
      <div class="grid gap-4 xl:grid-cols-2">
        <article
          v-for="row in rows"
          :key="row.meta.key"
          class="min-w-0 rounded-xl border border-[var(--border-subtle)] p-4"
        >
          <div class="flex items-center gap-3">
            <PluginIcon
              :plugin-key="row.meta.key"
              :name="row.meta.name"
              :icon="row.meta.icon"
              :has-icon="row.has_icon"
            />
            <h2 class="min-w-0 break-words font-semibold">
              {{ row.meta.name || row.meta.key }}
              <span class="text-xs text-[var(--text-muted)]"
                >{{ row.meta.version }} ·
                {{
                  row.source === 'factory'
                    ? t('taskPlugins.factory')
                    : t('taskPlugins.custom')
                }}</span
              >
            </h2>
          </div>
          <p class="mt-2 text-sm text-[var(--text-secondary)]">
            {{ description(row) }}
          </p>
          <p class="mt-2 break-words text-xs">
            {{ t('taskPlugins.status') }}: {{ row.runtime_status }} ·
            {{ t('taskPlugins.channels') }}: {{ row.channel_count }} ·
            {{ t('taskPlugins.inFlight') }}: {{ row.in_flight_count }}
          </p>
          <p
            v-if="row.runtime_error"
            class="break-words text-xs text-[var(--status-danger)]"
          >
            {{ row.runtime_error }}
          </p>
          <details class="mt-2">
            <summary class="cursor-pointer text-sm">
              {{ t('taskPlugins.models') }} ({{ row.meta.models?.length || 0 }})
            </summary>
            <p class="mt-2 break-words text-xs">
              {{ row.meta.models?.join(', ') }}
            </p>
          </details>
          <p class="mt-2 break-all font-mono text-[10px]">
            {{ row.source_hash }}
          </p>
          <div class="mt-3 flex flex-wrap gap-2">
            <ConsoleButton
              size="sm"
              variant="secondary"
              :disabled="busy"
              @click="action(() => viewVersions(row.meta.key))"
              >{{ t('taskPlugins.versions') }}</ConsoleButton
            ><ConsoleButton
              size="sm"
              variant="secondary"
              :disabled="busy"
              @click="
                row.enabled
                  ? beginDisable(row)
                  : action(() =>
                      mutate(() => taskPluginsApi.enabled(row.meta.key, true))
                    )
              "
              >{{
                t(row.enabled ? 'taskPlugins.disable' : 'taskPlugins.enable')
              }}</ConsoleButton
            >
          </div>
        </article>
      </div>
      <section
        v-if="selected"
        class="mt-6 space-y-3 border-t border-[var(--border-subtle)] pt-4"
      >
        <h2 class="font-semibold">
          {{ selected }} · {{ t('taskPlugins.versions') }}
        </h2>
        <RouterLink
          class="text-sm underline"
          :to="{ name: 'models-pricing' }"
          >{{ t('billing.modelPricing') }}</RouterLink
        >
        <div
          v-for="version in versions"
          :key="version.version"
          class="flex flex-wrap items-center gap-2"
        >
          <span class="break-all text-sm"
            >{{ version.version }}
            {{ version.active ? t('taskPlugins.active') : '' }}
            {{ version.factory ? t('taskPlugins.factory') : '' }}</span
          ><ConsoleButton
            size="sm"
            variant="secondary"
            :disabled="busy"
            @click="editSource(version)"
            >{{ t('taskPlugins.source') }}</ConsoleButton
          ><ConsoleButton
            v-if="!version.active && !version.factory"
            size="sm"
            variant="secondary"
            :disabled="busy"
            @click="
              action(() =>
                mutate(() => taskPluginsApi.activate(selected, version.version))
              )
            "
            >{{ t('taskPlugins.activate') }}</ConsoleButton
          ><ConsoleButton
            v-if="!version.active && !version.factory"
            size="sm"
            variant="danger"
            :disabled="busy"
            @click="action(() => remove(version))"
            >{{ t('taskPlugins.remove') }}</ConsoleButton
          >
        </div>
        <details>
          <summary class="cursor-pointer text-sm">
            {{ t('taskPlugins.run') }}
          </summary>
          <div class="mt-3 grid gap-3 sm:grid-cols-2">
            <FormField :label="t('taskPlugins.hook')"
              ><TextInput v-model="hook" /></FormField
            ><FormField :label="t('taskPlugins.member')"
              ><TextInput v-model="member"
            /></FormField>
          </div>
          <FormField :label="t('taskPlugins.args')">
            <textarea
              v-model="args"
              class="plugin-textarea"
              rows="4"
            /></FormField
          ><ConsoleButton :loading="busy" @click="action(runDebug)">{{
            t('taskPlugins.run')
          }}</ConsoleButton>
          <pre
            v-if="debugResult"
            class="mt-3 max-h-72 overflow-auto whitespace-pre-wrap break-all text-xs"
            :aria-label="t('taskPlugins.result')"
            >{{ debugResult }}</pre>
        </details>
      </section>
    </ConsoleCard>
    <ConsoleCard v-else-if="tab === 'market'">
      <p class="mb-4 text-sm text-[var(--text-secondary)]">
        {{ t('taskPlugins.marketHint') }}
      </p>
      <ConsoleButton :loading="busy" @click="action(loadMarket)">{{
        t('taskPlugins.loadMarket')
      }}</ConsoleButton>
      <p v-if="!market.length" class="mt-4 text-sm">
        {{ t('taskPlugins.marketEmpty') }}
      </p>
      <div class="mt-4 grid gap-4 lg:grid-cols-2">
        <article
          v-for="item in market"
          :key="`${item.indexURL}:${item.key}`"
          class="min-w-0 rounded-xl border border-[var(--border-subtle)] p-4"
        >
          <div class="flex items-center gap-3">
            <PluginIcon
              :plugin-key="item.key"
              :name="item.name"
              :icon="item.icon"
              :source-url="item.iconURL"
            />
            <h2 class="min-w-0 break-words font-semibold">{{ item.name }}</h2>
          </div>
          <p class="text-xs">{{ item.sourceName }} · {{ item.key }}</p>
          <p class="mt-2 text-sm">
            {{ localizedText(item.description, locale) }}
          </p>
          <div class="mt-3 flex flex-wrap gap-2">
            <select
              v-model="selectedVersions[`${item.indexURL}:${item.key}`]"
              class="max-w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2"
              :aria-label="t('taskPlugins.selectVersion')"
            >
              <option :value="undefined">{{ item.latest }}</option>
              <option
                v-for="version in item.versions"
                :key="version.version"
                :value="version.version"
              >
                {{ version.version }}
              </option></select
            ><ConsoleButton
              size="sm"
              variant="secondary"
              @click="
                changelog = {
                  plugin: item,
                  version:
                    selectedVersions[`${item.indexURL}:${item.key}`] ||
                    item.latest,
                }
              "
              >{{ t('taskPlugins.changelog') }}</ConsoleButton
            ><ConsoleButton
              size="sm"
              :disabled="busy"
              @click="action(() => importMarket(item))"
              >{{ t('taskPlugins.import') }}</ConsoleButton
            >
          </div>
        </article>
      </div>
    </ConsoleCard>
    <ConsoleCard v-else-if="tab === 'editor'">
      <p class="mb-4 text-sm text-[var(--text-secondary)]">
        {{ t('taskPlugins.pricing') }}
      </p>
      <label class="mb-3 block text-sm"
        >{{ t('taskPlugins.importFile')
        }}<input
          type="file"
          accept=".js,text/javascript"
          class="mt-2 block w-full text-sm"
          @change="fileSelected"
      /></label>
      <FormField :label="t('taskPlugins.source')">
        <textarea
          v-model="source"
          class="plugin-textarea"
          rows="16"
          spellcheck="false"
        />
      </FormField>
      <label class="my-3 flex items-center gap-2 text-sm"
        ><input v-model="enabled" type="checkbox" />{{
          t('taskPlugins.enableOnInstall')
        }}</label
      >
      <div class="flex flex-wrap gap-2">
        <ConsoleButton
          :loading="busy"
          :disabled="!source"
          variant="secondary"
          @click="action(validate)"
          >{{ t('taskPlugins.preview') }}</ConsoleButton
        ><ConsoleButton :disabled="busy || !preview" @click="action(install)">{{
          t('taskPlugins.install')
        }}</ConsoleButton>
      </div>
      <pre
        v-if="preview"
        class="mt-4 max-h-96 overflow-auto whitespace-pre-wrap break-all text-xs"
        >{{ readablePreview }}</pre>
      <p
        v-if="preview?.unresolved?.length"
        class="mt-3 break-words text-sm text-[var(--text-secondary)]"
      >
        {{ t('taskPlugins.unresolved') }}: {{ preview.unresolved.join(', ') }}
      </p>
    </ConsoleCard>
    <ConsoleCard v-else>
      <p class="mb-4 text-sm">{{ t('taskPlugins.sourcesHelp') }}</p>
      <FormField :label="t('taskPlugins.sources')">
        <textarea
          v-model="sourcesText"
          class="plugin-textarea"
          rows="10"
          spellcheck="false"
        /></FormField
      ><ConsoleButton
        class="mt-3"
        :loading="busy"
        @click="action(saveSources)"
        >{{ t('taskPlugins.saveSources') }}</ConsoleButton
      >
    </ConsoleCard>
    <ConsoleModal
      :open="disableTarget !== null"
      :title="
        t(disableUsage ? 'taskPlugins.inUseTitle' : 'taskPlugins.disableTitle')
      "
      :close-disabled="disabling"
      @close="closeDisable"
    >
      <div class="space-y-3 text-sm">
        <p class="break-words">
          {{
            t('taskPlugins.disableConfirm', {
              name: disableTarget?.meta.name || disableTarget?.meta.key,
            })
          }}
        </p>
        <template v-if="disableUsage">
          <p>{{ t('taskPlugins.usageChanged') }}</p>
          <ul v-if="disableUsage.channels.length" class="space-y-2">
            <li
              v-for="channel in disableUsage.channels"
              :key="channel.id"
              class="break-words rounded-lg border border-[var(--border-subtle)] p-2"
            >
              <span class="font-semibold"
                >{{ channel.name }} (#{{ channel.id }})</span
              >
              <p>
                {{
                  t(
                    channel.type === 60
                      ? 'taskPlugins.newApiUnbind'
                      : 'taskPlugins.channelDisable'
                  )
                }}
              </p>
            </li>
          </ul>
          <p>
            {{
              t('taskPlugins.pendingCount', {
                count: disableUsage.in_flight_count,
              })
            }}
          </p>
          <p
            v-if="disableUsage.in_flight_count > 0"
            class="text-[var(--status-danger)]"
          >
            {{ t('taskPlugins.forceWarning') }}
          </p>
        </template>
        <p
          v-if="disableError"
          role="alert"
          class="break-words text-[var(--status-danger)]"
        >
          {{ disableError }}
        </p>
      </div>
      <template #footer>
        <div class="flex flex-wrap gap-2">
          <ConsoleButton
            variant="secondary"
            :disabled="disabling"
            @click="closeDisable"
            >{{ t('common.cancel') }}</ConsoleButton
          >
          <ConsoleButton
            v-if="!disableUsage"
            variant="danger"
            :loading="disabling"
            @click="confirmDisable()"
            >{{ t('taskPlugins.confirmDisable') }}</ConsoleButton
          >
          <template v-else>
            <ConsoleButton
              v-if="disableUsage.channels.length"
              variant="danger"
              :loading="disabling"
              :disabled="disableUsage.in_flight_count > 0"
              @click="confirmDisable({ cascade: true })"
              >{{ t('taskPlugins.cascadeDisable') }}</ConsoleButton
            >
            <ConsoleButton
              v-if="disableUsage.in_flight_count > 0"
              variant="danger"
              :loading="disabling"
              @click="
                confirmDisable({
                  cascade: disableUsage.channels.length > 0,
                  force: true,
                })
              "
              >{{ t('taskPlugins.forceDisable') }}</ConsoleButton
            >
          </template>
        </div>
      </template>
    </ConsoleModal>
    <ConsoleModal
      :open="changelog !== null"
      :title="t('taskPlugins.changelog')"
      size="lg"
      @close="changelog = null"
    >
      <PluginChangelog
        v-if="changelog"
        :plugin="changelog.plugin"
        :version="changelog.version"
      />
      <template #footer
        ><ConsoleButton variant="secondary" @click="changelog = null">{{
          t('common.close')
        }}</ConsoleButton></template
      >
    </ConsoleModal>
  </div>
</template>

<style scoped>
.plugin-textarea {
  width: 100%;
  min-width: 0;
  border: 1px solid var(--border-subtle);
  border-radius: 0.6rem;
  background: var(--surface-solid);
  color: var(--text-primary);
  padding: 0.75rem;
  font: 0.8rem/1.5 monospace;
}
</style>
