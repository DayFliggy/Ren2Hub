<script setup lang="ts">
import { computed, nextTick, onMounted, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { Calculator, ChevronDown, RefreshCw, Users } from 'lucide-vue-next'
import { isUsageDiscountConflict, usageDiscountApi } from '@/api/usageDiscount'
import { ApiError } from '@/api/types'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import UsageDiscountTierTable from '@/components/admin/UsageDiscountTierTable.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { registerSettingsDraft } from './settingsDraftGuard'
import { rateToPercent } from '@/features/usage-discount/amount'
import {
  buildConfig,
  draftFromConfig,
  draftKey,
  type DraftErrors,
  type UsageDiscountDraft,
} from '@/features/usage-discount/draft'
import { usageDiscountMessages } from '@/i18n/usageDiscount'
import {
  USAGE_DISCOUNT_WINDOWS,
  type UsageDiscountEstimate,
  type UsageDiscountSettings,
  type UsageDiscountWindow,
} from '@/types/usageDiscount'
import { formatQuota } from '@/utils/format'
import '@/components/admin/admin.css'

const { t } = useI18n({ useScope: 'local', messages: usageDiscountMessages })
const id = useId()
const RATE_FIELDS = ['fixed', 'cap'] as const
const selectedWindow = ref<UsageDiscountWindow>('day')

// The saved version this draft edits; the draft is the page's only editable
// copy and survives every failed save.
const baseline = ref<UsageDiscountSettings | null>(null)
const draft = ref<UsageDiscountDraft | null>(null)
const savedKey = ref('')
const loading = ref(false),
  saving = ref(false),
  reloadOpen = ref(false)
const loadError = ref(''),
  saveError = ref(''),
  message = ref('')
// Set by a 409. version is the server's current version once it is read.
const conflict = ref<{ version: number | null; error: string } | null>(null)
const configRequest = useLatestRequest()

const result = computed(() => (draft.value ? buildConfig(draft.value) : null))
const errors = computed<DraftErrors>(() =>
  result.value && !result.value.ok ? result.value.errors : {}
)
const dirty = computed(
  () => draft.value !== null && draftKey(draft.value) !== savedKey.value
)
const invalid = computed(() => result.value?.ok === false)
const canSave = computed(
  () =>
    baseline.value !== null &&
    !loading.value &&
    !saving.value &&
    !conflict.value &&
    dirty.value &&
    result.value?.ok === true
)

// Server errors carry English text; known codes get a translated reason.
function failure(cause: unknown) {
  const reasons: Record<string, string> = {
    usage_discount_invalid_config: 'invalidConfig',
    usage_discount_schema_unsupported: 'schemaUnsupported',
    usage_discount_unavailable: 'unavailable',
  }
  const detail = cause instanceof Error ? cause.message : String(cause)
  const key = cause instanceof ApiError && cause.code && reasons[cause.code]
  return key ? `${t(key)} ${detail}` : detail
}
function issue(path: string) {
  const found = errors.value[path]
  return found ? t(found.key, found.params ?? {}) : ''
}

function apply(settings: UsageDiscountSettings) {
  baseline.value = settings
  draft.value = draftFromConfig(settings.config)
  savedKey.value = draftKey(draft.value)
  conflict.value = null
}
async function load() {
  loading.value = true
  loadError.value = ''
  const run = await configRequest.run((signal) => usageDiscountApi.load(signal))
  if (run.stale) return
  loading.value = false
  if (run.ok) apply(run.value)
  else loadError.value = failure(run.error)
}
// A 409 keeps the draft and reads the server version, which only a reload
// may adopt.
async function recordConflict() {
  conflict.value = { version: null, error: '' }
  const run = await configRequest.run((signal) => usageDiscountApi.load(signal))
  if (run.stale || !conflict.value) return
  conflict.value = run.ok
    ? { version: run.value.version, error: '' }
    : { version: null, error: failure(run.error) }
}
async function save() {
  if (!canSave.value || !baseline.value || !result.value?.ok) return
  saving.value = true
  saveError.value = ''
  message.value = ''
  try {
    const saved = await usageDiscountApi.save(
      baseline.value.version,
      result.value.config
    )
    apply(saved)
    message.value = t('saved', { version: saved.version })
  } catch (cause) {
    if (isUsageDiscountConflict(cause)) void recordConflict()
    else saveError.value = `${t('saveFailed')} ${failure(cause)}`
  } finally {
    saving.value = false
  }
}
function reload() {
  if (saving.value) return
  if (dirty.value) reloadOpen.value = true
  else refresh()
}
function confirmReload() {
  reloadOpen.value = false
  refresh()
}
function refresh() {
  message.value = ''
  saveError.value = ''
  void load()
}

// An estimate belongs to the draft text it ran on; any edit makes it stale.
const estimate = ref<UsageDiscountEstimate | null>(null)
const estimateKey = ref('')
const estimating = ref(false),
  estimateError = ref('')
const estimateRequest = useLatestRequest()
const estimateStale = computed(
  () =>
    estimate.value !== null &&
    draft.value !== null &&
    draftKey(draft.value) !== estimateKey.value
)
async function runEstimate() {
  if (!draft.value || !result.value?.ok) return
  const key = draftKey(draft.value)
  const config = result.value.config
  estimating.value = true
  estimateError.value = ''
  const run = await estimateRequest.run((signal) =>
    usageDiscountApi.estimate(config, signal)
  )
  if (run.stale) return
  estimating.value = false
  if (run.ok) {
    estimate.value = run.value
    estimateKey.value = key
  } else estimateError.value = `${t('estimate.failed')} ${failure(run.error)}`
}

// Times are shown in the business time zone, which defines the cutoff.
function zonedTime(epoch: number, timeZone: string) {
  const text = new Intl.DateTimeFormat('sv-SE', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).format(new Date(epoch * 1000))
  return `${text} (${timeZone})`
}
function percent(rate: string) {
  return `${rateToPercent(rate)}%`
}
const versionText = computed(() => {
  const settings = baseline.value
  if (!settings) return ''
  if (!settings.created_at) return t('editor.builtIn')
  return `${t('editor.version', { version: settings.version })} · ${t(
    'editor.savedAt',
    { time: zonedTime(settings.created_at, settings.config.timezone) }
  )}`
})

function windowErrors(window: UsageDiscountWindow) {
  return Object.keys(errors.value).filter(
    (key) => key === window || key.startsWith(`${window}.`)
  ).length
}
async function onWindowKeydown(event: KeyboardEvent, index: number) {
  let next: number
  if (event.key === 'ArrowRight') next = (index + 1) % 3
  else if (event.key === 'ArrowLeft') next = (index + 2) % 3
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = 2
  else return
  event.preventDefault()
  selectedWindow.value = USAGE_DISCOUNT_WINDOWS[next]!
  await nextTick()
  document.getElementById(`${id}-tab-${selectedWindow.value}`)?.focus()
}

registerSettingsDraft({
  dirty: () => dirty.value,
  busy: () => saving.value,
})
onMounted(load)
</script>
<template>
  <section
    class="discount-settings"
    :aria-label="t('title')"
    data-discount-editor
  >
    <header class="discount-heading">
      <div>
        <h2 class="display-title">{{ t('title') }}</h2>
        <p>{{ t('description') }}</p>
      </div>
      <span class="discount-scope">{{ t('editor.usagePricing') }}</span>
    </header>

    <ErrorBanner v-if="loadError" :message="loadError" @retry="refresh" />
    <div
      v-if="conflict"
      role="alert"
      class="discount-alert is-warning"
      data-discount-conflict
    >
      <p>
        {{
          conflict.version !== null
            ? t('conflict', { version: conflict.version })
            : conflict.error
              ? t('conflictReadFailed', { reason: conflict.error })
              : t('conflictPending')
        }}
      </p>
      <ConsoleButton
        variant="secondary"
        size="sm"
        :disabled="loading || saving"
        @click="reload"
        >{{ t('reloadServer') }}</ConsoleButton
      >
    </div>
    <p v-if="saveError" role="alert" class="discount-alert is-danger">
      {{ saveError }}
    </p>
    <p v-if="message && !dirty" role="status" class="discount-alert is-success">
      {{ message }}
    </p>

    <div
      v-if="!draft && loading"
      class="discount-loading"
      :aria-label="t('loading')"
      role="status"
    >
      <SkeletonBlock v-for="i in 4" :key="i" rounded="lg" class="h-20" />
    </div>
    <fieldset v-if="draft" class="discount-form" :disabled="loading || saving">
      <section
        class="discount-block discount-basics"
        :aria-labelledby="`${id}-composition`"
      >
        <div class="discount-block-heading">
          <div>
            <h3 :id="`${id}-composition`">{{ t('editor.composition') }}</h3>
            <p class="admin-muted">{{ t('editor.formula') }}</p>
          </div>
        </div>
        <div class="discount-rate-fields">
          <div
            v-for="key in RATE_FIELDS"
            :key="key"
            class="admin-field discount-rate-field"
          >
            <label :for="`${id}-${key}`">{{ t(`editor.${key}`) }}</label>
            <span class="admin-affix">
              <input
                :id="`${id}-${key}`"
                v-model="draft[key]"
                class="has-end"
                inputmode="decimal"
                :aria-invalid="Boolean(issue(key))"
                :aria-describedby="`${id}-${key}-hint`"
              />
              <span class="admin-affix-mark is-end" aria-hidden="true">%</span>
            </span>
            <span :id="`${id}-${key}-hint`" class="grid gap-1">
              <span v-if="issue(key)" class="admin-error">{{
                issue(key)
              }}</span>
              <span class="admin-muted">{{ t(`editor.${key}Hint`) }}</span>
            </span>
          </div>
        </div>
      </section>

      <section class="discount-block">
        <UsageDiscountTierTable
          table-key="site"
          :table="draft.tables.site"
          :errors="errors"
          @change="draft.tables.site = $event"
        />
      </section>

      <section
        class="discount-block discount-personal"
        :aria-labelledby="`${id}-personal`"
      >
        <div class="discount-block-heading">
          <div>
            <h3 :id="`${id}-personal`">{{ t('editor.personal') }}</h3>
            <p class="admin-muted">{{ t('editor.personalHint') }}</p>
          </div>
          <span class="discount-scope">{{ t('editor.highestOnly') }}</span>
        </div>
        <div
          class="discount-window-tabs"
          role="tablist"
          :aria-label="t('editor.personal')"
        >
          <button
            v-for="(window, index) in USAGE_DISCOUNT_WINDOWS"
            :id="`${id}-tab-${window}`"
            :key="window"
            type="button"
            role="tab"
            class="discount-window-tab focus-ring"
            :aria-selected="selectedWindow === window"
            :aria-controls="`${id}-window-${window}`"
            :tabindex="selectedWindow === window ? 0 : -1"
            :data-invalid="windowErrors(window) > 0 || undefined"
            @click="selectedWindow = window"
            @keydown="onWindowKeydown($event, index)"
          >
            <span>{{ t(`editor.windows.${window}`) }}</span>
            <span class="discount-window-state">{{
              windowErrors(window)
                ? t('editor.errorCount', { count: windowErrors(window) })
                : t(`table.modes.${draft.tables[window].mode}`)
            }}</span>
          </button>
        </div>
        <div
          v-for="window in USAGE_DISCOUNT_WINDOWS"
          v-show="selectedWindow === window"
          :id="`${id}-window-${window}`"
          :key="window"
          role="tabpanel"
          :aria-labelledby="`${id}-tab-${window}`"
        >
          <UsageDiscountTierTable
            :table-key="window"
            :table="draft.tables[window]"
            :errors="errors"
            @change="draft.tables[window] = $event"
          />
        </div>
      </section>

      <details
        class="discount-block discount-disclosure"
        data-discount-exclusions
      >
        <summary class="focus-ring">
          <Users :size="17" aria-hidden="true" />
          <span class="discount-disclosure-label"
            ><span>{{ t('editor.excluded') }}</span
            ><small>{{ t('editor.excludedSummary') }}</small></span
          >
          <StatusChip v-if="issue('excluded')" tone="warning">{{
            t('editor.needsReview')
          }}</StatusChip>
          <ChevronDown class="discount-chevron" :size="16" aria-hidden="true" />
        </summary>
        <div class="admin-field discount-disclosure-content">
          <label :for="`${id}-excluded`">{{ t('editor.userIds') }}</label>
          <textarea
            :id="`${id}-excluded`"
            v-model="draft.excluded"
            rows="2"
            :aria-invalid="Boolean(issue('excluded'))"
            :aria-describedby="`${id}-excluded-hint`"
          />
          <span :id="`${id}-excluded-hint`" class="grid gap-1">
            <span v-if="issue('excluded')" class="admin-error">{{
              issue('excluded')
            }}</span>
            <span class="admin-muted">{{ t('editor.excludedHint') }}</span>
          </span>
        </div>
      </details>
    </fieldset>

    <details class="discount-block discount-disclosure" data-discount-estimate>
      <summary class="focus-ring">
        <Calculator :size="17" aria-hidden="true" />
        <span class="discount-disclosure-label"
          ><span>{{ t('estimate.title') }}</span
          ><small>{{ t('estimate.summary') }}</small></span
        >
        <ChevronDown class="discount-chevron" :size="16" aria-hidden="true" />
      </summary>
      <div class="discount-disclosure-content">
        <p class="admin-muted">{{ t('estimate.note') }}</p>
        <div class="discount-estimate-actions">
          <ConsoleButton
            variant="secondary"
            size="sm"
            :loading="estimating"
            :disabled="!draft || invalid || loading || saving"
            @click="runEstimate"
            >{{ t('estimate.run') }}</ConsoleButton
          >
          <StatusChip v-if="estimateStale" tone="warning" data-estimate-stale>{{
            t('estimate.stale')
          }}</StatusChip>
        </div>
        <p v-if="estimateError" class="admin-error" role="alert">
          {{ estimateError }}
        </p>
        <p v-if="!estimate" class="admin-muted">{{ t('estimate.empty') }}</p>
        <dl
          v-else-if="draft"
          class="discount-facts"
          :class="{ 'is-stale': estimateStale }"
          data-estimate-result
        >
          <div class="is-wide">
            <dt>{{ t('estimate.period') }}</dt>
            <dd>
              {{
                t('estimate.periodValue', {
                  from: estimate.from_date,
                  to: estimate.to_date,
                  days: estimate.days,
                })
              }}
            </dd>
          </div>
          <div>
            <dt>{{ t('estimate.revenue') }}</dt>
            <dd>{{ formatQuota(estimate.revenue_quota) }}</dd>
          </div>
          <div>
            <dt>{{ t('estimate.discount') }}</dt>
            <dd>{{ formatQuota(estimate.discount_quota) }}</dd>
          </div>
          <div>
            <dt>{{ t('estimate.discountShare') }}</dt>
            <dd>{{ percent(estimate.discount_share) }}</dd>
          </div>
          <div>
            <dt>{{ t('estimate.cappedShare') }}</dt>
            <dd>{{ percent(estimate.capped_revenue_share) }}</dd>
          </div>
          <div class="is-wide">
            <dt>{{ t('estimate.history') }}</dt>
            <dd>
              <StatusChip
                :tone="estimate.history_complete ? 'success' : 'warning'"
                >{{
                  t(
                    estimate.history_complete
                      ? 'estimate.historyComplete'
                      : 'estimate.historyIncomplete'
                  )
                }}</StatusChip
              >
              <span
                v-if="estimate.earliest_hour_utc !== null"
                class="admin-muted"
                >{{
                  t('estimate.earliest', {
                    time: zonedTime(estimate.earliest_hour_utc, draft.timezone),
                  })
                }}</span
              >
            </dd>
          </div>
        </dl>
      </div>
    </details>

    <footer class="discount-save-bar">
      <div class="discount-save-state" aria-live="polite">
        <span v-if="dirty" class="discount-dirty">{{ t('dirty') }}</span>
        <span v-else class="admin-muted">{{ versionText }}</span>
        <span v-if="invalid" class="admin-error" role="alert">{{
          t('invalidDraft')
        }}</span>
        <span v-else-if="dirty" class="admin-muted">{{ t('saveHint') }}</span>
      </div>
      <div class="discount-save-actions">
        <ConsoleButton
          variant="secondary"
          size="sm"
          :disabled="saving || loading"
          @click="reload"
        >
          <RefreshCw :size="14" aria-hidden="true" />{{ t('reload') }}
        </ConsoleButton>
        <ConsoleButton
          size="sm"
          :loading="saving"
          :disabled="!canSave"
          @click="save"
          >{{ t('save') }}</ConsoleButton
        >
      </div>
    </footer>

    <ConfirmDialog
      :open="reloadOpen"
      :title="t('reload')"
      :message="t('reloadConfirm')"
      :confirm-text="t('reload')"
      @confirm="confirmReload"
      @cancel="reloadOpen = false"
    />
  </section>
</template>

<style scoped>
.discount-settings {
  min-width: 0;
  display: grid;
  gap: 1rem;
  container-type: inline-size;
  color: var(--text-primary);
}
.discount-heading,
.discount-block-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
}
.discount-heading {
  padding: 0.25rem 0;
}
.discount-heading h2 {
  font-size: 1.25rem;
  font-weight: 700;
}
.discount-heading p {
  margin-top: 0.375rem;
  color: var(--text-secondary);
  font-size: 0.8125rem;
  line-height: 1.6;
}
.discount-block-heading h3 {
  margin-bottom: 0.25rem;
  font-size: 0.9375rem;
  font-weight: 650;
}
.discount-scope {
  flex-shrink: 0;
  border: 1px solid var(--border-subtle);
  border-radius: 999px;
  padding: 0.25rem 0.625rem;
  color: var(--text-secondary);
  font-size: 0.6875rem;
  line-height: 1.5;
}
.discount-form {
  display: grid;
  gap: 1rem;
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}
.discount-block {
  min-width: 0;
  padding: 1.25rem;
  border: 1px solid var(--border-subtle);
  border-radius: 1rem;
  background: var(--surface-solid);
  box-shadow: var(--elevation-1);
}
.discount-basics {
  background:
    linear-gradient(125deg, var(--accent-soft), transparent 70%),
    var(--surface-solid);
}
.discount-rate-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1.5rem;
  margin-top: 1rem;
}
.discount-rate-field input {
  min-height: 3rem;
  font-size: 1.25rem;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
}
.discount-rate-field label {
  color: var(--text-primary);
}
.discount-personal {
  display: grid;
  gap: 1rem;
}
.discount-window-tabs {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.375rem;
  border-radius: 0.875rem;
  padding: 0.375rem;
  background: var(--surface-muted);
}
.discount-window-tab {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 0.25rem 0.625rem;
  min-width: 0;
  min-height: 3rem;
  padding: 0.5rem;
  border: 1px solid transparent;
  border-radius: 0.625rem;
  color: var(--text-secondary);
  font-size: 0.8125rem;
  font-weight: 650;
}
.discount-window-tab[aria-selected='true'] {
  background: var(--surface-solid);
  border-color: var(--border-default);
  color: var(--accent-text);
  box-shadow: var(--elevation-1);
}
.discount-window-state {
  font-size: 0.6875rem;
  font-weight: 400;
}
.discount-window-tab[data-invalid] .discount-window-state {
  color: var(--status-danger-text);
  font-weight: 650;
}
.discount-disclosure {
  padding: 0;
}
.discount-disclosure summary {
  min-height: 4rem;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 1rem 1.25rem;
  border-radius: 1rem;
  cursor: pointer;
  list-style: none;
}
.discount-disclosure summary::-webkit-details-marker {
  display: none;
}
.discount-disclosure summary > svg {
  flex-shrink: 0;
  color: var(--text-secondary);
}
.discount-disclosure-label {
  display: grid;
  gap: 0.25rem;
  min-width: 0;
  font-size: 0.875rem;
  font-weight: 600;
}
.discount-disclosure-label small {
  color: var(--text-secondary);
  font-size: 0.75rem;
  font-weight: 400;
}
.discount-chevron {
  margin-left: auto;
  transition: transform 0.15s;
}
.discount-disclosure[open] .discount-chevron {
  transform: rotate(180deg);
}
.discount-disclosure-content {
  padding: 0 1.25rem 1.25rem;
}
.discount-estimate-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem;
  margin: 1rem 0;
}
.discount-alert {
  padding: 0.875rem 1rem;
  border: 1px solid currentColor;
  border-radius: 0.75rem;
  font-size: 0.8125rem;
  line-height: 1.6;
  overflow-wrap: anywhere;
}
.discount-alert button {
  margin-top: 0.75rem;
}
.is-warning {
  background: var(--status-warning-soft);
  color: var(--status-warning-text);
}
.is-danger {
  background: var(--status-danger-soft);
  color: var(--status-danger-text);
}
.is-success {
  background: var(--status-success-soft);
  color: var(--status-success-text);
}
.discount-loading {
  display: grid;
  gap: 1rem;
}
.discount-save-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.875rem 1rem;
  border: 1px solid var(--border-default);
  border-radius: 1rem;
  background: var(--surface-solid);
  box-shadow: var(--card-shadow);
}
.discount-save-state {
  display: grid;
  gap: 0.25rem;
  min-width: 0;
  font-size: 0.8125rem;
  overflow-wrap: anywhere;
}
.discount-dirty {
  color: var(--status-warning-text);
  font-weight: 600;
}
.discount-save-actions {
  display: flex;
  flex-shrink: 0;
  gap: 0.625rem;
}
.discount-facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
  margin-top: 1rem;
  font-size: 0.8125rem;
}
.discount-facts > .is-wide {
  grid-column: 1 / -1;
}
.discount-facts dt {
  color: var(--text-secondary);
}
.discount-facts dd {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  margin-top: 0.25rem;
  overflow-wrap: anywhere;
  font-variant-numeric: tabular-nums;
  color: var(--text-primary);
}
.discount-facts.is-stale {
  opacity: 0.65;
}
@container (max-width: 30rem) {
  .discount-heading {
    flex-wrap: wrap;
  }
  .discount-block {
    padding: 1rem;
  }
  .discount-rate-fields {
    gap: 0.875rem;
  }
  .discount-block-heading {
    flex-wrap: wrap;
    gap: 0.5rem;
  }
  .discount-window-tab {
    flex-direction: column;
  }
  .discount-disclosure {
    padding: 0;
  }
  .discount-disclosure summary {
    padding: 1rem;
  }
  .discount-disclosure-content {
    padding: 0 1rem 1rem;
  }
  .discount-save-bar {
    flex-wrap: wrap;
    gap: 0.75rem;
  }
  .discount-save-actions {
    width: 100%;
  }
  .discount-save-actions > button {
    flex: 1;
  }
  .discount-save-state:empty {
    display: none;
  }
}
@media (prefers-reduced-motion: reduce) {
  .discount-chevron {
    transition: none;
  }
}
</style>
