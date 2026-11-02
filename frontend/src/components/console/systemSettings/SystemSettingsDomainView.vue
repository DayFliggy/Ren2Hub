<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import PasskeyDomainsPanel from '@/components/console/systemSettings/PasskeyDomainsPanel.vue'
import CustomOAuthProvidersPanel from '@/components/console/systemSettings/CustomOAuthProvidersPanel.vue'
import SysSettingsFormCard from '@/components/console/systemSettings/SysSettingsFormCard.vue'
import SystemSettingsMaintenanceActions from '@/components/console/systemSettings/SystemSettingsMaintenanceActions.vue'
import SystemSettingsField from '@/components/console/systemSettings/SystemSettingsField.vue'
import GmpayPanel from '@/components/console/systemSettings/GmpayPanel.vue'
import UsageDiscountPanel from '@/components/console/systemSettings/UsageDiscountPanel.vue'
import WaffoPancakePanel from '@/components/console/systemSettings/WaffoPancakePanel.vue'
import PaymentCompliancePanel from '@/components/console/systemSettings/PaymentCompliancePanel.vue'
import SystemSettingsOperationsPanel from '@/components/console/systemSettings/SystemSettingsOperationsPanel.vue'
import {
  getSystemSettingsDomain,
  type SystemSettingsDomainId,
} from '@/constants/systemSettingsCatalog'
import {
  useSystemSettings,
  type SystemSettingValue,
} from '@/composables/useSystemSettings'
import { useToast } from '@/composables/useToast'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import RequestPolicyPanel from '@/components/console/systemSettings/RequestPolicyPanel.vue'
import { discoverOidcEndpoints } from '@/api/oidcDiscovery'
import { ApiError } from '@/api/types'
import { provideSettingsDraftGuard } from './settingsDraftGuard'

const props = defineProps<{ domain: SystemSettingsDomainId }>()

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const toast = useToast()
const {
  load,
  loaded,
  loading,
  saving: sharedSaving,
  rawOptions,
  rawValue,
  isSecretConfigured,
  saveOptions,
  statusRefreshing,
  statusRefreshError,
  refreshStatus,
} = useSystemSettings()

const domainConfig = computed(() => getSystemSettingsDomain(props.domain))
const activeSection = computed(() => {
  const domain = domainConfig.value
  if (!domain) return undefined
  const requested = String(route.params.section ?? '')
  return (
    domain.sections.find((section) => section.id === requested) ??
    domain.sections.find((section) => section.id === domain.defaultSection)
  )
})

const draft = reactive<Record<string, SystemSettingValue>>({})
const baseline = reactive<Record<string, SystemSettingValue>>({})
const draftValid = reactive<Record<string, boolean>>({})
const saving = reactive({ value: false })
const searchQuery = ref('')

const activeFields = computed(() => {
  const fields = activeSection.value?.fields ?? []
  if (activeSection.value?.id !== 'email') return fields
  const provider = String(draft.EmailProvider ?? 'smtp')
  return fields.filter((field) => {
    if (field.key === 'EmailProvider') return true
    if (provider === 'smtp') return !field.key.startsWith('Resend')
    if (provider === 'resend') return field.key.startsWith('Resend')
    return true
  })
})

const visibleFields = computed(() => {
  let fields = activeFields.value.filter(
    (field) =>
      field.key !== 'DisplayInCurrencyEnabled' || draft[field.key] === false
  )

  if (activeSection.value?.id === 'email') {
    fields.sort((left, right) => {
      if (left.key === 'EmailProvider') return -1
      if (right.key === 'EmailProvider') return 1
      return 0
    })
  }

  const query = searchQuery.value.trim().toLocaleLowerCase()
  if (!query) return fields
  return fields.filter((field) => {
    const text = [
      field.key,
      t(field.labelKey),
      field.descriptionKey ? t(field.descriptionKey) : '',
    ]
      .filter(Boolean)
      .join(' ')
      .toLocaleLowerCase()
    return text.includes(query)
  })
})

function readField(key: string, fallback: SystemSettingValue) {
  return rawValue(key, fallback)
}

