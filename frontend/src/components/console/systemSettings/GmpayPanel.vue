<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/console'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import FormField from '@/components/common/FormField.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useSystemSettings } from '@/composables/useSystemSettings'
import { useToast } from '@/composables/useToast'

/**
 * One asset the gateway reports as collectable. The administrator picks the
 * concrete token and network requested when creating an order.
 */
interface SupportedAsset {
  network: string
  display_name: string
  tokens: string[]
}

interface SelectedAsset {
  name: string
  token: string
  network: string
  minTopUp: string
  icon?: string
  color?: string
}

const emit = defineEmits<{ saved: [] }>()

const { t } = useI18n()
const toast = useToast()
const { load, rawValue, isSecretConfigured } = useSystemSettings()
const probing = ref(false)
const saving = ref(false)
const configurationReady = ref(false)
const configurationLoading = ref(true)
const configurationFailed = ref(false)
const supported = ref<SupportedAsset[] | null>(null)
const selected = ref<SelectedAsset[]>([])
let probeController: AbortController | null = null
let disposed = false
const form = reactive({
  payAddress: '',
  merchantId: '',
  secretKey: '',
  price: 1,
  minTopUp: 1,
  groupRatio: '{"default":1}',
})

const secretConfigured = computed(() => isSecretConfigured('GmpayKey'))
const selectedCount = computed(() => selected.value.length)

function parseStoredAssets(raw: string): SelectedAsset[] {
  const parsed: unknown = JSON.parse(raw)
  // The backend also accepts JSON null as an empty catalog.
  if (parsed === null) return []
  if (!Array.isArray(parsed)) throw new Error('Invalid stored asset catalog')
  return parsed.map((record) => {
    if (
      !record ||
      typeof record !== 'object' ||
      typeof record.name !== 'string' ||
      !record.name.trim() ||
      typeof record.token !== 'string' ||
      !record.token.trim() ||
      typeof record.network !== 'string' ||
      !record.network.trim() ||
      (record.min_topup !== undefined && typeof record.min_topup !== 'string')
    ) {
      throw new Error('Invalid stored asset catalog')
    }
    return {
      name: record.name.trim(),
      token: record.token.trim().toLowerCase(),
      network: record.network.trim().toLowerCase(),
      minTopUp: (record.min_topup ?? '').trim(),
      ...(typeof record.icon === 'string' ? { icon: record.icon } : {}),
      ...(typeof record.color === 'string' ? { color: record.color } : {}),
    }
  })
}

function syncForm() {
  form.payAddress = String(rawValue('GmpayPayAddress', ''))
  form.merchantId = String(rawValue('GmpayId', ''))
  form.price = Number(rawValue('GmpayPrice', 1))
  form.minTopUp = Number(rawValue('GmpayMinTopUp', 1))
  form.groupRatio = String(rawValue('GmpayGroupRatio', '{"default":1}'))
  selected.value = parseStoredAssets(String(rawValue('GmpayAssets', '[]')))
}

function isSelected(network: string, token: string): boolean {
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
  selected.value = [
    ...selected.value,
    {
      name: `${token.toUpperCase()} ${asset.display_name || asset.network}`,
      token: token.toLowerCase(),
      network: asset.network.toLowerCase(),
      minTopUp: '',
    },
  ]
}

function setAssetName(token: string, network: string, name: string) {
  selected.value = selected.value.map((item) =>
    item.token === token && item.network === network ? { ...item, name } : item
  )
}

