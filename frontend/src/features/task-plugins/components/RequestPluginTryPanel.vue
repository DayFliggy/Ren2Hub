<script setup lang="ts">
// Upstream request-plugin-try-panel.tsx: runs the instance being edited on a
// sample or pasted request. Ren2Hub difference: image size facts are not shown.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Play, Sparkles } from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import { localizedText } from '@/utils/localizedText'
import { dryRunRequestPlugins } from '../api'
import {
  REQUEST_PLUGIN_DRY_RUN_FORMATS,
  parseRequestPluginBody,
  requestPluginChangedTexts,
  requestPluginDryRunExample,
  requestPluginDryRunPath,
} from '../lib/request-plugins'
import {
  REQUEST_PLUGIN_SAMPLE_IMAGE,
  requestPluginSampleBody,
  requestPluginSamplesFor,
  requestPluginSampleTexts,
} from '../lib/request-plugin-samples'
import type {
  RequestPluginDryRunResult,
  RequestPluginInstance,
  RequestPluginSample,
  TaskPluginOption,
} from '../types'
import JsonCodeEditor from './JsonCodeEditor.vue'

const props = defineProps<{
  plugin: TaskPluginOption
  /** The instance as edited; null while its settings have problems. */
  instance: RequestPluginInstance | null
}>()
const { t, n, locale } = useI18n()

let sampleImage: string | null | undefined

/**
 * Upstream drawSampleImage: a dusk landscape drawn once per page, a PNG of
 * about 690 KB that a lossless WebP makes much smaller, so image plugins show
 * their work with defaults. It stays local and never sends a remote URL.
 */
function drawSampleImage(): string | null {
  if (sampleImage !== undefined) return sampleImage
  sampleImage = null
  const width = 2000
  const height = 1250
  const canvas = document.createElement('canvas')
  canvas.width = width
  canvas.height = height
  const context = canvas.getContext('2d')
  if (!context) return null
  const sky = context.createLinearGradient(0, 0, 0, height)
  sky.addColorStop(0, '#1e3a8a')
  sky.addColorStop(0.45, '#7c3aed')
  sky.addColorStop(0.75, '#f472b6')
  sky.addColorStop(1, '#fdba74')
  context.fillStyle = sky
  context.fillRect(0, 0, width, height)
  const sun = context.createRadialGradient(
    width * 0.7,
    height * 0.62,
    0,
    width * 0.7,
    height * 0.62,
    height * 0.35
  )
  sun.addColorStop(0, 'rgba(255, 237, 160, 1)')
  sun.addColorStop(0.3, 'rgba(253, 186, 116, 0.8)')
  sun.addColorStop(1, 'rgba(253, 186, 116, 0)')
  context.fillStyle = sun
  context.fillRect(0, 0, width, height)
  const hills: [string, number, number][] = [
    ['#4c1d95', 0.7, 0.08],
    ['#3b0764', 0.8, 0.06],
    ['#1e1b4b', 0.9, 0.05],
  ]
  for (const [color, base, amplitude] of hills) {
    context.beginPath()
    context.moveTo(0, height)
    for (let x = 0; x <= width; x += 8) {
      const wave =
        Math.sin((x / width) * 9 + base * 10) + Math.sin((x / width) * 23) * 0.3
      context.lineTo(x, height * base + wave * height * amplitude)
    }
    context.lineTo(width, height)
    context.closePath()
    context.fillStyle = color
    context.fill()
  }
  sampleImage = canvas.toDataURL('image/png')
  return sampleImage
}

const SAMPLE_FORMAT = REQUEST_PLUGIN_DRY_RUN_FORMATS[0]
const image = drawSampleImage()
const formatOptions = REQUEST_PLUGIN_DRY_RUN_FORMATS.map((item) => ({
  value: item.value,
  label: item.label,
}))

const mode = ref<'sample' | 'paste'>('sample')
const sampleIndex = ref(0)
const format = ref<string>(SAMPLE_FORMAT.value)
const pasted = ref(
  JSON.stringify(
    requestPluginDryRunExample(SAMPLE_FORMAT.value, SAMPLE_FORMAT.model),
    null,
    2
  )
)
const pastedReply = ref('')
const inputError = ref('')
const runError = ref('')
const running = ref(false)
const result = ref<RequestPluginDryRunResult | null>(null)
const sent = ref<{ body: unknown; reply?: string; instance: string } | null>(
  null
)
let request: AbortController | null = null

