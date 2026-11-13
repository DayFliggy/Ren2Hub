<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Eye, EyeOff } from 'lucide-vue-next'
import TextInput from '@/components/common/TextInput.vue'
defineOptions({ inheritAttrs: false })
const model = defineModel<string>({ default: '' })
const visible = ref(false)
const { t } = useI18n()
</script>
<template>
  <div class="relative">
    <TextInput
      v-bind="$attrs"
      v-model="model"
      :type="visible ? 'text' : 'password'"
      class="pr-10"
    /><button
      type="button"
      class="absolute right-2 top-2 rounded-md p-1.5 text-[var(--text-secondary)] focus-ring"
      :aria-label="
        t(visible ? 'passwordFeedback.hide' : 'passwordFeedback.show')
      "
      :aria-pressed="visible"
      @click="visible = !visible"
    >
      <EyeOff v-if="visible" :size="18" aria-hidden="true" /><Eye
        v-else
        :size="18"
        aria-hidden="true"
      />
    </button>
  </div>
</template>
