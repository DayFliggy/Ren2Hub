<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, Check, Copy } from 'lucide-vue-next'
import { useClipboard } from '@vueuse/core'
import {
  catalogGroupRatio,
  catalogPrices,
  publicCatalogApi,
  type CatalogModel,
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
import { publicPageMessages } from '@/i18n/publicPages'

const route = useRoute()
const { t, locale } = useI18n({
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
const group = ref(
  typeof route.query.group === 'string' ? route.query.group : ''
)
const unit = ref(route.query.tokenUnit === 'K' ? 'K' : 'M')
const page = ref(1)
const pageSize = ref(20)
const billing = ref('')
let controller: AbortController | undefined
const detailId = computed(() =>
  typeof route.params.modelId === 'string' ? route.params.modelId : ''
)
const detail = computed(() =>
  catalog.value?.models.find((model) => model.model_name === detailId.value)
)
const unitOptions = computed(() => [
  { value: 'M', label: t('publicPage.million') },
  { value: 'K', label: t('publicPage.thousand') },
])
const groupOptions = computed(() =>
  Object.entries(catalog.value?.usableGroups ?? {}).map(([value, label]) => ({
    value,
    label: label ? `${value} · ${label}` : value,
  }))
)
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
  { value: 'tiered_expr', label: t('publicPage.dynamic') },
])
const billingMode = (model: CatalogModel) =>
  model.billing_mode === 'tiered_expr'
    ? 'tiered_expr'
    : model.quota_type === 1
      ? 'request'
      : 'token'
const ratio = (model: CatalogModel) =>
  catalog.value ? catalogGroupRatio(catalog.value, model, group.value) : null
const filtered = computed(() =>
  (catalog.value?.models ?? []).filter((model) => {
    const search = keyword.value.trim().toLowerCase()
    return (
      (!search ||
        `${model.model_name} ${vendorName(model)} ${model.description} ${model.tags}`
          .toLowerCase()
          .includes(search)) &&
      (!vendor.value || vendorName(model) === vendor.value) &&
      (!billing.value || billingMode(model) === billing.value) &&
      ratio(model) !== null
    )
  })
)
const pageModels = computed(() =>
  filtered.value.slice(
    (page.value - 1) * pageSize.value,
    page.value * pageSize.value
  )
)
const money = (value: number) =>
  new Intl.NumberFormat(locale.value, {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: 8,
  }).format(value)
const prices = (model: CatalogModel) => {
  const multiplier = ratio(model)
  return multiplier === null
    ? []
    : catalogPrices(model, multiplier, unit.value === 'K' ? 'K' : 'M')
}
const detailEndpoints = computed(() =>
  (detail.value?.supported_endpoint_types ?? []).map((name) => ({
    name,
    ...catalog.value?.endpoints[name],
  }))
)
watch([keyword, vendor, billing, group, pageSize], () => {
  page.value = 1
})

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
    if (!(group.value in result.usableGroups))
      group.value = Object.keys(result.usableGroups)[0] ?? ''
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
        :to="{ name: 'pricing', query: { group, tokenUnit: unit } }"
        class="inline-flex min-h-11 items-center gap-2 text-sm text-[var(--accent-text)]"
        ><ArrowLeft :size="16" />{{ t('publicPage.pricing') }}</RouterLink
      >
      <span v-else class="text-sm text-[var(--text-secondary)]">{{
        t('publicPage.modelsCount', { count: filtered.length })
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
          v-model="group"
          :options="groupOptions"
          :label="t('publicPage.group')"
          :prefix-label="t('publicPage.group')"
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
      <EmptyState
        v-if="!groupOptions.length"
        :title="t('publicPage.noGroup')"
      />
      <template v-else-if="detailId">
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
            <template v-if="detail.billing_mode === 'tiered_expr'"
              ><p class="mb-3 text-sm text-[var(--accent-text)]">
                {{ t('publicPage.dynamic') }}
              </p>
              <details>
                <summary class="cursor-pointer text-sm">
                  {{ t('publicPage.expression') }}
                </summary>
                <pre
                  class="mt-3 overflow-x-auto whitespace-pre-wrap break-all bg-[var(--surface-muted)] p-4 text-xs"
                  >{{ detail.billing_expr }}</pre>
              </details></template
            >
            <p
              v-else-if="ratio(detail) === null"
              class="text-sm text-[var(--text-secondary)]"
            >
              {{ t('publicPage.unknownPrice') }}
            </p>
            <dl
              v-else
              class="grid grid-cols-2 gap-x-8 gap-y-5 border-y border-[var(--border-subtle)] py-5 sm:grid-cols-3 lg:grid-cols-4"
            >
              <div v-for="price in prices(detail)" :key="price.key">
                <dt class="text-xs text-[var(--text-secondary)]">
                  {{ t(`publicPage.${price.key}`) }}
                </dt>
                <dd class="mt-2 font-mono text-lg">{{ money(price.value) }}</dd>
              </div>
            </dl>
            <p class="mt-4 break-words text-xs text-[var(--text-tertiary)]">
              {{ t('publicPage.availableGroups') }}:
              {{ detail.enable_groups.join(', ') }}
            </p>
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
                <th>{{ t('publicPage.input') }}</th>
                <th>{{ t('publicPage.output') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="model in pageModels" :key="model.model_name">
                <td class="max-w-96">
                  <RouterLink
                    :to="{
                      name: 'pricing-model',
                      params: { modelId: model.model_name },
                      query: { group, tokenUnit: unit },
                    }"
                    class="break-words font-medium text-[var(--accent-text)]"
                    >{{ model.model_name }}</RouterLink
                  >
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
                      `publicPage.${billingMode(model) === 'tiered_expr' ? 'dynamic' : billingMode(model) === 'request' ? 'requestBilling' : 'tokenBilling'}`
                    )
                  }}
                </td>
                <td class="whitespace-nowrap font-mono">
                  {{ prices(model)[0] ? money(prices(model)[0]!.value) : '--' }}
                </td>
                <td class="whitespace-nowrap font-mono">
                  {{ prices(model)[1] ? money(prices(model)[1]!.value) : '--' }}
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
