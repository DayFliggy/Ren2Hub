<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import { getAuthSessionGeneration } from '@/api/authSession'
import { localizedText } from '@/utils/localizedText'
import {
  continuePluginLogin,
  startPluginLogin,
  readPluginLoginMessage,
  PLUGIN_LOGIN_BROADCAST_CHANNEL,
  type PluginLoginInput,
  type PluginLoginStep,
} from '../lib/plugin-login'
const props = defineProps<{
  channelId: number
  keyIndex?: number | null
  add?: boolean
}>()
const emit = defineEmits<{ close: []; done: [] }>()
const { t, locale } = useI18n()
const step = ref<PluginLoginStep | null>(null)
const flow = ref('')
const busy = ref(false)
const error = ref('')
const callbackUrl = ref('')
const fields = reactive<Record<string, string>>(Object.create(null))
const now = ref(Date.now())
let generation = 0
let pollTimer: ReturnType<typeof setTimeout> | undefined
let request: AbortController | undefined
let alive = true
let notified = false
let interval = 5
const session = getAuthSessionGeneration()
const expires = computed(() =>
  step.value?.type === 'device' && step.value.expires_at
    ? Math.max(0, Math.ceil(step.value.expires_at - now.value / 1000))
    : null
)
const tick = setInterval(() => {
  now.value = Date.now()
}, 1000)
const broadcast =
  typeof BroadcastChannel === 'undefined'
    ? null
    : new BroadcastChannel(PLUGIN_LOGIN_BROADCAST_CHANNEL)
