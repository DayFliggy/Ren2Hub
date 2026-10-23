<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { parseWaffoPancakePairFailure, paymentRequest } from '@/api/topup'
import { ApiError } from '@/api/types'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useSystemSettings } from '@/composables/useSystemSettings'
import { useToast } from '@/composables/useToast'
import { registerSettingsDraft } from './settingsDraftGuard'

interface CatalogProduct {
  id: string
  name: string
  status: string
}

interface CatalogStore {
  id: string
  name: string
  status: string
  prodEnabled: boolean
  onetimeProducts: CatalogProduct[]
}

interface Catalog {
  stores: CatalogStore[]
}

const emit = defineEmits<{ saved: [] }>()

const { t } = useI18n()
const toast = useToast()
const { load, rawValue, saveOptions, refreshStatus } = useSystemSettings()
const loadingCatalog = ref(false)
const saving = ref(false)
const creating = ref(false)
const catalog = ref<Catalog | null>(null)
const form = reactive({
  merchantId: '',
  privateKey: '',
  returnUrl: '',
  storeId: '',
  productId: '',
  unitPrice: 1,
  minTopUp: 1,
})

const baseline = ref('')
const snapshot = () => JSON.stringify(form)
registerSettingsDraft({ dirty: () => baseline.value !== '' && snapshot() !== baseline.value, busy: () => saving.value || creating.value || loadingCatalog.value })

const selectedStore = computed(() =>
  catalog.value?.stores.find((store) => store.id === form.storeId)
)
function syncForm() {
  form.merchantId = String(rawValue('WaffoPancakeMerchantID', ''))
  form.returnUrl = String(rawValue('WaffoPancakeReturnURL', ''))
  form.storeId = String(rawValue('WaffoPancakeStoreID', ''))
  form.productId = String(rawValue('WaffoPancakeProductID', ''))
  form.unitPrice = Number(rawValue('WaffoPancakeUnitPrice', 1)) || 1
  form.minTopUp = Number(rawValue('WaffoPancakeMinTopUp', 1)) || 1
  baseline.value = snapshot()
}

function ensureProductBelongsToStore() {
  if (
    !selectedStore.value?.onetimeProducts.some(
      (item) => item.id === form.productId
    )
  ) {
    form.productId = ''
  }
}

async function saveConfiguration(loadCatalogAfterSave = false) {
  if (!form.merchantId.trim() || !form.returnUrl.trim()) {
    toast.error(t('systemSettings.waffoPancake.credentialsRequired'))
    return false
  }

  saving.value = true
  try {
    await paymentRequest('/api/option/waffo-pancake/save', {
      merchant_id: form.merchantId,
      private_key: form.privateKey,
      return_url: form.returnUrl,
      store_id: form.storeId,
      product_id: form.productId,
    })
    form.privateKey = ''
    const committedForm = { ...form }
    committedForm.unitPrice = JSON.parse(baseline.value).unitPrice
    committedForm.minTopUp = JSON.parse(baseline.value).minTopUp
    baseline.value = JSON.stringify(committedForm)
    await refreshStatus(true)
    if (
      !(await saveOptions({
        WaffoPancakeUnitPrice: form.unitPrice,
        WaffoPancakeMinTopUp: form.minTopUp,
      }, {
        onApplied(key, value) {
          const saved = JSON.parse(baseline.value)
          if (key === 'WaffoPancakeUnitPrice') saved.unitPrice = Number(value)
          if (key === 'WaffoPancakeMinTopUp') saved.minTopUp = Number(value)
          baseline.value = JSON.stringify(saved)
        },
      }))
    )
      return false
    emit('saved')
    toast.success(t('systemSettings.waffoPancake.saved'))
    if (loadCatalogAfterSave) await loadCatalog()
    return true
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
    return false
  } finally {
    saving.value = false
  }
}

async function loadCatalog() {
  loadingCatalog.value = true
  try {
    catalog.value = (
      await paymentRequest<Catalog>(
        '/api/option/waffo-pancake/catalog',
        undefined,
        undefined,
        'GET'
      )
    ).data
    ensureProductBelongsToStore()
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    loadingCatalog.value = false
  }
}

async function createPair() {
  if (!form.merchantId.trim() || !form.returnUrl.trim()) {
    toast.error(t('systemSettings.waffoPancake.pairCredentialsRequired'))
    return
  }
  if (
    !form.privateKey.trim() &&
    form.merchantId.trim() !== String(rawValue('WaffoPancakeMerchantID', ''))
  ) {
    toast.error(t('systemSettings.waffoPancake.pairPrivateKeyRequired'))
    return
  }

  creating.value = true
  try {
    const result = (
      await paymentRequest<{
        store_id: string
        product_id: string
      }>('/api/option/waffo-pancake/pair', {
        ...(form.privateKey.trim()
          ? { merchant_id: form.merchantId, private_key: form.privateKey }
          : {}),
        return_url: form.returnUrl,
      })
    ).data
    form.storeId = result.store_id
    form.productId = result.product_id
    toast.success(t('systemSettings.waffoPancake.paired'))
  } catch (error) {
    const failure =
      error instanceof ApiError
        ? parseWaffoPancakePairFailure(error.data)
        : undefined
    if (failure?.orphan_store) {
      form.storeId = failure.store_id
      form.productId = ''
      toast.error(
        t('systemSettings.waffoPancake.orphanStore', {
          id: failure.store_id,
          reason: failure.error,
        })
      )
    } else if (failure)
      toast.error(
        t('systemSettings.waffoPancake.pairFailed', { reason: failure.error })
      )
    else toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    creating.value = false
  }
}

