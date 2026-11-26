<script setup lang="ts">
interface Props {
  disabled?: boolean
  label?: string
  size?: 'sm' | 'md'
  loading?: boolean
  id?: string
  name?: string
}

const props = withDefaults(defineProps<Props>(), {
  disabled: false,
  label: '',
  size: 'md',
  loading: false,
  id: undefined,
  name: undefined,
})

const model = defineModel<boolean>({ default: false })

function toggle(event: MouseEvent) {
  if (props.disabled || props.loading) {
    event.preventDefault()
    return
  }
  model.value = !model.value
}
</script>

<template>
  <button
    :id="id"
    :name="name"
    type="button"
    role="switch"
    :aria-checked="model"
    :aria-label="label || undefined"
    :aria-disabled="disabled || loading"
    :disabled="disabled || loading"
    data-handdrawn="toggle"
    :data-state="model ? 'checked' : 'unchecked'"
    :data-size="size"
    class="console-toggle relative inline-flex shrink-0 items-center select-none transition-all focus-visible:outline-hidden"
    :class="[
      size === 'sm' ? 'h-5 w-9' : 'h-6 w-11',
      model ? 'is-checked' : 'is-unchecked',
      disabled ? 'is-disabled' : '',
      loading ? 'is-loading' : '',
    ]"
    @click="toggle"
  >
    <span
      class="console-toggle-thumb absolute rounded-full flex items-center justify-center pointer-events-none"
      :class="[
        size === 'sm' ? 'h-4 w-4' : 'h-5 w-5',
        model ? 'thumb-checked' : 'thumb-unchecked',
      ]"
      :style="{
        transform: model
          ? // Track width - thumb width - both 2px insets, in rem like the
            // track so the travel stays symmetric as the console root grows.
            `translateX(${size === 'sm' ? 'calc(1.25rem - 4px)' : 'calc(1.5rem - 4px)'})`
          : 'translateX(0)',
      }"
    >
      <svg
        v-if="loading"
        class="animate-spin text-current opacity-70"
        :class="size === 'sm' ? 'h-2.5 w-2.5' : 'h-3 w-3'"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        viewBox="0 0 24 24"
      >
        <circle
          class="opacity-25"
          cx="12"
          cy="12"
          r="10"
          stroke="currentColor"
          stroke-width="3"
        />
        <path
          class="opacity-75"
          fill="currentColor"
          d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
        />
      </svg>
    </span>
  </button>
</template>

<style scoped>
.console-toggle {
  padding: 0;
  cursor: pointer;
  border-width: 1.5px;
  border-style: solid;
  transition:
    background-color 0.22s ease,
    border-color 0.22s ease,
    box-shadow 0.22s ease,
    transform 0.15s ease;
  will-change: transform;
}

.console-toggle:active:not(.is-disabled) {
  transform: scale(0.96);
}

.console-toggle.is-disabled {
  cursor: not-allowed;
  opacity: 0.42;
  pointer-events: none;
}

.console-toggle:focus-visible {
  outline: none;
}

/* Keep the compact track while reserving a centred 44px touch target. */
.console-toggle::after {
  content: '';
  position: absolute;
  top: 50%;
  left: 50%;
  width: 100%;
  height: 100%;
  min-width: 44px;
  min-height: 44px;
  transform: translate(-50%, -50%);
}
@media (min-width: 1024px) and (pointer: fine) {
  .console-toggle::after {
    min-width: 0;
    min-height: 0;
  }
}

.console-toggle-thumb {
  top: 2px;
  left: 2px;
  border-radius: 9999px;
  border-width: 1px;
  border-style: solid;
  transition:
    transform 0.26s cubic-bezier(0.16, 1, 0.3, 1),
    background-color 0.2s ease,
    border-color 0.2s ease,
    box-shadow 0.2s ease;
  will-change: transform;
}

/* ═════════════════════════════════════════════════════════════════
   语义令牌驱动（日间 Desert Ledger / 夜间 B1 Studio 由 tokens.css 统管）
   ═════════════════════════════════════════════════════════════════ */
html:not(.dark) .console-toggle {
  border-radius: var(--sketch-border-radius-sm);
}

html.dark .console-toggle {
  border-radius: 9999px;
}

.console-toggle.is-unchecked {
  background-color: var(--toggle-track-off);
  border-color: var(--toggle-track-off-border);
  box-shadow: var(--toggle-track-off-shadow);
}

.console-toggle.is-unchecked:hover:not(.is-disabled) {
  background-color: var(--toggle-track-off-hover);
  border-color: var(--toggle-track-off-border-hover);
}

.console-toggle.is-checked {
  background-color: var(--accent);
  border-color: var(--toggle-track-on-border);
  box-shadow: var(--toggle-track-on-shadow);
}

.console-toggle.is-checked:hover:not(.is-disabled) {
  background-color: var(--accent-hover);
  box-shadow: var(--toggle-track-on-shadow-hover);
}

.console-toggle-thumb.thumb-unchecked {
  background-color: var(--toggle-thumb-off);
  border-color: var(--toggle-thumb-off-border);
  box-shadow: var(--toggle-thumb-off-shadow);
}

.console-toggle:hover:not(.is-disabled) .console-toggle-thumb.thumb-unchecked {
  background-color: var(--toggle-thumb-off-hover);
  box-shadow: var(--toggle-thumb-off-shadow-hover);
}

.console-toggle-thumb.thumb-checked {
  background-color: var(--toggle-thumb-on);
  border-color: var(--toggle-thumb-on-border);
  box-shadow: var(--toggle-thumb-on-shadow);
}

.console-toggle:hover:not(.is-disabled) .console-toggle-thumb.thumb-checked {
  background-color: var(--toggle-thumb-on);
  box-shadow: var(--toggle-thumb-on-shadow-hover);
}

/* 焦点阴影替换轨道阴影，规则放在末尾且选择器权重高于 checked:hover。 */
html:not(.dark)
  .console-toggle:is(.is-checked, .is-unchecked):focus-visible:not(
    .is-disabled
  ) {
  box-shadow: 0 0 0 3px var(--state-focus-layer);
}

html.dark
  .console-toggle:is(.is-checked, .is-unchecked):focus-visible:not(
    .is-disabled
  ) {
  box-shadow: var(--focus-halo);
}
</style>
