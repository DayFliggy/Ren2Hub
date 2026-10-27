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
  box-shadow: 0 0 0 3px var(--state-focus-layer) !important;
}

/* 触控命中区扩大至 44px（WCAG 规范） */
.console-toggle::after {
  content: '';
  position: absolute;
  inset: -10px -4px;
}
@media (min-width: 640px) {
  .console-toggle::after {
    inset: 0;
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
   日间模式：Desert Ledger（温和暖土 · 纸墨手账质感）
   ═════════════════════════════════════════════════════════════════ */
html:not(.dark) .console-toggle {
  border-radius: var(--sketch-border-radius-sm);
}

html:not(.dark) .console-toggle.is-unchecked {
  background-color: rgba(56, 55, 43, 0.12);
  border-color: var(--pencil-line-soft);
  box-shadow:
    inset 0 1.5px 2.5px rgba(56, 55, 43, 0.1),
    1px 1px 0 var(--pencil-line-faint);
}

html:not(.dark) .console-toggle.is-unchecked:hover:not(.is-disabled) {
  background-color: rgba(56, 55, 43, 0.16);
  border-color: var(--pencil-line);
}

html:not(.dark) .console-toggle.is-checked {
  background-color: var(--accent);
  border-color: var(--pencil-line-strong);
  box-shadow:
    inset 0 1.5px 2.5px rgba(56, 55, 43, 0.22),
    1px 1px 0 var(--pencil-line-faint);
}

html:not(.dark) .console-toggle.is-checked:hover:not(.is-disabled) {
  background-color: var(--accent-hover);
}

/* 日间滑块：纯净天然奶白宣纸色，手账纸张抬升投影与墨线轮廓 */
html:not(.dark) .console-toggle-thumb {
  background-color: var(--surface-solid);
  border-color: var(--pencil-line-soft);
  box-shadow:
    1px 1.5px 0 rgba(56, 55, 43, 0.16),
    0 2px 4px rgba(56, 55, 43, 0.08);
}

html:not(.dark) .console-toggle:hover:not(.is-disabled) .console-toggle-thumb {
  box-shadow:
    1.5px 2px 0 rgba(56, 55, 43, 0.2),
    0 3px 6px rgba(56, 55, 43, 0.12);
}

/* ═════════════════════════════════════════════════════════════════
   夜间模式：One Night（沉静炭蓝 · 精密流光高阶质感）
   ═════════════════════════════════════════════════════════════════ */
html.dark .console-toggle {
  border-radius: 9999px;
}

/* 夜间未激活：深邃下沉炭黑凹槽，冷灰蓝轮廓清晰，拒绝浑浊一片 */
html.dark .console-toggle.is-unchecked {
  background-color: #171b25;
  border-color: rgba(152, 164, 192, 0.28);
  box-shadow: inset 0 2px 4px rgba(0, 0, 0, 0.45);
}

html.dark .console-toggle.is-unchecked:hover:not(.is-disabled) {
  background-color: #1a202d;
  border-color: rgba(152, 164, 192, 0.45);
}

/* 夜间激活：星辉浅金光辉轨道，环绕环境微光晕 */
html.dark .console-toggle.is-checked {
  background-color: var(--accent);
  border-color: rgba(226, 188, 85, 0.85);
  box-shadow:
    0 0 12px rgba(226, 188, 85, 0.38),
    inset 0 1px 1px rgba(255, 255, 255, 0.35);
}

html.dark .console-toggle.is-checked:hover:not(.is-disabled) {
  background-color: var(--accent-hover);
  box-shadow:
    0 0 16px rgba(226, 188, 85, 0.5),
    inset 0 1px 1px rgba(255, 255, 255, 0.45);
}

/* 夜间滑块 — 彻底修复核心对比度缺陷！ */
/* 未激活：冰霜高对比冷银白（绝非暗黑炭灰），表面微光立体饱满 */
html.dark .console-toggle-thumb.thumb-unchecked {
  background-color: #cbd5e1;
  border-color: rgba(255, 255, 255, 0.4);
  box-shadow:
    0 2px 5px rgba(0, 0, 0, 0.45),
    0 1px 2px rgba(0, 0, 0, 0.25);
}

html.dark
  .console-toggle:hover:not(.is-disabled)
  .console-toggle-thumb.thumb-unchecked {
  background-color: #e2e8f0;
  box-shadow:
    0 3px 7px rgba(0, 0, 0, 0.5),
    0 1px 3px rgba(0, 0, 0, 0.3);
}

/* 激活：纯洁高亮亮白（绝非暗黑炭灰），金辉衬托下的莹润晶莹白球 */
html.dark .console-toggle-thumb.thumb-checked {
  background-color: #ffffff;
  border-color: rgba(255, 255, 255, 0.95);
  box-shadow:
    0 2px 5px rgba(0, 0, 0, 0.3),
    0 1px 2px rgba(0, 0, 0, 0.2);
}

html.dark
  .console-toggle:hover:not(.is-disabled)
  .console-toggle-thumb.thumb-checked {
  background-color: #ffffff;
  box-shadow:
    0 3px 8px rgba(0, 0, 0, 0.35),
    0 0 5px rgba(255, 255, 255, 0.6);
}
</style>
