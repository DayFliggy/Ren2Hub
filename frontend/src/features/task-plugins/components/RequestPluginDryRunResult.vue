<script setup lang="ts">
// Upstream request-plugin-dry-run-dialog.tsx DryRunResult: the outcome, the
// steps, the body diff, header changes and what users see in their logs.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import type { RequestPluginDryRunResult } from '../types'
import { requestPluginStageKey } from '../lib/request-plugins'
import SourceDiff from './SourceDiff.vue'

const props = defineProps<{
  result: RequestPluginDryRunResult
  /** The body as it was sent, formatted the same way as the result body. */
  sentBody: string
  instanceName: (id: string) => string
  pluginName: (key: string) => string
}>()
const { t, n } = useI18n()

const steps = computed(() => props.result.steps ?? [])
const headerSet = computed(() =>
  Object.entries(props.result.headers?.set ?? {})
)
const headerRemove = computed(() => props.result.headers?.remove ?? [])
const afterBody = computed(() =>
  JSON.stringify(props.result.body ?? null, null, 2)
)
const outcome = computed(() => {
  const { rejected, failed } = props.result
  if (rejected)
    return t('pluginExtensions.rp.refusedBy', {
      name: props.instanceName(rejected.instance),
      message: rejected.message,
      code: rejected.code,
    })
  if (failed)
    return t('pluginExtensions.rp.failedWith', {
      name: props.instanceName(failed.instance),
      error: failed.error,
    })
  return t('pluginExtensions.rp.passed')
})

function stageLabel(stage: string) {
  const key = requestPluginStageKey(stage)
  return key ? t(key) : stage
}

function actionLabel(action: string) {
  if (action === 'continue') return t('pluginExtensions.rp.actionPassed')
  if (action === 'reject') return t('pluginExtensions.rp.actionRefused')
  if (action === 'unchecked') return t('pluginExtensions.rp.actionUnchecked')
  return action
}

function findingsText(step: (typeof steps.value)[number]) {
  if (!step.findings?.length) return '—'
  return step.findings
    .map(
      (finding) =>
        `${finding.rule}${finding.category ? ` (${finding.category})` : ''} × ${n(finding.count)}`
    )
    .join(', ')
}
</script>

