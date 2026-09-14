<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Plus, RefreshCw, Save, Trash2 } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { gatewayApi, parseGatewayConfig } from '@/api/gateway'
import { ApiError } from '@/api/types'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import TextInput from '@/components/common/TextInput.vue'
import GatewayNumber from './GatewayNumber.vue'
import type {
  GatewayConfig,
  GatewayOverride,
  GatewayRateLimit,
} from '@/types/gateway'

const props = defineProps<{ channelId?: number }>()
const { t } = useI18n()
const toast = useToast()
const request = useLatestRequest()
const savingRequest = useLatestRequest()
const config = ref<GatewayConfig | null>(null)
const draft = ref<GatewayConfig | null>(null)
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const conflict = ref(false)
const groupName = ref('')
const scopeKind = ref<'channel_rates' | 'credential_rates'>('channel_rates')
const scopeID = ref('')
const credentialIDs = ref<string[]>([])
const groupFields = [
  { key: 'concurrency', max: 1000000 },
  { key: 'wait_ms', max: 5000 },
  { key: 'queue_size', max: 20 },
] as const
const defaultFields = [
  { key: 'user_concurrency', label: 'concurrency', max: 1000000 },
  { key: 'wait_ms', label: 'wait_ms', max: 5000 },
  { key: 'user_queue_size', label: 'queue_size', max: 20 },
  { key: 'channel_queue_size', label: 'channelQueue', max: 10000 },
] as const
const rateFields = ['rpm', 'tpm'] as const
const dirty = computed(
  () =>
    Boolean(draft.value) &&
    JSON.stringify(draft.value) !== JSON.stringify(config.value)
)
const valid = computed(() => {
  try {
    parseGatewayConfig(draft.value)
    return true
  } catch {
    return false
  }
})
const rateRows = computed(() => {
  if (!draft.value) return []
  return (['channel_rates', 'credential_rates'] as const).flatMap((kind) =>
    Object.entries(draft.value![kind])
      .filter(
        ([id]) =>
          !props.channelId ||
          (kind === 'channel_rates'
            ? id === String(props.channelId)
            : credentialIDs.value.includes(id))
      )
      .map(([id, limit]) => ({ kind, id, limit }))
  )
})
const availableCredentials = computed(() =>
  credentialIDs.value.filter((id) => !draft.value?.credential_rates[id])
)
const canAddRate = computed(() => {
  const id =
    props.channelId && scopeKind.value === 'channel_rates'
      ? String(props.channelId)
      : scopeID.value.trim()
  return Boolean(id) && !draft.value?.[scopeKind.value][id]
})

async function load() {
  loading.value = true
  error.value = ''
  conflict.value = false
  draft.value = null
  const result = await request.run(async (signal) => {
    const [current, credentials] = await Promise.all([
      gatewayApi.config(signal),
      props.channelId
        ? gatewayApi.credentials(props.channelId, signal)
        : Promise.resolve([]),
    ])
    return { current, credentials }
  })
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    error.value = String(result.error)
    return
  }
  config.value = result.value.current
  draft.value = structuredClone(result.value.current)
  credentialIDs.value = result.value.credentials
  scopeID.value = ''
}

function addGroup() {
  const name = groupName.value.trim()
  if (
    !draft.value ||
    !name ||
    name.length > 64 ||
    Object.hasOwn(draft.value.groups, name)
  )
    return
  draft.value.groups = {
    ...draft.value.groups,
    [name]: {
      concurrency: null,
      wait_ms: null,
      queue_size: null,
    } as GatewayOverride,
  }
  groupName.value = ''
}

function addRate() {
  if (!draft.value || !canAddRate.value) return
  const id =
    props.channelId && scopeKind.value === 'channel_rates'
      ? String(props.channelId)
      : scopeID.value.trim()
  const next = {
    ...draft.value,
    [scopeKind.value]: {
      ...draft.value[scopeKind.value],
      [id]: { rpm: 0, tpm: 0 } as GatewayRateLimit,
    },
  }
  try {
    parseGatewayConfig(next)
  } catch {
    error.value = t('gateway.invalidInput')
    return
  }
  draft.value = next
  scopeID.value = ''
  error.value = ''
}

async function save() {
  if (
    !draft.value ||
    !valid.value ||
    !dirty.value ||
    saving.value ||
    conflict.value
  )
    return
  saving.value = true
  error.value = ''
  const payload = parseGatewayConfig(draft.value)
  const result = await savingRequest.run((signal) =>
    gatewayApi.saveConfig(payload, signal)
  )
  if (result.stale) return
  saving.value = false
  if (!result.ok) {
    conflict.value =
      result.error instanceof ApiError && result.error.status === 409
    error.value = conflict.value ? t('gateway.conflict') : String(result.error)
    return
  }
  config.value = result.value
  draft.value = structuredClone(result.value)
  toast.success(t('gateway.saved'))
}

watch(
  () => props.channelId,
  () => {
    savingRequest.cancel()
    saving.value = false
    void load()
  },
  { immediate: true }
)
</script>

