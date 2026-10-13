<script setup lang="ts">
// Upstream components/plugin-sandbox.tsx: run one hook of the active source
// through POST /:key/dryrun. "renderers.openai_video" calls a member.
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Play } from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import FormField from '@/components/common/FormField.vue'

import { dryRunTaskPlugin } from '../api'

const props = defineProps<{ pluginKey: string }>()
const { t } = useI18n()
const hooks = [
  'resolveRequest',
  'buildSubmitRequest',
  'parseSubmitResponse',
  'extractUsage',
  'extractUsageOnSubmit',
  'extractUsageOnComplete',
  'buildQueryRequest',
  'parseTaskResult',
  'buildBatchQueryRequest',
  'parseBatchResult',
  'buildContentRequest',
  'renderers.openai_video',
].map((hook) => ({ value: hook, label: hook }))
const hook = ref('buildSubmitRequest')
const args = ref('[{}]')
const output = ref('')
const running = ref(false)

async function run() {
  running.value = true
  try {
    const parsed: unknown = JSON.parse(args.value)
    if (!Array.isArray(parsed))
      throw new Error(t('taskPlugins.argsMustBeArray'))
    const separator = hook.value.indexOf('.')
    const value = await dryRunTaskPlugin(props.pluginKey, {
      hook: separator < 0 ? hook.value : hook.value.slice(0, separator),
      member: separator < 0 ? undefined : hook.value.slice(separator + 1),
      args: parsed,
    })
    output.value = JSON.stringify(value, null, 2)
  } catch (error) {
    // The result panel shows the failure in place, exactly like upstream.
    const message = error instanceof Error ? error.message : String(error)
    output.value = JSON.stringify({ error: message }, null, 2)
  } finally {
    running.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <FilterSelect
      v-model="hook"
      :options="hooks"
      :label="t('taskPlugins.hook')"
      :prefix-label="`${t('taskPlugins.hook')}:`"
    />
    <FormField :label="t('taskPlugins.argumentsJson')">
      <textarea
        v-model="args"
        rows="12"
        spellcheck="false"
        :aria-label="t('taskPlugins.argumentsJson')"
        class="w-full rounded-xl border border-[var(--border-default)] bg-[var(--surface-solid)] p-3 font-mono text-xs text-[var(--text-primary)] outline-hidden focus:border-[var(--accent)] focus-ring"
      />
    </FormField>
    <ConsoleButton :loading="running" @click="run">
      <Play :size="15" aria-hidden="true" />
      {{
        running ? t('taskPlugins.runningDryRun') : t('taskPlugins.runDryRun')
      }}
    </ConsoleButton>
    <section v-if="output" class="sibling-space-y-2">
      <h3 class="text-sm font-medium">{{ t('taskPlugins.dryRunResult') }}</h3>
      <pre
        class="max-h-96 overflow-auto whitespace-pre-wrap break-all rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3 font-mono text-xs"
        >{{ output }}</pre>
    </section>
  </div>
</template>
