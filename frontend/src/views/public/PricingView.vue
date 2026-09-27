<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, Check, Copy } from 'lucide-vue-next'
import { useClipboard } from '@vueuse/core'
import {
  catalogPrices,
  publicCatalogApi,
  type CatalogModel,
  type CatalogPrice,
  type PricingCatalog,
} from '@/api/publicCatalog'
import PublicPage from '@/components/public/PublicPage.vue'
import VendorLogo from '@/components/console/models/VendorLogo.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { formatDecimalUSD } from '@/utils/decimal'
import { publicPageMessages } from '@/i18n/publicPages'

const route = useRoute()
const { t } = useI18n({
  useScope: 'local',
  messages: publicPageMessages,
})
const { copy, copied } = useClipboard()
const catalog = ref<PricingCatalog | null>(null)
const error = ref('')
const loading = ref(false)
const keyword = ref(typeof route.query.q === 'string' ? route.query.q : '')
const vendor = ref(
  typeof route.query.vendor === 'string' ? route.query.vendor : ''
)
const unit = ref(route.query.tokenUnit === 'K' ? 'K' : 'M')
const page = ref(1)
const pageSize = ref(20)
const billing = ref('')
const selectedVersion = ref(
  typeof route.query.priceVersion === 'string' ? route.query.priceVersion : ''
)
let controller: AbortController | undefined
const detailId = computed(() =>
  typeof route.params.modelId === 'string' ? route.params.modelId : ''
)
const modelVersions = computed(() =>
  (catalog.value?.models ?? []).flatMap((model) =>
    (model.price_versions ?? [model.price_version]).map((version) => ({
      ...model,
      price_version: version,
    }))
  )
)
const detailVersions = computed(() =>
  modelVersions.value.filter((model) => model.model_name === detailId.value)
)
const detail = computed(() =>
  detailVersions.value.find(
    (model) =>
      !selectedVersion.value ||
      String(model.price_version.id) === selectedVersion.value
  )
)
const versionOptions = computed(() =>
  detailVersions.value.map((model) => ({
    value: String(model.price_version.id),
    label: `${model.price_version.scope_kind ?? 'global_model'}${model.price_version.channel_id ? ` · ${model.price_version.channel_id}` : ''} · v${model.price_version.id}`,
  }))
)
const unitOptions = computed(() => [
  { value: 'M', label: t('publicPage.million') },
  { value: 'K', label: t('publicPage.thousand') },
])
const vendorName = (model: CatalogModel) =>
  catalog.value?.vendors.find((entry) => entry.id === model.vendor_id)?.name ||
  model.owner_by
const vendorOptions = computed(() => [
  { value: '', label: t('publicPage.all') },
  ...[...new Set((catalog.value?.models ?? []).map(vendorName))]
    .sort()
    .map((name) => ({ value: name, label: name })),
])
const billingOptions = computed(() => [
  { value: '', label: t('publicPage.all') },
  { value: 'token', label: t('publicPage.tokenBilling') },
  { value: 'request', label: t('publicPage.requestBilling') },
  { value: 'expression', label: t('publicPage.dynamic') },
  { value: 'free', label: t('publicPage.free') },
])
const billingMode = (model: CatalogModel) =>
  model.price_version.mode === 'expression'
    ? 'expression'
    : model.price_version.mode === 'free'
      ? 'free'
      : model.price_version.prices.some(
            (price) => price.unit === 'per_million_tokens'
          )
        ? 'token'
        : 'request'
const filtered = computed(() =>
  modelVersions.value.filter((model) => {
    const search = keyword.value.trim().toLowerCase()
    return (
      (!search ||
        `${model.model_name} ${vendorName(model)} ${model.description} ${model.tags}`
          .toLowerCase()
          .includes(search)) &&
      (!vendor.value || vendorName(model) === vendor.value) &&
      (!billing.value || billingMode(model) === billing.value)
    )
  })
)
const pageModels = computed(() =>
  filtered.value.slice(
    (page.value - 1) * pageSize.value,
    page.value * pageSize.value
  )
)
const money = (value: string) => formatDecimalUSD(value, 3)
const prices = (model: CatalogModel) => {
  const tokenUnit = unit.value === 'K' ? 'K' : 'M'
  const lines = catalogPrices(model, tokenUnit)
  const version = model.price_version
  const flatFee: CatalogPrice[] =
    version.flat_fee && version.flat_fee !== '0'
      ? [{ key: 'flatFee', value: version.flat_fee, unit: 'per_call' }]
      : []
  return [...lines, ...flatFee].map((price) => ({
    ...price,
    formatted: price.tiers?.length
      ? price.tiers
          .map((tier) => {
            const bound = tier.up_to ? `≤${tier.up_to}` : '∞'
            return `${bound}: ${money(tier.unit_price)}`
          })
          .join(' / ')
      : money(price.value),
  }))
}
const detailEndpoints = computed(() =>
  (detail.value?.supported_endpoint_types ?? []).map((name) => ({
    name,
    ...catalog.value?.endpoints[name],
  }))
)
watch([keyword, vendor, billing, pageSize], () => {
  page.value = 1
})
watch(
  () => route.query.priceVersion,
  (value) => {
    selectedVersion.value = typeof value === 'string' ? value : ''
  }
)

