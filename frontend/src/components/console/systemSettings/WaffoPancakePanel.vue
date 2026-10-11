<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/console'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import FormField from '@/components/common/FormField.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useSystemSettings } from '@/composables/useSystemSettings'
import { useToast } from '@/composables/useToast'

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
const { load, rawValue, isSecretConfigured } = useSystemSettings()
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

const selectedStore = computed(() =>
  catalog.value?.stores.find((store) => store.id === form.storeId)
)
const privateKeyConfigured = computed(() =>
  isSecretConfigured('WaffoPancakePrivateKey')
)

function syncForm() {
  form.merchantId = String(rawValue('WaffoPancakeMerchantID', ''))
  form.returnUrl = String(rawValue('WaffoPancakeReturnURL', ''))
  form.storeId = String(rawValue('WaffoPancakeStoreID', ''))
  form.productId = String(rawValue('WaffoPancakeProductID', ''))
  form.unitPrice = Number(rawValue('WaffoPancakeUnitPrice', 1)) || 1
  form.minTopUp = Number(rawValue('WaffoPancakeMinTopUp', 1)) || 1
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
  if (!form.privateKey.trim() && !privateKeyConfigured.value) {
    toast.error(t('systemSettings.waffoPancake.privateKeyRequired'))
    return false
  }

  saving.value = true
  try {
    await api.post('/api/option/waffo-pancake/save', {
      merchant_id: form.merchantId,
      private_key: form.privateKey,
      return_url: form.returnUrl,
      store_id: form.storeId,
      product_id: form.productId,
      unit_price: form.unitPrice,
      min_top_up: form.minTopUp,
    })
    form.privateKey = ''
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
    catalog.value = await api.get<Catalog>('/api/option/waffo-pancake/catalog')
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
  if (!form.privateKey.trim() && !privateKeyConfigured.value) {
    toast.error(t('systemSettings.waffoPancake.pairPrivateKeyRequired'))
    return
  }

  creating.value = true
  try {
    const result = await api.post<{
      store_id: string
      product_id: string
    }>('/api/option/waffo-pancake/pair', {
      merchant_id: form.merchantId,
      private_key: form.privateKey,
      return_url: form.returnUrl,
    })
    form.storeId = result.store_id
    form.productId = result.product_id
    toast.success(t('systemSettings.waffoPancake.paired'))
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
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
        <div v-if="privateKeyConfigured" class="mt-1 flex items-center gap-1.5">
          <StatusChip tone="success" class="text-xs">{{
            t('systemSettings.waffoPancake.configured')
          }}</StatusChip>
          <span class="text-xs text-[var(--text-tertiary)]">{{
            t('systemSettings.waffoPancake.keepPrivateKey')
          }}</span>
        </div>
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
