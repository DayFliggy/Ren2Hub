<script setup lang="ts">
import { computed, inject, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, routerKey } from 'vue-router'
import {
  channelUpstreamRatioApi,
  type ChannelUpstreamRatio,
  type ChannelUpstreamRatioUpdate,
  type UpstreamRatioMode,
} from '@/api/channelUpstreamRatio'
import { ApiError } from '@/api/types'
import { upstreamsApi, type UpstreamBinding } from '@/api/upstreams'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import { upstreamErrorText } from '@/features/upstreams/lib/upstreamText'
import type { AdminChannel } from '@/types/console'
import { isValidChannelRatio } from '@/utils/channelRatio'
import { compareDecimal, isDecimal, multiplyDecimal } from '@/utils/decimal'
import { relativeTime } from '@/utils/format'
import {
  upstreamRatioErrorText,
  upstreamRatioStatusTone,
  upstreamRatioValue,
} from './upstreamRatioText'

const props = withDefaults(
  defineProps<{
    channel: AdminChannel | null
    canOperate: boolean
    canWrite: boolean
    /** Upstream account binding; ratio sync then writes the channel ratio. */
    binding?: UpstreamBinding | null
    /** Display name of the bound upstream account, when loaded. */
    accountName?: string
    /** True when the operator holds both upstream:write and channel:sensitive_write. */
    canManageBinding?: boolean
  }>(),
  {
    binding: null,
    accountName: '',
    canManageBinding: false,
  }
)
const emit = defineEmits<{
  close: []
  saved: [view: ChannelUpstreamRatio]
  'binding-updated': []
}>()
const { t, te, locale } = useI18n()
const toast = useToast()
const hasRouter = Boolean(inject(routerKey, null))
const request = useLatestRequest()
const view = ref<ChannelUpstreamRatio | null>(null)
const mode = ref<UpstreamRatioMode>('auto')
const manualRatio = ref('')
const groupName = ref('')
const busy = ref(false)
const error = ref('')
const bindingBusy = ref(false)
const bindingError = ref('')
const syncDraft = reactive({ enabled: false, factor: '1' })

const activeBinding = computed(() => {
  const b = props.binding
  if (!b || b.channel_id !== props.channel?.id) return null
  return b
})

const syncNote = computed(() => {
  const binding = activeBinding.value
  if (!binding || binding.stale || !binding.ratio_sync_enabled) return ''
  return t('upstreams.channelLink.ratioDialogSync', {
    factor: binding.ratio_sync_factor,
  })
})

watch(
  activeBinding,
  (next) => {
    bindingError.value = ''
    if (!next) return
    syncDraft.enabled = next.ratio_sync_enabled
    syncDraft.factor = next.ratio_sync_factor
  },
  { immediate: true }
)

const syncFactorValid = computed(() =>
  isValidChannelRatio(syncDraft.factor.trim())
)
const syncDirty = computed(() => {
  const b = activeBinding.value
  if (!b) return false
  return (
    syncDraft.enabled !== b.ratio_sync_enabled ||
    syncDraft.factor.trim() !== b.ratio_sync_factor
  )
})
const currentUpstreamRatio = computed(() =>
  view.value ? upstreamRatioValue(view.value) : ''
)
const previewTargetRatio = computed(() => {
  const upstream = currentUpstreamRatio.value
  const factor = syncDraft.factor.trim()
  if (
    !upstream ||
    !isDecimal(upstream) ||
    compareDecimal(upstream, '0') <= 0 ||
    !isValidChannelRatio(factor)
  ) {
    return ''
  }
  const product = multiplyDecimal(upstream, factor)
  if (
    compareDecimal(product, '0') <= 0 ||
    compareDecimal(product, '1000') > 0
  ) {
    return ''
  }
  return product
})

async function saveBindingSync() {
  const b = activeBinding.value
  if (
    !b ||
    !props.canManageBinding ||
    !syncDirty.value ||
    !syncFactorValid.value
  )
    return
  bindingBusy.value = true
  bindingError.value = ''
  try {
    await upstreamsApi.updateBinding(b.account_id, b.channel_id, {
      ...(syncDraft.enabled !== b.ratio_sync_enabled
        ? { ratio_sync_enabled: syncDraft.enabled }
        : {}),
      ...(syncDraft.factor.trim() !== b.ratio_sync_factor
        ? { ratio_sync_factor: syncDraft.factor.trim() }
        : {}),
    })
    toast.success(t('upstreams.bindings.saved'))
    emit('binding-updated')
    load()
  } catch (cause) {
    bindingError.value = upstreamErrorText(cause, t, te)
  } finally {
    bindingBusy.value = false
  }
}

