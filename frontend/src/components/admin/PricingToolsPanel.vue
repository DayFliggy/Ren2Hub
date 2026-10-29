<script setup lang="ts">
import { computed, ref } from 'vue'
import { useSystemSettings } from '@/composables/useSystemSettings'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ToolPricingEditor from './ToolPricingEditor.vue'
// The saved setting is the editor's baseline. A new revision (page reload)
// or a completed save rebuilds the editor, discarding its rows even when the
// saved text did not change.
defineProps<{ revision: number }>()
const emit = defineEmits<{ dirty: [value: boolean]; saved: [] }>()
const settings = useSystemSettings()
const saved = computed(() =>
  String(settings.rawValue('tool_price_setting.prices', '{}'))
)
const saves = ref(0)
async function save(value: string) {
  if (!(await settings.saveOptions({ 'tool_price_setting.prices': value })))
    return
  saves.value++
  emit('saved')
}
</script>
<template>
  <ConsoleCard :padded="true">
    <ToolPricingEditor
      :key="`${revision}:${saves}`"
      :value="saved"
      @save="save"
      @dirty="emit('dirty', $event)"
    />
  </ConsoleCard>
</template>