function syncDraft() {
  const section = activeSection.value
  if (!section) return
  for (const key of Object.keys(draft)) delete draft[key]
  for (const key of Object.keys(draftValid)) delete draftValid[key]
  for (const key of Object.keys(baseline)) delete baseline[key]
  for (const field of section.fields) {
    draft[field.key] =
      field.kind === 'secret' || field.kind === 'secret-textarea'
        ? ''
        : readField(field.key, field.defaultValue)
    draftValid[field.key] = true
    baseline[field.key] = draft[field.key]
  }
}

function serialize(value: SystemSettingValue) {
  return String(value)
}

const dirty = computed(() => {
  const section = activeSection.value
  if (!section) return false
  return activeFields.value.some((field) => {
    const next = draft[field.key]
    if (
      (field.kind === 'secret' || field.kind === 'secret-textarea') &&
      !String(next ?? '').trim()
    )
      return false
    return serialize(next) !== serialize(baseline[field.key])
  })
})

provideSettingsDraftGuard(
  { dirty: () => dirty.value, busy: () => saving.value || sharedSaving.value },
  () => t('systemSettings.discardDraft')
)

function toSection(section: string) {
  router.push({
    name: `system-settings-${props.domain}`,
    params: { section },
  })
}

function normalizeRoute() {
  const domain = domainConfig.value
  if (!domain || activeSection.value?.id === route.params.section) return
  router.replace({
    name: `system-settings-${props.domain}`,
    params: { section: domain.defaultSection },
  })
}

async function saveSection() {
  const section = activeSection.value
  if (
    !section ||
    !dirty.value ||
    !loaded.value ||
    loading.value ||
    saving.value ||
    sharedSaving.value
  )
    return
  const fields = activeFields.value
  if (fields.some((field) => draftValid[field.key] === false)) {
    toast.error(t('systemSettings.invalidConfiguration'))
    return
  }
  const patch: Record<string, SystemSettingValue> = {}

  for (const field of fields) {
    const next = draft[field.key]
    if (
      (field.kind === 'secret' || field.kind === 'secret-textarea') &&
      !String(next ?? '').trim()
    )
      continue
    if (field.kind === 'json') {
      try {
        JSON.parse(String(next))
      } catch {
        toast.error(
          t('systemSettings.invalidJson', {
            field: t(field.labelKey),
          })
        )
        return
      }
    }
    if (serialize(next) !== serialize(baseline[field.key])) {
      patch[field.key] = next
    }
  }

  saving.value = true
  const savedDomain = props.domain
  const isCurrentSection = () =>
    props.domain === savedDomain && activeSection.value?.id === section.id
  try {
    const wellKnown = String(draft['oidc.well_known'] ?? '').trim()
    if (
      savedDomain === 'auth' &&
      section.id === 'oauth' &&
      patch['oidc.enabled'] !== false &&
      wellKnown &&
      (wellKnown !== String(baseline['oidc.well_known'] ?? '').trim() ||
        patch['oidc.enabled'] === true)
    ) {
      const endpoints = await discoverOidcEndpoints(wellKnown)
      if (!isCurrentSection()) return
      for (const [key, value] of Object.entries(endpoints)) {
        draft[key] = value
        if (value !== String(baseline[key] ?? '')) patch[key] = value
        else delete patch[key]
      }
    }
    const ok = await saveOptions(patch, {
      onApplied(key, value, sensitive) {
        if (!isCurrentSection()) return
        baseline[key] = sensitive ? '' : value
        if (sensitive) draft[key] = ''
      },
    })
    if (ok && isCurrentSection()) {
      syncDraft()
      toast.success(t('systemSettings.saved'))
    }
  } catch (error) {
    toast.error(
      error instanceof ApiError && error.code === 'INVALID_OIDC_DISCOVERY'
        ? t('systemSettings.oidcDiscoveryInvalid')
        : error instanceof Error
          ? error.message
          : String(error)
    )
  } finally {
    saving.value = false
  }
}

watch(
  () => [props.domain, route.params.section],
  () => {
    normalizeRoute()
    syncDraft()
  },
  { immediate: true }
)
watch(rawOptions, () => {
  if (!dirty.value) syncDraft()
})
watch(loaded, (ready) => {
  if (ready && !dirty.value) syncDraft()
})

onMounted(() => load())
</script>

