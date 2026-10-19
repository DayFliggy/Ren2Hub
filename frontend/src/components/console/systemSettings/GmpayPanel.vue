<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/client'
import { invalidResponse, isRecord } from '@/api/contracts'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import FormField from '@/components/common/FormField.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import { paymentRestorationMessages } from '@/i18n/paymentRestoration'

interface SupportedAsset {
  network: string
  display_name: string
  tokens: string[]
}
interface SelectedAsset {
  name: string
  token: string
  network: string
  icon?: string
  color?: string
}
const endpoint = '/api/option/gmpay/config'
const identifier = /^[a-z0-9_-]+$/
function parseAssets(value: unknown): SelectedAsset[] {
  if (
    !Array.isArray(value) ||
    value.some(
      (a) =>
        !isRecord(a) ||
        typeof a.name !== 'string' ||
        !a.name.trim() ||
        typeof a.token !== 'string' ||
        !identifier.test(a.token) ||
        typeof a.network !== 'string' ||
        !identifier.test(a.network) ||
        ['icon', 'color'].some(
          (k) => a[k] !== undefined && typeof a[k] !== 'string'
        )
    )
  )
    invalidResponse(endpoint)
  return value as SelectedAsset[]
}
function parseConfig(value: unknown) {
  if (
    !isRecord(value) ||
    typeof value.pay_address !== 'string' ||
    typeof value.merchant_id !== 'string' ||
    typeof value.has_secret !== 'boolean' ||
    typeof value.enabled !== 'boolean'
  )
    invalidResponse(endpoint)
  return {
    pay_address: value.pay_address,
    merchant_id: value.merchant_id,
    has_secret: value.has_secret,
    enabled: value.enabled,
    assets: parseAssets(value.assets),
  }
}
const emit = defineEmits<{ saved: [] }>()
const { t } = useI18n({ messages: paymentRestorationMessages })
const toast = useToast()
const request = useLatestRequest()
const probeRequest = useLatestRequest()
const probing = ref(false)
const saving = ref(false)
const configurationReady = ref(false)
const configurationLoading = ref(true)
const configurationFailed = ref(false)
const configurationError = ref('')
const actionError = ref('')
const supported = ref<SupportedAsset[] | null>(null)
const selected = ref<SelectedAsset[]>([])
const secretConfigured = ref(false)
const form = reactive({
  payAddress: '',
  merchantId: '',
  secretKey: '',
  enabled: false,
})
const selectedCount = computed(() => selected.value.length)
const failure = (error: unknown) =>
  error instanceof Error ? error.message : String(error)
