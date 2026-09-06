<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, RefreshCw, X } from 'lucide-vue-next'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import { metadataApi } from '@/api/adminManagement'
import { modelMetadataMessages } from '@/i18n/modelMetadata'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: []; create: [name: string] }>()
const { t } = useI18n({ useScope: 'local', messages: modelMetadataMessages })
const models = ref<string[]>([])
const search = ref('')
const page = ref(1)
const pageSize = ref(10)
const loading = ref(false)
const error = ref('')
let controller: AbortController | undefined
const filtered = computed(() =>
  models.value.filter((name) =>
    name.toLocaleLowerCase().includes(search.value.toLocaleLowerCase())
  )
)
const visible = computed(() =>
  filtered.value.slice(
    (page.value - 1) * pageSize.value,
    page.value * pageSize.value
  )
)
watch(search, () => {
  page.value = 1
})
async function load() {
  controller?.abort()
  const current = new AbortController()
  controller = current
  loading.value = true
  error.value = ''
  try {
    const data = await metadataApi.missing(current.signal)
    if (!current.signal.aborted) models.value = data
  } catch (cause) {
    if (!current.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!current.signal.aborted) loading.value = false
  }
}
watch(
  () => props.open,
  (open, _, cleanup) => {
    if (open) {
      search.value = ''
      page.value = 1
      void load()
    }
    cleanup(() => controller?.abort())
  },
  { immediate: true }
)
</script>
<template>
  <ConsoleModal
    :open="open"
    :title="t('missing')"
    size="lg"
    @close="emit('close')"
  >
    <div class="admin-form">
      <div class="missing-tools">
        <label class="admin-field"
          ><span class="sr-only">{{ t('missingSearch') }}</span
          ><input v-model="search" :placeholder="t('missingSearch')" /></label
        ><ConsoleButton variant="ghost" :loading="loading" @click="load"
          ><RefreshCw :size="16" />{{ t('refresh') }}</ConsoleButton
        >
      </div>
      <p v-if="error" class="admin-error" role="alert">{{ error }}</p>
      <p v-if="loading" class="admin-muted" role="status">{{ t('loading') }}</p>
      <ul v-else class="missing-list">
        <li v-for="name in visible" :key="name">
          <span>{{ name }}</span
          ><ConsoleButton
            size="sm"
            variant="ghost"
            @click="emit('create', name)"
            ><Plus :size="16" />{{ t('missingCreate') }}</ConsoleButton
          >
        </li>
      </ul>
      <p v-if="!loading && !error && !visible.length" class="admin-muted">
        {{ search ? t('empty') : t('missingEmpty') }}
      </p>
      <TablePagination
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="filtered.length"
      />
    </div>
    <template #footer
      ><ConsoleButton variant="ghost" @click="emit('close')"
        ><X :size="16" />{{ t('close') }}</ConsoleButton
      ></template
    >
  </ConsoleModal>
</template>
<style scoped>
.missing-tools {
  display: flex;
  gap: 0.75rem;
  flex-wrap: wrap;
}
.missing-tools label {
  flex: 1;
  min-width: 12rem;
}
.missing-list li {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.75rem 0;
  border-bottom: 1px solid var(--border-subtle);
}
.missing-list li span {
  min-width: 0;
  overflow-wrap: anywhere;
}
.missing-list li button {
  flex-shrink: 0;
}
</style>