async function reverifyBinding() {
  const b = activeBinding.value
  if (!b || !props.canManageBinding) return
  bindingBusy.value = true
  bindingError.value = ''
  try {
    await upstreamsApi.reverifyBinding(b.account_id, b.channel_id)
    toast.success(t('upstreams.bindings.reverified'))
    emit('binding-updated')
    load()
  } catch (cause) {
    bindingError.value = upstreamErrorText(cause, t, te)
  } finally {
    bindingBusy.value = false
  }
}

const mappableGroups = computed(() =>
  (view.value?.groups ?? []).filter(
    (group) => group.name !== 'auto' && group.ratio !== null
  )
)
const canMapGroup = computed(
  () => view.value?.source === 'newapi' && mappableGroups.value.length > 0
)
// 与服务端一致：最多 18 位小数，数值不超过 1000。按字符串精确比较，避免 Number 舍入。
const manualValid = computed(() => {
  const value = manualRatio.value
  if (!isDecimal(value)) return false
  const [whole = '', fraction = ''] = value.split('.')
  if (fraction.length > 18) return false
  if (whole.length < 4) return true
  return whole === '1000' && /^0*$/.test(fraction)
})
const canSave = computed(
  () =>
    props.canWrite &&
    view.value !== null &&
    !busy.value &&
    (mode.value === 'auto' || manualValid.value)
)

function reset(next: ChannelUpstreamRatio) {
  view.value = next
  mode.value = next.mode
  manualRatio.value = next.mode === 'manual' ? next.ratio : ''
  groupName.value = next.mapping === 'manual' ? next.group_name : ''
}

function messageOf(cause: unknown): string {
  if (cause instanceof ApiError && cause.status === 409)
    return t('channels.upstreamRatio.conflict')
  return cause instanceof Error ? cause.message : String(cause)
}

async function run(
  task: (signal: AbortSignal) => Promise<ChannelUpstreamRatio>
) {
  busy.value = true
  error.value = ''
  const result = await request.run(task)
  if (result.stale) return null
  busy.value = false
  if (result.ok) {
    reset(result.value)
    return result.value
  }
  // load() clears error synchronously, so the message is set after it.
  if (result.error instanceof ApiError && result.error.status === 409) load()
  error.value = messageOf(result.error)
  return null
}

function load() {
  const id = props.channel?.id
  if (id) void run((signal) => channelUpstreamRatioApi.get(id, signal))
}

async function refresh() {
  const id = props.channel?.id
  if (!id || !props.canOperate) return
  const next = await run((signal) =>
    channelUpstreamRatioApi.refresh(id, signal)
  )
  if (next) emit('saved', next)
}

function buildUpdate(
  current: ChannelUpstreamRatio
): ChannelUpstreamRatioUpdate {
  if (mode.value === 'manual')
    return {
      mode: 'manual',
      ratio: manualRatio.value,
      version: current.version,
    }
  const update: ChannelUpstreamRatioUpdate = {
    mode: 'auto',
    version: current.version,
  }
  if (canMapGroup.value) update.group_name = groupName.value
  return update
}

async function save() {
  const id = props.channel?.id
  const current = view.value
  if (!id || !current || !canSave.value) return
  const update = buildUpdate(current)
  const next = await run((signal) =>
    channelUpstreamRatioApi.update(id, update, signal)
  )
  if (next) emit('saved', next)
}

