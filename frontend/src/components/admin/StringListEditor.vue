<script setup lang="ts">
import { Plus, X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { adminManagementMessages } from '@/i18n/adminManagement'
import IconButton from '@/components/common/IconButton.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
const props = defineProps<{ modelValue: string[]; label: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>()
const { t } = useI18n({ useScope: 'local', messages: adminManagementMessages })
function update(index: number, event: Event) {
  const items = [...props.modelValue]
  items[index] = (event.target as HTMLInputElement).value
  emit('update:modelValue', items)
}
</script>
<template>
  <div class="admin-list-editor">
    <div
      v-for="(item, index) in modelValue"
      :key="index"
      class="admin-list-row"
    >
      <input
        :value="item"
        :aria-label="`${label} ${index + 1}`"
        class="admin-input"
        @input="update(index, $event)"
      />
      <IconButton
        :label="t('remove')"
        @click="
          emit(
            'update:modelValue',
            modelValue.filter((_, i) => i !== index)
          )
        "
        ><X :size="16"
      /></IconButton>
    </div>
    <ConsoleButton
      size="sm"
      variant="ghost"
      @click="emit('update:modelValue', [...modelValue, ''])"
      ><Plus :size="16" />{{ t('addItem') }}</ConsoleButton
    >
  </div>
</template>