function isSelected(network: string, token: string) {
  return selected.value.some(
    (item) => item.network === network && item.token === token
  )
}
function removeAsset(network: string, token: string) {
  selected.value = selected.value.filter(
    (item) => !(item.network === network && item.token === token)
  )
}
function toggleAsset(asset: SupportedAsset, token: string) {
  if (isSelected(asset.network, token)) {
    removeAsset(asset.network, token)
    return
  }
  selected.value.push({
    name: `${token.toUpperCase()} ${asset.display_name || asset.network}`,
    token,
    network: asset.network,
  })
}
function setAssetName(token: string, network: string, name: string) {
  selected.value = selected.value.map((item) =>
    item.token === token && item.network === network ? { ...item, name } : item
  )
}
function setAssetMetadata(
  token: string,
  network: string,
  key: 'icon' | 'color',
  value: string
) {
  selected.value = selected.value.map((item) =>
    item.token === token && item.network === network
      ? { ...item, [key]: value }
      : item
  )
}
async function loadConfiguration() {
  configurationReady.value = false
  configurationLoading.value = true
  configurationFailed.value = false
  configurationError.value = ''
  const result = await request.run(async (signal) =>
    parseConfig(await api.get<unknown>(endpoint, undefined, { signal }))
  )
  if (result.stale) return
  configurationLoading.value = false
  if (!result.ok) {
    configurationFailed.value = true
    configurationError.value = failure(result.error)
    return
  }
  const data = result.value
  Object.assign(form, {
    payAddress: data.pay_address,
    merchantId: data.merchant_id,
    secretKey: '',
    enabled: data.enabled,
  })
  secretConfigured.value = data.has_secret
  selected.value = data.assets.map((item) => ({ ...item }))
  configurationReady.value = true
}
async function probe() {
  if (!configurationReady.value || saving.value || probing.value) return
  if (!form.payAddress.trim()) {
    actionError.value = t('systemSettings.gmpayPanel.addressRequired')
    return
  }
  probing.value = true
  actionError.value = ''
  supported.value = null
  const result = await probeRequest.run(async (signal) => {
    const value = await api.post<unknown>(
      '/api/option/gmpay/probe',
      { pay_address: form.payAddress.trim() },
      { signal }
    )
    if (!isRecord(value) || !Array.isArray(value.supported_assets))
      invalidResponse('/api/option/gmpay/probe')
    return value.supported_assets.map((asset) => {
      if (
        !isRecord(asset) ||
        typeof asset.network !== 'string' ||
        !identifier.test(asset.network.toLowerCase()) ||
        typeof asset.display_name !== 'string' ||
        !Array.isArray(asset.tokens) ||
        asset.tokens.some(
          (token) =>
            typeof token !== 'string' || !identifier.test(token.toLowerCase())
        )
      )
        invalidResponse('/api/option/gmpay/probe')
      return {
        network: asset.network.toLowerCase(),
        display_name: asset.display_name,
        tokens: asset.tokens.map((token: string) => token.toLowerCase()),
      }
    })
  })
  if (result.stale) return
  probing.value = false
  if (!result.ok) {
    actionError.value = failure(result.error)
    return
  }
  supported.value = result.value
  if (result.value.length) toast.success(t('systemSettings.gmpayPanel.probed'))
}
async function save() {
  if (saving.value || !configurationReady.value) return
  actionError.value = ''
  let assets: SelectedAsset[]
  try {
    assets = parseAssets(
      selected.value.map((item) => ({ ...item, name: item.name.trim() }))
    )
  } catch (error) {
    actionError.value = failure(error)
    return
  }
  saving.value = true
  const result = await request.run((signal) =>
    api.post(
      '/api/option/gmpay/save',
      {
        pay_address: form.payAddress,
        merchant_id: form.merchantId,
        secret_key: form.secretKey,
        enabled: form.enabled,
        assets,
      },
      { signal }
    )
  )
  if (result.stale) return
  saving.value = false
  if (!result.ok) {
    actionError.value = failure(result.error)
    return
  }
  form.secretKey = ''
  emit('saved')
  toast.success(t('systemSettings.gmpayPanel.saved'))
  await loadConfiguration()
}
watch(
  () => form.payAddress,
  () => {
    probeRequest.cancel()
    probing.value = false
    supported.value = null
  }
)
onMounted(loadConfiguration)
</script>