function setAssetMinimum(token: string, network: string, value: string) {
  selected.value = selected.value.map((item) =>
    item.token === token && item.network === network
      ? { ...item, minTopUp: value }
      : item
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

function assetPayload(): string {
  return JSON.stringify(
    selected.value.map((item) => ({
      name: item.name.trim() || item.token.toUpperCase(),
      token: item.token,
      network: item.network,
      ...(item.icon === undefined ? {} : { icon: item.icon }),
      ...(item.color === undefined ? {} : { color: item.color }),
      ...(item.minTopUp.trim() === ''
        ? {}
        : { min_topup: String(Number(item.minTopUp)) }),
    }))
  )
}

/** The gateway secret is verified by an order, so the probe never needs it. */
async function probe() {
  if (!configurationReady.value || saving.value) return
  if (!form.payAddress.trim()) {
    toast.error(t('systemSettings.gmpayPanel.addressRequired'))
    return
  }
  probeController?.abort()
  const controller = new AbortController()
  probeController = controller
  supported.value = null
  probing.value = true
  try {
    const result = await api.post<{ supported_assets: SupportedAsset[] }>(
      '/api/option/gmpay/probe',
      {
        pay_address: form.payAddress.trim(),
      },
      { signal: controller.signal }
    )
    if (controller.signal.aborted) return
    if (
      !Array.isArray(result.supported_assets) ||
      result.supported_assets.some(
        (asset) =>
          !asset ||
          typeof asset.network !== 'string' ||
          !asset.network ||
          typeof asset.display_name !== 'string' ||
          !Array.isArray(asset.tokens) ||
          asset.tokens.some((token) => typeof token !== 'string' || !token)
      )
    ) {
      throw new Error(t('systemSettings.gmpayPanel.invalidCatalog'))
    }
    supported.value = result.supported_assets.map((asset) => ({
      ...asset,
      network: asset.network.toLowerCase(),
      tokens: asset.tokens.map((token) => token.toLowerCase()),
    }))
    if (!supported.value.length) {
      toast.error(t('systemSettings.gmpayPanel.noAssets'))
      return
    }
    toast.success(t('systemSettings.gmpayPanel.probed'))
  } catch (error) {
    if (!controller.signal.aborted)
      toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    if (probeController === controller) probing.value = false
  }
}

async function save() {
  if (saving.value || !configurationReady.value) return
  if (!form.payAddress.trim() || !form.merchantId.trim()) {
    toast.error(t('systemSettings.gmpayPanel.credentialsRequired'))
    return
  }
  if (!form.secretKey.trim() && !secretConfigured.value) {
    toast.error(t('systemSettings.gmpayPanel.secretRequired'))
    return
  }
  try {
    const ratios: unknown = JSON.parse(form.groupRatio)
    if (
      ratios === null ||
      typeof ratios !== 'object' ||
      Array.isArray(ratios) ||
      Object.values(ratios).some(
        (ratio) =>
          typeof ratio !== 'number' || !Number.isFinite(ratio) || ratio <= 0
      )
    ) {
      throw new Error('Invalid group ratios')
    }
  } catch {
    toast.error(t('systemSettings.gmpayPanel.invalidGroupRatios'))
    return
  }
  if (
    !Number.isFinite(form.price) ||
    form.price <= 0 ||
    !Number.isSafeInteger(form.minTopUp) ||
    form.minTopUp < 1 ||
    form.minTopUp > 18446744073709 ||
    selected.value.some(
      (item) =>
        item.minTopUp.trim() !== '' &&
        (!Number.isSafeInteger(Number(item.minTopUp)) ||
          Number(item.minTopUp) < 1 ||
          Number(item.minTopUp) > 18446744073709)
    )
  ) {
    toast.error(t('systemSettings.gmpayPanel.invalidAmounts'))
    return
  }
  saving.value = true
  try {
    await api.post('/api/option/gmpay/save', {
      pay_address: form.payAddress,
      merchant_id: form.merchantId,
      secret_key: form.secretKey,
      assets: assetPayload(),
      price: form.price,
      min_topup: form.minTopUp,
      group_ratio: form.groupRatio,
    })
    if (disposed) {
      // The write still completed. Refresh the shared cache for the next visit.
      await load(true)
      return
    }
    form.secretKey = ''
    emit('saved')
    toast.success(t('systemSettings.gmpayPanel.saved'))
    await loadConfiguration(true)
  } catch (error) {
    if (!disposed)
      toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    saving.value = false
  }
}

async function loadConfiguration(force = false) {
  configurationReady.value = false
  configurationLoading.value = true
  configurationFailed.value = false
  try {
    const loaded = await load(force)
    if (disposed) return
    if (!loaded) {
      configurationFailed.value = true
      return
    }
    syncForm()
    configurationReady.value = true
  } catch {
    if (!disposed) configurationFailed.value = true
  } finally {
    if (!disposed) configurationLoading.value = false
  }
}

onMounted(() => void loadConfiguration())

watch(
  () => form.payAddress,
  () => {
    probeController?.abort()
    probeController = null
    probing.value = false
    supported.value = null
  }
)

onUnmounted(() => {
  disposed = true
  probeController?.abort()
})
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
      <p>{{ t('systemSettings.gmpayPanel.loadFailed') }}</p>
      <ConsoleButton
        class="mt-2"
        variant="secondary"
        size="sm"
        @click="loadConfiguration(true)"
      >
        {{ t('systemSettings.gmpayPanel.retryLoad') }}
      </ConsoleButton>
    </div>
    <fieldset
      :disabled="saving || !configurationReady"
      class="min-w-0 border-0 p-0"
    >
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
        <FormField :label="t('systemSettings.gmpayPanel.price')">
          <TextInput
            :model-value="String(form.price)"
            type="number"
            min="0.0001"
            step="0.01"
            autocomplete="off"
            @update:model-value="form.price = Number($event)"
          />
        </FormField>
        <FormField :label="t('systemSettings.gmpayPanel.minimum')">
          <TextInput
            :model-value="String(form.minTopUp)"
            type="number"
            min="1"
            step="1"
            autocomplete="off"
            @update:model-value="form.minTopUp = Number($event)"
          />
        </FormField>
        <FormField
          :label="t('systemSettings.gmpayPanel.groupRatio')"
          :hint="t('systemSettings.gmpayPanel.groupRatioHint')"
          class="sm:col-span-2"
        >
          <TextInput
            v-model="form.groupRatio"
            class="font-mono"
            autocomplete="off"
          />
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
        {{ t('systemSettings.gmpayPanel.noSelectedAssets') }}
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
              t('systemSettings.gmpayPanel.assetMinimum', {
                asset: `${item.token.toUpperCase()} · ${item.network}`,
              })
            "
            class="mt-2"
          >
            <TextInput
              :model-value="item.minTopUp"
              type="number"
              min="1"
              step="1"
              autocomplete="off"
              :placeholder="
                t('systemSettings.gmpayPanel.assetMinimumPlaceholder')
              "
              @update:model-value="
                setAssetMinimum(item.token, item.network, String($event))
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
