<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/client'
import { isRecord, invalidResponse } from '@/api/contracts'
import { useLatestRequest } from '@/composables/useLatestRequest'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
interface Asset {
  name: string
  token: string
  network: string
  icon?: string
  color?: string
}
interface Config {
  pay_address: string
  merchant_id: string
  has_secret: boolean
  assets: Asset[]
  enabled: boolean
}
const endpoint = '/api/option/gmpay/config'
function parseAssets(value: unknown): Asset[] {
  if (
    !Array.isArray(value) ||
    value.some(
      (a) =>
        !isRecord(a) ||
        typeof a.name !== 'string' ||
        !a.name.trim() ||
        typeof a.token !== 'string' ||
        typeof a.network !== 'string' ||
        !/^[a-z0-9_-]+$/.test(a.token) ||
        !/^[a-z0-9_-]+$/.test(a.network) ||
        ['icon', 'color'].some(
          (k) => a[k] !== undefined && typeof a[k] !== 'string'
        )
    )
  )
    invalidResponse(endpoint)
  return value as Asset[]
}
function parseConfig(data: unknown): Config {
  if (
    !isRecord(data) ||
    typeof data.pay_address !== 'string' ||
    typeof data.merchant_id !== 'string' ||
    typeof data.has_secret !== 'boolean' ||
    typeof data.enabled !== 'boolean'
  )
    invalidResponse(endpoint)
  return {
    pay_address: data.pay_address,
    merchant_id: data.merchant_id,
    has_secret: data.has_secret,
    enabled: data.enabled,
    assets: parseAssets(data.assets),
  }
}
const emit = defineEmits<{ saved: [] }>(),
  { t } = useI18n()
const form = reactive({
    pay_address: '',
    merchant_id: '',
    secret_key: '',
    enabled: false,
  }),
  assets = ref('[]'),
  hasSecret = ref(false),
  supported = ref<unknown>(null),
  loading = ref(false),
  ready = ref(false),
  error = ref('')
const request = useLatestRequest(),
  probeRequest = useLatestRequest(),
  probing = ref(false)
const failure = (e: unknown) => (e instanceof Error ? e.message : String(e))
async function load() {
  if (loading.value) return
  loading.value = true
  ready.value = false
  error.value = ''
  const result = await request.run(async (signal) =>
    parseConfig(await api.get<unknown>(endpoint, undefined, { signal }))
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    error.value = failure(result.error)
    return
  }
  const data = result.value
  Object.assign(form, {
    pay_address: data.pay_address,
    merchant_id: data.merchant_id,
    enabled: data.enabled,
    secret_key: '',
  })
  assets.value = JSON.stringify(data.assets, null, 2)
  hasSecret.value = data.has_secret
  ready.value = true
}
async function probe() {
  if (probing.value || loading.value || !ready.value) return
  probing.value = true
  error.value = ''
  const result = await probeRequest.run(
    async (signal) =>
      (
        await api.post<{ supported_assets: unknown }>(
          '/api/option/gmpay/probe',
          { pay_address: form.pay_address },
          { signal }
        )
      ).supported_assets
  )
  if (result.stale) return
  probing.value = false
  if (result.ok) supported.value = result.value
  else error.value = failure(result.error)
}
watch(
  () => form.pay_address,
  () => {
    probeRequest.cancel()
    probing.value = false
    supported.value = null
  }
)
async function save() {
  if (loading.value || !ready.value) return
  error.value = ''
  let parsed: Asset[]
  try {
    parsed = parseAssets(JSON.parse(assets.value))
  } catch (e) {
    error.value = failure(e)
    return
  }
  loading.value = true
  const result = await request.run((signal) =>
    api.post('/api/option/gmpay/save', { ...form, assets: parsed }, { signal })
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    error.value = failure(result.error)
    return
  }
  emit('saved')
  await load()
}
onMounted(load)
</script>
<template>
  <ConsoleCard title="GMPay"
    ><ErrorBanner v-if="error" :message="error" @retry="load" />
    <fieldset :disabled="loading || !ready" class="space-y-4">
      <label class="block"
        ><input
          v-model="form.enabled"
          type="checkbox"
          aria-describedby="gmpay-new-orders-hint"
        />
        {{ t('systemSettings.gmpayPanel.enableNewOrders') }}</label
      >
      <p
        id="gmpay-new-orders-hint"
        class="text-sm text-[var(--text-secondary)]"
      >
        {{ t('systemSettings.gmpayPanel.enableNewOrdersHint') }}
      </p>
      <label class="block"
        >{{ t('systemSettings.gmpayPanel.address')
        }}<input
          v-model="form.pay_address"
          type="url"
          class="console-input mt-2 w-full" /></label
      ><label class="block"
        >{{ t('systemSettings.gmpayPanel.merchantId')
        }}<input
          v-model="form.merchant_id"
          class="console-input mt-2 w-full" /></label
      ><label class="block"
        >{{ t('systemSettings.gmpayPanel.secret')
        }}<input
          v-model="form.secret_key"
          type="password"
          autocomplete="new-password"
          class="console-input mt-2 w-full"
          :placeholder="hasSecret ? '••••••••' : ''" /></label
      ><ConsoleButton variant="secondary" :disabled="probing" @click="probe">{{
        t('systemSettings.gmpayPanel.probe')
      }}</ConsoleButton>
      <pre v-if="supported" class="overflow-auto text-xs">{{
        JSON.stringify(supported, null, 2)
      }}</pre>
      <label class="block"
        >{{ t('systemSettings.gmpayPanel.selectedAssets')
        }}<textarea
          v-model="assets"
          rows="8"
          class="console-input mt-2 w-full font-mono"
        /></label
      ><ConsoleButton @click="save">{{
        t('systemSettings.gmpayPanel.save')
      }}</ConsoleButton>
    </fieldset></ConsoleCard
  >
</template>
