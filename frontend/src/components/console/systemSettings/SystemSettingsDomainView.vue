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
import GatewayConfigPanel from '@/components/console/gateway/GatewayConfigPanel.vue'

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

const visibleFields = computed(() => {
  const fields = activeSection.value?.fields ?? []
  const query = searchQuery.value.trim().toLocaleLowerCase()
  if (!query) return fields
  return fields.filter((field) => {
    const text = [
      field.key,
      field.label,
      field.description,
      field.labelKey ? t(field.labelKey) : '',
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
  return section.fields.some((field) => {
    const next = draft[field.key]
    if (
      (field.kind === 'secret' || field.kind === 'secret-textarea') &&
      !String(next ?? '').trim()
    )
      return false
    return serialize(next) !== serialize(baseline[field.key])
  })
})

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
  if (section.fields.some((field) => draftValid[field.key] === false)) {
    toast.error(t('systemSettings.invalidConfiguration'))
    return
  }
  const patch: Record<string, SystemSettingValue> = {}

  for (const field of section.fields) {
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
            field: field.labelKey ? t(field.labelKey) : field.label,
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
  const ok = await saveOptions(patch)
  saving.value = false
  if (ok) {
    syncDraft()
    toast.success(t('systemSettings.saved'))
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
      <nav class="settings-section-nav" :aria-label="domainConfig.titleKey">
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
          <span class="truncate">{{
            section.titleKey ? t(section.titleKey) : section.title
          }}</span>
        </button>
      </nav>
    </aside>

    <div class="min-w-0 flex-1">
      <SysSettingsFormCard
        :title="
          activeSection.titleKey
            ? t(activeSection.titleKey)
            : activeSection.title
        "
        :description="
          activeSection.descriptionKey
            ? t(activeSection.descriptionKey)
            : activeSection.description
        "
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
        <GmpayPanel v-else-if="activeSection.integration === 'gmpay'" />
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
      <GatewayConfigPanel
        v-if="domain === 'security' && activeSection.id === 'rate-limit'"
        class="mt-6 border-t border-[var(--border-subtle)] pt-6"
      />
    </div>
  </div>
</template>

<style scoped>
.settings-domain-layout {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
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
}
</style>