<template>
  <div v-if="domainConfig && activeSection" class="settings-domain-layout">
    <!-- Left Section Navigation Rail -->
    <aside class="settings-section-aside">
      <nav class="settings-section-nav" :aria-label="t(domainConfig.titleKey)">
        <button
          v-for="section in domainConfig.sections"
          :key="section.id"
          type="button"
          :disabled="saving.value || sharedSaving"
          class="settings-section-link focus-ring"
          :class="section.id === activeSection.id ? 'is-active' : ''"
          :aria-current="section.id === activeSection.id ? 'page' : undefined"
          @click="toSection(section.id)"
        >
          <span
            v-if="section.id === activeSection.id"
            class="settings-active-bar"
            aria-hidden="true"
          />
          <span class="truncate">{{ t(section.titleKey) }}</span>
        </button>
      </nav>
    </aside>

    <div class="min-w-0 flex-1">
      <div
        v-if="statusRefreshError"
        role="alert"
        class="mb-4 text-sm text-[var(--status-warning-text)]"
      >
        <p>{{ statusRefreshError }}</p>
        <ConsoleButton
          class="mt-2"
          size="sm"
          variant="secondary"
          :loading="statusRefreshing"
          @click="refreshStatus()"
        >
          {{ t('systemSettings.retryStatusRefresh') }}
        </ConsoleButton>
      </div>
      <SysSettingsFormCard
        v-if="
          !['gmpay', 'usage-discount'].includes(activeSection.integration ?? '')
        "
        :title="t(activeSection.titleKey)"
        :description="t(activeSection.descriptionKey)"
        :saving="saving.value"
        :dirty="dirty && loaded && !loading && !sharedSaving"
        @save="saveSection"
      >
        <div class="settings-search" role="search">
          <label class="sr-only" :for="'settings-search-' + domain">
            {{ t('systemSettings.searchPlaceholder') }}
          </label>
          <input
            :id="'settings-search-' + domain"
            v-model="searchQuery"
            type="search"
            class="settings-search-input"
            :placeholder="t('systemSettings.searchPlaceholder')"
            autocomplete="off"
            @keydown.esc="searchQuery = ''"
          />
          <span
            v-if="searchQuery.trim()"
            class="settings-search-count"
            role="status"
          >
            {{ visibleFields.length }}
          </span>
        </div>
        <p
          v-if="loading"
          role="status"
          class="mb-4 text-sm text-[var(--text-secondary)]"
        >
          {{ t('systemSettings.loading') }}
        </p>
        <div
          v-else-if="!loaded"
          role="alert"
          class="mb-4 text-sm text-[var(--status-danger-text)]"
        >
          <p>{{ t('systemSettings.loadFailed') }}</p>
          <ConsoleButton
            class="mt-2"
            variant="secondary"
            size="sm"
            @click="load(true)"
            >{{ t('systemSettings.retryLoad') }}</ConsoleButton
          >
        </div>
        <fieldset
          :disabled="!loaded || loading || saving.value || sharedSaving"
          class="min-w-0 border-0 p-0"
        >
          <div class="settings-fields-grid">
            <SystemSettingsField
              v-for="field in visibleFields"
              :key="field.key"
              v-model="draft[field.key]"
              :field="field"
              :secret-configured="
                (field.kind === 'secret' || field.kind === 'secret-textarea') &&
                isSecretConfigured(field.key)
              "
              @validity="draftValid[field.key] = $event"
            />
          </div>
          <p
            v-if="searchQuery.trim() && visibleFields.length === 0"
            class="settings-search-empty"
            role="status"
          >
            {{ t('systemSettings.searchEmpty') }}
          </p>
        </fieldset>
        <PasskeyDomainsPanel
          v-if="domain === 'auth' && activeSection?.id === 'passkey'"
        />
        <CustomOAuthProvidersPanel
          v-if="activeSection.integration === 'custom-oauth'"
        />
        <WaffoPancakePanel
          v-else-if="activeSection.integration === 'waffo-pancake'"
          @saved="load(true)"
        />
        <PaymentCompliancePanel
          v-else-if="activeSection.integration === 'payment-compliance'"
          @confirmed="load(true)"
        />
        <SystemSettingsOperationsPanel
          v-else-if="activeSection.integration === 'operations-maintenance'"
        />
        <SystemSettingsMaintenanceActions
          v-else-if="activeSection.integration === 'channel-affinity'"
          kind="channel-affinity"
        />
        <SystemSettingsMaintenanceActions
          v-else-if="activeSection.integration === 'performance'"
          kind="performance"
        />
      </SysSettingsFormCard>
      <GmpayPanel v-if="activeSection.integration === 'gmpay'" />
      <UsageDiscountPanel
        v-if="activeSection.integration === 'usage-discount'"
      />
      <RequestPolicyPanel
        v-if="props.domain === 'security' && activeSection?.id === 'rate-limit'"
      />
    </div>
  </div>
