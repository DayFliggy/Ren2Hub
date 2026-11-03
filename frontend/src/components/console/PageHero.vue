<script setup lang="ts">
import { computed } from 'vue'

import Breadcrumb from './Breadcrumb.vue'
import ConsoleTabs, { type TabItem } from '@/components/common/ConsoleTabs.vue'

const tab = defineModel<string>('tab', { default: '' })

interface PageHeroBaseProps {
  title: string
  titleAccent?: string
  /** Sits between title and accent, outside the painted mark. */
  titleSeparator?: string
  /** Sits inside the painted mark, ahead of the accent phrase. */
  titleAccentPrefix?: string
  /** Trails the accent, outside the painted mark — e.g. sentence punctuation. */
  titleSuffix?: string
  /** `brush` paints a filled swash; `underline` draws a thin hand-drawn rule. */
  accentVariant?: 'brush' | 'underline'
  crumbs?: string[]
  titleSide?: 'left' | 'right'
}

/* eslint-disable vue/require-default-prop -- defaults weaken this paired prop contract */
type PageHeroProps = PageHeroBaseProps &
  (
    | { tabs: TabItem[]; tabPanelId: string }
    | { tabs?: never; tabPanelId?: never }
  )
/* eslint-enable vue/require-default-prop */

// The prefix default reproduces the original hardcoded "& " accent join, so
// existing callers keep rendering unchanged.
const props = withDefaults(defineProps<PageHeroProps>(), {
  titleAccent: '',
  titleSeparator: ' ',
  titleAccentPrefix: '& ',
  titleSuffix: '',
  accentVariant: 'brush',
  crumbs: () => [],
  titleSide: 'left',
})

const accentClass = computed(() => [
  'brush-highlight',
  'text-[var(--accent-text)]',
  props.accentVariant === 'underline' ? 'brush-highlight--underline' : '',
])
</script>

<template>
  <div class="mb-4 lg:mb-8" data-handdrawn="page-hero">
    <!-- breadcrumb -->
    <Breadcrumb :crumbs="crumbs" />

    <!-- title row -->
    <div
      class="gap-2.5 lg:gap-4"
      :class="
        titleSide === 'right'
          ? 'grid grid-cols-[minmax(0,1fr)_auto] items-start'
          : 'flex flex-wrap items-start justify-between'
      "
      :data-title-side="titleSide"
    >
      <div class="min-w-0" :class="titleSide === 'right' ? 'self-end' : ''">
        <h1
          v-if="titleSide === 'left'"
          class="display-title text-4xl font-bold text-[var(--text-primary)] lg:text-5xl leading-tight"
          :class="{ 'gesture-mark': accentVariant !== 'underline' }"
          :data-accent-variant="accentVariant"
        >
          {{ title
          }}<template v-if="titleAccent"
            >{{
              titleSeparator
            }}<!-- accent phrase carries the painted mark; punctuation stays outside it,
              but never starts a line on its own --><span
              class="whitespace-nowrap"
              ><span :class="accentClass" class="page-hero-accent"
                >{{ titleAccentPrefix }}{{ titleAccent }}</span
              >{{ titleSuffix }}</span
            ></template
          ><template v-else>{{ titleSuffix }}</template>
        </h1>
        <!-- hero metric slot (wallet balance, etc.) -->
        <slot />
      </div>
      <div
        class="flex min-w-0 gap-5"
        :class="
          titleSide === 'right'
            ? 'items-start justify-end text-right'
            : 'items-center'
        "
      >
        <h1
          v-if="titleSide === 'right'"
          class="display-title whitespace-nowrap text-right text-4xl font-bold leading-tight text-[var(--text-primary)] lg:text-5xl"
          :class="{ 'gesture-mark': accentVariant !== 'underline' }"
          :data-accent-variant="accentVariant"
        >
          {{ title
          }}<template v-if="titleAccent"
            >{{ titleSeparator
            }}<span :class="accentClass"
              >{{ titleAccentPrefix }}{{ titleAccent }}</span
            ></template
          >{{ titleSuffix }}
        </h1>
        <!-- right-side actions slot -->
        <slot name="actions" />
        <!-- decorative spot illustration (desktop only) -->
        <div v-if="$slots.art" class="hidden shrink-0 lg:block">
          <slot name="art" />
        </div>
      </div>
    </div>

    <ConsoleTabs
      v-if="tabs?.length"
      v-model="tab"
      :items="tabs"
      :panel-id="tabPanelId!"
      class="mt-3 lg:mt-5"
    />
    <!-- breathing ink line below hero when no tabs own the boundary -->
    <div v-else class="ink-divider mt-4 lg:mt-6" aria-hidden="true" />
  </div>
</template>

<style scoped>
/* A long accent wraps inside itself and stays one em narrower than the
   heading, so the punctuation held beside it still fits on the line. */
.page-hero-accent {
  max-width: calc(100% - 1em);
  white-space: normal;
  overflow-wrap: break-word;
}

:global(html.dark [data-handdrawn='page-hero']) {
  margin-bottom: 20px;
}

:global(html.dark [data-handdrawn='page-hero'] > nav) {
  font-size: 0.75rem;
  letter-spacing: 0.02em;
}

:global(html.dark [data-handdrawn='page-hero'] > nav > span:last-of-type) {
  font-weight: 500;
}

:global(html.dark [data-handdrawn='page-hero'] h1) {
  font-family: var(--font-display);
  font-size: 2.125rem;
  line-height: 1.25;
  letter-spacing: -0.022em;
  font-weight: 600;
  color: var(--text-primary);
}

:global(html.dark [data-handdrawn='page-hero'] > .ink-divider) {
  height: 1px;
  background: linear-gradient(
    90deg,
    rgba(228, 192, 107, 0.34) 0%,
    rgba(226, 232, 245, 0.11) 46%,
    transparent 94%
  );
  opacity: 1;
}

@media (max-width: 1023px) {
  [data-handdrawn='page-hero'] {
    margin-bottom: 1rem;
  }

  [data-handdrawn='page-hero'] [data-title-side='left'] {
    gap: 0.625rem;
  }

  [data-handdrawn='page-hero'] :deep(.console-tabs) {
    margin-top: 0.75rem;
  }

  :global(html.dark [data-handdrawn='page-hero'] h1) {
    font-size: 1.8125rem;
    line-height: 1.25;
    letter-spacing: -0.025em;
  }
}
</style>
