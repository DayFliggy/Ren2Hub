<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  ANNOUNCEMENT_NOTIFY_MODES,
  ANNOUNCEMENT_STATUSES,
  ANNOUNCEMENT_TYPES,
  type AdminAnnouncement,
  type AnnouncementNotifyMode,
  type AnnouncementStatus,
  type AnnouncementType,
  type AnnouncementWriteInput,
} from '@/api/announcements'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import FormField from '@/components/common/FormField.vue'
import MarkdownDocument from '@/components/common/MarkdownDocument.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import TextInput from '@/components/common/TextInput.vue'
import {
  localInputToUnix,
  unixToLocalInput,
} from '@/features/announcements/lib/announcementHelpers'

const props = defineProps<{
  open: boolean
  editing: AdminAnnouncement | null
  saving: boolean
  errorMessage?: string
}>()

const emit = defineEmits<{
  close: []
  submit: [payload: AnnouncementWriteInput]
}>()

const { t } = useI18n()

const contentMode = ref<'edit' | 'preview'>('edit')
const validationError = ref('')

const form = reactive({
  title: '',
  content: '',
  type: 'default' as AnnouncementType,
  status: 'draft' as AnnouncementStatus,
  notify_mode: 'silent' as AnnouncementNotifyMode,
  pinned: false,
  startsAtInput: '',
  endsAtInput: '',
})

const typeOptions = computed(() =>
  ANNOUNCEMENT_TYPES.map((value) => ({
    value,
    label: t(`announcements.types.${value}`),
  }))
)

const statusOptions = computed(() =>
  ANNOUNCEMENT_STATUSES.map((value) => ({
    value,
    label: t(`announcements.statuses.${value}`),
  }))
)

const notifyModeOptions = computed(() =>
  ANNOUNCEMENT_NOTIFY_MODES.map((value) => ({
    value,
    label: t(`announcements.notifyModes.${value}`),
  }))
)

const contentTabs = computed(() => [
  { value: 'edit', label: t('announcements.admin.form.editTab') },
  { value: 'preview', label: t('announcements.admin.form.previewTab') },
])

function resetFromProps() {
  validationError.value = ''
  contentMode.value = 'edit'
  if (props.editing) {
    form.title = props.editing.title
    form.content = props.editing.content
    form.type = props.editing.type
    form.status = props.editing.status
    form.notify_mode = props.editing.notify_mode
    form.pinned = props.editing.pinned
    form.startsAtInput = unixToLocalInput(props.editing.starts_at)
    form.endsAtInput = unixToLocalInput(props.editing.ends_at)
    return
  }
  form.title = ''
  form.content = ''
  form.type = 'default'
  form.status = 'active'
  form.notify_mode = 'silent'
  form.pinned = false
  form.startsAtInput = ''
  form.endsAtInput = ''
}

watch(
  () => [props.open, props.editing?.id] as const,
  ([isOpen]) => {
    if (isOpen) resetFromProps()
  },
  { immediate: true }
)

function handleSubmit() {
  validationError.value = ''
  const title = form.title.trim()
  const content = form.content.trim()
  if (!title) {
    validationError.value = t('announcements.admin.form.titleRequired')
    return
  }
  if ([...title].length > 200) {
    validationError.value = t('announcements.admin.form.titleTooLong')
    return
  }
  if (!content) {
    validationError.value = t('announcements.admin.form.contentRequired')
    return
  }
  if ([...content].length > 20000) {
    validationError.value = t('announcements.admin.form.contentTooLong')
    return
  }

  const startsAt = localInputToUnix(form.startsAtInput)
  const endsAt = localInputToUnix(form.endsAtInput)
  if (startsAt !== null && endsAt !== null && startsAt >= endsAt) {
    validationError.value = t('announcements.admin.form.invalidSchedule')
    return
  }

  emit('submit', {
    title,
    content,
    type: form.type,
    status: form.status,
    notify_mode: form.notify_mode,
    pinned: form.pinned,
    starts_at: startsAt,
    ends_at: endsAt,
  })
}
</script>

