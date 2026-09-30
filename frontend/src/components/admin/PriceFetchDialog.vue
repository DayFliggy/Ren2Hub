<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import {
  pricingConfigApi,
  PRICE_SOURCES,
  type FetchedPriceSource,
  type FetchedModelPrice,
} from '@/api/pricingConfig'
import { modelPricingMessages } from '@/i18n/modelPricing'
import type { PricingDraft } from '@/features/pricing/draft'
const props = defineProps<{ draft: PricingDraft }>()
const emit = defineEmits<{ close: []; fill: [rows: FetchedModelPrice[]] }>()
const { t } = useI18n({ useScope: 'local', messages: modelPricingMessages })
const sources = ref<FetchedPriceSource[]>([])
const source = ref(''),
  search = ref(''),
  selected = ref<string[]>([])
const loading = ref(false),
  error = ref('')
const page = ref(1),
  pageSize = ref(50)
let controller: AbortController | undefined
const names: Record<string, string> = {
  sub2api: 'Sub2API',
  axonhub: 'AxonHub',
  basellm: 'basellm',
  models_dev: 'models.dev',
  openrouter: 'OpenRouter',
}
const current = computed(() =>
  sources.value.find((item) => item.id === source.value)
)
const keyFor = (model: FetchedModelPrice) =>
  JSON.stringify([model.model_name, model.provider])
