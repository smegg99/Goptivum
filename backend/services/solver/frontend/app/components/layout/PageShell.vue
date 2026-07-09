<!-- app/components/layout/PageShell.vue -->

<script setup lang="ts">
withDefaults(defineProps<{
  padded?: boolean
}>(), {
  padded: true,
})
</script>

<template>
  <div class="page-shell">
    <main class="page-shell__main">
      <div
        class="page-shell__content"
        :class="{ 'page-shell__content--padded': padded }"
      >
        <slot />
      </div>
    </main>
  </div>
</template>