onMounted(async () => {
  await load()
  syncForm()
})
</script>

<template>
  <section
    class="waffo-pancake-panel"
    :aria-label="t('systemSettings.waffoPancake.label')"
  >
    <fieldset :disabled="saving || creating || loadingCatalog" class="min-w-0 border-0 p-0">
    <div class="grid gap-4 sm:grid-cols-2">
      <FormField :label="t('systemSettings.waffoPancake.merchantId')">
        <TextInput v-model="form.merchantId" autocomplete="off" />
      </FormField>
      <FormField :label="t('systemSettings.waffoPancake.privateKey')">
        <TextInput
          v-model="form.privateKey"
          type="password"
          autocomplete="new-password"
        />
        <p class="mt-1 text-xs text-[var(--text-tertiary)]">
          {{ t('systemSettings.waffoPancake.keepPrivateKey') }}
        </p>
      </FormField>
      <FormField
        :label="t('systemSettings.waffoPancake.returnUrl')"
        class="sm:col-span-2"
      >
        <TextInput v-model="form.returnUrl" type="url" autocomplete="off" />
      </FormField>
      <FormField :label="t('systemSettings.waffoPancake.unitPrice')">
        <TextInput
          :model-value="String(form.unitPrice)"
          type="number"
          min="0.0001"
          step="0.01"
          autocomplete="off"
          @update:model-value="form.unitPrice = Number($event)"
        />
      </FormField>
      <FormField :label="t('systemSettings.waffoPancake.minTopUp')">
        <TextInput
          :model-value="String(form.minTopUp)"
          type="number"
          min="1"
          step="1"
          autocomplete="off"
          @update:model-value="form.minTopUp = Number($event)"
        />
      </FormField>
    </div>

    <div class="waffo-actions">
      <ConsoleButton
        variant="secondary"
        size="sm"
        :loading="creating"
        @click="createPair"
      >
        {{ t('systemSettings.waffoPancake.createPair') }}
      </ConsoleButton>
      <ConsoleButton
        variant="secondary"
        size="sm"
        :loading="saving || loadingCatalog"
        @click="saveConfiguration(true)"
      >
        {{ t('systemSettings.waffoPancake.saveAndLoad') }}
      </ConsoleButton>
      <ConsoleButton size="sm" :loading="saving" @click="saveConfiguration()">
        {{ t('systemSettings.waffoPancake.save') }}
      </ConsoleButton>
    </div>

    <div v-if="catalog" class="mt-5 grid gap-4 sm:grid-cols-2">
      <FormField :label="t('systemSettings.waffoPancake.store')">
        <select
          v-model="form.storeId"
          class="waffo-select"
          @change="ensureProductBelongsToStore"
        >
          <option value="">
            {{ t('systemSettings.waffoPancake.selectStore') }}
          </option>
          <option
            v-for="store in catalog.stores"
            :key="store.id"
            :value="store.id"
          >
            {{ store.name }} ({{ store.id }})
          </option>
        </select>
      </FormField>
      <FormField :label="t('systemSettings.waffoPancake.product')">
        <select
          v-model="form.productId"
          class="waffo-select"
          :disabled="!selectedStore"
        >
          <option value="">
            {{ t('systemSettings.waffoPancake.selectProduct') }}
          </option>
          <option
            v-for="product in selectedStore?.onetimeProducts ?? []"
            :key="product.id"
            :value="product.id"
          >
            {{ product.name }} ({{ product.id }})
          </option>
        </select>
      </FormField>
    </div>
    </fieldset>
  </section>
</template>

<style scoped>
.waffo-pancake-panel {
  margin-top: 1.5rem;
  border-top: 1px dashed var(--border-default);
  padding-top: 1rem;
}
.waffo-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  margin-top: 1rem;
}
.waffo-select {
  width: 100%;
  height: 2.75rem;
  border: 1.5px solid var(--border-default);
  border-radius: var(--sketch-border-radius-sm);
  background: transparent;
  padding: 0 0.75rem;
  color: var(--text-primary);
  font: inherit;
}
.waffo-select:focus {
  border-color: var(--accent);
  outline: none;
  box-shadow: 0 0 0 3px var(--accent-soft);
}
.waffo-hint {
  margin-top: 0.25rem;
  font-size: 0.75rem;
  color: var(--signal);
}
</style>
