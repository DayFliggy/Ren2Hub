<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RefreshCw, Save } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { gatewayApi, parseGatewayOverride } from '@/api/gateway'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import GatewayNumber from './GatewayNumber.vue'
import GatewayRuntimeDetails from './GatewayRuntimeDetails.vue'
import type {
  GatewayAdmission,
  GatewayOverride,
  GatewayRuntime,
} from '@/types/gateway'

const props = withDefaults(
  defineProps<{ userId?: number; editable?: boolean }>(),
  { editable: false, userId: undefined }
)
const { t } = useI18n()
const toast = useToast()
const request = useLatestRequest()
const savingRequest = useLatestRequest()
const admission = ref<GatewayAdmission | null>(null)
const runtime = ref<GatewayRuntime | null>(null)
const draft = ref<GatewayOverride>({
  concurrency: null,
  wait_ms: null,
  queue_size: null,
})
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const runtimeError = ref('')
const fields = [
  { key: 'concurrency', max: 1000000 },
  { key: 'wait_ms', max: 5000 },
  { key: 'queue_size', max: 20 },
] as const
const dirty = computed(
  () =>
    Boolean(admission.value) &&
    JSON.stringify(draft.value) !== JSON.stringify(admission.value?.override)
)
const valid = computed(() => {
  try {
    parseGatewayOverride(draft.value)
    return true
  } catch {
    return false
  }
})

async function load() {
  loading.value = true
  error.value = ''
  runtimeError.value = ''
  admission.value = null
  runtime.value = null
  const result = await request.run(async (signal) =>
    Promise.allSettled([
      gatewayApi.admission(props.userId, signal),
      gatewayApi.userRuntime(props.userId, signal),
    ])
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    error.value = String(result.error)
    return
  }
  const [policy, live] = result.value
  if (policy.status === 'fulfilled') {
    admission.value = policy.value
    draft.value = { ...policy.value.override }
  } else error.value = String(policy.reason)
  if (live.status === 'fulfilled') runtime.value = live.value
  else runtimeError.value = t('gateway.runtimeUnavailable')
}

async function save() {
  if (!props.editable || !props.userId || !valid.value || saving.value) return
  saving.value = true
  const result = await savingRequest.run((signal) =>
    gatewayApi.saveUser(props.userId!, draft.value, signal)
  )
  if (result.stale) return
  saving.value = false
  if (!result.ok) {
    error.value = String(result.error)
    return
  }
  toast.success(t('gateway.saved'))
  await load()
}

watch(
  () => props.userId,
  () => {
    savingRequest.cancel()
    saving.value = false
    void load()
  },
  { immediate: true }
)
</script>

<template>
  <section
    class="min-w-0 sibling-space-y-4 border-t border-[var(--border-subtle)] py-4"
    data-testid="gateway-user-panel"
  >
    <div class="flex items-center justify-between gap-3">
      <h3 class="text-sm font-semibold text-[var(--text-primary)]">
        {{ t('gateway.accountLimits') }}
      </h3>
      <ConsoleButton
        variant="ghost"
        size="sm"
        :title="t('common.refresh')"
        :aria-label="t('common.refresh')"
        :disabled="loading || saving"
        @click="load"
        ><RefreshCw class="h-4 w-4"
      /></ConsoleButton>
    </div>
    <p v-if="loading" class="text-sm text-[var(--text-tertiary)]">
      {{ t('common.loading') }}
    </p>
    <p
      v-if="error"
      role="alert"
      class="break-words text-sm text-[var(--status-danger-text)]"
    >
      {{ error }}
    </p>
    <div v-if="admission" class="grid gap-4 sm:grid-cols-3">
      <div v-for="field in fields" :key="field.key" class="min-w-0">
        <GatewayNumber
          v-if="editable"
          v-model="draft[field.key]"
          :label="t(`gateway.${field.key}`)"
          :max="field.max"
          nullable
          :disabled="saving"
        />
        <p v-else class="text-xs text-[var(--text-secondary)]">
          {{ t(`gateway.${field.key}`) }}
        </p>
        <p class="mt-1 text-sm font-semibold text-[var(--text-primary)]">
          {{
            field.key === 'concurrency' && admission.effective[field.key] === 0
              ? t('gateway.unlimited')
              : admission.effective[field.key]
          }}
          <span class="text-xs font-normal text-[var(--text-tertiary)]">{{
            t(`gateway.sources.${admission.sources[field.key]}`)
          }}</span>
        </p>
      </div>
    </div>
    <div
      class="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm text-[var(--text-secondary)]"
    >
      <template v-if="runtime"
        ><span
          >{{ t('gateway.inFlight') }}
          <strong
            >{{ runtime.in_flight }} /
            {{
              runtime.capacity === 0 ? t('gateway.unlimited') : runtime.capacity
            }}</strong
          ></span
        ><span
          >{{ t('gateway.queued') }} <strong>{{ runtime.queued }}</strong></span
        ></template
      >
      <span
        v-else-if="runtimeError"
        role="status"
        class="text-[var(--status-warning-text)]"
        >{{ runtimeError }}</span
      >
      <ConsoleButton
        v-if="editable && admission"
        size="sm"
        :loading="saving"
        :disabled="!dirty || !valid"
        @click="save"
        ><Save class="h-4 w-4" />{{ t('gateway.saveLimits') }}</ConsoleButton
      >
    </div>
    <GatewayRuntimeDetails v-if="runtime" :runtime="runtime" />
  </section>
</template>