function stop() {
  clearTimeout(pollTimer)
  request?.abort()
  generation++
}
function isCurrent(current: number) {
  return (
    alive && current === generation && session === getAuthSessionGeneration()
  )
}
function receive(next: PluginLoginStep) {
  if (step.value?.type === 'done') return
  if (
    (next.type === 'pending' || next.type === 'device') &&
    typeof next.interval === 'number' &&
    Number.isFinite(next.interval) &&
    next.interval > 0
  )
    interval = Math.max(1, next.interval)
  if (!(next.type === 'pending' && step.value?.type === 'device'))
    step.value = next
  if (next.type === 'done') {
    stop()
    busy.value = false
    if (!notified) {
      notified = true
      emit('done')
    }
    return
  }
  if (
    (step.value?.type === 'device' || step.value?.type === 'pending') &&
    expires.value !== 0
  )
    pollTimer = setTimeout(() => {
      if (session === getAuthSessionGeneration() && expires.value !== 0)
        void send({ poll: true })
    }, interval * 1000)
}
async function send(input?: PluginLoginInput) {
  if (
    busy.value ||
    !alive ||
    session !== getAuthSessionGeneration() ||
    step.value?.type === 'done'
  )
    return
  clearTimeout(pollTimer)
  busy.value = true
  error.value = ''
  request = new AbortController()
  const current = generation
  try {
    if (input) {
      const next = await continuePluginLogin(
        props.channelId,
        flow.value,
        input,
        request.signal
      )
      if (isCurrent(current)) receive(next)
    } else {
      const result = await startPluginLogin(
        props.channelId,
        props.keyIndex ?? null,
        props.add ?? false,
        request.signal
      )
      if (isCurrent(current)) {
        flow.value = result.flow
        receive(result.step)
      }
    }
  } catch (cause) {
    if (isCurrent(current))
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (isCurrent(current)) busy.value = false
  }
}
function submit() {
  if (step.value?.type === 'paste') {
    if (callbackUrl.value.trim())
      void send({ callback_url: callbackUrl.value.trim() })
    return
  }
  if (step.value?.type === 'fields') {
    if (
      step.value.fields.some(
        (field) => field.required && !fields[field.name]?.trim()
      )
    ) {
      error.value = t('pluginExtensions.fieldsRequired')
      return
    }
    void send({
      fields: Object.fromEntries(
        step.value.fields.map((field) => [field.name, fields[field.name] ?? ''])
      ),
    })
  }
}
function restart() {
  stop()
  step.value = null
  busy.value = false
  notified = false
  flow.value = ''
  callbackUrl.value = ''
  Object.keys(fields).forEach((key) => delete fields[key])
  void send()
}
broadcast?.addEventListener('message', (event) => {
  if (
    !alive ||
    session !== getAuthSessionGeneration() ||
    step.value?.type !== 'redirect'
  )
    return
  const next = readPluginLoginMessage(event.data, props.channelId, flow.value)
  if (next) {
    error.value = ''
    receive(next)
    return
  }
  if (
    event.data?.state === flow.value &&
    typeof event.data?.error === 'string'
  ) {
    stop()
    busy.value = false
    error.value = event.data.error
  }
})
onBeforeUnmount(() => {
  alive = false
  stop()
  clearInterval(tick)
  broadcast?.close()
})
void send()
</script>
<template>
  <ConsoleModal
    :open="true"
    :title="t('pluginExtensions.login')"
    @close="emit('close')"
  >
    <div class="sibling-space-y-4">
      <p v-if="error" role="alert" class="text-sm text-[var(--status-danger)]">
        {{ error }}
      </p>
      <p v-if="!step && busy">{{ t('pluginExtensions.pending') }}</p>
      <template v-if="step?.type === 'redirect' || step?.type === 'paste'"
        ><a
          :href="step.url"
          target="_blank"
          rel="noopener noreferrer"
          class="text-[var(--accent)] underline"
          >{{ t('pluginExtensions.openLogin') }}</a
        ><template v-if="step.type === 'paste'"
          ><p v-if="step.hint" class="text-sm">
            {{ localizedText(step.hint, locale) }}
          </p>
          <FormField :label="t('pluginExtensions.paste')"
            ><TextInput v-model="callbackUrl" /></FormField
        ></template>
        <p v-else>{{ t('pluginExtensions.waiting') }}</p></template
      >
      <template v-else-if="step?.type === 'device'"
        ><p class="font-mono text-xl">{{ step.user_code }}</p>
        <a
          :href="step.verification_uri_complete || step.verification_uri"
          target="_blank"
          rel="noopener noreferrer"
          class="text-[var(--accent)] underline"
          >{{ t('pluginExtensions.openLogin') }}</a
        >
        <p>
          {{
            expires === 0
              ? t('pluginExtensions.expired')
              : t('pluginExtensions.waiting')
          }}
          <span v-if="expires !== null && expires > 0">{{ expires }}s</span>
        </p></template
      >
      <p v-else-if="step?.type === 'pending'">
        {{ t('pluginExtensions.waiting') }}
      </p>
      <template v-else-if="step?.type === 'fields'"
        ><FormField
          v-for="field in step.fields"
          :key="field.name"
          :label="
            localizedText(field.label ?? field.title ?? undefined, locale) ||
            field.name
          "
        >
          <textarea
            v-if="field.type === 'textarea'"
            v-model="fields[field.name]"
            :placeholder="localizedText(field.placeholder ?? undefined, locale)"
            :required="field.required"
            rows="4"
            class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3"
          /><TextInput
            v-else
            v-model="fields[field.name]"
            :type="
              field.secret || field.type === 'password' ? 'password' : 'text'
            "
            :placeholder="localizedText(field.placeholder ?? undefined, locale)"
            :required="field.required"
          />
          <p
            v-if="field.description"
            class="text-xs text-[var(--text-secondary)]"
          >
            {{ localizedText(field.description, locale) }}
          </p></FormField
        ></template
      >
      <template v-else-if="step?.type === 'done'"
        ><p>{{ t('pluginExtensions.done') }}</p>
        <p v-if="step.credential">
          {{ step.credential.label || step.credential.account_id }}
        </p></template
      >
      <div class="flex flex-wrap gap-2">
        <ConsoleButton
          v-if="step?.type === 'paste' || step?.type === 'fields'"
          :disabled="busy"
          @click="submit"
          >{{ t('pluginExtensions.continue') }}</ConsoleButton
        ><ConsoleButton
          v-if="error || expires === 0"
          :disabled="busy"
          @click="restart"
          >{{ t('pluginExtensions.restart') }}</ConsoleButton
        ><ConsoleButton variant="secondary" @click="emit('close')">{{
          t('common.close')
        }}</ConsoleButton>
      </div>
    </div>
  </ConsoleModal>
</template>