const checksReplies = computed(() =>
  Boolean(props.plugin.transform_hooks?.includes('reply'))
)
const samples = computed<RequestPluginSample[]>(() => {
  const own = requestPluginSamplesFor(props.plugin.samples ?? [], locale.value)
  if (own.length) return own
  return [
    {
      name: '',
      format: 'openai',
      body: {
        model: SAMPLE_FORMAT.model,
        messages: [
          {
            role: 'user',
            content: [
              { type: 'text', text: t('pluginExtensions.rp.genericSample') },
              REQUEST_PLUGIN_SAMPLE_IMAGE,
            ],
          },
        ],
      },
    },
  ]
})
const sample = computed(
  () => samples.value[Math.min(sampleIndex.value, samples.value.length - 1)]!
)
const sampleBody = computed(() =>
  requestPluginSampleBody(sample.value.body, sample.value.format, image)
)
const sampleReply = computed(() =>
  checksReplies.value
    ? (sample.value.reply ?? t('pluginExtensions.rp.genericReply'))
    : undefined
)
const sampleOptions = computed(() =>
  samples.value.map((item, position) => ({
    value: String(position),
    label:
      localizedText(item.name, locale.value) ||
      t('pluginExtensions.rp.sampleRequest'),
  }))
)
const sampleTexts = computed(() => requestPluginSampleTexts(sampleBody.value))
const modeOptions = computed(() => [
  { value: 'sample', label: t('pluginExtensions.rp.sampleRequest') },
  { value: 'paste', label: t('pluginExtensions.rp.pasteRequest') },
])
const instanceKey = computed(() =>
  props.instance ? JSON.stringify(props.instance) : ''
)
const stale = computed(
  () =>
    Boolean(result.value && sent.value) &&
    sent.value?.instance !== instanceKey.value
)
const duration = computed(() =>
  (result.value?.steps ?? []).reduce(
    (total, step) => total + (step.duration_ms ?? 0),
    0
  )
)
const changedTexts = computed(() =>
  result.value && sent.value
    ? requestPluginChangedTexts(sent.value.body, result.value.body)
    : []
)
// An optional plugin's failure is skipped, so only its step tells of it.
const failure = computed(() => {
  const value = result.value
  if (!value) return null
  if (value.failed) return value.failed
  const step = (value.steps ?? []).find((item) => item.error)
  return step ? { instance: step.instance, error: step.error ?? '' } : null
})
const failedTitle = computed(() => {
  const onReply = (result.value?.steps ?? []).some(
    (step) => step.error && step.stage === 'reply'
  )
  if (onReply && props.plugin.optional)
    return t('pluginExtensions.rp.failedReplyGoesOn')
  if (onReply) return t('pluginExtensions.rp.failedReplyStopped')
  if (props.plugin.optional) return t('pluginExtensions.rp.failedRequestGoesOn')
  return t('pluginExtensions.rp.failedRequestRefused')
})
const replyMasked = computed(() => {
  const reply = result.value?.reply
  return Boolean(
    reply && sent.value?.reply !== undefined && reply.text !== sent.value.reply
  )
})
const unchanged = computed(() => {
  const value = result.value
  if (!value || value.rejected || failure.value) return false
  return (
    !changedTexts.value.length &&
    !value.reasoning_effort &&
    !value.reply?.stopped &&
    !replyMasked.value &&
    !Object.keys(value.headers?.set ?? {}).length &&
    !(value.headers?.remove ?? []).length &&
    !(value.record ?? []).length
  )
})

function cancel() {
  request?.abort()
  request = null
  running.value = false
}

function clearResult() {
  cancel()
  result.value = null
  sent.value = null
  runError.value = ''
}

// The result of another sample, or of another plugin, would read as this one's.
watch(sampleIndex, clearResult)
watch(
  () => props.plugin.key,
  () => {
    sampleIndex.value = 0
    clearResult()
  }
)

function effortLabel(effort: string | undefined) {
  return effort || t('pluginExtensions.rp.notSet')
}

async function start() {
  const instance = props.instance
  if (!instance) return
  let body: unknown = sampleBody.value
  let runFormat: string = sample.value.format
  let reply = sampleReply.value
  if (mode.value === 'paste') {
    const parsed = parseRequestPluginBody(pasted.value)
    if (!parsed) {
      inputError.value = t('pluginExtensions.rp.bodyNotObject')
      return
    }
    body = parsed
    runFormat = format.value
    reply =
      checksReplies.value && pastedReply.value.trim()
        ? pastedReply.value
        : undefined
  }
  inputError.value = ''
  runError.value = ''
  const bodyModel = (body as { model?: unknown }).model
  const model =
    typeof bodyModel === 'string' && bodyModel.trim()
      ? bodyModel
      : (REQUEST_PLUGIN_DRY_RUN_FORMATS.find((item) => item.value === runFormat)
          ?.model ?? SAMPLE_FORMAT.model)
  // The result belongs to the instance as it was sent, not as it is on reply.
  const sentInstance = instanceKey.value
  cancel()
  const controller = new AbortController()
  request = controller
  running.value = true
  try {
    const response = await dryRunRequestPlugins(
      {
        format: runFormat,
        path: requestPluginDryRunPath(runFormat, model),
        model,
        body,
        instance,
        ...(reply !== undefined ? { reply } : {}),
      },
      controller.signal
    )
    if (controller.signal.aborted) return
    sent.value = { body, reply, instance: sentInstance }
    result.value = response
  } catch (cause) {
    if (controller.signal.aborted) return
    result.value = null
    runError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (request === controller) {
      request = null
      running.value = false
    }
  }
}