const rows = computed(() =>
  (current.value?.models ?? []).filter((model) =>
    `${model.model_name} ${model.provider}`
      .toLowerCase()
      .includes(search.value.toLowerCase())
  )
)
const visibleRows = computed(() =>
  rows.value.slice(
    (page.value - 1) * pageSize.value,
    page.value * pageSize.value
  )
)
const selectedKeys = computed(() => new Set(selected.value))
const chosen = computed(() =>
  (current.value?.models ?? []).filter((model) =>
    selectedKeys.value.has(keyFor(model))
  )
)
const overwrite = computed(
  () =>
    chosen.value.filter(
      (model) => Object.keys(props.draft[model.model_name] ?? {}).length
    ).length
)
const duplicate = computed(
  () =>
    new Set(chosen.value.map((model) => model.model_name)).size !==
    chosen.value.length
)
watch(source, () => {
  selected.value = []
  search.value = ''
})
watch([source, search, pageSize], () => {
  page.value = 1
})
async function fetchPrices() {
  controller?.abort()
  controller = new AbortController()
  const request = controller
  loading.value = true
  error.value = ''
  sources.value = []
  selected.value = []
  source.value = ''
  try {
    const result = await pricingConfigApi.fetch(request.signal)
    if (!request.signal.aborted) sources.value = result
  } catch (cause) {
    if (!request.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (controller === request) loading.value = false
  }
}
function selectVisible() {
  selected.value = rows.value
    .filter((row) => !row.unsupported_reason && Object.keys(row.pricing).length)
    .map(keyFor)
}
onMounted(fetchPrices)
onBeforeUnmount(() => controller?.abort())
</script>
<template>
  <ConsoleModal
    :open="true"
    :title="t('sourceTitle')"
    size="xl"
    @close="emit('close')"
  >
    <div class="price-fetch" :aria-busy="loading">
      <p class="admin-muted">{{ t('sourceHint') }}</p>
      <fieldset class="price-source-list">
        <legend class="sr-only">{{ t('source') }}</legend>
        <label
          v-for="id in PRICE_SOURCES"
          :key="id"
          class="price-source"
          :data-selected="source === id"
        >
          <input
            v-model="source"
            type="radio"
            name="price-source"
            :value="id"
            :disabled="
              loading ||
              !sources
                .find((item) => item.id === id)
                ?.models.some(
                  (model) =>
                    !model.unsupported_reason &&
                    Object.keys(model.pricing).length
                )
            "
          />
          <span>{{ names[id] }}</span>
          <small>{{
            t(
              loading
                ? 'loading'
                : (sources.find((item) => item.id === id)?.status ??
                    'unselected')
            )
          }}</small>
          <small v-if="!loading">{{
            t('sourceCount', {
              count:
                sources
                  .find((item) => item.id === id)
                  ?.models.filter((model) => !model.unsupported_reason)
                  .length ?? 0,
            })
          }}</small>
        </label>
      </fieldset>
      <p v-if="error" class="admin-error" role="alert">{{ error }}</p>
      <ul
        v-for="item in sources.filter((item) => item.errors.length)"
        :key="item.id"
        class="admin-error"
      >
        <li v-for="message in item.errors" :key="message">
          {{ item.name }}: {{ message }}
        </li>
      </ul>
      <template v-if="current">
        <div class="price-fetch-tools">
          <label class="admin-field"
            ><span class="sr-only">{{ t('search') }}</span
            ><input v-model="search" :placeholder="t('search')"
          /></label>
          <ConsoleButton variant="ghost" size="sm" @click="selectVisible">{{
            t('chooseAll')
          }}</ConsoleButton>
          <ConsoleButton variant="ghost" size="sm" @click="selected = []">{{
            t('clearSelection')
          }}</ConsoleButton>
        </div>
        <div class="price-fetch-table">
          <table class="admin-table">
            <thead>
              <tr>
                <th>{{ t('choose') }}</th>
                <th>{{ t('name') }}</th>
                <th>{{ t('provider') }}</th>
                <th>{{ t('current') }}</th>
                <th>{{ t('candidate') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="model in visibleRows" :key="keyFor(model)">
                <td>
                  <input
                    v-model="selected"
                    type="checkbox"
                    :value="keyFor(model)"
                    :aria-label="`${t('choose')} ${model.model_name} ${model.provider}`"
                    :disabled="
                      Boolean(model.unsupported_reason) ||
                      !Object.keys(model.pricing).length
                    "
                  />
                </td>
                <td>{{ model.model_name }}</td>
                <td>{{ model.provider }}</td>
                <td>
                  <code>{{
                    JSON.stringify(draft[model.model_name] ?? {})
                  }}</code>
                </td>
                <td>
                  <p v-if="model.unsupported_reason" class="admin-error">
                    {{ model.unsupported_reason }}
                  </p>
                  <code v-else>{{ JSON.stringify(model.pricing) }}</code>
                  <p
                    v-for="warning in model.warnings ?? []"
                    :key="warning"
                    class="admin-muted"
                  >
                    {{ warning }}
                  </p>
                </td>
              </tr>
            </tbody>
          </table>
          <p v-if="!rows.length" class="admin-muted">{{ t('noResults') }}</p>
        </div>
        <TablePagination
          :page="page"
          :page-size="pageSize"
          :total="rows.length"
          @update:page="page = $event"
          @update:page-size="pageSize = $event"
        />
        <p role="status">{{ t('selected', { count: chosen.length }) }}</p>
        <p v-if="overwrite" class="price-overwrite">
          {{ t('overwrite', { count: overwrite }) }}
        </p>
        <p v-if="duplicate" role="alert" class="admin-error">
          {{ t('duplicate') }}
        </p>
      </template>
      <p v-else-if="!loading" class="admin-muted">{{ t('noSource') }}</p>
      <div class="price-fetch-tools price-fetch-footer">
        <ConsoleButton
          variant="ghost"
          :loading="loading"
          @click="fetchPrices"
          >{{ t('retry') }}</ConsoleButton
        >
        <ConsoleButton variant="secondary" @click="emit('close')">{{
          t('cancel')
        }}</ConsoleButton>
        <ConsoleButton
          :disabled="loading || !chosen.length || duplicate"
          @click="emit('fill', chosen)"
          >{{ t('fill') }}</ConsoleButton
        >
      </div>
    </div>
  </ConsoleModal>
</template>
<style scoped>
.price-fetch {
  display: grid;
  gap: 1rem;
  min-width: 0;
}
.price-source-list {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 0.5rem;
  border: 0;
  padding: 0;
}
.price-source {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  border: 1px solid var(--border-subtle);
  padding: 0.75rem;
  border-radius: 0.65rem;
  min-width: 0;
}
.price-source[data-selected='true'] {
  border-color: var(--text-primary);
  background: var(--surface-hover);
}
.price-source small {
  width: 100%;
  color: var(--text-secondary);
}
.price-fetch-tools {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}
.price-fetch-footer {
  justify-content: flex-end;
  border-top: 1px solid var(--border-subtle);
  padding-top: 0.75rem;
}
.price-fetch-table {
  max-height: 22rem;
  overflow: auto;
}
.price-fetch-table table {
  min-width: 700px;
}
.price-fetch-table code {
  display: block;
  max-width: 16rem;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 0.75rem;
}
.price-overwrite {
  padding: 0.75rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
}
@media (max-width: 640px) {
  .price-source-list {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