<template>
  <section
    class="sibling-space-y-4 border-t border-[var(--border-subtle)] pt-4"
    :aria-label="t('pluginExtensions.rp.result')"
    data-dry-run-result
  >
    <h3 class="text-sm font-semibold text-[var(--text-primary)]">
      {{ t('pluginExtensions.rp.result') }}
    </h3>
    <p
      role="status"
      class="break-words text-sm"
      :class="
        result.rejected || result.failed
          ? 'text-[var(--status-danger-text)]'
          : 'text-[var(--text-primary)]'
      "
    >
      {{ outcome }}
    </p>
    <p v-if="!steps.length" class="text-sm text-[var(--text-tertiary)]">
      {{ t('pluginExtensions.rp.noSteps') }}
    </p>
    <div
      v-else
      class="subtle-scroll overflow-x-auto rounded-xl border border-[var(--border-subtle)]"
    >
      <table class="w-full min-w-[40rem] text-left text-sm">
        <caption class="sr-only">
          {{
            t('pluginExtensions.rp.steps')
          }}
        </caption>
        <thead
          class="bg-[var(--surface-muted)] text-xs text-[var(--text-tertiary)]"
        >
          <tr>
            <th class="px-3 py-2 font-medium">
              {{ t('pluginExtensions.rp.instance') }}
            </th>
            <th class="px-3 py-2 font-medium">
              {{ t('pluginExtensions.rp.stage') }}
            </th>
            <th class="px-3 py-2 font-medium">
              {{ t('pluginExtensions.rp.action') }}
            </th>
            <th class="px-3 py-2 font-medium">
              {{ t('pluginExtensions.rp.changes') }}
            </th>
            <th class="px-3 py-2 font-medium">
              {{ t('pluginExtensions.rp.findings') }}
            </th>
            <th class="px-3 py-2 font-medium">
              {{ t('pluginExtensions.rp.duration') }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="step in steps"
            :key="`${step.instance}-${step.stage}`"
            class="border-t border-[var(--border-subtle)] align-top"
          >
            <td class="max-w-48 break-words px-3 py-2">
              {{ step.name || instanceName(step.instance) }}
              <span class="block text-xs text-[var(--text-tertiary)]">{{
                pluginName(step.plugin)
              }}</span>
            </td>
            <td class="px-3 py-2">{{ stageLabel(step.stage) }}</td>
            <td class="max-w-56 break-words px-3 py-2">
              {{ actionLabel(step.action) }}
              <span
                v-if="step.error"
                class="block text-xs text-[var(--status-danger-text)]"
                >{{ step.error }}</span
              >
            </td>
            <td class="px-3 py-2 tabular-nums">{{ n(step.effects) }}</td>
            <td class="max-w-56 break-words px-3 py-2">
              {{ findingsText(step) }}
            </td>
            <td class="whitespace-nowrap px-3 py-2 tabular-nums">
              {{ t('pluginExtensions.rp.ms', { value: n(step.duration_ms) }) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="sibling-space-y-2">
      <h4 class="text-sm font-medium">{{ t('pluginExtensions.rp.body') }}</h4>
      <SourceDiff
        :before="sentBody"
        :after="afterBody"
        :label="t('pluginExtensions.rp.bodyChanges')"
        :empty-text="t('pluginExtensions.rp.bodyUnchanged')"
      />
    </div>
    <div class="sibling-space-y-2">
      <h4 class="text-sm font-medium">
        {{ t('pluginExtensions.rp.headerChanges') }}
      </h4>
      <p
        v-if="!headerSet.length && !headerRemove.length"
        class="text-sm text-[var(--text-tertiary)]"
      >
        {{ t('pluginExtensions.rp.noHeaderChanges') }}
      </p>
      <ul v-else class="sibling-space-y-1 break-all font-mono text-xs">
        <li v-for="[name, value] in headerSet" :key="`set-${name}`">
          {{ t('pluginExtensions.rp.headerSet') }} {{ name }}: {{ value }}
        </li>
        <li v-for="name in headerRemove" :key="`remove-${name}`">
          {{ t('pluginExtensions.rp.headerRemove') }} {{ name }}
        </li>
      </ul>
    </div>
    <p v-if="result.reasoning_effort" class="text-sm">
      {{
        t('pluginExtensions.rp.reasoningEffort', {
          effort: result.reasoning_effort,
        })
      }}
    </p>
    <div v-if="result.reply" class="sibling-space-y-2">
      <h4 class="text-sm font-medium">
        {{ t('pluginExtensions.rp.replyResult') }}
      </h4>
      <p
        v-if="result.reply.stopped"
        role="status"
        class="break-words text-sm text-[var(--status-danger-text)]"
      >
        {{
          t('pluginExtensions.rp.replyStopped', {
            name: instanceName(result.reply.stopped.instance),
            message: result.reply.stopped.message,
            code: result.reply.stopped.code,
          })
        }}
      </p>
      <pre
        class="max-h-60 overflow-auto whitespace-pre-wrap break-words rounded-xl bg-[var(--surface-muted)] p-3 text-xs"
        >{{ result.reply.text }}</pre>
    </div>
    <div class="sibling-space-y-2">
      <h4 class="text-sm font-medium">
        {{ t('pluginExtensions.rp.userLogs') }}
      </h4>
      <pre
        v-if="result.record?.length"
        class="max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-xl bg-[var(--surface-muted)] p-3 text-xs"
        >{{ JSON.stringify(result.record, null, 2) }}</pre>
      <p v-else class="text-sm text-[var(--text-tertiary)]">
        {{ t('pluginExtensions.rp.noUserLogs') }}
      </p>
    </div>
  </section>
</template>