<template>
  <section class="min-w-0 space-y-5" data-testid="gateway-config-panel">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h3 class="text-sm font-semibold text-[var(--text-primary)]">
        {{ t(channelId ? 'gateway.providerLimits' : 'gateway.title') }}
      </h3>
      <div class="flex items-center gap-2">
        <span v-if="config" class="text-xs text-[var(--text-tertiary)]">{{
          t('gateway.version', { version: config.version })
        }}</span>
        <ConsoleButton
          variant="ghost"
          size="sm"
          :title="t('gateway.reload')"
          :aria-label="t('gateway.reload')"
          :disabled="loading || saving"
          @click="load"
          ><RefreshCw class="h-4 w-4"
        /></ConsoleButton>
      </div>
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
    <form v-if="draft" class="space-y-5" @submit.prevent="save">
      <fieldset :disabled="saving || conflict" class="min-w-0 space-y-5">
        <template v-if="!channelId">
          <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <GatewayNumber
              v-for="field in defaultFields"
              :key="field.key"
              v-model="draft[field.key]"
              :label="t(`gateway.${field.label}`)"
              :max="field.max"
            />
          </div>
          <div class="space-y-4 border-t border-[var(--border-subtle)] pt-4">
            <h4 class="text-sm font-semibold text-[var(--text-primary)]">
              {{ t('gateway.groups') }}
            </h4>
            <div
              v-for="(override, name) in draft.groups"
              :key="name"
              class="grid min-w-0 gap-3 border-b border-[var(--border-subtle)] pb-4 sm:grid-cols-3"
            >
              <div
                class="flex min-w-0 items-center justify-between gap-2 sm:col-span-3"
              >
                <span class="break-all text-sm font-medium">{{ name }}</span
                ><ConsoleButton
                  variant="ghost"
                  size="sm"
                  :aria-label="t('gateway.removeGroup', { name })"
                  :title="t('gateway.removeGroup', { name })"
                  @click="delete draft.groups[name]"
                  ><Trash2 class="h-4 w-4"
                /></ConsoleButton>
              </div>
              <GatewayNumber
                v-for="field in groupFields"
                :key="field.key"
                v-model="override[field.key]"
                :label="t(`gateway.${field.key}`)"
                :max="field.max"
                nullable
              />
            </div>
            <div class="flex min-w-0 gap-2">
              <TextInput
                v-model="groupName"
                :aria-label="t('gateway.groupName')"
                :placeholder="t('gateway.groupName')"
                class="min-w-0 flex-1"
              /><ConsoleButton
                variant="secondary"
                :disabled="
                  !groupName.trim() ||
                  Object.hasOwn(draft.groups, groupName.trim())
                "
                :title="t('gateway.addGroup')"
                :aria-label="t('gateway.addGroup')"
                @click="addGroup"
                ><Plus class="h-4 w-4"
              /></ConsoleButton>
            </div>
          </div>
        </template>
        <div class="space-y-4 border-t border-[var(--border-subtle)] pt-4">
          <h4
            v-if="!channelId"
            class="text-sm font-semibold text-[var(--text-primary)]"
          >
            {{ t('gateway.providerLimits') }}
          </h4>
          <div
            v-for="row in rateRows"
            :key="`${row.kind}:${row.id}`"
            class="grid min-w-0 gap-3 border-b border-[var(--border-subtle)] pb-4 sm:grid-cols-2"
          >
            <div
              class="flex min-w-0 items-center justify-between gap-2 sm:col-span-2"
            >
              <div class="min-w-0">
                <span class="text-xs text-[var(--text-tertiary)]">{{
                  t(`gateway.${row.kind}`)
                }}</span>
                <p
                  class="break-all font-mono text-xs text-[var(--text-primary)]"
                >
                  {{ row.id }}
                </p>
              </div>
              <ConsoleButton
                variant="ghost"
                size="sm"
                :aria-label="t('gateway.removeRate')"
                :title="t('gateway.removeRate')"
                @click="delete draft[row.kind][row.id]"
                ><Trash2 class="h-4 w-4"
              /></ConsoleButton>
            </div>
            <GatewayNumber
              v-for="field in rateFields"
              :key="field"
              v-model="row.limit[field]"
              :label="t(`gateway.${field}`)"
              :max="1000000000"
            />
          </div>
          <p
            v-if="!rateRows.length"
            class="text-sm text-[var(--text-tertiary)]"
          >
            {{ t('gateway.noRateLimits') }}
          </p>
          <div
            class="grid min-w-0 gap-2 sm:grid-cols-[160px_minmax(0,1fr)_auto]"
          >
            <select
              v-model="scopeKind"
              :aria-label="t('gateway.scope')"
              class="h-10 min-w-0 rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-3 text-sm text-[var(--text-primary)] focus-ring"
            >
              <option value="channel_rates">
                {{ t('gateway.channel_rates') }}
              </option>
              <option value="credential_rates">
                {{ t('gateway.credential_rates') }}
              </option>
            </select>
            <select
              v-if="channelId && scopeKind === 'credential_rates'"
              v-model="scopeID"
              :aria-label="t('gateway.credentialID')"
              class="h-10 w-full min-w-0 rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-2 font-mono text-xs text-[var(--text-primary)] focus-ring"
            >
              <option value="">{{ t('gateway.selectCredential') }}</option>
              <option v-for="id in availableCredentials" :key="id" :value="id">
                {{ id }}
              </option>
            </select>
            <TextInput
              v-else
              v-model="scopeID"
              :disabled="Boolean(channelId)"
              :placeholder="
                channelId
                  ? String(channelId)
                  : t(
                      scopeKind === 'channel_rates'
                        ? 'gateway.channelID'
                        : 'gateway.credentialID'
                    )
              "
              :aria-label="
                t(
                  scopeKind === 'channel_rates'
                    ? 'gateway.channelID'
                    : 'gateway.credentialID'
                )
              "
              class="min-w-0"
            />
            <ConsoleButton
              variant="secondary"
              :disabled="!canAddRate"
              :title="t('gateway.addRate')"
              :aria-label="t('gateway.addRate')"
              @click="addRate"
              ><Plus class="h-4 w-4"
            /></ConsoleButton>
          </div>
        </div>
      </fieldset>
      <ConsoleButton
        type="submit"
        :loading="saving"
        :disabled="!dirty || !valid || conflict"
        ><Save class="h-4 w-4" />{{ t('gateway.saveLimits') }}</ConsoleButton
      >
    </form>
  </section>
</template>