async function load() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  error.value = ''
  try {
    const result = await publicCatalogApi.pricing(request.signal)
    if (request.signal.aborted) return
    catalog.value = result
  } catch (cause) {
    if (!request.signal.aborted)
      error.value =
        cause instanceof Error ? cause.message : t('common.loadFailed')
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}
async function copyName(name: string) {
  try {
    await copy(name)
  } catch (cause) {
    error.value =
      cause instanceof Error ? cause.message : t('common.loadFailed')
  }
}
onMounted(load)
onScopeDispose(() => controller?.abort())
</script>

<template>
  <PublicPage :title="detailId || t('publicPage.pricing')">
    <template #actions>
      <RouterLink
        v-if="detailId"
        :to="{ name: 'pricing', query: { tokenUnit: unit } }"
        class="inline-flex min-h-11 items-center gap-2 text-sm text-[var(--accent-text)]"
        ><ArrowLeft :size="16" />{{ t('publicPage.pricing') }}</RouterLink
      >
      <span v-else class="text-sm text-[var(--text-secondary)]">{{
        t('publicPage.pricesCount', { count: filtered.length })
      }}</span>
    </template>
    <div
      v-if="loading"
      role="status"
      :aria-label="t('publicPage.loading')"
      class="space-y-4"
    >
      <SkeletonBlock class="h-12 w-full" /><SkeletonBlock class="h-80 w-full" />
    </div>
    <ErrorBanner v-else-if="error" :message="error" @retry="load" />
    <template v-else-if="catalog">
      <div class="mb-6 flex flex-wrap items-center gap-3">
        <SearchInput
          v-if="!detailId"
          v-model="keyword"
          :placeholder="t('publicPage.search')"
          :aria-label="t('publicPage.search')"
          class="w-full sm:w-64"
        />
        <FilterSelect
          v-if="!detailId"
          v-model="vendor"
          :options="vendorOptions"
          :label="t('publicPage.vendor')"
          :prefix-label="t('publicPage.vendor')"
        />
        <FilterSelect
          v-if="!detailId"
          v-model="billing"
          :options="billingOptions"
          :label="t('publicPage.billing')"
          :prefix-label="t('publicPage.billing')"
        />
        <FilterSelect
          v-if="detailId && versionOptions.length > 1"
          v-model="selectedVersion"
          :options="versionOptions"
          :label="t('publicPage.priceScope')"
        />
        <FilterSelect
          v-model="unit"
          :options="unitOptions"
          :label="t('publicPage.unit')"
        />
        <span class="text-xs text-[var(--text-tertiary)]">{{
          t('publicPage.usd')
        }}</span>
      </div>
      <template v-if="detailId">
        <EmptyState v-if="!detail" :title="t('publicPage.detailMissing')" />
        <article v-else class="space-y-8">
          <div class="flex items-start gap-4">
            <VendorLogo :vendor="vendorName(detail)" :size="48" />
            <div class="min-w-0 flex-1">
              <h2 class="text-lg font-semibold">{{ vendorName(detail) }}</h2>
              <p
                class="mt-2 whitespace-pre-line break-words text-sm leading-6 text-[var(--text-secondary)]"
              >
                {{ detail.description }}
              </p>
              <p
                v-if="detail.tags"
                class="mt-2 break-words text-xs text-[var(--text-tertiary)]"
              >
                {{ detail.tags }}
              </p>
            </div>
            <ConsoleButton
              variant="ghost"
              :aria-label="t('publicPage.copy')"
              :title="copied ? t('publicPage.copied') : t('publicPage.copy')"
              @click="copyName(detail.model_name)"
              ><Check v-if="copied" :size="18" /><Copy v-else :size="18"
            /></ConsoleButton>
          </div>
          <section>
            <h2 class="mb-4 text-lg font-semibold">
              {{ t('publicPage.billing') }}
            </h2>
            <template v-if="detail.price_version.mode === 'expression'"
              ><p class="mb-3 text-sm text-[var(--accent-text)]">
                {{ t('publicPage.dynamic') }}
              </p>
              <details>
                <summary class="cursor-pointer text-sm">
                  {{ t('publicPage.expression') }}
                </summary>
                <pre
                  class="mt-3 overflow-x-auto whitespace-pre-wrap break-all bg-[var(--surface-muted)] p-4 text-xs"
                  >{{ detail.price_version.expression }}</pre>
              </details></template
            >
            <dl
              v-if="prices(detail).length"
              class="grid grid-cols-2 gap-x-8 gap-y-5 border-y border-[var(--border-subtle)] py-5 sm:grid-cols-3 lg:grid-cols-4"
            >
              <div v-for="price in prices(detail)" :key="price.key">
                <dt class="text-xs text-[var(--text-secondary)]">
                  {{ t(`publicPage.${price.key}`, price.key) }}
                  <span v-if="price.tier_mode">
                    · {{ t(`publicPage.tierModes.${price.tier_mode}`) }}</span
                  >
                </dt>
                <dd class="mt-2 break-words font-mono text-sm">
                  {{ price.formatted }}
                  <span class="block text-xs text-[var(--text-tertiary)]">{{
                    price.unit ? t(`publicPage.units.${price.unit}`) : ''
                  }}</span>
                </dd>
              </div>
            </dl>
            <dl
              class="mt-4 grid gap-2 text-xs text-[var(--text-tertiary)] sm:grid-cols-3"
              data-price-version-evidence
            >
              <div>
                <dt>{{ t('publicPage.priceScope') }}</dt>
                <dd class="font-mono">
                  {{ detail.price_version.scope_kind || 'global_model'
                  }}<span v-if="detail.price_version.channel_id">
                    · {{ detail.price_version.channel_id }}</span
                  >
                </dd>
              </div>
              <div>
                <dt>{{ t('publicPage.priceVersion') }}</dt>
                <dd class="font-mono">{{ detail.price_version.id }}</dd>
              </div>
              <div>
                <dt>{{ t('publicPage.priceHash') }}</dt>
                <dd class="break-all font-mono">
                  {{ detail.price_version.hash }}
                </dd>
              </div>
            </dl>
          </section>
          <section v-if="detailEndpoints.length">
            <h2 class="mb-4 text-lg font-semibold">
              {{ t('publicPage.endpoint') }}
            </h2>
            <div class="overflow-x-auto">
              <table>
                <thead>
                  <tr>
                    <th>{{ t('publicPage.endpoint') }}</th>
                    <th>{{ t('publicPage.method') }}</th>
                    <th>{{ t('publicPage.path') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="endpoint in detailEndpoints" :key="endpoint.name">
                    <td>{{ endpoint.name }}</td>
                    <td>{{ endpoint.method || '--' }}</td>
                    <td class="font-mono">{{ endpoint.path || '--' }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </article>
      </template>
      <template v-else>
        <EmptyState v-if="!filtered.length" :title="t('publicPage.noModels')" />
        <div
          v-else
          class="overflow-x-auto"
          tabindex="0"
          role="region"
          :aria-label="t('publicPage.pricing')"
        >
          <table class="min-w-[660px]">
            <thead>
              <tr>
                <th>{{ t('publicPage.model') }}</th>
                <th>{{ t('publicPage.vendor') }}</th>
                <th>{{ t('publicPage.billing') }}</th>
                <th>{{ t('publicPage.prices') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="model in pageModels"
                :key="`${model.model_name}:${model.price_version.id}`"
              >
                <td class="max-w-96">
                  <RouterLink
                    :to="{
                      name: 'pricing-model',
                      params: { modelId: model.model_name },
                      query: {
                        tokenUnit: unit,
                        priceVersion: String(model.price_version.id),
                      },
                    }"
                    class="break-words font-medium text-[var(--accent-text)]"
                    >{{ model.model_name }}</RouterLink
                  >
                  <p class="mt-1 text-xs text-[var(--text-tertiary)]">
                    {{ model.price_version.scope_kind ?? 'global_model'
                    }}<span v-if="model.price_version.channel_id">
                      · {{ model.price_version.channel_id }}</span
                    >
                    · v{{ model.price_version.id }}
                  </p>
                </td>
                <td>
                  <span class="flex items-center gap-2"
                    ><VendorLogo :vendor="vendorName(model)" :size="28" />{{
                      vendorName(model)
                    }}</span
                  >
                </td>
                <td>
                  {{
                    t(
                      `publicPage.${billingMode(model) === 'expression' ? 'dynamic' : billingMode(model) === 'request' ? 'requestBilling' : billingMode(model) === 'free' ? 'free' : 'tokenBilling'}`
                    )
                  }}
                </td>
                <td>
                  <p
                    v-for="price in prices(model)"
                    :key="price.key"
                    class="mb-1 flex flex-wrap gap-x-2 text-xs"
                  >
                    <span
                      >{{ t(`publicPage.${price.key}`, price.key)
                      }}<span v-if="price.tier_mode">
                        ·
                        {{ t(`publicPage.tierModes.${price.tier_mode}`) }}</span
                      ></span
                    ><span class="font-mono">{{ price.formatted }}</span
                    ><span
                      v-if="price.unit"
                      class="text-[var(--text-tertiary)]"
                      >{{ t(`publicPage.units.${price.unit}`) }}</span
                    >
                  </p>
                  <span v-if="!prices(model).length">{{
                    t('publicPage.dynamic')
                  }}</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <TablePagination
          v-model:page="page"
          v-model:page-size="pageSize"
          :total="filtered.length"
        />
      </template>
    </template>
  </PublicPage>
</template>