watch(
  () => props.channel?.id,
  (id) => {
    request.cancel()
    view.value = null
    busy.value = false
    error.value = ''
    if (id) load()
  },
  { immediate: true }
)
</script>
<template>
  <ConsoleModal
    :open="channel !== null"
    :title="t('channels.upstreamRatio.title', { name: channel?.name ?? '' })"
    footer-border
    @close="emit('close')"
  >
    <p class="mb-4 text-sm text-[var(--text-secondary)]">
      {{ t('channels.upstreamRatio.hint') }}
    </p>
    <p
      v-if="syncNote"
      role="note"
      class="mb-4 rounded-lg bg-[var(--surface-muted)] px-3 py-2 text-sm text-[var(--text-secondary)]"
      data-upstream-ratio-sync-note
    >
      {{ syncNote }}
    </p>
    <section
      v-if="activeBinding && canManageBinding"
      data-upstream-binding-controls
      class="mb-4 space-y-3 rounded-xl border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3.5 text-sm"
    >
      <div class="flex flex-wrap items-center justify-between gap-2">
        <span class="font-semibold text-[var(--text-primary)]">
          {{ t('upstreams.channelLink.syncSectionTitle') }}
        </span>
        <RouterLink
          v-if="hasRouter"
          :to="{
            name: 'upstreams',
            query: {
              account: String(activeBinding.account_id),
              tab: 'bindings',
            },
          }"
          class="text-xs font-medium text-[var(--accent-text)] hover:underline"
        >
          {{
            accountName
              ? t('upstreams.channelLink.accountNamed', {
                  name: accountName,
                  id: activeBinding.account_id,
                })
              : t('upstreams.channelLink.account', {
                  id: activeBinding.account_id,
                })
          }}
        </RouterLink>
      </div>
      <div
        v-if="activeBinding.stale"
        class="flex flex-wrap items-center justify-between gap-2 rounded-lg bg-[var(--status-warning-soft)] px-3 py-2 text-xs text-[var(--status-warning-text)]"
      >
        <span>{{ t('upstreams.bindings.stale') }}</span>
        <ConsoleButton
          size="sm"
          variant="secondary"
          :loading="bindingBusy"
          data-upstream-ratio-reverify
          @click="reverifyBinding"
        >
          {{ t('upstreams.bindings.reverify') }}
        </ConsoleButton>
      </div>
      <template v-else>
        <div class="flex flex-wrap items-end gap-3">
          <ConsoleToggle
            v-model="syncDraft.enabled"
            :label="t('upstreams.bindings.ratioSync')"
            name="ratio-dialog-sync-enabled"
            :disabled="bindingBusy"
          />
          <label class="w-28">
            <span class="mb-1 block text-xs text-[var(--text-tertiary)]">
              {{ t('upstreams.bindings.factor') }}
            </span>
            <TextInput
              v-model="syncDraft.factor"
              name="ratio-dialog-sync-factor"
              inputmode="decimal"
              :readonly="bindingBusy"
            />
          </label>
          <ConsoleButton
            size="sm"
            variant="secondary"
            :disabled="!syncDirty || !syncFactorValid || bindingBusy"
            :loading="bindingBusy"
            data-upstream-ratio-save-sync
            @click="saveBindingSync"
          >
            {{ t('upstreams.bindings.save') }}
          </ConsoleButton>
        </div>
        <p
          v-if="!syncFactorValid"
          class="text-xs text-[var(--status-danger-text)]"
        >
          {{ t('upstreams.channelForm.factorInvalid') }}
        </p>
        <p
          v-else-if="syncDraft.enabled && previewTargetRatio"
          class="font-mono text-xs tabular-nums text-[var(--text-secondary)]"
          data-upstream-ratio-preview
        >
          {{
            t('upstreams.bindings.ratioFormula', {
              upstream: currentUpstreamRatio,
              factor: syncDraft.factor.trim(),
              target: previewTargetRatio,
            })
          }}
          <template v-if="channel?.channel_ratio">
            ·
            {{
              t('upstreams.bindings.channelRatio', {
                current: channel.channel_ratio,
              })
            }}
          </template>
        </p>
        <p
          v-else-if="!syncDraft.enabled"
          class="text-xs text-[var(--text-tertiary)]"
        >
          {{ t('upstreams.channelLink.syncDisabled') }}
        </p>
      </template>
      <p
        v-if="bindingError"
        role="alert"
        class="text-xs text-[var(--status-danger-text)]"
      >
        {{ bindingError }}
      </p>
    </section>
    <p
      v-if="error"
      role="alert"
      class="mb-3 break-words text-sm text-[var(--status-danger-text)]"
    >
      {{ error }}
    </p>
    <p v-if="busy && !view" role="status">{{ t('common.loading') }}</p>
    <div v-if="view" class="space-y-4 text-sm">
      <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
        <dt class="text-[var(--text-tertiary)]">
          {{ t('channels.upstreamRatio.source') }}
        </dt>
        <dd>
          {{ t(`channels.upstreamRatio.sources.${view.source || view.kind}`) }}
        </dd>
        <dt class="text-[var(--text-tertiary)]">
          {{ t('channels.upstreamRatio.status') }}
        </dt>
        <dd class="flex flex-wrap items-center gap-1.5">
          <StatusChip :tone="upstreamRatioStatusTone(view.status)">{{
            t(`channels.upstreamRatio.statuses.${view.status || 'none'}`)
          }}</StatusChip>
          <StatusChip v-if="view.stale" tone="warning">{{
            t('channels.upstreamRatio.stale')
          }}</StatusChip>
          <span v-if="view.last_error" class="text-[var(--text-secondary)]">{{
            upstreamRatioErrorText(view.last_error, t)
          }}</span>
        </dd>
        <dt class="text-[var(--text-tertiary)]">
          {{ t('channels.upstreamRatio.value') }}
        </dt>
        <dd class="font-semibold tabular-nums">
          {{
            upstreamRatioValue(view)
              ? `${upstreamRatioValue(view)}×`
              : t('channels.upstreamRatio.empty')
          }}
        </dd>
        <template v-if="view.token_name">
          <dt class="text-[var(--text-tertiary)]">
            {{ t('channels.upstreamRatio.tokenName') }}
          </dt>
          <dd class="break-all">{{ view.token_name }}</dd>
        </template>
        <template v-if="view.group_name">
          <dt class="text-[var(--text-tertiary)]">
            {{ t('channels.upstreamRatio.group') }}
          </dt>
          <dd class="break-all">
            {{ view.group_name }}
            <span v-if="view.mapping" class="text-[var(--text-tertiary)]">{{
              t(`channels.upstreamRatio.mappings.${view.mapping}`)
            }}</span>
          </dd>
        </template>
        <template v-if="view.sub2api">
          <dt class="text-[var(--text-tertiary)]">
            {{ t('channels.upstreamRatio.sub2apiRates') }}
          </dt>
          <dd class="tabular-nums">
            {{
              t('channels.upstreamRatio.sub2apiDetail', {
                group: view.sub2api.group_rate_multiplier,
                user: view.sub2api.user_rate_multiplier ?? '—',
                resolved: view.sub2api.resolved_rate_multiplier,
              })
            }}
            <span v-if="view.sub2api.peak_rate_enabled" class="block">{{
              t('channels.upstreamRatio.peakDetail', {
                start: view.sub2api.peak_start,
                end: view.sub2api.peak_end,
                timezone: view.sub2api.timezone,
                ratio: view.sub2api.peak_rate_multiplier,
              })
            }}</span>
          </dd>
        </template>
        <template v-if="view.received_at">
          <dt class="text-[var(--text-tertiary)]">
            {{ t('channels.upstreamRatio.receivedAt') }}
          </dt>
          <dd>{{ relativeTime(view.received_at, locale) }}</dd>
        </template>
      </dl>
      <fieldset v-if="canWrite" class="space-y-3" :disabled="busy">
        <legend class="mb-2 font-semibold">
          {{ t('channels.upstreamRatio.modeLabel') }}
        </legend>
        <label class="flex cursor-pointer items-center gap-2"
          ><input
            v-model="mode"
            type="radio"
            value="auto"
            class="h-4 w-4 accent-[var(--accent)]"
          />{{ t('channels.upstreamRatio.modes.auto') }}</label
        >
        <label v-if="mode === 'auto' && canMapGroup" class="block">
          <span class="mb-1 block text-[var(--text-secondary)]">{{
            t('channels.upstreamRatio.groupSelect')
          }}</span>
          <select v-model="groupName" class="console-input w-full">
            <option value="">
              {{ t('channels.upstreamRatio.groupByName') }}
            </option>
            <option
              v-for="group in mappableGroups"
              :key="group.name"
              :value="group.name"
            >
              {{ group.name }} · {{ group.ratio }}×{{
                group.description ? ` · ${group.description}` : ''
              }}
            </option>
          </select>
        </label>
        <label class="flex cursor-pointer items-center gap-2"
          ><input
            v-model="mode"
            type="radio"
            value="manual"
            class="h-4 w-4 accent-[var(--accent)]"
          />{{ t('channels.upstreamRatio.modes.manual') }}</label
        >
        <label v-if="mode === 'manual'" class="block">
          <span class="mb-1 block text-[var(--text-secondary)]">{{
            t('channels.upstreamRatio.manualRatio')
          }}</span>
          <input
            v-model.trim="manualRatio"
            inputmode="decimal"
            class="console-input w-full tabular-nums"
            :aria-invalid="manualRatio !== '' && !manualValid"
          />
          <span
            v-if="manualRatio !== '' && !manualValid"
            class="mt-1 block text-[var(--status-danger-text)]"
            >{{ t('channels.upstreamRatio.manualInvalid') }}</span
          >
        </label>
      </fieldset>
    </div>
    <template #footer>
      <div class="flex flex-wrap items-center justify-end gap-3">
        <ConsoleButton
          variant="ghost"
          :disabled="busy && view !== null"
          @click="emit('close')"
          >{{
            canWrite ? t('common.cancel') : t('common.close')
          }}</ConsoleButton
        >
        <ConsoleButton
          v-if="canOperate && view && view.kind !== 'unsupported'"
          variant="secondary"
          :disabled="busy"
          @click="refresh"
          >{{
            view.kind === 'detect' && !view.source
              ? t('channels.upstreamRatio.detect')
              : t('channels.upstreamRatio.refresh')
          }}</ConsoleButton
        >
        <ConsoleButton
          v-if="canWrite"
          :loading="busy && view !== null"
          :disabled="!canSave"
          @click="save"
          >{{ t('common.save') }}</ConsoleButton
        >
      </div>
    </template>
  </ConsoleModal>
</template>
