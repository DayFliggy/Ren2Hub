<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/client'
import { publicCatalogApi, type PricingCatalog } from '@/api/publicCatalog'
import { parseUserModels } from '@/api/liveContracts'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import ModelPriceItems from '@/components/console/models/ModelPriceItems.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
const { t } = useI18n(),
  catalog = ref<PricingCatalog | null>(null),
  allowed = ref<string[]>([]),
  groups = ref<Record<string, { ratio: number | 'auto'; desc: string }>>({}),
  group = ref(''),
  keyword = ref(''),
  error = ref(''),
  loading = ref(false),
  request = useLatestRequest()
const models = computed(() =>
  (catalog.value?.models ?? []).filter(
    (model) =>
      allowed.value.includes(model.model_name) &&
      (!keyword.value ||
        `${model.model_name} ${model.description}`
          .toLowerCase()
          .includes(keyword.value.toLowerCase()))
  )
)
const ratio = computed(() => {
  const value = groups.value[group.value]?.ratio
  return typeof value === 'number' ? value : 1
})
async function load() {
  loading.value = true
  error.value = ''
  const result = await request.run(async (signal) => {
    const [prices, names] = await Promise.all([
      publicCatalogApi.pricing(signal),
      api.get<unknown>(
        '/api/user/models',
        group.value ? { group: group.value } : undefined,
        { signal }
      ),
    ])
    return { prices, names: parseUserModels(names) }
  })
  if (result.stale) return
  loading.value = false
  if (result.ok) {
    catalog.value = result.value.prices
    allowed.value = result.value.names
  } else
    error.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
}
onMounted(async () => {
  try {
    groups.value = await api.get('/api/user/self/groups')
    group.value = Object.keys(groups.value).find((key) => key !== 'auto') ?? ''
    if (!group.value) await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
})
watch(group, load)
</script>
<template>
  <div>
    <PageBreadcrumb :crumbs="[t('nav.models')]" /><ConsoleCard
      :title="t('nav.models')"
      ><template #action
        ><ConsoleButton variant="secondary" :disabled="loading" @click="load">{{
          t('common.refresh')
        }}</ConsoleButton></template
      >
      <div class="mb-5 flex flex-wrap gap-3">
        <SearchInput v-model="keyword" /><label
          >{{ t('users.group') }}
          <select v-model="group" class="console-input">
            <option
              v-for="(item, name) in groups"
              :key="name"
              :value="String(name)"
            >
              {{ name }} · {{ item.desc }}
              <template v-if="typeof item.ratio === 'number'"
                >· {{ item.ratio }}×</template
              >
            </option>
          </select></label
        >
      </div>
      <p
        v-if="groups[group]?.ratio === 'auto'"
        class="mb-4 text-sm text-[var(--text-secondary)]"
      >
        {{ t('models.autoPriceHint') }}
      </p>
      <ErrorBanner v-if="error" :message="error" />
      <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        <article
          v-for="model in models"
          :key="model.model_name"
          class="rounded-xl border border-[var(--border-subtle)] p-4"
        >
          <h2 class="mb-2 break-all font-semibold">{{ model.model_name }}</h2>
          <p class="mb-4 text-sm text-[var(--text-secondary)]">
            {{ model.description }}
          </p>
          <ModelPriceItems :pricing="model" :group-ratio="ratio" />
          <p class="mt-4 break-all text-xs text-[var(--text-tertiary)]">
            {{ model.supported_endpoint_types.join(' · ') }}
          </p>
        </article>
      </div>
      <p v-if="!loading && !error && !models.length" class="p-8 text-center">
        {{ t('common.none') }}
      </p></ConsoleCard
    >
  </div>
</template>
