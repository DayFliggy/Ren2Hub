<script setup lang="ts">
import { computed, onMounted, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { RefreshCw } from 'lucide-vue-next'
import { isUsageDiscountConflict, usageDiscountApi } from '@/api/usageDiscount'
import { ApiError } from '@/api/types'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import UsageDiscountTierTable from '@/components/admin/UsageDiscountTierTable.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'
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
  USAGE_DISCOUNT_HEALTH_KEYS,
  USAGE_DISCOUNT_WINDOWS,
  type UsageDiscountEstimate,
  type UsageDiscountSettings,
  type UsageDiscountStatus,
} from '@/types/usageDiscount'
import { formatQuota } from '@/utils/format'
import '@/components/admin/admin.css'

const { t } = useI18n({ useScope: 'local', messages: usageDiscountMessages })
const { t: tGlobal } = useI18n()
const id = useId()
const RATE_FIELDS = ['cap', 'fixed'] as const

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
    void loadStatus()
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
  void loadStatus()
}

const status = ref<UsageDiscountStatus | null>(null)
const statusLoading = ref(false),
  statusError = ref('')
const statusRequest = useLatestRequest()
async function loadStatus() {
  statusLoading.value = true
  statusError.value = ''
  const run = await statusRequest.run((signal) =>
    usageDiscountApi.status(signal)
  )
  if (run.stale) return
  statusLoading.value = false
  status.value = run.ok ? run.value : null
  if (!run.ok) statusError.value = `${t('status.failed')} ${failure(run.error)}`
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

useUnsavedChanges({
  dirty: () => dirty.value,
  message: () => t('unsaved'),
  busy: () => saving.value,
})
onMounted(() => {
  void load()
  void loadStatus()
})
</script>
<template>
  <section class="usage-discount-page" :aria-label="t('title')">
    <PageBreadcrumb :crumbs="[tGlobal('nav.groupAdmin'), t('title')]">
      <template #action>
        <div class="flex flex-wrap items-center justify-end gap-2">
          <StatusChip v-if="dirty" tone="warning">{{ t('dirty') }}</StatusChip>
          <ConsoleButton
            variant="secondary"
            size="sm"
            :disabled="saving"
            @click="reload"
            ><RefreshCw :size="14" aria-hidden="true" />{{
              t('reload')
            }}</ConsoleButton
          >
          <ConsoleButton
            size="sm"
            :loading="saving"
            :disabled="!canSave"
            @click="save"
            >{{ t('save') }}</ConsoleButton
          >
        </div>
      </template>
    </PageBreadcrumb>

    <ErrorBanner
      v-if="loadError"
      class="mb-4"
      :message="loadError"
      @retry="refresh"
    />
    <div
      v-if="conflict"
      role="alert"
      class="mb-4 flex flex-wrap items-center gap-3 rounded-xl border bg-[var(--status-warning-soft)] px-4 py-2.5 text-sm text-[var(--status-warning-text)]"
      data-discount-conflict
    >
      <span class="mr-auto min-w-0">{{
        conflict.version !== null
          ? t('conflict', { version: conflict.version })
          : conflict.error
            ? t('conflictReadFailed', { reason: conflict.error })
            : t('conflictPending')
      }}</span>
      <ConsoleButton variant="secondary" size="sm" @click="reload">{{
        t('reloadServer')
      }}</ConsoleButton>
    </div>
    <div
      v-if="saveError"
      role="alert"
      class="mb-4 rounded-xl border bg-[var(--status-danger-soft)] px-4 py-2.5 text-sm text-[var(--status-danger-text)]"
    >
      {{ saveError }}
    </div>
    <div
      v-if="message"
      role="status"
      class="mb-4 rounded-xl border bg-[var(--status-success-soft)] px-4 py-2.5 text-sm text-[var(--status-success-text)]"
    >
      {{ message }}
    </div>

    <div class="usage-discount-grid">
      <ConsoleCard
        class="usage-discount-status"
        :title="t('status.title')"
        :aria-label="t('status.title')"
        data-discount-status
      >
        <template #action>
          <ConsoleButton
            variant="ghost"
            size="sm"
            :loading="statusLoading"
            @click="loadStatus"
            >{{ t('status.refresh') }}</ConsoleButton
          >
        </template>
        <p v-if="statusError" class="admin-error" role="alert">
          {{ statusError }}
        </p>
        <div v-else-if="!status" class="grid gap-2">
          <SkeletonBlock v-for="i in 4" :key="i" rounded="md" class="h-5" />
        </div>
        <template v-else>
          <dl class="discount-facts">
            <div>
              <dt>{{ t('status.businessDate') }}</dt>
              <dd>{{ status.business_date }}</dd>
            </div>
            <div>
              <dt>{{ t('status.cutoff') }}</dt>
              <dd>{{ zonedTime(status.cutoff_utc, status.timezone) }}</dd>
            </div>
            <div>
              <dt>{{ t('status.configVersion') }}</dt>
              <dd>{{ status.config_version }}</dd>
            </div>
            <div>
              <dt>{{ t('status.cachedUsers') }}</dt>
              <dd>{{ status.cached_users }}</dd>
            </div>
            <div>
              <dt>{{ t('status.siteMonth') }}</dt>
              <dd>{{ formatQuota(status.site_month_quota) }}</dd>
            </div>
            <div>
              <dt>{{ t('status.siteRate') }}</dt>
              <dd>{{ percent(status.site_tier_rate) }}</dd>
            </div>
            <div class="is-wide">
              <dt>{{ t('status.earliest') }}</dt>
              <dd>
                {{
                  status.earliest_hour_utc === null
                    ? t('status.noHistory')
                    : zonedTime(status.earliest_hour_utc, status.timezone)
                }}
              </dd>
            </div>
          </dl>
          <h3 class="mt-4 text-sm font-semibold">{{ t('status.health') }}</h3>
          <p class="admin-muted">{{ t('status.healthHint') }}</p>
          <dl class="discount-facts mt-2">
            <div
              v-for="key in USAGE_DISCOUNT_HEALTH_KEYS"
              :key="key"
              class="is-wide"
              :data-health="key"
              :data-failing="status.health[key].count > 0 || undefined"
            >
              <dt>{{ t(`status.${key}`) }}</dt>
              <dd>
                <StatusChip
                  :tone="status.health[key].count > 0 ? 'warning' : 'neutral'"
                  >{{ status.health[key].count }}</StatusChip
                >
                <span class="admin-muted ml-2">{{
                  status.health[key].last_at
                    ? t('status.lastAt', {
                        time: zonedTime(
                          status.health[key].last_at,
                          status.timezone
                        ),
                      })
                    : t('status.never')
                }}</span>
              </dd>
            </div>
          </dl>
        </template>
      </ConsoleCard>
      <ConsoleCard
        class="usage-discount-editor"
        :title="t('editor.title')"
        :aria-label="t('editor.title')"
        data-discount-editor
      >
        <div v-if="!draft" class="grid gap-3">
          <SkeletonBlock v-for="i in 5" :key="i" rounded="lg" class="h-10" />
        </div>
        <fieldset
          v-else
          class="usage-discount-form"
          :disabled="loading || saving"
        >
          <p class="admin-muted">{{ versionText }}</p>
          <p class="admin-muted">{{ t('editor.formula') }}</p>
          <div class="admin-fields">
            <div v-for="key in RATE_FIELDS" :key="key" class="admin-field">
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
                <span class="admin-affix-mark is-end" aria-hidden="true"
                  >%</span
                >
              </span>
              <span :id="`${id}-${key}-hint`" class="grid gap-1">
                <span v-if="issue(key)" class="admin-error">{{
                  issue(key)
                }}</span>
                <span class="admin-muted">{{ t(`editor.${key}Hint`) }}</span>
              </span>
            </div>
          </div>
          <div class="admin-section">
            <UsageDiscountTierTable
              table-key="site"
              :table="draft.tables.site"
              :errors="errors"
              @change="draft.tables.site = $event"
            />
          </div>
          <div class="admin-section">
            <div>
              <h3 class="text-sm font-semibold">{{ t('editor.personal') }}</h3>
              <p class="admin-muted">{{ t('editor.personalHint') }}</p>
            </div>
            <UsageDiscountTierTable
              v-for="window in USAGE_DISCOUNT_WINDOWS"
              :key="window"
              :table-key="window"
              :table="draft.tables[window]"
              :errors="errors"
              class="discount-personal-table"
              @change="draft.tables[window] = $event"
            />
          </div>
          <div class="admin-section">
            <div class="admin-field">
              <label :for="`${id}-excluded`">{{ t('editor.excluded') }}</label>
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
          </div>
          <p v-if="dirty && invalid" class="admin-error" role="alert">
            {{ t('invalidDraft') }}
          </p>
        </fieldset>
      </ConsoleCard>
      <ConsoleCard
        class="usage-discount-estimate"
        :title="t('estimate.title')"
        :aria-label="t('estimate.title')"
        data-discount-estimate
      >
        <p class="admin-muted">{{ t('estimate.note') }}</p>
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <ConsoleButton
            variant="secondary"
            size="sm"
            :loading="estimating"
            :disabled="!draft || invalid"
            @click="runEstimate"
            >{{ t('estimate.run') }}</ConsoleButton
          >
          <StatusChip v-if="estimateStale" tone="warning" data-estimate-stale>{{
            t('estimate.stale')
          }}</StatusChip>
        </div>
        <p v-if="estimateError" class="admin-error mt-3" role="alert">
          {{ estimateError }}
        </p>
        <p v-if="!estimate" class="admin-muted mt-3">
          {{ t('estimate.empty') }}
        </p>
        <dl
          v-else-if="draft"
          class="discount-facts mt-3"
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
                class="admin-muted ml-2"
                >{{
                  t('estimate.earliest', {
                    time: zonedTime(estimate.earliest_hour_utc, draft.timezone),
                  })
                }}</span
              >
            </dd>
          </div>
        </dl>
      </ConsoleCard>
    </div>

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
.usage-discount-page {
  min-width: 0;
  container-type: inline-size;
}
.usage-discount-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  grid-template-areas: 'status' 'editor' 'estimate';
  gap: 1.25rem;
  align-items: start;
}
.usage-discount-status {
  grid-area: status;
}
.usage-discount-editor {
  grid-area: editor;
}
.usage-discount-estimate {
  grid-area: estimate;
}
/* Wide pages edit on the left and keep status and estimate beside it. */
@container (min-width: 68rem) {
  .usage-discount-grid {
    grid-template-columns: minmax(0, 1.6fr) minmax(20rem, 1fr);
    grid-template-rows: auto 1fr;
    grid-template-areas: 'editor status' 'editor estimate';
  }
}
.usage-discount-form {
  display: grid;
  gap: 1rem;
  min-width: 0;
  margin: 0;
  border: 0;
  padding: 0;
}
.discount-personal-table + .discount-personal-table {
  border-top: 1px dashed var(--border-subtle);
  padding-top: 0.75rem;
}
.discount-facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.75rem 1rem;
  font-size: 0.8125rem;
}
.discount-facts > .is-wide {
  grid-column: 1 / -1;
}
.discount-facts dt {
  color: var(--text-secondary);
}
.discount-facts dd {
  margin-top: 0.25rem;
  overflow-wrap: anywhere;
  font-variant-numeric: tabular-nums;
  color: var(--text-primary);
}
.discount-facts.is-stale {
  opacity: 0.55;
}
</style>