<template>
  <section
    class="gmpay-panel"
    :aria-label="t('systemSettings.gmpayPanel.title')"
  >
    <p v-if="configurationLoading" class="gmpay-hint" role="status">
      {{ t('systemSettings.gmpayPanel.loading') }}
    </p>
    <div v-if="configurationFailed" class="gmpay-hint" role="alert">
      <p>{{ configurationError }}</p>
      <ConsoleButton
        class="mt-2"
        variant="secondary"
        size="sm"
        @click="loadConfiguration"
      >
        {{ t('systemSettings.gmpayPanel.retryLoad') }}
      </ConsoleButton>
    </div>
    <p
      v-if="actionError"
      role="alert"
      class="mt-3 text-sm text-[var(--status-danger-text)]"
    >
      {{ actionError }}
    </p>
    <fieldset
      :disabled="saving || !configurationReady"
      class="min-w-0 border-0 p-0"
    >
      <label class="mb-4 flex items-center gap-3 text-sm font-medium">
        <input
          v-model="form.enabled"
          type="checkbox"
          class="size-4 accent-[var(--accent)]"
          aria-describedby="gmpay-new-orders-hint"
        />
        {{ t('systemSettings.gmpayPanel.enableNewOrders') }}
      </label>
      <p
        id="gmpay-new-orders-hint"
        class="mb-5 text-xs leading-5 text-[var(--text-tertiary)]"
      >
        {{ t('systemSettings.gmpayPanel.enableNewOrdersHint') }}
      </p>
      <div class="grid gap-4 sm:grid-cols-2">
        <FormField
          :label="t('systemSettings.gmpayPanel.address')"
          class="sm:col-span-2"
        >
          <TextInput v-model="form.payAddress" type="url" autocomplete="off" />
        </FormField>
        <FormField :label="t('systemSettings.gmpayPanel.merchantId')">
          <TextInput v-model="form.merchantId" autocomplete="off" />
        </FormField>
        <FormField :label="t('systemSettings.gmpayPanel.secret')">
          <TextInput
            v-model="form.secretKey"
            type="password"
            autocomplete="new-password"
          />
          <div v-if="secretConfigured" class="mt-1 flex items-center gap-1.5">
            <StatusChip tone="success" class="text-xs">{{
              t('systemSettings.gmpayPanel.configured')
            }}</StatusChip>
            <span class="text-xs text-[var(--text-tertiary)]">{{
              t('systemSettings.gmpayPanel.keepSecret')
            }}</span>
          </div>
        </FormField>
      </div>

      <p class="gmpay-hint">
        {{ t('systemSettings.gmpayPanel.probeHint') }}
      </p>
      <p class="gmpay-hint">
        {{ t('systemSettings.gmpayPanel.callbackHint') }}
      </p>

      <div class="gmpay-actions">
        <ConsoleButton
          variant="secondary"
          size="sm"
          :loading="probing"
          @click="probe"
        >
          {{ t('systemSettings.gmpayPanel.probe') }}
        </ConsoleButton>
        <ConsoleButton size="sm" :loading="saving" @click="save">
          {{ t('systemSettings.gmpayPanel.save') }}
        </ConsoleButton>
      </div>

      <div v-if="supported" class="mt-5 grid gap-4">
        <fieldset
          v-for="asset in supported"
          :key="asset.network"
          class="min-w-0 border-0 p-0"
        >
          <legend
            class="mb-1.5 text-sm font-medium text-[var(--text-secondary)]"
          >
            {{ asset.display_name || asset.network }}
          </legend>
          <div class="flex flex-wrap gap-2">
            <button
              v-for="token in asset.tokens"
              :key="`${asset.network}-${token}`"
              type="button"
              class="gmpay-asset focus-ring"
              :class="{
                'gmpay-asset-active': isSelected(asset.network, token),
              }"
              :aria-pressed="isSelected(asset.network, token)"
              @click="toggleAsset(asset, token)"
            >
              {{ token.toUpperCase() }} · {{ asset.network }}
            </button>
          </div>
        </fieldset>
      </div>

      <p v-if="supported && !supported.length" class="gmpay-hint" role="status">
        {{ t('systemSettings.gmpayPanel.noAssets') }}
      </p>
      <p v-if="!selectedCount" class="gmpay-hint" role="status">
        {{ t('paymentRestoration.noSelectedAssets') }}
      </p>
      <div v-if="selectedCount" class="mt-5 grid gap-4 sm:grid-cols-2">
        <div v-for="item in selected" :key="`${item.token}.${item.network}`">
          <FormField
            :label="
              t('systemSettings.gmpayPanel.assetName', {
                asset: `${item.token.toUpperCase()} · ${item.network}`,
              })
            "
          >
            <TextInput
              :model-value="item.name"
              autocomplete="off"
              @update:model-value="
                setAssetName(item.token, item.network, String($event))
              "
            />
          </FormField>

          <FormField
            :label="
              t('systemSettings.gmpayPanel.assetIcon', {
                asset: `${item.token.toUpperCase()} · ${item.network}`,
              })
            "
            class="mt-2"
          >
            <TextInput
              :model-value="item.icon ?? ''"
              autocomplete="off"
              @update:model-value="
                setAssetMetadata(
                  item.token,
                  item.network,
                  'icon',
                  String($event)
                )
              "
            />
          </FormField>
          <FormField
            :label="
              t('systemSettings.gmpayPanel.assetColor', {
                asset: `${item.token.toUpperCase()} · ${item.network}`,
              })
            "
            :hint="t('systemSettings.gmpayPanel.metadataHint')"
            class="mt-2"
          >
            <TextInput
              :model-value="item.color ?? ''"
              autocomplete="off"
              @update:model-value="
                setAssetMetadata(
                  item.token,
                  item.network,
                  'color',
                  String($event)
                )
              "
            />
          </FormField>
          <ConsoleButton
            class="mt-2"
            variant="ghost"
            size="sm"
            :aria-label="
              t('systemSettings.gmpayPanel.removeAsset', {
                asset: `${item.token.toUpperCase()} · ${item.network}`,
              })
            "
            @click="removeAsset(item.network, item.token)"
          >
            {{ t('systemSettings.gmpayPanel.remove') }}
          </ConsoleButton>
        </div>
      </div>
    </fieldset>
  </section>
</template>

<style scoped>
.gmpay-panel {
  margin-top: 1.5rem;
  border-top: 1px dashed var(--border-default);
  padding-top: 1rem;
}
.gmpay-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  margin-top: 1rem;
}
.gmpay-asset {
  height: 2.25rem;
  border-radius: var(--sketch-border-radius-sm);
  background: var(--surface-muted);
  padding: 0 0.75rem;
  color: var(--text-secondary);
  font-size: 0.75rem;
  font-weight: 500;
}
.gmpay-asset-active {
  background: var(--accent);
  color: var(--accent-contrast);
}
.gmpay-hint {
  margin-top: 1rem;
  font-size: 0.75rem;
  color: var(--text-tertiary);
}
</style>
