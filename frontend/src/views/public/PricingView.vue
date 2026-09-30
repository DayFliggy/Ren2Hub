<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { publicCatalogApi, type PricingCatalog } from '@/api/publicCatalog'
import { modelPricingMessages } from '@/i18n/modelPricing'
import PublicPage from '@/components/public/PublicPage.vue'
import ModelPriceItems from '@/components/console/models/ModelPriceItems.vue'
import VendorLogo from '@/components/console/models/VendorLogo.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
const { t } = useI18n({ useScope: 'local', messages: modelPricingMessages }),
  route = useRoute()
const catalog = ref<PricingCatalog | null>(null),
  loading = ref(false),
  error = ref('')
const search = ref(typeof route.query.q === 'string' ? route.query.q : ''),
  vendor = ref(''),
  mode = ref(''),
  unit = ref<'M' | 'K'>(route.query.tokenUnit === 'K' ? 'K' : 'M'),
  page = ref(1),
  pageSize = ref(20)
let controller: AbortController | undefined
const detailId = computed(() => String(route.params.modelId ?? ''))
const detail = computed(() =>
  catalog.value?.models.find((model) => model.model_name === detailId.value)
)
const models = computed(() =>
  (catalog.value?.models ?? []).filter(
    (model) =>
      (!search.value ||
        `${model.model_name} ${model.description} ${model.tags}`
          .toLowerCase()
          .includes(search.value.toLowerCase())) &&
      (!vendor.value || String(model.vendor_id) === vendor.value) &&
      (!mode.value ||
        (model.billing_mode === 'unconfigured'
          ? 'unconfigured'
          : model.billing_mode === 'tiered_expr'
            ? 'expression'
            : model.quota_type === 1
              ? 'request'
              : 'token') === mode.value)
  )
)
const visible = computed(() =>
  models.value.slice(
    (page.value - 1) * pageSize.value,
    page.value * pageSize.value
  )
)
watch([search, vendor, mode, pageSize], () => {
  page.value = 1
})
async function load() {
  controller?.abort()
  controller = new AbortController()
  const request = controller
  loading.value = true
  error.value = ''
  try {
    const value = await publicCatalogApi.pricing(request.signal)
    if (!request.signal.aborted) catalog.value = value
  } catch (cause) {
    if (!request.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (controller === request) loading.value = false
  }
}
async function copy(name: string) {
  try {
    await navigator.clipboard.writeText(name)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  }
}
onMounted(load)
onBeforeUnmount(() => controller?.abort())
</script>
<template>
  <PublicPage :title="detailId || t('title')">
    <template #actions
      ><RouterLink v-if="detailId" :to="{ name: 'pricing' }">{{
        t('title')
      }}</RouterLink
      ><span v-else>{{ t('total', { count: models.length }) }}</span></template
    >
    <ErrorBanner v-if="error" :message="error" />
    <p v-if="loading" role="status">{{ t('loading') }}</p>
    <p
      v-if="catalog?.issues.length"
      role="status"
      class="text-sm text-[var(--text-secondary)]"
    >
      {{ t('unsupportedPrice') }}: {{ catalog.issues.length }}
    </p>
    <template v-if="detail"
      ><div class="public-pricing-detail">
        <VendorLogo :vendor="detail.owner_by" />
        <div>
          <h2>{{ detail.model_name }}</h2>
          <p>{{ detail.description }}</p>
        </div>
        <ConsoleButton variant="ghost" @click="copy(detail.model_name)">{{
          t('copy')
        }}</ConsoleButton>
      </div>
      <ModelPriceItems
        :pricing="detail"
        :unit="unit"
        class="public-pricing-card"
      />
      <dl class="public-pricing-endpoints">
        <div
          v-for="endpoint in detail.supported_endpoint_types"
          :key="endpoint"
        >
          <dt>{{ endpoint }}</dt>
          <dd>
            {{ catalog?.endpoints[endpoint]?.method }}
            {{ catalog?.endpoints[endpoint]?.path }}
          </dd>
        </div>
      </dl></template
    >
    <template v-else-if="!detailId"
      ><div class="public-pricing-filters">
        <SearchInput v-model="search" :placeholder="t('search')" /><select
          v-model="vendor"
          :aria-label="t('provider')"
        >
          <option value="">{{ t('all') }}</option>
          <option
            v-for="item in catalog?.vendors ?? []"
            :key="item.id"
            :value="String(item.id)"
          >
            {{ item.name }}
          </option></select
        ><select v-model="mode" :aria-label="t('mode')">
          <option value="">{{ t('all') }}</option>
          <option value="expression">{{ t('expression') }}</option>
          <option value="token">{{ t('perToken') }}</option>
          <option value="request">{{ t('perCall') }}</option>
          <option value="unconfigured">{{ t('unpriced') }}</option></select
        ><select v-model="unit" :aria-label="t('unit')">
          <option value="M">1M tokens</option>
          <option value="K">1K tokens</option>
        </select>
      </div>
      <div class="public-pricing-grid">
        <article
          v-for="model in visible"
          :key="model.model_name"
          class="public-pricing-card"
        >
          <RouterLink
            :to="{
              name: 'pricing-model',
              params: { modelId: model.model_name },
              query: { tokenUnit: unit },
            }"
            ><h2>{{ model.model_name }}</h2></RouterLink
          >
          <p>{{ model.description }}</p>
          <ModelPriceItems :pricing="model" :unit="unit" />
        </article>
      </div>
      <TablePagination
        :page="page"
        :page-size="pageSize"
        :total="models.length"
        @update:page="page = $event"
        @update:page-size="pageSize = $event"
    /></template>
    <EmptyState
      v-if="!loading && !error && (detailId ? !detail : !models.length)"
      :title="t('emptyPublished')"
      :hint="t('noDefault')"
    />
  </PublicPage>
</template>
<style scoped>
.public-pricing-filters {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem;
  margin-bottom: 1.5rem;
}
.public-pricing-filters select {
  background: var(--surface-solid);
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
  padding: 0.5rem;
  max-width: 100%;
}
.public-pricing-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 20rem), 1fr));
  gap: 1rem;
}
.public-pricing-card {
  min-width: 0;
  padding: 1.25rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  background: var(--surface-solid);
  display: grid;
  gap: 0.75rem;
}
h2 {
  font-weight: 600;
  overflow-wrap: anywhere;
}
.public-pricing-card > p {
  font-size: 0.85rem;
  color: var(--text-secondary);
}
.public-pricing-detail {
  display: flex;
  align-items: center;
  gap: 1rem;
  margin-bottom: 1.25rem;
}
.public-pricing-endpoints {
  margin-top: 1rem;
  display: grid;
  gap: 0.5rem;
  overflow-wrap: anywhere;
}
</style>