<template>
  <ConsoleModal
    :open="open"
    :title="
      editing ? t('announcements.admin.edit') : t('announcements.admin.create')
    "
    size="xl"
    :close-disabled="saving"
    @close="emit('close')"
  >
    <form
      id="announcement-editor-form"
      class="space-y-4"
      data-announcement-form
      @submit.prevent="handleSubmit"
    >
      <ErrorBanner
        v-if="validationError || errorMessage"
        :message="validationError || errorMessage || ''"
      />

      <FormField :label="t('announcements.admin.form.title')" required>
        <TextInput
          id="announcement-title"
          v-model="form.title"
          :placeholder="t('announcements.admin.form.titlePlaceholder')"
          maxlength="200"
          data-announcement-title-input
        />
      </FormField>

      <div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <FormField :label="t('announcements.admin.form.type')">
          <FilterSelect
            v-model="form.type"
            :options="typeOptions"
            :label="t('announcements.admin.form.type')"
            class="w-full"
          />
        </FormField>
        <FormField :label="t('announcements.admin.form.status')">
          <FilterSelect
            v-model="form.status"
            :options="statusOptions"
            :label="t('announcements.admin.form.status')"
            class="w-full"
          />
        </FormField>
        <FormField
          :label="t('announcements.admin.form.notifyMode')"
          :hint="t('announcements.admin.form.notifyModeHint')"
        >
          <FilterSelect
            v-model="form.notify_mode"
            :options="notifyModeOptions"
            :label="t('announcements.admin.form.notifyMode')"
            class="w-full"
          />
        </FormField>
      </div>

      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <FormField
          :label="t('announcements.admin.form.startsAt')"
          :hint="t('announcements.admin.form.startsAtHint')"
        >
          <div class="flex items-center gap-2">
            <input
              id="announcement-starts-at"
              v-model="form.startsAtInput"
              type="datetime-local"
              max="9999-12-31T23:59"
              class="console-input w-full"
            />
            <ConsoleButton
              v-if="form.startsAtInput"
              variant="ghost"
              size="sm"
              @click="form.startsAtInput = ''"
            >
              {{ t('announcements.admin.form.clearTime') }}
            </ConsoleButton>
          </div>
        </FormField>
        <FormField
          :label="t('announcements.admin.form.endsAt')"
          :hint="t('announcements.admin.form.endsAtHint')"
        >
          <div class="flex items-center gap-2">
            <input
              id="announcement-ends-at"
              v-model="form.endsAtInput"
              type="datetime-local"
              max="9999-12-31T23:59"
              class="console-input w-full"
            />
            <ConsoleButton
              v-if="form.endsAtInput"
              variant="ghost"
              size="sm"
              @click="form.endsAtInput = ''"
            >
              {{ t('announcements.admin.form.clearTime') }}
            </ConsoleButton>
          </div>
        </FormField>
      </div>

      <div
        class="flex items-center justify-between gap-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-4 py-3"
      >
        <div class="min-w-0">
          <p class="text-sm font-medium text-[var(--text-primary)]">
            {{ t('announcements.admin.form.pinned') }}
          </p>
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ t('announcements.admin.form.pinnedHint') }}
          </p>
        </div>
        <ConsoleToggle
          v-model="form.pinned"
          :label="t('announcements.admin.form.pinned')"
        />
      </div>

      <div class="space-y-2">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <label
            for="announcement-content"
            class="text-sm font-medium text-[var(--text-primary)]"
          >
            {{ t('announcements.admin.form.content') }}
            <span class="text-[var(--status-danger-text)]">*</span>
          </label>
          <SegmentedToggle
            v-model="contentMode"
            :options="contentTabs"
            :label="t('announcements.admin.form.content')"
          />
        </div>

        <textarea
          v-if="contentMode === 'edit'"
          id="announcement-content"
          v-model="form.content"
          rows="8"
          class="console-input w-full resize-y font-mono text-sm leading-relaxed"
          :placeholder="t('announcements.admin.form.contentPlaceholder')"
          data-announcement-content-input
        />
        <div
          v-else
          class="min-h-48 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-4"
          data-announcement-markdown-preview
        >
          <MarkdownDocument
            v-if="form.content.trim()"
            :markdown="form.content"
          />
          <p v-else class="text-sm text-[var(--text-tertiary)]">
            {{ t('announcements.admin.form.previewEmpty') }}
          </p>
        </div>
      </div>
    </form>

    <template #footer>
      <div class="flex items-center justify-end gap-2.5">
        <ConsoleButton
          variant="secondary"
          :disabled="saving"
          @click="emit('close')"
        >
          {{ t('common.cancel') }}
        </ConsoleButton>
        <ConsoleButton
          type="submit"
          form="announcement-editor-form"
          :loading="saving"
          data-announcement-submit
        >
          {{ t('common.save') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
</template>
