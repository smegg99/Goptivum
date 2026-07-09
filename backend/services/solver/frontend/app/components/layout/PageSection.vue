<!-- app/components/layout/PageSection.vue -->

<script setup lang="ts">
defineProps<{ title: string }>()
</script>

<template>
  <section class="page-section">
    <div class="page-section__header d-flex align-center ga-3">
      <h2 class="text-overline text-medium-emphasis font-weight-bold ma-0">
        {{ title }}
      </h2>
      <v-spacer />
      <slot name="header-actions" />
    </div>
    <v-divider class="page-section__divider" />
    <div class="page-section__content">
      <slot />
    </div>
  </section>
</template>