onBeforeUnmount(cancel)
</script>

<template>
  <aside
    :aria-label="t('pluginExtensions.rp.tryIt')"
    class="sibling-space-y-3 h-fit rounded-2xl bg-[var(--surface-muted)] p-4"
    data-try-panel
  >
    <div class="flex items-center justify-between gap-2">
      <h3 class="flex items-center gap-1.5 text-sm font-semibold">
        <Sparkles :size="16" aria-hidden="true" class="text-[var(--accent)]" />
        {{ t('pluginExtensions.rp.tryIt') }}
      </h3>
      <ConsoleButton
        size="sm"
        variant="secondary"
        :loading="running"
        :disabled="!instance || running"
        @click="start"
      >
        <Play v-if="!running" :size="14" aria-hidden="true" />
        {{ t('pluginExtensions.rp.run') }}
      </ConsoleButton>
    </div>
    <SegmentedToggle
      v-model="mode"
      :options="modeOptions"
      :label="t('pluginExtensions.rp.tryIt')"
      size="sm"
      class="w-full"
    />
    <div v-if="mode === 'paste'" class="sibling-space-y-2">
      <FilterSelect
        v-model="format"
        :options="formatOptions"
        :label="t('pluginExtensions.rp.requestFormat')"
      />
      <JsonCodeEditor
        id="request-plugin-try-body"
        v-model="pasted"
        :label="t('pluginExtensions.rp.body')"
      />
      <p
        v-if="inputError"
        role="alert"
        class="text-xs text-[var(--status-danger-text)]"
      >
        {{ inputError }}
      </p>
      <label v-if="checksReplies" class="block">
        <span class="mb-1.5 block text-xs text-[var(--text-tertiary)]">{{
          t('pluginExtensions.rp.replyToCheck')
        }}</span>
        <textarea
          v-model="pastedReply"
          rows="3"
          :placeholder="t('pluginExtensions.rp.genericReply')"
          class="w-full rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3 text-sm text-[var(--text-primary)] outline-none focus-ring"
        />
      </label>
    </div>
    <div v-else class="sibling-space-y-2">
      <SegmentedToggle
        v-if="sampleOptions.length > 1"
        :model-value="String(sampleIndex)"
        :options="sampleOptions"
        :label="t('pluginExtensions.rp.samples')"
        size="sm"
        @update:model-value="sampleIndex = Number($event)"
      />
      <div
        v-if="!result"
        class="sibling-space-y-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3"
      >
        <p
          v-for="(text, position) in sampleTexts"
          :key="position"
          class="line-clamp-6 whitespace-pre-wrap break-words text-sm leading-6"
        >
          {{ text }}
        </p>
        <div
          v-if="sampleReply !== undefined"
          class="sibling-space-y-1 border-t border-[var(--border-subtle)] pt-3"
        >
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ t('pluginExtensions.rp.sampleReply') }}
          </p>
          <p
            class="line-clamp-6 whitespace-pre-wrap break-words text-sm leading-6"
          >
            {{ sampleReply }}
          </p>
        </div>
      </div>
    </div>
    <p v-if="!instance" class="text-xs text-[var(--text-tertiary)]">
      {{ t('pluginExtensions.rp.fixFirst') }}
    </p>
    <p
      v-if="runError"
      role="alert"
      class="whitespace-pre-wrap break-words text-xs text-[var(--status-danger-text)]"
    >
      {{ runError }}
    </p>
    <p
      v-if="stale"
      class="flex items-center gap-1.5 text-xs text-[var(--text-tertiary)]"
    >
      <span
        aria-hidden="true"
        class="h-1.5 w-1.5 rounded-full bg-[var(--status-warning)]"
      />
      {{ t('pluginExtensions.rp.stale') }}
    </p>
    <div v-if="result && sent" class="sibling-space-y-2" data-try-result>
      <div
        v-if="result.rejected"
        class="sibling-space-y-1 rounded-xl border border-[var(--status-danger)] bg-[var(--status-danger-soft)] p-3 text-sm"
      >
        <p class="font-medium text-[var(--status-danger-text)]">
          {{ t('pluginExtensions.rp.wouldRefuse') }}
        </p>
        <p v-if="result.rejected.message" class="break-words">
          {{ result.rejected.message }}
        </p>
        <p class="font-mono text-xs text-[var(--text-tertiary)]">
          {{ result.rejected.code }}
        </p>
      </div>
      <div
        v-else-if="failure"
        class="sibling-space-y-1 rounded-xl border border-[var(--status-warning)] bg-[var(--status-warning-soft)] p-3 text-sm"
      >
        <p class="font-medium">{{ failedTitle }}</p>
        <p class="break-all font-mono text-xs text-[var(--text-tertiary)]">
          {{ failure.error }}
        </p>
      </div>
      <p
        v-else-if="unchanged"
        class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3 text-sm text-[var(--text-tertiary)]"
      >
        {{
          result.reply
            ? t('pluginExtensions.rp.noChangesWithReply')
            : t('pluginExtensions.rp.noChanges')
        }}
      </p>
      <template v-if="!result.rejected">
        <div
          v-for="text in changedTexts"
          :key="text.path"
          class="sibling-space-y-2"
        >
          <div
            class="sibling-space-y-1 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3"
          >
            <p class="text-xs text-[var(--text-tertiary)]">
              {{ t('pluginExtensions.rp.originalText') }}
            </p>
            <p class="line-clamp-6 break-words text-sm leading-6">
              {{ text.before }}
            </p>
          </div>
          <div
            class="sibling-space-y-1 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3"
          >
            <p class="text-xs text-[var(--text-tertiary)]">
              {{ t('pluginExtensions.rp.sentUpstream') }}
            </p>
            <p class="line-clamp-6 break-words text-sm leading-6">
              {{ text.after }}
            </p>
          </div>
        </div>
        <div
          v-if="result.reasoning_effort"
          class="flex items-center justify-between gap-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3 text-sm"
        >
          <span class="text-xs text-[var(--text-tertiary)]">{{
            t('pluginExtensions.rp.reasoningEffortLabel')
          }}</span>
          <span class="flex items-center gap-2">
            <span class="text-[var(--text-tertiary)]">{{
              effortLabel(result.reasoning_effort_before)
            }}</span>
            <span aria-hidden="true">→</span>
            <span class="font-medium text-[var(--accent)]">{{
              result.reasoning_effort
            }}</span>
          </span>
        </div>
        <div
          v-if="result.reply?.stopped"
          class="sibling-space-y-1 rounded-xl border border-[var(--status-danger)] bg-[var(--status-danger-soft)] p-3 text-sm"
        >
          <p class="font-medium text-[var(--status-danger-text)]">
            {{ t('pluginExtensions.rp.wouldStopReply') }}
          </p>
          <p v-if="result.reply.stopped.message" class="break-words">
            {{ result.reply.stopped.message }}
          </p>
          <p class="font-mono text-xs text-[var(--text-tertiary)]">
            {{ result.reply.stopped.code }}
          </p>
        </div>
        <div v-else-if="result.reply && replyMasked" class="sibling-space-y-2">
          <div
            class="sibling-space-y-1 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3"
          >
            <p class="text-xs text-[var(--text-tertiary)]">
              {{ t('pluginExtensions.rp.modelAnswer') }}
            </p>
            <p class="line-clamp-6 break-words text-sm leading-6">
              {{ sent.reply }}
            </p>
          </div>
          <div
            class="sibling-space-y-1 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3"
          >
            <p class="text-xs text-[var(--text-tertiary)]">
              {{ t('pluginExtensions.rp.sentToClient') }}
            </p>
            <p class="line-clamp-6 break-words text-sm leading-6">
              {{ result.reply.text }}
            </p>
          </div>
        </div>
      </template>
      <div v-if="result.record?.length" class="sibling-space-y-1.5 pt-2">
        <p class="text-xs text-[var(--text-tertiary)]">
          {{ t('pluginExtensions.rp.usageLogShows') }}
        </p>
        <pre
          class="max-h-60 overflow-auto whitespace-pre-wrap break-all rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3 text-xs"
          >{{ JSON.stringify(result.record, null, 2) }}</pre>
      </div>
      <p class="pt-1 text-[11px] text-[var(--text-tertiary)]">
        {{ t('pluginExtensions.rp.took', { ms: n(duration) }) }}
      </p>
    </div>
    <p v-else class="text-[11px] text-[var(--text-tertiary)]">
      {{ t('pluginExtensions.rp.usesCurrent') }}
    </p>
  </aside>
</template>
