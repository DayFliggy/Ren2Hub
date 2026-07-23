<script setup lang="ts">
/**
 * Theme-aware brand mark: same glyph as /favicon.svg, but resolved through
 * semantic tokens (accent tile + contrast glyph) so it reads on any surface
 * in both themes. Size/shape are driven by the caller's class.
 */
</script>

<template>
  <svg viewBox="0 0 32 32" aria-hidden="true" class="shrink-0">
    <rect width="32" height="32" rx="8" style="fill: var(--accent)" />
    <path
      d="M9 22 15 10h3.4l-6 12H9zm7.6 0 6-12H26l-6 12h-3.4z"
      style="fill: var(--accent-contrast)"
    />
  </svg>
</template>
