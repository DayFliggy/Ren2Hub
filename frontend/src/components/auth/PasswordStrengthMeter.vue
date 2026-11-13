<script setup lang="ts">
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  evaluatePasswordStrength,
  getPasswordConfirmationState,
} from '@/utils/password-strength'
const props = defineProps<{
  password: string
  confirmation?: string
  id?: string
}>()
const { t } = useI18n()
const generatedId = useId()
const result = computed(() => evaluatePasswordStrength(props.password))
const confirmationState = computed(() =>
  getPasswordConfirmationState(props.password, props.confirmation ?? '')
)
const names = [
  'strengthEmpty',
  'strengthWeak',
  'strengthFair',
  'strengthGood',
  'strengthStrong',
]
const tone = computed(() =>
  result.value.score <= 1
    ? 'var(--status-danger)'
    : result.value.score === 2
      ? 'var(--status-warning)'
      : 'var(--accent)'
)
</script>
<template>
  <div :id="id || generatedId" class="sibling-space-y-2 text-xs">
    <div
      v-if="password"
      class="flex items-center gap-3"
      role="status"
      aria-live="polite"
    >
      <div class="flex flex-1 gap-1.5" aria-hidden="true">
        <span
          v-for="i in 4"
          :key="i"
          class="h-1.5 flex-1 rounded-full"
          :style="{
            background: i <= result.score ? tone : 'var(--border-subtle)',
          }"
        />
      </div>
      <span :style="{ color: tone }">{{
        t(`passwordFeedback.${names[result.score]}`)
      }}</span>
    </div>
    <p class="text-[var(--text-secondary)]">
      {{ t('passwordFeedback.passwordHint') }}
    </p>
    <p
      v-if="password && !result.meetsRequirements"
      class="text-[var(--status-danger)]"
    >
      {{ t('passwordFeedback.passwordLength') }}
    </p>
    <p
      v-if="confirmationState !== 'empty'"
      role="status"
      :class="
        confirmationState === 'match'
          ? 'text-[var(--status-success)]'
          : 'text-[var(--status-danger)]'
      "
    >
      {{
        t(
          confirmationState === 'match'
            ? 'passwordFeedback.passwordMatch'
            : 'passwordFeedback.passwordMismatch'
        )
      }}
    </p>
  </div>
</template>