</template>

<style scoped>
.settings-domain-layout {
  display: grid;
  grid-template-columns: 13.75rem minmax(0, 1fr);
  gap: 1.75rem;
  align-items: start;
}
.settings-section-aside {
  position: sticky;
  top: 1rem;
}
.settings-section-nav {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  padding: 0.5rem;
  border-radius: 1.25rem;
  border: 1px solid var(--border-subtle);
  background: var(--surface-solid);
  box-shadow: var(--card-shadow);
}
.settings-section-link {
  min-height: 2.75rem;
  border: 0;
  border-radius: 0.75rem;
  background: transparent;
  padding: 0.625rem 0.875rem;
  text-align: left;
  font-size: 0.875rem;
  font-weight: 500;
  color: var(--text-primary);
  display: flex;
  align-items: center;
  gap: 0.5rem;
  transition: all 0.15s ease;
}
.settings-section-link:hover {
  color: var(--text-primary);
  background: var(--surface-hover);
}
.settings-section-link.is-active {
  color: var(--accent-text);
  font-weight: 700;
  background: var(--accent-soft);
}
.settings-active-bar {
  width: 3.5px;
  height: 1.125rem;
  border-radius: 9999px;
  background: var(--accent);
  flex-shrink: 0;
}
.settings-fields-grid {
  display: grid;
  gap: 1.25rem 1.5rem;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
.settings-fields-grid > :deep(.settings-field-toggle) {
  grid-column: 1 / -1;
}
/* A full-row toggle keeps its label, switch and hint within the first column,
   so the switch lines up with the inputs instead of the far card edge. */
.settings-fields-grid > :deep(.settings-field-toggle) > * {
  max-width: calc((100% - 1.5rem) / 2);
}
.settings-fields-grid > :deep(.settings-field-control) {
  min-width: 0;
}
.settings-fields-grid > :deep(.settings-json-editor) {
  grid-column: 1 / -1;
}
.settings-fields-grid > :deep(.settings-field-wide) {
  grid-column: 1 / -1;
}
.settings-search {
  position: relative;
  margin-bottom: 1.25rem;
}
.settings-search-input {
  width: 100%;
  min-height: 2.75rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--shape-control);
  background: var(--surface-container-low);
  color: var(--text-primary);
  padding: 0.625rem 3rem 0.625rem 0.875rem;
  font-size: 0.8125rem;
  outline: none;
  transition:
    border-color 0.15s ease,
    box-shadow 0.15s ease,
    background-color 0.15s ease;
}
.settings-search-input::placeholder {
  color: var(--text-tertiary);
}
.settings-search-input:focus {
  border-color: var(--accent);
  background: var(--surface-container);
  box-shadow: var(--focus-halo);
}
.settings-search-count {
  position: absolute;
  top: 50%;
  right: 0.75rem;
  min-width: 1.5rem;
  padding: 0.125rem 0.375rem;
  border-radius: 9999px;
  background: var(--accent-soft);
  color: var(--accent-text);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.6875rem;
  text-align: center;
  transform: translateY(-50%);
}
.settings-search-empty {
  padding: 1rem;
  border: 1px dashed var(--border-default);
  border-radius: var(--shape-control);
  color: var(--text-secondary);
  font-size: 0.8125rem;
  text-align: center;
}
@media (max-width: 860px) {
  .settings-domain-layout {
    display: block;
  }
  .settings-section-aside {
    position: static;
    margin-bottom: 1.25rem;
  }
  .settings-section-nav {
    flex-direction: row;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .settings-section-nav::-webkit-scrollbar {
    display: none;
  }
  .settings-section-link {
    flex: 0 0 auto;
    white-space: nowrap;
  }
  .settings-fields-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .settings-fields-grid > :deep(.settings-field-toggle) > * {
    max-width: none;
  }
}
</style>
